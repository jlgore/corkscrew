package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// DynamicAzureProvider extends the regular Azure provider with dynamic scanner capabilities
type DynamicAzureProvider struct {
	*AzureProvider
	dynamicLoader *DynamicScannerLoader
	scannerDir    string
}

// NewDynamicAzureProvider creates a new Azure provider with dynamic scanner support
func NewDynamicAzureProvider(scannerDir string) (*DynamicAzureProvider, error) {
	// Create the base Azure provider
	baseProvider := NewAzureProvider()
	
	// Initialize the dynamic loader
	loader, err := NewDynamicScannerLoader(scannerDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic loader: %w", err)
	}

	provider := &DynamicAzureProvider{
		AzureProvider: baseProvider,
		dynamicLoader: loader,
		scannerDir:    scannerDir,
	}

	return provider, nil
}

// InitializeDynamicScanners loads all available scanners and starts watching for updates
func (p *DynamicAzureProvider) InitializeDynamicScanners() error {
	log.Printf("Initializing dynamic scanners from directory: %s", p.scannerDir)

	// Load all scanner plugins
	pattern := filepath.Join(p.scannerDir, "*.so")
	if err := p.dynamicLoader.LoadScanners(pattern); err != nil {
		log.Printf("Warning: Failed to load some scanners: %v", err)
		// Continue even if some scanners fail to load
	}

	// Start watching for updates
	if err := p.dynamicLoader.WatchForUpdates(p.scannerDir); err != nil {
		log.Printf("Warning: Failed to start file watcher: %v", err)
		// Continue without hot-reload capability
	}

	// Log loaded scanners
	services := p.dynamicLoader.GetLoadedServices()
	log.Printf("Loaded %d dynamic scanners: %v", len(services), services)

	return nil
}

// ScanWithDynamicScanners uses dynamic scanners if available, falls back to base implementation
func (p *DynamicAzureProvider) ScanWithDynamicScanners(ctx context.Context, config *pb.ScanServiceRequest) ([]*pb.Resource, error) {
	if p.dynamicLoader == nil {
		// Fall back to base implementation - convert to BatchScanRequest
		batchReq := &pb.BatchScanRequest{
			Services: config.ResourceTypes,
			Filters:  config.Options,
		}
		response, err := p.AzureProvider.BatchScan(ctx, batchReq)
		if err != nil {
			return nil, err
		}
		return response.Resources, nil
	}

	// Extract Azure credentials and subscription ID
	credential, subscriptionID, err := p.extractCredentials(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to extract credentials: %w", err)
	}

	// Prepare filters from config options
	filters := make(map[string]string)
	if config.Region != "" {
		filters["location"] = config.Region
	}
	
	// Convert options to filters
	for key, value := range config.Options {
		filters[key] = value
	}

	var allResources []*pb.Resource

	// Check if specific resource types are requested
	if len(config.ResourceTypes) > 0 {
		// Scan specific resource types
		for _, resourceType := range config.ResourceTypes {
			// Extract service name from resource type (e.g., Microsoft.Compute/virtualMachines -> compute)
			service := p.extractServiceFromResourceType(resourceType)
			
			// Try dynamic scanner first
			if resources, err := p.scanServiceWithDynamicLoader(ctx, service, credential, subscriptionID, filters); err == nil {
				allResources = append(allResources, resources...)
				log.Printf("Scanned %d resources for service %s using dynamic scanner", len(resources), service)
			} else {
				log.Printf("Dynamic scanner failed for service %s, falling back to base implementation: %v", service, err)
				// Fall back to base implementation for this service
				if fallbackResources, fallbackErr := p.scanServiceWithBase(ctx, service, config); fallbackErr == nil {
					allResources = append(allResources, fallbackResources...)
				}
			}
		}
	} else {
		// Scan all services using dynamic scanners
		if scannerResults, err := p.dynamicLoader.ScanAllServices(ctx, credential, subscriptionID, filters); err == nil {
			for service, resources := range scannerResults {
				allResources = append(allResources, resources...)
				log.Printf("Scanned %d resources for service %s", len(resources), service)
			}
		} else {
			log.Printf("Dynamic scanning failed, falling back to base implementation: %v", err)
			// Fall back to base implementation
			batchReq := &pb.BatchScanRequest{
				Services: []string{"all"},
				Filters:  config.Options,
			}
			response, err := p.AzureProvider.BatchScan(ctx, batchReq)
			if err != nil {
				return nil, err
			}
			return response.Resources, nil
		}
	}

	return allResources, nil
}

// scanServiceWithDynamicLoader uses the dynamic loader to scan a specific service
func (p *DynamicAzureProvider) scanServiceWithDynamicLoader(ctx context.Context, service string, credential azcore.TokenCredential, subscriptionID string, filters map[string]string) ([]*pb.Resource, error) {
	return p.dynamicLoader.ScanService(ctx, service, credential, subscriptionID, filters)
}

// scanServiceWithBase falls back to the base implementation for a specific service
func (p *DynamicAzureProvider) scanServiceWithBase(ctx context.Context, service string, config *pb.ScanServiceRequest) ([]*pb.Resource, error) {
	// Create a config for just this service
	serviceConfig := &pb.ScanServiceRequest{
		Region:        config.Region,
		Service:       service,
		ResourceTypes: config.ResourceTypes,
		Filters:       config.Filters,
	}

	batchReq := &pb.BatchScanRequest{
		Services: []string{service},
		Filters:  serviceConfig.Options,
	}
	response, err := p.AzureProvider.BatchScan(ctx, batchReq)
	if err != nil {
		return nil, err
	}
	return response.Resources, nil
}

// extractCredentials extracts Azure credentials and subscription ID from the scan config
func (p *DynamicAzureProvider) extractCredentials(ctx context.Context, config *pb.ScanServiceRequest) (azcore.TokenCredential, string, error) {
	// For now, create new credentials using the same method as the base provider
	// This could be optimized to reuse the provider's credentials in the future
	
	// Create default Azure credentials
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Azure credentials: %w", err)
	}

	// Get subscription ID from environment or Azure CLI
	subscriptionID := getAzureCliSubscription()
	if subscriptionID == "" {
		return nil, "", fmt.Errorf("subscription ID not available - set AZURE_SUBSCRIPTION_ID or run 'az login'")
	}

	return credential, subscriptionID, nil
}

// extractServiceFromResourceType extracts service name from Azure resource type
func (p *DynamicAzureProvider) extractServiceFromResourceType(resourceType string) string {
	// Microsoft.Compute/virtualMachines -> compute
	parts := strings.Split(resourceType, "/")
	if len(parts) > 0 {
		provider := parts[0]
		if strings.HasPrefix(provider, "Microsoft.") {
			return strings.ToLower(strings.TrimPrefix(provider, "Microsoft."))
		}
	}
	return "unknown"
}

// GetDynamicScannerStatus returns status information about loaded scanners
func (p *DynamicAzureProvider) GetDynamicScannerStatus() map[string]interface{} {
	if p.dynamicLoader == nil {
		return map[string]interface{}{
			"enabled": false,
			"error":   "dynamic loader not initialized",
		}
	}

	status := p.dynamicLoader.GetHealthStatus()
	status["enabled"] = true
	status["loaded_services"] = p.dynamicLoader.GetLoadedServices()
	
	// Add metadata for each scanner
	metadata := p.dynamicLoader.GetAllScannerMetadata()
	status["scanner_metadata"] = metadata

	return status
}

// RefreshDynamicScanners manually refreshes all loaded scanners
func (p *DynamicAzureProvider) RefreshDynamicScanners() error {
	if p.dynamicLoader == nil {
		return fmt.Errorf("dynamic loader not initialized")
	}

	return p.dynamicLoader.RefreshScanners()
}

// ValidateDynamicScanner validates a scanner plugin before loading
func (p *DynamicAzureProvider) ValidateDynamicScanner(filePath string) error {
	if p.dynamicLoader == nil {
		return fmt.Errorf("dynamic loader not initialized")
	}

	return p.dynamicLoader.ValidateScanner(filePath)
}

// Stop gracefully shuts down the dynamic provider
func (p *DynamicAzureProvider) Stop() error {
	if p.dynamicLoader != nil {
		return p.dynamicLoader.Stop()
	}
	return nil
}

// Example usage function for demonstration
func ExampleDynamicProviderUsage() {
	// Initialize the dynamic provider
	provider, err := NewDynamicAzureProvider("/path/to/generated/scanners")
	if err != nil {
		log.Fatalf("Failed to create dynamic provider: %v", err)
	}

	// Initialize dynamic scanners
	if err := provider.InitializeDynamicScanners(); err != nil {
		log.Printf("Warning: Failed to initialize dynamic scanners: %v", err)
	}

	// Defer cleanup
	defer func() {
		if err := provider.Stop(); err != nil {
			log.Printf("Error stopping dynamic provider: %v", err)
		}
	}()

	// Create a scan configuration
	config := &pb.ScanServiceRequest{
		Region:        "eastus",
		ResourceTypes: []string{"Microsoft.Compute/virtualMachines", "Microsoft.Storage/storageAccounts", "Microsoft.Network/virtualNetworks"},
		Filters:       map[string]string{},
	}

	// Perform the scan
	ctx := context.Background()
	resources, err := provider.ScanWithDynamicScanners(ctx, config)
	if err != nil {
		log.Printf("Scan failed: %v", err)
		return
	}

	log.Printf("Scanned %d total resources", len(resources))

	// Get status information
	status := provider.GetDynamicScannerStatus()
	log.Printf("Dynamic scanner status: %+v", status)
}
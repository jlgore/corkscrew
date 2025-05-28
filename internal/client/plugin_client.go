package client

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

// PluginManager manages both cloud provider plugins and sophisticated scanner plugins
type PluginManager struct {
	mu                 sync.RWMutex
	clients            map[string]*plugin.Client
	providers          map[string]shared.CloudProvider
	scanners           map[string]shared.Scanner
	scannerClients     map[string]*plugin.Client
	scannerInitialized map[string]bool
	pluginDir          string
	debug              bool
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(pluginDir string) *PluginManager {
	return &PluginManager{
		clients:            make(map[string]*plugin.Client),
		providers:          make(map[string]shared.CloudProvider),
		scanners:           make(map[string]shared.Scanner),
		scannerClients:     make(map[string]*plugin.Client),
		scannerInitialized: make(map[string]bool),
		pluginDir:          pluginDir,
		debug:              false,
	}
}

// SetDebug enables or disables debug logging
func (pm *PluginManager) SetDebug(debug bool) {
	pm.debug = debug
}

// LoadProvider loads a cloud provider plugin
func (pm *PluginManager) LoadProvider(providerName string) (shared.CloudProvider, error) {
	pm.mu.RLock()
	if provider, exists := pm.providers[providerName]; exists {
		pm.mu.RUnlock()
		return provider, nil
	}
	pm.mu.RUnlock()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if provider, exists := pm.providers[providerName]; exists {
		return provider, nil
	}

	// Build plugin path
	pluginPath := filepath.Join(pm.pluginDir, fmt.Sprintf("corkscrew-%s", providerName))
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("plugin not found: %s", pluginPath)
	}

	// Create plugin client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins:         shared.PluginMap,
		Cmd:             exec.Command(pluginPath),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolNetRPC, plugin.ProtocolGRPC},
		SyncStdout:   os.Stdout,
		SyncStderr:   os.Stderr,
		StartTimeout: 5 * time.Minute, // Allow 5 minutes for plugin startup/discovery
	})

	// Connect via gRPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to create plugin client: %w", err)
	}

	// Get the provider
	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense provider: %w", err)
	}

	provider := raw.(shared.CloudProvider)
	pm.clients[providerName] = client
	pm.providers[providerName] = provider

	return provider, nil
}

// LoadScannerPlugin loads a sophisticated scanner plugin for a specific service
func (pm *PluginManager) LoadScannerPlugin(ctx context.Context, serviceName, region string) (shared.Scanner, error) {
	pm.mu.RLock()
	if scanner, exists := pm.scanners[serviceName]; exists && pm.scannerInitialized[serviceName] {
		pm.mu.RUnlock()
		return scanner, nil
	}
	pm.mu.RUnlock()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if scanner, exists := pm.scanners[serviceName]; exists && pm.scannerInitialized[serviceName] {
		return scanner, nil
	}

	// Build plugin path
	pluginPath := filepath.Join(pm.pluginDir, fmt.Sprintf("corkscrew-%s", serviceName))
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("scanner plugin not found: %s", pluginPath)
	}

	if pm.debug {
		fmt.Printf("🔌 Loading sophisticated scanner plugin: %s\n", pluginPath)
	}

	// Create plugin client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  shared.HandshakeConfig,
		Plugins:          shared.PluginMap,
		Cmd:              exec.Command(pluginPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		SyncStdout:       os.Stdout,
		SyncStderr:       os.Stderr,
		StartTimeout:     30 * time.Second,
	})

	// Connect via gRPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to create scanner plugin client: %w", err)
	}

	// Get the scanner
	raw, err := rpcClient.Dispense("scanner")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense scanner: %w", err)
	}

	scanner := raw.(shared.Scanner)

	// Initialize the scanner with region
	if initializer, ok := scanner.(interface {
		Initialize(ctx context.Context, region string) error
	}); ok {
		if err := initializer.Initialize(ctx, region); err != nil {
			client.Kill()
			return nil, fmt.Errorf("failed to initialize scanner: %w", err)
		}
	}

	pm.scannerClients[serviceName] = client
	pm.scanners[serviceName] = scanner
	pm.scannerInitialized[serviceName] = true

	if pm.debug {
		fmt.Printf("✅ Successfully loaded and initialized sophisticated scanner for %s\n", serviceName)
	}

	return scanner, nil
}

// GetScannerPlugin returns an already loaded sophisticated scanner plugin
func (pm *PluginManager) GetScannerPlugin(serviceName string) (shared.Scanner, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	scanner, exists := pm.scanners[serviceName]
	return scanner, exists && pm.scannerInitialized[serviceName]
}

// ScanServiceWithPlugin performs a scan using a sophisticated scanner plugin
func (pm *PluginManager) ScanServiceWithPlugin(ctx context.Context, serviceName, region string, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	scanner, err := pm.LoadScannerPlugin(ctx, serviceName, region)
	if err != nil {
		return nil, fmt.Errorf("failed to load sophisticated scanner for %s: %w", serviceName, err)
	}

	return scanner.Scan(ctx, req)
}

// StreamScanServiceWithPlugin performs a streaming scan using a sophisticated scanner plugin
func (pm *PluginManager) StreamScanServiceWithPlugin(ctx context.Context, serviceName, region string, req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	scanner, err := pm.LoadScannerPlugin(ctx, serviceName, region)
	if err != nil {
		return fmt.Errorf("failed to load sophisticated scanner for %s: %w", serviceName, err)
	}

	return scanner.StreamScan(req, stream)
}

// GetScannerPluginInfo gets information about a sophisticated scanner plugin
func (pm *PluginManager) GetScannerPluginInfo(ctx context.Context, serviceName string) (*pb.ServiceInfoResponse, error) {
	scanner, exists := pm.GetScannerPlugin(serviceName)
	if !exists {
		return nil, fmt.Errorf("sophisticated scanner %s not loaded", serviceName)
	}

	return scanner.GetServiceInfo(ctx, &pb.Empty{})
}

// BatchScanWithPlugins performs batch scanning using sophisticated scanner plugins
func (pm *PluginManager) BatchScanWithPlugins(ctx context.Context, services []string, region string, filters map[string]string) (*pb.BatchScanResponse, error) {
	start := time.Now()
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
	}
	var allResources []*pb.Resource

	for _, serviceName := range services {
		if pm.debug {
			fmt.Printf("🔍 Batch scanning service with plugin: %s\n", serviceName)
		}

		scanner, err := pm.LoadScannerPlugin(ctx, serviceName, region)
		if err != nil {
			if pm.debug {
				fmt.Printf("⚠️  Failed to load sophisticated scanner for %s: %v\n", serviceName, err)
			}
			continue
		}

		// Create scan request
		req := &pb.ScanRequest{
			Region:  region,
			Options: filters,
		}

		resp, err := scanner.Scan(ctx, req)
		if err != nil {
			if pm.debug {
				fmt.Printf("⚠️  Failed to scan %s with plugin: %v\n", serviceName, err)
			}
			stats.FailedResources++
			continue
		}

		if resp.Error != "" {
			if pm.debug {
				fmt.Printf("⚠️  Scanner plugin %s returned error: %s\n", serviceName, resp.Error)
			}
			stats.FailedResources++
			continue
		}

		allResources = append(allResources, resp.Resources...)

		// Merge resource counts
		for resourceType, count := range resp.Stats.ResourceCounts {
			stats.ResourceCounts[fmt.Sprintf("%s_%s", serviceName, resourceType)] = count
		}

		if pm.debug {
			fmt.Printf("✅ %s plugin: found %d resources\n", serviceName, len(resp.Resources))
		}
	}

	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(start).Milliseconds()

	return &pb.BatchScanResponse{
		Resources: allResources,
		Stats:     stats,
	}, nil
}

// ListLoadedScannerPlugins returns a list of currently loaded sophisticated scanner plugins
func (pm *PluginManager) ListLoadedScannerPlugins() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var services []string
	for serviceName, initialized := range pm.scannerInitialized {
		if initialized {
			services = append(services, serviceName)
		}
	}
	return services
}

// UnloadScannerPlugin unloads a specific sophisticated scanner plugin
func (pm *PluginManager) UnloadScannerPlugin(serviceName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if client, exists := pm.scannerClients[serviceName]; exists {
		if pm.debug {
			fmt.Printf("🔌 Unloading sophisticated scanner plugin: %s\n", serviceName)
		}
		client.Kill()
		delete(pm.scannerClients, serviceName)
		delete(pm.scanners, serviceName)
		delete(pm.scannerInitialized, serviceName)
	}
}

// InitializeProvider initializes a provider with configuration
func (pm *PluginManager) InitializeProvider(ctx context.Context, providerName string, config map[string]string, cacheDir string) error {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return err
	}

	req := &pb.InitializeRequest{
		Provider: providerName,
		Config:   config,
		CacheDir: cacheDir,
	}

	resp, err := provider.Initialize(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to initialize provider %s: %w", providerName, err)
	}

	if !resp.Success {
		return fmt.Errorf("provider initialization failed: %s", resp.Error)
	}

	return nil
}

// DiscoverServices discovers available services for a provider
func (pm *PluginManager) DiscoverServices(ctx context.Context, providerName string, forceRefresh bool) (*pb.DiscoverServicesResponse, error) {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return nil, err
	}

	req := &pb.DiscoverServicesRequest{
		ForceRefresh: forceRefresh,
	}

	return provider.DiscoverServices(ctx, req)
}

// GenerateServiceScanners generates scanners for specified services
func (pm *PluginManager) GenerateServiceScanners(ctx context.Context, providerName string, services []string, generateAll bool) (*pb.GenerateScannersResponse, error) {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return nil, err
	}

	req := &pb.GenerateScannersRequest{
		Services:    services,
		GenerateAll: generateAll,
	}

	return provider.GenerateServiceScanners(ctx, req)
}

// ListResources lists resources of a specific type
func (pm *PluginManager) ListResources(ctx context.Context, providerName, service, resourceType, region string, filters map[string]string) (*pb.ListResourcesResponse, error) {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return nil, err
	}

	req := &pb.ListResourcesRequest{
		Service:      service,
		ResourceType: resourceType,
		Region:       region,
		Filters:      filters,
	}

	return provider.ListResources(ctx, req)
}

// DescribeResource gets detailed information about a specific resource
func (pm *PluginManager) DescribeResource(ctx context.Context, providerName string, resourceRef *pb.ResourceRef, includeRelationships, includeTags bool) (*pb.DescribeResourceResponse, error) {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return nil, err
	}

	req := &pb.DescribeResourceRequest{
		ResourceRef:          resourceRef,
		IncludeRelationships: includeRelationships,
		IncludeTags:          includeTags,
	}

	return provider.DescribeResource(ctx, req)
}

// BatchScan performs batch scanning across multiple services
func (pm *PluginManager) BatchScan(ctx context.Context, providerName string, services, resourceTypes []string, region string, filters map[string]string, includeRelationships bool, concurrency int32) (*pb.BatchScanResponse, error) {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return nil, err
	}

	req := &pb.BatchScanRequest{
		Services:             services,
		ResourceTypes:        resourceTypes,
		Region:               region,
		Filters:              filters,
		IncludeRelationships: includeRelationships,
		Concurrency:          concurrency,
	}

	return provider.BatchScan(ctx, req)
}

// GetSchemas gets SQL schemas for specified services
func (pm *PluginManager) GetSchemas(ctx context.Context, providerName string, services []string, format string) (*pb.SchemaResponse, error) {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return nil, err
	}

	req := &pb.GetSchemasRequest{
		Services: services,
		Format:   format,
	}

	return provider.GetSchemas(ctx, req)
}

// GetSchemasFromPlugins gets schemas from sophisticated scanner plugins
func (pm *PluginManager) GetSchemasFromPlugins(ctx context.Context, services []string) (*pb.SchemaResponse, error) {
	var allSchemas []*pb.Schema

	for _, serviceName := range services {
		scanner, err := pm.LoadScannerPlugin(ctx, serviceName, "us-east-1") // Default region for schema queries
		if err != nil {
			if pm.debug {
				fmt.Printf("⚠️  Failed to load sophisticated scanner for %s: %v\n", serviceName, err)
			}
			continue
		}

		resp, err := scanner.GetSchemas(ctx, &pb.Empty{})
		if err != nil {
			if pm.debug {
				fmt.Printf("⚠️  Failed to get schemas from %s plugin: %v\n", serviceName, err)
			}
			continue
		}

		allSchemas = append(allSchemas, resp.Schemas...)
	}

	return &pb.SchemaResponse{Schemas: allSchemas}, nil
}

// GetProviderInfo gets information about a provider
func (pm *PluginManager) GetProviderInfo(ctx context.Context, providerName string) (*pb.ProviderInfoResponse, error) {
	provider, err := pm.LoadProvider(providerName)
	if err != nil {
		return nil, err
	}

	return provider.GetProviderInfo(ctx, &pb.Empty{})
}

// UnloadProvider unloads a provider plugin
func (pm *PluginManager) UnloadProvider(providerName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if client, exists := pm.clients[providerName]; exists {
		client.Kill()
		delete(pm.clients, providerName)
		delete(pm.providers, providerName)
	}
}

// Shutdown shuts down all loaded plugins
func (pm *PluginManager) Shutdown() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.debug {
		fmt.Printf("🔌 Shutting down %d provider clients and %d scanner clients\n", len(pm.clients), len(pm.scannerClients))
	}

	// Shutdown provider clients
	for _, client := range pm.clients {
		client.Kill()
	}

	// Shutdown scanner clients
	for serviceName, client := range pm.scannerClients {
		if pm.debug {
			fmt.Printf("🔌 Killing sophisticated scanner client: %s\n", serviceName)
		}
		client.Kill()
	}

	pm.clients = make(map[string]*plugin.Client)
	pm.providers = make(map[string]shared.CloudProvider)
	pm.scanners = make(map[string]shared.Scanner)
	pm.scannerClients = make(map[string]*plugin.Client)
	pm.scannerInitialized = make(map[string]bool)

	if pm.debug {
		fmt.Printf("✅ All plugin clients shut down\n")
	}
}

// ListLoadedProviders returns a list of loaded provider names
func (pm *PluginManager) ListLoadedProviders() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	providers := make([]string, 0, len(pm.providers))
	for provider := range pm.providers {
		providers = append(providers, provider)
	}
	return providers
}

// GetStats returns statistics about loaded plugins
func (pm *PluginManager) GetStats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return map[string]interface{}{
		"provider_clients":       len(pm.clients),
		"scanner_clients":        len(pm.scannerClients),
		"loaded_providers":       pm.ListLoadedProviders(),
		"loaded_scanner_plugins": pm.ListLoadedScannerPlugins(),
		"plugin_directory":       pm.pluginDir,
	}
}

// Legacy methods for backward compatibility
// TODO: Remove once migration is complete

// LoadScanner loads a scanner (legacy method)
func (pm *PluginManager) LoadScanner(service string) (shared.Scanner, error) {
	// For now, assume it's an AWS service
	provider, err := pm.LoadProvider("aws")
	if err != nil {
		return nil, err
	}

	// Return a wrapper that implements the legacy Scanner interface
	return &legacyScanner{provider: provider, service: service}, nil
}

// ScanService scans a service (legacy method)
func (pm *PluginManager) ScanService(ctx context.Context, service string, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	scanner, err := pm.LoadScanner(service)
	if err != nil {
		return nil, err
	}

	return scanner.Scan(ctx, req)
}

// GetScannerInfo gets scanner info (legacy method)
func (pm *PluginManager) GetScannerInfo(service string) (*pb.ServiceInfoResponse, error) {
	scanner, err := pm.LoadScanner(service)
	if err != nil {
		return nil, err
	}

	return scanner.GetServiceInfo(context.Background(), &pb.Empty{})
}

// GetServiceSchemas gets service schemas (legacy method)
func (pm *PluginManager) GetServiceSchemas(service string) (*pb.SchemaResponse, error) {
	scanner, err := pm.LoadScanner(service)
	if err != nil {
		return nil, err
	}

	return scanner.GetSchemas(context.Background(), &pb.Empty{})
}

// legacyScanner wraps a CloudProvider to implement the legacy Scanner interface
type legacyScanner struct {
	provider shared.CloudProvider
	service  string
}

func (ls *legacyScanner) Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	// Convert legacy ScanRequest to BatchScanRequest
	batchReq := &pb.BatchScanRequest{
		Services:      []string{ls.service},
		ResourceTypes: req.ResourceTypes,
		Region:        req.Region,
		Filters:       req.Options,
	}

	batchResp, err := ls.provider.BatchScan(ctx, batchReq)
	if err != nil {
		return nil, err
	}

	// Convert BatchScanResponse to legacy ScanResponse
	return &pb.ScanResponse{
		Resources: batchResp.Resources,
		Stats:     batchResp.Stats,
		Metadata: map[string]string{
			"service": ls.service,
		},
	}, nil
}

func (ls *legacyScanner) GetSchemas(ctx context.Context, req *pb.Empty) (*pb.SchemaResponse, error) {
	schemaReq := &pb.GetSchemasRequest{
		Services: []string{ls.service},
		Format:   "sql",
	}

	return ls.provider.GetSchemas(ctx, schemaReq)
}

func (ls *legacyScanner) GetServiceInfo(ctx context.Context, req *pb.Empty) (*pb.ServiceInfoResponse, error) {
	providerInfo, err := ls.provider.GetProviderInfo(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert ProviderInfoResponse to legacy ServiceInfoResponse
	return &pb.ServiceInfoResponse{
		ServiceName:         ls.service,
		Version:             providerInfo.Version,
		SupportedResources:  []string{}, // Would need to be populated from service discovery
		RequiredPermissions: []string{}, // Would need to be populated from service discovery
		Capabilities:        providerInfo.Capabilities,
	}, nil
}

func (ls *legacyScanner) StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	// This would need a proper streaming implementation
	// For now, just do a batch scan and stream the results
	batchReq := &pb.BatchScanRequest{
		Services:      []string{ls.service},
		ResourceTypes: req.ResourceTypes,
		Region:        req.Region,
		Filters:       req.Options,
	}

	batchResp, err := ls.provider.BatchScan(stream.Context(), batchReq)
	if err != nil {
		return err
	}

	for _, resource := range batchResp.Resources {
		if err := stream.Send(resource); err != nil {
			return err
		}
	}

	return nil
}

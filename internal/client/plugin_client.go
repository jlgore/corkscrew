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

// PluginManager manages cloud provider plugins
type PluginManager struct {
	mu        sync.RWMutex
	clients   map[string]*plugin.Client
	providers map[string]shared.CloudProvider
	pluginDir string
	debug     bool
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(pluginDir string) *PluginManager {
	return &PluginManager{
		clients:   make(map[string]*plugin.Client),
		providers: make(map[string]shared.CloudProvider),
		pluginDir: pluginDir,
		debug:     false,
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

// GetSchemasFromPlugins is deprecated - use CloudProvider.GetSchemas instead
func (pm *PluginManager) GetSchemasFromPlugins(ctx context.Context, services []string) (*pb.SchemaResponse, error) {
	return nil, fmt.Errorf("GetSchemasFromPlugins is deprecated - use CloudProvider.GetSchemas instead")
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
		fmt.Printf("🔌 Shutting down %d provider clients\n", len(pm.clients))
	}

	// Shutdown provider clients
	for _, client := range pm.clients {
		client.Kill()
	}

	pm.clients = make(map[string]*plugin.Client)
	pm.providers = make(map[string]shared.CloudProvider)

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
		"provider_clients": len(pm.clients),
		"loaded_providers": pm.ListLoadedProviders(),
		"plugin_directory": pm.pluginDir,
	}
}


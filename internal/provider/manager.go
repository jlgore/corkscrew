package provider

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-plugin"
)

// ProviderManager manages cloud provider plugins
type ProviderManager struct {
	pluginDir string
	plugins   map[string]*plugin.Client
	providers map[string]CloudProvider
	mu        sync.RWMutex
}

// NewProviderManager creates a new provider manager
func NewProviderManager(pluginDir string) *ProviderManager {
	return &ProviderManager{
		pluginDir: pluginDir,
		plugins:   make(map[string]*plugin.Client),
		providers: make(map[string]CloudProvider),
	}
}

// LoadProvider loads a cloud provider plugin
func (pm *ProviderManager) LoadProvider(providerName string) (CloudProvider, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if already loaded
	if provider, exists := pm.providers[providerName]; exists {
		return provider, nil
	}

	// Construct plugin path
	pluginPath := filepath.Join(pm.pluginDir, fmt.Sprintf("%s-provider", providerName))

	// Create plugin client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "CORKSCREW_PLUGIN",
			MagicCookieValue: "corkscrew-cloud-provider",
		},
		Plugins: map[string]plugin.Plugin{
			providerName: &CloudProviderPlugin{},
		},
		Cmd: exec.Command(pluginPath),
	})

	// Connect via RPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to connect to %s provider plugin: %w", providerName, err)
	}

	// Request the plugin interface
	raw, err := rpcClient.Dispense(providerName)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense %s provider plugin: %w", providerName, err)
	}

	provider := raw.(CloudProvider)

	// Cache the plugin and provider
	pm.plugins[providerName] = client
	pm.providers[providerName] = provider

	return provider, nil
}

// GetProvider returns a cached provider or loads it if not cached
func (pm *ProviderManager) GetProvider(providerName string) (CloudProvider, error) {
	pm.mu.RLock()
	provider, exists := pm.providers[providerName]
	pm.mu.RUnlock()

	if exists {
		return provider, nil
	}

	return pm.LoadProvider(providerName)
}

// ListProviders returns all loaded providers
func (pm *ProviderManager) ListProviders() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	providers := make([]string, 0, len(pm.providers))
	for name := range pm.providers {
		providers = append(providers, name)
	}
	return providers
}

// UnloadProvider unloads a provider and kills its plugin
func (pm *ProviderManager) UnloadProvider(providerName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Close the provider if it exists
	if provider, exists := pm.providers[providerName]; exists {
		if err := provider.Close(); err != nil {
			return fmt.Errorf("failed to close provider %s: %w", providerName, err)
		}
		delete(pm.providers, providerName)
	}

	// Kill the plugin if it exists
	if client, exists := pm.plugins[providerName]; exists {
		client.Kill()
		delete(pm.plugins, providerName)
	}

	return nil
}

// Shutdown shuts down all plugins
func (pm *ProviderManager) Shutdown() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Close all providers
	for name, provider := range pm.providers {
		if err := provider.Close(); err != nil {
			// Log error but continue shutdown
			fmt.Printf("Error closing provider %s: %v\n", name, err)
		}
	}

	// Kill all plugins
	for _, client := range pm.plugins {
		client.Kill()
	}

	// Clear maps
	pm.providers = make(map[string]CloudProvider)
	pm.plugins = make(map[string]*plugin.Client)
}

// DiscoverServices discovers services across all loaded providers
func (pm *ProviderManager) DiscoverServices(ctx context.Context, options map[string]*DiscoveryOptions) (map[string]*DiscoveryResult, error) {
	results := make(map[string]*DiscoveryResult)
	errors := make(map[string]error)

	for providerName, opts := range options {
		provider, err := pm.GetProvider(providerName)
		if err != nil {
			errors[providerName] = err
			continue
		}

		result, err := provider.DiscoverServices(ctx, opts)
		if err != nil {
			errors[providerName] = err
			continue
		}

		results[providerName] = result
	}

	if len(errors) > 0 && len(results) == 0 {
		// Return first error if no results
		for _, err := range errors {
			return nil, err
		}
	}

	return results, nil
}

// ScanResources scans resources across multiple providers
func (pm *ProviderManager) ScanResources(ctx context.Context, requests map[string]*ScanRequest) (map[string]*ScanResult, error) {
	results := make(map[string]*ScanResult)
	errors := make(map[string]error)

	for providerName, request := range requests {
		provider, err := pm.GetProvider(providerName)
		if err != nil {
			errors[providerName] = err
			continue
		}

		result, err := provider.ScanResources(ctx, request)
		if err != nil {
			errors[providerName] = err
			continue
		}

		results[providerName] = result
	}

	if len(errors) > 0 && len(results) == 0 {
		// Return first error if no results
		for _, err := range errors {
			return nil, err
		}
	}

	return results, nil
}

// ValidateProviders validates all loaded providers
func (pm *ProviderManager) ValidateProviders(ctx context.Context) map[string]error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	errors := make(map[string]error)
	for name, provider := range pm.providers {
		if err := provider.Validate(ctx); err != nil {
			errors[name] = err
		}
	}

	return errors
}

// GetProviderInfo returns information about a provider
func (pm *ProviderManager) GetProviderInfo(ctx context.Context, providerName string) (*ProviderInfo, error) {
	provider, err := pm.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	services, err := provider.GetSupportedServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get supported services: %w", err)
	}

	return &ProviderInfo{
		Name:              provider.GetProviderName(),
		SupportedServices: services,
		IsLoaded:          true,
	}, nil
}

// ProviderInfo contains information about a provider
type ProviderInfo struct {
	Name              string        `json:"name"`
	SupportedServices []ServiceInfo `json:"supported_services"`
	IsLoaded          bool          `json:"is_loaded"`
	Version           string        `json:"version,omitempty"`
	Description       string        `json:"description,omitempty"`
}
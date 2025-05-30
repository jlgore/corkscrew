package orchestrator

import (
	"fmt"
	"sync"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ProviderOrchestratorRegistry manages provider-specific analyzers and generators
// for use with gRPC providers in the orchestrator
type ProviderOrchestratorRegistry struct {
	analyzers  map[string]ProviderAnalyzer
	generators map[string]ProviderGenerator
	clients    map[string]pb.CloudProviderClient
	mu         sync.RWMutex
}

// NewProviderOrchestratorRegistry creates a new provider registry
func NewProviderOrchestratorRegistry() *ProviderOrchestratorRegistry {
	return &ProviderOrchestratorRegistry{
		analyzers:  make(map[string]ProviderAnalyzer),
		generators: make(map[string]ProviderGenerator),
		clients:    make(map[string]pb.CloudProviderClient),
	}
}

// RegisterProviderClient registers a gRPC provider client and creates adapters
func (r *ProviderOrchestratorRegistry) RegisterProviderClient(providerName string, client pb.CloudProviderClient) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if client == nil {
		return fmt.Errorf("client cannot be nil")
	}

	// Store the client
	r.clients[providerName] = client

	// Create and register the analyzer adapter
	analyzer := NewGRPCProviderAnalyzer(client, providerName)
	r.analyzers[providerName] = analyzer

	// Create and register the generator adapter
	generator := NewGRPCProviderGenerator(client, providerName)
	r.generators[providerName] = generator

	return nil
}

// GetAnalyzer returns the analyzer for a provider
func (r *ProviderOrchestratorRegistry) GetAnalyzer(providerName string) (ProviderAnalyzer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	analyzer, exists := r.analyzers[providerName]
	if !exists {
		return nil, fmt.Errorf("no analyzer registered for provider: %s", providerName)
	}

	return analyzer, nil
}

// GetGenerator returns the generator for a provider
func (r *ProviderOrchestratorRegistry) GetGenerator(providerName string) (ProviderGenerator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	generator, exists := r.generators[providerName]
	if !exists {
		return nil, fmt.Errorf("no generator registered for provider: %s", providerName)
	}

	return generator, nil
}

// GetClient returns the gRPC client for a provider
func (r *ProviderOrchestratorRegistry) GetClient(providerName string) (pb.CloudProviderClient, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	client, exists := r.clients[providerName]
	if !exists {
		return nil, fmt.Errorf("no client registered for provider: %s", providerName)
	}

	return client, nil
}

// RegisterWithOrchestrator registers all analyzers and generators with an orchestrator
func (r *ProviderOrchestratorRegistry) RegisterWithOrchestrator(orch Orchestrator) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Type assert to get access to the registration methods
	defaultOrch, ok := orch.(*defaultOrchestrator)
	if !ok {
		return fmt.Errorf("orchestrator does not support registration methods")
	}

	// Register all analyzers
	for providerName, analyzer := range r.analyzers {
		defaultOrch.RegisterAnalyzer(providerName, analyzer)
	}

	// Register all generators
	for providerName, generator := range r.generators {
		defaultOrch.RegisterGenerator(providerName, generator)
	}

	return nil
}

// ListProviders returns a list of registered provider names
func (r *ProviderOrchestratorRegistry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]string, 0, len(r.clients))
	for providerName := range r.clients {
		providers = append(providers, providerName)
	}

	return providers
}

// UnregisterProvider removes a provider and its adapters from the registry
func (r *ProviderOrchestratorRegistry) UnregisterProvider(providerName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.clients, providerName)
	delete(r.analyzers, providerName)
	delete(r.generators, providerName)
}

// IsRegistered checks if a provider is registered
func (r *ProviderOrchestratorRegistry) IsRegistered(providerName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.clients[providerName]
	return exists
}

// GetProviderInfo returns information about all registered providers
func (r *ProviderOrchestratorRegistry) GetProviderInfo() map[string]ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info := make(map[string]ProviderInfo)
	for providerName := range r.clients {
		info[providerName] = ProviderInfo{
			Name:         providerName,
			Type:         "gRPC",
			HasAnalyzer:  true,
			HasGenerator: true,
		}
	}

	return info
}

// ProviderInfo contains information about a registered provider
type ProviderInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`          // "gRPC", "local", etc.
	HasAnalyzer  bool   `json:"has_analyzer"`
	HasGenerator bool   `json:"has_generator"`
}
package factory

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
)

// UnifiedClientFactory provides a unified interface for all client creation strategies
type UnifiedClientFactory struct {
	// Core components
	config   aws.Config
	registry registry.DynamicServiceRegistry
	
	// Different factory strategies
	optimizedFactory *OptimizedClientFactory
	reflectionClient *ReflectionClient
	dynamicLoader    *DynamicLoader
	
	// Fallback chain configuration
	strategies []CreationStrategy
	
	// Client storage
	clients sync.Map
}

// CreationStrategy represents a client creation strategy
type CreationStrategy interface {
	Name() string
	CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error)
	Priority() int // Lower number = higher priority
}

// NewUnifiedClientFactory creates a new unified client factory
func NewUnifiedClientFactory(config aws.Config, registry registry.DynamicServiceRegistry) *UnifiedClientFactory {
	options := FactoryOptions{
		EnableCaching:        true,
		EnableMetrics:        true,
		EnablePreWarming:     true,
		PreWarmServices:      []string{"s3", "ec2", "dynamodb", "lambda"}, // Common services
		MaxConcurrentCreates: 20,
		PluginDirectory:      "/usr/lib/corkscrew/plugins",
	}

	factory := &UnifiedClientFactory{
		config:           config,
		registry:         registry,
		optimizedFactory: NewOptimizedClientFactory(config, registry, options),
		reflectionClient: NewReflectionClient(registry),
		dynamicLoader:    NewDynamicLoader(options.PluginDirectory),
	}

	// Setup default fallback chain
	factory.setupDefaultStrategies()

	return factory
}

// setupDefaultStrategies configures the default fallback chain
func (f *UnifiedClientFactory) setupDefaultStrategies() {
	f.strategies = []CreationStrategy{
		// 1. Try optimized factory first (includes caching)
		&optimizedStrategy{factory: f.optimizedFactory},
		
		// 2. Try reflection with registry metadata
		&reflectionStrategy{client: f.reflectionClient},
		
		// 3. Try dynamic loading (plugins)
		&dynamicLoadingStrategy{loader: f.dynamicLoader},
		
		// 4. Try generic client as last resort
		&genericStrategy{},
	}
}

// CreateClient creates a client using the configured fallback chain
func (f *UnifiedClientFactory) CreateClient(ctx context.Context, serviceName string) (interface{}, error) {
	// Check if already created
	if client, ok := f.clients.Load(serviceName); ok {
		return client, nil
	}

	// Try each strategy in order
	var lastError error
	for _, strategy := range f.strategies {
		client, err := strategy.CreateClient(ctx, serviceName, f.config)
		if err == nil && client != nil {
			// Success - cache and return
			f.clients.Store(serviceName, client)
			return client, nil
		}
		if err != nil {
			lastError = fmt.Errorf("%s strategy failed: %w", strategy.Name(), err)
		}
	}

	// All strategies failed
	if lastError != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", serviceName, lastError)
	}
	return nil, fmt.Errorf("no strategy could create client for %s", serviceName)
}

// GetClient retrieves an existing client or creates a new one
func (f *UnifiedClientFactory) GetClient(ctx context.Context, serviceName string) (interface{}, error) {
	return f.CreateClient(ctx, serviceName)
}

// ListAvailableServices returns all services that can be created
func (f *UnifiedClientFactory) ListAvailableServices() []string {
	return f.registry.ListServices()
}

// GetServiceInfo returns detailed information about a service
func (f *UnifiedClientFactory) GetServiceInfo(serviceName string) (*ServiceInfo, error) {
	service, exists := f.registry.GetService(serviceName)
	if !exists {
		return nil, fmt.Errorf("service %s not found", serviceName)
	}

	return &ServiceInfo{
		Name:                    service.Name,
		DisplayName:             service.DisplayName,
		Description:             service.Description,
		PackagePath:             service.PackagePath,
		ClientType:              service.ClientType,
		RateLimit:               float64(service.RateLimit),
		BurstLimit:              service.BurstLimit,
		GlobalService:           service.GlobalService,
		RequiresRegion:          service.RequiresRegion,
		SupportsPagination:      service.SupportsPagination,
		SupportsResourceExplorer: service.SupportsResourceExplorer,
	}, nil
}

// ServiceInfo provides service information
type ServiceInfo struct {
	Name                    string
	DisplayName             string
	Description             string
	PackagePath             string
	ClientType              string
	RateLimit               float64
	BurstLimit              int
	GlobalService           bool
	RequiresRegion          bool
	SupportsPagination      bool
	SupportsResourceExplorer bool
}

// Strategy implementations

type optimizedStrategy struct {
	factory *OptimizedClientFactory
}

func (s *optimizedStrategy) Name() string { return "optimized" }
func (s *optimizedStrategy) Priority() int { return 1 }
func (s *optimizedStrategy) CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error) {
	return s.factory.CreateClient(ctx, serviceName)
}

type reflectionStrategy struct {
	client *ReflectionClient
}

func (s *reflectionStrategy) Name() string { return "reflection" }
func (s *reflectionStrategy) Priority() int { return 2 }
func (s *reflectionStrategy) CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error) {
	result, err := s.client.CreateClient(ctx, serviceName, config)
	if err != nil {
		return nil, err
	}
	return result.Client, result.Error
}

type dynamicLoadingStrategy struct {
	loader *DynamicLoader
}

func (s *dynamicLoadingStrategy) Name() string { return "dynamic_loading" }
func (s *dynamicLoadingStrategy) Priority() int { return 3 }
func (s *dynamicLoadingStrategy) CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error) {
	return s.loader.LoadServiceClient(serviceName, config)
}

type genericStrategy struct{}

func (s *genericStrategy) Name() string { return "generic" }
func (s *genericStrategy) Priority() int { return 99 }
func (s *genericStrategy) CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error) {
	return &GenericServiceClient{
		ServiceName: serviceName,
		Config:      config,
	}, nil
}

// Additional factory methods

// InvalidateCache removes a service from all caches
func (f *UnifiedClientFactory) InvalidateCache(serviceName string) {
	f.clients.Delete(serviceName)
	f.optimizedFactory.InvalidateCache(serviceName)
}

// ClearAllCaches clears all cached clients
func (f *UnifiedClientFactory) ClearAllCaches() {
	f.clients = sync.Map{}
	f.optimizedFactory.ClearAllCaches()
	f.reflectionClient.ClearCache()
}

// GetMetrics returns performance metrics
func (f *UnifiedClientFactory) GetMetrics() FactoryMetrics {
	return f.optimizedFactory.GetMetrics()
}

// UpdateRegistry updates the service registry
func (f *UnifiedClientFactory) UpdateRegistry(registry registry.DynamicServiceRegistry) {
	f.registry = registry
	f.optimizedFactory.UpdateRegistry(registry)
	f.reflectionClient = NewReflectionClient(registry)
	f.ClearAllCaches()
}

// AddStrategy adds a custom creation strategy
func (f *UnifiedClientFactory) AddStrategy(strategy CreationStrategy) {
	// Insert based on priority
	inserted := false
	for i, existing := range f.strategies {
		if strategy.Priority() < existing.Priority() {
			// Insert before this one
			f.strategies = append(f.strategies[:i], append([]CreationStrategy{strategy}, f.strategies[i:]...)...)
			inserted = true
			break
		}
	}
	if !inserted {
		f.strategies = append(f.strategies, strategy)
	}
}

// RemoveStrategy removes a strategy by name
func (f *UnifiedClientFactory) RemoveStrategy(name string) {
	for i, strategy := range f.strategies {
		if strategy.Name() == name {
			f.strategies = append(f.strategies[:i], f.strategies[i+1:]...)
			break
		}
	}
}

// GetStrategies returns the current strategy chain
func (f *UnifiedClientFactory) GetStrategies() []string {
	names := make([]string, len(f.strategies))
	for i, strategy := range f.strategies {
		names[i] = strategy.Name()
	}
	return names
}
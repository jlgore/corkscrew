package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
)

type SmartDiscoveryEngine struct {
	providers        map[string]CloudProvider
	regionDetector   *RegionDetector
	serviceDetector  *ServiceDetector
	correlationEngine *CorrelationEngine
	mutex            sync.RWMutex
}

type CloudProvider interface {
	GetName() string
	GetRegions(ctx context.Context) ([]models.Region, error)
	GetServices(ctx context.Context, region string) ([]models.Service, error)
	DiscoverResources(ctx context.Context, service models.Service) ([]models.Resource, error)
}

type SmartDiscoveryConfig struct {
	EnableRegionDetection     bool
	EnableServiceDetection    bool
	EnableCrossProviderCorre  bool
	ParallelDiscoveryWorkers  int
	DiscoveryTimeout         time.Duration
	CacheExpiration          time.Duration
}

type DiscoveryResult struct {
	Provider    string
	Regions     []models.Region
	Services    []models.Service
	Resources   []models.Resource
	Correlations []models.ResourceCorrelation
	Metadata    map[string]interface{}
	Duration    time.Duration
	Errors      []error
}

func NewSmartDiscoveryEngine(config SmartDiscoveryConfig) *SmartDiscoveryEngine {
	return &SmartDiscoveryEngine{
		providers:         make(map[string]CloudProvider),
		regionDetector:    NewRegionDetector(config),
		serviceDetector:   NewServiceDetector(config),
		correlationEngine: NewCorrelationEngine(config),
	}
}

func (sde *SmartDiscoveryEngine) RegisterProvider(provider CloudProvider) {
	sde.mutex.Lock()
	defer sde.mutex.Unlock()
	sde.providers[provider.GetName()] = provider
}

func (sde *SmartDiscoveryEngine) DiscoverAll(ctx context.Context) (*DiscoveryResult, error) {
	start := time.Now()
	result := &DiscoveryResult{
		Metadata: make(map[string]interface{}),
		Errors:   make([]error, 0),
	}

	var wg sync.WaitGroup
	resultChan := make(chan *DiscoveryResult, len(sde.providers))

	sde.mutex.RLock()
	for _, provider := range sde.providers {
		wg.Add(1)
		go func(p CloudProvider) {
			defer wg.Done()
			providerResult, err := sde.discoverProvider(ctx, p)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("provider %s: %w", p.GetName(), err))
			}
			resultChan <- providerResult
		}(provider)
	}
	sde.mutex.RUnlock()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for providerResult := range resultChan {
		result.Regions = append(result.Regions, providerResult.Regions...)
		result.Services = append(result.Services, providerResult.Services...)
		result.Resources = append(result.Resources, providerResult.Resources...)
		result.Errors = append(result.Errors, providerResult.Errors...)
	}

	if len(result.Resources) > 0 {
		correlations, err := sde.correlationEngine.FindCorrelations(ctx, result.Resources)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("correlation analysis: %w", err))
		} else {
			result.Correlations = correlations
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (sde *SmartDiscoveryEngine) discoverProvider(ctx context.Context, provider CloudProvider) (*DiscoveryResult, error) {
	result := &DiscoveryResult{
		Provider: provider.GetName(),
		Metadata: make(map[string]interface{}),
	}

	regions, err := sde.regionDetector.DetectOptimalRegions(ctx, provider)
	if err != nil {
		return result, fmt.Errorf("region detection: %w", err)
	}
	result.Regions = regions

	for _, region := range regions {
		services, err := sde.serviceDetector.DetectActiveServices(ctx, provider, region.Name)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("service detection in %s: %w", region.Name, err))
			continue
		}
		result.Services = append(result.Services, services...)

		for _, service := range services {
			resources, err := provider.DiscoverResources(ctx, service)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("resource discovery for %s: %w", service.Name, err))
				continue
			}
			result.Resources = append(result.Resources, resources...)
		}
	}

	return result, nil
}

func (sde *SmartDiscoveryEngine) DiscoverByProvider(ctx context.Context, providerName string) (*DiscoveryResult, error) {
	sde.mutex.RLock()
	provider, exists := sde.providers[providerName]
	sde.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("provider %s not registered", providerName)
	}

	return sde.discoverProvider(ctx, provider)
}

func (sde *SmartDiscoveryEngine) GetRegisteredProviders() []string {
	sde.mutex.RLock()
	defer sde.mutex.RUnlock()

	providers := make([]string, 0, len(sde.providers))
	for name := range sde.providers {
		providers = append(providers, name)
	}
	return providers
}
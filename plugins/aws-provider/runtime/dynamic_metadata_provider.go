package runtime

import (
	"fmt"
	"sync"

	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
	"golang.org/x/time/rate"
)

// DynamicMetadataProvider replaces hardcoded metadata functions with registry-based lookups
type DynamicMetadataProvider struct {
	registry *registry.UnifiedServiceRegistry
	cache    sync.Map // Cache for frequently accessed metadata
	mu       sync.RWMutex
}

// NewDynamicMetadataProvider creates a new metadata provider
func NewDynamicMetadataProvider(serviceRegistry *registry.UnifiedServiceRegistry) *DynamicMetadataProvider {
	return &DynamicMetadataProvider{
		registry: serviceRegistry,
	}
}

// GetResourceTypesForService returns resource types for a service from the registry
func (p *DynamicMetadataProvider) GetResourceTypesForService(serviceName string) []string {
	// Check cache first
	if cached, ok := p.cache.Load(serviceName + "_resourceTypes"); ok {
		return cached.([]string)
	}

	// Get from registry
	service, exists := p.registry.GetService(serviceName)
	if !exists {
		return []string{} // Return empty instead of hardcoded fallback
	}

	// Extract resource type names
	resourceTypes := make([]string, len(service.ResourceTypes))
	for i, rt := range service.ResourceTypes {
		resourceTypes[i] = rt.Name
	}

	// Cache the result
	p.cache.Store(serviceName+"_resourceTypes", resourceTypes)
	
	return resourceTypes
}

// GetPermissionsForService returns required permissions from the registry
func (p *DynamicMetadataProvider) GetPermissionsForService(serviceName string) []string {
	// Check cache first
	if cached, ok := p.cache.Load(serviceName + "_permissions"); ok {
		return cached.([]string)
	}

	// Get from registry
	service, exists := p.registry.GetService(serviceName)
	if !exists {
		// Return generic permissions pattern as fallback
		return []string{
			fmt.Sprintf("%s:Describe*", serviceName),
			fmt.Sprintf("%s:List*", serviceName),
			fmt.Sprintf("%s:Get*", serviceName),
		}
	}

	// Cache the result
	p.cache.Store(serviceName+"_permissions", service.Permissions)
	
	return service.Permissions
}

// GetRateLimitForService returns rate limit from the registry
func (p *DynamicMetadataProvider) GetRateLimitForService(serviceName string) rate.Limit {
	// Check cache first
	if cached, ok := p.cache.Load(serviceName + "_rateLimit"); ok {
		return cached.(rate.Limit)
	}

	// Get from registry
	service, exists := p.registry.GetService(serviceName)
	if !exists {
		return rate.Limit(10) // Default rate limit
	}

	rateLimit := service.RateLimit
	if rateLimit == 0 {
		rateLimit = rate.Limit(10) // Default if not set
	}

	// Cache the result
	p.cache.Store(serviceName+"_rateLimit", rateLimit)
	
	return rateLimit
}

// GetBurstLimitForService returns burst limit from the registry
func (p *DynamicMetadataProvider) GetBurstLimitForService(serviceName string) int {
	// Check cache first
	if cached, ok := p.cache.Load(serviceName + "_burstLimit"); ok {
		return cached.(int)
	}

	// Get from registry
	service, exists := p.registry.GetService(serviceName)
	if !exists {
		// Default to 2x rate limit
		rateLimit := p.GetRateLimitForService(serviceName)
		return int(rateLimit * 2)
	}

	burstLimit := service.BurstLimit
	if burstLimit == 0 {
		// Default to 2x rate limit if not set
		burstLimit = int(service.RateLimit * 2)
		if burstLimit == 0 {
			burstLimit = 20 // Absolute default
		}
	}

	// Cache the result
	p.cache.Store(serviceName+"_burstLimit", burstLimit)
	
	return burstLimit
}

// GetServiceMetadata returns complete metadata for a service
func (p *DynamicMetadataProvider) GetServiceMetadata(serviceName string) (*ServiceMetadata, error) {
	service, exists := p.registry.GetService(serviceName)
	if !exists {
		return nil, fmt.Errorf("service %s not found in registry", serviceName)
	}

	// Build comprehensive metadata
	metadata := &ServiceMetadata{
		ServiceName:              service.Name,
		DisplayName:              service.DisplayName,
		Description:              service.Description,
		ResourceTypes:            p.GetResourceTypesForService(serviceName),
		Permissions:              service.Permissions,
		RateLimit:                service.RateLimit,
		BurstLimit:               service.BurstLimit,
		SupportsPagination:       service.SupportsPagination,
		SupportsResourceExplorer: service.SupportsResourceExplorer,
		GlobalService:            service.GlobalService,
		RequiresRegion:           service.RequiresRegion,
		CustomHandlerRequired:    service.CustomHandlerRequired,
	}

	// Add resource-specific metadata
	for _, rt := range service.ResourceTypes {
		metadata.ResourceMetadata = append(metadata.ResourceMetadata, ResourceMetadata{
			ResourceType:        rt.Name,
			ListOperation:       rt.ListOperation,
			DescribeOperation:   rt.DescribeOperation,
			RequiredPermissions: rt.RequiredPermissions,
			SupportsTags:        rt.SupportsTags,
			Paginated:           rt.Paginated,
		})
	}

	return metadata, nil
}

// ServiceMetadata contains comprehensive service metadata
type ServiceMetadata struct {
	ServiceName              string
	DisplayName              string
	Description              string
	ResourceTypes            []string
	Permissions              []string
	RateLimit                rate.Limit
	BurstLimit               int
	SupportsPagination       bool
	SupportsResourceExplorer bool
	GlobalService            bool
	RequiresRegion           bool
	CustomHandlerRequired    bool
	ResourceMetadata         []ResourceMetadata
}

// ResourceMetadata contains metadata for a specific resource type
type ResourceMetadata struct {
	ResourceType        string
	ListOperation       string
	DescribeOperation   string
	RequiredPermissions []string
	SupportsTags        bool
	Paginated           bool
}

// ClearCache clears the metadata cache
func (p *DynamicMetadataProvider) ClearCache() {
	p.cache = sync.Map{}
}

// RefreshCache refreshes cached metadata for all services
func (p *DynamicMetadataProvider) RefreshCache() {
	services := p.registry.ListServices()
	for _, serviceName := range services {
		// Force refresh by clearing and re-fetching
		p.cache.Delete(serviceName + "_resourceTypes")
		p.cache.Delete(serviceName + "_permissions")
		p.cache.Delete(serviceName + "_rateLimit")
		p.cache.Delete(serviceName + "_burstLimit")
		
		// Pre-populate cache
		p.GetResourceTypesForService(serviceName)
		p.GetPermissionsForService(serviceName)
		p.GetRateLimitForService(serviceName)
		p.GetBurstLimitForService(serviceName)
	}
}

// GlobalMetadataProvider is a singleton instance for backward compatibility
var globalMetadataProvider *DynamicMetadataProvider
var metadataProviderOnce sync.Once

// GetGlobalMetadataProvider returns the global metadata provider instance
func GetGlobalMetadataProvider() *DynamicMetadataProvider {
	metadataProviderOnce.Do(func() {
		// This will be initialized with the actual registry during plugin setup
		globalMetadataProvider = &DynamicMetadataProvider{}
	})
	return globalMetadataProvider
}

// SetGlobalMetadataProvider sets the global metadata provider
func SetGlobalMetadataProvider(provider *DynamicMetadataProvider) {
	globalMetadataProvider = provider
}

// Compatibility functions that use the global provider

// GetResourceTypesForService (backward compatible)
func GetResourceTypesForServiceCompat(service string) []string {
	if provider := GetGlobalMetadataProvider(); provider != nil && provider.registry != nil {
		return provider.GetResourceTypesForService(service)
	}
	// Fallback to empty
	return []string{}
}

// GetPermissionsForService (backward compatible)
func GetPermissionsForServiceCompat(service string) []string {
	if provider := GetGlobalMetadataProvider(); provider != nil && provider.registry != nil {
		return provider.GetPermissionsForService(service)
	}
	// Fallback to generic pattern
	return []string{
		fmt.Sprintf("%s:Describe*", service),
		fmt.Sprintf("%s:List*", service),
		fmt.Sprintf("%s:Get*", service),
	}
}

// GetRateLimitForService (backward compatible)
func GetRateLimitForServiceCompat(service string) rate.Limit {
	if provider := GetGlobalMetadataProvider(); provider != nil && provider.registry != nil {
		return provider.GetRateLimitForService(service)
	}
	return rate.Limit(10) // Default
}

// GetBurstLimitForService (backward compatible)
func GetBurstLimitForServiceCompat(service string) int {
	if provider := GetGlobalMetadataProvider(); provider != nil && provider.registry != nil {
		return provider.GetBurstLimitForService(service)
	}
	return 20 // Default
}
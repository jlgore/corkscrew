package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// AzureServiceDiscovery handles dynamic discovery of Azure services
type AzureServiceDiscovery struct {
	credential      azcore.TokenCredential
	subscriptionID  string
	providersClient *armresources.ProvidersClient
	cache           map[string]*ProviderInfo
	mu              sync.RWMutex
}

// ProviderInfo contains information about an Azure resource provider
type ProviderInfo struct {
	Namespace         string
	ResourceTypes     []ResourceTypeInfo
	RegistrationState string
	Locations         []string
}

// ResourceTypeInfo contains information about a specific resource type
type ResourceTypeInfo struct {
	ResourceType string
	Locations    []string
	APIVersions  []string
	Capabilities []string
	Properties   map[string]interface{}
}

// NewAzureServiceDiscovery creates a new Azure service discovery instance
func NewAzureServiceDiscovery(cred azcore.TokenCredential, subID string) *AzureServiceDiscovery {
	client, _ := armresources.NewProvidersClient(subID, cred, nil)
	return &AzureServiceDiscovery{
		credential:      cred,
		subscriptionID:  subID,
		providersClient: client,
		cache:           make(map[string]*ProviderInfo),
	}
}

// DiscoverProviders discovers all Azure resource providers and their capabilities
func (d *AzureServiceDiscovery) DiscoverProviders(ctx context.Context) ([]*ProviderInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var providers []*ProviderInfo

	// List all resource providers with detailed information
	pager := d.providersClient.NewListPager(&armresources.ProvidersClientListOptions{
		Expand: to.Ptr("resourceTypes/aliases"), // Get detailed info including aliases
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list providers: %w", err)
		}

		for _, provider := range page.Value {
			if provider.Namespace == nil {
				continue
			}

			providerInfo := &ProviderInfo{
				Namespace:     *provider.Namespace,
				ResourceTypes: []ResourceTypeInfo{},
			}

			// Set registration state
			if provider.RegistrationState != nil {
				providerInfo.RegistrationState = *provider.RegistrationState
			}

			// Extract resource types
			if provider.ResourceTypes != nil {
				for _, rt := range provider.ResourceTypes {
					if rt.ResourceType == nil {
						continue
					}

					resourceType := ResourceTypeInfo{
						ResourceType: *rt.ResourceType,
						Properties:   make(map[string]interface{}),
					}

					// Get locations
					if rt.Locations != nil {
						resourceType.Locations = make([]string, 0, len(rt.Locations))
						for _, loc := range rt.Locations {
							if loc != nil {
								resourceType.Locations = append(resourceType.Locations, *loc)
							}
						}
					}

					// Get API versions
					if rt.APIVersions != nil {
						resourceType.APIVersions = make([]string, 0, len(rt.APIVersions))
						for _, ver := range rt.APIVersions {
							if ver != nil {
								resourceType.APIVersions = append(resourceType.APIVersions, *ver)
							}
						}
					}

					// Get capabilities
					if rt.Capabilities != nil {
						resourceType.Capabilities = []string{*rt.Capabilities}
					}

					// Extract additional properties
					if rt.Properties != nil {
						// Store any additional properties that might be useful
						resourceType.Properties["properties"] = rt.Properties
					}

					// Check if resource type supports common operations
					resourceType.Properties["supports_list"] = d.supportsListOperation(providerInfo.Namespace, resourceType.ResourceType)
					resourceType.Properties["supports_get"] = d.supportsGetOperation(providerInfo.Namespace, resourceType.ResourceType)
					resourceType.Properties["supports_tags"] = d.supportsTags(providerInfo.Namespace, resourceType.ResourceType)

					providerInfo.ResourceTypes = append(providerInfo.ResourceTypes, resourceType)
				}
			}

			// Only include registered providers or those we specifically want
			if d.shouldIncludeProvider(providerInfo) {
				providers = append(providers, providerInfo)
				d.cache[providerInfo.Namespace] = providerInfo
			}
		}
	}

	return providers, nil
}

// GetProvider returns cached provider information
func (d *AzureServiceDiscovery) GetProvider(namespace string) *ProviderInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cache[namespace]
}

// GetProviderByService returns provider information by service name (e.g., "compute" -> "Microsoft.Compute")
func (d *AzureServiceDiscovery) GetProviderByService(serviceName string) *ProviderInfo {
	namespace := fmt.Sprintf("Microsoft.%s", strings.Title(serviceName))
	return d.GetProvider(namespace)
}

// ListRegisteredProviders returns only registered providers
func (d *AzureServiceDiscovery) ListRegisteredProviders(ctx context.Context) ([]*ProviderInfo, error) {
	allProviders, err := d.DiscoverProviders(ctx)
	if err != nil {
		return nil, err
	}

	var registered []*ProviderInfo
	for _, provider := range allProviders {
		if provider.RegistrationState == "Registered" {
			registered = append(registered, provider)
		}
	}

	return registered, nil
}

// GetResourceTypesForProvider returns all resource types for a specific provider
func (d *AzureServiceDiscovery) GetResourceTypesForProvider(ctx context.Context, namespace string) ([]ResourceTypeInfo, error) {
	provider := d.GetProvider(namespace)
	if provider != nil {
		return provider.ResourceTypes, nil
	}

	// If not cached, fetch specifically for this provider
	providerClient := d.providersClient
	result, err := providerClient.Get(ctx, namespace, &armresources.ProvidersClientGetOptions{
		Expand: to.Ptr("resourceTypes"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get provider %s: %w", namespace, err)
	}

	var resourceTypes []ResourceTypeInfo
	if result.ResourceTypes != nil {
		for _, rt := range result.ResourceTypes {
			if rt.ResourceType == nil {
				continue
			}

			resourceType := ResourceTypeInfo{
				ResourceType: *rt.ResourceType,
				Properties:   make(map[string]interface{}),
			}

			// Extract details as in DiscoverProviders
			if rt.Locations != nil {
				resourceType.Locations = make([]string, 0, len(rt.Locations))
				for _, loc := range rt.Locations {
					if loc != nil {
						resourceType.Locations = append(resourceType.Locations, *loc)
					}
				}
			}

			if rt.APIVersions != nil {
				resourceType.APIVersions = make([]string, 0, len(rt.APIVersions))
				for _, ver := range rt.APIVersions {
					if ver != nil {
						resourceType.APIVersions = append(resourceType.APIVersions, *ver)
					}
				}
			}

			resourceTypes = append(resourceTypes, resourceType)
		}
	}

	return resourceTypes, nil
}

// shouldIncludeProvider determines if a provider should be included in discovery
func (d *AzureServiceDiscovery) shouldIncludeProvider(provider *ProviderInfo) bool {
	// Include registered providers
	if provider.RegistrationState == "Registered" {
		return true
	}

	// Include important Microsoft providers even if not registered
	importantProviders := []string{
		"Microsoft.Compute",
		"Microsoft.Storage",
		"Microsoft.Network",
		"Microsoft.KeyVault",
		"Microsoft.Sql",
		"Microsoft.Web",
		"Microsoft.ContainerService",
		"Microsoft.ContainerRegistry",
		"Microsoft.DocumentDB",
		"Microsoft.Insights",
		"Microsoft.Authorization",
		"Microsoft.Resources",
	}

	for _, important := range importantProviders {
		if provider.Namespace == important {
			return true
		}
	}

	// Exclude providers with no resource types
	return len(provider.ResourceTypes) > 0
}

// supportsListOperation checks if a resource type supports list operations
func (d *AzureServiceDiscovery) supportsListOperation(namespace, resourceType string) bool {
	// Most Azure resource types support list operations
	// This could be enhanced with more specific logic
	excludedTypes := []string{
		"operations",
		"locations",
		"checkNameAvailability",
	}

	lowerType := strings.ToLower(resourceType)
	for _, excluded := range excludedTypes {
		if strings.Contains(lowerType, excluded) {
			return false
		}
	}

	return true
}

// supportsGetOperation checks if a resource type supports get operations
func (d *AzureServiceDiscovery) supportsGetOperation(namespace, resourceType string) bool {
	// Most Azure resource types support get operations
	return d.supportsListOperation(namespace, resourceType)
}

// supportsTags checks if a resource type supports tags
func (d *AzureServiceDiscovery) supportsTags(namespace, resourceType string) bool {
	// Most Azure resources support tags, with some exceptions
	nonTaggableTypes := []string{
		"operations",
		"locations",
		"checkNameAvailability",
		"usages",
		"quotas",
		"metrics",
		"diagnosticSettings",
	}

	lowerType := strings.ToLower(resourceType)
	for _, nonTaggable := range nonTaggableTypes {
		if strings.Contains(lowerType, nonTaggable) {
			return false
		}
	}

	return true
}

// GetLatestAPIVersion returns the latest API version for a resource type
func (d *AzureServiceDiscovery) GetLatestAPIVersion(namespace, resourceType string) string {
	provider := d.GetProvider(namespace)
	if provider == nil {
		return ""
	}

	for _, rt := range provider.ResourceTypes {
		if rt.ResourceType == resourceType && len(rt.APIVersions) > 0 {
			// API versions are typically sorted with latest first
			return rt.APIVersions[0]
		}
	}

	return ""
}

// GetSupportedLocations returns supported locations for a resource type
func (d *AzureServiceDiscovery) GetSupportedLocations(namespace, resourceType string) []string {
	provider := d.GetProvider(namespace)
	if provider == nil {
		return nil
	}

	for _, rt := range provider.ResourceTypes {
		if rt.ResourceType == resourceType {
			return rt.Locations
		}
	}

	return nil
}

// GetProviderCapabilities returns capabilities for a provider
func (d *AzureServiceDiscovery) GetProviderCapabilities(namespace string) map[string]interface{} {
	provider := d.GetProvider(namespace)
	if provider == nil {
		return nil
	}

	capabilities := make(map[string]interface{})
	capabilities["namespace"] = provider.Namespace
	capabilities["registration_state"] = provider.RegistrationState
	capabilities["resource_type_count"] = len(provider.ResourceTypes)

	// Aggregate capabilities across resource types
	var allLocations []string
	var allAPIVersions []string
	locationSet := make(map[string]bool)
	versionSet := make(map[string]bool)

	for _, rt := range provider.ResourceTypes {
		for _, loc := range rt.Locations {
			if !locationSet[loc] {
				allLocations = append(allLocations, loc)
				locationSet[loc] = true
			}
		}
		for _, ver := range rt.APIVersions {
			if !versionSet[ver] {
				allAPIVersions = append(allAPIVersions, ver)
				versionSet[ver] = true
			}
		}
	}

	capabilities["supported_locations"] = allLocations
	capabilities["api_versions"] = allAPIVersions

	return capabilities
}

// RefreshProvider refreshes information for a specific provider
func (d *AzureServiceDiscovery) RefreshProvider(ctx context.Context, namespace string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.providersClient.Get(ctx, namespace, &armresources.ProvidersClientGetOptions{
		Expand: to.Ptr("resourceTypes/aliases"),
	})
	if err != nil {
		return fmt.Errorf("failed to refresh provider %s: %w", namespace, err)
	}

	if result.Namespace == nil {
		return fmt.Errorf("invalid provider response for %s", namespace)
	}

	providerInfo := &ProviderInfo{
		Namespace:     *result.Namespace,
		ResourceTypes: []ResourceTypeInfo{},
	}

	if result.RegistrationState != nil {
		providerInfo.RegistrationState = *result.RegistrationState
	}

	// Process resource types
	if result.ResourceTypes != nil {
		for _, rt := range result.ResourceTypes {
			if rt.ResourceType == nil {
				continue
			}

			resourceType := ResourceTypeInfo{
				ResourceType: *rt.ResourceType,
				Properties:   make(map[string]interface{}),
			}

			// Extract details
			if rt.Locations != nil {
				resourceType.Locations = make([]string, 0, len(rt.Locations))
				for _, loc := range rt.Locations {
					if loc != nil {
						resourceType.Locations = append(resourceType.Locations, *loc)
					}
				}
			}

			if rt.APIVersions != nil {
				resourceType.APIVersions = make([]string, 0, len(rt.APIVersions))
				for _, ver := range rt.APIVersions {
					if ver != nil {
						resourceType.APIVersions = append(resourceType.APIVersions, *ver)
					}
				}
			}

			providerInfo.ResourceTypes = append(providerInfo.ResourceTypes, resourceType)
		}
	}

	// Update cache
	d.cache[namespace] = providerInfo

	return nil
}


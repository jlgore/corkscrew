package main

import (
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
)

// AzureClientFactory creates SDK clients dynamically
type AzureClientFactory struct {
	credential     azcore.TokenCredential
	subscriptionID string
	clientCache    map[string]interface{}
	mu             sync.RWMutex
}

// NewAzureClientFactory creates a new Azure client factory
func NewAzureClientFactory(cred azcore.TokenCredential, subID string) *AzureClientFactory {
	return &AzureClientFactory{
		credential:     cred,
		subscriptionID: subID,
		clientCache:    make(map[string]interface{}),
	}
}

// GetClient returns a client for the specified resource type
func (f *AzureClientFactory) GetClient(resourceType string) (interface{}, error) {
	f.mu.RLock()
	if client, exists := f.clientCache[resourceType]; exists {
		f.mu.RUnlock()
		return client, nil
	}
	f.mu.RUnlock()

	// Create client based on resource type
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := f.clientCache[resourceType]; exists {
		return client, nil
	}

	client, err := f.createClient(resourceType)
	if err != nil {
		return nil, err
	}

	f.clientCache[resourceType] = client
	return client, nil
}

// createClient creates a client based on resource type
func (f *AzureClientFactory) createClient(resourceType string) (interface{}, error) {
	// Map resource types to SDK clients
	switch resourceType {
	case "Microsoft.Compute/virtualMachines":
		return armcompute.NewVirtualMachinesClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Compute/virtualMachineScaleSets":
		return armcompute.NewVirtualMachineScaleSetsClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Compute/disks":
		return armcompute.NewDisksClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Compute/snapshots":
		return armcompute.NewSnapshotsClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Compute/images":
		return armcompute.NewImagesClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Compute/availabilitySets":
		return armcompute.NewAvailabilitySetsClient(f.subscriptionID, f.credential, nil)

	case "Microsoft.Storage/storageAccounts":
		return armstorage.NewAccountsClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Storage/storageAccounts/blobServices":
		return armstorage.NewBlobServicesClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Storage/storageAccounts/fileServices":
		return armstorage.NewFileServicesClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Storage/storageAccounts/queueServices":
		return armstorage.NewQueueServicesClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Storage/storageAccounts/tableServices":
		return armstorage.NewTableServicesClient(f.subscriptionID, f.credential, nil)

	case "Microsoft.Network/virtualNetworks":
		return armnetwork.NewVirtualNetworksClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Network/networkSecurityGroups":
		return armnetwork.NewSecurityGroupsClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Network/publicIPAddresses":
		return armnetwork.NewPublicIPAddressesClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Network/networkInterfaces":
		return armnetwork.NewInterfacesClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Network/loadBalancers":
		return armnetwork.NewLoadBalancersClient(f.subscriptionID, f.credential, nil)
	case "Microsoft.Network/applicationGateways":
		return armnetwork.NewApplicationGatewaysClient(f.subscriptionID, f.credential, nil)

	default:
		// For unknown types, return the generic resources client
		return armresources.NewClient(f.subscriptionID, f.credential, nil)
	}
}

// GetComputeClient returns a compute client
func (f *AzureClientFactory) GetComputeClient() (*armcompute.VirtualMachinesClient, error) {
	client, err := f.GetClient("Microsoft.Compute/virtualMachines")
	if err != nil {
		return nil, err
	}
	return client.(*armcompute.VirtualMachinesClient), nil
}

// GetStorageClient returns a storage client
func (f *AzureClientFactory) GetStorageClient() (*armstorage.AccountsClient, error) {
	client, err := f.GetClient("Microsoft.Storage/storageAccounts")
	if err != nil {
		return nil, err
	}
	return client.(*armstorage.AccountsClient), nil
}

// GetNetworkClient returns a network client
func (f *AzureClientFactory) GetNetworkClient() (*armnetwork.VirtualNetworksClient, error) {
	client, err := f.GetClient("Microsoft.Network/virtualNetworks")
	if err != nil {
		return nil, err
	}
	return client.(*armnetwork.VirtualNetworksClient), nil
}

// GetResourcesClient returns the generic resources client
func (f *AzureClientFactory) GetResourcesClient() (*armresources.Client, error) {
	client, err := f.GetClient("Microsoft.Resources/resources")
	if err != nil {
		return nil, err
	}
	return client.(*armresources.Client), nil
}

// ClearCache clears the client cache
func (f *AzureClientFactory) ClearCache() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clientCache = make(map[string]interface{})
}

// GetCacheSize returns the number of cached clients
func (f *AzureClientFactory) GetCacheSize() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.clientCache)
}

// GetCachedClientTypes returns the types of cached clients
func (f *AzureClientFactory) GetCachedClientTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.clientCache))
	for resourceType := range f.clientCache {
		types = append(types, resourceType)
	}
	return types
}

// ValidateClient validates that a client can be created for a resource type
func (f *AzureClientFactory) ValidateClient(resourceType string) error {
	_, err := f.createClient(resourceType)
	return err
}

// GetSupportedResourceTypes returns the resource types that have specific client implementations
func (f *AzureClientFactory) GetSupportedResourceTypes() []string {
	return []string{
		"Microsoft.Compute/virtualMachines",
		"Microsoft.Compute/virtualMachineScaleSets",
		"Microsoft.Compute/disks",
		"Microsoft.Compute/snapshots",
		"Microsoft.Compute/images",
		"Microsoft.Compute/availabilitySets",
		"Microsoft.Storage/storageAccounts",
		"Microsoft.Storage/storageAccounts/blobServices",
		"Microsoft.Storage/storageAccounts/fileServices",
		"Microsoft.Storage/storageAccounts/queueServices",
		"Microsoft.Storage/storageAccounts/tableServices",
		"Microsoft.Network/virtualNetworks",
		"Microsoft.Network/networkSecurityGroups",
		"Microsoft.Network/publicIPAddresses",
		"Microsoft.Network/networkInterfaces",
		"Microsoft.Network/loadBalancers",
		"Microsoft.Network/applicationGateways",
	}
}

// IsResourceTypeSupported checks if a resource type has a specific client implementation
func (f *AzureClientFactory) IsResourceTypeSupported(resourceType string) bool {
	supported := f.GetSupportedResourceTypes()
	for _, supportedType := range supported {
		if supportedType == resourceType {
			return true
		}
	}
	return false
}

// GetClientStatistics returns statistics about client usage
func (f *AzureClientFactory) GetClientStatistics() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_cached_clients"] = len(f.clientCache)
	stats["subscription_id"] = f.subscriptionID

	// Group by service
	serviceGroups := make(map[string]int)
	for resourceType := range f.clientCache {
		service := extractServiceFromResourceType(resourceType)
		serviceGroups[service]++
	}
	stats["clients_by_service"] = serviceGroups

	return stats
}

// extractServiceFromResourceType extracts service name from resource type
func extractServiceFromResourceType(resourceType string) string {
	// Microsoft.Compute/virtualMachines -> Compute
	parts := strings.Split(resourceType, "/")
	if len(parts) > 0 {
		provider := parts[0]
		return strings.TrimPrefix(provider, "Microsoft.")
	}
	return "Unknown"
}



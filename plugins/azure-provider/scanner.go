package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AzureResourceScanner handles resource scanning operations
type AzureResourceScanner struct {
	credential      azcore.TokenCredential
	subscriptionID  string
	resourcesClient *armresources.Client
	clientFactory   *AzureClientFactory
	mu              sync.RWMutex
}

// NewAzureResourceScanner creates a new Azure resource scanner
func NewAzureResourceScanner(cred azcore.TokenCredential, subID string) *AzureResourceScanner {
	client, _ := armresources.NewClient(subID, cred, nil)
	return &AzureResourceScanner{
		credential:      cred,
		subscriptionID:  subID,
		resourcesClient: client,
		clientFactory:   NewAzureClientFactory(cred, subID),
	}
}

// ScanService scans resources for a specific service
func (s *AzureResourceScanner) ScanService(ctx context.Context, service string, filters map[string]string) ([]*pb.ResourceRef, error) {
	log.Printf("Scanning service: %s with filters: %v", service, filters)
	
	// For now, let's scan all resources and filter by service
	// This is because Azure ARM filter syntax is limited
	allResources, err := s.scanWithFilter(ctx, "")
	if err != nil {
		return nil, err
	}
	
	// Map common service names to Azure resource provider namespaces
	serviceMapping := map[string][]string{
		"storage":   {"Microsoft.Storage/storageAccounts"},
		"compute":   {"Microsoft.Compute/virtualMachines", "Microsoft.Compute/disks", "Microsoft.Compute/virtualMachineScaleSets"},
		"network":   {"Microsoft.Network/virtualNetworks", "Microsoft.Network/networkInterfaces", "Microsoft.Network/publicIPAddresses", "Microsoft.Network/privateDnsZones", "Microsoft.Network/networkSecurityGroups"},
		"keyvault":  {"Microsoft.KeyVault/vaults"},
		"sql":       {"Microsoft.Sql/servers", "Microsoft.Sql/servers/databases"},
		"cosmosdb":  {"Microsoft.DocumentDB/databaseAccounts"},
		"appservice": {"Microsoft.Web/sites", "Microsoft.Web/serverFarms"},
		"functions": {"Microsoft.Web/sites"},
		"aks":       {"Microsoft.ContainerService/managedClusters"},
		"containerregistry": {"Microsoft.ContainerRegistry/registries"},
		"monitor":   {"Microsoft.Insights/components", "Microsoft.OperationalInsights/workspaces"},
		"eventhub":  {"Microsoft.EventHub/namespaces"},
		"managedidentity": {"Microsoft.ManagedIdentity/userAssignedIdentities"},
	}
	
	// Get the Azure resource types for this service
	resourceTypes := serviceMapping[strings.ToLower(service)]
	if len(resourceTypes) == 0 {
		// If no mapping, try to match by provider namespace
		resourceTypes = []string{fmt.Sprintf("Microsoft.%s/", strings.Title(service))}
	}
	
	// Filter resources by type
	var filteredResources []*pb.ResourceRef
	for _, resource := range allResources {
		for _, targetType := range resourceTypes {
			if strings.HasPrefix(resource.Type, targetType) {
				// Apply additional filters
				if rgFilter, ok := filters["resource_group"]; ok {
					if rg, ok := resource.BasicAttributes["resource_group"]; ok && rg != rgFilter {
						continue
					}
				}
				if locationFilter, ok := filters["location"]; ok {
					if resource.Region != locationFilter {
						continue
					}
				}
				filteredResources = append(filteredResources, resource)
				break
			}
		}
	}
	
	log.Printf("Found %d resources for service %s", len(filteredResources), service)
	return filteredResources, nil
}

// ScanAllResources scans all resources using ARM API
func (s *AzureResourceScanner) ScanAllResources(ctx context.Context, filters map[string]string) ([]*pb.ResourceRef, error) {
	var filter string
	var filterParts []string

	if rgFilter, ok := filters["resource_group"]; ok {
		filterParts = append(filterParts, fmt.Sprintf("resourceGroup eq '%s'", rgFilter))
	}

	if locationFilter, ok := filters["location"]; ok {
		filterParts = append(filterParts, fmt.Sprintf("location eq '%s'", locationFilter))
	}

	if len(filterParts) > 0 {
		filter = strings.Join(filterParts, " and ")
	}

	return s.scanWithFilter(ctx, filter)
}

// ScanServiceForResources scans a service and returns full Resource objects
func (s *AzureResourceScanner) ScanServiceForResources(ctx context.Context, service string, filters map[string]string) ([]*pb.Resource, error) {
	resourceRefs, err := s.ScanService(ctx, service, filters)
	if err != nil {
		return nil, err
	}

	// Convert ResourceRef to Resource with full details
	resources := make([]*pb.Resource, 0, len(resourceRefs))
	for _, ref := range resourceRefs {
		// Try to get full resource details
		resource, err := s.DescribeResource(ctx, ref)
		if err != nil {
			// If describe fails, fall back to basic conversion
			log.Printf("Failed to describe resource %s, using basic data: %v", ref.Id, err)
			resource = &pb.Resource{
				Provider:     "azure",
				Service:      ref.Service,
				Type:         ref.Type,
				Id:           ref.Id,
				Name:         ref.Name,
				Region:       ref.Region,
				Tags:         make(map[string]string),
				DiscoveredAt: timestamppb.Now(),
			}

			// Extract metadata from BasicAttributes
			if ref.BasicAttributes != nil {
				if rg, ok := ref.BasicAttributes["resource_group"]; ok {
					resource.ParentId = rg
				}
				if subID, ok := ref.BasicAttributes["subscription_id"]; ok {
					resource.AccountId = subID
				}

				// Extract tags
				for k, v := range ref.BasicAttributes {
					if strings.HasPrefix(k, "tag_") {
						tagName := strings.TrimPrefix(k, "tag_")
						resource.Tags[tagName] = v
					}
				}

				// Store raw data - prefer full raw_data over just properties
				if rawData, ok := ref.BasicAttributes["raw_data"]; ok {
					resource.RawData = rawData
				} else if props, ok := ref.BasicAttributes["properties"]; ok {
					resource.RawData = props
				}
			}
		}
		
		// Extract relationships from the resource
		relationships := s.extractRelationshipsFromResource(resource, ref)
		for _, rel := range relationships {
			resource.Relationships = append(resource.Relationships, rel)
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// getAPIVersionForResourceType returns the appropriate API version for a resource type
func (s *AzureResourceScanner) getAPIVersionForResourceType(resourceType string) string {
	// Map of resource types to their recommended API versions
	apiVersionMap := map[string]string{
		"Microsoft.Storage/storageAccounts":           "2023-01-01",
		"Microsoft.Compute/virtualMachines":           "2023-03-01",
		"Microsoft.Network/virtualNetworks":           "2023-05-01",
		"Microsoft.KeyVault/vaults":                   "2023-02-01",
		"Microsoft.EventHub/namespaces":               "2021-11-01",
		"Microsoft.Web/sites":                         "2022-09-01",
		"Microsoft.ContainerService/managedClusters":  "2023-05-01",
		"Microsoft.Sql/servers":                       "2022-05-01-preview",
		"Microsoft.DocumentDB/databaseAccounts":       "2023-04-15",
	}
	
	// Check for exact match
	if version, ok := apiVersionMap[resourceType]; ok {
		return version
	}
	
	// Check for partial match (e.g., Microsoft.Storage/* resources)
	parts := strings.Split(resourceType, "/")
	if len(parts) >= 2 {
		provider := parts[0] + "/" + parts[1]
		for key, version := range apiVersionMap {
			if strings.HasPrefix(key, provider) {
				return version
			}
		}
	}
	
	// Default fallback
	return "2023-01-01"
}

// DescribeResource provides detailed information about a specific resource
func (s *AzureResourceScanner) DescribeResource(ctx context.Context, resourceRef *pb.ResourceRef) (*pb.Resource, error) {
	if resourceRef == nil || resourceRef.Id == "" {
		return nil, fmt.Errorf("resource reference is required")
	}

	log.Printf("DEBUG: DescribeResource called for %s (type: %s)", resourceRef.Id, resourceRef.Type)
	
	// Determine the appropriate API version
	apiVersion := s.getAPIVersionForResourceType(resourceRef.Type)
	log.Printf("DEBUG: Using API version %s for resource type %s", apiVersion, resourceRef.Type)

	// Get resource details using ARM API
	result, err := s.resourcesClient.GetByID(ctx, resourceRef.Id, apiVersion, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource %s with API version %s: %w", resourceRef.Id, apiVersion, err)
	}
	
	log.Printf("DEBUG: ARM API returned data for %s", resourceRef.Id)

	resource := &pb.Resource{
		Provider:     "azure",
		Service:      resourceRef.Service,
		Type:         resourceRef.Type,
		Id:           resourceRef.Id,
		Name:         resourceRef.Name,
		Region:       resourceRef.Region,
		Tags:         make(map[string]string),
		DiscoveredAt: timestamppb.Now(),
	}

	// Extract detailed information
	if result.ID != nil {
		resource.Id = *result.ID
	}
	if result.Name != nil {
		resource.Name = *result.Name
	}
	if result.Type != nil {
		resource.Type = *result.Type
		resource.Service = s.extractServiceFromType(*result.Type)
	}
	if result.Location != nil {
		resource.Region = *result.Location
	}

	// Extract resource group from ID
	if resourceGroup := s.extractResourceGroupFromID(resource.Id); resourceGroup != "" {
		resource.ParentId = resourceGroup
	}

	// Extract subscription ID
	resource.AccountId = s.subscriptionID

	// Extract tags
	if result.Tags != nil {
		for k, v := range result.Tags {
			if v != nil {
				resource.Tags[k] = *v
			}
		}
	}

	// Build complete resource structure with all fields
	fullResource := map[string]interface{}{
		"id":         resource.Id,
		"name":       resource.Name,
		"type":       resource.Type,
		"location":   resource.Region,
		"tags":       resource.Tags,
		"properties": result.Properties,
	}
	
	// Add SKU if present
	if result.SKU != nil {
		skuData := map[string]interface{}{}
		if result.SKU.Name != nil {
			skuData["name"] = *result.SKU.Name
		}
		if result.SKU.Tier != nil {
			skuData["tier"] = *result.SKU.Tier
		}
		if result.SKU.Size != nil {
			skuData["size"] = *result.SKU.Size
		}
		if result.SKU.Family != nil {
			skuData["family"] = *result.SKU.Family
		}
		if result.SKU.Capacity != nil {
			skuData["capacity"] = *result.SKU.Capacity
		}
		fullResource["sku"] = skuData
	}
	
	// Add other fields if present
	if result.Kind != nil {
		fullResource["kind"] = *result.Kind
	}
	// Note: Etag is not available in the current SDK version
	if result.Plan != nil {
		fullResource["plan"] = result.Plan
	}
	if result.Identity != nil {
		fullResource["identity"] = result.Identity
	}
	if result.ManagedBy != nil {
		fullResource["managedBy"] = *result.ManagedBy
	}
	
	// Store the complete resource data as raw data
	if fullData, err := json.Marshal(fullResource); err == nil {
		resource.RawData = string(fullData)
		log.Printf("DEBUG: Stored complete resource data for %s, length: %d", resource.Name, len(resource.RawData))
	} else {
		log.Printf("DEBUG: Failed to marshal resource data for %s: %v", resource.Name, err)
	}

	return resource, nil
}

// StreamScanResources streams resources as they are discovered
func (s *AzureResourceScanner) StreamScanResources(ctx context.Context, services []string, resourceChan chan<- *pb.Resource) error {
	defer close(resourceChan)

	for _, service := range services {
		resources, err := s.ScanServiceForResources(ctx, service, nil)
		if err != nil {
			log.Printf("Failed to scan service %s: %v", service, err)
			continue
		}

		for _, resource := range resources {
			select {
			case resourceChan <- resource:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// scanWithFilter scans resources with the given OData filter
func (s *AzureResourceScanner) scanWithFilter(ctx context.Context, filter string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef

	options := &armresources.ClientListOptions{}
	if filter != "" {
		options.Filter = &filter
	}

	pager := s.resourcesClient.NewListPager(options)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list resources: %w", err)
		}

		for _, resource := range page.Value {
			ref := s.convertToResourceRef(*resource)
			if ref != nil {
				resources = append(resources, ref)
			}
		}
	}

	return resources, nil
}

// convertToResourceRef converts ARM resource to ResourceRef
func (s *AzureResourceScanner) convertToResourceRef(resource armresources.GenericResourceExpanded) *pb.ResourceRef {
	if resource.ID == nil || resource.Name == nil || resource.Type == nil {
		return nil
	}

	ref := &pb.ResourceRef{
		Id:              *resource.ID,
		Name:            *resource.Name,
		Type:            *resource.Type,
		Service:         s.extractServiceFromType(*resource.Type),
		BasicAttributes: make(map[string]string),
	}

	// Extract location
	if resource.Location != nil {
		ref.Region = *resource.Location
	}

	// Extract resource group from ID
	if resourceGroup := s.extractResourceGroupFromID(*resource.ID); resourceGroup != "" {
		ref.BasicAttributes["resource_group"] = resourceGroup
	}

	// Add subscription ID
	ref.BasicAttributes["subscription_id"] = s.subscriptionID

	// Add tags as metadata
	if resource.Tags != nil {
		for k, v := range resource.Tags {
			if v != nil {
				ref.BasicAttributes["tag_"+k] = *v
			}
		}
	}

	// Store properties as JSON
	if resource.Properties != nil {
		if propsJSON, err := json.Marshal(resource.Properties); err == nil {
			ref.BasicAttributes["properties"] = string(propsJSON)
		}
	}

	return ref
}

// extractServiceFromType extracts service name from resource type
func (s *AzureResourceScanner) extractServiceFromType(resourceType string) string {
	// Microsoft.Compute/virtualMachines -> compute
	parts := strings.Split(resourceType, "/")
	if len(parts) > 0 {
		provider := parts[0]
		return strings.ToLower(strings.TrimPrefix(provider, "Microsoft."))
	}
	return "unknown"
}

// extractResourceGroupFromID extracts resource group name from resource ID
func (s *AzureResourceScanner) extractResourceGroupFromID(resourceID string) string {
	// Format: /subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}


// matchesTags checks if a resource matches the specified tags
func (s *AzureResourceScanner) matchesTags(resource *pb.ResourceRef, tags map[string]string) bool {
	if resource.BasicAttributes == nil {
		return len(tags) == 0
	}

	for key, value := range tags {
		tagKey := "tag_" + key
		if resourceValue, exists := resource.BasicAttributes[tagKey]; !exists || resourceValue != value {
			return false
		}
	}

	return true
}


// extractProviderFromType extracts provider namespace from resource type
func (s *AzureResourceScanner) extractProviderFromType(resourceType string) string {
	// Microsoft.Compute/virtualMachines -> Microsoft.Compute
	parts := strings.Split(resourceType, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}


// extractRelationshipsFromResource extracts relationships from an Azure resource
func (s *AzureResourceScanner) extractRelationshipsFromResource(resource *pb.Resource, ref *pb.ResourceRef) []*pb.Relationship {
	relationships := []*pb.Relationship{}
	
	// Extract parent-child relationships from resource ID structure
	// Azure resource IDs follow pattern: /subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}
	if resource.ParentId != "" && resource.ParentId != resource.Id {
		relationships = append(relationships, &pb.Relationship{
			TargetId:         resource.ParentId,
			TargetType:       "resourceGroup",
			RelationshipType: "child_of",
		})
	}
	
	// Extract relationships from properties if available
	if ref.BasicAttributes != nil && ref.BasicAttributes["properties"] != "" {
		var props map[string]interface{}
		if err := json.Unmarshal([]byte(ref.BasicAttributes["properties"]), &props); err == nil {
			// Virtual Machine specific relationships
			if strings.Contains(resource.Type, "Microsoft.Compute/virtualMachines") {
				s.extractVMRelationships(resource.Id, props, &relationships)
			}
			
			// Storage Account relationships
			if strings.Contains(resource.Type, "Microsoft.Storage/storageAccounts") {
				s.extractStorageRelationships(resource.Id, props, &relationships)
			}
			
			// Virtual Network relationships
			if strings.Contains(resource.Type, "Microsoft.Network/virtualNetworks") {
				s.extractVNetRelationships(resource.Id, props, &relationships)
			}
			
			// Network Interface relationships
			if strings.Contains(resource.Type, "Microsoft.Network/networkInterfaces") {
				s.extractNICRelationships(resource.Id, props, &relationships)
			}
			
			// Key Vault relationships
			if strings.Contains(resource.Type, "Microsoft.KeyVault/vaults") {
				s.extractKeyVaultRelationships(resource.Id, props, &relationships)
			}
		}
	}
	
	return relationships
}

// Helper methods for extracting specific resource type relationships

func (s *AzureResourceScanner) extractVMRelationships(vmID string, props map[string]interface{}, relationships *[]*pb.Relationship) {
	// Network interfaces
	if netProfile, ok := props["networkProfile"].(map[string]interface{}); ok {
		if interfaces, ok := netProfile["networkInterfaces"].([]interface{}); ok {
			for _, nic := range interfaces {
				if nicMap, ok := nic.(map[string]interface{}); ok {
					if nicID, ok := nicMap["id"].(string); ok {
						*relationships = append(*relationships, &pb.Relationship{
							TargetId:         nicID,
							TargetType:       "Microsoft.Network/networkInterfaces",
							RelationshipType: "uses",
						})
					}
				}
			}
		}
	}
	
	// Availability Set
	if availSet, ok := props["availabilitySet"].(map[string]interface{}); ok {
		if availSetID, ok := availSet["id"].(string); ok {
			*relationships = append(*relationships, &pb.Relationship{
				TargetId:         availSetID,
				TargetType:       "Microsoft.Compute/availabilitySets",
				RelationshipType: "member_of",
			})
		}
	}
	
	// Storage Profile - OS and Data Disks
	if storageProfile, ok := props["storageProfile"].(map[string]interface{}); ok {
		// OS Disk
		if osDisk, ok := storageProfile["osDisk"].(map[string]interface{}); ok {
			if managedDisk, ok := osDisk["managedDisk"].(map[string]interface{}); ok {
				if diskID, ok := managedDisk["id"].(string); ok {
					*relationships = append(*relationships, &pb.Relationship{
						TargetId:         diskID,
						TargetType:       "Microsoft.Compute/disks",
						RelationshipType: "uses",
					})
				}
			}
		}
		
		// Data Disks
		if dataDisks, ok := storageProfile["dataDisks"].([]interface{}); ok {
			for _, disk := range dataDisks {
				if diskMap, ok := disk.(map[string]interface{}); ok {
					if managedDisk, ok := diskMap["managedDisk"].(map[string]interface{}); ok {
						if diskID, ok := managedDisk["id"].(string); ok {
							*relationships = append(*relationships, &pb.Relationship{
								TargetId:         diskID,
								TargetType:       "Microsoft.Compute/disks",
								RelationshipType: "uses",
							})
						}
					}
				}
			}
		}
	}
}

func (s *AzureResourceScanner) extractStorageRelationships(storageID string, props map[string]interface{}, relationships *[]*pb.Relationship) {
	// Private endpoints
	if privateEndpoints, ok := props["privateEndpointConnections"].([]interface{}); ok {
		for _, pe := range privateEndpoints {
			if peMap, ok := pe.(map[string]interface{}); ok {
				if peProps, ok := peMap["properties"].(map[string]interface{}); ok {
					if privateEndpoint, ok := peProps["privateEndpoint"].(map[string]interface{}); ok {
						if peID, ok := privateEndpoint["id"].(string); ok {
							*relationships = append(*relationships, &pb.Relationship{
								TargetId:         peID,
								TargetType:       "Microsoft.Network/privateEndpoints",
								RelationshipType: "connected_to",
							})
						}
					}
				}
			}
		}
	}
}

func (s *AzureResourceScanner) extractVNetRelationships(vnetID string, props map[string]interface{}, relationships *[]*pb.Relationship) {
	// Subnets (parent-child relationship)
	if subnets, ok := props["subnets"].([]interface{}); ok {
		for _, subnet := range subnets {
			if subnetMap, ok := subnet.(map[string]interface{}); ok {
				if subnetID, ok := subnetMap["id"].(string); ok {
					*relationships = append(*relationships, &pb.Relationship{
						TargetId:         subnetID,
						TargetType:       "Microsoft.Network/virtualNetworks/subnets",
						RelationshipType: "contains",
					})
				}
			}
		}
	}
	
	// VNet Peerings
	if peerings, ok := props["virtualNetworkPeerings"].([]interface{}); ok {
		for _, peering := range peerings {
			if peerMap, ok := peering.(map[string]interface{}); ok {
				if peerProps, ok := peerMap["properties"].(map[string]interface{}); ok {
					if remoteVnet, ok := peerProps["remoteVirtualNetwork"].(map[string]interface{}); ok {
						if vnetID, ok := remoteVnet["id"].(string); ok {
							*relationships = append(*relationships, &pb.Relationship{
								TargetId:         vnetID,
								TargetType:       "Microsoft.Network/virtualNetworks",
								RelationshipType: "peered_with",
							})
						}
					}
				}
			}
		}
	}
}

func (s *AzureResourceScanner) extractNICRelationships(nicID string, props map[string]interface{}, relationships *[]*pb.Relationship) {
	// IP Configurations - Subnet and Public IP relationships
	if ipConfigs, ok := props["ipConfigurations"].([]interface{}); ok {
		for _, ipConfig := range ipConfigs {
			if ipConfigMap, ok := ipConfig.(map[string]interface{}); ok {
				if ipConfigProps, ok := ipConfigMap["properties"].(map[string]interface{}); ok {
					// Subnet relationship
					if subnet, ok := ipConfigProps["subnet"].(map[string]interface{}); ok {
						if subnetID, ok := subnet["id"].(string); ok {
							*relationships = append(*relationships, &pb.Relationship{
								TargetId:         subnetID,
								TargetType:       "Microsoft.Network/virtualNetworks/subnets",
								RelationshipType: "attached_to",
							})
						}
					}
					
					// Public IP relationship
					if publicIP, ok := ipConfigProps["publicIPAddress"].(map[string]interface{}); ok {
						if publicIPID, ok := publicIP["id"].(string); ok {
							*relationships = append(*relationships, &pb.Relationship{
								TargetId:         publicIPID,
								TargetType:       "Microsoft.Network/publicIPAddresses",
								RelationshipType: "uses",
							})
						}
					}
				}
			}
		}
	}
	
	// Network Security Group
	if nsg, ok := props["networkSecurityGroup"].(map[string]interface{}); ok {
		if nsgID, ok := nsg["id"].(string); ok {
			*relationships = append(*relationships, &pb.Relationship{
				TargetId:         nsgID,
				TargetType:       "Microsoft.Network/networkSecurityGroups",
				RelationshipType: "protected_by",
			})
		}
	}
}

func (s *AzureResourceScanner) extractKeyVaultRelationships(kvID string, props map[string]interface{}, relationships *[]*pb.Relationship) {
	// Access policies could reference other resources
	if accessPolicies, ok := props["accessPolicies"].([]interface{}); ok {
		for _, policy := range accessPolicies {
			if policyMap, ok := policy.(map[string]interface{}); ok {
				if objectID, ok := policyMap["objectId"].(string); ok {
					// This could be a user, group, or service principal
					*relationships = append(*relationships, &pb.Relationship{
						TargetId:         objectID,
						TargetType:       "identity",
						RelationshipType: "grants_access_to",
					})
				}
			}
		}
	}
}


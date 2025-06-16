// Package resourcegraph provides Azure Resource Graph integration for efficient resource discovery
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ResourceSchema represents the discovered schema for a resource type
type ResourceSchema struct {
	ResourceType string                 `json:"resource_type"`
	Properties   map[string]PropertyDef `json:"properties"`
	CommonTags   []string               `json:"common_tags"`
	Locations    []string               `json:"locations"`
	SampleCount  int                    `json:"sample_count"`
}

// PropertyDef defines a property in the resource schema
type PropertyDef struct {
	Name     string        `json:"name"`
	Type     string        `json:"type"` // string, number, boolean, object, array
	Required bool          `json:"required"`
	Examples []string      `json:"examples"`
	Nested   []PropertyDef `json:"nested,omitempty"`
}

// RelationshipPattern represents a discovered relationship pattern
type RelationshipPattern struct {
	SourceType        string   `json:"source_type"`
	TargetType        string   `json:"target_type"`
	RelationshipCount int      `json:"relationship_count"`
	SampleReferences  []string `json:"sample_references"`
	RelationshipType  string   `json:"relationship_type"` // DEPENDS_ON, CONTAINS, REFERENCES
}

// ResourceGraphClient provides efficient resource querying using Azure Resource Graph
type ResourceGraphClient struct {
	client         *armresourcegraph.Client
	subscriptions  []string
	queryCache     *QueryCache
	queryOptimizer *QueryOptimizer
	mu             sync.RWMutex
}

// NewResourceGraphClient creates a new Resource Graph client
func NewResourceGraphClient(credential azcore.TokenCredential, subscriptions []string) (*ResourceGraphClient, error) {
	client, err := armresourcegraph.NewClient(credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource graph client: %w", err)
	}

	return &ResourceGraphClient{
		client:         client,
		subscriptions:  subscriptions,
		queryCache:     NewQueryCache(15 * time.Minute),
		queryOptimizer: NewQueryOptimizer(),
	}, nil
}

// QueryAllResources queries all resources efficiently using Resource Graph
func (c *ResourceGraphClient) QueryAllResources(ctx context.Context) ([]*pb.ResourceRef, error) {
	query := `
	Resources
	| project id, name, type, location, resourceGroup, subscriptionId, tags, properties, kind, sku, plan, identity, zones, extendedLocation, managedBy, createdTime, changedTime
	| order by type asc, name asc
	`

	return c.executeQuery(ctx, query)
}

// DiscoverAllResourceTypes discovers all available resource types dynamically
func (c *ResourceGraphClient) DiscoverAllResourceTypes(ctx context.Context) ([]*pb.ServiceInfo, error) {
	// Query to discover all resource types with their schemas
	query := `
	Resources
	| summarize
		ResourceCount = count(),
		SampleProperties = any(properties),
		Locations = make_set(location),
		ResourceGroups = make_set(resourceGroup)
		by type
	| extend
		Provider = split(type, '/')[0],
		Service = split(type, '/')[1],
		ResourceType = split(type, '/')[2]
	| where isnotempty(Service) and isnotempty(ResourceType)
	| project
		type,
		Provider,
		Service,
		ResourceType,
		ResourceCount,
		SampleProperties,
		Locations,
		ResourceGroups
	| order by Provider asc, Service asc, ResourceType asc
	`

	return c.executeDiscoveryQuery(ctx, query)
}

// DiscoverResourceSchema discovers the schema for a specific resource type
func (c *ResourceGraphClient) DiscoverResourceSchema(ctx context.Context, resourceType string) (*ResourceSchema, error) {
	// Query to get sample resources and extract schema
	query := fmt.Sprintf(`
	Resources
	| where type == '%s'
	| project properties, tags, id, name, location, resourceGroup
	| limit 10
	`, resourceType)

	resources, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	// Analyze the sample resources to extract schema
	return c.extractSchemaFromSamples(resourceType, resources)
}

// DiscoverResourceRelationshipTypes discovers relationship patterns
func (c *ResourceGraphClient) DiscoverResourceRelationshipTypes(ctx context.Context) ([]*RelationshipPattern, error) {
	// Query to discover common relationship patterns
	query := `
	Resources
	| extend
		ReferencedResources = extract_all(@'\/subscriptions\/[^\/]+\/resourceGroups\/[^\/]+\/providers\/[^\/]+\/[^\/]+\/[^\/\s"]+', properties)
	| where array_length(ReferencedResources) > 0
	| project type, ReferencedResources
	| mv-expand ReferencedResource = ReferencedResources
	| extend ReferencedType = extract(@'\/providers\/([^\/]+\/[^\/]+)', 1, tostring(ReferencedResource))
	| where isnotempty(ReferencedType)
	| summarize
		RelationshipCount = count(),
		SampleReferences = make_set(ReferencedResource, 5)
		by SourceType = type, TargetType = ReferencedType
	| where RelationshipCount >= 2
	| order by RelationshipCount desc
	`

	return c.executeRelationshipDiscoveryQuery(ctx, query)
}

// QueryResourcesByType queries resources of specific types
func (c *ResourceGraphClient) QueryResourcesByType(ctx context.Context, resourceTypes []string) ([]*pb.ResourceRef, error) {
	// Build optimized query
	typeFilter := c.buildTypeFilter(resourceTypes)

	query := fmt.Sprintf(`
	Resources
	| where %s
	| project id, name, type, location, resourceGroup, subscriptionId, tags, properties
	| order by type asc, name asc
	`, typeFilter)

	return c.executeQuery(ctx, query)
}

// QueryResourcesWithFilter queries resources with complex filters
func (c *ResourceGraphClient) QueryResourcesWithFilter(ctx context.Context, filters map[string]interface{}) ([]*pb.ResourceRef, error) {
	// Build KQL query from filters
	query := c.queryOptimizer.BuildQuery(filters)

	// Check cache first
	cacheKey := c.generateCacheKey(query, c.subscriptions)
	if cached, found := c.queryCache.Get(cacheKey); found {
		return cached, nil
	}

	// Execute query
	resources, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	// Cache results
	c.queryCache.Set(cacheKey, resources)

	return resources, nil
}

// QueryResourceChanges queries for resource changes since a specific time
func (c *ResourceGraphClient) QueryResourceChanges(ctx context.Context, since time.Time) ([]*ResourceChange, error) {
	query := fmt.Sprintf(`
	resourcechanges
	| where timestamp > datetime(%s)
	| project timestamp, changeType, targetResourceId, targetResourceType, changes
	| order by timestamp desc
	`, since.Format(time.RFC3339))

	// Convert []string to []*string
	subscriptions := make([]*string, len(c.subscriptions))
	for i, sub := range c.subscriptions {
		subscriptions[i] = to.Ptr(sub)
	}

	request := armresourcegraph.QueryRequest{
		Query:         to.Ptr(query),
		Subscriptions: subscriptions,
		Options: &armresourcegraph.QueryRequestOptions{
			ResultFormat: to.Ptr(armresourcegraph.ResultFormatObjectArray),
		},
	}

	result, err := c.client.Resources(ctx, request, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query resource changes: %w", err)
	}

	return c.parseChangeResults(&result.QueryResponse)
}

// QueryResourceRelationships discovers relationships between resources
func (c *ResourceGraphClient) QueryResourceRelationships(ctx context.Context, resourceID string) ([]*ResourceRelationship, error) {
	// Query for resources that reference the given resource ID
	query := fmt.Sprintf(`
	Resources
	| where properties contains '%s' or dependsOn contains '%s'
	| project id, name, type, properties
	| limit 100
	`, resourceID, resourceID)

	resources, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	// Analyze properties to find relationships
	relationships := c.analyzeRelationships(resourceID, resources)

	return relationships, nil
}

// QueryResourcesByTags queries resources with specific tags
func (c *ResourceGraphClient) QueryResourcesByTags(ctx context.Context, tags map[string]string) ([]*pb.ResourceRef, error) {
	tagFilters := []string{}
	for key, value := range tags {
		if value == "" {
			tagFilters = append(tagFilters, fmt.Sprintf("tags contains '%s'", key))
		} else {
			tagFilters = append(tagFilters, fmt.Sprintf("tags['%s'] == '%s'", key, value))
		}
	}

	query := fmt.Sprintf(`
	Resources
	| where %s
	| project id, name, type, location, resourceGroup, subscriptionId, tags, properties
	| order by type asc, name asc
	`, strings.Join(tagFilters, " and "))

	return c.executeQuery(ctx, query)
}

// QueryResourceCosts queries cost information for resources (if available)
func (c *ResourceGraphClient) QueryResourceCosts(ctx context.Context, resourceGroup string) ([]*ResourceCost, error) {
	// This would integrate with Azure Cost Management APIs
	// For now, return a placeholder implementation
	query := fmt.Sprintf(`
	Resources
	| where resourceGroup == '%s'
	| project id, name, type, location, tags
	| join kind=leftouter (
		ResourceContainers
		| where type == 'microsoft.resources/subscriptions/resourcegroups'
		| project resourceGroup=name, subscriptionId
	) on resourceGroup
	`, resourceGroup)

	resources, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	// Convert to cost information (would need actual cost data)
	costs := make([]*ResourceCost, 0, len(resources))
	for _, resource := range resources {
		costs = append(costs, &ResourceCost{
			ResourceID:   resource.Id,
			ResourceType: resource.Type,
			Currency:     "USD",
			// Actual cost data would come from Cost Management API
		})
	}

	return costs, nil
}

// executeQuery executes a KQL query with pagination support
func (c *ResourceGraphClient) executeQuery(ctx context.Context, query string) ([]*pb.ResourceRef, error) {
	var allResources []*pb.ResourceRef
	skipToken := ""

	// Convert []string to []*string
	subscriptions := make([]*string, len(c.subscriptions))
	for i, sub := range c.subscriptions {
		subscriptions[i] = to.Ptr(sub)
	}

	for {
		request := armresourcegraph.QueryRequest{
			Query:         to.Ptr(query),
			Subscriptions: subscriptions,
			Options: &armresourcegraph.QueryRequestOptions{
				ResultFormat: to.Ptr(armresourcegraph.ResultFormatObjectArray),
				Top:          to.Ptr(int32(1000)), // Max results per page
			},
		}

		if skipToken != "" {
			request.Options.SkipToken = to.Ptr(skipToken)
		}

		result, err := c.client.Resources(ctx, request, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to execute resource graph query: %w", err)
		}

		// Parse results
		resources, err := c.parseQueryResults(&result.QueryResponse)
		if err != nil {
			return nil, err
		}

		allResources = append(allResources, resources...)

		// Check if there are more results
		if result.SkipToken == nil || *result.SkipToken == "" {
			break
		}
		skipToken = *result.SkipToken
	}

	return allResources, nil
}

// parseQueryResults parses Resource Graph query results
func (c *ResourceGraphClient) parseQueryResults(result *armresourcegraph.QueryResponse) ([]*pb.ResourceRef, error) {
	if result.Data == nil {
		return nil, nil
	}

	// Resource Graph returns data as []interface{}
	data, ok := result.Data.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result format")
	}

	resources := make([]*pb.ResourceRef, 0, len(data))

	for _, item := range data {
		resourceMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// Debug log the first resource to see what fields are available
		if len(resources) == 0 {
			fmt.Printf("DEBUG: Resource Graph returned fields: %v\n", resourceMap)
		}

		resource := c.parseResourceMap(resourceMap)
		if resource != nil {
			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// parseResourceMap converts a resource map to ResourceRef
func (c *ResourceGraphClient) parseResourceMap(resourceMap map[string]interface{}) *pb.ResourceRef {
	resource := &pb.ResourceRef{
		BasicAttributes: make(map[string]string),
	}

	// Extract standard fields
	if id, ok := resourceMap["id"].(string); ok {
		resource.Id = id
	}
	if name, ok := resourceMap["name"].(string); ok {
		resource.Name = name
	}
	if resourceType, ok := resourceMap["type"].(string); ok {
		resource.Type = resourceType
		resource.Service = c.extractServiceFromType(resourceType)
	}
	if location, ok := resourceMap["location"].(string); ok {
		resource.Region = location
	}
	if rg, ok := resourceMap["resourceGroup"].(string); ok {
		resource.BasicAttributes["resource_group"] = rg
	}
	if subID, ok := resourceMap["subscriptionId"].(string); ok {
		resource.BasicAttributes["subscription_id"] = subID
	}

	// Extract tags
	if tags, ok := resourceMap["tags"].(map[string]interface{}); ok {
		for k, v := range tags {
			if strVal, ok := v.(string); ok {
				resource.BasicAttributes["tag_"+k] = strVal
			}
		}
	}

	// Store properties as JSON
	if properties, ok := resourceMap["properties"].(map[string]interface{}); ok {
		if propsJSON, err := json.Marshal(properties); err == nil {
			resource.BasicAttributes["properties"] = string(propsJSON)
		}
	}

	// Store the entire resource data as raw_data for complete information
	if rawDataJSON, err := json.Marshal(resourceMap); err == nil {
		resource.BasicAttributes["raw_data"] = string(rawDataJSON)
	}

	// Extract additional fields
	if kind, ok := resourceMap["kind"].(string); ok {
		resource.BasicAttributes["kind"] = kind
	}
	if managedBy, ok := resourceMap["managedBy"].(string); ok {
		resource.BasicAttributes["managed_by"] = managedBy
	}
	if createdTime, ok := resourceMap["createdTime"].(string); ok {
		resource.BasicAttributes["created_time"] = createdTime
	}
	if changedTime, ok := resourceMap["changedTime"].(string); ok {
		resource.BasicAttributes["changed_time"] = changedTime
	}

	// Extract SKU information
	if sku, ok := resourceMap["sku"].(map[string]interface{}); ok {
		if skuJSON, err := json.Marshal(sku); err == nil {
			resource.BasicAttributes["sku"] = string(skuJSON)
		}
	}

	return resource
}

// buildTypeFilter builds a KQL filter for resource types
func (c *ResourceGraphClient) buildTypeFilter(resourceTypes []string) string {
	if len(resourceTypes) == 0 {
		return "true"
	}

	filters := make([]string, 0, len(resourceTypes))
	for _, rt := range resourceTypes {
		filters = append(filters, fmt.Sprintf("type == '%s'", rt))
	}

	return strings.Join(filters, " or ")
}

// extractServiceFromType extracts service name from resource type
func (c *ResourceGraphClient) extractServiceFromType(resourceType string) string {
	parts := strings.Split(strings.ToLower(resourceType), "/")
	if len(parts) > 0 {
		provider := parts[0]
		return strings.TrimPrefix(provider, "microsoft.")
	}
	return "unknown"
}

// generateCacheKey generates a cache key for queries
func (c *ResourceGraphClient) generateCacheKey(query string, subscriptions []string) string {
	return fmt.Sprintf("%x_%s", hashString(query), strings.Join(subscriptions, "_"))
}

// QueryOptimizer optimizes KQL queries for performance
type QueryOptimizer struct {
	commonPatterns map[string]string
}

func NewQueryOptimizer() *QueryOptimizer {
	return &QueryOptimizer{
		commonPatterns: map[string]string{
			"vm_with_size": `
				Resources
				| where type == "microsoft.compute/virtualmachines"
				| extend vmSize = properties.hardwareProfile.vmSize
				| project id, name, location, resourceGroup, vmSize, properties
			`,
			"storage_with_tier": `
				Resources
				| where type == "microsoft.storage/storageaccounts"
				| extend tier = sku.tier, kind = kind
				| project id, name, location, resourceGroup, tier, kind, properties
			`,
			"network_with_subnets": `
				Resources
				| where type == "microsoft.network/virtualnetworks"
				| extend subnetCount = array_length(properties.subnets)
				| project id, name, location, resourceGroup, subnetCount, properties
			`,
		},
	}
}

func (o *QueryOptimizer) BuildQuery(filters map[string]interface{}) string {
	// Start with base query
	query := "Resources"

	// Add filters
	var whereConditions []string

	for key, value := range filters {
		switch key {
		case "type":
			if types, ok := value.([]string); ok {
				typeFilters := make([]string, 0, len(types))
				for _, t := range types {
					typeFilters = append(typeFilters, fmt.Sprintf("type == '%s'", t))
				}
				whereConditions = append(whereConditions, "("+strings.Join(typeFilters, " or ")+")")
			} else if typeStr, ok := value.(string); ok {
				whereConditions = append(whereConditions, fmt.Sprintf("type == '%s'", typeStr))
			}
		case "location":
			whereConditions = append(whereConditions, fmt.Sprintf("location == '%s'", value))
		case "resourceGroup":
			whereConditions = append(whereConditions, fmt.Sprintf("resourceGroup == '%s'", value))
		case "tag":
			if tagMap, ok := value.(map[string]string); ok {
				for k, v := range tagMap {
					whereConditions = append(whereConditions, fmt.Sprintf("tags['%s'] == '%s'", k, v))
				}
			}
		}
	}

	// Build query
	if len(whereConditions) > 0 {
		query += "\n| where " + strings.Join(whereConditions, " and ")
	}

	// Default projection with all available fields
	query += "\n| project id, name, type, location, resourceGroup, subscriptionId, tags, properties, kind, sku, plan, identity, zones, extendedLocation, managedBy, createdTime, changedTime"

	return query
}

// QueryCache provides caching for Resource Graph queries
type QueryCache struct {
	cache map[string]*CacheEntry
	ttl   time.Duration
	mu    sync.RWMutex
}

type CacheEntry struct {
	Resources []*pb.ResourceRef
	Timestamp time.Time
}

func NewQueryCache(ttl time.Duration) *QueryCache {
	return &QueryCache{
		cache: make(map[string]*CacheEntry),
		ttl:   ttl,
	}
}

func (c *QueryCache) Get(key string) ([]*pb.ResourceRef, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.cache[key]
	if !exists {
		return nil, false
	}

	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}

	return entry.Resources, true
}

func (c *QueryCache) Set(key string, resources []*pb.ResourceRef) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[key] = &CacheEntry{
		Resources: resources,
		Timestamp: time.Now(),
	}
}

// Supporting types
type ResourceChange struct {
	Timestamp          time.Time
	ChangeType         string
	TargetResourceID   string
	TargetResourceType string
	Changes            map[string]interface{}
}

type ResourceRelationship struct {
	SourceID     string
	TargetID     string
	RelationType string
	Direction    string
}

type ResourceCost struct {
	ResourceID   string
	ResourceType string
	Currency     string
	MonthlyCost  float64
	DailyCost    float64
	Tags         map[string]string
}

// parseChangeResults parses resource change results
func (c *ResourceGraphClient) parseChangeResults(result *armresourcegraph.QueryResponse) ([]*ResourceChange, error) {
	if result.Data == nil {
		return nil, nil
	}

	data, ok := result.Data.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected change result format")
	}

	changes := make([]*ResourceChange, 0, len(data))

	for _, item := range data {
		changeMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		change := &ResourceChange{
			Changes: make(map[string]interface{}),
		}

		// Parse change fields
		if ts, ok := changeMap["timestamp"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
				change.Timestamp = parsed
			}
		}
		if ct, ok := changeMap["changeType"].(string); ok {
			change.ChangeType = ct
		}
		if id, ok := changeMap["targetResourceId"].(string); ok {
			change.TargetResourceID = id
		}
		if rt, ok := changeMap["targetResourceType"].(string); ok {
			change.TargetResourceType = rt
		}
		if ch, ok := changeMap["changes"].(map[string]interface{}); ok {
			change.Changes = ch
		}

		changes = append(changes, change)
	}

	return changes, nil
}

// analyzeRelationships analyzes resources to find relationships
func (c *ResourceGraphClient) analyzeRelationships(resourceID string, resources []*pb.ResourceRef) []*ResourceRelationship {
	relationships := make([]*ResourceRelationship, 0)

	for _, resource := range resources {
		if resource.Id == resourceID {
			continue // Skip self
		}

		// Analyze properties for references
		if propsJSON, ok := resource.BasicAttributes["properties"]; ok {
			var properties map[string]interface{}
			if err := json.Unmarshal([]byte(propsJSON), &properties); err == nil {
				// Look for references to the resource ID
				if c.containsReference(properties, resourceID) {
					relationships = append(relationships, &ResourceRelationship{
						SourceID:     resource.Id,
						TargetID:     resourceID,
						RelationType: "references",
						Direction:    "outbound",
					})
				}
			}
		}
	}

	return relationships
}

// containsReference checks if a data structure contains a reference to a resource ID
func (c *ResourceGraphClient) containsReference(data interface{}, resourceID string) bool {
	switch v := data.(type) {
	case string:
		return strings.Contains(v, resourceID)
	case map[string]interface{}:
		for _, value := range v {
			if c.containsReference(value, resourceID) {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if c.containsReference(item, resourceID) {
				return true
			}
		}
	}
	return false
}

// hashString creates a simple hash of a string
func hashString(s string) uint32 {
	var h uint32
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return h
}

// executeDiscoveryQuery executes a discovery query and converts results to ServiceInfo
func (c *ResourceGraphClient) executeDiscoveryQuery(ctx context.Context, query string) ([]*pb.ServiceInfo, error) {
	subscriptions := make([]*string, len(c.subscriptions))
	for i, sub := range c.subscriptions {
		subscriptions[i] = to.Ptr(sub)
	}

	request := armresourcegraph.QueryRequest{
		Query:         to.Ptr(query),
		Subscriptions: subscriptions,
		Options: &armresourcegraph.QueryRequestOptions{
			ResultFormat: to.Ptr(armresourcegraph.ResultFormatObjectArray),
		},
	}

	result, err := c.client.Resources(ctx, request, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute discovery query: %w", err)
	}

	return c.parseDiscoveryResults(&result.QueryResponse)
}

// parseDiscoveryResults converts Resource Graph results to ServiceInfo
func (c *ResourceGraphClient) parseDiscoveryResults(response *armresourcegraph.QueryResponse) ([]*pb.ServiceInfo, error) {
	if response.Data == nil {
		return nil, fmt.Errorf("no data in response")
	}

	data, ok := response.Data.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected data format")
	}

	serviceMap := make(map[string]*pb.ServiceInfo)

	for _, item := range data {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		provider := getString(row, "Provider")
		service := getString(row, "Service")
		resourceType := getString(row, "ResourceType")
		fullType := getString(row, "type")

		if provider == "" || service == "" || resourceType == "" {
			continue
		}

		// Normalize service name (Microsoft.Compute -> compute)
		serviceName := strings.ToLower(strings.TrimPrefix(service, "Microsoft."))
		serviceKey := fmt.Sprintf("%s.%s", provider, serviceName)

		// Get or create service info
		serviceInfo, exists := serviceMap[serviceKey]
		if !exists {
			serviceInfo = &pb.ServiceInfo{
				Name:          serviceName,
				DisplayName:   service,
				PackageName:   fmt.Sprintf("github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/%s", serviceName),
				ClientType:    fmt.Sprintf("%sClient", strings.Title(serviceName)),
				ResourceTypes: []*pb.ResourceType{},
			}
			serviceMap[serviceKey] = serviceInfo
		}

		// Add resource type
		resourceTypeInfo := &pb.ResourceType{
			Name:              resourceType,
			TypeName:          fullType,
			ListOperation:     "List",
			DescribeOperation: "Get",
			GetOperation:      "Get",
			IdField:           "id",
			NameField:         "name",
			SupportsTags:      true,
			Paginated:         true,
		}

		serviceInfo.ResourceTypes = append(serviceInfo.ResourceTypes, resourceTypeInfo)
	}

	// Convert map to slice
	services := make([]*pb.ServiceInfo, 0, len(serviceMap))
	for _, service := range serviceMap {
		services = append(services, service)
	}

	return services, nil
}

// Helper functions for parsing Resource Graph results
func getString(row map[string]interface{}, key string) string {
	if val, ok := row[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getInt32(row map[string]interface{}, key string) int32 {
	if val, ok := row[key]; ok {
		switch v := val.(type) {
		case int:
			return int32(v)
		case int32:
			return v
		case int64:
			return int32(v)
		case float64:
			return int32(v)
		}
	}
	return 0
}

// extractSchemaFromSamples analyzes sample resources to extract schema
func (c *ResourceGraphClient) extractSchemaFromSamples(resourceType string, samples []*pb.ResourceRef) (*ResourceSchema, error) {
	schema := &ResourceSchema{
		ResourceType: resourceType,
		Properties:   make(map[string]PropertyDef),
		CommonTags:   []string{},
		Locations:    []string{},
		SampleCount:  len(samples),
	}

	locationSet := make(map[string]bool)
	tagSet := make(map[string]bool)
	propertyTypes := make(map[string]map[string]bool)

	for _, sample := range samples {
		// Collect locations
		if sample.Region != "" {
			locationSet[sample.Region] = true
		}

		// Collect tags
		for key := range sample.BasicAttributes {
			if strings.HasPrefix(key, "tag_") {
				tagName := strings.TrimPrefix(key, "tag_")
				tagSet[tagName] = true
			}
		}

		// Analyze properties
		if propsJSON, ok := sample.BasicAttributes["properties"]; ok {
			var properties map[string]interface{}
			if err := json.Unmarshal([]byte(propsJSON), &properties); err == nil {
				c.analyzeProperties(properties, "", propertyTypes)
			}
		}
	}

	// Convert sets to slices
	for location := range locationSet {
		schema.Locations = append(schema.Locations, location)
	}
	for tag := range tagSet {
		schema.CommonTags = append(schema.CommonTags, tag)
	}

	// Convert property analysis to schema
	for propName, types := range propertyTypes {
		propDef := PropertyDef{
			Name:     propName,
			Type:     c.inferPropertyType(types),
			Required: len(types) == len(samples), // Required if present in all samples
			Examples: []string{},
		}
		schema.Properties[propName] = propDef
	}

	return schema, nil
}

// analyzeProperties recursively analyzes properties to determine types
func (c *ResourceGraphClient) analyzeProperties(properties map[string]interface{}, prefix string, propertyTypes map[string]map[string]bool) {
	for key, value := range properties {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		if propertyTypes[fullKey] == nil {
			propertyTypes[fullKey] = make(map[string]bool)
		}

		switch v := value.(type) {
		case string:
			propertyTypes[fullKey]["string"] = true
		case int, int32, int64:
			propertyTypes[fullKey]["integer"] = true
		case float32, float64:
			propertyTypes[fullKey]["number"] = true
		case bool:
			propertyTypes[fullKey]["boolean"] = true
		case map[string]interface{}:
			propertyTypes[fullKey]["object"] = true
			// Recursively analyze nested objects
			c.analyzeProperties(v, fullKey, propertyTypes)
		case []interface{}:
			propertyTypes[fullKey]["array"] = true
		default:
			propertyTypes[fullKey]["unknown"] = true
		}
	}
}

// inferPropertyType infers the most appropriate type from observed types
func (c *ResourceGraphClient) inferPropertyType(types map[string]bool) string {
	if len(types) == 1 {
		for t := range types {
			return t
		}
	}

	// Priority order for mixed types
	if types["object"] {
		return "object"
	}
	if types["array"] {
		return "array"
	}
	if types["string"] {
		return "string"
	}
	if types["number"] {
		return "number"
	}
	if types["integer"] {
		return "integer"
	}
	if types["boolean"] {
		return "boolean"
	}

	return "unknown"
}

// executeRelationshipDiscoveryQuery executes relationship discovery queries
func (c *ResourceGraphClient) executeRelationshipDiscoveryQuery(ctx context.Context, query string) ([]*RelationshipPattern, error) {
	subscriptions := make([]*string, len(c.subscriptions))
	for i, sub := range c.subscriptions {
		subscriptions[i] = to.Ptr(sub)
	}

	request := armresourcegraph.QueryRequest{
		Query:         to.Ptr(query),
		Subscriptions: subscriptions,
		Options: &armresourcegraph.QueryRequestOptions{
			ResultFormat: to.Ptr(armresourcegraph.ResultFormatObjectArray),
		},
	}

	result, err := c.client.Resources(ctx, request, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute relationship discovery query: %w", err)
	}

	return c.parseRelationshipResults(&result.QueryResponse)
}

// parseRelationshipResults parses relationship discovery results
func (c *ResourceGraphClient) parseRelationshipResults(response *armresourcegraph.QueryResponse) ([]*RelationshipPattern, error) {
	if response.Data == nil {
		return nil, fmt.Errorf("no data in response")
	}

	data, ok := response.Data.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected data format")
	}

	patterns := make([]*RelationshipPattern, 0, len(data))

	for _, item := range data {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		pattern := &RelationshipPattern{
			SourceType:        getString(row, "SourceType"),
			TargetType:        getString(row, "TargetType"),
			RelationshipCount: int(getInt32(row, "RelationshipCount")),
			SampleReferences:  []string{},
			RelationshipType:  "REFERENCES", // Default type
		}

		// Parse sample references if available
		if samples, ok := row["SampleReferences"].([]interface{}); ok {
			for _, sample := range samples {
				if sampleStr, ok := sample.(string); ok {
					pattern.SampleReferences = append(pattern.SampleReferences, sampleStr)
				}
			}
		}

		patterns = append(patterns, pattern)
	}

	return patterns, nil
}

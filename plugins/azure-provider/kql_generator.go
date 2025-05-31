package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// KQLQuery represents a complete KQL query with metadata
type KQLQuery struct {
	ResourceType     string            `json:"resourceType"`
	Query           string            `json:"query"`
	Projections     []string          `json:"projections"`
	Filters         map[string]string `json:"filters"`
	JoinTables      []string          `json:"joinTables,omitempty"`
	OptimizationTips []string         `json:"optimizationTips,omitempty"`
	Metadata        map[string]string `json:"metadata"`
}

// KQLQueryOptions provides configuration for query generation
type KQLQueryOptions struct {
	IncludeRelated     bool              `json:"includeRelated"`
	ProjectionMode     ProjectionMode    `json:"projectionMode"`
	FilterOptions      map[string]string `json:"filterOptions"`
	SubscriptionFilter []string          `json:"subscriptionFilter"`
	LocationFilter     []string          `json:"locationFilter"`
	TagFilters         map[string]string `json:"tagFilters"`
	Limit              int               `json:"limit"`
	EnableCaching      bool              `json:"enableCaching"`
}

// ProjectionMode defines how much detail to include in projections
type ProjectionMode string

const (
	ProjectionBasic      ProjectionMode = "basic"      // id, name, type, location
	ProjectionStandard   ProjectionMode = "standard"   // basic + common properties
	ProjectionDetailed   ProjectionMode = "detailed"   // all important properties
	ProjectionComplete   ProjectionMode = "complete"   // all properties
)

// ResourceInfo represents analyzed resource information from Phase 2
type ResourceInfo struct {
	Type        string                   `json:"type"`
	ARMType     string                   `json:"armType"`
	Service     string                   `json:"service"`
	Properties  map[string]PropertyInfo  `json:"properties"`
	Operations  map[string]OperationInfo `json:"operations"`
	Metadata    map[string]interface{}   `json:"metadata"`
}

// PropertyInfo represents a resource property
type PropertyInfo struct {
	Type        string `json:"type"`
	Path        string `json:"path"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// OperationInfo represents an operation on a resource
type OperationInfo struct {
	Method              string          `json:"method"`
	Type                string          `json:"type"`
	SupportsResourceGroup bool          `json:"supportsResourceGroup,omitempty"`
	Parameters          []ParameterInfo `json:"parameters,omitempty"`
}

// ParameterInfo represents operation parameters
type ParameterInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// RelationshipInfo represents relationships between resources
type RelationshipInfo struct {
	SourceType      string `json:"sourceType"`
	TargetType      string `json:"targetType"`
	RelationshipType string `json:"relationshipType"` // references, contains, depends_on
	PropertyPath    string `json:"propertyPath"`
	Cardinality     string `json:"cardinality"` // one-to-one, one-to-many, many-to-many
	JoinCondition   string `json:"joinCondition"`
}

// AzureKQLGenerator generates optimized KQL queries for Azure Resource Graph
type AzureKQLGenerator struct {
	queryCache       map[string]*KQLQuery
	relationshipMap  map[string][]RelationshipInfo
	templateCache    map[string]string
	optimizations    *QueryOptimizations
}

// QueryOptimizations contains various optimization strategies
type QueryOptimizations struct {
	EnableProjectionOptimization bool
	EnableFilterPushdown        bool
	EnableJoinOptimization      bool
	EnableBatchQueries          bool
	MaxResultsPerQuery          int
	CacheExpiration            time.Duration
}

// NewAzureKQLGenerator creates a new KQL query generator
func NewAzureKQLGenerator() *AzureKQLGenerator {
	return &AzureKQLGenerator{
		queryCache:      make(map[string]*KQLQuery),
		relationshipMap: make(map[string][]RelationshipInfo),
		templateCache:   make(map[string]string),
		optimizations: &QueryOptimizations{
			EnableProjectionOptimization: true,
			EnableFilterPushdown:        true,
			EnableJoinOptimization:      true,
			EnableBatchQueries:          true,
			MaxResultsPerQuery:          1000,
			CacheExpiration:            15 * time.Minute,
		},
	}
}

// GenerateResourceQuery generates an optimized KQL query for a specific resource type
func (g *AzureKQLGenerator) GenerateResourceQuery(resource ResourceInfo, options *KQLQueryOptions) *KQLQuery {
	if options == nil {
		options = &KQLQueryOptions{
			ProjectionMode: ProjectionStandard,
			EnableCaching:  true,
		}
	}

	// Check cache first
	cacheKey := g.generateCacheKey(resource.ARMType, options)
	if cached, exists := g.queryCache[cacheKey]; exists && options.EnableCaching {
		return cached
	}

	query := &KQLQuery{
		ResourceType: resource.ARMType,
		Filters:      make(map[string]string),
		Metadata:     make(map[string]string),
	}

	// Build the query components
	g.buildBaseQuery(query, resource, options)
	g.addProjections(query, resource, options)
	g.addFilters(query, resource, options)
	g.addOptimizations(query, resource, options)

	// Add relationships if requested
	if options.IncludeRelated {
		// For now, skip relationship joins in basic query generation
		// Relationships are handled via GenerateRelationshipQuery()
		query.Metadata["relationships_available"] = "true"
	}

	// Cache the query
	if options.EnableCaching {
		g.queryCache[cacheKey] = query
	}

	return query
}

// buildBaseQuery constructs the base Resources query
func (g *AzureKQLGenerator) buildBaseQuery(query *KQLQuery, resource ResourceInfo, options *KQLQueryOptions) {
	var queryParts []string

	// Start with Resources table
	queryParts = append(queryParts, "Resources")

	// Add type filter
	typeFilter := fmt.Sprintf(`where type == "%s"`, strings.ToLower(resource.ARMType))
	queryParts = append(queryParts, typeFilter)
	query.Filters["type"] = resource.ARMType

	// Add subscription filter if specified
	if len(options.SubscriptionFilter) > 0 {
		subscriptions := make([]string, len(options.SubscriptionFilter))
		for i, sub := range options.SubscriptionFilter {
			subscriptions[i] = fmt.Sprintf(`"%s"`, sub)
		}
		subFilter := fmt.Sprintf("| where subscriptionId in (%s)", strings.Join(subscriptions, ", "))
		queryParts = append(queryParts, subFilter)
		query.Filters["subscriptions"] = strings.Join(options.SubscriptionFilter, ",")
	}

	// Add location filter if specified
	if len(options.LocationFilter) > 0 {
		locations := make([]string, len(options.LocationFilter))
		for i, loc := range options.LocationFilter {
			locations[i] = fmt.Sprintf(`"%s"`, loc)
		}
		locFilter := fmt.Sprintf("| where location in (%s)", strings.Join(locations, ", "))
		queryParts = append(queryParts, locFilter)
		query.Filters["locations"] = strings.Join(options.LocationFilter, ",")
	}

	// Add tag filters if specified
	if len(options.TagFilters) > 0 {
		for tagKey, tagValue := range options.TagFilters {
			tagFilter := fmt.Sprintf(`| where tags["%s"] == "%s"`, tagKey, tagValue)
			queryParts = append(queryParts, tagFilter)
			query.Filters[fmt.Sprintf("tag_%s", tagKey)] = tagValue
		}
	}

	query.Query = strings.Join(queryParts, "\n")
}

// addProjections adds appropriate field projections based on the mode
func (g *AzureKQLGenerator) addProjections(query *KQLQuery, resource ResourceInfo, options *KQLQueryOptions) {
	var projections []string

	// Always include basic fields
	basicFields := []string{
		"id",
		"name", 
		"type",
		"location",
		"resourceGroup",
		"subscriptionId",
	}

	switch options.ProjectionMode {
	case ProjectionBasic:
		projections = basicFields
		
	case ProjectionStandard:
		projections = basicFields
		projections = append(projections, "tags")
		
		// Add resource-specific standard fields
		standardFields := g.getStandardFieldsForResource(resource)
		projections = append(projections, standardFields...)
		
	case ProjectionDetailed:
		projections = basicFields
		projections = append(projections, "tags", "properties")
		
		// Add important nested properties
		detailedFields := g.getDetailedFieldsForResource(resource)
		projections = append(projections, detailedFields...)
		
	case ProjectionComplete:
		// Project all available fields
		projections = append(basicFields, "tags", "properties", "kind", "sku", "zones", "identity")
		
		// Add all known properties for this resource type
		allFields := g.getAllFieldsForResource(resource)
		projections = append(projections, allFields...)
	}

	query.Projections = projections
	
	// Build projection clause
	if len(projections) > 0 {
		projectionClause := "| project " + strings.Join(projections, ", ")
		query.Query += "\n" + projectionClause
	}
}

// addFilters adds additional filtering capabilities
func (g *AzureKQLGenerator) addFilters(query *KQLQuery, resource ResourceInfo, options *KQLQueryOptions) {
	// Add limit if specified
	if options.Limit > 0 {
		limitClause := fmt.Sprintf("| limit %d", options.Limit)
		query.Query += "\n" + limitClause
		query.Filters["limit"] = fmt.Sprintf("%d", options.Limit)
	}

	// Add resource-specific filters
	resourceFilters := g.getResourceSpecificFilters(resource, options)
	for filterName, filterClause := range resourceFilters {
		query.Query += "\n" + filterClause
		query.Filters[filterName] = "enabled"
	}
}

// addOptimizations applies various optimization strategies
func (g *AzureKQLGenerator) addOptimizations(query *KQLQuery, resource ResourceInfo, options *KQLQueryOptions) {
	var optimizationTips []string

	// Add ordering for consistent results
	if g.optimizations.EnableProjectionOptimization {
		query.Query += "\n| order by name asc"
		optimizationTips = append(optimizationTips, "Results ordered by name for consistency")
	}

	// Add summarization hints for large datasets
	if g.optimizations.MaxResultsPerQuery > 0 && options.Limit == 0 {
		query.Query += fmt.Sprintf("\n| top %d by name asc", g.optimizations.MaxResultsPerQuery)
		optimizationTips = append(optimizationTips, fmt.Sprintf("Limited to top %d results for performance", g.optimizations.MaxResultsPerQuery))
	}

	query.OptimizationTips = optimizationTips
	query.Metadata["optimized"] = "true"
	query.Metadata["generated_at"] = time.Now().UTC().Format(time.RFC3339)
}

// GenerateRelationshipQuery generates a query that joins related resources
func (g *AzureKQLGenerator) GenerateRelationshipQuery(source, target ResourceInfo, options *KQLQueryOptions) *KQLQuery {
	relationship := g.findRelationship(source.ARMType, target.ARMType)
	if relationship == nil {
		// Create a basic relationship query if none is predefined
		relationship = g.inferRelationship(source, target)
	}

	query := &KQLQuery{
		ResourceType: fmt.Sprintf("%s+%s", source.ARMType, target.ARMType),
		JoinTables:   []string{source.ARMType, target.ARMType},
		Filters:      make(map[string]string),
		Metadata:     make(map[string]string),
	}

	g.buildRelationshipQuery(query, source, target, relationship, options)
	return query
}

// buildRelationshipQuery constructs a query with joins for related resources
func (g *AzureKQLGenerator) buildRelationshipQuery(query *KQLQuery, source, target ResourceInfo, relationship *RelationshipInfo, options *KQLQueryOptions) {
	var queryParts []string

	// Build source query
	sourceQuery := fmt.Sprintf(`Resources
| where type == "%s"`, strings.ToLower(source.ARMType))

	// Add source projections
	sourceProjections := g.getJoinProjectionsForResource(source, "source")
	if len(sourceProjections) > 0 {
		sourceQuery += "\n| project " + strings.Join(sourceProjections, ", ")
	}

	queryParts = append(queryParts, sourceQuery)

	// Build join clause
	joinType := "leftouter" // Default to left outer join
	if relationship.Cardinality == "one-to-one" {
		joinType = "inner"
	}

	joinClause := fmt.Sprintf(`| join kind=%s (
    Resources
    | where type == "%s"`, joinType, strings.ToLower(target.ARMType))

	// Add target projections
	targetProjections := g.getJoinProjectionsForResource(target, "target")
	if len(targetProjections) > 0 {
		joinClause += "\n    | project " + strings.Join(targetProjections, ", ")
	}

	joinClause += "\n) on " + relationship.JoinCondition

	queryParts = append(queryParts, joinClause)

	// Combine final projections
	finalProjections := g.getCombinedProjections(source, target, relationship)
	if len(finalProjections) > 0 {
		queryParts = append(queryParts, "| project "+strings.Join(finalProjections, ", "))
	}

	query.Query = strings.Join(queryParts, "\n")
	query.Projections = finalProjections
}

// GenerateTagBasedQuery generates queries for tag-based resource discovery
func (g *AzureKQLGenerator) GenerateTagBasedQuery(tagKey, tagValue string, options *KQLQueryOptions) *KQLQuery {
	query := &KQLQuery{
		ResourceType: "tag-based",
		Filters:      make(map[string]string),
		Metadata:     make(map[string]string),
	}

	var queryParts []string
	queryParts = append(queryParts, "Resources")

	if tagValue != "" {
		// Exact tag match
		queryParts = append(queryParts, fmt.Sprintf(`| where tags["%s"] == "%s"`, tagKey, tagValue))
		query.Filters["tag_exact"] = fmt.Sprintf("%s=%s", tagKey, tagValue)
	} else {
		// Tag key exists
		queryParts = append(queryParts, fmt.Sprintf(`| where isnotnull(tags["%s"])`, tagKey))
		query.Filters["tag_exists"] = tagKey
	}

	// Project useful fields
	projections := []string{
		"id", "name", "type", "location", "resourceGroup",
		fmt.Sprintf(`tagValue=tags["%s"]`, tagKey),
		"tags",
	}

	queryParts = append(queryParts, "| project "+strings.Join(projections, ", "))
	queryParts = append(queryParts, "| order by type asc, name asc")

	query.Query = strings.Join(queryParts, "\n")
	query.Projections = projections
	query.Metadata["query_type"] = "tag-based"

	return query
}

// GenerateLocationRollupQuery generates queries for location-based aggregations
func (g *AzureKQLGenerator) GenerateLocationRollupQuery(resourceTypes []string, options *KQLQueryOptions) *KQLQuery {
	query := &KQLQuery{
		ResourceType: "location-rollup",
		Filters:      make(map[string]string),
		Metadata:     make(map[string]string),
	}

	var queryParts []string
	queryParts = append(queryParts, "Resources")

	// Filter by resource types if specified
	if len(resourceTypes) > 0 {
		typeFilters := make([]string, len(resourceTypes))
		for i, rt := range resourceTypes {
			typeFilters[i] = fmt.Sprintf(`type == "%s"`, strings.ToLower(rt))
		}
		queryParts = append(queryParts, "| where "+strings.Join(typeFilters, " or "))
		query.Filters["resource_types"] = strings.Join(resourceTypes, ",")
	}

	// Summarize by location and type
	queryParts = append(queryParts, `| summarize ResourceCount=count() by location, type`)
	queryParts = append(queryParts, `| order by location asc, ResourceCount desc`)

	query.Query = strings.Join(queryParts, "\n")
	query.Projections = []string{"location", "type", "ResourceCount"}
	query.Metadata["query_type"] = "location-rollup"

	return query
}

// GenerateComplianceQuery generates queries for compliance and policy checking
func (g *AzureKQLGenerator) GenerateComplianceQuery(complianceType string, options *KQLQueryOptions) *KQLQuery {
	query := &KQLQuery{
		ResourceType: fmt.Sprintf("compliance-%s", complianceType),
		Filters:      make(map[string]string),
		Metadata:     make(map[string]string),
	}

	var queryBuilder strings.Builder

	switch complianceType {
	case "encryption":
		g.buildEncryptionComplianceQuery(&queryBuilder, options)
	case "public-access":
		g.buildPublicAccessComplianceQuery(&queryBuilder, options)
	case "tagging":
		g.buildTaggingComplianceQuery(&queryBuilder, options)
	case "backup":
		g.buildBackupComplianceQuery(&queryBuilder, options)
	default:
		g.buildGenericComplianceQuery(&queryBuilder, complianceType, options)
	}

	query.Query = queryBuilder.String()
	query.Metadata["query_type"] = "compliance"
	query.Metadata["compliance_type"] = complianceType

	return query
}

// GenerateChangeDetectionQuery generates queries for detecting resource changes
func (g *AzureKQLGenerator) GenerateChangeDetectionQuery(timeRange string, changeType string, options *KQLQueryOptions) *KQLQuery {
	query := &KQLQuery{
		ResourceType: "change-detection",
		Filters:      make(map[string]string),
		Metadata:     make(map[string]string),
	}

	var queryParts []string

	// Use ResourceChanges table for change detection
	queryParts = append(queryParts, "ResourceChanges")

	// Add time range filter
	timeFilter := fmt.Sprintf("| where TimeGenerated >= ago(%s)", timeRange)
	queryParts = append(queryParts, timeFilter)
	query.Filters["time_range"] = timeRange

	// Filter by change type if specified
	if changeType != "" {
		changeFilter := fmt.Sprintf(`| where ChangeType == "%s"`, changeType)
		queryParts = append(queryParts, changeFilter)
		query.Filters["change_type"] = changeType
	}

	// Project relevant change information
	projections := []string{
		"TimeGenerated",
		"ResourceId",
		"ChangeType",
		"PropertyChanges",
		"PreviousResourceSnapshotId",
		"NewResourceSnapshotId",
	}

	queryParts = append(queryParts, "| project "+strings.Join(projections, ", "))
	queryParts = append(queryParts, "| order by TimeGenerated desc")

	query.Query = strings.Join(queryParts, "\n")
	query.Projections = projections
	query.Metadata["query_type"] = "change-detection"

	return query
}

// GenerateBatchedQueries generates multiple optimized queries for batch execution
func (g *AzureKQLGenerator) GenerateBatchedQueries(resources []ResourceInfo, options *KQLQueryOptions) []*KQLQuery {
	var queries []*KQLQuery

	// Group resources by service for batch optimization
	serviceGroups := make(map[string][]ResourceInfo)
	for _, resource := range resources {
		serviceGroups[resource.Service] = append(serviceGroups[resource.Service], resource)
	}

	// Generate batched queries for each service
	for service, serviceResources := range serviceGroups {
		if len(serviceResources) > 1 && g.optimizations.EnableBatchQueries {
			// Create a combined query for multiple resource types in the same service
			batchQuery := g.generateServiceBatchQuery(service, serviceResources, options)
			queries = append(queries, batchQuery)
		} else {
			// Generate individual queries
			for _, resource := range serviceResources {
				query := g.GenerateResourceQuery(resource, options)
				queries = append(queries, query)
			}
		}
	}

	return queries
}

// Helper methods for building specific query components

func (g *AzureKQLGenerator) getStandardFieldsForResource(resource ResourceInfo) []string {
	standardFields := make(map[string]bool)

	// Add common fields based on resource type
	switch {
	case strings.Contains(resource.ARMType, "/virtualMachines"):
		standardFields["vmSize=properties.hardwareProfile.vmSize"] = true
		standardFields["osType=properties.storageProfile.osDisk.osType"] = true
		standardFields["powerState=properties.extended.instanceView.powerState.code"] = true

	case strings.Contains(resource.ARMType, "/storageAccounts"):
		standardFields["accountType=sku.name"] = true
		standardFields["accessTier=properties.accessTier"] = true
		standardFields["encryption=properties.encryption.services"] = true

	case strings.Contains(resource.ARMType, "/networkInterfaces"):
		standardFields["privateIP=properties.ipConfigurations[0].properties.privateIPAddress"] = true
		standardFields["subnet=properties.ipConfigurations[0].properties.subnet.id"] = true

	case strings.Contains(resource.ARMType, "/publicIPAddresses"):
		standardFields["ipAddress=properties.ipAddress"] = true
		standardFields["allocationMethod=properties.publicIPAllocationMethod"] = true

	case strings.Contains(resource.ARMType, "/loadBalancers"):
		standardFields["sku=sku.name"] = true
		standardFields["frontendIPs=properties.frontendIPConfigurations"] = true

	case strings.Contains(resource.ARMType, "/virtualNetworks"):
		standardFields["addressSpace=properties.addressSpace.addressPrefixes"] = true
		standardFields["subnets=properties.subnets"] = true
	}

	// Convert map to slice
	fields := make([]string, 0, len(standardFields))
	for field := range standardFields {
		fields = append(fields, field)
	}

	sort.Strings(fields)
	return fields
}

func (g *AzureKQLGenerator) getDetailedFieldsForResource(resource ResourceInfo) []string {
	detailedFields := g.getStandardFieldsForResource(resource)

	// Add more detailed fields based on resource type
	switch {
	case strings.Contains(resource.ARMType, "/virtualMachines"):
		detailedFields = append(detailedFields,
			"networkInterfaces=properties.networkProfile.networkInterfaces",
			"osDiskId=properties.storageProfile.osDisk.managedDisk.id",
			"dataDisks=properties.storageProfile.dataDisks",
			"availabilitySet=properties.availabilitySet.id",
		)

	case strings.Contains(resource.ARMType, "/storageAccounts"):
		detailedFields = append(detailedFields,
			"primaryEndpoints=properties.primaryEndpoints",
			"networkRules=properties.networkAcls",
			"minimumTlsVersion=properties.minimumTlsVersion",
		)
	}

	return detailedFields
}

func (g *AzureKQLGenerator) getAllFieldsForResource(resource ResourceInfo) []string {
	// Return all known properties for the resource type
	var allFields []string

	for propName, propInfo := range resource.Properties {
		if propInfo.Path != "" {
			allFields = append(allFields, fmt.Sprintf("%s=%s", propName, propInfo.Path))
		}
	}

	sort.Strings(allFields)
	return allFields
}

func (g *AzureKQLGenerator) getResourceSpecificFilters(resource ResourceInfo, options *KQLQueryOptions) map[string]string {
	filters := make(map[string]string)

	// Add filters based on common scenarios
	switch {
	case strings.Contains(resource.ARMType, "/virtualMachines"):
		// Filter out deallocated VMs by default
		filters["exclude_deallocated"] = `| where properties.extended.instanceView.powerState.code != "PowerState/deallocated"`

	case strings.Contains(resource.ARMType, "/storageAccounts"):
		// Filter for accessible storage accounts
		filters["accessible_only"] = `| where properties.provisioningState == "Succeeded"`

	case strings.Contains(resource.ARMType, "/publicIPAddresses"):
		// Filter for allocated public IPs
		filters["allocated_only"] = `| where isnotnull(properties.ipAddress)`
	}

	return filters
}

func (g *AzureKQLGenerator) findRelationship(sourceType, targetType string) *RelationshipInfo {
	relationships, exists := g.relationshipMap[sourceType]
	if !exists {
		return nil
	}

	for _, rel := range relationships {
		if rel.TargetType == targetType {
			return &rel
		}
	}

	return nil
}

func (g *AzureKQLGenerator) inferRelationship(source, target ResourceInfo) *RelationshipInfo {
	// Infer common Azure resource relationships
	sourceType := strings.ToLower(source.ARMType)
	targetType := strings.ToLower(target.ARMType)

	switch {
	case strings.Contains(sourceType, "virtualmachines") && strings.Contains(targetType, "networkinterfaces"):
		return &RelationshipInfo{
			SourceType:      source.ARMType,
			TargetType:      target.ARMType,
			RelationshipType: "uses",
			PropertyPath:    "properties.networkProfile.networkInterfaces",
			Cardinality:     "one-to-many",
			JoinCondition:   "$left.networkInterfaces[0].id == $right.id",
		}

	case strings.Contains(sourceType, "virtualmachines") && strings.Contains(targetType, "disks"):
		return &RelationshipInfo{
			SourceType:      source.ARMType,
			TargetType:      target.ARMType,
			RelationshipType: "uses",
			PropertyPath:    "properties.storageProfile.osDisk.managedDisk.id",
			Cardinality:     "one-to-many",
			JoinCondition:   "$left.osDiskId == $right.id",
		}

	case strings.Contains(sourceType, "networkinterfaces") && strings.Contains(targetType, "publicipaddresses"):
		return &RelationshipInfo{
			SourceType:      source.ARMType,
			TargetType:      target.ARMType,
			RelationshipType: "references",
			PropertyPath:    "properties.ipConfigurations[0].properties.publicIPAddress.id",
			Cardinality:     "one-to-one",
			JoinCondition:   "$left.publicIPId == $right.id",
		}

	default:
		// Generic relationship based on resource group
		return &RelationshipInfo{
			SourceType:      source.ARMType,
			TargetType:      target.ARMType,
			RelationshipType: "colocated",
			PropertyPath:    "resourceGroup",
			Cardinality:     "many-to-many",
			JoinCondition:   "$left.resourceGroup == $right.resourceGroup",
		}
	}
}

func (g *AzureKQLGenerator) getJoinProjectionsForResource(resource ResourceInfo, alias string) []string {
	var projections []string

	// Basic fields with alias
	projections = append(projections,
		fmt.Sprintf("%sId=id", alias),
		fmt.Sprintf("%sName=name", alias),
		fmt.Sprintf("%sType=type", alias),
		fmt.Sprintf("%sLocation=location", alias),
		fmt.Sprintf("%sResourceGroup=resourceGroup", alias),
	)

	// Add resource-specific fields
	specificFields := g.getStandardFieldsForResource(resource)
	for _, field := range specificFields {
		// Add alias prefix to avoid naming conflicts
		if strings.Contains(field, "=") {
			parts := strings.SplitN(field, "=", 2)
			projections = append(projections, fmt.Sprintf("%s%s=%s", alias, strings.Title(parts[0]), parts[1]))
		}
	}

	return projections
}

func (g *AzureKQLGenerator) getCombinedProjections(source, target ResourceInfo, relationship *RelationshipInfo) []string {
	var projections []string

	// Source projections
	sourceProjections := g.getJoinProjectionsForResource(source, "source")
	projections = append(projections, sourceProjections...)

	// Target projections
	targetProjections := g.getJoinProjectionsForResource(target, "target")
	projections = append(projections, targetProjections...)

	// Relationship information
	projections = append(projections,
		fmt.Sprintf("relationshipType=\"%s\"", relationship.RelationshipType),
		fmt.Sprintf("relationshipCardinality=\"%s\"", relationship.Cardinality),
	)

	return projections
}

func (g *AzureKQLGenerator) generateServiceBatchQuery(service string, resources []ResourceInfo, options *KQLQueryOptions) *KQLQuery {
	query := &KQLQuery{
		ResourceType: fmt.Sprintf("batch-%s", service),
		Filters:      make(map[string]string),
		Metadata:     make(map[string]string),
	}

	var queryParts []string
	queryParts = append(queryParts, "Resources")

	// Build type filter for multiple resource types
	typeFilters := make([]string, len(resources))
	for i, resource := range resources {
		typeFilters[i] = fmt.Sprintf(`type == "%s"`, strings.ToLower(resource.ARMType))
	}

	queryParts = append(queryParts, "| where "+strings.Join(typeFilters, " or "))

	// Add common projections
	projections := []string{"id", "name", "type", "location", "resourceGroup", "tags"}
	queryParts = append(queryParts, "| project "+strings.Join(projections, ", "))
	queryParts = append(queryParts, "| order by type asc, name asc")

	query.Query = strings.Join(queryParts, "\n")
	query.Projections = projections
	query.Metadata["query_type"] = "batch"
	query.Metadata["service"] = service

	// Add resource types to filters
	resourceTypes := make([]string, len(resources))
	for i, resource := range resources {
		resourceTypes[i] = resource.ARMType
	}
	query.Filters["resource_types"] = strings.Join(resourceTypes, ",")

	return query
}

// Compliance query builders

func (g *AzureKQLGenerator) buildEncryptionComplianceQuery(builder *strings.Builder, options *KQLQueryOptions) {
	builder.WriteString(`Resources
| where type in ("microsoft.storage/storageaccounts", "microsoft.compute/disks", "microsoft.keyvault/vaults")
| extend encryptionEnabled = case(
    type == "microsoft.storage/storageaccounts", tobool(properties.encryption.services.blob.enabled),
    type == "microsoft.compute/disks", tobool(properties.encryption.type),
    type == "microsoft.keyvault/vaults", tobool(properties.enabledForDiskEncryption),
    false
)
| project id, name, type, location, encryptionEnabled, tags
| where encryptionEnabled == false
| order by type asc, name asc`)
}

func (g *AzureKQLGenerator) buildPublicAccessComplianceQuery(builder *strings.Builder, options *KQLQueryOptions) {
	builder.WriteString(`Resources
| where type in ("microsoft.storage/storageaccounts", "microsoft.sql/servers")
| extend publicAccessEnabled = case(
    type == "microsoft.storage/storageaccounts", 
        properties.allowBlobPublicAccess == true or properties.networkAcls.defaultAction == "Allow",
    type == "microsoft.sql/servers",
        properties.publicNetworkAccess == "Enabled",
    false
)
| project id, name, type, location, publicAccessEnabled, tags
| where publicAccessEnabled == true
| order by type asc, name asc`)
}

func (g *AzureKQLGenerator) buildTaggingComplianceQuery(builder *strings.Builder, options *KQLQueryOptions) {
	requiredTags := []string{"Environment", "Owner", "CostCenter"}
	
	builder.WriteString(`Resources
| extend missingTags = array_length(`)
	
	missingTagChecks := make([]string, len(requiredTags))
	for i, tag := range requiredTags {
		missingTagChecks[i] = fmt.Sprintf(`iff(isnull(tags["%s"]) or tags["%s"] == "", "%s", "")`, tag, tag, tag)
	}
	
	builder.WriteString("pack_array(" + strings.Join(missingTagChecks, ", ") + "))")
	builder.WriteString(`
| where missingTags > 0
| project id, name, type, location, tags, missingTags
| order by missingTags desc, type asc, name asc`)
}

func (g *AzureKQLGenerator) buildBackupComplianceQuery(builder *strings.Builder, options *KQLQueryOptions) {
	builder.WriteString(`Resources
| where type == "microsoft.compute/virtualmachines"
| join kind=leftouter (
    Resources
    | where type == "microsoft.recoveryservices/vaults/backupfabrics/protectioncontainers/protecteditems"
    | project vmId=properties.sourceResourceId, backupEnabled=true
) on $left.id == $right.vmId
| extend backupConfigured = tobool(backupEnabled)
| project id, name, type, location, backupConfigured, tags
| where backupConfigured != true
| order by name asc`)
}

func (g *AzureKQLGenerator) buildGenericComplianceQuery(builder *strings.Builder, complianceType string, options *KQLQueryOptions) {
	builder.WriteString(fmt.Sprintf(`Resources
| where tags["ComplianceType"] == "%s"
| project id, name, type, location, tags
| order by type asc, name asc`, complianceType))
}

// Utility methods

func (g *AzureKQLGenerator) generateCacheKey(resourceType string, options *KQLQueryOptions) string {
	return fmt.Sprintf("%s-%s-%v-%v", 
		resourceType, 
		string(options.ProjectionMode), 
		options.IncludeRelated,
		len(options.FilterOptions))
}

// ClearCache clears the query cache
func (g *AzureKQLGenerator) ClearCache() {
	g.queryCache = make(map[string]*KQLQuery)
	g.templateCache = make(map[string]string)
}

// SetOptimizations updates optimization settings
func (g *AzureKQLGenerator) SetOptimizations(opts *QueryOptimizations) {
	g.optimizations = opts
}

// AddRelationship adds a custom relationship mapping
func (g *AzureKQLGenerator) AddRelationship(sourceType string, relationship RelationshipInfo) {
	if g.relationshipMap[sourceType] == nil {
		g.relationshipMap[sourceType] = []RelationshipInfo{}
	}
	g.relationshipMap[sourceType] = append(g.relationshipMap[sourceType], relationship)
}

// GetQueryStatistics returns statistics about generated queries
func (g *AzureKQLGenerator) GetQueryStatistics() map[string]interface{} {
	return map[string]interface{}{
		"cached_queries":     len(g.queryCache),
		"cached_templates":   len(g.templateCache),
		"known_relationships": len(g.relationshipMap),
		"optimization_enabled": g.optimizations.EnableProjectionOptimization,
	}
}
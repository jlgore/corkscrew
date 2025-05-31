package main

import (
	"fmt"
)

// ARMProperty represents a parsed ARM resource property
type ARMProperty struct {
	Path        string            // e.g., "properties.hardwareProfile.vmSize"
	Type        string            // ARM type (string, integer, boolean, object, array)
	DuckDBType  string            // Mapped DuckDB type
	Required    bool              // Whether the property is required
	Nested      bool              // Whether it contains nested objects
	Description string            // Property description
	Examples    []interface{}     // Example values found
	Metadata    map[string]string // Additional metadata
}

// AzureResourceInfo holds information about an Azure resource type for schema generation
type AzureResourceInfo struct {
	Service      string
	Type         string
	FullType     string // Microsoft.Compute/virtualMachines
	APIVersion   string
	Properties   map[string]interface{}
	SampleData   []map[string]interface{}
	CommonPaths  []string // Frequently accessed property paths
}

// SchemaOptimizationRules defines optimization rules for schema generation
type SchemaOptimizationRules struct {
	MaxColumnDepth     int      // Maximum nesting depth to flatten as columns
	FlattenPaths       []string // Specific paths to always flatten
	IndexPaths         []string // Paths that should have indexes
	RequiredPaths      []string // Paths that are always required
	AnalyticalPaths    []string // Paths commonly used in analytics
	CompressionHints   map[string]string // Compression hints for columns
	PartitioningHints  []string // Suggested partitioning columns
}

// AzureAdvancedSchemaGenerator generates DuckDB schemas from ARM resource definitions
type AzureAdvancedSchemaGenerator struct {
	rules           SchemaOptimizationRules
	typeCache       map[string][]ARMProperty
	schemaCache     map[string]string
	viewCache       map[string][]string
	knownPatterns   map[string]string // Known ARM property patterns
	commonQueries   map[string]string // Common query patterns for views
}

// NewAzureAdvancedSchemaGenerator creates a new schema generator with default optimization rules
func NewAzureAdvancedSchemaGenerator() *AzureAdvancedSchemaGenerator {
	return &AzureAdvancedSchemaGenerator{
		rules: SchemaOptimizationRules{
			MaxColumnDepth: 3,
			FlattenPaths: []string{
				"properties.hardwareProfile",
				"properties.storageProfile.osDisk",
				"properties.networkProfile.networkInterfaces[0]",
				"properties.osProfile",
				"sku",
				"identity",
			},
			IndexPaths: []string{
				"location",
				"resourceGroup", 
				"subscriptionId",
				"type",
				"properties.provisioningState",
				"tags.Environment",
				"tags.Application",
				"properties.hardwareProfile.vmSize",
				"sku.name",
				"sku.tier",
			},
			RequiredPaths: []string{
				"id",
				"name", 
				"type",
				"location",
				"resourceGroup",
				"subscriptionId",
			},
			AnalyticalPaths: []string{
				"properties.hardwareProfile.vmSize",
				"properties.storageProfile.osDisk.diskSizeGB",
				"sku.name",
				"sku.capacity",
				"properties.provisioningState",
				"properties.powerState",
			},
			CompressionHints: map[string]string{
				"id":            "ZSTD",
				"type":          "DICTIONARY",
				"location":      "DICTIONARY", 
				"resourceGroup": "DICTIONARY",
				"tags":          "ZSTD",
				"properties":    "ZSTD",
				"raw_data":      "ZSTD",
			},
			PartitioningHints: []string{
				"location",
				"resourceGroup",
				"date_trunc('month', discovered_at)",
			},
		},
		typeCache:     make(map[string][]ARMProperty),
		schemaCache:   make(map[string]string),
		viewCache:     make(map[string][]string),
		knownPatterns: initializeKnownPatterns(),
		commonQueries: initializeCommonQueries(),
	}
}

// ParseResourceProperties extracts and parses properties from ARM resource information
func (g *AzureAdvancedSchemaGenerator) ParseResourceProperties(resourceInfo AzureResourceInfo) []ARMProperty {
	cacheKey := fmt.Sprintf("%s_%s_%s", resourceInfo.Service, resourceInfo.Type, resourceInfo.APIVersion)
	
	if cached, exists := g.typeCache[cacheKey]; exists {
		return cached
	}
	
	var properties []ARMProperty
	
	// Parse from properties definition if available
	if resourceInfo.Properties != nil {
		properties = append(properties, g.parseObjectProperties("", resourceInfo.Properties, 0)...)
	}
	
	// Analyze sample data to discover additional patterns
	if len(resourceInfo.SampleData) > 0 {
		sampleProps := g.analyzeSampleData(resourceInfo.SampleData)
		properties = g.mergeProperties(properties, sampleProps)
	}
	
	// Add standard Azure resource properties
	standardProps := g.getStandardAzureProperties()
	properties = g.mergeProperties(standardProps, properties)
	
	// Apply optimization rules and common patterns
	properties = g.applyOptimizationRules(properties, resourceInfo)
	
	// Cache the result
	g.typeCache[cacheKey] = properties
	
	return properties
}

// Helper function stubs (truncated for brevity)
func (g *AzureAdvancedSchemaGenerator) parseObjectProperties(basePath string, obj map[string]interface{}, depth int) []ARMProperty {
	// Implementation truncated for space
	return []ARMProperty{}
}

func (g *AzureAdvancedSchemaGenerator) analyzeSampleData(samples []map[string]interface{}) []ARMProperty {
	// Implementation truncated for space  
	return []ARMProperty{}
}

func (g *AzureAdvancedSchemaGenerator) getStandardAzureProperties() []ARMProperty {
	return []ARMProperty{
		{Path: "id", Type: "string", DuckDBType: "VARCHAR", Required: true},
		{Path: "name", Type: "string", DuckDBType: "VARCHAR", Required: true},
		{Path: "type", Type: "string", DuckDBType: "VARCHAR", Required: true},
		{Path: "location", Type: "string", DuckDBType: "VARCHAR", Required: true},
		{Path: "resourceGroup", Type: "string", DuckDBType: "VARCHAR", Required: true},
		{Path: "subscriptionId", Type: "string", DuckDBType: "VARCHAR", Required: true},
		{Path: "tags", Type: "object", DuckDBType: "JSON", Required: false},
	}
}

func (g *AzureAdvancedSchemaGenerator) mergeProperties(existing, new []ARMProperty) []ARMProperty {
	// Implementation truncated for space
	return existing
}

func (g *AzureAdvancedSchemaGenerator) applyOptimizationRules(properties []ARMProperty, resourceInfo AzureResourceInfo) []ARMProperty {
	// Implementation truncated for space
	return properties
}

func initializeKnownPatterns() map[string]string {
	return map[string]string{
		"properties.hardwareProfile.vmSize": "VM size pattern (e.g., Standard_D2s_v3)",
		"properties.storageProfile.osDisk.diskSizeGB": "Disk size in GB",
		"properties.networkProfile.networkInterfaces[0].id": "Network interface resource ID",
		"sku.name": "SKU name pattern",
		"sku.tier": "SKU tier (Basic, Standard, Premium)",
		"properties.provisioningState": "Provisioning state (Succeeded, Failed, Creating)",
	}
}

func initializeCommonQueries() map[string]string {
	return map[string]string{
		"resource_count_by_location": "SELECT location, COUNT(*) FROM {} GROUP BY location",
		"resources_by_tag": "SELECT json_extract_string(tags, '$.{}'), COUNT(*) FROM {} GROUP BY 1",
		"provisioning_state_summary": "SELECT json_extract_string(properties, '$.provisioningState'), COUNT(*) FROM {} GROUP BY 1",
		"resource_age_analysis": "SELECT DATE_TRUNC('month', discovered_at), COUNT(*) FROM {} GROUP BY 1 ORDER BY 1",
	}
}
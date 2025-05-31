package main

import (
	"fmt"
	"sort"
	"strings"
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
	Indexed     bool              // Whether this property should be indexed
	Compressed  bool              // Whether this property should use compression
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

// parseObjectProperties recursively parses object properties from ARM resource definition
func (g *AzureAdvancedSchemaGenerator) parseObjectProperties(basePath string, obj map[string]interface{}, depth int) []ARMProperty {
	var properties []ARMProperty
	
	if depth > g.rules.MaxColumnDepth {
		// Stop recursion and treat as JSON
		return []ARMProperty{
			{
				Path:        basePath,
				Type:        "object",
				DuckDBType:  "JSON",
				Required:    false,
				Nested:      true,
				Description: fmt.Sprintf("Complex object at depth %d", depth),
			},
		}
	}
	
	for key, value := range obj {
		fullPath := key
		if basePath != "" {
			fullPath = basePath + "." + key
		}
		
		prop := ARMProperty{
			Path:        fullPath,
			Required:    g.isRequiredPath(fullPath),
			Indexed:     g.shouldIndex(fullPath),
			Compressed:  g.shouldCompress(fullPath),
			Metadata:    make(map[string]string),
		}
		
		// Determine type and DuckDB mapping
		switch v := value.(type) {
		case string:
			prop.Type = "string"
			prop.DuckDBType = "VARCHAR"
			prop.Description = "String property"
		case int, int32, int64:
			prop.Type = "integer"
			prop.DuckDBType = "INTEGER"
			prop.Description = "Integer property"
		case float32, float64:
			prop.Type = "number"
			prop.DuckDBType = "DOUBLE"
			prop.Description = "Numeric property"
		case bool:
			prop.Type = "boolean"
			prop.DuckDBType = "BOOLEAN"
			prop.Description = "Boolean property"
		case []interface{}:
			prop.Type = "array"
			prop.DuckDBType = "JSON"
			prop.Description = "Array property"
			prop.Nested = true
		case map[string]interface{}:
			prop.Type = "object"
			prop.DuckDBType = "JSON"
			prop.Description = "Object property"
			prop.Nested = true
			
			// Check if we should flatten this object
			if g.shouldFlatten(fullPath) {
				// Recursively parse nested object
				nestedProps := g.parseObjectProperties(fullPath, v, depth+1)
				properties = append(properties, nestedProps...)
				continue
			}
		default:
			prop.Type = "unknown"
			prop.DuckDBType = "JSON"
			prop.Description = "Unknown type property"
		}
		
		// Apply pattern matching
		g.applyKnownPatterns(&prop)
		
		properties = append(properties, prop)
	}
	
	return properties
}

// analyzeSampleData analyzes sample resource data to discover property patterns
func (g *AzureAdvancedSchemaGenerator) analyzeSampleData(samples []map[string]interface{}) []ARMProperty {
	var properties []ARMProperty
	propertyStats := make(map[string]*PropertyStats)
	
	// Analyze all samples to gather statistics
	for _, sample := range samples {
		g.analyzeResourceSample(sample, "", propertyStats, 0)
	}
	
	// Convert statistics to properties
	for path, stats := range propertyStats {
		prop := ARMProperty{
			Path:        path,
			Type:        stats.DominantType,
			DuckDBType:  g.mapTypeToDuckDB(stats.DominantType),
			Required:    stats.RequiredFrequency > 0.8, // Required if present in >80% of samples
			Indexed:     g.shouldIndex(path),
			Compressed:  g.shouldCompress(path),
			Description: stats.generateDescription(),
			Examples:    stats.getUniqueExamples(5),
			Metadata:    make(map[string]string),
		}
		
		// Apply validation patterns
		g.applyKnownPatterns(&prop)
		
		// Add statistical metadata
		prop.Metadata["sample_count"] = fmt.Sprintf("%d", stats.SampleCount)
		prop.Metadata["null_frequency"] = fmt.Sprintf("%.2f", stats.NullFrequency)
		
		properties = append(properties, prop)
	}
	
	return properties
}

// PropertyStats tracks statistics for a property across samples
type PropertyStats struct {
	Path              string
	SampleCount       int
	NullCount         int
	TypeCounts        map[string]int
	DominantType      string
	RequiredFrequency float64
	NullFrequency     float64
	Examples          []interface{}
	MinLength         int
	MaxLength         int
}

func (ps *PropertyStats) generateDescription() string {
	if ps.DominantType == "string" && ps.MinLength > 0 && ps.MaxLength > 0 {
		return fmt.Sprintf("String property (length: %d-%d)", ps.MinLength, ps.MaxLength)
	}
	return fmt.Sprintf("%s property", strings.Title(ps.DominantType))
}

func (ps *PropertyStats) getUniqueExamples(limit int) []interface{} {
	unique := make(map[interface{}]bool)
	var examples []interface{}
	
	for _, example := range ps.Examples {
		if !unique[example] && len(examples) < limit {
			unique[example] = true
			examples = append(examples, example)
		}
	}
	
	return examples
}

// analyzeResourceSample recursively analyzes a single resource sample
func (g *AzureAdvancedSchemaGenerator) analyzeResourceSample(sample map[string]interface{}, basePath string, stats map[string]*PropertyStats, depth int) {
	for key, value := range sample {
		fullPath := key
		if basePath != "" {
			fullPath = basePath + "." + key
		}
		
		// Get or create statistics for this path
		if _, exists := stats[fullPath]; !exists {
			stats[fullPath] = &PropertyStats{
				Path:       fullPath,
				TypeCounts: make(map[string]int),
				Examples:   []interface{}{},
				MinLength:  -1,
				MaxLength:  -1,
			}
		}
		
		propStats := stats[fullPath]
		propStats.SampleCount++
		
		if value == nil {
			propStats.NullCount++
		} else {
			// Determine type and update statistics
			valueType := g.getValueType(value)
			propStats.TypeCounts[valueType]++
			
			// Add example if not too many already
			if len(propStats.Examples) < 10 {
				propStats.Examples = append(propStats.Examples, value)
			}
			
			// Update string length statistics
			if str, ok := value.(string); ok {
				length := len(str)
				if propStats.MinLength == -1 || length < propStats.MinLength {
					propStats.MinLength = length
				}
				if length > propStats.MaxLength {
					propStats.MaxLength = length
				}
			}
			
			// Recursively analyze nested objects
			if obj, ok := value.(map[string]interface{}); ok && depth < g.rules.MaxColumnDepth {
				g.analyzeResourceSample(obj, fullPath, stats, depth+1)
			}
		}
		
		// Calculate frequencies
		propStats.RequiredFrequency = float64(propStats.SampleCount-propStats.NullCount) / float64(propStats.SampleCount)
		propStats.NullFrequency = float64(propStats.NullCount) / float64(propStats.SampleCount)
		
		// Determine dominant type
		maxCount := 0
		for typeName, count := range propStats.TypeCounts {
			if count > maxCount {
				maxCount = count
				propStats.DominantType = typeName
			}
		}
	}
}

func (g *AzureAdvancedSchemaGenerator) getValueType(value interface{}) string {
	switch value.(type) {
	case string:
		return "string"
	case int, int32, int64:
		return "integer"
	case float32, float64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

func (g *AzureAdvancedSchemaGenerator) getStandardAzureProperties() []ARMProperty {
	return []ARMProperty{
		{Path: "id", Type: "string", DuckDBType: "VARCHAR", Required: true, Indexed: true, Description: "Unique resource identifier"},
		{Path: "name", Type: "string", DuckDBType: "VARCHAR", Required: true, Indexed: true, Description: "Resource name"},
		{Path: "type", Type: "string", DuckDBType: "VARCHAR", Required: true, Indexed: true, Description: "ARM resource type"},
		{Path: "location", Type: "string", DuckDBType: "VARCHAR", Required: true, Indexed: true, Description: "Azure region/location"},
		{Path: "resourceGroup", Type: "string", DuckDBType: "VARCHAR", Required: true, Indexed: true, Description: "Resource group name"},
		{Path: "subscriptionId", Type: "string", DuckDBType: "VARCHAR", Required: true, Indexed: true, Description: "Azure subscription ID"},
		{Path: "tags", Type: "object", DuckDBType: "JSON", Required: false, Compressed: true, Description: "Resource tags"},
		{Path: "properties", Type: "object", DuckDBType: "JSON", Required: false, Compressed: true, Description: "Resource-specific properties"},
		{Path: "sku", Type: "object", DuckDBType: "JSON", Required: false, Description: "SKU information"},
		{Path: "identity", Type: "object", DuckDBType: "JSON", Required: false, Description: "Managed identity configuration"},
		{Path: "discoveredAt", Type: "timestamp", DuckDBType: "TIMESTAMP", Required: true, Indexed: true, Description: "Resource discovery timestamp"},
	}
}

func (g *AzureAdvancedSchemaGenerator) mergeProperties(existing, new []ARMProperty) []ARMProperty {
	propertyMap := make(map[string]ARMProperty)
	
	// Add existing properties
	for _, prop := range existing {
		propertyMap[prop.Path] = prop
	}
	
	// Merge or add new properties
	for _, newProp := range new {
		if existingProp, exists := propertyMap[newProp.Path]; exists {
			// Merge properties, preferring new values for most fields
			merged := existingProp
			
			// Keep the more specific type
			if newProp.Type != "unknown" && newProp.Type != "" {
				merged.Type = newProp.Type
				merged.DuckDBType = newProp.DuckDBType
			}
			
			// Combine examples
			merged.Examples = append(merged.Examples, newProp.Examples...)
			
			// Merge metadata
			if merged.Metadata == nil {
				merged.Metadata = make(map[string]string)
			}
			for k, v := range newProp.Metadata {
				merged.Metadata[k] = v
			}
			
			// Update description if new one is more detailed
			if len(newProp.Description) > len(merged.Description) {
				merged.Description = newProp.Description
			}
			
			propertyMap[newProp.Path] = merged
		} else {
			propertyMap[newProp.Path] = newProp
		}
	}
	
	// Convert back to slice and sort
	var merged []ARMProperty
	for _, prop := range propertyMap {
		merged = append(merged, prop)
	}
	
	// Sort by path for consistency
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Path < merged[j].Path
	})
	
	return merged
}

func (g *AzureAdvancedSchemaGenerator) applyOptimizationRules(properties []ARMProperty, resourceInfo AzureResourceInfo) []ARMProperty {
	optimized := make([]ARMProperty, len(properties))
	copy(optimized, properties)
	
	for i := range optimized {
		prop := &optimized[i]
		
		// Apply indexing rules
		prop.Indexed = g.shouldIndex(prop.Path)
		
		// Apply compression rules
		prop.Compressed = g.shouldCompress(prop.Path)
		
		// Apply flattening rules for analytics
		if g.isAnalyticalPath(prop.Path) {
			prop.Metadata["analytical"] = "true"
		}
		
		// Apply service-specific optimizations
		g.applyServiceSpecificRules(prop, resourceInfo.Service)
		
		// Ensure consistent DuckDB type mapping
		prop.DuckDBType = g.mapTypeToDuckDB(prop.Type)
	}
	
	return optimized
}

// Helper methods for optimization rules

func (g *AzureAdvancedSchemaGenerator) shouldFlatten(path string) bool {
	for _, flattenPath := range g.rules.FlattenPaths {
		if strings.HasPrefix(path, flattenPath) {
			return true
		}
	}
	return false
}

func (g *AzureAdvancedSchemaGenerator) shouldIndex(path string) bool {
	for _, indexPath := range g.rules.IndexPaths {
		if path == indexPath || strings.Contains(path, indexPath) {
			return true
		}
	}
	return false
}

func (g *AzureAdvancedSchemaGenerator) shouldCompress(path string) bool {
	// Check compression hints
	for hintPath, compression := range g.rules.CompressionHints {
		if strings.Contains(path, hintPath) && compression != "" {
			return true
		}
	}
	
	// Default rules for compression
	if strings.Contains(path, "properties") || 
	   strings.Contains(path, "metadata") ||
	   strings.Contains(path, "tags") {
		return true
	}
	
	return false
}

func (g *AzureAdvancedSchemaGenerator) isRequiredPath(path string) bool {
	for _, requiredPath := range g.rules.RequiredPaths {
		if path == requiredPath {
			return true
		}
	}
	return false
}

func (g *AzureAdvancedSchemaGenerator) isAnalyticalPath(path string) bool {
	for _, analyticalPath := range g.rules.AnalyticalPaths {
		if path == analyticalPath || strings.Contains(path, analyticalPath) {
			return true
		}
	}
	return false
}

func (g *AzureAdvancedSchemaGenerator) mapTypeToDuckDB(armType string) string {
	switch strings.ToLower(armType) {
	case "string", "varchar":
		return "VARCHAR"
	case "integer", "int", "int32", "int64", "number":
		return "INTEGER"
	case "float", "double":
		return "DOUBLE"
	case "boolean", "bool":
		return "BOOLEAN"
	case "timestamp", "datetime":
		return "TIMESTAMP"
	case "array", "object", "json":
		return "JSON"
	case "binary", "bytes":
		return "BLOB"
	default:
		return "JSON" // Default to JSON for unknown types
	}
}

func (g *AzureAdvancedSchemaGenerator) applyKnownPatterns(prop *ARMProperty) {
	path := strings.ToLower(prop.Path)
	
	// Apply known Azure patterns
	switch {
	case strings.Contains(path, "id") && strings.Contains(path, "resource"):
		prop.Metadata["pattern"] = "azure_resource_id"
		prop.Metadata["format"] = "arm_resource_id"
	case strings.Contains(path, "vmsize") || (strings.Contains(path, "sku") && strings.Contains(path, "name")):
		prop.Metadata["pattern"] = "azure_vm_size"
	case strings.Contains(path, "location") || path == "location":
		prop.Metadata["pattern"] = "azure_location"
		prop.Metadata["enum_source"] = "azure_regions"
	case strings.Contains(path, "provisioningstate"):
		prop.Metadata["pattern"] = "provisioning_state"
		prop.Metadata["enum_values"] = "Succeeded,Failed,Creating,Updating,Deleting"
	case strings.Contains(path, "timestamp") || strings.Contains(path, "time") || strings.Contains(path, "date"):
		if prop.Type == "string" {
			prop.DuckDBType = "TIMESTAMP"
			prop.Type = "timestamp"
		}
	case strings.HasPrefix(path, "tags."):
		prop.Metadata["tag_category"] = strings.TrimPrefix(path, "tags.")
	}
}

func (g *AzureAdvancedSchemaGenerator) applyServiceSpecificRules(prop *ARMProperty, serviceName string) {
	switch strings.ToLower(serviceName) {
	case "compute":
		g.applyComputeRules(prop)
	case "storage":
		g.applyStorageRules(prop)
	case "network":
		g.applyNetworkRules(prop)
	case "keyvault":
		g.applyKeyVaultRules(prop)
	}
}

func (g *AzureAdvancedSchemaGenerator) applyComputeRules(prop *ARMProperty) {
	path := strings.ToLower(prop.Path)
	
	switch {
	case strings.Contains(path, "vmsize"):
		prop.Indexed = true
		prop.Metadata["analytics_dimension"] = "vm_size"
	case strings.Contains(path, "ostype"):
		prop.Indexed = true
		prop.Metadata["analytics_dimension"] = "os_type"
	case strings.Contains(path, "powerstate"):
		prop.Indexed = true
		prop.Metadata["analytics_dimension"] = "power_state"
	}
}

func (g *AzureAdvancedSchemaGenerator) applyStorageRules(prop *ARMProperty) {
	path := strings.ToLower(prop.Path)
	
	switch {
	case strings.Contains(path, "accounttype") || strings.Contains(path, "skuname"):
		prop.Indexed = true
		prop.Metadata["analytics_dimension"] = "storage_type"
	case strings.Contains(path, "accesstier"):
		prop.Indexed = true
		prop.Metadata["analytics_dimension"] = "access_tier"
	}
}

func (g *AzureAdvancedSchemaGenerator) applyNetworkRules(prop *ARMProperty) {
	path := strings.ToLower(prop.Path)
	
	switch {
	case strings.Contains(path, "publicipaddress"):
		prop.Metadata["pii_category"] = "network_identifier"
	case strings.Contains(path, "privateipaddress"):
		prop.Metadata["pii_category"] = "network_identifier"
	}
}

func (g *AzureAdvancedSchemaGenerator) applyKeyVaultRules(prop *ARMProperty) {
	path := strings.ToLower(prop.Path)
	
	switch {
	case strings.Contains(path, "vault"):
		prop.Metadata["security_classification"] = "sensitive"
	case strings.Contains(path, "key") || strings.Contains(path, "secret"):
		prop.Metadata["security_classification"] = "restricted"
	}
}

// DuckDBSchema represents the generated DuckDB schema
type DuckDBSchema struct {
	TableName          string                 `json:"tableName"`
	Columns            []ColumnDefinition     `json:"columns"`
	Indexes            []IndexDefinition      `json:"indexes"`
	Views              []ViewDefinition       `json:"views"`
	MaterializedViews  []ViewDefinition       `json:"materializedViews"`
	Partitioning       *PartitioningStrategy  `json:"partitioning,omitempty"`
	CompressionConfig  map[string]string      `json:"compressionConfig"`
	AnalyticsFeatures  []string               `json:"analyticsFeatures"`
}

type ColumnDefinition struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Nullable    bool              `json:"nullable"`
	PrimaryKey  bool              `json:"primaryKey"`
	Default     string            `json:"default,omitempty"`
	Description string            `json:"description"`
	Constraints map[string]string `json:"constraints,omitempty"`
}

type IndexDefinition struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Type    string   `json:"type"` // btree, hash, gin
	Unique  bool     `json:"unique"`
}

type ViewDefinition struct {
	Name        string `json:"name"`
	Query       string `json:"query"`
	Description string `json:"description"`
	Materialized bool  `json:"materialized"`
}

type PartitioningStrategy struct {
	Type      string   `json:"type"` // range, hash, list
	Columns   []string `json:"columns"`
	Strategy  string   `json:"strategy"`
}

// GenerateOptimizedSchema generates an optimized DuckDB schema for a resource
func (g *AzureAdvancedSchemaGenerator) GenerateOptimizedSchema(
	resource AzureResourceInfo,
	sampleData []map[string]interface{},
) (*DuckDBSchema, error) {
	// Parse resource properties and sample data
	properties := g.ParseResourceProperties(resource)
	
	// Generate optimal DuckDB schema
	schema := &DuckDBSchema{
		TableName:         g.generateTableName(resource.FullType),
		Columns:           g.generateColumns(properties),
		Indexes:           g.generateIndexes(properties),
		Views:             g.generateAnalyticalViews(resource),
		MaterializedViews: g.generateMaterializedViews(resource),
		Partitioning:      g.suggestPartitioning(resource),
		CompressionConfig: g.generateCompressionConfig(properties),
		AnalyticsFeatures: g.generateAnalyticsFeatures(resource),
	}
	
	// Add resource-specific optimizations
	g.applyResourceOptimizations(schema, resource)
	
	return schema, nil
}

// generateTableName creates a normalized table name from ARM type
func (g *AzureAdvancedSchemaGenerator) generateTableName(armType string) string {
	// Convert Microsoft.Compute/virtualMachines -> azure_compute_virtual_machines
	parts := strings.Split(armType, "/")
	if len(parts) < 2 {
		return "azure_" + strings.ToLower(strings.ReplaceAll(armType, ".", "_"))
	}
	
	namespace := strings.ToLower(strings.TrimPrefix(parts[0], "Microsoft."))
	resourceType := strings.ToLower(parts[1])
	
	// Convert camelCase to snake_case
	resourceType = g.toSnakeCase(resourceType)
	
	return fmt.Sprintf("azure_%s_%s", namespace, resourceType)
}

func (g *AzureAdvancedSchemaGenerator) toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && (r >= 'A' && r <= 'Z') {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// generateColumns converts ARM properties to DuckDB column definitions
func (g *AzureAdvancedSchemaGenerator) generateColumns(properties []ARMProperty) []ColumnDefinition {
	var columns []ColumnDefinition
	
	for _, prop := range properties {
		column := ColumnDefinition{
			Name:        g.normalizeColumnName(prop.Path),
			Type:        prop.DuckDBType,
			Nullable:    !prop.Required,
			PrimaryKey:  prop.Path == "id",
			Description: prop.Description,
			Constraints: make(map[string]string),
		}
		
		// Add constraints from metadata
		for key, value := range prop.Metadata {
			if strings.HasPrefix(key, "constraint_") {
				column.Constraints[strings.TrimPrefix(key, "constraint_")] = value
			}
		}
		
		// Add pattern validation
		if pattern, exists := prop.Metadata["pattern"]; exists {
			column.Constraints["pattern"] = pattern
		}
		
		columns = append(columns, column)
	}
	
	return columns
}

func (g *AzureAdvancedSchemaGenerator) normalizeColumnName(path string) string {
	// Convert dotted path to snake_case column name
	// e.g., "properties.hardwareProfile.vmSize" -> "properties_hardware_profile_vm_size"
	normalized := strings.ReplaceAll(path, ".", "_")
	normalized = strings.ReplaceAll(normalized, "[", "_")
	normalized = strings.ReplaceAll(normalized, "]", "")
	normalized = g.toSnakeCase(normalized)
	return normalized
}

// generateIndexes creates optimal indexes based on property analysis
func (g *AzureAdvancedSchemaGenerator) generateIndexes(properties []ARMProperty) []IndexDefinition {
	var indexes []IndexDefinition
	
	// Primary key index
	indexes = append(indexes, IndexDefinition{
		Name:    "idx_id",
		Columns: []string{"id"},
		Type:    "btree",
		Unique:  true,
	})
	
	// Indexes for analytical queries
	analyticalColumns := []string{}
	for _, prop := range properties {
		if prop.Indexed {
			colName := g.normalizeColumnName(prop.Path)
			analyticalColumns = append(analyticalColumns, colName)
			
			// Single column index
			indexes = append(indexes, IndexDefinition{
				Name:    fmt.Sprintf("idx_%s", colName),
				Columns: []string{colName},
				Type:    "btree",
				Unique:  false,
			})
		}
	}
	
	// Composite indexes for common query patterns
	if len(analyticalColumns) >= 2 {
		indexes = append(indexes, IndexDefinition{
			Name:    "idx_analytics_composite",
			Columns: analyticalColumns[:2], // First two analytical columns
			Type:    "btree",
			Unique:  false,
		})
	}
	
	// Time-based index for discovery timestamp
	indexes = append(indexes, IndexDefinition{
		Name:    "idx_discovered_at",
		Columns: []string{"discovered_at"},
		Type:    "btree",
		Unique:  false,
	})
	
	return indexes
}

// generateAnalyticalViews creates views for common analytical queries
func (g *AzureAdvancedSchemaGenerator) generateAnalyticalViews(resource AzureResourceInfo) []ViewDefinition {
	tableName := g.generateTableName(resource.FullType)
	
	views := []ViewDefinition{
		{
			Name: fmt.Sprintf("%s_summary", tableName),
			Query: fmt.Sprintf(`
				SELECT 
					location,
					resource_group,
					COUNT(*) as resource_count,
					COUNT(DISTINCT subscription_id) as subscription_count,
					MIN(discovered_at) as first_discovered,
					MAX(discovered_at) as last_discovered
				FROM %s 
				GROUP BY location, resource_group
			`, tableName),
			Description: "Summary statistics by location and resource group",
		},
		{
			Name: fmt.Sprintf("%s_by_location", tableName),
			Query: fmt.Sprintf(`
				SELECT 
					location,
					COUNT(*) as count,
					COUNT(*) * 100.0 / SUM(COUNT(*)) OVER () as percentage
				FROM %s 
				GROUP BY location 
				ORDER BY count DESC
			`, tableName),
			Description: "Resource distribution by Azure location",
		},
		{
			Name: fmt.Sprintf("%s_timeline", tableName),
			Query: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('day', discovered_at) as discovery_date,
					COUNT(*) as resources_discovered,
					COUNT(DISTINCT resource_group) as resource_groups
				FROM %s 
				GROUP BY DATE_TRUNC('day', discovered_at)
				ORDER BY discovery_date
			`, tableName),
			Description: "Resource discovery timeline",
		},
	}
	
	// Add service-specific views
	if strings.Contains(resource.Service, "compute") {
		views = append(views, g.generateComputeViews(tableName)...)
	} else if strings.Contains(resource.Service, "storage") {
		views = append(views, g.generateStorageViews(tableName)...)
	} else if strings.Contains(resource.Service, "network") {
		views = append(views, g.generateNetworkViews(tableName)...)
	}
	
	return views
}

func (g *AzureAdvancedSchemaGenerator) generateComputeViews(tableName string) []ViewDefinition {
	return []ViewDefinition{
		{
			Name: fmt.Sprintf("%s_vm_sizes", tableName),
			Query: fmt.Sprintf(`
				SELECT 
					JSON_EXTRACT_STRING(properties, '$.hardwareProfile.vmSize') as vm_size,
					JSON_EXTRACT_STRING(properties, '$.provisioningState') as provisioning_state,
					COUNT(*) as count
				FROM %s 
				WHERE JSON_EXTRACT_STRING(properties, '$.hardwareProfile.vmSize') IS NOT NULL
				GROUP BY vm_size, provisioning_state
				ORDER BY count DESC
			`, tableName),
			Description: "Virtual machine size distribution",
		},
	}
}

func (g *AzureAdvancedSchemaGenerator) generateStorageViews(tableName string) []ViewDefinition {
	return []ViewDefinition{
		{
			Name: fmt.Sprintf("%s_storage_types", tableName),
			Query: fmt.Sprintf(`
				SELECT 
					JSON_EXTRACT_STRING(sku, '$.name') as sku_name,
					JSON_EXTRACT_STRING(sku, '$.tier') as sku_tier,
					COUNT(*) as count
				FROM %s 
				WHERE sku IS NOT NULL
				GROUP BY sku_name, sku_tier
				ORDER BY count DESC
			`, tableName),
			Description: "Storage account types and tiers",
		},
	}
}

func (g *AzureAdvancedSchemaGenerator) generateNetworkViews(tableName string) []ViewDefinition {
	return []ViewDefinition{
		{
			Name: fmt.Sprintf("%s_network_summary", tableName),
			Query: fmt.Sprintf(`
				SELECT 
					location,
					COUNT(*) as total_resources,
					COUNT(CASE WHEN JSON_EXTRACT_STRING(properties, '$.publicIPAddress') IS NOT NULL THEN 1 END) as public_resources,
					COUNT(CASE WHEN JSON_EXTRACT_STRING(properties, '$.privateIPAddress') IS NOT NULL THEN 1 END) as private_resources
				FROM %s 
				GROUP BY location
				ORDER BY total_resources DESC
			`, tableName),
			Description: "Network resource summary by location",
		},
	}
}

// generateMaterializedViews creates materialized views for performance
func (g *AzureAdvancedSchemaGenerator) generateMaterializedViews(resource AzureResourceInfo) []ViewDefinition {
	tableName := g.generateTableName(resource.FullType)
	
	// Only create materialized views for large datasets
	return []ViewDefinition{
		{
			Name: fmt.Sprintf("%s_hourly_stats", tableName),
			Query: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', discovered_at) as hour,
					location,
					resource_group,
					COUNT(*) as resource_count
				FROM %s 
				GROUP BY DATE_TRUNC('hour', discovered_at), location, resource_group
			`, tableName),
			Description: "Hourly resource statistics (materialized)",
			Materialized: true,
		},
	}
}

// suggestPartitioning suggests optimal partitioning strategy
func (g *AzureAdvancedSchemaGenerator) suggestPartitioning(resource AzureResourceInfo) *PartitioningStrategy {
	// For large datasets, suggest partitioning by location or time
	return &PartitioningStrategy{
		Type:     "range",
		Columns:  []string{"discovered_at"},
		Strategy: "monthly",
	}
}

// generateCompressionConfig optimizes compression settings
func (g *AzureAdvancedSchemaGenerator) generateCompressionConfig(properties []ARMProperty) map[string]string {
	config := make(map[string]string)
	
	for _, prop := range properties {
		if prop.Compressed {
			colName := g.normalizeColumnName(prop.Path)
			
			// Choose compression based on data type
			switch prop.DuckDBType {
			case "JSON":
				config[colName] = "ZSTD"
			case "VARCHAR":
				if strings.Contains(prop.Path, "id") {
					config[colName] = "DICTIONARY"
				} else {
					config[colName] = "ZSTD"
				}
			default:
				config[colName] = "ZSTD"
			}
		}
	}
	
	return config
}

// generateAnalyticsFeatures suggests analytics-specific features
func (g *AzureAdvancedSchemaGenerator) generateAnalyticsFeatures(resource AzureResourceInfo) []string {
	features := []string{
		"temporal_queries",
		"aggregation_pushdown",
		"columnar_storage",
	}
	
	// Add service-specific features
	switch strings.ToLower(resource.Service) {
	case "compute":
		features = append(features, "vm_metrics_analysis", "cost_optimization")
	case "storage":
		features = append(features, "storage_analytics", "capacity_planning")
	case "network":
		features = append(features, "network_topology", "security_analysis")
	}
	
	return features
}

// applyResourceOptimizations applies final optimizations to the schema
func (g *AzureAdvancedSchemaGenerator) applyResourceOptimizations(schema *DuckDBSchema, resource AzureResourceInfo) {
	// Optimize column order for better compression
	g.optimizeColumnOrder(schema)
	
	// Add resource-specific constraints
	g.addResourceConstraints(schema, resource)
	
	// Optimize index selection
	g.optimizeIndexes(schema)
}

func (g *AzureAdvancedSchemaGenerator) optimizeColumnOrder(schema *DuckDBSchema) {
	// Sort columns: primary key first, then indexed columns, then by frequency of use
	sort.Slice(schema.Columns, func(i, j int) bool {
		col1, col2 := schema.Columns[i], schema.Columns[j]
		
		// Primary key first
		if col1.PrimaryKey && !col2.PrimaryKey {
			return true
		}
		if !col1.PrimaryKey && col2.PrimaryKey {
			return false
		}
		
		// Fixed columns next (id, name, type, location, etc.)
		fixedOrder := []string{"id", "name", "type", "location", "resource_group", "subscription_id"}
		pos1, pos2 := findInSlice(fixedOrder, col1.Name), findInSlice(fixedOrder, col2.Name)
		
		if pos1 != -1 && pos2 != -1 {
			return pos1 < pos2
		}
		if pos1 != -1 && pos2 == -1 {
			return true
		}
		if pos1 == -1 && pos2 != -1 {
			return false
		}
		
		// Alphabetical order for the rest
		return col1.Name < col2.Name
	})
}

func findInSlice(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}

func (g *AzureAdvancedSchemaGenerator) addResourceConstraints(schema *DuckDBSchema, resource AzureResourceInfo) {
	// Add ARM-specific constraints
	for i := range schema.Columns {
		col := &schema.Columns[i]
		
		switch col.Name {
		case "id":
			col.Constraints["format"] = "azure_resource_id"
			col.Constraints["regex"] = "^/subscriptions/[a-f0-9-]+/.*"
		case "location":
			col.Constraints["enum_source"] = "azure_regions"
		case "type":
			col.Constraints["pattern"] = "microsoft\\.[a-z]+/[a-z]+"
		}
	}
}

func (g *AzureAdvancedSchemaGenerator) optimizeIndexes(schema *DuckDBSchema) {
	// Remove redundant indexes and optimize for query patterns
	optimizedIndexes := []IndexDefinition{}
	indexMap := make(map[string]bool)
	
	for _, index := range schema.Indexes {
		// Create a key for the index to detect duplicates
		key := fmt.Sprintf("%s_%v", index.Type, index.Columns)
		if !indexMap[key] {
			optimizedIndexes = append(optimizedIndexes, index)
			indexMap[key] = true
		}
	}
	
	schema.Indexes = optimizedIndexes
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
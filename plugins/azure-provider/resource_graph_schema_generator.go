package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ResourceGraphSchemaGenerator generates schemas using Resource Graph discovery
type ResourceGraphSchemaGenerator struct {
	resourceGraph *ResourceGraphClient
	schemaCache   map[string]*pb.Schema
	mu            sync.RWMutex
}

// NewResourceGraphSchemaGenerator creates a new Resource Graph-based schema generator
func NewResourceGraphSchemaGenerator(resourceGraph *ResourceGraphClient) *ResourceGraphSchemaGenerator {
	return &ResourceGraphSchemaGenerator{
		resourceGraph: resourceGraph,
		schemaCache:   make(map[string]*pb.Schema),
	}
}

// GenerateSchemas generates schemas for all discovered resource types
func (g *ResourceGraphSchemaGenerator) GenerateSchemas(ctx context.Context, services []*pb.ServiceInfo) ([]*pb.Schema, error) {
	var schemas []*pb.Schema
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process services concurrently
	semaphore := make(chan struct{}, 5) // Limit concurrency

	for _, service := range services {
		for _, resourceType := range service.ResourceTypes {
			wg.Add(1)
			go func(svc *pb.ServiceInfo, rt *pb.ResourceType) {
				defer wg.Done()

				semaphore <- struct{}{}        // Acquire
				defer func() { <-semaphore }() // Release

				schema, err := g.generateSchemaForResourceType(ctx, svc, rt)
				if err != nil {
					log.Printf("Failed to generate schema for %s: %v", rt.TypeName, err)
					return
				}

				mu.Lock()
				schemas = append(schemas, schema)
				mu.Unlock()
			}(service, resourceType)
		}
	}

	wg.Wait()
	return schemas, nil
}

// generateSchemaForResourceType generates a schema for a specific resource type
func (g *ResourceGraphSchemaGenerator) generateSchemaForResourceType(ctx context.Context, service *pb.ServiceInfo, resourceType *pb.ResourceType) (*pb.Schema, error) {
	g.mu.Lock()
	cacheKey := resourceType.TypeName
	if cached, exists := g.schemaCache[cacheKey]; exists {
		g.mu.Unlock()
		return cached, nil
	}
	g.mu.Unlock()

	// Discover schema using Resource Graph
	resourceSchema, err := g.resourceGraph.DiscoverResourceSchema(ctx, resourceType.TypeName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover schema for %s: %w", resourceType.TypeName, err)
	}

	// Convert to DuckDB schema
	tableName := g.generateTableName(service.Name, resourceType.Name)
	sqlSchema := g.generateDuckDBSchema(tableName, resourceSchema)

	schema := &pb.Schema{
		Name:         tableName,
		Service:      service.Name,
		ResourceType: resourceType.Name,
		Sql:          sqlSchema,
		Description:  fmt.Sprintf("Auto-generated schema for %s resources", resourceType.TypeName),
		Metadata: map[string]string{
			"provider":           "azure",
			"resource_type":      resourceType.TypeName,
			"discovery_method":   "resource_graph",
			"sample_count":       fmt.Sprintf("%d", resourceSchema.SampleCount),
			"property_count":     fmt.Sprintf("%d", len(resourceSchema.Properties)),
			"supports_tags":      "true",
			"supports_locations": "true",
		},
	}

	// Cache the schema
	g.mu.Lock()
	g.schemaCache[cacheKey] = schema
	g.mu.Unlock()

	return schema, nil
}

// generateTableName creates a consistent table name
func (g *ResourceGraphSchemaGenerator) generateTableName(serviceName, resourceTypeName string) string {
	// Convert to snake_case and ensure uniqueness
	tableName := fmt.Sprintf("azure_%s_%s", 
		strings.ToLower(strings.ReplaceAll(serviceName, "-", "_")),
		strings.ToLower(strings.ReplaceAll(resourceTypeName, "-", "_")))
	
	// Remove any invalid characters
	tableName = strings.ReplaceAll(tableName, ".", "_")
	tableName = strings.ReplaceAll(tableName, "/", "_")
	
	return tableName
}

// generateDuckDBSchema converts ResourceSchema to DuckDB CREATE TABLE statement
func (g *ResourceGraphSchemaGenerator) generateDuckDBSchema(tableName string, resourceSchema *ResourceSchema) string {
	var columns []string

	// Standard Azure resource columns
	columns = append(columns, 
		"id VARCHAR PRIMARY KEY",
		"name VARCHAR NOT NULL",
		"type VARCHAR NOT NULL", 
		"location VARCHAR",
		"resource_group VARCHAR",
		"subscription_id VARCHAR",
		"tags JSON",
		"discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP",
	)

	// Add discovered properties
	for propName, propDef := range resourceSchema.Properties {
		columnName := g.sanitizeColumnName(propName)
		columnType := g.mapPropertyTypeToDuckDB(propDef.Type)
		nullable := "NULL"
		if propDef.Required {
			nullable = "NOT NULL"
		}
		
		columns = append(columns, fmt.Sprintf("%s %s %s", columnName, columnType, nullable))
	}

	// Build CREATE TABLE statement
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n);", 
		tableName, 
		strings.Join(columns, ",\n  "))

	// Add indexes
	indexes := g.generateIndexes(tableName, resourceSchema)
	if len(indexes) > 0 {
		sql += "\n\n" + strings.Join(indexes, "\n")
	}

	return sql
}

// sanitizeColumnName ensures column names are valid for DuckDB
func (g *ResourceGraphSchemaGenerator) sanitizeColumnName(name string) string {
	// Replace dots and other special characters
	sanitized := strings.ReplaceAll(name, ".", "_")
	sanitized = strings.ReplaceAll(sanitized, "-", "_")
	sanitized = strings.ReplaceAll(sanitized, " ", "_")
	sanitized = strings.ToLower(sanitized)
	
	// Ensure it doesn't start with a number
	if len(sanitized) > 0 && sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "prop_" + sanitized
	}
	
	return sanitized
}

// mapPropertyTypeToDuckDB maps Resource Graph property types to DuckDB types
func (g *ResourceGraphSchemaGenerator) mapPropertyTypeToDuckDB(propType string) string {
	switch propType {
	case "string":
		return "VARCHAR"
	case "integer":
		return "BIGINT"
	case "number":
		return "DOUBLE"
	case "boolean":
		return "BOOLEAN"
	case "object", "array":
		return "JSON"
	default:
		return "VARCHAR" // Default to VARCHAR for unknown types
	}
}

// generateIndexes creates appropriate indexes for the table
func (g *ResourceGraphSchemaGenerator) generateIndexes(tableName string, resourceSchema *ResourceSchema) []string {
	var indexes []string

	// Standard indexes for Azure resources
	indexes = append(indexes,
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_resource_group ON %s(resource_group);", tableName, tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_location ON %s(location);", tableName, tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_type ON %s(type);", tableName, tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_discovered_at ON %s(discovered_at);", tableName, tableName),
	)

	// Add indexes for commonly queried properties
	for propName, propDef := range resourceSchema.Properties {
		if g.shouldIndex(propName, propDef) {
			columnName := g.sanitizeColumnName(propName)
			indexName := fmt.Sprintf("idx_%s_%s", tableName, columnName)
			indexes = append(indexes, 
				fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s(%s);", indexName, tableName, columnName))
		}
	}

	return indexes
}

// shouldIndex determines if a property should be indexed
func (g *ResourceGraphSchemaGenerator) shouldIndex(propName string, propDef PropertyDef) bool {
	// Index commonly queried properties
	commonIndexProps := []string{
		"status", "state", "provisioning_state", "power_state",
		"sku", "tier", "kind", "size", "version",
		"enabled", "public", "private",
	}

	lowerPropName := strings.ToLower(propName)
	for _, common := range commonIndexProps {
		if strings.Contains(lowerPropName, common) {
			return true
		}
	}

	// Index if it's a required property and not an object/array
	return propDef.Required && propDef.Type != "object" && propDef.Type != "array"
}

// GenerateUnifiedSchema generates a unified schema for all Azure resources
func (g *ResourceGraphSchemaGenerator) GenerateUnifiedSchema() *pb.Schema {
	return &pb.Schema{
		Name:         "azure_resources_unified",
		Service:      "core",
		ResourceType: "all",
		Sql: `
CREATE TABLE IF NOT EXISTS azure_resources_unified (
  id VARCHAR PRIMARY KEY,
  name VARCHAR NOT NULL,
  type VARCHAR NOT NULL,
  location VARCHAR,
  resource_group VARCHAR NOT NULL,
  subscription_id VARCHAR NOT NULL,
  tags JSON,
  properties JSON,
  discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  
  -- Common computed columns
  provider VARCHAR GENERATED ALWAYS AS (split_part(type, '/', 1)) STORED,
  service VARCHAR GENERATED ALWAYS AS (split_part(type, '/', 2)) STORED,
  resource_type VARCHAR GENERATED ALWAYS AS (split_part(type, '/', 3)) STORED
);

-- Indexes for the unified table
CREATE INDEX IF NOT EXISTS idx_azure_unified_resource_group ON azure_resources_unified(resource_group);
CREATE INDEX IF NOT EXISTS idx_azure_unified_location ON azure_resources_unified(location);
CREATE INDEX IF NOT EXISTS idx_azure_unified_type ON azure_resources_unified(type);
CREATE INDEX IF NOT EXISTS idx_azure_unified_provider ON azure_resources_unified(provider);
CREATE INDEX IF NOT EXISTS idx_azure_unified_service ON azure_resources_unified(service);
CREATE INDEX IF NOT EXISTS idx_azure_unified_discovered_at ON azure_resources_unified(discovered_at);
`,
		Description: "Unified table for all Azure resources discovered via Resource Graph",
		Metadata: map[string]string{
			"provider":         "azure",
			"table_type":       "unified",
			"discovery_method": "resource_graph",
			"supports_json":    "true",
			"supports_tags":    "true",
		},
	}
}

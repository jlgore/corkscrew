package main

import (
	"fmt"
	"strings"
	"sync"
)

// TableSchema represents a database table schema
type TableSchema struct {
	TableName string
	Columns   []*ColumnDefinition
	Indexes   []string
}

// ColumnDefinition represents a database column definition
// Note: This is now defined in advanced_schema_generator.go to avoid duplication
// type ColumnDefinition struct {
//	Name       string
//	Type       string
//	Nullable   bool
//	PrimaryKey bool
// }

// AzureSchemaGenerator generates database schemas for Azure resources
type AzureSchemaGenerator struct {
	schemaCache map[string]*TableSchema
	mu          sync.RWMutex
}

// NewAzureSchemaGenerator creates a new Azure schema generator
func NewAzureSchemaGenerator() *AzureSchemaGenerator {
	return &AzureSchemaGenerator{
		schemaCache: make(map[string]*TableSchema),
	}
}

// GenerateSchema generates a database schema for a resource type
func (g *AzureSchemaGenerator) GenerateSchema(resourceType ResourceTypeInfo) *TableSchema {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check cache
	tableName := g.generateTableName(resourceType.ResourceType)
	if schema, exists := g.schemaCache[tableName]; exists {
		return schema
	}

	// Generate schema based on resource type
	schema := &TableSchema{
		TableName: tableName,
		Columns: []*ColumnDefinition{
			// Common Azure resource columns
			{Name: "id", Type: "VARCHAR", PrimaryKey: true},
			{Name: "name", Type: "VARCHAR", Nullable: false},
			{Name: "type", Type: "VARCHAR", Nullable: false},
			{Name: "location", Type: "VARCHAR", Nullable: false},
			{Name: "resource_group", Type: "VARCHAR", Nullable: false},
			{Name: "subscription_id", Type: "VARCHAR", Nullable: false},
			{Name: "properties", Type: "JSON", Nullable: true},
			{Name: "tags", Type: "JSON", Nullable: true},
			{Name: "created_time", Type: "TIMESTAMP", Nullable: true},
			{Name: "changed_time", Type: "TIMESTAMP", Nullable: true},
			{Name: "provisioning_state", Type: "VARCHAR", Nullable: true},
			{Name: "discovered_at", Type: "TIMESTAMP", Nullable: false},
		},
		Indexes: []string{
			"idx_resource_group",
			"idx_location",
			"idx_type",
			"idx_provisioning_state",
		},
	}

	// Add resource-specific columns based on type
	g.addResourceSpecificColumns(schema, resourceType.ResourceType)

	// Cache the schema
	g.schemaCache[tableName] = schema

	return schema
}

// generateTableName creates a table name from resource type
func (g *AzureSchemaGenerator) generateTableName(resourceType string) string {
	// Microsoft.Compute/virtualMachines -> azure_compute_virtualmachines
	cleaned := strings.ToLower(resourceType)
	cleaned = strings.ReplaceAll(cleaned, "microsoft.", "azure_")
	cleaned = strings.ReplaceAll(cleaned, "/", "_")
	cleaned = strings.ReplaceAll(cleaned, "-", "_")
	cleaned = strings.ReplaceAll(cleaned, ".", "_")
	return cleaned
}

// addResourceSpecificColumns adds columns specific to certain resource types
func (g *AzureSchemaGenerator) addResourceSpecificColumns(schema *TableSchema, resourceType string) {
	// Add columns specific to certain resource types
	switch resourceType {
	case "Microsoft.Compute/virtualMachines":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "vm_size", Type: "VARCHAR", Nullable: true},
			{Name: "os_type", Type: "VARCHAR", Nullable: true},
			{Name: "os_disk_type", Type: "VARCHAR", Nullable: true},
			{Name: "availability_set_id", Type: "VARCHAR", Nullable: true},
			{Name: "network_interface_ids", Type: "JSON", Nullable: true},
			{Name: "power_state", Type: "VARCHAR", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_vm_size", "idx_os_type", "idx_power_state")

	case "Microsoft.Storage/storageAccounts":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "account_type", Type: "VARCHAR", Nullable: true},
			{Name: "access_tier", Type: "VARCHAR", Nullable: true},
			{Name: "encryption_enabled", Type: "BOOLEAN", Nullable: true},
			{Name: "https_traffic_only", Type: "BOOLEAN", Nullable: true},
			{Name: "blob_public_access", Type: "BOOLEAN", Nullable: true},
			{Name: "primary_endpoints", Type: "JSON", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_account_type", "idx_access_tier")

	case "Microsoft.Network/virtualNetworks":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "address_space", Type: "JSON", Nullable: true},
			{Name: "dns_servers", Type: "JSON", Nullable: true},
			{Name: "subnet_count", Type: "INTEGER", Nullable: true},
			{Name: "peering_count", Type: "INTEGER", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_subnet_count")

	case "Microsoft.Network/networkSecurityGroups":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "security_rules_count", Type: "INTEGER", Nullable: true},
			{Name: "default_rules_count", Type: "INTEGER", Nullable: true},
			{Name: "associated_subnets", Type: "JSON", Nullable: true},
			{Name: "associated_interfaces", Type: "JSON", Nullable: true},
		}...)

	case "Microsoft.Web/sites":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "app_service_plan_id", Type: "VARCHAR", Nullable: true},
			{Name: "runtime_stack", Type: "VARCHAR", Nullable: true},
			{Name: "https_only", Type: "BOOLEAN", Nullable: true},
			{Name: "client_affinity_enabled", Type: "BOOLEAN", Nullable: true},
			{Name: "default_hostname", Type: "VARCHAR", Nullable: true},
			{Name: "state", Type: "VARCHAR", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_app_service_plan_id", "idx_runtime_stack", "idx_state")

	case "Microsoft.Sql/servers":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "administrator_login", Type: "VARCHAR", Nullable: true},
			{Name: "version", Type: "VARCHAR", Nullable: true},
			{Name: "fully_qualified_domain_name", Type: "VARCHAR", Nullable: true},
			{Name: "public_network_access", Type: "VARCHAR", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_version", "idx_public_network_access")

	case "Microsoft.Sql/servers/databases":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "server_name", Type: "VARCHAR", Nullable: true},
			{Name: "database_id", Type: "VARCHAR", Nullable: true},
			{Name: "edition", Type: "VARCHAR", Nullable: true},
			{Name: "service_objective", Type: "VARCHAR", Nullable: true},
			{Name: "collation", Type: "VARCHAR", Nullable: true},
			{Name: "max_size_bytes", Type: "BIGINT", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_server_name", "idx_edition", "idx_service_objective")

	case "Microsoft.KeyVault/vaults":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "vault_uri", Type: "VARCHAR", Nullable: true},
			{Name: "tenant_id", Type: "VARCHAR", Nullable: true},
			{Name: "sku_name", Type: "VARCHAR", Nullable: true},
			{Name: "enabled_for_deployment", Type: "BOOLEAN", Nullable: true},
			{Name: "enabled_for_template_deployment", Type: "BOOLEAN", Nullable: true},
			{Name: "enabled_for_disk_encryption", Type: "BOOLEAN", Nullable: true},
			{Name: "soft_delete_enabled", Type: "BOOLEAN", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_tenant_id", "idx_sku_name")

	case "Microsoft.ContainerService/managedClusters":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "kubernetes_version", Type: "VARCHAR", Nullable: true},
			{Name: "dns_prefix", Type: "VARCHAR", Nullable: true},
			{Name: "fqdn", Type: "VARCHAR", Nullable: true},
			{Name: "node_resource_group", Type: "VARCHAR", Nullable: true},
			{Name: "agent_pool_count", Type: "INTEGER", Nullable: true},
			{Name: "network_plugin", Type: "VARCHAR", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_kubernetes_version", "idx_network_plugin")

	case "Microsoft.DocumentDB/databaseAccounts":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "database_account_offer_type", Type: "VARCHAR", Nullable: true},
			{Name: "consistency_policy", Type: "JSON", Nullable: true},
			{Name: "locations", Type: "JSON", Nullable: true},
			{Name: "failover_policies", Type: "JSON", Nullable: true},
			{Name: "enable_multiple_write_locations", Type: "BOOLEAN", Nullable: true},
		}...)

	case "Microsoft.Insights/components":
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "application_type", Type: "VARCHAR", Nullable: true},
			{Name: "application_id", Type: "VARCHAR", Nullable: true},
			{Name: "instrumentation_key", Type: "VARCHAR", Nullable: true},
			{Name: "retention_in_days", Type: "INTEGER", Nullable: true},
			{Name: "sampling_percentage", Type: "FLOAT", Nullable: true},
		}...)
		schema.Indexes = append(schema.Indexes, "idx_application_type")

	default:
		// For unknown resource types, add generic columns
		schema.Columns = append(schema.Columns, []*ColumnDefinition{
			{Name: "kind", Type: "VARCHAR", Nullable: true},
			{Name: "sku_name", Type: "VARCHAR", Nullable: true},
			{Name: "sku_tier", Type: "VARCHAR", Nullable: true},
			{Name: "managed_by", Type: "VARCHAR", Nullable: true},
		}...)
	}
}

// GenerateSchemaForProvider generates schemas for all resource types in a provider
func (g *AzureSchemaGenerator) GenerateSchemaForProvider(provider *ProviderInfo) []*TableSchema {
	var schemas []*TableSchema

	for _, resourceType := range provider.ResourceTypes {
		schema := g.GenerateSchema(resourceType)
		schemas = append(schemas, schema)
	}

	return schemas
}

// GetCachedSchema returns a cached schema if available
func (g *AzureSchemaGenerator) GetCachedSchema(resourceType string) *TableSchema {
	g.mu.RLock()
	defer g.mu.RUnlock()

	tableName := g.generateTableName(resourceType)
	return g.schemaCache[tableName]
}

// ClearCache clears the schema cache
func (g *AzureSchemaGenerator) ClearCache() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.schemaCache = make(map[string]*TableSchema)
}

// GenerateCreateTableSQL generates CREATE TABLE SQL for a schema
func (g *AzureSchemaGenerator) GenerateCreateTableSQL(schema *TableSchema) string {
	var columns []string
	for _, col := range schema.Columns {
		nullable := ""
		if col.Nullable {
			nullable = " NULL"
		} else {
			nullable = " NOT NULL"
		}

		pk := ""
		if col.PrimaryKey {
			pk = " PRIMARY KEY"
		}

		columns = append(columns, fmt.Sprintf("  %s %s%s%s", col.Name, col.Type, nullable, pk))
	}

	sql := fmt.Sprintf("CREATE TABLE %s (\n%s\n);", schema.TableName, strings.Join(columns, ",\n"))

	// Add indexes
	for _, index := range schema.Indexes {
		sql += fmt.Sprintf("\nCREATE INDEX %s ON %s (%s);", index, schema.TableName, strings.TrimPrefix(index, "idx_"))
	}

	return sql
}

// GenerateInsertSQL generates INSERT SQL for a resource
func (g *AzureSchemaGenerator) GenerateInsertSQL(schema *TableSchema, resourceData map[string]interface{}) string {
	var columns []string
	var placeholders []string

	for _, col := range schema.Columns {
		if _, exists := resourceData[col.Name]; exists {
			columns = append(columns, col.Name)
			placeholders = append(placeholders, "?")
		}
	}

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		schema.TableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))
}

// ValidateSchema validates a schema definition
func (g *AzureSchemaGenerator) ValidateSchema(schema *TableSchema) []string {
	var errors []string

	if schema.TableName == "" {
		errors = append(errors, "table name is required")
	}

	if len(schema.Columns) == 0 {
		errors = append(errors, "at least one column is required")
	}

	hasPrimaryKey := false
	columnNames := make(map[string]bool)

	for _, col := range schema.Columns {
		if col.Name == "" {
			errors = append(errors, "column name is required")
			continue
		}

		if columnNames[col.Name] {
			errors = append(errors, fmt.Sprintf("duplicate column name: %s", col.Name))
		}
		columnNames[col.Name] = true

		if col.Type == "" {
			errors = append(errors, fmt.Sprintf("column type is required for %s", col.Name))
		}

		if col.PrimaryKey {
			if hasPrimaryKey {
				errors = append(errors, "multiple primary keys not allowed")
			}
			hasPrimaryKey = true
		}
	}

	if !hasPrimaryKey {
		errors = append(errors, "primary key is required")
	}

	return errors
}

// GetSchemaStatistics returns statistics about cached schemas
func (g *AzureSchemaGenerator) GetSchemaStatistics() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_schemas"] = len(g.schemaCache)

	columnCounts := make(map[int]int)
	indexCounts := make(map[int]int)

	for _, schema := range g.schemaCache {
		columnCount := len(schema.Columns)
		indexCount := len(schema.Indexes)

		columnCounts[columnCount]++
		indexCounts[indexCount]++
	}

	stats["column_distribution"] = columnCounts
	stats["index_distribution"] = indexCounts

	return stats
}

// ExportSchemas exports all cached schemas
func (g *AzureSchemaGenerator) ExportSchemas() map[string]*TableSchema {
	g.mu.RLock()
	defer g.mu.RUnlock()

	exported := make(map[string]*TableSchema)
	for name, schema := range g.schemaCache {
		exported[name] = schema
	}

	return exported
}

// ImportSchemas imports schemas into the cache
func (g *AzureSchemaGenerator) ImportSchemas(schemas map[string]*TableSchema) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for name, schema := range schemas {
		g.schemaCache[name] = schema
	}
}

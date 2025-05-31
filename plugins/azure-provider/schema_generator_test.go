package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAzureSchemaGenerator(t *testing.T) {
	generator := NewAzureSchemaGenerator()
	
	assert.NotNil(t, generator)
	assert.NotNil(t, generator.schemaCache)
	assert.Equal(t, 0, len(generator.schemaCache))
}

func TestGenerateSchema(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	// Test basic resource type
	resourceType := ResourceTypeInfo{
		ResourceType: "Microsoft.Compute/virtualMachines",
		APIVersions:  []string{"2023-03-01"},
		Locations:    []string{"East US", "West US"},
	}

	schema := generator.GenerateSchema(resourceType)
	
	require.NotNil(t, schema)
	assert.Equal(t, "azure_compute_virtualmachines", schema.TableName)
	assert.NotEmpty(t, schema.Columns)
	assert.NotEmpty(t, schema.Indexes)

	// Verify common columns are present
	hasIDColumn := false
	hasNameColumn := false
	hasTypeColumn := false
	hasPrimaryKey := false

	for _, col := range schema.Columns {
		switch col.Name {
		case "id":
			hasIDColumn = true
			if col.PrimaryKey {
				hasPrimaryKey = true
			}
		case "name":
			hasNameColumn = true
		case "type":
			hasTypeColumn = true
		}
	}

	assert.True(t, hasIDColumn, "Missing ID column")
	assert.True(t, hasNameColumn, "Missing name column")
	assert.True(t, hasTypeColumn, "Missing type column")
	assert.True(t, hasPrimaryKey, "Missing primary key")
}

func TestGenerateTableName(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "Microsoft.Compute/virtualMachines",
			expected: "azure_compute_virtualmachines",
		},
		{
			input:    "Microsoft.Storage/storageAccounts",
			expected: "azure_storage_storageaccounts",
		},
		{
			input:    "Microsoft.Network/networkSecurityGroups",
			expected: "azure_network_networksecuritygroups",
		},
		{
			input:    "Microsoft.Sql/servers/databases",
			expected: "azure_sql_servers_databases",
		},
		{
			input:    "Microsoft.Web/sites",
			expected: "azure_web_sites",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := generator.generateTableName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAddResourceSpecificColumns(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	tests := []struct {
		name            string
		resourceType    string
		expectedColumns []string
		expectedIndexes []string
	}{
		{
			name:         "Virtual Machines",
			resourceType: "Microsoft.Compute/virtualMachines",
			expectedColumns: []string{
				"vm_size", "os_type", "os_disk_type", 
				"availability_set_id", "network_interface_ids", "power_state",
			},
			expectedIndexes: []string{
				"idx_vm_size", "idx_os_type", "idx_power_state",
			},
		},
		{
			name:         "Storage Accounts",
			resourceType: "Microsoft.Storage/storageAccounts",
			expectedColumns: []string{
				"account_type", "access_tier", "encryption_enabled",
				"https_traffic_only", "blob_public_access", "primary_endpoints",
			},
			expectedIndexes: []string{
				"idx_account_type", "idx_access_tier",
			},
		},
		{
			name:         "Virtual Networks",
			resourceType: "Microsoft.Network/virtualNetworks",
			expectedColumns: []string{
				"address_space", "dns_servers", "subnet_count", "peering_count",
			},
			expectedIndexes: []string{
				"idx_subnet_count",
			},
		},
		{
			name:         "Web Apps",
			resourceType: "Microsoft.Web/sites",
			expectedColumns: []string{
				"app_service_plan_id", "runtime_stack", "https_only",
				"client_affinity_enabled", "default_hostname", "state",
			},
			expectedIndexes: []string{
				"idx_app_service_plan_id", "idx_runtime_stack", "idx_state",
			},
		},
		{
			name:         "SQL Servers",
			resourceType: "Microsoft.Sql/servers",
			expectedColumns: []string{
				"administrator_login", "version", "fully_qualified_domain_name",
				"public_network_access",
			},
			expectedIndexes: []string{
				"idx_version", "idx_public_network_access",
			},
		},
		{
			name:         "Key Vaults",
			resourceType: "Microsoft.KeyVault/vaults",
			expectedColumns: []string{
				"vault_uri", "tenant_id", "sku_name", "enabled_for_deployment",
				"enabled_for_template_deployment", "enabled_for_disk_encryption",
				"soft_delete_enabled",
			},
			expectedIndexes: []string{
				"idx_tenant_id", "idx_sku_name",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create base schema
			schema := &TableSchema{
				TableName: generator.generateTableName(tt.resourceType),
				Columns: []*ColumnDefinition{
					{Name: "id", Type: "VARCHAR", PrimaryKey: true},
				},
				Indexes: []string{"idx_base"},
			}

			generator.addResourceSpecificColumns(schema, tt.resourceType)

			// Check for expected columns
			for _, expectedCol := range tt.expectedColumns {
				found := false
				for _, col := range schema.Columns {
					if col.Name == expectedCol {
						found = true
						break
					}
				}
				assert.True(t, found, "Missing expected column: %s", expectedCol)
			}

			// Check for expected indexes
			for _, expectedIdx := range tt.expectedIndexes {
				found := false
				for _, idx := range schema.Indexes {
					if idx == expectedIdx {
						found = true
						break
					}
				}
				assert.True(t, found, "Missing expected index: %s", expectedIdx)
			}
		})
	}
}

func TestGenerateSchemaForProvider(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	provider := &ProviderInfo{
		Namespace: "Microsoft.Test",
		ResourceTypes: []ResourceTypeInfo{
			{
				ResourceType: "Microsoft.Test/typeA",
				APIVersions:  []string{"2023-01-01"},
			},
			{
				ResourceType: "Microsoft.Test/typeB",
				APIVersions:  []string{"2023-01-01"},
			},
			{
				ResourceType: "Microsoft.Test/typeC",
				APIVersions:  []string{"2023-01-01"},
			},
		},
	}

	schemas := generator.GenerateSchemaForProvider(provider)

	assert.Len(t, schemas, 3)
	
	expectedTableNames := []string{
		"azure_test_typea",
		"azure_test_typeb",
		"azure_test_typec",
	}

	for i, schema := range schemas {
		assert.Equal(t, expectedTableNames[i], schema.TableName)
		assert.NotEmpty(t, schema.Columns)
	}
}

func TestGetCachedSchema(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	// Test non-existent schema
	schema := generator.GetCachedSchema("Microsoft.Test/nonExistent")
	assert.Nil(t, schema)

	// Generate a schema to cache it
	resourceType := ResourceTypeInfo{
		ResourceType: "Microsoft.Test/testResource",
	}
	generatedSchema := generator.GenerateSchema(resourceType)
	
	// Now test cached schema
	cachedSchema := generator.GetCachedSchema("Microsoft.Test/testResource")
	assert.NotNil(t, cachedSchema)
	assert.Equal(t, generatedSchema.TableName, cachedSchema.TableName)
}

func TestClearCache(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	// Generate some schemas
	resourceTypes := []ResourceTypeInfo{
		{ResourceType: "Microsoft.Test/type1"},
		{ResourceType: "Microsoft.Test/type2"},
	}

	for _, rt := range resourceTypes {
		generator.GenerateSchema(rt)
	}

	assert.Equal(t, 2, len(generator.schemaCache))

	// Clear cache
	generator.ClearCache()
	assert.Equal(t, 0, len(generator.schemaCache))
}

func TestGenerateCreateTableSQL(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	schema := &TableSchema{
		TableName: "test_table",
		Columns: []*ColumnDefinition{
			{Name: "id", Type: "VARCHAR", PrimaryKey: true, Nullable: false},
			{Name: "name", Type: "VARCHAR", Nullable: false},
			{Name: "description", Type: "TEXT", Nullable: true},
			{Name: "created_at", Type: "TIMESTAMP", Nullable: false},
		},
		Indexes: []string{"idx_name", "idx_created_at"},
	}

	sql := generator.GenerateCreateTableSQL(schema)

	// Verify CREATE TABLE statement
	assert.Contains(t, sql, "CREATE TABLE test_table")
	assert.Contains(t, sql, "id VARCHAR NOT NULL PRIMARY KEY")
	assert.Contains(t, sql, "name VARCHAR NOT NULL")
	assert.Contains(t, sql, "description TEXT NULL")
	assert.Contains(t, sql, "created_at TIMESTAMP NOT NULL")

	// Verify indexes
	assert.Contains(t, sql, "CREATE INDEX idx_name ON test_table (name)")
	assert.Contains(t, sql, "CREATE INDEX idx_created_at ON test_table (created_at)")
}

func TestGenerateInsertSQL(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	schema := &TableSchema{
		TableName: "test_table",
		Columns: []*ColumnDefinition{
			{Name: "id", Type: "VARCHAR"},
			{Name: "name", Type: "VARCHAR"},
			{Name: "type", Type: "VARCHAR"},
			{Name: "location", Type: "VARCHAR"},
		},
	}

	resourceData := map[string]interface{}{
		"id":       "test-id",
		"name":     "test-name",
		"type":     "test-type",
		"location": "East US",
		"extra":    "not-in-schema", // This should be ignored
	}

	sql := generator.GenerateInsertSQL(schema, resourceData)

	expected := "INSERT INTO test_table (id, name, type, location) VALUES (?, ?, ?, ?);"
	assert.Equal(t, expected, sql)
}

func TestValidateSchema(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	tests := []struct {
		name     string
		schema   *TableSchema
		hasError bool
		errorMsg string
	}{
		{
			name: "Valid schema",
			schema: &TableSchema{
				TableName: "valid_table",
				Columns: []*ColumnDefinition{
					{Name: "id", Type: "VARCHAR", PrimaryKey: true},
					{Name: "name", Type: "VARCHAR"},
				},
			},
			hasError: false,
		},
		{
			name: "Missing table name",
			schema: &TableSchema{
				Columns: []*ColumnDefinition{
					{Name: "id", Type: "VARCHAR", PrimaryKey: true},
				},
			},
			hasError: true,
			errorMsg: "table name is required",
		},
		{
			name: "No columns",
			schema: &TableSchema{
				TableName: "empty_table",
				Columns:   []*ColumnDefinition{},
			},
			hasError: true,
			errorMsg: "at least one column is required",
		},
		{
			name: "Missing primary key",
			schema: &TableSchema{
				TableName: "no_pk_table",
				Columns: []*ColumnDefinition{
					{Name: "name", Type: "VARCHAR"},
				},
			},
			hasError: true,
			errorMsg: "primary key is required",
		},
		{
			name: "Duplicate column names",
			schema: &TableSchema{
				TableName: "duplicate_table",
				Columns: []*ColumnDefinition{
					{Name: "id", Type: "VARCHAR", PrimaryKey: true},
					{Name: "name", Type: "VARCHAR"},
					{Name: "name", Type: "TEXT"}, // Duplicate
				},
			},
			hasError: true,
			errorMsg: "duplicate column name",
		},
		{
			name: "Multiple primary keys",
			schema: &TableSchema{
				TableName: "multi_pk_table",
				Columns: []*ColumnDefinition{
					{Name: "id1", Type: "VARCHAR", PrimaryKey: true},
					{Name: "id2", Type: "VARCHAR", PrimaryKey: true}, // Multiple PKs
				},
			},
			hasError: true,
			errorMsg: "multiple primary keys not allowed",
		},
		{
			name: "Missing column name",
			schema: &TableSchema{
				TableName: "missing_col_name_table",
				Columns: []*ColumnDefinition{
					{Name: "id", Type: "VARCHAR", PrimaryKey: true},
					{Name: "", Type: "VARCHAR"}, // Missing name
				},
			},
			hasError: true,
			errorMsg: "column name is required",
		},
		{
			name: "Missing column type",
			schema: &TableSchema{
				TableName: "missing_col_type_table",
				Columns: []*ColumnDefinition{
					{Name: "id", Type: "VARCHAR", PrimaryKey: true},
					{Name: "name", Type: ""}, // Missing type
				},
			},
			hasError: true,
			errorMsg: "column type is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := generator.ValidateSchema(tt.schema)

			if tt.hasError {
				assert.NotEmpty(t, errors, "Expected validation errors")
				if tt.errorMsg != "" {
					found := false
					for _, err := range errors {
						if strings.Contains(err, tt.errorMsg) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected error message '%s' not found in: %v", tt.errorMsg, errors)
				}
			} else {
				assert.Empty(t, errors, "Unexpected validation errors: %v", errors)
			}
		})
	}
}

func TestGetSchemaStatistics(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	// Generate some test schemas
	resourceTypes := []ResourceTypeInfo{
		{ResourceType: "Microsoft.Test/type1"},
		{ResourceType: "Microsoft.Test/type2"},
		{ResourceType: "Microsoft.Compute/virtualMachines"}, // Has specific columns
	}

	for _, rt := range resourceTypes {
		generator.GenerateSchema(rt)
	}

	stats := generator.GetSchemaStatistics()

	assert.Equal(t, 3, stats["total_schemas"])
	assert.NotNil(t, stats["column_distribution"])
	assert.NotNil(t, stats["index_distribution"])

	columnDist := stats["column_distribution"].(map[int]int)
	assert.NotEmpty(t, columnDist)

	indexDist := stats["index_distribution"].(map[int]int)
	assert.NotEmpty(t, indexDist)
}

func TestExportImportSchemas(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	// Generate some schemas
	resourceTypes := []ResourceTypeInfo{
		{ResourceType: "Microsoft.Test/type1"},
		{ResourceType: "Microsoft.Test/type2"},
	}

	for _, rt := range resourceTypes {
		generator.GenerateSchema(rt)
	}

	// Export schemas
	exported := generator.ExportSchemas()
	assert.Len(t, exported, 2)

	// Clear cache and import
	generator.ClearCache()
	assert.Equal(t, 0, len(generator.schemaCache))

	generator.ImportSchemas(exported)
	assert.Equal(t, 2, len(generator.schemaCache))

	// Verify imported schemas
	for tableName, schema := range exported {
		cachedSchema := generator.schemaCache[tableName]
		assert.NotNil(t, cachedSchema)
		assert.Equal(t, schema.TableName, cachedSchema.TableName)
		assert.Equal(t, len(schema.Columns), len(cachedSchema.Columns))
	}
}

// Test concurrent access
func TestConcurrentAccess(t *testing.T) {
	generator := NewAzureSchemaGenerator()

	// Create test resource types
	resourceTypes := make([]ResourceTypeInfo, 10)
	for i := 0; i < 10; i++ {
		resourceTypes[i] = ResourceTypeInfo{
			ResourceType: fmt.Sprintf("Microsoft.Test/type%d", i),
		}
	}

	// Generate schemas concurrently
	var wg sync.WaitGroup
	for _, rt := range resourceTypes {
		wg.Add(1)
		go func(resourceType ResourceTypeInfo) {
			defer wg.Done()
			generator.GenerateSchema(resourceType)
		}(rt)
	}

	wg.Wait()

	// Verify all schemas were generated
	assert.Equal(t, 10, len(generator.schemaCache))
}

// Benchmark tests
func BenchmarkGenerateSchema(b *testing.B) {
	generator := NewAzureSchemaGenerator()
	resourceType := ResourceTypeInfo{
		ResourceType: "Microsoft.Compute/virtualMachines",
		APIVersions:  []string{"2023-03-01"},
		Locations:    []string{"East US", "West US"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		generator.GenerateSchema(resourceType)
	}
}

func BenchmarkGenerateTableName(b *testing.B) {
	generator := NewAzureSchemaGenerator()
	resourceType := "Microsoft.Compute/virtualMachines"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		generator.generateTableName(resourceType)
	}
}

func BenchmarkValidateSchema(b *testing.B) {
	generator := NewAzureSchemaGenerator()
	schema := &TableSchema{
		TableName: "test_table",
		Columns: []*ColumnDefinition{
			{Name: "id", Type: "VARCHAR", PrimaryKey: true},
			{Name: "name", Type: "VARCHAR"},
			{Name: "type", Type: "VARCHAR"},
			{Name: "location", Type: "VARCHAR"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		generator.ValidateSchema(schema)
	}
}
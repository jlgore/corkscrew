package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBasicFunctionality(t *testing.T) {
	t.Run("TestNewAzureSchemaGenerator", func(t *testing.T) {
		generator := NewAzureSchemaGenerator()
		assert.NotNil(t, generator)
		assert.NotNil(t, generator.schemaCache)
	})

	t.Run("TestGenerateTableName", func(t *testing.T) {
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
		}

		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				result := generator.generateTableName(tt.input)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("TestSchemaGeneration", func(t *testing.T) {
		generator := NewAzureSchemaGenerator()
		
		resourceType := ResourceTypeInfo{
			ResourceType: "Microsoft.Compute/virtualMachines",
			APIVersions:  []string{"2023-03-01"},
			Locations:    []string{"East US", "West US"},
		}

		schema := generator.GenerateSchema(resourceType)
		
		assert.NotNil(t, schema)
		assert.Equal(t, "azure_compute_virtualmachines", schema.TableName)
		assert.NotEmpty(t, schema.Columns)
		
		// Check for required columns
		hasIDColumn := false
		hasNameColumn := false
		
		for _, col := range schema.Columns {
			if col.Name == "id" {
				hasIDColumn = true
			}
			if col.Name == "name" {
				hasNameColumn = true
			}
		}
		
		assert.True(t, hasIDColumn, "Schema should have ID column")
		assert.True(t, hasNameColumn, "Schema should have name column")
	})
}

func TestValidationHelpers(t *testing.T) {
	t.Run("TestSchemaValidation", func(t *testing.T) {
		generator := NewAzureSchemaGenerator()
		
		validSchema := &TableSchema{
			TableName: "test_table",
			Columns: []*ColumnDefinition{
				{Name: "id", Type: "VARCHAR", PrimaryKey: true},
				{Name: "name", Type: "VARCHAR"},
			},
		}
		
		errors := generator.ValidateSchema(validSchema)
		assert.Empty(t, errors, "Valid schema should have no errors")
	})

	t.Run("TestInvalidSchema", func(t *testing.T) {
		generator := NewAzureSchemaGenerator()
		
		invalidSchema := &TableSchema{
			TableName: "",  // Missing table name
			Columns: []*ColumnDefinition{
				{Name: "name", Type: "VARCHAR"},  // No primary key
			},
		}
		
		errors := generator.ValidateSchema(invalidSchema)
		assert.NotEmpty(t, errors, "Invalid schema should have errors")
	})
}
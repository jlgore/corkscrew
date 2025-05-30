package main

import (
	"fmt"
	"log"
	"reflect"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// TestDynamicSchemaGeneration demonstrates the dynamic schema generation functionality
func TestDynamicSchemaGeneration() {
	fmt.Println("=== Testing Dynamic Schema Generation ===")

	// Create a new dynamic schema generator
	generator := NewDynamicSchemaGenerator()

	// Test data structures
	testCases := []struct {
		name         string
		structType   reflect.Type
		tableName    string
		service      string
		resourceType string
	}{
		{
			name:         "EC2 Instance",
			structType:   reflect.TypeOf(EC2Instance{}),
			tableName:    "aws_ec2_instances",
			service:      "ec2",
			resourceType: "AWS::EC2::Instance",
		},
		{
			name:         "S3 Bucket",
			structType:   reflect.TypeOf(S3Bucket{}),
			tableName:    "aws_s3_buckets",
			service:      "s3",
			resourceType: "AWS::S3::Bucket",
		},
		{
			name:         "Lambda Function",
			structType:   reflect.TypeOf(LambdaFunction{}),
			tableName:    "aws_lambda_functions",
			service:      "lambda",
			resourceType: "AWS::Lambda::Function",
		},
	}

	for i, tc := range testCases {
		fmt.Printf("\n%d. Testing %s\n", i+1, tc.name)
		fmt.Println(strings.Repeat("-", 50))

		// Generate schema
		schema, err := generator.GenerateSchemaFromStruct(
			tc.structType,
			tc.tableName,
			tc.service,
			tc.resourceType,
		)
		if err != nil {
			log.Printf("Error generating schema for %s: %v", tc.name, err)
			continue
		}

		// Display schema information
		fmt.Printf("Table Name: %s\n", schema.Name)
		fmt.Printf("Service: %s\n", schema.Service)
		fmt.Printf("Resource Type: %s\n", schema.ResourceType)
		fmt.Printf("Description: %s\n", schema.Description)

		// Show metadata
		fmt.Println("\nMetadata:")
		for key, value := range schema.Metadata {
			fmt.Printf("  %s: %s\n", key, value)
		}

		// Show SQL (truncated for readability)
		fmt.Println("\nGenerated SQL:")
		sqlLines := strings.Split(schema.Sql, "\n")
		maxLines := 20
		if len(sqlLines) > maxLines {
			for i, line := range sqlLines[:maxLines] {
				fmt.Printf("%2d: %s\n", i+1, line)
			}
			fmt.Printf("... (%d more lines)\n", len(sqlLines)-maxLines)
		} else {
			for i, line := range sqlLines {
				fmt.Printf("%2d: %s\n", i+1, line)
			}
		}

		// Test field analysis
		fmt.Println("\nField Analysis:")
		fields, err := generator.AnalyzeStruct(tc.structType)
		if err != nil {
			log.Printf("Error analyzing fields: %v", err)
			continue
		}

		fmt.Printf("%-25s %-15s %-8s %-8s %-10s\n", "Field Name", "DuckDB Type", "Nullable", "Primary", "Index")
		fmt.Println(strings.Repeat("-", 80))
		for _, field := range fields {
			indexType := field.IndexType
			if indexType == "" {
				indexType = "none"
			}
			fmt.Printf("%-25s %-15s %-8t %-8t %-10s\n",
				field.Name, field.DuckDBType, field.Nullable, field.IsPrimary, indexType)
		}
	}

	// Test caching
	fmt.Println("\n=== Testing Schema Caching ===")
	cached := generator.GetCachedSchemas()
	fmt.Printf("Cached schemas: %d\n", len(cached))
	for name, schema := range cached {
		fmt.Printf("- %s (%s)\n", name, schema.Service)
	}
}

// TestProviderIntegration tests the integration with the AWS provider
func TestProviderIntegration() {
	fmt.Println("\n=== Testing Provider Integration ===")

	// Create a dynamic AWS provider
	provider := NewDynamicAWSProvider()

	// Test resource type mapping
	testResourceMappings := []struct {
		service      string
		resourceType string
		expectedType string
	}{
		{"ec2", "Instance", "EC2Instance"},
		{"s3", "Bucket", "S3Bucket"},
		{"lambda", "Function", "LambdaFunction"},
		{"unknown", "Resource", "nil"},
	}

	fmt.Println("\nResource Type Mappings:")
	fmt.Printf("%-15s %-15s %-20s\n", "Service", "Resource Type", "Go Type")
	fmt.Println(strings.Repeat("-", 50))

	for _, mapping := range testResourceMappings {
		goType := provider.getGoTypeForResource(mapping.service, mapping.resourceType)
		typeName := "nil"
		if goType != nil {
			typeName = goType.Name()
		}
		fmt.Printf("%-15s %-15s %-20s\n", mapping.service, mapping.resourceType, typeName)
	}
}

// TestTypeMappings tests the Go type to DuckDB type mappings
func TestTypeMappings() {
	fmt.Println("\n=== Testing Type Mappings ===")

	generator := NewDynamicSchemaGenerator()

	// Test various Go types
	testTypes := []reflect.Type{
		reflect.TypeOf(""),
		reflect.TypeOf((*string)(nil)).Elem(),
		reflect.TypeOf(int32(0)),
		reflect.TypeOf((*int64)(nil)).Elem(),
		reflect.TypeOf(float64(0)),
		reflect.TypeOf(true),
		reflect.TypeOf((*bool)(nil)).Elem(),
		reflect.TypeOf([]string{}),
		reflect.TypeOf(map[string]string{}),
		reflect.TypeOf(EC2Instance{}),
	}

	fmt.Printf("%-30s %-15s %-10s %-10s\n", "Go Type", "DuckDB Type", "Index", "Index Type")
	fmt.Println(strings.Repeat("-", 70))

	for _, goType := range testTypes {
		duckDBType, shouldIndex, indexType := generator.mapGoTypeToDuckDB(goType, "test_field")
		if indexType == "" {
			indexType = "none"
		}
		fmt.Printf("%-30s %-15s %-10t %-10s\n",
			goType.String(), duckDBType, shouldIndex, indexType)
	}
}

// TestSchemaComparison compares schemas generated by different methods
func TestSchemaComparison() {
	fmt.Println("\n=== Testing Schema Comparison ===")

	dynamicGenerator := NewDynamicSchemaGenerator()
	standardGenerator := NewSchemaGenerator()

	// Generate schema using dynamic generator
	dynamicSchema, err := dynamicGenerator.GenerateSchemaFromStruct(
		reflect.TypeOf(EC2Instance{}),
		"aws_ec2_instances",
		"ec2",
		"AWS::EC2::Instance",
	)
	if err != nil {
		log.Printf("Error generating dynamic schema: %v", err)
		return
	}

	// Generate schema using standard generator
	standardSchemas := standardGenerator.GenerateSchemas([]string{"ec2"})
	var standardSchema *pb.Schema
	for _, schema := range standardSchemas.Schemas {
		if schema.Service == "ec2" && strings.Contains(schema.Name, "ec2_instances") {
			standardSchema = schema
			break
		}
	}

	if standardSchema == nil {
		log.Println("Could not find standard EC2 schema for comparison")
		return
	}

	// Compare schemas
	fmt.Printf("Dynamic Schema SQL length: %d characters\n", len(dynamicSchema.Sql))
	fmt.Printf("Standard Schema SQL length: %d characters\n", len(standardSchema.Sql))

	fmt.Printf("\nDynamic Schema Type: %s\n", dynamicSchema.Metadata["schema_type"])
	fmt.Printf("Standard Schema Type: %s\n", standardSchema.Metadata["table_type"])

	// Count CREATE statements
	dynamicCreates := strings.Count(dynamicSchema.Sql, "CREATE")
	standardCreates := strings.Count(standardSchema.Sql, "CREATE")
	fmt.Printf("\nDynamic Schema CREATE statements: %d\n", dynamicCreates)
	fmt.Printf("Standard Schema CREATE statements: %d\n", standardCreates)

	// Show unique features
	fmt.Println("\nDynamic Schema Features:")
	if strings.Contains(dynamicSchema.Sql, "CREATE OR REPLACE VIEW") {
		fmt.Println("✓ Contains views")
	}
	if strings.Contains(dynamicSchema.Sql, "JSON") {
		fmt.Println("✓ Contains JSON columns")
	}
	if strings.Contains(dynamicSchema.Sql, "TIMESTAMP") {
		fmt.Println("✓ Contains timestamp columns")
	}
}

// RunAllTests runs all the dynamic schema generation tests
func RunAllTests() {
	fmt.Println("Starting Dynamic Schema Generation Test Suite")
	fmt.Println(strings.Repeat("=", 60))

	TestDynamicSchemaGeneration()
	TestProviderIntegration()
	TestTypeMappings()
	TestSchemaComparison()

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("All tests completed!")
}
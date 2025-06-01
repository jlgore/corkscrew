package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Sample GCP protobuf-style struct to test the schema generator
type Instance struct {
	Name         string               `json:"name" protobuf:"bytes,1,opt,name=name"`
	MachineType  string               `json:"machineType" protobuf:"bytes,2,opt,name=machine_type"`
	Zone         string               `json:"zone" protobuf:"bytes,3,opt,name=zone"`
	ProjectId    string               `json:"projectId" protobuf:"bytes,4,opt,name=project_id"`
	NetworkInterfaces []*NetworkInterface `json:"networkInterfaces" protobuf:"bytes,5,rep,name=network_interfaces"`
	Disks        []*AttachedDisk      `json:"disks" protobuf:"bytes,6,rep,name=disks"`
	Labels       map[string]string    `json:"labels" protobuf:"bytes,7,rep,name=labels"`
	Status       string               `json:"status" protobuf:"bytes,8,opt,name=status"`
	CreationTimestamp string          `json:"creationTimestamp" protobuf:"bytes,9,opt,name=creation_timestamp"`
	SelfLink     string               `json:"selfLink" protobuf:"bytes,10,opt,name=self_link"`
}

type NetworkInterface struct {
	Name      string `json:"name" protobuf:"bytes,1,opt,name=name"`
	NetworkIP string `json:"networkIP" protobuf:"bytes,2,opt,name=network_ip"`
}

type AttachedDisk struct {
	Type       string `json:"type" protobuf:"bytes,1,opt,name=type"`
	DiskSizeGb int64  `json:"diskSizeGb" protobuf:"varint,2,opt,name=disk_size_gb"`
}

func TestGCPSchemaGenerator(t *testing.T) {
	// Create a new schema generator
	generator := NewGCPSchemaGenerator()

	// Test generating schema from protobuf type
	instance := &Instance{}
	schema := generator.GenerateSchemaFromProtoType("Instance", instance)

	if schema == nil {
		t.Fatal("Expected schema to be generated, got nil")
	}

	// Verify the schema has the expected properties
	if schema.Name != "gcp_compute_instances" {
		t.Errorf("Expected table name 'gcp_compute_instances', got '%s'", schema.Name)
	}

	if schema.Service != "compute" {
		t.Errorf("Expected service 'compute', got '%s'", schema.Service)
	}

	// Check that SQL contains GCP-specific patterns
	if !containsGCPPatterns(schema.Sql) {
		t.Error("Schema SQL should contain GCP-specific patterns")
	}

	// Test generating schemas for multiple services
	response := generator.GenerateSchemas([]string{"compute", "storage"})
	
	if len(response.Schemas) == 0 {
		t.Error("Expected schemas to be generated")
	}

	// Check that cross-resource views are included
	hasAnalyticsViews := false
	for _, schema := range response.Schemas {
		if schema.Service == "analytics" && schema.ResourceType == "view" {
			hasAnalyticsViews = true
			break
		}
	}

	if !hasAnalyticsViews {
		t.Error("Expected analytics views to be generated")
	}

	// Print the generated schema for manual inspection
	fmt.Printf("Generated Schema:\n%s\n", schema.Sql)
}

func TestProtoTypeAnalyzer(t *testing.T) {
	generator := NewGCPSchemaGenerator()
	analyzer := generator.typeAnalyzer

	// Test type analysis
	instance := &Instance{}
	typeInfo := analyzer.AnalyzeType("Instance", instance)

	if typeInfo == nil {
		t.Fatal("Expected type info to be generated")
	}

	// Verify GCP resource detection
	if !typeInfo.GCPResource.HasProject {
		t.Error("Expected HasProject to be true")
	}

	if !typeInfo.GCPResource.HasZone {
		t.Error("Expected HasZone to be true")
	}

	if !typeInfo.GCPResource.HasLabels {
		t.Error("Expected HasLabels to be true")
	}

	if !typeInfo.GCPResource.Hierarchical {
		t.Error("Expected resource to be hierarchical")
	}

	// Verify field analysis
	if len(typeInfo.Fields) == 0 {
		t.Error("Expected fields to be analyzed")
	}

	// Check for specific fields
	hasNetworkInterfaces := false
	hasDisks := false
	for _, field := range typeInfo.Fields {
		if field.Name == "NetworkInterfaces" && field.IsRepeated {
			hasNetworkInterfaces = true
		}
		if field.Name == "Disks" && field.IsRepeated {
			hasDisks = true
		}
	}

	if !hasNetworkInterfaces {
		t.Error("Expected NetworkInterfaces field to be detected as repeated")
	}

	if !hasDisks {
		t.Error("Expected Disks field to be detected as repeated")
	}
}

func TestCrossResourceViews(t *testing.T) {
	generator := NewGCPSchemaGenerator()

	// Check that cross-resource views are initialized
	if len(generator.crossResourceViews) == 0 {
		t.Error("Expected cross-resource views to be initialized")
	}

	// Check for specific views
	expectedViews := []string{
		"gcp_compute_summary",
		"gcp_project_distribution", 
		"gcp_cost_optimization",
		"gcp_security_analysis",
		"gcp_iam_bindings",
	}

	for _, viewName := range expectedViews {
		if _, exists := generator.crossResourceViews[viewName]; !exists {
			t.Errorf("Expected view '%s' to exist", viewName)
		}
	}
}

func TestSchemaCompliance(t *testing.T) {
	generator := NewGCPSchemaGenerator()
	
	// Generate a schema and verify it meets GCP requirements
	instance := &Instance{}
	schema := generator.GenerateSchemaFromProtoType("Instance", instance)

	// Check metadata
	if schema.Metadata["provider"] != "gcp" {
		t.Error("Expected provider metadata to be 'gcp'")
	}

	if schema.Metadata["asset_type"] == "" {
		t.Error("Expected asset_type metadata to be set")
	}

	// Verify the schema contains required GCP columns
	requiredColumns := []string{
		"project_id",
		"zone", 
		"region",
		"labels",
		"discovered_at",
		"asset_inventory_data",
	}

	for _, column := range requiredColumns {
		if !containsColumn(schema.Sql, column) {
			t.Errorf("Expected schema to contain column '%s'", column)
		}
	}

	// Verify GCP-specific indexes
	requiredIndexes := []string{
		"idx_.*_project_zone",
		"idx_.*_labels_env",
		"idx_.*_labels_app",
	}

	for _, indexPattern := range requiredIndexes {
		if !containsPattern(schema.Sql, indexPattern) {
			t.Errorf("Expected schema to contain index pattern '%s'", indexPattern)
		}
	}
}

// Helper functions for testing
func containsGCPPatterns(sql string) bool {
	patterns := []string{
		"project_id",
		"zone",
		"region",
		"labels",
		"asset_inventory_data",
		"regexp_extract",
	}

	for _, pattern := range patterns {
		if !containsPattern(sql, pattern) {
			return false
		}
	}
	return true
}

func containsColumn(sql, column string) bool {
	return containsPattern(sql, column)
}

func containsPattern(text, pattern string) bool {
	return len(pattern) > 0 && 
		   (containsSubstring(text, pattern) || 
		    matchesRegexPattern(text, pattern))
}

func containsSubstring(text, substring string) bool {
	return len(substring) > 0 && 
		   findInString(text, substring) != -1
}

func findInString(text, substring string) int {
	for i := 0; i <= len(text)-len(substring); i++ {
		if text[i:i+len(substring)] == substring {
			return i
		}
	}
	return -1
}

func matchesRegexPattern(text, pattern string) bool {
	// Simple pattern matching for test purposes
	// Check if the pattern contains wildcards
	if strings.Contains(pattern, ".*") {
		// For patterns like "idx_.*_project_zone", check if the text contains the fixed parts
		parts := strings.Split(pattern, ".*")
		for _, part := range parts {
			if part != "" && !containsSubstring(text, part) {
				return false
			}
		}
		return true
	}
	return containsSubstring(text, pattern)
}

// Example of how to use the schema generator programmatically
func ExampleGCPSchemaGenerator() {
	// Create generator
	generator := NewGCPSchemaGenerator()

	// Define a GCP resource type
	type CloudSQLInstance struct {
		Name         string            `json:"name" protobuf:"bytes,1,opt,name=name"`
		ProjectId    string            `json:"projectId" protobuf:"bytes,2,opt,name=project_id"`
		Region       string            `json:"region" protobuf:"bytes,3,opt,name=region"`
		DatabaseVersion string         `json:"databaseVersion" protobuf:"bytes,4,opt,name=database_version"`
		Settings     map[string]string `json:"settings" protobuf:"bytes,5,rep,name=settings"`
		Labels       map[string]string `json:"labels" protobuf:"bytes,6,rep,name=labels"`
	}

	// Generate schema
	sqlInstance := &CloudSQLInstance{}
	schema := generator.GenerateSchemaFromProtoType("CloudSQLInstance", sqlInstance)

	// Convert to JSON for output
	schemaJSON, _ := json.MarshalIndent(schema, "", "  ")
	fmt.Printf("Generated Schema: %s\n", string(schemaJSON))

	// Generate all schemas
	response := generator.GenerateSchemas([]string{})
	fmt.Printf("Total schemas generated: %d\n", len(response.Schemas))
}
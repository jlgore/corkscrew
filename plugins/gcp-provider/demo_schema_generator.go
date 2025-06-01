package main

// This demo file is disabled to avoid conflicts with main.go
// To run this demo, create a separate cmd/demo/ directory

/*

import (
	"fmt"
	"log"
	"strings"
)

// Demo script showing how to use the GCP Schema Generator

// Example GCP protobuf types
type ComputeInstance struct {
	Name               string                 `json:"name" protobuf:"bytes,1,opt,name=name"`
	MachineType        string                 `json:"machineType" protobuf:"bytes,2,opt,name=machine_type"`
	Zone               string                 `json:"zone" protobuf:"bytes,3,opt,name=zone"`
	ProjectId          string                 `json:"projectId" protobuf:"bytes,4,opt,name=project_id"`
	NetworkInterfaces  []*NetworkInterface    `json:"networkInterfaces" protobuf:"bytes,5,rep,name=network_interfaces"`
	Disks              []*AttachedDisk        `json:"disks" protobuf:"bytes,6,rep,name=disks"`
	Labels             map[string]string      `json:"labels" protobuf:"bytes,7,rep,name=labels"`
	Status             string                 `json:"status" protobuf:"bytes,8,opt,name=status"`
	CreationTimestamp  string                 `json:"creationTimestamp" protobuf:"bytes,9,opt,name=creation_timestamp"`
	SelfLink           string                 `json:"selfLink" protobuf:"bytes,10,opt,name=self_link"`
	ServiceAccounts    []*ServiceAccount      `json:"serviceAccounts" protobuf:"bytes,11,rep,name=service_accounts"`
}

type StorageBucket struct {
	Name               string            `json:"name" protobuf:"bytes,1,opt,name=name"`
	ProjectId          string            `json:"projectId" protobuf:"bytes,2,opt,name=project_id"`
	Location           string            `json:"location" protobuf:"bytes,3,opt,name=location"`
	StorageClass       string            `json:"storageClass" protobuf:"bytes,4,opt,name=storage_class"`
	Labels             map[string]string `json:"labels" protobuf:"bytes,5,rep,name=labels"`
	CreationTimestamp  string            `json:"timeCreated" protobuf:"bytes,6,opt,name=time_created"`
	SelfLink           string            `json:"selfLink" protobuf:"bytes,7,opt,name=self_link"`
	Versioning         *BucketVersioning `json:"versioning" protobuf:"bytes,8,opt,name=versioning"`
}

type BigQueryDataset struct {
	DatasetId          string            `json:"datasetId" protobuf:"bytes,1,opt,name=dataset_id"`
	ProjectId          string            `json:"projectId" protobuf:"bytes,2,opt,name=project_id"`
	Location           string            `json:"location" protobuf:"bytes,3,opt,name=location"`
	Labels             map[string]string `json:"labels" protobuf:"bytes,4,rep,name=labels"`
	CreationTime       int64             `json:"creationTime" protobuf:"varint,5,opt,name=creation_time"`
	LastModifiedTime   int64             `json:"lastModifiedTime" protobuf:"varint,6,opt,name=last_modified_time"`
	SelfLink           string            `json:"selfLink" protobuf:"bytes,7,opt,name=self_link"`
}

type ServiceAccount struct {
	Email  string   `json:"email" protobuf:"bytes,1,opt,name=email"`
	Scopes []string `json:"scopes" protobuf:"bytes,2,rep,name=scopes"`
}

type BucketVersioning struct {
	Enabled bool `json:"enabled" protobuf:"varint,1,opt,name=enabled"`
}

func main() {
	fmt.Println("=== GCP Schema Generator Demo ===\n")

	// Create the schema generator
	generator := NewGCPSchemaGenerator()

	// Demo 1: Generate schema from protobuf types
	fmt.Println("1. Generating schemas from protobuf types:\n")
	
	// Compute Instance
	instance := &ComputeInstance{}
	instanceSchema := generator.GenerateSchemaFromProtoType("Instance", instance)
	if instanceSchema != nil {
		fmt.Printf("Generated schema for Compute Instance:\n")
		fmt.Printf("Table: %s\n", instanceSchema.Name)
		fmt.Printf("Service: %s\n", instanceSchema.Service)
		fmt.Printf("Asset Type: %s\n\n", instanceSchema.Metadata["asset_type"])
		fmt.Printf("SQL:\n%s\n", instanceSchema.Sql)
		fmt.Println(strings.Repeat("-", 80))
	}

	// Storage Bucket
	bucket := &StorageBucket{}
	bucketSchema := generator.GenerateSchemaFromProtoType("Bucket", bucket)
	if bucketSchema != nil {
		fmt.Printf("\nGenerated schema for Storage Bucket:\n")
		fmt.Printf("Table: %s\n", bucketSchema.Name)
		fmt.Printf("Service: %s\n", bucketSchema.Service)
		fmt.Printf("SQL:\n%s\n", bucketSchema.Sql)
		fmt.Println(strings.Repeat("-", 80))
	}

	// BigQuery Dataset
	dataset := &BigQueryDataset{}
	datasetSchema := generator.GenerateSchemaFromProtoType("Dataset", dataset)
	if datasetSchema != nil {
		fmt.Printf("\nGenerated schema for BigQuery Dataset:\n")
		fmt.Printf("Table: %s\n", datasetSchema.Name)
		fmt.Printf("Service: %s\n", datasetSchema.Service)
		fmt.Printf("SQL:\n%s\n", datasetSchema.Sql)
		fmt.Println(strings.Repeat("-", 80))
	}

	// Demo 2: Generate all predefined schemas
	fmt.Println("\n2. Generating all predefined schemas:\n")
	
	allSchemas := generator.GenerateSchemas([]string{})
	fmt.Printf("Total schemas generated: %d\n\n", len(allSchemas.Schemas))

	schemasByService := make(map[string]int)
	viewCount := 0
	
	for _, schema := range allSchemas.Schemas {
		if schema.ResourceType == "view" {
			viewCount++
		} else {
			schemasByService[schema.Service]++
		}
	}

	fmt.Println("Schemas by service:")
	for service, count := range schemasByService {
		fmt.Printf("  %s: %d tables\n", service, count)
	}
	fmt.Printf("  analytics: %d views\n", viewCount)

	// Demo 3: Show cross-resource views
	fmt.Println("\n3. Cross-resource analytics views:\n")
	
	for _, schema := range allSchemas.Schemas {
		if schema.ResourceType == "view" {
			fmt.Printf("View: %s\n", schema.Name)
			fmt.Printf("Description: %s\n\n", schema.Description)
		}
	}

	// Demo 4: Show GCP-specific patterns
	fmt.Println("4. GCP-specific schema patterns demonstrated:\n")
	fmt.Println("✓ Project-based organization (project_id column)")
	fmt.Println("✓ Zonal vs regional resources (zone/region columns)")
	fmt.Println("✓ Generated region from zone (GENERATED ALWAYS AS)")
	fmt.Println("✓ Label-based organization (labels JSON column)")
	fmt.Println("✓ GCP-specific indexes (project_zone, labels)")
	fmt.Println("✓ Asset inventory integration (asset_inventory_data)")
	fmt.Println("✓ Resource hierarchies (hierarchical metadata)")
	fmt.Println("✓ IAM integration support (iam_enabled metadata)")
	fmt.Println("✓ Cross-resource analytics views")
	fmt.Println("✓ Security analysis capabilities")
	fmt.Println("✓ Cost optimization insights")

	fmt.Println("\n=== Demo Complete ===")
}

// Helper function to avoid unused import warning
func init() {
	_ = log.Printf // Prevent unused import warning
}

*/
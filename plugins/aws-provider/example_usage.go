package main

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// Example AWS SDK structures for demonstration

// EC2Instance represents an EC2 instance from AWS SDK
type EC2Instance struct {
	InstanceId   *string    `json:"instanceId"`
	InstanceType *string    `json:"instanceType"`
	LaunchTime   *time.Time `json:"launchTime"`
	State        *InstanceState `json:"state"`
	Tags         []Tag      `json:"tags"`
	VpcId        *string    `json:"vpcId"`
	SubnetId     *string    `json:"subnetId"`
	PrivateIpAddress *string `json:"privateIpAddress"`
	PublicIpAddress  *string `json:"publicIpAddress"`
	SecurityGroups   []SecurityGroup `json:"securityGroups"`
	KeyName         *string    `json:"keyName"`
	Platform        *string    `json:"platform"`
	Architecture    *string    `json:"architecture"`
}

// InstanceState represents the state of an EC2 instance
type InstanceState struct {
	Code *int32  `json:"code"`
	Name *string `json:"name"`
}

// Tag represents a resource tag
type Tag struct {
	Key   *string `json:"key"`
	Value *string `json:"value"`
}

// SecurityGroup represents a security group reference
type SecurityGroup struct {
	GroupId   *string `json:"groupId"`
	GroupName *string `json:"groupName"`
}

// S3Bucket represents an S3 bucket from AWS SDK
type S3Bucket struct {
	Name                *string    `json:"name" db:"primary_key"`
	CreationDate        *time.Time `json:"creationDate"`
	Region              *string    `json:"region"`
	VersioningStatus    *string    `json:"versioningStatus"`
	EncryptionEnabled   *bool      `json:"encryptionEnabled"`
	PublicAccessBlocked *bool      `json:"publicAccessBlocked"`
	LifecycleConfiguration interface{} `json:"lifecycleConfiguration"`
	Tags                []Tag      `json:"tags"`
	Owner               *BucketOwner `json:"owner"`
	Policy              interface{}  `json:"policy"`
}

// BucketOwner represents the owner of an S3 bucket
type BucketOwner struct {
	DisplayName *string `json:"displayName"`
	ID          *string `json:"id"`
}

// LambdaFunction represents a Lambda function from AWS SDK
type LambdaFunction struct {
	FunctionName    *string                `json:"functionName" db:"primary_key"`
	FunctionArn     *string                `json:"functionArn"`
	Runtime         *string                `json:"runtime"`
	Role            *string                `json:"role"`
	Handler         *string                `json:"handler"`
	CodeSize        *int64                 `json:"codeSize"`
	Description     *string                `json:"description"`
	Timeout         *int32                 `json:"timeout"`
	MemorySize      *int32                 `json:"memorySize"`
	LastModified    *string                `json:"lastModified"`
	CodeSha256      *string                `json:"codeSha256"`
	Version         *string                `json:"version"`
	VpcConfig       *VpcConfig             `json:"vpcConfig"`
	Environment     map[string]interface{} `json:"environment"`
	DeadLetterConfig *DeadLetterConfig     `json:"deadLetterConfig"`
	KMSKeyArn       *string                `json:"kmsKeyArn"`
	TracingConfig   *TracingConfig         `json:"tracingConfig"`
	Tags            map[string]*string     `json:"tags"`
}

// VpcConfig represents VPC configuration for Lambda
type VpcConfig struct {
	SubnetIds        []*string `json:"subnetIds"`
	SecurityGroupIds []*string `json:"securityGroupIds"`
	VpcId            *string   `json:"vpcId"`
}

// DeadLetterConfig represents dead letter queue configuration
type DeadLetterConfig struct {
	TargetArn *string `json:"targetArn"`
}

// TracingConfig represents X-Ray tracing configuration
type TracingConfig struct {
	Mode *string `json:"mode"`
}

// ExampleUsage demonstrates how to use the DynamicSchemaGenerator
func ExampleUsage() {
	fmt.Println("=== Dynamic Schema Generator Example ===")
	
	// Create a new schema generator
	generator := NewDynamicSchemaGenerator()
	
	// Example 1: Generate schema for EC2 Instance
	fmt.Println("\n1. Generating schema for EC2 Instance:")
	ec2Schema, err := generator.GenerateSchemaFromStruct(
		reflect.TypeOf(EC2Instance{}),
		"aws_ec2_instances",
		"ec2",
		"AWS::EC2::Instance",
	)
	if err != nil {
		log.Printf("Error generating EC2 schema: %v", err)
		return
	}
	
	fmt.Printf("Table: %s\n", ec2Schema.Name)
	fmt.Printf("Service: %s\n", ec2Schema.Service)
	fmt.Printf("Resource Type: %s\n", ec2Schema.ResourceType)
	fmt.Printf("SQL:\n%s\n", ec2Schema.Sql)
	
	// Example 2: Generate schema for S3 Bucket
	fmt.Println("\n2. Generating schema for S3 Bucket:")
	s3Schema, err := generator.GenerateSchemaFromStruct(
		reflect.TypeOf(S3Bucket{}),
		"aws_s3_buckets",
		"s3",
		"AWS::S3::Bucket",
	)
	if err != nil {
		log.Printf("Error generating S3 schema: %v", err)
		return
	}
	
	fmt.Printf("Table: %s\n", s3Schema.Name)
	fmt.Printf("SQL:\n%s\n", s3Schema.Sql)
	
	// Example 3: Generate schema for Lambda Function
	fmt.Println("\n3. Generating schema for Lambda Function:")
	lambdaSchema, err := generator.GenerateSchemaFromStruct(
		reflect.TypeOf(LambdaFunction{}),
		"aws_lambda_functions",
		"lambda",
		"AWS::Lambda::Function",
	)
	if err != nil {
		log.Printf("Error generating Lambda schema: %v", err)
		return
	}
	
	fmt.Printf("Table: %s\n", lambdaSchema.Name)
	fmt.Printf("SQL:\n%s\n", lambdaSchema.Sql)
	
	// Example 4: Demonstrate field analysis
	fmt.Println("\n4. Field Analysis for EC2 Instance:")
	fields, err := generator.AnalyzeStruct(reflect.TypeOf(EC2Instance{}))
	if err != nil {
		log.Printf("Error analyzing struct: %v", err)
		return
	}
	
	fmt.Printf("%-20s %-15s %-10s %-10s %-15s\n", "Field", "DuckDB Type", "Nullable", "Primary", "Index Type")
	fmt.Println(strings.Repeat("-", 80))
	for _, field := range fields {
		fmt.Printf("%-20s %-15s %-10t %-10t %-15s\n", 
			field.Name, field.DuckDBType, field.Nullable, field.IsPrimary, field.IndexType)
	}
	
	// Example 5: Generate migration script
	fmt.Println("\n5. Migration Script Example:")
	migrationScript := generator.GenerateMigrationScript(ec2Schema, ec2Schema)
	fmt.Println(migrationScript)
	
	// Example 6: Show cached schemas
	fmt.Println("\n6. Cached Schemas:")
	cached := generator.GetCachedSchemas()
	for name, schema := range cached {
		fmt.Printf("- %s (Service: %s, Type: %s)\n", name, schema.Service, schema.ResourceType)
	}
}

// BatchSchemaGeneration demonstrates generating schemas for multiple resource types
func BatchSchemaGeneration() []*pb.Schema {
	generator := NewDynamicSchemaGenerator()
	
	// Define resource types to generate schemas for
	resourceTypes := []struct {
		Type         reflect.Type
		TableName    string
		Service      string
		ResourceType string
	}{
		{reflect.TypeOf(EC2Instance{}), "aws_ec2_instances", "ec2", "AWS::EC2::Instance"},
		{reflect.TypeOf(S3Bucket{}), "aws_s3_buckets", "s3", "AWS::S3::Bucket"},
		{reflect.TypeOf(LambdaFunction{}), "aws_lambda_functions", "lambda", "AWS::Lambda::Function"},
	}
	
	var schemas []*pb.Schema
	
	for _, rt := range resourceTypes {
		schema, err := generator.GenerateSchemaFromStruct(rt.Type, rt.TableName, rt.Service, rt.ResourceType)
		if err != nil {
			log.Printf("Error generating schema for %s: %v", rt.ResourceType, err)
			continue
		}
		schemas = append(schemas, schema)
	}
	
	return schemas
}

// ValidateGeneratedSchema demonstrates how to validate generated schemas
func ValidateGeneratedSchema() {
	generator := NewDynamicSchemaGenerator()
	
	// Generate a schema
	schema, err := generator.GenerateSchemaFromStruct(
		reflect.TypeOf(EC2Instance{}),
		"aws_ec2_instances",
		"ec2",
		"AWS::EC2::Instance",
	)
	if err != nil {
		log.Printf("Error generating schema: %v", err)
		return
	}
	
	// Basic validation
	fmt.Printf("Schema Validation for %s:\n", schema.Name)
	fmt.Printf("✓ Table name: %s\n", schema.Name)
	fmt.Printf("✓ Service: %s\n", schema.Service)
	fmt.Printf("✓ Resource type: %s\n", schema.ResourceType)
	fmt.Printf("✓ SQL length: %d characters\n", len(schema.Sql))
	fmt.Printf("✓ Metadata fields: %d\n", len(schema.Metadata))
	
	// Check for required elements in SQL
	requiredElements := []string{
		"CREATE TABLE",
		"PRIMARY KEY",
		"discovered_at",
		"raw_data JSON",
		"CREATE INDEX",
	}
	
	for _, element := range requiredElements {
		if strings.Contains(schema.Sql, element) {
			fmt.Printf("✓ Contains: %s\n", element)
		} else {
			fmt.Printf("✗ Missing: %s\n", element)
		}
	}
}

// Example main function to run demonstrations
func runExamples() {
	fmt.Println("Starting Dynamic Schema Generator Examples...")
	
	// Run basic example
	ExampleUsage()
	
	// Run batch generation
	fmt.Println("\n=== Batch Schema Generation ===")
	schemas := BatchSchemaGeneration()
	fmt.Printf("Generated %d schemas successfully\n", len(schemas))
	
	// Run validation
	fmt.Println("\n=== Schema Validation ===")
	ValidateGeneratedSchema()
	
	fmt.Println("\nAll examples completed!")
}
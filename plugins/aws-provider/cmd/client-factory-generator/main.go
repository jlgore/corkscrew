package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
	"golang.org/x/time/rate"
)

// ServicesJSON represents the structure of the services.json file
type ServicesJSON struct {
	Version    string    `json:"version"`
	AnalyzedAt time.Time `json:"analyzed_at"`
	SDKVersion string    `json:"sdk_version"`
	Services   []Service `json:"services"`
}

// Service represents a service definition in the services.json file
type Service struct {
	Name        string      `json:"name"`
	PackagePath string      `json:"package_path"`
	ClientType  string      `json:"client_type"`
	Operations  []Operation `json:"operations"`
	Resources   []Resource  `json:"resource_types,omitempty"`
}

// Operation represents an operation definition
type Operation struct {
	Name         string `json:"name"`
	InputType    string `json:"input_type"`
	OutputType   string `json:"output_type"`
	ResourceType string `json:"resource_type"`
	IsList       bool   `json:"is_list"`
	IsPaginated  bool   `json:"is_paginated"`
}

// Resource represents a resource type definition
type Resource struct {
	Name        string            `json:"name"`
	ResourceType string           `json:"resource_type"`
	Fields      map[string]string `json:"fields,omitempty"`
	PrimaryKey  string            `json:"primary_key,omitempty"`
}

func main() {
	var (
		servicesPath = flag.String("services", "generated/services.json", "Path to services.json file")
		outputPath   = flag.String("output", "generated/client_factory.go", "Output path for generated factory")
		packageName  = flag.String("package", "main", "Package name for generated code")
		buildTag     = flag.String("build-tag", "aws_services", "Build tag for conditional compilation")
		verbose      = flag.Bool("verbose", false, "Enable verbose output")
	)
	flag.Parse()

	if *verbose {
		log.Printf("Loading services from: %s", *servicesPath)
		log.Printf("Output path: %s", *outputPath)
		log.Printf("Package name: %s", *packageName)
		log.Printf("Build tag: %s", *buildTag)
	}

	// Load services.json
	servicesData, err := loadServicesJSON(*servicesPath)
	if err != nil {
		log.Fatalf("Failed to load services.json: %v", err)
	}

	if *verbose {
		log.Printf("Loaded %d services from %s", len(servicesData.Services), *servicesPath)
	}

	// Convert to service registry
	config := registry.RegistryConfig{
		EnableValidation: true,
		EnableCache:      false,
	}
	
	// Create a minimal AWS config for the registry
	awsCfg := aws.Config{
		Region: "us-east-1", // Default region for registry
	}
	
	reg := registry.NewUnifiedServiceRegistry(awsCfg, config)

	// Convert and register services
	for _, svc := range servicesData.Services {
		def := convertToServiceDefinition(svc)
		if err := reg.RegisterService(def); err != nil {
			log.Printf("Warning: Failed to register service %s: %v", svc.Name, err)
			continue
		}
		if *verbose {
			log.Printf("Registered service: %s (%s)", def.Name, def.PackagePath)
		}
	}

	// Create generator
	gen := generator.NewClientFactoryGenerator(reg, *outputPath)
	
	// Configure generator
	if *packageName != "main" {
		// Note: Need to check if ClientFactoryGenerator supports custom package names
		// For now, this will use the default "main" package
		if *verbose {
			log.Printf("Note: Using default package name 'main' (custom package names may need generator modification)")
		}
	}

	// Generate client factory
	if *verbose {
		log.Printf("Generating client factory...")
	}
	
	if err := gen.GenerateClientFactory(); err != nil {
		log.Fatalf("Failed to generate client factory: %v", err)
	}

	if *verbose {
		log.Printf("Client factory generation completed successfully!")
		log.Printf("Generated file: %s", *outputPath)
	}

	fmt.Printf("Client factory generated successfully: %s\n", *outputPath)
}

// loadServicesJSON loads and parses the services.json file
func loadServicesJSON(path string) (*ServicesJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read services file: %w", err)
	}

	var services ServicesJSON
	if err := json.Unmarshal(data, &services); err != nil {
		return nil, fmt.Errorf("failed to parse services JSON: %w", err)
	}

	return &services, nil
}

// convertToServiceDefinition converts a Service from services.json to registry.ServiceDefinition
func convertToServiceDefinition(svc Service) registry.ServiceDefinition {
	def := registry.ServiceDefinition{
		Name:        strings.ToLower(svc.Name),
		DisplayName: formatServiceDisplayName(svc.Name),
		Description: fmt.Sprintf("AWS %s service", strings.ToUpper(svc.Name)),
		PackagePath: svc.PackagePath,
		ClientType:  fmt.Sprintf("*%s.%s", extractPackageName(svc.PackagePath), svc.ClientType),
		
		// Set reasonable defaults for rate limiting
		RateLimit:  rate.Limit(20), // 20 requests per second
		BurstLimit: 40,             // Burst up to 40 requests
		
		// Service characteristics
		RequiresRegion:            !isGlobalService(svc.Name),
		GlobalService:             isGlobalService(svc.Name),
		SupportsPagination:        hasPaginatedOperations(svc.Operations),
		SupportsResourceExplorer:  true, // Most services support resource discovery
		SupportsParallelScan:      true, // Enable parallel scanning by default
		
		// Discovery metadata
		DiscoveredAt:     time.Now(),
		DiscoverySource:  "analyzer",
		DiscoveryVersion: "v2-latest",
		LastValidated:    time.Now(),
		ValidationStatus: "valid",
	}

	// Convert operations
	for _, op := range svc.Operations {
		opDef := registry.OperationDefinition{
			Name:          op.Name,
			DisplayName:   formatOperationDisplayName(op.Name),
			Description:   fmt.Sprintf("%s operation for %s", op.Name, svc.Name),
			OperationType: determineOperationType(op.Name),
			InputType:     op.InputType,
			OutputType:    op.OutputType,
			Paginated:     op.IsPaginated,
			
			// Set operation characteristics based on operation name
			RequiresResourceID: !op.IsList,
			IsGlobalOperation:  isGlobalService(svc.Name),
			IsMutating:        isMutatingOperation(op.Name),
			IsIdempotent:      !isMutatingOperation(op.Name),
			SupportsFiltering: op.IsList,
		}
		
		// Set required permissions based on operation
		opDef.RequiredPermissions = generatePermissions(svc.Name, op.Name)
		
		def.Operations = append(def.Operations, opDef)
	}

	// Convert resources
	for _, res := range svc.Resources {
		resDef := registry.ResourceTypeDefinition{
			Name:         res.Name,
			DisplayName:  res.Name,
			ResourceType: res.ResourceType,
			
			// Find associated operations
			ListOperation:       findListOperation(svc.Operations, res.ResourceType),
			DescribeOperation:   findDescribeOperation(svc.Operations, res.ResourceType),
			SupportedOperations: findAllOperations(svc.Operations, res.ResourceType),
			
			// Set resource characteristics
			IsGlobalResource: isGlobalService(svc.Name),
			SupportsTags:     true, // Most AWS resources support tags
			Paginated:        hasResourcePagination(svc.Operations, res.ResourceType),
			
			// Set identification fields
			IDField:          res.PrimaryKey,
			IdentifierFields: []string{res.PrimaryKey},
			FieldMappings:    res.Fields,
		}
		
		// Set required permissions
		resDef.RequiredPermissions = generateResourcePermissions(svc.Name, res.Name)
		
		def.ResourceTypes = append(def.ResourceTypes, resDef)
	}

	// Set service-level permissions
	def.Permissions = generateServicePermissions(svc.Name, def.Operations)

	return def
}

// Helper functions

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func extractPackageName(packagePath string) string {
	parts := strings.Split(packagePath, "/")
	return parts[len(parts)-1]
}

func formatServiceDisplayName(name string) string {
	switch strings.ToLower(name) {
	case "s3":
		return "Amazon S3"
	case "ec2":
		return "Amazon EC2"
	case "rds":
		return "Amazon RDS"
	case "iam":
		return "AWS IAM"
	case "kms":
		return "AWS KMS"
	case "lambda":
		return "AWS Lambda"
	case "dynamodb":
		return "Amazon DynamoDB"
	default:
		return fmt.Sprintf("AWS %s", strings.ToUpper(name))
	}
}

func formatOperationDisplayName(name string) string {
	// Convert "DescribeInstances" to "Describe Instances"
	result := ""
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result += " "
		}
		result += string(r)
	}
	return result
}

func determineOperationType(operationName string) string {
	name := strings.ToLower(operationName)
	switch {
	case strings.HasPrefix(name, "list"):
		return "List"
	case strings.HasPrefix(name, "describe"):
		return "Describe"
	case strings.HasPrefix(name, "get"):
		return "Get"
	case strings.HasPrefix(name, "create"):
		return "Create"
	case strings.HasPrefix(name, "update"), strings.HasPrefix(name, "modify"):
		return "Update"
	case strings.HasPrefix(name, "delete"), strings.HasPrefix(name, "remove"):
		return "Delete"
	case strings.HasPrefix(name, "put"):
		return "Put"
	default:
		return "Action"
	}
}

func isGlobalService(serviceName string) bool {
	globalServices := map[string]bool{
		"iam":         true,
		"s3":          true, // S3 buckets are global namespace but regionally stored
		"cloudfront":  true,
		"route53":     true,
		"waf":         true,
		"sts":         true,
	}
	return globalServices[strings.ToLower(serviceName)]
}

func isMutatingOperation(operationName string) bool {
	name := strings.ToLower(operationName)
	mutatingPrefixes := []string{"create", "update", "delete", "put", "modify", "remove", "attach", "detach"}
	for _, prefix := range mutatingPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func hasPaginatedOperations(operations []Operation) bool {
	for _, op := range operations {
		if op.IsPaginated {
			return true
		}
	}
	return false
}

func hasResourcePagination(operations []Operation, resourceType string) bool {
	for _, op := range operations {
		if op.ResourceType == resourceType && op.IsPaginated {
			return true
		}
	}
	return false
}

func findListOperation(operations []Operation, resourceType string) string {
	for _, op := range operations {
		if op.ResourceType == resourceType && op.IsList && strings.HasPrefix(strings.ToLower(op.Name), "list") {
			return op.Name
		}
	}
	for _, op := range operations {
		if op.ResourceType == resourceType && op.IsList && strings.HasPrefix(strings.ToLower(op.Name), "describe") {
			return op.Name
		}
	}
	return ""
}

func findDescribeOperation(operations []Operation, resourceType string) string {
	for _, op := range operations {
		if op.ResourceType == resourceType && strings.HasPrefix(strings.ToLower(op.Name), "describe") {
			return op.Name
		}
	}
	return ""
}

func findAllOperations(operations []Operation, resourceType string) []string {
	var result []string
	for _, op := range operations {
		if op.ResourceType == resourceType {
			result = append(result, op.Name)
		}
	}
	return result
}

func generatePermissions(serviceName, operationName string) []string {
	// Generate IAM permission strings based on service and operation
	service := strings.ToLower(serviceName)
	action := strings.ToLower(operationName)
	
	// Convert operation name to IAM action
	iamAction := operationName
	if strings.HasPrefix(action, "describe") {
		iamAction = strings.Replace(operationName, "Describe", "Describe", 1)
	} else if strings.HasPrefix(action, "list") {
		iamAction = strings.Replace(operationName, "List", "List", 1)
	}
	
	permission := fmt.Sprintf("%s:%s", service, iamAction)
	return []string{permission}
}

func generateResourcePermissions(serviceName, resourceName string) []string {
	service := strings.ToLower(serviceName)
	
	// Common permissions for most resources
	permissions := []string{
		fmt.Sprintf("%s:List*", service),
		fmt.Sprintf("%s:Describe*", service),
		fmt.Sprintf("%s:Get*", service),
	}
	
	return permissions
}

func generateServicePermissions(serviceName string, operations []registry.OperationDefinition) []string {
	permissionMap := make(map[string]bool)
	
	// Add permissions from all operations
	for _, op := range operations {
		for _, perm := range op.RequiredPermissions {
			permissionMap[perm] = true
		}
	}
	
	// Convert to slice
	var permissions []string
	for perm := range permissionMap {
		permissions = append(permissions, perm)
	}
	
	return permissions
}
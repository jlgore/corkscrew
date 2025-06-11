package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	
	"github.com/jlgore/corkscrew/internal/config"
)

// ServiceInfo represents discovered AWS service information
type ServiceInfo struct {
	Name          string            `json:"name"`
	PackagePath   string            `json:"package_path"`
	ClientType    string            `json:"client_type"`
	Operations    []OperationInfo   `json:"operations"`
	ResourceTypes []ResourceTypeInfo `json:"resource_types"`
	LastUpdated   time.Time         `json:"last_updated"`
}

// OperationInfo represents an AWS service operation
type OperationInfo struct {
	Name         string   `json:"name"`
	InputType    string   `json:"input_type"`
	OutputType   string   `json:"output_type"`
	ResourceType string   `json:"resource_type,omitempty"`
	IsList       bool     `json:"is_list"`
	IsPaginated  bool     `json:"is_paginated"`
	RequiredParams []string `json:"required_params,omitempty"`
}

// ResourceTypeInfo represents a discovered resource type
type ResourceTypeInfo struct {
	Name       string            `json:"name"`
	GoType     string            `json:"go_type"`
	PrimaryKey string            `json:"primary_key,omitempty"`
	Fields     map[string]string `json:"fields"`
}

// AnalysisResult contains all discovered services
type AnalysisResult struct {
	Version      string        `json:"version"`
	AnalyzedAt   time.Time     `json:"analyzed_at"`
	SDKVersion   string        `json:"sdk_version"`
	Services     []ServiceInfo `json:"services"`
	TotalOps     int           `json:"total_operations"`
	TotalResources int         `json:"total_resources"`
}

var (
	outputPath   = flag.String("output", "generated/services.json", "Output path for analysis results")
	sdkPath      = flag.String("sdk-path", "", "Path to AWS SDK")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	services     = flag.String("services", "", "Comma-separated list of services (overrides config)")
	configFile   = flag.String("config", "", "Path to configuration file")
	serviceGroup = flag.String("service-group", "", "Use services from named group")
	listServices = flag.Bool("list-services", false, "List configured services and exit")
)

func main() {
	flag.Parse()
	
	// Load configuration
	serviceConfig, err := loadConfiguration()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	
	// Get services to analyze
	servicesToAnalyze, err := getServicesToAnalyze(serviceConfig)
	if err != nil {
		log.Fatalf("Failed to determine services: %v", err)
	}
	
	// Handle list-services flag
	if *listServices {
		fmt.Println("Services to analyze:")
		for _, svc := range servicesToAnalyze {
			fmt.Printf("  - %s\n", svc)
		}
		fmt.Printf("\nTotal: %d services\n", len(servicesToAnalyze))
		return
	}
	
	if *verbose {
		log.Printf("Analyzing %d services: %v", len(servicesToAnalyze), servicesToAnalyze)
	}

	result := &AnalysisResult{
		Version:    "1.0.0",
		AnalyzedAt: time.Now(),
		SDKVersion: "v2-latest",
		Services:   []ServiceInfo{},
	}
	
	// Analyze services
	analysisConfig := serviceConfig.Providers["aws"].Analysis
	if analysisConfig.Workers > 1 {
		// TODO: Implement parallel analysis
		log.Printf("Parallel analysis with %d workers not yet implemented, using sequential", analysisConfig.Workers)
	}

	for _, serviceName := range servicesToAnalyze {
		if *verbose {
			log.Printf("Analyzing service: %s", serviceName)
		}

		// Find the service module path
		servicePath := findServiceModulePath(serviceName)
		if servicePath == "" {
			if *verbose {
				log.Printf("Service module not found: %s", serviceName)
			}
			continue
		}

		serviceInfo, err := analyzeService(servicePath, serviceName)
		if err != nil {
			log.Printf("Failed to analyze service %s: %v", serviceName, err)
			continue
		}

		if serviceInfo != nil {
			// Skip empty services if configured
			if analysisConfig.SkipEmpty && 
			   len(serviceInfo.Operations) == 0 && 
			   len(serviceInfo.ResourceTypes) == 0 {
				if *verbose {
					log.Printf("Skipping empty service: %s", serviceName)
				}
				continue
			}
			
			result.Services = append(result.Services, *serviceInfo)
			result.TotalOps += len(serviceInfo.Operations)
			result.TotalResources += len(serviceInfo.ResourceTypes)
		}
	}

	// Save results
	if err := saveResults(result, *outputPath); err != nil {
		log.Fatalf("Failed to save results: %v", err)
	}

	fmt.Printf("Analysis complete. Found %d services with %d operations and %d resource types\n",
		len(result.Services), result.TotalOps, result.TotalResources)
}

func loadConfiguration() (*config.ServiceConfig, error) {
	// Set config file path if provided
	if *configFile != "" {
		os.Setenv("CORKSCREW_CONFIG_FILE", *configFile)
	}
	
	return config.LoadServiceConfig()
}

func getServicesToAnalyze(cfg *config.ServiceConfig) ([]string, error) {
	// Command-line override takes precedence
	if *services != "" {
		serviceList := strings.Split(*services, ",")
		for i, svc := range serviceList {
			serviceList[i] = strings.TrimSpace(svc)
		}
		return serviceList, nil
	}
	
	// Service group specified
	if *serviceGroup != "" {
		return cfg.GetServiceGroup("aws", *serviceGroup)
	}
	
	// Use configuration
	return cfg.GetServicesForProvider("aws")
}

func findServiceModulePath(serviceName string) string {
	// Try to find service module in go module cache
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}

	// Look for AWS SDK v2 service modules
	serviceBasePath := filepath.Join(gopath, "pkg", "mod", "github.com", "aws", "aws-sdk-go-v2", "service")
	
	// Try to find the service directory
	entries, err := ioutil.ReadDir(serviceBasePath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), serviceName+"@") && entry.IsDir() {
			return filepath.Join(serviceBasePath, entry.Name())
		}
	}

	return ""
}

func getSDKVersion(sdkPath string) string {
	// Extract version from path or go.mod
	parts := strings.Split(sdkPath, "@")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

func analyzeService(servicePath, serviceName string) (*ServiceInfo, error) {
	info := &ServiceInfo{
		Name:          serviceName,
		PackagePath:   fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
		ClientType:    "Client",
		Operations:    []OperationInfo{},
		ResourceTypes: []ResourceTypeInfo{},
		LastUpdated:   time.Now(),
	}

	// For AWS SDK v2, we'll use a knowledge-based approach
	// since the module structure makes direct analysis complex
	
	// Add known operations and resource types for common services
	switch serviceName {
	case "ec2":
		info.Operations = append(info.Operations,
			OperationInfo{Name: "DescribeInstances", InputType: "DescribeInstancesInput", OutputType: "DescribeInstancesOutput", IsList: true, IsPaginated: true, ResourceType: "Instance"},
			OperationInfo{Name: "DescribeVolumes", InputType: "DescribeVolumesInput", OutputType: "DescribeVolumesOutput", IsList: true, IsPaginated: true, ResourceType: "Volume"},
			OperationInfo{Name: "DescribeSnapshots", InputType: "DescribeSnapshotsInput", OutputType: "DescribeSnapshotsOutput", IsList: true, IsPaginated: true, ResourceType: "Snapshot"},
			OperationInfo{Name: "DescribeVpcs", InputType: "DescribeVpcsInput", OutputType: "DescribeVpcsOutput", IsList: true, IsPaginated: true, ResourceType: "VPC"},
			OperationInfo{Name: "DescribeSubnets", InputType: "DescribeSubnetsInput", OutputType: "DescribeSubnetsOutput", IsList: true, IsPaginated: true, ResourceType: "Subnet"},
			OperationInfo{Name: "DescribeSecurityGroups", InputType: "DescribeSecurityGroupsInput", OutputType: "DescribeSecurityGroupsOutput", IsList: true, IsPaginated: true, ResourceType: "SecurityGroup"},
		)
		info.ResourceTypes = append(info.ResourceTypes,
			ResourceTypeInfo{Name: "Instance", GoType: "Instance", PrimaryKey: "InstanceId", Fields: map[string]string{"InstanceId": "*string", "InstanceType": "*string", "State": "*InstanceState", "VpcId": "*string", "SubnetId": "*string"}},
			ResourceTypeInfo{Name: "Volume", GoType: "Volume", PrimaryKey: "VolumeId", Fields: map[string]string{"VolumeId": "*string", "Size": "*int32", "State": "*string", "VolumeType": "*string"}},
			ResourceTypeInfo{Name: "Snapshot", GoType: "Snapshot", PrimaryKey: "SnapshotId", Fields: map[string]string{"SnapshotId": "*string", "VolumeId": "*string", "State": "*string", "Progress": "*string"}},
			ResourceTypeInfo{Name: "VPC", GoType: "Vpc", PrimaryKey: "VpcId", Fields: map[string]string{"VpcId": "*string", "CidrBlock": "*string", "State": "*string"}},
			ResourceTypeInfo{Name: "Subnet", GoType: "Subnet", PrimaryKey: "SubnetId", Fields: map[string]string{"SubnetId": "*string", "VpcId": "*string", "CidrBlock": "*string", "AvailabilityZone": "*string"}},
			ResourceTypeInfo{Name: "SecurityGroup", GoType: "SecurityGroup", PrimaryKey: "GroupId", Fields: map[string]string{"GroupId": "*string", "GroupName": "*string", "VpcId": "*string", "Description": "*string"}},
		)
		
	case "s3":
		info.Operations = append(info.Operations,
			OperationInfo{Name: "ListBuckets", InputType: "ListBucketsInput", OutputType: "ListBucketsOutput", IsList: true, ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketLocation", InputType: "GetBucketLocationInput", OutputType: "GetBucketLocationOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketVersioning", InputType: "GetBucketVersioningInput", OutputType: "GetBucketVersioningOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketEncryption", InputType: "GetBucketEncryptionInput", OutputType: "GetBucketEncryptionOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetPublicAccessBlock", InputType: "GetPublicAccessBlockInput", OutputType: "GetPublicAccessBlockOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketPolicy", InputType: "GetBucketPolicyInput", OutputType: "GetBucketPolicyOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketLifecycleConfiguration", InputType: "GetBucketLifecycleConfigurationInput", OutputType: "GetBucketLifecycleConfigurationOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketLogging", InputType: "GetBucketLoggingInput", OutputType: "GetBucketLoggingOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketNotificationConfiguration", InputType: "GetBucketNotificationConfigurationInput", OutputType: "GetBucketNotificationConfigurationOutput", ResourceType: "Bucket"},
			OperationInfo{Name: "GetBucketTagging", InputType: "GetBucketTaggingInput", OutputType: "GetBucketTaggingOutput", ResourceType: "Bucket"},
		)
		info.ResourceTypes = append(info.ResourceTypes,
			ResourceTypeInfo{Name: "Bucket", GoType: "Bucket", PrimaryKey: "Name", Fields: map[string]string{"Name": "*string", "CreationDate": "*time.Time"}},
		)
		
	case "lambda":
		info.Operations = append(info.Operations,
			OperationInfo{Name: "ListFunctions", InputType: "ListFunctionsInput", OutputType: "ListFunctionsOutput", IsList: true, IsPaginated: true, ResourceType: "Function"},
			OperationInfo{Name: "GetFunction", InputType: "GetFunctionInput", OutputType: "GetFunctionOutput", ResourceType: "Function"},
		)
		info.ResourceTypes = append(info.ResourceTypes,
			ResourceTypeInfo{Name: "Function", GoType: "FunctionConfiguration", PrimaryKey: "FunctionName", Fields: map[string]string{"FunctionName": "*string", "FunctionArn": "*string", "Runtime": "*string", "Handler": "*string", "CodeSize": "*int64"}},
		)
		
	case "rds":
		info.Operations = append(info.Operations,
			OperationInfo{Name: "DescribeDBInstances", InputType: "DescribeDBInstancesInput", OutputType: "DescribeDBInstancesOutput", IsList: true, IsPaginated: true, ResourceType: "DBInstance"},
			OperationInfo{Name: "DescribeDBClusters", InputType: "DescribeDBClustersInput", OutputType: "DescribeDBClustersOutput", IsList: true, IsPaginated: true, ResourceType: "DBCluster"},
		)
		info.ResourceTypes = append(info.ResourceTypes,
			ResourceTypeInfo{Name: "DBInstance", GoType: "DBInstance", PrimaryKey: "DBInstanceIdentifier", Fields: map[string]string{"DBInstanceIdentifier": "*string", "DBInstanceClass": "*string", "Engine": "*string", "DBInstanceStatus": "*string"}},
			ResourceTypeInfo{Name: "DBCluster", GoType: "DBCluster", PrimaryKey: "DBClusterIdentifier", Fields: map[string]string{"DBClusterIdentifier": "*string", "Engine": "*string", "Status": "*string"}},
		)
		
	case "dynamodb":
		info.Operations = append(info.Operations,
			OperationInfo{Name: "ListTables", InputType: "ListTablesInput", OutputType: "ListTablesOutput", IsList: true, IsPaginated: true, ResourceType: "Table"},
			OperationInfo{Name: "DescribeTable", InputType: "DescribeTableInput", OutputType: "DescribeTableOutput", ResourceType: "Table"},
		)
		info.ResourceTypes = append(info.ResourceTypes,
			ResourceTypeInfo{Name: "Table", GoType: "TableDescription", PrimaryKey: "TableName", Fields: map[string]string{"TableName": "*string", "TableStatus": "*string", "ItemCount": "*int64", "TableSizeBytes": "*int64"}},
		)
		
	case "iam":
		info.Operations = append(info.Operations,
			OperationInfo{Name: "ListUsers", InputType: "ListUsersInput", OutputType: "ListUsersOutput", IsList: true, IsPaginated: true, ResourceType: "User"},
			OperationInfo{Name: "ListRoles", InputType: "ListRolesInput", OutputType: "ListRolesOutput", IsList: true, IsPaginated: true, ResourceType: "Role"},
			OperationInfo{Name: "ListPolicies", InputType: "ListPoliciesInput", OutputType: "ListPoliciesOutput", IsList: true, IsPaginated: true, ResourceType: "Policy"},
		)
		info.ResourceTypes = append(info.ResourceTypes,
			ResourceTypeInfo{Name: "User", GoType: "User", PrimaryKey: "UserName", Fields: map[string]string{"UserName": "*string", "UserId": "*string", "Arn": "*string", "CreateDate": "*time.Time"}},
			ResourceTypeInfo{Name: "Role", GoType: "Role", PrimaryKey: "RoleName", Fields: map[string]string{"RoleName": "*string", "RoleId": "*string", "Arn": "*string", "CreateDate": "*time.Time"}},
			ResourceTypeInfo{Name: "Policy", GoType: "Policy", PrimaryKey: "PolicyName", Fields: map[string]string{"PolicyName": "*string", "PolicyId": "*string", "Arn": "*string", "CreateDate": "*time.Time"}},
		)
		
	default:
		// For unknown services, add basic list operation
		info.Operations = append(info.Operations,
			OperationInfo{Name: "List" + strings.Title(serviceName), InputType: "List" + strings.Title(serviceName) + "Input", OutputType: "List" + strings.Title(serviceName) + "Output", IsList: true},
		)
	}

	return info, nil
}

func analyzeOperations(servicePath string, info *ServiceInfo) error {
	// Look for operation files
	apiFiles, err := filepath.Glob(filepath.Join(servicePath, "api_op_*.go"))
	if err != nil {
		return err
	}

	for _, apiFile := range apiFiles {
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, apiFile, nil, parser.ParseComments)
		if err != nil {
			continue
		}

		// Extract operation info from file
		ast.Inspect(node, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Recv != nil && isClientMethod(x) {
					op := extractOperationInfo(x, node)
					if op != nil {
						info.Operations = append(info.Operations, *op)
					}
				}
			}
			return true
		})
	}

	return nil
}

func isClientMethod(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}

	// Check if receiver is *Client
	for _, field := range fn.Recv.List {
		if starExpr, ok := field.Type.(*ast.StarExpr); ok {
			if ident, ok := starExpr.X.(*ast.Ident); ok && ident.Name == "Client" {
				return true
			}
		}
	}

	return false
}

func extractOperationInfo(fn *ast.FuncDecl, file *ast.File) *OperationInfo {
	op := &OperationInfo{
		Name: fn.Name.Name,
	}

	// Extract input/output types
	if fn.Type.Params != nil && len(fn.Type.Params.List) > 1 {
		// Second parameter is usually the input type
		if len(fn.Type.Params.List) > 1 {
			if starExpr, ok := fn.Type.Params.List[1].Type.(*ast.StarExpr); ok {
				if ident, ok := starExpr.X.(*ast.Ident); ok {
					op.InputType = ident.Name
				}
			}
		}
	}

	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		// First result is usually the output type
		if starExpr, ok := fn.Type.Results.List[0].Type.(*ast.StarExpr); ok {
			if ident, ok := starExpr.X.(*ast.Ident); ok {
				op.OutputType = ident.Name
			}
		}
	}

	// Determine if it's a list operation
	op.IsList = strings.HasPrefix(op.Name, "List") || strings.HasPrefix(op.Name, "Describe")
	
	// Check for pagination
	op.IsPaginated = strings.HasSuffix(op.InputType, "Input") && 
		(strings.Contains(op.OutputType, "NextToken") || strings.Contains(op.OutputType, "NextMarker"))

	// Try to determine resource type
	op.ResourceType = extractResourceType(op.Name)

	return op
}

func extractResourceType(opName string) string {
	// Common patterns for resource operations
	prefixes := []string{"List", "Describe", "Get", "Create", "Update", "Delete"}
	
	for _, prefix := range prefixes {
		if strings.HasPrefix(opName, prefix) {
			resourceName := strings.TrimPrefix(opName, prefix)
			// Remove trailing 's' for plural
			if strings.HasSuffix(resourceName, "s") && len(resourceName) > 1 {
				resourceName = strings.TrimSuffix(resourceName, "s")
			}
			return resourceName
		}
	}

	return ""
}

func analyzeTypes(servicePath string, info *ServiceInfo) error {
	typesPath := filepath.Join(servicePath, "types.go")
	if _, err := os.Stat(typesPath); os.IsNotExist(err) {
		return nil // Not all services have types.go
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, typesPath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	// Look for struct types that represent resources
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.TypeSpec:
			if structType, ok := x.Type.(*ast.StructType); ok {
				if isResourceType(x.Name.Name) {
					resourceInfo := analyzeResourceType(x.Name.Name, structType)
					if resourceInfo != nil {
						info.ResourceTypes = append(info.ResourceTypes, *resourceInfo)
					}
				}
			}
		}
		return true
	})

	return nil
}

func isResourceType(typeName string) bool {
	// Heuristics to identify resource types
	// Skip input/output types
	if strings.HasSuffix(typeName, "Input") || strings.HasSuffix(typeName, "Output") {
		return false
	}
	
	// Skip internal types
	if strings.HasPrefix(typeName, "_") || strings.Contains(typeName, "internal") {
		return false
	}

	// Common resource type patterns
	resourcePatterns := []string{
		"Instance", "Bucket", "Function", "Table", "Queue", "Topic",
		"User", "Role", "Policy", "Group", "Key", "Secret",
		"Cluster", "Service", "Task", "Job", "Pipeline",
		"Database", "Snapshot", "Backup", "Volume", "Image",
	}

	for _, pattern := range resourcePatterns {
		if strings.Contains(typeName, pattern) {
			return true
		}
	}

	return false
}

func analyzeResourceType(typeName string, structType *ast.StructType) *ResourceTypeInfo {
	info := &ResourceTypeInfo{
		Name:   typeName,
		GoType: typeName,
		Fields: make(map[string]string),
	}

	// Analyze struct fields
	for _, field := range structType.Fields.List {
		if field.Names == nil || len(field.Names) == 0 {
			continue
		}

		fieldName := field.Names[0].Name
		fieldType := extractFieldType(field.Type)
		
		info.Fields[fieldName] = fieldType

		// Check for primary key field
		if isPrimaryKeyField(fieldName, field) {
			info.PrimaryKey = fieldName
		}
	}

	return info
}

func extractFieldType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + extractFieldType(t.X)
	case *ast.ArrayType:
		return "[]" + extractFieldType(t.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", extractFieldType(t.Key), extractFieldType(t.Value))
	default:
		return "interface{}"
	}
}

func isPrimaryKeyField(fieldName string, field *ast.Field) bool {
	// Check common primary key names
	pkNames := []string{"ID", "Id", "ARN", "Arn", "Name", "Key"}
	for _, pk := range pkNames {
		if fieldName == pk {
			return true
		}
	}

	// Check struct tags
	if field.Tag != nil {
		tag := field.Tag.Value
		if strings.Contains(tag, "primary") || strings.Contains(tag, "pk") {
			return true
		}
	}

	return false
}

func saveResults(result *AnalysisResult, outputPath string) error {
	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	// Write to file
	if err := ioutil.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}
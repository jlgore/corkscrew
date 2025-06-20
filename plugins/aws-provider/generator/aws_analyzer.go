package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"
)

// AWSServiceInfo represents metadata about an AWS service
type AWSServiceInfo struct {
	Name          string
	PackageName   string
	ClientType    string
	Operations    []AWSOperation
	ResourceTypes []AWSResourceType
}

// AWSOperation represents a service operation
type AWSOperation struct {
	Name         string
	Method       string
	InputType    string
	OutputType   string
	IsList       bool
	IsDescribe   bool
	IsGet        bool
	ResourceType string
	Paginated    bool
}

// AWSResourceType represents a resource that can be scanned
type AWSResourceType struct {
	Name          string
	TypeName      string
	IDField       string
	NameField     string
	ARNField      string
	TagsField     string
	Operations    []string
	Relationships []AWSRelationship
}

// AWSRelationship represents a relationship between resources
type AWSRelationship struct {
	TargetType       string
	RelationshipType string
	FieldName        string
	IsArray          bool
}

// AWSAnalyzer analyzes AWS SDK packages to extract service information
type AWSAnalyzer struct {
	fileSet *token.FileSet
}

// NewAWSAnalyzer creates a new AWS SDK analyzer
func NewAWSAnalyzer() *AWSAnalyzer {
	return &AWSAnalyzer{
		fileSet: token.NewFileSet(),
	}
}

// AnalyzeService analyzes an AWS service package and extracts metadata
func (a *AWSAnalyzer) AnalyzeService(serviceName, packagePath string) (*AWSServiceInfo, error) {
	service := &AWSServiceInfo{
		Name:        serviceName,
		PackageName: packagePath,
		ClientType:  fmt.Sprintf("%sClient", strings.Title(serviceName)),
	}

	// Parse the service package
	pkgs, err := parser.ParseDir(a.fileSet, packagePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse package %s: %w", packagePath, err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			a.analyzeFile(file, service)
		}
	}

	// Post-process to identify resource types and relationships
	a.identifyResourceTypes(service)
	a.identifyRelationships(service)

	return service, nil
}

// analyzeFile analyzes a single Go file for AWS operations
func (a *AWSAnalyzer) analyzeFile(file *ast.File, service *AWSServiceInfo) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if node.Recv != nil {
				a.analyzeFuncDecl(node, service)
			}
		case *ast.TypeSpec:
			a.analyzeTypeSpec(node, service)
		}
		return true
	})
}

// analyzeFuncDecl analyzes function declarations for AWS operations
func (a *AWSAnalyzer) analyzeFuncDecl(funcDecl *ast.FuncDecl, service *AWSServiceInfo) {
	funcName := funcDecl.Name.Name

	// Check if this is a client method
	if !a.isClientMethod(funcDecl, service.ClientType) {
		return
	}

	// Identify operation patterns
	operation := AWSOperation{
		Name:   funcName,
		Method: funcName,
	}

	// Determine operation type and extract base resource type
	if strings.HasPrefix(funcName, "List") {
		operation.IsList = true
		operation.ResourceType = a.extractBaseResourceType(funcName, "List")
	} else if strings.HasPrefix(funcName, "Describe") {
		operation.IsDescribe = true
		operation.ResourceType = a.extractBaseResourceType(funcName, "Describe")
	} else if strings.HasPrefix(funcName, "Get") {
		operation.IsGet = true
		operation.ResourceType = a.extractBaseResourceType(funcName, "Get")
	}
	
	// Debug: Print what we extracted
	fmt.Printf("DEBUG: Operation %s -> ResourceType: %s\n", funcName, operation.ResourceType)

	// Extract input/output types
	if funcDecl.Type.Params != nil && len(funcDecl.Type.Params.List) > 1 {
		if field := funcDecl.Type.Params.List[1]; field.Type != nil {
			if starExpr, ok := field.Type.(*ast.StarExpr); ok {
				if ident, ok := starExpr.X.(*ast.Ident); ok {
					operation.InputType = ident.Name
				}
			}
		}
	}

	if funcDecl.Type.Results != nil && len(funcDecl.Type.Results.List) > 0 {
		if field := funcDecl.Type.Results.List[0]; field.Type != nil {
			if starExpr, ok := field.Type.(*ast.StarExpr); ok {
				if ident, ok := starExpr.X.(*ast.Ident); ok {
					operation.OutputType = ident.Name
				}
			}
		}
	}

	// Check for pagination
	operation.Paginated = a.isPaginated(operation.InputType, operation.OutputType)

	service.Operations = append(service.Operations, operation)
}

// analyzeTypeSpec analyzes type specifications for resource structures
func (a *AWSAnalyzer) analyzeTypeSpec(typeSpec *ast.TypeSpec, service *AWSServiceInfo) {
	// This would analyze struct types to identify resource fields
	// Implementation would examine struct tags and field names
}

// isClientMethod checks if a function is a method of the service client
func (a *AWSAnalyzer) isClientMethod(funcDecl *ast.FuncDecl, clientType string) bool {
	if funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
		return false
	}

	recv := funcDecl.Recv.List[0]
	if starExpr, ok := recv.Type.(*ast.StarExpr); ok {
		if ident, ok := starExpr.X.(*ast.Ident); ok {
			return ident.Name == clientType
		}
	}

	return false
}

// isPaginated determines if an operation supports pagination
func (a *AWSAnalyzer) isPaginated(inputType, outputType string) bool {
	// Check for common pagination patterns
	paginationPatterns := []string{
		"NextToken", "Marker", "ContinuationToken", "PageToken",
	}

	for _, pattern := range paginationPatterns {
		if strings.Contains(inputType, pattern) || strings.Contains(outputType, pattern) {
			return true
		}
	}

	return false
}

// extractBaseResourceType extracts the base resource type from an operation name
func (a *AWSAnalyzer) extractBaseResourceType(operationName, prefix string) string {
	resourceType := strings.TrimPrefix(operationName, prefix)
	
	// Map configuration operations to their base resource types
	baseResourceMappings := map[string]string{
		// S3 bucket configuration operations
		"BucketEncryption":           "Bucket",
		"BucketVersioning":           "Bucket", 
		"BucketLocation":             "Bucket",
		"BucketPolicy":               "Bucket",
		"BucketLifecycleConfiguration": "Bucket",
		"BucketLogging":              "Bucket",
		"BucketNotificationConfiguration": "Bucket",
		"BucketTagging":              "Bucket",
		"BucketAcl":                  "Bucket",
		"BucketCors":                 "Bucket",
		"BucketWebsite":              "Bucket",
		"PublicAccessBlock":          "Bucket",
		
		// EC2 instance configuration operations  
		"InstanceAttribute":          "Instance",
		"InstanceStatus":             "Instance",
		"InstanceTypes":              "Instance",
		
		// Lambda function configuration operations
		"FunctionConfiguration":     "Function",
		"FunctionCodeSigningConfig": "Function",
		"Function":                  "Function", // GetFunction
		
		// RDS database configuration operations
		"DBInstanceAttribute":       "DBInstance",
		"DBClusterAttribute":        "DBCluster",
		
		// DynamoDB table configuration operations
		"TableTagging":              "Table",
		
		// More mappings can be added automatically
	}
	
	// Check if this is a configuration operation
	if baseResource, exists := baseResourceMappings[resourceType]; exists {
		return baseResource
	}
	
	// For operations that end with 's' (plurals), remove the 's'
	if len(resourceType) > 1 && strings.HasSuffix(resourceType, "s") {
		singular := resourceType[:len(resourceType)-1]
		// Avoid false positives like "Status" -> "Statu"
		if !strings.HasSuffix(singular, "Statu") && !strings.HasSuffix(singular, "Acces") {
			return singular
		}
	}
	
	// Return as-is for base resource operations
	return resourceType
}

// identifyResourceTypes identifies resource types from operations
func (a *AWSAnalyzer) identifyResourceTypes(service *AWSServiceInfo) {
	resourceMap := make(map[string]*AWSResourceType)

	for _, op := range service.Operations {
		if op.ResourceType == "" {
			continue
		}

		resourceType := op.ResourceType
		if resource, exists := resourceMap[resourceType]; exists {
			resource.Operations = append(resource.Operations, op.Name)
		} else {
			resource := &AWSResourceType{
				Name:       resourceType,
				TypeName:   resourceType,
				Operations: []string{op.Name},
			}

			// Infer common field names
			resource.IDField = a.inferIDField(resourceType)
			resource.NameField = a.inferNameField(resourceType)
			resource.ARNField = a.inferARNField(resourceType)
			resource.TagsField = "Tags"

			resourceMap[resourceType] = resource
		}
	}

	// Convert map to slice
	for _, resource := range resourceMap {
		service.ResourceTypes = append(service.ResourceTypes, *resource)
	}

	// Sort for consistency
	sort.Slice(service.ResourceTypes, func(i, j int) bool {
		return service.ResourceTypes[i].Name < service.ResourceTypes[j].Name
	})
}

// identifyRelationships identifies relationships between resources
func (a *AWSAnalyzer) identifyRelationships(service *AWSServiceInfo) {
	// This would analyze resource fields to identify relationships
	// For example, EC2 instances have VPC IDs, subnet IDs, security group IDs
	relationshipPatterns := map[string][]AWSRelationship{
		"Instance": {
			{TargetType: "VPC", RelationshipType: "member_of", FieldName: "VpcId"},
			{TargetType: "Subnet", RelationshipType: "member_of", FieldName: "SubnetId"},
			{TargetType: "SecurityGroup", RelationshipType: "protected_by", FieldName: "SecurityGroups", IsArray: true},
		},
		"Bucket": {
			{TargetType: "Object", RelationshipType: "contains", FieldName: "Objects", IsArray: true},
		},
	}

	for i, resource := range service.ResourceTypes {
		if relationships, exists := relationshipPatterns[resource.Name]; exists {
			service.ResourceTypes[i].Relationships = relationships
		}
	}
}

// inferIDField infers the ID field name for a resource type
func (a *AWSAnalyzer) inferIDField(resourceType string) string {
	patterns := []string{
		resourceType + "Id",
		resourceType + "ID",
		"Id",
		"ID",
		"Arn",
		"ARN",
	}

	// Return the most likely pattern
	return patterns[0]
}

// inferNameField infers the name field for a resource type
func (a *AWSAnalyzer) inferNameField(resourceType string) string {
	patterns := []string{
		resourceType + "Name",
		"Name",
		"DisplayName",
		"Title",
	}

	return patterns[0]
}

// inferARNField infers the ARN field for a resource type
func (a *AWSAnalyzer) inferARNField(resourceType string) string {
	patterns := []string{
		resourceType + "Arn",
		resourceType + "ARN",
		"Arn",
		"ARN",
	}

	return patterns[0]
}

// GetKnownServices returns a list of well-known AWS services for analysis
func (a *AWSAnalyzer) GetKnownServices() map[string]string {
	return map[string]string{
		"s3":                     "github.com/aws/aws-sdk-go-v2/service/s3",
		"ec2":                    "github.com/aws/aws-sdk-go-v2/service/ec2",
		"rds":                    "github.com/aws/aws-sdk-go-v2/service/rds",
		"lambda":                 "github.com/aws/aws-sdk-go-v2/service/lambda",
		"iam":                    "github.com/aws/aws-sdk-go-v2/service/iam",
		"dynamodb":               "github.com/aws/aws-sdk-go-v2/service/dynamodb",
		"cloudformation":         "github.com/aws/aws-sdk-go-v2/service/cloudformation",
		"ecs":                    "github.com/aws/aws-sdk-go-v2/service/ecs",
		"eks":                    "github.com/aws/aws-sdk-go-v2/service/eks",
		"elasticloadbalancingv2": "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2",
	}
}

// AnalyzeServiceFromReflection uses reflection to analyze a service client
func (a *AWSAnalyzer) AnalyzeServiceFromReflection(serviceName string, client interface{}) (*AWSServiceInfo, error) {
	service := &AWSServiceInfo{
		Name:        serviceName,
		PackageName: reflect.TypeOf(client).PkgPath(),
		ClientType:  reflect.TypeOf(client).Name(),
	}

	clientType := reflect.TypeOf(client)

	// Analyze methods
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		operation := a.analyzeMethodFromReflection(method)
		if operation != nil {
			service.Operations = append(service.Operations, *operation)
		}
	}

	// Post-process
	a.identifyResourceTypes(service)
	a.identifyRelationships(service)

	return service, nil
}

// analyzeMethodFromReflection analyzes a method using reflection
func (a *AWSAnalyzer) analyzeMethodFromReflection(method reflect.Method) *AWSOperation {
	methodName := method.Name

	// Skip non-operation methods
	if !a.isOperationMethod(methodName) {
		return nil
	}

	operation := &AWSOperation{
		Name:   methodName,
		Method: methodName,
	}

	// Determine operation type and resource
	if strings.HasPrefix(methodName, "List") {
		operation.IsList = true
		resourceType := strings.TrimPrefix(methodName, "List")

		// Special handling for S3 and other services
		if resourceType == "Buckets" {
			operation.ResourceType = "Bucket" // Singular form for resource type
		} else if strings.HasSuffix(resourceType, "s") {
			// Remove trailing 's' for most list operations
			operation.ResourceType = strings.TrimSuffix(resourceType, "s")
		} else {
			operation.ResourceType = resourceType
		}
	} else if strings.HasPrefix(methodName, "Describe") {
		operation.IsDescribe = true
		resourceType := strings.TrimPrefix(methodName, "Describe")
		operation.ResourceType = resourceType
	} else if strings.HasPrefix(methodName, "Get") {
		operation.IsGet = true
		resourceType := strings.TrimPrefix(methodName, "Get")

		// Skip bucket configuration operations for main resource scanning
		// These are secondary operations, not primary resource types
		if strings.HasPrefix(resourceType, "Bucket") && resourceType != "Bucket" {
			return nil // Skip BucketVersioning, BucketEncryption, etc.
		}

		operation.ResourceType = resourceType
	}

	// Only include operations that scan actual resources (not configurations)
	if operation.ResourceType == "" {
		return nil
	}

	// Analyze method signature
	methodType := method.Type
	if methodType.NumIn() > 2 { // receiver, context, input
		inputType := methodType.In(2)
		if inputType.Kind() == reflect.Ptr {
			operation.InputType = inputType.Elem().Name()
		}
	}

	if methodType.NumOut() > 0 {
		outputType := methodType.Out(0)
		if outputType.Kind() == reflect.Ptr {
			operation.OutputType = outputType.Elem().Name()
		}
	}

	// Check pagination
	operation.Paginated = a.isPaginated(operation.InputType, operation.OutputType)

	return operation
}

// isOperationMethod determines if a method name represents an AWS operation
func (a *AWSAnalyzer) isOperationMethod(methodName string) bool {
	operationPrefixes := []string{
		"List", "Describe", "Get", "Create", "Update", "Delete",
		"Put", "Start", "Stop", "Terminate", "Launch",
	}

	for _, prefix := range operationPrefixes {
		if strings.HasPrefix(methodName, prefix) {
			return true
		}
	}

	return false
}

// IdentifyResourceTypes processes operations to identify distinct resource types
func (a *AWSAnalyzer) IdentifyResourceTypes(service *AWSServiceInfo) {
	resourceMap := make(map[string]*AWSResourceType)

	// Group operations by resource type
	for _, operation := range service.Operations {
		if operation.ResourceType == "" {
			continue
		}

		resourceType, exists := resourceMap[operation.ResourceType]
		if !exists {
			resourceType = &AWSResourceType{
				Name:       operation.ResourceType,
				TypeName:   operation.ResourceType,
				Operations: []string{},
			}
			resourceMap[operation.ResourceType] = resourceType
		}

		resourceType.Operations = append(resourceType.Operations, operation.Name)

		// Set field mappings based on service patterns
		if resourceType.IDField == "" {
			resourceType.IDField = a.inferIDField(operation.ResourceType)
		}
		if resourceType.NameField == "" {
			resourceType.NameField = a.inferNameField(operation.ResourceType)
		}
		if resourceType.ARNField == "" {
			resourceType.ARNField = a.inferARNField(operation.ResourceType)
		}
		if resourceType.TagsField == "" {
			resourceType.TagsField = "Tags"
		}
	}

	// Convert map to slice
	for _, resourceType := range resourceMap {
		service.ResourceTypes = append(service.ResourceTypes, *resourceType)
	}

	// Sort resource types by name
	sort.Slice(service.ResourceTypes, func(i, j int) bool {
		return service.ResourceTypes[i].Name < service.ResourceTypes[j].Name
	})
}

// IdentifyRelationships analyzes resource types to identify potential relationships
func (a *AWSAnalyzer) IdentifyRelationships(service *AWSServiceInfo) {
	for i := range service.ResourceTypes {
		resourceType := &service.ResourceTypes[i]
		resourceType.Relationships = a.inferRelationships(service.Name, resourceType.Name)
	}
}

// inferRelationships determines potential relationships for a resource type
func (a *AWSAnalyzer) inferRelationships(serviceName, resourceTypeName string) []AWSRelationship {
	var relationships []AWSRelationship

	// Define common AWS service relationships
	relationshipPatterns := map[string]map[string][]AWSRelationship{
		"ec2": {
			"Instance": {
				{TargetType: "Vpc", RelationshipType: "contained_in", FieldName: "VpcId"},
				{TargetType: "Subnet", RelationshipType: "contained_in", FieldName: "SubnetId"},
				{TargetType: "SecurityGroup", RelationshipType: "protected_by", FieldName: "SecurityGroups", IsArray: true},
				{TargetType: "Volume", RelationshipType: "connected_to", FieldName: "BlockDeviceMappings", IsArray: true},
			},
			"Volume": {
				{TargetType: "Instance", RelationshipType: "attached_to", FieldName: "Attachments", IsArray: true},
			},
			"SecurityGroup": {
				{TargetType: "Vpc", RelationshipType: "contained_in", FieldName: "VpcId"},
			},
			"Subnet": {
				{TargetType: "Vpc", RelationshipType: "contained_in", FieldName: "VpcId"},
			},
		},
		"s3": {
			"Bucket": {
				// S3 buckets have minimal direct relationships, mostly through policies and access
			},
			"Object": {
				{TargetType: "Bucket", RelationshipType: "contained_in", FieldName: "Bucket"},
			},
		},
		"rds": {
			"DBInstance": {
				{TargetType: "DBSubnetGroup", RelationshipType: "member_of", FieldName: "DBSubnetGroup"},
				{TargetType: "SecurityGroup", RelationshipType: "protected_by", FieldName: "VpcSecurityGroups", IsArray: true},
				{TargetType: "DBCluster", RelationshipType: "member_of", FieldName: "DBClusterIdentifier"},
			},
			"DBCluster": {
				{TargetType: "DBSubnetGroup", RelationshipType: "member_of", FieldName: "DBSubnetGroup"},
				{TargetType: "SecurityGroup", RelationshipType: "protected_by", FieldName: "VpcSecurityGroups", IsArray: true},
			},
		},
		"lambda": {
			"Function": {
				{TargetType: "Vpc", RelationshipType: "connected_to", FieldName: "VpcConfig"},
				{TargetType: "SecurityGroup", RelationshipType: "protected_by", FieldName: "VpcConfig.SecurityGroupIds", IsArray: true},
				{TargetType: "Subnet", RelationshipType: "connected_to", FieldName: "VpcConfig.SubnetIds", IsArray: true},
			},
		},
	}

	if servicePatterns, exists := relationshipPatterns[serviceName]; exists {
		if resourceRelationships, exists := servicePatterns[resourceTypeName]; exists {
			relationships = append(relationships, resourceRelationships...)
		}
	}

	return relationships
}

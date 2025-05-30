package generator

import (
	"archive/zip"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// AWSServiceInfo represents metadata about an AWS service
type AWSServiceInfo struct {
	Name         string
	PackageName  string
	ClientType   string
	Operations   []AWSOperation
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
	Name         string
	TypeName     string
	IDField      string
	NameField    string
	ARNField     string
	TagsField    string
	Operations   []string
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
	fileSet     *token.FileSet
	cacheDir    string
	structTypes map[string]*ast.StructType
}

// NewAWSAnalyzer creates a new AWS SDK analyzer
func NewAWSAnalyzer() *AWSAnalyzer {
	cacheDir := filepath.Join(os.TempDir(), "aws-sdk-cache")
	os.MkdirAll(cacheDir, 0755)
	return &AWSAnalyzer{
		fileSet:     token.NewFileSet(),
		cacheDir:    cacheDir,
		structTypes: make(map[string]*ast.StructType),
	}
}

// AnalyzeService analyzes an AWS service package and extracts metadata
func (a *AWSAnalyzer) AnalyzeService(serviceName, packagePath string) (*AWSServiceInfo, error) {
	// Check if packagePath is a GitHub URL or local path
	if strings.HasPrefix(packagePath, "github.com") {
		localPath, err := a.fetchFromGitHub(packagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch from GitHub: %w", err)
		}
		packagePath = localPath
	}

	service := &AWSServiceInfo{
		Name:        serviceName,
		PackageName: packagePath,
		ClientType:  fmt.Sprintf("Client"),
	}

	// Parse the service package
	pkgs, err := parser.ParseDir(a.fileSet, packagePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse package %s: %w", packagePath, err)
	}

	// Reset struct types for this analysis
	a.structTypes = make(map[string]*ast.StructType)

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			a.analyzeFile(file, service)
		}
	}

	// Post-process to identify resource types and relationships
	a.identifyResourceTypes(service)
	a.analyzeStructRelationships(service)

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

	// Determine operation type
	if strings.HasPrefix(funcName, "List") {
		operation.IsList = true
		operation.ResourceType = strings.TrimPrefix(funcName, "List")
	} else if strings.HasPrefix(funcName, "Describe") {
		operation.IsDescribe = true
		operation.ResourceType = strings.TrimPrefix(funcName, "Describe")
	} else if strings.HasPrefix(funcName, "Get") {
		operation.IsGet = true
		operation.ResourceType = strings.TrimPrefix(funcName, "Get")
	}

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
	operation.Paginated = a.isPaginatedAdvanced(operation.InputType, operation.OutputType)

	service.Operations = append(service.Operations, operation)
}

// analyzeTypeSpec analyzes type specifications for resource structures
func (a *AWSAnalyzer) analyzeTypeSpec(typeSpec *ast.TypeSpec, service *AWSServiceInfo) {
	if structType, ok := typeSpec.Type.(*ast.StructType); ok {
		a.structTypes[typeSpec.Name.Name] = structType
	}
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
		"s3":         "github.com/aws/aws-sdk-go-v2/service/s3",
		"ec2":        "github.com/aws/aws-sdk-go-v2/service/ec2",
		"rds":        "github.com/aws/aws-sdk-go-v2/service/rds",
		"lambda":     "github.com/aws/aws-sdk-go-v2/service/lambda",
		"iam":        "github.com/aws/aws-sdk-go-v2/service/iam",
		"dynamodb":   "github.com/aws/aws-sdk-go-v2/service/dynamodb",
		"cloudformation": "github.com/aws/aws-sdk-go-v2/service/cloudformation",
		"ecs":        "github.com/aws/aws-sdk-go-v2/service/ecs",
		"eks":        "github.com/aws/aws-sdk-go-v2/service/eks",
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
		operation.ResourceType = strings.TrimPrefix(methodName, "List")
	} else if strings.HasPrefix(methodName, "Describe") {
		operation.IsDescribe = true
		operation.ResourceType = strings.TrimPrefix(methodName, "Describe")
	} else if strings.HasPrefix(methodName, "Get") {
		operation.IsGet = true
		operation.ResourceType = strings.TrimPrefix(methodName, "Get")
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
	operation.Paginated = a.isPaginatedAdvanced(operation.InputType, operation.OutputType)

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

// fetchFromGitHub downloads AWS SDK package from GitHub
func (a *AWSAnalyzer) fetchFromGitHub(packagePath string) (string, error) {
	// Extract service name from package path
	parts := strings.Split(packagePath, "/")
	if len(parts) < 5 {
		return "", fmt.Errorf("invalid package path: %s", packagePath)
	}
	serviceName := parts[len(parts)-1]
	
	// Check cache first
	cacheDir := filepath.Join(a.cacheDir, serviceName)
	if _, err := os.Stat(cacheDir); err == nil {
		return cacheDir, nil
	}

	// Download from GitHub
	url := fmt.Sprintf("https://github.com/aws/aws-sdk-go-v2/archive/refs/heads/main.zip")
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download SDK: %w", err)
	}
	defer resp.Body.Close()

	// Save to temp file
	tmpFile := filepath.Join(a.cacheDir, "aws-sdk.zip")
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to save zip file: %w", err)
	}
	out.Close()

	// Extract the specific service
	err = a.extractService(tmpFile, serviceName)
	if err != nil {
		return "", fmt.Errorf("failed to extract service: %w", err)
	}

	return cacheDir, nil
}

// extractService extracts a specific service from the AWS SDK zip
func (a *AWSAnalyzer) extractService(zipPath, serviceName string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	servicePrefix := fmt.Sprintf("aws-sdk-go-v2-main/service/%s/", serviceName)
	extractPath := filepath.Join(a.cacheDir, serviceName)

	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, servicePrefix) {
			continue
		}

		// Skip directories and non-Go files
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".go") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		// Create the file path
		relativePath := strings.TrimPrefix(f.Name, servicePrefix)
		destPath := filepath.Join(extractPath, relativePath)
		
		// Create directory if needed
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			rc.Close()
			return err
		}

		// Create the file
		outFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

// analyzeStructRelationships analyzes struct fields to identify relationships
func (a *AWSAnalyzer) analyzeStructRelationships(service *AWSServiceInfo) {
	for i := range service.ResourceTypes {
		resourceType := &service.ResourceTypes[i]
		
		// Find the struct type for this resource
		var structName string
		for _, op := range service.Operations {
			if op.ResourceType == resourceType.Name && op.OutputType != "" {
				structName = op.OutputType
				break
			}
		}

		if structName == "" {
			continue
		}

		// Analyze the struct fields
		relationships := a.findStructRelationships(structName, service.Name)
		resourceType.Relationships = append(resourceType.Relationships, relationships...)
		
		// Update field mappings based on actual struct fields
		a.updateResourceFields(resourceType, structName)
	}
}

// findStructRelationships analyzes struct fields to find relationships
func (a *AWSAnalyzer) findStructRelationships(structName, serviceName string) []AWSRelationship {
	var relationships []AWSRelationship
	
	structType, exists := a.structTypes[structName]
	if !exists {
		return relationships
	}

	// Patterns for identifying relationship fields
	relationshipPatterns := map[*regexp.Regexp]string{
		regexp.MustCompile(`.*VpcId$`):           "Vpc",
		regexp.MustCompile(`.*SubnetId$`):        "Subnet", 
		regexp.MustCompile(`.*SecurityGroupId$`): "SecurityGroup",
		regexp.MustCompile(`.*SecurityGroups$`):  "SecurityGroup",
		regexp.MustCompile(`.*InstanceId$`):      "Instance",
		regexp.MustCompile(`.*VolumeId$`):        "Volume",
		regexp.MustCompile(`.*ClusterId$`):       "Cluster",
		regexp.MustCompile(`.*BucketName$`):      "Bucket",
		regexp.MustCompile(`.*KeyName$`):         "Key",
		regexp.MustCompile(`.*LoadBalancerArn$`): "LoadBalancer",
		regexp.MustCompile(`.*TargetGroupArn$`):  "TargetGroup",
	}

	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fieldName := name.Name
			
			// Check each pattern
			for pattern, targetType := range relationshipPatterns {
				if pattern.MatchString(fieldName) {
					isArray := a.isArrayField(field.Type)
					relationshipType := "references"
					if strings.Contains(strings.ToLower(fieldName), "vpc") || 
					   strings.Contains(strings.ToLower(fieldName), "subnet") {
						relationshipType = "contained_in"
					}
					
					relationships = append(relationships, AWSRelationship{
						TargetType:       targetType,
						RelationshipType: relationshipType,
						FieldName:        fieldName,
						IsArray:          isArray,
					})
					break
				}
			}
		}
	}

	return relationships
}

// isArrayField checks if a field type is an array or slice
func (a *AWSAnalyzer) isArrayField(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.ArrayType:
		return true
	case *ast.StarExpr:
		return a.isArrayField(t.X)
	}
	return false
}

// updateResourceFields updates resource field mappings based on actual struct
func (a *AWSAnalyzer) updateResourceFields(resource *AWSResourceType, structName string) {
	structType, exists := a.structTypes[structName]
	if !exists {
		return
	}

	// Common field patterns
	fieldPatterns := map[string][]string{
		"ID": {resource.Name + "Id", resource.Name + "ID", "Id", "ID", "Arn", "ARN"},
		"Name": {resource.Name + "Name", "Name", "DisplayName", "Title"},
		"ARN": {resource.Name + "Arn", resource.Name + "ARN", "Arn", "ARN"},
		"Tags": {"Tags", "TagList", "TagSet"},
	}

	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fieldName := name.Name
			
			// Check ID patterns
			for _, pattern := range fieldPatterns["ID"] {
				if fieldName == pattern {
					resource.IDField = fieldName
					goto nextField
				}
			}
			
			// Check Name patterns
			for _, pattern := range fieldPatterns["Name"] {
				if fieldName == pattern {
					resource.NameField = fieldName
					goto nextField
				}
			}
			
			// Check ARN patterns
			for _, pattern := range fieldPatterns["ARN"] {
				if fieldName == pattern {
					resource.ARNField = fieldName
					goto nextField
				}
			}
			
			// Check Tags patterns
			for _, pattern := range fieldPatterns["Tags"] {
				if fieldName == pattern {
					resource.TagsField = fieldName
					goto nextField
				}
			}
			
			nextField:
		}
	}
}

// isPaginatedAdvanced checks for pagination using struct field analysis
func (a *AWSAnalyzer) isPaginatedAdvanced(inputType, outputType string) bool {
	// Check basic patterns first
	if a.isPaginated(inputType, outputType) {
		return true
	}

	// Check struct fields for pagination tokens
	paginationFields := []string{
		"NextToken", "Marker", "ContinuationToken", "PageToken",
		"MaxItems", "MaxResults", "Limit", "PageSize",
	}

	// Check input struct
	if inputStruct, exists := a.structTypes[inputType]; exists {
		for _, field := range inputStruct.Fields.List {
			for _, name := range field.Names {
				for _, paginationField := range paginationFields {
					if name.Name == paginationField {
						return true
					}
				}
			}
		}
	}

	// Check output struct
	if outputStruct, exists := a.structTypes[outputType]; exists {
		for _, field := range outputStruct.Fields.List {
			for _, name := range field.Names {
				for _, paginationField := range paginationFields {
					if name.Name == paginationField {
						return true
					}
				}
			}
		}
	}

	return false
}

// ScannerConfig represents configuration for generating scanners
type ScannerConfig struct {
	ServiceName   string                    `json:"service_name"`
	PackageName   string                    `json:"package_name"`
	Resources     []ResourceScannerConfig   `json:"resources"`
	Relationships []RelationshipConfig      `json:"relationships"`
}

// ResourceScannerConfig represents scanner configuration for a resource
type ResourceScannerConfig struct {
	Name          string            `json:"name"`
	TypeName      string            `json:"type_name"`
	ListOperation string            `json:"list_operation,omitempty"`
	GetOperation  string            `json:"get_operation,omitempty"`
	IDField       string            `json:"id_field"`
	NameField     string            `json:"name_field,omitempty"`
	ARNField      string            `json:"arn_field,omitempty"`
	TagsField     string            `json:"tags_field,omitempty"`
	Paginated     bool              `json:"paginated"`
	InputType     string            `json:"input_type"`
	OutputType    string            `json:"output_type"`
	Fields        map[string]string `json:"fields,omitempty"`
}

// RelationshipConfig represents relationship configuration
type RelationshipConfig struct {
	SourceResource   string `json:"source_resource"`
	TargetResource   string `json:"target_resource"`
	RelationshipType string `json:"relationship_type"`
	FieldName        string `json:"field_name"`
	IsArray          bool   `json:"is_array"`
}

// GenerateScannerConfig generates scanner configuration from analyzed service
func (a *AWSAnalyzer) GenerateScannerConfig(service *AWSServiceInfo) *ScannerConfig {
	config := &ScannerConfig{
		ServiceName: service.Name,
		PackageName: service.PackageName,
		Resources:   make([]ResourceScannerConfig, 0, len(service.ResourceTypes)),
	}

	// Convert resource types to scanner configs
	for _, resource := range service.ResourceTypes {
		resourceConfig := ResourceScannerConfig{
			Name:      resource.Name,
			TypeName:  resource.TypeName,
			IDField:   resource.IDField,
			NameField: resource.NameField,
			ARNField:  resource.ARNField,
			TagsField: resource.TagsField,
			Fields:    make(map[string]string),
		}

		// Find operations for this resource
		for _, op := range service.Operations {
			if op.ResourceType == resource.Name {
				if op.IsList {
					resourceConfig.ListOperation = op.Name
					resourceConfig.InputType = op.InputType
					resourceConfig.OutputType = op.OutputType
					resourceConfig.Paginated = op.Paginated
				} else if op.IsGet || op.IsDescribe {
					resourceConfig.GetOperation = op.Name
					if resourceConfig.InputType == "" {
						resourceConfig.InputType = op.InputType
					}
					if resourceConfig.OutputType == "" {
						resourceConfig.OutputType = op.OutputType
					}
				}
			}
		}

		// Add struct fields if available
		if structType, exists := a.structTypes[resourceConfig.OutputType]; exists {
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					fieldName := name.Name
					fieldType := a.getFieldTypeName(field.Type)
					resourceConfig.Fields[fieldName] = fieldType
				}
			}
		}

		config.Resources = append(config.Resources, resourceConfig)
	}

	// Convert relationships
	for _, resource := range service.ResourceTypes {
		for _, rel := range resource.Relationships {
			config.Relationships = append(config.Relationships, RelationshipConfig{
				SourceResource:   resource.Name,
				TargetResource:   rel.TargetType,
				RelationshipType: rel.RelationshipType,
				FieldName:        rel.FieldName,
				IsArray:          rel.IsArray,
			})
		}
	}

	return config
}

// getFieldTypeName extracts the type name from an AST expression
func (a *AWSAnalyzer) getFieldTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + a.getFieldTypeName(t.X)
	case *ast.ArrayType:
		return "[]" + a.getFieldTypeName(t.Elt)
	case *ast.SelectorExpr:
		pkg := a.getFieldTypeName(t.X)
		return pkg + "." + t.Sel.Name
	default:
		return "interface{}"
	}
}

// AnalyzeServiceFromGitHub analyzes a service directly from GitHub
func (a *AWSAnalyzer) AnalyzeServiceFromGitHub(serviceName string) (*AWSServiceInfo, error) {
	packagePath := fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName)
	return a.AnalyzeService(serviceName, packagePath)
}

// AnalyzeMultipleServices analyzes multiple AWS services
func (a *AWSAnalyzer) AnalyzeMultipleServices(serviceNames []string) (map[string]*AWSServiceInfo, error) {
	services := make(map[string]*AWSServiceInfo)
	
	for _, serviceName := range serviceNames {
		service, err := a.AnalyzeServiceFromGitHub(serviceName)
		if err != nil {
			return nil, fmt.Errorf("failed to analyze service %s: %w", serviceName, err)
		}
		services[serviceName] = service
	}
	
	return services, nil
}

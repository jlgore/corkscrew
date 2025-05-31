package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Command line flags
var (
	sdkPath    = flag.String("sdk-path", "./azure-sdk-for-go", "Path to Azure SDK for Go repository")
	outputFile = flag.String("output", "azure-sdk-analysis.json", "Output file for analysis results")
	updateSDK  = flag.Bool("update", false, "Clone or update Azure SDK repository")
	verbose    = flag.Bool("verbose", false, "Enable verbose logging")
	services   = flag.String("services", "", "Comma-separated list of services to analyze (empty = all)")
)

// Enhanced analysis structures for comprehensive service catalog
type ServiceCatalog struct {
	Services      map[string]ServiceDefinition `json:"services"`
	Relationships []ResourceRelationship       `json:"relationships"`
	CommonPatterns map[string]Pattern         `json:"commonPatterns"`
	GeneratedAt   time.Time                   `json:"generatedAt"`
	SDKVersion    string                      `json:"sdkVersion"`
	Summary       AnalysisSummary             `json:"summary"`
}

type ServiceDefinition struct {
	Name        string                `json:"name"`
	Namespace   string                `json:"namespace"`
	Package     string                `json:"package"`
	Version     string                `json:"version"`
	Resources   []ResourceDefinition  `json:"resources"`
	SharedTypes []TypeDefinition      `json:"sharedTypes"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type ResourceDefinition struct {
	Type                 string                    `json:"type"`
	ARMType             string                    `json:"armType"`
	Operations          map[string]OperationDef   `json:"operations"`
	Properties          []PropertyDef             `json:"properties"`
	RequiresResourceGroup bool                    `json:"requiresResourceGroup"`
	SupportsListAll     bool                      `json:"supportsListAll"`
	PaginationType      string                    `json:"paginationType"`
	RelatedResources    []string                  `json:"relatedResources"`
	ResourceGraphQuery  string                    `json:"resourceGraphQuery"`
	CommonTags          []string                  `json:"commonTags"`
	Metadata            map[string]interface{}    `json:"metadata"`
}

type OperationDef struct {
	Method              string              `json:"method"`
	OperationType       string              `json:"operationType"` // list, get, create, update, delete
	SupportsResourceGroup bool              `json:"supportsResourceGroup"`
	ResponseType        string              `json:"responseType"`
	Parameters          []ParameterInfo     `json:"parameters"`
	IsPaginated         bool                `json:"isPaginated"`
	PaginationType      string              `json:"paginationType"` // token, offset, cursor
	RequiresAuth        []string            `json:"requiresAuth"`   // permissions needed
	RateLimits          map[string]int      `json:"rateLimits"`
	ResourceGraphOptimal bool               `json:"resourceGraphOptimal"`
	Metadata            map[string]string   `json:"metadata"`
}

type PropertyDef struct {
	Name        string      `json:"name"`
	Path        string      `json:"path"`        // nested path like "properties.hardwareProfile.vmSize"
	Type        string      `json:"type"`        // ARM type
	DuckDBType  string      `json:"duckdbType"`  // mapped DuckDB type
	Required    bool        `json:"required"`
	Indexed     bool        `json:"indexed"`     // should be indexed in DuckDB
	Compressed  bool        `json:"compressed"`  // should use compression
	Description string      `json:"description"`
	Examples    []interface{} `json:"examples"`
	Constraints map[string]interface{} `json:"constraints"` // validation rules
}

type TypeDefinition struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // struct, interface, enum
	Fields      []FieldDefinition      `json:"fields"`
	UsedBy      []string               `json:"usedBy"` // which resources use this type
	Metadata    map[string]interface{} `json:"metadata"`
}

type FieldDefinition struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Examples    []interface{} `json:"examples"`
}

type ResourceRelationship struct {
	SourceType      string            `json:"sourceType"`
	TargetType      string            `json:"targetType"`
	RelationType    string            `json:"relationType"` // depends_on, contains, references
	Direction       string            `json:"direction"`    // unidirectional, bidirectional
	PropertyPath    string            `json:"propertyPath"` // path in source that references target
	Cardinality     string            `json:"cardinality"`  // one_to_one, one_to_many, many_to_many
	Required        bool              `json:"required"`
	Metadata        map[string]string `json:"metadata"`
}

type Pattern struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Examples    []string          `json:"examples"`
	Regex       string            `json:"regex"`
	Validation  map[string]string `json:"validation"`
}

type ParameterInfo struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description"`
	DefaultValue interface{} `json:"defaultValue"`
	Constraints map[string]interface{} `json:"constraints"`
}

// Legacy structures for backward compatibility
type ServiceAnalysis struct {
	Name      string                    `json:"name"`
	Namespace string                    `json:"namespace"`
	Package   string                    `json:"package"`
	Version   string                    `json:"version"`
	Resources []ResourceAnalysis        `json:"resources"`
	Metadata  map[string]interface{}    `json:"metadata"`
}

type ResourceAnalysis struct {
	Type       string                    `json:"type"`
	ARMType    string                    `json:"armType"`
	Operations map[string]OperationInfo  `json:"operations"`
	Properties map[string]PropertyInfo   `json:"properties"`
	Metadata   map[string]interface{}    `json:"metadata"`
}

type OperationInfo struct {
	Method                string              `json:"method"`
	SupportsResourceGroup bool                `json:"supportsResourceGroup,omitempty"`
	ResponseType          string              `json:"responseType,omitempty"`
	Parameters            []ParameterInfo     `json:"parameters,omitempty"`
	IsPaginated          bool                `json:"isPaginated,omitempty"`
	Metadata             map[string]string   `json:"metadata,omitempty"`
}

type PropertyInfo struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type AnalysisResult struct {
	Timestamp   time.Time         `json:"timestamp"`
	SDKVersion  string            `json:"sdkVersion"`
	Services    []ServiceAnalysis `json:"services"`
	Summary     AnalysisSummary   `json:"summary"`
	// New comprehensive catalog
	ServiceCatalog *ServiceCatalog `json:"serviceCatalog,omitempty"`
}

type AnalysisSummary struct {
	TotalServices     int `json:"totalServices"`
	TotalResources    int `json:"totalResources"`
	TotalOperations   int `json:"totalOperations"`
	TotalRelationships int `json:"totalRelationships"`
	AnalysisTimeMs    int `json:"analysisTimeMs"`
}

// Azure SDK analyzer
type AzureSDKAnalyzer struct {
	sdkPath        string
	verbose        bool
	fileSet        *token.FileSet
	serviceMap     map[string]*ServiceAnalysis
	serviceCatalog *ServiceCatalog
	clientTypes    map[string]*ast.StructType
	responseTypes  map[string]*ast.StructType
	
	// Enhanced analysis components
	relationshipTracker map[string][]ResourceRelationship
	patternMatcher      map[string]*regexp.Regexp
	typeRegistry        map[string]TypeDefinition
	resourceGraphQueries map[string]string
}

func main() {
	flag.Parse()
	
	analyzer := &AzureSDKAnalyzer{
		sdkPath:              *sdkPath,
		verbose:              *verbose,
		fileSet:              token.NewFileSet(),
		serviceMap:           make(map[string]*ServiceAnalysis),
		clientTypes:          make(map[string]*ast.StructType),
		responseTypes:        make(map[string]*ast.StructType),
		relationshipTracker:  make(map[string][]ResourceRelationship),
		patternMatcher:       make(map[string]*regexp.Regexp),
		typeRegistry:         make(map[string]TypeDefinition),
		resourceGraphQueries: make(map[string]string),
	}
	
	// Initialize comprehensive service catalog
	analyzer.serviceCatalog = &ServiceCatalog{
		Services:       make(map[string]ServiceDefinition),
		Relationships:  []ResourceRelationship{},
		CommonPatterns: make(map[string]Pattern),
		GeneratedAt:    time.Now(),
	}
	
	// Initialize common patterns
	analyzer.initializeCommonPatterns()
	
	log.Printf("🚀 Starting Azure SDK analysis")
	startTime := time.Now()
	
	// Update or clone SDK if requested
	if *updateSDK {
		if err := analyzer.updateSDK(); err != nil {
			log.Fatalf("Failed to update SDK: %v", err)
		}
	}
	
	// Verify SDK path exists
	if _, err := os.Stat(*sdkPath); os.IsNotExist(err) {
		log.Fatalf("SDK path does not exist: %s. Use -update flag to clone it.", *sdkPath)
	}
	
	// Parse services filter
	var targetServices []string
	if *services != "" {
		targetServices = strings.Split(*services, ",")
		for i, s := range targetServices {
			targetServices[i] = strings.TrimSpace(s)
		}
	}
	
	// Analyze the SDK
	result, err := analyzer.analyzeSDK(targetServices)
	if err != nil {
		log.Fatalf("Analysis failed: %v", err)
	}
	
	// Calculate analysis time
	analysisTime := time.Since(startTime)
	result.Summary.AnalysisTimeMs = int(analysisTime.Milliseconds())
	
	// Generate comprehensive service catalog
	if err := analyzer.generateServiceCatalog(result); err != nil {
		log.Printf("Warning: Failed to generate comprehensive service catalog: %v", err)
	} else {
		result.ServiceCatalog = analyzer.serviceCatalog
		log.Printf("✅ Generated comprehensive service catalog with %d services", len(analyzer.serviceCatalog.Services))
	}
	
	// Write results
	if err := analyzer.writeResults(result, *outputFile); err != nil {
		log.Fatalf("Failed to write results: %v", err)
	}
	
	log.Printf("✅ Analysis complete in %v", analysisTime)
	log.Printf("📊 Found %d services, %d resources, %d operations", 
		result.Summary.TotalServices, 
		result.Summary.TotalResources, 
		result.Summary.TotalOperations)
	log.Printf("📄 Results written to: %s", *outputFile)
}

// updateSDK clones or updates the Azure SDK repository
func (a *AzureSDKAnalyzer) updateSDK() error {
	log.Printf("📥 Updating Azure SDK repository...")
	
	// Check if directory exists
	if _, err := os.Stat(a.sdkPath); os.IsNotExist(err) {
		// Clone repository
		log.Printf("Cloning Azure SDK for Go...")
		cmd := exec.Command("git", "clone", "https://github.com/Azure/azure-sdk-for-go.git", a.sdkPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git clone failed: %v\nOutput: %s", err, output)
		}
	} else {
		// Update existing repository
		log.Printf("Updating existing Azure SDK repository...")
		cmd := exec.Command("git", "pull", "origin", "main")
		cmd.Dir = a.sdkPath
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git pull failed: %v\nOutput: %s", err, output)
		}
	}
	
	return nil
}

// analyzeSDK performs comprehensive analysis of the Azure SDK
func (a *AzureSDKAnalyzer) analyzeSDK(targetServices []string) (*AnalysisResult, error) {
	log.Printf("🔍 Analyzing Azure SDK at: %s", a.sdkPath)
	
	// Find all resource manager services
	resourceManagerPath := filepath.Join(a.sdkPath, "sdk", "resourcemanager")
	if _, err := os.Stat(resourceManagerPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("resource manager path not found: %s", resourceManagerPath)
	}
	
	// Discover services
	services, err := a.discoverServices(resourceManagerPath, targetServices)
	if err != nil {
		return nil, fmt.Errorf("service discovery failed: %v", err)
	}
	
	log.Printf("📋 Found %d services to analyze", len(services))
	
	// Analyze each service
	for _, service := range services {
		if a.verbose {
			log.Printf("🔬 Analyzing service: %s", service.Name)
		}
		
		if err := a.analyzeService(service); err != nil {
			log.Printf("⚠️  Failed to analyze service %s: %v", service.Name, err)
			continue
		}
	}
	
	// Convert to slice and sort
	servicesList := make([]ServiceAnalysis, 0, len(a.serviceMap))
	for _, service := range a.serviceMap {
		servicesList = append(servicesList, *service)
	}
	
	sort.Slice(servicesList, func(i, j int) bool {
		return servicesList[i].Name < servicesList[j].Name
	})
	
	// Calculate summary
	summary := a.calculateSummary(servicesList)
	
	return &AnalysisResult{
		Timestamp:  time.Now(),
		SDKVersion: a.getSDKVersion(),
		Services:   servicesList,
		Summary:    summary,
	}, nil
}

// discoverServices finds all available services in the resource manager
func (a *AzureSDKAnalyzer) discoverServices(resourceManagerPath string, targetServices []string) ([]*ServiceAnalysis, error) {
	var services []*ServiceAnalysis
	
	// Create target service map for filtering
	targetMap := make(map[string]bool)
	if len(targetServices) > 0 {
		for _, service := range targetServices {
			targetMap[service] = true
		}
	}
	
	// Walk through resource manager directory
	err := filepath.Walk(resourceManagerPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip non-directories
		if !info.IsDir() {
			return nil
		}
		
		// Check if this is a service directory (contains arm* subdirectory)
		serviceName := info.Name()
		armDirs, err := filepath.Glob(filepath.Join(path, "arm*"))
		if err != nil || len(armDirs) == 0 {
			return nil
		}
		
		// Skip if not in target services (when specified)
		if len(targetServices) > 0 && !targetMap[serviceName] {
			return nil
		}
		
		// Skip if this is the resourcemanager directory itself
		if path == resourceManagerPath {
			return nil
		}
		
		// Skip nested directories
		relPath, _ := filepath.Rel(resourceManagerPath, path)
		if strings.Contains(relPath, string(filepath.Separator)) {
			return nil
		}
		
		// Create service analysis
		service := &ServiceAnalysis{
			Name:      serviceName,
			Namespace: a.generateNamespace(serviceName),
			Resources: []ResourceAnalysis{},
			Metadata:  make(map[string]interface{}),
		}
		
		// Find package and version info
		for _, armDir := range armDirs {
			if pkg, version := a.extractPackageInfo(armDir); pkg != "" {
				service.Package = pkg
				service.Version = version
				break
			}
		}
		
		services = append(services, service)
		return nil
	})
	
	return services, err
}

// analyzeService performs detailed analysis of a single service
func (a *AzureSDKAnalyzer) analyzeService(service *ServiceAnalysis) error {
	// Find the service package directory
	packagePath := a.findServicePackagePath(service.Name)
	if packagePath == "" {
		return fmt.Errorf("package path not found for service: %s", service.Name)
	}
	
	// Parse Go files in the package
	packages, err := parser.ParseDir(a.fileSet, packagePath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse package %s: %v", packagePath, err)
	}
	
	// Store service in map for cross-referencing
	a.serviceMap[service.Name] = service
	
	// Analyze each package
	for packageName, pkg := range packages {
		if a.verbose {
			log.Printf("  📦 Analyzing package: %s", packageName)
		}
		
		// First pass: collect client types and response types
		a.collectTypes(pkg)
		
		// Second pass: analyze operations and extract resources
		if err := a.analyzePackageOperations(service, pkg); err != nil {
			log.Printf("⚠️  Failed to analyze operations in package %s: %v", packageName, err)
		}
	}
	
	return nil
}

// collectTypes collects client and response types for later analysis
func (a *AzureSDKAnalyzer) collectTypes(pkg *ast.Package) {
	for _, file := range pkg.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GenDecl:
				if node.Tok == token.TYPE {
					for _, spec := range node.Specs {
						if typeSpec, ok := spec.(*ast.TypeSpec); ok {
							typeName := typeSpec.Name.Name
							
							// Collect client types
							if strings.HasSuffix(typeName, "Client") {
								if structType, ok := typeSpec.Type.(*ast.StructType); ok {
									a.clientTypes[typeName] = structType
								}
							}
							
							// Collect response types
							if strings.HasSuffix(typeName, "Response") || 
							   strings.HasSuffix(typeName, "Result") ||
							   (strings.Contains(typeName, "Response") && !strings.HasPrefix(typeName, "HTTP")) {
								if structType, ok := typeSpec.Type.(*ast.StructType); ok {
									a.responseTypes[typeName] = structType
								}
							}
						}
					}
				}
			}
			return true
		})
	}
}

// analyzePackageOperations analyzes operations in a package and extracts resources
func (a *AzureSDKAnalyzer) analyzePackageOperations(service *ServiceAnalysis, pkg *ast.Package) error {
	resources := make(map[string]*ResourceAnalysis)
	
	for _, file := range pkg.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				// Only analyze methods with receivers
				if node.Recv != nil && len(node.Recv.List) > 0 {
					operation := a.analyzeOperation(node)
					if operation != nil {
						resourceType := a.extractResourceTypeFromOperation(node.Name.Name)
						if resourceType != "" {
							// Get or create resource
							resource, exists := resources[resourceType]
							if !exists {
								resource = &ResourceAnalysis{
									Type:       resourceType,
									ARMType:    a.generateARMType(service.Namespace, resourceType),
									Operations: make(map[string]OperationInfo),
									Properties: make(map[string]PropertyInfo),
									Metadata:   make(map[string]interface{}),
								}
								resources[resourceType] = resource
							}
							
							// Add operation to resource
							operationType := a.classifyOperation(node.Name.Name)
							resource.Operations[operationType] = *operation
							
							// Extract properties from response type
							a.extractPropertiesFromOperation(resource, node)
						}
					}
				}
			}
			return true
		})
	}
	
	// Convert map to slice
	for _, resource := range resources {
		service.Resources = append(service.Resources, *resource)
	}
	
	// Sort resources by type
	sort.Slice(service.Resources, func(i, j int) bool {
		return service.Resources[i].Type < service.Resources[j].Type
	})
	
	return nil
}

// analyzeOperation analyzes a single operation method
func (a *AzureSDKAnalyzer) analyzeOperation(funcDecl *ast.FuncDecl) *OperationInfo {
	operation := &OperationInfo{
		Method:     funcDecl.Name.Name,
		Parameters: []ParameterInfo{},
		Metadata:   make(map[string]string),
	}
	
	// Check if it's a paginated operation
	operation.IsPaginated = strings.Contains(funcDecl.Name.Name, "Pager") || 
							strings.Contains(funcDecl.Name.Name, "NewList")
	
	// Check if it supports resource groups
	operation.SupportsResourceGroup = strings.Contains(funcDecl.Name.Name, "ByResourceGroup") ||
									  a.hasResourceGroupParameter(funcDecl)
	
	// Extract parameters
	if funcDecl.Type.Params != nil {
		for _, param := range funcDecl.Type.Params.List {
			for _, name := range param.Names {
				paramInfo := ParameterInfo{
					Name: name.Name,
					Type: a.extractTypeString(param.Type),
					Required: !strings.Contains(name.Name, "options") && name.Name != "ctx",
				}
				operation.Parameters = append(operation.Parameters, paramInfo)
			}
		}
	}
	
	// Extract response type
	if funcDecl.Type.Results != nil && len(funcDecl.Type.Results.List) > 0 {
		operation.ResponseType = a.extractTypeString(funcDecl.Type.Results.List[0].Type)
	}
	
	return operation
}

// extractResourceTypeFromOperation extracts resource type from operation name
func (a *AzureSDKAnalyzer) extractResourceTypeFromOperation(operationName string) string {
	// Remove common prefixes and suffixes
	resourceType := operationName
	
	// Remove method prefixes
	prefixes := []string{"New", "Begin", "Get", "List", "Create", "Update", "Delete"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(resourceType, prefix) {
			resourceType = strings.TrimPrefix(resourceType, prefix)
			break
		}
	}
	
	// Remove method suffixes
	suffixes := []string{"Pager", "Poller", "Response", "Result", "ByResourceGroup"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(resourceType, suffix) {
			resourceType = strings.TrimSuffix(resourceType, suffix)
			break
		}
	}
	
	// Handle special cases
	switch {
	case strings.Contains(operationName, "VirtualMachine"):
		return "virtualMachines"
	case strings.Contains(operationName, "StorageAccount"):
		return "storageAccounts"
	case strings.Contains(operationName, "NetworkInterface"):
		return "networkInterfaces"
	case strings.Contains(operationName, "PublicIPAddress"):
		return "publicIPAddresses"
	case strings.Contains(operationName, "LoadBalancer"):
		return "loadBalancers"
	case strings.Contains(operationName, "NetworkSecurityGroup"):
		return "networkSecurityGroups"
	case strings.Contains(operationName, "VirtualNetwork"):
		return "virtualNetworks"
	case strings.Contains(operationName, "Subnet"):
		return "subnets"
	}
	
	// Convert to camelCase and pluralize if needed
	if resourceType != "" && !strings.HasSuffix(resourceType, "s") {
		resourceType = strings.ToLower(resourceType[:1]) + resourceType[1:] + "s"
	}
	
	return resourceType
}

// classifyOperation classifies operation type
func (a *AzureSDKAnalyzer) classifyOperation(operationName string) string {
	switch {
	case strings.Contains(operationName, "List") || strings.Contains(operationName, "Pager"):
		return "list"
	case strings.Contains(operationName, "Get") && !strings.Contains(operationName, "List"):
		return "get"
	case strings.Contains(operationName, "Create") || strings.Contains(operationName, "BeginCreate"):
		return "create"
	case strings.Contains(operationName, "Update") || strings.Contains(operationName, "BeginUpdate"):
		return "update"
	case strings.Contains(operationName, "Delete") || strings.Contains(operationName, "BeginDelete"):
		return "delete"
	default:
		return "other"
	}
}

// extractPropertiesFromOperation extracts properties from operation response types
func (a *AzureSDKAnalyzer) extractPropertiesFromOperation(resource *ResourceAnalysis, funcDecl *ast.FuncDecl) {
	// Extract response type and analyze its structure
	if funcDecl.Type.Results != nil && len(funcDecl.Type.Results.List) > 0 {
		responseType := a.extractTypeString(funcDecl.Type.Results.List[0].Type)
		a.analyzeResponseTypeProperties(resource, responseType)
	}
}

// analyzeResponseTypeProperties analyzes properties from response types
func (a *AzureSDKAnalyzer) analyzeResponseTypeProperties(resource *ResourceAnalysis, responseTypeName string) {
	// Find the response type in our collected types
	for typeName, structType := range a.responseTypes {
		if strings.Contains(typeName, responseTypeName) || strings.Contains(responseTypeName, typeName) {
			a.extractStructProperties(resource, structType, "")
			break
		}
	}
}

// extractStructProperties recursively extracts properties from struct types
func (a *AzureSDKAnalyzer) extractStructProperties(resource *ResourceAnalysis, structType *ast.StructType, prefix string) {
	if structType.Fields == nil {
		return
	}
	
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fieldName := name.Name
			if prefix != "" {
				fieldName = prefix + "." + fieldName
			}
			
			// Skip unexported fields
			if !ast.IsExported(name.Name) {
				continue
			}
			
			propertyInfo := PropertyInfo{
				Type: a.extractTypeString(field.Type),
			}
			
			// Extract description from comments
			if field.Comment != nil {
				propertyInfo.Description = strings.TrimSpace(field.Comment.Text())
			}
			
			resource.Properties[fieldName] = propertyInfo
		}
	}
}

// Utility functions

func (a *AzureSDKAnalyzer) generateNamespace(serviceName string) string {
	// Map common service names to their Microsoft namespaces
	namespaceMap := map[string]string{
		"compute":               "Microsoft.Compute",
		"storage":               "Microsoft.Storage",
		"network":               "Microsoft.Network",
		"keyvault":              "Microsoft.KeyVault",
		"resources":             "Microsoft.Resources",
		"authorization":         "Microsoft.Authorization",
		"managementgroups":      "Microsoft.Management",
		"subscription":          "Microsoft.Subscription",
		"sql":                   "Microsoft.Sql",
		"web":                   "Microsoft.Web",
		"containerservice":      "Microsoft.ContainerService",
		"containerregistry":     "Microsoft.ContainerRegistry",
		"applicationinsights":   "Microsoft.Insights",
		"monitor":               "Microsoft.Insights",
		"cosmos":                "Microsoft.DocumentDB",
		"servicebus":            "Microsoft.ServiceBus",
		"eventhub":              "Microsoft.EventHub",
	}
	
	if namespace, exists := namespaceMap[serviceName]; exists {
		return namespace
	}
	
	// Default: capitalize first letter and add Microsoft prefix
	return "Microsoft." + strings.Title(serviceName)
}

func (a *AzureSDKAnalyzer) generateARMType(namespace, resourceType string) string {
	return fmt.Sprintf("%s/%s", namespace, resourceType)
}

func (a *AzureSDKAnalyzer) extractPackageInfo(armDir string) (string, string) {
	// Extract package name and version from directory structure
	parts := strings.Split(armDir, string(filepath.Separator))
	if len(parts) == 0 {
		return "", ""
	}
	
	armPackage := parts[len(parts)-1]
	
	// Extract version from package name (e.g., armcompute -> v5)
	versionRegex := regexp.MustCompile(`v(\d+)$`)
	version := "v1" // default
	if matches := versionRegex.FindStringSubmatch(armPackage); len(matches) > 1 {
		version = "v" + matches[1]
	}
	
	// Build full package path
	packagePath := "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager"
	relPath, _ := filepath.Rel(filepath.Join(a.sdkPath, "sdk", "resourcemanager"), armDir)
	packagePath = packagePath + "/" + strings.ReplaceAll(relPath, string(filepath.Separator), "/")
	
	return packagePath, version
}

func (a *AzureSDKAnalyzer) findServicePackagePath(serviceName string) string {
	// Find the arm* package for the service
	serviceDir := filepath.Join(a.sdkPath, "sdk", "resourcemanager", serviceName)
	matches, err := filepath.Glob(filepath.Join(serviceDir, "arm*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	
	// Return the first match (usually there's only one)
	return matches[0]
}

func (a *AzureSDKAnalyzer) extractTypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + a.extractTypeString(t.X)
	case *ast.SelectorExpr:
		return a.extractTypeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + a.extractTypeString(t.Elt)
	case *ast.MapType:
		return "map[" + a.extractTypeString(t.Key) + "]" + a.extractTypeString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.ChanType:
		return "chan " + a.extractTypeString(t.Value)
	case *ast.FuncType:
		return "func"
	default:
		return "unknown"
	}
}

func (a *AzureSDKAnalyzer) hasResourceGroupParameter(funcDecl *ast.FuncDecl) bool {
	if funcDecl.Type.Params == nil {
		return false
	}
	
	for _, param := range funcDecl.Type.Params.List {
		for _, name := range param.Names {
			if strings.Contains(strings.ToLower(name.Name), "resourcegroup") {
				return true
			}
		}
	}
	return false
}

func (a *AzureSDKAnalyzer) getSDKVersion() string {
	// Try to get version from git
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = a.sdkPath
	if output, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(output))
	}
	
	// Fallback to commit hash
	cmd = exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = a.sdkPath
	if output, err := cmd.Output(); err == nil {
		return "commit-" + strings.TrimSpace(string(output))
	}
	
	return "unknown"
}

func (a *AzureSDKAnalyzer) calculateSummary(services []ServiceAnalysis) AnalysisSummary {
	summary := AnalysisSummary{
		TotalServices: len(services),
	}
	
	for _, service := range services {
		summary.TotalResources += len(service.Resources)
		for _, resource := range service.Resources {
			summary.TotalOperations += len(resource.Operations)
		}
	}
	
	return summary
}

func (a *AzureSDKAnalyzer) writeResults(result *AnalysisResult, outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()
	
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// initializeCommonPatterns sets up pattern recognition for Azure resources
func (a *AzureSDKAnalyzer) initializeCommonPatterns() {
	patterns := map[string]Pattern{
		"resourceId": {
			Name:        "Azure Resource ID",
			Description: "Standard Azure resource identifier pattern",
			Examples:    []string{"/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProviderNamespace}/{resourceType}/{resourceName}"},
			Regex:       `^/subscriptions/[a-f0-9-]+/resourceGroups/[^/]+/providers/[^/]+/[^/]+/[^/]+`,
			Validation:  map[string]string{"format": "arm_resource_id"},
		},
		"vmSize": {
			Name:        "Virtual Machine Size",
			Description: "Azure VM size pattern",
			Examples:    []string{"Standard_D2s_v3", "Basic_A1", "Standard_B1ms"},
			Regex:       `^(Basic|Standard)_[A-Z][0-9]+[a-z]*(_v[0-9]+)?$`,
			Validation:  map[string]string{"category": "vm_size"},
		},
		"storageAccountName": {
			Name:        "Storage Account Name",
			Description: "Azure storage account naming pattern",
			Examples:    []string{"mystorageaccount", "storage123"},
			Regex:       `^[a-z0-9]{3,24}$`,
			Validation:  map[string]string{"min_length": "3", "max_length": "24", "chars": "lowercase_alphanumeric"},
		},
		"location": {
			Name:        "Azure Location",
			Description: "Azure region/location pattern",
			Examples:    []string{"eastus", "westeurope", "southeastasia"},
			Regex:       `^[a-z]+[a-z0-9]*$`,
			Validation:  map[string]string{"type": "azure_location"},
		},
	}
	
	a.serviceCatalog.CommonPatterns = patterns
	
	// Compile regex patterns
	for name, pattern := range patterns {
		if compiled, err := regexp.Compile(pattern.Regex); err == nil {
			a.patternMatcher[name] = compiled
		}
	}
}

// generateServiceCatalog creates the comprehensive service catalog
func (a *AzureSDKAnalyzer) generateServiceCatalog(legacyResult *AnalysisResult) error {
	a.serviceCatalog.SDKVersion = legacyResult.SDKVersion
	a.serviceCatalog.GeneratedAt = legacyResult.Timestamp
	
	// Convert legacy services to new format
	for _, legacyService := range legacyResult.Services {
		serviceDef := a.convertLegacyService(legacyService)
		a.serviceCatalog.Services[legacyService.Name] = serviceDef
	}
	
	// Generate relationships between resources
	a.generateResourceRelationships()
	
	// Generate Resource Graph queries
	a.generateResourceGraphQueries()
	
	// Update summary
	totalResources := 0
	totalOperations := 0
	for _, service := range a.serviceCatalog.Services {
		totalResources += len(service.Resources)
		for _, resource := range service.Resources {
			totalOperations += len(resource.Operations)
		}
	}
	
	a.serviceCatalog.Summary = AnalysisSummary{
		TotalServices:      len(a.serviceCatalog.Services),
		TotalResources:     totalResources,
		TotalOperations:    totalOperations,
		TotalRelationships: len(a.serviceCatalog.Relationships),
		AnalysisTimeMs:     legacyResult.Summary.AnalysisTimeMs,
	}
	
	return nil
}

// convertLegacyService converts legacy service analysis to new comprehensive format
func (a *AzureSDKAnalyzer) convertLegacyService(legacy ServiceAnalysis) ServiceDefinition {
	serviceDef := ServiceDefinition{
		Name:        legacy.Name,
		Namespace:   legacy.Namespace,
		Package:     legacy.Package,
		Version:     legacy.Version,
		Resources:   []ResourceDefinition{},
		SharedTypes: []TypeDefinition{},
		Metadata:    legacy.Metadata,
	}
	
	// Convert resources
	for _, legacyResource := range legacy.Resources {
		resourceDef := a.convertLegacyResource(legacyResource, legacy.Name)
		serviceDef.Resources = append(serviceDef.Resources, resourceDef)
	}
	
	return serviceDef
}

// convertLegacyResource converts legacy resource analysis to new comprehensive format
func (a *AzureSDKAnalyzer) convertLegacyResource(legacy ResourceAnalysis, serviceName string) ResourceDefinition {
	resourceDef := ResourceDefinition{
		Type:                  legacy.Type,
		ARMType:              legacy.ARMType,
		Operations:           make(map[string]OperationDef),
		Properties:           []PropertyDef{},
		RequiresResourceGroup: a.determineResourceGroupRequirement(legacy),
		SupportsListAll:      a.determineListAllSupport(legacy),
		PaginationType:       a.determinePaginationType(legacy),
		RelatedResources:     []string{},
		CommonTags:          a.getCommonTags(legacy.ARMType),
		Metadata:            legacy.Metadata,
	}
	
	// Convert operations
	for opName, legacyOp := range legacy.Operations {
		opDef := OperationDef{
			Method:               legacyOp.Method,
			OperationType:        a.classifyOperationType(opName, legacyOp.Method),
			SupportsResourceGroup: legacyOp.SupportsResourceGroup,
			ResponseType:         legacyOp.ResponseType,
			Parameters:           legacyOp.Parameters,
			IsPaginated:          legacyOp.IsPaginated,
			PaginationType:       a.determinePaginationType(legacy),
			RequiresAuth:         a.getRequiredPermissions(legacy.ARMType, opName),
			RateLimits:          a.getOperationRateLimits(legacy.ARMType, opName),
			ResourceGraphOptimal: a.isResourceGraphOptimal(legacy.ARMType, opName),
			Metadata:            legacyOp.Metadata,
		}
		resourceDef.Operations[opName] = opDef
	}
	
	// Convert properties with enhanced metadata
	for propName, legacyProp := range legacy.Properties {
		propertyDef := PropertyDef{
			Name:        propName,
			Path:        propName,
			Type:        legacyProp.Type,
			DuckDBType:  a.mapToDuckDBType(legacyProp.Type),
			Required:    legacyProp.Required,
			Indexed:     a.shouldBeIndexed(propName, legacy.ARMType),
			Compressed:  a.shouldBeCompressed(propName, legacyProp.Type),
			Description: legacyProp.Description,
			Examples:    []interface{}{},
			Constraints: make(map[string]interface{}),
		}
		
		// Apply pattern validation
		a.applyPatternValidation(&propertyDef)
		
		resourceDef.Properties = append(resourceDef.Properties, propertyDef)
	}
	
	// Generate optimal Resource Graph query
	resourceDef.ResourceGraphQuery = a.generateResourceGraphQuery(legacy.ARMType)
	
	return resourceDef
}

// Helper methods for enhanced analysis

func (a *AzureSDKAnalyzer) determineResourceGroupRequirement(resource ResourceAnalysis) bool {
	// Check if any operation requires resource group
	for _, op := range resource.Operations {
		if op.SupportsResourceGroup {
			return true
		}
	}
	
	// Most ARM resources require resource groups except subscription-level resources
	subscriptionLevelTypes := []string{
		"Microsoft.Subscription/",
		"Microsoft.Management/",
		"Microsoft.Authorization/policyDefinitions",
		"Microsoft.Authorization/roleDefinitions",
	}
	
	for _, prefix := range subscriptionLevelTypes {
		if strings.HasPrefix(resource.ARMType, prefix) {
			return false
		}
	}
	
	return true
}

func (a *AzureSDKAnalyzer) determineListAllSupport(resource ResourceAnalysis) bool {
	// Check if there's a list operation that doesn't require resource group
	for opName, op := range resource.Operations {
		if strings.Contains(strings.ToLower(opName), "list") && !op.SupportsResourceGroup {
			return true
		}
	}
	return false
}

func (a *AzureSDKAnalyzer) determinePaginationType(resource ResourceAnalysis) string {
	for _, op := range resource.Operations {
		if op.IsPaginated {
			if strings.Contains(op.ResponseType, "Pager") {
				return "azure_pager"
			}
			if strings.Contains(op.ResponseType, "Iterator") {
				return "iterator"
			}
			return "token"
		}
	}
	return "none"
}

func (a *AzureSDKAnalyzer) classifyOperationType(opName, method string) string {
	opName = strings.ToLower(opName)
	switch {
	case strings.Contains(opName, "list"):
		return "list"
	case strings.Contains(opName, "get") && !strings.Contains(opName, "list"):
		return "get"
	case strings.Contains(opName, "create") || strings.Contains(opName, "begincreate"):
		return "create"
	case strings.Contains(opName, "update") || strings.Contains(opName, "beginupdate"):
		return "update"
	case strings.Contains(opName, "delete") || strings.Contains(opName, "begindelete"):
		return "delete"
	case strings.Contains(opName, "start") || strings.Contains(opName, "stop") || strings.Contains(opName, "restart"):
		return "action"
	default:
		return "other"
	}
}

func (a *AzureSDKAnalyzer) mapToDuckDBType(armType string) string {
	switch strings.ToLower(armType) {
	case "string", "varchar", "*string":
		return "VARCHAR"
	case "int", "int32", "int64", "*int", "*int32", "*int64", "integer":
		return "INTEGER"
	case "float", "float32", "float64", "*float32", "*float64", "double":
		return "DOUBLE"
	case "bool", "*bool", "boolean":
		return "BOOLEAN"
	case "time.time", "*time.time", "datetime":
		return "TIMESTAMP"
	case "[]string", "[]interface{}", "map[string]interface{}", "map[string]string":
		return "JSON"
	case "[]byte":
		return "BLOB"
	default:
		if strings.HasPrefix(armType, "[]") {
			return "JSON" // Arrays as JSON
		}
		if strings.HasPrefix(armType, "map[") {
			return "JSON" // Maps as JSON
		}
		return "JSON" // Complex types as JSON
	}
}

func (a *AzureSDKAnalyzer) shouldBeIndexed(propName, armType string) bool {
	indexedProperties := []string{
		"id", "name", "type", "location", "resourcegroup", 
		"subscriptionid", "provisioningstate", "sku", "kind",
		"properties.hardwareprofile.vmsize", "properties.provisioningstate",
		"tags.environment", "tags.application", "tags.owner",
	}
	
	propLower := strings.ToLower(propName)
	for _, indexed := range indexedProperties {
		if strings.Contains(propLower, indexed) {
			return true
		}
	}
	
	return false
}

func (a *AzureSDKAnalyzer) shouldBeCompressed(propName, propType string) bool {
	// Compress large text fields and JSON
	compressedTypes := []string{"json", "varchar", "text", "blob"}
	compressedNames := []string{"properties", "tags", "metadata", "rawdata", "configuration"}
	
	typeLower := strings.ToLower(propType)
	nameLower := strings.ToLower(propName)
	
	for _, ct := range compressedTypes {
		if strings.Contains(typeLower, ct) {
			return true
		}
	}
	
	for _, cn := range compressedNames {
		if strings.Contains(nameLower, cn) {
			return true
		}
	}
	
	return false
}

func (a *AzureSDKAnalyzer) getCommonTags(armType string) []string {
	// Standard Azure tags that are commonly used
	commonTags := []string{"Environment", "Application", "Owner", "CostCenter", "Project"}
	
	// Add resource-specific common tags
	if strings.Contains(armType, "Compute") {
		commonTags = append(commonTags, "Workload", "OSType")
	}
	if strings.Contains(armType, "Storage") {
		commonTags = append(commonTags, "DataClassification", "BackupPolicy")
	}
	if strings.Contains(armType, "Network") {
		commonTags = append(commonTags, "NetworkZone", "SecurityGroup")
	}
	
	return commonTags
}

func (a *AzureSDKAnalyzer) getRequiredPermissions(armType, operation string) []string {
	// Map operations to required permissions
	opType := a.classifyOperationType(operation, "")
	basePermission := strings.ToLower(armType)
	
	switch opType {
	case "list", "get":
		return []string{basePermission + "/read"}
	case "create":
		return []string{basePermission + "/write"}
	case "update":
		return []string{basePermission + "/write"}
	case "delete":
		return []string{basePermission + "/delete"}
	case "action":
		return []string{basePermission + "/action"}
	default:
		return []string{basePermission + "/read"}
	}
}

func (a *AzureSDKAnalyzer) getOperationRateLimits(armType, operation string) map[string]int {
	// Azure ARM API rate limits (requests per minute)
	limits := make(map[string]int)
	
	opType := a.classifyOperationType(operation, "")
	switch opType {
	case "list":
		limits["requests_per_minute"] = 12000 // Higher for read operations
	case "get":
		limits["requests_per_minute"] = 12000
	case "create", "update", "delete":
		limits["requests_per_minute"] = 1200 // Lower for write operations
	default:
		limits["requests_per_minute"] = 6000
	}
	
	return limits
}

func (a *AzureSDKAnalyzer) isResourceGraphOptimal(armType, operation string) bool {
	// Resource Graph is optimal for list operations of most resource types
	opType := a.classifyOperationType(operation, "")
	if opType != "list" {
		return false
	}
	
	// Some resources are not available in Resource Graph
	nonResourceGraphTypes := []string{
		"Microsoft.Authorization/roleAssignments",
		"Microsoft.Insights/metrics", 
		"Microsoft.Insights/logs",
	}
	
	for _, nonRGType := range nonResourceGraphTypes {
		if strings.Contains(armType, nonRGType) {
			return false
		}
	}
	
	return true
}

func (a *AzureSDKAnalyzer) generateResourceGraphQuery(armType string) string {
	// Generate optimal KQL query for Resource Graph
	baseQuery := "Resources"
	
	// Add type filter
	if armType != "" {
		baseQuery += fmt.Sprintf(" | where type =~ '%s'", strings.ToLower(armType))
	}
	
	// Add common projections
	baseQuery += " | project id, name, type, location, resourceGroup, subscriptionId, tags, properties"
	
	// Add resource-specific optimizations
	if strings.Contains(armType, "virtualMachines") {
		baseQuery += ", properties.hardwareProfile.vmSize, properties.provisioningState, properties.powerState"
	} else if strings.Contains(armType, "storageAccounts") {
		baseQuery += ", sku.name, sku.tier, properties.primaryEndpoints"
	} else if strings.Contains(armType, "virtualNetworks") {
		baseQuery += ", properties.addressSpace, properties.subnets"
	}
	
	return baseQuery
}

func (a *AzureSDKAnalyzer) applyPatternValidation(prop *PropertyDef) {
	propLower := strings.ToLower(prop.Name)
	
	// Apply pattern validation based on property name
	switch {
	case strings.Contains(propLower, "id") && strings.Contains(propLower, "resource"):
		prop.Constraints["pattern"] = "resourceId"
		prop.Constraints["format"] = "azure_resource_id"
	case strings.Contains(propLower, "vmsize") || strings.Contains(propLower, "size"):
		prop.Constraints["pattern"] = "vmSize"
	case strings.Contains(propLower, "location"):
		prop.Constraints["pattern"] = "location"
	case strings.Contains(propLower, "storage") && strings.Contains(propLower, "account"):
		prop.Constraints["pattern"] = "storageAccountName"
	}
}

func (a *AzureSDKAnalyzer) generateResourceRelationships() {
	// Analyze relationships between resources
	for serviceName, service := range a.serviceCatalog.Services {
		for _, resource := range service.Resources {
			relationships := a.findResourceRelationships(resource, serviceName)
			a.serviceCatalog.Relationships = append(a.serviceCatalog.Relationships, relationships...)
		}
	}
}

func (a *AzureSDKAnalyzer) findResourceRelationships(resource ResourceDefinition, serviceName string) []ResourceRelationship {
	var relationships []ResourceRelationship
	
	// Common relationship patterns in Azure
	relationshipPatterns := map[string]string{
		"networkInterface": "Microsoft.Network/networkInterfaces",
		"subnet":          "Microsoft.Network/virtualNetworks/subnets", 
		"virtualNetwork":  "Microsoft.Network/virtualNetworks",
		"storageAccount":  "Microsoft.Storage/storageAccounts",
		"keyVault":        "Microsoft.KeyVault/vaults",
		"loadBalancer":    "Microsoft.Network/loadBalancers",
		"publicIP":        "Microsoft.Network/publicIPAddresses",
	}
	
	// Analyze properties for relationship indicators
	for _, prop := range resource.Properties {
		propLower := strings.ToLower(prop.Name)
		
		for pattern, targetType := range relationshipPatterns {
			if strings.Contains(propLower, strings.ToLower(pattern)) && 
			   strings.Contains(propLower, "id") {
				
				relationship := ResourceRelationship{
					SourceType:      resource.ARMType,
					TargetType:      targetType,
					RelationType:    "references",
					Direction:       "unidirectional",
					PropertyPath:    prop.Path,
					Cardinality:     "many_to_one",
					Required:        prop.Required,
					Metadata: map[string]string{
						"discovered_from": "property_analysis",
						"property_name":   prop.Name,
					},
				}
				
				relationships = append(relationships, relationship)
			}
		}
	}
	
	return relationships
}

func (a *AzureSDKAnalyzer) generateResourceGraphQueries() {
	// Generate Resource Graph queries for each resource type
	for serviceName, service := range a.serviceCatalog.Services {
		for i, resource := range service.Resources {
			if resource.ResourceGraphQuery == "" {
				query := a.generateResourceGraphQuery(resource.ARMType)
				service.Resources[i].ResourceGraphQuery = query
				a.resourceGraphQueries[resource.ARMType] = query
			}
		}
		a.serviceCatalog.Services[serviceName] = service
	}
}
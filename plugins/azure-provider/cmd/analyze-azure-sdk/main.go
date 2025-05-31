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

// Analysis result structures
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

type ParameterInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
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
}

type AnalysisSummary struct {
	TotalServices   int `json:"totalServices"`
	TotalResources  int `json:"totalResources"`
	TotalOperations int `json:"totalOperations"`
	AnalysisTimeMs  int `json:"analysisTimeMs"`
}

// Azure SDK analyzer
type AzureSDKAnalyzer struct {
	sdkPath      string
	verbose      bool
	fileSet      *token.FileSet
	serviceMap   map[string]*ServiceAnalysis
	clientTypes  map[string]*ast.StructType
	responseTypes map[string]*ast.StructType
}

func main() {
	flag.Parse()
	
	analyzer := &AzureSDKAnalyzer{
		sdkPath:       *sdkPath,
		verbose:       *verbose,
		fileSet:       token.NewFileSet(),
		serviceMap:    make(map[string]*ServiceAnalysis),
		clientTypes:   make(map[string]*ast.StructType),
		responseTypes: make(map[string]*ast.StructType),
	}
	
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
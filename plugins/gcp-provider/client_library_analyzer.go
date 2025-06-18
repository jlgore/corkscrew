package main

import (
	"context"
	"fmt"
	"go/doc"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ClientLibraryAnalyzer analyzes GCP client libraries to discover resource patterns
type ClientLibraryAnalyzer struct {
	GoPath       string
	ModulePath   string
	ServiceMappings map[string]*ServiceMapping
	AssetInventoryMappings map[string]string
}

// ServiceMapping contains discovered service information
type ServiceMapping struct {
	ServiceName      string                 `json:"service_name"`
	PackagePath      string                 `json:"package_path"`
	ClientName       string                 `json:"client_name"`
	ResourcePatterns []*ResourcePattern     `json:"resource_patterns"`
	IteratorPatterns []*IteratorPattern     `json:"iterator_patterns"`
	AssetMappings    map[string]string      `json:"asset_mappings"`
	ParentHierarchy  []*ParentRelationship  `json:"parent_hierarchy"`
}

// ResourcePattern represents a discoverable resource pattern
type ResourcePattern struct {
	MethodName      string            `json:"method_name"`
	ResourceType    string            `json:"resource_type"`
	ListMethod      bool              `json:"list_method"`
	GetMethod       bool              `json:"get_method"`
	PaginationStyle string            `json:"pagination_style"`
	Parameters      []string          `json:"parameters"`
	ReturnType      string            `json:"return_type"`
	AssetType       string            `json:"asset_type"`
	Metadata        map[string]string `json:"metadata"`
}

// IteratorPattern represents iterator-based resource listing
type IteratorPattern struct {
	ClientMethod    string            `json:"client_method"`
	IteratorType    string            `json:"iterator_type"`
	NextMethod      bool              `json:"next_method"`
	PagesMethod     bool              `json:"pages_method"`
	PageTokenSupport bool             `json:"page_token_support"`
	Metadata        map[string]string `json:"metadata"`
}

// ParentRelationship represents resource hierarchy
type ParentRelationship struct {
	ChildResource  string   `json:"child_resource"`
	ParentResource string   `json:"parent_resource"`
	RequiredParams []string `json:"required_params"`
	Scope          string   `json:"scope"` // "project", "global", "regional", "zonal"
}

// AssetInventoryMetadata holds Asset Inventory specific information
type AssetInventoryMetadata struct {
	AssetType      string            `json:"asset_type"`
	ResourceName   string            `json:"resource_name"`
	LocationType   string            `json:"location_type"` // "global", "regional", "zonal"
	ParentRequired bool              `json:"parent_required"`
	Attributes     map[string]string `json:"attributes"`
}

// NewClientLibraryAnalyzer creates a new analyzer instance
func NewClientLibraryAnalyzer(goPath, modulePath string) *ClientLibraryAnalyzer {
	return &ClientLibraryAnalyzer{
		GoPath:                 goPath,
		ModulePath:             modulePath,
		ServiceMappings:        make(map[string]*ServiceMapping),
		AssetInventoryMappings: make(map[string]string),
	}
}

// AnalyzeGCPLibraries analyzes all GCP client libraries
func (cla *ClientLibraryAnalyzer) AnalyzeGCPLibraries(ctx context.Context) ([]*pb.ServiceInfo, error) {
	log.Printf("Starting GCP client library analysis...")

	// Initialize built-in Asset Inventory mappings
	cla.initializeAssetInventoryMappings()

	// Find GCP client library packages
	gcpPackages, err := cla.findGCPPackages()
	if err != nil {
		return nil, fmt.Errorf("failed to find GCP packages: %w", err)
	}

	log.Printf("Found %d GCP client packages", len(gcpPackages))

	var services []*pb.ServiceInfo

	// Analyze each package
	for _, pkgPath := range gcpPackages {
		service, err := cla.analyzePackage(ctx, pkgPath)
		if err != nil {
			log.Printf("Error analyzing package %s: %v", pkgPath, err)
			continue
		}

		if service != nil {
			services = append(services, service)
		}
	}

	log.Printf("Successfully analyzed %d GCP services", len(services))
	return services, nil
}

// findGCPPackages finds all GCP client library packages
func (cla *ClientLibraryAnalyzer) findGCPPackages() ([]string, error) {
	var packages []string

	// Look for cloud.google.com/go/* packages in GOPATH/pkg/mod
	modPath := filepath.Join(cla.GoPath, "pkg", "mod", "cloud.google.com", "go")
	
	if _, err := os.Stat(modPath); os.IsNotExist(err) {
		// Try alternative locations
		modPath = filepath.Join(os.Getenv("HOME"), "go", "pkg", "mod", "cloud.google.com", "go")
	}

	err := filepath.Walk(modPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on errors
		}

		if info.IsDir() && strings.Contains(path, "@") {
			// Skip version directories
			return filepath.SkipDir
		}

		if strings.HasSuffix(path, ".go") && !strings.Contains(path, "test") {
			// Extract package path
			rel, err := filepath.Rel(modPath, filepath.Dir(path))
			if err == nil && rel != "." {
				packagePath := "cloud.google.com/go/" + rel
				if !contains(packages, packagePath) {
					packages = append(packages, packagePath)
				}
			}
		}

		return nil
	})

	if err != nil || len(packages) == 0 {
		// Fallback to well-known GCP services
		packages = cla.getWellKnownGCPServices()
	}

	return packages, nil
}

// getWellKnownGCPServices returns a list of well-known GCP service packages
func (cla *ClientLibraryAnalyzer) getWellKnownGCPServices() []string {
	return []string{
		"cloud.google.com/go/compute/apiv1",
		"cloud.google.com/go/storage",
		"cloud.google.com/go/bigquery",
		"cloud.google.com/go/pubsub",
		"cloud.google.com/go/container/apiv1",
		"cloud.google.com/go/cloudsql",
		"cloud.google.com/go/run/apiv2",
		"cloud.google.com/go/functions/apiv1",
		"cloud.google.com/go/appengine/apiv1",
		"cloud.google.com/go/dataflow/apiv1beta3",
		"cloud.google.com/go/dataproc/apiv1",
		"cloud.google.com/go/spanner",
		"cloud.google.com/go/firestore",
		"cloud.google.com/go/logging",
		"cloud.google.com/go/monitoring/apiv3",
		"cloud.google.com/go/iam/apiv1",
		"cloud.google.com/go/resourcemanager/apiv3",
		"cloud.google.com/go/dns",
		"cloud.google.com/go/secretmanager/apiv1",
		"cloud.google.com/go/artifactregistry/apiv1",
		"cloud.google.com/go/kms/apiv1",
		"cloud.google.com/go/cloudbuild/apiv1",
		"cloud.google.com/go/cloudtasks/apiv2",
		"cloud.google.com/go/scheduler/apiv1",
	}
}

// analyzePackage analyzes a specific GCP package for resource patterns
func (cla *ClientLibraryAnalyzer) analyzePackage(ctx context.Context, packagePath string) (*pb.ServiceInfo, error) {
	// Extract service name from package path
	serviceName := cla.extractServiceName(packagePath)
	if serviceName == "" {
		return nil, fmt.Errorf("could not extract service name from %s", packagePath)
	}

	log.Printf("Analyzing package: %s (service: %s)", packagePath, serviceName)

	// Create service mapping
	mapping := &ServiceMapping{
		ServiceName:      serviceName,
		PackagePath:      packagePath,
		ResourcePatterns: []*ResourcePattern{},
		IteratorPatterns: []*IteratorPattern{},
		AssetMappings:    make(map[string]string),
		ParentHierarchy:  []*ParentRelationship{},
	}

	// Try to parse package documentation and source
	if err := cla.parsePackageSource(packagePath, mapping); err != nil {
		// If parsing fails, use static patterns
		log.Printf("Package parsing failed for %s, using static patterns: %v", packagePath, err)
		cla.addStaticPatterns(serviceName, mapping)
	}

	// Store the mapping
	cla.ServiceMappings[serviceName] = mapping

	// Convert to ServiceInfo
	serviceInfo := &pb.ServiceInfo{
		Name:           serviceName,
		DisplayName:    cla.getDisplayName(serviceName),
		PackageName:    packagePath,
		ClientType:     "google-cloud-go",
		ResourceTypes:  cla.convertToResourceTypes(mapping),
	}

	return serviceInfo, nil
}

// parsePackageSource parses Go source code to extract patterns
func (cla *ClientLibraryAnalyzer) parsePackageSource(packagePath string, mapping *ServiceMapping) error {
	// Convert package path to file path
	packageDir := strings.Replace(packagePath, "cloud.google.com/go/", "", 1)
	srcPath := filepath.Join(cla.GoPath, "pkg", "mod", "cloud.google.com", "go", packageDir)

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("package source not found at %s", srcPath)
	}

	fset := token.NewFileSet()
	
	// Parse all Go files in the package
	packages, err := parser.ParseDir(fset, srcPath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse package: %w", err)
	}

	for _, pkg := range packages {
		if strings.HasSuffix(pkg.Name, "_test") {
			continue // Skip test packages
		}

		// Extract documentation
		doc := doc.New(pkg, "./", doc.AllDecls)
		
		// Analyze types and functions
		cla.analyzeTypes(doc, mapping)
		cla.analyzeFunctions(doc, mapping)
	}

	return nil
}

// analyzeTypes analyzes type declarations for client and iterator patterns
func (cla *ClientLibraryAnalyzer) analyzeTypes(doc *doc.Package, mapping *ServiceMapping) {
	for _, typeDoc := range doc.Types {
		typeName := typeDoc.Name

		// Look for client types
		if strings.HasSuffix(typeName, "Client") {
			mapping.ClientName = typeName
			log.Printf("Found client type: %s", typeName)
		}

		// Look for iterator types
		if strings.HasSuffix(typeName, "Iterator") {
			iteratorPattern := &IteratorPattern{
				IteratorType: typeName,
				NextMethod:   cla.hasMethod(typeDoc, "Next"),
				PagesMethod:  cla.hasMethod(typeDoc, "Pages"),
				Metadata:     make(map[string]string),
			}
			
			mapping.IteratorPatterns = append(mapping.IteratorPatterns, iteratorPattern)
			log.Printf("Found iterator type: %s", typeName)
		}
	}
}

// analyzeFunctions analyzes function declarations for resource patterns
func (cla *ClientLibraryAnalyzer) analyzeFunctions(doc *doc.Package, mapping *ServiceMapping) {
	for _, funcDoc := range doc.Funcs {
		funcName := funcDoc.Name

		// Skip constructors and utility functions
		if strings.HasPrefix(funcName, "New") || 
		   strings.HasPrefix(funcName, "Default") ||
		   strings.Contains(funcName, "Option") {
			continue
		}

		// Analyze resource operation patterns
		pattern := cla.analyzeResourceMethod(funcName, funcDoc)
		if pattern != nil {
			pattern.AssetType = cla.getAssetType(mapping.ServiceName, pattern.ResourceType)
			mapping.ResourcePatterns = append(mapping.ResourcePatterns, pattern)
			log.Printf("Found resource pattern: %s -> %s", funcName, pattern.ResourceType)
		}
	}
}

// analyzeResourceMethod analyzes a method for resource operation patterns
func (cla *ClientLibraryAnalyzer) analyzeResourceMethod(funcName string, funcDoc *doc.Func) *ResourcePattern {
	// Match List* patterns
	if match := regexp.MustCompile(`^List([A-Z][a-zA-Z]+)s?$`).FindStringSubmatch(funcName); match != nil {
		return &ResourcePattern{
			MethodName:      funcName,
			ResourceType:    match[1],
			ListMethod:      true,
			PaginationStyle: cla.detectPaginationStyle(funcDoc),
			Parameters:      cla.extractParameters(funcDoc),
			ReturnType:      cla.extractReturnType(funcDoc),
			Metadata:        make(map[string]string),
		}
	}

	// Match Get* patterns
	if match := regexp.MustCompile(`^Get([A-Z][a-zA-Z]+)$`).FindStringSubmatch(funcName); match != nil {
		return &ResourcePattern{
			MethodName:   funcName,
			ResourceType: match[1],
			GetMethod:    true,
			Parameters:   cla.extractParameters(funcDoc),
			ReturnType:   cla.extractReturnType(funcDoc),
			Metadata:     make(map[string]string),
		}
	}

	// Match Create*, Update*, Delete* patterns
	for _, prefix := range []string{"Create", "Update", "Delete"} {
		pattern := fmt.Sprintf(`^%s([A-Z][a-zA-Z]+)$`, prefix)
		if match := regexp.MustCompile(pattern).FindStringSubmatch(funcName); match != nil {
			return &ResourcePattern{
				MethodName:   funcName,
				ResourceType: match[1],
				Parameters:   cla.extractParameters(funcDoc),
				ReturnType:   cla.extractReturnType(funcDoc),
				Metadata:     map[string]string{"operation": strings.ToLower(prefix)},
			}
		}
	}

	return nil
}

// addStaticPatterns adds well-known static patterns for services
func (cla *ClientLibraryAnalyzer) addStaticPatterns(serviceName string, mapping *ServiceMapping) {
	staticPatterns := map[string][]*ResourcePattern{
		"compute": {
			{MethodName: "ListInstances", ResourceType: "Instance", ListMethod: true, 
			 PaginationStyle: "pages", AssetType: "compute.googleapis.com/Instance",
			 Parameters: []string{"project", "zone"}, Metadata: map[string]string{"scope": "zonal"}},
			{MethodName: "GetInstance", ResourceType: "Instance", GetMethod: true,
			 AssetType: "compute.googleapis.com/Instance",
			 Parameters: []string{"project", "zone", "instance"}, Metadata: map[string]string{"scope": "zonal"}},
			{MethodName: "ListDisks", ResourceType: "Disk", ListMethod: true,
			 PaginationStyle: "pages", AssetType: "compute.googleapis.com/Disk",
			 Parameters: []string{"project", "zone"}, Metadata: map[string]string{"scope": "zonal"}},
			{MethodName: "ListNetworks", ResourceType: "Network", ListMethod: true,
			 PaginationStyle: "pages", AssetType: "compute.googleapis.com/Network",
			 Parameters: []string{"project"}, Metadata: map[string]string{"scope": "global"}},
			{MethodName: "ListSubnetworks", ResourceType: "Subnetwork", ListMethod: true,
			 PaginationStyle: "pages", AssetType: "compute.googleapis.com/Subnetwork",
			 Parameters: []string{"project", "region"}, Metadata: map[string]string{"scope": "regional"}},
		},
		"storage": {
			{MethodName: "ListBuckets", ResourceType: "Bucket", ListMethod: true,
			 PaginationStyle: "iterator", AssetType: "storage.googleapis.com/Bucket",
			 Parameters: []string{"project"}, Metadata: map[string]string{"scope": "global"}},
			{MethodName: "ListObjects", ResourceType: "Object", ListMethod: true,
			 PaginationStyle: "iterator", AssetType: "storage.googleapis.com/Object",
			 Parameters: []string{"bucket"}, Metadata: map[string]string{"scope": "bucket"}},
		},
		"container": {
			{MethodName: "ListClusters", ResourceType: "Cluster", ListMethod: true,
			 PaginationStyle: "simple", AssetType: "container.googleapis.com/Cluster",
			 Parameters: []string{"project", "location"}, Metadata: map[string]string{"scope": "regional"}},
			{MethodName: "ListNodePools", ResourceType: "NodePool", ListMethod: true,
			 PaginationStyle: "simple", AssetType: "container.googleapis.com/NodePool",
			 Parameters: []string{"project", "location", "cluster"}, Metadata: map[string]string{"scope": "cluster"}},
		},
		"bigquery": {
			{MethodName: "ListDatasets", ResourceType: "Dataset", ListMethod: true,
			 PaginationStyle: "iterator", AssetType: "bigqueryadmin.googleapis.com/Dataset",
			 Parameters: []string{"project"}, Metadata: map[string]string{"scope": "project"}},
			{MethodName: "ListTables", ResourceType: "Table", ListMethod: true,
			 PaginationStyle: "iterator", AssetType: "bigqueryadmin.googleapis.com/Table",
			 Parameters: []string{"dataset"}, Metadata: map[string]string{"scope": "dataset"}},
		},
		"pubsub": {
			{MethodName: "ListTopics", ResourceType: "Topic", ListMethod: true,
			 PaginationStyle: "iterator", AssetType: "pubsub.googleapis.com/Topic",
			 Parameters: []string{"project"}, Metadata: map[string]string{"scope": "project"}},
			{MethodName: "ListSubscriptions", ResourceType: "Subscription", ListMethod: true,
			 PaginationStyle: "iterator", AssetType: "pubsub.googleapis.com/Subscription",
			 Parameters: []string{"project"}, Metadata: map[string]string{"scope": "project"}},
		},
	}

	if patterns, ok := staticPatterns[serviceName]; ok {
		mapping.ResourcePatterns = append(mapping.ResourcePatterns, patterns...)
		log.Printf("Added %d static patterns for service %s", len(patterns), serviceName)
	}
}

// Helper methods

func (cla *ClientLibraryAnalyzer) extractServiceName(packagePath string) string {
	// Extract service name from package path
	parts := strings.Split(packagePath, "/")
	if len(parts) < 3 {
		return ""
	}

	serviceName := parts[len(parts)-1]
	
	// Remove version suffixes
	serviceName = regexp.MustCompile(`v\d+$`).ReplaceAllString(serviceName, "")
	serviceName = regexp.MustCompile(`apiv\d+$`).ReplaceAllString(serviceName, "")
	
	return serviceName
}

func (cla *ClientLibraryAnalyzer) getDisplayName(serviceName string) string {
	displayNames := map[string]string{
		"compute":             "Compute Engine",
		"storage":             "Cloud Storage", 
		"bigquery":            "BigQuery",
		"pubsub":              "Pub/Sub",
		"container":           "Kubernetes Engine",
		"cloudsql":            "Cloud SQL",
		"run":                 "Cloud Run",
		"functions":           "Cloud Functions",
		"appengine":           "App Engine",
		"dataflow":            "Dataflow",
		"dataproc":            "Dataproc",
		"spanner":             "Spanner",
		"firestore":           "Firestore",
		"logging":             "Cloud Logging",
		"monitoring":          "Cloud Monitoring",
		"iam":                 "Identity and Access Management",
		"resourcemanager":     "Resource Manager",
		"dns":                 "Cloud DNS",
		"secretmanager":       "Secret Manager",
		"artifactregistry":    "Artifact Registry",
		"kms":                 "Key Management Service",
		"cloudbuild":          "Cloud Build",
		"cloudtasks":          "Cloud Tasks",
		"scheduler":           "Cloud Scheduler",
	}

	if displayName, ok := displayNames[serviceName]; ok {
		return displayName
	}

	// Capitalize first letter and replace dashes/underscores
	serviceName = strings.ReplaceAll(serviceName, "-", " ")
	serviceName = strings.ReplaceAll(serviceName, "_", " ")
	if len(serviceName) > 0 {
		serviceName = strings.ToUpper(serviceName[:1]) + serviceName[1:]
	}
	
	return serviceName
}

func (cla *ClientLibraryAnalyzer) getAssetType(serviceName, resourceType string) string {
	key := fmt.Sprintf("%s.%s", serviceName, resourceType)
	if assetType, ok := cla.AssetInventoryMappings[key]; ok {
		return assetType
	}

	// Generate default asset type
	return fmt.Sprintf("%s.googleapis.com/%s", serviceName, resourceType)
}

func (cla *ClientLibraryAnalyzer) initializeAssetInventoryMappings() {
	// Initialize well-known Asset Inventory type mappings
	cla.AssetInventoryMappings = map[string]string{
		"compute.Instance":     "compute.googleapis.com/Instance",
		"compute.Disk":         "compute.googleapis.com/Disk",
		"compute.Network":      "compute.googleapis.com/Network",
		"compute.Subnetwork":   "compute.googleapis.com/Subnetwork",
		"compute.Firewall":     "compute.googleapis.com/Firewall",
		"storage.Bucket":       "storage.googleapis.com/Bucket",
		"storage.Object":       "storage.googleapis.com/Object",
		"bigquery.Dataset":     "bigqueryadmin.googleapis.com/Dataset",
		"bigquery.Table":       "bigqueryadmin.googleapis.com/Table",
		"pubsub.Topic":         "pubsub.googleapis.com/Topic",
		"pubsub.Subscription":  "pubsub.googleapis.com/Subscription",
		"container.Cluster":    "container.googleapis.com/Cluster",
		"container.NodePool":   "container.googleapis.com/NodePool",
		"cloudsql.Instance":    "sqladmin.googleapis.com/Instance",
		"run.Service":          "run.googleapis.com/Service",
		"functions.Function":   "cloudfunctions.googleapis.com/Function",
	}
}

func (cla *ClientLibraryAnalyzer) convertToResourceTypes(mapping *ServiceMapping) []*pb.ResourceType {
	var resourceTypes []*pb.ResourceType
	
	// Convert resource patterns to resource types
	seen := make(map[string]bool)
	for _, pattern := range mapping.ResourcePatterns {
		if !seen[pattern.ResourceType] {
			resourceType := &pb.ResourceType{
				Name:     pattern.ResourceType,
				TypeName: pattern.AssetType,
			}
			resourceTypes = append(resourceTypes, resourceType)
			seen[pattern.ResourceType] = true
		}
	}

	return resourceTypes
}

func (cla *ClientLibraryAnalyzer) hasMethod(typeDoc *doc.Type, methodName string) bool {
	for _, method := range typeDoc.Methods {
		if method.Name == methodName {
			return true
		}
	}
	return false
}

func (cla *ClientLibraryAnalyzer) detectPaginationStyle(funcDoc *doc.Func) string {
	// Analyze function signature and documentation to detect pagination style
	if funcDoc == nil {
		return "unknown"
	}

	docText := funcDoc.Doc
	
	if strings.Contains(docText, "Iterator") || strings.Contains(docText, "Next()") {
		return "iterator"
	}
	
	if strings.Contains(docText, "Pages") || strings.Contains(docText, "PageToken") {
		return "pages"
	}
	
	if strings.Contains(docText, "callback") {
		return "callback"
	}

	return "simple"
}

func (cla *ClientLibraryAnalyzer) extractParameters(funcDoc *doc.Func) []string {
	// This would extract parameter names from function signature
	// For now, return empty slice as it requires more complex AST parsing
	return []string{}
}

func (cla *ClientLibraryAnalyzer) extractReturnType(funcDoc *doc.Func) string {
	// This would extract return type from function signature
	// For now, return empty string as it requires more complex AST parsing
	return ""
}

// GenerateAnalysisReport generates a comprehensive analysis report
func (cla *ClientLibraryAnalyzer) GenerateAnalysisReport() map[string]interface{} {
	report := map[string]interface{}{
		"analyzed_at":     time.Now().Format(time.RFC3339),
		"total_services":  len(cla.ServiceMappings),
		"services":        cla.ServiceMappings,
		"asset_mappings":  cla.AssetInventoryMappings,
	}

	// Add summary statistics
	totalPatterns := 0
	totalIterators := 0
	for _, mapping := range cla.ServiceMappings {
		totalPatterns += len(mapping.ResourcePatterns)
		totalIterators += len(mapping.IteratorPatterns)
	}

	report["total_resource_patterns"] = totalPatterns
	report["total_iterator_patterns"] = totalIterators

	return report
}
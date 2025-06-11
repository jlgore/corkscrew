package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// Command line flags
var (
	catalogPath = flag.String("catalog", "", "Path to Azure service catalog JSON file")
	outputDir   = flag.String("output", "generated/", "Output directory for generated scanners")
	templateDir = flag.String("template", "templates/", "Template directory")
	optimize    = flag.Bool("optimize", true, "Enable optimization for scanner generation")
	services    = flag.String("services", "", "Comma-separated list of services to generate (empty = all)")
	verbose     = flag.Bool("verbose", false, "Enable verbose logging")
	mergeWith   = flag.String("merge-with", "", "Merge generated catalog with existing catalog")
)

// ServiceCatalog represents the comprehensive Azure service catalog
type ServiceCatalog struct {
	Services       map[string]ServiceDefinition `json:"services"`
	Relationships  []ResourceRelationship       `json:"relationships"`
	CommonPatterns map[string]Pattern           `json:"commonPatterns"`
	GeneratedAt    time.Time                    `json:"generatedAt"`
	SDKVersion     string                       `json:"sdkVersion"`
	Summary        AnalysisSummary              `json:"summary"`
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
	OperationType       string              `json:"operationType"`
	SupportsResourceGroup bool              `json:"supportsResourceGroup"`
	ResponseType        string              `json:"responseType"`
	Parameters          []ParameterInfo     `json:"parameters"`
	IsPaginated         bool                `json:"isPaginated"`
	PaginationType      string              `json:"paginationType"`
	RequiresAuth        []string            `json:"requiresAuth"`
	RateLimits          map[string]int      `json:"rateLimits"`
	ResourceGraphOptimal bool               `json:"resourceGraphOptimal"`
	Metadata            map[string]string   `json:"metadata"`
}

type PropertyDef struct {
	Name        string                 `json:"name"`
	Path        string                 `json:"path"`
	Type        string                 `json:"type"`
	DuckDBType  string                 `json:"duckdbType"`
	Required    bool                   `json:"required"`
	Indexed     bool                   `json:"indexed"`
	Compressed  bool                   `json:"compressed"`
	Description string                 `json:"description"`
	Examples    []interface{}          `json:"examples"`
	Constraints map[string]interface{} `json:"constraints"`
}

type TypeDefinition struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Fields      []FieldDefinition      `json:"fields"`
	UsedBy      []string               `json:"usedBy"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type FieldDefinition struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Description string        `json:"description"`
	Required    bool          `json:"required"`
	Examples    []interface{} `json:"examples"`
}

type ResourceRelationship struct {
	SourceType      string            `json:"sourceType"`
	TargetType      string            `json:"targetType"`
	RelationType    string            `json:"relationType"`
	Direction       string            `json:"direction"`
	PropertyPath    string            `json:"propertyPath"`
	Cardinality     string            `json:"cardinality"`
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
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Required     bool                   `json:"required"`
	Description  string                 `json:"description"`
	DefaultValue interface{}            `json:"defaultValue"`
	Constraints  map[string]interface{} `json:"constraints"`
}

type AnalysisSummary struct {
	TotalServices      int `json:"totalServices"`
	TotalResources     int `json:"totalResources"`
	TotalOperations    int `json:"totalOperations"`
	TotalRelationships int `json:"totalRelationships"`
	AnalysisTimeMs     int `json:"analysisTimeMs"`
}

// ScannerGenerator generates optimized scanners from service catalog
type ScannerGenerator struct {
	catalog         *ServiceCatalog
	outputDir       string
	templateDir     string
	optimize        bool
	verbose         bool
	generatedFiles  []string
	templates       map[string]*template.Template
	helperFunctions template.FuncMap
}

// GeneratedScanner represents a generated scanner file
type GeneratedScanner struct {
	Name        string
	Path        string
	Type        string // service, resource, batch, relationship
	Service     string
	ResourceType string
	FileSize    int64
	Generated   time.Time
}

func main() {
	flag.Parse()
	
	if *catalogPath == "" {
		log.Fatal("catalog path is required")
	}
	
	log.Printf("🔧 Starting Azure Scanner Generator")
	startTime := time.Now()
	
	// Load service catalog
	catalog, err := loadServiceCatalog(*catalogPath)
	if err != nil {
		log.Fatalf("Failed to load service catalog: %v", err)
	}
	
	// Merge with existing catalog if specified
	if *mergeWith != "" {
		existingCatalog, err := loadServiceCatalog(*mergeWith)
		if err != nil {
			log.Printf("Warning: Failed to load existing catalog for merging: %v", err)
		} else {
			catalog = mergeCatalogs(catalog, existingCatalog)
			log.Printf("Merged with existing catalog")
		}
	}
	
	log.Printf("📋 Loaded catalog with %d services, %d resources", 
		len(catalog.Services), catalog.Summary.TotalResources)
	
	// Create scanner generator
	generator := &ScannerGenerator{
		catalog:     catalog,
		outputDir:   *outputDir,
		templateDir: *templateDir,
		optimize:    *optimize,
		verbose:     *verbose,
		templates:   make(map[string]*template.Template),
	}
	
	// Initialize templates and helper functions
	if err := generator.initializeTemplates(); err != nil {
		log.Fatalf("Failed to initialize templates: %v", err)
	}
	
	// Parse services filter
	var targetServices []string
	if *services != "" {
		targetServices = strings.Split(*services, ",")
		for i, s := range targetServices {
			targetServices[i] = strings.TrimSpace(s)
		}
	}
	
	// Generate scanners
	generatedScanners, err := generator.GenerateScanners(targetServices)
	if err != nil {
		log.Fatalf("Failed to generate scanners: %v", err)
	}
	
	generationTime := time.Since(startTime)
	
	// Write generation report
	reportPath := filepath.Join(*outputDir, "generation_report.json")
	if err := generator.writeGenerationReport(generatedScanners, generationTime, reportPath); err != nil {
		log.Printf("Warning: Failed to write generation report: %v", err)
	}
	
	log.Printf("✅ Scanner generation complete in %v", generationTime)
	log.Printf("📊 Generated %d scanners across %d services", 
		len(generatedScanners), len(targetServices))
	log.Printf("📄 Generation report: %s", reportPath)
}

// loadServiceCatalog loads and parses the service catalog JSON
func loadServiceCatalog(path string) (*ServiceCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read catalog file: %w", err)
	}
	
	var catalog ServiceCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("failed to parse catalog JSON: %w", err)
	}
	
	return &catalog, nil
}

// mergeCatalogs merges two service catalogs
func mergeCatalogs(new, existing *ServiceCatalog) *ServiceCatalog {
	merged := &ServiceCatalog{
		Services:       make(map[string]ServiceDefinition),
		Relationships:  []ResourceRelationship{},
		CommonPatterns: make(map[string]Pattern),
		GeneratedAt:    time.Now(),
		SDKVersion:     new.SDKVersion,
	}
	
	// Merge services
	for name, service := range existing.Services {
		merged.Services[name] = service
	}
	for name, service := range new.Services {
		merged.Services[name] = service // New services override existing
	}
	
	// Merge relationships
	merged.Relationships = append(merged.Relationships, existing.Relationships...)
	merged.Relationships = append(merged.Relationships, new.Relationships...)
	
	// Merge patterns
	for name, pattern := range existing.CommonPatterns {
		merged.CommonPatterns[name] = pattern
	}
	for name, pattern := range new.CommonPatterns {
		merged.CommonPatterns[name] = pattern
	}
	
	// Update summary
	totalResources := 0
	totalOperations := 0
	for _, service := range merged.Services {
		totalResources += len(service.Resources)
		for _, resource := range service.Resources {
			totalOperations += len(resource.Operations)
		}
	}
	
	merged.Summary = AnalysisSummary{
		TotalServices:      len(merged.Services),
		TotalResources:     totalResources,
		TotalOperations:    totalOperations,
		TotalRelationships: len(merged.Relationships),
	}
	
	return merged
}

// initializeTemplates loads and parses scanner templates
func (g *ScannerGenerator) initializeTemplates() error {
	g.helperFunctions = template.FuncMap{
		"toLower":     strings.ToLower,
		"toUpper":     strings.ToUpper,
		"toTitle":     strings.Title,
		"toCamel":     g.toCamelCase,
		"toPascal":    g.toPascalCase,
		"toSnake":     g.toSnakeCase,
		"join":        strings.Join,
		"replace":     strings.ReplaceAll,
		"contains":    strings.Contains,
		"hasPrefix":   strings.HasPrefix,
		"hasSuffix":   strings.HasSuffix,
		"formatTime":  g.formatTime,
		"formatJSON":  g.formatJSON,
		"quote":       g.quote,
		"escape":      g.escapeString,
		"indent":      g.indentString,
		"comment":     g.commentString,
		"resourceName": g.generateResourceName,
		"packageName": g.generatePackageName,
		"clientName":  g.generateClientName,
	}
	
	// Load template files
	templateFiles := map[string]string{
		"service":      "azure-service-scanner.tmpl",
		"resource":     "azure-resource-scanner.tmpl", 
		"batch":        "azure-batch-scanner.tmpl",
		"relationship": "azure-relationship-scanner.tmpl",
		"master":       "azure-master-scanner.tmpl",
	}
	
	for name, filename := range templateFiles {
		templatePath := filepath.Join(g.templateDir, filename)
		
		// Create default templates if they don't exist
		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			if err := g.createDefaultTemplate(templatePath, name); err != nil {
				log.Printf("Warning: Failed to create default template %s: %v", name, err)
				continue
			}
		}
		
		tmpl, err := template.New(filename).Funcs(g.helperFunctions).ParseFiles(templatePath)
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", name, err)
		}
		
		g.templates[name] = tmpl
	}
	
	return nil
}

// GenerateScanners generates all types of scanners from the catalog
func (g *ScannerGenerator) GenerateScanners(targetServices []string) ([]GeneratedScanner, error) {
	var allScanners []GeneratedScanner
	
	// Create output directory
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	
	// Filter services if specified
	servicesToGenerate := make(map[string]ServiceDefinition)
	if len(targetServices) > 0 {
		for _, serviceName := range targetServices {
			if service, exists := g.catalog.Services[serviceName]; exists {
				servicesToGenerate[serviceName] = service
			} else {
				log.Printf("Warning: Service %s not found in catalog", serviceName)
			}
		}
	} else {
		servicesToGenerate = g.catalog.Services
	}
	
	log.Printf("🔧 Generating scanners for %d services", len(servicesToGenerate))
	
	// Generate service-level scanners
	for serviceName, service := range servicesToGenerate {
		if g.verbose {
			log.Printf("  📦 Generating service scanner: %s", serviceName)
		}
		
		scanners, err := g.generateServiceScanner(service)
		if err != nil {
			log.Printf("Warning: Failed to generate service scanner for %s: %v", serviceName, err)
			continue
		}
		allScanners = append(allScanners, scanners...)
		
		// Generate resource-specific scanners
		for _, resource := range service.Resources {
			if g.verbose {
				log.Printf("    🔍 Generating resource scanner: %s", resource.Type)
			}
			
			resourceScanners, err := g.generateResourceScanner(resource, service)
			if err != nil {
				log.Printf("Warning: Failed to generate resource scanner for %s: %v", resource.Type, err)
				continue
			}
			allScanners = append(allScanners, resourceScanners...)
		}
		
		// Generate relationship scanners if service has relationships
		relationships := g.findServiceRelationships(service)
		if len(relationships) > 0 {
			if g.verbose {
				log.Printf("    🔗 Generating relationship scanner: %s", serviceName)
			}
			
			relScanners, err := g.generateRelationshipScanner(service, relationships)
			if err != nil {
				log.Printf("Warning: Failed to generate relationship scanner for %s: %v", serviceName, err)
			} else {
				allScanners = append(allScanners, relScanners...)
			}
		}
	}
	
	// Generate master batch scanner
	if g.verbose {
		log.Printf("  🚀 Generating master batch scanner")
	}
	
	batchScanners, err := g.generateBatchScanner(servicesToGenerate)
	if err != nil {
		log.Printf("Warning: Failed to generate batch scanner: %v", err)
	} else {
		allScanners = append(allScanners, batchScanners...)
	}
	
	return allScanners, nil
}

// generateServiceScanner generates a service-level scanner
func (g *ScannerGenerator) generateServiceScanner(service ServiceDefinition) ([]GeneratedScanner, error) {
	filename := fmt.Sprintf("%s_service_scanner.go", g.toSnakeCase(service.Name))
	outputPath := filepath.Join(g.outputDir, filename)
	
	data := struct {
		Service       ServiceDefinition
		GeneratedAt   time.Time
		GeneratorInfo string
		Optimize      bool
	}{
		Service:       service,
		GeneratedAt:   time.Now(),
		GeneratorInfo: "Azure Scanner Generator v1.0.0",
		Optimize:      g.optimize,
	}
	
	content, err := g.executeTemplate("service", data)
	if err != nil {
		return nil, err
	}
	
	if err := g.writeFormattedGoFile(outputPath, content); err != nil {
		return nil, err
	}
	
	scanner := GeneratedScanner{
		Name:        fmt.Sprintf("%sServiceScanner", g.toPascalCase(service.Name)),
		Path:        outputPath,
		Type:        "service",
		Service:     service.Name,
		Generated:   time.Now(),
	}
	
	if stat, err := os.Stat(outputPath); err == nil {
		scanner.FileSize = stat.Size()
	}
	
	return []GeneratedScanner{scanner}, nil
}

// generateResourceScanner generates resource-specific scanners with optimizations
func (g *ScannerGenerator) generateResourceScanner(resource ResourceDefinition, service ServiceDefinition) ([]GeneratedScanner, error) {
	filename := fmt.Sprintf("%s_%s_scanner.go", 
		g.toSnakeCase(service.Name), g.toSnakeCase(resource.Type))
	outputPath := filepath.Join(g.outputDir, filename)
	
	data := struct {
		Resource         ResourceDefinition
		Service          ServiceDefinition
		GeneratedAt      time.Time
		GeneratorInfo    string
		Optimize         bool
		UseResourceGraph bool
		BatchSize        int
		RetryConfig      map[string]interface{}
	}{
		Resource:         resource,
		Service:          service,
		GeneratedAt:      time.Now(),
		GeneratorInfo:    "Azure Scanner Generator v1.0.0",
		Optimize:         g.optimize,
		UseResourceGraph: g.shouldUseResourceGraph(resource),
		BatchSize:        g.calculateOptimalBatchSize(resource),
		RetryConfig:      g.generateRetryConfig(resource),
	}
	
	content, err := g.executeTemplate("resource", data)
	if err != nil {
		return nil, err
	}
	
	if err := g.writeFormattedGoFile(outputPath, content); err != nil {
		return nil, err
	}
	
	scanner := GeneratedScanner{
		Name:         fmt.Sprintf("%s%sScanner", g.toPascalCase(service.Name), g.toPascalCase(resource.Type)),
		Path:         outputPath,
		Type:         "resource",
		Service:      service.Name,
		ResourceType: resource.Type,
		Generated:    time.Now(),
	}
	
	if stat, err := os.Stat(outputPath); err == nil {
		scanner.FileSize = stat.Size()
	}
	
	return []GeneratedScanner{scanner}, nil
}

// generateBatchScanner generates master batch scanner for multiple resources
func (g *ScannerGenerator) generateBatchScanner(services map[string]ServiceDefinition) ([]GeneratedScanner, error) {
	filename := "azure_batch_scanner.go"
	outputPath := filepath.Join(g.outputDir, filename)
	
	data := struct {
		Services      map[string]ServiceDefinition
		Catalog       *ServiceCatalog
		GeneratedAt   time.Time
		GeneratorInfo string
		Optimize      bool
		MaxConcurrency int
		BatchConfig   map[string]interface{}
	}{
		Services:       services,
		Catalog:        g.catalog,
		GeneratedAt:    time.Now(),
		GeneratorInfo:  "Azure Scanner Generator v1.0.0",
		Optimize:       g.optimize,
		MaxConcurrency: g.calculateOptimalConcurrency(),
		BatchConfig:    g.generateBatchConfig(),
	}
	
	content, err := g.executeTemplate("batch", data)
	if err != nil {
		return nil, err
	}
	
	if err := g.writeFormattedGoFile(outputPath, content); err != nil {
		return nil, err
	}
	
	scanner := GeneratedScanner{
		Name:      "AzureBatchScanner",
		Path:      outputPath,
		Type:      "batch",
		Service:   "all",
		Generated: time.Now(),
	}
	
	if stat, err := os.Stat(outputPath); err == nil {
		scanner.FileSize = stat.Size()
	}
	
	return []GeneratedScanner{scanner}, nil
}

// generateRelationshipScanner generates relationship scanners
func (g *ScannerGenerator) generateRelationshipScanner(service ServiceDefinition, relationships []ResourceRelationship) ([]GeneratedScanner, error) {
	filename := fmt.Sprintf("%s_relationships_scanner.go", g.toSnakeCase(service.Name))
	outputPath := filepath.Join(g.outputDir, filename)
	
	data := struct {
		Service       ServiceDefinition
		Relationships []ResourceRelationship
		GeneratedAt   time.Time
		GeneratorInfo string
		Optimize      bool
	}{
		Service:       service,
		Relationships: relationships,
		GeneratedAt:   time.Now(),
		GeneratorInfo: "Azure Scanner Generator v1.0.0",
		Optimize:      g.optimize,
	}
	
	content, err := g.executeTemplate("relationship", data)
	if err != nil {
		return nil, err
	}
	
	if err := g.writeFormattedGoFile(outputPath, content); err != nil {
		return nil, err
	}
	
	scanner := GeneratedScanner{
		Name:      fmt.Sprintf("%sRelationshipScanner", g.toPascalCase(service.Name)),
		Path:      outputPath,
		Type:      "relationship",
		Service:   service.Name,
		Generated: time.Now(),
	}
	
	if stat, err := os.Stat(outputPath); err == nil {
		scanner.FileSize = stat.Size()
	}
	
	return []GeneratedScanner{scanner}, nil
}

// Helper methods for optimization and configuration

func (g *ScannerGenerator) shouldUseResourceGraph(resource ResourceDefinition) bool {
	for _, op := range resource.Operations {
		if op.ResourceGraphOptimal && op.OperationType == "list" {
			return true
		}
	}
	return false
}

func (g *ScannerGenerator) calculateOptimalBatchSize(resource ResourceDefinition) int {
	// Calculate optimal batch size based on resource type and operations
	baseSize := 100
	
	// Adjust based on operation rate limits
	for _, op := range resource.Operations {
		if limit, exists := op.RateLimits["requests_per_minute"]; exists {
			// Adjust batch size to stay within rate limits
			if limit < 6000 {
				baseSize = 50 // Lower batch size for rate-limited operations
			}
		}
	}
	
	// Adjust based on resource complexity
	if len(resource.Properties) > 50 {
		baseSize = 75 // Smaller batches for complex resources
	}
	
	return baseSize
}

func (g *ScannerGenerator) calculateOptimalConcurrency() int {
	// Calculate optimal concurrency based on catalog size
	totalResources := g.catalog.Summary.TotalResources
	
	switch {
	case totalResources < 10:
		return 2
	case totalResources < 50:
		return 5
	case totalResources < 100:
		return 10
	default:
		return 15
	}
}

func (g *ScannerGenerator) generateRetryConfig(resource ResourceDefinition) map[string]interface{} {
	config := map[string]interface{}{
		"maxRetries":    3,
		"baseDelay":     "1s",
		"maxDelay":      "30s",
		"backoffFactor": 2.0,
	}
	
	// Adjust retry config based on operation characteristics
	for _, op := range resource.Operations {
		if op.OperationType == "list" && op.IsPaginated {
			config["maxRetries"] = 5 // More retries for paginated operations
		}
		if limit, exists := op.RateLimits["requests_per_minute"]; exists && limit < 1200 {
			config["baseDelay"] = "2s" // Longer delays for rate-limited operations
		}
	}
	
	return config
}

func (g *ScannerGenerator) generateBatchConfig() map[string]interface{} {
	return map[string]interface{}{
		"maxConcurrentServices": g.calculateOptimalConcurrency(),
		"maxConcurrentResources": 10,
		"batchTimeout":          "10m",
		"enableStreaming":       true,
		"enableCaching":         true,
		"cacheTTL":             "1h",
	}
}

func (g *ScannerGenerator) findServiceRelationships(service ServiceDefinition) []ResourceRelationship {
	var relationships []ResourceRelationship
	
	for _, rel := range g.catalog.Relationships {
		// Check if any resource in this service is involved in the relationship
		for _, resource := range service.Resources {
			if rel.SourceType == resource.ARMType || rel.TargetType == resource.ARMType {
				relationships = append(relationships, rel)
				break
			}
		}
	}
	
	return relationships
}

// Template execution and file writing

func (g *ScannerGenerator) executeTemplate(templateName string, data interface{}) (string, error) {
	tmpl, exists := g.templates[templateName]
	if !exists {
		return "", fmt.Errorf("template %s not found", templateName)
	}
	
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", templateName, err)
	}
	
	return buf.String(), nil
}

func (g *ScannerGenerator) writeFormattedGoFile(path, content string) error {
	// Format Go code
	formatted, err := format.Source([]byte(content))
	if err != nil {
		// If formatting fails, write unformatted code with warning
		log.Printf("Warning: Failed to format Go code for %s: %v", path, err)
		formatted = []byte(content)
	}
	
	return os.WriteFile(path, formatted, 0644)
}

func (g *ScannerGenerator) writeGenerationReport(scanners []GeneratedScanner, duration time.Duration, reportPath string) error {
	report := struct {
		GeneratedAt    time.Time          `json:"generatedAt"`
		Duration       string             `json:"duration"`
		TotalScanners  int                `json:"totalScanners"`
		ScannersByType map[string]int     `json:"scannersByType"`
		Scanners       []GeneratedScanner `json:"scanners"`
		Catalog        struct {
			Services      int `json:"services"`
			Resources     int `json:"resources"`
			Operations    int `json:"operations"`
			Relationships int `json:"relationships"`
		} `json:"catalog"`
	}{
		GeneratedAt:   time.Now(),
		Duration:      duration.String(),
		TotalScanners: len(scanners),
		ScannersByType: make(map[string]int),
		Scanners:      scanners,
	}
	
	// Count scanners by type
	for _, scanner := range scanners {
		report.ScannersByType[scanner.Type]++
	}
	
	// Add catalog statistics
	report.Catalog.Services = g.catalog.Summary.TotalServices
	report.Catalog.Resources = g.catalog.Summary.TotalResources
	report.Catalog.Operations = g.catalog.Summary.TotalOperations
	report.Catalog.Relationships = g.catalog.Summary.TotalRelationships
	
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(reportPath, data, 0644)
}

// Helper functions for templates

func (g *ScannerGenerator) toCamelCase(s string) string {
	if s == "" {
		return s
	}
	parts := strings.FieldsFunc(s, func(c rune) bool {
		return c == '_' || c == '-' || c == ' ' || c == '.'
	})
	
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.Title(parts[i])
	}
	
	return strings.Join(parts, "")
}

func (g *ScannerGenerator) toPascalCase(s string) string {
	if s == "" {
		return s
	}
	camel := g.toCamelCase(s)
	if len(camel) > 0 {
		return strings.ToUpper(camel[:1]) + camel[1:]
	}
	return camel
}

func (g *ScannerGenerator) toSnakeCase(s string) string {
	if s == "" {
		return s
	}
	
	var result strings.Builder
	for i, r := range s {
		if i > 0 && (r >= 'A' && r <= 'Z') {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	
	return strings.ToLower(result.String())
}

func (g *ScannerGenerator) formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05 UTC")
}

func (g *ScannerGenerator) formatJSON(v interface{}) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return string(data)
}

func (g *ScannerGenerator) quote(s string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(s, `"`, `\"`))
}

func (g *ScannerGenerator) escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func (g *ScannerGenerator) indentString(s string, indent int) string {
	prefix := strings.Repeat("\t", indent)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

func (g *ScannerGenerator) commentString(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = "// " + line
	}
	return strings.Join(lines, "\n")
}

func (g *ScannerGenerator) generateResourceName(armType string) string {
	parts := strings.Split(armType, "/")
	if len(parts) >= 2 {
		return g.toPascalCase(parts[len(parts)-1])
	}
	return g.toPascalCase(armType)
}

func (g *ScannerGenerator) generatePackageName(namespace string) string {
	return strings.ToLower(strings.TrimPrefix(namespace, "Microsoft."))
}

func (g *ScannerGenerator) generateClientName(service ServiceDefinition) string {
	return g.toPascalCase(service.Name) + "Client"
}

// createDefaultTemplate creates default templates if they don't exist
func (g *ScannerGenerator) createDefaultTemplate(templatePath, templateType string) error {
	templateContent := g.getDefaultTemplateContent(templateType)
	
	dir := filepath.Dir(templatePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	
	return os.WriteFile(templatePath, []byte(templateContent), 0644)
}

func (g *ScannerGenerator) getDefaultTemplateContent(templateType string) string {
	switch templateType {
	case "service":
		return `// Code generated by Azure Scanner Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt | formatTime}}
// Generator: {{.GeneratorInfo}}

package generated

import (
	"context"
	"fmt"
	"time"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// {{.Service.Name | toPascal}}ServiceScanner scans all resources in the {{.Service.Name}} service
type {{.Service.Name | toPascal}}ServiceScanner struct {
	credential     azcore.TokenCredential
	subscriptionID string
	resourceGraph  ResourceGraphClient
	{{range .Service.Resources}}{{.Type | toCamel}}Scanner *{{$.Service.Name | toPascal}}{{.Type | toPascal}}Scanner
	{{end}}
}

// New{{.Service.Name | toPascal}}ServiceScanner creates a new service scanner
func New{{.Service.Name | toPascal}}ServiceScanner(cred azcore.TokenCredential, subscriptionID string) *{{.Service.Name | toPascal}}ServiceScanner {
	return &{{.Service.Name | toPascal}}ServiceScanner{
		credential:     cred,
		subscriptionID: subscriptionID,
	}
}

// ScanAll scans all resources in the {{.Service.Name}} service
func (s *{{.Service.Name | toPascal}}ServiceScanner) ScanAll(ctx context.Context) ([]*pb.Resource, error) {
	var allResources []*pb.Resource
	
	{{range .Service.Resources}}
	// Scan {{.Type}} resources
	{{.Type | toCamel}}Resources, err := s.{{.Type | toCamel}}Scanner.Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to scan {{.Type}}: %w", err)
	}
	allResources = append(allResources, {{.Type | toCamel}}Resources...)
	{{end}}
	
	return allResources, nil
}
`

	case "resource":
		return `// Code generated by Azure Scanner Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt | formatTime}}
// Generator: {{.GeneratorInfo}}

package generated

import (
	"context"
	"fmt"
	"time"
	"strings"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// {{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner scans {{.Resource.Type}} resources
type {{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner struct {
	credential     azcore.TokenCredential
	subscriptionID string
	{{if .UseResourceGraph}}resourceGraph  ResourceGraphClient{{end}}
	client         interface{} // TODO: Replace with actual client type
	batchSize      int
	retryConfig    RetryConfig
}

// New{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner creates a new resource scanner
func New{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner(cred azcore.TokenCredential, subscriptionID string) *{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner {
	return &{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner{
		credential:     cred,
		subscriptionID: subscriptionID,
		batchSize:      {{.BatchSize}},
	}
}

// Scan scans {{.Resource.Type}} resources
func (s *{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner) Scan(ctx context.Context) ([]*pb.Resource, error) {
	{{if .UseResourceGraph}}
	// Try Resource Graph first for optimal performance
	if resources, err := s.scanWithResourceGraph(ctx); err == nil {
		return resources, nil
	}
	{{end}}
	
	// Fall back to ARM API scanning
	return s.scanWithARM(ctx)
}

{{if .UseResourceGraph}}
// scanWithResourceGraph uses Azure Resource Graph for efficient scanning
func (s *{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner) scanWithResourceGraph(ctx context.Context) ([]*pb.Resource, error) {
	query := {{.Resource.ResourceGraphQuery | quote}}
	
	results, err := s.resourceGraph.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("resource graph query failed: %w", err)
	}
	
	var resources []*pb.Resource
	for _, result := range results {
		resource := s.convertResourceGraphResult(result)
		resources = append(resources, resource)
	}
	
	return resources, nil
}
{{end}}

// scanWithARM uses ARM API for scanning
func (s *{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner) scanWithARM(ctx context.Context) ([]*pb.Resource, error) {
	// TODO: Implement ARM API scanning
	return []*pb.Resource{}, nil
}

// convertResourceGraphResult converts Resource Graph result to pb.Resource
func (s *{{.Service.Name | toPascal}}{{.Resource.Type | toPascal}}Scanner) convertResourceGraphResult(result map[string]interface{}) *pb.Resource {
	resource := &pb.Resource{
		Provider:     "azure",
		Service:      {{.Service.Name | quote}},
		Type:         {{.Resource.ARMType | quote}},
		DiscoveredAt: timestamppb.Now(),
		Tags:         make(map[string]string),
	}
	
	// Extract standard fields
	if id, ok := result["id"].(string); ok {
		resource.Id = id
	}
	if name, ok := result["name"].(string); ok {
		resource.Name = name
	}
	if location, ok := result["location"].(string); ok {
		resource.Region = location
	}
	
	// Extract tags
	if tags, ok := result["tags"].(map[string]interface{}); ok {
		for key, value := range tags {
			if strValue, ok := value.(string); ok {
				resource.Tags[key] = strValue
			}
		}
	}
	
	// Store raw data
	if properties, ok := result["properties"]; ok {
		resource.RawData = fmt.Sprintf("%v", properties)
	}
	
	return resource
}

type RetryConfig struct {
	MaxRetries    int
	BaseDelay     string
	MaxDelay      string
	BackoffFactor float64
}
`

	case "batch":
		return `// Code generated by Azure Scanner Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt | formatTime}}
// Generator: {{.GeneratorInfo}}

package generated

import (
	"context"
	"fmt"
	"sync"
	"time"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// AzureBatchScanner performs batch scanning of multiple Azure services
type AzureBatchScanner struct {
	credential        azcore.TokenCredential
	subscriptionID    string
	maxConcurrency    int
	scanners          map[string]ServiceScanner
	enableStreaming   bool
	enableCaching     bool
	cacheTimeout      time.Duration
}

// ServiceScanner interface for service-level scanners
type ServiceScanner interface {
	ScanAll(ctx context.Context) ([]*pb.Resource, error)
	GetServiceName() string
}

// NewAzureBatchScanner creates a new batch scanner
func NewAzureBatchScanner(cred azcore.TokenCredential, subscriptionID string) *AzureBatchScanner {
	scanner := &AzureBatchScanner{
		credential:     cred,
		subscriptionID: subscriptionID,
		maxConcurrency: {{.MaxConcurrency}},
		scanners:       make(map[string]ServiceScanner),
		enableStreaming: true,
		enableCaching:  true,
		cacheTimeout:   time.Hour,
	}
	
	// Initialize service scanners
	{{range $name, $service := .Services}}
	scanner.scanners[{{$name | quote}}] = New{{$service.Name | toPascal}}ServiceScanner(cred, subscriptionID)
	{{end}}
	
	return scanner
}

// ScanAll scans all configured services
func (s *AzureBatchScanner) ScanAll(ctx context.Context) ([]*pb.Resource, error) {
	return s.ScanServices(ctx, nil) // Scan all services
}

// ScanServices scans specified services (nil = all services)
func (s *AzureBatchScanner) ScanServices(ctx context.Context, serviceNames []string) ([]*pb.Resource, error) {
	var servicesToScan []ServiceScanner
	
	if serviceNames == nil {
		// Scan all services
		for _, scanner := range s.scanners {
			servicesToScan = append(servicesToScan, scanner)
		}
	} else {
		// Scan specified services
		for _, name := range serviceNames {
			if scanner, exists := s.scanners[name]; exists {
				servicesToScan = append(servicesToScan, scanner)
			}
		}
	}
	
	// Parallel scanning with concurrency control
	semaphore := make(chan struct{}, s.maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResources []*pb.Resource
	var errors []error
	
	for _, scanner := range servicesToScan {
		wg.Add(1)
		go func(sc ServiceScanner) {
			defer wg.Done()
			
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release
			
			resources, err := sc.ScanAll(ctx)
			
			mu.Lock()
			if err != nil {
				errors = append(errors, fmt.Errorf("service %s: %w", sc.GetServiceName(), err))
			} else {
				allResources = append(allResources, resources...)
			}
			mu.Unlock()
		}(scanner)
	}
	
	wg.Wait()
	
	if len(errors) > 0 {
		return allResources, fmt.Errorf("batch scan completed with %d errors: %v", len(errors), errors[0])
	}
	
	return allResources, nil
}

// StreamScan streams resources as they are discovered
func (s *AzureBatchScanner) StreamScan(ctx context.Context, serviceNames []string, resourceChan chan<- *pb.Resource) error {
	if !s.enableStreaming {
		return fmt.Errorf("streaming not enabled")
	}
	
	// Implementation would stream resources as they are discovered
	// This is a placeholder for the actual streaming implementation
	resources, err := s.ScanServices(ctx, serviceNames)
	if err != nil {
		return err
	}
	
	for _, resource := range resources {
		select {
		case resourceChan <- resource:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	
	return nil
}
`

	case "relationship":
		return `// Code generated by Azure Scanner Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt | formatTime}}
// Generator: {{.GeneratorInfo}}

package generated

import (
	"context"
	"fmt"
	"strings"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// {{.Service.Name | toPascal}}RelationshipScanner scans relationships for {{.Service.Name}} resources
type {{.Service.Name | toPascal}}RelationshipScanner struct {
	resourceGraph ResourceGraphClient
	relationships []ResourceRelationship
}

// ResourceRelationship represents a relationship between resources
type ResourceRelationship struct {
	SourceType   string
	TargetType   string
	RelationType string
	PropertyPath string
}

// New{{.Service.Name | toPascal}}RelationshipScanner creates a new relationship scanner
func New{{.Service.Name | toPascal}}RelationshipScanner(resourceGraph ResourceGraphClient) *{{.Service.Name | toPascal}}RelationshipScanner {
	return &{{.Service.Name | toPascal}}RelationshipScanner{
		resourceGraph: resourceGraph,
		relationships: []ResourceRelationship{
			{{range .Relationships}}
			{
				SourceType:   {{.SourceType | quote}},
				TargetType:   {{.TargetType | quote}},
				RelationType: {{.RelationType | quote}},
				PropertyPath: {{.PropertyPath | quote}},
			},
			{{end}}
		},
	}
}

// ScanRelationships discovers and returns resource relationships
func (s *{{.Service.Name | toPascal}}RelationshipScanner) ScanRelationships(ctx context.Context, resourceId string) ([]*pb.Relationship, error) {
	var relationships []*pb.Relationship
	
	for _, rel := range s.relationships {
		relatedResources, err := s.findRelatedResources(ctx, resourceId, rel)
		if err != nil {
			continue // Skip failed relationship queries
		}
		
		for _, related := range relatedResources {
			relationship := &pb.Relationship{
				TargetId:         related,
				TargetType:       rel.TargetType,
				RelationshipType: rel.RelationType,
			}
			relationships = append(relationships, relationship)
		}
	}
	
	return relationships, nil
}

// findRelatedResources finds resources related through the specified relationship
func (s *{{.Service.Name | toPascal}}RelationshipScanner) findRelatedResources(ctx context.Context, resourceId string, rel ResourceRelationship) ([]string, error) {
	// Build KQL query to find related resources
	query := fmt.Sprintf(` + "`" + `
		Resources
		| where id =~ '%s'
		| project %s
		| where isnotempty(%s)
	` + "`" + `, resourceId, rel.PropertyPath, rel.PropertyPath)
	
	results, err := s.resourceGraph.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	
	var relatedIds []string
	for _, result := range results {
		if id, ok := result[rel.PropertyPath].(string); ok && id != "" {
			relatedIds = append(relatedIds, id)
		}
	}
	
	return relatedIds, nil
}
`

	default:
		return "// Default template content"
	}
}
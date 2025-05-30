package generator

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// ScannerGenerator generates Go scanner code from AWS service analysis
type ScannerGenerator struct {
	outputDir    string
	packageName  string
	templates    *ScannerTemplates
}

// ScannerTemplates holds all code generation templates
type ScannerTemplates struct {
	ServiceScanner *template.Template
	ResourceScan   *template.Template
	PaginatedScan  *template.Template
	ErrorHandling  *template.Template
}

// GenerationOptions controls scanner generation
type GenerationOptions struct {
	Services      []string // specific services to generate, empty = all
	OutputDir     string   // output directory for generated files
	PackageName   string   // Go package name
	WithRetry     bool     // include exponential backoff retry logic
	WithMetrics   bool     // include metrics collection
}

// NewScannerGenerator creates a new scanner generator
func NewScannerGenerator(opts GenerationOptions) (*ScannerGenerator, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = "./generated/scanners"
	}
	if opts.PackageName == "" {
		opts.PackageName = "scanners"
	}

	// Create output directory
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	templates, err := loadScannerTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}

	return &ScannerGenerator{
		outputDir:   opts.OutputDir,
		packageName: opts.PackageName,
		templates:   templates,
	}, nil
}

// GenerateService generates scanner code for a single AWS service
func (g *ScannerGenerator) GenerateService(service *AWSServiceInfo, opts GenerationOptions) error {
	// Skip if specific services requested and this isn't one of them
	if len(opts.Services) > 0 && !contains(opts.Services, service.Name) && !contains(opts.Services, "all") {
		return nil
	}

	filename := fmt.Sprintf("%s_scanner.go", strings.ToLower(service.Name))
	outputPath := filepath.Join(g.outputDir, filename)

	// Generate scanner data
	scannerData := g.prepareScannerData(service, opts)

	// Generate code from template
	var buf strings.Builder
	if err := g.templates.ServiceScanner.Execute(&buf, scannerData); err != nil {
		return fmt.Errorf("failed to execute template for %s: %w", service.Name, err)
	}

	// Format the generated code
	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		// If formatting fails, save unformatted code for debugging
		fmt.Printf("Warning: failed to format generated code for %s: %v\n", service.Name, err)
		formatted = []byte(buf.String())
	}

	// Write to file
	if err := os.WriteFile(outputPath, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write scanner file %s: %w", outputPath, err)
	}

	fmt.Printf("Generated scanner: %s\n", outputPath)
	return nil
}

// GenerateAllServices generates scanner code for multiple services
func (g *ScannerGenerator) GenerateAllServices(services map[string]*AWSServiceInfo, opts GenerationOptions) error {
	for name, service := range services {
		if err := g.GenerateService(service, opts); err != nil {
			return fmt.Errorf("failed to generate scanner for %s: %w", name, err)
		}
	}

	// Generate a main scanner registry file
	return g.generateScannerRegistry(services, opts)
}

// ScannerData holds template data for code generation
type ScannerData struct {
	PackageName     string
	ServiceName     string
	ServiceNameCaps string
	ClientType      string
	Resources       []ResourceScanData
	WithRetry       bool
	WithMetrics     bool
	GeneratedAt     string
	Package         string
}

// ResourceScanData holds data for scanning a specific resource type
type ResourceScanData struct {
	Name            string
	TypeName        string
	ListOperation   *OperationData
	GetOperation    *OperationData
	IDField         string
	NameField       string
	ARNField        string
	TagsField       string
	Paginated       bool
	OutputType      string
	ItemsField      string
	PaginationToken string
}

// OperationData holds AWS operation information
type OperationData struct {
	Name       string
	InputType  string
	OutputType string
	Paginated  bool
}

// prepareScannerData converts AWSServiceInfo to template data
func (g *ScannerGenerator) prepareScannerData(service *AWSServiceInfo, opts GenerationOptions) ScannerData {
	data := ScannerData{
		PackageName:     g.packageName,
		ServiceName:     service.Name,
		ServiceNameCaps: strings.Title(service.Name),
		ClientType:      service.ClientType,
		WithRetry:       opts.WithRetry,
		WithMetrics:     opts.WithMetrics,
		GeneratedAt:     time.Now().Format(time.RFC3339),
		Package:         service.PackageName,
	}

	// Convert resources
	for _, resource := range service.ResourceTypes {
		resourceData := ResourceScanData{
			Name:       resource.Name,
			TypeName:   resource.TypeName,
			IDField:    resource.IDField,
			NameField:  resource.NameField,
			ARNField:   resource.ARNField,
			TagsField:  resource.TagsField,
			ItemsField: g.inferItemsField(resource.Name),
		}

		// Find list and get operations
		for _, op := range service.Operations {
			if op.ResourceType == resource.Name {
				opData := &OperationData{
					Name:       op.Name,
					InputType:  op.InputType,
					OutputType: op.OutputType,
					Paginated:  op.Paginated,
				}

				if op.IsList {
					resourceData.ListOperation = opData
					resourceData.Paginated = op.Paginated
					resourceData.OutputType = op.OutputType
					resourceData.PaginationToken = g.inferPaginationToken(op.OutputType)
				} else if op.IsGet || op.IsDescribe {
					resourceData.GetOperation = opData
				}
			}
		}

		data.Resources = append(data.Resources, resourceData)
	}

	return data
}

// inferItemsField infers the field name containing the list of items
func (g *ScannerGenerator) inferItemsField(resourceName string) string {
	// Common patterns for AWS response fields
	patterns := []string{
		resourceName + "s",           // Buckets, Instances
		resourceName + "List",        // InstanceList
		resourceName + "Set",         // SecurityGroupSet
		"Items",                      // Generic Items
		"Results",                    // Generic Results
	}
	
	// Return the most likely pattern (pluralized resource name)
	return patterns[0]
}

// inferPaginationToken infers the pagination token field name
func (g *ScannerGenerator) inferPaginationToken(outputType string) string {
	// Common pagination patterns in AWS
	patterns := []string{
		"NextToken",
		"Marker",
		"ContinuationToken",
		"PageToken",
	}
	
	// Return the most common one
	return patterns[0]
}

// generateScannerRegistry creates a registry file that lists all generated scanners
func (g *ScannerGenerator) generateScannerRegistry(services map[string]*AWSServiceInfo, opts GenerationOptions) error {
	registryPath := filepath.Join(g.outputDir, "registry.go")
	
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("// Code generated at %s. DO NOT EDIT.\n\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("package %s\n\n", g.packageName))
	
	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\tpb \"github.com/jlgore/corkscrew/proto\"\n")
	buf.WriteString(")\n\n")
	
	buf.WriteString("// ServiceScanner interface for all AWS service scanners\n")
	buf.WriteString("type ServiceScanner interface {\n")
	buf.WriteString("\tScan(ctx context.Context) ([]*pb.ResourceRef, error)\n")
	buf.WriteString("\tDescribeResource(ctx context.Context, id string) (*pb.ResourceRef, error)\n")
	buf.WriteString("\tGetServiceName() string\n")
	buf.WriteString("}\n\n")
	
	buf.WriteString("// ScannerRegistry holds all available service scanners\n")
	buf.WriteString("type ScannerRegistry struct {\n")
	buf.WriteString("\tscanners map[string]ServiceScanner\n")
	buf.WriteString("}\n\n")
	
	buf.WriteString("// NewScannerRegistry creates a new scanner registry\n")
	buf.WriteString("func NewScannerRegistry(clientFactory *ClientFactory) *ScannerRegistry {\n")
	buf.WriteString("\treturn &ScannerRegistry{\n")
	buf.WriteString("\t\tscanners: map[string]ServiceScanner{\n")
	
	for name := range services {
		if len(opts.Services) > 0 && !contains(opts.Services, name) && !contains(opts.Services, "all") {
			continue
		}
		buf.WriteString(fmt.Sprintf("\t\t\t\"%s\": New%sScanner(clientFactory),\n", name, strings.Title(name)))
	}
	
	buf.WriteString("\t\t},\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")
	
	buf.WriteString("// GetScanner returns a scanner for the specified service\n")
	buf.WriteString("func (r *ScannerRegistry) GetScanner(serviceName string) (ServiceScanner, error) {\n")
	buf.WriteString("\tscanner, exists := r.scanners[serviceName]\n")
	buf.WriteString("\tif !exists {\n")
	buf.WriteString("\t\treturn nil, fmt.Errorf(\"no scanner found for service: %s\", serviceName)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn scanner, nil\n")
	buf.WriteString("}\n\n")
	
	buf.WriteString("// GetAllScanners returns all available scanners\n")
	buf.WriteString("func (r *ScannerRegistry) GetAllScanners() map[string]ServiceScanner {\n")
	buf.WriteString("\treturn r.scanners\n")
	buf.WriteString("}\n\n")
	
	buf.WriteString("// ListServices returns all available service names\n")
	buf.WriteString("func (r *ScannerRegistry) ListServices() []string {\n")
	buf.WriteString("\tservices := make([]string, 0, len(r.scanners))\n")
	buf.WriteString("\tfor name := range r.scanners {\n")
	buf.WriteString("\t\tservices = append(services, name)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn services\n")
	buf.WriteString("}\n")
	
	// Format and write
	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		formatted = []byte(buf.String())
	}
	
	if err := os.WriteFile(registryPath, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write registry file: %w", err)
	}
	
	fmt.Printf("Generated scanner registry: %s\n", registryPath)
	return nil
}

// loadScannerTemplates loads all code generation templates
func loadScannerTemplates() (*ScannerTemplates, error) {
	funcMap := template.FuncMap{
		"lower": strings.ToLower,
		"title": strings.Title,
	}

	tmpl, err := template.New("advancedServiceScanner").Funcs(funcMap).Parse(createAdvancedScannerTemplate())
	if err != nil {
		return nil, fmt.Errorf("failed to parse advanced service scanner template: %w", err)
	}

	return &ScannerTemplates{
		ServiceScanner: tmpl,
	}, nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// GCPScannerGenerator generates Go scanner code for GCP services
type GCPScannerGenerator struct {
	outputDir   string
	packageName string
	templates   *GCPScannerTemplates
}

// GCPScannerTemplates holds all GCP code generation templates
type GCPScannerTemplates struct {
	ServiceScanner *template.Template
}

// GCPGenerationOptions controls GCP scanner generation
type GCPGenerationOptions struct {
	Services    []string // specific services to generate, empty = all
	OutputDir   string   // output directory for generated files
	PackageName string   // Go package name
	WithRetry   bool     // include exponential backoff retry logic
	WithMetrics bool     // include metrics collection
}

// GCPServiceInfo represents information about a GCP service
type GCPServiceInfo struct {
	Name          string
	ClientType    string
	PackageName   string
	ResourceTypes []GCPResourceType
}

// GCPResourceType represents a GCP resource type
type GCPResourceType struct {
	Name               string
	TypeName           string
	AssetInventoryType string
	IsRegional         bool
	IsZonal            bool
	IsGlobal           bool
	ListCode           string
	IDField            string
	NameField          string
	LabelsField        string
}

// NewGCPScannerGenerator creates a new GCP scanner generator
func NewGCPScannerGenerator(opts GCPGenerationOptions) (*GCPScannerGenerator, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = "./generated/scanners"
	}
	if opts.PackageName == "" {
		opts.PackageName = "main"
	}

	// Create output directory
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	templates, err := loadGCPScannerTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}

	return &GCPScannerGenerator{
		outputDir:   opts.OutputDir,
		packageName: opts.PackageName,
		templates:   templates,
	}, nil
}

// GenerateService generates scanner code for a single GCP service
func (g *GCPScannerGenerator) GenerateService(service *GCPServiceInfo, opts GCPGenerationOptions) error {
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

	fmt.Printf("Generated GCP scanner: %s\n", outputPath)
	return nil
}

// GenerateAllServices generates scanner code for multiple GCP services
func (g *GCPScannerGenerator) GenerateAllServices(services map[string]*GCPServiceInfo, opts GCPGenerationOptions) error {
	for name, service := range services {
		if err := g.GenerateService(service, opts); err != nil {
			return fmt.Errorf("failed to generate scanner for %s: %w", name, err)
		}
	}

	// Generate a main scanner registry file
	return g.generateScannerRegistry(services, opts)
}

// GCPScannerData holds template data for GCP code generation
type GCPScannerData struct {
	PackageName     string
	ServiceName     string
	ServiceNameCaps string
	ClientType      string
	Resources       []GCPResourceScanData
	AssetTypes      []string
	WithRetry       bool
	WithMetrics     bool
	GeneratedAt     string
}

// GCPResourceScanData holds data for scanning a specific GCP resource type
type GCPResourceScanData struct {
	Name               string
	TypeName           string
	AssetInventoryType string
	IsRegional         bool
	IsZonal            bool
	IsGlobal           bool
	ListCode           string
	IDField            string
	NameField          string
	LabelsField        string
}

// prepareScannerData converts GCPServiceInfo to template data
func (g *GCPScannerGenerator) prepareScannerData(service *GCPServiceInfo, opts GCPGenerationOptions) GCPScannerData {
	data := GCPScannerData{
		PackageName:     g.packageName,
		ServiceName:     service.Name,
		ServiceNameCaps: cases.Title(language.English).String(service.Name),
		ClientType:      service.ClientType,
		WithRetry:       opts.WithRetry,
		WithMetrics:     opts.WithMetrics,
		GeneratedAt:     time.Now().Format(time.RFC3339),
	}

	// Convert resources and collect asset types
	var assetTypes []string
	for _, resource := range service.ResourceTypes {
		resourceData := GCPResourceScanData{
			Name:               resource.Name,
			TypeName:           resource.TypeName,
			AssetInventoryType: resource.AssetInventoryType,
			IsRegional:         resource.IsRegional,
			IsZonal:            resource.IsZonal,
			IsGlobal:           resource.IsGlobal,
			ListCode:           resource.ListCode,
			IDField:            resource.IDField,
			NameField:          resource.NameField,
			LabelsField:        resource.LabelsField,
		}

		data.Resources = append(data.Resources, resourceData)
		if resource.AssetInventoryType != "" {
			assetTypes = append(assetTypes, resource.AssetInventoryType)
		}
	}

	data.AssetTypes = assetTypes
	return data
}

// generateScannerRegistry creates a registry file that lists all generated GCP scanners
func (g *GCPScannerGenerator) generateScannerRegistry(services map[string]*GCPServiceInfo, opts GCPGenerationOptions) error {
	registryPath := filepath.Join(g.outputDir, "registry.go")

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("// Code generated at %s. DO NOT EDIT.\n\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("package %s\n\n", g.packageName))

	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\tpb \"github.com/jlgore/corkscrew/internal/proto\"\n")
	buf.WriteString(")\n\n")

	buf.WriteString("// ServiceScanner interface for all GCP service scanners\n")
	buf.WriteString("type ServiceScanner interface {\n")
	buf.WriteString("\tScan(ctx context.Context) ([]*pb.ResourceRef, error)\n")
	buf.WriteString("\tDescribeResource(ctx context.Context, id string) (*pb.ResourceRef, error)\n")
	buf.WriteString("\tGetServiceName() string\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// ScannerRegistry holds all available GCP service scanners\n")
	buf.WriteString("type ScannerRegistry struct {\n")
	buf.WriteString("\tscanners map[string]ServiceScanner\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// NewScannerRegistry creates a new GCP scanner registry\n")
	buf.WriteString("func NewScannerRegistry(clientFactory *ClientFactory, assetInventory *AssetInventoryClient) *ScannerRegistry {\n")
	buf.WriteString("\treturn &ScannerRegistry{\n")
	buf.WriteString("\t\tscanners: map[string]ServiceScanner{\n")

	for name := range services {
		if len(opts.Services) > 0 && !contains(opts.Services, name) && !contains(opts.Services, "all") {
			continue
		}
		buf.WriteString(fmt.Sprintf("\t\t\t\"%s\": New%sScanner(clientFactory, assetInventory),\n", name, cases.Title(language.English).String(name)))
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

	fmt.Printf("Generated GCP scanner registry: %s\n", registryPath)
	return nil
}

// loadGCPScannerTemplates loads all GCP code generation templates
func loadGCPScannerTemplates() (*GCPScannerTemplates, error) {
	funcMap := template.FuncMap{
		"lower": strings.ToLower,
		"title": cases.Title(language.English).String,
	}

	tmpl, err := template.New("gcpServiceScanner").Funcs(funcMap).Parse(createGCPScannerTemplate())
	if err != nil {
		return nil, fmt.Errorf("failed to parse GCP service scanner template: %w", err)
	}

	return &GCPScannerTemplates{
		ServiceScanner: tmpl,
	}, nil
}

// createGCPScannerTemplate creates the GCP scanner template with Asset Inventory fallback
func createGCPScannerTemplate() string {
	return `// Code generated at {{.GeneratedAt}}. DO NOT EDIT.

package {{.PackageName}}

import (
	"context"
	"fmt"
	"log"
	"strings"
	{{if .WithRetry}}"time"{{end}}
	{{if .WithMetrics}}"github.com/prometheus/client_golang/prometheus"{{end}}

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// {{.ServiceNameCaps}}Scanner implements ServiceScanner for GCP {{.ServiceNameCaps}}
type {{.ServiceNameCaps}}Scanner struct {
	assetInventory *AssetInventoryClient
	clientFactory  *ClientFactory
	assetTypes     []string // Asset Inventory types for this service
	{{if .WithMetrics}}
	metrics struct {
		scanDuration     prometheus.Histogram
		resourcesScanned prometheus.Counter
		errorsTotal      prometheus.Counter
	}
	{{end}}
}

// New{{.ServiceNameCaps}}Scanner creates a new {{.ServiceName}} scanner
func New{{.ServiceNameCaps}}Scanner(clientFactory *ClientFactory, assetInventory *AssetInventoryClient) *{{.ServiceNameCaps}}Scanner {
	scanner := &{{.ServiceNameCaps}}Scanner{
		assetInventory: assetInventory,
		clientFactory:  clientFactory,
		assetTypes:     []string{
			{{range .AssetTypes}}
			"{{.}}",
			{{end}}
		},
	}
	{{if .WithMetrics}}
	scanner.initMetrics()
	{{end}}
	return scanner
}

{{if .WithMetrics}}
// initMetrics initializes Prometheus metrics
func (s *{{.ServiceNameCaps}}Scanner) initMetrics() {
	s.metrics.scanDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "corkscrew_gcp_{{.ServiceName}}_scan_duration_seconds",
		Help: "Time spent scanning GCP {{.ServiceName}} resources",
	})
	s.metrics.resourcesScanned = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "corkscrew_gcp_{{.ServiceName}}_resources_scanned_total",
		Help: "Total number of GCP {{.ServiceName}} resources scanned",
	})
	s.metrics.errorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "corkscrew_gcp_{{.ServiceName}}_errors_total",
		Help: "Total number of errors while scanning GCP {{.ServiceName}}",
	})
	
	prometheus.MustRegister(s.metrics.scanDuration, s.metrics.resourcesScanned, s.metrics.errorsTotal)
}
{{end}}

// GetServiceName returns the service name
func (s *{{.ServiceNameCaps}}Scanner) GetServiceName() string {
	return "{{.ServiceName}}"
}

// Scan discovers all {{.ServiceName}} resources using Asset Inventory fallback to client libraries
func (s *{{.ServiceNameCaps}}Scanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	{{if .WithMetrics}}
	start := time.Now()
	defer func() {
		s.metrics.scanDuration.Observe(time.Since(start).Seconds())
	}()
	{{end}}

	// Try Asset Inventory first
	if s.assetInventory != nil && s.assetInventory.IsHealthy(ctx) {
		resources, err := s.assetInventory.QueryAssetsByType(ctx, s.assetTypes)
		if err == nil {
			{{if .WithMetrics}}s.metrics.resourcesScanned.Add(float64(len(resources))){{end}}
			return resources, nil
		}
		log.Printf("Asset Inventory failed for {{.ServiceName}}, using client library: %v", err)
	}

	// Fall back to client library scanning
	return s.scanUsingSDK(ctx)
}

// scanUsingSDK scans resources using GCP client libraries
func (s *{{.ServiceNameCaps}}Scanner) scanUsingSDK(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := s.clientFactory.Get{{.ServiceNameCaps}}Client(ctx)
	if err != nil {
		{{if .WithMetrics}}s.metrics.errorsTotal.Inc(){{end}}
		return nil, fmt.Errorf("failed to get {{.ServiceName}} client: %w", err)
	}

	var resources []*pb.ResourceRef

	{{range .Resources}}
	// List {{.Name}} resources
	{{if .IsRegional}}
	// Iterate through all regions
	for _, region := range s.getRegions(ctx) {
		{{.ListCode}}
	}
	{{else if .IsZonal}}
	// Iterate through all zones
	for _, zone := range s.getZones(ctx) {
		{{.ListCode}}
	}
	{{else}}
	// Global resource
	{{.ListCode}}
	{{end}}
	{{end}}

	{{if .WithMetrics}}s.metrics.resourcesScanned.Add(float64(len(resources))){{end}}
	return resources, nil
}

// getRegions returns all GCP regions for the configured projects
func (s *{{.ServiceNameCaps}}Scanner) getRegions(ctx context.Context) []string {
	// Common GCP regions - in production this should be dynamically retrieved
	return []string{
		"us-central1", "us-east1", "us-east4", "us-west1", "us-west2", "us-west3", "us-west4",
		"europe-west1", "europe-west2", "europe-west3", "europe-west4", "europe-west6",
		"asia-east1", "asia-east2", "asia-northeast1", "asia-south1", "asia-southeast1",
	}
}

// getZones returns all GCP zones for the configured projects
func (s *{{.ServiceNameCaps}}Scanner) getZones(ctx context.Context) []string {
	// Common GCP zones - in production this should be dynamically retrieved
	return []string{
		"us-central1-a", "us-central1-b", "us-central1-c", "us-central1-f",
		"us-east1-a", "us-east1-b", "us-east1-c", "us-east1-d",
		"us-west1-a", "us-west1-b", "us-west1-c",
		"europe-west1-a", "europe-west1-b", "europe-west1-c", "europe-west1-d",
		"asia-east1-a", "asia-east1-b", "asia-east1-c",
	}
}

// getProjects returns all configured project IDs
func (s *{{.ServiceNameCaps}}Scanner) getProjects() []string {
	return s.clientFactory.GetProjectIDs()
}

// Asset type mappings
func (s *{{.ServiceNameCaps}}Scanner) getAssetTypes() []string {
	return s.assetTypes
}

// DescribeResource gets detailed information about a specific resource
func (s *{{.ServiceNameCaps}}Scanner) DescribeResource(ctx context.Context, resourceID string) (*pb.ResourceRef, error) {
	// Parse resource ID to extract project, region/zone, and resource name
	parts := strings.Split(resourceID, "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid resource ID format: %s", resourceID)
	}

	client, err := s.clientFactory.Get{{.ServiceNameCaps}}Client(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get {{.ServiceName}} client: %w", err)
	}

	{{range .Resources}}
	// Try to describe as {{.Name}} resource
	if strings.Contains(resourceID, "{{.TypeName | lower}}") {
		return s.describe{{.Name}}(ctx, client, resourceID)
	}
	{{end}}

	return nil, fmt.Errorf("unsupported resource type for ID: %s", resourceID)
}

{{range .Resources}}
// describe{{.Name}} describes a {{.Name}} resource
func (s *{{$.ServiceNameCaps}}Scanner) describe{{.Name}}(ctx context.Context, client interface{}, resourceID string) (*pb.ResourceRef, error) {
	// Implementation specific to {{.Name}} resources
	// This would contain the actual GCP API calls to get detailed resource information
	
	resource := &pb.ResourceRef{
		Id:           resourceID,
		Type:         "{{.AssetInventoryType}}",
		Service:      "{{$.ServiceName}}",
		DiscoveredAt: timestamppb.Now(),
		BasicAttributes: make(map[string]string),
	}

	// TODO: Implement actual resource description logic
	// This is a placeholder that should be replaced with real API calls

	return resource, nil
}
{{end}}

// extractLabels extracts GCP labels (not tags) from resource data
func (s *{{.ServiceNameCaps}}Scanner) extractLabels(labelsMap map[string]string) map[string]string {
	labels := make(map[string]string)
	for k, v := range labelsMap {
		labels["label_"+k] = v
	}
	return labels
}

// extractProjectFromResourceID extracts project ID from GCP resource ID
func (s *{{.ServiceNameCaps}}Scanner) extractProjectFromResourceID(resourceID string) string {
	if strings.Contains(resourceID, "projects/") {
		parts := strings.Split(resourceID, "/")
		for i, part := range parts {
			if part == "projects" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	return ""
}

// extractRegionFromResourceID extracts region from GCP resource ID
func (s *{{.ServiceNameCaps}}Scanner) extractRegionFromResourceID(resourceID string) string {
	if strings.Contains(resourceID, "regions/") {
		parts := strings.Split(resourceID, "/")
		for i, part := range parts {
			if part == "regions" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	return ""
}

// extractZoneFromResourceID extracts zone from GCP resource ID
func (s *{{.ServiceNameCaps}}Scanner) extractZoneFromResourceID(resourceID string) string {
	if strings.Contains(resourceID, "zones/") {
		parts := strings.Split(resourceID, "/")
		for i, part := range parts {
			if part == "zones" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	return ""
}

{{if .WithRetry}}
// retryOperation performs an operation with exponential backoff
func (s *{{.ServiceNameCaps}}Scanner) retryOperation(ctx context.Context, operation func() error) error {
	maxRetries := 3
	baseDelay := time.Millisecond * 100
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}
		
		if attempt == maxRetries {
			return err
		}
		
		// Exponential backoff
		delay := time.Duration(1<<uint(attempt)) * baseDelay
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			continue
		}
	}
	
	return fmt.Errorf("operation failed after %d retries", maxRetries)
}
{{end}}
`
}

// contains function is defined in client_library_analyzer.go
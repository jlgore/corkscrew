package main

import (
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	var (
		service   = flag.String("service", "", "GCP service to generate scanner for (compute, storage, container, bigquery, cloudsql, all)")
		outputDir = flag.String("output", "./generated", "Output directory for generated scanners")
		list      = flag.Bool("list", false, "List available GCP services")
		help      = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		showHelp()
		return
	}

	if *list {
		listServices()
		return
	}

	if *service == "" {
		fmt.Println("Error: -service flag is required")
		showHelp()
		os.Exit(1)
	}

	// Create output directory
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory %s: %v", *outputDir, err)
	}

	// Generate scanners
	if *service == "all" {
		fmt.Printf("Generating scanners for all GCP services to %s...\n", *outputDir)
		if err := GenerateAllGCPScanners(*outputDir); err != nil {
			log.Fatalf("Failed to generate all GCP scanners: %v", err)
		}
		fmt.Println("Successfully generated all GCP scanners!")
	} else {
		fmt.Printf("Generating scanner for GCP service '%s' to %s...\n", *service, *outputDir)
		if err := GenerateGCPServiceScanner(*service, *outputDir); err != nil {
			log.Fatalf("Failed to generate scanner for service %s: %v", *service, err)
		}
		fmt.Printf("Successfully generated scanner for service '%s'!\n", *service)
	}

	// List generated files
	fmt.Println("\nGenerated files:")
	err := filepath.Walk(*outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			relPath, _ := filepath.Rel(*outputDir, path)
			fmt.Printf("  %s\n", relPath)
		}
		return nil
	})
	if err != nil {
		log.Printf("Warning: failed to list generated files: %v", err)
	}
}

func showHelp() {
	fmt.Println("GCP Scanner Generator")
	fmt.Println("Generates Go scanner code for GCP services with Asset Inventory fallback to client libraries")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s [flags]\n", os.Args[0])
	fmt.Println()
	fmt.Println("Flags:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s -service compute -output ./scanners\n", os.Args[0])
	fmt.Printf("  %s -service all -output ./generated\n", os.Args[0])
	fmt.Printf("  %s -list\n", os.Args[0])
	fmt.Println()
	fmt.Println("Generated scanners follow this pattern:")
	fmt.Println("  1. Try Asset Inventory first for efficient bulk querying")
	fmt.Println("  2. Fall back to GCP client libraries if Asset Inventory fails")
	fmt.Println("  3. Handle GCP-specific patterns (projects, regions, zones, labels)")
	fmt.Println("  4. Support IAM binding discovery")
}

func listServices() {
	services := GetGCPServiceDefinitions()
	
	fmt.Println("Available GCP services for scanner generation:")
	fmt.Println()
	
	for name, service := range services {
		fmt.Printf("  %s (%s)\n", name, service.ClientType)
		for _, resource := range service.ResourceTypes {
			scope := "global"
			if resource.IsRegional {
				scope = "regional"
			} else if resource.IsZonal {
				scope = "zonal"
			}
			fmt.Printf("    - %s (%s, %s)\n", resource.Name, resource.AssetInventoryType, scope)
		}
		fmt.Println()
	}
	
	fmt.Println("Use 'all' to generate scanners for all services.")
}

// Embedded GCP service definitions and generator code
// This is a simplified version for the command-line tool

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

// GetGCPServiceDefinitions returns predefined GCP service information for scanner generation
func GetGCPServiceDefinitions() map[string]*GCPServiceInfo {
	return map[string]*GCPServiceInfo{
		"compute": {
			Name:        "compute",
			ClientType:  "*compute.Service",
			PackageName: "compute",
			ResourceTypes: []GCPResourceType{
				{
					Name:               "Instance",
					TypeName:           "Instance",
					AssetInventoryType: "compute.googleapis.com/Instance",
					IsZonal:            true,
					ListCode:           "// Instance list code would go here",
					IDField:            "Name",
					NameField:          "Name",
					LabelsField:        "Labels",
				},
			},
		},
		"storage": {
			Name:        "storage",
			ClientType:  "*storage.Service",
			PackageName: "storage",
			ResourceTypes: []GCPResourceType{
				{
					Name:               "Bucket",
					TypeName:           "Bucket",
					AssetInventoryType: "storage.googleapis.com/Bucket",
					IsGlobal:           true,
					ListCode:           "// Bucket list code would go here",
					IDField:            "Name",
					NameField:          "Name",
					LabelsField:        "Labels",
				},
			},
		},
	}
}

// GenerateGCPServiceScanner generates a scanner for a specific GCP service
func GenerateGCPServiceScanner(serviceName string, outputDir string) error {
	fmt.Printf("Generating simple scanner template for service: %s\n", serviceName)
	
	// Create a simple scanner template
	template := `package main

import (
	"context"
	"fmt"
	"log"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ` + strings.Title(serviceName) + `Scanner implements ServiceScanner for GCP ` + serviceName + `
type ` + strings.Title(serviceName) + `Scanner struct {
	assetInventory *AssetInventoryClient
	clientFactory  *ClientFactory
	assetTypes     []string
}

// New` + strings.Title(serviceName) + `Scanner creates a new ` + serviceName + ` scanner
func New` + strings.Title(serviceName) + `Scanner(clientFactory *ClientFactory, assetInventory *AssetInventoryClient) *` + strings.Title(serviceName) + `Scanner {
	return &` + strings.Title(serviceName) + `Scanner{
		assetInventory: assetInventory,
		clientFactory:  clientFactory,
		assetTypes:     []string{
			"` + serviceName + `.googleapis.com/*",
		},
	}
}

// GetServiceName returns the service name
func (s *` + strings.Title(serviceName) + `Scanner) GetServiceName() string {
	return "` + serviceName + `"
}

// Scan discovers all ` + serviceName + ` resources using Asset Inventory fallback to client libraries
func (s *` + strings.Title(serviceName) + `Scanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	// Try Asset Inventory first
	if s.assetInventory != nil && s.assetInventory.IsHealthy(ctx) {
		resources, err := s.assetInventory.QueryAssetsByType(ctx, s.assetTypes)
		if err == nil {
			return resources, nil
		}
		log.Printf("Asset Inventory failed for ` + serviceName + `, using client library: %v", err)
	}
	
	// Fall back to client library scanning
	return s.scanUsingSDK(ctx)
}

// scanUsingSDK scans resources using GCP client libraries
func (s *` + strings.Title(serviceName) + `Scanner) scanUsingSDK(ctx context.Context) ([]*pb.ResourceRef, error) {
	client, err := s.clientFactory.Get` + strings.Title(serviceName) + `Client(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ` + serviceName + ` client: %w", err)
	}

	var resources []*pb.ResourceRef
	
	// TODO: Implement actual resource scanning logic here
	// This is a template - replace with actual GCP API calls
	
	projectIDs := s.clientFactory.GetProjectIDs()
	for _, projectID := range projectIDs {
		// Example: List resources for project
		_ = projectID
		_ = client
		
		// Add your resource scanning logic here
		// Example:
		// req := client.SomeResource.List(projectID)
		// resp, err := req.Context(ctx).Do()
		// if err != nil { ... }
		// Convert response to ResourceRef and append to resources
	}

	return resources, nil
}

// DescribeResource gets detailed information about a specific resource
func (s *` + strings.Title(serviceName) + `Scanner) DescribeResource(ctx context.Context, resourceID string) (*pb.ResourceRef, error) {
	// TODO: Implement resource description logic
	return nil, fmt.Errorf("DescribeResource not yet implemented for ` + serviceName + ` resource: %s", resourceID)
}
`

	filename := fmt.Sprintf("%s_scanner.go", strings.ToLower(serviceName))
	outputPath := filepath.Join(outputDir, filename)

	// Format the generated code
	formatted, err := format.Source([]byte(template))
	if err != nil {
		fmt.Printf("Warning: failed to format generated code for %s: %v\n", serviceName, err)
		formatted = []byte(template)
	}

	// Write to file
	if err := os.WriteFile(outputPath, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write scanner file %s: %w", outputPath, err)
	}

	return nil
}

// GenerateAllGCPScanners generates scanners for all defined GCP services
func GenerateAllGCPScanners(outputDir string) error {
	services := []string{"compute", "storage", "container", "bigquery", "cloudsql"}
	
	for _, service := range services {
		if err := GenerateGCPServiceScanner(service, outputDir); err != nil {
			return fmt.Errorf("failed to generate scanner for %s: %w", service, err)
		}
	}
	
	// Generate registry
	return generateGCPScannerRegistry(services, outputDir)
}

// generateGCPScannerRegistry creates a registry file
func generateGCPScannerRegistry(services []string, outputDir string) error {
	registryPath := filepath.Join(outputDir, "registry.go")

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("// Code generated at %s. DO NOT EDIT.\n\n", time.Now().Format(time.RFC3339)))
	buf.WriteString("package main\n\n")

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

	for _, service := range services {
		buf.WriteString(fmt.Sprintf("\t\t\t\"%s\": New%sScanner(clientFactory, assetInventory),\n", service, strings.Title(service)))
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
	buf.WriteString("}\n")

	// Format and write
	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		formatted = []byte(buf.String())
	}

	if err := os.WriteFile(registryPath, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write registry file: %w", err)
	}

	return nil
}
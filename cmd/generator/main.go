package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

func main() {
	var (
		service    = flag.String("service", "", "AWS service to analyze and generate plugin for")
		outputDir  = flag.String("output-dir", "./generated/plugins", "Output directory for generated plugins")
		listOnly   = flag.Bool("list", false, "List available services for generation")
		analyzeAll = flag.Bool("all", false, "Generate plugins for all known services")
		verbose    = flag.Bool("verbose", false, "Verbose output")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "AWS Plugin Generator - Auto-generate plugins from AWS SDK Go v2\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s --list\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --service ec2\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --service lambda --output-dir ./my-plugins\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --all --verbose\n", os.Args[0])
	}

	flag.Parse()

	analyzer := generator.NewAWSAnalyzer()
	pluginGen := generator.NewPluginGenerator()

	// Get available services
	availableServices := getAvailableServices()

	if *listOnly {
		listAvailableServices(availableServices)
		return
	}

	if *analyzeAll {
		generateAllPlugins(analyzer, pluginGen, availableServices, *outputDir, *verbose)
		return
	}

	if *service == "" {
		fmt.Fprintf(os.Stderr, "Error: --service is required (or use --list to see available services)\n")
		flag.Usage()
		os.Exit(1)
	}

	if err := generateSinglePlugin(analyzer, pluginGen, *service, availableServices, *outputDir, *verbose); err != nil {
		log.Fatalf("Failed to generate plugin: %v", err)
	}
}

// getAvailableServices returns a map of service names to their client types
func getAvailableServices() map[string]interface{} {
	return map[string]interface{}{
		"s3":     &s3.Client{},
		"ec2":    &ec2.Client{},
		"rds":    &rds.Client{},
		"lambda": &lambda.Client{},
	}
}

// listAvailableServices lists all services available for generation
func listAvailableServices(services map[string]interface{}) {
	fmt.Println("Available AWS services for plugin generation:")
	fmt.Println()

	for serviceName, client := range services {
		clientType := reflect.TypeOf(client)
		packagePath := clientType.Elem().PkgPath()

		fmt.Printf("  %-10s - %s\n", serviceName, packagePath)
	}

	fmt.Printf("\nTotal: %d services available\n", len(services))
	fmt.Println("\nUsage:")
	fmt.Println("  generator --service <service-name>")
	fmt.Println("  generator --all")
}

// generateSinglePlugin generates a plugin for a single service
func generateSinglePlugin(analyzer *generator.AWSAnalyzer, pluginGen *generator.PluginGenerator,
	serviceName string, availableServices map[string]interface{}, outputDir string, verbose bool) error {

	client, exists := availableServices[serviceName]
	if !exists {
		return fmt.Errorf("service '%s' not available. Use --list to see available services", serviceName)
	}

	if verbose {
		fmt.Printf("Analyzing service: %s\n", serviceName)
	}

	// Analyze the service using reflection
	serviceInfo, err := analyzer.AnalyzeServiceFromReflection(serviceName, client)
	if err != nil {
		return fmt.Errorf("failed to analyze service %s: %w", serviceName, err)
	}

	if verbose {
		printServiceAnalysis(serviceInfo)
	}

	// Generate plugin code
	if verbose {
		fmt.Printf("Generating plugin code for %s...\n", serviceName)
	}

	pluginCode, err := pluginGen.GeneratePlugin(serviceInfo)
	if err != nil {
		return fmt.Errorf("failed to generate plugin code: %w", err)
	}

	// Write to file
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	serviceDir := filepath.Join(outputDir, serviceName)
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	outputFile := filepath.Join(serviceDir, "main.go")
	if err := os.WriteFile(outputFile, []byte(pluginCode), 0644); err != nil {
		return fmt.Errorf("failed to write plugin file: %w", err)
	}

	fmt.Printf("✓ Generated plugin for %s: %s\n", serviceName, outputFile)

	// Generate go.mod for the plugin
	if err := generatePluginGoMod(serviceDir, serviceName); err != nil {
		if verbose {
			fmt.Printf("Warning: failed to generate go.mod: %v\n", err)
		}
	}

	return nil
}

// generateAllPlugins generates plugins for all available services
func generateAllPlugins(analyzer *generator.AWSAnalyzer, pluginGen *generator.PluginGenerator,
	availableServices map[string]interface{}, outputDir string, verbose bool) {

	fmt.Printf("Generating plugins for %d services...\n", len(availableServices))

	successCount := 0
	failureCount := 0

	for serviceName := range availableServices {
		if verbose {
			fmt.Printf("\n--- Processing %s ---\n", serviceName)
		}

		err := generateSinglePlugin(analyzer, pluginGen, serviceName, availableServices, outputDir, verbose)
		if err != nil {
			fmt.Printf("✗ Failed to generate %s: %v\n", serviceName, err)
			failureCount++
		} else {
			successCount++
		}
	}

	fmt.Printf("\n=== Generation Summary ===\n")
	fmt.Printf("Success: %d plugins\n", successCount)
	fmt.Printf("Failed:  %d plugins\n", failureCount)
	fmt.Printf("Total:   %d plugins\n", successCount+failureCount)

	if successCount > 0 {
		fmt.Printf("\nGenerated plugins are in: %s\n", outputDir)
		fmt.Printf("To build all plugins, run:\n")
		fmt.Printf("  find %s -name 'main.go' -execdir go build -o ../../../plugins/corkscrew-{} . \\;\n", outputDir)
	}
}

// printServiceAnalysis prints detailed analysis of a service
func printServiceAnalysis(service *generator.AWSServiceInfo) {
	fmt.Printf("\n=== Service Analysis: %s ===\n", service.Name)
	fmt.Printf("Package: %s\n", service.PackageName)
	fmt.Printf("Client:  %s\n", service.ClientType)

	fmt.Printf("\nOperations (%d):\n", len(service.Operations))
	for _, op := range service.Operations {
		opType := "Other"
		if op.IsList {
			opType = "List"
		} else if op.IsDescribe {
			opType = "Describe"
		} else if op.IsGet {
			opType = "Get"
		}

		paginated := ""
		if op.Paginated {
			paginated = " (paginated)"
		}

		fmt.Printf("  %-20s %-10s -> %s%s\n", op.Name, opType, op.ResourceType, paginated)
	}

	fmt.Printf("\nResource Types (%d):\n", len(service.ResourceTypes))
	for _, resource := range service.ResourceTypes {
		fmt.Printf("  %-15s (%d operations)\n", resource.Name, len(resource.Operations))
		if len(resource.Relationships) > 0 {
			fmt.Printf("    Relationships:\n")
			for _, rel := range resource.Relationships {
				fmt.Printf("      -> %s (%s)\n", rel.TargetType, rel.RelationshipType)
			}
		}
	}
}

// generatePluginGoMod creates a go.mod file for the generated plugin
func generatePluginGoMod(pluginDir, serviceName string) error {
	goModContent := fmt.Sprintf(`module corkscrew-plugin-%s

go 1.21

require (
	github.com/aws/aws-sdk-go-v2 v1.24.0
	github.com/aws/aws-sdk-go-v2/config v1.26.1
	github.com/aws/aws-sdk-go-v2/service/%s v1.0.0
	github.com/hashicorp/go-plugin v1.6.0
	github.com/jlgore/corkscrew v0.1.0
	google.golang.org/protobuf v1.31.0
)

// Replace with local development path if needed
// replace github.com/jlgore/corkscrew => ../../..
`, serviceName, serviceName)

	goModPath := filepath.Join(pluginDir, "go.mod")
	return os.WriteFile(goModPath, []byte(goModContent), 0644)
}

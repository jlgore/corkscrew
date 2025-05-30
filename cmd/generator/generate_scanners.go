package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/jlgore/corkscrew/internal/generator"
)

func main() {
	var (
		services    = flag.String("services", "all", "Comma-separated list of services to generate scanners for (default: all)")
		outputDir   = flag.String("output", "./generated/scanners", "Output directory for generated scanner files")
		packageName = flag.String("package", "scanners", "Go package name for generated files")
		withRetry   = flag.Bool("retry", true, "Include exponential backoff retry logic")
		withMetrics = flag.Bool("metrics", false, "Include Prometheus metrics collection")
		help        = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		fmt.Println("AWS Scanner Generator")
		fmt.Println("====================")
		fmt.Println("Generates Go scanner code from AWS SDK analysis")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  generate-scanners [options]")
		fmt.Println()
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  # Generate scanners for all services")
		fmt.Println("  generate-scanners")
		fmt.Println()
		fmt.Println("  # Generate scanners for specific services")
		fmt.Println("  generate-scanners --services=s3,ec2,rds")
		fmt.Println()
		fmt.Println("  # Generate with custom output directory")
		fmt.Println("  generate-scanners --output=./internal/scanners --package=scanners")
		fmt.Println()
		fmt.Println("  # Generate with retry logic and metrics")
		fmt.Println("  generate-scanners --retry --metrics")
		return
	}

	// Parse services list
	var serviceList []string
	if *services != "" && *services != "all" {
		serviceList = strings.Split(*services, ",")
		for i, s := range serviceList {
			serviceList[i] = strings.TrimSpace(s)
		}
	}

	// Create analyzer
	analyzer := generator.NewAWSAnalyzer()

	// Get known services for analysis
	knownServices := analyzer.GetKnownServices()

	// Filter services if specific ones requested
	servicesToAnalyze := make(map[string]string)
	if len(serviceList) == 0 || contains(serviceList, "all") {
		servicesToAnalyze = knownServices
	} else {
		for _, service := range serviceList {
			if packagePath, exists := knownServices[service]; exists {
				servicesToAnalyze[service] = packagePath
			} else {
				log.Printf("Warning: unknown service '%s' requested", service)
			}
		}
	}

	if len(servicesToAnalyze) == 0 {
		log.Fatal("No valid services to analyze")
	}

	fmt.Printf("Analyzing %d services...\n", len(servicesToAnalyze))

	// Analyze services
	analyzedServices := make(map[string]*generator.AWSServiceInfo)
	for name, packagePath := range servicesToAnalyze {
		fmt.Printf("Analyzing %s service...\n", name)
		
		service, err := analyzer.AnalyzeService(name, packagePath)
		if err != nil {
			log.Printf("Failed to analyze %s: %v", name, err)
			continue
		}
		
		analyzedServices[name] = service
		fmt.Printf("  Found %d operations, %d resource types\n", 
			len(service.Operations), len(service.ResourceTypes))
	}

	if len(analyzedServices) == 0 {
		log.Fatal("No services successfully analyzed")
	}

	// Create scanner generator
	opts := generator.GenerationOptions{
		Services:    serviceList,
		OutputDir:   *outputDir,
		PackageName: *packageName,
		WithRetry:   *withRetry,
		WithMetrics: *withMetrics,
	}

	scannerGen, err := generator.NewScannerGenerator(opts)
	if err != nil {
		log.Fatalf("Failed to create scanner generator: %v", err)
	}

	// Generate scanners
	fmt.Printf("\nGenerating scanners to %s...\n", *outputDir)
	err = scannerGen.GenerateAllServices(analyzedServices, opts)
	if err != nil {
		log.Fatalf("Failed to generate scanners: %v", err)
	}

	fmt.Printf("\nGeneration complete!\n")
	fmt.Printf("Generated %d service scanners\n", len(analyzedServices))
	fmt.Printf("Output directory: %s\n", *outputDir)
	
	// Print summary
	fmt.Println("\nGenerated files:")
	for name := range analyzedServices {
		if len(serviceList) == 0 || contains(serviceList, name) || contains(serviceList, "all") {
			filename := fmt.Sprintf("%s_scanner.go", strings.ToLower(name))
			fmt.Printf("  %s\n", filename)
		}
	}
	fmt.Println("  registry.go")
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
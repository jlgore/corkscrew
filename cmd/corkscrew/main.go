package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jlgore/corkscrew-generator/internal/client"
	"github.com/jlgore/corkscrew-generator/internal/db"
	pb "github.com/jlgore/corkscrew-generator/internal/proto"
)

type Config struct {
	Services   []string
	Region     string
	PluginDir  string
	OutputFile string
	OutputDB   string
	Format     string
	Verbose    bool
	ListOnly   bool
	InfoOnly   bool
}

func main() {
	config := parseFlags()

	if config.ListOnly {
		listAvailablePlugins(config.PluginDir)
		return
	}

	if len(config.Services) == 0 {
		log.Fatal("At least one service must be specified")
	}

	// Create plugin manager
	pm := client.NewPluginManager(config.PluginDir)
	defer pm.Shutdown()

	if config.InfoOnly {
		showServiceInfo(pm, config.Services)
		return
	}

	// Perform scans
	ctx := context.Background()
	allResults := make(map[string]*pb.ScanResponse)

	for _, service := range config.Services {
		if config.Verbose {
			fmt.Printf("Scanning %s in region %s...\n", service, config.Region)
		}

		start := time.Now()
		result, err := pm.ScanService(ctx, service, &pb.ScanRequest{
			Region: config.Region,
			Options: map[string]string{
				"include_tags": "true",
			},
		})

		if err != nil {
			log.Printf("Error scanning %s: %v", service, err)
			continue
		}

		if result.Error != "" {
			log.Printf("Scan error for %s: %s", service, result.Error)
			continue
		}

		allResults[service] = result

		if config.Verbose {
			duration := time.Since(start)
			fmt.Printf("✓ %s: %d resources in %v\n", 
				service, result.Stats.TotalResources, duration)
		}
	}

	// Output results
	if err := outputResults(config, allResults); err != nil {
		log.Fatalf("Failed to output results: %v", err)
	}

	// Load into database if specified
	if config.OutputDB != "" {
		if err := loadIntoDatabase(config, allResults); err != nil {
			log.Fatalf("Failed to load into database: %v", err)
		}
	}

	// Print summary
	printSummary(allResults)
}

func parseFlags() *Config {
	config := &Config{}

	var servicesStr string
	flag.StringVar(&servicesStr, "services", "", "Comma-separated list of AWS services to scan")
	flag.StringVar(&config.Region, "region", "us-east-1", "AWS region to scan")
	flag.StringVar(&config.PluginDir, "plugin-dir", "./plugins", "Directory containing plugins")
	flag.StringVar(&config.OutputFile, "output", "", "Output file for results (JSON)")
	flag.StringVar(&config.OutputDB, "output-db", "", "DuckDB file to store results")
	flag.StringVar(&config.Format, "format", "json", "Output format (json, table)")
	flag.BoolVar(&config.Verbose, "verbose", false, "Verbose output")
	flag.BoolVar(&config.ListOnly, "list", false, "List available plugins and exit")
	flag.BoolVar(&config.InfoOnly, "info", false, "Show service information only")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Corkscrew Generator - Plugin-based AWS resource scanner\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s --services s3,ec2 --region us-west-2\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --services s3 --output s3-resources.json\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --services s3,ec2,rds --output-db infrastructure.db\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --list\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --services s3 --info\n", os.Args[0])
	}

	flag.Parse()

	if servicesStr != "" {
		config.Services = strings.Split(servicesStr, ",")
		for i, service := range config.Services {
			config.Services[i] = strings.TrimSpace(service)
		}
	}

	return config
}

func listAvailablePlugins(pluginDir string) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		log.Fatalf("Failed to read plugin directory: %v", err)
	}

	fmt.Printf("Available plugins in %s:\n", pluginDir)
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "corkscrew-") {
			service := strings.TrimPrefix(entry.Name(), "corkscrew-")
			fmt.Printf("  - %s\n", service)
			count++
		}
	}

	if count == 0 {
		fmt.Printf("  (no plugins found)\n")
		fmt.Printf("\nTo build plugins, run: make build-example-plugins\n")
	}
}

func showServiceInfo(pm *client.PluginManager, services []string) {
	for _, service := range services {
		fmt.Printf("\n=== %s Service Information ===\n", strings.ToUpper(service))

		info, err := pm.GetScannerInfo(service)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("Service: %s\n", info.ServiceName)
		fmt.Printf("Version: %s\n", info.Version)
		fmt.Printf("Supported Resources: %s\n", strings.Join(info.SupportedResources, ", "))
		fmt.Printf("Required Permissions:\n")
		for _, perm := range info.RequiredPermissions {
			fmt.Printf("  - %s\n", perm)
		}

		fmt.Printf("Capabilities:\n")
		for key, value := range info.Capabilities {
			fmt.Printf("  - %s: %s\n", key, value)
		}

		// Get schemas
		schemas, err := pm.GetServiceSchemas(service)
		if err != nil {
			fmt.Printf("Warning: Could not get schemas: %v\n", err)
		} else if len(schemas.Schemas) > 0 {
			fmt.Printf("Database Schemas:\n")
			for _, schema := range schemas.Schemas {
				fmt.Printf("  - %s: %s\n", schema.Name, schema.Description)
			}
		}
	}
}

func outputResults(config *Config, results map[string]*pb.ScanResponse) error {
	if config.OutputFile == "" && config.Format != "table" {
		return nil // No output requested
	}

	switch config.Format {
	case "json":
		return outputJSON(config, results)
	case "table":
		return outputTable(config, results)
	default:
		return fmt.Errorf("unsupported format: %s", config.Format)
	}
}

func outputJSON(config *Config, results map[string]*pb.ScanResponse) error {
	// Combine all resources
	allResources := make(map[string][]*pb.Resource)
	for service, result := range results {
		allResources[service] = result.Resources
	}

	data, err := json.MarshalIndent(allResources, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	if config.OutputFile != "" {
		if err := os.WriteFile(config.OutputFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Printf("Results written to %s\n", config.OutputFile)
	} else {
		fmt.Println(string(data))
	}

	return nil
}

func outputTable(config *Config, results map[string]*pb.ScanResponse) error {
	fmt.Printf("\n%-15s %-20s %-15s %-30s %-15s\n", 
		"SERVICE", "TYPE", "REGION", "NAME", "ID")
	fmt.Println(strings.Repeat("-", 95))

	for service, result := range results {
		for _, resource := range result.Resources {
			name := resource.Name
			if len(name) > 28 {
				name = name[:25] + "..."
			}
			id := resource.Id
			if len(id) > 13 {
				id = id[:10] + "..."
			}

			fmt.Printf("%-15s %-20s %-15s %-30s %-15s\n",
				service, resource.Type, resource.Region, name, id)
		}
	}

	return nil
}

func loadIntoDatabase(config *Config, results map[string]*pb.ScanResponse) error {
	if config.Verbose {
		fmt.Printf("Loading results into database: %s\n", config.OutputDB)
	}

	// Create graph loader
	gl, err := db.NewGraphLoader(config.OutputDB)
	if err != nil {
		return fmt.Errorf("failed to create graph loader: %w", err)
	}
	defer gl.Close()

	ctx := context.Background()

	// Load resources for each service
	for service, result := range results {
		if config.Verbose {
			fmt.Printf("Loading %d resources from %s...\n", 
				len(result.Resources), service)
		}

		// Load resources
		if err := gl.LoadResources(ctx, result.Resources); err != nil {
			return fmt.Errorf("failed to load resources for %s: %w", service, err)
		}

		// Load scan metadata
		if err := gl.LoadScanMetadata(ctx, service, config.Region, 
			result.Stats, result.Metadata); err != nil {
			return fmt.Errorf("failed to load scan metadata for %s: %w", service, err)
		}
	}

	// Create property graph (if supported)
	if err := gl.CreatePropertyGraph(ctx); err != nil {
		if config.Verbose {
			fmt.Printf("Warning: Could not create property graph: %v\n", err)
		}
	}

	if config.Verbose {
		fmt.Printf("✓ Database loaded successfully\n")
	}

	return nil
}

func printSummary(results map[string]*pb.ScanResponse) {
	fmt.Printf("\n=== Scan Summary ===\n")

	totalResources := int32(0)
	totalDuration := int64(0)
	totalFailed := int32(0)

	for service, result := range results {
		fmt.Printf("%-10s: %d resources (%d failed) in %dms\n",
			service,
			result.Stats.TotalResources,
			result.Stats.FailedResources,
			result.Stats.DurationMs)

		totalResources += result.Stats.TotalResources
		totalFailed += result.Stats.FailedResources
		totalDuration += result.Stats.DurationMs

		// Show resource type breakdown
		if len(result.Stats.ResourceCounts) > 0 {
			for resourceType, count := range result.Stats.ResourceCounts {
				fmt.Printf("  - %s: %d\n", resourceType, count)
			}
		}
	}

	fmt.Printf("\nTotal: %d resources (%d failed) in %dms across %d services\n",
		totalResources, totalFailed, totalDuration, len(results))
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jlgore/corkscrew-generator/internal/client"
	pb "github.com/jlgore/corkscrew-generator/internal/proto"
)

func main() {
	var (
		service    = flag.String("service", "", "AWS service to scan")
		region     = flag.String("region", "us-east-1", "AWS region")
		pluginDir  = flag.String("plugin-dir", "./plugins", "Plugin directory")
		outputFile = flag.String("output", "", "Output file (default: stdout)")
		listOnly   = flag.Bool("list", false, "List available plugins")
		info       = flag.Bool("info", false, "Show service info only")
	)
	flag.Parse()

	// Create plugin manager
	pm := client.NewPluginManager(*pluginDir)
	defer pm.Shutdown()

	if *listOnly {
		listAvailablePlugins(*pluginDir)
		return
	}

	if *service == "" {
		log.Fatal("Service is required (use -service flag)")
	}

	// Load the scanner plugin
	scanner, err := pm.LoadScanner(*service)
	if err != nil {
		log.Fatalf("Failed to load scanner: %v", err)
	}

	// Get service info
	serviceInfo, err := scanner.GetServiceInfo(context.Background(), &pb.Empty{})
	if err != nil {
		log.Fatalf("Failed to get service info: %v", err)
	}

	fmt.Printf("Loaded %s scanner v%s\n", serviceInfo.ServiceName, serviceInfo.Version)
	fmt.Printf("Supported resources: %v\n", serviceInfo.SupportedResources)
	fmt.Printf("Required permissions: %v\n", serviceInfo.RequiredPermissions)

	if *info {
		// Show schemas as well
		schemas, err := scanner.GetSchemas(context.Background(), &pb.Empty{})
		if err != nil {
			log.Printf("Warning: Failed to get schemas: %v", err)
		} else {
			fmt.Printf("\nAvailable schemas:\n")
			for _, schema := range schemas.Schemas {
				fmt.Printf("  - %s: %s\n", schema.Name, schema.Description)
			}
		}
		return
	}

	// Perform scan
	ctx := context.Background()
	resp, err := scanner.Scan(ctx, &pb.ScanRequest{
		Region: *region,
		Options: map[string]string{
			"include_tags": "true",
		},
	})

	if err != nil {
		log.Fatalf("Scan failed: %v", err)
	}

	if resp.Error != "" {
		log.Fatalf("Scan error: %s", resp.Error)
	}

	fmt.Printf("\nScan Results:\n")
	fmt.Printf("Total resources: %d\n", resp.Stats.TotalResources)
	fmt.Printf("Failed resources: %d\n", resp.Stats.FailedResources)
	fmt.Printf("Duration: %dms\n", resp.Stats.DurationMs)

	fmt.Printf("\nResource counts:\n")
	for rType, count := range resp.Stats.ResourceCounts {
		fmt.Printf("  %s: %d\n", rType, count)
	}

	// Show sample resources
	if len(resp.Resources) > 0 {
		fmt.Printf("\nSample resources:\n")
		for i, resource := range resp.Resources {
			if i >= 3 { // Show only first 3
				break
			}
			fmt.Printf("  - %s (%s): %s\n", resource.Type, resource.Id, resource.Name)
			if len(resource.Relationships) > 0 {
				fmt.Printf("    Relationships: %d\n", len(resource.Relationships))
			}
		}
	}

	// Output results
	if *outputFile != "" {
		data, err := json.MarshalIndent(resp.Resources, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal results: %v", err)
		}

		if err := os.WriteFile(*outputFile, data, 0644); err != nil {
			log.Fatalf("Failed to write output file: %v", err)
		}

		fmt.Printf("\nResults written to %s\n", *outputFile)
	}
}

func listAvailablePlugins(pluginDir string) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		log.Fatalf("Failed to read plugin directory: %v", err)
	}

	fmt.Printf("Available plugins in %s:\n", pluginDir)
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name()[:10] == "corkscrew-" {
			service := entry.Name()[10:] // Remove "corkscrew-" prefix
			fmt.Printf("  - %s\n", service)
		}
	}
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jlgore/corkscrew/internal/client"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

func main() {
	var (
		service    = flag.String("service", "", "AWS service to scan (e.g., s3, ec2, iam)")
		region     = flag.String("region", "us-east-1", "AWS region")
		pluginDir  = flag.String("plugin-dir", "./build/plugins", "Plugin directory")
		outputFile = flag.String("output", "", "Output file (default: stdout)")
		profile    = flag.String("profile", "", "AWS profile to use")
		debug      = flag.Bool("debug", false, "Enable debug output")
	)
	flag.Parse()

	if *service == "" {
		log.Fatal("Service is required. Use --service=s3 for example.")
	}

	// Set AWS profile if provided
	if *profile != "" {
		os.Setenv("AWS_PROFILE", *profile)
	}

	if *debug {
		fmt.Printf("🔧 Plugin Test Configuration:\n")
		fmt.Printf("  Service: %s\n", *service)
		fmt.Printf("  Region: %s\n", *region)
		fmt.Printf("  Plugin Dir: %s\n", *pluginDir)
		fmt.Printf("  Profile: %s\n", getEffectiveProfile(*profile))
		fmt.Println()
	}

	// Create plugin manager
	pm := client.NewPluginManager(*pluginDir)
	pm.SetDebug(*debug)
	defer pm.Shutdown()

	ctx := context.Background()

	// Load the scanner plugin
	if *debug {
		fmt.Printf("🔌 Loading %s scanner plugin...\n", *service)
	}

	_, err := pm.LoadScannerPlugin(ctx, *service, *region)
	if err != nil {
		log.Fatalf("Failed to load scanner plugin: %v", err)
	}

	// Get service info
	info, err := pm.GetScannerPluginInfo(ctx, *service)
	if err != nil {
		log.Fatalf("Failed to get service info: %v", err)
	}

	fmt.Printf("✅ Loaded %s scanner v%s\n", info.ServiceName, info.Version)
	fmt.Printf("📋 Supported resources: %v\n", info.SupportedResources)
	fmt.Printf("🔑 Required permissions: %v\n", info.RequiredPermissions)
	fmt.Printf("⚡ Capabilities: %v\n", info.Capabilities)
	fmt.Println()

	// Perform scan
	if *debug {
		fmt.Printf("🔍 Starting scan of %s in region %s...\n", *service, *region)
	}

	resp, err := pm.ScanServiceWithPlugin(ctx, *service, *region, &pb.ScanRequest{
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

	// Display results
	fmt.Printf("🎉 Scan Results:\n")
	fmt.Printf("  Total resources: %d\n", resp.Stats.TotalResources)
	fmt.Printf("  Failed resources: %d\n", resp.Stats.FailedResources)
	fmt.Printf("  Duration: %dms\n", resp.Stats.DurationMs)
	fmt.Println()

	if len(resp.Stats.ResourceCounts) > 0 {
		fmt.Printf("📊 Resource counts by type:\n")
		for resourceType, count := range resp.Stats.ResourceCounts {
			fmt.Printf("  %s: %d\n", resourceType, count)
		}
		fmt.Println()
	}

	// Show sample resources
	if len(resp.Resources) > 0 {
		fmt.Printf("📋 Sample resources:\n")
		for i, resource := range resp.Resources {
			if i >= 5 { // Show only first 5
				fmt.Printf("  ... and %d more\n", len(resp.Resources)-5)
				break
			}
			fmt.Printf("  🔹 %s (%s)\n", resource.Name, resource.Type)
			fmt.Printf("     ID: %s\n", resource.Id)
			if resource.Region != "" {
				fmt.Printf("     Region: %s\n", resource.Region)
			}
			if len(resource.Tags) > 0 {
				fmt.Printf("     Tags: %d\n", len(resource.Tags))
			}
			if len(resource.Relationships) > 0 {
				fmt.Printf("     Relationships: %d\n", len(resource.Relationships))
			}
		}
		fmt.Println()
	}

	// Output to file if requested
	if *outputFile != "" {
		data := map[string]interface{}{
			"service":   *service,
			"region":    *region,
			"stats":     resp.Stats,
			"resources": resp.Resources,
			"metadata":  resp.Metadata,
		}

		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal JSON: %v", err)
		}

		if err := os.WriteFile(*outputFile, jsonData, 0644); err != nil {
			log.Fatalf("Failed to write output file: %v", err)
		}

		fmt.Printf("📄 Results written to %s\n", *outputFile)
	}

	fmt.Printf("✅ Plugin test complete!\n")
}

func getEffectiveProfile(flagProfile string) string {
	if flagProfile != "" {
		return flagProfile
	}
	if envProfile := os.Getenv("AWS_PROFILE"); envProfile != "" {
		return envProfile
	}
	return "default"
}

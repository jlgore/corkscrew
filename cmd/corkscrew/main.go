package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/client"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "scan":
		runScan(os.Args[2:])
	case "discover":
		runDiscover(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "describe":
		runDescribe(os.Args[2:])
	case "info":
		runInfo(os.Args[2:])
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("🚀 Corkscrew Cloud Resource Scanner v2.0.0")
	fmt.Println("Plugin Architecture - AWS Provider")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  corkscrew scan --provider aws --services s3,ec2 --region us-east-1")
	fmt.Println("  corkscrew discover --provider aws")
	fmt.Println("  corkscrew list --provider aws --services s3 --region us-east-1")
	fmt.Println("  corkscrew describe --provider aws --resource-id bucket-name --service s3")
	fmt.Println("  corkscrew info --provider aws")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  scan        - Full resource scanning")
	fmt.Println("  discover    - Discover available services")
	fmt.Println("  list        - List resources")
	fmt.Println("  describe    - Describe specific resources")
	fmt.Println("  info        - Show provider information")
}

func runScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	
	provider := fs.String("provider", "aws", "Cloud provider (aws)")
	servicesStr := fs.String("services", "", "Comma-separated list of services")
	region := fs.String("region", "us-east-1", "AWS region")
	verbose := fs.Bool("verbose", false, "Verbose output")
	includeRelationships := fs.Bool("relationships", false, "Include resource relationships")
	
	fs.Parse(args)
	
	if *servicesStr == "" {
		log.Fatal("--services is required")
	}
	
	services := strings.Split(*servicesStr, ",")
	for i, service := range services {
		services[i] = strings.TrimSpace(service)
	}
	
	if *verbose {
		fmt.Printf("🔍 Scanning services %v in region %s using %s provider...\n", services, *region, *provider)
	}
	
	// Load AWS provider plugin
	providerClient, cleanup, err := loadAWSProvider(*verbose)
	if err != nil {
		log.Fatalf("Failed to load AWS provider: %v", err)
	}
	defer cleanup()
	
	ctx := context.Background()
	
	// Initialize provider
	initResp, err := providerClient.Initialize(ctx, &pb.InitializeRequest{
		Config: map[string]string{
			"region": *region,
		},
		CacheDir: filepath.Join(os.TempDir(), "corkscrew-cache"),
	})
	if err != nil {
		log.Fatalf("Failed to initialize provider: %v", err)
	}
	
	if !initResp.Success {
		log.Fatalf("Provider initialization failed: %s", initResp.Error)
	}
	
	if *verbose {
		fmt.Printf("✅ Provider initialized successfully\n")
	}
	
	// Batch scan
	start := time.Now()
	scanResp, err := providerClient.BatchScan(ctx, &pb.BatchScanRequest{
		Services:             services,
		Region:               *region,
		IncludeRelationships: *includeRelationships,
	})
	if err != nil {
		log.Fatalf("Failed to scan resources: %v", err)
	}
	
	duration := time.Since(start)
	
	// Display results
	fmt.Printf("\n🎯 Scan Results:\n")
	fmt.Printf("  Total Resources: %d\n", len(scanResp.Resources))
	fmt.Printf("  Duration: %v\n", duration)
	fmt.Printf("  Services Scanned: %d\n", len(services))
	
	if scanResp.Stats != nil {
		fmt.Printf("\n📊 Statistics:\n")
		for service, count := range scanResp.Stats.ResourceCounts {
			fmt.Printf("  %s: %d resources\n", service, count)
		}
		fmt.Printf("  Failed Resources: %d\n", scanResp.Stats.FailedResources)
		fmt.Printf("  Total Duration: %dms\n", scanResp.Stats.DurationMs)
	}
	
	if *verbose && len(scanResp.Resources) > 0 {
		fmt.Printf("\n📋 Sample Resources:\n")
		for i, resource := range scanResp.Resources {
			if i >= 5 { // Show first 5
				fmt.Printf("  ... and %d more\n", len(scanResp.Resources)-5)
				break
			}
			fmt.Printf("  %s/%s: %s (%s)\n", resource.Service, resource.Type, resource.Name, resource.Id)
		}
	}
}

func runDiscover(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	
	_ = fs.String("provider", "aws", "Cloud provider (aws)")
	verbose := fs.Bool("verbose", false, "Verbose output")
	forceRefresh := fs.Bool("force-refresh", false, "Force refresh of service catalog")
	
	fs.Parse(args)
	
	if *verbose {
		fmt.Printf("🔍 Discovering services for aws provider...\n")
	}
	
	// Load AWS provider plugin
	providerClient, cleanup, err := loadAWSProvider(*verbose)
	if err != nil {
		log.Fatalf("Failed to load AWS provider: %v", err)
	}
	defer cleanup()
	
	ctx := context.Background()
	
	// Initialize provider
	initResp, err := providerClient.Initialize(ctx, &pb.InitializeRequest{
		Config: map[string]string{
			"region": "us-east-1",
		},
		CacheDir: filepath.Join(os.TempDir(), "corkscrew-cache"),
	})
	if err != nil {
		log.Fatalf("Failed to initialize provider: %v", err)
	}
	
	if !initResp.Success {
		log.Fatalf("Provider initialization failed: %s", initResp.Error)
	}
	
	// Discover services
	discoverResp, err := providerClient.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
		ForceRefresh: *forceRefresh,
	})
	if err != nil {
		log.Fatalf("Failed to discover services: %v", err)
	}
	
	fmt.Printf("✅ Discovered %d services:\n", len(discoverResp.Services))
	for _, service := range discoverResp.Services {
		fmt.Printf("  🔧 %s - %s\n", service.Name, service.DisplayName)
		if *verbose {
			fmt.Printf("      Package: %s\n", service.PackageName)
			fmt.Printf("      Client: %s\n", service.ClientType)
		}
	}
	
	fmt.Printf("\nSDK Version: %s\n", discoverResp.SdkVersion)
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	
	_ = fs.String("provider", "aws", "Cloud provider (aws)")
	service := fs.String("service", "", "Service to list resources for")
	region := fs.String("region", "us-east-1", "AWS region")
	verbose := fs.Bool("verbose", false, "Verbose output")
	
	fs.Parse(args)
	
	if *service == "" {
		log.Fatal("--service is required")
	}
	
	if *verbose {
		fmt.Printf("📋 Listing %s resources in region %s...\n", *service, *region)
	}
	
	// Load AWS provider plugin
	providerClient, cleanup, err := loadAWSProvider(*verbose)
	if err != nil {
		log.Fatalf("Failed to load AWS provider: %v", err)
	}
	defer cleanup()
	
	ctx := context.Background()
	
	// Initialize provider
	initResp, err := providerClient.Initialize(ctx, &pb.InitializeRequest{
		Config: map[string]string{
			"region": *region,
		},
		CacheDir: filepath.Join(os.TempDir(), "corkscrew-cache"),
	})
	if err != nil {
		log.Fatalf("Failed to initialize provider: %v", err)
	}
	
	if !initResp.Success {
		log.Fatalf("Provider initialization failed: %s", initResp.Error)
	}
	
	// List resources
	listResp, err := providerClient.ListResources(ctx, &pb.ListResourcesRequest{
		Service: *service,
		Region:  *region,
	})
	if err != nil {
		log.Fatalf("Failed to list resources: %v", err)
	}
	
	fmt.Printf("📋 Found %d %s resources:\n", len(listResp.Resources), *service)
	for _, resource := range listResp.Resources {
		fmt.Printf("  🔍 %s: %s (%s)\n", resource.Type, resource.Name, resource.Id)
		if *verbose {
			fmt.Printf("      Region: %s\n", resource.Region)
			for key, value := range resource.BasicAttributes {
				fmt.Printf("      %s: %s\n", key, value)
			}
		}
	}
}

func runDescribe(args []string) {
	fs := flag.NewFlagSet("describe", flag.ExitOnError)
	
	_ = fs.String("provider", "aws", "Cloud provider (aws)")
	service := fs.String("service", "", "Service name")
	resourceType := fs.String("type", "", "Resource type")
	resourceId := fs.String("id", "", "Resource ID")
	region := fs.String("region", "us-east-1", "AWS region")
	includeTags := fs.Bool("tags", true, "Include tags")
	includeRelationships := fs.Bool("relationships", false, "Include relationships")
	verbose := fs.Bool("verbose", false, "Verbose output")
	
	fs.Parse(args)
	
	if *service == "" || *resourceType == "" || *resourceId == "" {
		log.Fatal("--service, --type, and --id are required")
	}
	
	if *verbose {
		fmt.Printf("🔍 Describing %s/%s: %s\n", *service, *resourceType, *resourceId)
	}
	
	// Load AWS provider plugin
	providerClient, cleanup, err := loadAWSProvider(*verbose)
	if err != nil {
		log.Fatalf("Failed to load AWS provider: %v", err)
	}
	defer cleanup()
	
	ctx := context.Background()
	
	// Initialize provider
	initResp, err := providerClient.Initialize(ctx, &pb.InitializeRequest{
		Config: map[string]string{
			"region": *region,
		},
		CacheDir: filepath.Join(os.TempDir(), "corkscrew-cache"),
	})
	if err != nil {
		log.Fatalf("Failed to initialize provider: %v", err)
	}
	
	if !initResp.Success {
		log.Fatalf("Provider initialization failed: %s", initResp.Error)
	}
	
	// Describe resource
	describeResp, err := providerClient.DescribeResource(ctx, &pb.DescribeResourceRequest{
		ResourceRef: &pb.ResourceRef{
			Id:      *resourceId,
			Type:    *resourceType,
			Service: *service,
			Region:  *region,
		},
		IncludeTags:          *includeTags,
		IncludeRelationships: *includeRelationships,
	})
	if err != nil {
		log.Fatalf("Failed to describe resource: %v", err)
	}
	
	resource := describeResp.Resource
	fmt.Printf("📋 Resource Details:\n")
	fmt.Printf("  ID: %s\n", resource.Id)
	fmt.Printf("  Name: %s\n", resource.Name)
	fmt.Printf("  Type: %s\n", resource.Type)
	fmt.Printf("  Service: %s\n", resource.Service)
	fmt.Printf("  Region: %s\n", resource.Region)
	fmt.Printf("  ARN: %s\n", resource.Arn)
	fmt.Printf("  Created: %s\n", resource.CreatedAt.AsTime().Format(time.RFC3339))
	
	if len(resource.Tags) > 0 {
		fmt.Printf("  Tags:\n")
		for key, value := range resource.Tags {
			fmt.Printf("    %s: %s\n", key, value)
		}
	}
	
	if len(resource.Relationships) > 0 {
		fmt.Printf("  Relationships:\n")
		for _, rel := range resource.Relationships {
			fmt.Printf("    %s -> %s (%s)\n", rel.RelationshipType, rel.TargetType, rel.TargetId)
		}
	}
	
	if *verbose {
		fmt.Printf("  Raw Data: %s\n", resource.RawData)
	}
}

func runInfo(args []string) {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	
	_ = fs.String("provider", "aws", "Cloud provider (aws)")
	verbose := fs.Bool("verbose", false, "Verbose output")
	
	fs.Parse(args)
	
	// Load AWS provider plugin
	providerClient, cleanup, err := loadAWSProvider(*verbose)
	if err != nil {
		log.Fatalf("Failed to load AWS provider: %v", err)
	}
	defer cleanup()
	
	ctx := context.Background()
	
	// Get provider info
	infoResp, err := providerClient.GetProviderInfo(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("Failed to get provider info: %v", err)
	}
	
	fmt.Printf("🚀 Provider Information:\n")
	fmt.Printf("  Name: %s\n", infoResp.Name)
	fmt.Printf("  Version: %s\n", infoResp.Version)
	fmt.Printf("  Description: %s\n", infoResp.Description)
	
	if len(infoResp.Capabilities) > 0 {
		fmt.Printf("  Capabilities:\n")
		for key, value := range infoResp.Capabilities {
			fmt.Printf("    %s: %s\n", key, value)
		}
	}
	
	if len(infoResp.SupportedServices) > 0 {
		fmt.Printf("  Supported Services: %d\n", len(infoResp.SupportedServices))
		if *verbose {
			for _, service := range infoResp.SupportedServices {
				fmt.Printf("    - %s\n", service)
			}
		}
	}
}

func loadAWSProvider(verbose bool) (*client.ProviderClient, func(), error) {
	pluginPath := "./plugins/aws-provider/aws-provider"
	
	// Check if plugin exists
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("AWS provider plugin not found at %s", pluginPath)
	}
	
	if verbose {
		fmt.Printf("🔌 Loading AWS provider plugin: %s\n", pluginPath)
	}
	
	// Create plugin client
	pluginClient := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins:         shared.PluginMap,
		Cmd:             exec.Command(pluginPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})
	
	// Connect via RPC
	rpcClient, err := pluginClient.Client()
	if err != nil {
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to get RPC client: %w", err)
	}
	
	// Request the plugin
	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to dispense plugin: %w", err)
	}
	
	// Cast to our provider interface
	providerInterface := raw.(shared.CloudProvider)
	provider := client.NewProviderClient(providerInterface)
	
	cleanup := func() {
		pluginClient.Kill()
	}
	
	if verbose {
		fmt.Printf("✅ AWS provider plugin loaded successfully\n")
	}
	
	return provider, cleanup, nil
}
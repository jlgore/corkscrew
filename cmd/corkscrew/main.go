package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	// tea "github.com/charmbracelet/bubbletea"
	"github.com/hashicorp/go-plugin"
	// "github.com/jlgore/corkscrew/diagrams/pkg/renderer"
	// "github.com/jlgore/corkscrew/diagrams/pkg/ui"
	"github.com/jlgore/corkscrew/internal/client"
	// "github.com/jlgore/corkscrew/internal/db"  // Used by plugins, not main CLI currently
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Build-time variables set by GoReleaser
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
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
	case "orchestrator-discover":
		if err := runOrchestratorDiscovery(os.Args[2:]); err != nil {
			log.Fatalf("Orchestrator discovery failed: %v", err)
		}
	case "list":
		runList(os.Args[2:])
	case "describe":
		runDescribe(os.Args[2:])
	case "info":
		runInfo(os.Args[2:])
	case "schemas":
		runSchemas(os.Args[2:])
	case "diagram":
		fmt.Println("❌ Diagram functionality is temporarily disabled")
		fmt.Println("This feature will be available in a future release")
		// runDiagram(os.Args[2:])
	case "plugin":
		runPlugin(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("Corkscrew %s (commit: %s, built: %s)\n", version, commit, date)
		return
	case "help", "--help", "-h":
		printUsage()
		return
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("🚀 Corkscrew Cloud Resource Scanner v2.0.0")
	fmt.Println("Multi-Cloud Plugin Architecture")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  # AWS Examples")
	fmt.Println("  corkscrew scan --provider aws --services s3,ec2 --region us-east-1")
	fmt.Println("  corkscrew discover --provider aws")
	fmt.Println("  corkscrew orchestrator-discover --provider aws --generate --verbose")
	fmt.Println("  corkscrew list --provider aws --services s3 --region us-east-1")
	fmt.Println("  corkscrew describe --provider aws --resource-id bucket-name --service s3")
	fmt.Println("  corkscrew info --provider aws")
	fmt.Println()
	fmt.Println("  # Azure Examples")
	fmt.Println("  corkscrew scan --provider azure --services compute,storage --region eastus")
	fmt.Println("  corkscrew discover --provider azure")
	fmt.Println("  corkscrew list --provider azure --services compute --region eastus")
	fmt.Println("  corkscrew info --provider azure")
	fmt.Println("  corkscrew schemas --provider azure --services storage,compute")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  scan                - Full resource scanning")
	fmt.Println("  discover            - Discover available services")
	fmt.Println("  orchestrator-discover - Advanced discovery using orchestrator")
	fmt.Println("  list                - List resources")
	fmt.Println("  describe            - Describe specific resources")
	fmt.Println("  info                - Show provider information")
	fmt.Println("  schemas             - Get database schemas for resources")
	fmt.Println("  diagram             - Interactive resource diagram viewer")
	fmt.Println("  plugin              - Plugin management (list, build, status)")
	fmt.Println("  version             - Show version information")
	fmt.Println()
	fmt.Println("Supported Providers:")
	fmt.Println("  aws         - Amazon Web Services")
	fmt.Println("  azure       - Microsoft Azure")
}

func runScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure)")
	servicesStr := fs.String("services", "", "Comma-separated list of services")
	region := fs.String("region", "us-east-1", "Region (us-east-1 for AWS, eastus for Azure)")
	verbose := fs.Bool("verbose", false, "Verbose output")
	includeRelationships := fs.Bool("relationships", false, "Include resource relationships")
	
	// Enhanced AWS features
	useConfig := fs.Bool("use-config", false, "Use AWS Config for resource discovery (if available)")
	useTagging := fs.Bool("use-tagging", true, "Use Resource Groups Tagging API for discovery")
	useOrgs := fs.Bool("use-organizations", false, "Use AWS Organizations for multi-account scanning")
	tagFilter := fs.String("tag-filter", "", "Tag filter in format key=value")

	fs.Parse(args)

	if *servicesStr == "" {
		log.Fatal("--services is required")
	}

	services := strings.Split(*servicesStr, ",")
	for i, service := range services {
		services[i] = strings.TrimSpace(service)
	}

	if *verbose {
		fmt.Printf("🔍 Scanning services %v in region %s using %s provider...\n", services, *region, *providerName)
	}

	// Load provider plugin
	providerClient, cleanup, err := loadProvider(*providerName, *verbose)
	if err != nil {
		log.Fatalf("Failed to load %s provider: %v", *providerName, err)
	}
	defer cleanup()

	ctx := context.Background()

	// Build initialization config
	initConfig := map[string]string{
		"region": *region,
	}
	
	// Add AWS profile if set
	if profile := os.Getenv("AWS_PROFILE"); profile != "" {
		initConfig["profile"] = profile
	}
	
	// Initialize provider
	initResp, err := providerClient.Initialize(ctx, &pb.InitializeRequest{
		Config:   initConfig,
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

	// Set environment variables for enhanced features
	if *providerName == "aws" {
		if *useConfig {
			os.Setenv("CORKSCREW_AWS_USE_CONFIG", "true")
		}
		if *useTagging {
			os.Setenv("CORKSCREW_AWS_USE_RESOURCE_TAGGING", "true")
		}
		if *useOrgs {
			os.Setenv("CORKSCREW_AWS_USE_ORGANIZATIONS", "true")
		}
	}
	
	// Build filters
	filters := make(map[string]string)
	if *tagFilter != "" {
		parts := strings.SplitN(*tagFilter, "=", 2)
		if len(parts) == 2 {
			filters[parts[0]] = parts[1]
		}
	}
	
	// Batch scan
	start := time.Now()
	scanResp, err := providerClient.BatchScan(ctx, &pb.BatchScanRequest{
		Services:             services,
		Region:               *region,
		IncludeRelationships: *includeRelationships,
		Filters:              filters,
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

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure)")
	verbose := fs.Bool("verbose", false, "Verbose output")
	forceRefresh := fs.Bool("force-refresh", false, "Force refresh of service catalog")

	fs.Parse(args)

	if *verbose {
		fmt.Printf("🔍 Discovering services for %s provider...\n", *providerName)
	}

	// Load provider plugin
	providerClient, cleanup, err := loadProvider(*providerName, *verbose)
	if err != nil {
		log.Fatalf("Failed to load %s provider: %v", *providerName, err)
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

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure)")
	service := fs.String("service", "", "Service to list resources for")
	region := fs.String("region", "us-east-1", "Region")
	verbose := fs.Bool("verbose", false, "Verbose output")

	fs.Parse(args)

	if *service == "" {
		log.Fatal("--service is required")
	}

	if *verbose {
		fmt.Printf("📋 Listing %s resources in region %s...\n", *service, *region)
	}

	// Load provider plugin
	providerClient, cleanup, err := loadProvider(*providerName, *verbose)
	if err != nil {
		log.Fatalf("Failed to load %s provider: %v", *providerName, err)
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

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure)")
	service := fs.String("service", "", "Service name")
	resourceType := fs.String("type", "", "Resource type")
	resourceId := fs.String("id", "", "Resource ID")
	region := fs.String("region", "us-east-1", "Region")
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

	// Load provider plugin
	providerClient, cleanup, err := loadProvider(*providerName, *verbose)
	if err != nil {
		log.Fatalf("Failed to load %s provider: %v", *providerName, err)
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

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure)")
	verbose := fs.Bool("verbose", false, "Verbose output")

	fs.Parse(args)

	// Load provider plugin
	providerClient, cleanup, err := loadProvider(*providerName, *verbose)
	if err != nil {
		log.Fatalf("Failed to load %s provider: %v", *providerName, err)
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

func runSchemas(args []string) {
	fs := flag.NewFlagSet("schemas", flag.ExitOnError)

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure)")
	servicesStr := fs.String("services", "", "Comma-separated list of services (optional)")
	format := fs.String("format", "sql", "Schema format (sql, json)")
	verbose := fs.Bool("verbose", false, "Verbose output")

	fs.Parse(args)

	var services []string
	if *servicesStr != "" {
		services = strings.Split(*servicesStr, ",")
		for i, service := range services {
			services[i] = strings.TrimSpace(service)
		}
	}

	if *verbose {
		fmt.Printf("🔍 Getting schemas for %s provider...\n", *providerName)
		if len(services) > 0 {
			fmt.Printf("  Services: %v\n", services)
		}
	}

	// Load provider plugin
	providerClient, cleanup, err := loadProvider(*providerName, *verbose)
	if err != nil {
		log.Fatalf("Failed to load %s provider: %v", *providerName, err)
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

	// Get schemas
	schemaResp, err := providerClient.GetSchemas(ctx, &pb.GetSchemasRequest{
		Services: services,
		Format:   *format,
	})
	if err != nil {
		log.Fatalf("Failed to get schemas: %v", err)
	}

	fmt.Printf("📊 Found %d schemas:\n\n", len(schemaResp.Schemas))
	
	for _, schema := range schemaResp.Schemas {
		fmt.Printf("🗄️  Schema: %s\n", schema.Name)
		fmt.Printf("   Service: %s\n", schema.Service)
		fmt.Printf("   Resource Type: %s\n", schema.ResourceType)
		fmt.Printf("   Description: %s\n", schema.Description)
		
		if *verbose && len(schema.Metadata) > 0 {
			fmt.Printf("   Metadata:\n")
			for key, value := range schema.Metadata {
				fmt.Printf("     %s: %s\n", key, value)
			}
		}
		
		if *format == "sql" && schema.Sql != "" {
			fmt.Printf("\n   SQL Definition:\n")
			fmt.Printf("   %s\n", strings.ReplaceAll(schema.Sql, "\n", "\n   "))
		}
		
		fmt.Println()
	}
}

func loadProvider(providerName string, verbose bool) (*client.ProviderClient, func(), error) {
	// Try different possible plugin paths
	possiblePaths := []string{}
	
	// First, check in user's home directory using new .corkscrew/plugins/ pattern
	if usr, err := user.Current(); err == nil {
		homePluginPath := filepath.Join(usr.HomeDir, ".corkscrew", "plugins", fmt.Sprintf("%s-provider", providerName))
		possiblePaths = append(possiblePaths, homePluginPath)
		
		// Legacy path for backward compatibility
		legacyPath := filepath.Join(usr.HomeDir, ".corkscrew", "bin", "plugin", fmt.Sprintf("corkscrew-%s", providerName))
		possiblePaths = append(possiblePaths, legacyPath)
	}
	
	// Then check current directory paths
	possiblePaths = append(possiblePaths,
		fmt.Sprintf("./plugins/build/corkscrew-%s", providerName),
		fmt.Sprintf("./build/plugins/corkscrew-%s", providerName),
		fmt.Sprintf("./plugins/%s-provider/%s-provider", providerName, providerName),
		fmt.Sprintf("./corkscrew-%s", providerName),
	)

	var pluginPath string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			pluginPath = path
			break
		}
	}

	if pluginPath == "" {
		if verbose {
			fmt.Printf("🔍 %s provider plugin not found in any of these paths: %v\n", providerName, possiblePaths)
		}
		
		if err := autoBuildPlugin(providerName, verbose); err != nil {
			return nil, nil, fmt.Errorf("failed to auto-build %s provider plugin: %w\n\nTry running manually: ./plugins/build-%s-plugin.sh", providerName, err, providerName)
		}
		
		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				pluginPath = path
				break
			}
		}
		
		if pluginPath == "" {
			return nil, nil, fmt.Errorf("plugin built successfully but not found in expected locations: %v", possiblePaths)
		}
	}

	if verbose {
		fmt.Printf("🔌 Loading %s provider plugin: %s\n", providerName, pluginPath)
	}

	// Create plugin client
	pluginClient := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  shared.HandshakeConfig,
		Plugins:          shared.PluginMap,
		Cmd:              exec.Command(pluginPath),
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
		fmt.Printf("✅ %s provider plugin loaded successfully\n", cases.Title(language.English).String(providerName))
	}

	return provider, cleanup, nil
}

func autoBuildPlugin(providerName string, verbose bool) error {
	if verbose {
		fmt.Printf("🔧 %s provider not found. Building now...\n", cases.Title(language.English).String(providerName))
	}

	pluginDir := fmt.Sprintf("./plugins/%s-provider", providerName)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin source directory not found: %s", pluginDir)
	}

	cmd := exec.Command("go", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Go is not installed or not in PATH")
	}

	if _, err := user.Current(); err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	buildScript := fmt.Sprintf("./plugins/build-%s-plugin.sh", providerName)
	if _, err := os.Stat(buildScript); err == nil {
		if verbose {
			fmt.Printf("📦 Using build script: %s\n", buildScript)
		}
		cmd := exec.Command("bash", buildScript)
		cmd.Stdout = os.Stdout
		if verbose {
			cmd.Stderr = os.Stderr
		}
		return cmd.Run()
	} else {
		if verbose {
			fmt.Printf("📦 Building plugin with go build...\n")
		}
		
		usr, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to get current user: %w", err)
		}
		
		pluginPath := filepath.Join(usr.HomeDir, ".corkscrew", "plugins")
		if err := os.MkdirAll(pluginPath, 0755); err != nil {
			return fmt.Errorf("failed to create plugin directory: %w", err)
		}
		
		outputPath := filepath.Join(pluginPath, fmt.Sprintf("%s-provider", providerName))
		
		cmd := exec.Command("go", "build", "-o", outputPath, ".")
		cmd.Dir = pluginDir
		if verbose {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go build failed: %w", err)
		}
		
		if err := os.Chmod(outputPath, 0755); err != nil {
			return fmt.Errorf("failed to make plugin executable: %w", err)
		}
		
		if verbose {
			fmt.Printf("✅ %s provider built successfully!\n", cases.Title(language.English).String(providerName))
		}
		
		return nil
	}
}

func detectPluginStatus(providerName string) (bool, string, error) {
	possiblePaths := []string{}
	
	if usr, err := user.Current(); err == nil {
		homePluginPath := filepath.Join(usr.HomeDir, ".corkscrew", "plugins", fmt.Sprintf("%s-provider", providerName))
		possiblePaths = append(possiblePaths, homePluginPath)
		
		// Legacy path for backward compatibility
		legacyPath := filepath.Join(usr.HomeDir, ".corkscrew", "bin", "plugin", fmt.Sprintf("corkscrew-%s", providerName))
		possiblePaths = append(possiblePaths, legacyPath)
	}
	
	possiblePaths = append(possiblePaths,
		fmt.Sprintf("./plugins/build/corkscrew-%s", providerName),
		fmt.Sprintf("./build/plugins/corkscrew-%s", providerName),
		fmt.Sprintf("./plugins/%s-provider/%s-provider", providerName, providerName),
		fmt.Sprintf("./corkscrew-%s", providerName),
	)

	for _, path := range possiblePaths {
		if stat, err := os.Stat(path); err == nil {
			if stat.Mode()&0111 != 0 {
				return true, path, nil
			} else {
				return false, path, fmt.Errorf("plugin found but not executable")
			}
		}
	}
	
	return false, "", nil
}

func runPlugin(args []string) {
	if len(args) == 0 {
		printPluginUsage()
		return
	}

	command := args[0]
	switch command {
	case "list":
		runPluginList(args[1:])
	case "build":
		runPluginBuild(args[1:])
	case "status":
		runPluginStatus(args[1:])
	default:
		fmt.Printf("Unknown plugin command: %s\n", command)
		printPluginUsage()
		os.Exit(1)
	}
}

func printPluginUsage() {
	fmt.Println("🔌 Plugin Management")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  corkscrew plugin list           - Show all plugins and their status")
	fmt.Println("  corkscrew plugin build <name>   - Build a specific plugin")
	fmt.Println("  corkscrew plugin status         - Health check all plugins")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  corkscrew plugin list")
	fmt.Println("  corkscrew plugin build aws")
	fmt.Println("  corkscrew plugin build azure")
	fmt.Println("  corkscrew plugin status")
}

func runPluginList(args []string) {
	fs := flag.NewFlagSet("plugin list", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Verbose output")
	fs.Parse(args)

	providers := []string{"aws", "azure", "gcp"}
	
	fmt.Println("🔌 Installed Plugins:")
	for _, provider := range providers {
		exists, path, err := detectPluginStatus(provider)
		
		if exists {
			fmt.Printf("  ✅ %s - %s", provider, path)
			if *verbose {
				if stat, err := os.Stat(path); err == nil {
					fmt.Printf(" (%s)", stat.ModTime().Format("2006-01-02 15:04:05"))
				}
			}
			fmt.Println()
		} else if err != nil {
			fmt.Printf("  ⚠️  %s - %s\n", provider, err.Error())
		} else {
			sourceDir := fmt.Sprintf("./plugins/%s-provider", provider)
			if _, err := os.Stat(sourceDir); err == nil {
				fmt.Printf("  📦 %s - available to build\n", provider)
			} else {
				fmt.Printf("  ❌ %s - not available\n", provider)
			}
		}
	}
}

func runPluginBuild(args []string) {
	fs := flag.NewFlagSet("plugin build", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Verbose output")
	force := fs.Bool("force", false, "Force rebuild even if plugin exists")
	fs.Parse(args)

	if len(fs.Args()) == 0 {
		fmt.Println("Provider name is required")
		fmt.Println("Usage: corkscrew plugin build <provider>")
		os.Exit(1)
	}

	providerName := fs.Args()[0]
	
	if !*force {
		exists, path, _ := detectPluginStatus(providerName)
		if exists {
			fmt.Printf("✅ %s plugin already exists at %s\n", providerName, path)
			fmt.Println("Use --force to rebuild")
			return
		}
	}

	start := time.Now()
	if err := autoBuildPlugin(providerName, *verbose); err != nil {
		fmt.Printf("❌ Failed to build %s plugin: %v\n", providerName, err)
		os.Exit(1)
	}
	
	duration := time.Since(start)
	fmt.Printf("🎉 %s plugin built successfully in %v\n", providerName, duration)
}

func runPluginStatus(args []string) {
	fs := flag.NewFlagSet("plugin status", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Verbose output")
	fs.Parse(args)

	providers := []string{"aws", "azure", "gcp"}
	
	fmt.Println("🏥 Plugin Health Check:")
	allGood := true
	
	for _, provider := range providers {
		exists, path, err := detectPluginStatus(provider)
		
		if exists {
			fmt.Printf("  %s: ", provider)
			if *verbose {
				fmt.Printf("(%s) ", path)
			}
			
			if stat, err := os.Stat(path); err == nil && stat.Mode()&0111 != 0 {
				fmt.Printf("✅ healthy")
				if *verbose {
					fmt.Printf(" (%s)", stat.ModTime().Format("2006-01-02 15:04:05"))
				}
				fmt.Printf("\n")
			} else {
				fmt.Printf("⚠️  plugin exists but not executable\n")
				allGood = false
			}
		} else if err != nil {
			fmt.Printf("  %s: ⚠️  %s\n", provider, err.Error())
			allGood = false
		} else {
			fmt.Printf("  %s: ❌ not installed\n", provider)
			allGood = false
		}
	}
	
	if allGood {
		fmt.Println("\n🎉 All available plugins are healthy!")
	} else {
		fmt.Println("\n⚠️  Some plugins need attention. Run 'corkscrew plugin build <provider>' to install missing plugins.")
	}
}

/*
func runDiagram(args []string) {
	// Temporarily disabled - will be re-enabled when diagram functionality is needed
	fmt.Println("❌ Diagram functionality is temporarily disabled")
}
*/

/*
func exportDiagram(graphLoader *db.GraphLoader, options renderer.DiagramOptions, filename string) error {
	// Temporarily disabled
	return fmt.Errorf("export functionality not yet implemented in integrated CLI")
}

func showDiagramHelp() {
	// Temporarily disabled
}
*/

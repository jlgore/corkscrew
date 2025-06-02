package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	// tea "github.com/charmbracelet/bubbletea"
	"github.com/hashicorp/go-plugin"
	// "github.com/jlgore/corkscrew/diagrams/pkg/renderer"
	// "github.com/jlgore/corkscrew/diagrams/pkg/ui"
	"github.com/jlgore/corkscrew/internal/client"
	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/pkg/query"
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
	case "query":
		runQuery(os.Args[2:])
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
	fmt.Println("  # Query Examples")
	fmt.Println("  corkscrew query \"SELECT COUNT(*) FROM aws_resources GROUP BY service\"")
	fmt.Println("  corkscrew query \"SELECT * FROM aws_resources WHERE type='Bucket'\" --output csv")
	fmt.Println("  echo \"SELECT * FROM azure_resources\" | corkscrew query --stdin --output json")
	fmt.Println("  corkscrew query --file analysis.sql --verbose")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  scan                - Full resource scanning")
	fmt.Println("  discover            - Discover available services")
	fmt.Println("  orchestrator-discover - Advanced discovery using orchestrator")
	fmt.Println("  list                - List resources")
	fmt.Println("  describe            - Describe specific resources")
	fmt.Println("  info                - Show provider information")
	fmt.Println("  schemas             - Get database schemas for resources")
	fmt.Println("  query               - Execute SQL queries against resource database")
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

func runQuery(args []string) {
	fs := flag.NewFlagSet("query", flag.ExitOnError)

	// Output format flags
	output := fs.String("output", "table", "Output format (table, csv, json)")
	
	// Database path override
	database := fs.String("database", "", "Database path (defaults to ~/.corkscrew/db/corkscrew.duckdb)")
	
	// Input options
	stdin := fs.Bool("stdin", false, "Read SQL query from stdin")
	file := fs.String("file", "", "Read SQL query from file")
	
	// Performance and debugging flags
	timeout := fs.Int("timeout", 30, "Query timeout in seconds")
	verbose := fs.Bool("verbose", false, "Show execution time and row count")
	stream := fs.Bool("stream", false, "Use streaming mode for large result sets")

	fs.Parse(args)

	// Determine SQL query source
	var sqlQuery string
	var err error

	if *stdin {
		// Read from stdin
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Failed to read from stdin: %v", err)
		}
		sqlQuery = string(stdinBytes)
	} else if *file != "" {
		// Read from file
		fileBytes, err := os.ReadFile(*file)
		if err != nil {
			log.Fatalf("Failed to read file %s: %v", *file, err)
		}
		sqlQuery = string(fileBytes)
	} else if len(fs.Args()) > 0 {
		// Use command line argument
		sqlQuery = fs.Args()[0]
	} else {
		fmt.Fprintf(os.Stderr, "❌ Error: SQL query is required\n")
		fmt.Fprintf(os.Stderr, "  💡 Usage options:\n")
		fmt.Fprintf(os.Stderr, "     corkscrew query \"SELECT * FROM aws_resources\"\n")
		fmt.Fprintf(os.Stderr, "     echo \"SELECT * FROM aws_resources\" | corkscrew query --stdin\n")
		fmt.Fprintf(os.Stderr, "     corkscrew query --file query.sql\n")
		os.Exit(1) // Exit code 1 for user input errors
	}

	sqlQuery = strings.TrimSpace(sqlQuery)
	if sqlQuery == "" {
		fmt.Fprintf(os.Stderr, "❌ Error: Empty SQL query provided\n")
		fmt.Fprintf(os.Stderr, "  💡 Usage examples:\n")
		fmt.Fprintf(os.Stderr, "     corkscrew query \"SELECT * FROM aws_resources\"\n")
		fmt.Fprintf(os.Stderr, "     echo \"SELECT * FROM aws_resources\" | corkscrew query --stdin\n")
		fmt.Fprintf(os.Stderr, "     corkscrew query --file query.sql\n")
		os.Exit(1) // Exit code 1 for user input errors
	}

	// Validate output format
	validFormats := map[string]bool{"table": true, "csv": true, "json": true}
	if !validFormats[*output] {
		fmt.Fprintf(os.Stderr, "❌ Error: Invalid output format: %s\n", *output)
		fmt.Fprintf(os.Stderr, "  💡 Valid options: table, csv, json\n")
		os.Exit(1) // Exit code 1 for user input errors
	}

	// Get database path
	var dbPath string
	if *database != "" {
		dbPath = *database
	} else {
		dbPath, err = db.GetUnifiedDatabasePath()
		if err != nil {
			log.Fatalf("Failed to get database path: %v", err)
		}
	}

	// Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatalf("Database not found at %s. Run a scan first to populate the database.", dbPath)
	}

	if *verbose {
		fmt.Printf("🔍 Executing query against database: %s\n", dbPath)
		fmt.Printf("📊 Output format: %s\n", *output)
		fmt.Printf("⏱️  Timeout: %d seconds\n", *timeout)
		fmt.Printf("📝 Query: %s\n\n", sqlQuery)
	}

	// Create database connection
	graphLoader, err := db.NewGraphLoader(dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer graphLoader.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()

	// Validate SQL syntax before execution
	if err := validateSQLSyntax(sqlQuery); err != nil {
		handleSQLError(err, sqlQuery)
		os.Exit(2) // Exit code 2 for syntax errors
	}

	// Execute query with streaming if appropriate
	start := time.Now()
	
	// Check if we should use streaming (forced with --stream flag or estimated large result)
	useStreaming := *stream || shouldUseStreaming(sqlQuery)
	
	if useStreaming {
		// Use streaming execution
		if err := executeStreamingQuery(ctx, graphLoader, sqlQuery, *output, start, *verbose); err != nil {
			if strings.Contains(err.Error(), "query execution failed") || strings.Contains(err.Error(), "syntax validation failed") {
				handleDuckDBError(err, sqlQuery, graphLoader)
				os.Exit(3) // Exit code 3 for execution errors
			} else {
				fmt.Fprintf(os.Stderr, "❌ Failed to execute streaming query: %v\n", err)
				os.Exit(4) // Exit code 4 for output formatting errors
			}
		}
	} else {
		// Use traditional execution for smaller results
		results, err := graphLoader.Query(ctx, sqlQuery)
		duration := time.Since(start)

		if err != nil {
			handleDuckDBError(err, sqlQuery, graphLoader)
			os.Exit(3) // Exit code 3 for execution errors
		}

		// Auto-switch to streaming if result set is very large (>10k rows)
		if len(results) > 10000 {
			fmt.Fprintf(os.Stderr, "⚠️  Large result set detected (%d rows). Consider using --stream flag for better performance.\n", len(results))
		}

		// Format and output results
		if err := formatAndOutputResults(results, *output, duration, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to format output: %v\n", err)
			os.Exit(4) // Exit code 4 for output formatting errors
		}
	}
}

func formatAndOutputResults(results []map[string]interface{}, format string, duration time.Duration, verbose bool) error {
	rowCount := len(results)
	
	switch format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(results); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}

	case "csv":
		writer := csv.NewWriter(os.Stdout)
		defer writer.Flush()

		if rowCount > 0 {
			// Write header
			var headers []string
			for key := range results[0] {
				headers = append(headers, key)
			}
			if err := writer.Write(headers); err != nil {
				return fmt.Errorf("failed to write CSV headers: %w", err)
			}

			// Write rows
			for _, result := range results {
				var row []string
				for _, header := range headers {
					val := result[header]
					if val == nil {
						row = append(row, "")
					} else {
						row = append(row, fmt.Sprintf("%v", val))
					}
				}
				if err := writer.Write(row); err != nil {
					return fmt.Errorf("failed to write CSV row: %w", err)
				}
			}
		}

	case "table":
		if rowCount == 0 {
			fmt.Println("No results found.")
		} else {
			// Create table output
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

			// Get column names from first row
			var columns []string
			for key := range results[0] {
				columns = append(columns, key)
			}

			// Write header
			for i, col := range columns {
				if i > 0 {
					fmt.Fprint(w, "\t")
				}
				fmt.Fprint(w, col)
			}
			fmt.Fprintln(w)

			// Write separator
			for i, col := range columns {
				if i > 0 {
					fmt.Fprint(w, "\t")
				}
				fmt.Fprint(w, strings.Repeat("-", len(col)))
			}
			fmt.Fprintln(w)

			// Write rows
			for _, result := range results {
				for i, col := range columns {
					if i > 0 {
						fmt.Fprint(w, "\t")
					}
					val := result[col]
					if val == nil {
						fmt.Fprint(w, "NULL")
					} else {
						// Format values nicely
						switch v := val.(type) {
						case string:
							// Truncate long strings for table display
							if len(v) > 50 {
								fmt.Fprint(w, v[:47]+"...")
							} else {
								fmt.Fprint(w, v)
							}
						case float64:
							if v == float64(int64(v)) {
								fmt.Fprintf(w, "%.0f", v)
							} else {
								fmt.Fprintf(w, "%.2f", v)
							}
						default:
							fmt.Fprint(w, val)
						}
					}
				}
				fmt.Fprintln(w)
			}

			w.Flush()
		}

	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}

	// Show execution stats if verbose or table format
	if verbose || format == "table" {
		fmt.Fprintf(os.Stderr, "\n")
		if verbose {
			fmt.Fprintf(os.Stderr, "Query executed in %v, returned %d rows\n", duration, rowCount)
		} else {
			fmt.Fprintf(os.Stderr, "(%d rows)\n", rowCount)
		}
	}

	return nil
}

// SQL Error Handling and Validation

// SQLError represents a SQL syntax or execution error with enhanced information
type SQLError struct {
	Type        string
	Message     string
	Line        int
	Column      int
	Query       string
	Suggestion  string
	AvailableCols []string
}

// Error implements the error interface
func (e *SQLError) Error() string {
	return e.Message
}

// validateSQLSyntax performs basic SQL syntax validation before execution
func validateSQLSyntax(query string) error {
	query = strings.TrimSpace(query)
	
	if query == "" {
		return &SQLError{
			Type:    "EmptyQuery",
			Message: "Empty SQL query provided",
		}
	}

	// Check for common SQL syntax issues
	if err := checkCommonSyntaxErrors(query); err != nil {
		return err
	}

	// Check for basic SQL structure
	if err := checkBasicSQLStructure(query); err != nil {
		return err
	}

	return nil
}

// checkCommonSyntaxErrors identifies common SQL syntax mistakes
func checkCommonSyntaxErrors(query string) error {
	// Convert to uppercase for pattern matching
	upperQuery := strings.ToUpper(query)
	
	// Common typos and their corrections
	commonErrors := map[string]string{
		"FORM":   "FROM",
		"SLEECT": "SELECT",
		"SLECT":  "SELECT",
		"SELCT":  "SELECT",
		"WHRE":   "WHERE",
		"GRUP":   "GROUP",
		"ODER":   "ORDER",
		"JION":   "JOIN",
		"CUNT":   "COUNT",
		"AVERGAE": "AVERAGE",
		"DESTINCT": "DISTINCT",
	}

	// Check for typos and provide suggestions
	for typo, correction := range commonErrors {
		if strings.Contains(upperQuery, typo) {
			line, col := findWordPosition(query, typo)
			return &SQLError{
				Type:       "TypoError",
				Message:    fmt.Sprintf("SQL syntax error at line %d, column %d", line, col),
				Line:       line,
				Column:     col,
				Query:      query,
				Suggestion: fmt.Sprintf("Did you mean \"%s\"?", correction),
			}
		}
	}

	// Check for missing quotes around string literals
	if strings.Contains(upperQuery, "= ") && !strings.Contains(query, "'") && !strings.Contains(query, "\"") {
		words := strings.Fields(query)
		for i, word := range words {
			if word == "=" && i+1 < len(words) {
				nextWord := words[i+1]
				if !isNumeric(nextWord) && !strings.HasPrefix(nextWord, "'") && !strings.HasPrefix(nextWord, "\"") {
					line, col := findWordPosition(query, nextWord)
					return &SQLError{
						Type:       "QuoteError",
						Message:    fmt.Sprintf("Possible missing quotes around string literal at line %d, column %d", line, col),
						Line:       line,
						Column:     col,
						Query:      query,
						Suggestion: fmt.Sprintf("Consider wrapping '%s' in quotes: '%s'", nextWord, nextWord),
					}
				}
			}
		}
	}

	return nil
}

// checkBasicSQLStructure validates basic SQL query structure
func checkBasicSQLStructure(query string) error {
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	
	// Must start with a valid SQL statement
	validStarts := []string{"SELECT", "WITH", "SHOW", "DESCRIBE", "EXPLAIN"}
	startsValid := false
	for _, start := range validStarts {
		if strings.HasPrefix(upperQuery, start) {
			startsValid = true
			break
		}
	}
	
	if !startsValid {
		return &SQLError{
			Type:       "InvalidStatement",
			Message:    "SQL query must start with a valid statement",
			Line:       1,
			Column:     1,
			Query:      query,
			Suggestion: "Valid statement types: SELECT, WITH, SHOW, DESCRIBE, EXPLAIN",
		}
	}

	// Check for balanced parentheses
	if err := checkBalancedParentheses(query); err != nil {
		return err
	}

	return nil
}

// checkBalancedParentheses ensures parentheses are properly balanced
func checkBalancedParentheses(query string) error {
	balance := 0
	line := 1
	col := 1
	
	for _, char := range query {
		if char == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		
		if char == '(' {
			balance++
		} else if char == ')' {
			balance--
			if balance < 0 {
				return &SQLError{
					Type:       "UnbalancedParentheses",
					Message:    fmt.Sprintf("Unmatched closing parenthesis at line %d, column %d", line, col),
					Line:       line,
					Column:     col,
					Query:      query,
					Suggestion: "Check for missing opening parenthesis",
				}
			}
		}
	}
	
	if balance > 0 {
		return &SQLError{
			Type:       "UnbalancedParentheses",
			Message:    "Unclosed parentheses in query",
			Line:       line,
			Column:     col,
			Query:      query,
			Suggestion: fmt.Sprintf("Missing %d closing parenthesis(es)", balance),
		}
	}
	
	return nil
}

// handleSQLError formats and displays SQL validation errors
func handleSQLError(err error, query string) {
	if sqlErr, ok := err.(*SQLError); ok {
		fmt.Fprintf(os.Stderr, "❌ Error: %s\n", sqlErr.Message)
		
		if sqlErr.Line > 0 && sqlErr.Column > 0 {
			lines := strings.Split(query, "\n")
			if sqlErr.Line <= len(lines) {
				fmt.Fprintf(os.Stderr, "  %s\n", lines[sqlErr.Line-1])
				if sqlErr.Column > 0 {
					padding := strings.Repeat(" ", sqlErr.Column-1)
					fmt.Fprintf(os.Stderr, "  %s^^^^\n", padding)
				}
			}
		}
		
		if sqlErr.Suggestion != "" {
			fmt.Fprintf(os.Stderr, "  💡 %s\n", sqlErr.Suggestion)
		}
	} else {
		fmt.Fprintf(os.Stderr, "❌ SQL validation error: %v\n", err)
	}
}

// handleDuckDBError parses and handles DuckDB-specific errors
func handleDuckDBError(err error, query string, graphLoader *db.GraphLoader) {
	errorMsg := err.Error()
	
	// Parse DuckDB error patterns
	if strings.Contains(errorMsg, "Binder Error") {
		handleBinderError(errorMsg, query, graphLoader)
	} else if strings.Contains(errorMsg, "Parser Error") {
		handleParserError(errorMsg, query)
	} else if strings.Contains(errorMsg, "Catalog Error") {
		handleCatalogError(errorMsg, query, graphLoader)
	} else if strings.Contains(errorMsg, "Conversion Error") {
		handleConversionError(errorMsg, query)
	} else if strings.Contains(errorMsg, "context deadline exceeded") {
		handleTimeoutError(errorMsg, query)
	} else {
		// Generic error handling
		fmt.Fprintf(os.Stderr, "❌ Query execution error: %s\n", errorMsg)
		suggestGeneralHelp(query)
	}
}

// handleBinderError handles column/table not found errors
func handleBinderError(errorMsg, query string, graphLoader *db.GraphLoader) {
	fmt.Fprintf(os.Stderr, "❌ Error: %s\n", errorMsg)
	
	// Extract column name from error message if possible
	columnNotFoundRegex := regexp.MustCompile(`Referenced column "([^"]+)" not found`)
	if matches := columnNotFoundRegex.FindStringSubmatch(errorMsg); len(matches) > 1 {
		columnName := matches[1]
		fmt.Fprintf(os.Stderr, "  🔍 Column '%s' not found\n", columnName)
		
		// Suggest available columns
		if availableColumns := getAvailableColumns(graphLoader, query); len(availableColumns) > 0 {
			fmt.Fprintf(os.Stderr, "  💡 Available columns: %s\n", strings.Join(availableColumns, ", "))
			
			// Suggest similar column names
			if suggestions := findSimilarColumns(columnName, availableColumns); len(suggestions) > 0 {
				fmt.Fprintf(os.Stderr, "  💡 Did you mean: %s?\n", strings.Join(suggestions, " or "))
			}
		}
	}
	
	// Check for table not found (DuckDB format)
	tableNotFoundRegex := regexp.MustCompile(`Table with name ([a-zA-Z_][a-zA-Z0-9_]*) does not exist`)
	if matches := tableNotFoundRegex.FindStringSubmatch(errorMsg); len(matches) > 1 {
		tableName := matches[1]
		fmt.Fprintf(os.Stderr, "  🔍 Table '%s' not found\n", tableName)
		
		availableTables := []string{"aws_resources", "azure_resources", "cloud_relationships", "scan_metadata", "api_action_metadata"}
		fmt.Fprintf(os.Stderr, "  💡 Available tables: %s\n", strings.Join(availableTables, ", "))
		
		if suggestions := findSimilarColumns(tableName, availableTables); len(suggestions) > 0 {
			fmt.Fprintf(os.Stderr, "  💡 Did you mean: %s?\n", strings.Join(suggestions, " or "))
		}
	}
}

// handleParserError handles SQL parsing errors
func handleParserError(errorMsg, query string) {
	fmt.Fprintf(os.Stderr, "❌ SQL Parser Error: %s\n", errorMsg)
	
	// Extract line information from error message
	lineRegex := regexp.MustCompile(`LINE (\d+):`)
	if matches := lineRegex.FindStringSubmatch(errorMsg); len(matches) > 1 {
		lineNum := matches[1]
		fmt.Fprintf(os.Stderr, "  🔍 Error occurred around line %s\n", lineNum)
	}
	
	// Show the problematic query with line numbers
	showQueryWithLineNumbers(query)
	
	// Provide common syntax suggestions
	fmt.Fprintf(os.Stderr, "  💡 Common fixes:\n")
	fmt.Fprintf(os.Stderr, "     - Check for missing commas between column names\n")
	fmt.Fprintf(os.Stderr, "     - Ensure proper quotes around string values\n")
	fmt.Fprintf(os.Stderr, "     - Verify parentheses are balanced\n")
	fmt.Fprintf(os.Stderr, "     - Check for reserved keywords (use quotes if needed)\n")
}

// handleCatalogError handles database catalog errors
func handleCatalogError(errorMsg, query string, graphLoader *db.GraphLoader) {
	fmt.Fprintf(os.Stderr, "❌ Database Catalog Error: %s\n", errorMsg)
	
	// Check for table not found (DuckDB format)
	tableNotFoundRegex := regexp.MustCompile(`Table with name ([a-zA-Z_][a-zA-Z0-9_]*) does not exist`)
	if matches := tableNotFoundRegex.FindStringSubmatch(errorMsg); len(matches) > 1 {
		tableName := matches[1]
		fmt.Fprintf(os.Stderr, "  🔍 Table '%s' not found\n", tableName)
		
		availableTables := []string{"aws_resources", "azure_resources", "cloud_relationships", "scan_metadata", "api_action_metadata"}
		fmt.Fprintf(os.Stderr, "  💡 Available tables: %s\n", strings.Join(availableTables, ", "))
		
		if suggestions := findSimilarColumns(tableName, availableTables); len(suggestions) > 0 {
			fmt.Fprintf(os.Stderr, "  💡 Did you mean: %s?\n", strings.Join(suggestions, " or "))
		}
		
		fmt.Fprintf(os.Stderr, "  💡 Make sure you've run a scan to populate the database\n")
		fmt.Fprintf(os.Stderr, "     Example: corkscrew scan --provider aws --services s3,ec2\n")
	} else if strings.Contains(errorMsg, "does not exist") {
		fmt.Fprintf(os.Stderr, "  💡 Make sure you've run a scan to populate the database\n")
		fmt.Fprintf(os.Stderr, "     Example: corkscrew scan --provider aws --services s3,ec2\n")
	}
}

// handleConversionError handles data type conversion errors
func handleConversionError(errorMsg, query string) {
	fmt.Fprintf(os.Stderr, "❌ Data Conversion Error: %s\n", errorMsg)
	fmt.Fprintf(os.Stderr, "  💡 Tips:\n")
	fmt.Fprintf(os.Stderr, "     - Check data types in comparisons\n")
	fmt.Fprintf(os.Stderr, "     - Use CAST() or :: for explicit conversions\n")
	fmt.Fprintf(os.Stderr, "     - Ensure numeric literals don't have quotes\n")
}

// handleTimeoutError handles query timeout errors
func handleTimeoutError(errorMsg, query string) {
	fmt.Fprintf(os.Stderr, "❌ Query Timeout: The query took too long to execute\n")
	fmt.Fprintf(os.Stderr, "  💡 Suggestions:\n")
	fmt.Fprintf(os.Stderr, "     - Increase timeout with --timeout flag\n")
	fmt.Fprintf(os.Stderr, "     - Add WHERE clauses to limit results\n")
	fmt.Fprintf(os.Stderr, "     - Use LIMIT to restrict the number of rows\n")
	fmt.Fprintf(os.Stderr, "     - Consider adding indexes for better performance\n")
}

// getAvailableColumns retrieves available columns for the tables in the query
func getAvailableColumns(graphLoader *db.GraphLoader, query string) []string {
	ctx := context.Background()
	
	// Extract table names from query
	tables := extractTableNames(query)
	var allColumns []string
	
	for _, table := range tables {
		// Get column information for each table
		columnQuery := fmt.Sprintf("DESCRIBE %s", table)
		results, err := graphLoader.Query(ctx, columnQuery)
		if err == nil {
			for _, row := range results {
				if columnName, ok := row["column_name"].(string); ok {
					allColumns = append(allColumns, columnName)
				}
			}
		}
	}
	
	// If no tables found or error, return common columns
	if len(allColumns) == 0 {
		allColumns = []string{"id", "name", "type", "service", "region", "arn", "tags", "created_at", "scanned_at"}
	}
	
	sort.Strings(allColumns)
	return allColumns
}

// extractTableNames extracts table names from SQL query
func extractTableNames(query string) []string {
	var tables []string
	
	// Enhanced regex patterns to handle various table name formats
	patterns := []string{
		// FROM/JOIN with optional schema and alias: FROM schema.table AS alias, FROM table alias
		`(?i)\b(?:FROM|JOIN)\s+(?:([a-zA-Z_][a-zA-Z0-9_]*\.)?([a-zA-Z_][a-zA-Z0-9_]*))(?:\s+(?:AS\s+)?[a-zA-Z_][a-zA-Z0-9_]*)?`,
		// Subqueries: FROM (SELECT ...) AS alias
		`(?i)\bFROM\s*\(\s*SELECT\s+.*?\)\s+(?:AS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)`,
		// CTE references: WITH cte AS (...) SELECT FROM cte
		`(?i)\bWITH\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+AS`,
	}
	
	for _, pattern := range patterns {
		regex := regexp.MustCompile(pattern)
		matches := regex.FindAllStringSubmatch(query, -1)
		
		for _, match := range matches {
			if len(match) > 1 {
				// For schema.table pattern, extract the table name (index 2)
				if len(match) > 2 && match[2] != "" {
					tables = append(tables, match[2])
				} else if match[1] != "" {
					// For other patterns, use index 1
					tables = append(tables, match[1])
				}
			}
		}
	}
	
	// Remove duplicates
	tables = removeDuplicateStrings(tables)
	
	// Default to common tables if none found
	if len(tables) == 0 {
		tables = []string{"aws_resources"}
	}
	
	return tables
}

// removeDuplicateStrings removes duplicate strings from a slice
func removeDuplicateStrings(slice []string) []string {
	keys := make(map[string]bool)
	var result []string
	
	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}
	
	return result
}

// findSimilarColumns finds columns with similar names using Levenshtein-like comparison
func findSimilarColumns(target string, available []string) []string {
	var suggestions []string
	target = strings.ToLower(target)
	
	for _, col := range available {
		colLower := strings.ToLower(col)
		
		// Exact substring match
		if strings.Contains(colLower, target) || strings.Contains(target, colLower) {
			suggestions = append(suggestions, col)
			continue
		}
		
		// Similar length and characters
		if len(target) > 2 && len(col) > 2 {
			if similarity := calculateSimilarity(target, colLower); similarity > 0.6 {
				suggestions = append(suggestions, col)
			}
		}
	}
	
	// Limit suggestions to avoid clutter
	if len(suggestions) > 3 {
		suggestions = suggestions[:3]
	}
	
	return suggestions
}

// calculateSimilarity calculates simple string similarity
func calculateSimilarity(a, b string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	
	longer, shorter := a, b
	if len(a) < len(b) {
		longer, shorter = b, a
	}
	
	common := 0
	for i := 0; i < len(shorter); i++ {
		if i < len(longer) && shorter[i] == longer[i] {
			common++
		}
	}
	
	return float64(common) / float64(len(longer))
}

// Helper functions

// findWordPosition finds the line and column position of a word in text
func findWordPosition(text, word string) (int, int) {
	lines := strings.Split(text, "\n")
	wordUpper := strings.ToUpper(word)
	
	for lineNum, line := range lines {
		lineUpper := strings.ToUpper(line)
		if col := strings.Index(lineUpper, wordUpper); col >= 0 {
			return lineNum + 1, col + 1
		}
	}
	return 1, 1
}

// isNumeric checks if a string represents a number
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			if r != '.' && r != '-' && r != '+' {
				return false
			}
		}
	}
	return true
}

// showQueryWithLineNumbers displays the query with line numbers
func showQueryWithLineNumbers(query string) {
	lines := strings.Split(query, "\n")
	fmt.Fprintf(os.Stderr, "  📝 Query:\n")
	for i, line := range lines {
		fmt.Fprintf(os.Stderr, "    %2d: %s\n", i+1, line)
	}
}

// suggestGeneralHelp provides general help for SQL queries
func suggestGeneralHelp(query string) {
	fmt.Fprintf(os.Stderr, "  💡 General suggestions:\n")
	fmt.Fprintf(os.Stderr, "     - Check table names: aws_resources, azure_resources, cloud_relationships\n")
	fmt.Fprintf(os.Stderr, "     - Use 'corkscrew query \"SHOW TABLES\"' to see available tables\n")
	fmt.Fprintf(os.Stderr, "     - Use 'corkscrew query \"DESCRIBE table_name\"' to see column information\n")
	fmt.Fprintf(os.Stderr, "     - Verify your SQL syntax against DuckDB documentation\n")
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

// shouldUseStreaming determines if a query should use streaming based on heuristics
func shouldUseStreaming(query string) bool {
	query = strings.ToUpper(strings.TrimSpace(query))
	
	// Use streaming for queries that are likely to return large result sets
	largeResultIndicators := []string{
		"SELECT * FROM",           // Full table scans
		"COUNT(*)",               // Potentially large aggregations
		"GROUP BY",               // Aggregations that might return many groups
		"ORDER BY",               // Sorting large datasets
		"DISTINCT",               // Potentially large distinct operations
	}
	
	// Avoid streaming for clearly small result queries
	smallResultIndicators := []string{
		"LIMIT 1",
		"LIMIT 10",
		"LIMIT 100",
		"TOP 1",
		"TOP 10",
		"TOP 100",
	}
	
	// Check for small result indicators first
	for _, indicator := range smallResultIndicators {
		if strings.Contains(query, indicator) {
			return false
		}
	}
	
	// Check for large result indicators
	for _, indicator := range largeResultIndicators {
		if strings.Contains(query, indicator) {
			return true
		}
	}
	
	return false
}

// executeStreamingQuery executes a query using streaming and formats output in real-time
func executeStreamingQuery(ctx context.Context, graphLoader *db.GraphLoader, sqlQuery, outputFormat string, startTime time.Time, verbose bool) error {
	// Create a query engine for streaming
	engine, err := query.NewDuckDBQueryEngine()
	if err != nil {
		return fmt.Errorf("failed to create query engine: %w", err)
	}
	defer engine.Close()
	
	// Execute streaming query
	rowChan, err := engine.ExecuteStreaming(ctx, sqlQuery)
	if err != nil {
		return fmt.Errorf("failed to start streaming query: %w", err)
	}
	
	// Process streaming results
	var columns []query.ColumnInfo
	var columnNames []string
	rowCount := 0
	
	// Create output writers based on format
	var csvWriter *csv.Writer
	var tabWriter *tabwriter.Writer
	var jsonFirst bool = true
	
	switch outputFormat {
	case "csv":
		csvWriter = csv.NewWriter(os.Stdout)
		defer csvWriter.Flush()
	case "table":
		tabWriter = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		defer tabWriter.Flush()
	case "json":
		fmt.Print("[")
		jsonFirst = true
	}
	
	// Process each streaming row
	for streamingRow := range rowChan {
		if streamingRow.Error != nil {
			return streamingRow.Error
		}
		
		if streamingRow.EOF {
			// Final row with statistics
			duration := time.Since(startTime)
			if streamingRow.Stats != nil {
				rowCount = streamingRow.Stats.RowsReturned
			}
			
			// Close JSON array
			if outputFormat == "json" {
				fmt.Println("]")
			}
			
			// Show statistics if verbose
			if verbose {
				fmt.Fprintf(os.Stderr, "\n🔍 Query completed in %v\n", duration)
				fmt.Fprintf(os.Stderr, "📊 Rows returned: %d\n", rowCount)
			}
			
			break
		}
		
		// Handle first row with column info
		if streamingRow.Columns != nil {
			columns = streamingRow.Columns
			columnNames = make([]string, len(columns))
			for i, col := range columns {
				columnNames[i] = col.Name
			}
			
			// Write headers for appropriate formats
			switch outputFormat {
			case "csv":
				if err := csvWriter.Write(columnNames); err != nil {
					return fmt.Errorf("failed to write CSV header: %w", err)
				}
			case "table":
				// Write header
				for i, col := range columnNames {
					if i > 0 {
						fmt.Fprint(tabWriter, "\t")
					}
					fmt.Fprint(tabWriter, col)
				}
				fmt.Fprintln(tabWriter)
				
				// Write separator
				for i, col := range columnNames {
					if i > 0 {
						fmt.Fprint(tabWriter, "\t")
					}
					fmt.Fprint(tabWriter, strings.Repeat("-", len(col)))
				}
				fmt.Fprintln(tabWriter)
			}
		}
		
		// Write data row
		if streamingRow.Data != nil {
			rowCount++
			
			switch outputFormat {
			case "csv":
				row := make([]string, len(columnNames))
				for i, colName := range columnNames {
					val := streamingRow.Data[colName]
					if val == nil {
						row[i] = ""
					} else {
						row[i] = fmt.Sprintf("%v", val)
					}
				}
				if err := csvWriter.Write(row); err != nil {
					return fmt.Errorf("failed to write CSV row: %w", err)
				}
				
			case "table":
				for i, colName := range columnNames {
					if i > 0 {
						fmt.Fprint(tabWriter, "\t")
					}
					val := streamingRow.Data[colName]
					if val == nil {
						fmt.Fprint(tabWriter, "")
					} else {
						fmt.Fprint(tabWriter, fmt.Sprintf("%v", val))
					}
				}
				fmt.Fprintln(tabWriter)
				
			case "json":
				if !jsonFirst {
					fmt.Print(",")
				}
				jsonFirst = false
				
				jsonBytes, err := json.Marshal(streamingRow.Data)
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Print(string(jsonBytes))
			}
		}
	}
	
	// Handle case where no rows were returned
	if rowCount == 0 && outputFormat == "table" {
		fmt.Println("No results found.")
	}
	
	return nil
}

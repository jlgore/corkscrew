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
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	// tea "github.com/charmbracelet/bubbletea"
	// "github.com/jlgore/corkscrew/diagrams/pkg/renderer"
	// "github.com/jlgore/corkscrew/diagrams/pkg/ui"
	"github.com/jlgore/corkscrew/internal/client"
	"github.com/jlgore/corkscrew/internal/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/server"
	"github.com/jlgore/corkscrew/pkg/query"
	"github.com/jlgore/corkscrew/pkg/query/compliance"
	"github.com/jlgore/corkscrew/pkg/smartscan"
	"gopkg.in/yaml.v3"
)

// Build-time variables set by GoReleaser
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// parameterFlags implements flag.Value for collecting multiple --param flags
type parameterFlags map[string]interface{}


func (p parameterFlags) String() string {
	return fmt.Sprintf("%v", map[string]interface{}(p))
}

func (p parameterFlags) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("parameter must be in format key=value")
	}
	
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	
	// Try to parse as different types
	if val == "true" {
		p[key] = true
	} else if val == "false" {
		p[key] = false
	} else if strings.Contains(val, ",") {
		// Handle list parameters
		items := strings.Split(val, ",")
		var list []interface{}
		for _, item := range items {
			list = append(list, strings.TrimSpace(item))
		}
		p[key] = list
	} else {
		// Default to string
		p[key] = val
	}
	
	return nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "init":
		runInit(os.Args[2:])
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
	case "config":
		runConfig(os.Args[2:])
	case "plugin":
		runPlugin(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
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
	fmt.Println("  # Multi-Region Scanning")
	fmt.Println("  corkscrew scan --provider aws --region us-east-1,us-west-2,eu-west-1")
	fmt.Println("  corkscrew scan --provider aws --region all")
	fmt.Println("  corkscrew scan --provider aws --services s3,ec2 --region us-east-1,us-west-2")
	fmt.Println("  corkscrew scan --provider azure --region eastus,westus2 --concurrency 5")
	fmt.Println("  corkscrew scan --provider aws --show-empty --output json")
	fmt.Println()
	fmt.Println("  # Other Commands")
	fmt.Println("  corkscrew discover --provider aws")
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
	fmt.Println("  # Compliance Query Examples")
	fmt.Println("  corkscrew query --control jlgore/cfi-ccc/CCC.C01")
	fmt.Println("  corkscrew query --pack jlgore/cfi-ccc/s3-security")
	fmt.Println("  corkscrew query --compliance --tag encryption --param required_encryption=aws:kms")
	fmt.Println("  corkscrew query --list-packs")
	fmt.Println()
	fmt.Println("  # Pack Management Examples")
	fmt.Println("  corkscrew query pack search \"aws security\"")
	fmt.Println("  corkscrew query pack install jlgore/cfi-ccc")
	fmt.Println("  corkscrew query pack list")
	fmt.Println("  corkscrew query pack update --all")
	fmt.Println("  corkscrew query pack validate jlgore/cfi-ccc")
	fmt.Println()
	fmt.Println("  # Configuration Examples")
	fmt.Println("  corkscrew config init         # Create default configuration file")
	fmt.Println("  corkscrew config show         # Display current configuration")
	fmt.Println("  corkscrew config validate     # Validate configuration file")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init                - Initialize Corkscrew with dependencies and plugins")
	fmt.Println("  config              - Manage Corkscrew configuration (init, show, validate)")
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
	fmt.Println("  serve               - Start gRPC API server")
	fmt.Println("  version             - Show version information")
	fmt.Println()
	fmt.Println("Supported Providers:")
	fmt.Println("  aws         - Amazon Web Services")
	fmt.Println("  azure       - Microsoft Azure")
}

func runScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure, gcp, kubernetes)")
	servicesStr := fs.String("services", "", "Comma-separated list of services (default: from config)")
	regionsStr := fs.String("region", "", "Comma-separated regions or 'all' (default: from config)")
	outputFormat := fs.String("output", "table", "Output format (table, json, csv)")
	showEmpty := fs.Bool("show-empty", false, "Show empty regions and services")
	configPath := fs.String("config", "", "Path to configuration file")
	concurrency := fs.Int("concurrency", 3, "Number of regions to scan concurrently")
	saveToFile := fs.Bool("save", false, "Save results to timestamped JSON file")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	// Parse services
	services := []string{}
	if *servicesStr != "" {
		services = strings.Split(*servicesStr, ",")
		for i, s := range services {
			services[i] = strings.TrimSpace(s)
		}
	}

	// Parse regions
	regions := []string{}
	if *regionsStr != "" {
		regions = strings.Split(*regionsStr, ",")
		for i, r := range regions {
			regions[i] = strings.TrimSpace(r)
		}
	}

	// Run enhanced multi-region scan
	options := smartscan.EnhancedScanOptions{
		Provider:       *providerName,
		Regions:        regions,
		Services:       services,
		OutputFormat:   *outputFormat,
		SaveToFile:     *saveToFile,
		ShowEmpty:      *showEmpty,
		ConfigPath:     *configPath,
		MaxConcurrency: *concurrency,
	}

	if err := smartscan.RunEnhancedScan(context.Background(), options); err != nil {
		log.Fatalf("Scan failed: %v", err)
	}
}


func runDiscover(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	
	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure, gcp, kubernetes)")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	outputFormat := fs.String("output", "table", "Output format (table, json)")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("🔍 Discovering services for provider: %s\n", *providerName)
	
	// Initialize plugin client
	pc, err := client.NewPluginClient(*providerName)
	if err != nil {
		log.Fatalf("Failed to initialize plugin client: %v", err)
	}
	defer pc.Close()
	
	provider, err := pc.GetProvider()
	if err != nil {
		log.Fatalf("Failed to get provider: %v", err)
	}
	
	// Create discover request
	req := &pb.DiscoverServicesRequest{
		ForceRefresh: *verbose, // Use verbose as force refresh for now
	}
	
	// Execute discovery
	resp, err := provider.DiscoverServices(context.Background(), req)
	if err != nil {
		log.Fatalf("Discovery failed: %v", err)
	}
	
	// Handle results based on output format
	switch *outputFormat {
	case "json":
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
	default:
		printDiscoverResults(resp)
	}
}

func printDiscoverResults(resp *pb.DiscoverServicesResponse) {
	fmt.Printf("\n✅ Discovery completed successfully!\n")
	fmt.Printf("📊 Found %d services\n\n", len(resp.Services))
	
	if len(resp.Services) > 0 {
		// Print all services
		fmt.Printf("✅ Available services (%d):\n", len(resp.Services))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "Service\tDisplay Name\tResource Types")
		fmt.Fprintln(w, "-------\t------------\t--------------")
		
		for _, svc := range resp.Services {
			fmt.Fprintf(w, "%s\t%s\t%d\n", 
				svc.Name, 
				svc.DisplayName,
				len(svc.ResourceTypes))
		}
		w.Flush()
	}
	
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	
	providerName := fs.String("provider", "aws", "Cloud provider")
	servicesStr := fs.String("services", "", "Comma-separated list of services")
	regionsStr := fs.String("region", "", "Region to list (default: current)")
	resourceType := fs.String("type", "", "Filter by resource type")
	limit := fs.Int("limit", 50, "Maximum number of resources to list")
	outputFormat := fs.String("output", "table", "Output format (table, json, csv)")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	// Parse services
	services := []string{}
	if *servicesStr != "" {
		services = strings.Split(*servicesStr, ",")
		for i, s := range services {
			services[i] = strings.TrimSpace(s)
		}
	}
	
	// Parse regions
	regions := []string{}
	if *regionsStr != "" {
		regions = strings.Split(*regionsStr, ",")
		for i, r := range regions {
			regions[i] = strings.TrimSpace(r)
		}
	}
	
	fmt.Printf("📋 Listing resources for provider: %s\n", *providerName)
	if len(services) > 0 {
		fmt.Printf("   Services: %s\n", strings.Join(services, ", "))
	}
	if len(regions) > 0 {
		fmt.Printf("   Regions: %s\n", strings.Join(regions, ", "))
	}
	if *resourceType != "" {
		fmt.Printf("   Resource type: %s\n", *resourceType)
	}
	
	// Initialize plugin client
	pc, err := client.NewPluginClient(*providerName)
	if err != nil {
		log.Fatalf("Failed to initialize plugin client: %v", err)
	}
	defer pc.Close()
	
	provider, err := pc.GetProvider()
	if err != nil {
		log.Fatalf("Failed to get provider: %v", err)
	}
	
	// Create list request
	req := &pb.ListResourcesRequest{
		Service:      "", // Services will be set based on first service
		Region:       "", // Region will be set based on first region
		ResourceType: *resourceType,
		MaxResults:   int32(*limit),
	}
	if len(services) > 0 {
		req.Service = services[0]
	}
	if len(regions) > 0 {
		req.Region = regions[0]
	}
	
	// Execute list
	resp, err := provider.ListResources(context.Background(), req)
	if err != nil {
		log.Fatalf("List failed: %v", err)
	}
	
	// Handle results based on output format
	switch *outputFormat {
	case "json":
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
	case "csv":
		printListCSV(resp)
	default:
		printListResults(resp)
	}
}

func printListResults(resp *pb.ListResourcesResponse) {
	fmt.Printf("\n📊 Found %d resources\n\n", len(resp.Resources))
	
	if len(resp.Resources) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "ID\tType\tService\tRegion\tStatus")
		fmt.Fprintln(w, "--\t----\t-------\t------\t------")
		
		for _, res := range resp.Resources {
			status := "active"
			if res.BasicAttributes != nil {
				if s, ok := res.BasicAttributes["status"]; ok {
					status = s
				}
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				truncateString(res.Id, 40),
				res.Type,
				res.Service,
				res.Region,
				status)
		}
		w.Flush()
		
		if resp.NextToken != "" {
			fmt.Printf("\n📌 More results available. Use --token %s to continue.\n", resp.NextToken)
		}
	} else {
		fmt.Println("No resources found matching the criteria.")
	}
}

func printListCSV(resp *pb.ListResourcesResponse) {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	
	// Write header
	w.Write([]string{"ID", "Type", "Service", "Region", "Status"})
	
	// Write data
	for _, res := range resp.Resources {
		status := "active"
		if res.BasicAttributes != nil {
			if s, ok := res.BasicAttributes["status"]; ok {
				status = s
			}
		}
		
		w.Write([]string{
			res.Id,
			res.Type,
			res.Service,
			res.Region,
			status,
		})
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func runDescribe(args []string) {
	fs := flag.NewFlagSet("describe", flag.ExitOnError)
	
	providerName := fs.String("provider", "aws", "Cloud provider")
	resourceID := fs.String("resource-id", "", "Resource ID to describe")
	service := fs.String("service", "", "Service name (required for some providers)")
	region := fs.String("region", "", "Region (if different from resource location)")
	outputFormat := fs.String("output", "yaml", "Output format (yaml, json)")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	if *resourceID == "" {
		fmt.Println("Error: --resource-id is required")
		fs.Usage()
		os.Exit(1)
	}
	
	fmt.Printf("🔍 Describing resource: %s\n", *resourceID)
	
	// Initialize plugin client
	pc, err := client.NewPluginClient(*providerName)
	if err != nil {
		log.Fatalf("Failed to initialize plugin client: %v", err)
	}
	defer pc.Close()
	
	provider, err := pc.GetProvider()
	if err != nil {
		log.Fatalf("Failed to get provider: %v", err)
	}
	
	// Create describe request
	req := &pb.DescribeResourceRequest{
		ResourceRef: &pb.ResourceRef{
			Id:      *resourceID,
			Service: *service,
			Region:  *region,
		},
	}
	
	// Execute describe
	resp, err := provider.DescribeResource(context.Background(), req)
	if err != nil {
		log.Fatalf("Describe failed: %v", err)
	}
	
	// Handle results based on output format
	switch *outputFormat {
	case "json":
		data, _ := json.MarshalIndent(resp.Resource, "", "  ")
		fmt.Println(string(data))
	default:
		printDescribeResults(resp)
	}
}

func printDescribeResults(resp *pb.DescribeResourceResponse) {
	if resp.Resource == nil {
		fmt.Println("Resource not found")
		return
	}
	
	res := resp.Resource
	fmt.Printf("\n📋 Resource Details:\n")
	fmt.Printf("   ID:      %s\n", res.Id)
	fmt.Printf("   Type:    %s\n", res.Type)
	fmt.Printf("   Service: %s\n", res.Service)
	fmt.Printf("   Region:  %s\n", res.Region)
	
	if res.Arn != "" {
		fmt.Printf("   ARN:     %s\n", res.Arn)
	}
	
	if res.CreatedAt != nil {
		fmt.Printf("   Created: %s\n", res.CreatedAt.AsTime().Format(time.RFC3339))
	}
	
	if res.ModifiedAt != nil {
		fmt.Printf("   Updated: %s\n", res.ModifiedAt.AsTime().Format(time.RFC3339))
	}
	
	if len(res.Tags) > 0 {
		fmt.Printf("\n🏷️  Tags:\n")
		for k, v := range res.Tags {
			fmt.Printf("   %s: %s\n", k, v)
		}
	}
	
	if res.Attributes != "" {
		fmt.Printf("\n📊 Attributes:\n")
		// Try to parse as JSON
		var attrs map[string]interface{}
		if err := json.Unmarshal([]byte(res.Attributes), &attrs); err == nil {
			for k, v := range attrs {
				fmt.Printf("   %s: %v\n", k, v)
			}
		} else {
			fmt.Printf("   %s\n", res.Attributes)
		}
	}
	
	if res.RawData != "" {
		fmt.Printf("\n📄 Raw Data:\n")
		// Try to pretty print JSON
		var data interface{}
		if err := json.Unmarshal([]byte(res.RawData), &data); err == nil {
			pretty, _ := json.MarshalIndent(data, "   ", "  ")
			fmt.Printf("   %s\n", string(pretty))
		} else {
			fmt.Printf("   %s\n", res.RawData)
		}
	}
}

func runInfo(args []string) {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	
	providerName := fs.String("provider", "aws", "Cloud provider")
	outputFormat := fs.String("output", "table", "Output format (table, json)")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("ℹ️  Getting info for provider: %s\n", *providerName)
	
	// Initialize plugin client
	pc, err := client.NewPluginClient(*providerName)
	if err != nil {
		log.Fatalf("Failed to initialize plugin client: %v", err)
	}
	defer pc.Close()
	
	provider, err := pc.GetProvider()
	if err != nil {
		log.Fatalf("Failed to get provider: %v", err)
	}
	
	// Get info
	resp, err := provider.GetProviderInfo(context.Background(), &pb.Empty{})
	if err != nil {
		log.Fatalf("GetProviderInfo failed: %v", err)
	}
	
	// Handle results based on output format
	switch *outputFormat {
	case "json":
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
	default:
		printInfoResults(resp)
	}
}

func printInfoResults(resp *pb.ProviderInfoResponse) {
	fmt.Printf("\n📋 Provider Information:\n")
	fmt.Printf("   Name:        %s\n", resp.Name)
	fmt.Printf("   Version:     %s\n", resp.Version)
	fmt.Printf("   Description: %s\n", resp.Description)
	
	if len(resp.SupportedServices) > 0 {
		fmt.Printf("\n📦 Supported Services (%d):\n", len(resp.SupportedServices))
		// Group services in columns
		cols := 3
		for i := 0; i < len(resp.SupportedServices); i += cols {
			fmt.Print("   ")
			for j := 0; j < cols && i+j < len(resp.SupportedServices); j++ {
				fmt.Printf("%-25s", resp.SupportedServices[i+j])
			}
			fmt.Println()
		}
	}
	
	if len(resp.Capabilities) > 0 {
		fmt.Printf("\n📊 Capabilities:\n")
		for k, v := range resp.Capabilities {
			fmt.Printf("   %s: %s\n", k, v)
		}
	}
}

func runSchemas(args []string) {
	fs := flag.NewFlagSet("schemas", flag.ExitOnError)
	
	providerName := fs.String("provider", "aws", "Cloud provider")
	servicesStr := fs.String("services", "", "Comma-separated list of services (empty for all)")
	outputFormat := fs.String("output", "sql", "Output format (sql, json)")
	dialect := fs.String("dialect", "duckdb", "SQL dialect (duckdb, postgres, sqlite)")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	// Parse services
	services := []string{}
	if *servicesStr != "" {
		services = strings.Split(*servicesStr, ",")
		for i, s := range services {
			services[i] = strings.TrimSpace(s)
		}
	}
	
	fmt.Printf("📊 Getting schemas for provider: %s\n", *providerName)
	if len(services) > 0 {
		fmt.Printf("   Services: %s\n", strings.Join(services, ", "))
	} else {
		fmt.Printf("   Services: all\n")
	}
	
	// Initialize plugin client
	pc, err := client.NewPluginClient(*providerName)
	if err != nil {
		log.Fatalf("Failed to initialize plugin client: %v", err)
	}
	defer pc.Close()
	
	provider, err := pc.GetProvider()
	if err != nil {
		log.Fatalf("Failed to get provider: %v", err)
	}
	
	// Get schemas
	req := &pb.GetSchemasRequest{
		Services: services,
		Format:   "sql",
	}
	
	resp, err := provider.GetSchemas(context.Background(), req)
	if err != nil {
		log.Fatalf("GetSchemas failed: %v", err)
	}
	
	// Handle results based on output format
	switch *outputFormat {
	case "json":
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
	default:
		printSchemaSQL(resp, *dialect)
	}
}

func printSchemaSQL(resp *pb.SchemaResponse, dialect string) {
	fmt.Println("\n-- Resource Schemas")
	fmt.Println("-- Generated by Corkscrew")
	fmt.Printf("-- Dialect: %s\n\n", dialect)
	
	for _, schema := range resp.Schemas {
		// Print schema info
		fmt.Printf("-- Service: %s\n", schema.Service)
		fmt.Printf("-- Resource Type: %s\n", schema.ResourceType)
		if schema.Description != "" {
			fmt.Printf("-- Description: %s\n", schema.Description)
		}
		
		// Print the SQL
		if schema.Sql != "" {
			fmt.Println(schema.Sql)
		} else {
			fmt.Printf("-- No SQL available for %s.%s\n", schema.Service, schema.ResourceType)
		}
		fmt.Println()
	}
}

func getSQLType(protoType string, dialect string) string {
	switch dialect {
	case "postgres":
		switch protoType {
		case "string":
			return "TEXT"
		case "int", "int32", "int64":
			return "BIGINT"
		case "float", "double":
			return "DOUBLE PRECISION"
		case "bool":
			return "BOOLEAN"
		case "timestamp":
			return "TIMESTAMP"
		case "json":
			return "JSONB"
		default:
			return "TEXT"
		}
	case "sqlite":
		switch protoType {
		case "string", "json":
			return "TEXT"
		case "int", "int32", "int64":
			return "INTEGER"
		case "float", "double":
			return "REAL"
		case "bool":
			return "INTEGER"
		case "timestamp":
			return "TEXT"
		default:
			return "TEXT"
		}
	default: // duckdb
		switch protoType {
		case "string":
			return "VARCHAR"
		case "int", "int32":
			return "INTEGER"
		case "int64":
			return "BIGINT"
		case "float":
			return "FLOAT"
		case "double":
			return "DOUBLE"
		case "bool":
			return "BOOLEAN"
		case "timestamp":
			return "TIMESTAMP"
		case "json":
			return "JSON"
		default:
			return "VARCHAR"
		}
	}
}

// runQuery executes SQL queries or compliance checks
func runQuery(args []string) {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	
	// SQL query options
	queryStr := fs.String("query", "", "SQL query to execute")
	queryFile := fs.String("file", "", "SQL file to execute")
	stdin := fs.Bool("stdin", false, "Read query from stdin")
	
	// Compliance options
	control := fs.String("control", "", "Control ID to check (e.g., jlgore/cfi-ccc/CCC.C01)")
	pack := fs.String("pack", "", "Compliance pack to run")
	compliance := fs.Bool("compliance", false, "Run compliance queries")
	tags := fs.String("tag", "", "Filter queries by tags (comma-separated)")
	dryRun := fs.Bool("dry-run", false, "Validate queries without executing")
	
	// Common options
	dbPath := fs.String("db", "", "Path to database file (default: ~/.corkscrew/corkscrew.db)")
	outputFormat := fs.String("output", "table", "Output format (table, json, csv)")
	verbose := fs.Bool("verbose", false, "Enable verbose output")
	noHeader := fs.Bool("no-header", false, "Omit header in table output")
	
	// Create parameter flags handler
	params := make(parameterFlags)
	fs.Var(params, "param", "Set parameter value (can be used multiple times)")
	
	// Special flags for pack management
	listPacks := fs.Bool("list-packs", false, "List installed compliance packs")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	// Check for pack management subcommand
	if len(args) > 0 && args[0] == "pack" {
		runPackCommand(args[1:])
		return
	}
	
	// Handle list-packs
	if *listPacks {
		listInstalledPacks(*outputFormat)
		return
	}
	
	// Determine query source
	var sqlQuery string
	if *control != "" || *pack != "" || *compliance {
		// Run compliance query
		runComplianceQuery(*control, *pack, *tags, params, *dryRun, *outputFormat, *verbose)
		return
	} else if *queryFile != "" {
		// Read from file
		content, err := os.ReadFile(*queryFile)
		if err != nil {
			log.Fatalf("Failed to read query file: %v", err)
		}
		sqlQuery = string(content)
	} else if *stdin {
		// Read from stdin
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Failed to read from stdin: %v", err)
		}
		sqlQuery = string(content)
	} else if len(args) > 0 && args[0] != "" && !strings.HasPrefix(args[0], "-") {
		// Query provided as positional argument
		sqlQuery = args[0]
	} else if *queryStr != "" {
		sqlQuery = *queryStr
	} else {
		fmt.Println("Error: No query provided")
		fmt.Println("Use -query, -file, -stdin, or provide query as argument")
		fs.Usage()
		os.Exit(1)
	}
	
	// Get database path
	if *dbPath == "" {
		home, _ := os.UserHomeDir()
		*dbPath = filepath.Join(home, ".corkscrew", "corkscrew.db")
	}
	
	// Execute query
	engine, err := query.NewEngine(*dbPath)
	if err != nil {
		log.Fatalf("Failed to create query engine: %v", err)
	}
	defer engine.Close()
	
	// Execute the query
	rows, columns, err := query.ExecuteQuery(engine, sqlQuery)
	if err != nil {
		handleQueryError(err, sqlQuery)
		os.Exit(1)
	}
	
	// Format and display results
	switch *outputFormat {
	case "json":
		printJSONResults(rows, columns)
	case "csv":
		printCSVQueryResults(rows, columns, *noHeader)
	default:
		printTableResults(rows, columns, *noHeader)
	}
}

// runComplianceQuery handles compliance-specific queries
func runComplianceQuery(controlID, packName, tags string, params map[string]interface{}, dryRun bool, outputFormat string, verbose bool) {
	// Initialize compliance executor
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".corkscrew", "corkscrew.db")
	
	executor, err := compliance.NewExecutor(dbPath)
	if err != nil {
		log.Fatalf("Failed to create compliance executor: %v", err)
	}
	defer executor.Close()
	
	// Build options
	options := compliance.ExecuteOptions{
		ControlID:  controlID,
		PackName:   packName,
		Tags:       parseTags(tags),
		Parameters: params,
		DryRun:     dryRun,
	}
	
	// Show what we're about to do
	printComplianceOptions(options)
	
	// Execute compliance check
	results, err := executor.Execute(options)
	if err != nil {
		log.Fatalf("Compliance check failed: %v", err)
	}
	
	// Display results
	switch outputFormat {
	case "json":
		printComplianceJSON(results)
	case "csv":
		printComplianceCSV(results)
	default:
		printComplianceTable(results, verbose)
	}
}

func parseTags(tagStr string) []string {
	if tagStr == "" {
		return nil
	}
	tags := strings.Split(tagStr, ",")
	for i, tag := range tags {
		tags[i] = strings.TrimSpace(tag)
	}
	return tags
}

func printComplianceTable(results []compliance.SimpleQueryResult, verbose bool) {
	if len(results) == 0 {
		fmt.Println("No compliance checks were executed")
		return
	}
	
	// Summary
	passed := 0
	failed := 0
	errors := 0
	
	for _, r := range results {
		if r.Error != nil {
			errors++
		} else if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	
	fmt.Printf("\n📊 Compliance Check Results\n")
	fmt.Printf("✅ Passed: %d | ❌ Failed: %d | ⚠️  Errors: %d\n\n", passed, failed, errors)
	
	// Detailed results
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Control\tStatus\tResources\tMessage")
	fmt.Fprintln(w, "-------\t------\t---------\t-------")
	
	for _, r := range results {
		status := "✅ PASS"
		if r.Error != nil {
			status = "⚠️  ERROR"
		} else if !r.Passed {
			status = "❌ FAIL"
		}
		
		message := r.Title
		if r.Error != nil {
			message = r.Error.Error()
		}
		
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", 
			r.ControlID, 
			status,
			r.ResourceCount,
			truncateString(message, 50))
	}
	w.Flush()
	
	// Show failed resources if verbose
	if verbose && failed > 0 {
		fmt.Printf("\n❌ Failed Resources:\n")
		for _, r := range results {
			if !r.Passed && r.Error == nil && len(r.FailedResources) > 0 {
				fmt.Printf("\n%s - %s:\n", r.ControlID, r.Title)
				for _, res := range r.FailedResources {
					fmt.Printf("  - %s\n", res)
				}
			}
		}
	}
}

func printComplianceJSON(results []compliance.SimpleQueryResult) {
	output := struct {
		Summary struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
			Errors int `json:"errors"`
		} `json:"summary"`
		Results []compliance.SimpleQueryResult `json:"results"`
	}{}
	
	output.Results = results
	output.Summary.Total = len(results)
	
	for _, r := range results {
		if r.Error != nil {
			output.Summary.Errors++
		} else if r.Passed {
			output.Summary.Passed++
		} else {
			output.Summary.Failed++
		}
	}
	
	data, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(data))
}

func printComplianceCSV(results []compliance.SimpleQueryResult) {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	
	// Header
	w.Write([]string{"ControlID", "Title", "Status", "ResourceCount", "Message"})
	
	// Data
	for _, r := range results {
		status := "PASS"
		message := ""
		
		if r.Error != nil {
			status = "ERROR"
			message = r.Error.Error()
		} else if !r.Passed {
			status = "FAIL"
		}
		
		w.Write([]string{
			r.ControlID,
			r.Title,
			status,
			fmt.Sprintf("%d", r.ResourceCount),
			message,
		})
	}
}

// Pack management subcommand
func runPackCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew query pack <command> [options]")
		fmt.Println("Commands: search, install, list, update, validate")
		return
	}
	
	command := args[0]
	switch command {
	case "search":
		searchPacks(args[1:])
	case "install":
		installPack(args[1:])
	case "list":
		listInstalledPacks("table")
	case "update":
		updatePacks(args[1:])
	case "validate":
		validatePack(args[1:])
	default:
		fmt.Printf("Unknown pack command: %s\n", command)
	}
}

func searchPacks(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew query pack search <query>")
		return
	}
	
	query := strings.Join(args, " ")
	fmt.Printf("🔍 Searching for packs matching: %s\n", query)
	
	// TODO: Implement pack registry search
	fmt.Println("Pack search will be available in a future release")
}

func installPack(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew query pack install <pack-name>")
		return
	}
	
	packName := args[0]
	fmt.Printf("📦 Installing pack: %s\n", packName)
	
	loader := compliance.NewLoader("")
	if err := loader.InstallPack(packName); err != nil {
		log.Fatalf("Failed to install pack: %v", err)
	}
	
	fmt.Printf("✅ Successfully installed pack: %s\n", packName)
}

func listInstalledPacks(format string) {
	loader := compliance.NewLoader("")
	packs, err := loader.ListPacks()
	if err != nil {
		log.Fatalf("Failed to list packs: %v", err)
	}
	
	if format == "json" {
		data, _ := json.MarshalIndent(packs, "", "  ")
		fmt.Println(string(data))
		return
	}
	
	fmt.Printf("📦 Installed Compliance Packs (%d)\n\n", len(packs))
	
	if len(packs) == 0 {
		fmt.Println("No packs installed. Use 'corkscrew query pack install' to add packs.")
		return
	}
	
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Pack\tVersion\tControls\tDescription")
	fmt.Fprintln(w, "----\t-------\t--------\t-----------")
	
	for _, pack := range packs {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			pack.Metadata.Name,
			pack.Metadata.Version,
			len(pack.Queries),
			truncateString(pack.Metadata.Description, 50))
	}
	w.Flush()
}

func updatePacks(args []string) {
	updateAll := false
	for _, arg := range args {
		if arg == "--all" {
			updateAll = true
			break
		}
	}
	
	if !updateAll && len(args) == 0 {
		fmt.Println("Usage: corkscrew query pack update <pack-name> or --all")
		return
	}
	
	if updateAll {
		fmt.Println("🔄 Updating all packs...")
		// TODO: Implement pack updates
		fmt.Println("Pack updates will be available in a future release")
	} else {
		packName := args[0]
		fmt.Printf("🔄 Updating pack: %s\n", packName)
		// TODO: Implement single pack update
		fmt.Println("Pack updates will be available in a future release")
	}
}

func validatePack(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew query pack validate <pack-name>")
		return
	}
	
	packName := args[0]
	fmt.Printf("🔍 Validating pack: %s\n", packName)
	
	loader := compliance.NewLoader("")
	pack, err := loader.LoadPack(packName)
	if err != nil {
		log.Fatalf("Failed to load pack: %v", err)
	}
	
	// Validate each query
	errors := 0
	warnings := 0
	
	fmt.Printf("\nValidating %d queries...\n", len(pack.Queries))
	
	for _, q := range pack.Queries {
		// Basic validation
		if q.ID == "" {
			fmt.Printf("❌ Query missing ID\n")
			errors++
			continue
		}
		
		if q.SQL == "" {
			fmt.Printf("❌ %s: Empty query\n", q.ID)
			errors++
			continue
		}
		
		// TODO: Add SQL validation
		fmt.Printf("✅ %s: Valid\n", q.ID)
	}
	
	if errors == 0 && warnings == 0 {
		fmt.Printf("\n✅ Pack validation successful!\n")
	} else {
		fmt.Printf("\n❌ Validation failed: %d errors, %d warnings\n", errors, warnings)
	}
}

func handleQueryError(err error, sqlQuery string) {
	fmt.Fprintf(os.Stderr, "❌ Query execution failed: %v\n", err)
	
	// Try to provide helpful error messages
	errorMsg := err.Error()
	
	// Check for common SQL errors
	if strings.Contains(errorMsg, "no such table") || strings.Contains(errorMsg, "does not exist") {
		fmt.Fprintf(os.Stderr, "\n💡 Hint: Make sure you've run 'corkscrew scan' to populate the database.\n")
		fmt.Fprintf(os.Stderr, "Available tables:\n")
		
		// List available tables
		home, _ := os.UserHomeDir()
		dbPath := filepath.Join(home, ".corkscrew", "corkscrew.db")
		if engine, err := query.NewEngine(dbPath); err == nil {
			defer engine.Close()
			if tables, _, err := query.ExecuteQuery(engine, "SELECT DISTINCT table_name FROM information_schema.tables WHERE table_schema = 'main'"); err == nil {
				for _, row := range tables {
					if tableName, ok := row[0].(string); ok {
						fmt.Fprintf(os.Stderr, "  - %s\n", tableName)
					}
				}
			}
		}
		
		// Check for table not found (DuckDB format)
		tableNotFoundRegex := regexp.MustCompile(`Table with name ([a-zA-Z_][a-zA-Z0-9_]*) does not exist`)
		if matches := tableNotFoundRegex.FindStringSubmatch(errorMsg); len(matches) > 1 {
			tableName := matches[1]
			fmt.Fprintf(os.Stderr, "  🔍 Table '%s' not found\n", tableName)
			
			// Suggest similar table names
			if suggestions := suggestTableNames(tableName); len(suggestions) > 0 {
				fmt.Fprintf(os.Stderr, "  💡 Did you mean one of these?\n")
				for _, suggestion := range suggestions {
					fmt.Fprintf(os.Stderr, "     - %s\n", suggestion)
				}
			}
		}
	} else if strings.Contains(errorMsg, "syntax error") {
		fmt.Fprintf(os.Stderr, "\n💡 Hint: Check your SQL syntax. DuckDB uses standard SQL.\n")
		
		// Try to highlight the error position if available
		if pos := extractErrorPosition(errorMsg); pos > 0 {
			lines := strings.Split(sqlQuery, "\n")
			lineNo := 1
			charCount := 0
			for _, line := range lines {
				if charCount+len(line) >= pos {
					fmt.Fprintf(os.Stderr, "Error near line %d:\n", lineNo)
					fmt.Fprintf(os.Stderr, "  %s\n", line)
					fmt.Fprintf(os.Stderr, "  %s^\n", strings.Repeat(" ", pos-charCount-1))
					break
				}
				charCount += len(line) + 1 // +1 for newline
				lineNo++
			}
		}
	}
}

func suggestTableNames(tableName string) []string {
	// Common table names in corkscrew
	commonTables := []string{
		"aws_resources",
		"aws_s3_buckets", 
		"aws_ec2_instances",
		"aws_iam_users",
		"aws_iam_roles",
		"aws_lambda_functions",
		"aws_rds_instances",
		"azure_resources",
		"gcp_resources",
		"kubernetes_resources",
	}
	
	suggestions := []string{}
	lowerInput := strings.ToLower(tableName)
	
	for _, table := range commonTables {
		lowerTable := strings.ToLower(table)
		// Check if input is a substring or vice versa
		if strings.Contains(lowerTable, lowerInput) || strings.Contains(lowerInput, lowerTable) {
			suggestions = append(suggestions, table)
		}
		// Check for common typos (simple edit distance)
		if levenshteinDistance(lowerInput, lowerTable) <= 3 {
			suggestions = append(suggestions, table)
		}
	}
	
	return suggestions
}

func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}
	
	// Create matrix
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}
	
	// Initialize first column and row
	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}
	
	// Fill matrix
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}
			
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}
	
	return matrix[len(s1)][len(s2)]
}

func min(nums ...int) int {
	minVal := nums[0]
	for _, n := range nums[1:] {
		if n < minVal {
			minVal = n
		}
	}
	return minVal
}

func extractErrorPosition(errorMsg string) int {
	// Try to extract position from error message
	// DuckDB format: "at or near position X"
	posRegex := regexp.MustCompile(`at or near position (\d+)`)
	if matches := posRegex.FindStringSubmatch(errorMsg); len(matches) > 1 {
		if pos, err := fmt.Sscanf(matches[1], "%d"); err == nil {
			return pos
		}
	}
	return 0
}

func printTableResults(rows [][]interface{}, columns []string, noHeader bool) {
	if len(rows) == 0 {
		fmt.Println("No results found.")
		return
	}
	
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	
	// Print header
	if !noHeader {
		fmt.Fprintln(w, strings.Join(columns, "\t"))
		separators := make([]string, len(columns))
		for i, col := range columns {
			separators[i] = strings.Repeat("-", len(col))
		}
		fmt.Fprintln(w, strings.Join(separators, "\t"))
	}
	
	// Print rows
	for _, row := range rows {
		values := make([]string, len(row))
		for i, val := range row {
			values[i] = formatValue(val)
		}
		fmt.Fprintln(w, strings.Join(values, "\t"))
	}
	
	w.Flush()
	
	fmt.Printf("\n(%d rows)\n", len(rows))
}

func printJSONResults(rows [][]interface{}, columns []string) {
	results := []map[string]interface{}{}
	
	for _, row := range rows {
		record := make(map[string]interface{})
		for i, col := range columns {
			if i < len(row) {
				record[col] = row[i]
			}
		}
		results = append(results, record)
	}
	
	data, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(data))
}

func printCSVQueryResults(rows [][]interface{}, columns []string, noHeader bool) {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	
	// Write header
	if !noHeader {
		w.Write(columns)
	}
	
	// Write data
	for _, row := range rows {
		values := make([]string, len(row))
		for i, val := range row {
			values[i] = formatValue(val)
		}
		w.Write(values)
	}
}

func formatValue(val interface{}) string {
	if val == nil {
		return "NULL"
	}
	
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// runDiagram runs the interactive diagram viewer
/*
func runDiagram(args []string) {
	fs := flag.NewFlagSet("diagram", flag.ExitOnError)
	
	providerName := fs.String("provider", "aws", "Cloud provider")
	resourceType := fs.String("type", "", "Resource type to visualize")
	outputFile := fs.String("output", "", "Output file (instead of interactive mode)")
	format := fs.String("format", "ascii", "Output format (ascii, mermaid)")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	// If output file is specified, generate static diagram
	if *outputFile != "" {
		generateStaticDiagram(*providerName, *resourceType, *outputFile, *format)
		return
	}
	
	// Otherwise, launch interactive viewer
	fmt.Println("🎨 Launching interactive diagram viewer...")
	
	model := ui.NewModel()
	p := tea.NewProgram(model, tea.WithAltScreen())
	
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running diagram viewer: %v", err)
	}
}

func generateStaticDiagram(provider, resourceType, outputFile, format string) {
	// TODO: Implement static diagram generation
	fmt.Printf("Generating %s diagram for %s resources...\n", format, provider)
	
	var content string
	switch format {
	case "mermaid":
		r := renderer.NewMermaidRenderer()
		content = r.Render(nil) // TODO: Pass actual graph
	default:
		r := renderer.NewASCIIRenderer()
		content = r.Render(nil) // TODO: Pass actual graph
	}
	
	if err := os.WriteFile(outputFile, []byte(content), 0644); err != nil {
		log.Fatalf("Failed to write diagram: %v", err)
	}
	
	fmt.Printf("✅ Diagram saved to: %s\n", outputFile)
}
*/

// runPlugin handles plugin management commands
func runPlugin(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew plugin <command>")
		fmt.Println("Commands: list, build, status")
		return
	}
	
	command := args[0]
	switch command {
	case "list":
		listPlugins()
	case "build":
		buildPlugins(args[1:])
	case "status":
		checkPluginStatus(args[1:])
	default:
		fmt.Printf("Unknown plugin command: %s\n", command)
	}
}

func listPlugins() {
	fmt.Println("📦 Available Plugins:")
	fmt.Println()
	
	plugins := []struct {
		name        string
		description string
		status      string
	}{
		{"aws", "Amazon Web Services provider", "✅ Installed"},
		{"azure", "Microsoft Azure provider", "✅ Installed"},
		{"gcp", "Google Cloud Platform provider", "🚧 Available"},
		{"kubernetes", "Kubernetes provider", "🚧 Available"},
	}
	
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Plugin\tDescription\tStatus")
	fmt.Fprintln(w, "------\t-----------\t------")
	
	for _, p := range plugins {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.name, p.description, p.status)
	}
	w.Flush()
}

func buildPlugins(args []string) {
	providers := []string{"aws", "azure", "gcp", "kubernetes"}
	if len(args) > 0 {
		providers = args
	}
	
	fmt.Println("🔨 Building plugins...")
	
	for _, provider := range providers {
		fmt.Printf("  Building %s plugin...", provider)
		
		cmd := exec.Command("make", fmt.Sprintf("plugin-%s", provider))
		output, err := cmd.CombinedOutput()
		
		if err != nil {
			fmt.Printf(" ❌ Failed\n")
			fmt.Printf("    Error: %v\n", err)
			if len(output) > 0 {
				fmt.Printf("    Output: %s\n", string(output))
			}
		} else {
			fmt.Printf(" ✅ Done\n")
		}
	}
}

func checkPluginStatus(args []string) {
	providers := []string{"aws", "azure"}
	if len(args) > 0 {
		providers = args
	}
	
	fmt.Println("🔍 Checking plugin status...")
	fmt.Println()
	
	for _, provider := range providers {
		pc, err := client.NewPluginClient(provider)
		if err != nil {
			fmt.Printf("❌ %s: Not available - %v\n", provider, err)
			continue
		}
		defer pc.Close()
		
		provider, err := pc.GetProvider()
		if err != nil {
			fmt.Printf("❌ %s: Failed to initialize - %v\n", provider, err)
			continue
		}
		
		info, err := provider.GetProviderInfo(context.Background(), &pb.Empty{})
		if err != nil {
			fmt.Printf("❌ %s: Failed to get info - %v\n", provider, err)
			continue
		}
		
		fmt.Printf("✅ %s: Ready\n", provider)
		fmt.Printf("   Version: %s\n", info.Version)
		fmt.Printf("   Services: %d\n", len(info.SupportedServices))
	}
}
func printComplianceOptions(options compliance.ExecuteOptions) {
	fmt.Println("\n🔍 Compliance Check Configuration:")
	
	if options.ControlID != "" {
		fmt.Printf("📌 Control: %s\n", options.ControlID)
	}
	
	if options.PackName != "" {
		fmt.Printf("📦 Pack: %s\n", options.PackName)
	}
	
	if len(options.Tags) > 0 {
		fmt.Printf("🏷️  Tags: %s\n", strings.Join(options.Tags, ", "))
	}
	
	if len(options.Parameters) > 0 {
		fmt.Printf("🔧 Parameters:\n")
		for key, value := range options.Parameters {
			fmt.Printf("  %s = %v\n", key, value)
		}
	}
	
	if options.DryRun {
		fmt.Printf("✅ Dry-run validation would be performed for tagged queries\n")
	}
}

// runConfig handles configuration management commands
func runConfig(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew config <command>")
		fmt.Println("Commands: init, show, validate")
		return
	}
	
	command := args[0]
	switch command {
	case "init":
		runConfigInit()
	case "show":
		runConfigShow()
	case "validate":
		runConfigValidate()
	default:
		fmt.Printf("Unknown config command: %s\n", command)
		fmt.Println("Available commands: init, show, validate")
	}
}

func runConfigInit() {
	fmt.Println("🔧 Initializing Corkscrew configuration...")
	
	if err := config.InitializeConfigFile(); err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}
	
	fmt.Println("✅ Configuration file created: corkscrew.yaml")
	fmt.Println("\nYou can now:")
	fmt.Println("  - Edit corkscrew.yaml to customize service discovery")
	fmt.Println("  - Run 'corkscrew config validate' to check your configuration")
	fmt.Println("  - Run 'corkscrew config show' to view the current configuration")
}

func runConfigShow() {
	cfg, err := config.LoadServiceConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	
	fmt.Println("📋 Current Configuration:")
	fmt.Println()
	
	// Convert to YAML for pretty printing
	data, err := yaml.Marshal(cfg)
	if err != nil {
		log.Fatalf("Failed to format configuration: %v", err)
	}
	
	fmt.Println(string(data))
	
	// Show resolved services
	fmt.Println("\n🔍 Resolved AWS Services:")
	services, err := cfg.GetServicesForProvider("aws")
	if err != nil {
		fmt.Printf("Error resolving services: %v\n", err)
	} else {
		fmt.Printf("Total services: %d\n", len(services))
		
		// Display in columns
		cols := 4
		for i := 0; i < len(services); i += cols {
			fmt.Print("  ")
			for j := 0; j < cols && i+j < len(services); j++ {
				fmt.Printf("%-20s", services[i+j])
			}
			fmt.Println()
		}
	}
}

func runConfigValidate() {
	fmt.Println("🔍 Validating configuration...")
	
	cfg, err := config.LoadServiceConfig()
	if err != nil {
		log.Fatalf("❌ Configuration is invalid: %v", err)
	}
	
	fmt.Println("✅ Configuration is valid")
	
	// Show summary
	for provider, pconfig := range cfg.Providers {
		fmt.Printf("\n📦 Provider: %s\n", provider)
		fmt.Printf("  Discovery mode: %s\n", pconfig.DiscoveryMode)
		
		services, err := cfg.GetServicesForProvider(provider)
		if err != nil {
			fmt.Printf("  ❌ Error resolving services: %v\n", err)
		} else {
			fmt.Printf("  ✅ Services configured: %d\n", len(services))
		}
		
		if len(pconfig.ServiceGroups) > 0 {
			fmt.Printf("  📁 Service groups: %d\n", len(pconfig.ServiceGroups))
			for group, svcs := range pconfig.ServiceGroups {
				fmt.Printf("    - %s (%d services)\n", group, len(svcs))
			}
		}
		
		fmt.Printf("  ⚙️  Analysis settings:\n")
		fmt.Printf("    - Skip empty: %v\n", pconfig.Analysis.SkipEmpty)
		fmt.Printf("    - Workers: %d\n", pconfig.Analysis.Workers)
		fmt.Printf("    - Cache: %v (TTL: %s)\n", pconfig.Analysis.CacheEnabled, pconfig.Analysis.CacheTTL)
	}
}

// runServe starts the gRPC API server
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	
	port := fs.Int("port", 9090, "Port to listen on")
	host := fs.String("host", "localhost", "Host to bind to")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("🚀 Starting Corkscrew gRPC API server...\n")
	fmt.Printf("📍 Listening on %s:%d\n", *host, *port)
	fmt.Printf("\n💡 Example commands to test the API:\n")
	fmt.Printf("  grpcurl -plaintext %s:%d list\n", *host, *port)
	fmt.Printf("  grpcurl -plaintext %s:%d corkscrew.api.CorkscrewAPI.HealthCheck\n", *host, *port)
	fmt.Printf("  grpcurl -plaintext %s:%d corkscrew.api.CorkscrewAPI.ListProviders\n", *host, *port)
	fmt.Printf("\n📖 For more gRPC client examples, visit the documentation\n")
	fmt.Printf("⏹️  Press Ctrl+C to stop the server\n\n")
	
	if err := server.StartAPIServer(*port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
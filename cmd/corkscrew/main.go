package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jlgore/corkscrew/internal/app/crosscloud"
	providerapp "github.com/jlgore/corkscrew/internal/app/providers"
	appquery "github.com/jlgore/corkscrew/internal/app/query"
	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
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

func defaultDatabasePath() string {
	path, err := db.GetUnifiedDatabasePath()
	if err == nil {
		return path
	}

	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return ".corkscrew.duckdb"
	}
	return filepath.Join(home, ".corkscrew", "db", "corkscrew.duckdb")
}

func mustProviderApplication() *providerapp.Application {
	application, err := providerapp.OpenDefault(os.Stderr)
	if err != nil {
		log.Fatalf("Failed to load provider runtime: %v", err)
	}
	return application
}

func groupServicesByCategory(services []*pb.ServiceInfo) map[string][]*pb.ServiceInfo {
	categories := map[string][]*pb.ServiceInfo{
		"Compute":    {},
		"Storage":    {},
		"Database":   {},
		"Networking": {},
		"Security":   {},
		"Monitoring": {},
		"Other":      {},
	}

	for _, svc := range services {
		category := categorizeService(svc.Name)
		categories[category] = append(categories[category], svc)
	}

	return categories
}

func categorizeService(serviceName string) string {
	serviceName = strings.ToLower(serviceName)

	// Compute services
	if strings.Contains(serviceName, "ec2") || strings.Contains(serviceName, "lambda") ||
		strings.Contains(serviceName, "ecs") || strings.Contains(serviceName, "eks") ||
		strings.Contains(serviceName, "batch") || strings.Contains(serviceName, "compute") {
		return "Compute"
	}

	// Storage services
	if strings.Contains(serviceName, "s3") || strings.Contains(serviceName, "ebs") ||
		strings.Contains(serviceName, "efs") || strings.Contains(serviceName, "fsx") ||
		strings.Contains(serviceName, "backup") || strings.Contains(serviceName, "storage") {
		return "Storage"
	}

	// Database services
	if strings.Contains(serviceName, "rds") || strings.Contains(serviceName, "dynamodb") ||
		strings.Contains(serviceName, "elasticache") || strings.Contains(serviceName, "redshift") ||
		strings.Contains(serviceName, "documentdb") || strings.Contains(serviceName, "database") {
		return "Database"
	}

	// Networking services
	if strings.Contains(serviceName, "vpc") || strings.Contains(serviceName, "elb") ||
		strings.Contains(serviceName, "route53") || strings.Contains(serviceName, "cloudfront") ||
		strings.Contains(serviceName, "apigateway") || strings.Contains(serviceName, "network") {
		return "Networking"
	}

	// Security services
	if strings.Contains(serviceName, "iam") || strings.Contains(serviceName, "kms") ||
		strings.Contains(serviceName, "secretsmanager") || strings.Contains(serviceName, "acm") ||
		strings.Contains(serviceName, "guardduty") || strings.Contains(serviceName, "security") {
		return "Security"
	}

	// Monitoring services
	if strings.Contains(serviceName, "cloudwatch") || strings.Contains(serviceName, "logs") ||
		strings.Contains(serviceName, "xray") || strings.Contains(serviceName, "sns") ||
		strings.Contains(serviceName, "sqs") || strings.Contains(serviceName, "monitoring") {
		return "Monitoring"
	}

	return "Other"
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
	os.Exit(runCLI(os.Args[1:]))
}

func printUnavailableCommand(command, reason string) {
	printUnavailableCommandMessage(os.Stderr, command, reason)
	os.Exit(1)
}

func printUnavailableCommandMessage(w io.Writer, command, reason string) {
	fmt.Fprintf(w, "Command unavailable: %s\n", command)
	fmt.Fprintf(w, "%s\n", reason)
}

func printUsage() {
	fmt.Println("🚀 Corkscrew Cloud Resource Scanner v2.0.0")
	fmt.Println("Multi-Cloud Plugin Architecture")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  # Interactive TUI Mode")
	fmt.Println("  corkscrew --tui                        # Launch main TUI interface")
	fmt.Println("  corkscrew scan --tui                   # Launch scan configuration TUI")
	fmt.Println("  corkscrew query --tui                  # Launch query builder TUI")
	fmt.Println("  corkscrew config --tui                 # Launch configuration wizard TUI")
	fmt.Println()
	fmt.Println("  # Multi-Region Scanning")
	fmt.Println("  corkscrew scan --provider aws --region us-east-1,us-west-2,eu-west-1")
	fmt.Println("  corkscrew scan --provider aws --region all")
	fmt.Println("  corkscrew scan --provider aws --services common --region us-east-1,us-west-2")
	fmt.Println("  corkscrew scan --provider aws --services compute,storage --region us-east-1")
	fmt.Println("  corkscrew scan --provider azure --region eastus,westus2 --concurrency 5")
	fmt.Println("  corkscrew scan --provider aws --output json")
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
	fmt.Println("  corkscrew query \"SELECT provider, COUNT(*) FROM all_cloud_resources GROUP BY provider\"")
	fmt.Println("  corkscrew query \"SELECT * FROM all_cloud_resources WHERE type='Bucket'\" --output csv")
	fmt.Println("  echo \"SELECT * FROM all_cloud_resources WHERE provider='azure'\" | corkscrew query --stdin --output json")
	fmt.Println("  corkscrew query --file analysis.sql --verbose")
	fmt.Println()
	fmt.Println("  # Compliance Query Examples")
	fmt.Println("  corkscrew query --control jlgore/cfi-ccc/CCC.C01")
	fmt.Println("  corkscrew query --pack jlgore/cfi-ccc/s3-security")
	fmt.Println("  corkscrew query --compliance --tag encryption --param required_encryption=aws:kms")
	fmt.Println("  corkscrew query --list-packs")
	fmt.Println()
	fmt.Println("  # Pack Management Examples")
	fmt.Println("  corkscrew pack search \"aws security\"")
	fmt.Println("  corkscrew pack install jlgore/cfi-ccc")
	fmt.Println("  corkscrew pack list")
	fmt.Println("  corkscrew pack validate jlgore/cfi-ccc")
	fmt.Println()
	fmt.Println("  # Cross-Cloud Examples")
	fmt.Println("  corkscrew crosscloud scan --providers aws,azure --regions us-east-1,eastus")
	fmt.Println("  corkscrew crosscloud correlate --confidence 0.8")
	fmt.Println("  corkscrew crosscloud topology --output json")
	fmt.Println("  corkscrew correlate ip --providers aws,azure")
	fmt.Println("  corkscrew correlate dns --providers aws,azure,gcp")
	fmt.Println()
	fmt.Println("  # Graph Query Examples")
	fmt.Println("  corkscrew graph traverse <resource-id> --db ~/.corkscrew/db/corkscrew.duckdb")
	fmt.Println("  corkscrew graph path <from-id> <to-id> --output json")
	fmt.Println("  corkscrew graph patterns list")
	fmt.Println()
	fmt.Println("  # GitHub Provider Examples")
	fmt.Println("  corkscrew github bootstrap-app --org my-org")
	fmt.Println()
	fmt.Println("  # Cloudflare Provider Examples")
	fmt.Println("  corkscrew cloudflare login --services workers,r2")
	fmt.Println("  corkscrew cloudflare auth status")
	fmt.Println()
	fmt.Println("  # Configuration Examples")
	fmt.Println("  corkscrew config init         # Create default configuration file")
	fmt.Println("  corkscrew config show         # Display current configuration")
	fmt.Println("  corkscrew config validate     # Validate configuration file")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init                - Initialize Corkscrew with dependencies and plugins")
	fmt.Println("  config              - Manage Corkscrew configuration (init, show, validate)")
	fmt.Println("  scan                - Full resource scanning (supports service groups)")
	fmt.Println("  discover            - Discover available services")
	fmt.Println("  list                - List resources")
	fmt.Println("  describe            - Describe specific resources")
	fmt.Println("  info                - Show provider information")
	fmt.Println("  schemas             - Get database schemas for resources")
	fmt.Println("  query               - Execute SQL queries against resource database")
	fmt.Println("  diagram             - Interactive resource diagram viewer")
	fmt.Println("  plugin              - Plugin management (list, build, status, groups)")
	fmt.Println("  graph               - Graph traversal and attack-path queries")
	fmt.Println("  github              - GitHub provider setup helpers")
	fmt.Println("  cloudflare          - Cloudflare provider setup helpers")
	fmt.Println("  serve               - Start gRPC API server")
	fmt.Println("  version             - Show version information")
	fmt.Println()
	fmt.Println("Supported Providers:")
	fmt.Println("  aws         - Amazon Web Services")
	fmt.Println("  azure       - Microsoft Azure")
	fmt.Println("  cloudflare  - Cloudflare")
	fmt.Println("  gcp         - Google Cloud Platform")
	fmt.Println("  kubernetes  - Kubernetes")
}

func runDiscover(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)

	providerName := fs.String("provider", "aws", "Cloud provider (aws, azure, cloudflare, gcp, kubernetes)")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	outputFormat := fs.String("output", "table", "Output format (table, json)")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🔍 Discovering services for provider: %s\n", *providerName)

	providers := mustProviderApplication()
	defer providers.Close()

	// Create discover request
	req := &pb.DiscoverServicesRequest{
		ForceRefresh: *verbose, // Use verbose as force refresh for now
	}

	// Execute discovery
	resp, err := providers.DiscoverServices(context.Background(), *providerName, nil, req)
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
		// Group services by category
		categories := groupServicesByCategory(resp.Services)

		for category, services := range categories {
			if len(services) == 0 {
				continue
			}

			fmt.Printf("📦 %s Services (%d):\n", category, len(services))

			// Show in columns with resource counts
			for i := 0; i < len(services); i += 3 {
				fmt.Print("   ")
				for j := 0; j < 3 && i+j < len(services); j++ {
					svc := services[i+j]
					// Show name with resource type count
					fmt.Printf("%-25s", fmt.Sprintf("%s (%d types)",
						svc.Name, len(svc.ResourceTypes)))
				}
				fmt.Println()
			}
			fmt.Println()
		}

		// Show popular services for quick start
		fmt.Println("💡 Quick Start - Popular Services:")
		fmt.Println("   corkscrew scan --provider aws --services common")
		fmt.Println("   corkscrew scan --provider aws --services compute,storage")
		fmt.Println("   corkscrew scan --provider aws --services database,security")
		fmt.Println()
		fmt.Println("📦 Service Groups:")
		fmt.Println("   corkscrew plugin groups  # Show all available service groups")
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

	providers := mustProviderApplication()
	defer providers.Close()
	initCfg := map[string]string{}
	if len(regions) > 0 {
		initCfg["region"] = regions[0]
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
	resp, err := providers.ListResources(context.Background(), *providerName, initCfg, req)
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

	providers := mustProviderApplication()
	defer providers.Close()
	initCfg := map[string]string{}
	if *region != "" {
		initCfg["region"] = *region
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
	resp, err := providers.DescribeResource(context.Background(), *providerName, initCfg, req)
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

	providers := mustProviderApplication()
	defer providers.Close()

	// Get info
	info, err := providers.GetInfo(context.Background(), *providerName, nil)
	if err != nil {
		log.Fatalf("GetProviderInfo failed: %v", err)
	}
	resp := info.Runtime

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

	providers := mustProviderApplication()
	defer providers.Close()

	// Get schemas
	req := &pb.GetSchemasRequest{
		Services: services,
		Format:   "sql",
	}

	resp, err := providers.GetSchemas(context.Background(), *providerName, nil, req)
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

// runQuery executes SQL queries or compliance checks
func runQuery(args []string) {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	defaultDBPath := defaultDatabasePath()

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
	dbPath := fs.String("db", defaultDBPath, "Path to database file, or a quack: URI for a remote server")
	token := fs.String("token", "", "Auth token for a remote quack: server (or CORKSCREW_QUACK_TOKEN)")
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
		runPack(args[1:])
		return
	}

	// Handle list-packs
	if *listPacks {
		listArgs := []string{"--output", *outputFormat}
		if *outputFormat == "csv" {
			listArgs = []string{"--output", "table"}
		}
		runPackList(listArgs)
		return
	}

	resolvedDBPath, quackOpts := resolveQuackConn(*dbPath, defaultDBPath, *token)

	// Determine query source
	var sqlQuery string
	if *control != "" || *pack != "" || *compliance {
		// Run compliance query
		runComplianceQuery(resolvedDBPath, *control, *pack, *tags, params, *dryRun, *outputFormat, *verbose)
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
	} else if rest := fs.Args(); len(rest) > 0 && rest[0] != "" {
		// Query provided as a positional argument (works before or after flags)
		sqlQuery = rest[0]
	} else if *queryStr != "" {
		sqlQuery = *queryStr
	} else {
		fmt.Println("Error: No query provided")
		fmt.Println("Use -query, -file, -stdin, or provide query as argument")
		fs.Usage()
		os.Exit(1)
	}

	// Execute query through the application workflow
	result, err := appquery.Run(context.Background(), appquery.Request{
		Target:  resolvedDBPath,
		Options: quackOpts,
		SQL:     sqlQuery,
	})
	if err != nil {
		var queryErr *appquery.Error
		if errors.As(err, &queryErr) {
			handleQueryError(queryErr)
		} else {
			fmt.Fprintf(os.Stderr, "❌ Query execution failed: %v\n", err)
		}
		os.Exit(1)
	}

	// Format and display results
	switch *outputFormat {
	case "json":
		printJSONResults(result.Rows, result.Columns)
	case "csv":
		printCSVQueryResults(result.Rows, result.Columns, *noHeader)
	default:
		printTableResults(result.Rows, result.Columns, *noHeader)
	}
}

// runComplianceQuery handles compliance-specific queries
func runComplianceQuery(dbPath, controlID, packName, tags string, params map[string]interface{}, dryRun bool, outputFormat string, verbose bool) {
	request := appquery.ComplianceRequest{
		Target:     dbPath,
		ControlID:  controlID,
		PackName:   packName,
		Tags:       parseTags(tags),
		Parameters: params,
		DryRun:     dryRun,
	}

	// Show what we're about to do
	printComplianceOptions(request)

	// Execute compliance check through the application workflow
	results, err := appquery.RunCompliance(context.Background(), request)
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

func printComplianceTable(results []appquery.ComplianceResult, verbose bool) {
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

func printComplianceJSON(results []appquery.ComplianceResult) {
	output := struct {
		Summary struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
			Errors int `json:"errors"`
		} `json:"summary"`
		Results []appquery.ComplianceResult `json:"results"`
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

func printComplianceCSV(results []appquery.ComplianceResult) {
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

func handleQueryError(qerr *appquery.Error) {
	fmt.Fprintf(os.Stderr, "❌ Query execution failed: %v\n", qerr)

	switch qerr.Kind {
	case appquery.ErrorTableNotFound:
		fmt.Fprintf(os.Stderr, "\n💡 Hint: Make sure you've run 'corkscrew scan' to populate the database.\n")
		if len(qerr.AvailableTables) > 0 {
			fmt.Fprintf(os.Stderr, "Available tables:\n")
			for _, table := range qerr.AvailableTables {
				fmt.Fprintf(os.Stderr, "  - %s\n", table)
			}
		}
		if qerr.MissingTable != "" {
			fmt.Fprintf(os.Stderr, "  🔍 Table '%s' not found\n", qerr.MissingTable)
			if len(qerr.Suggestions) > 0 {
				fmt.Fprintf(os.Stderr, "  💡 Did you mean one of these?\n")
				for _, suggestion := range qerr.Suggestions {
					fmt.Fprintf(os.Stderr, "     - %s\n", suggestion)
				}
			}
		}
	case appquery.ErrorSyntax:
		fmt.Fprintf(os.Stderr, "\n💡 Hint: Check your SQL syntax. DuckDB uses standard SQL.\n")
		if qerr.Position > 0 {
			printSyntaxErrorPosition(qerr.SQL, qerr.Position)
		}
	}
}

func printSyntaxErrorPosition(sqlQuery string, position int) {
	lines := strings.Split(sqlQuery, "\n")
	lineNo := 1
	charCount := 0
	for _, line := range lines {
		if charCount+len(line) >= position {
			fmt.Fprintf(os.Stderr, "Error near line %d:\n", lineNo)
			fmt.Fprintf(os.Stderr, "  %s\n", line)
			fmt.Fprintf(os.Stderr, "  %s^\n", strings.Repeat(" ", position-charCount-1))
			return
		}
		charCount += len(line) + 1 // +1 for newline
		lineNo++
	}
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

func printComplianceOptions(request appquery.ComplianceRequest) {
	fmt.Println("\n🔍 Compliance Check Configuration:")

	if request.ControlID != "" {
		fmt.Printf("📌 Control: %s\n", request.ControlID)
	}

	if request.PackName != "" {
		fmt.Printf("📦 Pack: %s\n", request.PackName)
	}

	if len(request.Tags) > 0 {
		fmt.Printf("🏷️  Tags: %s\n", strings.Join(request.Tags, ", "))
	}

	if len(request.Parameters) > 0 {
		fmt.Printf("🔧 Parameters:\n")
		for key, value := range request.Parameters {
			fmt.Printf("  %s = %v\n", key, value)
		}
	}

	if request.DryRun {
		fmt.Printf("✅ Dry-run validation would be performed for tagged queries\n")
	}
}

// runCrossCloud handles cross-cloud operations
func runCrossCloud(args []string) {
	if len(args) == 0 {
		printCrossCloudUsage()
		return
	}

	subcommand := args[0]
	switch subcommand {
	case "scan":
		runCrossCloudScan(args[1:])
	case "correlate":
		runCrossCloudCorrelate(args[1:])
	case "topology":
		runCrossCloudTopology(args[1:])
	case "export":
		runCrossCloudExport(args[1:])
	case "network":
		runCrossCloudNetwork(args[1:])
	case "analyze":
		runCrossCloudAnalyze(args[1:])
	default:
		fmt.Printf("Unknown crosscloud subcommand: %s\n", subcommand)
		printCrossCloudUsage()
		os.Exit(1)
	}
}

// runCorrelate handles correlation operations
func runCorrelate(args []string) {
	if len(args) == 0 {
		printCorrelateUsage()
		return
	}

	subcommand := args[0]
	switch subcommand {
	case "ip":
		runCorrelateIP(args[1:])
	case "dns":
		runCorrelateDNS(args[1:])
	case "network":
		runCorrelateNetwork(args[1:])
	case "all":
		runCorrelateAll(args[1:])
	default:
		fmt.Printf("Unknown correlate subcommand: %s\n", subcommand)
		printCorrelateUsage()
		os.Exit(1)
	}
}

// Cross-cloud scan implementation
func runCrossCloudScan(args []string) {
	fs := flag.NewFlagSet("crosscloud scan", flag.ExitOnError)

	providers := fs.String("providers", "", "Comma-separated list of providers (aws,azure,gcp)")
	regions := fs.String("regions", "", "Comma-separated list of regions")
	_ = fs.String("services", "", "Comma-separated list of services")
	output := fs.String("output", "table", "Output format (table, json, csv)")
	confidence := fs.Float64("confidence", 0.7, "Minimum confidence score for correlations")
	_ = fs.Int("parallel", 3, "Number of parallel provider scans")
	_ = fs.Duration("timeout", 30*time.Minute, "Scan timeout")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	if *providers == "" {
		fmt.Println("❌ Error: --providers flag is required")
		fmt.Println("Example: corkscrew crosscloud scan --providers aws,azure --regions us-east-1,eastus")
		os.Exit(1)
	}

	// Convert command-line options to NetworkAnalysisOptions
	var regionList, providerList []string
	if *regions != "" {
		regionList = strings.Split(*regions, ",")
	}
	if *providers != "" {
		providerList = strings.Split(*providers, ",")
	}

	options := NetworkAnalysisOptions{
		Providers:     providerList,
		Regions:       regionList,
		OutputFormat:  *output,
		MinConfidence: *confidence,
		MaxResults:    1000,
	}

	// Get database path
	dbPath := ""
	if len(args) > 0 {
		for i, arg := range args {
			if arg == "--db" && i+1 < len(args) {
				dbPath = args[i+1]
				break
			}
		}
	}

	// Run network scan using our Phase 2 implementation
	if err := runNetworkScan(dbPath, options); err != nil {
		log.Fatalf("Cross-cloud network scan failed: %v", err)
	}
}

// Cross-cloud correlation implementation
func runCrossCloudCorrelate(args []string) {
	fs := flag.NewFlagSet("crosscloud correlate", flag.ExitOnError)

	confidence := fs.Float64("confidence", 0.7, "Minimum confidence score")
	types := fs.String("types", "ip,dns,network", "Correlation types (ip,dns,network,all)")
	output := fs.String("output", "table", "Output format (table, json, csv)")
	dbPath := fs.String("db", "", "Database path")
	verify := fs.Bool("verify", false, "Verify correlations with additional checks")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🔗 Finding cross-cloud correlations...\n")
	fmt.Printf("🎯 Confidence threshold: %.2f\n", *confidence)
	fmt.Printf("📊 Correlation types: %s\n", *types)
	if *verify {
		fmt.Println("Verification is handled by graph extension fixtures and confidence thresholds")
	}
	fmt.Println()

	options := NetworkAnalysisOptions{
		CorrelationTypes: crosscloud.ParseCorrelationTypes(*types),
		MinConfidence:    *confidence,
		OutputFormat:     *output,
	}
	if err := runGraphCorrelations(*dbPath, options.CorrelationTypes, options); err != nil {
		log.Fatalf("Cross-cloud correlation failed: %v", err)
	}
}

// Cross-cloud topology implementation
func runCrossCloudTopology(args []string) {
	fs := flag.NewFlagSet("crosscloud topology", flag.ExitOnError)

	output := fs.String("output", "table", "Output format (table, json, csv, graph)")
	_ = fs.Int("depth", 3, "Maximum relationship depth")
	includePrivate := fs.Bool("include-private", false, "Include private network connections")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	// Convert command-line options to NetworkAnalysisOptions
	options := NetworkAnalysisOptions{
		OutputFormat:        *output,
		VisualizationFormat: "ascii", // Default to ASCII for terminal display
		ShowDetails:         *includePrivate,
		MaxResults:          1000,
	}

	// Handle graph output format
	if *output == "graph" {
		options.VisualizationFormat = "ascii"
		options.OutputFormat = "ascii"
	}

	// Get database path
	dbPath := ""
	if len(args) > 0 {
		for i, arg := range args {
			if arg == "--db" && i+1 < len(args) {
				dbPath = args[i+1]
				break
			}
		}
	}

	// Run network topology visualization using our Phase 2 implementation
	if err := runNetworkTopology(dbPath, options); err != nil {
		log.Fatalf("Cross-cloud topology generation failed: %v", err)
	}
}

// Cross-cloud export implementation
func runCrossCloudExport(args []string) {
	fs := flag.NewFlagSet("crosscloud export", flag.ExitOnError)

	format := fs.String("format", "json", "Export format (json, csv, yaml)")
	output := fs.String("output", "", "Output file (default: stdout)")
	includeRaw := fs.Bool("include-raw", false, "Include raw resource data")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	_ = format
	_ = output
	_ = includeRaw

	fmt.Fprintln(os.Stderr, "crosscloud export is not implemented; export cross-cloud data with `corkscrew query` against all_cloud_resources / cloud_relationships")
	os.Exit(1)
}

// IP correlation implementation
func runCorrelateIP(args []string) {
	fs := flag.NewFlagSet("correlate ip", flag.ExitOnError)

	providers := fs.String("providers", "", "Comma-separated list of providers")
	confidence := fs.Float64("confidence", 0.8, "Minimum confidence score")
	publicOnly := fs.Bool("public-only", false, "Only correlate public IP addresses")
	output := fs.String("output", "table", "Output format (table, json, csv)")
	dbPath := fs.String("db", "", "Database path")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🌐 Finding IP address correlations...\n")
	if *providers != "" {
		fmt.Printf("📍 Providers: %s\n", *providers)
	}
	fmt.Printf("🎯 Confidence threshold: %.2f\n", *confidence)
	if *publicOnly {
		fmt.Println("🌍 Public IPs only")
	}
	fmt.Println()

	options := NetworkAnalysisOptions{
		MinConfidence: *confidence,
		OutputFormat:  *output,
	}
	if err := runCorrelationAnalysis(*dbPath, "ip", options); err != nil {
		log.Fatalf("IP correlation failed: %v", err)
	}
}

// DNS correlation implementation
func runCorrelateDNS(args []string) {
	fs := flag.NewFlagSet("correlate dns", flag.ExitOnError)

	providers := fs.String("providers", "", "Comma-separated list of providers")
	confidence := fs.Float64("confidence", 0.8, "Minimum confidence score")
	includeCNAME := fs.Bool("include-cname", true, "Include CNAME chain analysis")
	output := fs.String("output", "table", "Output format (table, json, csv)")
	dbPath := fs.String("db", "", "Database path")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🌐 Finding DNS correlations...\n")
	if *providers != "" {
		fmt.Printf("📍 Providers: %s\n", *providers)
	}
	fmt.Printf("🎯 Confidence threshold: %.2f\n", *confidence)
	if *includeCNAME {
		fmt.Println("🔗 CNAME chain analysis enabled")
	}
	fmt.Println()

	options := NetworkAnalysisOptions{
		MinConfidence: *confidence,
		OutputFormat:  *output,
	}
	if err := runCorrelationAnalysis(*dbPath, "dns", options); err != nil {
		log.Fatalf("DNS correlation failed: %v", err)
	}
}

// Network correlation implementation
func runCorrelateNetwork(args []string) {
	fs := flag.NewFlagSet("correlate network", flag.ExitOnError)

	providers := fs.String("providers", "", "Comma-separated list of providers")
	confidence := fs.Float64("confidence", 0.7, "Minimum confidence score")
	includeVPN := fs.Bool("include-vpn", true, "Include VPN connections")
	includePeering := fs.Bool("include-peering", true, "Include peering connections")
	output := fs.String("output", "table", "Output format (table, json, csv)")
	dbPath := fs.String("db", "", "Database path")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🌐 Finding network correlations...\n")
	if *providers != "" {
		fmt.Printf("📍 Providers: %s\n", *providers)
	}
	fmt.Printf("🎯 Confidence threshold: %.2f\n", *confidence)
	if *includeVPN {
		fmt.Println("🔒 VPN analysis enabled")
	}
	if *includePeering {
		fmt.Println("🔗 Peering analysis enabled")
	}
	fmt.Println()

	options := NetworkAnalysisOptions{
		MinConfidence: *confidence,
		OutputFormat:  *output,
	}
	if err := runCorrelationAnalysis(*dbPath, "network", options); err != nil {
		log.Fatalf("Network correlation failed: %v", err)
	}
}

// All correlations implementation
func runCorrelateAll(args []string) {
	fs := flag.NewFlagSet("correlate all", flag.ExitOnError)

	providers := fs.String("providers", "", "Comma-separated list of providers")
	confidence := fs.Float64("confidence", 0.7, "Minimum confidence score")
	output := fs.String("output", "table", "Output format (table, json, csv)")
	parallel := fs.Bool("parallel", true, "Run correlations in parallel")
	dbPath := fs.String("db", "", "Database path")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("🌐 Running comprehensive correlation analysis...\n")
	if *providers != "" {
		fmt.Printf("📍 Providers: %s\n", *providers)
	}
	fmt.Printf("🎯 Confidence threshold: %.2f\n", *confidence)
	if *parallel {
		fmt.Println("⚡ Parallel processing enabled")
	}
	fmt.Println()

	options := NetworkAnalysisOptions{
		CorrelationTypes: crosscloud.AllCorrelationKinds(),
		MinConfidence:    *confidence,
		OutputFormat:     *output,
	}
	if err := runGraphCorrelations(*dbPath, options.CorrelationTypes, options); err != nil {
		log.Fatalf("Correlation analysis failed: %v", err)
	}
}

// Usage functions
func printCrossCloudUsage() {
	fmt.Println("Cross-Cloud Operations")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  corkscrew crosscloud <subcommand> [options]")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  scan        - Scan multiple cloud providers simultaneously")
	fmt.Println("  correlate   - Find correlations between cloud resources")
	fmt.Println("  topology    - Generate cross-cloud network topology")
	fmt.Println("  network     - Network-specific cross-cloud analysis")
	fmt.Println("  analyze     - Advanced cross-cloud analysis")
	fmt.Println("  export      - Export cross-cloud data")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  corkscrew crosscloud scan --providers aws,azure --regions us-east-1,eastus")
	fmt.Println("  corkscrew crosscloud correlate --confidence 0.8")
	fmt.Println("  corkscrew crosscloud topology --output graph")
	fmt.Println("  corkscrew crosscloud network analyze --providers aws,azure")
	fmt.Println("  corkscrew crosscloud network vpn --confidence 0.8")
	fmt.Println("  corkscrew crosscloud analyze relationships --details")
	fmt.Println("  corkscrew crosscloud export --format csv --output relationships.csv")
}

func printCorrelateUsage() {
	fmt.Println("Correlation Analysis")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  corkscrew correlate <type> [options]")
	fmt.Println()
	fmt.Println("Types:")
	fmt.Println("  ip       - Find resources sharing IP addresses")
	fmt.Println("  dns      - Find resources sharing DNS names")
	fmt.Println("  network  - Find network-level relationships")
	fmt.Println("  all      - Run all correlation types")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  corkscrew correlate ip --providers aws,azure --confidence 0.8")
	fmt.Println("  corkscrew correlate dns --include-cname")
	fmt.Println("  corkscrew correlate network --include-vpn --include-peering")
	fmt.Println("  corkscrew correlate all --parallel")
}

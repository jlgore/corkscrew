package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jlgore/corkscrew/pkg/query/compliance"
)

// runPack handles all pack management commands
func runPack(args []string) {
	if len(args) == 0 {
		printPackUsage()
		return
	}

	command := args[0]
	switch command {
	case "search":
		runPackSearch(args[1:])
	case "install":
		runPackInstall(args[1:])
	case "list":
		runPackList(args[1:])
	case "update":
		runPackUpdate(args[1:])
	case "validate":
		runPackValidate(args[1:])
	case "info":
		runPackInfo(args[1:])
	case "cache":
		runPackCache(args[1:])
	default:
		fmt.Printf("Unknown pack command: %s\n", command)
		printPackUsage()
		os.Exit(1)
	}
}

// printPackUsage displays pack management usage information
func printPackUsage() {
	fmt.Println("📦 Pack Management")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  corkscrew pack search <query>           - Search the registry for compliance packs")
	fmt.Println("  corkscrew pack install <namespace/pack> - Download and install a compliance pack")
	fmt.Println("  corkscrew pack list                     - Show installed compliance packs")
	fmt.Println("  corkscrew pack update [pack]            - Update installed packs (or all if no pack specified)")
	fmt.Println("  corkscrew pack validate [pack]          - Verify pack integrity and dependencies")
	fmt.Println("  corkscrew pack info <namespace/pack>    - Show detailed information about a pack")
	fmt.Println("  corkscrew pack cache <action>           - Manage registry cache (clear, info, update)")
	fmt.Println()
	fmt.Println("Registry Sources:")
	fmt.Println("  • Central registry: github.com/jlgore/cs-query-registry")
	fmt.Println("  • GitHub repositories: user/repo pattern for direct install")
	fmt.Println("  • Local filesystem: ~/.corkscrew/query-packs/")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Search for AWS security packs")
	fmt.Println("  corkscrew pack search \"aws security\"")
	fmt.Println()
	fmt.Println("  # Install pack from registry")
	fmt.Println("  corkscrew pack install jlgore/cfi-ccc")
	fmt.Println("  corkscrew pack install jlgore/aws-security")
	fmt.Println()
	fmt.Println("  # Install pack directly from GitHub")
	fmt.Println("  corkscrew pack install github.com/company/security-packs")
	fmt.Println()
	fmt.Println("  # Update all packs")
	fmt.Println("  corkscrew pack update")
	fmt.Println()
	fmt.Println("  # Validate specific pack")
	fmt.Println("  corkscrew pack validate jlgore/cfi-ccc")
}

// runPackSearch searches the registry for compliance packs
func runPackSearch(args []string) {
	fs := flag.NewFlagSet("pack search", flag.ExitOnError)
	
	provider := fs.String("provider", "", "Filter by provider (aws, azure, gcp, kubernetes)")
	framework := fs.String("framework", "", "Filter by compliance framework (ccc, iso27001, nist, sox)")
	category := fs.String("category", "", "Filter by category (security, cost, operations)")
	namespace := fs.String("namespace", "", "Filter by namespace (e.g., jlgore, company)")
	tags := fs.String("tags", "", "Filter by tags (comma-separated)")
	limit := fs.Int("limit", 20, "Maximum number of results")
	offset := fs.Int("offset", 0, "Offset for pagination")
	sort := fs.String("sort", "name", "Sort by: name, downloads, updated")
	order := fs.String("order", "asc", "Sort order: asc, desc")
	output := fs.String("output", "table", "Output format: table, json")
	verbose := fs.Bool("verbose", false, "Show detailed pack information")

	fs.Parse(args)

	var query string
	if len(fs.Args()) > 0 {
		query = strings.Join(fs.Args(), " ")
	}

	if query == "" && *provider == "" && *framework == "" && *category == "" && *namespace == "" && *tags == "" {
		fmt.Fprintf(os.Stderr, "❌ Search query or filter is required\n")
		fmt.Fprintf(os.Stderr, "💡 Examples:\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack search \"aws security\"\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack search --provider aws --framework ccc\n")
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("🔍 Searching registry for compliance packs...\n")
		if query != "" {
			fmt.Printf("  Query: %s\n", query)
		}
		if *provider != "" {
			fmt.Printf("  Provider: %s\n", *provider)
		}
		if *framework != "" {
			fmt.Printf("  Framework: %s\n", *framework)
		}
		fmt.Println()
	}

	// Create registry client
	registryClient := compliance.NewRegistryClient()
	
	// Check for GitHub token
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		registryClient = registryClient.WithGitHubToken(token)
	}

	ctx := context.Background()

	// Update registry cache if needed
	if err := registryClient.UpdateRegistry(ctx, false); err != nil {
		if *verbose {
			fmt.Printf("⚠️  Warning: Failed to update registry cache: %v\n", err)
			fmt.Printf("Using cached data...\n\n")
		}
	}

	// Build search criteria
	criteria := compliance.SearchCriteria{
		Query:     query,
		Provider:  *provider,
		Framework: *framework,
		Category:  *category,
		Namespace: *namespace,
		Sort:      *sort,
		Order:     *order,
		Limit:     *limit,
		Offset:    *offset,
	}

	if *tags != "" {
		criteria.Tags = strings.Split(*tags, ",")
		for i := range criteria.Tags {
			criteria.Tags[i] = strings.TrimSpace(criteria.Tags[i])
		}
	}

	// Execute search
	result, err := registryClient.SearchPacks(ctx, criteria)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Search failed: %v\n", err)
		os.Exit(1)
	}

	// Display results
	switch *output {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to encode JSON: %v\n", err)
			os.Exit(1)
		}

	case "table":
		displaySearchResults(result, *verbose)

	default:
		fmt.Fprintf(os.Stderr, "❌ Invalid output format: %s\n", *output)
		os.Exit(1)
	}
}

// displaySearchResults displays search results in table format
func displaySearchResults(result *compliance.SearchResult, verbose bool) {
	if len(result.Packs) == 0 {
		fmt.Println("No packs found matching your criteria.")
		return
	}

	fmt.Printf("📦 Found %d pack(s) (showing %d):\n\n", result.Total, len(result.Packs))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	
	if verbose {
		fmt.Fprintln(w, "NAMESPACE\tNAME\tVERSION\tPROVIDER\tFRAMEWORKS\tTAGS\tDESCRIPTION")
		fmt.Fprintln(w, "---------\t----\t-------\t--------\t----------\t----\t-----------")
		
		for _, pack := range result.Packs {
			frameworks := strings.Join(pack.Frameworks, ",")
			if frameworks == "" {
				frameworks = "-"
			}
			tags := strings.Join(pack.Tags, ",")
			if tags == "" {
				tags = "-"
			}
			
			description := pack.Description
			if len(description) > 50 {
				description = description[:47] + "..."
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				pack.Namespace, pack.Name, pack.LatestVersion, pack.Provider,
				frameworks, tags, description)
		}
	} else {
		fmt.Fprintln(w, "NAMESPACE\tNAME\tVERSION\tPROVIDER\tDESCRIPTION")
		fmt.Fprintln(w, "---------\t----\t-------\t--------\t-----------")
		
		for _, pack := range result.Packs {
			description := pack.Description
			if len(description) > 60 {
				description = description[:57] + "..."
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				pack.Namespace, pack.Name, pack.LatestVersion, pack.Provider, description)
		}
	}
	
	w.Flush()

	// Show pagination info
	if result.Total > len(result.Packs) {
		fmt.Printf("\n💡 Showing results %d-%d of %d total. Use --limit and --offset for pagination.\n",
			result.Offset+1, result.Offset+len(result.Packs), result.Total)
	}

	if verbose {
		fmt.Printf("\n⏱️  Search completed in %v\n", result.Duration)
	}
}

// runPackInstall installs a compliance pack
func runPackInstall(args []string) {
	fs := flag.NewFlagSet("pack install", flag.ExitOnError)
	
	version := fs.String("version", "latest", "Specific version to install")
	force := fs.Bool("force", false, "Force reinstall even if pack exists")
	dryRun := fs.Bool("dry-run", false, "Show what would be installed without actually installing")
	installDeps := fs.Bool("deps", true, "Install dependencies automatically")
	destDir := fs.String("dest", "", "Custom installation directory (defaults to ~/.corkscrew/query-packs)")
	verbose := fs.Bool("verbose", false, "Verbose output")

	fs.Parse(args)

	if len(fs.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "❌ Pack name/namespace is required\n")
		fmt.Fprintf(os.Stderr, "💡 Examples:\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack install jlgore/cfi-ccc\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack install github.com/company/security-packs\n")
		os.Exit(1)
	}

	packRef := fs.Args()[0]

	if *verbose {
		fmt.Printf("📦 Installing compliance pack: %s\n", packRef)
		if *version != "latest" {
			fmt.Printf("  Version: %s\n", *version)
		}
		if !*installDeps {
			fmt.Printf("  Dependencies: disabled\n")
		}
		fmt.Println()
	}

	// Determine destination directory
	var installDir string
	if *destDir != "" {
		installDir = *destDir
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to get home directory: %v\n", err)
			os.Exit(1)
		}
		installDir = filepath.Join(homeDir, ".corkscrew", "query-packs")
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(installDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create install directory: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// Check if it's a direct GitHub reference
	if strings.Contains(packRef, "github.com/") || strings.Contains(packRef, "/") && !strings.Contains(packRef, "@") {
		err := installFromGitHub(ctx, packRef, *version, installDir, *force, *dryRun, *verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Installation failed: %v\n", err)
			os.Exit(1)
		}
	} else {
		err := installFromRegistry(ctx, packRef, *version, installDir, *force, *dryRun, *installDeps, *verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Installation failed: %v\n", err)
			os.Exit(1)
		}
	}

	if !*dryRun {
		fmt.Printf("✅ Pack '%s' installed successfully!\n", packRef)
		fmt.Printf("📍 Location: %s\n", installDir)
		
		if *verbose {
			fmt.Printf("\n💡 You can now use this pack with:\n")
			fmt.Printf("  corkscrew query --pack %s\n", packRef)
		}
	}
}

// installFromRegistry installs a pack from the central registry
func installFromRegistry(ctx context.Context, packRef, version, installDir string, force, dryRun, installDeps, verbose bool) error {
	registryClient := compliance.NewRegistryClient()
	
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		registryClient = registryClient.WithGitHubToken(token)
	}

	if verbose {
		fmt.Printf("🔍 Searching registry for pack: %s\n", packRef)
	}

	// Update registry if needed
	if err := registryClient.UpdateRegistry(ctx, false); err != nil {
		if verbose {
			fmt.Printf("⚠️  Warning: Failed to update registry: %v\n", err)
		}
	}

	if dryRun {
		fmt.Printf("🔍 [DRY-RUN] Would install pack '%s' version '%s' to '%s'\n", packRef, version, installDir)
		return nil
	}

	// Download and install the pack
	pack, err := registryClient.DownloadPack(ctx, packRef, version, installDir)
	if err != nil {
		return fmt.Errorf("failed to download pack: %w", err)
	}

	if verbose {
		fmt.Printf("✅ Downloaded pack: %s v%s\n", pack.Metadata.Name, pack.Metadata.Version)
		fmt.Printf("📊 Queries: %d\n", len(pack.Queries))
		if len(pack.DependsOn) > 0 {
			fmt.Printf("🔗 Dependencies: %v\n", pack.DependsOn)
		}
	}

	return nil
}

// installFromGitHub installs a pack directly from GitHub
func installFromGitHub(ctx context.Context, packRef, version, installDir string, force, dryRun, verbose bool) error {
	if verbose {
		fmt.Printf("🐙 Installing from GitHub: %s\n", packRef)
	}

	if dryRun {
		fmt.Printf("🔍 [DRY-RUN] Would clone repository '%s' to '%s'\n", packRef, installDir)
		return nil
	}

	// For now, return a helpful message
	return fmt.Errorf("direct GitHub installation not yet implemented - use registry search to find published packs")
}

// runPackList lists installed compliance packs
func runPackList(args []string) {
	fs := flag.NewFlagSet("pack list", flag.ExitOnError)
	
	output := fs.String("output", "table", "Output format: table, json")
	verbose := fs.Bool("verbose", false, "Show detailed pack information")
	showQueries := fs.Bool("queries", false, "Show query count for each pack")
	filter := fs.String("filter", "", "Filter by provider, framework, or namespace")

	fs.Parse(args)

	if *verbose {
		fmt.Printf("📋 Scanning for installed compliance packs...\n\n")
	}

	// Create pack loader
	loader := compliance.NewPackLoader()
	
	ctx := context.Background()

	// Discover all available packs
	packs, err := loader.DiscoverPacks(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to discover packs: %v\n", err)
		os.Exit(1)
	}

	if len(packs) == 0 {
		fmt.Println("No compliance packs found.")
		fmt.Println("💡 Install packs using: corkscrew pack install <namespace/pack>")
		return
	}

	// Load pack information
	var loadedPacks []*compliance.QueryPack
	for _, packName := range packs {
		pack, err := loader.LoadPack(ctx, packName)
		if err != nil {
			if *verbose {
				fmt.Printf("⚠️  Failed to load pack '%s': %v\n", packName, err)
			}
			continue
		}

		// Apply filter if specified
		if *filter != "" {
			filter := strings.ToLower(*filter)
			if !strings.Contains(strings.ToLower(pack.Metadata.Provider), filter) &&
				!strings.Contains(strings.ToLower(pack.Namespace), filter) &&
				!containsFramework(pack.Metadata.Frameworks, filter) {
				continue
			}
		}

		loadedPacks = append(loadedPacks, pack)
	}

	// Sort packs by namespace and name
	sort.Slice(loadedPacks, func(i, j int) bool {
		if loadedPacks[i].Namespace != loadedPacks[j].Namespace {
			return loadedPacks[i].Namespace < loadedPacks[j].Namespace
		}
		return loadedPacks[i].Metadata.Name < loadedPacks[j].Metadata.Name
	})

	// Display results
	switch *output {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(loadedPacks); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to encode JSON: %v\n", err)
			os.Exit(1)
		}

	case "table":
		displayInstalledPacks(loadedPacks, *verbose, *showQueries)

	default:
		fmt.Fprintf(os.Stderr, "❌ Invalid output format: %s\n", *output)
		os.Exit(1)
	}
}

// displayInstalledPacks displays installed packs in table format
func displayInstalledPacks(packs []*compliance.QueryPack, verbose, showQueries bool) {
	if len(packs) == 0 {
		fmt.Println("No compliance packs found.")
		return
	}

	fmt.Printf("📦 Found %d installed compliance pack(s):\n\n", len(packs))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	
	if verbose {
		fmt.Fprintln(w, "NAMESPACE\tNAME\tVERSION\tPROVIDER\tFRAMEWORKS\tQUERIES\tLOADED FROM")
		fmt.Fprintln(w, "---------\t----\t-------\t--------\t----------\t-------\t-----------")
		
		for _, pack := range packs {
			frameworks := strings.Join(pack.Metadata.Frameworks, ",")
			if frameworks == "" {
				frameworks = "-"
			}
			
			loadedFrom := pack.LoadedFrom
			if len(loadedFrom) > 40 {
				loadedFrom = "..." + loadedFrom[len(loadedFrom)-37:]
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
				pack.Namespace, pack.Metadata.Name, pack.Metadata.Version,
				pack.Metadata.Provider, frameworks, len(pack.Queries), loadedFrom)
		}
	} else {
		header := "NAMESPACE\tNAME\tVERSION\tPROVIDER"
		separator := "---------\t----\t-------\t--------"
		
		if showQueries {
			header += "\tQUERIES"
			separator += "\t-------"
		}
		
		fmt.Fprintln(w, header)
		fmt.Fprintln(w, separator)
		
		for _, pack := range packs {
			line := fmt.Sprintf("%s\t%s\t%s\t%s",
				pack.Namespace, pack.Metadata.Name, pack.Metadata.Version, pack.Metadata.Provider)
			
			if showQueries {
				line += fmt.Sprintf("\t%d", len(pack.Queries))
			}
			
			fmt.Fprintln(w, line)
		}
	}
	
	w.Flush()

	// Summary
	providers := make(map[string]int)
	frameworks := make(map[string]int)
	totalQueries := 0
	
	for _, pack := range packs {
		providers[pack.Metadata.Provider]++
		totalQueries += len(pack.Queries)
		for _, framework := range pack.Metadata.Frameworks {
			frameworks[framework]++
		}
	}

	fmt.Printf("\n📊 Summary:\n")
	fmt.Printf("  Total Queries: %d\n", totalQueries)
	
	if len(providers) > 0 {
		fmt.Printf("  Providers: ")
		var providerList []string
		for provider, count := range providers {
			providerList = append(providerList, fmt.Sprintf("%s (%d)", provider, count))
		}
		fmt.Printf("%s\n", strings.Join(providerList, ", "))
	}
}

// runPackUpdate updates installed packs
func runPackUpdate(args []string) {
	fs := flag.NewFlagSet("pack update", flag.ExitOnError)
	
	all := fs.Bool("all", false, "Update all installed packs")
	dryRun := fs.Bool("dry-run", false, "Show what would be updated without actually updating")
	force := fs.Bool("force", false, "Force update even if no newer version available")
	verbose := fs.Bool("verbose", false, "Verbose output")

	fs.Parse(args)

	packRefs := fs.Args()
	
	if len(packRefs) == 0 && !*all {
		fmt.Fprintf(os.Stderr, "❌ Pack name(s) or --all flag required\n")
		fmt.Fprintf(os.Stderr, "💡 Examples:\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack update --all\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack update jlgore/cfi-ccc\n")
		os.Exit(1)
	}

	if *all {
		if *verbose {
			fmt.Printf("🔄 Updating all installed compliance packs...\n\n")
		}
		
		// Load all installed packs
		loader := compliance.NewPackLoader()
		ctx := context.Background()
		
		packs, err := loader.DiscoverPacks(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to discover packs: %v\n", err)
			os.Exit(1)
		}
		
		packRefs = packs
	}

	if *dryRun {
		fmt.Printf("🔍 [DRY-RUN] Would update the following packs:\n")
		for _, packRef := range packRefs {
			fmt.Printf("  • %s\n", packRef)
		}
		return
	}

	// Update each pack
	updated := 0
	failed := 0
	
	for _, packRef := range packRefs {
		if *verbose {
			fmt.Printf("🔄 Updating %s...\n", packRef)
		}
		
		err := updateSinglePack(packRef, *force, *verbose)
		if err != nil {
			fmt.Printf("❌ Failed to update %s: %v\n", packRef, err)
			failed++
		} else {
			if *verbose {
				fmt.Printf("✅ Updated %s\n", packRef)
			}
			updated++
		}
	}

	fmt.Printf("\n📊 Update Summary:\n")
	fmt.Printf("  ✅ Updated: %d\n", updated)
	if failed > 0 {
		fmt.Printf("  ❌ Failed: %d\n", failed)
	}
}

// updateSinglePack updates a single pack
func updateSinglePack(packRef string, force, verbose bool) error {
	// For now, return a placeholder implementation
	if verbose {
		fmt.Printf("  Checking for updates to %s...\n", packRef)
	}
	
	// TODO: Implement actual update logic
	// 1. Check current version
	// 2. Check latest version in registry
	// 3. Download and install if newer version available
	
	return fmt.Errorf("pack update not yet implemented")
}

// runPackValidate validates pack integrity and dependencies
func runPackValidate(args []string) {
	fs := flag.NewFlagSet("pack validate", flag.ExitOnError)
	
	all := fs.Bool("all", false, "Validate all installed packs")
	checkDeps := fs.Bool("deps", true, "Check dependencies")
	checkQueries := fs.Bool("queries", true, "Validate SQL queries")
	verbose := fs.Bool("verbose", false, "Verbose output")

	fs.Parse(args)

	packRefs := fs.Args()
	
	if len(packRefs) == 0 && !*all {
		fmt.Fprintf(os.Stderr, "❌ Pack name(s) or --all flag required\n")
		fmt.Fprintf(os.Stderr, "💡 Examples:\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack validate --all\n")
		fmt.Fprintf(os.Stderr, "  corkscrew pack validate jlgore/cfi-ccc\n")
		os.Exit(1)
	}

	loader := compliance.NewPackLoader()
	ctx := context.Background()

	if *all {
		if *verbose {
			fmt.Printf("🔍 Validating all installed compliance packs...\n\n")
		}
		
		packs, err := loader.DiscoverPacks(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to discover packs: %v\n", err)
			os.Exit(1)
		}
		
		packRefs = packs
	}

	valid := 0
	invalid := 0
	
	for _, packRef := range packRefs {
		if *verbose {
			fmt.Printf("🔍 Validating %s...\n", packRef)
		}
		
		err := validateSinglePack(ctx, loader, packRef, *checkDeps, *checkQueries, *verbose)
		if err != nil {
			fmt.Printf("❌ %s: %v\n", packRef, err)
			invalid++
		} else {
			fmt.Printf("✅ %s: valid\n", packRef)
			valid++
		}
	}

	fmt.Printf("\n📊 Validation Summary:\n")
	fmt.Printf("  ✅ Valid: %d\n", valid)
	if invalid > 0 {
		fmt.Printf("  ❌ Invalid: %d\n", invalid)
		os.Exit(1)
	}
}

// validateSinglePack validates a single pack
func validateSinglePack(ctx context.Context, loader *compliance.PackLoader, packRef string, checkDeps, checkQueries, verbose bool) error {
	pack, err := loader.LoadPack(ctx, packRef)
	if err != nil {
		return fmt.Errorf("failed to load pack: %w", err)
	}

	if verbose {
		fmt.Printf("  📋 Pack: %s v%s\n", pack.Metadata.Name, pack.Metadata.Version)
		fmt.Printf("  📊 Queries: %d\n", len(pack.Queries))
	}

	// Basic pack validation is done during loading
	// Additional validations can be added here

	if checkDeps && len(pack.DependsOn) > 0 {
		if verbose {
			fmt.Printf("  🔗 Checking dependencies...\n")
		}
		// TODO: Implement dependency validation
	}

	if checkQueries {
		if verbose {
			fmt.Printf("  📝 Validating SQL queries...\n")
		}
		// TODO: Implement SQL query validation
	}

	return nil
}

// runPackInfo shows detailed information about a pack
func runPackInfo(args []string) {
	fs := flag.NewFlagSet("pack info", flag.ExitOnError)
	
	output := fs.String("output", "table", "Output format: table, json")
	verbose := fs.Bool("verbose", false, "Show detailed information")

	fs.Parse(args)

	if len(fs.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "❌ Pack name/namespace is required\n")
		fmt.Fprintf(os.Stderr, "💡 Example: corkscrew pack info jlgore/cfi-ccc\n")
		os.Exit(1)
	}

	packRef := fs.Args()[0]

	loader := compliance.NewPackLoader()
	ctx := context.Background()

	pack, err := loader.LoadPack(ctx, packRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to load pack '%s': %v\n", packRef, err)
		os.Exit(1)
	}

	switch *output {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(pack); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to encode JSON: %v\n", err)
			os.Exit(1)
		}

	case "table":
		displayPackInfo(pack, *verbose)

	default:
		fmt.Fprintf(os.Stderr, "❌ Invalid output format: %s\n", *output)
		os.Exit(1)
	}
}

// displayPackInfo displays detailed pack information
func displayPackInfo(pack *compliance.QueryPack, verbose bool) {
	fmt.Printf("📦 Pack Information: %s\n\n", pack.Metadata.Name)
	
	fmt.Printf("Basic Information:\n")
	fmt.Printf("  Name: %s\n", pack.Metadata.Name)
	fmt.Printf("  Namespace: %s\n", pack.Namespace)
	fmt.Printf("  Version: %s\n", pack.Metadata.Version)
	fmt.Printf("  Description: %s\n", pack.Metadata.Description)
	fmt.Printf("  Provider: %s\n", pack.Metadata.Provider)
	
	if pack.Metadata.Author != "" {
		fmt.Printf("  Author: %s\n", pack.Metadata.Author)
	}
	
	if len(pack.Metadata.Maintainers) > 0 {
		fmt.Printf("  Maintainers: %s\n", strings.Join(pack.Metadata.Maintainers, ", "))
	}

	fmt.Printf("\nCompliance Information:\n")
	if len(pack.Metadata.Frameworks) > 0 {
		fmt.Printf("  Frameworks: %s\n", strings.Join(pack.Metadata.Frameworks, ", "))
	}
	
	if len(pack.Metadata.Resources) > 0 {
		fmt.Printf("  Resources: %s\n", strings.Join(pack.Metadata.Resources, ", "))
	}
	
	if len(pack.Metadata.Tags) > 0 {
		fmt.Printf("  Tags: %s\n", strings.Join(pack.Metadata.Tags, ", "))
	}

	fmt.Printf("\nContent:\n")
	fmt.Printf("  Queries: %d\n", len(pack.Queries))
	fmt.Printf("  Parameters: %d\n", len(pack.Parameters))
	fmt.Printf("  Dependencies: %d\n", len(pack.DependsOn))

	if verbose {
		if len(pack.Queries) > 0 {
			fmt.Printf("\nQueries:\n")
			for _, query := range pack.Queries {
				fmt.Printf("  • %s (%s) - %s\n", query.ID, query.Severity, query.Title)
			}
		}

		if len(pack.Parameters) > 0 {
			fmt.Printf("\nParameters:\n")
			for _, param := range pack.Parameters {
				required := ""
				if param.Required {
					required = " (required)"
				}
				fmt.Printf("  • %s (%s)%s - %s\n", param.Name, param.Type, required, param.Description)
			}
		}

		if len(pack.DependsOn) > 0 {
			fmt.Printf("\nDependencies:\n")
			for _, dep := range pack.DependsOn {
				fmt.Printf("  • %s\n", dep)
			}
		}
	}

	fmt.Printf("\nMetadata:\n")
	fmt.Printf("  Loaded From: %s\n", pack.LoadedFrom)
	fmt.Printf("  Loaded At: %s\n", pack.LoadedAt.Format(time.RFC3339))
}

// runPackCache manages registry cache
func runPackCache(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "❌ Cache action is required\n")
		fmt.Fprintf(os.Stderr, "💡 Available actions: info, clear, update\n")
		os.Exit(1)
	}

	action := args[0]
	switch action {
	case "info":
		runPackCacheInfo()
	case "clear":
		runPackCacheClear()
	case "update":
		runPackCacheUpdate()
	default:
		fmt.Fprintf(os.Stderr, "❌ Unknown cache action: %s\n", action)
		fmt.Fprintf(os.Stderr, "💡 Available actions: info, clear, update\n")
		os.Exit(1)
	}
}

// runPackCacheInfo shows cache information
func runPackCacheInfo() {
	registryClient := compliance.NewRegistryClient()
	
	info, err := registryClient.GetCacheInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to get cache info: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📋 Registry Cache Information:\n\n")
	for key, value := range info {
		fmt.Printf("  %s: %v\n", key, value)
	}
}

// runPackCacheClear clears the registry cache
func runPackCacheClear() {
	registryClient := compliance.NewRegistryClient()
	
	if err := registryClient.ClearCache(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to clear cache: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Registry cache cleared successfully\n")
}

// runPackCacheUpdate updates the registry cache
func runPackCacheUpdate() {
	registryClient := compliance.NewRegistryClient()
	
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		registryClient = registryClient.WithGitHubToken(token)
	}

	ctx := context.Background()
	
	fmt.Printf("🔄 Updating registry cache...\n")
	
	if err := registryClient.UpdateRegistry(ctx, true); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to update cache: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Registry cache updated successfully\n")
}

// Helper functions

func containsFramework(frameworks []string, filter string) bool {
	for _, framework := range frameworks {
		if strings.Contains(strings.ToLower(framework), filter) {
			return true
		}
	}
	return false
}
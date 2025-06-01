package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jlgore/corkscrew/internal/client"
	"github.com/jlgore/corkscrew/internal/orchestrator"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// runOrchestratorDiscovery demonstrates how to use the orchestrator for comprehensive discovery
func runOrchestratorDiscovery(args []string) error {
	// Parse command-line arguments
	fs := flag.NewFlagSet("orchestrator-discover", flag.ExitOnError)
	provider := fs.String("provider", "aws", "Cloud provider (aws, azure, gcp)")
	forceFlag := fs.Bool("force-refresh", false, "Force refresh of discovery data")
	generateFlag := fs.Bool("generate", false, "Generate scanner code after analysis")
	verbose := fs.Bool("verbose", false, "Verbose output")
	
	fs.Parse(args)
	ctx := context.Background()
	
	// Create orchestrator with memory cache
	cache := orchestrator.NewMemoryCache(100*1024*1024, "LRU") // 100MB LRU cache
	orch := orchestrator.NewOrchestrator(cache)

	// Load AWS provider plugin and register with orchestrator
	if *provider == "aws" {
		if err := registerAWSProvider(ctx, orch, *verbose); err != nil {
			log.Printf("⚠️  Failed to register AWS provider: %v", err)
			log.Printf("📝 Continuing with orchestrator-only discovery...")
		} else if *verbose {
			log.Printf("✅ AWS provider registered with orchestrator")
		}
	}
	
	// Prepare discovery sources
	var sources []orchestrator.DiscoverySource
	
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		githubSource := orchestrator.NewGitHubSource(token)
		sources = append(sources, githubSource)
		log.Printf("✅ GitHub discovery source configured")
	} else {
		log.Printf("⚠️  GITHUB_TOKEN not set - GitHub discovery disabled")
	}
	
	// Add API source for additional discovery
	if os.Getenv("AWS_API_TOKEN") != "" {
		apiSource := orchestrator.NewAPISource("https://api.aws.amazon.com", orchestrator.AuthConfig{
			Type: "bearer",
			Credentials: map[string]string{
				"token": os.Getenv("AWS_API_TOKEN"),
			},
		})
		sources = append(sources, apiSource)
		log.Printf("✅ AWS API discovery source configured")
	}
	
	// Configure discovery options
	options := orchestrator.DiscoveryOptions{
		Sources: sources,
		ForceRefresh:    *forceFlag,
		ConcurrentLimit: 10,
		CacheStrategy: orchestrator.CacheStrategy{
			Enabled:     true,
			TTL:         24 * time.Hour,
			MaxSize:     50 * 1024 * 1024, // 50MB
			EvictPolicy: "LRU",
		},
		Filters: map[string]interface{}{
			"include_services": []string{"ec2", "s3", "lambda", "rds"},
			"exclude_test":     true,
		},
		ProgressHandler: func(phase string, progress float64, message string) {
			fmt.Printf("📊 [%s] %.1f%% - %s\n", phase, progress*100, message)
		},
	}
	
	if *verbose {
		fmt.Printf("🔧 Using orchestrator-based discovery for %s provider\n", *provider)
	}
	
	// Step 1: Discover
	log.Printf("🔍 Starting orchestrator discovery for %s provider...", *provider)
	startTime := time.Now()
	
	discovered, err := orch.Discover(ctx, *provider, options)
	if err != nil {
		return fmt.Errorf("orchestrator discovery failed: %w", err)
	}
	
	discoveryDuration := time.Since(startTime)
	log.Printf("✅ Discovery complete in %v", discoveryDuration)
	
	// Calculate total items from all sources
	totalItems := 0
	for _, sourceData := range discovered.Sources {
		totalItems += getSourceItemCount(sourceData)
	}
	
	log.Printf("📈 Discovered %d items from %d sources", totalItems, len(discovered.Sources))
	
	// Print discovery summary
	fmt.Printf("\n🎯 Discovery Summary:\n")
	fmt.Printf("  Provider: %s\n", discovered.Provider)
	fmt.Printf("  Total Items: %d\n", totalItems)
	fmt.Printf("  Sources Used: %d\n", len(discovered.Sources))
	fmt.Printf("  Discovery Time: %v\n", discoveryDuration)
	
	for sourceName, sourceData := range discovered.Sources {
		fmt.Printf("  📡 Source '%s': %v items\n", sourceName, getSourceItemCount(sourceData))
	}
	
	if len(discovered.Errors) > 0 {
		fmt.Printf("\n⚠️  Discovery Warnings:\n")
		for _, err := range discovered.Errors {
			fmt.Printf("  - %s: %s\n", err.Source, err.Message)
		}
	}
	
	// Step 2: Analyze
	log.Printf("\n🔬 Analyzing discovered data...")
	analysisStart := time.Now()
	
	analysis, err := orch.Analyze(ctx, *provider, discovered)
	if err != nil {
		return fmt.Errorf("orchestrator analysis failed: %w", err)
	}
	
	analysisDuration := time.Since(analysisStart)
	log.Printf("✅ Analysis complete in %v", analysisDuration)
	
	// Print analysis summary
	fmt.Printf("\n📊 Analysis Summary:\n")
	fmt.Printf("  Services: %d\n", len(analysis.Services))
	fmt.Printf("  Resources: %d\n", len(analysis.Resources))
	fmt.Printf("  Operations: %d\n", len(analysis.Operations))
	fmt.Printf("  Relationships: %d\n", len(analysis.Relationships))
	fmt.Printf("  Analysis Time: %v\n", analysisDuration)
	
	if len(analysis.Warnings) > 0 {
		fmt.Printf("\n⚠️  Analysis Warnings:\n")
		for _, warning := range analysis.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}
	
	// Show top services discovered
	fmt.Printf("\n🔧 Top Services Discovered:\n")
	for i, service := range analysis.Services {
		if i >= 5 { // Show first 5
			fmt.Printf("  ... and %d more services\n", len(analysis.Services)-5)
			break
		}
		fmt.Printf("  📦 %s - %s (v%s)\n", 
			service.Name, service.DisplayName, service.Version)
	}
	
	// Show resource types
	fmt.Printf("\n🏗️  Resource Types Found:\n")
	resourcesByService := make(map[string]int)
	for _, resource := range analysis.Resources {
		resourcesByService[resource.Service]++
	}
	
	for service, count := range resourcesByService {
		if count > 0 {
			fmt.Printf("  🔹 %s: %d resource types\n", service, count)
		}
	}
	
	// Step 3: Generate (if requested)
	var generation *orchestrator.GenerationResult
	if *generateFlag {
		log.Printf("\n🏗️  Generating scanner code...")
		generationStart := time.Now()
		
		var err error
		generation, err = orch.Generate(ctx, *provider, analysis)
		if err != nil {
			return fmt.Errorf("orchestrator generation failed: %w", err)
		}
		
		generationDuration := time.Since(generationStart)
		log.Printf("✅ Generation complete in %v", generationDuration)
		
		// Print generation summary
		fmt.Printf("\n📁 Generation Summary:\n")
		fmt.Printf("  Files Generated: %d\n", len(generation.Files))
		fmt.Printf("  Total Services: %d\n", generation.Stats.TotalServices)
		fmt.Printf("  Total Resources: %d\n", generation.Stats.TotalResources)
		fmt.Printf("  Total Operations: %d\n", generation.Stats.TotalOperations)
		fmt.Printf("  Generation Time: %v\n", generationDuration)
		
		if len(generation.Errors) > 0 {
			fmt.Printf("\n❌ Generation Errors:\n")
			for _, err := range generation.Errors {
				fmt.Printf("  - %s\n", err)
			}
		}
		
		// Show generated files
		fmt.Printf("\n📄 Generated Files:\n")
		for i, file := range generation.Files {
			if i >= 10 { // Show first 10
				fmt.Printf("  ... and %d more files\n", len(generation.Files)-10)
				break
			}
			fmt.Printf("  📝 %s (%s)\n", file.Path, file.Service)
		}
	}
	
	// Step 4: Get pipeline status
	status, err := orch.GetPipeline(*provider)
	if err == nil {
		fmt.Printf("\n🚀 Pipeline Status:\n")
		fmt.Printf("  Current Phase: %s\n", status.CurrentPhase)
		fmt.Printf("  Overall Progress: %.1f%%\n", status.Progress*100)
		fmt.Printf("  Started: %v\n", status.StartedAt.Format("2006-01-02 15:04:05"))
		
		if status.CompletedAt != nil {
			totalDuration := status.CompletedAt.Sub(status.StartedAt)
			fmt.Printf("  Completed: %v\n", status.CompletedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Total Duration: %v\n", totalDuration)
		}
		
		// Show phase details
		fmt.Printf("\n📋 Phase Details:\n")
		for phaseName, phaseInfo := range status.PhaseDetails {
			progressStr := fmt.Sprintf("%.1f%%", phaseInfo.Progress*100)
			fmt.Printf("  🔸 %s: %s (%s)\n", phaseName, phaseInfo.Status, progressStr)
			
			if phaseInfo.Error != "" {
				fmt.Printf("    ❌ Error: %s\n", phaseInfo.Error)
			}
		}
		
		if len(status.Errors) > 0 {
			fmt.Printf("\n❌ Pipeline Errors:\n")
			for _, err := range status.Errors {
				fmt.Printf("  - %s\n", err)
			}
		}
	}
	
	// Summary
	totalDuration := time.Since(startTime)
	fmt.Printf("\n🎉 Orchestrator Discovery Complete!\n")
	fmt.Printf("Total Time: %v\n", totalDuration)
	fmt.Printf("Items Discovered: %d\n", totalItems)
	fmt.Printf("Services Analyzed: %d\n", len(analysis.Services))
	if *generateFlag && generation != nil {
		fmt.Printf("Files Generated: %d\n", len(generation.Files))
	}
	
	return nil
}

// getSourceItemCount extracts item count from source data
func getSourceItemCount(sourceData interface{}) int {
	// This would need to be implemented based on the actual source data structure
	// For now, return a placeholder value
	switch data := sourceData.(type) {
	case *orchestrator.GitHubDiscoveryResult:
		return len(data.Files)
	case *orchestrator.APIDiscoveryResult:
		return 1 // API responses typically contain structured data
	default:
		return 1
	}
}


// registerAWSProvider loads the AWS provider plugin and registers it with the orchestrator
func registerAWSProvider(ctx context.Context, orch orchestrator.Orchestrator, verbose bool) error {
	// Create plugin manager
	pluginDir := "./plugins"
	if envDir := os.Getenv("CORKSCREW_PLUGIN_DIR"); envDir != "" {
		pluginDir = envDir
	}
	
	pluginManager := client.NewPluginManager(pluginDir)
	if verbose {
		pluginManager.SetDebug(true)
	}

	// Load AWS provider plugin
	awsProvider, err := pluginManager.LoadProvider("aws")
	if err != nil {
		return fmt.Errorf("failed to load AWS provider plugin: %w", err)
	}

	// Initialize the provider
	config := map[string]string{
		"debug": fmt.Sprintf("%t", verbose),
	}
	
	cacheDir := filepath.Join(os.TempDir(), "corkscrew-cache")
	err = pluginManager.InitializeProvider(ctx, "aws", config, cacheDir)
	if err != nil {
		return fmt.Errorf("failed to initialize AWS provider: %w", err)
	}

	// Extract the underlying gRPC client
	grpcClientProvider, ok := awsProvider.(interface{ GetUnderlyingClient() pb.CloudProviderClient })
	if !ok {
		return fmt.Errorf("AWS provider does not support gRPC client extraction")
	}
	grpcClient := grpcClientProvider.GetUnderlyingClient()

	// Create a provider registry and register the AWS provider
	registry := orchestrator.NewProviderOrchestratorRegistry()
	err = registry.RegisterProviderClient("aws", grpcClient)
	if err != nil {
		return fmt.Errorf("failed to register AWS provider with registry: %w", err)
	}

	// Register the adapters with the orchestrator
	err = registry.RegisterWithOrchestrator(orch)
	if err != nil {
		return fmt.Errorf("failed to register adapters with orchestrator: %w", err)
	}

	if verbose {
		log.Printf("🔌 AWS provider plugin loaded from: %s", pluginDir)
		log.Printf("🔧 AWS provider initialized with cache dir: %s", cacheDir)
	}

	return nil
}
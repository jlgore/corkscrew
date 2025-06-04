package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	awsprovider "github.com/jlgore/corkscrew/plugins/aws-provider"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
)

func main() {
	var (
		registryPath = flag.String("registry", "./registry/discovered_services.json", "Path to registry file")
		outputDir    = flag.String("output", "./generated", "Output directory for generated code")
		region       = flag.String("region", "", "AWS region (uses default if not specified)")
		profile      = flag.String("profile", "", "AWS profile to use")
		timeout      = flag.Duration("timeout", 10*time.Minute, "Discovery timeout")
		verbose      = flag.Bool("verbose", false, "Enable verbose output")
		
		// Cache options
		forceRefresh = flag.Bool("force-refresh", false, "Force fresh discovery, ignoring cache")
		cacheDir     = flag.String("cache-dir", "./registry/cache", "Cache directory")
		cacheTTL     = flag.Duration("cache-ttl", 24*time.Hour, "Cache TTL")
		showCache    = flag.Bool("show-cache", false, "Show cache statistics and exit")
		invalidateCache = flag.Bool("invalidate-cache", false, "Invalidate cache and exit")
	)
	flag.Parse()

	// Configure logging
	if !*verbose {
		log.SetOutput(os.Stderr)
	}

	// Load AWS configuration
	ctx := context.Background()
	cfgOpts := []func(*config.LoadOptions) error{}
	
	if *region != "" {
		cfgOpts = append(cfgOpts, config.WithRegion(*region))
	}
	
	if *profile != "" {
		cfgOpts = append(cfgOpts, config.WithSharedConfigProfile(*profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, cfgOpts...)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	// Create cache configuration
	cacheConfig := discovery.CacheConfig{
		CacheDir:        *cacheDir,
		TTL:             *cacheTTL,
		MaxCacheSize:    50 * 1024 * 1024, // 50MB
		EnableChecksum:  true,
		AutoRefresh:     false,
		RefreshInterval: 12 * time.Hour,
	}

	// Initialize registry with custom path
	registryConfig := registry.RegistryConfig{
		PersistencePath:     *registryPath,
		AutoPersist:         true,
		PersistenceInterval: 5 * time.Minute,
		EnableCache:         true,
		CacheTTL:            10 * time.Minute,
		MaxCacheSize:        1000,
		EnableDiscovery:     true,
		UseFallbackServices: true,
		EnableAuditLog:      true,
	}

	// Initialize the registry
	serviceRegistry := registry.NewServiceRegistry(registryConfig)
	
	// Create discovery instance with caching
	discoveryInstance := discovery.NewRegistryDiscoveryWithCacheConfig(cfg, cacheConfig).WithRegistry(serviceRegistry)

	// Set environment variable for registry path
	os.Setenv("CORKSCREW_REGISTRY_PATH", *registryPath)

	fmt.Println("AWS Service Discovery and Registration Tool")
	fmt.Println("==========================================")
	fmt.Printf("Registry Path: %s\n", *registryPath)
	fmt.Printf("Cache Directory: %s\n", *cacheDir)
	fmt.Printf("Cache TTL: %v\n", *cacheTTL)
	fmt.Printf("Output Directory: %s\n", *outputDir)
	fmt.Printf("Region: %s\n", cfg.Region)
	fmt.Printf("Timeout: %v\n", *timeout)
	fmt.Println()

	// Handle cache-only operations
	if *showCache {
		fmt.Println("📊 Cache Statistics:")
		printCacheStats(discoveryInstance)
		return
	}

	if *invalidateCache {
		fmt.Println("🗑️  Invalidating cache...")
		if err := discoveryInstance.InvalidateCache(); err != nil {
			log.Fatalf("Failed to invalidate cache: %v", err)
		}
		fmt.Println("✅ Cache invalidated successfully")
		return
	}

	// Try to load cache on startup
	fmt.Println("🔍 Checking for cached discovery results...")
	if cached, err := discoveryInstance.LoadCacheOnStartup(); err != nil {
		if *verbose {
			fmt.Printf("No valid cache found: %v\n", err)
		}
	} else if cached {
		fmt.Println("📦 Found valid cache, will use if fresh")
	}

	// Show initial cache stats
	if *verbose {
		fmt.Println("\n📊 Initial Cache Status:")
		printCacheStats(discoveryInstance)
		fmt.Println()
	}

	// Run discovery with timeout
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	fmt.Println("Starting service discovery...")
	startTime := time.Now()

	var err error
	if *forceRefresh {
		fmt.Println("🔄 Force refresh requested, ignoring cache...")
		_, err = discoveryInstance.ForceRefresh(ctx)
		if err == nil {
			err = discoveryInstance.DiscoverAndRegister(ctx)
		}
	} else {
		err = discoveryInstance.DiscoverAndRegister(ctx)
	}

	if err != nil {
		fmt.Printf("⚠️  Discovery completed with warnings: %v\n", err)
	} else {
		fmt.Println("✅ Discovery completed successfully!")
	}

	duration := time.Since(startTime)
	fmt.Printf("\nDiscovery took: %v\n", duration)

	// Display statistics
	stats := serviceRegistry.GetStats()
	fmt.Printf("\nDiscovery Results:\n")
	fmt.Printf("- Total Services Discovered: %d\n", stats.TotalServices)
	fmt.Printf("- Total Resource Types: %d\n", stats.TotalResourceTypes)
	fmt.Printf("- Total Operations: %d\n", stats.TotalOperations)
	fmt.Printf("- Success Count: %d\n", stats.DiscoverySuccess)
	fmt.Printf("- Failure Count: %d\n", stats.DiscoveryFailures)

	// List services by source
	fmt.Printf("\nServices by Source:\n")
	for source, count := range stats.ServicesBySource {
		fmt.Printf("- %s: %d\n", source, count)
	}

	// Show sample of discovered services
	services := serviceRegistry.ListServices()
	fmt.Printf("\nSample of Discovered Services (%d total):\n", len(services))
	for i, serviceName := range services {
		if i >= 20 {
			fmt.Printf("... and %d more\n", len(services)-20)
			break
		}
		
		if svc, ok := serviceRegistry.GetService(serviceName); ok {
			fmt.Printf("- %-20s %s (%d resources)\n", 
				svc.Name, svc.DisplayName, len(svc.ResourceTypes))
		}
	}

	// Validate registry
	fmt.Printf("\nValidating registry...\n")
	if errors := serviceRegistry.ValidateRegistry(); len(errors) > 0 {
		fmt.Printf("⚠️  Found %d validation errors:\n", len(errors))
		for i, err := range errors {
			if i >= 5 {
				fmt.Printf("... and %d more\n", len(errors)-5)
				break
			}
			fmt.Printf("  - %v\n", err)
		}
	} else {
		fmt.Println("✅ Registry validation passed!")
	}

	// Save final state
	fmt.Printf("\nPersisting registry to %s...\n", *registryPath)
	if err := serviceRegistry.PersistToFile(*registryPath); err != nil {
		log.Fatalf("Failed to persist registry: %v", err)
	}
	fmt.Println("✅ Registry saved successfully!")

	// Generate code if requested
	if *outputDir != "" {
		fmt.Printf("\nGenerating client factory in %s...\n", *outputDir)
		if err := os.MkdirAll(*outputDir, 0755); err != nil {
			log.Fatalf("Failed to create output directory: %v", err)
		}

		// The discovery already triggered generation, but we can check the results
		factoryPath := fmt.Sprintf("%s/client_factory.go", *outputDir)
		if _, err := os.Stat(factoryPath); err == nil {
			fmt.Printf("✅ Client factory generated at: %s\n", factoryPath)
		} else {
			fmt.Printf("⚠️  Client factory not found at: %s\n", factoryPath)
		}

		wrapperPath := fmt.Sprintf("%s/client_factory_dynamic.go", *outputDir)
		if _, err := os.Stat(wrapperPath); err == nil {
			fmt.Printf("✅ Dynamic wrapper generated at: %s\n", wrapperPath)
		} else {
			fmt.Printf("⚠️  Dynamic wrapper not found at: %s\n", wrapperPath)
		}
	}

	// Show final cache stats
	if *verbose {
		fmt.Println("\n📊 Final Cache Status:")
		printCacheStats(discoveryInstance)
	}

	// Cleanup old cache files
	fmt.Println("\n🧹 Cleaning up old cache files...")
	if err := discoveryInstance.CleanupOldCaches(); err != nil {
		fmt.Printf("⚠️  Cache cleanup warning: %v\n", err)
	} else {
		fmt.Println("✅ Cache cleanup completed")
	}

	fmt.Println("\n✨ Discovery and registration complete!")
}

// printCacheStats displays cache statistics in a readable format
func printCacheStats(discovery *discovery.RegistryDiscovery) {
	stats := discovery.GetCacheStats()

	for key, value := range stats {
		switch key {
		case "cache_exists":
			if value.(bool) {
				fmt.Printf("  ✅ Cache exists\n")
			} else {
				fmt.Printf("  ❌ No cache\n")
			}
		case "is_fresh":
			if value.(bool) {
				fmt.Printf("  🟢 Cache is fresh\n")
			} else {
				fmt.Printf("  🟡 Cache is stale\n")
			}
		case "service_count":
			fmt.Printf("  📊 Services: %v\n", value)
		case "age_hours":
			if hours, ok := value.(float64); ok && hours >= 0 {
				fmt.Printf("  ⏰ Age: %.1f hours\n", hours)
			}
		case "remaining_hours":
			if hours, ok := value.(float64); ok && hours >= 0 {
				fmt.Printf("  ⏳ Remaining: %.1f hours\n", hours)
			}
		case "manual_overrides_count":
			if count := value.(int); count > 0 {
				fmt.Printf("  🔧 Manual overrides: %d\n", count)
			}
		case "source":
			fmt.Printf("  📡 Source: %v\n", value)
		case "cached_at":
			if t, ok := value.(time.Time); ok {
				fmt.Printf("  📅 Cached: %v\n", t.Format("2006-01-02 15:04:05"))
			}
		case "expires_at":
			if t, ok := value.(time.Time); ok {
				fmt.Printf("  ⏰ Expires: %v\n", t.Format("2006-01-02 15:04:05"))
			}
		}
	}
}
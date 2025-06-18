package smartscan

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jlgore/corkscrew/internal/client"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

type EnhancedScanOptions struct {
	Provider       string
	Regions        []string
	Services       []string
	OutputFormat   string
	SaveToFile     bool
	ShowEmpty      bool
	ConfigPath     string
	MaxConcurrency int
}

func RunEnhancedScan(ctx context.Context, options EnhancedScanOptions) error {
	// Load configuration
	config, err := LoadSmartScanConfig(options.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate provider
	if err := config.ValidateProvider(options.Provider); err != nil {
		return err
	}

	if !config.IsProviderEnabled(options.Provider) {
		return fmt.Errorf("provider %s is disabled in configuration", options.Provider)
	}

	// Get regions from config if not specified in command line
	regions := options.Regions
	if len(regions) == 0 {
		regions, err = config.GetRegionsForProvider(options.Provider)
		if err != nil {
			return fmt.Errorf("failed to get regions from config: %w", err)
		}
	}

	// Get services from config if not specified in command line
	services := options.Services
	if len(services) == 0 {
		services, err = config.GetServicesForProvider(options.Provider)
		if err != nil {
			return fmt.Errorf("failed to get services from config: %w", err)
		}
	}

	// Initialize plugin client
	pc, err := client.NewPluginClient(options.Provider)
	if err != nil {
		return fmt.Errorf("failed to initialize plugin client: %w", err)
	}
	defer pc.Close()

	provider, err := pc.GetProvider()
	if err != nil {
		return fmt.Errorf("failed to get provider: %w", err)
	}

	// Initialize provider
	initReq := &pb.InitializeRequest{
		Config: map[string]string{
			"region": func() string {
				if len(regions) > 0 && regions[0] != "all" {
					return regions[0]
				}
				return ""
			}(),
		},
	}
	_, err = provider.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Create smart scan config
	smartConfig := config.GetSmartScanConfig(options.Provider)
	if options.MaxConcurrency > 0 {
		smartConfig.MaxConcurrency = options.MaxConcurrency
	}

	// Override hiding empty regions/services if explicitly requested
	if options.ShowEmpty {
		smartConfig.HideEmptyRegions = false
		smartConfig.HideEmptyServices = false
	}

	// Create multi-region scanner
	scanner := NewMultiRegionScanner(provider, options.Provider, smartConfig)

	// Print scan information
	fmt.Printf("🔍 Enhanced scan starting:\n")
	fmt.Printf("   Provider: %s\n", options.Provider)
	if len(regions) == 1 && regions[0] == "all" {
		fmt.Printf("   Regions: all available regions\n")
	} else {
		fmt.Printf("   Regions: %s (%d)\n", strings.Join(regions, ", "), len(regions))
	}
	if len(services) > 0 {
		fmt.Printf("   Services: %s (%d)\n", strings.Join(services, ", "), len(services))
	} else {
		fmt.Printf("   Services: all configured services\n")
	}
	fmt.Printf("   Concurrency: %d regions\n", smartConfig.MaxConcurrency)
	fmt.Printf("   Empty filtering: regions=%t, services=%t\n", 
		smartConfig.HideEmptyRegions, smartConfig.HideEmptyServices)

	// Execute multi-region scan
	results, err := scanner.ScanMultipleRegions(ctx, regions, services)
	if err != nil {
		return fmt.Errorf("multi-region scan failed: %w", err)
	}

	// Apply service filtering
	scanner.FilterEmptyServices(results)

	// Print results based on output format
	switch options.OutputFormat {
	case "json":
		return printJSONResults(results)
	case "csv":
		return printCSVResults(results)
	default:
		return printTableResults(results, smartConfig)
	}
}

func printTableResults(results *AggregatedResults, config *SmartScanConfig) error {
	// Print summary
	fmt.Printf("\n✅ Enhanced scan completed!\n")
	
	if results.Summary != nil {
		fmt.Printf("📊 Summary:\n")
		fmt.Printf("   Total resources: %d\n", results.Summary.TotalResources)
		fmt.Printf("   Active regions: %d/%d\n", results.Summary.ActiveRegions, results.Summary.TotalRegions)
		fmt.Printf("   Duration: %s\n", results.Summary.TotalDuration.Round(time.Millisecond))

		if len(results.Summary.EmptyRegions) > 0 && !config.HideEmptyRegions {
			fmt.Printf("   Empty regions: %s\n", strings.Join(results.Summary.EmptyRegions, ", "))
		}
	}

	// Print region breakdown
	if len(results.RegionResults) > 0 {
		fmt.Printf("\n📍 Region breakdown:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "Region\tResources\tDuration\tStatus")
		fmt.Fprintln(w, "------\t---------\t--------\t------")

		// Sort regions by resource count (descending)
		type regionStat struct {
			name      string
			count     int
			duration  time.Duration
			hasErrors bool
		}

		var stats []regionStat
		for region, result := range results.RegionResults {
			stats = append(stats, regionStat{
				name:      region,
				count:     len(result.Resources),
				duration:  result.Duration,
				hasErrors: len(result.Errors) > 0,
			})
		}

		sort.Slice(stats, func(i, j int) bool {
			return stats[i].count > stats[j].count
		})

		for _, stat := range stats {
			status := "✅"
			if stat.hasErrors {
				status = "⚠️"
			}
			if stat.count == 0 {
				status = "📭"
			}

			fmt.Fprintf(w, "%s\t%d\t%s\t%s\n",
				stat.name,
				stat.count,
				stat.duration.Round(time.Millisecond),
				status)
		}
		w.Flush()
	}

	// Print service breakdown
	if results.Summary != nil && len(results.Summary.ServiceCounts) > 0 {
		fmt.Printf("\n📋 Service breakdown:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "Service\tResource Count\tActive Regions")
		fmt.Fprintln(w, "-------\t--------------\t--------------")

		// Sort services by resource count (descending)
		type serviceStat struct {
			name           string
			count          int32
			activeRegions  int
		}

		var serviceStats []serviceStat
		for service, count := range results.Summary.ServiceCounts {
			if count == 0 && config.HideEmptyServices {
				continue
			}

			// Count regions where this service has resources
			activeRegions := 0
			for _, regionResult := range results.RegionResults {
				if regionResult.Stats != nil {
					if regionCount, exists := regionResult.Stats.ServiceCounts[service]; exists && regionCount > 0 {
						activeRegions++
					}
				}
			}

			serviceStats = append(serviceStats, serviceStat{
				name:          service,
				count:         count,
				activeRegions: activeRegions,
			})
		}

		sort.Slice(serviceStats, func(i, j int) bool {
			return serviceStats[i].count > serviceStats[j].count
		})

		for _, stat := range serviceStats {
			fmt.Fprintf(w, "%s\t%d\t%d\n",
				stat.name,
				stat.count,
				stat.activeRegions)
		}
		w.Flush()
	}

	// Print errors if any
	if len(results.Errors) > 0 {
		fmt.Printf("\n⚠️  Issues encountered:\n")
		for _, err := range results.Errors {
			fmt.Printf("   - %s\n", err)
		}
	}

	return nil
}

func printJSONResults(results *AggregatedResults) error {
	output := struct {
		Summary       *ScanSummary                     `json:"summary"`
		RegionResults map[string]*RegionScanResult     `json:"region_results"`
		AllResources  []*pb.Resource                   `json:"all_resources"`
		Errors        []string                         `json:"errors"`
	}{
		Summary:       results.Summary,
		RegionResults: results.RegionResults,
		AllResources:  results.AllResources,
		Errors:        results.Errors,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func printCSVResults(results *AggregatedResults) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	// Write header
	w.Write([]string{"Region", "Service", "ResourceType", "ResourceID", "ResourceName", "ARN"})

	// Write data
	for _, resource := range results.AllResources {
		w.Write([]string{
			resource.Region,
			resource.Service,
			resource.Type,
			resource.Id,
			resource.Name,
			resource.Arn,
		})
	}

	return nil
}

func SaveResultsToFile(results *AggregatedResults, filename string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func GenerateTimestampedFilename(provider string) string {
	timestamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("enhanced-scan-%s-%s.json", provider, timestamp)
}
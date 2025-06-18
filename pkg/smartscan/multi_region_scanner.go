package smartscan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

type MultiRegionScanner struct {
	provider     shared.CloudProvider
	providerName string
	config       *SmartScanConfig
}

type SmartScanConfig struct {
	HideEmptyRegions   bool
	HideEmptyServices  bool
	MaxConcurrency     int
	RegionTimeout      time.Duration
	PreferredRegions   []string
}

type RegionScanResult struct {
	Region    string
	Resources []*pb.Resource
	Stats     *pb.ScanStats
	Errors    []string
	Duration  time.Duration
}

type AggregatedResults struct {
	AllResources   []*pb.Resource
	RegionResults  map[string]*RegionScanResult
	TotalStats     *pb.ScanStats
	Summary        *ScanSummary
	Errors         []string
}

type ScanSummary struct {
	TotalRegions     int
	ActiveRegions    int
	EmptyRegions     []string
	ServiceCounts    map[string]int32
	RegionCounts     map[string]int32
	TotalResources   int32
	TotalDuration    time.Duration
}

func NewMultiRegionScanner(provider shared.CloudProvider, providerName string, config *SmartScanConfig) *MultiRegionScanner {
	if config == nil {
		config = &SmartScanConfig{
			HideEmptyRegions:  true,
			HideEmptyServices: true,
			MaxConcurrency:    3,
			RegionTimeout:     5 * time.Minute,
		}
	}
	
	return &MultiRegionScanner{
		provider:     provider,
		providerName: providerName,
		config:       config,
	}
}

func (mrs *MultiRegionScanner) ScanMultipleRegions(ctx context.Context, regions []string, services []string) (*AggregatedResults, error) {
	if len(regions) == 0 {
		return nil, fmt.Errorf("no regions specified")
	}

	// Handle "all" regions
	if len(regions) == 1 && regions[0] == "all" {
		discoveredRegions, err := mrs.discoverAllRegions(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to discover all regions: %w", err)
		}
		regions = discoveredRegions
	}

	// Sort regions by priority (preferred regions first)
	regions = mrs.prioritizeRegions(regions)

	start := time.Now()
	results := &AggregatedResults{
		AllResources:  make([]*pb.Resource, 0),
		RegionResults: make(map[string]*RegionScanResult),
		Errors:        make([]string, 0),
	}

	// Scan regions concurrently with limited concurrency
	semaphore := make(chan struct{}, mrs.config.MaxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, region := range regions {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release

			regionResult := mrs.scanSingleRegion(ctx, r, services)
			
			mu.Lock()
			results.RegionResults[r] = regionResult
			results.AllResources = append(results.AllResources, regionResult.Resources...)
			results.Errors = append(results.Errors, regionResult.Errors...)
			mu.Unlock()
		}(region)
	}

	wg.Wait()

	// Generate aggregated stats and summary
	results.TotalStats = mrs.aggregateStats(results.RegionResults)
	results.Summary = mrs.generateSummary(results.RegionResults, time.Since(start))

	// Apply filtering
	if mrs.config.HideEmptyRegions {
		mrs.filterEmptyRegions(results)
	}

	return results, nil
}

func (mrs *MultiRegionScanner) scanSingleRegion(ctx context.Context, region string, services []string) *RegionScanResult {
	start := time.Now()
	
	// Create region-specific timeout context
	regionCtx, cancel := context.WithTimeout(ctx, mrs.config.RegionTimeout)
	defer cancel()

	result := &RegionScanResult{
		Region:    region,
		Resources: make([]*pb.Resource, 0),
		Errors:    make([]string, 0),
	}

	// Create BatchScan request for this region
	req := &pb.BatchScanRequest{
		Services:             services,
		Region:               region,
		IncludeRelationships: false, // Keep it fast for smart scanning
		Concurrency:          int32(mrs.config.MaxConcurrency),
	}

	// Execute scan
	resp, err := mrs.provider.BatchScan(regionCtx, req)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("region %s: %v", region, err))
		result.Duration = time.Since(start)
		return result
	}

	// Process successful response
	result.Resources = resp.Resources
	result.Stats = resp.Stats
	result.Errors = append(result.Errors, resp.Errors...)
	result.Duration = time.Since(start)

	return result
}

func (mrs *MultiRegionScanner) discoverAllRegions(ctx context.Context) ([]string, error) {
	// Use provider-specific region discovery
	switch mrs.providerName {
	case "aws":
		return mrs.discoverAWSRegions(ctx)
	case "azure":
		return mrs.discoverAzureLocations(ctx)
	case "gcp":
		return mrs.discoverGCPRegions(ctx)
	case "kubernetes":
		return mrs.discoverKubernetesClusters(ctx)
	default:
		return []string{}, fmt.Errorf("region discovery not supported for provider: %s", mrs.providerName)
	}
}

func (mrs *MultiRegionScanner) discoverAWSRegions(ctx context.Context) ([]string, error) {
	// Try to discover services first to get region info
	req := &pb.DiscoverServicesRequest{ForceRefresh: false}
	_, err := mrs.provider.DiscoverServices(ctx, req)
	if err != nil {
		// Fallback to common AWS regions
		return []string{
			"us-east-1", "us-east-2", "us-west-1", "us-west-2",
			"eu-west-1", "eu-west-2", "eu-central-1",
			"ap-southeast-1", "ap-southeast-2", "ap-northeast-1",
		}, nil
	}

	// Note: ServiceInfo doesn't have Metadata field in current proto
	// For now, just return common AWS regions
	// This could be enhanced to parse region info from service discovery

	// Final fallback
	return []string{"us-east-1", "us-west-2", "eu-west-1"}, nil
}

func (mrs *MultiRegionScanner) discoverAzureLocations(ctx context.Context) ([]string, error) {
	// Common Azure locations
	return []string{
		"eastus", "eastus2", "westus", "westus2", "westus3",
		"centralus", "northcentralus", "southcentralus",
		"westeurope", "northeurope", "uksouth", "ukwest",
		"eastasia", "southeastasia", "japaneast", "japanwest",
	}, nil
}

func (mrs *MultiRegionScanner) discoverGCPRegions(ctx context.Context) ([]string, error) {
	// Common GCP regions
	return []string{
		"us-central1", "us-east1", "us-east4", "us-west1", "us-west2", "us-west3", "us-west4",
		"europe-west1", "europe-west2", "europe-west3", "europe-west4", "europe-west6",
		"asia-east1", "asia-east2", "asia-northeast1", "asia-south1", "asia-southeast1",
	}, nil
}

func (mrs *MultiRegionScanner) discoverKubernetesClusters(ctx context.Context) ([]string, error) {
	// For Kubernetes, "regions" are actually cluster contexts
	// This would need to read from kubeconfig
	return []string{"default"}, nil
}

func (mrs *MultiRegionScanner) prioritizeRegions(regions []string) []string {
	if len(mrs.config.PreferredRegions) == 0 {
		return regions
	}

	// Put preferred regions first
	prioritized := make([]string, 0, len(regions))
	regionSet := make(map[string]bool)
	for _, r := range regions {
		regionSet[r] = true
	}

	// Add preferred regions first (if they exist in the list)
	for _, preferred := range mrs.config.PreferredRegions {
		if regionSet[preferred] {
			prioritized = append(prioritized, preferred)
			delete(regionSet, preferred)
		}
	}

	// Add remaining regions
	for region := range regionSet {
		prioritized = append(prioritized, region)
	}

	return prioritized
}

func (mrs *MultiRegionScanner) aggregateStats(regionResults map[string]*RegionScanResult) *pb.ScanStats {
	totalStats := &pb.ScanStats{
		ServiceCounts: make(map[string]int32),
	}

	for _, result := range regionResults {
		if result.Stats != nil {
			totalStats.TotalResources += result.Stats.TotalResources
			totalStats.DurationMs += result.Stats.DurationMs

			// Aggregate service counts
			for service, count := range result.Stats.ServiceCounts {
				totalStats.ServiceCounts[service] += count
			}
		}
	}

	return totalStats
}

func (mrs *MultiRegionScanner) generateSummary(regionResults map[string]*RegionScanResult, totalDuration time.Duration) *ScanSummary {
	summary := &ScanSummary{
		TotalRegions:  len(regionResults),
		ServiceCounts: make(map[string]int32),
		RegionCounts:  make(map[string]int32),
		EmptyRegions:  make([]string, 0),
		TotalDuration: totalDuration,
	}

	for region, result := range regionResults {
		resourceCount := int32(len(result.Resources))
		summary.RegionCounts[region] = resourceCount
		summary.TotalResources += resourceCount

		if resourceCount > 0 {
			summary.ActiveRegions++
		} else {
			summary.EmptyRegions = append(summary.EmptyRegions, region)
		}

		// Aggregate service counts across regions
		if result.Stats != nil {
			for service, count := range result.Stats.ServiceCounts {
				summary.ServiceCounts[service] += count
			}
		}
	}

	return summary
}

func (mrs *MultiRegionScanner) filterEmptyRegions(results *AggregatedResults) {
	if !mrs.config.HideEmptyRegions {
		return
	}

	// Remove empty regions from results
	for region, result := range results.RegionResults {
		if len(result.Resources) == 0 {
			delete(results.RegionResults, region)
		}
	}

	// Update summary
	if results.Summary != nil {
		results.Summary.ActiveRegions = len(results.RegionResults)
	}
}

func (mrs *MultiRegionScanner) FilterEmptyServices(results *AggregatedResults) {
	if !mrs.config.HideEmptyServices {
		return
	}

	// Remove services with 0 resources from stats
	if results.TotalStats != nil && results.TotalStats.ServiceCounts != nil {
		for service, count := range results.TotalStats.ServiceCounts {
			if count == 0 {
				delete(results.TotalStats.ServiceCounts, service)
			}
		}
	}

	if results.Summary != nil && results.Summary.ServiceCounts != nil {
		for service, count := range results.Summary.ServiceCounts {
			if count == 0 {
				delete(results.Summary.ServiceCounts, service)
			}
		}
	}
}

func (mrs *MultiRegionScanner) GetNonEmptyRegions(results *AggregatedResults) []string {
	nonEmpty := make([]string, 0)
	for region, result := range results.RegionResults {
		if len(result.Resources) > 0 {
			nonEmpty = append(nonEmpty, region)
		}
	}
	return nonEmpty
}

func (mrs *MultiRegionScanner) PrintSummary(results *AggregatedResults) {
	if results.Summary == nil {
		return
	}

	fmt.Printf("\n📊 Multi-Region Scan Summary:\n")
	fmt.Printf("   Total regions: %d\n", results.Summary.TotalRegions)
	fmt.Printf("   Active regions: %d\n", results.Summary.ActiveRegions)
	fmt.Printf("   Total resources: %d\n", results.Summary.TotalResources)
	fmt.Printf("   Duration: %s\n", results.Summary.TotalDuration.Round(time.Millisecond))

	if len(results.Summary.EmptyRegions) > 0 && !mrs.config.HideEmptyRegions {
		fmt.Printf("   Empty regions: %s\n", strings.Join(results.Summary.EmptyRegions, ", "))
	}

	// Show top regions by resource count
	if len(results.Summary.RegionCounts) > 0 {
		fmt.Printf("\n📍 Top regions by resource count:\n")
		// Sort and show top 5
		type regionCount struct {
			region string
			count  int32
		}
		
		var topRegions []regionCount
		for region, count := range results.Summary.RegionCounts {
			if count > 0 {
				topRegions = append(topRegions, regionCount{region, count})
			}
		}

		// Simple sort (top 5)
		for i := 0; i < len(topRegions) && i < 5; i++ {
			maxIdx := i
			for j := i + 1; j < len(topRegions); j++ {
				if topRegions[j].count > topRegions[maxIdx].count {
					maxIdx = j
				}
			}
			if maxIdx != i {
				topRegions[i], topRegions[maxIdx] = topRegions[maxIdx], topRegions[i]
			}
			fmt.Printf("   %s: %d resources\n", topRegions[i].region, topRegions[i].count)
		}
	}
}
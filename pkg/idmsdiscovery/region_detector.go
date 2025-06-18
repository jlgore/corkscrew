package discovery

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
)

type RegionDetector struct {
	config          SmartDiscoveryConfig
	regionCache     map[string][]models.Region
	performanceData map[string]RegionPerformance
	mutex           sync.RWMutex
}

type RegionPerformance struct {
	Latency         time.Duration
	ResourceCount   int
	ServiceCount    int
	LastUpdated     time.Time
	ErrorRate       float64
}

type RegionPriority struct {
	Region models.Region
	Score  float64
	Reason string
}

func NewRegionDetector(config SmartDiscoveryConfig) *RegionDetector {
	return &RegionDetector{
		config:          config,
		regionCache:     make(map[string][]models.Region),
		performanceData: make(map[string]RegionPerformance),
	}
}

func (rd *RegionDetector) DetectOptimalRegions(ctx context.Context, provider CloudProvider) ([]models.Region, error) {
	providerName := provider.GetName()

	rd.mutex.RLock()
	cached, exists := rd.regionCache[providerName]
	rd.mutex.RUnlock()

	if exists && time.Since(rd.getCacheTime(providerName)) < rd.config.CacheExpiration {
		return rd.prioritizeRegions(cached), nil
	}

	regions, err := provider.GetRegions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get regions: %w", err)
	}

	regions = rd.analyzeRegionPerformance(ctx, provider, regions)

	rd.mutex.Lock()
	rd.regionCache[providerName] = regions
	rd.mutex.Unlock()

	return rd.prioritizeRegions(regions), nil
}

func (rd *RegionDetector) analyzeRegionPerformance(ctx context.Context, provider CloudProvider, regions []models.Region) []models.Region {
	var wg sync.WaitGroup
	performanceChan := make(chan RegionPerformanceData, len(regions))

	for _, region := range regions {
		wg.Add(1)
		go func(r models.Region) {
			defer wg.Done()
			perf := rd.measureRegionPerformance(ctx, provider, r)
			performanceChan <- RegionPerformanceData{Region: r, Performance: perf}
		}(region)
	}

	go func() {
		wg.Wait()
		close(performanceChan)
	}()

	rd.mutex.Lock()
	for perfData := range performanceChan {
		rd.performanceData[rd.getRegionKey(provider.GetName(), perfData.Region.Name)] = perfData.Performance
	}
	rd.mutex.Unlock()

	return regions
}

type RegionPerformanceData struct {
	Region      models.Region
	Performance RegionPerformance
}

func (rd *RegionDetector) measureRegionPerformance(ctx context.Context, provider CloudProvider, region models.Region) RegionPerformance {
	start := time.Now()
	
	services, err := provider.GetServices(ctx, region.Name)
	latency := time.Since(start)
	
	errorRate := 0.0
	if err != nil {
		errorRate = 1.0
		services = []models.Service{}
	}

	return RegionPerformance{
		Latency:       latency,
		ResourceCount: 0, // Will be updated during resource discovery
		ServiceCount:  len(services),
		LastUpdated:   time.Now(),
		ErrorRate:     errorRate,
	}
}

func (rd *RegionDetector) prioritizeRegions(regions []models.Region) []models.Region {
	priorities := make([]RegionPriority, len(regions))
	
	for i, region := range regions {
		priorities[i] = RegionPriority{
			Region: region,
			Score:  rd.calculateRegionScore(region),
			Reason: rd.getScoreReason(region),
		}
	}

	sort.Slice(priorities, func(i, j int) bool {
		return priorities[i].Score > priorities[j].Score
	})

	optimizedRegions := make([]models.Region, len(priorities))
	for i, priority := range priorities {
		region := priority.Region
		region.Metadata = map[string]interface{}{
			"priority_score": priority.Score,
			"priority_reason": priority.Reason,
		}
		optimizedRegions[i] = region
	}

	return optimizedRegions
}

func (rd *RegionDetector) calculateRegionScore(region models.Region) float64 {
	rd.mutex.RLock()
	defer rd.mutex.RUnlock()

	key := rd.getRegionKey("", region.Name) // Provider name not needed for score calculation
	perf, exists := rd.performanceData[key]
	if !exists {
		return 0.5 // Default moderate score
	}

	score := 1.0

	// Penalize for high latency
	if perf.Latency > time.Second {
		score -= 0.3
	} else if perf.Latency > 500*time.Millisecond {
		score -= 0.1
	}

	// Penalize for errors
	score -= perf.ErrorRate * 0.5

	// Boost for high service count (indicates active region)
	if perf.ServiceCount > 50 {
		score += 0.2
	} else if perf.ServiceCount > 20 {
		score += 0.1
	}

	// Regional preferences based on common patterns
	score += rd.getRegionalPreferenceScore(region)

	return max(0, min(1, score))
}

func (rd *RegionDetector) getRegionalPreferenceScore(region models.Region) float64 {
	// Boost commonly used regions
	commonRegions := map[string]float64{
		"us-east-1":      0.2, // AWS primary
		"us-west-2":      0.15,
		"eu-west-1":      0.15,
		"eastus":         0.2, // Azure primary
		"westus2":        0.15,
		"westeurope":     0.15,
		"us-central1":    0.2, // GCP primary
		"us-west1":       0.15,
		"europe-west1":   0.15,
	}
	
	if boost, exists := commonRegions[region.Name]; exists {
		return boost
	}
	
	return 0
}

func (rd *RegionDetector) getScoreReason(region models.Region) string {
	score := rd.calculateRegionScore(region)
	
	if score > 0.8 {
		return "High priority: Low latency, active region"
	} else if score > 0.6 {
		return "Medium priority: Moderate performance"
	} else if score > 0.4 {
		return "Low priority: High latency or errors"
	} else {
		return "Very low priority: Poor performance"
	}
}

func (rd *RegionDetector) getRegionKey(provider, region string) string {
	return fmt.Sprintf("%s:%s", provider, region)
}

func (rd *RegionDetector) getCacheTime(provider string) time.Time {
	// This would typically be stored with the cache
	return time.Now().Add(-rd.config.CacheExpiration + time.Minute)
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
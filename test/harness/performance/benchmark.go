package performance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/test/harness"
	"github.com/jlgore/corkscrew/test/harness/scenarios/aws"
)

// PerformanceBenchmark manages performance testing and regression analysis
type PerformanceBenchmark struct {
	baselineFile string
	resultsDir   string
	ctx          context.Context
}

// BenchmarkConfig configures performance benchmark parameters
type BenchmarkConfig struct {
	ResourceCounts    []int    // Number of resources to test (1, 10, 100, 1000)
	Scenarios         []string // Scenarios to benchmark
	Iterations        int      // Number of iterations per test
	WarmupIterations  int      // Warmup iterations to exclude
	ConcurrencyLevels []int    // Concurrent scan levels to test
	BaselineFile      string   // File containing baseline performance data
	ResultsDir        string   // Directory to store results
}

// BenchmarkResult contains comprehensive benchmark results
type BenchmarkResult struct {
	Timestamp        time.Time `json:"timestamp"`
	TestID           string    `json:"test_id"`
	Scenario         string    `json:"scenario"`
	ResourceCount    int       `json:"resource_count"`
	ConcurrencyLevel int       `json:"concurrency_level"`
	Iterations       int       `json:"iterations"`

	// Timing metrics
	DeploymentTime   time.Duration `json:"deployment_time"`
	ScanTime         time.Duration `json:"scan_time"`
	VerificationTime time.Duration `json:"verification_time"`
	CleanupTime      time.Duration `json:"cleanup_time"`
	TotalTime        time.Duration `json:"total_time"`

	// Performance metrics
	DatabaseSize int64         `json:"database_size"`
	MemoryUsage  MemoryMetrics `json:"memory_usage"`
	CPUUsage     CPUMetrics    `json:"cpu_usage"`

	// DuckDB specific metrics
	InsertRate       float64       `json:"insert_rate"`       // Records per second
	QueryTime        time.Duration `json:"query_time"`        // Average query time
	CompressionRatio float64       `json:"compression_ratio"` // Raw data compression

	// Resource metrics
	ResourcesCreated int     `json:"resources_created"`
	ResourcesScanned int     `json:"resources_scanned"`
	ScanSuccessRate  float64 `json:"scan_success_rate"`

	// Errors and warnings
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`

	// Comparison with baseline
	BaselineComparison *BaselineComparison `json:"baseline_comparison,omitempty"`
}

// MemoryMetrics tracks memory usage during benchmark
type MemoryMetrics struct {
	StartMemory uint64        `json:"start_memory"`
	PeakMemory  uint64        `json:"peak_memory"`
	EndMemory   uint64        `json:"end_memory"`
	AllocatedMB uint64        `json:"allocated_mb"`
	GCPauses    int           `json:"gc_pauses"`
	GCTotalTime time.Duration `json:"gc_total_time"`
}

// CPUMetrics tracks CPU usage during benchmark
type CPUMetrics struct {
	UserTime   time.Duration `json:"user_time"`
	SystemTime time.Duration `json:"system_time"`
	Goroutines int           `json:"goroutines"`
}

// BaselineComparison compares current results with baseline
type BaselineComparison struct {
	DeploymentTimeRatio   float64 `json:"deployment_time_ratio"`
	ScanTimeRatio         float64 `json:"scan_time_ratio"`
	MemoryUsageRatio      float64 `json:"memory_usage_ratio"`
	DatabaseSizeRatio     float64 `json:"database_size_ratio"`
	PerformanceRegression bool    `json:"performance_regression"`
	RegressionThreshold   float64 `json:"regression_threshold"`
}

// BenchmarkReport aggregates multiple benchmark results
type BenchmarkReport struct {
	Timestamp       time.Time         `json:"timestamp"`
	TestID          string            `json:"test_id"`
	Config          BenchmarkConfig   `json:"config"`
	Results         []BenchmarkResult `json:"results"`
	Summary         BenchmarkSummary  `json:"summary"`
	Regressions     []string          `json:"regressions"`
	Recommendations []string          `json:"recommendations"`
}

// BenchmarkSummary provides high-level performance metrics
type BenchmarkSummary struct {
	TotalTests         int                `json:"total_tests"`
	SuccessfulTests    int                `json:"successful_tests"`
	FailedTests        int                `json:"failed_tests"`
	AverageDeployTime  time.Duration      `json:"average_deploy_time"`
	AverageScanTime    time.Duration      `json:"average_scan_time"`
	PeakMemoryUsage    uint64             `json:"peak_memory_usage"`
	OptimalConcurrency int                `json:"optimal_concurrency"`
	ScalabilityMetrics map[string]float64 `json:"scalability_metrics"`
}

// NewPerformanceBenchmark creates a new performance benchmark
func NewPerformanceBenchmark(ctx context.Context, baselineFile, resultsDir string) *PerformanceBenchmark {
	return &PerformanceBenchmark{
		baselineFile: baselineFile,
		resultsDir:   resultsDir,
		ctx:          ctx,
	}
}

// RunBenchmarkSuite executes a comprehensive performance benchmark
func (pb *PerformanceBenchmark) RunBenchmarkSuite(config BenchmarkConfig) (*BenchmarkReport, error) {
	// Ensure results directory exists
	if err := os.MkdirAll(pb.resultsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create results directory: %w", err)
	}

	testID := fmt.Sprintf("perf-%d", time.Now().Unix())

	report := &BenchmarkReport{
		Timestamp:       time.Now(),
		TestID:          testID,
		Config:          config,
		Results:         []BenchmarkResult{},
		Regressions:     []string{},
		Recommendations: []string{},
	}

	fmt.Printf("🚀 Starting performance benchmark suite: %s\n", testID)
	fmt.Printf("   Resource counts: %v\n", config.ResourceCounts)
	fmt.Printf("   Scenarios: %v\n", config.Scenarios)
	fmt.Printf("   Concurrency levels: %v\n", config.ConcurrencyLevels)

	// Load baseline data for comparison
	baseline, err := pb.loadBaseline()
	if err != nil {
		fmt.Printf("⚠️ Warning: Could not load baseline data: %v\n", err)
	}

	// Run benchmarks for each configuration
	for _, scenario := range config.Scenarios {
		for _, resourceCount := range config.ResourceCounts {
			for _, concurrency := range config.ConcurrencyLevels {
				fmt.Printf("\n📊 Running benchmark: %s with %d resources, concurrency %d\n",
					scenario, resourceCount, concurrency)

				result, err := pb.runSingleBenchmark(testID, scenario, resourceCount, concurrency, config.Iterations, config.WarmupIterations)
				if err != nil {
					result.Errors = append(result.Errors, err.Error())
					fmt.Printf("❌ Benchmark failed: %v\n", err)
				} else {
					fmt.Printf("✅ Benchmark completed: scan=%v, memory=%dMB\n",
						result.ScanTime, result.MemoryUsage.PeakMemory/1024/1024)
				}

				// Compare with baseline if available
				if baseline != nil {
					result.BaselineComparison = pb.compareWithBaseline(result, baseline)
					if result.BaselineComparison.PerformanceRegression {
						regression := fmt.Sprintf("%s-%d-%d: %.1fx slower",
							scenario, resourceCount, concurrency, result.BaselineComparison.ScanTimeRatio)
						report.Regressions = append(report.Regressions, regression)
					}
				}

				report.Results = append(report.Results, *result)
			}
		}
	}

	// Generate summary and recommendations
	report.Summary = pb.generateSummary(report.Results)
	report.Recommendations = pb.generateRecommendations(report.Results)

	// Save detailed results
	if err := pb.saveResults(report); err != nil {
		fmt.Printf("⚠️ Warning: Could not save results: %v\n", err)
	}

	// Update baseline if all tests passed
	if report.Summary.FailedTests == 0 && len(report.Regressions) == 0 {
		if err := pb.updateBaseline(report.Results); err != nil {
			fmt.Printf("⚠️ Warning: Could not update baseline: %v\n", err)
		} else {
			fmt.Printf("📈 Baseline updated with new performance data\n")
		}
	}

	fmt.Printf("\n🎉 Benchmark suite completed: %d tests, %d regressions\n",
		report.Summary.TotalTests, len(report.Regressions))

	return report, nil
}

// runSingleBenchmark executes a single benchmark configuration
func (pb *PerformanceBenchmark) runSingleBenchmark(testID, scenario string, resourceCount, concurrency, iterations, warmupIterations int) (*BenchmarkResult, error) {
	benchTestID := fmt.Sprintf("%s-%s-%d-%d", testID, scenario, resourceCount, concurrency)

	result := &BenchmarkResult{
		Timestamp:        time.Now(),
		TestID:           benchTestID,
		Scenario:         scenario,
		ResourceCount:    resourceCount,
		ConcurrencyLevel: concurrency,
		Iterations:       iterations,
		Errors:           []string{},
		Warnings:         []string{},
	}

	// Create appropriate scenario based on resource count
	scenarioInstance, err := pb.createScaledScenario(scenario, resourceCount)
	if err != nil {
		return result, fmt.Errorf("failed to create scenario: %w", err)
	}

	var totalDeployTime, totalScanTime, totalVerifyTime, totalCleanupTime time.Duration
	var totalMemoryUsage uint64
	var successfulRuns int

	// Run warmup iterations (not counted in results)
	for i := 0; i < warmupIterations; i++ {
		fmt.Printf("   Warmup %d/%d\n", i+1, warmupIterations)
		_, err := pb.executeSingleRun(benchTestID+fmt.Sprintf("-warmup-%d", i), scenarioInstance, concurrency)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Warmup %d failed: %v", i, err))
		}
	}

	// Run actual benchmark iterations
	for i := 0; i < iterations; i++ {
		fmt.Printf("   Iteration %d/%d\n", i+1, iterations)

		runResult, err := pb.executeSingleRun(benchTestID+fmt.Sprintf("-run-%d", i), scenarioInstance, concurrency)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Iteration %d failed: %v", i, err))
			continue
		}

		totalDeployTime += runResult.DeploymentDuration
		totalScanTime += runResult.Metrics.ScanDuration
		totalVerifyTime += runResult.Metrics.VerificationDuration
		if runResult.CleanupResult != nil {
			totalCleanupTime += runResult.CleanupResult.CleanupDuration
		}
		totalMemoryUsage += runResult.Metrics.MemoryUsed

		result.ResourcesCreated = runResult.Metrics.ResourcesDeployed
		result.ResourcesScanned = runResult.Metrics.ResourcesScanned
		result.DatabaseSize = runResult.Metrics.DatabaseSize

		successfulRuns++
	}

	if successfulRuns == 0 {
		return result, fmt.Errorf("all benchmark iterations failed")
	}

	// Calculate averages
	result.DeploymentTime = totalDeployTime / time.Duration(successfulRuns)
	result.ScanTime = totalScanTime / time.Duration(successfulRuns)
	result.VerificationTime = totalVerifyTime / time.Duration(successfulRuns)
	result.CleanupTime = totalCleanupTime / time.Duration(successfulRuns)
	result.TotalTime = result.DeploymentTime + result.ScanTime + result.VerificationTime + result.CleanupTime

	result.ScanSuccessRate = float64(successfulRuns) / float64(iterations) * 100.0

	// Calculate performance metrics
	if result.ScanTime > 0 {
		result.InsertRate = float64(result.ResourcesScanned) / result.ScanTime.Seconds()
	}

	// Estimate compression ratio (simplified)
	if result.DatabaseSize > 0 {
		estimatedRawSize := int64(result.ResourcesScanned * 2048) // Assume 2KB per resource raw data
		result.CompressionRatio = float64(estimatedRawSize) / float64(result.DatabaseSize)
	}

	return result, nil
}

// executeSingleRun executes a single test run and measures performance
func (pb *PerformanceBenchmark) executeSingleRun(testID string, scenario harness.Scenario, concurrency int) (*harness.TestResult, error) {
	// Start memory profiling
	var startMemStats, endMemStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&startMemStats)

	startTime := time.Now()

	// Create test orchestrator
	orchestrator, err := harness.NewTestOrchestrator(pb.ctx, harness.OrchestratorConfig{
		Provider:      "aws",
		Region:        "us-east-1",
		ScenarioName:  scenario.GetName(),
		TestID:        testID,
		CorkscrewPath: "../../../corkscrew",
		DBPath:        fmt.Sprintf("bench_%s.db", testID),
		KeepOnFail:    false,
		Timeout:       30 * time.Minute,
		CleanupDelay:  5 * time.Second, // Minimal delay for benchmarks
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Run the test with performance monitoring
	result, err := orchestrator.RunTest(pb.ctx)
	if err != nil {
		return nil, fmt.Errorf("test execution failed: %w", err)
	}

	// End memory profiling
	runtime.GC()
	runtime.ReadMemStats(&endMemStats)

	endTime := time.Now()

	// Update result with performance metrics
	result.Metrics.MemoryUsed = endMemStats.TotalAlloc - startMemStats.TotalAlloc

	// Cleanup test database
	dbPath := fmt.Sprintf("bench_%s.db", testID)
	os.Remove(dbPath)
	os.Remove(dbPath + ".wal")

	return result, nil
}

// createScaledScenario creates a scenario scaled to the specified resource count
func (pb *PerformanceBenchmark) createScaledScenario(scenarioName string, resourceCount int) (harness.Scenario, error) {
	switch scenarioName {
	case "simple-s3":
		return aws.NewSimpleS3Scenario(), nil
	case "edge-cases":
		return aws.NewEdgeCasesScenario(), nil
	case "cross-region":
		return aws.NewCrossRegionScenario(), nil
	case "performance-scaled":
		// Create a custom scenario with specified resource count
		return aws.NewPerformanceScaledScenario(resourceCount), nil
	default:
		return nil, fmt.Errorf("unknown scenario: %s", scenarioName)
	}
}

// compareWithBaseline compares current result with baseline performance
func (pb *PerformanceBenchmark) compareWithBaseline(result BenchmarkResult, baseline map[string]BenchmarkResult) *BaselineComparison {
	key := fmt.Sprintf("%s-%d-%d", result.Scenario, result.ResourceCount, result.ConcurrencyLevel)
	baselineResult, exists := baseline[key]
	if !exists {
		return nil
	}

	comparison := &BaselineComparison{
		RegressionThreshold: 1.5, // 50% performance degradation threshold
	}

	if baselineResult.DeploymentTime > 0 {
		comparison.DeploymentTimeRatio = float64(result.DeploymentTime) / float64(baselineResult.DeploymentTime)
	}

	if baselineResult.ScanTime > 0 {
		comparison.ScanTimeRatio = float64(result.ScanTime) / float64(baselineResult.ScanTime)
	}

	if baselineResult.MemoryUsage.PeakMemory > 0 {
		comparison.MemoryUsageRatio = float64(result.MemoryUsage.PeakMemory) / float64(baselineResult.MemoryUsage.PeakMemory)
	}

	if baselineResult.DatabaseSize > 0 {
		comparison.DatabaseSizeRatio = float64(result.DatabaseSize) / float64(baselineResult.DatabaseSize)
	}

	// Check for performance regression
	comparison.PerformanceRegression = comparison.ScanTimeRatio > comparison.RegressionThreshold ||
		comparison.MemoryUsageRatio > comparison.RegressionThreshold

	return comparison
}

// generateSummary creates a summary of all benchmark results
func (pb *PerformanceBenchmark) generateSummary(results []BenchmarkResult) BenchmarkSummary {
	summary := BenchmarkSummary{
		TotalTests:         len(results),
		ScalabilityMetrics: make(map[string]float64),
	}

	if len(results) == 0 {
		return summary
	}

	var totalDeployTime, totalScanTime time.Duration
	var maxMemory uint64
	var successfulTests int

	for _, result := range results {
		if len(result.Errors) == 0 {
			successfulTests++
			totalDeployTime += result.DeploymentTime
			totalScanTime += result.ScanTime

			if result.MemoryUsage.PeakMemory > maxMemory {
				maxMemory = result.MemoryUsage.PeakMemory
			}
		}
	}

	summary.SuccessfulTests = successfulTests
	summary.FailedTests = summary.TotalTests - successfulTests

	if successfulTests > 0 {
		summary.AverageDeployTime = totalDeployTime / time.Duration(successfulTests)
		summary.AverageScanTime = totalScanTime / time.Duration(successfulTests)
	}

	summary.PeakMemoryUsage = maxMemory

	// Calculate scalability metrics
	summary.ScalabilityMetrics = pb.calculateScalabilityMetrics(results)

	// Determine optimal concurrency level
	summary.OptimalConcurrency = pb.findOptimalConcurrency(results)

	return summary
}

// calculateScalabilityMetrics analyzes how performance scales with resource count
func (pb *PerformanceBenchmark) calculateScalabilityMetrics(results []BenchmarkResult) map[string]float64 {
	metrics := make(map[string]float64)

	// Group results by scenario and concurrency
	groups := make(map[string][]BenchmarkResult)
	for _, result := range results {
		if len(result.Errors) == 0 {
			key := fmt.Sprintf("%s-%d", result.Scenario, result.ConcurrencyLevel)
			groups[key] = append(groups[key], result)
		}
	}

	// Calculate scalability for each group
	for groupKey, groupResults := range groups {
		if len(groupResults) < 2 {
			continue
		}

		// Sort by resource count
		for i := 0; i < len(groupResults)-1; i++ {
			for j := i + 1; j < len(groupResults); j++ {
				if groupResults[i].ResourceCount > groupResults[j].ResourceCount {
					groupResults[i], groupResults[j] = groupResults[j], groupResults[i]
				}
			}
		}

		// Calculate scaling factor
		first := groupResults[0]
		last := groupResults[len(groupResults)-1]

		resourceRatio := float64(last.ResourceCount) / float64(first.ResourceCount)
		timeRatio := float64(last.ScanTime) / float64(first.ScanTime)

		scalingFactor := timeRatio / resourceRatio // Ideal would be 1.0 (linear)
		metrics[groupKey+"_scaling"] = scalingFactor
	}

	return metrics
}

// findOptimalConcurrency determines the best concurrency level
func (pb *PerformanceBenchmark) findOptimalConcurrency(results []BenchmarkResult) int {
	// Group by scenario and resource count, find best concurrency
	groups := make(map[string][]BenchmarkResult)
	for _, result := range results {
		if len(result.Errors) == 0 {
			key := fmt.Sprintf("%s-%d", result.Scenario, result.ResourceCount)
			groups[key] = append(groups[key], result)
		}
	}

	optimalConcurrency := 1
	bestThroughput := 0.0

	for _, groupResults := range groups {
		for _, result := range groupResults {
			throughput := result.InsertRate
			if throughput > bestThroughput {
				bestThroughput = throughput
				optimalConcurrency = result.ConcurrencyLevel
			}
		}
	}

	return optimalConcurrency
}

// generateRecommendations creates performance improvement recommendations
func (pb *PerformanceBenchmark) generateRecommendations(results []BenchmarkResult) []string {
	recommendations := []string{}

	// Analyze results for recommendations
	avgMemoryMB := int64(0)
	avgScanTime := time.Duration(0)
	successfulTests := 0

	for _, result := range results {
		if len(result.Errors) == 0 {
			avgMemoryMB += int64(result.MemoryUsage.PeakMemory / 1024 / 1024)
			avgScanTime += result.ScanTime
			successfulTests++
		}
	}

	if successfulTests > 0 {
		avgMemoryMB /= int64(successfulTests)
		avgScanTime /= time.Duration(successfulTests)
	}

	// Memory recommendations
	if avgMemoryMB > 1000 {
		recommendations = append(recommendations,
			fmt.Sprintf("High memory usage detected (avg %dMB). Consider implementing streaming or batch processing.", avgMemoryMB))
	}

	// Performance recommendations
	if avgScanTime > 60*time.Second {
		recommendations = append(recommendations,
			"Slow scan times detected. Consider optimizing database queries and indexing.")
	}

	// Add general recommendations
	recommendations = append(recommendations,
		"Monitor database size growth and implement periodic maintenance.",
		"Consider implementing result caching for repeated scans.",
		"Evaluate concurrent scanning benefits for large resource sets.")

	return recommendations
}

// loadBaseline loads baseline performance data from file
func (pb *PerformanceBenchmark) loadBaseline() (map[string]BenchmarkResult, error) {
	if pb.baselineFile == "" {
		return nil, fmt.Errorf("no baseline file specified")
	}

	data, err := os.ReadFile(pb.baselineFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read baseline file: %w", err)
	}

	var baseline map[string]BenchmarkResult
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("failed to parse baseline data: %w", err)
	}

	return baseline, nil
}

// updateBaseline updates the baseline with new performance data
func (pb *PerformanceBenchmark) updateBaseline(results []BenchmarkResult) error {
	if pb.baselineFile == "" {
		return fmt.Errorf("no baseline file specified")
	}

	baseline := make(map[string]BenchmarkResult)

	// Load existing baseline
	if existingBaseline, err := pb.loadBaseline(); err == nil {
		baseline = existingBaseline
	}

	// Update with new results
	for _, result := range results {
		if len(result.Errors) == 0 {
			key := fmt.Sprintf("%s-%d-%d", result.Scenario, result.ResourceCount, result.ConcurrencyLevel)
			baseline[key] = result
		}
	}

	// Save updated baseline
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal baseline data: %w", err)
	}

	if err := os.WriteFile(pb.baselineFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write baseline file: %w", err)
	}

	return nil
}

// saveResults saves benchmark results to file
func (pb *PerformanceBenchmark) saveResults(report *BenchmarkReport) error {
	filename := fmt.Sprintf("%s/benchmark_report_%s.json", pb.resultsDir, report.TestID)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write report file: %w", err)
	}

	return nil
}

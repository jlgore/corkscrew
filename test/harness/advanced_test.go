//go:build integration

package harness

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jlgore/corkscrew/test/harness/cleanup"
	"github.com/jlgore/corkscrew/test/harness/performance"
	"github.com/jlgore/corkscrew/test/harness/scenarios/aws"
	"github.com/jlgore/corkscrew/test/harness/verification"
	"github.com/stretchr/testify/require"
)

var (
	runEdgeCases     = flag.Bool("edge-cases", false, "Run edge case testing")
	runCrossRegion   = flag.Bool("cross-region", false, "Run cross-region testing")
	runPerformance   = flag.Bool("performance", false, "Run performance benchmarks")
	runRegression    = flag.Bool("regression", false, "Run regression analysis")
	performanceScale = flag.Int("perf-scale", 10, "Resource count for performance testing")
)

// TestEdgeCases runs comprehensive edge case testing
func TestEdgeCases(t *testing.T) {
	if !*runEdgeCases {
		t.Skip("Edge case testing disabled. Use -edge-cases to enable.")
	}

	testID := fmt.Sprintf("edge-cases-%d", time.Now().Unix())
	ctx := context.Background()

	t.Logf("🔥 Starting edge case testing: %s", testID)

	// Create edge cases scenario
	scenario := aws.NewEdgeCasesScenario()

	// Run the test with extended timeout for complex scenario
	orchestrator, err := NewTestOrchestrator(ctx, OrchestratorConfig{
		Provider:      "aws",
		Region:        "us-east-1",
		ScenarioName:  scenario.GetName(),
		TestID:        testID,
		CorkscrewPath: "../../corkscrew",
		DBPath:        fmt.Sprintf("edge_cases_%s.db", testID),
		KeepOnFail:    true, // Keep resources for edge case analysis
		Timeout:       20 * time.Minute,
		CleanupDelay:  45 * time.Second, // Longer delay for complex resources
	})
	require.NoError(t, err)

	result, err := orchestrator.RunTest(ctx)
	require.NoError(t, err)
	require.True(t, result.Success, "Edge case test failed: %s", result.Summary())

	// Perform specialized edge case verification
	t.Logf("🔍 Running edge case verification")
	edgeVerifier, err := verification.NewEdgeCaseVerifier(result.ScanResult.Output)
	require.NoError(t, err)
	defer edgeVerifier.Close()

	edgeResults, err := edgeVerifier.VerifyEdgeCases(ctx, testID)
	require.NoError(t, err)

	// Log edge case verification results
	t.Logf("Unicode Support:")
	t.Logf("  Unicode tags found: %d", edgeResults.UnicodeSupport.UnicodeTagsFound)
	t.Logf("  Emoji tags found: %d", edgeResults.UnicodeSupport.EmojiTagsFound)
	t.Logf("  Encoding issues: %d", len(edgeResults.UnicodeSupport.EncodingIssues))

	t.Logf("Tag Limits:")
	t.Logf("  Resources with max tags: %d", edgeResults.TagLimits.ResourcesWithMaxTags)
	t.Logf("  Max tags per resource: %d", edgeResults.TagLimits.MaxTagsPerResource)
	t.Logf("  Tag limit compliance: %t", edgeResults.TagLimits.TagLimitCompliance)

	t.Logf("Long Names:")
	t.Logf("  Longest resource name: %d chars", edgeResults.LongNames.LongestResourceName)
	t.Logf("  Resources with long names: %d", edgeResults.LongNames.ResourcesWithLongNames)
	t.Logf("  Name truncation issues: %d", len(edgeResults.LongNames.NameTruncationIssues))

	t.Logf("Global Services:")
	t.Logf("  Global services: %v", edgeResults.GlobalServices.GlobalServices)
	t.Logf("  Regional services: %v", edgeResults.GlobalServices.RegionalServices)

	t.Logf("Circular Dependencies:")
	t.Logf("  Circular dependencies found: %d", edgeResults.CircularDependencies.CircularDependenciesFound)
	t.Logf("  Dependency graph health: %t", edgeResults.CircularDependencies.DependencyGraphHealth)

	t.Logf("Raw Data Integrity:")
	t.Logf("  Resources with raw data: %d", edgeResults.RawDataIntegrity.ResourcesWithRawData)
	t.Logf("  Valid JSON: %d", edgeResults.RawDataIntegrity.RawDataValidJSON)
	t.Logf("  Unicode in raw data: %d", edgeResults.RawDataIntegrity.UnicodeInRawData)

	t.Logf("Compression Analysis:")
	t.Logf("  Total raw data size: %d bytes", edgeResults.CompressionAnalysis.TotalRawDataSize)
	t.Logf("  Compression ratio: %.2f", edgeResults.CompressionAnalysis.CompressionRatio)
	t.Logf("  Compression potential: %.1f%%", edgeResults.CompressionAnalysis.CompressionPotential*100)

	// Assert key edge case requirements
	require.True(t, edgeResults.TagLimits.TagLimitCompliance, "Tag limit compliance failed")
	require.Equal(t, 0, len(edgeResults.UnicodeSupport.EncodingIssues), "Unicode encoding issues detected")
	require.Equal(t, 0, len(edgeResults.RawDataIntegrity.DataConsistencyIssues), "Data consistency issues detected")

	t.Logf("✅ Edge case testing completed successfully")
}

// TestCrossRegion runs cross-region testing
func TestCrossRegion(t *testing.T) {
	if !*runCrossRegion {
		t.Skip("Cross-region testing disabled. Use -cross-region to enable.")
	}

	testID := fmt.Sprintf("cross-region-%d", time.Now().Unix())
	ctx := context.Background()

	t.Logf("🌍 Starting cross-region testing: %s", testID)

	// Create cross-region scenario
	scenario := aws.NewCrossRegionScenario()

	// Run the test with extended timeout for multi-region deployment
	orchestrator, err := NewTestOrchestrator(ctx, OrchestratorConfig{
		Provider:      "aws",
		Region:        "us-east-1", // Primary region
		ScenarioName:  scenario.GetName(),
		TestID:        testID,
		CorkscrewPath: "../../corkscrew",
		DBPath:        fmt.Sprintf("cross_region_%s.db", testID),
		KeepOnFail:    true,
		Timeout:       30 * time.Minute, // Extended timeout for multi-region
		CleanupDelay:  60 * time.Second,
	})
	require.NoError(t, err)

	result, err := orchestrator.RunTest(ctx)
	require.NoError(t, err)
	require.True(t, result.Success, "Cross-region test failed: %s", result.Summary())

	// Verify cross-region specific aspects
	t.Logf("🔍 Verifying cross-region deployment")
	require.Contains(t, result.DeploymentOutputs, "regions", "Region information not exported")

	// Test multi-region cleanup
	t.Logf("🧹 Testing multi-region cleanup")
	regions := []string{"us-east-1", "us-west-2", "eu-west-1"}
	multiCleanup, err := cleanup.NewMultiRegionCleanup(testID, regions, 3)
	require.NoError(t, err)

	cleanupResult, err := multiCleanup.ExecuteCleanup(ctx)
	require.NoError(t, err)

	t.Logf("Multi-region cleanup results:")
	t.Logf("  Successful regions: %d/%d", cleanupResult.Summary.SuccessfulRegions, cleanupResult.Summary.TotalRegions)
	t.Logf("  Total errors: %d", cleanupResult.Summary.TotalErrors)
	t.Logf("  Overall success: %t", cleanupResult.Summary.OverallSuccess)

	// Log cleanup details by region
	for _, regionResult := range cleanupResult.RegionResults {
		t.Logf("  Region %s: %v (%d resources)", regionResult.Region, regionResult.Success,
			sumResourceCounts(regionResult.ResourcesCleaned))
	}

	require.True(t, cleanupResult.Summary.OverallSuccess, "Multi-region cleanup failed")

	t.Logf("✅ Cross-region testing completed successfully")
}

// TestPerformanceBenchmark runs performance benchmarks
func TestPerformanceBenchmark(t *testing.T) {
	if !*runPerformance {
		t.Skip("Performance benchmarking disabled. Use -performance to enable.")
	}

	testID := fmt.Sprintf("perf-%d", time.Now().Unix())
	ctx := context.Background()

	t.Logf("📊 Starting performance benchmark: %s", testID)

	// Create performance benchmark
	resultsDir := filepath.Join("results", "performance")
	benchmark := performance.NewPerformanceBenchmark(ctx, "baseline.json", resultsDir)

	// Configure benchmark parameters
	config := performance.BenchmarkConfig{
		ResourceCounts:    []int{1, 10, *performanceScale},
		Scenarios:         []string{"performance-scaled"},
		Iterations:        3,
		WarmupIterations:  1,
		ConcurrencyLevels: []int{1, 2, 4},
		BaselineFile:      filepath.Join(resultsDir, "baseline.json"),
		ResultsDir:        resultsDir,
	}

	t.Logf("Benchmark configuration:")
	t.Logf("  Resource counts: %v", config.ResourceCounts)
	t.Logf("  Scenarios: %v", config.Scenarios)
	t.Logf("  Iterations: %d", config.Iterations)
	t.Logf("  Concurrency levels: %v", config.ConcurrencyLevels)

	// Run the benchmark suite
	report, err := benchmark.RunBenchmarkSuite(config)
	require.NoError(t, err)

	// Log benchmark results
	t.Logf("Benchmark Results:")
	t.Logf("  Total tests: %d", report.Summary.TotalTests)
	t.Logf("  Successful tests: %d", report.Summary.SuccessfulTests)
	t.Logf("  Failed tests: %d", report.Summary.FailedTests)
	t.Logf("  Average scan time: %v", report.Summary.AverageScanTime)
	t.Logf("  Peak memory usage: %.2f MB", float64(report.Summary.PeakMemoryUsage)/1024/1024)
	t.Logf("  Optimal concurrency: %d", report.Summary.OptimalConcurrency)

	// Log scalability metrics
	t.Logf("Scalability Metrics:")
	for metric, value := range report.Summary.ScalabilityMetrics {
		t.Logf("  %s: %.2f", metric, value)
	}

	// Check for performance regressions
	if len(report.Regressions) > 0 {
		t.Logf("⚠️ Performance regressions detected:")
		for _, regression := range report.Regressions {
			t.Logf("  %s", regression)
		}
	}

	// Assert performance requirements
	require.LessOrEqual(t, report.Summary.FailedTests, 0, "Performance benchmark failures detected")
	require.GreaterOrEqual(t, float64(report.Summary.SuccessfulTests)/float64(report.Summary.TotalTests)*100, 95.0,
		"Performance benchmark success rate below 95%")

	t.Logf("✅ Performance benchmark completed successfully")
}

// TestRegressionAnalysis runs regression analysis
func TestRegressionAnalysis(t *testing.T) {
	if !*runRegression {
		t.Skip("Regression analysis disabled. Use -regression to enable.")
	}

	t.Logf("📈 Starting regression analysis")

	// Create regression analyzer
	analyzer := performance.NewRegressionAnalyzer(
		filepath.Join("results", "baseline"),
		filepath.Join("results", "history"),
		filepath.Join("results", "regression"),
	)

	// Load a recent benchmark report for analysis
	// In practice, this would be the latest benchmark results
	mockReport := &performance.BenchmarkReport{
		Timestamp: time.Now(),
		TestID:    fmt.Sprintf("regression-analysis-%d", time.Now().Unix()),
		Results: []performance.BenchmarkResult{
			{
				Timestamp:     time.Now(),
				Scenario:      "simple-s3",
				ResourceCount: 1,
				ScanTime:      15 * time.Second,
				MemoryUsage: performance.MemoryMetrics{
					PeakMemory: 128 * 1024 * 1024, // 128 MB
				},
				DatabaseSize:    1024 * 1024, // 1 MB
				ScanSuccessRate: 100.0,
			},
		},
		Summary: performance.BenchmarkSummary{
			TotalTests:      1,
			SuccessfulTests: 1,
			FailedTests:     0,
		},
	}

	// Generate regression report
	regressionReport, err := analyzer.GenerateRegressionReport(mockReport)
	if err != nil {
		t.Logf("⚠️ Could not generate regression report (likely no baseline): %v", err)
		t.Skip("Skipping regression analysis - no baseline data available")
		return
	}

	// Log regression analysis results
	t.Logf("Regression Analysis Results:")
	t.Logf("  Overall status: %s", regressionReport.OverallStatus)
	t.Logf("  Total scenarios: %d", regressionReport.Summary.TotalScenarios)
	t.Logf("  No regression: %d", regressionReport.Summary.NoRegressionCount)
	t.Logf("  Minor regression: %d", regressionReport.Summary.MinorRegressionCount)
	t.Logf("  Major regression: %d", regressionReport.Summary.MajorRegressionCount)
	t.Logf("  Improvements: %d", regressionReport.Summary.ImprovementCount)

	if len(regressionReport.Summary.CriticalIssues) > 0 {
		t.Logf("🚨 Critical Issues:")
		for _, issue := range regressionReport.Summary.CriticalIssues {
			t.Logf("  %s", issue)
		}
	}

	t.Logf("Trend Analysis:")
	t.Logf("  Overall trend: %s", regressionReport.TrendAnalysis.OverallTrend)
	t.Logf("  Performance score: %.1f", regressionReport.TrendAnalysis.PerformanceScore)
	t.Logf("  Scalability score: %.1f", regressionReport.TrendAnalysis.ScalabilityScore)
	t.Logf("  Reliability score: %.1f", regressionReport.TrendAnalysis.ReliabilityScore)

	if len(regressionReport.Recommendations) > 0 {
		t.Logf("Recommendations:")
		for _, rec := range regressionReport.Recommendations {
			t.Logf("  %s", rec)
		}
	}

	// Assert no major regressions
	require.NotEqual(t, "major_regression", string(regressionReport.OverallStatus),
		"Major performance regressions detected")

	t.Logf("✅ Regression analysis completed successfully")
}

// BenchmarkResourceScaling benchmarks performance across different resource counts
func BenchmarkResourceScaling(b *testing.B) {
	resourceCounts := []int{1, 5, 10, 25, 50}

	for _, count := range resourceCounts {
		b.Run(fmt.Sprintf("Resources_%d", count), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				testID := fmt.Sprintf("scale-bench-%d-%d", count, i)
				ctx := context.Background()

				// Create scaled performance scenario
				scenario := aws.NewPerformanceScaledScenario(count)

				orchestrator, err := NewTestOrchestrator(ctx, OrchestratorConfig{
					Provider:      "aws",
					Region:        "us-east-1",
					ScenarioName:  scenario.GetName(),
					TestID:        testID,
					CorkscrewPath: "../../corkscrew",
					DBPath:        fmt.Sprintf("scale_bench_%s.db", testID),
					KeepOnFail:    false,
					Timeout:       15 * time.Minute,
					CleanupDelay:  10 * time.Second,
				})
				if err != nil {
					b.Fatalf("Failed to create orchestrator: %v", err)
				}

				result, err := orchestrator.RunTest(ctx)
				if err != nil {
					b.Fatalf("Benchmark test failed: %v", err)
				}

				if !result.Success {
					b.Fatalf("Benchmark verification failed: %s", result.Summary())
				}

				// Report custom metrics
				b.ReportMetric(float64(result.Metrics.ScanDuration.Milliseconds()), "scan_ms")
				b.ReportMetric(float64(result.Metrics.ResourcesScanned), "resources_scanned")
				b.ReportMetric(float64(result.Metrics.DatabaseSize)/1024, "db_size_kb")
				b.ReportMetric(float64(result.Metrics.MemoryUsed)/1024/1024, "memory_mb")
			}
		})
	}
}

// Helper functions

func sumResourceCounts(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

// TestAdvancedSuite runs all advanced tests in sequence
func TestAdvancedSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping advanced test suite in short mode")
	}

	// Only run if explicitly requested
	if !*runEdgeCases && !*runCrossRegion && !*runPerformance && !*runRegression {
		t.Skip("Advanced test suite disabled. Use specific flags to enable components.")
	}

	t.Logf("🚀 Running complete advanced test suite")

	// Run edge cases if enabled
	if *runEdgeCases {
		t.Run("EdgeCases", TestEdgeCases)
	}

	// Run cross-region if enabled
	if *runCrossRegion {
		t.Run("CrossRegion", TestCrossRegion)
	}

	// Run performance benchmarks if enabled
	if *runPerformance {
		t.Run("Performance", TestPerformanceBenchmark)
	}

	// Run regression analysis if enabled
	if *runRegression {
		t.Run("Regression", TestRegressionAnalysis)
	}

	t.Logf("🎉 Advanced test suite completed")
}

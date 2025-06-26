package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jlgore/corkscrew/test/harness/scenarios"
	"github.com/stretchr/testify/require"
)

// TestConfiguration defines parameters for a parameterized test
type TestConfiguration struct {
	Provider     string            `json:"provider"`
	Region       string            `json:"region"`
	Scenario     string            `json:"scenario"`
	Config       map[string]string `json:"config"`
	Tags         map[string]string `json:"tags"`
	Encrypted    bool              `json:"encrypted"`
	TestTimeout  time.Duration     `json:"test_timeout"`
	CleanupDelay time.Duration     `json:"cleanup_delay"`
}

// TestMatrix defines a matrix of test configurations
type TestMatrix struct {
	Providers []string                     `json:"providers"`
	Regions   []string                     `json:"regions"`
	Scenarios []string                     `json:"scenarios"`
	Configs   map[string]TestConfiguration `json:"configs"`
}

// DefaultTestMatrix returns a comprehensive test matrix
func DefaultTestMatrix() TestMatrix {
	return TestMatrix{
		Providers: []string{"aws"},
		Regions:   []string{"us-east-1", "us-west-2", "eu-west-1"},
		Scenarios: []string{"simple-s3", "network-stack", "compute-stack", "security-stack", "storage-stack"},
		Configs: map[string]TestConfiguration{
			"quick": {
				TestTimeout:  5 * time.Minute,
				CleanupDelay: 15 * time.Second,
				Config: map[string]string{
					"instance_type": "t2.nano",
					"storage_size":  "10",
				},
			},
			"standard": {
				TestTimeout:  10 * time.Minute,
				CleanupDelay: 30 * time.Second,
				Config: map[string]string{
					"instance_type": "t2.micro",
					"storage_size":  "20",
				},
			},
			"encrypted": {
				TestTimeout:  15 * time.Minute,
				CleanupDelay: 30 * time.Second,
				Encrypted:    true,
				Config: map[string]string{
					"instance_type": "t2.micro",
					"storage_size":  "20",
					"encryption":    "true",
				},
			},
			"performance": {
				TestTimeout:  20 * time.Minute,
				CleanupDelay: 45 * time.Second,
				Config: map[string]string{
					"instance_type": "t3.small",
					"storage_size":  "100",
					"iops":          "3000",
				},
			},
		},
	}
}

// TestFramework provides parameterized testing capabilities
type TestFramework struct {
	matrix       TestMatrix
	results      map[string]*TestResult
	resultsMutex sync.RWMutex
	parallel     bool
	maxWorkers   int
}

// NewTestFramework creates a new test framework
func NewTestFramework(matrix TestMatrix) *TestFramework {
	return &TestFramework{
		matrix:     matrix,
		results:    make(map[string]*TestResult),
		parallel:   true,
		maxWorkers: 3, // Limit concurrent infrastructure deployments
	}
}

// SetParallel enables or disables parallel test execution
func (tf *TestFramework) SetParallel(parallel bool) {
	tf.parallel = parallel
}

// SetMaxWorkers sets the maximum number of concurrent tests
func (tf *TestFramework) SetMaxWorkers(maxWorkers int) {
	tf.maxWorkers = maxWorkers
}

// RunMatrix executes all test combinations in the matrix
func (tf *TestFramework) RunMatrix(t *testing.T) {
	ctx := context.Background()
	
	// Generate all test combinations
	combinations := tf.generateTestCombinations()
	
	t.Logf("Generated %d test combinations", len(combinations))
	
	if tf.parallel && len(combinations) > 1 {
		tf.runParallel(t, ctx, combinations)
	} else {
		tf.runSequential(t, ctx, combinations)
	}
	
	// Generate comprehensive report
	tf.generateReport(t)
}

// RunSpecific runs a specific test configuration
func (tf *TestFramework) RunSpecific(t *testing.T, provider, region, scenario, configName string) {
	ctx := context.Background()
	
	testConfig := tf.matrix.Configs[configName]
	testConfig.Provider = provider
	testConfig.Region = region
	testConfig.Scenario = scenario
	
	testName := fmt.Sprintf("%s-%s-%s-%s", provider, region, scenario, configName)
	result := tf.runSingleTest(ctx, testName, testConfig)
	
	tf.storeResult(testName, result)
	
	// Assert test passed
	if !result.Success {
		t.Errorf("Test %s failed: %v", testName, result.Error)
	}
}

// runParallel executes tests in parallel with worker pool
func (tf *TestFramework) runParallel(t *testing.T, ctx context.Context, combinations []testCombination) {
	semaphore := make(chan struct{}, tf.maxWorkers)
	var wg sync.WaitGroup
	
	for _, combo := range combinations {
		wg.Add(1)
		go func(combo testCombination) {
			defer wg.Done()
			semaphore <- struct{}{} // Acquire
			defer func() { <-semaphore }() // Release
			
			result := tf.runSingleTest(ctx, combo.name, combo.config)
			tf.storeResult(combo.name, result)
			
			if !result.Success {
				t.Errorf("Test %s failed: %v", combo.name, result.Error)
			}
		}(combo)
	}
	
	wg.Wait()
}

// runSequential executes tests one after another
func (tf *TestFramework) runSequential(t *testing.T, ctx context.Context, combinations []testCombination) {
	for _, combo := range combinations {
		t.Run(combo.name, func(t *testing.T) {
			result := tf.runSingleTest(ctx, combo.name, combo.config)
			tf.storeResult(combo.name, result)
			
			require.True(t, result.Success, "Test failed: %v", result.Error)
		})
	}
}

// runSingleTest executes a single test configuration
func (tf *TestFramework) runSingleTest(ctx context.Context, testName string, config TestConfiguration) *TestResult {
	testID := fmt.Sprintf("%s-%d", testName, time.Now().Unix())
	
	orchestratorConfig := OrchestratorConfig{
		Provider:      config.Provider,
		Region:        config.Region,
		ScenarioName:  config.Scenario,
		TestID:        testID,
		CorkscrewPath: "../../corkscrew",
		DBPath:        fmt.Sprintf("test_%s.db", testID),
		KeepOnFail:    false, // Clean up by default in matrix tests
		Timeout:       config.TestTimeout,
		CleanupDelay:  config.CleanupDelay,
	}
	
	// Create test context with timeout
	testCtx, cancel := context.WithTimeout(ctx, config.TestTimeout)
	defer cancel()
	
	orchestrator, err := NewTestOrchestrator(testCtx, orchestratorConfig)
	if err != nil {
		return &TestResult{
			TestID:       testID,
			ScenarioName: config.Scenario,
			Provider:     config.Provider,
			Region:       config.Region,
			StartTime:    time.Now(),
			EndTime:      time.Now(),
			Phase:        "initialization",
			Success:      false,
			Error:        fmt.Errorf("failed to create orchestrator: %w", err),
		}
	}
	
	result, err := orchestrator.RunTest(testCtx)
	if err != nil && result.Error == nil {
		result.Error = err
		result.Success = false
	}
	
	return result
}

// testCombination represents a single test configuration
type testCombination struct {
	name   string
	config TestConfiguration
}

// generateTestCombinations creates all possible test combinations
func (tf *TestFramework) generateTestCombinations() []testCombination {
	var combinations []testCombination
	
	for _, provider := range tf.matrix.Providers {
		for _, region := range tf.matrix.Regions {
			for _, scenario := range tf.matrix.Scenarios {
				for configName, baseConfig := range tf.matrix.Configs {
					// Create a copy of the config
					config := baseConfig
					config.Provider = provider
					config.Region = region
					config.Scenario = scenario
					
					// Add region-specific tags
					if config.Tags == nil {
						config.Tags = make(map[string]string)
					}
					config.Tags["Region"] = region
					config.Tags["ConfigType"] = configName
					
					name := fmt.Sprintf("%s-%s-%s-%s", provider, region, scenario, configName)
					combinations = append(combinations, testCombination{
						name:   name,
						config: config,
					})
				}
			}
		}
	}
	
	return combinations
}

// storeResult safely stores a test result
func (tf *TestFramework) storeResult(testName string, result *TestResult) {
	tf.resultsMutex.Lock()
	defer tf.resultsMutex.Unlock()
	tf.results[testName] = result
}

// GetResults returns all test results
func (tf *TestFramework) GetResults() map[string]*TestResult {
	tf.resultsMutex.RLock()
	defer tf.resultsMutex.RUnlock()
	
	// Return a copy to avoid race conditions
	results := make(map[string]*TestResult)
	for k, v := range tf.results {
		results[k] = v
	}
	return results
}

// generateReport creates a comprehensive test report
func (tf *TestFramework) generateReport(t *testing.T) {
	results := tf.GetResults()
	
	// Calculate summary statistics
	var (
		totalTests     = len(results)
		passedTests    = 0
		failedTests    = 0
		totalDuration  time.Duration
		scenarioStats  = make(map[string]*ScenarioStats)
		regionStats    = make(map[string]*RegionStats)
		configStats    = make(map[string]*ConfigStats)
	)
	
	for testName, result := range results {
		totalDuration += result.Duration
		
		if result.Success {
			passedTests++
		} else {
			failedTests++
		}
		
		// Update scenario statistics
		if scenarioStats[result.ScenarioName] == nil {
			scenarioStats[result.ScenarioName] = &ScenarioStats{}
		}
		scenarioStats[result.ScenarioName].Update(result)
		
		// Update region statistics
		if regionStats[result.Region] == nil {
			regionStats[result.Region] = &RegionStats{}
		}
		regionStats[result.Region].Update(result)
		
		// Extract config type from test name
		configType := extractConfigType(testName)
		if configStats[configType] == nil {
			configStats[configType] = &ConfigStats{}
		}
		configStats[configType].Update(result)
	}
	
	// Generate detailed report
	report := TestMatrixReport{
		Summary: TestSummary{
			TotalTests:      totalTests,
			PassedTests:     passedTests,
			FailedTests:     failedTests,
			SuccessRate:     float64(passedTests) / float64(totalTests) * 100,
			TotalDuration:   totalDuration,
			AverageDuration: totalDuration / time.Duration(totalTests),
		},
		ScenarioStats: scenarioStats,
		RegionStats:   regionStats,
		ConfigStats:   configStats,
		Results:       results,
	}
	
	// Save report to file
	reportFile := fmt.Sprintf("test_matrix_report_%d.json", time.Now().Unix())
	if err := tf.saveReport(reportFile, report); err != nil {
		t.Logf("Warning: Failed to save report: %v", err)
	}
	
	// Print summary to test output
	tf.printReportSummary(t, report)
}

// saveReport saves the test report to a JSON file
func (tf *TestFramework) saveReport(filename string, report TestMatrixReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	
	resultsDir := "results"
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return err
	}
	
	return os.WriteFile(filepath.Join(resultsDir, filename), data, 0644)
}

// printReportSummary prints a summary of the test results
func (tf *TestFramework) printReportSummary(t *testing.T, report TestMatrixReport) {
	t.Logf("\n" + strings.Repeat("=", 80))
	t.Logf("🧪 TEST MATRIX SUMMARY")
	t.Logf(strings.Repeat("=", 80))
	t.Logf("Total Tests: %d", report.Summary.TotalTests)
	t.Logf("Passed: %d (%.1f%%)", report.Summary.PassedTests, report.Summary.SuccessRate)
	t.Logf("Failed: %d", report.Summary.FailedTests)
	t.Logf("Total Duration: %v", report.Summary.TotalDuration)
	t.Logf("Average Duration: %v", report.Summary.AverageDuration)
	
	t.Logf("\n📊 SCENARIO BREAKDOWN:")
	for scenario, stats := range report.ScenarioStats {
		t.Logf("- %s: %d/%d passed (%.1f%%), avg: %v", 
			scenario, stats.Passed, stats.Total, stats.SuccessRate, stats.AverageDuration)
	}
	
	t.Logf("\n🌍 REGION BREAKDOWN:")
	for region, stats := range report.RegionStats {
		t.Logf("- %s: %d/%d passed (%.1f%%), avg: %v", 
			region, stats.Passed, stats.Total, stats.SuccessRate, stats.AverageDuration)
	}
	
	if report.Summary.FailedTests > 0 {
		t.Logf("\n❌ FAILED TESTS:")
		for testName, result := range report.Results {
			if !result.Success {
				t.Logf("- %s: %v", testName, result.Error)
			}
		}
	}
	
	t.Logf(strings.Repeat("=", 80))
}

// extractConfigType extracts the configuration type from a test name
func extractConfigType(testName string) string {
	parts := splitTestName(testName)
	if len(parts) >= 4 {
		return parts[3] // provider-region-scenario-config
	}
	return "unknown"
}

// splitTestName splits a test name into its components
func splitTestName(testName string) []string {
	// Remove timestamp suffix if present
	if idx := findLastDash(testName); idx > 0 {
		testName = testName[:idx]
	}
	return []string{"aws", "us-east-1", "simple-s3", "standard"} // placeholder
}

// findLastDash finds the last dash in a string
func findLastDash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '-' {
			return i
		}
	}
	return -1
}

// Statistics structures

type TestSummary struct {
	TotalTests      int           `json:"total_tests"`
	PassedTests     int           `json:"passed_tests"`
	FailedTests     int           `json:"failed_tests"`
	SuccessRate     float64       `json:"success_rate"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
}

type ScenarioStats struct {
	Total           int           `json:"total"`
	Passed          int           `json:"passed"`
	Failed          int           `json:"failed"`
	SuccessRate     float64       `json:"success_rate"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
}

func (s *ScenarioStats) Update(result *TestResult) {
	s.Total++
	s.TotalDuration += result.Duration
	s.AverageDuration = s.TotalDuration / time.Duration(s.Total)
	
	if result.Success {
		s.Passed++
	} else {
		s.Failed++
	}
	
	s.SuccessRate = float64(s.Passed) / float64(s.Total) * 100
}

type RegionStats struct {
	Total           int           `json:"total"`
	Passed          int           `json:"passed"`
	Failed          int           `json:"failed"`
	SuccessRate     float64       `json:"success_rate"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
}

func (s *RegionStats) Update(result *TestResult) {
	s.Total++
	s.TotalDuration += result.Duration
	s.AverageDuration = s.TotalDuration / time.Duration(s.Total)
	
	if result.Success {
		s.Passed++
	} else {
		s.Failed++
	}
	
	s.SuccessRate = float64(s.Passed) / float64(s.Total) * 100
}

type ConfigStats struct {
	Total           int           `json:"total"`
	Passed          int           `json:"passed"`
	Failed          int           `json:"failed"`
	SuccessRate     float64       `json:"success_rate"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
}

func (s *ConfigStats) Update(result *TestResult) {
	s.Total++
	s.TotalDuration += result.Duration
	s.AverageDuration = s.TotalDuration / time.Duration(s.Total)
	
	if result.Success {
		s.Passed++
	} else {
		s.Failed++
	}
	
	s.SuccessRate = float64(s.Passed) / float64(s.Total) * 100
}

type TestMatrixReport struct {
	Summary       TestSummary                `json:"summary"`
	ScenarioStats map[string]*ScenarioStats  `json:"scenario_stats"`
	RegionStats   map[string]*RegionStats    `json:"region_stats"`
	ConfigStats   map[string]*ConfigStats    `json:"config_stats"`
	Results       map[string]*TestResult     `json:"results"`
}

// Test functions for the framework

func TestFrameworkSimple(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	// Simple test matrix with just one scenario
	matrix := TestMatrix{
		Providers: []string{"aws"},
		Regions:   []string{"us-east-1"},
		Scenarios: []string{"simple-s3"},
		Configs: map[string]TestConfiguration{
			"quick": {
				TestTimeout:  5 * time.Minute,
				CleanupDelay: 15 * time.Second,
			},
		},
	}
	
	framework := NewTestFramework(matrix)
	framework.SetParallel(false) // Run sequentially for simpler debugging
	framework.RunMatrix(t)
}

func TestFrameworkRegions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	// Test across multiple regions
	matrix := TestMatrix{
		Providers: []string{"aws"},
		Regions:   []string{"us-east-1", "us-west-2"},
		Scenarios: []string{"simple-s3"},
		Configs: map[string]TestConfiguration{
			"quick": {
				TestTimeout:  5 * time.Minute,
				CleanupDelay: 15 * time.Second,
			},
		},
	}
	
	framework := NewTestFramework(matrix)
	framework.SetParallel(true)
	framework.SetMaxWorkers(2)
	framework.RunMatrix(t)
}

func TestFrameworkConfigurations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	// Test different configurations
	matrix := TestMatrix{
		Providers: []string{"aws"},
		Regions:   []string{"us-east-1"},
		Scenarios: []string{"simple-s3", "network-stack"},
		Configs: map[string]TestConfiguration{
			"quick": {
				TestTimeout:  5 * time.Minute,
				CleanupDelay: 15 * time.Second,
			},
			"standard": {
				TestTimeout:  10 * time.Minute,
				CleanupDelay: 30 * time.Second,
			},
		},
	}
	
	framework := NewTestFramework(matrix)
	framework.SetParallel(true)
	framework.SetMaxWorkers(2)
	framework.RunMatrix(t)
}

// Benchmark test to measure performance across scenarios
func BenchmarkScenarios(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}
	
	scenarioNames := scenarios.DefaultRegistry.List()
	
	for _, scenarioName := range scenarioNames {
		b.Run(scenarioName, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				testConfig := TestConfiguration{
					Provider:     "aws",
					Region:       "us-east-1",
					Scenario:     scenarioName,
					TestTimeout:  10 * time.Minute,
					CleanupDelay: 30 * time.Second,
				}
				
				framework := NewTestFramework(TestMatrix{})
				testName := fmt.Sprintf("bench-%s-%d", scenarioName, i)
				ctx := context.Background()
				
				result := framework.runSingleTest(ctx, testName, testConfig)
				if !result.Success {
					b.Errorf("Benchmark test failed: %v", result.Error)
				}
			}
		})
	}
}
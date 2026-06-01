package harness

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jlgore/corkscrew/test/harness/reporting"
	"github.com/jlgore/corkscrew/test/harness/safety"
	"github.com/stretchr/testify/require"
)

var (
	provider   = flag.String("provider", "aws", "Provider to test (aws, azure, gcp, kubernetes)")
	scenario   = flag.String("scenario", "simple-s3", "Scenario to run")
	region     = flag.String("region", "us-east-1", "Region to test")
	testID     = flag.String("testid", "", "Test ID (auto-generated if empty)")
	keepOnFail = flag.Bool("keepOnFail", false, "Keep resources on failure")
	timeout    = flag.Duration("timeout", 15*time.Minute, "Maximum test duration")
	dryRun     = flag.Bool("dry-run", false, "Run safety checks only")
	accountID  = flag.String("account-id", "", "AWS Account ID for safety checks")
)

// TestProviderIntegration is the main integration test entry point
func TestProviderIntegration(t *testing.T) {
	flag.Parse()

	// Generate test ID if not provided
	if *testID == "" {
		*testID = fmt.Sprintf("test-%d", time.Now().Unix())
	}

	ctx := context.Background()

	// Create safety guardian
	guardian, err := safety.NewGuardian(ctx, safety.GuardianConfig{
		Region:           *region,
		MaxCostThreshold: 5.0, // $5 threshold
		MaxTestDuration:  *timeout,
		AccountID:        *accountID,
	})
	require.NoError(t, err, "Failed to create safety guardian")

	// Run safety checks
	t.Logf("🛡️ Running safety checks for test %s", *testID)
	safetyChecks, err := guardian.RunSafetyChecks(*testID)
	require.NoError(t, err, "Safety checks failed")

	safetyReport := guardian.GenerateSafetyReport(*testID, safetyChecks)
	safetyReport.Print()

	// Check if any safety checks failed
	if safetyReport.OverallStatus == safety.CheckFailed {
		t.Fatalf("Safety checks failed - aborting test")
	}

	if safetyReport.OverallStatus == safety.CheckWarning {
		t.Logf("Safety checks passed with warnings - proceeding")
	}

	// If dry run, stop here
	if *dryRun {
		t.Logf("Dry run completed - safety checks passed")
		return
	}

	// Set up timeout guard
	timeoutGuard := safety.NewTimeoutGuard(*timeout, func() {
		t.Logf("Test timeout - initiating emergency cleanup")
		emergencyCleanup, err := safety.NewEmergencyCleanup(*region)
		if err != nil {
			t.Logf("Failed to create emergency cleanup: %v", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := emergencyCleanup.CleanupTestResources(ctx, *testID); err != nil {
			t.Logf("Emergency cleanup failed: %v", err)
		}
	})
	timeoutGuard.Start()
	defer timeoutGuard.Stop()

	// Create test orchestrator
	orchestrator, err := NewTestOrchestrator(timeoutGuard.Context(), OrchestratorConfig{
		Provider:      *provider,
		Region:        *region,
		ScenarioName:  *scenario,
		TestID:        *testID,
		CorkscrewPath: "../../corkscrew",
		DBPath:        fmt.Sprintf("test_%s-%s.db", *scenario, *testID),
		KeepOnFail:    *keepOnFail,
		Timeout:       *timeout,
		CleanupDelay:  30 * time.Second,
	})
	require.NoError(t, err, "Failed to create test orchestrator")

	// Create cost alert
	if err := guardian.CreateCostAlert(*testID, 10.0); err != nil {
		t.Logf("Warning: Failed to create cost alert: %v", err)
	} else {
		// Ensure cost alert cleanup
		defer func() {
			if err := guardian.CleanupCostAlert(*testID); err != nil {
				t.Logf("Warning: Failed to cleanup cost alert: %v", err)
			}
		}()
	}

	// Run the test
	t.Logf("🚀 Starting integration test: %s/%s in %s", *provider, *scenario, *region)
	result, err := orchestrator.RunTest(timeoutGuard.Context())

	// Handle timeout
	if timeoutGuard.IsTimedOut() {
		t.Fatalf("Test timed out after %v", *timeout)
	}

	// Generate comprehensive reports regardless of test outcome
	if err := generateTestReports(result); err != nil {
		t.Logf("Warning: Failed to generate reports: %v", err)
	}

	// Assert test success
	require.NoError(t, err, "Integration test failed")
	require.True(t, result.Success, "Test verification failed: %s", result.Summary())

	t.Logf("✅ Integration test completed successfully")
	t.Logf("   Duration: %v", result.Duration)
	t.Logf("   Resources: %d deployed, %d scanned, %d verified",
		result.Metrics.ResourcesDeployed,
		result.Metrics.ResourcesScanned,
		result.Metrics.ResourcesVerified)

	if result.VerificationResult != nil {
		t.Logf("   Success rate: %.1f%%", result.VerificationResult.GetSuccessRate())
	}
}

// TestEmergencyCleanup tests the emergency cleanup functionality
func TestEmergencyCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping emergency cleanup test in short mode")
	}

	flag.Parse()

	ctx := context.Background()

	emergencyCleanup, err := safety.NewEmergencyCleanup(*region)
	require.NoError(t, err, "Failed to create emergency cleanup")

	// Test pattern for cleanup (should not match any real resources in test)
	testPattern := "emergency-cleanup-test-nonexistent"

	t.Logf("🚨 Testing emergency cleanup with pattern: %s", testPattern)

	err = emergencyCleanup.CleanupTestResources(ctx, testPattern)
	require.NoError(t, err, "Emergency cleanup test failed")

	t.Logf("✅ Emergency cleanup test completed")
}

// BenchmarkTestExecution benchmarks the test execution performance
func BenchmarkTestExecution(b *testing.B) {
	flag.Parse()

	if *scenario != "simple-s3" {
		b.Skip("Benchmark only runs on simple-s3 scenario")
	}

	for i := 0; i < b.N; i++ {
		testID := fmt.Sprintf("bench-%d-%d", time.Now().Unix(), i)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		orchestrator, err := NewTestOrchestrator(ctx, OrchestratorConfig{
			Provider:      *provider,
			Region:        *region,
			ScenarioName:  *scenario,
			TestID:        testID,
			CorkscrewPath: "../../corkscrew",
			DBPath:        fmt.Sprintf("bench_%s-%s.db", *scenario, testID),
			KeepOnFail:    false, // Never keep resources in benchmark
			Timeout:       10 * time.Minute,
			CleanupDelay:  10 * time.Second, // Shorter delay for benchmarks
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

		// Record custom metrics
		b.ReportMetric(float64(result.Duration.Milliseconds()), "ms/test")
		b.ReportMetric(float64(result.Metrics.ResourcesDeployed), "resources_deployed")
		b.ReportMetric(float64(result.Metrics.ResourcesScanned), "resources_scanned")
		b.ReportMetric(float64(result.Metrics.DatabaseSize), "db_bytes")
	}
}

// generateTestReports creates comprehensive test reports
func generateTestReports(result *TestResult) error {
	if result == nil {
		return fmt.Errorf("no test result to report")
	}

	// Create reports directory
	reportsDir := filepath.Join("results", "reports")
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		return fmt.Errorf("failed to create reports directory: %w", err)
	}

	// Generate comprehensive reports
	generator := reporting.NewReportGenerator(reportsDir)
	if err := generator.GenerateReport(result); err != nil {
		return fmt.Errorf("failed to generate reports: %w", err)
	}

	// Generate GitHub PR comment format
	prComment := generator.GeneratePRComment(result)

	// Save PR comment to file for CI/CD
	prCommentFile := filepath.Join(reportsDir, "pr_comment.md")
	if err := os.WriteFile(prCommentFile, []byte(prComment), 0644); err != nil {
		return fmt.Errorf("failed to save PR comment: %w", err)
	}

	// Save JSON result for CI/CD matrix reports
	resultFile := filepath.Join("results", fmt.Sprintf("test_results_%s-%s.json", result.ScenarioName, result.TestID))
	if err := SaveTestResult(result, resultFile); err != nil {
		return fmt.Errorf("failed to save test result: %w", err)
	}

	return nil
}

// Example usage test
func ExampleTestProviderIntegration() {
	// This example shows how to run the integration test programmatically

	// Set test parameters
	*provider = "aws"
	*scenario = "simple-s3"
	*region = "us-east-1"
	*testID = "example-test"
	*keepOnFail = false
	*timeout = 10 * time.Minute

	// Run in a test context
	// Note: In practice, this would be called by the testing framework
	// TestProviderIntegration(t)

	fmt.Println("Example integration test configuration complete")
	// Output: Example integration test configuration complete
}

// Helper functions for test utilities

// SaveTestResult saves the test result to a JSON file
func SaveTestResult(result *TestResult, filename string) error {
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Save as JSON
	return result.SaveToFile(filename)
}

// LoadTestResult loads a test result from a JSON file
func LoadTestResult(filename string) (*TestResult, error) {
	var result TestResult
	if err := result.LoadFromFile(filename); err != nil {
		return nil, err
	}
	return &result, nil
}

// CleanupTestArtifacts removes test databases and temporary files
func CleanupTestArtifacts(testID string) error {
	patterns := []string{
		fmt.Sprintf("test_*-%s.db", testID),
		fmt.Sprintf("test_*-%s.db.wal", testID),
		fmt.Sprintf("*-%s.log", testID),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				fmt.Printf("Warning: Failed to remove %s: %v\n", match, err)
			}
		}
	}

	return nil
}

// Test setup and teardown helpers

func TestMain(m *testing.M) {
	// Global test setup
	flag.Parse()

	// Ensure corkscrew binary exists
	if _, err := os.Stat("../../corkscrew"); os.IsNotExist(err) {
		fmt.Println("❌ Corkscrew binary not found. Run 'make build' first.")
		os.Exit(1)
	}

	// Create results directory
	if err := os.MkdirAll("results", 0755); err != nil {
		fmt.Printf("❌ Failed to create results directory: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Global test cleanup
	// Note: Individual test cleanup is handled in each test
	// This is for any global cleanup that might be needed

	os.Exit(code)
}

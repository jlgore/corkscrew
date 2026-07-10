//go:build integration

package harness

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/jlgore/corkscrew/test/harness/scenarios"
	"github.com/jlgore/corkscrew/test/harness/verification"
)

// TestOrchestrator coordinates the entire test workflow
type TestOrchestrator struct {
	harness  *automation.TestHarness
	verifier *verification.Verifier
	config   OrchestratorConfig
	scenario automation.Scenario
}

// OrchestratorConfig contains configuration for the test orchestrator
type OrchestratorConfig struct {
	Provider      string
	Region        string
	ScenarioName  string
	TestID        string
	CorkscrewPath string
	DBPath        string
	KeepOnFail    bool
	Timeout       time.Duration
	CleanupDelay  time.Duration
}

// NewTestOrchestrator creates a new test orchestrator
func NewTestOrchestrator(ctx context.Context, config OrchestratorConfig) (*TestOrchestrator, error) {
	// Get scenario from registry
	scenario, err := scenarios.DefaultRegistry.Get(config.ScenarioName)
	if err != nil {
		return nil, fmt.Errorf("failed to get scenario: %w", err)
	}

	// Create test harness
	harnessConfig := automation.HarnessConfig{
		Provider:      config.Provider,
		Region:        config.Region,
		Scenario:      config.ScenarioName,
		TestID:        config.TestID,
		KeepOnFail:    config.KeepOnFail,
		Timeout:       config.Timeout,
		CorkscrewPath: config.CorkscrewPath,
		DBPath:        config.DBPath,
	}

	harness, err := automation.NewTestHarness(ctx, harnessConfig, scenario)
	if err != nil {
		return nil, fmt.Errorf("failed to create test harness: %w", err)
	}

	return &TestOrchestrator{
		harness:  harness,
		config:   config,
		scenario: scenario,
	}, nil
}

// RunTest executes the complete test workflow
func (o *TestOrchestrator) RunTest(ctx context.Context) (*TestResult, error) {
	result := &TestResult{
		TestID:       o.config.TestID,
		ScenarioName: o.scenario.GetName(),
		Provider:     o.config.Provider,
		Region:       o.config.Region,
		StartTime:    time.Now(),
		Phase:        "initialization",
		Metrics:      TestMetrics{},
	}

	fmt.Printf("🚀 Starting integration test: %s\n", o.scenario.GetName())
	fmt.Printf("   Provider: %s, Region: %s, TestID: %s\n", o.config.Provider, o.config.Region, o.config.TestID)

	// Phase 1: Deploy infrastructure
	fmt.Println("\n📦 Phase 1: Deploying test infrastructure...")
	result.Phase = "deploy"
	deployStart := time.Now()

	if err := o.harness.Deploy(); err != nil {
		result.Error = fmt.Errorf("deployment failed: %w", err)
		return result, result.Error
	}

	result.DeploymentDuration = time.Since(deployStart)
	result.DeploymentOutputs = o.harness.GetOutputs()
	result.Metrics.ResourcesDeployed = o.countDeployedResources(result.DeploymentOutputs)

	fmt.Printf("✅ Deployment complete in %v\n", result.DeploymentDuration)

	// Wait for resources to stabilize
	if o.config.CleanupDelay > 0 {
		fmt.Printf("⏳ Waiting %v for resources to stabilize...\n", o.config.CleanupDelay)
		time.Sleep(o.config.CleanupDelay)
	} else {
		fmt.Println("⏳ Waiting 30s for resources to stabilize...")
		time.Sleep(30 * time.Second)
	}

	// Phase 2: Run Corkscrew scan
	fmt.Println("\n🔍 Phase 2: Running Corkscrew scan...")
	result.Phase = "scan"

	scanResult, err := o.harness.Scan()
	if err != nil {
		result.Error = fmt.Errorf("corkscrew scan failed: %w", err)
		result.ScanResult = scanResult
		return result, result.Error
	}

	result.ScanResult = scanResult
	result.Metrics.ScanDuration = scanResult.Duration
	fmt.Printf("✅ Scan complete in %v\n", scanResult.Duration)

	// Phase 3: Verify results
	fmt.Println("\n✅ Phase 3: Verifying results in DuckDB...")
	result.Phase = "verify"
	verifyStart := time.Now()

	verifyResult, err := o.verifyResults(ctx)
	if err != nil {
		result.Error = fmt.Errorf("verification failed: %w", err)
		return result, result.Error
	}

	result.VerificationResult = verifyResult
	result.Metrics.VerificationDuration = time.Since(verifyStart)
	result.Metrics.ResourcesScanned = verifyResult.TotalFound
	result.Metrics.ResourcesVerified = len(verifyResult.Matches)

	// Get database statistics
	if dbStats, err := o.getDatabaseStats(ctx); err == nil {
		if dbSize, ok := dbStats["database_size"].(int64); ok {
			result.Metrics.DatabaseSize = dbSize
		}
	}

	fmt.Printf("✅ Verification complete in %v\n", result.Metrics.VerificationDuration)

	// Phase 4: Cleanup (unless keeping on fail and test failed)
	result.Phase = "cleanup"
	shouldCleanup := !o.config.KeepOnFail || verifyResult.AllPassed()

	if shouldCleanup {
		fmt.Println("\n🧹 Phase 4: Cleaning up test infrastructure...")
		cleanupErr := o.harness.Destroy()

		// Capture cleanup result
		result.CleanupResult = o.harness.GetCleanupResult()

		if cleanupErr != nil {
			fmt.Printf("⚠️ Warning: Cleanup failed: %v\n", cleanupErr)
			// Don't fail the test for cleanup errors, but record them
			result.Error = cleanupErr
		}
	} else {
		fmt.Printf("\n🔧 Keeping resources for debugging. Stack: %s\n", o.harness.GetStackName())
		fmt.Printf("   Database: %s\n", o.harness.GetDBPath())
		fmt.Printf("   To cleanup manually: pulumi destroy -s %s\n", o.harness.GetStackName())
	}

	// Calculate final results
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Success = verifyResult.AllPassed()

	// Print summary
	o.printTestSummary(result)

	return result, nil
}

// verifyResults performs verification against the DuckDB database
func (o *TestOrchestrator) verifyResults(ctx context.Context) (*VerificationResult, error) {
	// Create verifier with the test database
	verifier, err := verification.NewVerifierWithPath(o.harness.GetDBPath())
	if err != nil {
		return nil, fmt.Errorf("failed to create verifier: %w", err)
	}
	defer verifier.Close()

	o.verifier = verifier

	// Get expected resources from the scenario
	expectedResources := o.harness.GetExpectedResources()

	// Perform enhanced verification
	enhancedResult, err := verifier.VerifyResources(ctx, expectedResources)
	if err != nil {
		return nil, fmt.Errorf("resource verification failed: %w", err)
	}

	// Verify relationships if defined
	relationshipChecks, err := verifier.VerifyRelationships(ctx, expectedResources)
	if err != nil {
		fmt.Printf("⚠️ Warning: Relationship verification failed: %v\n", err)
		// Don't fail the test for relationship verification errors
	}

	// Convert enhanced result to standard VerificationResult
	result := &VerificationResult{
		TotalExpected:      enhancedResult.TotalExpected,
		TotalFound:         enhancedResult.TotalFound,
		TotalMissing:       enhancedResult.TotalMissing,
		Matches:            convertMatches(enhancedResult.Matches),
		Missing:            convertMissing(enhancedResult.Missing),
		AttributeChecks:    convertAttributeChecks(enhancedResult.AttributeChecks),
		RelationshipChecks: convertRelationshipChecks(relationshipChecks),
		Success:            enhancedResult.Success,
		Details:            o.generateVerificationDetails(enhancedResult),
	}

	return result, nil
}

// getDatabaseStats retrieves database statistics
func (o *TestOrchestrator) getDatabaseStats(ctx context.Context) (map[string]interface{}, error) {
	if o.verifier == nil {
		return nil, fmt.Errorf("verifier not initialized")
	}

	stats, err := o.verifier.GetDatabaseStats(ctx)
	if err != nil {
		return nil, err
	}

	// Add database file size
	if dbInfo, err := os.Stat(o.harness.GetDBPath()); err == nil {
		stats["database_size"] = dbInfo.Size()
	}

	return stats, nil
}

// countDeployedResources counts the number of resources deployed based on outputs
func (o *TestOrchestrator) countDeployedResources(outputs map[string]interface{}) int {
	count := 0

	if expectedResources, ok := outputs["expectedResources"].(map[string]interface{}); ok {
		for _, serviceResources := range expectedResources {
			if resourceArray, ok := serviceResources.([]interface{}); ok {
				count += len(resourceArray)
			}
		}
	}

	return count
}

// generateVerificationDetails creates a detailed verification report
func (o *TestOrchestrator) generateVerificationDetails(result *verification.EnhancedVerificationResult) string {
	details := fmt.Sprintf("Verification Summary:\n")
	details += fmt.Sprintf("- Total Expected: %d\n", result.TotalExpected)
	details += fmt.Sprintf("- Total Found: %d\n", result.TotalFound)
	details += fmt.Sprintf("- Total Missing: %d\n", result.TotalMissing)
	details += fmt.Sprintf("- Success Rate: %.1f%%\n", result.GetSuccessRate())
	details += fmt.Sprintf("- Duration: %v\n", result.Duration)

	if len(result.Missing) > 0 {
		details += "\nMissing Resources:\n"
		for _, missing := range result.Missing {
			details += fmt.Sprintf("- %s: %s\n", missing.Type, missing.Name)
		}
	}

	if len(result.Errors) > 0 {
		details += "\nErrors:\n"
		for _, err := range result.Errors {
			details += fmt.Sprintf("- %s\n", err)
		}
	}

	return details
}

// printTestSummary prints a comprehensive test summary
func (o *TestOrchestrator) printTestSummary(result *TestResult) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Printf("🧪 TEST SUMMARY: %s\n", result.ScenarioName)
	fmt.Println(strings.Repeat("=", 80))

	status := "❌ FAILED"
	if result.Success {
		status = "✅ PASSED"
	}

	fmt.Printf("Status: %s\n", status)
	fmt.Printf("Duration: %v\n", result.Duration)
	fmt.Printf("Test ID: %s\n", result.TestID)
	fmt.Printf("Provider: %s\n", result.Provider)
	fmt.Printf("Region: %s\n", result.Region)

	fmt.Println("\n📊 METRICS:")
	fmt.Printf("- Deployment Time: %v\n", result.DeploymentDuration)
	fmt.Printf("- Scan Time: %v\n", result.Metrics.ScanDuration)
	fmt.Printf("- Verification Time: %v\n", result.Metrics.VerificationDuration)
	fmt.Printf("- Resources Deployed: %d\n", result.Metrics.ResourcesDeployed)
	fmt.Printf("- Resources Scanned: %d\n", result.Metrics.ResourcesScanned)
	fmt.Printf("- Resources Verified: %d\n", result.Metrics.ResourcesVerified)

	if result.Metrics.DatabaseSize > 0 {
		fmt.Printf("- Database Size: %.2f KB\n", float64(result.Metrics.DatabaseSize)/1024)
	}

	if result.VerificationResult != nil {
		fmt.Println("\n🔍 VERIFICATION RESULTS:")
		vr := result.VerificationResult
		fmt.Printf("- Success Rate: %.1f%% (%d/%d)\n", vr.GetSuccessRate(), vr.TotalFound, vr.TotalExpected)
		fmt.Printf("- Attribute Checks: %d\n", len(vr.AttributeChecks))
		fmt.Printf("- Relationship Checks: %d\n", len(vr.RelationshipChecks))

		if vr.TotalMissing > 0 {
			fmt.Printf("- Missing Resources: %d\n", vr.TotalMissing)
		}
	}

	if result.CleanupResult != nil {
		fmt.Println("\n🧹 CLEANUP RESULTS:")
		cr := result.CleanupResult
		fmt.Printf("- Pulumi Success: %t\n", cr.PulumiSuccess)
		fmt.Printf("- AWS Verified: %t\n", cr.AWSVerified)
		fmt.Printf("- Cleanup Duration: %v\n", cr.CleanupDuration)

		if len(cr.ResourcesFound) > 0 {
			fmt.Printf("- Remaining Resources: %d\n", len(cr.ResourcesFound))
		}

		if len(cr.ManualCleanup) > 0 {
			successful := 0
			for _, action := range cr.ManualCleanup {
				if action.Success {
					successful++
				}
			}
			fmt.Printf("- Manual Cleanup Actions: %d/%d successful\n", successful, len(cr.ManualCleanup))
		}
	}

	if result.Error != nil {
		fmt.Printf("\n❌ ERROR: %v\n", result.Error)
	}

	fmt.Println(strings.Repeat("=", 80))
}

// Helper functions for converting between verification result types

func convertMatches(matches []verification.ResourceMatch) []ResourceMatch {
	result := make([]ResourceMatch, len(matches))
	for i, match := range matches {
		result[i] = ResourceMatch{
			Expected:       convertExpectedResource(match.Expected),
			Actual:         match.Actual,
			Match:          match.Match,
			AttributeScore: match.AttributeScore,
		}
	}
	return result
}

func convertMissing(missing []verification.ExpectedResource) []ExpectedResource {
	result := make([]ExpectedResource, len(missing))
	for i, res := range missing {
		result[i] = convertExpectedResource(res)
	}
	return result
}

func convertExpectedResource(res verification.ExpectedResource) ExpectedResource {
	return ExpectedResource{
		Type:       res.Type,
		Name:       res.Name,
		ARN:        res.ARN,
		ID:         res.ID,
		Region:     res.Region,
		Attributes: res.Attributes,
		Tags:       res.Tags,
	}
}

func convertAttributeChecks(checks []verification.AttributeCheck) []AttributeCheck {
	result := make([]AttributeCheck, len(checks))
	for i, check := range checks {
		result[i] = AttributeCheck{
			ResourceID:  check.ResourceID,
			Attribute:   check.Attribute,
			Expected:    check.Expected,
			Actual:      check.Actual,
			Match:       check.Match,
			Description: check.Description,
		}
	}
	return result
}

func convertRelationshipChecks(checks []verification.RelationshipCheck) []RelationshipCheck {
	result := make([]RelationshipCheck, len(checks))
	for i, check := range checks {
		result[i] = RelationshipCheck{
			FromResource:     check.FromResource,
			ToResource:       check.ToResource,
			RelationshipType: check.RelationshipType,
			Expected:         check.Expected,
			Found:            check.Found,
			Description:      check.Description,
		}
	}
	return result
}

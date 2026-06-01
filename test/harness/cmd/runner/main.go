package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jlgore/corkscrew/test/harness"
	"github.com/jlgore/corkscrew/test/harness/scenarios"
)

func main() {
	var (
		provider      = flag.String("provider", "aws", "Provider to test")
		region        = flag.String("region", "us-east-1", "Region to test")
		scenario      = flag.String("scenario", "simple-s3", "Scenario to run")
		keepOnFail    = flag.Bool("keep-on-fail", false, "Keep resources on failure")
		timeout       = flag.Duration("timeout", 15*time.Minute, "Test timeout")
		corkscrewPath = flag.String("corkscrew", "../../../../corkscrew", "Path to corkscrew binary")
		listScenarios = flag.Bool("list-scenarios", false, "List available scenarios")
		outputFormat  = flag.String("output", "text", "Output format: text, json")
		verbose       = flag.Bool("verbose", false, "Verbose output")
	)
	flag.Parse()

	if *listScenarios {
		listAvailableScenarios()
		return
	}

	// Validate scenario exists
	registry := scenarios.DefaultRegistry
	if _, err := registry.Get(*scenario); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Available scenarios:\n")
		for _, name := range registry.List() {
			fmt.Fprintf(os.Stderr, "  - %s\n", name)
		}
		os.Exit(1)
	}

	// Create test configuration
	testID := fmt.Sprintf("cli-%d", time.Now().Unix())
	config := harness.TestConfig{
		Provider:      *provider,
		Region:        *region,
		ScenarioName:  *scenario,
		TestID:        testID,
		DBPath:        fmt.Sprintf("test_%s-%s.db", *scenario, testID),
		CorkscrewPath: *corkscrewPath,
		KeepOnFail:    *keepOnFail,
		Timeout:       *timeout,
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if *verbose {
		fmt.Printf("Starting Corkscrew integration test\n")
		fmt.Printf("Provider: %s\n", *provider)
		fmt.Printf("Region: %s\n", *region)
		fmt.Printf("Scenario: %s\n", *scenario)
		fmt.Printf("Test ID: %s\n", testID)
		fmt.Printf("Timeout: %s\n", *timeout)
		fmt.Printf("Corkscrew Path: %s\n", *corkscrewPath)
		fmt.Println()
	}

	// Create and run test orchestrator
	orchestrator, err := harness.NewTestOrchestrator(ctx, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create test orchestrator: %v\n", err)
		os.Exit(1)
	}

	// Ensure cleanup
	defer func() {
		if err := orchestrator.Cleanup(); err != nil && *verbose {
			fmt.Printf("Cleanup warning: %v\n", err)
		}
		// Clean up database file unless keeping on failure
		if !*keepOnFail {
			os.Remove(config.DBPath)
		}
	}()

	// Run the test
	result, err := orchestrator.RunTest(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Test execution failed: %v\n", err)
		if result != nil {
			outputResult(result, *outputFormat)
		}
		os.Exit(1)
	}

	// Output results
	outputResult(result, *outputFormat)

	// Exit with appropriate code
	if result.Success {
		if *verbose {
			fmt.Println("\n✅ Test completed successfully!")
		}
		os.Exit(0)
	} else {
		if *verbose {
			fmt.Println("\n❌ Test failed!")
		}
		os.Exit(1)
	}
}

func listAvailableScenarios() {
	registry := scenarios.DefaultRegistry
	scenarioInfo := registry.GetScenarioInfo()

	fmt.Println("Available Test Scenarios:")
	fmt.Println("========================")

	for name, info := range scenarioInfo {
		fmt.Printf("\n%s:\n", name)
		fmt.Printf("  Description: %s\n", info.Description)
		fmt.Printf("  Services: %v\n", info.Services)
	}
}

func outputResult(result *harness.TestResult, format string) {
	switch format {
	case "json":
		outputJSON(result)
	case "text":
		outputText(result)
	default:
		fmt.Fprintf(os.Stderr, "Unknown output format: %s\n", format)
		outputText(result)
	}
}

func outputJSON(result *harness.TestResult) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal result to JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

func outputText(result *harness.TestResult) {
	fmt.Printf("Test Results\n")
	fmt.Printf("============\n")
	fmt.Printf("Test ID: %s\n", result.TestID)
	fmt.Printf("Scenario: %s\n", result.ScenarioName)
	fmt.Printf("Provider: %s\n", result.Provider)
	fmt.Printf("Region: %s\n", result.Region)
	fmt.Printf("Duration: %s\n", result.Duration)
	fmt.Printf("Success: %t\n", result.Success)

	if result.Error != nil {
		fmt.Printf("Error: %v\n", result.Error)
		fmt.Printf("Failed Phase: %s\n", result.Phase)
	}

	fmt.Printf("\nMetrics:\n")
	fmt.Printf("--------\n")
	fmt.Printf("Resources Deployed: %d\n", result.Metrics.ResourcesDeployed)
	fmt.Printf("Resources Scanned: %d\n", result.Metrics.ResourcesScanned)
	fmt.Printf("Resources Verified: %d\n", result.Metrics.ResourcesVerified)
	fmt.Printf("Deployment Duration: %s\n", result.DeploymentDuration)
	fmt.Printf("Scan Duration: %s\n", result.Metrics.ScanDuration)
	fmt.Printf("Verification Duration: %s\n", result.Metrics.VerificationDuration)
	fmt.Printf("Database Size: %d bytes\n", result.Metrics.DatabaseSize)

	if result.VerificationResult != nil {
		vr := result.VerificationResult
		fmt.Printf("\nVerification:\n")
		fmt.Printf("-------------\n")
		fmt.Printf("Expected Resources: %d\n", vr.TotalExpected)
		fmt.Printf("Found Resources: %d\n", vr.TotalFound)
		fmt.Printf("Missing Resources: %d\n", vr.TotalMissing)
		fmt.Printf("Success Rate: %.1f%%\n", vr.GetSuccessRate())
		fmt.Printf("Attribute Checks: %d\n", len(vr.AttributeChecks))
		fmt.Printf("Relationship Checks: %d\n", len(vr.RelationshipChecks))

		if len(vr.Missing) > 0 {
			fmt.Printf("\nMissing Resources:\n")
			for _, miss := range vr.Missing {
				fmt.Printf("  - %s: %s (ID: %s)\n", miss.Type, miss.Name, miss.ID)
			}
		}

		if len(vr.AttributeChecks) > 0 {
			failedChecks := 0
			for _, check := range vr.AttributeChecks {
				if !check.Match {
					failedChecks++
				}
			}
			if failedChecks > 0 {
				fmt.Printf("\nFailed Attribute Checks: %d\n", failedChecks)
			}
		}
	}

	if result.ScanResult != nil {
		sr := result.ScanResult
		fmt.Printf("\nScan Result:\n")
		fmt.Printf("------------\n")
		fmt.Printf("Exit Code: %d\n", sr.ExitCode)
		fmt.Printf("Duration: %s\n", sr.Duration)
		if sr.Error != nil {
			fmt.Printf("Scan Error: %v\n", sr.Error)
		}
	}
}

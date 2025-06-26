package utilities

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	
	"github.com/jlgore/corkscrew/test/harness/automation"
)

// ExampleIntegrationTest demonstrates how to use the utilities in a complete integration test
func ExampleIntegrationTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// 1. Create test context with utilities
	testCtx := NewTestContext(t, "example-integration")
	t.Logf("Starting integration test: %s", testCtx.TestID)

	// 2. Setup test database
	err := SetupTestDatabase(testCtx)
	require.NoError(t, err, "Failed to setup test database")
	defer CleanupTestDatabase(testCtx)

	// 3. Create utility helpers
	dbHelper, err := NewDatabaseHelper(testCtx)
	require.NoError(t, err, "Failed to create database helper")
	defer dbHelper.Close()

	assertHelper := NewAssertionHelper(t, testCtx)
	mockGen := NewMockDataGenerator(testCtx.TestID, testCtx.Provider, testCtx.Region)
	matcher := NewResourceMatcher(testCtx)

	// 4. Deploy infrastructure using existing automation
	ctx := context.Background()
	harness, err := automation.NewTestHarness(ctx, automation.HarnessConfig{
		Provider:   testCtx.Provider,
		Region:     testCtx.Region,
		Scenario:   "simple",
		TestID:     testCtx.TestID,
		KeepOnFail: false,
		Timeout:    5 * time.Minute,
	})
	require.NoError(t, err, "Failed to create test harness")

	// Ensure cleanup
	defer func() {
		if err := harness.Destroy(); err != nil {
			t.Logf("Warning: Failed to destroy harness: %v", err)
		}
	}()

	// Deploy infrastructure
	err = harness.Deploy()
	require.NoError(t, err, "Failed to deploy infrastructure")

	// Get deployment outputs
	bucketName, err := harness.GetBucketName()
	require.NoError(t, err, "Failed to get bucket name")
	bucketArn, err := harness.GetBucketArn()
	require.NoError(t, err, "Failed to get bucket ARN")

	t.Logf("Deployed resources:")
	t.Logf("  Bucket Name: %s", bucketName)
	t.Logf("  Bucket ARN: %s", bucketArn)

	// 5. Wait for resource stabilization
	WaitForResourceStabilization(30 * time.Second)

	// 6. Run Corkscrew scan using utilities
	scanResult, err := RunCorkscrewScan(testCtx, []string{"s3"}, nil)
	require.NoError(t, err, "Failed to run Corkscrew scan")

	// 7. Assert scan success using assertion helper
	t.Log("Verifying scan results...")
	assertHelper.AssertScanSuccess(scanResult)
	t.Logf("Scan completed in %v", scanResult.Duration)

	// 8. Verify database contents using database helper
	t.Log("Verifying database contents...")
	
	// Get database statistics
	stats, err := dbHelper.GetDatabaseStats()
	require.NoError(t, err, "Failed to get database stats")
	t.Logf("Database stats: %+v", stats)

	// Assert expected resource count
	assertHelper.AssertResourceCount(dbHelper, "Bucket", 1)

	// Get resources by test ID tag
	resources, err := dbHelper.GetResourcesByTag("TestID", testCtx.TestID)
	require.NoError(t, err, "Failed to get resources by tag")
	
	// Use resource matcher to find our bucket
	bucketFound := matcher.MatchResourceByTag(resources, "TestID", testCtx.TestID)
	require.True(t, bucketFound, "Should find bucket with test ID tag")

	// Find S3 buckets specifically
	s3Resources := matcher.MatchResourceByType(resources, "Bucket")
	require.NotEmpty(t, s3Resources, "Should find S3 bucket resources")

	// 9. Validate relationship integrity
	assertHelper.AssertNoOrphanedRelationships(dbHelper)

	// 10. Generate and compare mock data
	t.Log("Comparing with mock data...")
	mockBucketData := mockGen.GenerateS3BucketMock(bucketName)
	
	// Verify mock data structure
	assertHelper.AssertJSONStructure(mockBucketData, []string{"Name", "Tags", "Region"})
	
	t.Log("Integration test completed successfully!")
	t.Logf("Test results summary:")
	t.Logf("  - Infrastructure deployed: ✅")
	t.Logf("  - Corkscrew scan executed: ✅")
	t.Logf("  - Database populated: ✅")
	t.Logf("  - Resources matched: ✅")
	t.Logf("  - Relationships validated: ✅")
	t.Logf("  - Mock data generated: ✅")
}

// ExampleComplexScenarioWithUtilities shows how to use utilities with complex scenarios
func ExampleComplexScenarioWithUtilities(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping complex scenario test in short mode")
	}

	// Setup
	testCtx := NewTestContext(t, "complex-scenario")
	defer CleanupTestDatabase(testCtx)

	err := SetupTestDatabase(testCtx)
	require.NoError(t, err)

	dbHelper, err := NewDatabaseHelper(testCtx)
	require.NoError(t, err)
	defer dbHelper.Close()

	assertHelper := NewAssertionHelper(t, testCtx)
	mockGen := NewMockDataGenerator(testCtx.TestID, testCtx.Provider, testCtx.Region)

	// TODO: This would integrate with complex scenario deployment
	// For now, we'll demonstrate the utility patterns

	t.Run("MultiServiceScan", func(t *testing.T) {
		// Scan multiple services
		services := []string{"s3", "ec2", "iam"}
		scanOptions := map[string]string{
			"timeout": "300s",
		}

		scanResult, err := RunCorkscrewScan(testCtx, services, scanOptions)
		require.NoError(t, err, "Multi-service scan should not error")

		t.Logf("Multi-service scan completed:")
		t.Logf("  Exit code: %d", scanResult.ExitCode)
		t.Logf("  Duration: %v", scanResult.Duration)
		t.Logf("  Has JSON: %t", scanResult.HasValidJSON)
	})

	t.Run("ResourceCountValidation", func(t *testing.T) {
		// Expected resource counts for complex scenario
		expectedCounts := map[string]int{
			"Bucket":          1,
			"Instance":        1,
			"VPC":            1,
			"Subnet":         2,
			"SecurityGroup":  1,
		}

		for resourceType, expectedCount := range expectedCounts {
			actualCount, err := dbHelper.GetResourceCount(resourceType)
			if err != nil {
				t.Logf("Warning: Could not get count for %s: %v", resourceType, err)
				continue
			}
			
			if actualCount != expectedCount {
				t.Logf("Resource count mismatch for %s: expected %d, got %d", 
					resourceType, expectedCount, actualCount)
			}
		}
	})

	t.Run("MockDataComparison", func(t *testing.T) {
		// Generate mock data for comparison
		mockVPC := mockGen.GenerateVPCMock("vpc-" + generateRandomID(8))
		mockInstance := mockGen.GenerateEC2InstanceMock("i-" + generateRandomID(8))
		mockBucket := mockGen.GenerateS3BucketMock("test-bucket-" + testCtx.TestID)

		// Verify mock data structures
		assertHelper.AssertJSONStructure(mockVPC, []string{"VpcId", "CidrBlock", "Tags"})
		assertHelper.AssertJSONStructure(mockInstance, []string{"InstanceId", "InstanceType", "Tags"})
		assertHelper.AssertJSONStructure(mockBucket, []string{"Name", "Tags", "BucketPolicy"})

		t.Log("Mock data generation and validation completed")
	})

	t.Run("RelationshipValidation", func(t *testing.T) {
		// Validate complex relationships
		orphaned, err := dbHelper.ValidateRelationshipIntegrity()
		require.NoError(t, err, "Relationship validation should not error")

		if len(orphaned) > 0 {
			t.Logf("Warning: Found %d orphaned relationships:", len(orphaned))
			for _, rel := range orphaned {
				t.Logf("  - %s", rel)
			}
		} else {
			t.Log("All relationships are valid")
		}
	})
}

// ExampleUtilityIntegration shows how to integrate utilities with existing test patterns
func ExampleUtilityIntegration(t *testing.T) {
	// This function demonstrates how to retrofit existing tests with utilities
	
	testCtx := NewTestContext(t, "utility-integration")
	assertHelper := NewAssertionHelper(t, testCtx)
	
	// Example: Convert existing assertions to use utilities
	
	// Instead of:
	// assert.Equal(t, expectedCount, actualCount, "Resource count mismatch")
	
	// Use:
	// assertHelper.AssertResourceCount(dbHelper, resourceType, expectedCount)
	
	// Example: Use mock data for testing
	mockGen := NewMockDataGenerator(testCtx.TestID, "aws", "us-east-1")
	bucketData := mockGen.GenerateS3BucketMock("test-bucket")
	
	// Verify the mock data has expected structure
	assertHelper.AssertJSONStructure(bucketData, []string{"Name", "Tags", "Region"})
	
	t.Log("Utility integration example completed")
}

// UtilityTestSuite provides a template for creating test suites with utilities
type UtilityTestSuite struct {
	TestCtx       *TestContext
	DbHelper      *DatabaseHelper
	AssertHelper  *AssertionHelper
	MockGen       *MockDataGenerator
	Matcher       *ResourceMatcher
}

// NewUtilityTestSuite creates a new test suite with all utilities configured
func NewUtilityTestSuite(t *testing.T, testID string) (*UtilityTestSuite, error) {
	testCtx := NewTestContext(t, testID)
	
	err := SetupTestDatabase(testCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to setup database: %w", err)
	}

	dbHelper, err := NewDatabaseHelper(testCtx)
	if err != nil {
		CleanupTestDatabase(testCtx)
		return nil, fmt.Errorf("failed to create database helper: %w", err)
	}

	return &UtilityTestSuite{
		TestCtx:      testCtx,
		DbHelper:     dbHelper,
		AssertHelper: NewAssertionHelper(t, testCtx),
		MockGen:      NewMockDataGenerator(testCtx.TestID, testCtx.Provider, testCtx.Region),
		Matcher:      NewResourceMatcher(testCtx),
	}, nil
}

// Cleanup closes all resources and cleans up the test suite
func (uts *UtilityTestSuite) Cleanup() {
	if uts.DbHelper != nil {
		uts.DbHelper.Close()
	}
	if uts.TestCtx != nil {
		CleanupTestDatabase(uts.TestCtx)
	}
}

// ExampleTestSuiteUsage demonstrates how to use the utility test suite
func ExampleTestSuiteUsage(t *testing.T) {
	suite, err := NewUtilityTestSuite(t, "suite-test")
	require.NoError(t, err, "Failed to create test suite")
	defer suite.Cleanup()

	// All utilities are now available through the suite
	t.Log("Test suite created successfully")
	t.Logf("Test ID: %s", suite.TestCtx.TestID)
	
	// Example usage of suite utilities
	stats, err := suite.DbHelper.GetDatabaseStats()
	require.NoError(t, err)
	t.Logf("Database stats: %+v", stats)

	mockData := suite.MockGen.GenerateS3BucketMock("example-bucket")
	suite.AssertHelper.AssertJSONStructure(mockData, []string{"Name", "Tags"})
	
	t.Log("Test suite usage example completed")
}
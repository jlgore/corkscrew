package utilities

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUtilitiesUsageExample demonstrates how to use the test utilities
func TestUtilitiesUsageExample(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping utilities example test in short mode")
	}

	// 1. Create test context
	testCtx := NewTestContext(t, "example-test")
	t.Logf("Created test context with ID: %s", testCtx.TestID)

	// 2. Setup test database
	err := SetupTestDatabase(testCtx)
	require.NoError(t, err, "Failed to setup test database")
	defer CleanupTestDatabase(testCtx)

	// 3. Create database helper
	dbHelper, err := NewDatabaseHelper(testCtx)
	require.NoError(t, err, "Failed to create database helper")
	defer dbHelper.Close()

	// 4. Create assertion helper
	assertHelper := NewAssertionHelper(t, testCtx)

	// 5. Create mock data generator
	mockGen := NewMockDataGenerator(testCtx.TestID, testCtx.Provider, testCtx.Region)

	// 6. Generate mock data examples
	t.Run("MockDataGeneration", func(t *testing.T) {
		bucketData := mockGen.GenerateS3BucketMock("test-bucket-" + testCtx.TestID)
		assert.Contains(t, bucketData, "Name")
		assert.Contains(t, bucketData, "Tags")
		assert.Contains(t, bucketData, "Region")

		instanceData := mockGen.GenerateEC2InstanceMock("i-" + generateRandomID(8))
		assert.Contains(t, instanceData, "InstanceId")
		assert.Contains(t, instanceData, "InstanceType")
		assert.Contains(t, instanceData, "Tags")

		vpcData := mockGen.GenerateVPCMock("vpc-" + generateRandomID(8))
		assert.Contains(t, vpcData, "VpcId")
		assert.Contains(t, vpcData, "CidrBlock")
		assert.Contains(t, vpcData, "Tags")

		t.Logf("Generated mock data successfully")
	})

	// 7. Test database operations
	t.Run("DatabaseOperations", func(t *testing.T) {
		// Get database stats (should be empty initially)
		stats, err := dbHelper.GetDatabaseStats()
		require.NoError(t, err, "Failed to get database stats")

		totalResources, ok := stats["total_resources"].(int)
		require.True(t, ok, "total_resources should be an integer")

		t.Logf("Total resources in database: %d", totalResources)

		// Validate relationship integrity (should have no orphaned relationships)
		orphaned, err := dbHelper.ValidateRelationshipIntegrity()
		require.NoError(t, err, "Failed to validate relationship integrity")
		assert.Empty(t, orphaned, "Should have no orphaned relationships")

		t.Logf("Database validation passed")
	})

	// 8. Test resource matching
	t.Run("ResourceMatching", func(t *testing.T) {
		matcher := NewResourceMatcher(testCtx)

		// Create some mock resources
		resources := []map[string]interface{}{
			{
				"id":   "resource-1",
				"type": "AWS::S3::Bucket",
				"tags": map[string]interface{}{
					"TestID":      testCtx.TestID,
					"Environment": "test",
				},
			},
			{
				"id":   "resource-2",
				"type": "AWS::EC2::Instance",
				"tags": map[string]interface{}{
					"TestID":      testCtx.TestID,
					"Environment": "production",
				},
			},
		}

		// Test matching by ID
		found := matcher.MatchResourceByID(resources, "resource-1")
		assert.True(t, found, "Should find resource by ID")

		// Test matching by tag
		found = matcher.MatchResourceByTag(resources, "TestID", testCtx.TestID)
		assert.True(t, found, "Should find resource by tag")

		// Test matching by type
		s3Resources := matcher.MatchResourceByType(resources, "AWS::S3::Bucket")
		assert.Len(t, s3Resources, 1, "Should find one S3 bucket")

		t.Logf("Resource matching tests passed")
	})

	// 9. Test assertion helpers
	t.Run("AssertionHelpers", func(t *testing.T) {
		// Test JSON structure assertion
		jsonData := map[string]interface{}{
			"resources":     []interface{}{},
			"relationships": []interface{}{},
			"metadata": map[string]interface{}{
				"scan_time": time.Now().Unix(),
				"region":    testCtx.Region,
			},
		}

		expectedKeys := []string{"resources", "relationships", "metadata"}
		assertHelper.AssertJSONStructure(jsonData, expectedKeys)

		t.Logf("JSON structure assertion passed")
	})
}

// TestScanIntegration demonstrates how to use the scan utilities
func TestScanIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scan integration test in short mode")
	}

	testCtx := NewTestContext(t, "scan-integration")
	defer CleanupTestDatabase(testCtx)

	// Setup database
	err := SetupTestDatabase(testCtx)
	require.NoError(t, err, "Failed to setup test database")

	// Run a dry-run scan to test the infrastructure
	t.Run("DryRunScan", func(t *testing.T) {
		scanOptions := map[string]string{
			"dry-run": "true",
		}

		result, err := RunCorkscrewScan(testCtx, []string{"s3"}, scanOptions)
		require.NoError(t, err, "Failed to run scan")

		// The scan might not succeed in a test environment, but we can verify
		// that the infrastructure is working
		t.Logf("Scan exit code: %d", result.ExitCode)
		t.Logf("Scan duration: %v", result.Duration)
		t.Logf("Scan output length: %d chars", len(result.Output))

		if result.HasValidJSON {
			t.Logf("Scan produced valid JSON output")
		}
	})
}

// TestDatabaseHelperFunctionality tests database helper functions
func TestDatabaseHelperFunctionality(t *testing.T) {
	testCtx := NewTestContext(t, "db-helper-test")
	defer CleanupTestDatabase(testCtx)

	// Setup database
	err := SetupTestDatabase(testCtx)
	require.NoError(t, err, "Failed to setup test database")

	dbHelper, err := NewDatabaseHelper(testCtx)
	require.NoError(t, err, "Failed to create database helper")
	defer dbHelper.Close()

	t.Run("BasicQueries", func(t *testing.T) {
		// Test resource count queries (should return 0 for empty database)
		count, err := dbHelper.GetResourceCount("AWS::S3::Bucket")
		require.NoError(t, err, "Failed to get resource count")
		assert.Equal(t, 0, count, "Should have 0 S3 buckets in empty database")

		// Test relationship count queries
		relCount, err := dbHelper.GetRelationshipCount("contains")
		require.NoError(t, err, "Failed to get relationship count")
		assert.Equal(t, 0, relCount, "Should have 0 relationships in empty database")

		// Test getting resources by tag (should return empty slice)
		resources, err := dbHelper.GetResourcesByTag("TestID", testCtx.TestID)
		require.NoError(t, err, "Failed to get resources by tag")
		assert.Empty(t, resources, "Should have no resources with test tag")

		t.Logf("Basic database queries completed successfully")
	})

	t.Run("DatabaseStats", func(t *testing.T) {
		stats, err := dbHelper.GetDatabaseStats()
		require.NoError(t, err, "Failed to get database stats")

		// Verify expected keys exist
		expectedKeys := []string{
			"total_resources",
			"resources_by_service",
			"resources_by_type",
			"total_relationships",
			"raw_data_stats",
		}

		for _, key := range expectedKeys {
			assert.Contains(t, stats, key, "Stats should contain key %s", key)
		}

		t.Logf("Database stats: %+v", stats)
	})
}

// TestMockDataGeneration tests mock data generation utilities
func TestMockDataGeneration(t *testing.T) {
	testCtx := NewTestContext(t, "mock-data-test")
	mockGen := NewMockDataGenerator(testCtx.TestID, testCtx.Provider, testCtx.Region)

	t.Run("S3BucketMock", func(t *testing.T) {
		bucketName := "test-bucket-" + testCtx.TestID
		bucketData := mockGen.GenerateS3BucketMock(bucketName)

		// Verify structure
		assert.Equal(t, bucketName, bucketData["Name"])
		assert.Equal(t, testCtx.Region, bucketData["Region"])
		assert.Contains(t, bucketData, "Tags")
		assert.Contains(t, bucketData, "BucketPolicy")
		assert.Contains(t, bucketData, "Versioning")
		assert.Contains(t, bucketData, "Encryption")

		// Verify tags
		tags, ok := bucketData["Tags"].([]map[string]string)
		require.True(t, ok, "Tags should be array of string maps")

		foundTestID := false
		for _, tag := range tags {
			if tag["Key"] == "TestID" && tag["Value"] == testCtx.TestID {
				foundTestID = true
				break
			}
		}
		assert.True(t, foundTestID, "Should have TestID tag")
	})

	t.Run("EC2InstanceMock", func(t *testing.T) {
		instanceID := "i-" + generateRandomID(8)
		instanceData := mockGen.GenerateEC2InstanceMock(instanceID)

		// Verify structure
		assert.Equal(t, instanceID, instanceData["InstanceId"])
		assert.Equal(t, "t3.micro", instanceData["InstanceType"])
		assert.Contains(t, instanceData, "State")
		assert.Contains(t, instanceData, "LaunchTime")
		assert.Contains(t, instanceData, "Placement")
		assert.Contains(t, instanceData, "SecurityGroups")
		assert.Contains(t, instanceData, "Tags")

		// Verify placement
		placement, ok := instanceData["Placement"].(map[string]string)
		require.True(t, ok, "Placement should be string map")
		assert.Equal(t, testCtx.Region+"a", placement["AvailabilityZone"])
	})

	t.Run("VPCMock", func(t *testing.T) {
		vpcID := "vpc-" + generateRandomID(8)
		vpcData := mockGen.GenerateVPCMock(vpcID)

		// Verify structure
		assert.Equal(t, vpcID, vpcData["VpcId"])
		assert.Equal(t, "available", vpcData["State"])
		assert.Equal(t, "10.0.0.0/16", vpcData["CidrBlock"])
		assert.Contains(t, vpcData, "Tags")
		assert.Equal(t, false, vpcData["IsDefault"])
		assert.Equal(t, true, vpcData["EnableDnsHostnames"])
		assert.Equal(t, true, vpcData["EnableDnsSupport"])
	})
}

// TestResourceMatcher tests resource matching utilities
func TestResourceMatcher(t *testing.T) {
	testCtx := NewTestContext(t, "matcher-test")
	matcher := NewResourceMatcher(testCtx)

	// Create test resources
	resources := []map[string]interface{}{
		{
			"id":   "bucket-1",
			"type": "AWS::S3::Bucket",
			"name": "test-bucket-1",
			"tags": map[string]interface{}{
				"Environment": "test",
				"TestID":      testCtx.TestID,
			},
		},
		{
			"id":   "instance-1",
			"type": "AWS::EC2::Instance",
			"name": "test-instance-1",
			"tags": map[string]interface{}{
				"Environment": "production",
				"TestID":      testCtx.TestID,
			},
		},
		{
			"id":   "bucket-2",
			"type": "AWS::S3::Bucket",
			"name": "test-bucket-2",
			"tags": map[string]interface{}{
				"Environment": "test",
				"TestID":      "different-test-id",
			},
		},
	}

	t.Run("MatchByID", func(t *testing.T) {
		found := matcher.MatchResourceByID(resources, "bucket-1")
		assert.True(t, found, "Should find resource by ID")

		notFound := matcher.MatchResourceByID(resources, "nonexistent")
		assert.False(t, notFound, "Should not find nonexistent resource")
	})

	t.Run("MatchByTag", func(t *testing.T) {
		found := matcher.MatchResourceByTag(resources, "TestID", testCtx.TestID)
		assert.True(t, found, "Should find resource by tag")

		found = matcher.MatchResourceByTag(resources, "Environment", "test")
		assert.True(t, found, "Should find resource by environment tag")

		notFound := matcher.MatchResourceByTag(resources, "NonexistentTag", "value")
		assert.False(t, notFound, "Should not find resource with nonexistent tag")
	})

	t.Run("MatchByType", func(t *testing.T) {
		s3Buckets := matcher.MatchResourceByType(resources, "AWS::S3::Bucket")
		assert.Len(t, s3Buckets, 2, "Should find 2 S3 buckets")

		ec2Instances := matcher.MatchResourceByType(resources, "AWS::EC2::Instance")
		assert.Len(t, ec2Instances, 1, "Should find 1 EC2 instance")

		rdsInstances := matcher.MatchResourceByType(resources, "AWS::RDS::Instance")
		assert.Len(t, rdsInstances, 0, "Should find 0 RDS instances")
	})

	t.Run("CompareFields", func(t *testing.T) {
		resource1 := resources[0]
		resource2 := map[string]interface{}{
			"id":   "bucket-1",
			"type": "AWS::S3::Bucket",
			"name": "test-bucket-1",
		}

		// Compare matching fields
		fieldsMatch := matcher.CompareResourceFields(resource1, resource2, []string{"id", "type", "name"})
		assert.True(t, fieldsMatch, "Fields should match")

		// Compare non-matching fields
		resource3 := map[string]interface{}{
			"id":   "bucket-1",
			"type": "AWS::EC2::Instance", // Different type
			"name": "test-bucket-1",
		}

		fieldsMatch = matcher.CompareResourceFields(resource1, resource3, []string{"id", "type", "name"})
		assert.False(t, fieldsMatch, "Fields should not match")
	})
}

// TestUtilityFunctions tests various utility functions
func TestUtilityFunctions(t *testing.T) {
	t.Run("GenerateTestID", func(t *testing.T) {
		id1 := GenerateTestID("test")
		id2 := GenerateTestID("test")

		assert.NotEqual(t, id1, id2, "Generated IDs should be unique")
		assert.Contains(t, id1, "test", "ID should contain prefix")
		assert.Contains(t, id2, "test", "ID should contain prefix")
	})

	t.Run("ExtractOutputValue", func(t *testing.T) {
		outputs := map[string]interface{}{
			"bucketName": "test-bucket",
			"bucketArn":  "arn:aws:s3:::test-bucket",
			"numValue":   123,
		}

		// Test successful extraction
		bucketName, err := ExtractOutputValue(outputs, "bucketName")
		require.NoError(t, err, "Should extract bucket name")
		assert.Equal(t, "test-bucket", bucketName)

		// Test missing key
		_, err = ExtractOutputValue(outputs, "nonexistent")
		assert.Error(t, err, "Should error for missing key")

		// Test non-string value
		_, err = ExtractOutputValue(outputs, "numValue")
		assert.Error(t, err, "Should error for non-string value")
	})

	t.Run("RandomIDGeneration", func(t *testing.T) {
		id1 := generateRandomID(8)
		id2 := generateRandomID(8)

		assert.Len(t, id1, 8, "ID should be 8 characters")
		assert.Len(t, id2, 8, "ID should be 8 characters")
		assert.NotEqual(t, id1, id2, "IDs should be different")

		// Verify only contains valid characters
		validChars := "abcdefghijklmnopqrstuvwxyz0123456789"
		for _, char := range id1 {
			assert.Contains(t, validChars, string(char), "ID should only contain valid characters")
		}
	})
}

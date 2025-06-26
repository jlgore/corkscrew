# Test Harness Utilities

This package provides comprehensive utilities for testing Corkscrew integration scenarios. The utilities are designed to be reusable across different test scenarios and provide common functionality for database operations, mock data generation, resource matching, and assertions.

## Components

### 1. TestContext
The central configuration object that holds test parameters and state.

```go
testCtx := NewTestContext(t, "my-test-id")
```

### 2. DatabaseHelper
Provides database connection and query utilities for DuckDB operations.

```go
dbHelper, err := NewDatabaseHelper(testCtx)
defer dbHelper.Close()

// Get resource counts
count, err := dbHelper.GetResourceCount("AWS::S3::Bucket")

// Get resources by tag
resources, err := dbHelper.GetResourcesByTag("TestID", testCtx.TestID)

// Get database statistics
stats, err := dbHelper.GetDatabaseStats()

// Validate relationship integrity
orphaned, err := dbHelper.ValidateRelationshipIntegrity()
```

### 3. MockDataGenerator
Generates realistic mock data for AWS resources.

```go
mockGen := NewMockDataGenerator(testCtx.TestID, testCtx.Provider, testCtx.Region)

// Generate S3 bucket mock data
bucketData := mockGen.GenerateS3BucketMock("test-bucket")

// Generate EC2 instance mock data
instanceData := mockGen.GenerateEC2InstanceMock("i-1234567890abcdef0")

// Generate VPC mock data
vpcData := mockGen.GenerateVPCMock("vpc-1234567890abcdef0")
```

### 4. ResourceMatcher
Provides utilities for matching and comparing resources.

```go
matcher := NewResourceMatcher(testCtx)

// Match by ID
found := matcher.MatchResourceByID(resources, "resource-id")

// Match by tag
found := matcher.MatchResourceByTag(resources, "Environment", "test")

// Match by type
s3Buckets := matcher.MatchResourceByType(resources, "AWS::S3::Bucket")

// Compare resource fields
match := matcher.CompareResourceFields(resource1, resource2, []string{"id", "type"})
```

### 5. AssertionHelper
Enhanced assertion utilities for test scenarios.

```go
assertHelper := NewAssertionHelper(t, testCtx)

// Assert resource counts
assertHelper.AssertResourceCount(dbHelper, "AWS::S3::Bucket", 1)

// Assert relationship counts
assertHelper.AssertRelationshipCount(dbHelper, "contains", 2)

// Assert resource exists
assertHelper.AssertResourceExists(dbHelper, "TestID", testCtx.TestID)

// Assert no orphaned relationships
assertHelper.AssertNoOrphanedRelationships(dbHelper)

// Assert JSON structure
assertHelper.AssertJSONStructure(jsonData, []string{"resources", "metadata"})

// Assert scan success
assertHelper.AssertScanSuccess(scanResult)

// Assert deployment success
assertHelper.AssertDeploymentSuccess(deploymentResult)
```

## Utility Functions

### Scan Operations
```go
// Run Corkscrew scan
services := []string{"s3", "ec2"}
options := map[string]string{"dry-run": "true"}
result, err := RunCorkscrewScan(testCtx, services, options)
```

### Database Management
```go
// Setup test database
err := SetupTestDatabase(testCtx)

// Cleanup test database
err := CleanupTestDatabase(testCtx)
```

### Test Helpers
```go
// Generate unique test ID
testID := GenerateTestID("integration-test")

// Extract Pulumi output values
bucketName, err := ExtractOutputValue(outputs, "bucketName")

// Wait for resource stabilization
WaitForResourceStabilization(30 * time.Second)
```

## Usage Examples

### Basic Integration Test
```go
func TestBasicIntegration(t *testing.T) {
    // 1. Setup test context
    testCtx := NewTestContext(t, "basic-integration")
    defer CleanupTestDatabase(testCtx)

    // 2. Setup database
    err := SetupTestDatabase(testCtx)
    require.NoError(t, err)

    // 3. Create helpers
    dbHelper, err := NewDatabaseHelper(testCtx)
    require.NoError(t, err)
    defer dbHelper.Close()

    assertHelper := NewAssertionHelper(t, testCtx)

    // 4. Run scan
    result, err := RunCorkscrewScan(testCtx, []string{"s3"}, nil)
    require.NoError(t, err)

    // 5. Verify results
    assertHelper.AssertScanSuccess(result)
    assertHelper.AssertResourceCount(dbHelper, "AWS::S3::Bucket", 1)
}
```

### Mock Data Testing
```go
func TestMockDataGeneration(t *testing.T) {
    testCtx := NewTestContext(t, "mock-test")
    mockGen := NewMockDataGenerator(testCtx.TestID, "aws", "us-east-1")

    // Generate mock data
    bucketData := mockGen.GenerateS3BucketMock("test-bucket")
    
    // Verify structure
    assert.Contains(t, bucketData, "Name")
    assert.Contains(t, bucketData, "Tags")
    assert.Equal(t, "test-bucket", bucketData["Name"])
}
```

### Resource Matching
```go
func TestResourceMatching(t *testing.T) {
    testCtx := NewTestContext(t, "matching-test")
    matcher := NewResourceMatcher(testCtx)

    // Get resources from database
    dbHelper, _ := NewDatabaseHelper(testCtx)
    resources, _ := dbHelper.GetResourcesByTag("TestID", testCtx.TestID)

    // Match resources
    s3Buckets := matcher.MatchResourceByType(resources, "AWS::S3::Bucket")
    assert.NotEmpty(t, s3Buckets)
}
```

### Complex Verification
```go
func TestComplexVerification(t *testing.T) {
    testCtx := NewTestContext(t, "complex-test")
    dbHelper, _ := NewDatabaseHelper(testCtx)
    assertHelper := NewAssertionHelper(t, testCtx)

    // Get database statistics
    stats, err := dbHelper.GetDatabaseStats()
    require.NoError(t, err)

    // Verify expected resource counts
    expectedCounts := map[string]int{
        "AWS::S3::Bucket":      2,
        "AWS::EC2::Instance":   1,
        "AWS::EC2::VPC":        1,
    }

    for resourceType, expectedCount := range expectedCounts {
        assertHelper.AssertResourceCount(dbHelper, resourceType, expectedCount)
    }

    // Verify no orphaned relationships
    assertHelper.AssertNoOrphanedRelationships(dbHelper)
}
```

## Data Structures

### ScanResult
```go
type ScanResult struct {
    Output       string                 // Raw command output
    JSONOutput   map[string]interface{} // Parsed JSON output
    ExitCode     int                    // Command exit code
    Duration     time.Duration          // Scan duration
    Error        error                  // Any error that occurred
    HasValidJSON bool                   // Whether JSON parsing succeeded
}
```

### DeploymentResult
```go
type DeploymentResult struct {
    Stack     auto.Stack              // Pulumi stack
    Outputs   map[string]interface{}  // Deployment outputs
    Resources []string                // Created resource IDs
    Duration  time.Duration           // Deployment duration
    Error     error                   // Any error that occurred
}
```

### VerificationResult
```go
type VerificationResult struct {
    ResourceCounts     map[string]int         // Resource counts by type
    RelationshipCounts map[string]int         // Relationship counts by type
    RawDataStats       map[string]interface{} // Raw data statistics
    ValidationErrors   []string               // Any validation errors
    AllPassed          bool                   // Whether all validations passed
}
```

## Best Practices

1. **Always use defer for cleanup**:
   ```go
   defer CleanupTestDatabase(testCtx)
   defer dbHelper.Close()
   ```

2. **Use unique test IDs**:
   ```go
   testID := GenerateTestID("my-test")
   ```

3. **Check for test environment**:
   ```go
   if testing.Short() {
       t.Skip("Skipping integration test in short mode")
   }
   ```

4. **Use assertion helpers for better error messages**:
   ```go
   assertHelper.AssertResourceCount(dbHelper, "AWS::S3::Bucket", 1)
   ```

5. **Validate scan results**:
   ```go
   assertHelper.AssertScanSuccess(result)
   if result.HasValidJSON {
       // Process JSON output
   }
   ```

## Environment Variables

- `CORKSCREW_PATH`: Path to the Corkscrew binary
- `AWS_REGION`: AWS region for testing (default: us-east-1)
- `AWS_PROFILE`: AWS profile for authentication

## Dependencies

- DuckDB Go driver (`github.com/marcboeker/go-duckdb`)
- Testify for assertions (`github.com/stretchr/testify`)
- Pulumi SDK for infrastructure automation

## Thread Safety

The utilities are designed to be used within individual test functions and are not thread-safe. Each test should create its own instances of helpers and use unique test IDs to avoid conflicts.
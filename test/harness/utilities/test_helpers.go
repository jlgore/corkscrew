package utilities

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContext holds common test data and configuration
type TestContext struct {
	TestID        string
	Region        string
	Provider      string
	DatabasePath  string
	CorkscrewPath string
	StartTime     time.Time
	T             *testing.T
	Ctx           context.Context
}

// DatabaseHelper provides database connection and query utilities
type DatabaseHelper struct {
	db      *sql.DB
	dbPath  string
	testCtx *TestContext
}

// MockDataGenerator generates test data for various scenarios
type MockDataGenerator struct {
	testID   string
	provider string
	region   string
}

// ResourceMatcher provides utilities for matching and comparing resources
type ResourceMatcher struct {
	testCtx *TestContext
}

// AssertionHelper provides enhanced assertion utilities for test scenarios
type AssertionHelper struct {
	t       *testing.T
	testCtx *TestContext
}

// ScanResult represents the result of a Corkscrew scan operation
type ScanResult struct {
	Output       string
	JSONOutput   map[string]interface{}
	ExitCode     int
	Duration     time.Duration
	Error        error
	HasValidJSON bool
}

// DeploymentResult represents the result of infrastructure deployment
type DeploymentResult struct {
	Stack     auto.Stack
	Outputs   map[string]interface{}
	Resources []string
	Duration  time.Duration
	Error     error
}

// VerificationResult represents the result of data verification
type VerificationResult struct {
	ResourceCounts     map[string]int
	RelationshipCounts map[string]int
	RawDataStats       map[string]interface{}
	ValidationErrors   []string
	AllPassed          bool
}

// NewTestContext creates a new test context with default configuration
func NewTestContext(t *testing.T, testID string) *TestContext {
	if testID == "" {
		testID = fmt.Sprintf("test-%d-%d", time.Now().Unix(), rand.Intn(1000))
	}

	return &TestContext{
		TestID:        testID,
		Region:        getEnvOrDefault("AWS_REGION", "us-east-1"),
		Provider:      "aws",
		DatabasePath:  fmt.Sprintf("./test-%s.db", testID),
		CorkscrewPath: findCorkscrewBinary(),
		StartTime:     time.Now(),
		T:             t,
		Ctx:           context.Background(),
	}
}

// NewDatabaseHelper creates a new database helper
func NewDatabaseHelper(testCtx *TestContext) (*DatabaseHelper, error) {
	db, err := sql.Open("duckdb", testCtx.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DatabaseHelper{
		db:      db,
		dbPath:  testCtx.DatabasePath,
		testCtx: testCtx,
	}, nil
}

// Close closes the database connection
func (dh *DatabaseHelper) Close() error {
	if dh.db != nil {
		return dh.db.Close()
	}
	return nil
}

// GetResourceCount returns the count of resources by type
func (dh *DatabaseHelper) GetResourceCount(resourceType string) (int, error) {
	query := `SELECT COUNT(*) FROM aws_resources WHERE type = ?`
	var count int
	err := dh.db.QueryRow(query, resourceType).Scan(&count)
	return count, err
}

// GetRelationshipCount returns the count of relationships by type
func (dh *DatabaseHelper) GetRelationshipCount(relationshipType string) (int, error) {
	query := `SELECT COUNT(*) FROM aws_relationships WHERE relationship_type = ?`
	var count int
	err := dh.db.QueryRow(query, relationshipType).Scan(&count)
	return count, err
}

// GetResourcesByTag returns resources matching the given tag key-value pair
func (dh *DatabaseHelper) GetResourcesByTag(tagKey, tagValue string) ([]map[string]interface{}, error) {
	query := `
		SELECT id, type, name, arn, region, tags, raw_data 
		FROM aws_resources 
		WHERE JSON_EXTRACT(tags, ?) = ?
	`

	rows, err := dh.db.Query(query, fmt.Sprintf("$.%s", tagKey), tagValue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []map[string]interface{}
	for rows.Next() {
		resource := make(map[string]interface{})
		var id, resourceType, name, arn, region, tags, rawData sql.NullString

		err := rows.Scan(&id, &resourceType, &name, &arn, &region, &tags, &rawData)
		if err != nil {
			continue
		}

		resource["id"] = nullStringToString(id)
		resource["type"] = nullStringToString(resourceType)
		resource["name"] = nullStringToString(name)
		resource["arn"] = nullStringToString(arn)
		resource["region"] = nullStringToString(region)

		if tags.Valid {
			var tagsMap map[string]interface{}
			if err := json.Unmarshal([]byte(tags.String), &tagsMap); err == nil {
				resource["tags"] = tagsMap
			}
		}

		if rawData.Valid {
			var rawDataMap map[string]interface{}
			if err := json.Unmarshal([]byte(rawData.String), &rawDataMap); err == nil {
				resource["raw_data"] = rawDataMap
			}
		}

		resources = append(resources, resource)
	}

	return resources, rows.Err()
}

// GetDatabaseStats returns comprehensive database statistics
func (dh *DatabaseHelper) GetDatabaseStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total resources
	totalResources, err := dh.getTotalResourceCount()
	if err != nil {
		return nil, err
	}
	stats["total_resources"] = totalResources

	// Resources by service
	serviceStats, err := dh.getResourcesByService()
	if err != nil {
		return nil, err
	}
	stats["resources_by_service"] = serviceStats

	// Resources by type
	typeStats, err := dh.getResourcesByType()
	if err != nil {
		return nil, err
	}
	stats["resources_by_type"] = typeStats

	// Total relationships
	totalRelationships, err := dh.getTotalRelationshipCount()
	if err != nil {
		return nil, err
	}
	stats["total_relationships"] = totalRelationships

	// Raw data statistics
	rawDataStats, err := dh.getRawDataStats()
	if err != nil {
		return nil, err
	}
	stats["raw_data_stats"] = rawDataStats

	return stats, nil
}

// ValidateRelationshipIntegrity checks for orphaned relationships
func (dh *DatabaseHelper) ValidateRelationshipIntegrity() ([]string, error) {
	query := `
		SELECT r.id, r.from_resource_id, r.to_resource_id, r.relationship_type
		FROM aws_relationships r
		LEFT JOIN aws_resources from_res ON r.from_resource_id = from_res.id
		LEFT JOIN aws_resources to_res ON r.to_resource_id = to_res.id
		WHERE from_res.id IS NULL OR to_res.id IS NULL
	`

	rows, err := dh.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orphaned []string
	for rows.Next() {
		var id, fromID, toID, relType sql.NullString
		if err := rows.Scan(&id, &fromID, &toID, &relType); err != nil {
			continue
		}
		orphaned = append(orphaned, fmt.Sprintf("Relationship %s (%s): %s -> %s",
			nullStringToString(id), nullStringToString(relType),
			nullStringToString(fromID), nullStringToString(toID)))
	}

	return orphaned, rows.Err()
}

// NewMockDataGenerator creates a new mock data generator
func NewMockDataGenerator(testID, provider, region string) *MockDataGenerator {
	return &MockDataGenerator{
		testID:   testID,
		provider: provider,
		region:   region,
	}
}

// GenerateS3BucketMock generates mock S3 bucket data
func (mdg *MockDataGenerator) GenerateS3BucketMock(bucketName string) map[string]interface{} {
	return map[string]interface{}{
		"Name":         bucketName,
		"CreationDate": time.Now().UTC().Format(time.RFC3339),
		"Tags": []map[string]string{
			{"Key": "TestID", "Value": mdg.testID},
			{"Key": "Environment", "Value": "test"},
			{"Key": "Provider", "Value": mdg.provider},
		},
		"Region": mdg.region,
		"BucketPolicy": map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Effect":    "Allow",
					"Principal": "*",
					"Action":    "s3:GetObject",
					"Resource":  fmt.Sprintf("arn:aws:s3:::%s/*", bucketName),
				},
			},
		},
		"Versioning": map[string]string{
			"Status": "Enabled",
		},
		"Encryption": map[string]interface{}{
			"Rules": []map[string]interface{}{
				{
					"ApplyServerSideEncryptionByDefault": map[string]string{
						"SSEAlgorithm": "AES256",
					},
				},
			},
		},
	}
}

// GenerateEC2InstanceMock generates mock EC2 instance data
func (mdg *MockDataGenerator) GenerateEC2InstanceMock(instanceID string) map[string]interface{} {
	return map[string]interface{}{
		"InstanceId":   instanceID,
		"InstanceType": "t3.micro",
		"State": map[string]string{
			"Code": "16",
			"Name": "running",
		},
		"LaunchTime": time.Now().UTC().Format(time.RFC3339),
		"Placement": map[string]string{
			"AvailabilityZone": mdg.region + "a",
			"Tenancy":          "default",
		},
		"SecurityGroups": []map[string]string{
			{"GroupId": "sg-" + generateRandomID(8), "GroupName": "default"},
		},
		"Tags": []map[string]string{
			{"Key": "TestID", "Value": mdg.testID},
			{"Key": "Name", "Value": fmt.Sprintf("test-instance-%s", mdg.testID)},
		},
		"PublicIpAddress":  fmt.Sprintf("54.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255)),
		"PrivateIpAddress": fmt.Sprintf("10.0.1.%d", rand.Intn(254)+1),
	}
}

// GenerateVPCMock generates mock VPC data
func (mdg *MockDataGenerator) GenerateVPCMock(vpcID string) map[string]interface{} {
	return map[string]interface{}{
		"VpcId":     vpcID,
		"State":     "available",
		"CidrBlock": "10.0.0.0/16",
		"Tags": []map[string]string{
			{"Key": "TestID", "Value": mdg.testID},
			{"Key": "Name", "Value": fmt.Sprintf("test-vpc-%s", mdg.testID)},
		},
		"IsDefault":                   false,
		"EnableDnsHostnames":          true,
		"EnableDnsSupport":            true,
		"InstanceTenancy":             "default",
		"Ipv6CidrBlockAssociationSet": []interface{}{},
	}
}

// NewResourceMatcher creates a new resource matcher
func NewResourceMatcher(testCtx *TestContext) *ResourceMatcher {
	return &ResourceMatcher{testCtx: testCtx}
}

// MatchResourceByID checks if a resource with the given ID exists
func (rm *ResourceMatcher) MatchResourceByID(resources []map[string]interface{}, resourceID string) bool {
	for _, resource := range resources {
		if id, ok := resource["id"].(string); ok && id == resourceID {
			return true
		}
	}
	return false
}

// MatchResourceByTag checks if a resource with the given tag exists
func (rm *ResourceMatcher) MatchResourceByTag(resources []map[string]interface{}, tagKey, tagValue string) bool {
	for _, resource := range resources {
		if tags, ok := resource["tags"].(map[string]interface{}); ok {
			if value, exists := tags[tagKey]; exists && fmt.Sprintf("%v", value) == tagValue {
				return true
			}
		}
	}
	return false
}

// MatchResourceByType returns all resources of the specified type
func (rm *ResourceMatcher) MatchResourceByType(resources []map[string]interface{}, resourceType string) []map[string]interface{} {
	var matches []map[string]interface{}
	for _, resource := range resources {
		if rType, ok := resource["type"].(string); ok && rType == resourceType {
			matches = append(matches, resource)
		}
	}
	return matches
}

// CompareResourceFields compares specific fields between two resources
func (rm *ResourceMatcher) CompareResourceFields(resource1, resource2 map[string]interface{}, fields []string) bool {
	for _, field := range fields {
		val1, exists1 := resource1[field]
		val2, exists2 := resource2[field]

		if exists1 != exists2 || !reflect.DeepEqual(val1, val2) {
			return false
		}
	}
	return true
}

// NewAssertionHelper creates a new assertion helper
func NewAssertionHelper(t *testing.T, testCtx *TestContext) *AssertionHelper {
	return &AssertionHelper{t: t, testCtx: testCtx}
}

// AssertResourceCount asserts that the expected number of resources exist
func (ah *AssertionHelper) AssertResourceCount(dbHelper *DatabaseHelper, resourceType string, expectedCount int) {
	actualCount, err := dbHelper.GetResourceCount(resourceType)
	require.NoError(ah.t, err, "Failed to get resource count for %s", resourceType)
	assert.Equal(ah.t, expectedCount, actualCount, "Resource count mismatch for %s", resourceType)
}

// AssertRelationshipCount asserts that the expected number of relationships exist
func (ah *AssertionHelper) AssertRelationshipCount(dbHelper *DatabaseHelper, relationshipType string, expectedCount int) {
	actualCount, err := dbHelper.GetRelationshipCount(relationshipType)
	require.NoError(ah.t, err, "Failed to get relationship count for %s", relationshipType)
	assert.Equal(ah.t, expectedCount, actualCount, "Relationship count mismatch for %s", relationshipType)
}

// AssertResourceExists asserts that a resource with the given properties exists
func (ah *AssertionHelper) AssertResourceExists(dbHelper *DatabaseHelper, tagKey, tagValue string) {
	resources, err := dbHelper.GetResourcesByTag(tagKey, tagValue)
	require.NoError(ah.t, err, "Failed to query resources by tag %s=%s", tagKey, tagValue)
	assert.NotEmpty(ah.t, resources, "No resources found with tag %s=%s", tagKey, tagValue)
}

// AssertNoOrphanedRelationships asserts that no orphaned relationships exist
func (ah *AssertionHelper) AssertNoOrphanedRelationships(dbHelper *DatabaseHelper) {
	orphaned, err := dbHelper.ValidateRelationshipIntegrity()
	require.NoError(ah.t, err, "Failed to validate relationship integrity")
	assert.Empty(ah.t, orphaned, "Found orphaned relationships: %v", orphaned)
}

// AssertJSONStructure asserts that the given JSON has the expected structure
func (ah *AssertionHelper) AssertJSONStructure(jsonData map[string]interface{}, expectedKeys []string) {
	for _, key := range expectedKeys {
		assert.Contains(ah.t, jsonData, key, "Expected key %s not found in JSON", key)
	}
}

// AssertScanSuccess asserts that a scan completed successfully
func (ah *AssertionHelper) AssertScanSuccess(result *ScanResult) {
	assert.NoError(ah.t, result.Error, "Scan should not have error")
	assert.Equal(ah.t, 0, result.ExitCode, "Scan should exit with code 0")
	assert.NotEmpty(ah.t, result.Output, "Scan should produce output")
	if result.HasValidJSON {
		assert.NotEmpty(ah.t, result.JSONOutput, "Scan should produce valid JSON output")
	}
}

// AssertDeploymentSuccess asserts that deployment completed successfully
func (ah *AssertionHelper) AssertDeploymentSuccess(result *DeploymentResult) {
	assert.NoError(ah.t, result.Error, "Deployment should not have error")
	assert.NotNil(ah.t, result.Stack, "Stack should be created")
	assert.NotEmpty(ah.t, result.Outputs, "Deployment should produce outputs")
}

// RunCorkscrewScan executes a Corkscrew scan with the given parameters
func RunCorkscrewScan(testCtx *TestContext, services []string, options map[string]string) (*ScanResult, error) {
	start := time.Now()

	// Build command arguments
	args := []string{"scan", "--provider", testCtx.Provider, "--region", testCtx.Region}

	if len(services) > 0 {
		args = append(args, "--services", strings.Join(services, ","))
	}

	args = append(args, "--output", "json", "--database", testCtx.DatabasePath)

	// Add custom options
	for key, value := range options {
		args = append(args, fmt.Sprintf("--%s", key), value)
	}

	// Initialize Corkscrew first
	initCmd := exec.CommandContext(testCtx.Ctx, testCtx.CorkscrewPath, "init", "--database", testCtx.DatabasePath)
	initCmd.Env = append(os.Environ(), "AWS_PROFILE=sandbox")
	initOutput, initErr := initCmd.CombinedOutput()

	if initErr != nil {
		testCtx.T.Logf("Init output: %s", string(initOutput))
		testCtx.T.Logf("Init error: %v", initErr)
	}

	// Run the scan
	cmd := exec.CommandContext(testCtx.Ctx, testCtx.CorkscrewPath, args...)
	cmd.Env = append(os.Environ(), "AWS_PROFILE=sandbox")

	output, err := cmd.CombinedOutput()

	result := &ScanResult{
		Output:   string(output),
		Duration: time.Since(start),
		Error:    err,
	}

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		}
	}

	// Try to extract and parse JSON from output
	if jsonStr := extractJSONFromOutput(result.Output); jsonStr != "" {
		var jsonData map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(jsonStr), &jsonData); jsonErr == nil {
			result.JSONOutput = jsonData
			result.HasValidJSON = true
		}
	}

	return result, nil
}

// SetupTestDatabase initializes a test database with required schema
func SetupTestDatabase(testCtx *TestContext) error {
	// Initialize the database using Corkscrew init
	initCmd := exec.CommandContext(testCtx.Ctx, testCtx.CorkscrewPath, "init", "--database", testCtx.DatabasePath)
	output, err := initCmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("failed to initialize test database: %w, output: %s", err, string(output))
	}

	return nil
}

// CleanupTestDatabase removes the test database files
func CleanupTestDatabase(testCtx *TestContext) error {
	files := []string{
		testCtx.DatabasePath,
		testCtx.DatabasePath + ".wal",
		testCtx.DatabasePath + "-journal",
	}

	for _, file := range files {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove database file %s: %w", file, err)
		}
	}

	return nil
}

// WaitForResourceStabilization waits for AWS resources to stabilize
func WaitForResourceStabilization(duration time.Duration) {
	time.Sleep(duration)
}

// GenerateTestID creates a unique test identifier
func GenerateTestID(prefix string) string {
	timestamp := time.Now().Unix()
	random := rand.Intn(10000)
	return fmt.Sprintf("%s-%d-%d", prefix, timestamp, random)
}

// ExtractOutputValue safely extracts a value from Pulumi outputs
func ExtractOutputValue(outputs map[string]interface{}, key string) (string, error) {
	value, exists := outputs[key]
	if !exists {
		return "", fmt.Errorf("key %s not found in outputs", key)
	}

	strValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("value for key %s is not a string", key)
	}

	return strValue, nil
}

// Helper functions

func getTotalResourceCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM aws_resources").Scan(&count)
	return count, err
}

func (dh *DatabaseHelper) getTotalResourceCount() (int, error) {
	return getTotalResourceCount(dh.db)
}

func (dh *DatabaseHelper) getResourcesByService() (map[string]int, error) {
	query := `SELECT service, COUNT(*) FROM aws_resources GROUP BY service`
	rows, err := dh.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var service sql.NullString
		var count int
		if err := rows.Scan(&service, &count); err != nil {
			continue
		}
		result[nullStringToString(service)] = count
	}

	return result, rows.Err()
}

func (dh *DatabaseHelper) getResourcesByType() (map[string]int, error) {
	query := `SELECT type, COUNT(*) FROM aws_resources GROUP BY type`
	rows, err := dh.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var resourceType sql.NullString
		var count int
		if err := rows.Scan(&resourceType, &count); err != nil {
			continue
		}
		result[nullStringToString(resourceType)] = count
	}

	return result, rows.Err()
}

func (dh *DatabaseHelper) getTotalRelationshipCount() (int, error) {
	var count int
	err := dh.db.QueryRow("SELECT COUNT(*) FROM aws_relationships").Scan(&count)
	return count, err
}

func (dh *DatabaseHelper) getRawDataStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count resources with raw data
	var withRawData int
	err := dh.db.QueryRow("SELECT COUNT(*) FROM aws_resources WHERE raw_data IS NOT NULL AND raw_data != ''").Scan(&withRawData)
	if err != nil {
		return nil, err
	}
	stats["resources_with_raw_data"] = withRawData

	// Count resources without raw data
	var withoutRawData int
	err = dh.db.QueryRow("SELECT COUNT(*) FROM aws_resources WHERE raw_data IS NULL OR raw_data = ''").Scan(&withoutRawData)
	if err != nil {
		return nil, err
	}
	stats["resources_without_raw_data"] = withoutRawData

	// Average raw data size
	var avgSize sql.NullFloat64
	err = dh.db.QueryRow("SELECT AVG(LENGTH(raw_data)) FROM aws_resources WHERE raw_data IS NOT NULL AND raw_data != ''").Scan(&avgSize)
	if err != nil {
		return nil, err
	}
	if avgSize.Valid {
		stats["average_raw_data_size"] = int(avgSize.Float64)
	} else {
		stats["average_raw_data_size"] = 0
	}

	return stats, nil
}

func nullStringToString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func extractJSONFromOutput(output string) string {
	start := -1
	end := -1
	braceCount := 0

	for i, ch := range output {
		if ch == '{' {
			if start == -1 {
				start = i
			}
			braceCount++
		} else if ch == '}' {
			braceCount--
			if braceCount == 0 && start != -1 {
				end = i + 1
				break
			}
		}
	}

	if start != -1 && end != -1 {
		return output[start:end]
	}

	return ""
}

func findCorkscrewBinary() string {
	// Try environment variable first
	if path := os.Getenv("CORKSCREW_PATH"); path != "" {
		return path
	}

	// Try relative path from test directory
	testDir, err := os.Getwd()
	if err == nil {
		rootDir := filepath.Join(testDir, "..", "..")
		corkscrewPath := filepath.Join(rootDir, "corkscrew")
		if _, err := os.Stat(corkscrewPath); err == nil {
			return corkscrewPath
		}
	}

	// Try PATH
	if path, err := exec.LookPath("corkscrew"); err == nil {
		return path
	}

	return "corkscrew"
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func generateRandomID(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}

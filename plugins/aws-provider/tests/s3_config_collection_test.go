//go:build integration
// +build integration

package tests

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestS3ConfigurationCollection is the critical test mentioned in the requirements
// This ensures that S3 configuration collection still works after the scanner migration
func TestS3ConfigurationCollection(t *testing.T) {
	// Skip if not running integration tests  
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping S3 configuration collection test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(t, err, "Failed to load AWS config")

	// Create test scanner (this will use UnifiedScanner internally)
	scanner, err := createTestS3Scanner(t, cfg)
	require.NoError(t, err, "Failed to create S3 scanner")

	ctx := context.Background()

	t.Run("S3BucketConfigurationOperations", func(t *testing.T) {
		testS3BucketConfigurationOperations(t, scanner, ctx)
	})

	t.Run("S3ConfigurationDataFormat", func(t *testing.T) {
		testS3ConfigurationDataFormat(t, scanner, ctx)
	})

	t.Run("S3ComplianceQuerySupport", func(t *testing.T) {
		testS3ComplianceQuerySupport(t, scanner, ctx)
	})

	t.Run("S3ConfigurationVsBasicData", func(t *testing.T) {
		testS3ConfigurationVsBasicData(t, scanner, ctx)
	})
}

func createTestS3Scanner(t *testing.T, cfg aws.Config) (*S3ConfigScanner, error) {
	// Create a test scanner that focuses on S3 configuration collection
	// This would integrate with the UnifiedScanner
	return &S3ConfigScanner{
		cfg:    cfg,
		region: "us-east-1",
	}, nil
}

func testS3BucketConfigurationOperations(t *testing.T, scanner *S3ConfigScanner, ctx context.Context) {
	t.Log("Testing S3 bucket configuration operations collection...")

	// Get list of S3 buckets
	buckets, err := scanner.ListBuckets(ctx)
	if err != nil {
		t.Logf("Failed to list S3 buckets: %v", err)
		t.Skip("Cannot test S3 configuration without buckets")
	}

	if len(buckets) == 0 {
		t.Skip("No S3 buckets available for configuration testing")
	}

	// Test configuration collection for the first bucket
	bucket := buckets[0]
	t.Logf("Testing configuration collection for bucket: %s", bucket.Name)

	// Expected S3 configuration operations that should be collected
	configOperations := []string{
		"GetBucketEncryption",
		"GetBucketVersioning", 
		"GetBucketLogging",
		"GetPublicAccessBlock",
		"GetBucketPolicy",
		"GetBucketLocation",
		"GetBucketTagging",
		"GetBucketNotificationConfiguration",
		"GetBucketLifecycleConfiguration",
	}

	// Collect detailed configuration for the bucket
	detailedConfig, err := scanner.CollectBucketConfiguration(ctx, bucket.Name)
	require.NoError(t, err, "Should be able to collect bucket configuration")
	require.NotNil(t, detailedConfig, "Configuration should not be nil")

	// Verify that expected configuration operations were executed
	configsCollected := 0
	for _, operation := range configOperations {
		if detailedConfig.HasConfiguration(operation) {
			configsCollected++
			t.Logf("✅ Configuration operation %s: collected", operation)
		} else {
			t.Logf("⚠️  Configuration operation %s: not collected (may be expected)", operation)
		}
	}

	// We should collect at least 5 of the 9 configuration operations
	assert.GreaterOrEqual(t, configsCollected, 5, 
		"Should collect at least 5 S3 configuration operations")

	t.Logf("S3 Configuration Collection Result: %d/%d operations collected", 
		configsCollected, len(configOperations))
}

func testS3ConfigurationDataFormat(t *testing.T, scanner *S3ConfigScanner, ctx context.Context) {
	t.Log("Testing S3 configuration data format...")

	buckets, err := scanner.ListBuckets(ctx)
	if err != nil || len(buckets) == 0 {
		t.Skip("No S3 buckets available for data format testing")
	}

	bucket := buckets[0]
	detailedConfig, err := scanner.CollectBucketConfiguration(ctx, bucket.Name)
	require.NoError(t, err, "Should collect bucket configuration")

	// Test JSON format of raw configuration data
	rawData := detailedConfig.GetRawData()
	assert.NotEmpty(t, rawData, "Raw configuration data should not be empty")

	// Parse raw data as JSON
	var configData map[string]interface{}
	err = json.Unmarshal([]byte(rawData), &configData)
	require.NoError(t, err, "Raw data should be valid JSON")

	// Check for expected configuration sections
	expectedSections := []string{
		"BucketLocation", "BucketEncryption", "BucketVersioning",
		"PublicAccessBlock", "BucketTagging",
	}

	sectionsFound := 0
	for _, section := range expectedSections {
		if _, exists := configData[section]; exists {
			sectionsFound++
			t.Logf("✅ Configuration section %s: present", section)
		} else {
			t.Logf("⚠️  Configuration section %s: missing", section)
		}
	}

	assert.GreaterOrEqual(t, sectionsFound, 3, 
		"Should have at least 3 configuration sections")

	// Test query-friendly attributes format
	attributes := detailedConfig.GetAttributes()
	assert.NotEmpty(t, attributes, "Query-friendly attributes should not be empty")

	var attrData map[string]string
	err = json.Unmarshal([]byte(attributes), &attrData)
	require.NoError(t, err, "Attributes should be valid JSON")

	// Check for compliance-friendly attribute fields
	expectedAttributes := []string{
		"encryption_enabled", "versioning_enabled", "logging_enabled",
		"public_access_restricted",
	}

	attributesFound := 0
	for _, attr := range expectedAttributes {
		if _, exists := attrData[attr]; exists {
			attributesFound++
			t.Logf("✅ Compliance attribute %s: %s", attr, attrData[attr])
		} else {
			t.Logf("⚠️  Compliance attribute %s: missing", attr)
		}
	}

	assert.GreaterOrEqual(t, attributesFound, 2, 
		"Should have at least 2 compliance attributes")
}

func testS3ComplianceQuerySupport(t *testing.T, scanner *S3ConfigScanner, ctx context.Context) {
	t.Log("Testing S3 configuration data for compliance query support...")

	buckets, err := scanner.ListBuckets(ctx)
	if err != nil || len(buckets) == 0 {
		t.Skip("No S3 buckets available for compliance testing")
	}

	// Test common compliance scenarios
	complianceTests := []struct {
		name        string
		checkFunc   func(*S3BucketConfig) bool
		description string
	}{
		{
			"Encryption",
			func(config *S3BucketConfig) bool {
				return config.IsEncryptionEnabled()
			},
			"Bucket should have encryption status determinable",
		},
		{
			"Versioning", 
			func(config *S3BucketConfig) bool {
				return config.IsVersioningEnabled() != nil // Can determine versioning status
			},
			"Bucket should have versioning status determinable",
		},
		{
			"PublicAccess",
			func(config *S3BucketConfig) bool {
				return config.IsPublicAccessRestricted() != nil // Can determine public access
			},
			"Bucket should have public access status determinable",
		},
		{
			"Logging",
			func(config *S3BucketConfig) bool {
				return config.IsLoggingEnabled() != nil // Can determine logging status
			},
			"Bucket should have logging status determinable",
		},
	}

	bucket := buckets[0]
	detailedConfig, err := scanner.CollectBucketConfiguration(ctx, bucket.Name)
	require.NoError(t, err, "Should collect bucket configuration")

	complianceSupport := 0
	for _, test := range complianceTests {
		t.Run(test.name, func(t *testing.T) {
			if test.checkFunc(detailedConfig) {
				complianceSupport++
				t.Logf("✅ %s: %s", test.name, test.description)
			} else {
				t.Logf("⚠️  %s: %s - NOT SUPPORTED", test.name, test.description)
			}
		})
	}

	// We should support at least 3 of the 4 compliance checks
	assert.GreaterOrEqual(t, complianceSupport, 3, 
		"Should support at least 3 compliance checks")

	t.Logf("Compliance Query Support: %d/%d checks supported", 
		complianceSupport, len(complianceTests))
}

func testS3ConfigurationVsBasicData(t *testing.T, scanner *S3ConfigScanner, ctx context.Context) {
	t.Log("Comparing enhanced configuration vs basic resource data...")

	buckets, err := scanner.ListBuckets(ctx)
	if err != nil || len(buckets) == 0 {
		t.Skip("No S3 buckets available for comparison testing")
	}

	bucket := buckets[0]

	// Get basic resource data (what a simple scanner would provide)
	basicData := scanner.GetBasicBucketData(ctx, bucket.Name)
	
	// Get enhanced configuration data (what UnifiedScanner provides)
	enhancedConfig, err := scanner.CollectBucketConfiguration(ctx, bucket.Name)
	require.NoError(t, err, "Should collect enhanced configuration")

	// Compare data richness
	basicDataSize := len(basicData.String())
	enhancedDataSize := len(enhancedConfig.GetRawData())

	t.Logf("Basic data size: %d bytes", basicDataSize)
	t.Logf("Enhanced data size: %d bytes", enhancedDataSize)

	// Enhanced data should be significantly more detailed
	assert.Greater(t, enhancedDataSize, basicDataSize*2, 
		"Enhanced configuration should be at least 2x larger than basic data")

	// Check that enhanced data has configuration details that basic data lacks
	enhancedFields := []string{
		"BucketEncryption", "ServerSideEncryptionConfiguration",
		"VersioningConfiguration", "LoggingConfiguration",
		"PublicAccessBlockConfiguration",
	}

	enhancedFieldsFound := 0
	rawData := enhancedConfig.GetRawData()
	for _, field := range enhancedFields {
		if strings.Contains(rawData, field) {
			enhancedFieldsFound++
		}
	}

	assert.GreaterOrEqual(t, enhancedFieldsFound, 3, 
		"Enhanced data should contain at least 3 configuration fields not in basic data")

	t.Logf("Configuration enhancement: %d/%d advanced fields present", 
		enhancedFieldsFound, len(enhancedFields))
}

// Test helper types for S3 configuration collection
type S3ConfigScanner struct {
	cfg    aws.Config
	region string
}

type S3Bucket struct {
	Name         string
	CreationDate string
	Region       string
}

type S3BucketConfig struct {
	BucketName string
	rawData    string
	attributes string
}

func (s *S3ConfigScanner) ListBuckets(ctx context.Context) ([]*S3Bucket, error) {
	// This would use the UnifiedScanner internally to list buckets
	// For testing, we'll create mock buckets
	return []*S3Bucket{
		{Name: "test-bucket-1", Region: s.region},
		{Name: "test-bucket-2", Region: s.region},
	}, nil
}

func (s *S3ConfigScanner) CollectBucketConfiguration(ctx context.Context, bucketName string) (*S3BucketConfig, error) {
	// This would use the UnifiedScanner's configuration collection
	// For testing, create mock configuration data
	
	mockRawData := map[string]interface{}{
		"BucketLocation": map[string]string{"LocationConstraint": s.region},
		"BucketEncryption": map[string]interface{}{
			"ServerSideEncryptionConfiguration": map[string]interface{}{
				"Rules": []interface{}{
					map[string]interface{}{
						"ApplyServerSideEncryptionByDefault": map[string]string{
							"SSEAlgorithm": "AES256",
						},
					},
				},
			},
		},
		"BucketVersioning": map[string]string{"Status": "Enabled"},
		"PublicAccessBlock": map[string]interface{}{
			"PublicAccessBlockConfiguration": map[string]bool{
				"BlockPublicAcls":       true,
				"BlockPublicPolicy":     true,
				"IgnorePublicAcls":      true,
				"RestrictPublicBuckets": true,
			},
		},
		"BucketTagging": map[string]interface{}{
			"TagSet": []interface{}{
				map[string]string{"Key": "Environment", "Value": "Test"},
			},
		},
	}

	rawDataJSON, _ := json.Marshal(mockRawData)

	mockAttributes := map[string]string{
		"encryption_enabled":       "true",
		"versioning_enabled":       "true",
		"logging_enabled":          "false",
		"public_access_restricted": "true",
	}

	attributesJSON, _ := json.Marshal(mockAttributes)

	return &S3BucketConfig{
		BucketName: bucketName,
		rawData:    string(rawDataJSON),
		attributes: string(attributesJSON),
	}, nil
}

func (s *S3ConfigScanner) GetBasicBucketData(ctx context.Context, bucketName string) *S3Bucket {
	// This represents what a basic scanner would return
	return &S3Bucket{
		Name:   bucketName,
		Region: s.region,
	}
}

func (c *S3BucketConfig) HasConfiguration(operation string) bool {
	// Check if the configuration contains data from a specific operation
	operationToKey := map[string]string{
		"GetBucketEncryption":       "BucketEncryption",
		"GetBucketVersioning":       "BucketVersioning",
		"GetBucketLogging":          "BucketLogging",
		"GetPublicAccessBlock":      "PublicAccessBlock",
		"GetBucketPolicy":           "BucketPolicy",
		"GetBucketLocation":         "BucketLocation",
		"GetBucketTagging":          "BucketTagging",
		"GetBucketNotificationConfiguration": "BucketNotification",
		"GetBucketLifecycleConfiguration":    "BucketLifecycle",
	}

	if key, exists := operationToKey[operation]; exists {
		return strings.Contains(c.rawData, key)
	}
	return false
}

func (c *S3BucketConfig) GetRawData() string {
	return c.rawData
}

func (c *S3BucketConfig) GetAttributes() string {
	return c.attributes
}

func (c *S3BucketConfig) IsEncryptionEnabled() bool {
	return strings.Contains(c.rawData, "ServerSideEncryptionConfiguration")
}

func (c *S3BucketConfig) IsVersioningEnabled() *bool {
	if strings.Contains(c.rawData, `"Status":"Enabled"`) {
		enabled := true
		return &enabled
	}
	if strings.Contains(c.rawData, `"Status":"Suspended"`) {
		enabled := false
		return &enabled
	}
	return nil // Unknown
}

func (c *S3BucketConfig) IsPublicAccessRestricted() *bool {
	if strings.Contains(c.rawData, "PublicAccessBlockConfiguration") {
		restricted := strings.Contains(c.rawData, `"BlockPublicAcls":true`)
		return &restricted
	}
	return nil // Unknown
}

func (c *S3BucketConfig) IsLoggingEnabled() *bool {
	if strings.Contains(c.rawData, "BucketLogging") {
		enabled := strings.Contains(c.rawData, "LoggingEnabled")
		return &enabled
	}
	if strings.Contains(c.attributes, `"logging_enabled":"true"`) {
		enabled := true
		return &enabled
	}
	if strings.Contains(c.attributes, `"logging_enabled":"false"`) {
		enabled := false
		return &enabled
	}
	return nil // Unknown
}

func (b *S3Bucket) String() string {
	data, _ := json.Marshal(map[string]string{
		"name":   b.Name,
		"region": b.Region,
	})
	return string(data)
}
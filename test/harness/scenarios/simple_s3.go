package scenarios

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// SimpleS3Scenario creates a single S3 bucket with versioning for basic testing
type SimpleS3Scenario struct {
	expectedResources map[string]interface{}
}

// NewSimpleS3Scenario creates a new simple S3 scenario
func NewSimpleS3Scenario() *SimpleS3Scenario {
	return &SimpleS3Scenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *SimpleS3Scenario) GetName() string {
	return "simple-s3"
}

// GetServices returns the AWS services this scenario tests
func (s *SimpleS3Scenario) GetServices() []string {
	return []string{"s3"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *SimpleS3Scenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("simple-s3"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create S3 bucket with shorter prefix to avoid 37 char limit
	shortID := testID
	if len(testID) > 20 {
		shortID = testID[:20]
	}
	bucket, err := s3.NewBucket(ctx, "test-bucket", &s3.BucketArgs{
		BucketPrefix: pulumi.String(fmt.Sprintf("corkscrew-%s", shortID)),
		Tags:         tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create S3 bucket: %w", err)
	}

	// Enable versioning
	versioning, err := s3.NewBucketVersioningV2(ctx, "test-versioning", &s3.BucketVersioningV2Args{
		Bucket: bucket.ID(),
		VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
			Status: pulumi.String("Enabled"),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to enable bucket versioning: %w", err)
	}

	// Configure server-side encryption
	encryption, err := s3.NewBucketServerSideEncryptionConfigurationV2(ctx, "test-encryption", &s3.BucketServerSideEncryptionConfigurationV2Args{
		Bucket: bucket.ID(),
		Rules: s3.BucketServerSideEncryptionConfigurationV2RuleArray{
			&s3.BucketServerSideEncryptionConfigurationV2RuleArgs{
				ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationV2RuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("AES256"),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to configure bucket encryption: %w", err)
	}

	// Export resource details for verification
	ctx.Export("bucketName", bucket.Bucket)
	ctx.Export("bucketArn", bucket.Arn)
	ctx.Export("bucketId", bucket.ID())
	ctx.Export("versioningId", versioning.ID())
	ctx.Export("encryptionId", encryption.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"s3": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"name": bucket.Bucket,
				"arn":  bucket.Arn,
				"id":   bucket.ID(),
				"attributes": pulumi.Map{
					"versioning_enabled": pulumi.Bool(true),
					"encryption_enabled": pulumi.Bool(true),
				},
				"tags": tags,
			},
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"s3": []interface{}{
				map[string]interface{}{
					"type": "Bucket",
					"name": "", // Will be filled by output
					"arn":  "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"versioning_enabled": true,
						"encryption_enabled": true,
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "simple-s3",
						"CreatedBy":   "corkscrew-test",
					},
				},
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *SimpleS3Scenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

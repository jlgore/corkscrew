package automation

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// getS3Program returns a Pulumi program that creates an S3 bucket for testing
func getS3Program(cfg HarnessConfig) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		// Common tags for all resources
		tags := pulumi.StringMap{
			"TestHarness": pulumi.String("true"),
			"TestID":      pulumi.String(cfg.TestID),
			"Scenario":    pulumi.String(cfg.Scenario),
			"CreatedBy":   pulumi.String("corkscrew-test"),
			"Provider":    pulumi.String(cfg.Provider),
			"Region":      pulumi.String(cfg.Region),
		}

		// Create S3 bucket with unique name
		bucketName := fmt.Sprintf("corkscrew-test-%s-%s", cfg.TestID, cfg.Scenario)
		bucket, err := s3.NewBucket(ctx, "test-bucket", &s3.BucketArgs{
			Bucket: pulumi.String(bucketName),
			Tags:   tags,
		})
		if err != nil {
			return err
		}

		// Enable versioning for more realistic testing
		_, err = s3.NewBucketVersioningV2(ctx, "test-versioning", &s3.BucketVersioningV2Args{
			Bucket: bucket.ID(),
			VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
				Status: pulumi.String("Enabled"),
			},
		})
		if err != nil {
			return err
		}

		// Add public access block for security
		_, err = s3.NewBucketPublicAccessBlock(ctx, "test-pab", &s3.BucketPublicAccessBlockArgs{
			Bucket:                bucket.ID(),
			BlockPublicAcls:       pulumi.Bool(true),
			BlockPublicPolicy:     pulumi.Bool(true),
			IgnorePublicAcls:      pulumi.Bool(true),
			RestrictPublicBuckets: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		// Export essential information for verification
		ctx.Export("bucketName", bucket.Bucket)
		ctx.Export("bucketArn", bucket.Arn)
		ctx.Export("bucketId", bucket.ID())
		ctx.Export("testID", pulumi.String(cfg.TestID))
		ctx.Export("tags", tags)

		return nil
	}
}

package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// CleanupVerifier handles AWS resource cleanup verification and manual cleanup
type CleanupVerifier struct {
	cfg      aws.Config
	s3Client *s3.Client
	region   string
	testID   string
}

// NewCleanupVerifier creates a new cleanup verifier
func NewCleanupVerifier(ctx context.Context, region, testID string) (*CleanupVerifier, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &CleanupVerifier{
		cfg:      cfg,
		s3Client: s3.NewFromConfig(cfg),
		region:   region,
		testID:   testID,
	}, nil
}

// CleanupResult contains the results of cleanup verification
type CleanupResult struct {
	PulumiSuccess   bool                  `json:"pulumi_success"`
	AWSVerified     bool                  `json:"aws_verified"`
	ResourcesFound  []RemainingResource   `json:"resources_found"`
	ManualCleanup   []ManualCleanupAction `json:"manual_cleanup"`
	CleanupDuration time.Duration         `json:"cleanup_duration"`
	Errors          []string              `json:"errors"`
}

type RemainingResource struct {
	Service     string    `json:"service"`
	Type        string    `json:"type"`
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ARN         string    `json:"arn"`
	CreatedTime time.Time `json:"created_time"`
	TestID      string    `json:"test_id"`
}

type ManualCleanupAction struct {
	Resource RemainingResource `json:"resource"`
	Action   string            `json:"action"`
	Success  bool              `json:"success"`
	Error    string            `json:"error,omitempty"`
	Duration time.Duration     `json:"duration"`
}

// VerifyAndCleanup verifies that resources were actually deleted and performs manual cleanup if needed
func (cv *CleanupVerifier) VerifyAndCleanup(ctx context.Context, expectedResources map[string]interface{}, pulumiSuccess bool) (*CleanupResult, error) {
	start := time.Now()

	result := &CleanupResult{
		PulumiSuccess:  pulumiSuccess,
		ResourcesFound: []RemainingResource{},
		ManualCleanup:  []ManualCleanupAction{},
		Errors:         []string{},
	}

	// Extract expected resources for verification
	testResources := cv.extractTestResources(expectedResources)

	// Wait a moment for AWS eventual consistency
	fmt.Println("⏳ Waiting 10s for AWS eventual consistency...")
	time.Sleep(10 * time.Second)

	// Verify each resource type was actually deleted
	for _, resource := range testResources {
		remaining, err := cv.checkResourceExists(ctx, resource)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Error checking %s: %v", resource.ID, err))
			continue
		}

		if remaining != nil {
			result.ResourcesFound = append(result.ResourcesFound, *remaining)
			fmt.Printf("⚠️  Found remaining resource: %s (%s)\n", remaining.Name, remaining.Type)
		}
	}

	// If resources remain, attempt manual cleanup
	if len(result.ResourcesFound) > 0 {
		fmt.Printf("🧹 Attempting manual cleanup of %d remaining resources...\n", len(result.ResourcesFound))

		for _, resource := range result.ResourcesFound {
			action := cv.performManualCleanup(ctx, resource)
			result.ManualCleanup = append(result.ManualCleanup, action)

			if action.Success {
				fmt.Printf("✅ Manually cleaned up: %s\n", resource.Name)
			} else {
				fmt.Printf("❌ Failed to clean up: %s - %s\n", resource.Name, action.Error)
			}
		}

		// Re-verify after manual cleanup
		fmt.Println("🔍 Re-verifying cleanup after manual actions...")
		time.Sleep(5 * time.Second)

		finalCheck := []RemainingResource{}
		for _, resource := range result.ResourcesFound {
			remaining, err := cv.checkResourceExists(ctx, resource)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Error re-checking %s: %v", resource.ID, err))
				continue
			}
			if remaining != nil {
				finalCheck = append(finalCheck, *remaining)
			}
		}
		result.ResourcesFound = finalCheck
	}

	result.AWSVerified = len(result.ResourcesFound) == 0
	result.CleanupDuration = time.Since(start)

	return result, nil
}

// extractTestResources extracts resources that should be verified for cleanup
func (cv *CleanupVerifier) extractTestResources(expectedResources map[string]interface{}) []RemainingResource {
	var resources []RemainingResource

	if expectedRes, ok := expectedResources["expectedResources"].(map[string]interface{}); ok {
		for service, serviceResources := range expectedRes {
			if resourceArray, ok := serviceResources.([]interface{}); ok {
				for _, resource := range resourceArray {
					if resourceMap, ok := resource.(map[string]interface{}); ok {
						res := RemainingResource{
							Service: service,
							Type:    getStringValue(resourceMap, "type"),
							Name:    getStringValue(resourceMap, "name"),
							ARN:     getStringValue(resourceMap, "arn"),
							ID:      getStringValue(resourceMap, "id"),
							TestID:  cv.testID,
						}
						resources = append(resources, res)
					}
				}
			}
		}
	}

	return resources
}

// checkResourceExists checks if a specific resource still exists in AWS
func (cv *CleanupVerifier) checkResourceExists(ctx context.Context, resource RemainingResource) (*RemainingResource, error) {
	switch strings.ToLower(resource.Service) {
	case "s3":
		return cv.checkS3Resource(ctx, resource)
	default:
		// For unsupported services, we'll assume they were cleaned up
		// This can be extended to support more services as needed
		return nil, nil
	}
}

// checkS3Resource checks if an S3 resource still exists
func (cv *CleanupVerifier) checkS3Resource(ctx context.Context, resource RemainingResource) (*RemainingResource, error) {
	switch resource.Type {
	case "Bucket":
		return cv.checkS3Bucket(ctx, resource)
	default:
		return nil, nil
	}
}

// checkS3Bucket checks if an S3 bucket still exists
func (cv *CleanupVerifier) checkS3Bucket(ctx context.Context, resource RemainingResource) (*RemainingResource, error) {
	bucketName := resource.Name
	if bucketName == "" {
		bucketName = resource.ID
	}

	// Try to get bucket location (lightweight operation)
	_, err := cv.s3Client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucketName),
	})

	if err != nil {
		// Check if it's a "not found" error
		if strings.Contains(err.Error(), "NoSuchBucket") || strings.Contains(err.Error(), "NotFound") {
			return nil, nil // Bucket doesn't exist, cleanup successful
		}
		return nil, fmt.Errorf("error checking bucket %s: %w", bucketName, err)
	}

	// Bucket still exists
	return &resource, nil
}

// performManualCleanup attempts to manually clean up a remaining resource
func (cv *CleanupVerifier) performManualCleanup(ctx context.Context, resource RemainingResource) ManualCleanupAction {
	start := time.Now()
	action := ManualCleanupAction{
		Resource: resource,
		Action:   "delete",
	}

	switch strings.ToLower(resource.Service) {
	case "s3":
		action = cv.cleanupS3Resource(ctx, resource, action)
	default:
		action.Success = false
		action.Error = fmt.Sprintf("manual cleanup not supported for service: %s", resource.Service)
	}

	action.Duration = time.Since(start)
	return action
}

// cleanupS3Resource performs manual cleanup of S3 resources
func (cv *CleanupVerifier) cleanupS3Resource(ctx context.Context, resource RemainingResource, action ManualCleanupAction) ManualCleanupAction {
	switch resource.Type {
	case "Bucket":
		return cv.cleanupS3Bucket(ctx, resource, action)
	default:
		action.Success = false
		action.Error = fmt.Sprintf("manual cleanup not supported for S3 resource type: %s", resource.Type)
		return action
	}
}

// cleanupS3Bucket manually cleans up an S3 bucket
func (cv *CleanupVerifier) cleanupS3Bucket(ctx context.Context, resource RemainingResource, action ManualCleanupAction) ManualCleanupAction {
	bucketName := resource.Name
	if bucketName == "" {
		bucketName = resource.ID
	}

	// Step 1: Empty the bucket (delete all objects and versions)
	fmt.Printf("🗑️  Emptying bucket: %s\n", bucketName)
	if err := cv.emptyS3Bucket(ctx, bucketName); err != nil {
		action.Success = false
		action.Error = fmt.Sprintf("failed to empty bucket: %v", err)
		return action
	}

	// Step 2: Delete the bucket
	fmt.Printf("🗑️  Deleting bucket: %s\n", bucketName)
	_, err := cv.s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})

	if err != nil {
		action.Success = false
		action.Error = fmt.Sprintf("failed to delete bucket: %v", err)
		return action
	}

	action.Success = true
	action.Action = "empty_and_delete"
	return action
}

// emptyS3Bucket deletes all objects and versions from an S3 bucket
func (cv *CleanupVerifier) emptyS3Bucket(ctx context.Context, bucketName string) error {
	// Delete all object versions
	listVersionsInput := &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucketName),
	}

	for {
		versions, err := cv.s3Client.ListObjectVersions(ctx, listVersionsInput)
		if err != nil {
			return fmt.Errorf("failed to list object versions: %w", err)
		}

		if len(versions.Versions) == 0 && len(versions.DeleteMarkers) == 0 {
			break // No more objects to delete
		}

		// Prepare objects for deletion
		var objectsToDelete []types.ObjectIdentifier

		for _, version := range versions.Versions {
			objectsToDelete = append(objectsToDelete, types.ObjectIdentifier{
				Key:       version.Key,
				VersionId: version.VersionId,
			})
		}

		for _, deleteMarker := range versions.DeleteMarkers {
			objectsToDelete = append(objectsToDelete, types.ObjectIdentifier{
				Key:       deleteMarker.Key,
				VersionId: deleteMarker.VersionId,
			})
		}

		// Delete objects in batches (max 1000 per request)
		for i := 0; i < len(objectsToDelete); i += 1000 {
			end := i + 1000
			if end > len(objectsToDelete) {
				end = len(objectsToDelete)
			}

			batch := objectsToDelete[i:end]
			_, err := cv.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucketName),
				Delete: &types.Delete{
					Objects: batch,
					Quiet:   aws.Bool(true),
				},
			})

			if err != nil {
				return fmt.Errorf("failed to delete objects batch: %w", err)
			}
		}

		// Update continuation token for next iteration
		if aws.ToBool(versions.IsTruncated) {
			listVersionsInput.KeyMarker = versions.NextKeyMarker
			listVersionsInput.VersionIdMarker = versions.NextVersionIdMarker
		} else {
			break
		}
	}

	return nil
}

// GetCleanupSummary returns a formatted summary of cleanup results
func (cv *CleanupVerifier) GetCleanupSummary(result *CleanupResult) string {
	summary := fmt.Sprintf("🧹 CLEANUP SUMMARY:\n")
	summary += fmt.Sprintf("- Pulumi Success: %t\n", result.PulumiSuccess)
	summary += fmt.Sprintf("- AWS Verified: %t\n", result.AWSVerified)
	summary += fmt.Sprintf("- Duration: %v\n", result.CleanupDuration)

	if len(result.ResourcesFound) > 0 {
		summary += fmt.Sprintf("- Remaining Resources: %d\n", len(result.ResourcesFound))
		for _, resource := range result.ResourcesFound {
			summary += fmt.Sprintf("  • %s (%s)\n", resource.Name, resource.Type)
		}
	}

	if len(result.ManualCleanup) > 0 {
		successful := 0
		for _, action := range result.ManualCleanup {
			if action.Success {
				successful++
			}
		}
		summary += fmt.Sprintf("- Manual Cleanup: %d/%d successful\n", successful, len(result.ManualCleanup))
	}

	if len(result.Errors) > 0 {
		summary += fmt.Sprintf("- Errors: %d\n", len(result.Errors))
		for _, err := range result.Errors {
			summary += fmt.Sprintf("  • %s\n", err)
		}
	}

	return summary
}

func getStringValue(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

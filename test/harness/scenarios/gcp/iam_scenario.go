//go:build integration

package gcp

import (
	"fmt"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// IAMScenario creates GCP service accounts and IAM bindings
type IAMScenario struct {
	expectedResources map[string]interface{}
}

// NewIAMScenario creates a new GCP IAM scenario
func NewIAMScenario() automation.Scenario {
	return &IAMScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *IAMScenario) GetName() string {
	return "gcp-iam"
}

// GetServices returns the GCP services this scenario tests
func (s *IAMScenario) GetServices() []string {
	return []string{"serviceaccount", "projects", "storage"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *IAMScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common labels for all resources
	labels := pulumi.StringMap{
		"test-harness": pulumi.String("true"),
		"test-id":      pulumi.String(testID),
		"scenario":     pulumi.String("gcp-iam"),
		"created-by":   pulumi.String("corkscrew-test"),
	}

	// Create service account for storage access
	storageServiceAccount, err := serviceaccount.NewAccount(ctx, "storage-sa", &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(fmt.Sprintf("corkscrew-storage-%s", testID)),
		DisplayName: pulumi.String("Corkscrew Storage Service Account"),
		Description: pulumi.String("Service account for storage access in Corkscrew test"),
	})
	if err != nil {
		return fmt.Errorf("failed to create storage service account: %w", err)
	}

	// Create service account for compute access
	computeServiceAccount, err := serviceaccount.NewAccount(ctx, "compute-sa", &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(fmt.Sprintf("corkscrew-compute-%s", testID)),
		DisplayName: pulumi.String("Corkscrew Compute Service Account"),
		Description: pulumi.String("Service account for compute access in Corkscrew test"),
	})
	if err != nil {
		return fmt.Errorf("failed to create compute service account: %w", err)
	}

	// Create GCS bucket for testing permissions
	shortID := testID
	if len(testID) > 15 {
		shortID = testID[:15]
	}
	bucketName := fmt.Sprintf("corkscrew-iam-test-%s", shortID)

	bucket, err := storage.NewBucket(ctx, "iam-test-bucket", &storage.BucketArgs{
		Name:         pulumi.String(bucketName),
		Location:     pulumi.String("US"),
		StorageClass: pulumi.String("STANDARD"),
		Labels:       labels,
		UniformBucketLevelAccess: &storage.BucketUniformBucketLevelAccessArgs{
			Enabled: pulumi.Bool(true),
		},
		PublicAccessPrevention: pulumi.String("enforced"),
	})
	if err != nil {
		return fmt.Errorf("failed to create test bucket: %w", err)
	}

	// Get current project for IAM bindings
	current := projects.GetProjectOutput(ctx, projects.GetProjectOutputArgs{}, nil)
	projectId := current.ProjectId()

	// Create IAM binding - Storage Object Viewer for storage service account
	storageBinding, err := projects.NewIAMBinding(ctx, "storage-object-viewer", &projects.IAMBindingArgs{
		Project: projectId,
		Role:    pulumi.String("roles/storage.objectViewer"),
		Members: pulumi.StringArray{
			pulumi.Sprintf("serviceAccount:%s", storageServiceAccount.Email),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create storage object viewer binding: %w", err)
	}

	// Create IAM binding - Storage Object Admin for storage service account on specific bucket
	bucketBinding, err := storage.NewBucketIAMBinding(ctx, "bucket-admin", &storage.BucketIAMBindingArgs{
		Bucket: bucket.Name,
		Role:   pulumi.String("roles/storage.objectAdmin"),
		Members: pulumi.StringArray{
			pulumi.Sprintf("serviceAccount:%s", storageServiceAccount.Email),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create bucket admin binding: %w", err)
	}

	// Create IAM binding - Compute Instance Admin for compute service account
	computeBinding, err := projects.NewIAMBinding(ctx, "compute-instance-admin", &projects.IAMBindingArgs{
		Project: projectId,
		Role:    pulumi.String("roles/compute.instanceAdmin"),
		Members: pulumi.StringArray{
			pulumi.Sprintf("serviceAccount:%s", computeServiceAccount.Email),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create compute instance admin binding: %w", err)
	}

	// Create IAM member - Monitoring Metric Writer for storage service account
	monitoringMember, err := projects.NewIAMMember(ctx, "monitoring-metric-writer", &projects.IAMMemberArgs{
		Project: projectId,
		Role:    pulumi.String("roles/monitoring.metricWriter"),
		Member:  pulumi.Sprintf("serviceAccount:%s", storageServiceAccount.Email),
	})
	if err != nil {
		return fmt.Errorf("failed to create monitoring metric writer member: %w", err)
	}

	// Create service account key for storage service account
	storageKey, err := serviceaccount.NewKey(ctx, "storage-sa-key", &serviceaccount.KeyArgs{
		ServiceAccountId: storageServiceAccount.Name,
		PublicKeyType:    pulumi.String("TYPE_X509_PEM_FILE"),
	})
	if err != nil {
		return fmt.Errorf("failed to create storage service account key: %w", err)
	}

	// Export resource details for verification
	ctx.Export("storageServiceAccountEmail", storageServiceAccount.Email)
	ctx.Export("storageServiceAccountId", storageServiceAccount.ID())
	ctx.Export("computeServiceAccountEmail", computeServiceAccount.Email)
	ctx.Export("computeServiceAccountId", computeServiceAccount.ID())
	ctx.Export("bucketName", bucket.Name)
	ctx.Export("bucketId", bucket.ID())
	ctx.Export("storageBindingId", storageBinding.ID())
	ctx.Export("bucketBindingId", bucketBinding.ID())
	ctx.Export("computeBindingId", computeBinding.ID())
	ctx.Export("monitoringMemberId", monitoringMember.ID())
	ctx.Export("storageKeyId", storageKey.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"serviceaccount": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Account"),
				"name": storageServiceAccount.Email,
				"id":   storageServiceAccount.ID(),
				"attributes": pulumi.Map{
					"account_id":   pulumi.String(fmt.Sprintf("corkscrew-storage-%s", testID)),
					"display_name": pulumi.String("Corkscrew Storage Service Account"),
					"account_type": pulumi.String("storage"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Account"),
				"name": computeServiceAccount.Email,
				"id":   computeServiceAccount.ID(),
				"attributes": pulumi.Map{
					"account_id":   pulumi.String(fmt.Sprintf("corkscrew-compute-%s", testID)),
					"display_name": pulumi.String("Corkscrew Compute Service Account"),
					"account_type": pulumi.String("compute"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Key"),
				"id":   storageKey.ID(),
				"attributes": pulumi.Map{
					"public_key_type": pulumi.String("TYPE_X509_PEM_FILE"),
					"key_algorithm":   pulumi.String("KEY_ALG_RSA_2048"),
				},
			},
		},
		"projects": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("IAMBinding"),
				"id":   storageBinding.ID(),
				"attributes": pulumi.Map{
					"role":         pulumi.String("roles/storage.objectViewer"),
					"binding_type": pulumi.String("project"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("IAMBinding"),
				"id":   computeBinding.ID(),
				"attributes": pulumi.Map{
					"role":         pulumi.String("roles/compute.instanceAdmin"),
					"binding_type": pulumi.String("project"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("IAMMember"),
				"id":   monitoringMember.ID(),
				"attributes": pulumi.Map{
					"role":        pulumi.String("roles/monitoring.metricWriter"),
					"member_type": pulumi.String("project"),
				},
			},
		},
		"storage": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"name": bucket.Name,
				"id":   bucket.ID(),
				"attributes": pulumi.Map{
					"location":                    pulumi.String("US"),
					"storage_class":               pulumi.String("STANDARD"),
					"uniform_bucket_level_access": pulumi.Bool(true),
					"public_access_prevention":    pulumi.String("enforced"),
				},
				"labels": labels,
			},
			pulumi.Map{
				"type": pulumi.String("BucketIAMBinding"),
				"id":   bucketBinding.ID(),
				"attributes": pulumi.Map{
					"role":         pulumi.String("roles/storage.objectAdmin"),
					"binding_type": pulumi.String("bucket"),
				},
			},
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"serviceaccount": []interface{}{
				map[string]interface{}{
					"type": "Account",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"account_id":   fmt.Sprintf("corkscrew-storage-%s", testID),
						"display_name": "Corkscrew Storage Service Account",
						"account_type": "storage",
					},
				},
				map[string]interface{}{
					"type": "Account",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"account_id":   fmt.Sprintf("corkscrew-compute-%s", testID),
						"display_name": "Corkscrew Compute Service Account",
						"account_type": "compute",
					},
				},
				map[string]interface{}{
					"type": "Key",
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"public_key_type": "TYPE_X509_PEM_FILE",
						"key_algorithm":   "KEY_ALG_RSA_2048",
					},
				},
			},
			"projects": []interface{}{
				map[string]interface{}{
					"type": "IAMBinding",
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"role":         "roles/storage.objectViewer",
						"binding_type": "project",
					},
				},
				map[string]interface{}{
					"type": "IAMBinding",
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"role":         "roles/compute.instanceAdmin",
						"binding_type": "project",
					},
				},
				map[string]interface{}{
					"type": "IAMMember",
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"role":        "roles/monitoring.metricWriter",
						"member_type": "project",
					},
				},
			},
			"storage": []interface{}{
				map[string]interface{}{
					"type": "Bucket",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"location":                    "US",
						"storage_class":               "STANDARD",
						"uniform_bucket_level_access": true,
						"public_access_prevention":    "enforced",
					},
					"labels": map[string]string{
						"test-harness": "true",
						"test-id":      testID,
						"scenario":     "gcp-iam",
						"created-by":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "BucketIAMBinding",
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"role":         "roles/storage.objectAdmin",
						"binding_type": "bucket",
					},
				},
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *IAMScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

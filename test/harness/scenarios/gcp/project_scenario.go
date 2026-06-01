package gcp

import (
	"fmt"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ProjectScenario creates basic GCP project resources with GCS bucket and compute instance
type ProjectScenario struct {
	expectedResources map[string]interface{}
}

// NewProjectScenario creates a new GCP project scenario
func NewProjectScenario() automation.Scenario {
	return &ProjectScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *ProjectScenario) GetName() string {
	return "gcp-project"
}

// GetServices returns the GCP services this scenario tests
func (s *ProjectScenario) GetServices() []string {
	return []string{"storage", "compute"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *ProjectScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common labels for all resources
	labels := pulumi.StringMap{
		"test-harness": pulumi.String("true"),
		"test-id":      pulumi.String(testID),
		"scenario":     pulumi.String("gcp-project"),
		"created-by":   pulumi.String("corkscrew-test"),
	}

	// Create GCS bucket with unique name
	shortID := testID
	if len(testID) > 20 {
		shortID = testID[:20]
	}
	bucketName := fmt.Sprintf("corkscrew-test-%s", shortID)

	bucket, err := storage.NewBucket(ctx, "test-bucket", &storage.BucketArgs{
		Name:         pulumi.String(bucketName),
		Location:     pulumi.String("US"),
		StorageClass: pulumi.String("STANDARD"),
		Labels:       labels,
		Versioning: &storage.BucketVersioningArgs{
			Enabled: pulumi.Bool(true),
		},
		UniformBucketLevelAccess: &storage.BucketUniformBucketLevelAccessArgs{
			Enabled: pulumi.Bool(true),
		},
		PublicAccessPrevention: pulumi.String("enforced"),
	})
	if err != nil {
		return fmt.Errorf("failed to create GCS bucket: %w", err)
	}

	// Create Compute instance
	instance, err := compute.NewInstance(ctx, "test-instance", &compute.InstanceArgs{
		Name:        pulumi.String(fmt.Sprintf("corkscrew-test-%s", testID)),
		MachineType: pulumi.String("e2-micro"),
		Zone:        pulumi.String("us-central1-a"),
		BootDisk: &compute.InstanceBootDiskArgs{
			InitializeParams: &compute.InstanceBootDiskInitializeParamsArgs{
				Image: pulumi.String("debian-cloud/debian-11"),
				Size:  pulumi.Int(10),
				Type:  pulumi.String("pd-standard"),
			},
		},
		NetworkInterfaces: compute.InstanceNetworkInterfaceArray{
			&compute.InstanceNetworkInterfaceArgs{
				Network: pulumi.String("default"),
				AccessConfigs: compute.InstanceNetworkInterfaceAccessConfigArray{
					&compute.InstanceNetworkInterfaceAccessConfigArgs{
						NatIp:       pulumi.String(""),
						NetworkTier: pulumi.String("PREMIUM"),
					},
				},
			},
		},
		Metadata: pulumi.StringMap{
			"startup-script": pulumi.String("#!/bin/bash\necho 'Corkscrew test instance started' > /var/log/startup.log"),
		},
		Labels: labels,
		Tags:   pulumi.StringArray{pulumi.String("corkscrew-test"), pulumi.String("http-server")},
		ServiceAccount: &compute.InstanceServiceAccountArgs{
			Scopes: pulumi.StringArray{
				pulumi.String("https://www.googleapis.com/auth/cloud-platform"),
			},
		},
		AllowStoppingForUpdate: pulumi.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to create compute instance: %w", err)
	}

	// Create firewall rule for the instance
	firewall, err := compute.NewFirewall(ctx, "test-firewall", &compute.FirewallArgs{
		Name:    pulumi.String(fmt.Sprintf("corkscrew-firewall-%s", testID)),
		Network: pulumi.String("default"),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports: pulumi.StringArray{
					pulumi.String("80"),
					pulumi.String("443"),
				},
			},
		},
		SourceRanges: pulumi.StringArray{
			pulumi.String("0.0.0.0/0"),
		},
		TargetTags: pulumi.StringArray{
			pulumi.String("http-server"),
		},
		Description: pulumi.String("Allow HTTP and HTTPS traffic for Corkscrew test"),
	})
	if err != nil {
		return fmt.Errorf("failed to create firewall rule: %w", err)
	}

	// Export resource details for verification
	ctx.Export("bucketName", bucket.Name)
	ctx.Export("bucketUrl", bucket.Url)
	ctx.Export("bucketId", bucket.ID())
	ctx.Export("instanceName", instance.Name)
	ctx.Export("instanceId", instance.ID())
	ctx.Export("instanceSelfLink", instance.SelfLink)
	ctx.Export("firewallName", firewall.Name)
	ctx.Export("firewallId", firewall.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"storage": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"name": bucket.Name,
				"id":   bucket.ID(),
				"attributes": pulumi.Map{
					"location":                    pulumi.String("US"),
					"storage_class":               pulumi.String("STANDARD"),
					"versioning_enabled":          pulumi.Bool(true),
					"uniform_bucket_level_access": pulumi.Bool(true),
					"public_access_prevention":    pulumi.String("enforced"),
				},
				"labels": labels,
			},
		},
		"compute": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Instance"),
				"name": instance.Name,
				"id":   instance.ID(),
				"attributes": pulumi.Map{
					"machine_type": pulumi.String("e2-micro"),
					"zone":         pulumi.String("us-central1-a"),
					"disk_size":    pulumi.Int(10),
					"disk_type":    pulumi.String("pd-standard"),
					"image":        pulumi.String("debian-cloud/debian-11"),
				},
				"labels": labels,
			},
			pulumi.Map{
				"type": pulumi.String("Firewall"),
				"name": firewall.Name,
				"id":   firewall.ID(),
				"attributes": pulumi.Map{
					"network":       pulumi.String("default"),
					"protocols":     pulumi.StringArray{pulumi.String("tcp")},
					"ports":         pulumi.StringArray{pulumi.String("80"), pulumi.String("443")},
					"source_ranges": pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					"target_tags":   pulumi.StringArray{pulumi.String("http-server")},
				},
			},
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"storage": []interface{}{
				map[string]interface{}{
					"type": "Bucket",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"location":                    "US",
						"storage_class":               "STANDARD",
						"versioning_enabled":          true,
						"uniform_bucket_level_access": true,
						"public_access_prevention":    "enforced",
					},
					"labels": map[string]string{
						"test-harness": "true",
						"test-id":      testID,
						"scenario":     "gcp-project",
						"created-by":   "corkscrew-test",
					},
				},
			},
			"compute": []interface{}{
				map[string]interface{}{
					"type": "Instance",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"machine_type": "e2-micro",
						"zone":         "us-central1-a",
						"disk_size":    10,
						"disk_type":    "pd-standard",
						"image":        "debian-cloud/debian-11",
					},
					"labels": map[string]string{
						"test-harness": "true",
						"test-id":      testID,
						"scenario":     "gcp-project",
						"created-by":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "Firewall",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"network":       "default",
						"protocols":     []string{"tcp"},
						"ports":         []string{"80", "443"},
						"source_ranges": []string{"0.0.0.0/0"},
						"target_tags":   []string{"http-server"},
					},
				},
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *ProjectScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

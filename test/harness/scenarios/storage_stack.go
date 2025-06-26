package scenarios

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ebs"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/efs"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// StorageStackScenario creates S3 buckets, EBS volumes, and EFS file systems
type StorageStackScenario struct {
	expectedResources map[string]interface{}
}

// NewStorageStackScenario creates a new storage stack scenario
func NewStorageStackScenario() *StorageStackScenario {
	return &StorageStackScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *StorageStackScenario) GetName() string {
	return "storage-stack"
}

// GetServices returns the AWS services this scenario tests
func (s *StorageStackScenario) GetServices() []string {
	return []string{"s3", "ebs", "efs", "ec2"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *StorageStackScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("storage-stack"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create VPC for EFS (required for mount targets)
	vpc, err := ec2.NewVpc(ctx, "storage-vpc", &ec2.VpcArgs{
		CidrBlock: pulumi.String("10.2.0.0/16"),
		Tags:      tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create VPC: %w", err)
	}

	// Create subnet for EFS mount target
	subnet, err := ec2.NewSubnet(ctx, "storage-subnet", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String("10.2.1.0/24"),
		AvailabilityZone: pulumi.String("us-east-1a"),
		Tags:             tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w", err)
	}

	// Create multiple S3 buckets with different configurations
	// 1. Standard bucket with versioning
	standardBucket, err := s3.NewBucket(ctx, "standard-bucket", &s3.BucketArgs{
		BucketPrefix: pulumi.String(fmt.Sprintf("corkscrew-standard-%s", testID)),
		Tags:         tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create standard bucket: %w", err)
	}

	_, err = s3.NewBucketVersioningV2(ctx, "standard-versioning", &s3.BucketVersioningV2Args{
		Bucket: standardBucket.ID(),
		VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
			Status: pulumi.String("Enabled"),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to enable standard bucket versioning: %w", err)
	}

	// 2. Encrypted bucket
	encryptedBucket, err := s3.NewBucket(ctx, "encrypted-bucket", &s3.BucketArgs{
		BucketPrefix: pulumi.String(fmt.Sprintf("corkscrew-encrypted-%s", testID)),
		Tags:         tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create encrypted bucket: %w", err)
	}

	_, err = s3.NewBucketServerSideEncryptionConfigurationV2(ctx, "encrypted-bucket-sse", &s3.BucketServerSideEncryptionConfigurationV2Args{
		Bucket: encryptedBucket.ID(),
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

	// 3. Bucket with lifecycle configuration
	lifecycleBucket, err := s3.NewBucket(ctx, "lifecycle-bucket", &s3.BucketArgs{
		BucketPrefix: pulumi.String(fmt.Sprintf("corkscrew-lifecycle-%s", testID)),
		Tags:         tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create lifecycle bucket: %w", err)
	}

	_, err = s3.NewBucketLifecycleConfigurationV2(ctx, "lifecycle-config", &s3.BucketLifecycleConfigurationV2Args{
		Bucket: lifecycleBucket.ID(),
		Rules: s3.BucketLifecycleConfigurationV2RuleArray{
			&s3.BucketLifecycleConfigurationV2RuleArgs{
				Id:     pulumi.String("test-lifecycle-rule"),
				Status: pulumi.String("Enabled"),
				Transitions: s3.BucketLifecycleConfigurationV2RuleTransitionArray{
					&s3.BucketLifecycleConfigurationV2RuleTransitionArgs{
						Days:         pulumi.Int(30),
						StorageClass: pulumi.String("STANDARD_IA"),
					},
					&s3.BucketLifecycleConfigurationV2RuleTransitionArgs{
						Days:         pulumi.Int(90),
						StorageClass: pulumi.String("GLACIER"),
					},
				},
				Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{
					Days: pulumi.Int(365),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to configure lifecycle: %w", err)
	}

	// Create EBS volumes with different types
	// 1. GP3 volume
	gp3Volume, err := ebs.NewVolume(ctx, "gp3-volume", &ebs.VolumeArgs{
		AvailabilityZone: pulumi.String("us-east-1a"),
		Size:             pulumi.Int(20),
		Type:             pulumi.String("gp3"),
		Iops:             pulumi.Int(3000),
		Throughput:       pulumi.Int(125),
		Encrypted:        pulumi.Bool(true),
		Tags:             tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create GP3 volume: %w", err)
	}

	// 2. IO2 volume for high IOPS
	io2Volume, err := ebs.NewVolume(ctx, "io2-volume", &ebs.VolumeArgs{
		AvailabilityZone: pulumi.String("us-east-1a"),
		Size:             pulumi.Int(100),
		Type:             pulumi.String("io2"),
		Iops:             pulumi.Int(1000),
		Encrypted:        pulumi.Bool(true),
		Tags:             tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create IO2 volume: %w", err)
	}

	// 3. Standard volume
	standardVolume, err := ebs.NewVolume(ctx, "standard-volume", &ebs.VolumeArgs{
		AvailabilityZone: pulumi.String("us-east-1a"),
		Size:             pulumi.Int(50),
		Type:             pulumi.String("gp2"),
		Tags:             tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create standard volume: %w", err)
	}

	// Create EFS file system
	efsFileSystem, err := efs.NewFileSystem(ctx, "test-efs", &efs.FileSystemArgs{
		CreationToken:   pulumi.String(fmt.Sprintf("corkscrew-test-%s", testID)),
		PerformanceMode: pulumi.String("generalPurpose"),
		ThroughputMode:  pulumi.String("provisioned"),
		ProvisionedThroughputInMibps: pulumi.Float64(100),
		Encrypted: pulumi.Bool(true),
		Tags:      tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create EFS file system: %w", err)
	}

	// Create security group for EFS
	efsSecurityGroup, err := ec2.NewSecurityGroup(ctx, "efs-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Security group for EFS mount targets"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("tcp"),
				FromPort:   pulumi.Int(2049),
				ToPort:     pulumi.Int(2049),
				CidrBlocks: pulumi.StringArray{pulumi.String("10.2.0.0/16")},
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create EFS security group: %w", err)
	}

	// Create EFS mount target
	efsMountTarget, err := efs.NewMountTarget(ctx, "efs-mount-target", &efs.MountTargetArgs{
		FileSystemId:   efsFileSystem.ID(),
		SubnetId:       subnet.ID(),
		SecurityGroups: pulumi.StringArray{efsSecurityGroup.ID()},
	})
	if err != nil {
		return fmt.Errorf("failed to create EFS mount target: %w", err)
	}

	// Create EFS access point
	efsAccessPoint, err := efs.NewAccessPoint(ctx, "efs-access-point", &efs.AccessPointArgs{
		FileSystemId: efsFileSystem.ID(),
		PosixUser: &efs.AccessPointPosixUserArgs{
			Gid: pulumi.Int(1000),
			Uid: pulumi.Int(1000),
		},
		RootDirectory: &efs.AccessPointRootDirectoryArgs{
			Path: pulumi.String("/test"),
			CreationInfo: &efs.AccessPointRootDirectoryCreationInfoArgs{
				OwnerGid:    pulumi.Int(1000),
				OwnerUid:    pulumi.Int(1000),
				Permissions: pulumi.String("0755"),
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create EFS access point: %w", err)
	}

	// Create EC2 instance to demonstrate volume attachment
	ec2Instance, err := ec2.NewInstance(ctx, "storage-instance", &ec2.InstanceArgs{
		Ami:          pulumi.String("ami-0c55b159cbfafe1f0"), // Amazon Linux 2
		InstanceType: pulumi.String("t2.micro"),
		SubnetId:     subnet.ID(),
		Tags:         tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create EC2 instance: %w", err)
	}

	// Attach GP3 volume to instance
	_, err = ec2.NewVolumeAttachment(ctx, "gp3-attachment", &ec2.VolumeAttachmentArgs{
		DeviceName: pulumi.String("/dev/sdf"),
		VolumeId:   gp3Volume.ID(),
		InstanceId: ec2Instance.ID(),
	})
	if err != nil {
		return fmt.Errorf("failed to attach GP3 volume: %w", err)
	}

	// Create EBS snapshot
	ebsSnapshot, err := ebs.NewSnapshot(ctx, "test-snapshot", &ebs.SnapshotArgs{
		VolumeId:    standardVolume.ID(),
		Description: pulumi.String("Test snapshot for Corkscrew integration tests"),
		Tags:        tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create EBS snapshot: %w", err)
	}

	// Export resource details for verification
	ctx.Export("standardBucketName", standardBucket.Bucket)
	ctx.Export("encryptedBucketName", encryptedBucket.Bucket)
	ctx.Export("lifecycleBucketName", lifecycleBucket.Bucket)
	ctx.Export("gp3VolumeId", gp3Volume.ID())
	ctx.Export("io2VolumeId", io2Volume.ID())
	ctx.Export("standardVolumeId", standardVolume.ID())
	ctx.Export("efsFileSystemId", efsFileSystem.ID())
	ctx.Export("efsAccessPointId", efsAccessPoint.ID())
	ctx.Export("ec2InstanceId", ec2Instance.ID())
	ctx.Export("ebsSnapshotId", ebsSnapshot.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"s3": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"name": standardBucket.Bucket,
				"attributes": pulumi.Map{
					"versioning_enabled": pulumi.Bool(true),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"name": encryptedBucket.Bucket,
				"attributes": pulumi.Map{
					"encryption_enabled": pulumi.Bool(true),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"name": lifecycleBucket.Bucket,
				"attributes": pulumi.Map{
					"lifecycle_configured": pulumi.Bool(true),
				},
			},
		},
		"ebs": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Volume"),
				"id":   gp3Volume.ID(),
				"attributes": pulumi.Map{
					"volume_type": pulumi.String("gp3"),
					"size":        pulumi.Int(20),
					"encrypted":   pulumi.Bool(true),
					"iops":        pulumi.Int(3000),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Volume"),
				"id":   io2Volume.ID(),
				"attributes": pulumi.Map{
					"volume_type": pulumi.String("io2"),
					"size":        pulumi.Int(100),
					"encrypted":   pulumi.Bool(true),
					"iops":        pulumi.Int(1000),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Volume"),
				"id":   standardVolume.ID(),
				"attributes": pulumi.Map{
					"volume_type": pulumi.String("gp2"),
					"size":        pulumi.Int(50),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Snapshot"),
				"id":   ebsSnapshot.ID(),
				"attributes": pulumi.Map{
					"description": pulumi.String("Test snapshot for Corkscrew integration tests"),
				},
			},
		},
		"efs": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("FileSystem"),
				"id":   efsFileSystem.ID(),
				"attributes": pulumi.Map{
					"performance_mode":   pulumi.String("generalPurpose"),
					"throughput_mode":    pulumi.String("provisioned"),
					"encrypted":          pulumi.Bool(true),
				},
			},
			pulumi.Map{
				"type": pulumi.String("AccessPoint"),
				"id":   efsAccessPoint.ID(),
				"attributes": pulumi.Map{
					"path": pulumi.String("/test"),
				},
			},
		},
		"ec2": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Instance"),
				"id":   ec2Instance.ID(),
				"attributes": pulumi.Map{
					"instance_type": pulumi.String("t2.micro"),
				},
			},
		},
	})

	// Export relationships for verification
	ctx.Export("relationships", pulumi.Map{
		"volume_to_instance": pulumi.Map{
			"from": gp3Volume.ID(),
			"to":   ec2Instance.ID(),
			"type": pulumi.String("attached_to"),
		},
		"snapshot_to_volume": pulumi.Map{
			"from": ebsSnapshot.ID(),
			"to":   standardVolume.ID(),
			"type": pulumi.String("snapshot_of"),
		},
		"efs_to_mount_target": pulumi.Map{
			"from": efsFileSystem.ID(),
			"to":   efsMountTarget.ID(),
			"type": pulumi.String("has_mount_target"),
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"s3": []interface{}{
				map[string]interface{}{
					"type": "Bucket",
					"attributes": map[string]interface{}{
						"versioning_enabled": true,
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "storage-stack",
						"CreatedBy":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "Bucket",
					"attributes": map[string]interface{}{
						"encryption_enabled": true,
					},
				},
				map[string]interface{}{
					"type": "Bucket",
					"attributes": map[string]interface{}{
						"lifecycle_configured": true,
					},
				},
			},
			"ebs": []interface{}{
				map[string]interface{}{
					"type": "Volume",
					"attributes": map[string]interface{}{
						"volume_type": "gp3",
						"size":        20,
						"encrypted":   true,
						"iops":        3000,
					},
				},
				map[string]interface{}{
					"type": "Volume",
					"attributes": map[string]interface{}{
						"volume_type": "io2",
						"size":        100,
						"encrypted":   true,
						"iops":        1000,
					},
				},
				map[string]interface{}{
					"type": "Volume",
					"attributes": map[string]interface{}{
						"volume_type": "gp2",
						"size":        50,
					},
				},
				map[string]interface{}{
					"type": "Snapshot",
					"attributes": map[string]interface{}{
						"description": "Test snapshot for Corkscrew integration tests",
					},
				},
			},
			"efs": []interface{}{
				map[string]interface{}{
					"type": "FileSystem",
					"attributes": map[string]interface{}{
						"performance_mode": "generalPurpose",
						"throughput_mode":  "provisioned",
						"encrypted":        true,
					},
				},
				map[string]interface{}{
					"type": "AccessPoint",
					"attributes": map[string]interface{}{
						"path": "/test",
					},
				},
			},
			"ec2": []interface{}{
				map[string]interface{}{
					"type": "Instance",
					"attributes": map[string]interface{}{
						"instance_type": "t2.micro",
					},
				},
			},
		},
		"relationships": map[string]interface{}{
			"volume_to_instance": map[string]interface{}{
				"type": "attached_to",
			},
			"snapshot_to_volume": map[string]interface{}{
				"type": "snapshot_of",
			},
			"efs_to_mount_target": map[string]interface{}{
				"type": "has_mount_target",
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *StorageStackScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}
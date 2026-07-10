//go:build integration

package aws

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// PerformanceScaledScenario creates a scalable number of resources for performance testing
type PerformanceScaledScenario struct {
	resourceCount     int
	expectedResources map[string]interface{}
}

// NewPerformanceScaledScenario creates a new performance scaled scenario
func NewPerformanceScaledScenario(resourceCount int) *PerformanceScaledScenario {
	return &PerformanceScaledScenario{
		resourceCount:     resourceCount,
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *PerformanceScaledScenario) GetName() string {
	return fmt.Sprintf("performance-scaled-%d", s.resourceCount)
}

// GetServices returns the AWS services this scenario tests
func (s *PerformanceScaledScenario) GetServices() []string {
	return []string{"s3", "ec2"}
}

// DefineResources creates the specified number of resources for performance testing
func (s *PerformanceScaledScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	baseTags := pulumi.StringMap{
		"TestHarness":   pulumi.String("true"),
		"TestID":        pulumi.String(testID),
		"Scenario":      pulumi.String("performance-scaled"),
		"CreatedBy":     pulumi.String("corkscrew-test"),
		"ResourceCount": pulumi.String(fmt.Sprintf("%d", s.resourceCount)),
	}

	// Create a VPC to house all resources
	vpc, err := ec2.NewVpc(ctx, "perf-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String("10.0.0.0/16"),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               baseTags,
	})
	if err != nil {
		return fmt.Errorf("failed to create VPC: %w", err)
	}

	// Create subnet for instances
	subnet, err := ec2.NewSubnet(ctx, "perf-subnet", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String("10.0.1.0/24"),
		AvailabilityZone: pulumi.String("us-east-1a"),
		Tags:             baseTags,
	})
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w", err)
	}

	// Calculate resource distribution
	// For balanced testing: 50% S3 buckets, 30% EC2 instances, 20% security groups
	s3Count := s.resourceCount / 2
	ec2Count := (s.resourceCount * 3) / 10
	sgCount := s.resourceCount / 5

	// Ensure minimum counts
	if s3Count == 0 && s.resourceCount > 0 {
		s3Count = 1
	}
	if ec2Count == 0 && s.resourceCount > 1 {
		ec2Count = 1
	}
	if sgCount == 0 && s.resourceCount > 2 {
		sgCount = 1
	}

	// Adjust if we've exceeded the target
	totalPlanned := s3Count + ec2Count + sgCount
	if totalPlanned > s.resourceCount {
		excess := totalPlanned - s.resourceCount
		// Remove excess from largest category first
		if s3Count >= excess {
			s3Count -= excess
		} else {
			ec2Count -= (excess - s3Count)
			s3Count = 0
		}
	}

	// Create S3 buckets
	buckets := make([]*s3.Bucket, s3Count)
	for i := 0; i < s3Count; i++ {
		bucketTags := make(pulumi.StringMap)
		for k, v := range baseTags {
			bucketTags[k] = v
		}
		bucketTags["ResourceIndex"] = pulumi.String(fmt.Sprintf("s3-%d", i))
		bucketTags["ResourceType"] = pulumi.String("S3Bucket")

		bucket, err := s3.NewBucket(ctx, fmt.Sprintf("perf-bucket-%d", i), &s3.BucketArgs{
			BucketPrefix: pulumi.String(fmt.Sprintf("corkscrew-perf-%s-%d-", testID[:8], i)),
			Tags:         bucketTags,
		})
		if err != nil {
			return fmt.Errorf("failed to create S3 bucket %d: %w", i, err)
		}
		buckets[i] = bucket

		// Add versioning to some buckets for variety
		if i%3 == 0 {
			_, err = s3.NewBucketVersioningV2(ctx, fmt.Sprintf("perf-versioning-%d", i), &s3.BucketVersioningV2Args{
				Bucket: bucket.ID(),
				VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
					Status: pulumi.String("Enabled"),
				},
			})
			if err != nil {
				return fmt.Errorf("failed to enable versioning on bucket %d: %w", i, err)
			}
		}

		// Add encryption to some buckets
		if i%2 == 0 {
			_, err = s3.NewBucketServerSideEncryptionConfigurationV2(ctx, fmt.Sprintf("perf-encryption-%d", i), &s3.BucketServerSideEncryptionConfigurationV2Args{
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
				return fmt.Errorf("failed to configure encryption on bucket %d: %w", i, err)
			}
		}
	}

	// Create security groups
	securityGroups := make([]*ec2.SecurityGroup, sgCount)
	for i := 0; i < sgCount; i++ {
		sgTags := make(pulumi.StringMap)
		for k, v := range baseTags {
			sgTags[k] = v
		}
		sgTags["ResourceIndex"] = pulumi.String(fmt.Sprintf("sg-%d", i))
		sgTags["ResourceType"] = pulumi.String("SecurityGroup")

		sg, err := ec2.NewSecurityGroup(ctx, fmt.Sprintf("perf-sg-%d", i), &ec2.SecurityGroupArgs{
			Name:        pulumi.String(fmt.Sprintf("corkscrew-perf-sg-%s-%d", testID, i)),
			Description: pulumi.String(fmt.Sprintf("Performance test security group %d", i)),
			VpcId:       vpc.ID(),
			Tags:        sgTags,

			// Add some ingress rules for variety
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					FromPort:   pulumi.Int(80 + i),
					ToPort:     pulumi.Int(80 + i),
					Protocol:   pulumi.String("tcp"),
					CidrBlocks: pulumi.StringArray{pulumi.String("10.0.0.0/16")},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create security group %d: %w", i, err)
		}
		securityGroups[i] = sg
	}

	// Create EC2 instances
	instances := make([]*ec2.Instance, ec2Count)
	for i := 0; i < ec2Count; i++ {
		instanceTags := make(pulumi.StringMap)
		for k, v := range baseTags {
			instanceTags[k] = v
		}
		instanceTags["ResourceIndex"] = pulumi.String(fmt.Sprintf("ec2-%d", i))
		instanceTags["ResourceType"] = pulumi.String("EC2Instance")
		instanceTags["Name"] = pulumi.String(fmt.Sprintf("corkscrew-perf-instance-%s-%d", testID, i))

		// Vary instance types for testing
		instanceType := "t2.nano"
		if i%5 == 0 {
			instanceType = "t2.micro"
		}

		// Select security group (round-robin)
		var securityGroupIds pulumi.StringArray
		if len(securityGroups) > 0 {
			sgIndex := i % len(securityGroups)
			securityGroupIds = pulumi.StringArray{securityGroups[sgIndex].ID()}
		}

		instance, err := ec2.NewInstance(ctx, fmt.Sprintf("perf-instance-%d", i), &ec2.InstanceArgs{
			Ami:                 pulumi.String("ami-0c55b159cbfafe1f0"), // Amazon Linux 2
			InstanceType:        pulumi.String(instanceType),
			SubnetId:            subnet.ID(),
			VpcSecurityGroupIds: securityGroupIds,
			Tags:                instanceTags,
			UserData: pulumi.String(fmt.Sprintf(`#!/bin/bash
echo "Performance test instance %d" > /tmp/instance-info.txt
echo "Test ID: %s" >> /tmp/instance-info.txt
echo "Resource count: %d" >> /tmp/instance-info.txt
echo "Instance index: %d" >> /tmp/instance-info.txt
`, i, testID, s.resourceCount, i)),
		})
		if err != nil {
			return fmt.Errorf("failed to create EC2 instance %d: %w", i, err)
		}
		instances[i] = instance
	}

	// Export resource counts and details
	ctx.Export("vpcId", vpc.ID())
	ctx.Export("subnetId", subnet.ID())
	ctx.Export("resourceCounts", pulumi.Map{
		"s3Buckets":      pulumi.Int(s3Count),
		"ec2Instances":   pulumi.Int(ec2Count),
		"securityGroups": pulumi.Int(sgCount),
		"total":          pulumi.Int(s3Count + ec2Count + sgCount + 2), // +2 for VPC and subnet
	})

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"s3": pulumi.Array{
			pulumi.Map{
				"type":  pulumi.String("Bucket"),
				"count": pulumi.Int(s3Count),
				"attributes": pulumi.Map{
					"versioning_enabled": pulumi.Int(s3Count / 3), // Every 3rd bucket
					"encryption_enabled": pulumi.Int(s3Count / 2), // Every 2nd bucket
				},
			},
		},
		"ec2": pulumi.Array{
			pulumi.Map{
				"type":  pulumi.String("VPC"),
				"count": pulumi.Int(1),
			},
			pulumi.Map{
				"type":  pulumi.String("Subnet"),
				"count": pulumi.Int(1),
			},
			pulumi.Map{
				"type":  pulumi.String("SecurityGroup"),
				"count": pulumi.Int(sgCount),
				"attributes": pulumi.Map{
					"ingress_rules": pulumi.Int(sgCount), // One rule per SG
				},
			},
			pulumi.Map{
				"type":  pulumi.String("Instance"),
				"count": pulumi.Int(ec2Count),
				"attributes": pulumi.Map{
					"user_data_set":   pulumi.Bool(true),
					"security_groups": pulumi.Int(sgCount),
				},
			},
		},
	})

	// Store expected resources for verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"s3": []interface{}{
				map[string]interface{}{
					"type":  "Bucket",
					"count": s3Count,
					"attributes": map[string]interface{}{
						"versioning_enabled": s3Count / 3,
						"encryption_enabled": s3Count / 2,
					},
				},
			},
			"ec2": []interface{}{
				map[string]interface{}{
					"type":  "VPC",
					"count": 1,
				},
				map[string]interface{}{
					"type":  "Subnet",
					"count": 1,
				},
				map[string]interface{}{
					"type":  "SecurityGroup",
					"count": sgCount,
					"attributes": map[string]interface{}{
						"ingress_rules": sgCount,
					},
				},
				map[string]interface{}{
					"type":  "Instance",
					"count": ec2Count,
					"attributes": map[string]interface{}{
						"user_data_set":   true,
						"security_groups": sgCount,
					},
				},
			},
		},
		"totalResources": s3Count + ec2Count + sgCount + 2,
		"resourceDistribution": map[string]int{
			"s3":              s3Count,
			"ec2_instances":   ec2Count,
			"security_groups": sgCount,
			"vpc_subnet":      2,
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *PerformanceScaledScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

//go:build integration

package aws

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudfront"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// EdgeCasesScenario tests various edge cases and boundary conditions
type EdgeCasesScenario struct {
	expectedResources map[string]interface{}
}

// NewEdgeCasesScenario creates a new edge cases test scenario
func NewEdgeCasesScenario() *EdgeCasesScenario {
	return &EdgeCasesScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *EdgeCasesScenario) GetName() string {
	return "edge-cases"
}

// GetServices returns the AWS services this scenario tests
func (s *EdgeCasesScenario) GetServices() []string {
	return []string{"s3", "ec2", "iam", "cloudfront"}
}

// DefineResources creates edge case resources for testing
func (s *EdgeCasesScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Generate maximum tags (AWS limit is 50 for most resources)
	maxTags := s.generateMaxTags(testID)

	// Test 1: S3 bucket with maximum tags and unicode name
	unicodeBucket, err := s.createUnicodeBucket(ctx, testID, maxTags)
	if err != nil {
		return err
	}

	// Test 2: EC2 instance with long name and special characters
	longNameInstance, err := s.createLongNameInstance(ctx, testID, maxTags)
	if err != nil {
		return err
	}

	// Test 3: IAM role with complex policy (global service)
	globalRole, err := s.createGlobalIAMRole(ctx, testID, maxTags)
	if err != nil {
		return err
	}

	// Test 4: CloudFront distribution (global service with regional origins)
	distribution, err := s.createCloudFrontDistribution(ctx, testID, unicodeBucket)
	if err != nil {
		return err
	}

	// Test 5: VPC with circular dependencies through security groups
	circularVPC, circularSG1, circularSG2, err := s.createCircularDependencies(ctx, testID, maxTags)
	if err != nil {
		return err
	}

	// Test 6: Resource with special state (stopped instance)
	stoppedInstance, err := s.createStoppedInstance(ctx, testID, maxTags, circularVPC)
	if err != nil {
		return err
	}

	// Export all resources for verification
	s.exportResources(ctx, map[string]interface{}{
		"unicodeBucket":    unicodeBucket,
		"longNameInstance": longNameInstance,
		"globalRole":       globalRole,
		"distribution":     distribution,
		"circularVPC":      circularVPC,
		"circularSG1":      circularSG1,
		"circularSG2":      circularSG2,
		"stoppedInstance":  stoppedInstance,
	}, testID, maxTags)

	return nil
}

// generateMaxTags creates the maximum number of tags allowed
func (s *EdgeCasesScenario) generateMaxTags(testID string) pulumi.StringMap {
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("edge-cases"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Add tags up to AWS limit (50), accounting for the 4 we already have
	for i := 5; i <= 50; i++ {
		key := fmt.Sprintf("EdgeTag%02d", i)
		// Include unicode and special characters in some values
		value := fmt.Sprintf("Val%d-测试-🔥-Special!@#$%%", i)
		tags[key] = pulumi.String(value)
	}

	return tags
}

// createUnicodeBucket creates an S3 bucket with unicode characters in name
func (s *EdgeCasesScenario) createUnicodeBucket(ctx *pulumi.Context, testID string, tags pulumi.StringMap) (*s3.Bucket, error) {
	// S3 bucket names have restrictions, so we use a safe name but unicode in tags
	bucketName := fmt.Sprintf("corkscrew-unicode-%s", strings.ToLower(testID[:10]))

	bucket, err := s3.NewBucket(ctx, "unicode-bucket", &s3.BucketArgs{
		Bucket: pulumi.String(bucketName),
		Tags:   tags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create unicode bucket: %w", err)
	}

	// Add lifecycle configuration with complex rules
	_, err = s3.NewBucketLifecycleConfigurationV2(ctx, "unicode-lifecycle", &s3.BucketLifecycleConfigurationV2Args{
		Bucket: bucket.ID(),
		Rules: s3.BucketLifecycleConfigurationV2RuleArray{
			&s3.BucketLifecycleConfigurationV2RuleArgs{
				Id:     pulumi.String("unicode-rule-测试"),
				Status: pulumi.String("Enabled"),
				Filter: &s3.BucketLifecycleConfigurationV2RuleFilterArgs{
					Prefix: pulumi.String("测试/"),
				},
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
		return nil, fmt.Errorf("failed to create lifecycle configuration: %w", err)
	}

	return bucket, nil
}

// createLongNameInstance creates an EC2 instance with very long name
func (s *EdgeCasesScenario) createLongNameInstance(ctx *pulumi.Context, testID string, tags pulumi.StringMap) (*ec2.Instance, error) {
	// Create a very long name (EC2 allows up to 255 characters for Name tag)
	longName := fmt.Sprintf("corkscrew-very-long-instance-name-with-special-chars-测试-🚀-"+
		"this-name-is-intentionally-very-long-to-test-edge-cases-in-resource-scanning-"+
		"and-database-storage-capabilities-including-unicode-characters-and-emojis-%s", testID)

	// Add the long name to tags
	instanceTags := make(pulumi.StringMap)
	for k, v := range tags {
		instanceTags[k] = v
	}
	instanceTags["Name"] = pulumi.String(longName)

	instance, err := ec2.NewInstance(ctx, "long-name-instance", &ec2.InstanceArgs{
		Ami:          pulumi.String("ami-0c55b159cbfafe1f0"), // Amazon Linux 2
		InstanceType: pulumi.String("t2.nano"),               // Smallest instance
		Tags:         instanceTags,
		UserData: pulumi.String(`#!/bin/bash
# User data with unicode and special characters
echo "测试 Edge Case Instance 🔥" > /tmp/edge-test.txt
echo "Special chars: !@#$%^&*()_+-=[]{}|;':\",./<>?" >> /tmp/edge-test.txt
`),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create long name instance: %w", err)
	}

	return instance, nil
}

// createGlobalIAMRole creates an IAM role (global service) with complex policy
func (s *EdgeCasesScenario) createGlobalIAMRole(ctx *pulumi.Context, testID string, tags pulumi.StringMap) (*iam.Role, error) {
	// IAM role name with unicode is not allowed, but we can test complex policies
	roleName := fmt.Sprintf("corkscrew-edge-role-%s", testID)

	assumeRolePolicy := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {
					"Service": "ec2.amazonaws.com"
				},
				"Action": "sts:AssumeRole",
				"Condition": {
					"StringEquals": {
						"sts:ExternalId": "unicode-测试-🔥"
					}
				}
			}
		]
	}`

	role, err := iam.NewRole(ctx, "global-edge-role", &iam.RoleArgs{
		Name:             pulumi.String(roleName),
		AssumeRolePolicy: pulumi.String(assumeRolePolicy),
		Tags:             tags,
		Description:      pulumi.String("Edge case IAM role with unicode description: 测试 🚀"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create global IAM role: %w", err)
	}

	// Attach a complex inline policy
	complexPolicy := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Action": [
					"s3:GetObject",
					"s3:PutObject"
				],
				"Resource": "arn:aws:s3:::*测试*/*",
				"Condition": {
					"StringLike": {
						"s3:prefix": ["unicode/测试/*", "emoji/🔥/*"]
					}
				}
			},
			{
				"Effect": "Deny",
				"Action": "*",
				"Resource": "*",
				"Condition": {
					"Bool": {
						"aws:SecureTransport": "false"
					}
				}
			}
		]
	}`

	_, err = iam.NewRolePolicy(ctx, "edge-policy", &iam.RolePolicyArgs{
		Role:   role.ID(),
		Name:   pulumi.String("EdgeCasePolicy"),
		Policy: pulumi.String(complexPolicy),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create complex policy: %w", err)
	}

	return role, nil
}

// createCloudFrontDistribution creates a CloudFront distribution (global service)
func (s *EdgeCasesScenario) createCloudFrontDistribution(ctx *pulumi.Context, testID string, originBucket *s3.Bucket) (*cloudfront.Distribution, error) {
	distribution, err := cloudfront.NewDistribution(ctx, "edge-distribution", &cloudfront.DistributionArgs{
		Comment: pulumi.String(fmt.Sprintf("Edge case distribution 测试 🌐 - %s", testID)),
		Enabled: pulumi.Bool(true),

		Origins: cloudfront.DistributionOriginArray{
			&cloudfront.DistributionOriginArgs{
				DomainName: originBucket.BucketDomainName,
				OriginId:   pulumi.String("edge-origin-测试"),
				S3OriginConfig: &cloudfront.DistributionOriginS3OriginConfigArgs{
					OriginAccessIdentity: pulumi.String(""),
				},
			},
		},

		DefaultCacheBehavior: &cloudfront.DistributionDefaultCacheBehaviorArgs{
			TargetOriginId:       pulumi.String("edge-origin-测试"),
			ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
			AllowedMethods: pulumi.StringArray{
				pulumi.String("DELETE"),
				pulumi.String("GET"),
				pulumi.String("HEAD"),
				pulumi.String("OPTIONS"),
				pulumi.String("PATCH"),
				pulumi.String("POST"),
				pulumi.String("PUT"),
			},
			CachedMethods: pulumi.StringArray{
				pulumi.String("GET"),
				pulumi.String("HEAD"),
			},
			ForwardedValues: &cloudfront.DistributionDefaultCacheBehaviorForwardedValuesArgs{
				QueryString: pulumi.Bool(false),
				Cookies: &cloudfront.DistributionDefaultCacheBehaviorForwardedValuesCookiesArgs{
					Forward: pulumi.String("none"),
				},
			},
			MinTtl:     pulumi.Int(0),
			DefaultTtl: pulumi.Int(3600),
			MaxTtl:     pulumi.Int(86400),
		},

		Restrictions: &cloudfront.DistributionRestrictionsArgs{
			GeoRestriction: &cloudfront.DistributionRestrictionsGeoRestrictionArgs{
				RestrictionType: pulumi.String("none"),
			},
		},

		ViewerCertificate: &cloudfront.DistributionViewerCertificateArgs{
			CloudfrontDefaultCertificate: pulumi.Bool(true),
		},

		Tags: pulumi.StringMap{
			"TestHarness": pulumi.String("true"),
			"TestID":      pulumi.String(testID),
			"EdgeCase":    pulumi.String("cloudfront-global-测试"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create CloudFront distribution: %w", err)
	}

	return distribution, nil
}

// createCircularDependencies creates VPC and security groups with circular references
func (s *EdgeCasesScenario) createCircularDependencies(ctx *pulumi.Context, testID string, tags pulumi.StringMap) (*ec2.Vpc, *ec2.SecurityGroup, *ec2.SecurityGroup, error) {
	// Create VPC
	vpc, err := ec2.NewVpc(ctx, "circular-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String("10.0.0.0/16"),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               tags,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create VPC: %w", err)
	}

	// Create first security group
	sg1, err := ec2.NewSecurityGroup(ctx, "circular-sg1", &ec2.SecurityGroupArgs{
		Name:        pulumi.String(fmt.Sprintf("corkscrew-circular-sg1-%s", testID)),
		Description: pulumi.String("Security group 1 with circular dependency 测试"),
		VpcId:       vpc.ID(),
		Tags:        tags,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create security group 1: %w", err)
	}

	// Create second security group
	sg2, err := ec2.NewSecurityGroup(ctx, "circular-sg2", &ec2.SecurityGroupArgs{
		Name:        pulumi.String(fmt.Sprintf("corkscrew-circular-sg2-%s", testID)),
		Description: pulumi.String("Security group 2 with circular dependency 测试"),
		VpcId:       vpc.ID(),
		Tags:        tags,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create security group 2: %w", err)
	}

	// Create circular dependency: SG1 allows traffic from SG2, SG2 allows traffic from SG1
	_, err = ec2.NewSecurityGroupRule(ctx, "sg1-to-sg2", &ec2.SecurityGroupRuleArgs{
		Type:                  pulumi.String("ingress"),
		FromPort:              pulumi.Int(80),
		ToPort:                pulumi.Int(80),
		Protocol:              pulumi.String("tcp"),
		SourceSecurityGroupId: sg2.ID(),
		SecurityGroupId:       sg1.ID(),
		Description:           pulumi.String("Circular dependency: SG1 <- SG2 测试"),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create SG1 rule: %w", err)
	}

	_, err = ec2.NewSecurityGroupRule(ctx, "sg2-to-sg1", &ec2.SecurityGroupRuleArgs{
		Type:                  pulumi.String("ingress"),
		FromPort:              pulumi.Int(443),
		ToPort:                pulumi.Int(443),
		Protocol:              pulumi.String("tcp"),
		SourceSecurityGroupId: sg1.ID(),
		SecurityGroupId:       sg2.ID(),
		Description:           pulumi.String("Circular dependency: SG2 <- SG1 测试"),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create SG2 rule: %w", err)
	}

	return vpc, sg1, sg2, nil
}

// createStoppedInstance creates an EC2 instance and stops it
func (s *EdgeCasesScenario) createStoppedInstance(ctx *pulumi.Context, testID string, tags pulumi.StringMap, vpc *ec2.Vpc) (*ec2.Instance, error) {
	// First create a subnet in the VPC
	subnet, err := ec2.NewSubnet(ctx, "stopped-instance-subnet", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String("10.0.1.0/24"),
		AvailabilityZone: pulumi.String("us-east-1a"),
		Tags:             tags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet: %w", err)
	}

	// Create the instance
	stoppedTags := make(pulumi.StringMap)
	for k, v := range tags {
		stoppedTags[k] = v
	}
	stoppedTags["Name"] = pulumi.String(fmt.Sprintf("corkscrew-stopped-instance-%s", testID))
	stoppedTags["EdgeCase"] = pulumi.String("stopped-state-测试")

	instance, err := ec2.NewInstance(ctx, "stopped-instance", &ec2.InstanceArgs{
		Ami:          pulumi.String("ami-0c55b159cbfafe1f0"),
		InstanceType: pulumi.String("t2.nano"),
		SubnetId:     subnet.ID(),
		Tags:         stoppedTags,
		UserData: pulumi.String(`#!/bin/bash
# This instance will be stopped after launch
echo "Instance created for edge case testing" > /tmp/edge-case.txt
# Schedule automatic stop after 2 minutes
echo "sudo shutdown -h +2" | at now
`),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create stopped instance: %w", err)
	}

	// Note: In a real scenario, we would use a custom resource or AWS Lambda
	// to stop the instance after it's running. For this test, we rely on
	// the user data to self-stop, or manual stopping in complex scenarios.

	return instance, nil
}

// exportResources exports all edge case resources for verification
func (s *EdgeCasesScenario) exportResources(ctx *pulumi.Context, resources map[string]interface{}, testID string, maxTags pulumi.StringMap) {
	// Export individual resource details
	for name, resource := range resources {
		switch r := resource.(type) {
		case *s3.Bucket:
			ctx.Export(fmt.Sprintf("%sName", name), r.Bucket)
			ctx.Export(fmt.Sprintf("%sArn", name), r.Arn)
		case *ec2.Instance:
			ctx.Export(fmt.Sprintf("%sId", name), r.ID())
		case *iam.Role:
			ctx.Export(fmt.Sprintf("%sArn", name), r.Arn)
		case *cloudfront.Distribution:
			ctx.Export(fmt.Sprintf("%sId", name), r.ID())
			ctx.Export(fmt.Sprintf("%sDomainName", name), r.DomainName)
		case *ec2.Vpc:
			ctx.Export(fmt.Sprintf("%sId", name), r.ID())
		case *ec2.SecurityGroup:
			ctx.Export(fmt.Sprintf("%sId", name), r.ID())
		}
	}

	// Export expected resources structure
	ctx.Export("expectedResources", pulumi.Map{
		"s3": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"attributes": pulumi.Map{
					"tag_count":       pulumi.Int(50),
					"unicode_support": pulumi.Bool(true),
					"lifecycle_rules": pulumi.Int(1),
				},
			},
		},
		"ec2": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Instance"),
				"attributes": pulumi.Map{
					"long_name":     pulumi.Bool(true),
					"unicode_tags":  pulumi.Bool(true),
					"user_data_set": pulumi.Bool(true),
				},
			},
			pulumi.Map{
				"type": pulumi.String("VPC"),
				"attributes": pulumi.Map{
					"circular_deps": pulumi.Bool(true),
				},
			},
			pulumi.Map{
				"type": pulumi.String("SecurityGroup"),
				"attributes": pulumi.Map{
					"circular_refs": pulumi.Bool(true),
				},
			},
		},
		"iam": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Role"),
				"attributes": pulumi.Map{
					"global_service": pulumi.Bool(true),
					"complex_policy": pulumi.Bool(true),
					"unicode_desc":   pulumi.Bool(true),
				},
			},
		},
		"cloudfront": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Distribution"),
				"attributes": pulumi.Map{
					"global_service": pulumi.Bool(true),
					"unicode_origin": pulumi.Bool(true),
				},
			},
		},
	})

	// Store expected resources for verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"s3": []interface{}{
				map[string]interface{}{
					"type": "Bucket",
					"attributes": map[string]interface{}{
						"tag_count":       50,
						"unicode_support": true,
						"lifecycle_rules": 1,
					},
				},
			},
			"ec2": []interface{}{
				map[string]interface{}{
					"type": "Instance",
					"attributes": map[string]interface{}{
						"long_name":     true,
						"unicode_tags":  true,
						"user_data_set": true,
					},
				},
				map[string]interface{}{
					"type": "VPC",
					"attributes": map[string]interface{}{
						"circular_deps": true,
					},
				},
				map[string]interface{}{
					"type": "SecurityGroup",
					"attributes": map[string]interface{}{
						"circular_refs": true,
					},
				},
			},
			"iam": []interface{}{
				map[string]interface{}{
					"type": "Role",
					"attributes": map[string]interface{}{
						"global_service": true,
						"complex_policy": true,
						"unicode_desc":   true,
					},
				},
			},
			"cloudfront": []interface{}{
				map[string]interface{}{
					"type": "Distribution",
					"attributes": map[string]interface{}{
						"global_service": true,
						"unicode_origin": true,
					},
				},
			},
		},
	}
}

// GetExpectedResources returns the expected resources for verification
func (s *EdgeCasesScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

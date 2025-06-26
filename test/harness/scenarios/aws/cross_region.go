package aws

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudfront"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// CrossRegionScenario tests resources across multiple AWS regions
type CrossRegionScenario struct {
	expectedResources map[string]interface{}
	regions          []string
}

// NewCrossRegionScenario creates a new cross-region test scenario
func NewCrossRegionScenario() *CrossRegionScenario {
	return &CrossRegionScenario{
		expectedResources: make(map[string]interface{}),
		regions:          []string{"us-east-1", "us-west-2", "eu-west-1"},
	}
}

// GetName returns the scenario name
func (s *CrossRegionScenario) GetName() string {
	return "cross-region"
}

// GetServices returns the AWS services this scenario tests
func (s *CrossRegionScenario) GetServices() []string {
	return []string{"s3", "ec2", "iam", "cloudfront"}
}

// DefineResources creates cross-region resources for testing
func (s *CrossRegionScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	baseTags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("cross-region"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create resources in multiple regions
	regionalResources := make(map[string]interface{})
	
	for i, region := range s.regions {
		regionTags := make(pulumi.StringMap)
		for k, v := range baseTags {
			regionTags[k] = v
		}
		regionTags["Region"] = pulumi.String(region)
		regionTags["RegionIndex"] = pulumi.String(fmt.Sprintf("%d", i))

		// Create regional provider
		provider, err := aws.NewProvider(ctx, fmt.Sprintf("provider-%s", region), &aws.ProviderArgs{
			Region: pulumi.String(region),
		})
		if err != nil {
			return fmt.Errorf("failed to create provider for %s: %w", region, err)
		}

		// Create regional resources
		regionResources, err := s.createRegionalResources(ctx, testID, region, i, regionTags, provider)
		if err != nil {
			return fmt.Errorf("failed to create resources in %s: %w", region, err)
		}
		
		regionalResources[region] = regionResources
	}

	// Create global resources (IAM, CloudFront)
	globalResources, err := s.createGlobalResources(ctx, testID, baseTags, regionalResources)
	if err != nil {
		return fmt.Errorf("failed to create global resources: %w", err)
	}

	// Create cross-region relationships
	crossRegionResources, err := s.createCrossRegionRelationships(ctx, testID, baseTags, regionalResources)
	if err != nil {
		return fmt.Errorf("failed to create cross-region relationships: %w", err)
	}

	// Export all resources
	s.exportCrossRegionResources(ctx, regionalResources, globalResources, crossRegionResources, testID)

	return nil
}

// createRegionalResources creates resources specific to each region
func (s *CrossRegionScenario) createRegionalResources(ctx *pulumi.Context, testID, region string, regionIndex int, tags pulumi.StringMap, provider *aws.Provider) (map[string]interface{}, error) {
	resources := make(map[string]interface{})
	
	// Create VPC in each region
	vpc, err := ec2.NewVpc(ctx, fmt.Sprintf("vpc-%s", region), &ec2.VpcArgs{
		CidrBlock:          pulumi.String(fmt.Sprintf("10.%d.0.0/16", regionIndex)),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               tags,
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create VPC in %s: %w", region, err)
	}
	resources["vpc"] = vpc

	// Create subnet
	subnet, err := ec2.NewSubnet(ctx, fmt.Sprintf("subnet-%s", region), &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String(fmt.Sprintf("10.%d.1.0/24", regionIndex)),
		AvailabilityZone: pulumi.String(fmt.Sprintf("%sa", region)),
		Tags:             tags,
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet in %s: %w", region, err)
	}
	resources["subnet"] = subnet

	// Create S3 bucket in each region
	bucketName := fmt.Sprintf("corkscrew-cross-region-%s-%s", strings.ToLower(region), strings.ToLower(testID[:8]))
	bucket, err := s3.NewBucket(ctx, fmt.Sprintf("bucket-%s", region), &s3.BucketArgs{
		Bucket: pulumi.String(bucketName),
		Tags:   tags,
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 bucket in %s: %w", region, err)
	}
	resources["bucket"] = bucket

	// Enable versioning on the bucket
	_, err = s3.NewBucketVersioningV2(ctx, fmt.Sprintf("versioning-%s", region), &s3.BucketVersioningV2Args{
		Bucket: bucket.ID(),
		VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
			Status: pulumi.String("Enabled"),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, fmt.Errorf("failed to enable versioning in %s: %w", region, err)
	}

	// Create EC2 instance in each region (different AMI per region)
	amiMap := map[string]string{
		"us-east-1": "ami-0c55b159cbfafe1f0", // Amazon Linux 2
		"us-west-2": "ami-0d1cd67c26f5fca19", // Amazon Linux 2
		"eu-west-1": "ami-08935252a36e25f85", // Amazon Linux 2
	}
	
	instance, err := ec2.NewInstance(ctx, fmt.Sprintf("instance-%s", region), &ec2.InstanceArgs{
		Ami:          pulumi.String(amiMap[region]),
		InstanceType: pulumi.String("t2.micro"),
		SubnetId:     subnet.ID(),
		Tags:         tags,
		UserData: pulumi.String(fmt.Sprintf(`#!/bin/bash
echo "Region: %s" > /tmp/region.txt
echo "Test ID: %s" >> /tmp/region.txt
echo "Cross-region test instance" >> /tmp/region.txt
`, region, testID)),
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create instance in %s: %w", region, err)
	}
	resources["instance"] = instance

	return resources, nil
}

// createGlobalResources creates resources that are global (IAM, CloudFront)
func (s *CrossRegionScenario) createGlobalResources(ctx *pulumi.Context, testID string, tags pulumi.StringMap, regionalResources map[string]interface{}) (map[string]interface{}, error) {
	resources := make(map[string]interface{})

	// Create IAM role (global service)
	role, err := iam.NewRole(ctx, "cross-region-role", &iam.RoleArgs{
		Name: pulumi.String(fmt.Sprintf("corkscrew-cross-region-role-%s", testID)),
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Principal": {
						"Service": "ec2.amazonaws.com"
					},
					"Action": "sts:AssumeRole"
				}
			]
		}`),
		Tags: tags,
		Description: pulumi.String(fmt.Sprintf("Cross-region IAM role for test %s", testID)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create IAM role: %w", err)
	}
	resources["role"] = role

	// Create instance profile for the role
	instanceProfile, err := iam.NewInstanceProfile(ctx, "cross-region-profile", &iam.InstanceProfileArgs{
		Name: pulumi.String(fmt.Sprintf("corkscrew-cross-region-profile-%s", testID)),
		Role: role.Name,
		Tags: tags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create instance profile: %w", err)
	}
	resources["instanceProfile"] = instanceProfile

	// Create CloudFront distribution with origins from multiple regions
	origins := cloudfront.DistributionOriginArray{}
	
	for region, regionRes := range regionalResources {
		if regionResources, ok := regionRes.(map[string]interface{}); ok {
			if bucket, ok := regionResources["bucket"].(*s3.Bucket); ok {
				origin := &cloudfront.DistributionOriginArgs{
					DomainName: bucket.BucketDomainName,
					OriginId:   pulumi.String(fmt.Sprintf("origin-%s", region)),
					S3OriginConfig: &cloudfront.DistributionOriginS3OriginConfigArgs{
						OriginAccessIdentity: pulumi.String(""),
					},
				}
				origins = append(origins, origin)
			}
		}
	}

	if len(origins) > 0 {
		distribution, err := cloudfront.NewDistribution(ctx, "cross-region-distribution", &cloudfront.DistributionArgs{
			Comment: pulumi.String(fmt.Sprintf("Cross-region CloudFront distribution for test %s", testID)),
			Enabled: pulumi.Bool(true),
			Origins: origins,
			
			DefaultCacheBehavior: &cloudfront.DistributionDefaultCacheBehaviorArgs{
				TargetOriginId:       origins[0].OriginId,
				ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
				AllowedMethods: pulumi.StringArray{
					pulumi.String("GET"), pulumi.String("HEAD"),
				},
				CachedMethods: pulumi.StringArray{
					pulumi.String("GET"), pulumi.String("HEAD"),
				},
				ForwardedValues: &cloudfront.DistributionDefaultCacheBehaviorForwardedValuesArgs{
					QueryString: pulumi.Bool(false),
					Cookies: &cloudfront.DistributionDefaultCacheBehaviorForwardedValuesCookiesArgs{
						Forward: pulumi.String("none"),
					},
				},
			},
			
			// Add cache behaviors for other regions
			CacheBehaviors: s.createCacheBehaviors(origins[1:]),
			
			Restrictions: &cloudfront.DistributionRestrictionsArgs{
				GeoRestriction: &cloudfront.DistributionRestrictionsGeoRestrictionArgs{
					RestrictionType: pulumi.String("none"),
				},
			},
			
			ViewerCertificate: &cloudfront.DistributionViewerCertificateArgs{
				CloudfrontDefaultCertificate: pulumi.Bool(true),
			},
			
			Tags: tags,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create CloudFront distribution: %w", err)
		}
		resources["distribution"] = distribution
	}

	return resources, nil
}

// createCacheBehaviors creates cache behaviors for additional origins
func (s *CrossRegionScenario) createCacheBehaviors(origins cloudfront.DistributionOriginArray) cloudfront.DistributionCacheBehaviorArray {
	behaviors := cloudfront.DistributionCacheBehaviorArray{}
	
	for i, origin := range origins {
		behavior := &cloudfront.DistributionCacheBehaviorArgs{
			PathPattern:          pulumi.String(fmt.Sprintf("/region%d/*", i+2)),
			TargetOriginId:       origin.OriginId,
			ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
			AllowedMethods: pulumi.StringArray{
				pulumi.String("GET"), pulumi.String("HEAD"),
			},
			CachedMethods: pulumi.StringArray{
				pulumi.String("GET"), pulumi.String("HEAD"),
			},
			ForwardedValues: &cloudfront.DistributionCacheBehaviorForwardedValuesArgs{
				QueryString: pulumi.Bool(false),
				Cookies: &cloudfront.DistributionCacheBehaviorForwardedValuesCookiesArgs{
					Forward: pulumi.String("none"),
				},
			},
		}
		behaviors = append(behaviors, behavior)
	}
	
	return behaviors
}

// createCrossRegionRelationships creates relationships between regions
func (s *CrossRegionScenario) createCrossRegionRelationships(ctx *pulumi.Context, testID string, tags pulumi.StringMap, regionalResources map[string]interface{}) (map[string]interface{}, error) {
	resources := make(map[string]interface{})

	// Create VPC peering connections between regions
	sourceRegion := "us-east-1"
	targetRegions := []string{"us-west-2", "eu-west-1"}

	sourceVPC := s.getVPCFromRegion(regionalResources, sourceRegion)
	if sourceVPC == nil {
		return nil, fmt.Errorf("source VPC not found in %s", sourceRegion)
	}

	for _, targetRegion := range targetRegions {
		targetVPC := s.getVPCFromRegion(regionalResources, targetRegion)
		if targetVPC == nil {
			continue
		}

		// Create VPC peering connection
		peeringName := fmt.Sprintf("peering-%s-%s", sourceRegion, targetRegion)
		
		// Note: This creates the peering connection request from source region
		peeringConnection, err := ec2.NewVpcPeeringConnection(ctx, peeringName, &ec2.VpcPeeringConnectionArgs{
			VpcId:        sourceVPC.ID(),
			PeerVpcId:    targetVPC.ID(),
			PeerRegion:   pulumi.String(targetRegion),
			AutoAccept:   pulumi.Bool(false), // Cross-region peering requires manual accept
			Tags:         tags,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create VPC peering %s-%s: %w", sourceRegion, targetRegion, err)
		}
		
		resources[peeringName] = peeringConnection
	}

	// Create S3 cross-region replication
	sourceBucket := s.getBucketFromRegion(regionalResources, sourceRegion)
	targetBucket := s.getBucketFromRegion(regionalResources, "us-west-2")
	
	if sourceBucket != nil && targetBucket != nil {
		// Create IAM role for replication
		replicationRole, err := iam.NewRole(ctx, "s3-replication-role", &iam.RoleArgs{
			Name: pulumi.String(fmt.Sprintf("corkscrew-s3-replication-%s", testID)),
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [
					{
						"Effect": "Allow",
						"Principal": {
							"Service": "s3.amazonaws.com"
						},
						"Action": "sts:AssumeRole"
					}
				]
			}`),
			Tags: tags,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 replication role: %w", err)
		}

		// Attach policy to replication role
		_, err = iam.NewRolePolicy(ctx, "s3-replication-policy", &iam.RolePolicyArgs{
			Role: replicationRole.ID(),
			Policy: pulumi.All(sourceBucket.Arn, targetBucket.Arn).ApplyT(func(args []interface{}) string {
				sourceArn := args[0].(string)
				targetArn := args[1].(string)
				return fmt.Sprintf(`{
					"Version": "2012-10-17",
					"Statement": [
						{
							"Effect": "Allow",
							"Action": [
								"s3:GetObjectVersionForReplication",
								"s3:GetObjectVersionAcl"
							],
							"Resource": "%s/*"
						},
						{
							"Effect": "Allow",
							"Action": [
								"s3:ListBucket"
							],
							"Resource": "%s"
						},
						{
							"Effect": "Allow",
							"Action": [
								"s3:ReplicateObject",
								"s3:ReplicateDelete"
							],
							"Resource": "%s/*"
						}
					]
				}`, sourceArn, sourceArn, targetArn)
			}).(pulumi.StringOutput),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 replication policy: %w", err)
		}

		// Create replication configuration
		_, err = s3.NewBucketReplicationConfigurationV2(ctx, "s3-replication", &s3.BucketReplicationConfigurationV2Args{
			Role:   replicationRole.Arn,
			Bucket: sourceBucket.ID(),
			Rules: s3.BucketReplicationConfigurationV2RuleArray{
				&s3.BucketReplicationConfigurationV2RuleArgs{
					Id:     pulumi.String("cross-region-replication"),
					Status: pulumi.String("Enabled"),
					Destination: &s3.BucketReplicationConfigurationV2RuleDestinationArgs{
						Bucket: targetBucket.Arn,
					},
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 replication: %w", err)
		}

		resources["replicationRole"] = replicationRole
	}

	return resources, nil
}

// Helper functions to extract resources from regional data
func (s *CrossRegionScenario) getVPCFromRegion(regionalResources map[string]interface{}, region string) *ec2.Vpc {
	if regionRes, ok := regionalResources[region]; ok {
		if regionMap, ok := regionRes.(map[string]interface{}); ok {
			if vpc, ok := regionMap["vpc"].(*ec2.Vpc); ok {
				return vpc
			}
		}
	}
	return nil
}

func (s *CrossRegionScenario) getBucketFromRegion(regionalResources map[string]interface{}, region string) *s3.Bucket {
	if regionRes, ok := regionalResources[region]; ok {
		if regionMap, ok := regionRes.(map[string]interface{}); ok {
			if bucket, ok := regionMap["bucket"].(*s3.Bucket); ok {
				return bucket
			}
		}
	}
	return nil
}

// exportCrossRegionResources exports all cross-region resources for verification
func (s *CrossRegionScenario) exportCrossRegionResources(ctx *pulumi.Context, regionalResources, globalResources, crossRegionResources map[string]interface{}, testID string) {
	// Export regional resource details
	for region, resources := range regionalResources {
		if regionMap, ok := resources.(map[string]interface{}); ok {
			for resourceType, resource := range regionMap {
				name := fmt.Sprintf("%s_%s", region, resourceType)
				switch r := resource.(type) {
				case *ec2.Vpc:
					ctx.Export(fmt.Sprintf("%s_id", name), r.ID())
				case *ec2.Instance:
					ctx.Export(fmt.Sprintf("%s_id", name), r.ID())
				case *s3.Bucket:
					ctx.Export(fmt.Sprintf("%s_name", name), r.Bucket)
					ctx.Export(fmt.Sprintf("%s_arn", name), r.Arn)
				}
			}
		}
	}

	// Export global resource details
	for resourceType, resource := range globalResources {
		switch r := resource.(type) {
		case *iam.Role:
			ctx.Export(fmt.Sprintf("global_%s_arn", resourceType), r.Arn)
		case *cloudfront.Distribution:
			ctx.Export(fmt.Sprintf("global_%s_id", resourceType), r.ID())
			ctx.Export(fmt.Sprintf("global_%s_domain", resourceType), r.DomainName)
		}
	}

	// Export cross-region relationship details
	for relationshipName, resource := range crossRegionResources {
		switch r := resource.(type) {
		case *ec2.VpcPeeringConnection:
			ctx.Export(fmt.Sprintf("%s_id", relationshipName), r.ID())
		case *iam.Role:
			ctx.Export(fmt.Sprintf("%s_arn", relationshipName), r.Arn)
		}
	}

	// Export expected resources structure
	ctx.Export("expectedResources", pulumi.Map{
		"regions": pulumi.StringArray{
			pulumi.String("us-east-1"),
			pulumi.String("us-west-2"),
			pulumi.String("eu-west-1"),
		},
		"s3": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Bucket"),
				"regions": pulumi.Int(3),
				"attributes": pulumi.Map{
					"cross_region_replication": pulumi.Bool(true),
					"versioning_enabled":       pulumi.Bool(true),
				},
			},
		},
		"ec2": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("VPC"),
				"regions": pulumi.Int(3),
				"attributes": pulumi.Map{
					"peering_connections": pulumi.Int(2),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Instance"),
				"regions": pulumi.Int(3),
				"attributes": pulumi.Map{
					"different_amis": pulumi.Bool(true),
				},
			},
		},
		"iam": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Role"),
				"global": pulumi.Bool(true),
				"attributes": pulumi.Map{
					"cross_region_policies": pulumi.Bool(true),
				},
			},
		},
		"cloudfront": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Distribution"),
				"global": pulumi.Bool(true),
				"attributes": pulumi.Map{
					"multi_region_origins": pulumi.Int(3),
					"cache_behaviors":      pulumi.Int(2),
				},
			},
		},
	})

	// Store expected resources for verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"regions": []string{"us-east-1", "us-west-2", "eu-west-1"},
			"s3": []interface{}{
				map[string]interface{}{
					"type":    "Bucket",
					"regions": 3,
					"attributes": map[string]interface{}{
						"cross_region_replication": true,
						"versioning_enabled":       true,
					},
				},
			},
			"ec2": []interface{}{
				map[string]interface{}{
					"type":    "VPC",
					"regions": 3,
					"attributes": map[string]interface{}{
						"peering_connections": 2,
					},
				},
				map[string]interface{}{
					"type":    "Instance",
					"regions": 3,
					"attributes": map[string]interface{}{
						"different_amis": true,
					},
				},
			},
			"iam": []interface{}{
				map[string]interface{}{
					"type":   "Role",
					"global": true,
					"attributes": map[string]interface{}{
						"cross_region_policies": true,
					},
				},
			},
			"cloudfront": []interface{}{
				map[string]interface{}{
					"type":   "Distribution",
					"global": true,
					"attributes": map[string]interface{}{
						"multi_region_origins": 3,
						"cache_behaviors":      2,
					},
				},
			},
		},
	}
}

// GetExpectedResources returns the expected resources for verification
func (s *CrossRegionScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}
package cleanup

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// MultiRegionCleanup manages cleanup across multiple AWS regions
type MultiRegionCleanup struct {
	regions     []string
	configs     map[string]aws.Config
	testID      string
	concurrency int
}

// RegionCleanupResult contains cleanup results for a specific region
type RegionCleanupResult struct {
	Region           string         `json:"region"`
	StartTime        time.Time      `json:"start_time"`
	EndTime          time.Time      `json:"end_time"`
	Duration         time.Duration  `json:"duration"`
	ResourcesCleaned map[string]int `json:"resources_cleaned"`
	Errors           []string       `json:"errors"`
	Success          bool           `json:"success"`
}

// MultiRegionCleanupResult aggregates results from all regions
type MultiRegionCleanupResult struct {
	TestID        string                `json:"test_id"`
	StartTime     time.Time             `json:"start_time"`
	EndTime       time.Time             `json:"end_time"`
	Duration      time.Duration         `json:"duration"`
	RegionResults []RegionCleanupResult `json:"region_results"`
	GlobalResults GlobalCleanupResult   `json:"global_results"`
	Summary       CleanupSummary        `json:"summary"`
}

// GlobalCleanupResult contains results for global services (IAM, CloudFront)
type GlobalCleanupResult struct {
	StartTime        time.Time      `json:"start_time"`
	EndTime          time.Time      `json:"end_time"`
	Duration         time.Duration  `json:"duration"`
	ResourcesCleaned map[string]int `json:"resources_cleaned"`
	Errors           []string       `json:"errors"`
	Success          bool           `json:"success"`
}

// CleanupSummary provides aggregated cleanup statistics
type CleanupSummary struct {
	TotalRegions          int            `json:"total_regions"`
	SuccessfulRegions     int            `json:"successful_regions"`
	FailedRegions         int            `json:"failed_regions"`
	TotalResourcesCleaned map[string]int `json:"total_resources_cleaned"`
	TotalErrors           int            `json:"total_errors"`
	OverallSuccess        bool           `json:"overall_success"`
}

// NewMultiRegionCleanup creates a new multi-region cleanup manager
func NewMultiRegionCleanup(testID string, regions []string, concurrency int) (*MultiRegionCleanup, error) {
	if concurrency <= 0 {
		concurrency = 3 // Default to 3 concurrent regions
	}

	mrc := &MultiRegionCleanup{
		regions:     regions,
		configs:     make(map[string]aws.Config),
		testID:      testID,
		concurrency: concurrency,
	}

	// Load AWS config for each region
	ctx := context.Background()
	for _, region := range regions {
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("failed to load config for region %s: %w", region, err)
		}
		mrc.configs[region] = cfg
	}

	return mrc, nil
}

// ExecuteCleanup performs cleanup across all regions and global services
func (mrc *MultiRegionCleanup) ExecuteCleanup(ctx context.Context) (*MultiRegionCleanupResult, error) {
	result := &MultiRegionCleanupResult{
		TestID:        mrc.testID,
		StartTime:     time.Now(),
		RegionResults: make([]RegionCleanupResult, 0, len(mrc.regions)),
		Summary: CleanupSummary{
			TotalResourcesCleaned: make(map[string]int),
		},
	}

	fmt.Printf("🌍 Starting multi-region cleanup for test %s\n", mrc.testID)
	fmt.Printf("   Regions: %v\n", mrc.regions)
	fmt.Printf("   Concurrency: %d\n", mrc.concurrency)

	// Phase 1: Clean up regional resources in parallel
	regionalResults, err := mrc.cleanupRegionalResources(ctx)
	if err != nil {
		return result, fmt.Errorf("regional cleanup failed: %w", err)
	}
	result.RegionResults = regionalResults

	// Phase 2: Clean up global resources (IAM, CloudFront)
	globalResult, err := mrc.cleanupGlobalResources(ctx)
	if err != nil {
		fmt.Printf("⚠️ Global cleanup failed: %v\n", err)
		// Don't fail the entire cleanup for global resource issues
	}
	result.GlobalResults = *globalResult

	// Calculate summary
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Summary = mrc.calculateSummary(regionalResults, globalResult)

	fmt.Printf("🎉 Multi-region cleanup completed in %v\n", result.Duration)
	fmt.Printf("   Successful regions: %d/%d\n", result.Summary.SuccessfulRegions, result.Summary.TotalRegions)
	fmt.Printf("   Total errors: %d\n", result.Summary.TotalErrors)

	return result, nil
}

// cleanupRegionalResources cleans up resources in all regions concurrently
func (mrc *MultiRegionCleanup) cleanupRegionalResources(ctx context.Context) ([]RegionCleanupResult, error) {
	results := make([]RegionCleanupResult, len(mrc.regions))

	// Create semaphore for concurrency control
	semaphore := make(chan struct{}, mrc.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Process regions with controlled concurrency
	for i, region := range mrc.regions {
		wg.Add(1)
		go func(index int, region string) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			fmt.Printf("🧹 Cleaning region: %s\n", region)

			result := mrc.cleanupSingleRegion(ctx, region)

			mu.Lock()
			results[index] = *result
			mu.Unlock()

			status := "✅"
			if !result.Success {
				status = "❌"
			}
			fmt.Printf("%s Region %s cleanup completed in %v\n", status, region, result.Duration)
		}(i, region)
	}

	wg.Wait()
	return results, nil
}

// cleanupSingleRegion cleans up resources in a specific region
func (mrc *MultiRegionCleanup) cleanupSingleRegion(ctx context.Context, region string) *RegionCleanupResult {
	result := &RegionCleanupResult{
		Region:           region,
		StartTime:        time.Now(),
		ResourcesCleaned: make(map[string]int),
		Errors:           []string{},
	}

	cfg := mrc.configs[region]

	// Clean up EC2 resources
	if err := mrc.cleanupEC2Resources(ctx, cfg, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("EC2 cleanup failed: %v", err))
	}

	// Clean up S3 resources
	if err := mrc.cleanupS3Resources(ctx, cfg, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("S3 cleanup failed: %v", err))
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Success = len(result.Errors) == 0

	return result
}

// cleanupEC2Resources cleans up EC2 resources in a region
func (mrc *MultiRegionCleanup) cleanupEC2Resources(ctx context.Context, cfg aws.Config, result *RegionCleanupResult) error {
	ec2Client := ec2.NewFromConfig(cfg)

	// Cleanup instances
	if err := mrc.cleanupEC2Instances(ctx, ec2Client, result); err != nil {
		return fmt.Errorf("instance cleanup failed: %w", err)
	}

	// Cleanup VPC peering connections
	if err := mrc.cleanupVPCPeeringConnections(ctx, ec2Client, result); err != nil {
		return fmt.Errorf("VPC peering cleanup failed: %w", err)
	}

	// Cleanup security groups
	if err := mrc.cleanupSecurityGroups(ctx, ec2Client, result); err != nil {
		return fmt.Errorf("security group cleanup failed: %w", err)
	}

	// Cleanup subnets
	if err := mrc.cleanupSubnets(ctx, ec2Client, result); err != nil {
		return fmt.Errorf("subnet cleanup failed: %w", err)
	}

	// Cleanup VPCs (must be last)
	if err := mrc.cleanupVPCs(ctx, ec2Client, result); err != nil {
		return fmt.Errorf("VPC cleanup failed: %w", err)
	}

	return nil
}

// cleanupEC2Instances terminates test EC2 instances
func (mrc *MultiRegionCleanup) cleanupEC2Instances(ctx context.Context, client *ec2.Client, result *RegionCleanupResult) error {
	// Find instances with test tags
	describeInput := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:TestHarness"),
				Values: []string{"true"},
			},
			{
				Name:   aws.String("tag:TestID"),
				Values: []string{mrc.testID},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running", "stopped", "pending"},
			},
		},
	}

	describeOutput, err := client.DescribeInstances(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("failed to describe instances: %w", err)
	}

	var instanceIds []string
	for _, reservation := range describeOutput.Reservations {
		for _, instance := range reservation.Instances {
			instanceIds = append(instanceIds, *instance.InstanceId)
		}
	}

	if len(instanceIds) > 0 {
		// Terminate instances
		terminateInput := &ec2.TerminateInstancesInput{
			InstanceIds: instanceIds,
		}

		_, err := client.TerminateInstances(ctx, terminateInput)
		if err != nil {
			return fmt.Errorf("failed to terminate instances: %w", err)
		}

		result.ResourcesCleaned["EC2Instances"] = len(instanceIds)

		// Wait for instances to terminate (with timeout)
		waiterCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		waiter := ec2.NewInstanceTerminatedWaiter(client)
		if err := waiter.Wait(waiterCtx, &ec2.DescribeInstancesInput{
			InstanceIds: instanceIds,
		}, 10*time.Minute); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Timeout waiting for instance termination: %v", err))
		}
	}

	return nil
}

// cleanupVPCPeeringConnections deletes VPC peering connections
func (mrc *MultiRegionCleanup) cleanupVPCPeeringConnections(ctx context.Context, client *ec2.Client, result *RegionCleanupResult) error {
	// Find peering connections with test tags
	describeInput := &ec2.DescribeVpcPeeringConnectionsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:TestHarness"),
				Values: []string{"true"},
			},
			{
				Name:   aws.String("tag:TestID"),
				Values: []string{mrc.testID},
			},
		},
	}

	describeOutput, err := client.DescribeVpcPeeringConnections(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("failed to describe VPC peering connections: %w", err)
	}

	for _, connection := range describeOutput.VpcPeeringConnections {
		deleteInput := &ec2.DeleteVpcPeeringConnectionInput{
			VpcPeeringConnectionId: connection.VpcPeeringConnectionId,
		}

		if _, err := client.DeleteVpcPeeringConnection(ctx, deleteInput); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete VPC peering connection %s: %v",
				*connection.VpcPeeringConnectionId, err))
		} else {
			result.ResourcesCleaned["VPCPeeringConnections"]++
		}
	}

	return nil
}

// cleanupSecurityGroups deletes test security groups
func (mrc *MultiRegionCleanup) cleanupSecurityGroups(ctx context.Context, client *ec2.Client, result *RegionCleanupResult) error {
	// Find security groups with test tags
	describeInput := &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:TestHarness"),
				Values: []string{"true"},
			},
			{
				Name:   aws.String("tag:TestID"),
				Values: []string{mrc.testID},
			},
		},
	}

	describeOutput, err := client.DescribeSecurityGroups(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("failed to describe security groups: %w", err)
	}

	for _, sg := range describeOutput.SecurityGroups {
		// Skip default security group
		if *sg.GroupName == "default" {
			continue
		}

		deleteInput := &ec2.DeleteSecurityGroupInput{
			GroupId: sg.GroupId,
		}

		if _, err := client.DeleteSecurityGroup(ctx, deleteInput); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete security group %s: %v",
				*sg.GroupId, err))
		} else {
			result.ResourcesCleaned["SecurityGroups"]++
		}
	}

	return nil
}

// cleanupSubnets deletes test subnets
func (mrc *MultiRegionCleanup) cleanupSubnets(ctx context.Context, client *ec2.Client, result *RegionCleanupResult) error {
	// Find subnets with test tags
	describeInput := &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:TestHarness"),
				Values: []string{"true"},
			},
			{
				Name:   aws.String("tag:TestID"),
				Values: []string{mrc.testID},
			},
		},
	}

	describeOutput, err := client.DescribeSubnets(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("failed to describe subnets: %w", err)
	}

	for _, subnet := range describeOutput.Subnets {
		deleteInput := &ec2.DeleteSubnetInput{
			SubnetId: subnet.SubnetId,
		}

		if _, err := client.DeleteSubnet(ctx, deleteInput); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete subnet %s: %v",
				*subnet.SubnetId, err))
		} else {
			result.ResourcesCleaned["Subnets"]++
		}
	}

	return nil
}

// cleanupVPCs deletes test VPCs
func (mrc *MultiRegionCleanup) cleanupVPCs(ctx context.Context, client *ec2.Client, result *RegionCleanupResult) error {
	// Find VPCs with test tags
	describeInput := &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:TestHarness"),
				Values: []string{"true"},
			},
			{
				Name:   aws.String("tag:TestID"),
				Values: []string{mrc.testID},
			},
		},
	}

	describeOutput, err := client.DescribeVpcs(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("failed to describe VPCs: %w", err)
	}

	for _, vpc := range describeOutput.Vpcs {
		// Skip default VPC
		if *vpc.IsDefault {
			continue
		}

		deleteInput := &ec2.DeleteVpcInput{
			VpcId: vpc.VpcId,
		}

		if _, err := client.DeleteVpc(ctx, deleteInput); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete VPC %s: %v",
				*vpc.VpcId, err))
		} else {
			result.ResourcesCleaned["VPCs"]++
		}
	}

	return nil
}

// cleanupS3Resources cleans up S3 resources in a region
func (mrc *MultiRegionCleanup) cleanupS3Resources(ctx context.Context, cfg aws.Config, result *RegionCleanupResult) error {
	s3Client := s3.NewFromConfig(cfg)

	// List all buckets (S3 is global but we check from each region for completeness)
	listOutput, err := s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	for _, bucket := range listOutput.Buckets {
		bucketName := *bucket.Name

		// Check if this is a test bucket
		if !mrc.isTestBucket(bucketName) {
			continue
		}

		// Check bucket tags to confirm it's our test
		tagsOutput, err := s3Client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: &bucketName,
		})
		if err != nil {
			// Bucket might not have tags, skip
			continue
		}

		isTestBucket := false
		for _, tag := range tagsOutput.TagSet {
			if *tag.Key == "TestID" && *tag.Value == mrc.testID {
				isTestBucket = true
				break
			}
		}

		if !isTestBucket {
			continue
		}

		// Empty the bucket first
		if err := mrc.emptyS3Bucket(ctx, s3Client, bucketName); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to empty bucket %s: %v", bucketName, err))
			continue
		}

		// Delete the bucket
		_, err = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: &bucketName,
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete bucket %s: %v", bucketName, err))
		} else {
			result.ResourcesCleaned["S3Buckets"]++
		}
	}

	return nil
}

// isTestBucket checks if a bucket name matches test bucket patterns
func (mrc *MultiRegionCleanup) isTestBucket(bucketName string) bool {
	testPatterns := []string{
		"corkscrew-test",
		"corkscrew-perf",
		"corkscrew-cross-region",
		"corkscrew-unicode",
		fmt.Sprintf("corkscrew-%s", mrc.testID),
	}

	for _, pattern := range testPatterns {
		if strings.Contains(bucketName, pattern) {
			return true
		}
	}

	return false
}

// emptyS3Bucket removes all objects from an S3 bucket
func (mrc *MultiRegionCleanup) emptyS3Bucket(ctx context.Context, client *s3.Client, bucketName string) error {
	// List all objects
	listInput := &s3.ListObjectsV2Input{
		Bucket: &bucketName,
	}

	for {
		listOutput, err := client.ListObjectsV2(ctx, listInput)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		if len(listOutput.Contents) == 0 {
			break
		}

		// Prepare batch delete
		var objectsToDelete []s3types.ObjectIdentifier
		for _, obj := range listOutput.Contents {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		// Batch delete objects
		deleteInput := &s3.DeleteObjectsInput{
			Bucket: &bucketName,
			Delete: &s3types.Delete{
				Objects: objectsToDelete,
			},
		}

		_, err = client.DeleteObjects(ctx, deleteInput)
		if err != nil {
			return fmt.Errorf("failed to delete objects: %w", err)
		}

		// Continue if there are more objects
		if !listOutput.IsTruncated {
			break
		}
		listInput.ContinuationToken = listOutput.NextContinuationToken
	}

	return nil
}

// cleanupGlobalResources cleans up global AWS resources (IAM, CloudFront)
func (mrc *MultiRegionCleanup) cleanupGlobalResources(ctx context.Context) (*GlobalCleanupResult, error) {
	result := &GlobalCleanupResult{
		StartTime:        time.Now(),
		ResourcesCleaned: make(map[string]int),
		Errors:           []string{},
	}

	fmt.Printf("🌐 Cleaning global resources for test %s\n", mrc.testID)

	// Use us-east-1 config for global services
	cfg := mrc.configs["us-east-1"]
	if len(mrc.configs) == 0 {
		// Fallback: load default config
		var err error
		cfg, err = config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
		if err != nil {
			return result, fmt.Errorf("failed to load config for global cleanup: %w", err)
		}
	}

	// Clean up IAM resources
	if err := mrc.cleanupIAMResources(ctx, cfg, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("IAM cleanup failed: %v", err))
	}

	// Clean up CloudFront distributions
	if err := mrc.cleanupCloudFrontResources(ctx, cfg, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("CloudFront cleanup failed: %v", err))
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Success = len(result.Errors) == 0

	return result, nil
}

// cleanupIAMResources cleans up IAM roles, policies, and instance profiles
func (mrc *MultiRegionCleanup) cleanupIAMResources(ctx context.Context, cfg aws.Config, result *GlobalCleanupResult) error {
	iamClient := iam.NewFromConfig(cfg)

	// Clean up roles
	if err := mrc.cleanupIAMRoles(ctx, iamClient, result); err != nil {
		return fmt.Errorf("failed to cleanup IAM roles: %w", err)
	}

	return nil
}

// cleanupIAMRoles deletes test IAM roles and associated policies
func (mrc *MultiRegionCleanup) cleanupIAMRoles(ctx context.Context, client *iam.Client, result *GlobalCleanupResult) error {
	// List all roles
	listInput := &iam.ListRolesInput{}

	for {
		listOutput, err := client.ListRoles(ctx, listInput)
		if err != nil {
			return fmt.Errorf("failed to list IAM roles: %w", err)
		}

		for _, role := range listOutput.Roles {
			roleName := *role.RoleName

			// Check if this is a test role
			if !mrc.isTestRole(roleName) {
				continue
			}

			// Verify with tags
			tagsOutput, err := client.ListRoleTags(ctx, &iam.ListRoleTagsInput{
				RoleName: &roleName,
			})
			if err != nil {
				continue // Skip if we can't get tags
			}

			isTestRole := false
			for _, tag := range tagsOutput.Tags {
				if *tag.Key == "TestID" && *tag.Value == mrc.testID {
					isTestRole = true
					break
				}
			}

			if !isTestRole {
				continue
			}

			// Delete inline policies
			listPoliciesOutput, err := client.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
				RoleName: &roleName,
			})
			if err == nil {
				for _, policyName := range listPoliciesOutput.PolicyNames {
					_, err := client.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
						RoleName:   &roleName,
						PolicyName: &policyName,
					})
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete inline policy %s: %v", policyName, err))
					}
				}
			}

			// Detach managed policies
			listAttachedOutput, err := client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
				RoleName: &roleName,
			})
			if err == nil {
				for _, policy := range listAttachedOutput.AttachedPolicies {
					_, err := client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
						RoleName:  &roleName,
						PolicyArn: policy.PolicyArn,
					})
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("Failed to detach policy %s: %v", *policy.PolicyArn, err))
					}
				}
			}

			// Remove role from instance profiles
			listProfilesOutput, err := client.ListInstanceProfilesForRole(ctx, &iam.ListInstanceProfilesForRoleInput{
				RoleName: &roleName,
			})
			if err == nil {
				for _, profile := range listProfilesOutput.InstanceProfiles {
					_, err := client.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
						InstanceProfileName: profile.InstanceProfileName,
						RoleName:            &roleName,
					})
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove role from instance profile %s: %v",
							*profile.InstanceProfileName, err))
					}
				}
			}

			// Delete the role
			_, err = client.DeleteRole(ctx, &iam.DeleteRoleInput{
				RoleName: &roleName,
			})
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete IAM role %s: %v", roleName, err))
			} else {
				result.ResourcesCleaned["IAMRoles"]++
			}
		}

		if !listOutput.IsTruncated {
			break
		}
		listInput.Marker = listOutput.Marker
	}

	return nil
}

// isTestRole checks if a role name matches test role patterns
func (mrc *MultiRegionCleanup) isTestRole(roleName string) bool {
	testPatterns := []string{
		"corkscrew-test",
		"corkscrew-edge",
		"corkscrew-cross-region",
		fmt.Sprintf("corkscrew-%s", mrc.testID),
	}

	for _, pattern := range testPatterns {
		if strings.Contains(roleName, pattern) {
			return true
		}
	}

	return false
}

// cleanupCloudFrontResources cleans up CloudFront distributions
func (mrc *MultiRegionCleanup) cleanupCloudFrontResources(ctx context.Context, cfg aws.Config, result *GlobalCleanupResult) error {
	cfClient := cloudfront.NewFromConfig(cfg)

	// List distributions
	listOutput, err := cfClient.ListDistributions(ctx, &cloudfront.ListDistributionsInput{})
	if err != nil {
		return fmt.Errorf("failed to list CloudFront distributions: %w", err)
	}

	if listOutput.DistributionList == nil {
		return nil
	}

	for _, distribution := range listOutput.DistributionList.Items {
		distributionId := *distribution.Id

		// Get distribution tags
		tagsOutput, err := cfClient.ListTagsForResource(ctx, &cloudfront.ListTagsForResourceInput{
			Resource: aws.String(fmt.Sprintf("arn:aws:cloudfront::%s:distribution/%s",
				mrc.getAccountID(ctx), distributionId)),
		})
		if err != nil {
			continue // Skip if we can't get tags
		}

		isTestDistribution := false
		for _, tag := range tagsOutput.Tags.Items {
			if *tag.Key == "TestID" && *tag.Value == mrc.testID {
				isTestDistribution = true
				break
			}
		}

		if !isTestDistribution {
			continue
		}

		// Disable distribution first (required before deletion)
		getOutput, err := cfClient.GetDistribution(ctx, &cloudfront.GetDistributionInput{
			Id: &distributionId,
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to get distribution %s: %v", distributionId, err))
			continue
		}

		if *getOutput.Distribution.DistributionConfig.Enabled {
			// Disable the distribution
			getOutput.Distribution.DistributionConfig.Enabled = aws.Bool(false)

			_, err = cfClient.UpdateDistribution(ctx, &cloudfront.UpdateDistributionInput{
				Id:                 &distributionId,
				DistributionConfig: getOutput.Distribution.DistributionConfig,
				IfMatch:            getOutput.ETag,
			})
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to disable distribution %s: %v", distributionId, err))
				continue
			}

			// Wait for distribution to be disabled
			waiter := cloudfront.NewDistributionDeployedWaiter(cfClient)
			err = waiter.Wait(ctx, &cloudfront.GetDistributionInput{Id: &distributionId}, 20*time.Minute)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Timeout waiting for distribution %s to be disabled: %v", distributionId, err))
				continue
			}
		}

		// Now delete the distribution
		getOutput, err = cfClient.GetDistribution(ctx, &cloudfront.GetDistributionInput{
			Id: &distributionId,
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to get distribution for deletion %s: %v", distributionId, err))
			continue
		}

		_, err = cfClient.DeleteDistribution(ctx, &cloudfront.DeleteDistributionInput{
			Id:      &distributionId,
			IfMatch: getOutput.ETag,
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to delete distribution %s: %v", distributionId, err))
		} else {
			result.ResourcesCleaned["CloudFrontDistributions"]++
		}
	}

	return nil
}

// getAccountID retrieves the AWS account ID (simplified implementation)
func (mrc *MultiRegionCleanup) getAccountID(ctx context.Context) string {
	// This is a simplified implementation
	// In practice, you would use STS to get the account ID
	return "123456789012" // Placeholder
}

// calculateSummary aggregates results from all regions and global cleanup
func (mrc *MultiRegionCleanup) calculateSummary(regionalResults []RegionCleanupResult, globalResult *GlobalCleanupResult) CleanupSummary {
	summary := CleanupSummary{
		TotalRegions:          len(regionalResults),
		TotalResourcesCleaned: make(map[string]int),
	}

	// Aggregate regional results
	for _, result := range regionalResults {
		if result.Success {
			summary.SuccessfulRegions++
		} else {
			summary.FailedRegions++
			summary.TotalErrors += len(result.Errors)
		}

		// Sum up resources cleaned by type
		for resourceType, count := range result.ResourcesCleaned {
			summary.TotalResourcesCleaned[resourceType] += count
		}
	}

	// Add global results
	if globalResult != nil {
		if !globalResult.Success {
			summary.TotalErrors += len(globalResult.Errors)
		}

		for resourceType, count := range globalResult.ResourcesCleaned {
			summary.TotalResourcesCleaned[resourceType] += count
		}
	}

	summary.OverallSuccess = summary.FailedRegions == 0 && (globalResult == nil || globalResult.Success)

	return summary
}

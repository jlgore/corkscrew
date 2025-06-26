package scenarios

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/kms"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/secretsmanager"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// SecurityStackScenario creates IAM roles, policies, KMS keys, and secrets
type SecurityStackScenario struct {
	expectedResources map[string]interface{}
}

// NewSecurityStackScenario creates a new security stack scenario
func NewSecurityStackScenario() *SecurityStackScenario {
	return &SecurityStackScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *SecurityStackScenario) GetName() string {
	return "security-stack"
}

// GetServices returns the AWS services this scenario tests
func (s *SecurityStackScenario) GetServices() []string {
	return []string{"iam", "kms", "secretsmanager"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *SecurityStackScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("security-stack"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create KMS key for encryption
	kmsKey, err := kms.NewKey(ctx, "test-kms-key", &kms.KeyArgs{
		Description: pulumi.String("Test KMS key for Corkscrew integration tests"),
		KeyUsage:    pulumi.String("ENCRYPT_DECRYPT"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Sid": "Enable IAM User Permissions",
					"Effect": "Allow",
					"Principal": {
						"AWS": "arn:aws:iam::*:root"
					},
					"Action": "kms:*",
					"Resource": "*"
				}
			]
		}`),
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create KMS key: %w", err)
	}

	// Create KMS alias
	kmsAlias, err := kms.NewAlias(ctx, "test-kms-alias", &kms.AliasArgs{
		Name:         pulumi.String(fmt.Sprintf("alias/corkscrew-test-%s", testID)),
		TargetKeyId:  kmsKey.KeyId,
	})
	if err != nil {
		return fmt.Errorf("failed to create KMS alias: %w", err)
	}

	// Create IAM role for Lambda execution
	lambdaRole, err := iam.NewRole(ctx, "lambda-execution-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Action": "sts:AssumeRole",
					"Effect": "Allow",
					"Principal": {
						"Service": "lambda.amazonaws.com"
					}
				}
			]
		}`),
		Description: pulumi.String("Role for Lambda function execution"),
		Tags:        tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create Lambda execution role: %w", err)
	}

	// Create IAM role for EC2 instances
	ec2Role, err := iam.NewRole(ctx, "ec2-instance-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Action": "sts:AssumeRole",
					"Effect": "Allow",
					"Principal": {
						"Service": "ec2.amazonaws.com"
					}
				}
			]
		}`),
		Description: pulumi.String("Role for EC2 instances"),
		Tags:        tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create EC2 instance role: %w", err)
	}

	// Create custom IAM policy for S3 access
	s3Policy, err := iam.NewPolicy(ctx, "s3-access-policy", &iam.PolicyArgs{
		Description: pulumi.String("Custom policy for S3 access"),
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"s3:GetObject",
						"s3:PutObject",
						"s3:DeleteObject"
					],
					"Resource": "arn:aws:s3:::corkscrew-test-*/*"
				},
				{
					"Effect": "Allow",
					"Action": [
						"s3:ListBucket"
					],
					"Resource": "arn:aws:s3:::corkscrew-test-*"
				}
			]
		}`),
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create S3 access policy: %w", err)
	}

	// Create custom IAM policy for KMS access
	kmsPolicy, err := iam.NewPolicy(ctx, "kms-access-policy", &iam.PolicyArgs{
		Description: pulumi.String("Custom policy for KMS access"),
		Policy: pulumi.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": [
						"kms:Encrypt",
						"kms:Decrypt",
						"kms:ReEncrypt*",
						"kms:GenerateDataKey*",
						"kms:DescribeKey"
					],
					"Resource": "%s"
				}
			]
		}`, kmsKey.Arn),
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create KMS access policy: %w", err)
	}

	// Attach AWS managed policies to Lambda role
	_, err = iam.NewRolePolicyAttachment(ctx, "lambda-basic-execution", &iam.RolePolicyAttachmentArgs{
		Role:      lambdaRole.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
	})
	if err != nil {
		return fmt.Errorf("failed to attach basic execution policy to Lambda role: %w", err)
	}

	// Attach custom policies to roles
	_, err = iam.NewRolePolicyAttachment(ctx, "lambda-s3-access", &iam.RolePolicyAttachmentArgs{
		Role:      lambdaRole.Name,
		PolicyArn: s3Policy.Arn,
	})
	if err != nil {
		return fmt.Errorf("failed to attach S3 policy to Lambda role: %w", err)
	}

	_, err = iam.NewRolePolicyAttachment(ctx, "ec2-s3-access", &iam.RolePolicyAttachmentArgs{
		Role:      ec2Role.Name,
		PolicyArn: s3Policy.Arn,
	})
	if err != nil {
		return fmt.Errorf("failed to attach S3 policy to EC2 role: %w", err)
	}

	_, err = iam.NewRolePolicyAttachment(ctx, "lambda-kms-access", &iam.RolePolicyAttachmentArgs{
		Role:      lambdaRole.Name,
		PolicyArn: kmsPolicy.Arn,
	})
	if err != nil {
		return fmt.Errorf("failed to attach KMS policy to Lambda role: %w", err)
	}

	// Create instance profile for EC2 role
	instanceProfile, err := iam.NewInstanceProfile(ctx, "ec2-instance-profile", &iam.InstanceProfileArgs{
		Role: ec2Role.Name,
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create instance profile: %w", err)
	}

	// Create IAM user for testing
	testUser, err := iam.NewUser(ctx, "test-user", &iam.UserArgs{
		Path: pulumi.String("/test/"),
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create test user: %w", err)
	}

	// Create access key for test user
	accessKey, err := iam.NewAccessKey(ctx, "test-user-access-key", &iam.AccessKeyArgs{
		User: testUser.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to create access key: %w", err)
	}

	// Create secret in Secrets Manager to store the access key
	secret, err := secretsmanager.NewSecret(ctx, "test-user-credentials", &secretsmanager.SecretArgs{
		Name:        pulumi.String(fmt.Sprintf("corkscrew-test-%s/user-credentials", testID)),
		Description: pulumi.String("Test user credentials for Corkscrew integration tests"),
		KmsKeyId:    kmsKey.Arn,
		Tags:        tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create secret: %w", err)
	}

	// Store the access key in the secret
	_, err = secretsmanager.NewSecretVersion(ctx, "test-user-credentials-version", &secretsmanager.SecretVersionArgs{
		SecretId: secret.ID(),
		SecretString: pulumi.Sprintf(`{
			"access_key_id": "%s",
			"secret_access_key": "%s",
			"user_name": "%s"
		}`, accessKey.ID(), accessKey.Secret, testUser.Name),
	})
	if err != nil {
		return fmt.Errorf("failed to create secret version: %w", err)
	}

	// Create IAM group
	testGroup, err := iam.NewGroup(ctx, "test-group", &iam.GroupArgs{
		Path: pulumi.String("/test/"),
	})
	if err != nil {
		return fmt.Errorf("failed to create test group: %w", err)
	}

	// Add user to group
	_, err = iam.NewGroupMembership(ctx, "test-group-membership", &iam.GroupMembershipArgs{
		Name:  pulumi.String("test-group-membership"),
		Users: pulumi.StringArray{testUser.Name},
		Group: testGroup.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to add user to group: %w", err)
	}

	// Attach read-only policy to group
	_, err = iam.NewGroupPolicyAttachment(ctx, "test-group-readonly", &iam.GroupPolicyAttachmentArgs{
		Group:     testGroup.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/ReadOnlyAccess"),
	})
	if err != nil {
		return fmt.Errorf("failed to attach readonly policy to group: %w", err)
	}

	// Export resource details for verification
	ctx.Export("kmsKeyId", kmsKey.KeyId)
	ctx.Export("kmsKeyArn", kmsKey.Arn)
	ctx.Export("kmsAliasName", kmsAlias.Name)
	ctx.Export("lambdaRoleArn", lambdaRole.Arn)
	ctx.Export("ec2RoleArn", ec2Role.Arn)
	ctx.Export("s3PolicyArn", s3Policy.Arn)
	ctx.Export("kmsAccessPolicyArn", kmsPolicy.Arn)
	ctx.Export("instanceProfileArn", instanceProfile.Arn)
	ctx.Export("testUserArn", testUser.Arn)
	ctx.Export("secretArn", secret.Arn)
	ctx.Export("testGroupArn", testGroup.Arn)

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"kms": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Key"),
				"id":   kmsKey.KeyId,
				"arn":  kmsKey.Arn,
				"attributes": pulumi.Map{
					"description": pulumi.String("Test KMS key for Corkscrew integration tests"),
					"key_usage":   pulumi.String("ENCRYPT_DECRYPT"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Alias"),
				"name": kmsAlias.Name,
				"attributes": pulumi.Map{
					"target_key_id": kmsKey.KeyId,
				},
			},
		},
		"iam": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Role"),
				"name": lambdaRole.Name,
				"arn":  lambdaRole.Arn,
				"attributes": pulumi.Map{
					"description": pulumi.String("Role for Lambda function execution"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Role"),
				"name": ec2Role.Name,
				"arn":  ec2Role.Arn,
				"attributes": pulumi.Map{
					"description": pulumi.String("Role for EC2 instances"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Policy"),
				"name": s3Policy.Name,
				"arn":  s3Policy.Arn,
				"attributes": pulumi.Map{
					"description": pulumi.String("Custom policy for S3 access"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Policy"),
				"name": kmsPolicy.Name,
				"arn":  kmsPolicy.Arn,
				"attributes": pulumi.Map{
					"description": pulumi.String("Custom policy for KMS access"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("User"),
				"name": testUser.Name,
				"arn":  testUser.Arn,
				"attributes": pulumi.Map{
					"path": pulumi.String("/test/"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Group"),
				"name": testGroup.Name,
				"arn":  testGroup.Arn,
				"attributes": pulumi.Map{
					"path": pulumi.String("/test/"),
				},
			},
		},
		"secretsmanager": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Secret"),
				"arn":  secret.Arn,
				"attributes": pulumi.Map{
					"description": pulumi.String("Test user credentials for Corkscrew integration tests"),
					"kms_key_id":  kmsKey.Arn,
				},
			},
		},
	})

	// Export relationships for verification
	ctx.Export("relationships", pulumi.Map{
		"role_to_policy": pulumi.Map{
			"from": lambdaRole.Arn,
			"to":   s3Policy.Arn,
			"type": pulumi.String("has_policy"),
		},
		"secret_to_kms": pulumi.Map{
			"from": secret.Arn,
			"to":   kmsKey.Arn,
			"type": pulumi.String("encrypted_by"),
		},
		"user_to_group": pulumi.Map{
			"from": testUser.Arn,
			"to":   testGroup.Arn,
			"type": pulumi.String("member_of"),
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"kms": []interface{}{
				map[string]interface{}{
					"type": "Key",
					"attributes": map[string]interface{}{
						"description": "Test KMS key for Corkscrew integration tests",
						"key_usage":   "ENCRYPT_DECRYPT",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "security-stack",
						"CreatedBy":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "Alias",
					"name": fmt.Sprintf("alias/corkscrew-test-%s", testID),
				},
			},
			"iam": []interface{}{
				map[string]interface{}{
					"type": "Role",
					"attributes": map[string]interface{}{
						"description": "Role for Lambda function execution",
					},
				},
				map[string]interface{}{
					"type": "Role",
					"attributes": map[string]interface{}{
						"description": "Role for EC2 instances",
					},
				},
				map[string]interface{}{
					"type": "Policy",
					"attributes": map[string]interface{}{
						"description": "Custom policy for S3 access",
					},
				},
				map[string]interface{}{
					"type": "Policy",
					"attributes": map[string]interface{}{
						"description": "Custom policy for KMS access",
					},
				},
				map[string]interface{}{
					"type": "User",
					"attributes": map[string]interface{}{
						"path": "/test/",
					},
				},
				map[string]interface{}{
					"type": "Group",
					"attributes": map[string]interface{}{
						"path": "/test/",
					},
				},
			},
			"secretsmanager": []interface{}{
				map[string]interface{}{
					"type": "Secret",
					"attributes": map[string]interface{}{
						"description": "Test user credentials for Corkscrew integration tests",
					},
				},
			},
		},
		"relationships": map[string]interface{}{
			"role_to_policy": map[string]interface{}{
				"type": "has_policy",
			},
			"secret_to_kms": map[string]interface{}{
				"type": "encrypted_by",
			},
			"user_to_group": map[string]interface{}{
				"type": "member_of",
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *SecurityStackScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}
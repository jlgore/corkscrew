package classification

import (
	"reflect"
	"strings"
)

// IAMHandler provides IAM-specific classification logic
type IAMHandler struct{}

func (h *IAMHandler) ClassifyOperation(operationName string, inputType reflect.Type) OperationClassification {
	// IAM-specific overrides
	iamSpecific := map[string]OperationClassification{
		"ListOrganizationsFeatures": {
			Type:        ParameterRequired,
			Confidence:  1.0,
			CanAttempt:  false,
			Reasoning:   "Requires organization master account access",
			Suggestions: []string{"Skip unless confirmed organization master account"},
		},
		"ListServiceSpecificCredentials": {
			Type:       CredentialDependent,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Only works with user credentials, not role credentials",
		},
		"ListAccessKeys": {
			Type:       CredentialDependent,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Must specify userName when calling with non-User credentials",
		},
		"ListMFADevices": {
			Type:       CredentialDependent,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Must specify userName when calling with non-User credentials",
		},
		"ListSigningCertificates": {
			Type:       CredentialDependent,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Must specify userName when calling with non-User credentials",
		},
		"ListSSHPublicKeys": {
			Type:       CredentialDependent,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Must specify userName when calling with non-User credentials",
		},
		// Sub-resource operations that require parent resource IDs
		"ListAttachedGroupPolicies": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires GroupName parameter",
		},
		"ListAttachedRolePolicies": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires RoleName parameter",
		},
		"ListAttachedUserPolicies": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires UserName parameter",
		},
		"ListGroupPolicies": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires GroupName parameter",
		},
		"ListRolePolicies": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires RoleName parameter",
		},
		"ListUserPolicies": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires UserName parameter",
		},
		"ListUserTags": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires UserName parameter",
		},
		"ListRoleTags": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires RoleName parameter",
		},
		"ListGroupsForUser": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires UserName parameter",
		},
		"ListEntitiesForPolicy": {
			Type:       ParameterRequired,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires PolicyArn parameter",
		},
		"ListPoliciesGrantingServiceAccess": {
			Type:       ParameterRequired,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires Arn and ServiceNamespaces parameters",
		},
	}

	if classification, exists := iamSpecific[operationName]; exists {
		return classification
	}

	return OperationClassification{Type: Unknown}
}

func (h *IAMHandler) CanProvideDefaults(operationName string, requiredFields []string) bool {
	// IAM generally doesn't support defaults for required fields
	return false
}

func (h *IAMHandler) GetDefaultParameters(operationName string) map[string]interface{} {
	// IAM operations typically don't have useful defaults
	return nil
}

// S3Handler provides S3-specific classification logic
type S3Handler struct{}

func (h *S3Handler) ClassifyOperation(operationName string, inputType reflect.Type) OperationClassification {
	s3Specific := map[string]OperationClassification{
		"ListBuckets": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "S3 ListBuckets is always parameter-free",
		},
		// All other List operations require bucket name
		"ListObjects": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires Bucket parameter",
		},
		"ListObjectsV2": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires Bucket parameter",
		},
		"ListMultipartUploads": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires Bucket parameter",
		},
		"ListParts": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires Bucket, Key, and UploadId parameters",
		},
	}

	if classification, exists := s3Specific[operationName]; exists {
		return classification
	}

	// Pattern-based classification for S3
	if strings.Contains(operationName, "Bucket") && operationName != "ListBuckets" {
		return OperationClassification{
			Type:       SubResource,
			Confidence: 0.9,
			CanAttempt: false,
			Reasoning:  "S3 bucket-specific operation requires bucket name",
		}
	}

	if strings.Contains(operationName, "Object") {
		return OperationClassification{
			Type:       SubResource,
			Confidence: 0.9,
			CanAttempt: false,
			Reasoning:  "S3 object operation requires bucket and key parameters",
		}
	}

	return OperationClassification{Type: Unknown}
}

func (h *S3Handler) CanProvideDefaults(operationName string, requiredFields []string) bool {
	return false // S3 operations requiring bucket names can't be defaulted
}

func (h *S3Handler) GetDefaultParameters(operationName string) map[string]interface{} {
	return nil
}

// EC2Handler provides EC2-specific classification logic
type EC2Handler struct{}

func (h *EC2Handler) ClassifyOperation(operationName string, inputType reflect.Type) OperationClassification {
	ec2Specific := map[string]OperationClassification{
		"DescribeInstances": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "EC2 DescribeInstances works without parameters",
		},
		"DescribeVolumes": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "EC2 DescribeVolumes works without parameters",
		},
		"DescribeSnapshots": {
			Type:          FilterRequired,
			Confidence:    1.0,
			CanAttempt:    true,
			Reasoning:     "EC2 DescribeSnapshots needs owner filter, can default to 'self'",
			DefaultParams: map[string]interface{}{"Owners": []string{"self"}},
		},
		"DescribeImages": {
			Type:          FilterRequired,
			Confidence:    1.0,
			CanAttempt:    true,
			Reasoning:     "EC2 DescribeImages needs owner filter, can default to 'self'",
			DefaultParams: map[string]interface{}{"Owners": []string{"self"}},
		},
		"DescribeVpcs": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "EC2 DescribeVpcs works without parameters",
		},
		"DescribeSubnets": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "EC2 DescribeSubnets works without parameters",
		},
		"DescribeSecurityGroups": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "EC2 DescribeSecurityGroups works without parameters",
		},
		"DescribeKeyPairs": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "EC2 DescribeKeyPairs works without parameters",
		},
	}

	if classification, exists := ec2Specific[operationName]; exists {
		return classification
	}

	return OperationClassification{Type: Unknown}
}

func (h *EC2Handler) CanProvideDefaults(operationName string, requiredFields []string) bool {
	// EC2 can provide owner filters for snapshots and images
	return operationName == "DescribeSnapshots" || operationName == "DescribeImages"
}

func (h *EC2Handler) GetDefaultParameters(operationName string) map[string]interface{} {
	switch operationName {
	case "DescribeSnapshots", "DescribeImages":
		return map[string]interface{}{"Owners": []string{"self"}}
	default:
		return nil
	}
}

// LambdaHandler provides Lambda-specific classification logic
type LambdaHandler struct{}

func (h *LambdaHandler) ClassifyOperation(operationName string, inputType reflect.Type) OperationClassification {
	lambdaSpecific := map[string]OperationClassification{
		"ListFunctions": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "Lambda ListFunctions is parameter-free",
		},
		"ListLayers": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "Lambda ListLayers is parameter-free",
		},
		"ListCodeSigningConfigs": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "Lambda ListCodeSigningConfigs is parameter-free",
		},
		"ListEventSourceMappings": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "Lambda ListEventSourceMappings can work without EventSourceArn",
		},
		// Operations requiring function name
		"ListAliases": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires FunctionName parameter",
		},
		"ListVersionsByFunction": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires FunctionName parameter",
		},
		"ListLayerVersions": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires LayerName parameter",
		},
		"ListProvisionedConcurrencyConfigs": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires FunctionName parameter",
		},
		"ListTags": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires Resource (ARN) parameter",
		},
	}

	if classification, exists := lambdaSpecific[operationName]; exists {
		return classification
	}

	return OperationClassification{Type: Unknown}
}

func (h *LambdaHandler) CanProvideDefaults(operationName string, requiredFields []string) bool {
	return false
}

func (h *LambdaHandler) GetDefaultParameters(operationName string) map[string]interface{} {
	return nil
}

// SNSHandler provides SNS-specific classification logic
type SNSHandler struct{}

func (h *SNSHandler) ClassifyOperation(operationName string, inputType reflect.Type) OperationClassification {
	snsSpecific := map[string]OperationClassification{
		"ListTopics": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "SNS ListTopics is parameter-free",
		},
		"ListSubscriptions": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "SNS ListSubscriptions is parameter-free",
		},
		"ListPlatformApplications": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "SNS ListPlatformApplications is parameter-free",
		},
		"ListOriginationNumbers": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "SNS ListOriginationNumbers is parameter-free",
		},
		"ListPhoneNumbersOptedOut": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "SNS ListPhoneNumbersOptedOut is parameter-free",
		},
		// Operations requiring topic ARN
		"ListSubscriptionsByTopic": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires TopicArn parameter",
		},
		"ListTagsForResource": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires ResourceArn parameter",
		},
		"ListEndpointsByPlatformApplication": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires PlatformApplicationArn parameter",
		},
	}

	if classification, exists := snsSpecific[operationName]; exists {
		return classification
	}

	return OperationClassification{Type: Unknown}
}

func (h *SNSHandler) CanProvideDefaults(operationName string, requiredFields []string) bool {
	return false
}

func (h *SNSHandler) GetDefaultParameters(operationName string) map[string]interface{} {
	return nil
}

// DynamoDBHandler provides DynamoDB-specific classification logic
type DynamoDBHandler struct{}

func (h *DynamoDBHandler) ClassifyOperation(operationName string, inputType reflect.Type) OperationClassification {
	dynamodbSpecific := map[string]OperationClassification{
		"ListTables": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "DynamoDB ListTables is parameter-free",
		},
		"ListBackups": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "DynamoDB ListBackups is parameter-free",
		},
		"ListGlobalTables": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "DynamoDB ListGlobalTables is parameter-free",
		},
		"ListContributorInsights": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "DynamoDB ListContributorInsights is parameter-free",
		},
		"ListExports": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "DynamoDB ListExports is parameter-free",
		},
		"ListImports": {
			Type:       GlobalResource,
			Confidence: 1.0,
			CanAttempt: true,
			Reasoning:  "DynamoDB ListImports is parameter-free",
		},
		// Operations requiring table name or resource ARN
		"ListTagsOfResource": {
			Type:       SubResource,
			Confidence: 1.0,
			CanAttempt: false,
			Reasoning:  "Requires ResourceArn parameter",
		},
	}

	if classification, exists := dynamodbSpecific[operationName]; exists {
		return classification
	}

	return OperationClassification{Type: Unknown}
}

func (h *DynamoDBHandler) CanProvideDefaults(operationName string, requiredFields []string) bool {
	return false
}

func (h *DynamoDBHandler) GetDefaultParameters(operationName string) map[string]interface{} {
	return nil
}

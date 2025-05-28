package classification

import (
	"testing"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

func TestOperationClassifier_ClassifyOperation(t *testing.T) {
	classifier := NewOperationClassifier()

	tests := []struct {
		name        string
		serviceName string
		operation   generator.AWSOperation
		expected    ClassificationType
		canAttempt  bool
		description string
	}{
		// Global Resource Operations
		{
			name:        "S3 ListBuckets",
			serviceName: "s3",
			operation:   generator.AWSOperation{Name: "ListBuckets", IsList: true},
			expected:    GlobalResource,
			canAttempt:  true,
			description: "S3 ListBuckets should be classified as global resource",
		},
		{
			name:        "IAM ListUsers",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListUsers", IsList: true},
			expected:    GlobalResource,
			canAttempt:  true,
			description: "IAM ListUsers should be classified as global resource",
		},
		{
			name:        "Lambda ListFunctions",
			serviceName: "lambda",
			operation:   generator.AWSOperation{Name: "ListFunctions", IsList: true},
			expected:    GlobalResource,
			canAttempt:  true,
			description: "Lambda ListFunctions should be classified as global resource",
		},
		{
			name:        "EC2 DescribeInstances",
			serviceName: "ec2",
			operation:   generator.AWSOperation{Name: "DescribeInstances", IsDescribe: true},
			expected:    GlobalResource,
			canAttempt:  true,
			description: "EC2 DescribeInstances should be classified as global resource",
		},
		{
			name:        "DynamoDB ListTables",
			serviceName: "dynamodb",
			operation:   generator.AWSOperation{Name: "ListTables", IsList: true},
			expected:    GlobalResource,
			canAttempt:  true,
			description: "DynamoDB ListTables should be classified as global resource",
		},

		// Sub-Resource Operations
		{
			name:        "S3 ListObjects",
			serviceName: "s3",
			operation:   generator.AWSOperation{Name: "ListObjects", IsList: true},
			expected:    SubResource,
			canAttempt:  false,
			description: "S3 ListObjects should require bucket name",
		},
		{
			name:        "IAM ListAttachedUserPolicies",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListAttachedUserPolicies", IsList: true},
			expected:    SubResource,
			canAttempt:  false,
			description: "IAM ListAttachedUserPolicies should require UserName",
		},
		{
			name:        "Lambda ListAliases",
			serviceName: "lambda",
			operation:   generator.AWSOperation{Name: "ListAliases", IsList: true},
			expected:    SubResource,
			canAttempt:  false,
			description: "Lambda ListAliases should require FunctionName",
		},
		{
			name:        "SNS ListSubscriptionsByTopic",
			serviceName: "sns",
			operation:   generator.AWSOperation{Name: "ListSubscriptionsByTopic", IsList: true},
			expected:    SubResource,
			canAttempt:  false,
			description: "SNS ListSubscriptionsByTopic should require TopicArn",
		},

		// Credential-Dependent Operations
		{
			name:        "IAM ListAccessKeys",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListAccessKeys", IsList: true},
			expected:    CredentialDependent,
			canAttempt:  false,
			description: "IAM ListAccessKeys should be credential-dependent",
		},
		{
			name:        "IAM ListMFADevices",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListMFADevices", IsList: true},
			expected:    CredentialDependent,
			canAttempt:  false,
			description: "IAM ListMFADevices should be credential-dependent",
		},

		// Filter-Required Operations
		{
			name:        "EC2 DescribeSnapshots",
			serviceName: "ec2",
			operation:   generator.AWSOperation{Name: "DescribeSnapshots", IsDescribe: true},
			expected:    FilterRequired,
			canAttempt:  true,
			description: "EC2 DescribeSnapshots should be filter-required but attemptable",
		},
		{
			name:        "EC2 DescribeImages",
			serviceName: "ec2",
			operation:   generator.AWSOperation{Name: "DescribeImages", IsDescribe: true},
			expected:    FilterRequired,
			canAttempt:  true,
			description: "EC2 DescribeImages should be filter-required but attemptable",
		},

		// Parameter-Required Operations
		{
			name:        "IAM ListPoliciesGrantingServiceAccess",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListPoliciesGrantingServiceAccess", IsList: true},
			expected:    ParameterRequired,
			canAttempt:  false,
			description: "IAM ListPoliciesGrantingServiceAccess should require specific parameters",
		},
		{
			name:        "IAM ListEntitiesForPolicy",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListEntitiesForPolicy", IsList: true},
			expected:    ParameterRequired,
			canAttempt:  false,
			description: "IAM ListEntitiesForPolicy should require PolicyArn",
		},

		// Pattern-based Classifications
		{
			name:        "Generic List operation with 'For' pattern",
			serviceName: "unknown",
			operation:   generator.AWSOperation{Name: "ListItemsForContainer", IsList: true},
			expected:    SubResource,
			canAttempt:  false,
			description: "Operations with 'For' pattern should be sub-resource",
		},
		{
			name:        "Generic List operation with 'By' pattern",
			serviceName: "unknown",
			operation:   generator.AWSOperation{Name: "ListItemsByCategory", IsList: true},
			expected:    SubResource,
			canAttempt:  false,
			description: "Operations with 'By' pattern should be sub-resource",
		},
		{
			name:        "Generic List operation with 'Tags' suffix",
			serviceName: "unknown",
			operation:   generator.AWSOperation{Name: "ListResourceTags", IsList: true},
			expected:    SubResource,
			canAttempt:  false,
			description: "Operations with 'Tags' suffix should be sub-resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifier.ClassifyOperation(tt.serviceName, tt.operation.Name, tt.operation)

			if result.Type != tt.expected {
				t.Errorf("ClassifyOperation() type = %v, expected %v", result.Type, tt.expected)
			}

			if result.CanAttempt != tt.canAttempt {
				t.Errorf("ClassifyOperation() canAttempt = %v, expected %v", result.CanAttempt, tt.canAttempt)
			}

			// Verify confidence is reasonable
			if result.Confidence < 0.0 || result.Confidence > 1.0 {
				t.Errorf("ClassifyOperation() confidence = %v, should be between 0.0 and 1.0", result.Confidence)
			}

			// Verify reasoning is provided
			if result.Reasoning == "" && result.Type != Unknown {
				t.Errorf("ClassifyOperation() should provide reasoning for classification")
			}

			t.Logf("Classification: %s (confidence: %.2f, reasoning: %s)",
				result.Type, result.Confidence, result.Reasoning)
		})
	}
}

func TestOperationClassifier_IsParameterFreeOperation(t *testing.T) {
	classifier := NewOperationClassifier()

	tests := []struct {
		name        string
		serviceName string
		operation   generator.AWSOperation
		expected    bool
	}{
		// Should be parameter-free
		{
			name:        "S3 ListBuckets",
			serviceName: "s3",
			operation:   generator.AWSOperation{Name: "ListBuckets", IsList: true},
			expected:    true,
		},
		{
			name:        "IAM ListUsers",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListUsers", IsList: true},
			expected:    true,
		},
		{
			name:        "EC2 DescribeInstances",
			serviceName: "ec2",
			operation:   generator.AWSOperation{Name: "DescribeInstances", IsDescribe: true},
			expected:    true,
		},
		{
			name:        "EC2 DescribeSnapshots with defaults",
			serviceName: "ec2",
			operation:   generator.AWSOperation{Name: "DescribeSnapshots", IsDescribe: true},
			expected:    true,
		},

		// Should NOT be parameter-free
		{
			name:        "S3 ListObjects",
			serviceName: "s3",
			operation:   generator.AWSOperation{Name: "ListObjects", IsList: true},
			expected:    false,
		},
		{
			name:        "IAM ListAccessKeys",
			serviceName: "iam",
			operation:   generator.AWSOperation{Name: "ListAccessKeys", IsList: true},
			expected:    false,
		},
		{
			name:        "Lambda ListAliases",
			serviceName: "lambda",
			operation:   generator.AWSOperation{Name: "ListAliases", IsList: true},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifier.IsParameterFreeOperation(tt.serviceName, tt.operation)
			if result != tt.expected {
				t.Errorf("IsParameterFreeOperation() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestOperationClassifier_GetOperationDefaults(t *testing.T) {
	classifier := NewOperationClassifier()

	tests := []struct {
		name        string
		serviceName string
		operation   string
		expectNil   bool
		expectOwner bool
	}{
		{
			name:        "EC2 DescribeSnapshots should have owner filter",
			serviceName: "ec2",
			operation:   "DescribeSnapshots",
			expectNil:   false,
			expectOwner: true,
		},
		{
			name:        "EC2 DescribeImages should have owner filter",
			serviceName: "ec2",
			operation:   "DescribeImages",
			expectNil:   false,
			expectOwner: true,
		},
		{
			name:        "S3 ListBuckets should have no defaults",
			serviceName: "s3",
			operation:   "ListBuckets",
			expectNil:   true,
			expectOwner: false,
		},
		{
			name:        "IAM ListUsers should have no defaults",
			serviceName: "iam",
			operation:   "ListUsers",
			expectNil:   true,
			expectOwner: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifier.GetOperationDefaults(tt.serviceName, tt.operation)

			if tt.expectNil && result != nil {
				t.Errorf("GetOperationDefaults() = %v, expected nil", result)
			}

			if !tt.expectNil && result == nil {
				t.Errorf("GetOperationDefaults() = nil, expected non-nil")
			}

			if tt.expectOwner {
				if owners, exists := result["Owners"]; exists {
					if ownerSlice, ok := owners.([]string); ok {
						if len(ownerSlice) == 0 || ownerSlice[0] != "self" {
							t.Errorf("GetOperationDefaults() owners = %v, expected ['self']", ownerSlice)
						}
					} else {
						t.Errorf("GetOperationDefaults() owners type = %T, expected []string", owners)
					}
				} else {
					t.Errorf("GetOperationDefaults() missing 'Owners' key")
				}
			}
		})
	}
}

func TestPatternMatching(t *testing.T) {
	classifier := NewOperationClassifier()

	tests := []struct {
		name           string
		operationName  string
		expectedType   ClassificationType
		expectedReason string
	}{
		{
			name:          "Plural list operation",
			operationName: "ListTopics",
			expectedType:  GlobalResource,
		},
		{
			name:          "List with 'For' pattern",
			operationName: "ListGroupsForUser",
			expectedType:  SubResource,
		},
		{
			name:          "List with 'By' pattern",
			operationName: "ListSubscriptionsByTopic",
			expectedType:  SubResource,
		},
		{
			name:          "List with 'Tags' suffix",
			operationName: "ListUserTags",
			expectedType:  SubResource,
		},
		{
			name:          "List with 'Attached' pattern",
			operationName: "ListAttachedUserPolicies",
			expectedType:  SubResource,
		},
		{
			name:          "List with 'Granting' pattern",
			operationName: "ListPoliciesGrantingServiceAccess",
			expectedType:  ParameterRequired,
		},
		{
			name:          "Credential-dependent operation",
			operationName: "ListAccessKeys",
			expectedType:  CredentialDependent,
		},
		{
			name:          "Filter-required operation",
			operationName: "DescribeSnapshots",
			expectedType:  FilterRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a dummy operation
			operation := generator.AWSOperation{
				Name:   tt.operationName,
				IsList: true,
			}
			if tt.operationName == "DescribeSnapshots" {
				operation.IsList = false
				operation.IsDescribe = true
			}

			result := classifier.classifyByPatterns(tt.operationName, operation)

			if result.Type != tt.expectedType {
				t.Errorf("Pattern matching for %s: got %v, expected %v",
					tt.operationName, result.Type, tt.expectedType)
			}

			// Verify that reasoning is provided
			if result.Reasoning == "" && result.Type != Unknown {
				t.Errorf("Pattern matching for %s: no reasoning provided", tt.operationName)
			}

			t.Logf("Operation %s: %s (confidence: %.2f, reasoning: %s)",
				tt.operationName, result.Type, result.Confidence, result.Reasoning)
		})
	}
}

func TestFallbackClassification(t *testing.T) {
	classifier := NewOperationClassifier()

	tests := []struct {
		name          string
		operationName string
		expectedType  ClassificationType
	}{
		{
			name:          "Simple List operation",
			operationName: "ListSomething",
			expectedType:  GlobalResource,
		},
		{
			name:          "List with sub-resource indicator",
			operationName: "ListSomethingConfiguration",
			expectedType:  SubResource,
		},
		{
			name:          "Simple Describe operation",
			operationName: "DescribeSomething",
			expectedType:  GlobalResource,
		},
		{
			name:          "Describe with parameter indicator",
			operationName: "DescribeSomethingAttribute",
			expectedType:  ParameterRequired,
		},
		{
			name:          "Unknown operation type",
			operationName: "CreateSomething",
			expectedType:  Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifier.fallbackClassification(tt.operationName)
			if result != tt.expectedType {
				t.Errorf("Fallback classification for %s: got %v, expected %v",
					tt.operationName, result, tt.expectedType)
			}
		})
	}
}

func TestServiceHandlerRegistration(t *testing.T) {
	classifier := NewOperationClassifier()

	// Test that service handlers are properly registered
	expectedServices := []string{"iam", "s3", "ec2", "lambda", "sns", "dynamodb"}

	for _, service := range expectedServices {
		if _, exists := classifier.serviceHandlers[service]; !exists {
			t.Errorf("Service handler for %s not registered", service)
		}
	}

	// Test registering a custom handler
	customHandler := &IAMHandler{} // Reuse IAM handler for testing
	classifier.RegisterServiceHandler("custom", customHandler)

	if _, exists := classifier.serviceHandlers["custom"]; !exists {
		t.Errorf("Custom service handler not registered")
	}
}

func BenchmarkClassifyOperation(b *testing.B) {
	classifier := NewOperationClassifier()
	operation := generator.AWSOperation{Name: "ListBuckets", IsList: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		classifier.ClassifyOperation("s3", "ListBuckets", operation)
	}
}

func BenchmarkPatternMatching(b *testing.B) {
	classifier := NewOperationClassifier()
	operation := generator.AWSOperation{Name: "ListSomeComplexOperationName", IsList: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		classifier.classifyByPatterns("ListSomeComplexOperationName", operation)
	}
}

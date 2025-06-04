package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
)

// TestUnifiedScannerUnit provides unit tests that don't require AWS credentials
func TestUnifiedScannerUnit(t *testing.T) {
	// Create a mock AWS config for testing
	cfg := aws.Config{
		Region: "us-east-1",
	}

	// Create UnifiedScanner components
	clientFactory := NewClientFactory(cfg)
	scanner := NewUnifiedScanner(clientFactory)

	t.Run("ClientFactoryServices", func(t *testing.T) {
		testClientFactoryServices(t, clientFactory)
	})

	t.Run("ScannerCreation", func(t *testing.T) {
		assert.NotNil(t, scanner, "UnifiedScanner should be created successfully")
	})

	t.Run("OperationClassification", func(t *testing.T) {
		testOperationClassification(t, scanner)
	})

	t.Run("FieldClassification", func(t *testing.T) {
		testFieldClassification(t, scanner)
	})

	t.Run("ResourceTypeInference", func(t *testing.T) {
		testResourceTypeInference(t, scanner)
	})

	t.Run("ARNHandling", func(t *testing.T) {
		testARNHandling(t, scanner)
	})
}

func testClientFactoryServices(t *testing.T, factory *ClientFactory) {
	t.Log("Testing ClientFactory service support...")

	// Test that all expected services are available
	availableServices := factory.GetAvailableServices()
	expectedServices := []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"route53", "redshift",
	}

	assert.GreaterOrEqual(t, len(availableServices), len(expectedServices),
		"Should have at least the expected number of services")

	for _, expectedService := range expectedServices {
		assert.Contains(t, availableServices, expectedService,
			"Service %s should be available", expectedService)
	}

	// Test client creation
	for _, service := range expectedServices {
		t.Run("Client_"+service, func(t *testing.T) {
			client := factory.GetClient(service)
			assert.NotNil(t, client, "Should be able to create client for %s", service)
			
			if client != nil {
				assert.True(t, factory.HasClient(service), 
					"HasClient should return true for %s", service)
			}
		})
	}

	// Test invalid service
	t.Run("InvalidService", func(t *testing.T) {
		client := factory.GetClient("nonexistent-service")
		assert.Nil(t, client, "Should return nil for nonexistent service")
		
		assert.False(t, factory.HasClient("nonexistent-service"),
			"HasClient should return false for nonexistent service")
	})
}

func testOperationClassification(t *testing.T, scanner *UnifiedScanner) {
	t.Log("Testing operation classification logic...")

	testCases := []struct {
		methodName   string
		expectList   bool
		description  string
	}{
		// Core list operations
		{"ListBuckets", true, "S3 list buckets"},
		{"DescribeInstances", true, "EC2 describe instances"},
		{"ListFunctions", true, "Lambda list functions"},
		{"DescribeDBInstances", true, "RDS describe DB instances"},
		{"ListTables", true, "DynamoDB list tables"},
		{"ListUsers", true, "IAM list users"},
		{"ListRoles", true, "IAM list roles"},
		{"DescribeVolumes", true, "EC2 describe volumes"},
		{"DescribeSecurityGroups", true, "EC2 describe security groups"},
		{"DescribeVpcs", true, "EC2 describe VPCs"},
		{"DescribeDBClusters", true, "RDS describe DB clusters"},

		// Operations that should be excluded
		{"ListTagsForResource", false, "tag operation should be excluded"},
		{"ListBucketAnalytics", false, "bucket analytics should be excluded"},
		{"ListBucketInventory", false, "bucket inventory should be excluded"},
		{"ListBucketMetrics", false, "bucket metrics should be excluded"},
		{"ListMultipartUploads", false, "multipart uploads should be excluded"},
		{"ListObjectVersions", false, "object versions should be excluded"},
		{"ListParts", false, "parts should be excluded"},
		{"ListVersions", false, "versions should be excluded"},
		{"ListAccountSettings", false, "account settings should be excluded"},
		{"ListContributorInsights", false, "contributor insights should be excluded"},
		{"ListBackups", false, "backups should be excluded"},
		{"ListExports", false, "exports should be excluded"},
		{"ListImports", false, "imports should be excluded"},

		// Non-list operations
		{"CreateBucket", false, "create operation"},
		{"DeleteBucket", false, "delete operation"},
		{"PutObject", false, "put operation"},
		{"GetObject", false, "get operation"},
		{"UpdateFunction", false, "update operation"},
		{"StartInstance", false, "start operation"},
		{"StopInstance", false, "stop operation"},

		// Edge cases
		{"List", false, "too short"},
		{"Describe", false, "too short"},
		{"", false, "empty string"},
		{"list", false, "lowercase (should not match)"},
		{"describe", false, "lowercase (should not match)"},
	}

	for _, tc := range testCases {
		t.Run("Op_"+tc.methodName, func(t *testing.T) {
			result := scanner.isListOperation(tc.methodName)
			assert.Equal(t, tc.expectList, result,
				"Method %s classification failed: %s", tc.methodName, tc.description)
		})
	}
}

func testFieldClassification(t *testing.T, scanner *UnifiedScanner) {
	t.Log("Testing field classification logic...")

	// Test identifier field detection
	identifierTests := []struct {
		fieldName string
		expected  bool
	}{
		{"id", true},
		{"resourceid", true},
		{"instanceid", true},
		{"functionname", true},
		{"tablename", true},
		{"bucketname", true},
		{"dbinstanceidentifier", true},
		{"clustername", true},
		{"groupname", true},
		{"policyname", true},
		{"rolename", true},
		{"username", true},
		{"keyid", true},
		{"queueurl", true},
		{"topicarn", true},
		{"streamname", true},
		{"domainname", true},
		{"repositoryname", true},
		{"random", false},
		{"name", false}, // Should be name field, not identifier
		{"description", false},
	}

	for _, test := range identifierTests {
		t.Run("ID_"+test.fieldName, func(t *testing.T) {
			result := scanner.isIdentifierField(test.fieldName)
			assert.Equal(t, test.expected, result,
				"Field %s identifier classification failed", test.fieldName)
		})
	}

	// Test name field detection
	nameTests := []struct {
		fieldName string
		expected  bool
	}{
		{"name", true},
		{"resourcename", true},
		{"displayname", true},
		{"title", true},
		{"label", true},
		{"id", false},
		{"arn", false},
		{"description", false},
	}

	for _, test := range nameTests {
		t.Run("Name_"+test.fieldName, func(t *testing.T) {
			result := scanner.isNameField(test.fieldName)
			assert.Equal(t, test.expected, result,
				"Field %s name classification failed", test.fieldName)
		})
	}

	// Test ARN field detection
	arnTests := []struct {
		fieldName string
		expected  bool
	}{
		{"arn", true},
		{"resourcearn", true},
		{"targetarn", true},
		{"sourcearn", true},
		{"name", false},
		{"id", false},
		{"description", false},
	}

	for _, test := range arnTests {
		t.Run("ARN_"+test.fieldName, func(t *testing.T) {
			result := scanner.isARNField(test.fieldName)
			assert.Equal(t, test.expected, result,
				"Field %s ARN classification failed", test.fieldName)
		})
	}

	// Test state field detection
	stateTests := []struct {
		fieldName string
		expected  bool
	}{
		{"state", true},
		{"status", true},
		{"health", true},
		{"condition", true},
		{"instancestate", true},
		{"dbstatus", true},
		{"name", false},
		{"id", false},
		{"arn", false},
	}

	for _, test := range stateTests {
		t.Run("State_"+test.fieldName, func(t *testing.T) {
			result := scanner.isStateField(test.fieldName)
			assert.Equal(t, test.expected, result,
				"Field %s state classification failed", test.fieldName)
		})
	}

	// Test region field detection
	regionTests := []struct {
		fieldName string
		expected  bool
	}{
		{"region", true},
		{"availabilityzone", false}, // Contains "zone" but not "region"
		{"placement", false},        // Contains "placement" but not "region"
		{"name", false},
		{"id", false},
	}

	for _, test := range regionTests {
		t.Run("Region_"+test.fieldName, func(t *testing.T) {
			result := scanner.isRegionField(test.fieldName)
			assert.Equal(t, test.expected, result,
				"Field %s region classification failed", test.fieldName)
		})
	}
}

func testResourceTypeInference(t *testing.T, scanner *UnifiedScanner) {
	t.Log("Testing resource type inference...")

	testCases := []struct {
		serviceName  string
		fieldName    string
		expectedType string
	}{
		// EC2 patterns
		{"ec2", "instances", "Instance"},
		{"ec2", "volumes", "Volume"},
		{"ec2", "snapshots", "Snapshot"},
		{"ec2", "vpcs", "Vpc"},
		{"ec2", "subnets", "Subnet"},
		{"ec2", "securitygroups", "SecurityGroup"},
		{"ec2", "routetables", "RouteTable"},
		{"ec2", "internetgateways", "InternetGateway"},

		// S3 patterns
		{"s3", "buckets", "Bucket"},
		{"s3", "objects", "Object"},

		// Lambda patterns
		{"lambda", "functions", "Function"},
		{"lambda", "layers", "Layer"},

		// RDS patterns
		{"rds", "dbinstances", "DBInstance"},
		{"rds", "dbclusters", "DBCluster"},
		{"rds", "dbsnapshots", "DBSnapshot"},

		// DynamoDB patterns
		{"dynamodb", "tables", "Table"},

		// IAM patterns
		{"iam", "users", "User"},
		{"iam", "roles", "Role"},
		{"iam", "policies", "Policy"},
		{"iam", "groups", "Group"},

		// EKS patterns
		{"eks", "clusters", "Cluster"},
		{"eks", "nodegroups", "NodeGroup"},

		// ELB patterns
		{"elb", "loadbalancers", "LoadBalancer"},
		{"elb", "targetgroups", "TargetGroup"},

		// Fallback cases
		{"unknown", "unknowns", "unknown"},
		{"test", "items", "item"},
		{"service", "resources", "resource"},
	}

	for _, tc := range testCases {
		t.Run("ResourceType_"+tc.serviceName+"_"+tc.fieldName, func(t *testing.T) {
			result := scanner.inferResourceType(tc.serviceName, tc.fieldName)
			assert.Equal(t, tc.expectedType, result,
				"Service %s field %s should infer type %s, got %s",
				tc.serviceName, tc.fieldName, tc.expectedType, result)
		})
	}
}

func testARNHandling(t *testing.T, scanner *UnifiedScanner) {
	t.Log("Testing ARN handling...")

	testCases := []struct {
		arn          string
		expectedName string
		description  string
	}{
		{
			"arn:aws:s3:::my-bucket",
			"my-bucket",
			"S3 bucket ARN",
		},
		{
			"arn:aws:ec2:us-east-1:123456789012:instance/i-0abcd1234efgh5678",
			"i-0abcd1234efgh5678",
			"EC2 instance ARN",
		},
		{
			"arn:aws:lambda:us-east-1:123456789012:function:my-function",
			"my-function",
			"Lambda function ARN",
		},
		{
			"arn:aws:rds:us-east-1:123456789012:db:mydb-instance",
			"mydb-instance",
			"RDS database ARN",
		},
		{
			"arn:aws:iam::123456789012:user/my-user",
			"my-user",
			"IAM user ARN",
		},
		{
			"arn:aws:iam::123456789012:role/service-role/my-role",
			"my-role",
			"IAM role ARN with path",
		},
		{
			"simple-name",
			"simple-name",
			"Non-ARN identifier",
		},
		{
			"",
			"",
			"Empty string",
		},
	}

	for _, tc := range testCases {
		t.Run("ARN_"+tc.description, func(t *testing.T) {
			result := scanner.extractNameFromId(tc.arn)
			assert.Equal(t, tc.expectedName, result,
				"ARN %s should extract name %s, got %s",
				tc.arn, tc.expectedName, result)
		})
	}
}

// TestClientDiscovery tests the ability to discover operations on AWS clients
func TestClientDiscovery(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1"}
	clientFactory := NewClientFactory(cfg)
	scanner := NewUnifiedScanner(clientFactory)

	// Test S3 client operation discovery
	t.Run("S3OperationDiscovery", func(t *testing.T) {
		client := clientFactory.GetClient("s3")
		require.NotNil(t, client, "S3 client should be available")

		listOps := scanner.findListOperations(client)
		t.Logf("S3 list operations found: %v", listOps)
		
		// S3 should have at least ListBuckets
		assert.Contains(t, listOps, "ListBuckets", "S3 should have ListBuckets operation")
		assert.GreaterOrEqual(t, len(listOps), 1, "S3 should have at least one list operation")
	})

	// Test EC2 client operation discovery
	t.Run("EC2OperationDiscovery", func(t *testing.T) {
		client := clientFactory.GetClient("ec2")
		require.NotNil(t, client, "EC2 client should be available")

		listOps := scanner.findListOperations(client)
		t.Logf("EC2 list operations found: %v", listOps)
		
		// EC2 should have describe operations
		expectedOps := []string{"DescribeInstances", "DescribeVolumes"}
		for _, expectedOp := range expectedOps {
			assert.Contains(t, listOps, expectedOp, "EC2 should have %s operation", expectedOp)
		}
		assert.GreaterOrEqual(t, len(listOps), 2, "EC2 should have multiple list operations")
	})
}

// TestServiceRegistry tests the service registry functionality
func TestServiceRegistry(t *testing.T) {
	// Test mock registry
	mockRegistry := &mockServiceRegistry{}
	mockRegistry.services = make(map[string]*scanner.ScannerServiceDefinition)
	
	// Add test service definition
	mockRegistry.services["s3"] = &scanner.ScannerServiceDefinition{
		Name: "s3",
		ResourceTypes: []scanner.ResourceTypeDefinition{
			{
				Name:         "Bucket",
				ResourceType: "Bucket",
				IDField:      "Name",
				ARNPattern:   "arn:aws:s3:::{name}",
			},
		},
	}

	cfg := aws.Config{Region: "us-east-1"}
	clientFactory := NewClientFactory(cfg)
	scanner := NewUnifiedScanner(clientFactory)
	scanner.SetServiceRegistry(mockRegistry)

	// Test resource type inference with registry
	t.Run("ResourceTypeInferenceWithRegistry", func(t *testing.T) {
		result := scanner.inferResourceType("s3", "buckets")
		assert.Equal(t, "Bucket", result, "Should infer Bucket type from registry")
	})
}

// Mock service registry for testing
type mockServiceRegistry struct {
	services map[string]*scanner.ScannerServiceDefinition
}

func (m *mockServiceRegistry) GetService(name string) (*scanner.ScannerServiceDefinition, bool) {
	service, exists := m.services[name]
	return service, exists
}

func (m *mockServiceRegistry) ListServices() []string {
	var services []string
	for name := range m.services {
		services = append(services, name)
	}
	return services
}
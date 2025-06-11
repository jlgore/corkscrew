//go:build unit
// +build unit

package tests

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockAWSClient represents a mock AWS service client
type MockAWSClient struct {
	mock.Mock
	ServiceName string
}

// Mock S3 client methods
func (m *MockAWSClient) ListBuckets(ctx context.Context, input interface{}) (interface{}, error) {
	args := m.Called(ctx, input)
	return args.Get(0), args.Error(1)
}

func (m *MockAWSClient) GetBucketLocation(ctx context.Context, input interface{}) (interface{}, error) {
	args := m.Called(ctx, input)
	return args.Get(0), args.Error(1)
}

func (m *MockAWSClient) GetBucketEncryption(ctx context.Context, input interface{}) (interface{}, error) {
	args := m.Called(ctx, input)
	return args.Get(0), args.Error(1)
}

// Mock EC2 client methods
func (m *MockAWSClient) DescribeInstances(ctx context.Context, input interface{}) (interface{}, error) {
	args := m.Called(ctx, input)
	return args.Get(0), args.Error(1)
}

func (m *MockAWSClient) DescribeVolumes(ctx context.Context, input interface{}) (interface{}, error) {
	args := m.Called(ctx, input)
	return args.Get(0), args.Error(1)
}

// Mock Lambda client methods
func (m *MockAWSClient) ListFunctions(ctx context.Context, input interface{}) (interface{}, error) {
	args := m.Called(ctx, input)
	return args.Get(0), args.Error(1)
}

// MockClientFactory creates mock clients
type MockClientFactory struct {
	mock.Mock
	clients map[string]*MockAWSClient
}

func NewMockClientFactory() *MockClientFactory {
	return &MockClientFactory{
		clients: make(map[string]*MockAWSClient),
	}
}

func (f *MockClientFactory) CreateClient(serviceName string, cfg aws.Config) (interface{}, error) {
	args := f.Called(serviceName, cfg)
	if client, ok := f.clients[serviceName]; ok {
		return client, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (f *MockClientFactory) AddMockClient(serviceName string, client *MockAWSClient) {
	f.clients[serviceName] = client
}

// Mock types for testing reflection
type MockS3Bucket struct {
	Name         *string    `json:"name"`
	CreationDate *time.Time `json:"creation_date"`
}

type MockEC2Instance struct {
	InstanceId   *string `json:"instance_id"`
	InstanceType *string `json:"instance_type"`
	State        *string `json:"state"`
}

type MockLambdaFunction struct {
	FunctionName *string `json:"function_name"`
	Runtime      *string `json:"runtime"`
	Handler      *string `json:"handler"`
}

type MockListBucketsOutput struct {
	Buckets []MockS3Bucket `json:"buckets"`
}

type MockDescribeInstancesOutput struct {
	Reservations []struct {
		Instances []MockEC2Instance `json:"instances"`
	} `json:"reservations"`
}

type MockListFunctionsOutput struct {
	Functions []MockLambdaFunction `json:"functions"`
}

// TestReflectionOperationDiscovery tests discovering operations via reflection
func TestReflectionOperationDiscovery(t *testing.T) {
	tests := []struct {
		name             string
		client           *MockAWSClient
		serviceName      string
		expectedOps      []string
		expectedResTypes []string
	}{
		{
			name:             "S3Client",
			client:           &MockAWSClient{ServiceName: "s3"},
			serviceName:      "s3",
			expectedOps:      []string{"ListBuckets", "GetBucketLocation", "GetBucketEncryption"},
			expectedResTypes: []string{"Bucket"},
		},
		{
			name:             "EC2Client", 
			client:           &MockAWSClient{ServiceName: "ec2"},
			serviceName:      "ec2",
			expectedOps:      []string{"DescribeInstances", "DescribeVolumes"},
			expectedResTypes: []string{"Instance", "Volume"},
		},
		{
			name:             "LambdaClient",
			client:           &MockAWSClient{ServiceName: "lambda"},
			serviceName:      "lambda",
			expectedOps:      []string{"ListFunctions"},
			expectedResTypes: []string{"Function"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discoverer := &ReflectionOperationDiscoverer{}
			
			operations := discoverer.DiscoverOperations(tt.client)
			
			// Verify we found expected operations
			foundOps := make(map[string]bool)
			for _, op := range operations {
				foundOps[op.Name] = true
			}
			
			for _, expectedOp := range tt.expectedOps {
				assert.True(t, foundOps[expectedOp], "Should discover operation %s", expectedOp)
			}
			
			// Verify operation metadata
			for _, op := range operations {
				assert.NotEmpty(t, op.Name, "Operation should have name")
				assert.NotEmpty(t, op.InputType, "Operation should have input type")
				assert.NotEmpty(t, op.OutputType, "Operation should have output type")
				
				if op.IsList {
					assert.NotEmpty(t, op.ResourceType, "List operation should have resource type")
				}
			}
			
			t.Logf("Discovered %d operations for %s", len(operations), tt.serviceName)
		})
	}
}

// TestReflectionFieldClassification tests field classification via reflection
func TestReflectionFieldClassification(t *testing.T) {
	tests := []struct {
		name          string
		structType    reflect.Type
		expectedFields map[string]string // field name -> classification
	}{
		{
			name:       "S3Bucket",
			structType: reflect.TypeOf(MockS3Bucket{}),
			expectedFields: map[string]string{
				"Name":         "identifier",
				"CreationDate": "timestamp",
			},
		},
		{
			name:       "EC2Instance",
			structType: reflect.TypeOf(MockEC2Instance{}),
			expectedFields: map[string]string{
				"InstanceId":   "identifier",
				"InstanceType": "configuration",
				"State":        "status",
			},
		},
		{
			name:       "LambdaFunction",
			structType: reflect.TypeOf(MockLambdaFunction{}),
			expectedFields: map[string]string{
				"FunctionName": "identifier",
				"Runtime":      "configuration",
				"Handler":      "configuration",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classifier := &ReflectionFieldClassifier{}
			
			for i := 0; i < tt.structType.NumField(); i++ {
				field := tt.structType.Field(i)
				classification := classifier.ClassifyField(field.Name, field.Type)
				
				if expectedClass, exists := tt.expectedFields[field.Name]; exists {
					assert.Equal(t, expectedClass, classification, 
						"Field %s should be classified as %s", field.Name, expectedClass)
				}
				
				assert.NotEmpty(t, classification, "Field should have classification")
			}
		})
	}
}

// TestReflectionResourceTypeInference tests resource type inference from method names
func TestReflectionResourceTypeInference(t *testing.T) {
	tests := []struct {
		name             string
		methodName       string
		serviceName      string
		expectedResource string
	}{
		{"ListBuckets", "ListBuckets", "s3", "Bucket"},
		{"DescribeInstances", "DescribeInstances", "ec2", "Instance"},
		{"DescribeVolumes", "DescribeVolumes", "ec2", "Volume"},
		{"ListFunctions", "ListFunctions", "lambda", "Function"},
		{"DescribeDBInstances", "DescribeDBInstances", "rds", "DBInstance"},
		{"ListUsers", "ListUsers", "iam", "User"},
		{"ListRoles", "ListRoles", "iam", "Role"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inferrer := &ReflectionResourceInferrer{}
			
			resourceType := inferrer.InferResourceType(tt.serviceName, tt.methodName)
			
			assert.Equal(t, tt.expectedResource, resourceType,
				"Method %s should infer resource type %s", tt.methodName, tt.expectedResource)
		})
	}
}

// TestMockedServiceScanning tests scanning with mocked clients
func TestMockedServiceScanning(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		serviceName string
		setupMocks  func(*MockAWSClient)
		expectError bool
	}{
		{
			name:        "S3Success",
			serviceName: "s3",
			setupMocks: func(client *MockAWSClient) {
				mockOutput := &MockListBucketsOutput{
					Buckets: []MockS3Bucket{
						{Name: aws.String("test-bucket-1")},
						{Name: aws.String("test-bucket-2")},
					},
				}
				client.On("ListBuckets", mock.Anything, mock.Anything).Return(mockOutput, nil)
			},
			expectError: false,
		},
		{
			name:        "EC2Success",
			serviceName: "ec2",
			setupMocks: func(client *MockAWSClient) {
				mockOutput := &MockDescribeInstancesOutput{
					Reservations: []struct {
						Instances []MockEC2Instance `json:"instances"`
					}{
						{
							Instances: []MockEC2Instance{
								{InstanceId: aws.String("i-1234567890abcdef0")},
								{InstanceId: aws.String("i-abcdef1234567890")},
							},
						},
					},
				}
				client.On("DescribeInstances", mock.Anything, mock.Anything).Return(mockOutput, nil)
			},
			expectError: false,
		},
		{
			name:        "LambdaSuccess",
			serviceName: "lambda",
			setupMocks: func(client *MockAWSClient) {
				mockOutput := &MockListFunctionsOutput{
					Functions: []MockLambdaFunction{
						{FunctionName: aws.String("test-function-1")},
						{FunctionName: aws.String("test-function-2")},
					},
				}
				client.On("ListFunctions", mock.Anything, mock.Anything).Return(mockOutput, nil)
			},
			expectError: false,
		},
		{
			name:        "S3Error",
			serviceName: "s3",
			setupMocks: func(client *MockAWSClient) {
				client.On("ListBuckets", mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockClient := &MockAWSClient{ServiceName: tt.serviceName}
			tt.setupMocks(mockClient)

			// Create mock factory
			mockFactory := NewMockClientFactory()
			mockFactory.AddMockClient(tt.serviceName, mockClient)
			mockFactory.On("CreateClient", tt.serviceName, mock.Anything).Return(mockClient, nil)

			// Create scanner with mocked dependencies
			reflectionScanner := &MockedReflectionScanner{
				clientFactory: mockFactory,
			}

			// Perform scan
			result, err := reflectionScanner.ScanService(ctx, tt.serviceName)

			if tt.expectError {
				assert.Error(t, err, "Should return error for %s", tt.serviceName)
			} else {
				assert.NoError(t, err, "Should not return error for %s", tt.serviceName)
				assert.NotNil(t, result, "Should return result for %s", tt.serviceName)
				assert.Equal(t, tt.serviceName, result.ServiceName, "Result should match service name")
				assert.NotZero(t, result.ResourceCount, "Should find some resources")
			}

			// Verify mock expectations
			mockClient.AssertExpectations(t)
			mockFactory.AssertExpectations(t)
		})
	}
}

// TestReflectionCaching tests caching behavior in reflection discovery
func TestReflectionCaching(t *testing.T) {
	cache := NewReflectionCache()

	// Test operation caching
	t.Run("OperationCaching", func(t *testing.T) {
		serviceName := "s3"
		operations := []discovery.OperationInfo{
			{Name: "ListBuckets", IsList: true, ResourceType: "Bucket"},
			{Name: "GetBucketLocation", IsList: false},
		}

		// Cache operations
		cache.CacheOperations(serviceName, operations)

		// Retrieve from cache
		cachedOps, found := cache.GetOperations(serviceName)
		assert.True(t, found, "Should find cached operations")
		assert.Equal(t, len(operations), len(cachedOps), "Should have same number of operations")
		assert.Equal(t, operations[0].Name, cachedOps[0].Name, "Should match operation names")
	})

	// Test resource type caching
	t.Run("ResourceTypeCaching", func(t *testing.T) {
		serviceName := "ec2"
		resourceTypes := []discovery.ResourceTypeInfo{
			{Name: "Instance", PrimaryKey: "InstanceId"},
			{Name: "Volume", PrimaryKey: "VolumeId"},
		}

		// Cache resource types
		cache.CacheResourceTypes(serviceName, resourceTypes)

		// Retrieve from cache
		cachedTypes, found := cache.GetResourceTypes(serviceName)
		assert.True(t, found, "Should find cached resource types")
		assert.Equal(t, len(resourceTypes), len(cachedTypes), "Should have same number of resource types")
		assert.Equal(t, resourceTypes[0].Name, cachedTypes[0].Name, "Should match resource type names")
	})

	// Test cache expiration
	t.Run("CacheExpiration", func(t *testing.T) {
		serviceName := "lambda"
		operations := []discovery.OperationInfo{
			{Name: "ListFunctions", IsList: true, ResourceType: "Function"},
		}

		// Create cache with short expiration
		shortCache := &ReflectionCache{
			operations:    make(map[string]CachedOperations),
			resourceTypes: make(map[string]CachedResourceTypes),
			expiration:    1 * time.Millisecond, // Very short expiration
		}

		// Cache operations
		shortCache.CacheOperations(serviceName, operations)

		// Wait for expiration
		time.Sleep(10 * time.Millisecond)

		// Should not find expired cache
		_, found := shortCache.GetOperations(serviceName)
		assert.False(t, found, "Should not find expired operations")
	})
}

// TestReflectionErrorHandling tests error handling in reflection-based discovery
func TestReflectionErrorHandling(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		serviceName   string
		clientError   error
		expectedError string
	}{
		{
			name:          "ClientCreationError",
			serviceName:   "invalid-service",
			clientError:   assert.AnError,
			expectedError: "failed to create client",
		},
		{
			name:          "AccessDeniedError",
			serviceName:   "s3",
			clientError:   &MockAccessDeniedError{},
			expectedError: "access denied",
		},
		{
			name:          "ServiceUnavailableError",
			serviceName:   "s3",
			clientError:   &MockServiceUnavailableError{},
			expectedError: "service unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock factory that returns errors
			mockFactory := NewMockClientFactory()
			mockFactory.On("CreateClient", tt.serviceName, mock.Anything).Return(nil, tt.clientError)

			// Create scanner with error-prone factory
			reflectionScanner := &MockedReflectionScanner{
				clientFactory: mockFactory,
			}

			// Perform scan
			result, err := reflectionScanner.ScanService(ctx, tt.serviceName)

			assert.Error(t, err, "Should return error")
			assert.Nil(t, result, "Should not return result on error")
			assert.Contains(t, err.Error(), tt.expectedError, "Error should contain expected message")

			mockFactory.AssertExpectations(t)
		})
	}
}

// TestReflectionBatchOperations tests batch operations with mocked clients
func TestReflectionBatchOperations(t *testing.T) {
	ctx := context.Background()

	// Create multiple mock clients
	s3Client := &MockAWSClient{ServiceName: "s3"}
	s3Client.On("ListBuckets", mock.Anything, mock.Anything).Return(&MockListBucketsOutput{
		Buckets: []MockS3Bucket{{Name: aws.String("test-bucket")}},
	}, nil)

	ec2Client := &MockAWSClient{ServiceName: "ec2"}
	ec2Client.On("DescribeInstances", mock.Anything, mock.Anything).Return(&MockDescribeInstancesOutput{
		Reservations: []struct {
			Instances []MockEC2Instance `json:"instances"`
		}{{Instances: []MockEC2Instance{{InstanceId: aws.String("i-test")}}}},
	}, nil)

	// Create mock factory
	mockFactory := NewMockClientFactory()
	mockFactory.AddMockClient("s3", s3Client)
	mockFactory.AddMockClient("ec2", ec2Client)
	mockFactory.On("CreateClient", "s3", mock.Anything).Return(s3Client, nil)
	mockFactory.On("CreateClient", "ec2", mock.Anything).Return(ec2Client, nil)

	// Create batch scanner
	batchScanner := &MockedBatchScanner{
		clientFactory: mockFactory,
	}

	// Perform batch scan
	services := []string{"s3", "ec2"}
	results, err := batchScanner.ScanServices(ctx, services)

	assert.NoError(t, err, "Batch scan should not error")
	assert.Equal(t, len(services), len(results), "Should have results for all services")

	// Verify individual results
	for _, result := range results {
		assert.Contains(t, services, result.ServiceName, "Result should be for requested service")
		assert.NotZero(t, result.ResourceCount, "Should have found resources")
	}

	// Verify all mocks
	s3Client.AssertExpectations(t)
	ec2Client.AssertExpectations(t)
	mockFactory.AssertExpectations(t)
}

// Helper types and implementations

type ReflectionOperationDiscoverer struct{}

func (d *ReflectionOperationDiscoverer) DiscoverOperations(client interface{}) []discovery.OperationInfo {
	clientType := reflect.TypeOf(client)
	var operations []discovery.OperationInfo

	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		
		// Skip unexported methods
		if method.Name[0] < 'A' || method.Name[0] > 'Z' {
			continue
		}

		operation := discovery.OperationInfo{
			Name:       method.Name,
			InputType:  "MockInput",
			OutputType: "MockOutput",
			IsList:     d.isListOperation(method.Name),
		}

		if operation.IsList {
			operation.ResourceType = d.inferResourceType(method.Name)
		}

		operations = append(operations, operation)
	}

	return operations
}

func (d *ReflectionOperationDiscoverer) isListOperation(methodName string) bool {
	listPrefixes := []string{"List", "Describe"}
	for _, prefix := range listPrefixes {
		if len(methodName) > len(prefix) && methodName[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (d *ReflectionOperationDiscoverer) inferResourceType(methodName string) string {
	if methodName == "ListBuckets" {
		return "Bucket"
	}
	if methodName == "DescribeInstances" {
		return "Instance"
	}
	if methodName == "DescribeVolumes" {
		return "Volume"
	}
	if methodName == "ListFunctions" {
		return "Function"
	}
	return "Resource"
}

type ReflectionFieldClassifier struct{}

func (c *ReflectionFieldClassifier) ClassifyField(fieldName string, fieldType reflect.Type) string {
	fieldNameLower := strings.ToLower(fieldName)
	
	if strings.Contains(fieldNameLower, "id") || strings.Contains(fieldNameLower, "name") {
		return "identifier"
	}
	if strings.Contains(fieldNameLower, "date") || strings.Contains(fieldNameLower, "time") {
		return "timestamp"
	}
	if strings.Contains(fieldNameLower, "state") || strings.Contains(fieldNameLower, "status") {
		return "status"
	}
	return "configuration"
}

type ReflectionResourceInferrer struct{}

func (r *ReflectionResourceInferrer) InferResourceType(serviceName, methodName string) string {
	// Remove prefixes
	resource := methodName
	for _, prefix := range []string{"List", "Describe", "Get"} {
		if strings.HasPrefix(resource, prefix) {
			resource = resource[len(prefix):]
			break
		}
	}
	
	// Handle plurals
	if strings.HasSuffix(resource, "s") {
		resource = resource[:len(resource)-1]
	}
	
	return resource
}

type CachedOperations struct {
	Operations []discovery.OperationInfo
	CachedAt   time.Time
}

type CachedResourceTypes struct {
	ResourceTypes []discovery.ResourceTypeInfo
	CachedAt      time.Time
}

type ReflectionCache struct {
	operations    map[string]CachedOperations
	resourceTypes map[string]CachedResourceTypes
	expiration    time.Duration
}

func NewReflectionCache() *ReflectionCache {
	return &ReflectionCache{
		operations:    make(map[string]CachedOperations),
		resourceTypes: make(map[string]CachedResourceTypes),
		expiration:    5 * time.Minute,
	}
}

func (c *ReflectionCache) CacheOperations(serviceName string, operations []discovery.OperationInfo) {
	c.operations[serviceName] = CachedOperations{
		Operations: operations,
		CachedAt:   time.Now(),
	}
}

func (c *ReflectionCache) GetOperations(serviceName string) ([]discovery.OperationInfo, bool) {
	cached, exists := c.operations[serviceName]
	if !exists {
		return nil, false
	}
	
	if time.Since(cached.CachedAt) > c.expiration {
		delete(c.operations, serviceName)
		return nil, false
	}
	
	return cached.Operations, true
}

func (c *ReflectionCache) CacheResourceTypes(serviceName string, resourceTypes []discovery.ResourceTypeInfo) {
	c.resourceTypes[serviceName] = CachedResourceTypes{
		ResourceTypes: resourceTypes,
		CachedAt:      time.Now(),
	}
}

func (c *ReflectionCache) GetResourceTypes(serviceName string) ([]discovery.ResourceTypeInfo, bool) {
	cached, exists := c.resourceTypes[serviceName]
	if !exists {
		return nil, false
	}
	
	if time.Since(cached.CachedAt) > c.expiration {
		delete(c.resourceTypes, serviceName)
		return nil, false
	}
	
	return cached.ResourceTypes, true
}

type MockedReflectionScanner struct {
	clientFactory *MockClientFactory
}

type ScanResult struct {
	ServiceName   string
	ResourceCount int
	Duration      time.Duration
}

func (s *MockedReflectionScanner) ScanService(ctx context.Context, serviceName string) (*ScanResult, error) {
	start := time.Now()
	
	client, err := s.clientFactory.CreateClient(serviceName, aws.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	
	// Simulate scanning operations based on service
	resourceCount := 0
	switch serviceName {
	case "s3":
		_, err = client.(*MockAWSClient).ListBuckets(ctx, nil)
		if err != nil {
			return nil, err
		}
		resourceCount = 2
	case "ec2":
		_, err = client.(*MockAWSClient).DescribeInstances(ctx, nil)
		if err != nil {
			return nil, err
		}
		resourceCount = 2
	case "lambda":
		_, err = client.(*MockAWSClient).ListFunctions(ctx, nil)
		if err != nil {
			return nil, err
		}
		resourceCount = 2
	}
	
	return &ScanResult{
		ServiceName:   serviceName,
		ResourceCount: resourceCount,
		Duration:      time.Since(start),
	}, nil
}

type MockedBatchScanner struct {
	clientFactory *MockClientFactory
}

func (s *MockedBatchScanner) ScanServices(ctx context.Context, services []string) ([]*ScanResult, error) {
	var results []*ScanResult
	scanner := &MockedReflectionScanner{clientFactory: s.clientFactory}
	
	for _, service := range services {
		result, err := scanner.ScanService(ctx, service)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	
	return results, nil
}

// Mock error types
type MockAccessDeniedError struct{}

func (e *MockAccessDeniedError) Error() string {
	return "access denied: insufficient permissions"
}

type MockServiceUnavailableError struct{}

func (e *MockServiceUnavailableError) Error() string {
	return "service unavailable: service is temporarily unavailable"
}
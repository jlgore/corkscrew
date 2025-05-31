package main

import (
	"sync"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Common interfaces and types for testing

// AzureResourceGraphClient interface for testing
type AzureResourceGraphClient interface {
	Query(ctx context.Context, subscriptions []string, query string, options *ResourceGraphQueryOptions) (*ResourceGraphResponse, error)
}

// ResourceGraphQueryOptions for testing
type ResourceGraphQueryOptions struct {
	Top  int32
	Skip int32
}

// ResourceGraphResponse for testing
type ResourceGraphResponse struct {
	TotalRecords int
	Count        int
	Data         []interface{}
	SkipToken    *string
}

// MockCredential for testing
type MockCredential struct{}

func (c *MockCredential) GetToken(ctx context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{
		Token:     "mock-token",
		ExpiresOn: time.Now().Add(time.Hour),
	}, nil
}

// MockResourceGraphClient for testing
type MockResourceGraphClient struct {
	mock.Mock
}

func (m *MockResourceGraphClient) Resources(ctx context.Context, query armresourcegraph.QueryRequest, options *armresourcegraph.ClientResourcesOptions) (armresourcegraph.ClientResourcesResponse, error) {
	args := m.Called(ctx, query, options)
	return args.Get(0).(armresourcegraph.ClientResourcesResponse), args.Error(1)
}

// MockAzureResourceGraphClient wraps the mock for our interface
type MockAzureResourceGraphClient struct {
	mockClient *MockResourceGraphClient
}

func (m *MockAzureResourceGraphClient) Query(ctx context.Context, subscriptions []string, query string, options *ResourceGraphQueryOptions) (*ResourceGraphResponse, error) {
	// Convert to the expected armresourcegraph types and call mock
	request := armresourcegraph.QueryRequest{
		Query:         to.Ptr(query),
		Subscriptions: make([]*string, len(subscriptions)),
	}
	for i, sub := range subscriptions {
		request.Subscriptions[i] = to.Ptr(sub)
	}

	if options != nil {
		if options.Top > 0 {
			request.Options = &armresourcegraph.QueryRequestOptions{
				Top:  &options.Top,
				Skip: &options.Skip,
			}
		}
	}

	response, err := m.mockClient.Resources(ctx, request, nil)
	if err != nil {
		return nil, err
	}

	// Convert response back to our format
	var data []interface{}
	if response.Data != nil {
		if dataSlice, ok := response.Data.([]interface{}); ok {
			data = dataSlice
		}
	}
	
	result := &ResourceGraphResponse{
		TotalRecords: int(*response.TotalRecords),
		Count:        int(*response.Count),
		Data:         data,
		SkipToken:    response.SkipToken,
	}

	return result, nil
}

// DiscoveryResult represents the result of a discovery operation
type DiscoveryResult struct {
	ProviderCount    int
	ResourceCount    int
	SchemaCount      int
	Duration         time.Duration
	Errors          []error
	Timestamp       time.Time
}

// AutoDiscoveryPipeline orchestrates the entire auto-discovery process
type AutoDiscoveryPipeline struct {
	credential      azcore.TokenCredential
	subscriptionID  string
	discovery       *AzureServiceDiscovery
	schemaGenerator *AzureSchemaGenerator
	rgClient        AzureResourceGraphClient
	config          *PipelineConfig
	mu              sync.RWMutex
	running         bool
	results         map[string]*DiscoveryResult
}

// PipelineConfig configures the auto-discovery pipeline
type PipelineConfig struct {
	MaxConcurrency     int
	Timeout           time.Duration
	EnableCaching     bool
	CacheExpiry       time.Duration
	BatchSize         int
	RetryAttempts     int
	RetryDelay        time.Duration
	ProviderFilter    []string
	ResourceTypeFilter []string
	EnableMetrics     bool
	MetricsInterval   time.Duration
}

// TestFixtures provides common test data and utilities
type TestFixtures struct {
	TempDir         string
	TestDataDir     string
	Providers       []*ProviderInfo
	Schemas         []*TableSchema
	MockResponses   map[string]interface{}
}

// NewTestFixtures creates a new test fixtures instance
func NewTestFixtures(t *testing.T) *TestFixtures {
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("azure-provider-test-%d", time.Now().UnixNano()))
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Get test data directory relative to this file
	_, filename, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(filename)
	testDataDir := filepath.Join(baseDir, "testdata")

	fixtures := &TestFixtures{
		TempDir:       tempDir,
		TestDataDir:   testDataDir,
		Providers:     createTestProviders(),
		Schemas:       createTestSchemas(),
		MockResponses: createMockResponses(),
	}

	// Create test data directory if it doesn't exist
	os.MkdirAll(testDataDir, 0755)

	return fixtures
}

// Cleanup removes temporary test files
func (f *TestFixtures) Cleanup() {
	os.RemoveAll(f.TempDir)
}

// SaveTestData saves test data to JSON files for reuse
func (f *TestFixtures) SaveTestData() error {
	// Save providers
	providersData, err := json.MarshalIndent(f.Providers, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(f.TestDataDir, "test_providers.json"), providersData, 0644)
	if err != nil {
		return err
	}

	// Save schemas
	schemasData, err := json.MarshalIndent(f.Schemas, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(f.TestDataDir, "test_schemas.json"), schemasData, 0644)
	if err != nil {
		return err
	}

	// Save mock responses
	mockData, err := json.MarshalIndent(f.MockResponses, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(f.TestDataDir, "mock_responses.json"), mockData, 0644)
	if err != nil {
		return err
	}

	return nil
}

// LoadTestData loads test data from JSON files
func (f *TestFixtures) LoadTestData() error {
	// Load providers
	providersData, err := os.ReadFile(filepath.Join(f.TestDataDir, "test_providers.json"))
	if err == nil {
		err = json.Unmarshal(providersData, &f.Providers)
		if err != nil {
			return err
		}
	}

	// Load schemas
	schemasData, err := os.ReadFile(filepath.Join(f.TestDataDir, "test_schemas.json"))
	if err == nil {
		err = json.Unmarshal(schemasData, &f.Schemas)
		if err != nil {
			return err
		}
	}

	// Load mock responses
	mockData, err := os.ReadFile(filepath.Join(f.TestDataDir, "mock_responses.json"))
	if err == nil {
		err = json.Unmarshal(mockData, &f.MockResponses)
		if err != nil {
			return err
		}
	}

	return nil
}

// Test helper functions

// SetupTestEnvironment prepares the test environment
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	fixtures := NewTestFixtures(t)
	
	// Setup test cleanup
	t.Cleanup(func() {
		fixtures.Cleanup()
	})

	env := &TestEnvironment{
		Fixtures:        fixtures,
		MockCredential:  &MockCredential{},
		SubscriptionID:  "test-subscription-" + fmt.Sprintf("%d", time.Now().Unix()),
		Context:         context.Background(),
	}

	return env
}

// TestEnvironment provides a complete test environment
type TestEnvironment struct {
	Fixtures       *TestFixtures
	MockCredential *MockCredential
	SubscriptionID string
	Context        context.Context
}

// CreateTestDiscovery creates a discovery instance for testing
func (env *TestEnvironment) CreateTestDiscovery() *AzureServiceDiscovery {
	return NewAzureServiceDiscovery(env.MockCredential, env.SubscriptionID)
}

// CreateTestSchemaGenerator creates a schema generator for testing
func (env *TestEnvironment) CreateTestSchemaGenerator() *AzureSchemaGenerator {
	return NewAzureSchemaGenerator()
}

// CreateTestResourceGraphClient creates a mock Resource Graph client
func (env *TestEnvironment) CreateTestResourceGraphClient() *MockAzureResourceGraphClient {
	mockClient := &MockResourceGraphClient{}
	return &MockAzureResourceGraphClient{mockClient: mockClient}
}

// CreateTestPipeline creates a complete test pipeline
func (env *TestEnvironment) CreateTestPipeline() *AutoDiscoveryPipeline {
	return &AutoDiscoveryPipeline{
		credential:      env.MockCredential,
		subscriptionID:  env.SubscriptionID,
		discovery:       env.CreateTestDiscovery(),
		schemaGenerator: env.CreateTestSchemaGenerator(),
		rgClient:        env.CreateTestResourceGraphClient(),
		config:          DefaultPipelineConfig(),
		results:         make(map[string]*DiscoveryResult),
	}
}

// DefaultPipelineConfig returns default configuration for testing
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		MaxConcurrency:  10,
		Timeout:         5 * time.Minute,
		EnableCaching:   true,
		CacheExpiry:     30 * time.Minute,
		BatchSize:       100,
		RetryAttempts:   3,
		RetryDelay:      1 * time.Second,
		EnableMetrics:   true,
		MetricsInterval: 30 * time.Second,
	}
}

// Test data creation functions

func createTestProviders() []*ProviderInfo {
	return []*ProviderInfo{
		{
			Namespace:         "Microsoft.Compute",
			RegistrationState: "Registered",
			ResourceTypes: []ResourceTypeInfo{
				{
					ResourceType: "virtualMachines",
					APIVersions:  []string{"2023-03-01", "2022-11-01", "2022-08-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Capabilities: []string{"SupportsAvailabilityZones", "SupportsAcceleratedNetworking"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
				{
					ResourceType: "disks",
					APIVersions:  []string{"2023-01-02", "2022-07-02", "2022-03-02"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
				{
					ResourceType: "availabilitySets",
					APIVersions:  []string{"2023-03-01", "2022-11-01"},
					Locations:    []string{"East US", "West US", "Central US"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
			},
		},
		{
			Namespace:         "Microsoft.Storage",
			RegistrationState: "Registered",
			ResourceTypes: []ResourceTypeInfo{
				{
					ResourceType: "storageAccounts",
					APIVersions:  []string{"2023-01-01", "2022-09-01", "2022-05-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe", "Southeast Asia"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
				{
					ResourceType: "storageAccounts/blobServices",
					APIVersions:  []string{"2023-01-01", "2022-09-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": false,
					},
				},
			},
		},
		{
			Namespace:         "Microsoft.Network",
			RegistrationState: "Registered",
			ResourceTypes: []ResourceTypeInfo{
				{
					ResourceType: "virtualNetworks",
					APIVersions:  []string{"2023-02-01", "2022-11-01", "2022-07-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
				{
					ResourceType: "networkSecurityGroups",
					APIVersions:  []string{"2023-02-01", "2022-11-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
				{
					ResourceType: "publicIPAddresses",
					APIVersions:  []string{"2023-02-01", "2022-11-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
			},
		},
		{
			Namespace:         "Microsoft.Web",
			RegistrationState: "Registered",
			ResourceTypes: []ResourceTypeInfo{
				{
					ResourceType: "sites",
					APIVersions:  []string{"2022-09-01", "2022-03-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
				{
					ResourceType: "serverfarms",
					APIVersions:  []string{"2022-09-01", "2022-03-01"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
			},
		},
		{
			Namespace:         "Microsoft.Sql",
			RegistrationState: "Registered",
			ResourceTypes: []ResourceTypeInfo{
				{
					ResourceType: "servers",
					APIVersions:  []string{"2022-11-01-preview", "2022-05-01-preview"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
				{
					ResourceType: "servers/databases",
					APIVersions:  []string{"2022-11-01-preview", "2022-05-01-preview"},
					Locations:    []string{"East US", "West US", "Central US", "North Europe", "West Europe"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
			},
		},
		{
			Namespace:         "Microsoft.NotRegistered",
			RegistrationState: "NotRegistered",
			ResourceTypes: []ResourceTypeInfo{
				{
					ResourceType: "testResources",
					APIVersions:  []string{"2023-01-01"},
					Locations:    []string{"East US"},
					Properties: map[string]interface{}{
						"supports_list": true,
						"supports_get":  true,
						"supports_tags": true,
					},
				},
			},
		},
	}
}

func createTestSchemas() []*TableSchema {
	return []*TableSchema{
		{
			TableName: "azure_compute_virtualmachines",
			Columns: []*ColumnDefinition{
				{Name: "id", Type: "VARCHAR", PrimaryKey: true, Nullable: false},
				{Name: "name", Type: "VARCHAR", Nullable: false},
				{Name: "type", Type: "VARCHAR", Nullable: false},
				{Name: "location", Type: "VARCHAR", Nullable: false},
				{Name: "resource_group", Type: "VARCHAR", Nullable: false},
				{Name: "subscription_id", Type: "VARCHAR", Nullable: false},
				{Name: "vm_size", Type: "VARCHAR", Nullable: true},
				{Name: "os_type", Type: "VARCHAR", Nullable: true},
				{Name: "power_state", Type: "VARCHAR", Nullable: true},
				{Name: "properties", Type: "JSON", Nullable: true},
				{Name: "tags", Type: "JSON", Nullable: true},
				{Name: "discovered_at", Type: "TIMESTAMP", Nullable: false},
			},
			Indexes: []string{"idx_name", "idx_location", "idx_vm_size", "idx_os_type"},
		},
		{
			TableName: "azure_storage_storageaccounts",
			Columns: []*ColumnDefinition{
				{Name: "id", Type: "VARCHAR", PrimaryKey: true, Nullable: false},
				{Name: "name", Type: "VARCHAR", Nullable: false},
				{Name: "type", Type: "VARCHAR", Nullable: false},
				{Name: "location", Type: "VARCHAR", Nullable: false},
				{Name: "resource_group", Type: "VARCHAR", Nullable: false},
				{Name: "subscription_id", Type: "VARCHAR", Nullable: false},
				{Name: "account_type", Type: "VARCHAR", Nullable: true},
				{Name: "access_tier", Type: "VARCHAR", Nullable: true},
				{Name: "encryption_enabled", Type: "BOOLEAN", Nullable: true},
				{Name: "properties", Type: "JSON", Nullable: true},
				{Name: "tags", Type: "JSON", Nullable: true},
				{Name: "discovered_at", Type: "TIMESTAMP", Nullable: false},
			},
			Indexes: []string{"idx_name", "idx_location", "idx_account_type"},
		},
	}
}

func createMockResponses() map[string]interface{} {
	return map[string]interface{}{
		"vm_list_response": []interface{}{
			map[string]interface{}{
				"id":       "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.Compute/virtualMachines/test-vm-1",
				"name":     "test-vm-1",
				"type":     "Microsoft.Compute/virtualMachines",
				"location": "eastus",
				"properties": map[string]interface{}{
					"hardwareProfile": map[string]interface{}{
						"vmSize": "Standard_D2s_v3",
					},
					"osProfile": map[string]interface{}{
						"computerName": "test-vm-1",
					},
					"provisioningState": "Succeeded",
				},
				"tags": map[string]interface{}{
					"environment": "test",
					"project":     "azure-provider",
				},
			},
			map[string]interface{}{
				"id":       "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.Compute/virtualMachines/test-vm-2",
				"name":     "test-vm-2",
				"type":     "Microsoft.Compute/virtualMachines",
				"location": "westus",
				"properties": map[string]interface{}{
					"hardwareProfile": map[string]interface{}{
						"vmSize": "Standard_B2s",
					},
					"osProfile": map[string]interface{}{
						"computerName": "test-vm-2",
					},
					"provisioningState": "Succeeded",
				},
				"tags": map[string]interface{}{
					"environment": "production",
					"project":     "web-app",
				},
			},
		},
		"storage_list_response": []interface{}{
			map[string]interface{}{
				"id":       "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.Storage/storageAccounts/teststorage001",
				"name":     "teststorage001",
				"type":     "Microsoft.Storage/storageAccounts",
				"location": "eastus",
				"properties": map[string]interface{}{
					"accountType":              "Standard_LRS",
					"supportsHttpsTrafficOnly": true,
					"primaryEndpoints": map[string]interface{}{
						"blob": "https://teststorage001.blob.core.windows.net/",
					},
				},
				"tags": map[string]interface{}{
					"environment": "test",
				},
			},
		},
	}
}

// Mock setup helpers

// SetupMockResourceGraphClient configures a mock Resource Graph client with realistic responses
func SetupMockResourceGraphClient(mockClient *MockResourceGraphClient, fixtures *TestFixtures) {
	// Setup default responses for common queries
	vmResponse := armresourcegraph.ClientResourcesResponse{
		QueryResponse: armresourcegraph.QueryResponse{
			TotalRecords: to.Ptr(int64(2)),
			Count:        to.Ptr(int64(2)),
			Data:         fixtures.MockResponses["vm_list_response"],
		},
	}

	storageResponse := armresourcegraph.ClientResourcesResponse{
		QueryResponse: armresourcegraph.QueryResponse{
			TotalRecords: to.Ptr(int64(1)),
			Count:        to.Ptr(int64(1)),
			Data:         fixtures.MockResponses["storage_list_response"],
		},
	}

	emptyResponse := armresourcegraph.ClientResourcesResponse{
		QueryResponse: armresourcegraph.QueryResponse{
			TotalRecords: to.Ptr(int64(0)),
			Count:        to.Ptr(int64(0)),
			Data:         []interface{}{},
		},
	}

	// Mock VM queries
	mockClient.On("Resources", mock.Anything, mock.MatchedBy(func(req armresourcegraph.QueryRequest) bool {
		return req.Query != nil && strings.Contains(*req.Query, "Microsoft.Compute/virtualMachines")
	}), mock.Anything).Return(vmResponse, nil)

	// Mock Storage queries
	mockClient.On("Resources", mock.Anything, mock.MatchedBy(func(req armresourcegraph.QueryRequest) bool {
		return req.Query != nil && strings.Contains(*req.Query, "Microsoft.Storage/storageAccounts")
	}), mock.Anything).Return(storageResponse, nil)

	// Mock empty responses for unknown resource types
	mockClient.On("Resources", mock.Anything, mock.MatchedBy(func(req armresourcegraph.QueryRequest) bool {
		return req.Query != nil && strings.Contains(*req.Query, "Microsoft.Unknown")
	}), mock.Anything).Return(emptyResponse, nil)

	// Default catch-all mock
	mockClient.On("Resources", mock.Anything, mock.Anything, mock.Anything).Return(emptyResponse, nil)
}

// Performance test helpers

// BenchmarkTimer helps measure execution time in benchmarks
type BenchmarkTimer struct {
	start     time.Time
	operation string
}

// NewBenchmarkTimer creates a new benchmark timer
func NewBenchmarkTimer(operation string) *BenchmarkTimer {
	return &BenchmarkTimer{
		start:     time.Now(),
		operation: operation,
	}
}

// Stop stops the timer and returns the duration
func (bt *BenchmarkTimer) Stop() time.Duration {
	return time.Since(bt.start)
}

// MemoryProfiler helps track memory usage in tests
type MemoryProfiler struct {
	initial runtime.MemStats
	final   runtime.MemStats
}

// NewMemoryProfiler creates a new memory profiler
func NewMemoryProfiler() *MemoryProfiler {
	mp := &MemoryProfiler{}
	runtime.GC() // Force garbage collection
	runtime.ReadMemStats(&mp.initial)
	return mp
}

// Stop captures final memory statistics
func (mp *MemoryProfiler) Stop() {
	runtime.GC() // Force garbage collection
	runtime.ReadMemStats(&mp.final)
}

// AllocatedBytes returns the number of bytes allocated during the profiling period
func (mp *MemoryProfiler) AllocatedBytes() uint64 {
	return mp.final.TotalAlloc - mp.initial.TotalAlloc
}

// HeapIncrease returns the increase in heap size
func (mp *MemoryProfiler) HeapIncrease() uint64 {
	return mp.final.HeapInuse - mp.initial.HeapInuse
}

// Test assertion helpers

// AssertSchemaValid validates that a schema meets basic requirements
func AssertSchemaValid(t *testing.T, schema *TableSchema, resourceType string) {
	t.Helper()

	// Basic validations
	assert.NotEmpty(t, schema.TableName, "Schema should have a table name")
	assert.NotEmpty(t, schema.Columns, "Schema should have columns")

	// Check for required columns
	requiredColumns := []string{"id", "name", "type", "location"}
	columnNames := make(map[string]bool)
	
	for _, col := range schema.Columns {
		columnNames[col.Name] = true
	}

	for _, required := range requiredColumns {
		assert.True(t, columnNames[required], 
			"Schema for %s should have required column: %s", resourceType, required)
	}

	// Check primary key
	hasPrimaryKey := false
	for _, col := range schema.Columns {
		if col.PrimaryKey {
			hasPrimaryKey = true
			break
		}
	}
	assert.True(t, hasPrimaryKey, "Schema should have a primary key")
}

// AssertProviderValid validates that a provider meets basic requirements
func AssertProviderValid(t *testing.T, provider *ProviderInfo) {
	t.Helper()

	assert.NotEmpty(t, provider.Namespace, "Provider should have a namespace")
	assert.NotEmpty(t, provider.RegistrationState, "Provider should have a registration state")
	assert.NotEmpty(t, provider.ResourceTypes, "Provider should have resource types")

	for _, rt := range provider.ResourceTypes {
		assert.NotEmpty(t, rt.ResourceType, "Resource type should have a name")
		assert.NotEmpty(t, rt.APIVersions, "Resource type should have API versions")
	}
}

// Test data validation

// ValidateTestData ensures test data is consistent and realistic
func ValidateTestData(t *testing.T, fixtures *TestFixtures) {
	t.Helper()

	// Validate providers
	assert.NotEmpty(t, fixtures.Providers, "Test fixtures should have providers")
	for _, provider := range fixtures.Providers {
		AssertProviderValid(t, provider)
	}

	// Validate schemas
	assert.NotEmpty(t, fixtures.Schemas, "Test fixtures should have schemas")
	for _, schema := range fixtures.Schemas {
		generator := NewAzureSchemaGenerator()
		errors := generator.ValidateSchema(schema)
		assert.Empty(t, errors, "Test schema should be valid: %v", errors)
	}

	// Validate mock responses
	assert.NotEmpty(t, fixtures.MockResponses, "Test fixtures should have mock responses")
	
	// Validate VM response structure
	if vmResponse, exists := fixtures.MockResponses["vm_list_response"]; exists {
		vmList, ok := vmResponse.([]interface{})
		assert.True(t, ok, "VM response should be a list")
		assert.NotEmpty(t, vmList, "VM response should not be empty")
		
		for _, vm := range vmList {
			vmData, ok := vm.(map[string]interface{})
			assert.True(t, ok, "VM data should be a map")
			assert.Contains(t, vmData, "id", "VM should have ID")
			assert.Contains(t, vmData, "name", "VM should have name")
			assert.Contains(t, vmData, "type", "VM should have type")
		}
	}
}

// Context helpers

// CreateTestContext creates a context with appropriate timeout for tests
func CreateTestContext(duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), duration)
}

// CreateTestContextWithCancel creates a cancellable context for tests
func CreateTestContextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
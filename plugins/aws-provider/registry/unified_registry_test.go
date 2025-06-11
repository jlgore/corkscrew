package registry

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// Test implementation of UnifiedScannerProvider for testing
type TestScannerProvider struct {
	scanResults    map[string][]*pb.ResourceRef
	resourceData   map[string]*pb.Resource
	scanCallCount  int
	describeCallCount int
}

func NewTestScannerProvider() *TestScannerProvider {
	return &TestScannerProvider{
		scanResults:  make(map[string][]*pb.ResourceRef),
		resourceData: make(map[string]*pb.Resource),
	}
}

func (t *TestScannerProvider) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	t.scanCallCount++
	if refs, exists := t.scanResults[serviceName]; exists {
		return refs, nil
	}
	
	// Return some test data
	return []*pb.ResourceRef{
		{
			Service: serviceName,
			Type:    "TestResource",
			Id:      "test-resource-1",
			Name:    "Test Resource 1",
			Region:  "us-east-1",
		},
	}, nil
}

func (t *TestScannerProvider) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	t.describeCallCount++
	
	if resource, exists := t.resourceData[ref.Id]; exists {
		return resource, nil
	}
	
	// Return test resource
	return &pb.Resource{
		Provider: "aws",
		Service:  ref.Service,
		Type:     ref.Type,
		Id:       ref.Id,
		Name:     ref.Name,
		Region:   ref.Region,
		Tags:     map[string]string{"test": "value"},
	}, nil
}

func (t *TestScannerProvider) GetMetrics() interface{} {
	return map[string]int{
		"scan_calls":     t.scanCallCount,
		"describe_calls": t.describeCallCount,
	}
}

func TestUnifiedServiceRegistry_BasicOperations(t *testing.T) {
	// Setup
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache:  true,
		CacheTTL:     5 * time.Minute,
		MaxCacheSize: 100,
		AutoPersist:  false,
	}

	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)

	// Test service registration
	testService := ServiceDefinition{
		Name:        "s3",
		DisplayName: "Amazon S3",
		Description: "Simple Storage Service",
		PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
		RateLimit:   rate.Limit(100),
		BurstLimit:  200,
		GlobalService: true,
		ResourceTypes: []ResourceTypeDefinition{
			{
				Name:         "Bucket",
				ResourceType: "AWS::S3::Bucket",
				ListOperation: "ListBuckets",
			},
		},
		Operations: []OperationDefinition{
			{
				Name:          "ListBuckets",
				OperationType: "List",
				Paginated:     false,
			},
		},
		DiscoverySource: "test",
	}

	err := registry.RegisterService(testService)
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// Test service retrieval
	retrieved, exists := registry.GetService("s3")
	if !exists {
		t.Fatal("Service not found after registration")
	}

	if retrieved.Name != "s3" {
		t.Errorf("Expected service name 's3', got '%s'", retrieved.Name)
	}

	if retrieved.DisplayName != "Amazon S3" {
		t.Errorf("Expected display name 'Amazon S3', got '%s'", retrieved.DisplayName)
	}

	// Test service listing
	services := registry.ListServices()
	if len(services) == 0 {
		t.Error("Expected at least one service in list")
	}

	found := false
	for _, service := range services {
		if service == "s3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Registered service not found in service list")
	}
}

func TestUnifiedServiceRegistry_ClientCreation(t *testing.T) {
	// Setup
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}

	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)

	// Register a service with client factory
	testService := ServiceDefinition{
		Name:        "test-service",
		DisplayName: "Test Service",
		RateLimit:   rate.Limit(10),
		BurstLimit:  20,
	}

	// Test client factory function
	factoryFunc := func(cfg aws.Config) interface{} {
		return &TestClient{ServiceName: "test-service", Config: cfg}
	}

	err := registry.RegisterServiceWithFactory(testService, factoryFunc)
	if err != nil {
		t.Fatalf("Failed to register service with factory: %v", err)
	}

	// Test client creation
	ctx := context.Background()
	client, err := registry.CreateClient(ctx, "test-service")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	testClient, ok := client.(*TestClient)
	if !ok {
		t.Fatalf("Expected *TestClient, got %T", client)
	}

	if testClient.ServiceName != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", testClient.ServiceName)
	}

	// Test client caching - second call should return cached client
	client2, err := registry.CreateClient(ctx, "test-service")
	if err != nil {
		t.Fatalf("Failed to create client on second call: %v", err)
	}

	// Should be the same instance due to caching
	if client != client2 {
		t.Error("Expected cached client to be returned")
	}
}

func TestUnifiedServiceRegistry_ScannerIntegration(t *testing.T) {
	// Setup
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}

	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)
	
	// Setup test scanner
	testScanner := NewTestScannerProvider()
	registry.SetUnifiedScanner(testScanner)

	// Register a test service
	testService := ServiceDefinition{
		Name:        "ec2",
		DisplayName: "Amazon EC2",
		RateLimit:   rate.Limit(20),
		BurstLimit:  40,
		RequiresRegion: true,
	}

	err := registry.RegisterService(testService)
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// Test service scanning
	ctx := context.Background()
	resources, err := registry.ScanService(ctx, "ec2", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to scan service: %v", err)
	}

	if len(resources) == 0 {
		t.Error("Expected at least one resource from scan")
	}

	// Verify resource structure
	resource := resources[0]
	if resource.Provider != "aws" {
		t.Errorf("Expected provider 'aws', got '%s'", resource.Provider)
	}

	if resource.Service != "ec2" {
		t.Errorf("Expected service 'ec2', got '%s'", resource.Service)
	}

	if resource.Region != "us-east-1" {
		t.Errorf("Expected region 'us-east-1', got '%s'", resource.Region)
	}

	// Verify scanner was called
	if testScanner.scanCallCount == 0 {
		t.Error("Expected scanner ScanService to be called")
	}

	if testScanner.describeCallCount == 0 {
		t.Error("Expected scanner DescribeResource to be called")
	}
}

func TestUnifiedServiceRegistry_RateLimiting(t *testing.T) {
	// Setup
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}

	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)

	// Register service with strict rate limiting
	testService := ServiceDefinition{
		Name:       "rate-limited-service",
		RateLimit:  rate.Limit(1), // 1 request per second
		BurstLimit: 1,             // No burst
	}

	err := registry.RegisterService(testService)
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// Test rate limiter
	limiter := registry.GetRateLimiter("rate-limited-service")
	if limiter == nil {
		t.Fatal("Expected rate limiter to be created")
	}

	// First request should succeed immediately
	ctx := context.Background()
	start := time.Now()
	err = limiter.Wait(ctx)
	if err != nil {
		t.Fatalf("First rate limit wait failed: %v", err)
	}
	firstDuration := time.Since(start)

	// Second request should be rate limited
	start = time.Now()
	err = limiter.Wait(ctx)
	if err != nil {
		t.Fatalf("Second rate limit wait failed: %v", err)
	}
	secondDuration := time.Since(start)

	// Second request should take significantly longer due to rate limiting
	if secondDuration <= firstDuration {
		t.Error("Expected second request to be rate limited")
	}
}

func TestUnifiedServiceRegistry_Statistics(t *testing.T) {
	// Setup
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}

	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)

	// Register multiple services
	services := []ServiceDefinition{
		{Name: "s3", DisplayName: "Amazon S3", DiscoverySource: "manual"},
		{Name: "ec2", DisplayName: "Amazon EC2", DiscoverySource: "reflection"},
		{Name: "lambda", DisplayName: "AWS Lambda", DiscoverySource: "manual"},
	}

	for _, service := range services {
		err := registry.RegisterService(service)
		if err != nil {
			t.Fatalf("Failed to register service %s: %v", service.Name, err)
		}
	}

	// Get statistics
	stats := registry.GetStats()

	if stats.TotalServices != 3 {
		t.Errorf("Expected 3 total services, got %d", stats.TotalServices)
	}

	if stats.ServicesBySource["manual"] != 2 {
		t.Errorf("Expected 2 manual services, got %d", stats.ServicesBySource["manual"])
	}

	if stats.ServicesBySource["reflection"] != 1 {
		t.Errorf("Expected 1 reflection service, got %d", stats.ServicesBySource["reflection"])
	}

	if stats.LastUpdated.IsZero() {
		t.Error("Expected LastUpdated to be set")
	}
}

func TestUnifiedServiceRegistry_Migration(t *testing.T) {
	// Create a mock old registry for testing migration
	oldRegistry := &MockOldRegistry{
		services: make(map[string]ServiceDefinition),
	}

	// Add services to old registry
	oldServices := []ServiceDefinition{
		{
			Name:        "dynamodb",
			DisplayName: "Amazon DynamoDB",
			Description: "NoSQL Database Service",
			RateLimit:   rate.Limit(25),
			BurstLimit:  50,
		},
		{
			Name:        "kms",
			DisplayName: "AWS Key Management Service",
			Description: "Managed encryption service",
			RateLimit:   rate.Limit(10),
			BurstLimit:  20,
		},
	}

	for _, service := range oldServices {
		err := oldRegistry.RegisterService(service)
		if err != nil {
			t.Fatalf("Failed to register service in old registry: %v", err)
		}
	}

	// Create new unified registry
	awsConfig := aws.Config{Region: "us-east-1"}
	newRegistryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}
	unifiedRegistry := NewUnifiedServiceRegistry(awsConfig, newRegistryConfig)

	// Test migration
	err := unifiedRegistry.MigrateFromDynamicRegistry(oldRegistry)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify services were migrated
	services := unifiedRegistry.ListServices()
	if len(services) != 2 {
		t.Errorf("Expected 2 migrated services, got %d", len(services))
	}

	// Check specific service
	service, exists := unifiedRegistry.GetService("dynamodb")
	if !exists {
		t.Error("DynamoDB service not found after migration")
	}

	if service.DisplayName != "Amazon DynamoDB" {
		t.Errorf("Expected display name to be preserved, got '%s'", service.DisplayName)
	}

	if service.RateLimit != rate.Limit(25) {
		t.Errorf("Expected rate limit to be preserved, got %v", service.RateLimit)
	}
}

// Helper types for testing

type TestClient struct {
	ServiceName string
	Config      aws.Config
}

// Benchmark tests

func BenchmarkUnifiedServiceRegistry_CreateClient(b *testing.B) {
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}

	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)

	// Register test service with factory
	testService := ServiceDefinition{
		Name:        "benchmark-service",
		DisplayName: "Benchmark Service",
		RateLimit:   rate.Limit(100),
		BurstLimit:  200,
	}

	factoryFunc := func(cfg aws.Config) interface{} {
		return &TestClient{ServiceName: "benchmark-service", Config: cfg}
	}

	registry.RegisterServiceWithFactory(testService, factoryFunc)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := registry.CreateClient(ctx, "benchmark-service")
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}
	}
}

func BenchmarkUnifiedServiceRegistry_GetService(b *testing.B) {
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}

	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)

	// Register test service
	testService := ServiceDefinition{
		Name:        "benchmark-service",
		DisplayName: "Benchmark Service",
		RateLimit:   rate.Limit(100),
		BurstLimit:  200,
	}

	registry.RegisterService(testService)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, exists := registry.GetService("benchmark-service")
		if !exists {
			b.Fatal("Service not found")
		}
	}
}

// MockOldRegistry implements DynamicServiceRegistry for testing migration
type MockOldRegistry struct {
	services map[string]ServiceDefinition
}

func (m *MockOldRegistry) RegisterService(def ServiceDefinition) error {
	m.services[def.Name] = def
	return nil
}

func (m *MockOldRegistry) GetService(name string) (*ServiceDefinition, bool) {
	service, exists := m.services[name]
	if !exists {
		return nil, false
	}
	return &service, true
}

func (m *MockOldRegistry) ListServiceDefinitions() []ServiceDefinition {
	defs := make([]ServiceDefinition, 0, len(m.services))
	for _, service := range m.services {
		defs = append(defs, service)
	}
	return defs
}

func TestUnifiedServiceRegistry_ServiceFiltering(t *testing.T) {
	awsConfig := aws.Config{Region: "us-east-1"}
	registryConfig := RegistryConfig{
		EnableCache: true,
		AutoPersist: false,
	}
	registry := NewUnifiedServiceRegistry(awsConfig, registryConfig)

	// Set filter for only s3 and ec2
	registry.SetServiceFilter([]string{"s3", "ec2"})

	// Try to register multiple services
	services := []ServiceDefinition{
		{Name: "s3", DisplayName: "Amazon S3", RateLimit: rate.Limit(10), BurstLimit: 20},
		{Name: "ec2", DisplayName: "Amazon EC2", RateLimit: rate.Limit(10), BurstLimit: 20},
		{Name: "lambda", DisplayName: "AWS Lambda", RateLimit: rate.Limit(10), BurstLimit: 20},
		{Name: "dynamodb", DisplayName: "Amazon DynamoDB", RateLimit: rate.Limit(10), BurstLimit: 20},
	}

	for _, svc := range services {
		registry.RegisterService(svc)
	}

	// Should only have s3 and ec2
	registered := registry.ListServices()
	if len(registered) != 2 {
		t.Fatalf("Expected 2 services, got %d", len(registered))
	}

	// Check that s3 and ec2 are present
	serviceMap := make(map[string]bool)
	for _, serviceName := range registered {
		serviceMap[serviceName] = true
	}

	if !serviceMap["s3"] {
		t.Error("s3 service should be registered")
	}
	if !serviceMap["ec2"] {
		t.Error("ec2 service should be registered")
	}
	if serviceMap["lambda"] {
		t.Error("lambda service should not be registered")
	}
	if serviceMap["dynamodb"] {
		t.Error("dynamodb service should not be registered")
	}

	// Test GetFilteredServices
	filtered := registry.GetFilteredServices()
	if len(filtered) != 2 {
		t.Fatalf("Expected 2 filtered services, got %d", len(filtered))
	}

	// Test IsServiceAllowed method
	if !registry.IsServiceAllowed("s3") {
		t.Error("s3 should be allowed")
	}
	if !registry.IsServiceAllowed("ec2") {
		t.Error("ec2 should be allowed")
	}
	if registry.IsServiceAllowed("lambda") {
		t.Error("lambda should not be allowed")
	}
	if registry.IsServiceAllowed("dynamodb") {
		t.Error("dynamodb should not be allowed")
	}

	// Test clearing filter
	registry.SetServiceFilter([]string{})
	
	// Now lambda should be allowed to register
	lambdaDef := ServiceDefinition{
		Name: "lambda", 
		DisplayName: "AWS Lambda", 
		RateLimit: rate.Limit(10), 
		BurstLimit: 20,
	}
	err := registry.RegisterService(lambdaDef)
	if err != nil {
		t.Fatalf("Failed to register lambda after clearing filter: %v", err)
	}

	// Should now have 3 services
	allServices := registry.ListServices()
	if len(allServices) != 3 {
		t.Fatalf("Expected 3 services after clearing filter, got %d", len(allServices))
	}

	// All services should now be allowed
	if !registry.IsServiceAllowed("lambda") {
		t.Error("lambda should be allowed after clearing filter")
	}
	if !registry.IsServiceAllowed("dynamodb") {
		t.Error("dynamodb should be allowed after clearing filter")
	}
}
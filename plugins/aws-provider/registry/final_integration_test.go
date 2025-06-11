package registry

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// TestUnifiedRegistry_CompleteIntegration validates the entire unified registry system
func TestUnifiedRegistry_CompleteIntegration(t *testing.T) {
	ctx := context.Background()
	
	// Create comprehensive configuration
	config := RegistryConfig{
		EnableCache:         true,
		CacheTTL:           15 * time.Minute,
		MaxCacheSize:       1000,
		EnableMetrics:      true,
		EnableAuditLog:     true,
		EnableDiscovery:    true,
		UseFallbackServices: true,
		PersistencePath:    "/tmp/test-unified-registry.json",
		AutoPersist:        false, // Don't auto-persist in tests
	}
	
	awsConfig := aws.Config{Region: "us-east-1"}
	registry := NewUnifiedServiceRegistry(awsConfig, config)
	
	t.Run("ServiceRegistrationAndRetrieval", func(t *testing.T) {
		testServices := []ServiceDefinition{
			{
				Name:        "s3",
				DisplayName: "Amazon S3",
				Description: "Simple Storage Service",
				PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
				GlobalService: true,
				RateLimit:   rate.Limit(100),
				BurstLimit:  200,
				ResourceTypes: []ResourceTypeDefinition{
					{
						Name:         "Bucket",
						ResourceType: "AWS::S3::Bucket",
						ListOperation: "ListBuckets",
						SupportsTags: true,
						Paginated:    false,
					},
				},
				Operations: []OperationDefinition{
					{
						Name:          "ListBuckets",
						OperationType: "List",
						Paginated:     false,
						RequiredPermissions: []string{"s3:ListAllMyBuckets"},
					},
				},
				Permissions: []string{"s3:ListAllMyBuckets"},
				DiscoverySource: "test",
			},
			{
				Name:        "ec2",
				DisplayName: "Amazon EC2",
				Description: "Elastic Compute Cloud",
				PackagePath: "github.com/aws/aws-sdk-go-v2/service/ec2",
				RequiresRegion: true,
				RateLimit:   rate.Limit(20),
				BurstLimit:  40,
				ResourceTypes: []ResourceTypeDefinition{
					{
						Name:         "Instance",
						ResourceType: "AWS::EC2::Instance",
						ListOperation: "DescribeInstances",
						SupportsTags: true,
						Paginated:    true,
					},
				},
				Operations: []OperationDefinition{
					{
						Name:          "DescribeInstances",
						OperationType: "Describe",
						Paginated:     true,
						RequiredPermissions: []string{"ec2:DescribeInstances"},
					},
				},
				Permissions: []string{"ec2:DescribeInstances"},
				DiscoverySource: "test",
			},
		}
		
		// Register services
		for _, service := range testServices {
			err := registry.RegisterService(service)
			require.NoError(t, err, "Failed to register service %s", service.Name)
		}
		
		// Verify retrieval
		for _, expected := range testServices {
			retrieved, exists := registry.GetService(expected.Name)
			require.True(t, exists, "Service %s not found", expected.Name)
			assert.Equal(t, expected.Name, retrieved.Name)
			assert.Equal(t, expected.DisplayName, retrieved.DisplayName)
			assert.Equal(t, expected.GlobalService, retrieved.GlobalService)
			assert.Equal(t, expected.RequiresRegion, retrieved.RequiresRegion)
		}
		
		// Verify list operations
		services := registry.ListServices()
		assert.GreaterOrEqual(t, len(services), len(testServices))
		
		definitions := registry.ListServiceDefinitions()
		assert.GreaterOrEqual(t, len(definitions), len(testServices))
	})
	
	t.Run("ClientFactoryIntegration", func(t *testing.T) {
		// Register client factories
		mockFactories := map[string]ClientFactoryFunc{
			"s3": func(cfg aws.Config) interface{} {
				return &TestS3Client{Config: cfg}
			},
			"ec2": func(cfg aws.Config) interface{} {
				return &TestEC2Client{Config: cfg}
			},
		}
		
		for serviceName, factory := range mockFactories {
			service, exists := registry.GetService(serviceName)
			require.True(t, exists, "Service %s not found for factory registration", serviceName)
			
			err := registry.RegisterServiceWithFactory(*service, factory)
			require.NoError(t, err, "Failed to register factory for %s", serviceName)
		}
		
		// Test client creation and caching
		for serviceName := range mockFactories {
			// First call - should create new client
			start := time.Now()
			client1, err := registry.CreateClient(ctx, serviceName)
			createDuration := time.Since(start)
			require.NoError(t, err, "Failed to create client for %s", serviceName)
			require.NotNil(t, client1)
			
			// Second call - should use cache
			start = time.Now()
			client2, err := registry.CreateClient(ctx, serviceName)
			cacheDuration := time.Since(start)
			require.NoError(t, err, "Failed to get cached client for %s", serviceName)
			require.NotNil(t, client2)
			
			// Verify caching worked (same instance)
			assert.Same(t, client1, client2, "Client caching failed for %s", serviceName)
			
			// Cache should be significantly faster
			assert.True(t, cacheDuration < createDuration/2, 
				"Cache lookup should be faster than creation for %s", serviceName)
		}
	})
	
	t.Run("ScannerIntegration", func(t *testing.T) {
		// Set up mock scanner
		mockScanner := &ComprehensiveTestScanner{
			resources: map[string][]*pb.ResourceRef{
				"s3": {
					{Service: "s3", Type: "Bucket", Id: "test-bucket-1", Name: "test-bucket-1", Region: "us-east-1"},
					{Service: "s3", Type: "Bucket", Id: "test-bucket-2", Name: "test-bucket-2", Region: "us-east-1"},
				},
				"ec2": {
					{Service: "ec2", Type: "Instance", Id: "i-1234567890abcdef0", Name: "test-instance", Region: "us-east-1"},
					{Service: "ec2", Type: "Volume", Id: "vol-049df61146c4d7901", Name: "test-volume", Region: "us-east-1"},
				},
			},
		}
		
		registry.SetUnifiedScanner(mockScanner)
		
		// Test scanning services
		testCases := []struct {
			serviceName     string
			expectedCount   int
		}{
			{"s3", 2},
			{"ec2", 2},
		}
		
		for _, tc := range testCases {
			resources, err := registry.ScanService(ctx, tc.serviceName, "us-east-1")
			require.NoError(t, err, "Failed to scan service %s", tc.serviceName)
			assert.Len(t, resources, tc.expectedCount, 
				"Expected %d resources for %s, got %d", tc.expectedCount, tc.serviceName, len(resources))
			
			// Verify resource details
			for _, resource := range resources {
				assert.Equal(t, "aws", resource.Provider)
				assert.Equal(t, tc.serviceName, resource.Service)
				assert.NotEmpty(t, resource.Id)
				assert.NotEmpty(t, resource.Type)
				assert.Equal(t, "us-east-1", resource.Region)
			}
		}
	})
	
	t.Run("RateLimitingIntegration", func(t *testing.T) {
		// Create service with strict rate limiting for testing
		strictService := ServiceDefinition{
			Name:       "rate-test",
			DisplayName: "Rate Limit Test Service",
			RateLimit:  rate.Limit(2), // 2 requests per second
			BurstLimit: 2,
			DiscoverySource: "test",
		}
		
		err := registry.RegisterService(strictService)
		require.NoError(t, err)
		
		limiter := registry.GetRateLimiter("rate-test")
		require.NotNil(t, limiter)
		
		// Test rate limiting timing
		requests := 3
		start := time.Now()
		
		for i := 0; i < requests; i++ {
			err := limiter.Wait(ctx)
			require.NoError(t, err, "Rate limit wait failed on request %d", i+1)
		}
		
		elapsed := time.Since(start)
		
		// Should take at least 1 second for 3 requests at 2/sec with burst of 2
		expectedMinDuration := 500 * time.Millisecond
		assert.GreaterOrEqual(t, elapsed, expectedMinDuration,
			"Rate limiting not working: %v should be >= %v", elapsed, expectedMinDuration)
	})
	
	t.Run("LazyLoadingIntegration", func(t *testing.T) {
		// Test lazy loading of well-known services
		wellKnownServices := []string{"lambda", "dynamodb", "rds"}
		
		for _, serviceName := range wellKnownServices {
			// First access should trigger lazy loading
			start := time.Now()
			client, err := registry.CreateClient(ctx, serviceName)
			lazyLoadDuration := time.Since(start)
			
			// Should succeed with well-known services
			require.NoError(t, err, "Lazy loading failed for %s", serviceName)
			require.NotNil(t, client, "Lazy loaded client is nil for %s", serviceName)
			
			// Verify service was loaded
			service, exists := registry.GetService(serviceName)
			assert.True(t, exists, "Service %s not loaded after lazy loading", serviceName)
			assert.Equal(t, "well-known", service.DiscoverySource)
			
			// Second access should be cached
			start = time.Now()
			_, err = registry.CreateClient(ctx, serviceName)
			cacheDuration := time.Since(start)
			require.NoError(t, err)
			
			// Cache should be faster
			assert.True(t, cacheDuration < lazyLoadDuration/2,
				"Cache access not faster than lazy load for %s", serviceName)
		}
	})
	
	t.Run("MigrationIntegration", func(t *testing.T) {
		// Create a mock legacy registry
		legacyRegistry := &MockOldRegistry{
			services: make(map[string]ServiceDefinition),
		}
		
		// Add services to legacy registry
		legacyServices := []ServiceDefinition{
			{
				Name:        "cloudformation",
				DisplayName: "AWS CloudFormation",
				Description: "Infrastructure as Code",
				DiscoverySource: "legacy",
			},
			{
				Name:        "iam",
				DisplayName: "AWS IAM",
				Description: "Identity and Access Management",
				DiscoverySource: "legacy",
			},
		}
		
		for _, service := range legacyServices {
			err := legacyRegistry.RegisterService(service)
			require.NoError(t, err)
		}
		
		// Test migration
		err := registry.MigrateFromDynamicRegistry(legacyRegistry)
		require.NoError(t, err, "Migration failed")
		
		// Verify migrated services
		for _, expected := range legacyServices {
			migrated, exists := registry.GetService(expected.Name)
			require.True(t, exists, "Migrated service %s not found", expected.Name)
			assert.Equal(t, expected.Name, migrated.Name)
			assert.Equal(t, expected.DisplayName, migrated.DisplayName)
			assert.Equal(t, expected.DiscoverySource, migrated.DiscoverySource)
		}
	})
	
	t.Run("StatisticsAndMetrics", func(t *testing.T) {
		stats := registry.GetStats()
		
		// Verify basic statistics
		assert.Greater(t, stats.TotalServices, 0, "No services reported in stats")
		assert.Greater(t, stats.TotalResourceTypes, 0, "No resource types reported in stats")
		assert.Greater(t, stats.TotalOperations, 0, "No operations reported in stats")
		
		// Verify statistics structure
		assert.NotNil(t, stats.ServicesBySource)
		assert.NotNil(t, stats.ServicesByStatus)
		assert.NotZero(t, stats.LastUpdated)
		
		// Verify we have services from different sources
		sources := []string{"test", "well-known", "legacy"}
		foundSources := 0
		for _, source := range sources {
			if count, exists := stats.ServicesBySource[source]; exists && count > 0 {
				foundSources++
			}
		}
		assert.Greater(t, foundSources, 0, "No services found from expected sources")
		
		// Test cache metrics (should have cache hits from previous tests)
		assert.GreaterOrEqual(t, stats.CacheHitRate, 0.0)
		assert.LessOrEqual(t, stats.CacheHitRate, 1.0)
	})
	
	t.Run("PersistenceIntegration", func(t *testing.T) {
		// Test backup creation
		err := registry.CreateBackup()
		require.NoError(t, err, "Failed to create backup")
		
		// Test persistence
		testPath := "/tmp/test-registry-persist.json"
		err = registry.PersistToFile(testPath)
		require.NoError(t, err, "Failed to persist registry")
		
		// Create new registry and load from file
		newConfig := config
		newConfig.PersistencePath = testPath
		newRegistry := NewUnifiedServiceRegistry(awsConfig, newConfig)
		
		err = newRegistry.LoadFromFile(testPath)
		require.NoError(t, err, "Failed to load registry from file")
		
		// Verify services were loaded
		originalServices := registry.ListServices()
		loadedServices := newRegistry.ListServices()
		
		assert.GreaterOrEqual(t, len(loadedServices), len(originalServices)/2,
			"Loaded registry has significantly fewer services than original")
	})
	
	t.Run("PerformanceBenchmarks", func(t *testing.T) {
		// Warm up the cache
		for i := 0; i < 10; i++ {
			registry.GetService("s3")
			registry.CreateClient(ctx, "s3")
		}
		
		// Benchmark service lookup
		iterations := 1000
		start := time.Now()
		for i := 0; i < iterations; i++ {
			_, _ = registry.GetService("s3")
		}
		lookupDuration := time.Since(start)
		avgLookup := lookupDuration / time.Duration(iterations)
		
		// Should be very fast (sub-microsecond with cache)
		assert.Less(t, avgLookup, 10*time.Microsecond,
			"Service lookup too slow: %v average", avgLookup)
		
		// Benchmark client creation (cached)
		start = time.Now()
		for i := 0; i < iterations; i++ {
			_, _ = registry.CreateClient(ctx, "s3")
		}
		clientDuration := time.Since(start)
		avgClient := clientDuration / time.Duration(iterations)
		
		// Cached client creation should be very fast
		assert.Less(t, avgClient, 1*time.Microsecond,
			"Cached client creation too slow: %v average", avgClient)
		
		t.Logf("Performance results:")
		t.Logf("  Service lookup: %v average", avgLookup)
		t.Logf("  Client creation (cached): %v average", avgClient)
	})
}

// ComprehensiveTestScanner implements UnifiedScannerProvider for testing
type ComprehensiveTestScanner struct {
	resources map[string][]*pb.ResourceRef
	scanCount int
	descCount int
}

func (c *ComprehensiveTestScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	c.scanCount++
	if resources, exists := c.resources[serviceName]; exists {
		return resources, nil
	}
	return []*pb.ResourceRef{}, nil
}

func (c *ComprehensiveTestScanner) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	c.descCount++
	return &pb.Resource{
		Provider: "aws",
		Service:  ref.Service,
		Type:     ref.Type,
		Id:       ref.Id,
		Name:     ref.Name,
		Region:   ref.Region,
		Tags: map[string]string{
			"Environment": "test",
			"ManagedBy":   "unified-registry",
		},
	}, nil
}

func (c *ComprehensiveTestScanner) GetMetrics() interface{} {
	return map[string]interface{}{
		"scan_calls":     c.scanCount,
		"describe_calls": c.descCount,
		"total_calls":    c.scanCount + c.descCount,
	}
}

// Mock clients for testing (use different names to avoid conflicts)
type TestS3Client struct {
	Config aws.Config
}

type TestEC2Client struct {
	Config aws.Config
}
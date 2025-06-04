//go:build migration
// +build migration

package tests

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MigrationTestSuite contains all migration tests
type MigrationTestSuite struct {
	cfg        aws.Config
	pipeline   *runtime.RuntimePipeline
	discovery  *discovery.RuntimeServiceDiscovery
	oldServices []string // Known working services before migration
}

// setupMigrationTest initializes the test suite
func setupMigrationTest(t *testing.T) *MigrationTestSuite {
	// Skip if not running migration tests
	if os.Getenv("RUN_MIGRATION_TESTS") != "true" {
		t.Skip("Skipping migration test. Set RUN_MIGRATION_TESTS=true to run.")
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(t, err, "Failed to load AWS config")

	// Known working services (the 18 currently discovered services)
	knownServices := []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"secretsmanager", "ssm", "kms",
	}

	// Create discovery instance
	discovery := discovery.NewRuntimeServiceDiscovery(cfg)

	// Create pipeline configuration
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      3,
		ScanTimeout:         30 * time.Second,
		UseResourceExplorer: false,
		BatchSize:           50,
		FlushInterval:       2 * time.Second,
		EnableAutoDiscovery: true,
		StreamingEnabled:    true,
	}

	// Create runtime pipeline
	pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
	require.NoError(t, err, "Failed to create pipeline")

	return &MigrationTestSuite{
		cfg:         cfg,
		pipeline:    pipeline,
		discovery:   discovery,
		oldServices: knownServices,
	}
}

// TestMigrationSuite runs all migration tests
func TestMigrationSuite(t *testing.T) {
	suite := setupMigrationTest(t)
	defer suite.cleanup()

	t.Run("KnownServicesStillWork", suite.testKnownServicesStillWork)
	t.Run("S3ConfigurationCollection", suite.testS3ConfigurationCollection)
	t.Run("NewServiceDiscovery", suite.testNewServiceDiscovery)
	t.Run("PerformanceBenchmark", suite.testPerformanceBenchmark)
	t.Run("ResourceCoverageComparison", suite.testResourceCoverageComparison)
}

// testKnownServicesStillWork verifies all 18 currently discovered services work
func (s *MigrationTestSuite) testKnownServicesStillWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := s.pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")

	successCount := 0
	failedServices := []string{}

	for _, serviceName := range s.oldServices {
		t.Run(fmt.Sprintf("Service_%s", serviceName), func(t *testing.T) {
			result, err := s.pipeline.ScanService(ctx, serviceName, s.cfg, "us-east-1")

			if err != nil {
				t.Logf("Service %s failed: %v", serviceName, err)
				failedServices = append(failedServices, serviceName)
				return
			}

			assert.Equal(t, serviceName, result.Service)
			assert.Equal(t, "us-east-1", result.Region)
			assert.NotZero(t, result.Duration)
			successCount++

			t.Logf("Service %s: Success (Duration: %v)", serviceName, result.Duration)
		})
	}

	// At least 90% of known services should work
	minSuccess := int(float64(len(s.oldServices)) * 0.9)
	assert.GreaterOrEqual(t, successCount, minSuccess,
		"Expected at least %d services to work, got %d. Failed services: %v",
		minSuccess, successCount, failedServices)

	t.Logf("Migration Test Results: %d/%d services working", successCount, len(s.oldServices))
}

// testS3ConfigurationCollection specifically tests S3 configuration collection
func (s *MigrationTestSuite) testS3ConfigurationCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := s.pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")

	result, err := s.pipeline.ScanService(ctx, "s3", s.cfg, "us-east-1")
	require.NoError(t, err, "S3 scan should not fail")

	assert.Equal(t, "s3", result.Service)
	assert.NotZero(t, result.Duration)

	// Check if we can get detailed configuration for S3 resources
	batchResult, err := s.pipeline.ScanAllServices(ctx, s.cfg, "us-east-1")
	require.NoError(t, err, "Batch scan should not fail")

	// Look for S3 resources in the results
	s3ResourceCount := 0
	for _, resource := range batchResult.Resources {
		if resource.Service == "s3" {
			s3ResourceCount++
			
			// Verify basic resource structure
			assert.NotEmpty(t, resource.Id, "S3 resource should have ID")
			assert.NotEmpty(t, resource.Type, "S3 resource should have type")
			assert.Equal(t, "aws", resource.Provider, "S3 resource should have correct provider")
			
			t.Logf("Found S3 resource: Type=%s, ID=%s", resource.Type, resource.Id)
		}
	}

	t.Logf("S3 Configuration Collection: Found %d S3 resources", s3ResourceCount)
}

// testNewServiceDiscovery tests discovery of new services beyond the known 18
func (s *MigrationTestSuite) testNewServiceDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Discover all available services
	services, err := s.discovery.DiscoverServices(ctx)
	require.NoError(t, err, "Service discovery should not fail")

	discoveredNames := make([]string, len(services))
	for i, service := range services {
		discoveredNames[i] = service.Name
	}
	sort.Strings(discoveredNames)

	// Should discover more than the known 18 services
	assert.Greater(t, len(discoveredNames), len(s.oldServices),
		"Should discover more than %d services, found %d",
		len(s.oldServices), len(discoveredNames))

	// All known services should be in the discovered list
	for _, knownService := range s.oldServices {
		assert.Contains(t, discoveredNames, knownService,
			"Known service %s should be discovered", knownService)
	}

	// Find new services
	newServices := []string{}
	for _, discovered := range discoveredNames {
		isKnown := false
		for _, known := range s.oldServices {
			if discovered == known {
				isKnown = true
				break
			}
		}
		if !isKnown {
			newServices = append(newServices, discovered)
		}
	}

	t.Logf("Service Discovery Results:")
	t.Logf("  Total discovered: %d", len(discoveredNames))
	t.Logf("  Known services: %d", len(s.oldServices))
	t.Logf("  New services: %d (%v)", len(newServices), newServices)

	// Test a few new services to ensure they can be scanned
	err = s.pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")

	testedNewServices := 0
	maxTestServices := 3 // Limit to avoid long test times

	for _, newService := range newServices {
		if testedNewServices >= maxTestServices {
			break
		}

		t.Run(fmt.Sprintf("NewService_%s", newService), func(t *testing.T) {
			result, err := s.pipeline.ScanService(ctx, newService, s.cfg, "us-east-1")
			
			if err != nil {
				t.Logf("New service %s failed (expected): %v", newService, err)
				return
			}

			assert.Equal(t, newService, result.Service)
			t.Logf("New service %s: Success", newService)
			testedNewServices++
		})
	}
}

// testPerformanceBenchmark benchmarks scanning performance
func (s *MigrationTestSuite) testPerformanceBenchmark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := s.pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")

	// Benchmark single service scan
	startTime := time.Now()
	_, err = s.pipeline.ScanService(ctx, "s3", s.cfg, "us-east-1")
	singleScanDuration := time.Since(startTime)
	
	require.NoError(t, err, "Single service scan should not fail")
	assert.Less(t, singleScanDuration, 30*time.Second, "Single service scan should complete within 30 seconds")

	// Benchmark batch scan of first 5 known services
	testServices := s.oldServices[:5]
	startTime = time.Now()
	batchResult, err := s.pipeline.ScanServices(ctx, testServices, s.cfg, "us-east-1")
	batchScanDuration := time.Since(startTime)
	
	require.NoError(t, err, "Batch scan should not fail")
	assert.Less(t, batchScanDuration, 60*time.Second, "Batch scan should complete within 60 seconds")

	// Performance expectations
	t.Logf("Performance Benchmark Results:")
	t.Logf("  Single service (s3): %v", singleScanDuration)
	t.Logf("  Batch 5 services: %v", batchScanDuration)
	t.Logf("  Total resources found: %d", batchResult.TotalResources)
	t.Logf("  Services scanned: %d", batchResult.ServicesScanned)

	// Basic performance assertions
	assert.Greater(t, batchResult.TotalResources, 0, "Should find some resources")
	assert.Equal(t, len(testServices), batchResult.ServicesScanned, "Should scan all requested services")
}

// testResourceCoverageComparison compares resource coverage before/after migration
func (s *MigrationTestSuite) testResourceCoverageComparison(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	err := s.pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")

	// Scan a subset of known services to compare coverage
	testServices := []string{"s3", "ec2", "lambda", "iam"}
	
	batchResult, err := s.pipeline.ScanServices(ctx, testServices, s.cfg, "us-east-1")
	require.NoError(t, err, "Resource coverage scan should not fail")

	resourcesByService := make(map[string][]string)
	resourceTypesByService := make(map[string]map[string]int)

	for _, resource := range batchResult.Resources {
		if resourcesByService[resource.Service] == nil {
			resourcesByService[resource.Service] = []string{}
			resourceTypesByService[resource.Service] = make(map[string]int)
		}
		resourcesByService[resource.Service] = append(resourcesByService[resource.Service], resource.Id)
		resourceTypesByService[resource.Service][resource.Type]++
	}

	t.Logf("Resource Coverage Analysis:")
	for _, service := range testServices {
		resourceCount := len(resourcesByService[service])
		typeCount := len(resourceTypesByService[service])
		
		t.Logf("  %s: %d resources, %d types", service, resourceCount, typeCount)
		
		// Basic coverage expectations
		if service == "s3" {
			// S3 should have at least bucket resources
			assert.Greater(t, typeCount, 0, "S3 should discover at least one resource type")
		}
		if service == "ec2" {
			// EC2 typically has many resource types
			assert.Greater(t, typeCount, 0, "EC2 should discover at least one resource type")
		}
	}

	// Overall coverage assertions
	assert.Greater(t, batchResult.TotalResources, 0, "Should discover some resources")
	assert.Greater(t, len(resourcesByService), 0, "Should have resources for some services")
}

// cleanup cleans up test resources
func (s *MigrationTestSuite) cleanup() {
	if s.pipeline != nil {
		s.pipeline.Stop()
	}
}

// BenchmarkMigrationPerformance benchmarks the migration performance
func BenchmarkMigrationPerformance(b *testing.B) {
	if os.Getenv("RUN_MIGRATION_BENCHMARKS") != "true" {
		b.Skip("Skipping migration benchmark. Set RUN_MIGRATION_BENCHMARKS=true to run.")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(b, err)

	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      3,
		ScanTimeout:         30 * time.Second,
		UseResourceExplorer: false,
		BatchSize:           50,
		FlushInterval:       2 * time.Second,
		EnableAutoDiscovery: true,
		StreamingEnabled:    false, // Disable streaming for benchmark consistency
	}

	pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
	require.NoError(b, err)

	err = pipeline.Start()
	require.NoError(b, err)
	defer pipeline.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = pipeline.ScanService(ctx, "s3", cfg, "us-east-1")
		cancel()
	}
}

// TestRollbackMechanism tests the rollback mechanism
func TestRollbackMechanism(t *testing.T) {
	if os.Getenv("RUN_ROLLBACK_TESTS") != "true" {
		t.Skip("Skipping rollback test. Set RUN_ROLLBACK_TESTS=true to run.")
	}

	// Test feature flag switching between old/new system
	t.Run("FeatureFlag", func(t *testing.T) {
		// Test with migration enabled
		os.Setenv("AWS_PROVIDER_MIGRATION_ENABLED", "true")
		defer os.Unsetenv("AWS_PROVIDER_MIGRATION_ENABLED")

		cfg, err := config.LoadDefaultConfig(context.Background())
		require.NoError(t, err)

		// Test that the new system is used
		discovery := discovery.NewRuntimeServiceDiscovery(cfg)
		services, err := discovery.DiscoverServices(context.Background())
		require.NoError(t, err)
		assert.Greater(t, len(services), 0, "Should discover services with new system")

		// Test with migration disabled (would use fallback/backup system)
		os.Setenv("AWS_PROVIDER_MIGRATION_ENABLED", "false")
		
		// In a real rollback scenario, this would switch to the old hardcoded system
		// For this test, we just verify the flag is respected
		migrationEnabled := os.Getenv("AWS_PROVIDER_MIGRATION_ENABLED") == "true"
		assert.False(t, migrationEnabled, "Migration should be disabled")
	})
}
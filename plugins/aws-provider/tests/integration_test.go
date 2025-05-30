//go:build integration
// +build integration

package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationPipeline(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(t, err, "Failed to load AWS config")

	// Create pipeline configuration
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      5,
		ScanTimeout:         2 * time.Minute,
		UseResourceExplorer: false, // Use generated scanners for testing
		EnableDuckDB:        true,
		DuckDBPath:          filepath.Join(t.TempDir(), "test.duckdb"),
		BatchSize:           100,
		FlushInterval:       5 * time.Second,
		EnableAutoDiscovery: false,
		ServiceFilter:       []string{"ec2", "s3", "lambda"}, // Test specific services
		StreamingEnabled:    true,
	}

	// Create runtime pipeline
	pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
	require.NoError(t, err, "Failed to create pipeline")

	// Start pipeline
	err = pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")
	defer pipeline.Stop()

	// Test scanning a specific service
	t.Run("ScanSingleService", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := pipeline.ScanService(ctx, "ec2", cfg, "us-east-1")
		require.NoError(t, err, "Failed to scan EC2 service")
		
		assert.Equal(t, "ec2", result.Service)
		assert.Equal(t, "us-east-1", result.Region)
		assert.NotZero(t, result.Duration)
		assert.Equal(t, "GeneratedScanner", result.Method)
	})

	// Test batch scanning
	t.Run("BatchScan", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		batchResult, err := pipeline.ScanAllServices(ctx, cfg, "us-east-1")
		require.NoError(t, err, "Failed to perform batch scan")
		
		assert.GreaterOrEqual(t, batchResult.ServicesScanned, 3)
		assert.NotZero(t, batchResult.Duration)
		
		// Check service-specific results
		for _, service := range []string{"ec2", "s3", "lambda"} {
			stats, exists := batchResult.ServiceStats[service]
			assert.True(t, exists, "Missing stats for service: %s", service)
			if exists {
				assert.True(t, stats.Success || stats.Error != "", "Service %s has invalid state", service)
			}
		}
	})

	// Test streaming client
	t.Run("StreamingClient", func(t *testing.T) {
		client := pipeline.NewStreamingClient()
		defer client.Close()

		// Trigger a scan to generate resources
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		go func() {
			_, _ = pipeline.ScanService(ctx, "s3", cfg, "us-east-1")
		}()

		// Try to receive some resources
		resourceCount := 0
		timeout := time.After(10 * time.Second)
		
		for {
			select {
			case <-timeout:
				t.Logf("Received %d resources via streaming", resourceCount)
				return
			default:
				resource, ok := client.Next()
				if ok && resource != nil {
					resourceCount++
					assert.NotEmpty(t, resource.Id)
					assert.NotEmpty(t, resource.Type)
				}
			}
		}
	})

	// Test pipeline statistics
	t.Run("PipelineStats", func(t *testing.T) {
		stats := pipeline.GetStats()
		
		assert.True(t, stats.IsRunning)
		assert.NotEmpty(t, stats.RegisteredServices)
		assert.Contains(t, stats.RegisteredServices, "ec2")
		
		if len(stats.ServiceStats) > 0 {
			for service, serviceStat := range stats.ServiceStats {
				t.Logf("Service %s: %d resources, duration: %v", 
					service, serviceStat.ResourceCount, serviceStat.Duration)
			}
		}
	})
}

func TestScannerRegistry(t *testing.T) {
	registry := runtime.NewScannerRegistry()

	// Test registering a scanner
	t.Run("RegisterScanner", func(t *testing.T) {
		testScanner := &mockScanner{
			serviceName: "test-service",
			resourceTypes: []string{"TestResource"},
		}
		
		metadata := &runtime.ScannerMetadata{
			ServiceName:    "test-service",
			ResourceTypes:  []string{"TestResource"},
			RateLimit:      10,
			BurstLimit:     20,
		}
		
		err := registry.Register(testScanner, metadata)
		assert.NoError(t, err)
		
		// Try to get the scanner
		scanner, exists := registry.Get("test-service")
		assert.True(t, exists)
		assert.Equal(t, "test-service", scanner.ServiceName())
		
		// Check metadata
		meta, exists := registry.GetMetadata("test-service")
		assert.True(t, exists)
		assert.Equal(t, 10, int(meta.RateLimit))
	})

	// Test duplicate registration
	t.Run("DuplicateRegistration", func(t *testing.T) {
		testScanner := &mockScanner{serviceName: "duplicate"}
		metadata := &runtime.ScannerMetadata{ServiceName: "duplicate"}
		
		err := registry.Register(testScanner, metadata)
		assert.NoError(t, err)
		
		// Try to register again
		err = registry.Register(testScanner, metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	// Test rate limiting
	t.Run("RateLimiting", func(t *testing.T) {
		limiter := registry.GetRateLimiter("test-service")
		assert.NotNil(t, limiter)
		
		// Test rate limit enforcement
		start := time.Now()
		for i := 0; i < 5; i++ {
			err := limiter.Wait(context.Background())
			assert.NoError(t, err)
		}
		elapsed := time.Since(start)
		
		// Should take some time due to rate limiting
		assert.True(t, elapsed > 100*time.Millisecond, "Rate limiting not working properly")
	})
}

// Mock scanner for testing
type mockScanner struct {
	serviceName   string
	resourceTypes []string
}

func (m *mockScanner) ServiceName() string {
	return m.serviceName
}

func (m *mockScanner) ResourceTypes() []string {
	return m.resourceTypes
}

func (m *mockScanner) Scan(ctx context.Context, cfg aws.Config, region string) ([]*pb.Resource, error) {
	// Return mock resources
	return []*pb.Resource{
		{
			Id:   "mock-resource-1",
			Type: m.resourceTypes[0],
			Name: "Mock Resource 1",
		},
	}, nil
}

func (m *mockScanner) SupportsPagination() bool {
	return false
}

func (m *mockScanner) SupportsResourceExplorer() bool {
	return false
}

func BenchmarkPipeline(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(b, err)

	pipelineConfig := runtime.DefaultPipelineConfig()
	pipelineConfig.EnableDuckDB = false // Disable for pure scanning benchmark
	pipelineConfig.ServiceFilter = []string{"ec2"}

	pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
	require.NoError(b, err)

	err = pipeline.Start()
	require.NoError(b, err)
	defer pipeline.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = pipeline.ScanService(ctx, "ec2", cfg, "us-east-1")
		cancel()
	}
}
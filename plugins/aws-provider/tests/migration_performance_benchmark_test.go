//go:build benchmark
// +build benchmark

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/stretchr/testify/require"
)

// BenchmarkMigrationComparison compares performance before and after migration
func BenchmarkMigrationComparison(b *testing.B) {
	if os.Getenv("RUN_MIGRATION_BENCHMARKS") != "true" {
		b.Skip("Skipping migration benchmarks. Set RUN_MIGRATION_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	suite := &MigrationBenchmarkSuite{
		cfg: cfg,
		ctx: ctx,
	}

	// Benchmark new reflection-based system
	b.Run("NewReflectionSystem", func(b *testing.B) {
		suite.benchmarkNewSystem(b)
	})

	// Benchmark generated analysis loading
	b.Run("GeneratedAnalysisLoading", func(b *testing.B) {
		suite.benchmarkGeneratedAnalysisLoading(b)
	})

	// Compare memory usage
	b.Run("MemoryUsageComparison", func(b *testing.B) {
		suite.benchmarkMemoryUsage(b)
	})

	// Benchmark discovery performance
	b.Run("ServiceDiscoveryPerformance", func(b *testing.B) {
		suite.benchmarkServiceDiscovery(b)
	})

	// Benchmark scanning performance
	b.Run("ScanningPerformance", func(b *testing.B) {
		suite.benchmarkScanning(b)
	})

	// Benchmark concurrent operations
	b.Run("ConcurrentOperations", func(b *testing.B) {
		suite.benchmarkConcurrentOperations(b)
	})
}

type MigrationBenchmarkSuite struct {
	cfg aws.Config
	ctx context.Context
}

// benchmarkNewSystem benchmarks the new reflection-based system
func (s *MigrationBenchmarkSuite) benchmarkNewSystem(b *testing.B) {
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      3,
		ScanTimeout:         30 * time.Second,
		UseResourceExplorer: false,
		BatchSize:           50,
		FlushInterval:       2 * time.Second,
		EnableAutoDiscovery: true,
		StreamingEnabled:    false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(b, err)

	err = pipeline.Start()
	require.NoError(b, err)
	defer pipeline.Stop()

	testServices := []string{"s3", "lambda", "ec2"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, service := range testServices {
			scanCtx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
			_, err := pipeline.ScanService(scanCtx, service, s.cfg, "us-east-1")
			cancel()
			
			if err != nil {
				b.Logf("Service %s failed: %v", service, err)
			}
		}
	}
}

// benchmarkGeneratedAnalysisLoading benchmarks loading generated analysis files
func (s *MigrationBenchmarkSuite) benchmarkGeneratedAnalysisLoading(b *testing.B) {
	generatedPath := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, err := os.ReadFile(generatedPath)
		if err != nil {
			b.Fatalf("Failed to read file: %v", err)
		}

		var analysis ServiceAnalysis
		err = json.Unmarshal(data, &analysis)
		if err != nil {
			b.Fatalf("Failed to parse JSON: %v", err)
		}

		// Process the analysis (simulate actual usage)
		serviceCount := len(analysis.Services)
		operationCount := analysis.TotalOperations
		
		// Prevent compiler optimization
		_ = serviceCount
		_ = operationCount
	}
}

// benchmarkMemoryUsage compares memory usage patterns
func (s *MigrationBenchmarkSuite) benchmarkMemoryUsage(b *testing.B) {
	b.Run("ServiceDiscovery", func(b *testing.B) {
		discovery := discovery.NewRuntimeServiceDiscovery(s.cfg)
		
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// Force garbage collection before each iteration
			runtime.GC()
			
			var m1, m2 runtime.MemStats
			runtime.ReadMemStats(&m1)

			services, err := discovery.DiscoverServices(s.ctx)
			if err != nil {
				b.Fatalf("Discovery failed: %v", err)
			}
			
			runtime.ReadMemStats(&m2)
			
			// Calculate memory used
			memUsed := m2.TotalAlloc - m1.TotalAlloc
			
			b.ReportMetric(float64(memUsed), "bytes/discovery")
			b.ReportMetric(float64(len(services)), "services/discovery")
		}
	})

	b.Run("PipelineOperation", func(b *testing.B) {
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:      1,
			ScanTimeout:         15 * time.Second,
			UseResourceExplorer: false,
			BatchSize:           25,
			FlushInterval:       1 * time.Second,
			EnableAutoDiscovery: false,
			StreamingEnabled:    false,
		}

		pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
		require.NoError(b, err)

		err = pipeline.Start()
		require.NoError(b, err)
		defer pipeline.Stop()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			runtime.GC()
			
			var m1, m2 runtime.MemStats
			runtime.ReadMemStats(&m1)

			scanCtx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
			result, err := pipeline.ScanService(scanCtx, "s3", s.cfg, "us-east-1")
			cancel()
			
			runtime.ReadMemStats(&m2)
			
			if err != nil {
				b.Logf("Scan failed: %v", err)
				continue
			}
			
			memUsed := m2.TotalAlloc - m1.TotalAlloc
			
			b.ReportMetric(float64(memUsed), "bytes/scan")
			b.ReportMetric(float64(result.Duration.Nanoseconds()), "ns/scan")
		}
	})
}

// benchmarkServiceDiscovery benchmarks service discovery performance
func (s *MigrationBenchmarkSuite) benchmarkServiceDiscovery(b *testing.B) {
	discovery := discovery.NewRuntimeServiceDiscovery(s.cfg)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		services, err := discovery.DiscoverServices(s.ctx)
		duration := time.Since(start)
		
		if err != nil {
			b.Fatalf("Discovery failed: %v", err)
		}

		b.ReportMetric(float64(duration.Nanoseconds()), "ns/discovery")
		b.ReportMetric(float64(len(services)), "services/discovery")
		
		// Calculate discovery rate
		if duration > 0 {
			rate := float64(len(services)) / duration.Seconds()
			b.ReportMetric(rate, "services/sec")
		}
	}
}

// benchmarkScanning benchmarks actual scanning performance
func (s *MigrationBenchmarkSuite) benchmarkScanning(b *testing.B) {
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      2,
		ScanTimeout:         20 * time.Second,
		UseResourceExplorer: false,
		BatchSize:           30,
		FlushInterval:       1 * time.Second,
		EnableAutoDiscovery: false,
		StreamingEnabled:    false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(b, err)

	err = pipeline.Start()
	require.NoError(b, err)
	defer pipeline.Stop()

	// Test different services
	testServices := []string{"s3", "lambda", "ec2", "iam"}

	for _, service := range testServices {
		b.Run(fmt.Sprintf("Service_%s", service), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				scanCtx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
				
				start := time.Now()
				result, err := pipeline.ScanService(scanCtx, service, s.cfg, "us-east-1")
				duration := time.Since(start)
				
				cancel()

				if err != nil {
					b.Logf("Service %s scan %d failed: %v", service, i, err)
					continue
				}

				b.ReportMetric(float64(duration.Nanoseconds()), "ns/scan")
				b.ReportMetric(float64(result.Duration.Nanoseconds()), "ns/internal")
				
				// Report scanning efficiency
				if duration > 0 {
					efficiency := float64(result.Duration.Nanoseconds()) / float64(duration.Nanoseconds())
					b.ReportMetric(efficiency, "efficiency")
				}
			}
		})
	}
}

// benchmarkConcurrentOperations benchmarks concurrent scanning
func (s *MigrationBenchmarkSuite) benchmarkConcurrentOperations(b *testing.B) {
	concurrencyLevels := []int{1, 2, 4, 8}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			pipelineConfig := &runtime.PipelineConfig{
				MaxConcurrency:      concurrency,
				ScanTimeout:         30 * time.Second,
				UseResourceExplorer: false,
				BatchSize:           25,
				FlushInterval:       1 * time.Second,
				EnableAutoDiscovery: false,
				StreamingEnabled:    false,
			}

			pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
			require.NoError(b, err)

			err = pipeline.Start()
			require.NoError(b, err)
			defer pipeline.Stop()

			services := []string{"s3", "lambda", "ec2", "iam"}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				scanCtx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
				
				start := time.Now()
				result, err := pipeline.ScanServices(scanCtx, services, s.cfg, "us-east-1")
				duration := time.Since(start)
				
				cancel()

				if err != nil {
					b.Logf("Batch scan %d failed: %v", i, err)
					continue
				}

				b.ReportMetric(float64(duration.Nanoseconds()), "ns/batch")
				b.ReportMetric(float64(result.ServicesScanned), "services/batch")
				b.ReportMetric(float64(result.TotalResources), "resources/batch")
				
				// Calculate throughput
				if duration > 0 {
					serviceRate := float64(result.ServicesScanned) / duration.Seconds()
					resourceRate := float64(result.TotalResources) / duration.Seconds()
					
					b.ReportMetric(serviceRate, "services/sec")
					b.ReportMetric(resourceRate, "resources/sec")
				}
			}
		})
	}
}

// BenchmarkReflectionOperations benchmarks reflection-based operations
func BenchmarkReflectionOperations(b *testing.B) {
	if os.Getenv("RUN_REFLECTION_BENCHMARKS") != "true" {
		b.Skip("Skipping reflection benchmarks. Set RUN_REFLECTION_BENCHMARKS=true to run.")
	}

	b.Run("OperationDiscovery", func(b *testing.B) {
		// Mock client for testing
		mockClient := &MockAWSClient{ServiceName: "s3"}
		discoverer := &ReflectionOperationDiscoverer{}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			operations := discoverer.DiscoverOperations(mockClient)
			
			// Prevent compiler optimization
			_ = len(operations)
		}
	})

	b.Run("FieldClassification", func(b *testing.B) {
		classifier := &ReflectionFieldClassifier{}
		testFields := []string{
			"InstanceId", "BucketName", "FunctionName", "State", "CreationDate",
			"VpcId", "SubnetId", "SecurityGroupId", "UserName", "RoleName",
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for _, field := range testFields {
				classification := classifier.ClassifyField(field, nil)
				
				// Prevent compiler optimization
				_ = classification
			}
		}
	})

	b.Run("ResourceTypeInference", func(b *testing.B) {
		inferrer := &ReflectionResourceInferrer{}
		testCases := []struct {
			service string
			method  string
		}{
			{"s3", "ListBuckets"},
			{"ec2", "DescribeInstances"},
			{"lambda", "ListFunctions"},
			{"iam", "ListUsers"},
			{"rds", "DescribeDBInstances"},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for _, testCase := range testCases {
				resourceType := inferrer.InferResourceType(testCase.service, testCase.method)
				
				// Prevent compiler optimization
				_ = resourceType
			}
		}
	})
}

// BenchmarkAnalysisFileOperations benchmarks analysis file operations
func BenchmarkAnalysisFileOperations(b *testing.B) {
	if os.Getenv("RUN_FILE_BENCHMARKS") != "true" {
		b.Skip("Skipping file benchmarks. Set RUN_FILE_BENCHMARKS=true to run.")
	}

	generatedPath := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"

	b.Run("FileRead", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			data, err := os.ReadFile(generatedPath)
			if err != nil {
				b.Fatalf("Failed to read file: %v", err)
			}
			
			// Prevent compiler optimization
			_ = len(data)
		}
	})

	b.Run("JSONParsing", func(b *testing.B) {
		// Read file once
		data, err := os.ReadFile(generatedPath)
		require.NoError(b, err)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var analysis ServiceAnalysis
			err := json.Unmarshal(data, &analysis)
			if err != nil {
				b.Fatalf("Failed to parse JSON: %v", err)
			}
			
			// Prevent compiler optimization
			_ = len(analysis.Services)
		}
	})

	b.Run("ServiceLookup", func(b *testing.B) {
		// Load analysis once
		data, err := os.ReadFile(generatedPath)
		require.NoError(b, err)

		var analysis ServiceAnalysis
		err = json.Unmarshal(data, &analysis)
		require.NoError(b, err)

		// Create service map
		serviceMap := make(map[string]ServiceInfo)
		for _, service := range analysis.Services {
			serviceMap[service.Name] = service
		}

		testServices := []string{"s3", "ec2", "lambda", "nonexistent"}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for _, serviceName := range testServices {
				service, exists := serviceMap[serviceName]
				
				// Prevent compiler optimization
				_ = exists
				_ = service.Name
			}
		}
	})
}

// BenchmarkCacheOperations benchmarks caching behavior
func BenchmarkCacheOperations(b *testing.B) {
	if os.Getenv("RUN_CACHE_BENCHMARKS") != "true" {
		b.Skip("Skipping cache benchmarks. Set RUN_CACHE_BENCHMARKS=true to run.")
	}

	cache := NewReflectionCache()

	// Setup cache with test data
	testOperations := []discovery.OperationInfo{
		{Name: "ListBuckets", IsList: true, ResourceType: "Bucket"},
		{Name: "GetBucketLocation", IsList: false},
		{Name: "GetBucketEncryption", IsList: false},
	}

	testResourceTypes := []discovery.ResourceTypeInfo{
		{Name: "Bucket", PrimaryKey: "Name"},
		{Name: "Instance", PrimaryKey: "InstanceId"},
	}

	b.Run("CacheWrite", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			serviceName := fmt.Sprintf("service-%d", i%10)
			cache.CacheOperations(serviceName, testOperations)
			cache.CacheResourceTypes(serviceName, testResourceTypes)
		}
	})

	b.Run("CacheRead", func(b *testing.B) {
		// Pre-populate cache
		for i := 0; i < 100; i++ {
			serviceName := fmt.Sprintf("service-%d", i)
			cache.CacheOperations(serviceName, testOperations)
			cache.CacheResourceTypes(serviceName, testResourceTypes)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			serviceName := fmt.Sprintf("service-%d", i%100)
			
			operations, found1 := cache.GetOperations(serviceName)
			resourceTypes, found2 := cache.GetResourceTypes(serviceName)
			
			// Prevent compiler optimization
			_ = found1
			_ = found2
			_ = len(operations)
			_ = len(resourceTypes)
		}
	})

	b.Run("CacheExpiry", func(b *testing.B) {
		// Create cache with short expiration
		shortCache := &ReflectionCache{
			operations:    make(map[string]CachedOperations),
			resourceTypes: make(map[string]CachedResourceTypes),
			expiration:    1 * time.Millisecond,
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			serviceName := "test-service"
			
			// Cache data
			shortCache.CacheOperations(serviceName, testOperations)
			
			// Wait for expiry
			time.Sleep(2 * time.Millisecond)
			
			// Try to read (should miss due to expiry)
			_, found := shortCache.GetOperations(serviceName)
			
			// Prevent compiler optimization
			_ = found
		}
	})
}

// BenchmarkErrorHandling benchmarks error handling performance
func BenchmarkErrorHandling(b *testing.B) {
	if os.Getenv("RUN_ERROR_BENCHMARKS") != "true" {
		b.Skip("Skipping error benchmarks. Set RUN_ERROR_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	b.Run("InvalidServiceError", func(b *testing.B) {
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:      1,
			ScanTimeout:         1 * time.Second,
			EnableAutoDiscovery: false,
		}

		pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
		require.NoError(b, err)

		err = pipeline.Start()
		require.NoError(b, err)
		defer pipeline.Stop()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			scanCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			
			start := time.Now()
			_, err := pipeline.ScanService(scanCtx, "invalid-service", cfg, "us-east-1")
			duration := time.Since(start)
			
			cancel()

			if err != nil {
				b.ReportMetric(float64(duration.Nanoseconds()), "ns/error")
			}
		}
	})

	b.Run("TimeoutError", func(b *testing.B) {
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:      1,
			ScanTimeout:         1 * time.Millisecond, // Very short timeout
			EnableAutoDiscovery: false,
		}

		pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
		require.NoError(b, err)

		err = pipeline.Start()
		require.NoError(b, err)
		defer pipeline.Stop()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			shortCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
			
			start := time.Now()
			_, err := pipeline.ScanService(shortCtx, "s3", cfg, "us-east-1")
			duration := time.Since(start)
			
			cancel()

			if err != nil {
				b.ReportMetric(float64(duration.Nanoseconds()), "ns/timeout")
			}
		}
	})
}

// BenchmarkResourceProcessing benchmarks resource processing performance
func BenchmarkResourceProcessing(b *testing.B) {
	if os.Getenv("RUN_PROCESSING_BENCHMARKS") != "true" {
		b.Skip("Skipping processing benchmarks. Set RUN_PROCESSING_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	b.Run("S3ResourceProcessing", func(b *testing.B) {
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:      1,
			ScanTimeout:         30 * time.Second,
			UseResourceExplorer: false,
			BatchSize:           50,
			FlushInterval:       1 * time.Second,
			EnableAutoDiscovery: false,
			StreamingEnabled:    false,
		}

		pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
		require.NoError(b, err)

		err = pipeline.Start()
		require.NoError(b, err)
		defer pipeline.Stop()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			
			start := time.Now()
			result, err := pipeline.ScanService(scanCtx, "s3", cfg, "us-east-1")
			processingTime := time.Since(start)
			
			cancel()

			if err != nil {
				b.Logf("S3 scan failed: %v", err)
				continue
			}

			b.ReportMetric(float64(processingTime.Nanoseconds()), "ns/processing")
			
			if processingTime > 0 {
				efficiency := float64(result.Duration.Nanoseconds()) / float64(processingTime.Nanoseconds())
				b.ReportMetric(efficiency, "processing_efficiency")
			}
		}
	})
}
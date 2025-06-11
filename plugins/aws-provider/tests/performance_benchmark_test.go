//go:build integration
// +build integration

package tests

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
	awsprovider "github.com/jlgore/corkscrew/plugins/aws-provider"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/stretchr/testify/require"
)

// BenchmarkCompleteProviderPipeline benchmarks the entire provider pipeline
func BenchmarkCompleteProviderPipeline(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	
	b.Run("ProviderInitialization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			provider := awsprovider.NewAWSProvider()
			
			b.StartTimer()
			_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
			b.StopTimer()
			
			require.NoError(b, err, "Provider initialization failed")
			provider.Cleanup()
		}
	})

	// Initialize provider once for subsequent benchmarks
	provider := awsprovider.NewAWSProvider()
	_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(b, err)
	defer provider.Cleanup()

	b.Run("ServiceDiscovery", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
				ForceRefresh: i == 0, // Only refresh on first iteration
			})
			if err != nil {
				b.Fatalf("Service discovery failed: %v", err)
			}
		}
	})

	b.Run("ResourceListingSingleService", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
				Service: "s3",
			})
			if err != nil {
				b.Fatalf("Resource listing failed: %v", err)
			}
		}
	})

	b.Run("BatchScanningMultipleServices", func(b *testing.B) {
		services := []string{"s3", "ec2"}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
				Services: services,
				Region:   "us-east-1",
			})
			if err != nil {
				b.Fatalf("Batch scanning failed: %v", err)
			}
		}
	})

	b.Run("ResourceDescription", func(b *testing.B) {
		// Get some resources first
		listResp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "s3",
		})
		require.NoError(b, err)
		
		if len(listResp.Resources) == 0 {
			b.Skip("No resources available for description benchmark")
		}

		resource := listResp.Resources[0]
		resourceRef := &pb.ResourceRef{
			Service: resource.Service,
			Type:    resource.Type,
			Id:      resource.Id,
			Name:    resource.Name,
			Region:  resource.Region,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := provider.DescribeResource(ctx, &pb.DescribeResourceRequest{
				ResourceRef: resourceRef,
			})
			if err != nil {
				b.Fatalf("Resource description failed: %v", err)
			}
		}
	})
}

// BenchmarkUnifiedScannerPerformance benchmarks the unified scanner component
func BenchmarkUnifiedScannerPerformance(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	clientFactory := client.NewClientFactory(cfg)
	unifiedScanner := scanner.NewUnifiedScanner(clientFactory)

	b.Run("ServiceScanning", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := unifiedScanner.ScanService(ctx, "s3")
			if err != nil {
				b.Fatalf("Service scanning failed: %v", err)
			}
		}
	})

	b.Run("AllServicesScanning", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := unifiedScanner.ScanAllServices(ctx)
			if err != nil {
				b.Fatalf("All services scanning failed: %v", err)
			}
		}
	})

	b.Run("ResourceDescription", func(b *testing.B) {
		// Get some resources first
		resources, err := unifiedScanner.ScanService(ctx, "s3")
		require.NoError(b, err)
		
		if len(resources) == 0 {
			b.Skip("No resources available for description benchmark")
		}

		resource := resources[0]
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := unifiedScanner.DescribeResource(ctx, resource)
			if err != nil {
				b.Fatalf("Resource description failed: %v", err)
			}
		}
	})
}

// BenchmarkRuntimePipelinePerformance benchmarks the runtime pipeline
func BenchmarkRuntimePipelinePerformance(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	// Create pipeline with optimized settings for benchmarking
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      5,
		ScanTimeout:         30 * time.Second,
		UseResourceExplorer: false, // Use unified scanner for consistent benchmarking
		BatchSize:           100,
		FlushInterval:       5 * time.Second,
		EnableAutoDiscovery: false,
		ServiceFilter:       []string{"s3"},
		StreamingEnabled:    true,
	}

	pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
	require.NoError(b, err)

	err = pipeline.Start()
	require.NoError(b, err)
	defer pipeline.Stop()

	b.Run("SingleServiceScan", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := pipeline.ScanService(ctx, "s3", cfg, "us-east-1")
			if err != nil {
				b.Fatalf("Pipeline service scan failed: %v", err)
			}
		}
	})

	b.Run("BatchServiceScan", func(b *testing.B) {
		services := []string{"s3"}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := pipeline.ScanServices(ctx, services, cfg, "us-east-1")
			if err != nil {
				b.Fatalf("Pipeline batch scan failed: %v", err)
			}
		}
	})
}

// BenchmarkConcurrentOperations benchmarks concurrent operations
func BenchmarkConcurrentOperations(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	provider := awsprovider.NewAWSProvider()
	_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(b, err)
	defer provider.Cleanup()

	b.Run("ConcurrentServiceDiscovery", func(b *testing.B) {
		concurrency := runtime.NumCPU()
		b.SetParallelism(concurrency)
		b.ResetTimer()
		
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
					ForceRefresh: false, // Use cache for benchmarking
				})
				if err != nil {
					b.Fatalf("Concurrent service discovery failed: %v", err)
				}
			}
		})
	})

	b.Run("ConcurrentResourceListing", func(b *testing.B) {
		concurrency := runtime.NumCPU()
		b.SetParallelism(concurrency)
		b.ResetTimer()
		
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
					Service: "s3",
				})
				if err != nil {
					b.Fatalf("Concurrent resource listing failed: %v", err)
				}
			}
		})
	})

	b.Run("ConcurrentBatchScanning", func(b *testing.B) {
		concurrency := 2 // Limit concurrency for batch operations
		b.SetParallelism(concurrency)
		services := []string{"s3"}
		b.ResetTimer()
		
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
					Services: services,
					Region:   "us-east-1",
				})
				if err != nil {
					b.Fatalf("Concurrent batch scanning failed: %v", err)
				}
			}
		})
	})
}

// BenchmarkMemoryUsage benchmarks memory usage patterns
func BenchmarkMemoryUsage(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	b.Run("ScannerMemoryUsage", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Create new scanner for each iteration to test memory cleanup
			clientFactory := client.NewClientFactory(cfg)
			unifiedScanner := scanner.NewUnifiedScanner(clientFactory)
			
			_, err := unifiedScanner.ScanService(ctx, "s3")
			if err != nil {
				b.Fatalf("Scanner memory benchmark failed: %v", err)
			}
			
			// Force garbage collection to test memory cleanup
			if i%10 == 0 {
				runtime.GC()
			}
		}
	})

	b.Run("ProviderMemoryUsage", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Create new provider for each iteration to test memory cleanup
			provider := awsprovider.NewAWSProvider()
			_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
			if err != nil {
				b.Fatalf("Provider initialization failed: %v", err)
			}
			
			_, err = provider.ListResources(ctx, &pb.ListResourcesRequest{
				Service: "s3",
			})
			if err != nil {
				b.Fatalf("Provider memory benchmark failed: %v", err)
			}
			
			err = provider.Cleanup()
			if err != nil {
				b.Fatalf("Provider cleanup failed: %v", err)
			}
			
			// Force garbage collection to test memory cleanup
			if i%5 == 0 {
				runtime.GC()
			}
		}
	})
}

// BenchmarkScalabilityTests benchmarks scalability with increasing load
func BenchmarkScalabilityTests(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	provider := awsprovider.NewAWSProvider()
	_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(b, err)
	defer provider.Cleanup()

	// Test scalability with increasing number of services
	serviceCounts := []int{1, 2, 5, 10}
	
	for _, serviceCount := range serviceCounts {
		b.Run(fmt.Sprintf("ServiceCount_%d", serviceCount), func(b *testing.B) {
			// Create list of services to scan
			allServices := []string{"s3", "ec2", "lambda", "rds", "iam", "dynamodb", "cloudwatch", "sns", "sqs", "ecs"}
			services := allServices[:serviceCount]
			if len(allServices) < serviceCount {
				services = allServices
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
					Services: services,
					Region:   "us-east-1",
				})
				if err != nil {
					b.Fatalf("Scalability test with %d services failed: %v", serviceCount, err)
				}
			}
		})
	}
}

// BenchmarkResourceVolumeHandling benchmarks handling of large resource volumes
func BenchmarkResourceVolumeHandling(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	clientFactory := client.NewClientFactory(cfg)
	unifiedScanner := scanner.NewUnifiedScanner(clientFactory)

	b.Run("LargeResourceSetProcessing", func(b *testing.B) {
		// First, get actual resources to work with
		resources, err := unifiedScanner.ScanService(ctx, "s3")
		require.NoError(b, err)
		
		if len(resources) == 0 {
			b.Skip("No resources available for volume testing")
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Process each resource (simulate detailed configuration collection)
			for j, resource := range resources {
				// Limit to first 100 resources to avoid extremely long benchmarks
				if j >= 100 {
					break
				}
				
				_, err := unifiedScanner.DescribeResource(ctx, resource)
				if err != nil {
					// Some failures are expected due to permissions
					continue
				}
			}
		}
	})

	b.Run("StreamingLargeResultSets", func(b *testing.B) {
		resourceChan := make(chan *pb.Resource, 1000)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Start streaming in goroutine
			go func() {
				err := unifiedScanner.StreamScanResources(ctx, []string{"s3"}, resourceChan)
				if err != nil {
					b.Logf("Streaming failed: %v", err)
				}
			}()
			
			// Consume streamed resources
			resourceCount := 0
			timeout := time.After(30 * time.Second)
			
		consumeLoop:
			for {
				select {
				case resource, ok := <-resourceChan:
					if !ok {
						break consumeLoop
					}
					if resource != nil {
						resourceCount++
					}
				case <-timeout:
					break consumeLoop
				}
			}
			
			b.Logf("Streamed %d resources in iteration %d", resourceCount, i)
		}
	})
}

// BenchmarkErrorHandlingOverhead benchmarks error handling overhead
func BenchmarkErrorHandlingOverhead(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	provider := awsprovider.NewAWSProvider()
	_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(b, err)
	defer provider.Cleanup()

	b.Run("InvalidServiceHandling", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
				Service: "nonexistent-service",
			})
			// Error or empty result is expected
			_ = err
		}
	})

	b.Run("TimeoutHandling", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Create very short timeout context
			shortCtx, cancel := context.WithTimeout(ctx, 1*time.Nanosecond)
			_, err := provider.ListResources(shortCtx, &pb.ListResourcesRequest{
				Service: "s3",
			})
			cancel()
			// Timeout error is expected
			_ = err
		}
	})
}

// Helper function to run performance comparison tests
func runPerformanceComparison(b *testing.B, name string, operation func() error) {
	b.Run(name, func(b *testing.B) {
		var totalDuration time.Duration
		var minDuration = time.Hour
		var maxDuration time.Duration
		var successCount int

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			start := time.Now()
			err := operation()
			duration := time.Since(start)
			
			if err == nil {
				successCount++
				totalDuration += duration
				if duration < minDuration {
					minDuration = duration
				}
				if duration > maxDuration {
					maxDuration = duration
				}
			}
		}
		
		if successCount > 0 {
			avgDuration := totalDuration / time.Duration(successCount)
			b.ReportMetric(float64(avgDuration.Nanoseconds()), "avg_ns/op")
			b.ReportMetric(float64(minDuration.Nanoseconds()), "min_ns/op")
			b.ReportMetric(float64(maxDuration.Nanoseconds()), "max_ns/op")
			b.ReportMetric(float64(successCount)/float64(b.N)*100, "success_rate_%")
		}
	})
}

// BenchmarkDetailedMetrics provides detailed performance metrics
func BenchmarkDetailedMetrics(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	provider := awsprovider.NewAWSProvider()
	_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(b, err)
	defer provider.Cleanup()

	runPerformanceComparison(b, "ServiceDiscoveryMetrics", func() error {
		_, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
			ForceRefresh: false,
		})
		return err
	})

	runPerformanceComparison(b, "ResourceListingMetrics", func() error {
		_, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "s3",
		})
		return err
	})

	runPerformanceComparison(b, "BatchScanningMetrics", func() error {
		_, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
			Services: []string{"s3"},
			Region:   "us-east-1",
		})
		return err
	})
}
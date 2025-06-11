//go:build integration
// +build integration

package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestCompleteAWSProviderPipeline tests the entire AWS provider pipeline end-to-end
func TestCompleteAWSProviderPipeline(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	
	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err, "Failed to load AWS config")

	t.Run("FullProviderWorkflow", func(t *testing.T) {
		// Create and initialize AWS provider
		provider := awsprovider.NewAWSProvider()
		
		// Initialize provider
		initResp, err := provider.Initialize(ctx, &pb.InitializeRequest{})
		require.NoError(t, err, "Provider initialization failed")
		assert.True(t, initResp.Success, "Provider initialization was not successful")
		assert.Equal(t, "3.0.0", initResp.Version)
		
		// Verify metadata
		assert.Contains(t, initResp.Metadata, "region")
		assert.Contains(t, initResp.Metadata, "scanner_mode")
		assert.Equal(t, "unified_only", initResp.Metadata["scanner_mode"])
		
		// Test service discovery
		discoverResp, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
			ForceRefresh: true,
		})
		require.NoError(t, err, "Service discovery failed")
		assert.Greater(t, len(discoverResp.Services), 0, "No services discovered")
		
		// Verify common services are discovered
		serviceNames := make([]string, len(discoverResp.Services))
		for i, svc := range discoverResp.Services {
			serviceNames[i] = svc.Name
		}
		assert.Contains(t, serviceNames, "s3", "S3 service not discovered")
		assert.Contains(t, serviceNames, "ec2", "EC2 service not discovered")
		
		// Test resource listing for specific service
		listResp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "s3",
		})
		require.NoError(t, err, "Resource listing failed")
		assert.NotNil(t, listResp.Resources)
		assert.Contains(t, listResp.Metadata, "resource_count")
		
		// Test batch scanning
		batchResp, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
			Services: []string{"s3", "ec2"},
			Region:   "us-east-1",
		})
		require.NoError(t, err, "Batch scan failed")
		assert.NotNil(t, batchResp.Resources)
		assert.NotNil(t, batchResp.Stats)
		assert.Greater(t, batchResp.Stats.TotalResources, int32(0))
		
		// Test resource description if we have resources
		if len(batchResp.Resources) > 0 {
			resource := batchResp.Resources[0]
			resourceRef := &pb.ResourceRef{
				Service: resource.Service,
				Type:    resource.Type,
				Id:      resource.Id,
				Name:    resource.Name,
				Region:  resource.Region,
			}
			
			descResp, err := provider.DescribeResource(ctx, &pb.DescribeResourceRequest{
				ResourceRef: resourceRef,
			})
			require.NoError(t, err, "Resource description failed")
			assert.NotNil(t, descResp.Resource)
			assert.Equal(t, resource.Id, descResp.Resource.Id)
		}
		
		// Test schema generation
		schemaResp, err := provider.GetSchemas(ctx, &pb.GetSchemasRequest{
			Services: []string{"s3", "ec2"},
		})
		require.NoError(t, err, "Schema generation failed")
		assert.NotNil(t, schemaResp.Tables)
		
		// Test streaming scan
		streamResp := &mockStreamScanServer{
			resources: make([]*pb.Resource, 0),
			ctx:       ctx,
		}
		
		err = provider.StreamScan(&pb.StreamScanRequest{
			Services: []string{"s3"},
		}, streamResp)
		require.NoError(t, err, "Stream scan failed")
		
		// Cleanup
		err = provider.Cleanup()
		assert.NoError(t, err, "Provider cleanup failed")
	})
}

// TestDiscoveryPipeline tests the service discovery pipeline
func TestDiscoveryPipeline(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err)

	t.Run("RuntimeServiceDiscovery", func(t *testing.T) {
		// Test runtime service discovery
		runtimeDiscovery := discovery.NewRuntimeServiceDiscovery(cfg)
		
		services, err := runtimeDiscovery.DiscoverServices(ctx)
		require.NoError(t, err, "Runtime service discovery failed")
		assert.Greater(t, len(services), 0, "No services discovered by runtime discovery")
		
		// Verify service structure
		for _, service := range services {
			assert.NotEmpty(t, service.Name, "Service name is empty")
			assert.NotEmpty(t, service.DisplayName, "Service display name is empty")
			assert.NotEmpty(t, service.Version, "Service version is empty")
		}
	})

	t.Run("ServiceAnalysisGeneration", func(t *testing.T) {
		// Test that analysis files are generated during discovery
		clientFactory := client.NewClientFactory(cfg)
		
		// Create a mock client factory adapter
		adapter := &mockClientFactoryAdapter{factory: clientFactory}
		
		runtimeDiscovery := discovery.NewRuntimeServiceDiscovery(cfg)
		runtimeDiscovery.SetClientFactory(adapter)
		
		// Enable analysis generation
		runtimeDiscovery.EnableAnalysisGeneration(true)
		
		services, err := runtimeDiscovery.DiscoverServices(ctx)
		require.NoError(t, err, "Service discovery with analysis failed")
		assert.Greater(t, len(services), 0, "No services discovered")
		
		// Check that analysis files were generated
		generatedDir := "generated"
		if _, err := os.Stat(generatedDir); err == nil {
			// Count analysis files
			files, err := filepath.Glob(filepath.Join(generatedDir, "*_final.json"))
			if err == nil && len(files) > 0 {
				t.Logf("Found %d analysis files in %s", len(files), generatedDir)
				
				// Verify structure of one analysis file
				analysisData, err := os.ReadFile(files[0])
				if err == nil {
					var analysis map[string]interface{}
					err = json.Unmarshal(analysisData, &analysis)
					assert.NoError(t, err, "Failed to parse analysis file")
					assert.Contains(t, analysis, "services", "Analysis file missing services")
				}
			}
		}
	})
}

// TestScanningPipeline tests the scanning pipeline components
func TestScanningPipeline(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err)

	t.Run("UnifiedScannerIntegration", func(t *testing.T) {
		// Test unified scanner directly
		clientFactory := client.NewClientFactory(cfg)
		unifiedScanner := scanner.NewUnifiedScanner(clientFactory)
		
		// Test service scanning
		resources, err := unifiedScanner.ScanService(ctx, "s3")
		require.NoError(t, err, "Unified scanner service scan failed")
		
		// Verify resource structure
		if len(resources) > 0 {
			resource := resources[0]
			assert.NotEmpty(t, resource.Id, "Resource ID is empty")
			assert.NotEmpty(t, resource.Service, "Resource service is empty")
			assert.NotEmpty(t, resource.Type, "Resource type is empty")
			assert.Equal(t, "s3", resource.Service, "Incorrect service name")
		}
		
		// Test resource description
		if len(resources) > 0 {
			detailedResource, err := unifiedScanner.DescribeResource(ctx, resources[0])
			require.NoError(t, err, "Resource description failed")
			assert.NotNil(t, detailedResource)
			assert.Equal(t, "aws", detailedResource.Provider)
			assert.NotNil(t, detailedResource.DiscoveredAt)
		}
	})

	t.Run("RuntimePipelineIntegration", func(t *testing.T) {
		// Test runtime pipeline
		pipelineConfig := &runtime.PipelineConfig{
			MaxConcurrency:      3,
			ScanTimeout:         30 * time.Second,
			UseResourceExplorer: false, // Use unified scanner for testing
			BatchSize:           10,
			FlushInterval:       5 * time.Second,
			EnableAutoDiscovery: false,
			ServiceFilter:       []string{"s3"},
			StreamingEnabled:    true,
		}
		
		pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
		require.NoError(t, err, "Pipeline creation failed")
		
		// Start pipeline
		err = pipeline.Start()
		require.NoError(t, err, "Pipeline start failed")
		defer pipeline.Stop()
		
		// Test service scanning
		result, err := pipeline.ScanService(ctx, "s3", cfg, "us-east-1")
		require.NoError(t, err, "Pipeline service scan failed")
		
		assert.Equal(t, "s3", result.Service)
		assert.Equal(t, "us-east-1", result.Region)
		assert.NotZero(t, result.Duration)
		assert.NotNil(t, result.Resources)
		
		// Test batch scanning
		batchResult, err := pipeline.ScanServices(ctx, []string{"s3"}, cfg, "us-east-1")
		require.NoError(t, err, "Pipeline batch scan failed")
		
		assert.Equal(t, 1, batchResult.ServicesScanned)
		assert.Contains(t, batchResult.ServiceStats, "s3")
		assert.NotZero(t, batchResult.Duration)
		
		// Test pipeline statistics
		stats := pipeline.GetStats()
		assert.True(t, stats.IsRunning)
		assert.Contains(t, stats.RegisteredServices, "s3")
	})
}

// TestPerformanceAndReliability tests performance and reliability aspects
func TestPerformanceAndReliability(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err)

	t.Run("ConcurrentScanning", func(t *testing.T) {
		// Test concurrent scanning performance
		clientFactory := client.NewClientFactory(cfg)
		scanner := scanner.NewUnifiedScanner(clientFactory)
		
		services := []string{"s3", "ec2", "lambda"}
		results := make(chan scanResult, len(services))
		
		// Start concurrent scans
		for _, service := range services {
			go func(svc string) {
				start := time.Now()
				resources, err := scanner.ScanService(ctx, svc)
				results <- scanResult{
					service:   svc,
					resources: len(resources),
					duration:  time.Since(start),
					err:       err,
				}
			}(service)
		}
		
		// Collect results
		successCount := 0
		totalDuration := time.Duration(0)
		
		for i := 0; i < len(services); i++ {
			select {
			case result := <-results:
				if result.err == nil {
					successCount++
					totalDuration += result.duration
					t.Logf("Service %s: %d resources in %v", result.service, result.resources, result.duration)
				} else {
					t.Logf("Service %s failed: %v", result.service, result.err)
				}
			case <-time.After(2 * time.Minute):
				t.Fatal("Concurrent scanning timeout")
			}
		}
		
		assert.Greater(t, successCount, 0, "No concurrent scans succeeded")
		avgDuration := totalDuration / time.Duration(successCount)
		t.Logf("Average scan duration: %v", avgDuration)
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		// Test error handling with invalid configurations
		provider := awsprovider.NewAWSProvider()
		
		// Test uninitialized provider
		_, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "s3",
		})
		assert.Error(t, err, "Expected error for uninitialized provider")
		assert.Contains(t, err.Error(), "not initialized")
		
		// Initialize provider
		_, err = provider.Initialize(ctx, &pb.InitializeRequest{})
		require.NoError(t, err)
		
		// Test invalid service
		resp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "invalid-service",
		})
		// Should not fail hard - should return empty results or graceful error
		if err != nil {
			t.Logf("Invalid service returned error (expected): %v", err)
		} else {
			assert.NotNil(t, resp.Resources)
		}
	})

	t.Run("ResourceExplorerFallback", func(t *testing.T) {
		// Test fallback behavior when Resource Explorer is not available
		clientFactory := client.NewClientFactory(cfg)
		scanner := scanner.NewUnifiedScanner(clientFactory)
		
		// Don't set Resource Explorer - should fall back to SDK scanning
		resources, err := scanner.ScanService(ctx, "s3")
		require.NoError(t, err, "SDK fallback scan failed")
		
		// Should still get results via SDK
		if len(resources) > 0 {
			assert.Equal(t, "s3", resources[0].Service)
		}
	})
}

// TestDataIntegrity tests data integrity and consistency
func TestDataIntegrity(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err)

	t.Run("ResourceReferenceConsistency", func(t *testing.T) {
		// Test that resource references are consistent across scan and describe
		clientFactory := client.NewClientFactory(cfg)
		scanner := scanner.NewUnifiedScanner(clientFactory)
		
		// Scan for resources
		resourceRefs, err := scanner.ScanService(ctx, "s3")
		require.NoError(t, err, "Resource scan failed")
		
		if len(resourceRefs) > 0 {
			ref := resourceRefs[0]
			
			// Describe the resource
			resource, err := scanner.DescribeResource(ctx, ref)
			require.NoError(t, err, "Resource description failed")
			
			// Verify consistency
			assert.Equal(t, ref.Id, resource.Id, "Resource ID mismatch")
			assert.Equal(t, ref.Service, resource.Service, "Service mismatch")
			assert.Equal(t, ref.Type, resource.Type, "Resource type mismatch")
			assert.Equal(t, ref.Region, resource.Region, "Region mismatch")
			
			// Verify resource has proper timestamps
			assert.NotNil(t, resource.DiscoveredAt, "Missing discovery timestamp")
			
			// Verify provider is set correctly
			assert.Equal(t, "aws", resource.Provider, "Incorrect provider")
		}
	})

	t.Run("ConfigurationDataValidation", func(t *testing.T) {
		// Test that configuration data is properly collected and valid
		provider := awsprovider.NewAWSProvider()
		
		// Initialize provider
		_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
		require.NoError(t, err)
		
		// Get resources
		listResp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "s3",
		})
		require.NoError(t, err)
		
		if len(listResp.Resources) > 0 {
			resource := listResp.Resources[0]
			
			// Verify resource has required fields
			assert.NotEmpty(t, resource.Id, "Resource ID is empty")
			assert.NotEmpty(t, resource.Service, "Resource service is empty")
			assert.NotEmpty(t, resource.Type, "Resource type is empty")
			
			// If raw data exists, verify it's valid JSON
			if resource.RawData != "" {
				var rawDataMap map[string]interface{}
				err := json.Unmarshal([]byte(resource.RawData), &rawDataMap)
				assert.NoError(t, err, "Raw data is not valid JSON")
			}
			
			// If attributes exist, verify it's valid JSON
			if resource.Attributes != "" {
				var attributesMap map[string]interface{}
				err := json.Unmarshal([]byte(resource.Attributes), &attributesMap)
				assert.NoError(t, err, "Attributes are not valid JSON")
			}
			
			// Verify tags are properly formatted
			assert.NotNil(t, resource.Tags, "Tags map is nil")
		}
	})
}

// Helper types and functions

type scanResult struct {
	service   string
	resources int
	duration  time.Duration
	err       error
}

type mockStreamScanServer struct {
	resources []*pb.Resource
	ctx       context.Context
}

func (m *mockStreamScanServer) Send(resource *pb.Resource) error {
	m.resources = append(m.resources, resource)
	return nil
}

func (m *mockStreamScanServer) Context() context.Context {
	return m.ctx
}

type mockClientFactoryAdapter struct {
	factory *client.ClientFactory
}

func (m *mockClientFactoryAdapter) GetClient(serviceName string) interface{} {
	return m.factory.GetClient(serviceName)
}

func (m *mockClientFactoryAdapter) GetAvailableServices() []string {
	return m.factory.GetAvailableServices()
}

// BenchmarkEndToEndPipeline benchmarks the complete pipeline
func BenchmarkEndToEndPipeline(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	provider := awsprovider.NewAWSProvider()
	_, err = provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(b, err)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "s3",
		})
		if err != nil {
			b.Fatalf("Benchmark iteration %d failed: %v", i, err)
		}
	}
}

// BenchmarkConcurrentScanning benchmarks concurrent scanning
func BenchmarkConcurrentScanning(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(b, err)

	clientFactory := client.NewClientFactory(cfg)
	scanner := scanner.NewUnifiedScanner(clientFactory)
	services := []string{"s3", "ec2", "lambda"}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		results := make(chan error, len(services))
		
		for _, service := range services {
			go func(svc string) {
				_, err := scanner.ScanService(ctx, svc)
				results <- err
			}(service)
		}
		
		for j := 0; j < len(services); j++ {
			<-results
		}
	}
}
//go:build integration
// +build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
	awsprovider "github.com/jlgore/corkscrew/plugins/aws-provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompleteProviderWorkflow tests the entire provider workflow from initialization to cleanup
func TestCompleteProviderWorkflow(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	provider := awsprovider.NewAWSProvider()

	t.Run("ProviderLifecycle", func(t *testing.T) {
		// Phase 1: Provider Information (before initialization)
		t.Run("Phase1_ProviderInfo", func(t *testing.T) {
			info, err := provider.GetProviderInfo(ctx, &pb.Empty{})
			require.NoError(t, err, "Failed to get provider info")
			
			assert.Equal(t, "aws-v3", info.Name)
			assert.Equal(t, "3.0.0", info.Version)
			assert.Contains(t, info.Description, "AWS cloud provider")
			
			// Verify capabilities
			assert.Equal(t, "true", info.Capabilities["discovery"])
			assert.Equal(t, "true", info.Capabilities["scanning"])
			assert.Equal(t, "true", info.Capabilities["streaming"])
			assert.Equal(t, "true", info.Capabilities["multi_region"])
		})

		// Phase 2: Provider Initialization
		t.Run("Phase2_Initialization", func(t *testing.T) {
			initResp, err := provider.Initialize(ctx, &pb.InitializeRequest{})
			require.NoError(t, err, "Provider initialization failed")
			
			assert.True(t, initResp.Success, "Initialization was not successful")
			assert.Equal(t, "3.0.0", initResp.Version)
			
			// Verify metadata contains expected keys
			expectedMetadataKeys := []string{
				"region", "resource_explorer", "max_concurrency", 
				"runtime_pipeline", "discovered_services", "scanner_mode", "analysis_generation",
			}
			for _, key := range expectedMetadataKeys {
				assert.Contains(t, initResp.Metadata, key, "Missing metadata key: %s", key)
			}
			
			// Verify specific values
			assert.Equal(t, "unified_only", initResp.Metadata["scanner_mode"])
			assert.Equal(t, "enabled", initResp.Metadata["analysis_generation"])
		})

		// Phase 3: Service Discovery
		var discoveredServices []*pb.ServiceInfo
		t.Run("Phase3_ServiceDiscovery", func(t *testing.T) {
			discoverResp, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
				ForceRefresh: true,
			})
			require.NoError(t, err, "Service discovery failed")
			
			assert.Greater(t, len(discoverResp.Services), 0, "No services discovered")
			assert.NotNil(t, discoverResp.DiscoveredAt)
			assert.NotEmpty(t, discoverResp.SdkVersion)
			
			discoveredServices = discoverResp.Services
			
			// Verify service structure
			for _, service := range discoverResp.Services {
				assert.NotEmpty(t, service.Name, "Service name is empty")
				assert.NotEmpty(t, service.DisplayName, "Service display name is empty")
				assert.NotEmpty(t, service.Version, "Service version is empty")
			}
			
			// Verify common AWS services are discovered
			serviceNames := make(map[string]bool)
			for _, svc := range discoverResp.Services {
				serviceNames[svc.Name] = true
			}
			
			expectedServices := []string{"s3", "ec2"}
			for _, expected := range expectedServices {
				assert.True(t, serviceNames[expected], "Expected service %s not discovered", expected)
			}
			
			t.Logf("Discovered %d services", len(discoverResp.Services))
		})

		// Phase 4: Resource Listing and Scanning
		t.Run("Phase4_ResourceOperations", func(t *testing.T) {
			if len(discoveredServices) == 0 {
				t.Skip("No services discovered, skipping resource operations")
			}

			// Test single service resource listing
			t.Run("SingleServiceListing", func(t *testing.T) {
				listResp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
					Service: "s3",
				})
				require.NoError(t, err, "S3 resource listing failed")
				
				assert.NotNil(t, listResp.Resources)
				assert.Contains(t, listResp.Metadata, "resource_count")
				assert.Contains(t, listResp.Metadata, "scan_time")
				assert.Contains(t, listResp.Metadata, "method")
				
				t.Logf("S3 listing found %d resources", len(listResp.Resources))
			})

			// Test all services resource listing
			t.Run("AllServicesListing", func(t *testing.T) {
				listResp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{})
				require.NoError(t, err, "All services resource listing failed")
				
				assert.NotNil(t, listResp.Resources)
				t.Logf("All services listing found %d resources", len(listResp.Resources))
			})

			// Test batch scanning
			t.Run("BatchScanning", func(t *testing.T) {
				testServices := []string{"s3", "ec2"}
				batchResp, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
					Services: testServices,
					Region:   "us-east-1",
				})
				require.NoError(t, err, "Batch scan failed")
				
				assert.NotNil(t, batchResp.Resources)
				assert.NotNil(t, batchResp.Stats)
				assert.GreaterOrEqual(t, batchResp.Stats.TotalResources, int32(0))
				
				// Verify stats structure
				assert.NotNil(t, batchResp.Stats.ServiceCounts)
				assert.NotNil(t, batchResp.Stats.ResourceCounts)
				assert.Greater(t, batchResp.Stats.DurationMs, int64(0))
				
				t.Logf("Batch scan completed in %dms with %d resources", 
					batchResp.Stats.DurationMs, batchResp.Stats.TotalResources)
			})
		})

		// Phase 5: Resource Description and Enrichment
		t.Run("Phase5_ResourceEnrichment", func(t *testing.T) {
			// Get some resources first
			listResp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
				Service: "s3",
			})
			require.NoError(t, err)
			
			if len(listResp.Resources) == 0 {
				t.Skip("No S3 resources found for enrichment test")
			}

			resource := listResp.Resources[0]
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
			assert.Equal(t, "aws", descResp.Resource.Provider)
			assert.NotNil(t, descResp.Resource.DiscoveredAt)
			
			// Verify enriched data
			if descResp.Resource.RawData != "" {
				var rawData map[string]interface{}
				err := json.Unmarshal([]byte(descResp.Resource.RawData), &rawData)
				assert.NoError(t, err, "Raw data is not valid JSON")
				t.Logf("Resource has %d configuration entries", len(rawData))
			}
			
			if descResp.Resource.Attributes != "" {
				var attributes map[string]interface{}
				err := json.Unmarshal([]byte(descResp.Resource.Attributes), &attributes)
				assert.NoError(t, err, "Attributes are not valid JSON")
				t.Logf("Resource has %d structured attributes", len(attributes))
			}
		})

		// Phase 6: Streaming Operations
		t.Run("Phase6_StreamingOperations", func(t *testing.T) {
			streamServer := &testStreamScanServer{
				resources: make([]*pb.Resource, 0),
				ctx:       ctx,
			}

			// Start streaming in goroutine
			streamErr := make(chan error, 1)
			go func() {
				err := provider.StreamScan(&pb.StreamScanRequest{
					Services: []string{"s3"},
				}, streamServer)
				streamErr <- err
			}()

			// Wait for streaming to complete or timeout
			select {
			case err := <-streamErr:
				assert.NoError(t, err, "Stream scan failed")
			case <-time.After(30 * time.Second):
				t.Log("Stream scan timeout - may be expected for large accounts")
			}

			t.Logf("Streaming collected %d resources", len(streamServer.resources))
		})

		// Phase 7: Schema Operations
		t.Run("Phase7_SchemaOperations", func(t *testing.T) {
			schemaResp, err := provider.GetSchemas(ctx, &pb.GetSchemasRequest{
				Services: []string{"s3", "ec2"},
			})
			require.NoError(t, err, "Schema generation failed")
			
			assert.NotNil(t, schemaResp.Tables)
			assert.Greater(t, len(schemaResp.Tables), 0, "No schema tables generated")
			
			// Verify schema structure
			for _, table := range schemaResp.Tables {
				assert.NotEmpty(t, table.Name, "Table name is empty")
				assert.NotEmpty(t, table.Service, "Table service is empty")
				assert.Greater(t, len(table.Columns), 0, "Table has no columns")
				
				// Verify column structure
				for _, column := range table.Columns {
					assert.NotEmpty(t, column.Name, "Column name is empty")
					assert.NotEmpty(t, column.Type, "Column type is empty")
				}
			}
			
			t.Logf("Generated schemas for %d tables", len(schemaResp.Tables))
		})

		// Phase 8: Analysis and Discovery Configuration
		t.Run("Phase8_AdvancedFeatures", func(t *testing.T) {
			// Test discovery configuration
			configResp, err := provider.ConfigureDiscovery(ctx, &pb.ConfigureDiscoveryRequest{
				Sources: []*pb.DiscoverySource{
					{
						SourceType: "api",
						Config: map[string]string{
							"endpoint": "https://api.aws.amazon.com",
						},
					},
				},
			})
			require.NoError(t, err, "Discovery configuration failed")
			
			// Should either succeed or fail gracefully
			if configResp.Success {
				assert.Greater(t, len(configResp.ConfiguredSources), 0, "No sources configured")
				t.Logf("Configured %d discovery sources", len(configResp.ConfiguredSources))
			} else {
				t.Logf("Discovery configuration failed as expected: %s", configResp.Error)
			}

			// Test analysis of discovered data
			analysisResp, err := provider.AnalyzeDiscoveredData(ctx, &pb.AnalyzeRequest{
				SourceType: "runtime",
			})
			require.NoError(t, err, "Data analysis failed")
			
			if analysisResp.Success {
				assert.Greater(t, len(analysisResp.Services), 0, "No services in analysis")
				assert.Greater(t, len(analysisResp.Resources), 0, "No resources in analysis")
				assert.Greater(t, len(analysisResp.Operations), 0, "No operations in analysis")
				t.Logf("Analysis found %d services, %d resources, %d operations",
					len(analysisResp.Services), len(analysisResp.Resources), len(analysisResp.Operations))
			}
		})

		// Phase 9: Cleanup
		t.Run("Phase9_Cleanup", func(t *testing.T) {
			err := provider.Cleanup()
			assert.NoError(t, err, "Provider cleanup failed")
		})
	})
}

// TestProviderConcurrentOperations tests concurrent operations on the provider
func TestProviderConcurrentOperations(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	provider := awsprovider.NewAWSProvider()

	// Initialize provider
	_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(t, err, "Provider initialization failed")
	defer provider.Cleanup()

	t.Run("ConcurrentServiceDiscovery", func(t *testing.T) {
		// Test concurrent service discovery calls
		concurrency := 3
		results := make(chan discoverResult, concurrency)
		
		for i := 0; i < concurrency; i++ {
			go func(id int) {
				start := time.Now()
				resp, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
					ForceRefresh: id == 0, // Only first call forces refresh
				})
				results <- discoverResult{
					id:       id,
					services: len(resp.Services),
					duration: time.Since(start),
					err:      err,
				}
			}(i)
		}

		// Collect results
		for i := 0; i < concurrency; i++ {
			select {
			case result := <-results:
				assert.NoError(t, result.err, "Concurrent discovery %d failed", result.id)
				assert.Greater(t, result.services, 0, "Concurrent discovery %d found no services", result.id)
				t.Logf("Discovery %d: %d services in %v", result.id, result.services, result.duration)
			case <-time.After(60 * time.Second):
				t.Fatal("Concurrent discovery timeout")
			}
		}
	})

	t.Run("ConcurrentResourceListing", func(t *testing.T) {
		// Test concurrent resource listing for different services
		services := []string{"s3", "ec2", "lambda"}
		results := make(chan listResult, len(services))
		
		for _, service := range services {
			go func(svc string) {
				start := time.Now()
				resp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
					Service: svc,
				})
				results <- listResult{
					service:   svc,
					resources: len(resp.Resources),
					duration:  time.Since(start),
					err:       err,
				}
			}(service)
		}

		// Collect results
		for i := 0; i < len(services); i++ {
			select {
			case result := <-results:
				if result.err != nil {
					t.Logf("Service %s listing failed (may be expected): %v", result.service, result.err)
				} else {
					t.Logf("Service %s: %d resources in %v", result.service, result.resources, result.duration)
				}
			case <-time.After(60 * time.Second):
				t.Fatal("Concurrent listing timeout")
			}
		}
	})

	t.Run("ConcurrentBatchScanning", func(t *testing.T) {
		// Test concurrent batch scanning operations
		concurrency := 2
		results := make(chan batchResult, concurrency)
		
		for i := 0; i < concurrency; i++ {
			go func(id int) {
				start := time.Now()
				resp, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
					Services: []string{"s3"},
					Region:   "us-east-1",
				})
				
				resources := 0
				if resp != nil && resp.Stats != nil {
					resources = int(resp.Stats.TotalResources)
				}
				
				results <- batchResult{
					id:        id,
					resources: resources,
					duration:  time.Since(start),
					err:       err,
				}
			}(i)
		}

		// Collect results
		for i := 0; i < concurrency; i++ {
			select {
			case result := <-results:
				assert.NoError(t, result.err, "Concurrent batch scan %d failed", result.id)
				t.Logf("Batch scan %d: %d resources in %v", result.id, result.resources, result.duration)
			case <-time.After(120 * time.Second):
				t.Fatal("Concurrent batch scan timeout")
			}
		}
	})
}

// TestProviderErrorScenarios tests various error scenarios
func TestProviderErrorScenarios(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()

	t.Run("UninitializedProviderOperations", func(t *testing.T) {
		// Test operations on uninitialized provider
		provider := awsprovider.NewAWSProvider()
		
		// These should all fail with "not initialized" errors
		_, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not initialized")
		
		_, err = provider.ListResources(ctx, &pb.ListResourcesRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not initialized")
		
		_, err = provider.BatchScan(ctx, &pb.BatchScanRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not initialized")
	})

	t.Run("InvalidRequestParameters", func(t *testing.T) {
		provider := awsprovider.NewAWSProvider()
		_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
		require.NoError(t, err)
		defer provider.Cleanup()

		// Test invalid service name
		resp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "nonexistent-service",
		})
		// Should not crash - either return empty results or graceful error
		if err != nil {
			t.Logf("Invalid service returned error (expected): %v", err)
		} else {
			assert.NotNil(t, resp.Resources)
		}

		// Test empty resource reference
		descResp, err := provider.DescribeResource(ctx, &pb.DescribeResourceRequest{
			ResourceRef: nil,
		})
		if err == nil {
			assert.NotEmpty(t, descResp.Error, "Expected error for nil resource reference")
		}
	})

	t.Run("NetworkTimeoutScenarios", func(t *testing.T) {
		provider := awsprovider.NewAWSProvider()
		_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
		require.NoError(t, err)
		defer provider.Cleanup()

		// Test with very short timeout
		shortCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
		defer cancel()

		_, err = provider.ListResources(shortCtx, &pb.ListResourcesRequest{
			Service: "s3",
		})
		// Should get timeout error or succeed very quickly
		if err != nil {
			assert.True(t, 
				strings.Contains(err.Error(), "timeout") || 
				strings.Contains(err.Error(), "context") ||
				strings.Contains(err.Error(), "deadline"),
				"Expected timeout-related error, got: %v", err)
		}
	})
}

// Helper types and structs for concurrent testing

type discoverResult struct {
	id       int
	services int
	duration time.Duration
	err      error
}

type listResult struct {
	service   string
	resources int
	duration  time.Duration
	err       error
}

type batchResult struct {
	id        int
	resources int
	duration  time.Duration
	err       error
}

type testStreamScanServer struct {
	resources []*pb.Resource
	ctx       context.Context
	mu        sync.Mutex
}

func (s *testStreamScanServer) Send(resource *pb.Resource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources = append(s.resources, resource)
	return nil
}

func (s *testStreamScanServer) Context() context.Context {
	return s.ctx
}

// BenchmarkProviderOperations benchmarks key provider operations
func BenchmarkProviderOperations(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	ctx := context.Background()
	provider := awsprovider.NewAWSProvider()
	
	// Initialize once
	_, err := provider.Initialize(ctx, &pb.InitializeRequest{})
	require.NoError(b, err)
	defer provider.Cleanup()

	b.Run("ServiceDiscovery", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
				ForceRefresh: false, // Use cache for benchmarking
			})
			if err != nil {
				b.Fatalf("Service discovery failed: %v", err)
			}
		}
	})

	b.Run("ResourceListing", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
				Service: "s3",
			})
			if err != nil {
				b.Fatalf("Resource listing failed: %v", err)
			}
		}
	})

	b.Run("BatchScanning", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
				Services: []string{"s3"},
				Region:   "us-east-1",
			})
			if err != nil {
				b.Fatalf("Batch scanning failed: %v", err)
			}
		}
	})
}
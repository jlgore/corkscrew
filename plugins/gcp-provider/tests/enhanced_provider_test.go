package tests

import (
	"context"
	"testing"
	"time"
	
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnhancedProviderIntegration tests the enhanced GCP provider
func TestEnhancedProviderIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}
	
	ctx := context.Background()
	provider := NewGCPProvider()
	
	// Test initialization with database
	t.Run("Initialize with Enhanced Features", func(t *testing.T) {
		config := map[string]string{
			"database_path": "./test_gcp_resources.db",
			"enable_resource_graph": "true",
		}
		
		resp, err := provider.Initialize(ctx, &pb.InitializeRequest{
			Config: config,
		})
		
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "2.0.0", resp.Version)
		assert.Equal(t, "true", resp.Metadata["dynamic_discovery"])
		assert.Equal(t, "true", resp.Metadata["database_enabled"])
		
		// Cleanup
		defer provider.Cleanup()
	})
	
	// Test dynamic discovery
	t.Run("Dynamic Service Discovery", func(t *testing.T) {
		// Ensure provider is initialized
		if !provider.initialized {
			provider.Initialize(ctx, &pb.InitializeRequest{})
		}
		
		resp, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
			ForceRefresh: true,
		})
		
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Services)
		assert.Equal(t, "enhanced-discovery-v2", resp.SdkVersion)
		assert.Equal(t, "dynamic_asset_inventory", resp.Metadata["discovery_method"])
		
		// Should discover more than the hardcoded list
		t.Logf("Discovered %d services dynamically", len(resp.Services))
		
		// Check for common services
		serviceMap := make(map[string]bool)
		for _, svc := range resp.Services {
			serviceMap[svc.Name] = true
		}
		
		// These should always be discovered
		assert.True(t, serviceMap["compute"] || serviceMap["storage"])
	})
	
	// Test resource graph query
	t.Run("Resource Graph Query", func(t *testing.T) {
		if provider.resourceGraph == nil {
			t.Skip("Resource graph not available")
		}
		
		resp, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
			Service: "compute",
		})
		
		require.NoError(t, err)
		assert.Equal(t, "resource_graph", resp.Metadata["method"])
		
		// Resources should be returned
		if len(resp.Resources) > 0 {
			// Check resource has required fields
			resource := resp.Resources[0]
			assert.NotEmpty(t, resource.Id)
			assert.NotEmpty(t, resource.Type)
			assert.Equal(t, "compute", resource.Service)
		}
	})
	
	// Test dynamic schema generation
	t.Run("Dynamic Schema Generation", func(t *testing.T) {
		resp, err := provider.GetSchemas(ctx, &pb.GetSchemasRequest{
			Services: []string{"compute", "storage"},
		})
		
		require.NoError(t, err)
		assert.NotEmpty(t, resp.UnifiedSchema)
		assert.NotEmpty(t, resp.ServiceSchemas)
		
		// Should have schemas for requested services
		assert.Contains(t, resp.ServiceSchemas, "compute")
		assert.Contains(t, resp.ServiceSchemas, "storage")
		
		// Schemas should contain table definitions
		assert.Contains(t, resp.UnifiedSchema, "CREATE TABLE")
	})
	
	// Test batch scan with database persistence
	t.Run("Batch Scan with Database", func(t *testing.T) {
		if provider.database == nil {
			t.Skip("Database not available")
		}
		
		startTime := time.Now()
		resp, err := provider.BatchScan(ctx, &pb.BatchScanRequest{
			Services: []string{"compute", "storage"},
		})
		
		require.NoError(t, err)
		assert.NotNil(t, resp.Stats)
		assert.Equal(t, "enhanced_batch", resp.Metadata["scan_method"])
		assert.Equal(t, "true", resp.Metadata["database_persisted"])
		
		// Check performance
		duration := time.Since(startTime)
		t.Logf("Batch scan completed in %v", duration)
		t.Logf("Scanned %d resources", resp.Stats.TotalResources)
		
		// Verify data was persisted (would need actual DB query here)
		if provider.database != nil {
			// In real test, query the database to verify persistence
			// rows, err := provider.database.QueryResources("SELECT COUNT(*) FROM gcp_resources")
		}
	})
}

// TestDynamicDiscoveryWithoutAssetInventory tests fallback behavior
func TestDynamicDiscoveryWithoutAssetInventory(t *testing.T) {
	ctx := context.Background()
	provider := NewGCPProvider()
	
	// Initialize without Asset Inventory (simulate no access)
	provider.Initialize(ctx, &pb.InitializeRequest{})
	
	// Should still work with fallback
	resp, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{})
	
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Services)
}

// TestResourceGraphCaching tests caching behavior
func TestResourceGraphCaching(t *testing.T) {
	ctx := context.Background()
	provider := NewGCPProvider()
	
	provider.Initialize(ctx, &pb.InitializeRequest{
		Config: map[string]string{
			"database_path": "./test_cache.db",
		},
	})
	defer provider.Cleanup()
	
	// First call - should hit Asset Inventory
	start1 := time.Now()
	resp1, err := provider.ListResources(ctx, &pb.ListResourcesRequest{})
	require.NoError(t, err)
	duration1 := time.Since(start1)
	
	// Second call - should use cache
	start2 := time.Now()
	resp2, err := provider.ListResources(ctx, &pb.ListResourcesRequest{})
	require.NoError(t, err)
	duration2 := time.Since(start2)
	
	// Cache should make second call faster
	assert.Less(t, duration2, duration1/2)
	assert.Equal(t, len(resp1.Resources), len(resp2.Resources))
	
	t.Logf("First call: %v, Second call: %v", duration1, duration2)
}

// Benchmark enhanced provider
func BenchmarkEnhancedDiscovery(b *testing.B) {
	ctx := context.Background()
	provider := NewGCPProvider()
	provider.Initialize(ctx, &pb.InitializeRequest{})
	defer provider.Cleanup()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{})
	}
}
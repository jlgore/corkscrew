package orchestrator

import (
	"context"
	"testing"
	"time"
)

// TestMemoryCache tests the memory cache implementation
func TestMemoryCache(t *testing.T) {
	cache := NewMemoryCache(1024*1024, "LRU") // 1MB cache
	ctx := context.Background()
	
	// Test Set and Get
	key := "test:key"
	value := "test value"
	ttl := 1 * time.Hour
	
	if err := cache.Set(ctx, key, value, ttl); err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}
	
	retrieved, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get from cache: %v", err)
	}
	
	if retrieved != value {
		t.Errorf("Expected %v, got %v", value, retrieved)
	}
	
	// Test Stats
	stats := cache.Stats()
	if stats.Sets != 1 {
		t.Errorf("Expected 1 set, got %d", stats.Sets)
	}
	if stats.Hits != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.Hits)
	}
	
	// Test Delete
	if err := cache.Delete(ctx, key); err != nil {
		t.Fatalf("Failed to delete from cache: %v", err)
	}
	
	_, err = cache.Get(ctx, key)
	if err == nil {
		t.Error("Expected error for deleted key")
	}
}

// TestOrchestrator tests the orchestrator
func TestOrchestrator(t *testing.T) {
	cache := NewMemoryCache(1024*1024, "LRU")
	orch := NewOrchestrator(cache).(*defaultOrchestrator)
	
	// Register a mock source
	mockSource := &mockDiscoverySource{name: "mock"}
	orch.RegisterSource("mock", mockSource)
	
	// Test discovery
	ctx := context.Background()
	options := DiscoveryOptions{
		Sources:         []DiscoverySource{mockSource},
		ForceRefresh:    true,
		ConcurrentLimit: 1,
		CacheStrategy:   CacheStrategy{Enabled: false},
	}
	
	result, err := orch.Discover(ctx, "test-provider", options)
	if err != nil {
		t.Fatalf("Failed to discover: %v", err)
	}
	
	if result.Provider != "test-provider" {
		t.Errorf("Expected provider test-provider, got %s", result.Provider)
	}
	
	if len(result.Sources) != 1 {
		t.Errorf("Expected 1 source result, got %d", len(result.Sources))
	}
}

// mockDiscoverySource is a mock implementation for testing
type mockDiscoverySource struct {
	name string
}

func (m *mockDiscoverySource) Name() string {
	return m.name
}

func (m *mockDiscoverySource) Discover(ctx context.Context, config SourceConfig) (interface{}, error) {
	return map[string]string{"mock": "data"}, nil
}

func (m *mockDiscoverySource) Validate() error {
	return nil
}

// TestAnalysisPipeline tests the analysis pipeline
func TestAnalysisPipeline(t *testing.T) {
	config := PipelineConfig{
		MaxConcurrentStages: 1,
		StageTimeout:        5 * time.Second,
		ContinueOnError:     false,
	}
	
	pipeline := NewAnalysisPipeline(config)
	
	// Add a transformation stage
	transformer := NewTransformationStage("uppercase", func(ctx context.Context, input interface{}) (interface{}, error) {
		str, ok := input.(string)
		if !ok {
			return nil, nil
		}
		return str + " TRANSFORMED", nil
	})
	
	pipeline.AddStage("transform", transformer)
	
	// Execute pipeline
	ctx := context.Background()
	result, err := pipeline.Execute(ctx, "test input")
	if err != nil {
		t.Fatalf("Pipeline execution failed: %v", err)
	}
	
	expected := "test input TRANSFORMED"
	if result != expected {
		t.Errorf("Expected %s, got %v", expected, result)
	}
}

// TestCacheKeyBuilder tests cache key generation
func TestCacheKeyBuilder(t *testing.T) {
	builder := NewCacheKeyBuilder("corkscrew")
	
	// Test discovery key
	key := builder.Discovery("aws", "github", "api")
	expected := "corkscrew:discovery:aws:github:api"
	if key != expected {
		t.Errorf("Expected %s, got %s", expected, key)
	}
	
	// Test analysis key
	key = builder.Analysis("aws", "v1.0")
	expected = "corkscrew:analysis:aws:v1.0"
	if key != expected {
		t.Errorf("Expected %s, got %s", expected, key)
	}
}
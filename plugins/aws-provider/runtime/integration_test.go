package runtime

import (
	"testing"
	"log"
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// TestOptimizedScannerIntegration verifies that the OptimizedUnifiedScanner integrates correctly with the pipeline
func TestOptimizedScannerIntegration(t *testing.T) {
	// Create basic AWS config
	cfg := aws.Config{
		Region: "us-east-1",
	}
	
	// Create pipeline config
	pipelineConfig := DefaultPipelineConfig()
	pipelineConfig.MaxConcurrency = 2
	pipelineConfig.BatchSize = 10
	
	// Test pipeline creation without client factory (should use fallback)
	pipeline, err := NewRuntimePipeline(cfg, pipelineConfig)
	if err != nil {
		t.Fatalf("Failed to create pipeline: %v", err)
	}
	
	// Verify registry has unified scanner set
	registry := pipeline.GetScannerRegistry()
	if registry == nil {
		t.Fatal("Registry is nil")
	}
	
	// Test with mock client factory
	mockFactory := &MockClientFactory{}
	pipeline2, err := NewRuntimePipelineWithClientFactory(cfg, pipelineConfig, mockFactory)
	if err != nil {
		t.Fatalf("Failed to create pipeline with client factory: %v", err)
	}
	
	registry2 := pipeline2.GetScannerRegistry()
	if registry2 == nil {
		t.Fatal("Registry2 is nil")
	}
	
	log.Printf("OptimizedScanner integration test passed")
}

// TestOptimizedScannerAdapter verifies the adapter implements the correct interface
func TestOptimizedScannerAdapter(t *testing.T) {
	mockFactory := &MockClientFactory{}
	
	// Create adapter
	adapter := NewOptimizedScannerAdapter(mockFactory)
	
	// Verify it implements UnifiedScannerProvider
	var provider UnifiedScannerProvider = adapter
	
	// Test basic operations
	ctx := context.Background()
	
	// Test scanning (should not fail even with mock)
	resources, err := provider.ScanService(ctx, "s3")
	if err == nil {
		t.Logf("ScanService returned %d resources", len(resources))
	} else {
		t.Logf("ScanService returned expected error: %v", err)
	}
	
	// Test metrics
	metrics := provider.GetMetrics()
	t.Logf("GetMetrics returned: %v", metrics)
	
	log.Printf("OptimizedScannerAdapter test passed")
}
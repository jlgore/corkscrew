package runtime

import (
	"context"
	"log"
)

// TestOptimizedScannerIntegration tests the optimized scanner integration
func TestOptimizedScannerIntegration() {
	log.Printf("Testing OptimizedScanner integration")

	// Create a mock client factory
	mockFactory := &MockClientFactory{}
	
	// Create the optimized scanner adapter
	adapter := NewOptimizedScannerAdapter(mockFactory)
	
	// Test that the adapter implements the UnifiedScannerProvider interface
	var provider UnifiedScannerProvider = adapter
	
	// Test scanning a service
	ctx := context.Background()
	resources, err := provider.ScanService(ctx, "s3")
	if err != nil {
		log.Printf("ScanService returned error: %v", err)
	} else {
		log.Printf("ScanService returned %d resources", len(resources))
	}
	
	// Test getting metrics
	metrics := provider.GetMetrics()
	log.Printf("GetMetrics returned: %v", metrics)
	
	log.Printf("OptimizedScanner integration test completed")
}

// MockClientFactory is a simple mock for testing
type MockClientFactory struct{}

// GetClient implements scanner.ClientFactory.GetClient
func (m *MockClientFactory) GetClient(serviceName string) interface{} {
	return nil
}

// GetAvailableServices implements scanner.ClientFactory.GetAvailableServices
func (m *MockClientFactory) GetAvailableServices() []string {
	return []string{"s3", "ec2", "lambda"}
}

// GetClientMethodNames implements scanner.ClientFactory.GetClientMethodNames
func (m *MockClientFactory) GetClientMethodNames(serviceName string) []string {
	return []string{}
}

// GetListOperations implements scanner.ClientFactory.GetListOperations
func (m *MockClientFactory) GetListOperations(serviceName string) []string {
	return []string{}
}

// GetDescribeOperations implements scanner.ClientFactory.GetDescribeOperations
func (m *MockClientFactory) GetDescribeOperations(serviceName string) []string {
	return []string{}
}

// HasClient implements scanner.ClientFactory.HasClient
func (m *MockClientFactory) HasClient(serviceName string) bool {
	return true
}
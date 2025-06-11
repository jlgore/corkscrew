package runtime

import (
	"context"
	"fmt"
	"log"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
)

// OptimizedScannerAdapter adapts the OptimizedUnifiedScanner to implement the UnifiedScannerProvider interface
type OptimizedScannerAdapter struct {
	optimizedScanner *scanner.OptimizedUnifiedScanner
}

// NewOptimizedScannerAdapter creates a new adapter that wraps the OptimizedUnifiedScanner
func NewOptimizedScannerAdapter(clientFactory scanner.ClientFactory) *OptimizedScannerAdapter {
	optimizedScanner := scanner.NewOptimizedUnifiedScanner(clientFactory)
	
	return &OptimizedScannerAdapter{
		optimizedScanner: optimizedScanner,
	}
}

// ScanService implements UnifiedScannerProvider.ScanService
func (a *OptimizedScannerAdapter) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	if a.optimizedScanner == nil {
		return nil, fmt.Errorf("optimized scanner not initialized")
	}
	
	log.Printf("OptimizedScannerAdapter.ScanService called for service: %s", serviceName)
	
	// Delegate to the optimized scanner
	return a.optimizedScanner.ScanService(ctx, serviceName)
}

// DescribeResource implements UnifiedScannerProvider.DescribeResource
func (a *OptimizedScannerAdapter) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	if a.optimizedScanner == nil {
		return nil, fmt.Errorf("optimized scanner not initialized")
	}
	
	// Delegate to the optimized scanner
	return a.optimizedScanner.DescribeResource(ctx, ref)
}

// GetMetrics implements UnifiedScannerProvider.GetMetrics and returns the optimized scanner's performance metrics
func (a *OptimizedScannerAdapter) GetMetrics() interface{} {
	if a.optimizedScanner == nil {
		return nil
	}
	
	// Return the performance metrics from the optimized scanner
	return a.optimizedScanner.GetMetrics()
}

// GetOptimizedScanner returns the underlying OptimizedUnifiedScanner for direct access if needed
func (a *OptimizedScannerAdapter) GetOptimizedScanner() *scanner.OptimizedUnifiedScanner {
	return a.optimizedScanner
}

// ClientFactoryWrapper wraps the actual client factory to implement scanner.ClientFactory interface
type ClientFactoryWrapper struct {
	underlying interface{} // Should be client.ClientFactory but we use interface{} for flexibility
}

// NewDynamicClientFactory creates a basic client factory wrapper 
func NewDynamicClientFactory(config interface{}) scanner.ClientFactory {
	return &ClientFactoryWrapper{
		underlying: nil, // Will be a basic implementation
	}
}

// NewClientFactoryWrapper creates a wrapper around an existing client factory
func NewClientFactoryWrapper(underlying interface{}) scanner.ClientFactory {
	return &ClientFactoryWrapper{
		underlying: underlying,
	}
}

// GetClient implements scanner.ClientFactory.GetClient
func (cf *ClientFactoryWrapper) GetClient(serviceName string) interface{} {
	// Try to delegate to underlying factory if it has the right interface
	if cf.underlying != nil {
		// Use reflection or type assertion to call GetClient on underlying factory
		if clientFactory, ok := cf.underlying.(interface{ GetClient(string) interface{} }); ok {
			return clientFactory.GetClient(serviceName)
		}
	}
	
	log.Printf("ClientFactoryWrapper.GetClient called for service: %s - no underlying factory", serviceName)
	return nil
}

// GetAvailableServices implements scanner.ClientFactory.GetAvailableServices
func (cf *ClientFactoryWrapper) GetAvailableServices() []string {
	if cf.underlying != nil {
		if clientFactory, ok := cf.underlying.(interface{ GetAvailableServices() []string }); ok {
			return clientFactory.GetAvailableServices()
		}
	}
	return []string{}
}

// GetClientMethodNames implements scanner.ClientFactory.GetClientMethodNames
func (cf *ClientFactoryWrapper) GetClientMethodNames(serviceName string) []string {
	// This is not typically implemented by the main client factory, so return empty
	return []string{}
}

// GetListOperations implements scanner.ClientFactory.GetListOperations  
func (cf *ClientFactoryWrapper) GetListOperations(serviceName string) []string {
	// This is not typically implemented by the main client factory, so return empty
	return []string{}
}

// GetDescribeOperations implements scanner.ClientFactory.GetDescribeOperations
func (cf *ClientFactoryWrapper) GetDescribeOperations(serviceName string) []string {
	// This is not typically implemented by the main client factory, so return empty
	return []string{}
}

// HasClient implements scanner.ClientFactory.HasClient
func (cf *ClientFactoryWrapper) HasClient(serviceName string) bool {
	if cf.underlying != nil {
		if clientFactory, ok := cf.underlying.(interface{ HasClient(string) bool }); ok {
			return clientFactory.HasClient(serviceName)
		}
	}
	return false
}
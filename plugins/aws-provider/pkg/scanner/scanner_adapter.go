package scanner

import (
	"context"
	"log"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// OptimizedScannerAdapter adapts OptimizedUnifiedScanner to the runtime interface
type OptimizedScannerAdapter struct {
	scanner *OptimizedUnifiedScanner
	tracker *ScannerPerformanceTracker
}

// NewOptimizedScannerAdapter creates a new adapter
func NewOptimizedScannerAdapter(clientFactory ClientFactory) *OptimizedScannerAdapter {
	scanner := NewOptimizedUnifiedScanner(clientFactory)
	return &OptimizedScannerAdapter{
		scanner: scanner,
		tracker: NewScannerPerformanceTracker(),
	}
}

// ScanService implements UnifiedScannerProvider
func (a *OptimizedScannerAdapter) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	startTime := time.Now()
	
	refs, err := a.scanner.ScanService(ctx, serviceName)
	
	duration := time.Since(startTime)
	a.tracker.TrackScan(serviceName, duration, len(refs))
	
	return refs, err
}

// DescribeResource implements UnifiedScannerProvider
func (a *OptimizedScannerAdapter) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	startTime := time.Now()
	
	resource, err := a.scanner.DescribeResource(ctx, ref)
	
	duration := time.Since(startTime)
	a.tracker.TrackEnrichment(ref.Service, duration)
	
	return resource, err
}

// GetMetrics implements UnifiedScannerProvider
func (a *OptimizedScannerAdapter) GetMetrics() interface{} {
	metrics := a.scanner.GetMetrics()
	
	// Log metrics periodically
	log.Printf("Scanner Metrics - Cache Hits: %d, Misses: %d, Hit Rate: %.2f%%",
		metrics.ReflectionCacheHits,
		metrics.ReflectionCacheMisses,
		float64(metrics.ReflectionCacheHits)/float64(metrics.ReflectionCacheHits+metrics.ReflectionCacheMisses)*100)
	
	log.Printf("Analysis Cache - Hits: %d, Misses: %d",
		metrics.AnalysisCacheHits,
		metrics.AnalysisCacheMisses)
	
	log.Printf("Total Resources Scanned: %d", metrics.ResourcesScanned)
	
	return metrics
}

// SetServiceRegistry sets the service registry for dynamic discovery
func (a *OptimizedScannerAdapter) SetServiceRegistry(registry ServiceRegistry) {
	a.scanner.SetServiceRegistry(registry)
}

// SetResourceExplorer sets the Resource Explorer for high-performance scanning
func (a *OptimizedScannerAdapter) SetResourceExplorer(explorer ResourceExplorer) {
	a.scanner.SetResourceExplorer(explorer)
}

// SetRelationshipExtractor sets the relationship extractor
func (a *OptimizedScannerAdapter) SetRelationshipExtractor(extractor RelationshipExtractor) {
	a.scanner.SetRelationshipExtractor(extractor)
}

// LogPerformanceSummary logs a performance summary
func (a *OptimizedScannerAdapter) LogPerformanceSummary() {
	a.tracker.LogServiceSummary()
	a.GetMetrics()
}

// EnablePooling wraps the client factory with pooling
func (a *OptimizedScannerAdapter) EnablePooling(cfg interface{}) {
	// Client pooling is already handled in the OptimizedUnifiedScanner
	log.Println("Client pooling is handled by OptimizedUnifiedScanner")
}
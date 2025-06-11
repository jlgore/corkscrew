package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"golang.org/x/time/rate"
)

// PipelineConfig contains configuration for the runtime pipeline
type PipelineConfig struct {
	// Scanner configuration
	MaxConcurrency      int
	ScanTimeout         time.Duration
	UseResourceExplorer bool  // Try Resource Explorer first, fallback to UnifiedScanner
	
	// DuckDB configuration (disabled - handled by main CLI)
	// EnableDuckDB     bool
	// DuckDBPath       string
	BatchSize        int      // Still used for batching resources before returning to CLI
	FlushInterval    time.Duration  // Still used for periodic flushing
	
	// Service discovery
	EnableAutoDiscovery bool
	ServiceFilter       []string
	RegionFilter        []string
	
	// Performance tuning
	CacheEnabled     bool
	CacheTTL         time.Duration
	StreamingEnabled bool
}

// DefaultPipelineConfig returns default pipeline configuration
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		MaxConcurrency:      10,
		ScanTimeout:         5 * time.Minute,
		UseResourceExplorer: true,  // Try Resource Explorer first when available
		// EnableDuckDB:        true,  // Disabled - handled by main CLI
		BatchSize:           1000,
		FlushInterval:       30 * time.Second,
		EnableAutoDiscovery: true,
		CacheEnabled:        true,
		CacheTTL:            5 * time.Minute,
		StreamingEnabled:    true,
	}
}


// RuntimePipeline orchestrates the resource discovery and storage pipeline
type RuntimePipeline struct {
	config       *PipelineConfig
	registry     *ScannerRegistry
	reClient     *resourceexplorer2.Client
	discoverer   *discovery.ServiceDiscovery
	
	// State management
	mu          sync.RWMutex
	isRunning   bool
	lastScan    time.Time
	scanResults map[string]*ScanResult
	
	// Resource streaming
	resourceChan chan *pb.Resource
	errorChan    chan error
	
	// Shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRuntimePipeline creates a new runtime pipeline
func NewRuntimePipeline(cfg aws.Config, config *PipelineConfig) (*RuntimePipeline, error) {
	return NewRuntimePipelineWithClientFactory(cfg, config, nil)
}

// NewRuntimePipelineWithClientFactory creates a new runtime pipeline with a provided client factory
func NewRuntimePipelineWithClientFactory(cfg aws.Config, config *PipelineConfig, clientFactory interface{}) (*RuntimePipeline, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	pipeline := &RuntimePipeline{
		config:       config,
		registry:     NewScannerRegistry(),
		scanResults:  make(map[string]*ScanResult),
		resourceChan: make(chan *pb.Resource, config.BatchSize),
		errorChan:    make(chan error, 100),
		ctx:          ctx,
		cancel:       cancel,
	}
	
	// Initialize Resource Explorer client if enabled
	if config.UseResourceExplorer {
		pipeline.reClient = resourceexplorer2.NewFromConfig(cfg)
	}
	
	// Skip DuckDB initialization in plugin - let main CLI handle database operations
	// This prevents database locking conflicts between plugin and CLI processes
	log.Printf("DuckDB operations will be handled by main CLI process")
	
	// Initialize service discovery
	if config.EnableAutoDiscovery {
		pipeline.discoverer = discovery.NewAWSServiceDiscovery("")
	}
	
	// Load scanners
	log.Printf("DEBUG: About to create ScannerLoader")
	loader := NewScannerLoader(pipeline.registry)
	log.Printf("DEBUG: ScannerLoader created, about to call LoadAll()")
	if err := loader.LoadAll(); err != nil {
		log.Printf("DEBUG: LoadAll() returned error: %v", err)
		return nil, fmt.Errorf("failed to load scanners: %w", err)
	}
	log.Printf("DEBUG: LoadAll() completed successfully")
	
	// Initialize optimized unified scanner
	log.Printf("DEBUG: Initializing OptimizedUnifiedScanner")
	if err := pipeline.initializeOptimizedScanner(cfg, clientFactory); err != nil {
		log.Printf("WARN: Failed to initialize optimized scanner, falling back to basic scanner: %v", err)
	}
	
	return pipeline, nil
}

// initializeOptimizedScanner sets up the optimized unified scanner
func (p *RuntimePipeline) initializeOptimizedScanner(cfg aws.Config, providedClientFactory interface{}) error {
	var scannerClientFactory scanner.ClientFactory
	
	// Use provided client factory if available, otherwise create a basic one
	if providedClientFactory != nil {
		log.Printf("DEBUG: Using provided client factory for OptimizedUnifiedScanner")
		
		// Try to use the provided factory directly if it implements scanner.ClientFactory
		if scf, ok := providedClientFactory.(scanner.ClientFactory); ok {
			scannerClientFactory = scf
			log.Printf("DEBUG: Provided client factory implements scanner.ClientFactory directly")
		} else {
			// Wrap the provided client factory
			scannerClientFactory = NewClientFactoryWrapper(providedClientFactory)
			log.Printf("DEBUG: Wrapped provided client factory")
		}
	} else {
		// Create a basic dynamic client factory as fallback
		scannerClientFactory = NewDynamicClientFactory(cfg)
		log.Printf("DEBUG: Created fallback dynamic client factory for OptimizedUnifiedScanner")
	}
	
	// Create optimized scanner adapter
	optimizedScanner := NewOptimizedScannerAdapter(scannerClientFactory)
	
	// Set the unified scanner in the registry
	p.registry.SetUnifiedScanner(optimizedScanner)
	
	log.Printf("DEBUG: OptimizedUnifiedScanner initialized successfully with adapter")
	return nil
}


// GetScannerRegistry returns the scanner registry for external configuration
func (p *RuntimePipeline) GetScannerRegistry() *ScannerRegistry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.registry
}

// Start starts the runtime pipeline
func (p *RuntimePipeline) Start() error {
	p.mu.Lock()
	if p.isRunning {
		p.mu.Unlock()
		return fmt.Errorf("pipeline already running")
	}
	p.isRunning = true
	p.mu.Unlock()
	
	// Start resource processor
	go p.resourceProcessor()
	
	// Start error handler
	go p.errorHandler()
	
	// Start periodic scanner if auto-discovery is enabled
	if p.config.EnableAutoDiscovery {
		go p.periodicScanner()
	}
	
	log.Println("Runtime pipeline started")
	return nil
}

// Stop stops the runtime pipeline
func (p *RuntimePipeline) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if !p.isRunning {
		return fmt.Errorf("pipeline not running")
	}
	
	log.Println("Stopping runtime pipeline...")
	
	// Close resource channel to signal processor to flush
	close(p.resourceChan)
	
	// Give processor time to flush remaining resources
	time.Sleep(500 * time.Millisecond)
	
	// Cancel context
	p.cancel()
	
	// Close error channel
	close(p.errorChan)
	
	// No DuckDB connection to close - handled by main CLI
	
	p.isRunning = false
	log.Println("Runtime pipeline stopped")
	return nil
}

// ScanService scans a specific service
func (p *RuntimePipeline) ScanService(ctx context.Context, serviceName string, cfg aws.Config, region string) (*ScanResult, error) {
	result := &ScanResult{
		Service:   serviceName,
		Region:    region,
		StartTime: time.Now(),
		Resources: []*pb.Resource{},
	}
	
	// Try Resource Explorer first if enabled
	if p.config.UseResourceExplorer && p.reClient != nil {
		resources, err := p.scanWithResourceExplorer(ctx, serviceName, region)
		if err == nil && len(resources) > 0 {
			result.Resources = resources
			result.Method = "ResourceExplorer"
			result.ResourceCount = len(resources)
			log.Printf("Resource Explorer found %d resources for %s in region %s", len(resources), serviceName, region)
		} else {
			// Always fallback to UnifiedScanner (no legacy scanner option)
			log.Printf("Resource Explorer unavailable or returned no results, using UnifiedScanner")
			resources, err = p.registry.ScanServiceUnified(ctx, serviceName, cfg, region)
			if err != nil {
				result.Error = err
				log.Printf("Unified scan failed for %s in region %s: %v", serviceName, region, err)
			} else {
				result.Resources = resources
				result.Method = "UnifiedScanner"
				result.ResourceCount = len(resources)
				log.Printf("UnifiedScanner found %d resources for %s in region %s", len(resources), serviceName, region)
			}
		}
	} else {
		// Use UnifiedScanner directly
		resources, err := p.registry.ScanServiceUnified(ctx, serviceName, cfg, region)
		if err != nil {
			result.Error = err
			log.Printf("Unified scan failed for %s in region %s: %v", serviceName, region, err)
		} else {
			result.Resources = resources
			result.Method = "UnifiedScanner"
			result.ResourceCount = len(resources)
			log.Printf("UnifiedScanner found %d resources for %s in region %s", len(resources), serviceName, region)
		}
	}
	
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	
	// Performance metrics
	if result.ResourceCount > 0 {
		avgEnrichmentTime := result.Duration / time.Duration(result.ResourceCount)
		log.Printf("Performance: %s scan completed in %v (%v per resource)", serviceName, result.Duration, avgEnrichmentTime)
	}
	
	// Stream resources for processing
	if p.config.StreamingEnabled && len(result.Resources) > 0 {
		for _, resource := range result.Resources {
			select {
			case p.resourceChan <- resource:
			case <-ctx.Done():
				return result, ctx.Err()
			}
		}
	}
	
	// Store result
	p.mu.Lock()
	p.scanResults[serviceName] = result
	p.lastScan = time.Now()
	p.mu.Unlock()
	
	return result, result.Error
}

// ScanAllServices scans all discovered services with full configuration collection
func (p *RuntimePipeline) ScanAllServices(ctx context.Context, cfg aws.Config, region string) (*BatchScanResult, error) {
	// Use empty services list to trigger default behavior in ScanServices
	return p.ScanServices(ctx, []string{}, cfg, region)
}

// ScanServices scans specified services with full configuration collection
func (p *RuntimePipeline) ScanServices(ctx context.Context, requestedServices []string, cfg aws.Config, region string) (*BatchScanResult, error) {
	// Use requested services if provided, otherwise use default service list
	var services []string
	if len(requestedServices) > 0 {
		// Use requested services directly - UnifiedScanner can handle any service
		services = requestedServices
	} else {
		// Default to common AWS services if none specified
		services = []string{"s3", "ec2", "lambda", "rds", "iam", "dynamodb"}
		if len(p.config.ServiceFilter) > 0 {
			services = p.filterServices(services, p.config.ServiceFilter)
		}
	}
	
	startTime := time.Now()
	batchResult := &BatchScanResult{
		StartTime:       startTime,
		ServicesScanned: len(services),
		Resources:       make([]*pb.Resource, 0),
		Errors:          make(map[string]error),
		ServiceStats:    make(map[string]*ServiceScanStats),
	}
	
	// Scan each service and collect detailed configuration
	for _, service := range services {
		serviceStartTime := time.Now()
		
		// Use pipeline's own ScanService method for rich configuration
		result, err := p.ScanService(ctx, service, cfg, region)
		if err != nil {
			batchResult.Errors[service] = err
			batchResult.TotalErrors++
			batchResult.ServiceStats[service] = &ServiceScanStats{
				Success: false,
				Error:   err.Error(),
			}
			continue
		}
		
		// Add resources to batch result
		batchResult.Resources = append(batchResult.Resources, result.Resources...)
		batchResult.TotalResources += len(result.Resources)
		
		// Record service stats
		batchResult.ServiceStats[service] = &ServiceScanStats{
			ResourceCount: len(result.Resources),
			Success:       true,
		}
		
		// Resources are automatically streamed to database via pipeline's resourceProcessor
		log.Printf("Pipeline batch scan: %s completed with %d resources in %v", 
			service, len(result.Resources), time.Since(serviceStartTime))
	}
	
	batchResult.EndTime = time.Now()
	batchResult.Duration = batchResult.EndTime.Sub(batchResult.StartTime)
	
	return batchResult, nil
}

// scanWithResourceExplorer uses AWS Resource Explorer for discovery
func (p *RuntimePipeline) scanWithResourceExplorer(ctx context.Context, serviceName string, region string) ([]*pb.Resource, error) {
	// Build query for specific service
	query := fmt.Sprintf("service:%s", serviceName)
	if region != "" && region != "all" {
		query += fmt.Sprintf(" AND region:%s", region)
	}
	
	input := &resourceexplorer2.SearchInput{
		QueryString: aws.String(query),
		MaxResults:  aws.Int32(1000),
	}
	
	var allResources []*pb.Resource
	paginator := resourceexplorer2.NewSearchPaginator(p.reClient, input)
	
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("resource explorer search failed: %w", err)
		}
		
		// Convert Resource Explorer results to our format
		for _, item := range output.Resources {
			resource := p.convertResourceExplorerItem(item)
			if resource != nil {
				allResources = append(allResources, resource)
			}
		}
	}
	
	return allResources, nil
}

// convertResourceExplorerItem converts a Resource Explorer item to our resource format
func (p *RuntimePipeline) convertResourceExplorerItem(item interface{}) *pb.Resource {
	// This would convert the Resource Explorer format to pb.Resource
	// Implementation depends on the actual Resource Explorer response structure
	return &pb.Resource{
		// Conversion logic here
	}
}

// resourceProcessor processes resources in the background
func (p *RuntimePipeline) resourceProcessor() {
	log.Printf("DEBUG: resourceProcessor started")
	batch := make([]*pb.Resource, 0, p.config.BatchSize)
	ticker := time.NewTicker(p.config.FlushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case resource, ok := <-p.resourceChan:
			if !ok {
				// Channel closed, flush remaining batch
				log.Printf("DEBUG: resourceChan closed, flushing %d resources", len(batch))
				if len(batch) > 0 {
					p.processBatch(batch)
				}
				return
			}
			
			log.Printf("DEBUG: Received resource %s in processor", resource.Id)
			batch = append(batch, resource)
			
			// Process batch if it reaches the configured size
			if len(batch) >= p.config.BatchSize {
				log.Printf("DEBUG: Batch size reached (%d), processing", len(batch))
				p.processBatch(batch)
				batch = make([]*pb.Resource, 0, p.config.BatchSize)
			}
			
		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				log.Printf("DEBUG: Timer flush - processing %d resources", len(batch))
				p.processBatch(batch)
				batch = make([]*pb.Resource, 0, p.config.BatchSize)
			}
			
		case <-p.ctx.Done():
			// Shutdown requested
			log.Printf("DEBUG: Context done, flushing %d resources", len(batch))
			if len(batch) > 0 {
				p.processBatch(batch)
			}
			return
		}
	}
}

// processBatch processes a batch of resources
func (p *RuntimePipeline) processBatch(resources []*pb.Resource) {
	if len(resources) == 0 {
		return
	}
	
	log.Printf("DEBUG: processBatch called with %d resources", len(resources))
	
	// Skip DuckDB storage - resources will be saved by main CLI after being returned
	log.Printf("DEBUG: Processed batch of %d resources (to be saved by main CLI)", len(resources))
	
	// Log resource details for debugging
	for i, res := range resources {
		log.Printf("DEBUG: Resource %d: ID=%s, Service=%s, Type=%s, RawDataLen=%d, AttributesLen=%d", 
			i, res.Id, res.Service, res.Type, len(res.RawData), len(res.Attributes))
	}
	
	// Update relationships
	p.updateRelationships(resources)
}

// updateRelationships extracts and stores resource relationships
func (p *RuntimePipeline) updateRelationships(resources []*pb.Resource) {
	// Extract relationships from resources
	// This would analyze resource attributes to find relationships
	// For example: EC2 instance -> VPC, Security Groups, etc.
	
	// Placeholder for relationship extraction logic
}

// errorHandler handles errors from the pipeline
func (p *RuntimePipeline) errorHandler() {
	for {
		select {
		case err, ok := <-p.errorChan:
			if !ok {
				return
			}
			log.Printf("Pipeline error: %v", err)
			
		case <-p.ctx.Done():
			return
		}
	}
}

// periodicScanner runs periodic service discovery and scanning
func (p *RuntimePipeline) periodicScanner() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Discover new services
			if p.discoverer != nil {
				ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
				services, err := p.discoverer.DiscoverAWSServices(ctx, false)
				cancel()
				
				if err != nil {
					p.errorChan <- fmt.Errorf("service discovery failed: %w", err)
					continue
				}
				
				// Log discovered services - UnifiedScanner handles all services dynamically
				log.Printf("Service discovery found %d services", len(services))
				
				// Ensure rate limiters exist for new services
				for _, service := range services {
					if _, exists := p.registry.GetMetadata(service); !exists {
						log.Printf("Discovered new service: %s, registering rate limiter", service)
						// Register default rate limiter for new service
						metadata := &ScannerMetadata{
							ServiceName:          service,
							RateLimit:           rate.Limit(10), // Default rate limit
							BurstLimit:          20,
							SupportsPagination:   true,
							SupportsParallelScan: true,
						}
						p.registry.RegisterServiceMetadata(service, metadata)
					}
				}
			}
			
		case <-p.ctx.Done():
			return
		}
	}
}

// filterServices filters services based on the provided filter
func (p *RuntimePipeline) filterServices(services []string, filter []string) []string {
	filterMap := make(map[string]bool)
	for _, f := range filter {
		filterMap[f] = true
	}
	
	filtered := make([]string, 0)
	for _, service := range services {
		if filterMap[service] {
			filtered = append(filtered, service)
		}
	}
	
	return filtered
}

// GetStats returns current pipeline statistics
func (p *RuntimePipeline) GetStats() *PipelineStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	stats := &PipelineStats{
		IsRunning:        p.isRunning,
		LastScan:         p.lastScan,
		ServicesScanned:  len(p.scanResults),
		TotalResources:   0,
		ServiceStats:     make(map[string]*ServiceStats),
		RegisteredServices: p.registry.ListServices(),
	}
	
	for service, result := range p.scanResults {
		stats.TotalResources += result.ResourceCount
		stats.ServiceStats[service] = &ServiceStats{
			LastScan:      result.EndTime,
			ResourceCount: result.ResourceCount,
			Method:        result.Method,
			Duration:      result.Duration,
			HasError:      result.Error != nil,
		}
	}
	
	return stats
}

// ScanResult contains results from scanning a service
type ScanResult struct {
	Service       string
	Region        string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	ResourceCount int
	Resources     []*pb.Resource
	Method        string // "ResourceExplorer" or "GeneratedScanner"
	Error         error
}

// PipelineStats contains pipeline statistics
type PipelineStats struct {
	IsRunning          bool
	LastScan           time.Time
	ServicesScanned    int
	TotalResources     int
	ServiceStats       map[string]*ServiceStats
	RegisteredServices []string
}

// ServiceStats contains statistics for a single service
type ServiceStats struct {
	LastScan      time.Time
	ResourceCount int
	Method        string
	Duration      time.Duration
	HasError      bool
}

// StreamingClient provides streaming access to discovered resources
type StreamingClient struct {
	pipeline *RuntimePipeline
	ch       chan *pb.Resource
	done     chan struct{}
}

// NewStreamingClient creates a new streaming client
func (p *RuntimePipeline) NewStreamingClient() *StreamingClient {
	client := &StreamingClient{
		pipeline: p,
		ch:       make(chan *pb.Resource, 100),
		done:     make(chan struct{}),
	}
	
	// Start forwarding resources
	go client.forward()
	
	return client
}

// forward forwards resources from the pipeline to the client
func (c *StreamingClient) forward() {
	for {
		select {
		case resource := <-c.pipeline.resourceChan:
			select {
			case c.ch <- resource:
			case <-c.done:
				return
			}
		case <-c.done:
			return
		}
	}
}

// Next returns the next resource
func (c *StreamingClient) Next() (*pb.Resource, bool) {
	select {
	case resource, ok := <-c.ch:
		return resource, ok
	case <-c.done:
		return nil, false
	}
}

// Close closes the streaming client
func (c *StreamingClient) Close() {
	close(c.done)
	close(c.ch)
}
package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"golang.org/x/sync/errgroup"
)

// PipelineConfig contains configuration for the runtime pipeline
type PipelineConfig struct {
	// Scanner configuration
	MaxConcurrency      int
	ScanTimeout         time.Duration
	UseResourceExplorer bool
	FallbackToScanners  bool
	
	// DuckDB configuration
	EnableDuckDB     bool
	DuckDBPath       string
	BatchSize        int
	FlushInterval    time.Duration
	
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
		UseResourceExplorer: true,
		FallbackToScanners:  true,
		EnableDuckDB:        true,
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
	graphLoader  *db.GraphLoader
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
	
	// Initialize DuckDB if enabled
	if config.EnableDuckDB && config.DuckDBPath != "" {
		graphLoader, err := db.NewGraphLoader(config.DuckDBPath)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize DuckDB: %w", err)
		}
		pipeline.graphLoader = graphLoader
	}
	
	// Initialize service discovery
	if config.EnableAutoDiscovery {
		pipeline.discoverer = discovery.NewAWSServiceDiscovery("")
	}
	
	// Load scanners
	loader := NewScannerLoader(pipeline.registry)
	if err := loader.LoadAll(); err != nil {
		return nil, fmt.Errorf("failed to load scanners: %w", err)
	}
	
	return pipeline, nil
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
	
	// Cancel context
	p.cancel()
	
	// Close channels
	close(p.resourceChan)
	close(p.errorChan)
	
	// Close DuckDB connection
	if p.graphLoader != nil {
		p.graphLoader.Close()
	}
	
	p.isRunning = false
	log.Println("Runtime pipeline stopped")
	return nil
}

// ScanService scans a specific service
func (p *RuntimePipeline) ScanService(ctx context.Context, serviceName string, cfg aws.Config, region string) (*ScanResult, error) {
	startTime := time.Now()
	result := &ScanResult{
		Service:   serviceName,
		Region:    region,
		StartTime: startTime,
		Resources: []*pb.Resource{},
	}
	
	// Try Resource Explorer first if enabled
	if p.config.UseResourceExplorer && p.reClient != nil {
		resources, err := p.scanWithResourceExplorer(ctx, serviceName, region)
		if err == nil && len(resources) > 0 {
			result.Resources = resources
			result.Method = "ResourceExplorer"
		} else if p.config.FallbackToScanners {
			// Fallback to generated scanners
			resources, err = p.registry.ScanService(ctx, serviceName, cfg, region)
			if err != nil {
				result.Error = err
			} else {
				result.Resources = resources
				result.Method = "GeneratedScanner"
			}
		}
	} else {
		// Use generated scanners directly
		resources, err := p.registry.ScanService(ctx, serviceName, cfg, region)
		if err != nil {
			result.Error = err
		} else {
			result.Resources = resources
			result.Method = "GeneratedScanner"
		}
	}
	
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.ResourceCount = len(result.Resources)
	
	// Stream resources if enabled
	if p.config.StreamingEnabled {
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
	p.mu.Unlock()
	
	return result, result.Error
}

// ScanAllServices scans all discovered services
func (p *RuntimePipeline) ScanAllServices(ctx context.Context, cfg aws.Config, region string) (*BatchScanResult, error) {
	// Get list of services to scan
	services := p.registry.ListServices()
	if len(p.config.ServiceFilter) > 0 {
		services = p.filterServices(services, p.config.ServiceFilter)
	}
	
	// Create batch scanner
	batchScanner := NewBatchScanner(p.registry, p.config.MaxConcurrency)
	batchScanner.timeout = p.config.ScanTimeout
	
	// Perform batch scan
	return batchScanner.ScanServices(ctx, services, cfg, region)
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
	batch := make([]*pb.Resource, 0, p.config.BatchSize)
	ticker := time.NewTicker(p.config.FlushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case resource, ok := <-p.resourceChan:
			if !ok {
				// Channel closed, flush remaining batch
				if len(batch) > 0 {
					p.processBatch(batch)
				}
				return
			}
			
			batch = append(batch, resource)
			
			// Process batch if it reaches the configured size
			if len(batch) >= p.config.BatchSize {
				p.processBatch(batch)
				batch = make([]*pb.Resource, 0, p.config.BatchSize)
			}
			
		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				p.processBatch(batch)
				batch = make([]*pb.Resource, 0, p.config.BatchSize)
			}
			
		case <-p.ctx.Done():
			// Shutdown requested
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
	
	// Store in DuckDB if enabled
	if p.graphLoader != nil {
		ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
		defer cancel()
		
		if err := p.graphLoader.LoadResources(ctx, resources); err != nil {
			p.errorChan <- fmt.Errorf("failed to store %d resources in DuckDB: %w", len(resources), err)
		} else {
			log.Printf("Stored %d resources in DuckDB", len(resources))
		}
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
				services, err := p.discoverer.DiscoverServices(ctx)
				cancel()
				
				if err != nil {
					p.errorChan <- fmt.Errorf("service discovery failed: %w", err)
					continue
				}
				
				// Register any new services
				for _, service := range services {
					if _, exists := p.registry.Get(service); !exists {
						log.Printf("Discovered new service: %s", service)
						// Would dynamically load scanner for new service
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
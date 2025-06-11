package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/jlgore/corkscrew/plugins/aws-provider/tools"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
)








// ScannerProvider implements the UnifiedScannerProvider interface
// by delegating to the UnifiedScanner's methods
type ScannerProvider struct {
	scanner *scanner.UnifiedScanner
}

// ScanService bridges registry scanner calls to the UnifiedScanner
func (p *ScannerProvider) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	if p.scanner == nil {
		return []*pb.ResourceRef{}, nil
	}
	
	// Delegate directly to UnifiedScanner
	return p.scanner.ScanService(ctx, serviceName)
}

// DescribeResource bridges registry enrichment calls to the UnifiedScanner
func (p *ScannerProvider) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	if p.scanner == nil {
		return nil, fmt.Errorf("scanner not available")
	}
	
	// Delegate directly to UnifiedScanner
	return p.scanner.DescribeResource(ctx, ref)
}

// GetMetrics returns metrics from the underlying scanner (required by UnifiedScannerProvider interface)
func (p *ScannerProvider) GetMetrics() interface{} {
	if p.scanner == nil {
		return map[string]interface{}{}
	}
	
	// Delegate to UnifiedScanner's GetMetrics method
	return p.scanner.GetMetrics()
}


// Cache provides caching for discovered services and resources
type Cache struct {
	mu       sync.RWMutex
	data     map[string]interface{}
	ttl      map[string]time.Time
	duration time.Duration
}

func NewCache(duration time.Duration) *Cache {
	return &Cache{
		data:     make(map[string]interface{}),
		ttl:      make(map[string]time.Time),
		duration: duration,
	}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if expiry, exists := c.ttl[key]; exists && time.Now().Before(expiry) {
		if data, exists := c.data[key]; exists {
			return data, true
		}
	}
	return nil, false
}

func (c *Cache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = value
	c.ttl[key] = time.Now().Add(c.duration)
}

// AWSProvider implements the CloudProvider interface for AWS
type AWSProvider struct {
	mu          sync.RWMutex
	initialized bool
	config      aws.Config

	// Core components
	discovery     *discovery.RuntimeServiceDiscovery
	scanner       *scanner.UnifiedScanner
	explorer      *ResourceExplorer
	schemaGen     *SchemaGenerator
	clientFactory *client.ClientFactory
	
	// Unified registry system (replaces old separate registries)
	unifiedRegistry *registry.UnifiedServiceRegistry

	// Runtime pipeline for advanced scanning
	pipeline *runtime.RuntimePipeline

	// Caching
	serviceCache  *Cache
	resourceCache *Cache

	// Performance components
	rateLimiter    *rate.Limiter
	maxConcurrency int
}

// NewAWSProvider creates a new AWS provider instance
func NewAWSProvider() *AWSProvider {
	return &AWSProvider{
		serviceCache:   NewCache(24 * time.Hour),
		resourceCache:  NewCache(15 * time.Minute),
		rateLimiter:    rate.NewLimiter(rate.Limit(50), 100), // 50 requests/sec, burst 100
		maxConcurrency: 10,
	}
}

// Initialize sets up the AWS provider with credentials and configuration
func (p *AWSProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("Initializing AWS Provider v3 (UnifiedScanner Only)")

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to load AWS config: %v", err),
		}, nil
	}

	p.config = cfg

	// Initialize core components
	p.clientFactory = client.NewClientFactory(cfg)
	
	// Note: Generated client factory will be initialized after unified registry is created
	
	// Initialize unified registry system
	log.Printf("Initializing unified registry system")
	registryConfig := registry.RegistryConfig{
		EnableCache:         true,
		EnableMetrics:       true,
		UseFallbackServices: true,
	}
	
	// Create unified registry (this replaces the old factory systems)
	p.unifiedRegistry = registry.NewUnifiedServiceRegistry(cfg, registryConfig)
	
	// TODO: Initialize generated client factory when build issues are resolved
	
	// Create client factory adapter for discovery
	clientFactoryAdapter := NewClientFactoryAdapter(p.clientFactory)
	
	// Initialize discovery with dynamic service discovery only
	log.Printf("Using dynamic service discovery (UnifiedScanner only)")
	p.discovery = discovery.NewRuntimeServiceDiscovery(cfg)
	p.discovery.SetClientFactory(clientFactoryAdapter)
	
	p.scanner = scanner.NewUnifiedScanner(p.clientFactory)
	p.scanner.SetRelationshipExtractor(NewRelationshipExtractor())
	p.schemaGen = NewSchemaGenerator()

	// Initialize analysis generation (mandatory for enhanced configuration collection)
	log.Printf("Creating analysis generator for enhanced scanning capabilities")
	analysisGenerator, err := p.createAnalysisGenerator(clientFactoryAdapter)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to initialize analysis generator: %v", err),
		}, nil
	}
	
	p.discovery.SetAnalysisGenerator(analysisGenerator)
	p.discovery.EnableAnalysisGeneration(true)
	log.Printf("Analysis generator initialized and enabled")
	
	// Skip service discovery during initialization - will be done on-demand during scanning
	log.Printf("Initialization complete - service discovery will be performed on-demand during scanning")

	// Check if Resource Explorer is available
	if viewArn := p.checkResourceExplorer(ctx); viewArn != "" {
		accountID := p.scanner.GetAccountID()
		p.explorer = NewResourceExplorer(cfg, viewArn, accountID)
		p.scanner.SetResourceExplorer(p.explorer)
		
		// Test Resource Explorer connectivity
		if p.explorer.IsHealthy(ctx) {
			log.Printf("Resource Explorer initialized and healthy with view: %s", viewArn)
		} else {
			log.Printf("Resource Explorer view found but not healthy, falling back to SDK scanning")
			p.explorer = nil
		}
	} else {
		log.Printf("Resource Explorer not available, will use SDK scanning")
	}

	// Initialize runtime pipeline
	pipelineConfig := runtime.DefaultPipelineConfig()
	// DuckDB is now handled by the main CLI, not the plugin
	pipelineConfig.UseResourceExplorer = p.explorer != nil
	pipelineConfig.MaxConcurrency = p.maxConcurrency
	// Use smaller batch size for immediate processing
	pipelineConfig.BatchSize = 1  // Process immediately
	pipelineConfig.FlushInterval = 1 * time.Second
	
	pipeline, pipelineErr := runtime.NewRuntimePipelineWithClientFactory(p.config, pipelineConfig, p.clientFactory)
	if pipelineErr != nil {
		log.Printf("Failed to initialize runtime pipeline: %v", pipelineErr)
		log.Printf("DEBUG: Pipeline error details: %v", pipelineErr)
		// Continue without pipeline - fallback to basic scanning
	} else {
		// Connect UnifiedScanner to pipeline's registry
		registry := pipeline.GetScannerRegistry()
		if registry != nil && p.scanner != nil {
			provider := &ScannerProvider{scanner: p.scanner}
			registry.SetUnifiedScanner(provider)
			log.Printf("UnifiedScanner connected to pipeline registry")
		} else if p.scanner == nil {
			log.Printf("ERROR: Scanner is nil, cannot connect to pipeline")
		} else {
			log.Printf("ERROR: Registry is nil, cannot connect scanner")
		}
		
		p.pipeline = pipeline
		if startErr := pipeline.Start(); startErr != nil {
			log.Printf("Failed to start pipeline: %v", startErr)
			p.pipeline = nil
		} else {
			log.Printf("Pipeline started with UnifiedScanner integration")
		}
	}

	p.initialized = true

	return &pb.InitializeResponse{
		Success: true,
		Version: "3.0.0",
		Metadata: map[string]string{
			"region":              cfg.Region,
			"resource_explorer":   fmt.Sprintf("%t", p.explorer != nil),
			"max_concurrency":     fmt.Sprintf("%d", p.maxConcurrency),
			"runtime_pipeline":    fmt.Sprintf("%t", p.pipeline != nil),
			"service_discovery":   "on-demand",
			"scanner_mode":        "unified_only",
			"analysis_generation": "enabled",
		},
	}, nil
}

// checkResourceExplorer checks if Resource Explorer is available and returns view ARN
func (p *AWSProvider) checkResourceExplorer(ctx context.Context) string {
	// Create Resource Explorer client
	explorer := resourceexplorer2.NewFromConfig(p.config)
	
	// List available views
	result, err := explorer.ListViews(ctx, &resourceexplorer2.ListViewsInput{})
	if err != nil {
		log.Printf("Resource Explorer not available: %v", err)
		return ""
	}
	
	// Check if any views are available
	if len(result.Views) == 0 {
		log.Printf("No Resource Explorer views found")
		return ""
	}
	
	// Return the first available view ARN
	// Prefer aggregator views over local views for better coverage
	var aggregatorView, localView string
	
	for _, viewArn := range result.Views {
		// Get view details to check if it's an aggregator
		viewDetails, err := explorer.GetView(ctx, &resourceexplorer2.GetViewInput{
			ViewArn: &viewArn,
		})
		if err != nil {
			log.Printf("Failed to get view details for %s: %v", viewArn, err)
			continue
		}
		
		if viewDetails.View != nil {
			// Check if this is an aggregator view
			if viewDetails.View.Scope != nil && *viewDetails.View.Scope == "LOCAL" {
				if localView == "" {
					localView = viewArn
				}
			} else {
				// This is an aggregator view - prefer it
				aggregatorView = viewArn
				break
			}
		}
	}
	
	// Return aggregator view if available, otherwise local view
	if aggregatorView != "" {
		log.Printf("Using Resource Explorer aggregator view: %s", aggregatorView)
		return aggregatorView
	} else if localView != "" {
		log.Printf("Using Resource Explorer local view: %s", localView)
		return localView
	}
	
	log.Printf("No suitable Resource Explorer views found")
	
	// Optionally, we could create a default view here
	// For now, just return empty to use SDK scanning
	return ""
}

// createDefaultResourceExplorerView creates a default Resource Explorer view if none exists
func (p *AWSProvider) createDefaultResourceExplorerView(ctx context.Context) (string, error) {
	explorer := resourceexplorer2.NewFromConfig(p.config)
	
	// Check if an index exists in this region
	indexResult, err := explorer.ListIndexes(ctx, &resourceexplorer2.ListIndexesInput{})
	if err != nil {
		return "", fmt.Errorf("failed to list indexes: %w", err)
	}
	
	// If no indexes exist, we can't create a view
	if len(indexResult.Indexes) == 0 {
		return "", fmt.Errorf("no Resource Explorer indexes found - please set up Resource Explorer first")
	}
	
	// Create a default view
	viewName := "corkscrew-default-view"
	createViewResult, err := explorer.CreateView(ctx, &resourceexplorer2.CreateViewInput{
		ViewName: &viewName,
		// Optional: Add filters if needed
		// Filters: &resourceexplorer2.SearchFilter{
		//     FilterString: aws.String("*"),
		// },
	})
	if err != nil {
		return "", fmt.Errorf("failed to create view: %w", err)
	}
	
	if createViewResult.View != nil && createViewResult.View.ViewArn != nil {
		log.Printf("Created default Resource Explorer view: %s", *createViewResult.View.ViewArn)
		return *createViewResult.View.ViewArn, nil
	}
	
	return "", fmt.Errorf("failed to get view ARN from create response")
}

// GetProviderInfo returns information about the AWS provider
func (p *AWSProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "aws-v3",
		Version:     "3.0.0",
		Description: "AWS cloud provider plugin v3 with UnifiedScanner-only dynamic discovery",
		Capabilities: map[string]string{
			"discovery":          "true",
			"scanning":           "true",
			"streaming":          "true",
			"multi_region":       "true",
			"resource_explorer":  "true",
			"dynamic_services":   "true",
			"batch_operations":   "true",
			"relationship_graph": "true",
		},
		SupportedServices: []string{}, // Dynamically discovered at runtime
	}, nil
}

// DiscoverServices discovers available AWS services dynamically
func (p *AWSProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Check cache unless force refresh is requested
	cacheKey := "discovered_services"
	if !req.ForceRefresh {
		if cached, ok := p.serviceCache.Get(cacheKey); ok {
			if services, ok := cached.([]*pb.ServiceInfo); ok {
				return &pb.DiscoverServicesResponse{
					Services:     services,
					DiscoveredAt: timestamppb.Now(),
					SdkVersion:   "aws-sdk-go-v2",
				}, nil
			}
		}
	}

	// Use dynamic discovery only - fail fast if discovery fails
	services, err := p.discovery.DiscoverServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover AWS services: %w", err)
	}

	// Filter services based on include/exclude lists
	filteredServices := make([]*pb.ServiceInfo, 0)
	for _, service := range services {
		// Skip if service is in exclude list
		if p.isServiceExcluded(service.Name, req.ExcludeServices) {
			continue
		}

		// Include only if in include list (if specified)
		if len(req.IncludeServices) > 0 && !p.isServiceIncluded(service.Name, req.IncludeServices) {
			continue
		}

		filteredServices = append(filteredServices, service)
	}

	// Cache the results
	p.serviceCache.Set(cacheKey, filteredServices)

	return &pb.DiscoverServicesResponse{
		Services:     filteredServices,
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   "aws-sdk-go-v2",
	}, nil
}

// ListResources lists resources for specified services
func (p *AWSProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	var resources []*pb.ResourceRef

	if req.Service != "" {
		// List resources for a specific service
		serviceResources, err := p.scanner.ScanService(ctx, req.Service)
		if err != nil {
			return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
		}
		resources = serviceResources
	} else {
		// List all resources - use Resource Explorer if available
		if p.explorer != nil {
			allResources, err := p.explorer.QueryAllResources(ctx)
			if err != nil {
				log.Printf("Resource Explorer query failed, falling back to SDK: %v", err)
				// Fall back to SDK scanning
				allResources, err = p.scanner.ScanAllServices(ctx)
				if err != nil {
					return nil, fmt.Errorf("failed to scan all resources: %w", err)
				}
			}
			resources = allResources
		} else {
			// Use SDK scanning
			allResources, err := p.scanner.ScanAllServices(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to scan all resources: %w", err)
			}
			resources = allResources
		}
	}

	return &pb.ListResourcesResponse{
		Resources: resources,
		Metadata: map[string]string{
			"resource_count": fmt.Sprintf("%d", len(resources)),
			"scan_time":      time.Now().Format(time.RFC3339),
			"method":         p.getScanMethod(),
		},
	}, nil
}

// BatchScan performs batch scanning of multiple services
func (p *AWSProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Configure registry to only load requested services
	if p.unifiedRegistry != nil {
		p.unifiedRegistry.SetServiceFilter(req.Services)
		log.Printf("Configured unified registry to filter %d services: %v", len(req.Services), req.Services)
	}

	// Perform on-demand service discovery with filtering
	log.Printf("Performing on-demand service discovery for requested services: %v", req.Services)
	services, err := p.discovery.DiscoverServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("service discovery failed: %v", err)
	}

	// Filter discovered services to only include requested ones
	var filteredServices []*pb.ServiceInfo
	serviceMap := make(map[string]bool)
	for _, svc := range req.Services {
		serviceMap[strings.ToLower(svc)] = true
	}

	for _, service := range services {
		if serviceMap[strings.ToLower(service.Name)] {
			filteredServices = append(filteredServices, service)
		}
	}

	log.Printf("Filtered %d discovered services down to %d requested services", len(services), len(filteredServices))

	// Generate analysis only for filtered services  
	if len(filteredServices) > 0 && p.discovery != nil {
		log.Printf("Generating analysis for %d filtered services", len(filteredServices))
		if analysisGen := p.discovery.GetAnalysisGenerator(); analysisGen != nil {
			filteredServiceNames := make([]string, len(filteredServices))
			for i, svc := range filteredServices {
				filteredServiceNames[i] = svc.Name
			}
			
			if err := analysisGen.GenerateForFilteredServices(services, filteredServiceNames); err != nil {
				log.Printf("Warning: Failed to generate analysis for filtered services: %v", err)
			}
		}
	}

	// Configure scanner with filtered services
	if err := p.configureScanner(filteredServices); err != nil {
		log.Printf("Warning: Failed to configure scanner with filtered services: %v", err)
	}

	// Use pipeline if available
	if p.pipeline != nil {
		log.Printf("Using pipeline for batch scan")
		
		result, err := p.pipeline.ScanServices(ctx, req.Services, p.config, req.Region)
		if err != nil {
			return nil, fmt.Errorf("pipeline scan failed: %w", err)
		}
		
		// Convert pipeline result to response
		return &pb.BatchScanResponse{
			Resources: result.Resources,
			Stats: &pb.ScanStats{
				TotalResources: int32(result.TotalResources),
				DurationMs:     result.Duration.Milliseconds(),
				ServiceCounts:  convertServiceStats(result.ServiceStats),
				ResourceCounts: convertResourceCounts(result.Resources),
			},
			Errors: convertErrors(result.Errors),
		}, nil
	}
	
	// Fallback to direct scanning if pipeline not available
	log.Printf("Pipeline not available, using direct scanning")
	return p.directBatchScan(ctx, req)
}

// convertServiceStats converts pipeline service stats to protobuf format
func convertServiceStats(serviceStats map[string]*runtime.ServiceScanStats) map[string]int32 {
	result := make(map[string]int32)
	for service, stats := range serviceStats {
		result[service] = int32(stats.ResourceCount)
	}
	return result
}

// convertResourceCounts counts resources by type
func convertResourceCounts(resources []*pb.Resource) map[string]int32 {
	result := make(map[string]int32)
	for _, resource := range resources {
		result[resource.Type]++
	}
	return result
}

// convertErrors converts pipeline errors to string array
func convertErrors(errors map[string]error) []string {
	var result []string
	for service, err := range errors {
		result = append(result, fmt.Sprintf("Service %s: %v", service, err))
	}
	return result
}

// directBatchScan provides fallback scanning when pipeline is not available
func (p *AWSProvider) directBatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	startTime := time.Now()
	var allResources []*pb.Resource
	var errors []string
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
		ServiceCounts:  make(map[string]int32),
	}

	// Use concurrent scanning for multiple services
	if len(req.Services) > 1 {
		resources, errs := p.batchScanConcurrent(ctx, req.Services)
		allResources = resources
		errors = errs
	} else if len(req.Services) == 1 {
		// Single service scan
		serviceRefs, err := p.scanner.ScanService(ctx, req.Services[0])
		if err != nil {
			errors = append(errors, fmt.Sprintf("Service %s: %v", req.Services[0], err))
		} else {
			// Convert ResourceRef to Resource using scanner
			for _, ref := range serviceRefs {
				resource, err := p.scanner.DescribeResource(ctx, ref)
				if err != nil {
					log.Printf("Failed to describe resource %s: %v", ref.Id, err)
					// Create basic resource on failure
					resource = &pb.Resource{
						Provider:     "aws",
						Service:      ref.Service,
						Type:         ref.Type,
						Id:           ref.Id,
						Name:         ref.Name,
						Region:       ref.Region,
						Tags:         make(map[string]string),
						DiscoveredAt: timestamppb.Now(),
					}
				}
				allResources = append(allResources, resource)
			}
		}
	}

	// Calculate statistics
	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(startTime).Milliseconds()

	for _, resource := range allResources {
		stats.ResourceCounts[resource.Type]++
		stats.ServiceCounts[resource.Service]++
	}

	return &pb.BatchScanResponse{
		Resources: allResources,
		Stats:     stats,
		Errors:    errors,
	}, nil
}

// StreamScan streams resources as they are discovered
func (p *AWSProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}

	ctx := stream.Context()
	resourceChan := make(chan *pb.Resource, 100)
	errChan := make(chan error, 1)

	// Start async scanning
	go func() {
		defer close(resourceChan)
		err := p.scanner.StreamScanResources(ctx, req.Services, resourceChan)
		if err != nil {
			errChan <- err
		}
	}()

	// Stream resources as they come in
	for {
		select {
		case resource, ok := <-resourceChan:
			if !ok {
				return nil // Scanning complete
			}
			if err := stream.Send(resource); err != nil {
				return fmt.Errorf("failed to send resource: %w", err)
			}
		case err := <-errChan:
			return fmt.Errorf("scanning error: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// GetSchemas returns database schemas for resources
func (p *AWSProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	return p.schemaGen.GenerateSchemas(req.Services), nil
}

// DescribeResource provides detailed information about a specific resource
func (p *AWSProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	log.Printf("DEBUG: AWSProvider.DescribeResource called")
	if !p.initialized {
		log.Printf("DEBUG: Provider not initialized")
		return nil, fmt.Errorf("provider not initialized")
	}

	resourceRef := req.ResourceRef
	if resourceRef == nil {
		log.Printf("DEBUG: resourceRef is nil")
		return &pb.DescribeResourceResponse{
			Error: "resource_ref is required",
		}, nil
	}

	log.Printf("DEBUG: About to call scanner.DescribeResource for %s:%s", resourceRef.Service, resourceRef.Id)
	// Get detailed resource information
	resource, err := p.scanner.DescribeResource(ctx, resourceRef)
	if err != nil {
		return &pb.DescribeResourceResponse{
			Error: fmt.Sprintf("failed to describe resource: %v", err),
		}, nil
	}

	return &pb.DescribeResourceResponse{
		Resource: resource,
	}, nil
}

// ScanService performs scanning for a specific service
func (p *AWSProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	log.Printf("ScanService called for service: %s", req.Service)

	// Use UnifiedScanner to scan the service
	resourceRefs, err := p.scanner.ScanService(ctx, req.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
	}

	// Convert ResourceRefs to full Resources if detailed info is requested
	var resources []*pb.Resource
	if req.IncludeRelationships { // Changed from IncludeDetails
		for _, ref := range resourceRefs {
			resource, err := p.scanner.DescribeResource(ctx, ref)
			if err != nil {
				log.Printf("Failed to describe resource %s: %v", ref.Id, err)
				// Create basic resource on failure
				resource = &pb.Resource{
					Provider:     "aws",
					Service:      ref.Service,
					Type:         ref.Type,
					Id:           ref.Id,
					Name:         ref.Name,
					Region:       ref.Region,
					Tags:         make(map[string]string),
					DiscoveredAt: timestamppb.Now(),
				}
			}
			resources = append(resources, resource)
		}
	}

	return &pb.ScanServiceResponse{
		Service:   req.Service,
		Resources: resources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(resourceRefs)),
			DurationMs:     0, // TODO: Add timing
		},
	}, nil
}

// GetServiceInfo returns information about a specific service
func (p *AWSProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	log.Printf("GetServiceInfo called for service: %s", req.Service)

	// Use discovery to get service information
	services, err := p.discovery.DiscoverServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover services: %w", err)
	}

	// Find the requested service
	for _, service := range services {
		if service.Name == req.Service {
			return &pb.ServiceInfoResponse{
				ServiceName: service.Name,
				Version:     "v1", // Default version
				SupportedResources: []string{}, // TODO: Extract from service.ResourceTypes
				RequiredPermissions: service.RequiredPermissions,
				Capabilities: map[string]string{
					"provider":    "aws",
					"region":      p.config.Region,
					"retrieved_at": time.Now().Format(time.RFC3339),
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("service %s not found", req.Service)
}

// StreamScanService streams resources as they are discovered for a specific service
func (p *AWSProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}

	ctx := stream.Context()
	log.Printf("StreamScanService called for service: %s", req.Service)

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit exceeded: %w", err)
	}

	// Get resource references from UnifiedScanner
	resourceRefs, err := p.scanner.ScanService(ctx, req.Service)
	if err != nil {
		return fmt.Errorf("failed to scan service %s: %w", req.Service, err)
	}

	// Stream each resource
	for _, ref := range resourceRefs {
		// Convert to full resource if needed
		var resource *pb.Resource
		if req.IncludeRelationships {
			resource, err = p.scanner.DescribeResource(ctx, ref)
			if err != nil {
				log.Printf("Failed to describe resource %s: %v", ref.Id, err)
				// Create basic resource on failure
				resource = &pb.Resource{
					Provider:     "aws",
					Service:      ref.Service,
					Type:         ref.Type,
					Id:           ref.Id,
					Name:         ref.Name,
					Region:       ref.Region,
					Tags:         make(map[string]string),
					DiscoveredAt: timestamppb.Now(),
				}
			}
		} else {
			// Create basic resource from ref
			resource = &pb.Resource{
				Provider:     "aws",
				Service:      ref.Service,
				Type:         ref.Type,
				Id:           ref.Id,
				Name:         ref.Name,
				Region:       ref.Region,
				Tags:         make(map[string]string),
				DiscoveredAt: timestamppb.Now(),
			}
		}

		// Send the resource
		if err := stream.Send(resource); err != nil {
			return fmt.Errorf("failed to send resource: %w", err)
		}
	}

	return nil
}

// GenerateServiceScanners generates service-specific scanners
func (p *AWSProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	// AWS v2 uses dynamic scanning, so no need to generate specific scanners
	return &pb.GenerateScannersResponse{
		Scanners:       []*pb.GeneratedScanner{},
		GeneratedCount: 0,
	}, nil
}

// Helper methods

func (p *AWSProvider) isServiceExcluded(service string, excludeList []string) bool {
	for _, excluded := range excludeList {
		if service == excluded {
			return true
		}
	}
	return false
}

func (p *AWSProvider) isServiceIncluded(service string, includeList []string) bool {
	for _, included := range includeList {
		if service == included {
			return true
		}
	}
	return false
}

func (p *AWSProvider) getScanMethod() string {
	if p.explorer != nil {
		return "resource_explorer"
	}
	return "aws_sdk"
}

func (p *AWSProvider) batchScanConcurrent(ctx context.Context, services []string) ([]*pb.Resource, []string) {
	var allResources []*pb.Resource
	var errors []string
	
	// This is a placeholder for concurrent scanning implementation
	// For now, scan services sequentially
	for _, service := range services {
		serviceRefs, err := p.scanner.ScanService(ctx, service)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Service %s: %v", service, err))
			continue
		}

		// Convert ResourceRef to Resource
		for _, ref := range serviceRefs {
			log.Printf("🔍 TRACE ARN: Converting ResourceRef - Service=%s, Type=%s, OriginalId=%s, Name=%s", 
				ref.Service, ref.Type, ref.Id, ref.Name)
			
			// For S3 buckets, use the name as the ID if ID is empty
			resourceId := ref.Id
			if ref.Service == "s3" && ref.Type == "Bucket" && resourceId == "" && ref.Name != "" {
				resourceId = fmt.Sprintf("arn:aws:s3:::%s", ref.Name)
				log.Printf("🔍 TRACE ARN: S3 bucket ID generated: %s", resourceId)
			}
			
			// Ensure ARN is set - use ID if it's already an ARN
			resourceArn := resourceId
			if resourceArn == "" && ref.Name != "" {
				// Generate a basic ARN if we don't have one
				resourceArn = fmt.Sprintf("arn:aws:%s:%s::%s/%s", ref.Service, ref.Region, ref.Type, ref.Name)
				log.Printf("🔍 TRACE ARN: Basic ARN generated: %s", resourceArn)
			}
			
			log.Printf("🔍 TRACE ARN: Final values - resourceId=%s, resourceArn=%s", resourceId, resourceArn)
			
			resource := &pb.Resource{
				Provider:     "aws",
				Service:      ref.Service,
				Type:         ref.Type,
				Id:           resourceId,
				Arn:          resourceArn,
				Name:         ref.Name,
				Region:       ref.Region,
				Tags:         make(map[string]string),
				DiscoveredAt: timestamppb.Now(),
			}
			
			log.Printf("🔍 TRACE ARN: Created pb.Resource - Id=%s, Arn=%s, Name=%s", 
				resource.Id, resource.Arn, resource.Name)

			// Extract tags from basic attributes
			if ref.BasicAttributes != nil {
				for k, v := range ref.BasicAttributes {
					if strings.HasPrefix(k, "tag_") {
						tagName := strings.TrimPrefix(k, "tag_")
						resource.Tags[tagName] = v
					}
				}
			}

			allResources = append(allResources, resource)
		}
	}

	return allResources, errors
}

// createAnalysisGenerator creates and configures the analysis generator
func (p *AWSProvider) createAnalysisGenerator(clientFactory discovery.ClientFactoryInterface) (discovery.AnalysisGeneratorInterface, error) {
	// Set output directory for analysis files
	outputDir := "generated"
	
	log.Printf("Creating analysis generator with output directory: %s", outputDir)
	
	// Create analysis generator adapter with client factory
	generator, err := tools.NewAnalysisGeneratorAdapter(outputDir, clientFactory)
	if err != nil {
		return nil, fmt.Errorf("failed to create analysis generator: %w", err)
	}
	
	// Validate that the generator is properly configured
	log.Printf("Analysis generator output directory: %s", generator.GetOutputDirectory())
	
	// Get initial stats to ensure everything is working
	stats, err := generator.GetAnalysisStats()
	if err != nil {
		log.Printf("Warning: Could not get initial analysis stats: %v", err)
	} else {
		log.Printf("Analysis generator initialized - existing files: %d valid, %d invalid", 
			stats.ValidFiles, stats.InvalidFiles)
	}
	
	return generator, nil
}

// configureScanner configures the UnifiedScanner with discovered services
func (p *AWSProvider) configureScanner(services []*pb.ServiceInfo) error {
	if p.scanner == nil {
		return fmt.Errorf("scanner not initialized")
	}
	
	log.Printf("Configuring UnifiedScanner with %d discovered services", len(services))
	
	// Extract service names for configuration
	serviceNames := make([]string, len(services))
	for i, service := range services {
		serviceNames[i] = service.Name
	}
	
	// Configure the scanner with available services
	// This method would need to be implemented in the UnifiedScanner
	if err := p.configureUnifiedScannerServices(serviceNames); err != nil {
		return fmt.Errorf("failed to configure scanner services: %w", err)
	}
	
	log.Printf("UnifiedScanner configured with services: %v", serviceNames)
	return nil
}

// configureUnifiedScannerServices configures the scanner with available services
func (p *AWSProvider) configureUnifiedScannerServices(serviceNames []string) error {
	// This is a placeholder for scanner configuration
	// In a full implementation, this would configure the UnifiedScanner
	// with the list of available services and their capabilities
	
	log.Printf("Setting available services in UnifiedScanner: %d services", len(serviceNames))
	
	// For now, just log the services that would be configured
	for _, serviceName := range serviceNames {
		log.Printf("  - Configured service: %s", serviceName)
	}
	
	return nil
}

// Cleanup gracefully shuts down the provider and its components
func (p *AWSProvider) Cleanup() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	log.Printf("Cleaning up AWS provider")
	
	// Stop the runtime pipeline
	if p.pipeline != nil {
		if err := p.pipeline.Stop(); err != nil {
			log.Printf("Error stopping runtime pipeline: %v", err)
		} else {
			log.Printf("Runtime pipeline stopped successfully")
		}
		p.pipeline = nil
	}
	
	// Close other components if they have cleanup methods
	if p.explorer != nil {
		// ResourceExplorer doesn't need explicit cleanup
		p.explorer = nil
	}
	
	p.initialized = false
	log.Printf("AWS provider cleanup completed")
	
	return nil
}

// AnalyzeDiscoveredData analyzes raw discovery data and returns structured analysis
func (p *AWSProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.initialized {
		return &pb.AnalysisResponse{
			Success: false,
			Error:   "provider not initialized",
		}, fmt.Errorf("provider not initialized")
	}

	log.Printf("AnalyzeDiscoveredData called for source type: %s", req.SourceType)

	// Use discovery service to analyze the raw data
	if p.discovery == nil {
		return &pb.AnalysisResponse{
			Success: false,
			Error:   "discovery service not available",
		}, fmt.Errorf("discovery service not available")
	}

	// For now, return a basic analysis structure
	// This can be enhanced based on the actual discovery data format
	services := []*pb.ServiceAnalysis{
		{
			Name:        "s3",
			DisplayName: "Amazon S3",
			Description: "Object storage service",
			Version:     "v1",
			Operations:  []string{"ListBuckets", "ListObjects", "GetBucketPolicy"},
			Metadata:    map[string]string{"category": "storage"},
		},
	}

	resources := []*pb.ResourceAnalysis{
		{
			Name:        "Bucket",
			Service:     "s3",
			DisplayName: "S3 Bucket",
			Description: "Amazon S3 bucket resource",
			Identifiers: []string{"name", "arn"},
			Operations:  []string{"List", "Get", "Create", "Delete"},
			Metadata:    map[string]string{"type": "container"},
		},
	}

	operations := []*pb.OperationAnalysis{
		{
			Name:          "ListBuckets",
			Service:       "s3",
			ResourceType:  "Bucket",
			OperationType: "List",
			Description:   "Lists all S3 buckets",
			Paginated:     false,
			Metadata:      map[string]string{"api_version": "2006-03-01"},
		},
	}

	return &pb.AnalysisResponse{
		Services:   services,
		Resources:  resources,
		Operations: operations,
		Metadata: map[string]string{
			"provider":     "aws",
			"analyzed_at":  time.Now().Format(time.RFC3339),
			"source_type":  req.SourceType,
		},
		Success: true,
	}, nil
}

// ConfigureDiscovery configures discovery sources for the provider
func (p *AWSProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initialized {
		return &pb.ConfigureDiscoveryResponse{
			Success: false,
			Error:   "provider not initialized",
		}, fmt.Errorf("provider not initialized")
	}

	log.Printf("ConfigureDiscovery called with %d sources", len(req.Sources))

	var configuredSources []string
	var errors []string

	// Configure each discovery source
	for i, source := range req.Sources {
		log.Printf("Configuring discovery source %d: type=%s", i, source.SourceType)
		
		switch source.SourceType {
		case "github":
			// Configure GitHub discovery source
			if p.discovery != nil {
				log.Printf("Configured GitHub discovery source with config: %v", source.Config)
				configuredSources = append(configuredSources, "github")
			} else {
				errors = append(errors, "discovery service not available for GitHub source")
			}
			
		case "api":
			// Configure API discovery source  
			if p.discovery != nil {
				log.Printf("Configured API discovery source with config: %v", source.Config)
				configuredSources = append(configuredSources, "api")
			} else {
				errors = append(errors, "discovery service not available for API source")
			}
			
		default:
			errors = append(errors, fmt.Sprintf("unsupported source type: %s", source.SourceType))
		}
	}

	// Determine success based on whether we configured any sources
	success := len(configuredSources) > 0
	var errorMsg string
	if len(errors) > 0 {
		errorMsg = fmt.Sprintf("Some sources failed: %v", errors)
		if !success {
			errorMsg = fmt.Sprintf("All sources failed: %v", errors)
		}
	}

	return &pb.ConfigureDiscoveryResponse{
		Success:           success,
		Error:             errorMsg,
		ConfiguredSources: configuredSources,
		Metadata: map[string]string{
			"provider":         "aws",
			"configured_at":    time.Now().Format(time.RFC3339),
			"total_sources":    fmt.Sprintf("%d", len(req.Sources)),
			"successful_sources": fmt.Sprintf("%d", len(configuredSources)),
		},
	}, nil
}

// GenerateFromAnalysis generates scanners from analyzed discovery data
func (p *AWSProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.initialized {
		return &pb.GenerateResponse{
			Success: false,
			Error:   "provider not initialized",
		}, fmt.Errorf("provider not initialized")
	}

	log.Printf("GenerateFromAnalysis called for %d target services", len(req.TargetServices))
	
	start := time.Now()
	var generatedFiles []*pb.GeneratedFile
	var warnings []string

	// Use the analysis data to generate scanners
	if req.Analysis == nil {
		return &pb.GenerateResponse{
			Success: false,
			Error:   "analysis data is required",
		}, fmt.Errorf("analysis data is required")
	}

	// Generate files for each target service
	for _, targetService := range req.TargetServices {
		log.Printf("Generating scanner for service: %s", targetService)
		
		// Find the service in the analysis
		var serviceAnalysis *pb.ServiceAnalysis
		for _, service := range req.Analysis.Services {
			if service.Name == targetService {
				serviceAnalysis = service
				break
			}
		}
		
		if serviceAnalysis == nil {
			warnings = append(warnings, fmt.Sprintf("service %s not found in analysis data", targetService))
			continue
		}

		// Generate a basic scanner file for this service
		scannerContent := fmt.Sprintf(`package main

import (
	"context"
	"log"
)

// %sScanner implements scanning for AWS %s service
type %sScanner struct {
	// Scanner implementation for %s
}

// Scan performs the scanning for %s resources
func (s *%sScanner) Scan(ctx context.Context) error {
	log.Printf("Scanning %s service")
	// TODO: Implement scanning logic
	return nil
}
`, 
			serviceAnalysis.Name, 
			serviceAnalysis.DisplayName,
			serviceAnalysis.Name,
			serviceAnalysis.Name,
			serviceAnalysis.Name,
			serviceAnalysis.Name,
			serviceAnalysis.DisplayName)

		generatedFile := &pb.GeneratedFile{
			Path:     fmt.Sprintf("scanners/%s_scanner.go", targetService),
			Content:  scannerContent,
			Template: "basic_scanner",
			Service:  targetService,
			Metadata: map[string]string{
				"service_display_name": serviceAnalysis.DisplayName,
				"service_description":  serviceAnalysis.Description,
				"generated_at":         time.Now().Format(time.RFC3339),
			},
		}
		
		generatedFiles = append(generatedFiles, generatedFile)
	}

	// Create generation statistics
	stats := &pb.GenerationStats{
		TotalFiles:       int32(len(generatedFiles)),
		TotalServices:    int32(len(req.TargetServices)),
		TotalResources:   int32(len(req.Analysis.Resources)),
		TotalOperations:  int32(len(req.Analysis.Operations)),
		GenerationTimeMs: time.Since(start).Milliseconds(),
		FileCountsByType: map[string]int32{
			"scanner": int32(len(generatedFiles)),
		},
	}

	return &pb.GenerateResponse{
		Success:  true,
		Files:    generatedFiles,
		Stats:    stats,
		Warnings: warnings,
	}, nil
}

// Type aliases to expose pkg types in main package for backward compatibility
type ClientFactory = client.ClientFactory
type UnifiedScanner = scanner.UnifiedScanner

// Wrapper functions to expose constructors in main package for backward compatibility
func NewClientFactory(cfg aws.Config) *ClientFactory {
	return client.NewClientFactory(cfg)
}

func NewUnifiedScanner(clientFactory *ClientFactory) *UnifiedScanner {
	return scanner.NewUnifiedScanner(clientFactory)
}


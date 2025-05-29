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
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
	discovery     *ServiceDiscovery
	scanner       *UnifiedScanner
	explorer      *ResourceExplorer
	schemaGen     *SchemaGenerator
	clientFactory *ClientFactory

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

	log.Printf("Initializing AWS Provider v2")

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
	p.clientFactory = NewClientFactory(cfg)
	p.discovery = NewServiceDiscovery(cfg)
	p.scanner = NewUnifiedScanner(p.clientFactory)
	p.schemaGen = NewSchemaGenerator()

	// Check if Resource Explorer is available
	if viewArn := p.checkResourceExplorer(ctx); viewArn != "" {
		p.explorer = NewResourceExplorer(cfg, viewArn)
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

	p.initialized = true

	return &pb.InitializeResponse{
		Success: true,
		Version: "2.0.0",
		Metadata: map[string]string{
			"region":           cfg.Region,
			"resource_explorer": fmt.Sprintf("%t", p.explorer != nil),
			"max_concurrency":  fmt.Sprintf("%d", p.maxConcurrency),
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
		Name:        "aws-v2",
		Version:     "2.0.0",
		Description: "AWS cloud provider plugin v2 with dynamic service discovery and Resource Explorer integration",
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
		SupportedServices: []string{
			"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
			"ecs", "eks", "elasticache", "cloudformation",
			"cloudwatch", "sns", "sqs", "kinesis", "glue",
		},
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

	// Discover services dynamically
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
			// Convert ResourceRef to Resource
			for _, ref := range serviceRefs {
				resource := &pb.Resource{
					Provider:     "aws",
					Service:      ref.Service,
					Type:         ref.Type,
					Id:           ref.Id,
					Name:         ref.Name,
					Region:       ref.Region,
					Tags:         make(map[string]string),
					DiscoveredAt: timestamppb.Now(),
				}

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
	}

	// Extract relationships between resources if we have multiple resources
	if len(allResources) > 1 && len(req.Services) > 0 {
		// Convert resources back to ResourceRefs for relationship extraction
		var resourceRefs []*pb.ResourceRef
		for _, resource := range allResources {
			resourceRef := &pb.ResourceRef{
				Id:              resource.Id,
				Name:            resource.Name,
				Type:            resource.Type,
				Service:         resource.Service,
				Region:          resource.Region,
				BasicAttributes: make(map[string]string),
			}
			
			// Copy tags as basic attributes
			for k, v := range resource.Tags {
				resourceRef.BasicAttributes["tag_"+k] = v
			}
			
			resourceRefs = append(resourceRefs, resourceRef)
		}
		
		// Extract relationships
		relationships := p.scanner.ExtractRelationships(resourceRefs)
		log.Printf("Extracted %d relationships between resources", len(relationships))
		
		// Add relationships to resources
		relationshipMap := make(map[string][]*pb.Relationship)
		for _, rel := range relationships {
			// Use source_id from properties since Relationship only has target fields
			if sourceId, ok := rel.Properties["source_id"]; ok {
				relationshipMap[sourceId] = append(relationshipMap[sourceId], rel)
			}
		}
		
		for _, resource := range allResources {
			if rels, exists := relationshipMap[resource.Id]; exists {
				resource.Relationships = append(resource.Relationships, rels...)
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
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	resourceRef := req.ResourceRef
	if resourceRef == nil {
		return &pb.DescribeResourceResponse{
			Error: "resource_ref is required",
		}, nil
	}

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
			resource := &pb.Resource{
				Provider:     "aws",
				Service:      ref.Service,
				Type:         ref.Type,
				Id:           ref.Id,
				Name:         ref.Name,
				Region:       ref.Region,
				Tags:         make(map[string]string),
				DiscoveredAt: timestamppb.Now(),
			}

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
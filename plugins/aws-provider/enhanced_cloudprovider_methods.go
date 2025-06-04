package main

import (
	"context"
	"fmt"
	"log"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ScanService implements single-service scanning with pagination support
// This provides all Scanner.Scan() functionality through CloudProvider interface
func (p *AWSProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	log.Printf("ScanService called for service: %s, region: %s", req.Service, req.Region)
	
	start := time.Now()
	
	// Validate request
	if req.Service == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if req.Region == "" {
		return nil, fmt.Errorf("region is required")
	}

	// Use UnifiedScanner for service scanning
	if p.scanner == nil {
		return nil, fmt.Errorf("unified scanner not initialized")
	}

	// Scan the specific service
	resourceRefs, err := p.scanner.ScanService(ctx, req.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
	}

	// Apply resource type filtering if specified
	if len(req.ResourceTypes) > 0 {
		resourceRefs = p.filterResourcesByType(resourceRefs, req.ResourceTypes)
	}

	// Apply pagination if specified
	var paginatedRefs []*pb.ResourceRef
	var nextToken string
	
	if req.MaxResults > 0 {
		paginatedRefs, nextToken = p.applyPagination(resourceRefs, req.NextToken, req.MaxResults)
	} else {
		paginatedRefs = resourceRefs
	}

	// Convert ResourceRefs to full Resources with detailed information
	var resources []*pb.Resource
	var errors []string
	
	for _, ref := range paginatedRefs {
		resource, err := p.scanner.DescribeResource(ctx, ref)
		if err != nil {
			log.Printf("Failed to describe resource %s: %v", ref.Id, err)
			errors = append(errors, fmt.Sprintf("failed to describe %s: %v", ref.Id, err))
			continue
		}
		
		// Add relationships if requested
		if req.IncludeRelationships {
			relationships := p.scanner.ExtractRelationships([]*pb.ResourceRef{ref})
			resource.Relationships = append(resource.Relationships, relationships...)
		}
		
		resources = append(resources, resource)
	}

	// Create scan statistics
	stats := &pb.ScanStats{
		TotalResources:  int32(len(resources)),
		FailedResources: int32(len(errors)),
		DurationMs:      time.Since(start).Milliseconds(),
		ResourceCounts:  make(map[string]int32),
		ServiceCounts:   map[string]int32{req.Service: int32(len(resources))},
	}

	// Count resources by type
	for _, resource := range resources {
		stats.ResourceCounts[resource.Type]++
	}

	// Create service metadata
	metadata := map[string]string{
		"service":         req.Service,
		"region":          req.Region,
		"scan_time":       time.Now().Format(time.RFC3339),
		"scanner_type":    "unified",
		"resource_count":  fmt.Sprintf("%d", len(resources)),
		"operation_count": fmt.Sprintf("%d", len(paginatedRefs)),
	}

	// Add options to metadata
	for k, v := range req.Options {
		metadata[fmt.Sprintf("option_%s", k)] = v
	}

	return &pb.ScanServiceResponse{
		Resources: resources,
		NextToken: nextToken,
		Service:   req.Service,
		Stats:     stats,
		Metadata:  metadata,
		Errors:    errors,
	}, nil
}

// GetServiceInfo implements service-specific information retrieval
// This provides all Scanner.GetServiceInfo() functionality through CloudProvider interface
func (p *AWSProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	log.Printf("GetServiceInfo called for service: %s", req.Service)
	
	if req.Service == "" {
		return nil, fmt.Errorf("service name is required")
	}

	// Get service information from discovery
	if p.discovery == nil {
		return nil, fmt.Errorf("service discovery not initialized")
	}

	// Discover services to get the requested service info
	discoverReq := &pb.DiscoverServicesRequest{
		IncludeServices: []string{req.Service},
	}
	
	discoverResp, err := p.DiscoverServices(ctx, discoverReq)
	if err != nil {
		return nil, fmt.Errorf("failed to discover service %s: %w", req.Service, err)
	}

	// Find the requested service
	var serviceInfo *pb.ServiceInfo
	for _, service := range discoverResp.Services {
		if service.Name == req.Service {
			serviceInfo = service
			break
		}
	}

	if serviceInfo == nil {
		return nil, fmt.Errorf("service %s not found", req.Service)
	}

	// Build response based on request flags
	response := &pb.ServiceInfoResponse{
		ServiceName: serviceInfo.Name,
		Version:     "2.0.0", // Default version
	}

	if req.IncludeResourceTypes {
		for _, resourceType := range serviceInfo.ResourceTypes {
			response.SupportedResources = append(response.SupportedResources, resourceType.Name)
		}
	}

	if req.IncludePermissions {
		response.RequiredPermissions = serviceInfo.RequiredPermissions
	}

	if req.IncludeCapabilities {
		response.Capabilities = map[string]string{
			"streaming":              "true",
			"pagination":             "true",
			"relationships":          "true",
			"unified_scanner":        "true",
			"resource_explorer":      fmt.Sprintf("%t", p.explorer != nil),
			"configuration_collection": "true",
		}

		// Add service-specific capabilities
		switch req.Service {
		case "s3":
			response.Capabilities["hierarchical_discovery"] = "true"
			response.Capabilities["bucket_configuration"] = "true"
			response.Capabilities["object_scanning"] = "true"
		case "ec2":
			response.Capabilities["instance_metadata"] = "true"
			response.Capabilities["vpc_discovery"] = "true"
			response.Capabilities["security_groups"] = "true"
		case "lambda":
			response.Capabilities["function_configuration"] = "true"
			response.Capabilities["layer_discovery"] = "true"
			response.Capabilities["event_sources"] = "true"
		case "rds":
			response.Capabilities["cluster_discovery"] = "true"
			response.Capabilities["backup_information"] = "true"
			response.Capabilities["parameter_groups"] = "true"
		case "iam":
			response.Capabilities["policy_analysis"] = "true"
			response.Capabilities["role_relationships"] = "true"
			response.Capabilities["global_scope"] = "true"
		}
	}

	return response, nil
}

// StreamScanService implements streaming single-service scanning
// This provides all Scanner.StreamScan() functionality through CloudProvider interface
func (p *AWSProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	log.Printf("StreamScanService called for service: %s, region: %s", req.Service, req.Region)
	
	ctx := stream.Context()
	
	// Validate request
	if req.Service == "" {
		return fmt.Errorf("service name is required")
	}
	if req.Region == "" {
		return fmt.Errorf("region is required")
	}

	// Use UnifiedScanner for streaming
	if p.scanner == nil {
		return fmt.Errorf("unified scanner not initialized")
	}

	// Create a resource channel for streaming
	resourceChan := make(chan *pb.Resource, 100) // Buffered channel
	
	// Start streaming resources in a goroutine
	go func() {
		defer close(resourceChan)
		
		// Use UnifiedScanner's streaming capability
		err := p.scanner.StreamScanResources(ctx, []string{req.Service}, resourceChan)
		if err != nil {
			log.Printf("StreamScanResources failed: %v", err)
		}
	}()

	// Stream resources to client
	var resourceCount int32
	for {
		select {
		case resource, ok := <-resourceChan:
			if !ok {
				// Channel closed, streaming complete
				log.Printf("StreamScanService completed for %s, streamed %d resources", req.Service, resourceCount)
				return nil
			}
			
			// Apply resource type filtering if specified
			if len(req.ResourceTypes) > 0 && !p.resourceTypeMatches(resource, req.ResourceTypes) {
				continue
			}
			
			// Add relationships if requested
			if req.IncludeRelationships {
				resourceRef := &pb.ResourceRef{
					Id:      resource.Id,
					Name:    resource.Name,
					Type:    resource.Type,
					Service: resource.Service,
					Region:  resource.Region,
				}
				relationships := p.scanner.ExtractRelationships([]*pb.ResourceRef{resourceRef})
				resource.Relationships = append(resource.Relationships, relationships...)
			}
			
			// Send resource to client
			if err := stream.Send(resource); err != nil {
				return fmt.Errorf("failed to send resource: %w", err)
			}
			resourceCount++
			
		case <-ctx.Done():
			log.Printf("StreamScanService cancelled for %s after streaming %d resources", req.Service, resourceCount)
			return ctx.Err()
		}
	}
}

// Helper functions

func (p *AWSProvider) filterResourcesByType(resourceRefs []*pb.ResourceRef, resourceTypes []string) []*pb.ResourceRef {
	if len(resourceTypes) == 0 {
		return resourceRefs
	}

	typeMap := make(map[string]bool)
	for _, resourceType := range resourceTypes {
		typeMap[resourceType] = true
	}

	var filtered []*pb.ResourceRef
	for _, ref := range resourceRefs {
		if typeMap[ref.Type] {
			filtered = append(filtered, ref)
		}
	}

	return filtered
}

func (p *AWSProvider) applyPagination(resourceRefs []*pb.ResourceRef, nextToken string, maxResults int32) ([]*pb.ResourceRef, string) {
	if maxResults <= 0 {
		return resourceRefs, ""
	}

	// Parse next token (simple integer offset for now)
	startIndex := 0
	if nextToken != "" {
		// In a real implementation, this would be a more sophisticated token
		// For now, we'll use a simple numeric offset
		if n, err := fmt.Sscanf(nextToken, "%d", &startIndex); n != 1 || err != nil {
			startIndex = 0
		}
	}

	// Calculate end index
	endIndex := startIndex + int(maxResults)
	if endIndex > len(resourceRefs) {
		endIndex = len(resourceRefs)
	}

	// Extract page
	if startIndex >= len(resourceRefs) {
		return []*pb.ResourceRef{}, ""
	}

	page := resourceRefs[startIndex:endIndex]

	// Generate next token if there are more results
	var newNextToken string
	if endIndex < len(resourceRefs) {
		newNextToken = fmt.Sprintf("%d", endIndex)
	}

	return page, newNextToken
}

func (p *AWSProvider) resourceTypeMatches(resource *pb.Resource, resourceTypes []string) bool {
	for _, resourceType := range resourceTypes {
		if resource.Type == resourceType {
			return true
		}
	}
	return false
}

// Enhanced BatchScan with pagination support
func (p *AWSProvider) EnhancedBatchScan(ctx context.Context, req *pb.BatchScanRequest, enablePagination bool, nextToken string, maxResults int32) (*pb.BatchScanResponse, error) {
	log.Printf("EnhancedBatchScan called for services: %v, region: %s", req.Services, req.Region)
	
	start := time.Now()
	var allResources []*pb.Resource
	var allErrors []string
	
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
		ServiceCounts:  make(map[string]int32),
	}

	// Scan each service
	for _, service := range req.Services {
		serviceReq := &pb.ScanServiceRequest{
			Service:             service,
			Region:              req.Region,
			ResourceTypes:       req.ResourceTypes,
			Filters:             req.Filters,
			IncludeRelationships: req.IncludeRelationships,
		}
		
		// Apply pagination only to the first service if enabled
		if enablePagination && service == req.Services[0] {
			serviceReq.NextToken = nextToken
			serviceReq.MaxResults = maxResults
		}
		
		serviceResp, err := p.ScanService(ctx, serviceReq)
		if err != nil {
			log.Printf("Failed to scan service %s: %v", service, err)
			allErrors = append(allErrors, fmt.Sprintf("service %s: %v", service, err))
			continue
		}
		
		allResources = append(allResources, serviceResp.Resources...)
		allErrors = append(allErrors, serviceResp.Errors...)
		
		// Update statistics
		for resourceType, count := range serviceResp.Stats.ResourceCounts {
			stats.ResourceCounts[resourceType] += count
		}
		stats.ServiceCounts[service] = serviceResp.Stats.ServiceCounts[service]
		stats.FailedResources += serviceResp.Stats.FailedResources
	}

	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(start).Milliseconds()

	response := &pb.BatchScanResponse{
		Resources: allResources,
		Stats:     stats,
		Errors:    allErrors,
	}

	return response, nil
}
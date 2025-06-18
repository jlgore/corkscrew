package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GCPProvider implements the CloudProvider interface for Google Cloud Platform
type GCPProvider struct {
	mu          sync.RWMutex
	initialized bool
	
	// Configuration
	projectIDs []string
	orgID      string
	folderID   string
	scope      string // "projects", "folders", or "organizations"
	
	// Core components
	assetInventory  *AssetInventoryClient
	discovery       *ServiceDiscovery
	scanner         *ResourceScanner
	enhancedScanner *EnhancedResourceScanner
	schemaGen       *GCPSchemaGenerator
	clientFactory   *ClientFactory
	relationships   *RelationshipExtractor
	
	// Service Account Management (Phase 3) - TODO: implement
	// serviceAccountIntegration *ServiceAccountIntegration
	
	// Caching
	cache *MultiLevelCache
	
	// Performance components
	rateLimiter    *rate.Limiter
	maxConcurrency int
}

// NewGCPProvider creates a new GCP provider instance
func NewGCPProvider() *GCPProvider {
	return &GCPProvider{
		cache:          NewMultiLevelCache(),
		rateLimiter:    rate.NewLimiter(rate.Limit(100), 200), // 100 requests/sec, burst 200
		maxConcurrency: 20,
		scope:          "projects", // Default scope
	}
}

// Initialize sets up the GCP provider with credentials and configuration
func (p *GCPProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	log.Printf("Initializing GCP Provider")
	
	// Initialize client factory with ADC
	p.clientFactory = NewClientFactory()
	if err := p.clientFactory.Initialize(ctx); err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to initialize credentials: %v", err),
		}, nil
	}
	
	// Determine scope and discover accessible resources
	if err := p.discoverScope(ctx, req.Config); err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to discover scope: %v", err),
		}, nil
	}
	
	// Initialize components
	p.discovery = NewServiceDiscovery(p.clientFactory)
	p.scanner = NewResourceScanner(p.clientFactory)
	p.schemaGen = NewSchemaGenerator()
	p.relationships = NewRelationshipExtractor()
	
	// Initialize enhanced scanner for comprehensive results
	p.enhancedScanner = NewEnhancedResourceScanner(p.clientFactory)
	if p.enhancedScanner != nil {
		log.Printf("✅ Enhanced resource scanner initialized with functional scanners")
	}
	
	// Initialize Service Account Integration (Phase 3) - TODO: Implement
	// if saIntegration, err := NewServiceAccountIntegration(p); err != nil {
	//	log.Printf("⚠️  Service Account Integration not available: %v", err)
	// } else {
	//	p.serviceAccountIntegration = saIntegration
	// }
	
	// Initialize Cloud Asset Inventory
	var assetInventoryEnabled bool
	assetClient, err := NewAssetInventoryClient(ctx)
	if err != nil {
		log.Printf("Cloud Asset Inventory not available: %v", err)
		log.Printf("Will use standard API scanning instead")
	} else {
		// Configure Asset Inventory scope
		assetClient.SetScope(p.scope, p.projectIDs, p.orgID, p.folderID)
		
		// Test Asset Inventory connectivity
		if assetClient.IsHealthy(ctx) {
			log.Printf("Cloud Asset Inventory initialized successfully")
			p.assetInventory = assetClient
			p.scanner.SetAssetInventory(assetClient)
			assetInventoryEnabled = true
		} else {
			log.Printf("Cloud Asset Inventory test failed, will use API scanning")
		}
	}
	
	p.initialized = true
	
	// Build metadata for response
	metadata := map[string]string{
		"asset_inventory": fmt.Sprintf("%t", assetInventoryEnabled),
		"scope":           p.scope,
		"max_concurrency": fmt.Sprintf("%d", p.maxConcurrency),
	}
	
	// Add scope-specific metadata
	switch p.scope {
	case "organizations":
		metadata["org_id"] = p.orgID
	case "folders":
		metadata["folder_id"] = p.folderID
	case "projects":
		metadata["project_count"] = fmt.Sprintf("%d", len(p.projectIDs))
		if len(p.projectIDs) > 0 {
			metadata["project_ids"] = strings.Join(p.projectIDs, ",")
		}
	}
	
	return &pb.InitializeResponse{
		Success:  true,
		Version:  "1.0.0",
		Metadata: metadata,
	}, nil
}

// discoverScope determines the scanning scope based on configuration and permissions
func (p *GCPProvider) discoverScope(ctx context.Context, config map[string]string) error {
	// Check if specific scope is requested
	if scope, ok := config["scope"]; ok {
		p.scope = scope
	}
	
	// Check for organization ID
	if orgID, ok := config["org_id"]; ok && orgID != "" {
		p.orgID = orgID
		p.scope = "organizations"
		log.Printf("Using organization scope: %s", orgID)
		return nil
	}
	
	// Check for folder ID
	if folderID, ok := config["folder_id"]; ok && folderID != "" {
		p.folderID = folderID
		p.scope = "folders"
		log.Printf("Using folder scope: %s", folderID)
		return nil
	}
	
	// Check for explicit project IDs
	if projectIDs, ok := config["project_ids"]; ok && projectIDs != "" {
		p.projectIDs = strings.Split(projectIDs, ",")
		p.scope = "projects"
		log.Printf("Using explicit projects: %v", p.projectIDs)
		return nil
	}
	
	// Try to discover accessible resources
	log.Printf("Discovering accessible GCP resources...")
	
	// First, try to list organizations
	if orgs, err := p.discoverOrganizations(ctx); err == nil && len(orgs) > 0 {
		// Use the first accessible organization
		p.orgID = orgs[0]
		p.scope = "organizations"
		log.Printf("Discovered organization: %s", p.orgID)
		return nil
	}
	
	// Next, try to list accessible projects
	projects, err := p.discoverProjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover projects: %w", err)
	}
	
	if len(projects) == 0 {
		// Try to get project from metadata or environment
		if projectID := p.detectDefaultProject(); projectID != "" {
			projects = []string{projectID}
		} else {
			return fmt.Errorf("no accessible projects found")
		}
	}
	
	p.projectIDs = projects
	p.scope = "projects"
	log.Printf("Discovered %d accessible projects", len(p.projectIDs))
	
	return nil
}

// discoverOrganizations attempts to list accessible organizations
func (p *GCPProvider) discoverOrganizations(ctx context.Context) ([]string, error) {
	client, err := resourcemanager.NewOrganizationsClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	
	var orgs []string
	
	// Search for organizations
	it := client.SearchOrganizations(ctx, &resourcemanagerpb.SearchOrganizationsRequest{})
	
	for {
		org, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Permission denied is expected if user doesn't have org access
			if strings.Contains(err.Error(), "permission") {
				return nil, nil
			}
			return nil, err
		}
		
		// Extract org ID from name (organizations/123456)
		if parts := strings.Split(org.Name, "/"); len(parts) == 2 {
			orgs = append(orgs, parts[1])
		}
	}
	
	return orgs, nil
}

// discoverProjects lists all accessible projects
func (p *GCPProvider) discoverProjects(ctx context.Context) ([]string, error) {
	client, err := resourcemanager.NewProjectsClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	
	var projects []string
	
	// List all projects the user has access to
	it := client.ListProjects(ctx, &resourcemanagerpb.ListProjectsRequest{})
	
	for {
		project, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list projects: %w", err)
		}
		
		// Only include active projects
		if project.State == resourcemanagerpb.Project_ACTIVE {
			projects = append(projects, project.ProjectId)
		}
	}
	
	return projects, nil
}

// detectDefaultProject attempts to detect the default project
func (p *GCPProvider) detectDefaultProject() string {
	// Try from client factory
	if projectIDs := p.clientFactory.GetProjectIDs(); len(projectIDs) > 0 {
		return projectIDs[0]
	}
	
	// Try from metadata service (when running on GCE/GKE)
	if metadata.OnGCE() {
		if projectID, err := metadata.ProjectID(); err == nil {
			return projectID
		}
	}
	
	return ""
}

// GetProviderInfo returns information about the GCP provider
func (p *GCPProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "gcp",
		Version:     "1.0.0", 
		Description: "Google Cloud Platform provider plugin with Cloud Asset Inventory integration",
		Capabilities: map[string]string{
			"discovery":          "true",
			"scanning":           "true",
			"streaming":          "true",
			"multi_project":      "true",
			"asset_inventory":    "true",
			"dynamic_services":   "true",
			"batch_operations":   "true",
			"relationship_graph": "true",
			"change_history":     "true",
			"organization_scope": "true",
			"folder_scope":       "true",
		},
		SupportedServices: []string{
			"compute", "storage", "bigquery", "pubsub", "cloudsql",
			"container", "appengine", "run", "cloudfunctions",
			"dataflow", "dataproc", "composer", "spanner", "firestore",
			"memorystore", "filestore", "vpn", "loadbalancing", "dns",
			"logging", "monitoring", "iam", "resourcemanager",
		},
	}, nil
}

// DiscoverServices discovers available GCP services dynamically
func (p *GCPProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	
	// Check cache unless force refresh is requested
	cacheKey := fmt.Sprintf("discovered_services_%s", p.scope)
	if !req.ForceRefresh {
		if cached, ok := p.cache.GetServiceCache().Get(cacheKey); ok {
			if services, ok := cached.([]*pb.ServiceInfo); ok {
				return &pb.DiscoverServicesResponse{
					Services:     services,
					DiscoveredAt: timestamppb.Now(),
					SdkVersion:   "google-cloud-go",
				}, nil
			}
		}
	}
	
	// Discover services dynamically using enhanced library analysis
	services, err := p.discovery.DiscoverServicesWithLibraryAnalysis(ctx, p.projectIDs)
	if err != nil {
		// Fallback to standard discovery
		services, err = p.discovery.DiscoverServices(ctx, p.projectIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to discover GCP services: %w", err)
		}
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
	p.cache.GetServiceCache().Set(cacheKey, filteredServices)
	
	return &pb.DiscoverServicesResponse{
		Services:     filteredServices,
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   "google-cloud-go",
	}, nil
}

// ListResources lists resources for specified services
func (p *GCPProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	
	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}
	
	var resources []*pb.ResourceRef
	
	// Use Cloud Asset Inventory if available
	if p.assetInventory != nil {
		if req.Service != "" {
			// Map service to asset types
			assetTypes := p.mapServiceToAssetTypes(req.Service)
			assets, err := p.assetInventory.QueryAssetsByType(ctx, assetTypes)
			if err != nil {
				log.Printf("Asset Inventory query failed, falling back to API: %v", err)
				resources, err = p.scanner.ScanService(ctx, req.Service)
				if err != nil {
					return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
				}
			} else {
				resources = assets
			}
		} else {
			// Query all assets
			assets, err := p.assetInventory.QueryAllAssets(ctx)
			if err != nil {
				log.Printf("Asset Inventory query failed, falling back to API: %v", err)
				resources, err = p.scanner.ScanAllServices(ctx)
				if err != nil {
					return nil, fmt.Errorf("failed to scan all services: %w", err)
				}
			} else {
				resources = assets
			}
		}
	} else {
		// Fall back to API scanning
		if req.Service != "" {
			var err error
			resources, err = p.scanner.ScanService(ctx, req.Service)
			if err != nil {
				return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
			}
		} else {
			var err error
			resources, err = p.scanner.ScanAllServices(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to scan all services: %w", err)
			}
		}
	}
	
	return &pb.ListResourcesResponse{
		Resources: resources,
		Metadata: map[string]string{
			"resource_count": fmt.Sprintf("%d", len(resources)),
			"scan_time":      time.Now().Format(time.RFC3339),
			"method":         p.getScanMethod(),
			"scope":          p.scope,
		},
	}, nil
}

// BatchScan performs batch scanning of multiple services
func (p *GCPProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
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
	
	// Use enhanced scanner for comprehensive results if available
	if p.enhancedScanner != nil && (len(req.Services) == 0 || len(req.Services) > 3) {
		log.Printf("🚀 Using enhanced scanner for comprehensive batch scan")
		resources, err := p.enhancedScanner.ScanAllServicesEnhanced(ctx)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Enhanced scan error: %v", err))
		} else {
			allResources = resources
			log.Printf("✅ Enhanced scan completed: %d resources found", len(resources))
		}
	} else if len(req.Services) > 1 {
		// Use concurrent scanning for multiple services
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
				resource := p.convertRefToResource(ref)
				allResources = append(allResources, resource)
			}
		}
	} else {
		// No services specified, use enhanced scanner if available
		if p.enhancedScanner != nil {
			log.Printf("🚀 Using enhanced scanner for full scan")
			resources, err := p.enhancedScanner.ScanAllServicesEnhanced(ctx)
			if err != nil {
				errors = append(errors, fmt.Sprintf("Enhanced scan error: %v", err))
			} else {
				allResources = resources
				log.Printf("✅ Enhanced full scan completed: %d resources found", len(resources))
			}
		} else {
			// Fall back to standard scanner
			serviceRefs, err := p.scanner.ScanAllServices(ctx)
			if err != nil {
				errors = append(errors, fmt.Sprintf("Standard scan error: %v", err))
			} else {
				for _, ref := range serviceRefs {
					resource := p.convertRefToResource(ref)
					allResources = append(allResources, resource)
				}
			}
		}
	}
	
	// Extract relationships if we have multiple resources
	if len(allResources) > 1 {
		// Convert Resources to ResourceRefs for relationship extraction
		refs := make([]*pb.ResourceRef, len(allResources))
		for i, resource := range allResources {
			refs[i] = &pb.ResourceRef{
				Id:              resource.Id,
				Name:            resource.Name,
				Type:            resource.Type,
				Service:         resource.Service,
				Region:          resource.Region,
				BasicAttributes: make(map[string]string),
			}
			// Copy relevant attributes
			if resource.RawData != "" {
				refs[i].BasicAttributes["resource_data"] = resource.RawData
			}
		}
		
		relationships := p.relationships.ExtractRelationships(refs)
		log.Printf("Extracted %d relationships between resources", len(relationships))
		
		// Add relationships to resources
		p.attachRelationshipsToResources(allResources, relationships)
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
func (p *GCPProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
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
func (p *GCPProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	
	return p.schemaGen.GenerateSchemas(req.Services), nil
}

// DescribeResource provides detailed information about a specific resource
func (p *GCPProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
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

// GenerateServiceScanners generates service-specific scanners (not needed for GCP dynamic scanning)
func (p *GCPProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	// GCP uses dynamic scanning, so no need to generate specific scanners
	return &pb.GenerateScannersResponse{
		Scanners:       []*pb.GeneratedScanner{},
		GeneratedCount: 0,
	}, nil
}

// ConfigureDiscovery configures the discovery settings for the provider
func (p *GCPProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Configure discovery settings
	success := true
	var errors []string

	// Update project scope if provided
	if projectIDs, ok := req.Options["project_ids"]; ok && projectIDs != "" {
		p.projectIDs = strings.Split(projectIDs, ",")
		p.scope = "projects"
	}

	// Update organization scope if provided
	if orgID, ok := req.Options["org_id"]; ok && orgID != "" {
		p.orgID = orgID
		p.scope = "organizations"
	}

	// Update folder scope if provided
	if folderID, ok := req.Options["folder_id"]; ok && folderID != "" {
		p.folderID = folderID
		p.scope = "folders"
	}

	// Configure rate limiting if provided
	if rateLimitStr, ok := req.Options["rate_limit"]; ok && rateLimitStr != "" {
		// Parse rate limit and update rate limiter
		// Implementation would parse the rate limit and update p.rateLimiter
	}

	return &pb.ConfigureDiscoveryResponse{
		Success: success,
		Error:   strings.Join(errors, "; "),
		Metadata: map[string]string{
			"scope":         p.scope,
			"project_count": fmt.Sprintf("%d", len(p.projectIDs)),
		},
	}, nil
}

// AnalyzeDiscoveredData analyzes discovered data using the client library analyzer
func (p *GCPProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	startTime := time.Now()
	
	// Get the service mapping from discovery if available
	var analysisData map[string]interface{}
	var analysisErrors []string

	if p.discovery != nil {
		// Use the enhanced discovery with library analysis
		hierarchyAnalyzer := p.discovery.GetHierarchyAnalyzer()
		if hierarchyAnalyzer != nil {
			// Generate relationship analysis
			relationships := hierarchyAnalyzer.GenerateHierarchyRelationships()
			
			analysisData = map[string]interface{}{
				"analysis_type":      "gcp_client_library_analysis",
				"total_relationships": len(relationships),
				"relationships":      relationships,
				"scope":             p.scope,
				"analyzed_projects": p.projectIDs,
			}

			// Add service mappings if available (simplified for now)
			analysisData["hierarchy_available"] = true
		} else {
			analysisErrors = append(analysisErrors, "hierarchy analyzer not available")
		}
	} else {
		analysisErrors = append(analysisErrors, "discovery component not available")
	}

	// Fallback basic analysis
	if analysisData == nil {
		analysisData = map[string]interface{}{
			"analysis_type": "basic_gcp_analysis",
			"scope":        p.scope,
			"provider":     "gcp",
		}
	}

	analysisData["analysis_duration_ms"] = time.Since(startTime).Milliseconds()

	return &pb.AnalysisResponse{
		Success:  len(analysisErrors) == 0,
		Error:    strings.Join(analysisErrors, "; "),
		Warnings: []string{}, // Could add warnings here
		Metadata: map[string]string{
			"analyzer_version": "1.0.0",
			"analysis_time":   time.Now().Format(time.RFC3339),
			"analysis_type":   "gcp_client_library_analysis",
		},
	}, nil
}

// GenerateFromAnalysis generates scanners or other artifacts from analysis data
func (p *GCPProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	var generatedFiles []*pb.GeneratedFile
	var errors []string

	// Generate scanner configurations based on analysis
	if p.discovery != nil && p.discovery.GetHierarchyAnalyzer() != nil {
		// Generate scanner configurations (simplified for now)
		file1 := &pb.GeneratedFile{
			Path:     "scanners/gcp_scanner.json",
			Content:  `{"provider": "gcp", "analysis_type": "client_library"}`,
			Service:  "gcp",
			Metadata: map[string]string{
				"type": "scanner_config",
				"dependencies": "gcp-provider",
			},
		}
		generatedFiles = append(generatedFiles, file1)

		// Generate Asset Inventory query configurations (simplified)
		file2 := &pb.GeneratedFile{
			Path:    "queries/gcp_asset_inventory.json",
			Content: `{"queries": ["compute.googleapis.com/*", "storage.googleapis.com/*"]}`,
			Service: "gcp",
			Metadata: map[string]string{
				"type": "asset_queries",
			},
		}
		generatedFiles = append(generatedFiles, file2)
	} else {
		errors = append(errors, "analysis data not available for generation")
	}

	return &pb.GenerateResponse{
		Success:  len(errors) == 0,
		Error:    strings.Join(errors, "; "),
		Files:    generatedFiles,
		Warnings: []string{},
	}, nil
}

// Helper methods

func (p *GCPProvider) mapServiceToAssetTypes(service string) []string {
	serviceToAssetTypes := map[string][]string{
		"compute": {
			"compute.googleapis.com/Instance",
			"compute.googleapis.com/Disk", 
			"compute.googleapis.com/Network",
			"compute.googleapis.com/Subnetwork",
			"compute.googleapis.com/Firewall",
			"compute.googleapis.com/Address",
			"compute.googleapis.com/Snapshot",
			"compute.googleapis.com/Image",
		},
		"storage": {
			"storage.googleapis.com/Bucket",
		},
		"bigquery": {
			"bigqueryadmin.googleapis.com/Dataset",
			"bigqueryadmin.googleapis.com/Table",
		},
		"pubsub": {
			"pubsub.googleapis.com/Topic",
			"pubsub.googleapis.com/Subscription",
			"pubsub.googleapis.com/Schema",
		},
		"container": {
			"container.googleapis.com/Cluster",
			"container.googleapis.com/NodePool",
		},
		"cloudsql": {
			"sqladmin.googleapis.com/Instance",
		},
		"run": {
			"run.googleapis.com/Service",
			"run.googleapis.com/Revision",
		},
		"cloudfunctions": {
			"cloudfunctions.googleapis.com/Function",
		},
		"appengine": {
			"appengine.googleapis.com/Application",
			"appengine.googleapis.com/Service",
			"appengine.googleapis.com/Version",
		},
	}
	
	if types, ok := serviceToAssetTypes[service]; ok {
		return types
	}
	
	// Default: try to construct asset type
	return []string{fmt.Sprintf("%s.googleapis.com/*", service)}
}

func (p *GCPProvider) getScanMethod() string {
	if p.assetInventory != nil {
		return "cloud_asset_inventory"
	}
	return "gcp_api"
}

func (p *GCPProvider) isServiceExcluded(service string, excludeList []string) bool {
	for _, excluded := range excludeList {
		if service == excluded {
			return true
		}
	}
	return false
}

func (p *GCPProvider) isServiceIncluded(service string, includeList []string) bool {
	for _, included := range includeList {
		if service == included {
			return true
		}
	}
	return false
}

func (p *GCPProvider) convertRefToResource(ref *pb.ResourceRef) *pb.Resource {
	resource := &pb.Resource{
		Provider:     "gcp",
		Service:      ref.Service,
		Type:         ref.Type,
		Id:           ref.Id,
		Name:         ref.Name,
		Region:       ref.Region,
		Tags:         make(map[string]string), // Note: GCP uses labels, but we'll store them as tags for consistency
		DiscoveredAt: timestamppb.Now(),
	}
	
	// Extract labels as tags
	if ref.BasicAttributes != nil {
		for k, v := range ref.BasicAttributes {
			if strings.HasPrefix(k, "label_") {
				labelName := strings.TrimPrefix(k, "label_")
				resource.Tags[labelName] = v
			}
		}
		
		// Store raw resource data if available
		if rawData, ok := ref.BasicAttributes["resource_data"]; ok {
			resource.RawData = rawData
		}
	}
	
	return resource
}

func (p *GCPProvider) batchScanConcurrent(ctx context.Context, services []string) ([]*pb.Resource, []string) {
	var allResources []*pb.Resource
	var errors []string
	
	// Use semaphore for concurrency control
	sem := make(chan struct{}, p.maxConcurrency)
	resultChan := make(chan *serviceResult, len(services))
	
	var wg sync.WaitGroup
	wg.Add(len(services))
	
	for _, service := range services {
		go func(svc string) {
			defer wg.Done()
			
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release
			
			result := &serviceResult{service: svc}
			
			// Apply rate limiting
			if err := p.rateLimiter.Wait(ctx); err != nil {
				result.err = fmt.Errorf("rate limit error: %w", err)
				resultChan <- result
				return
			}
			
			// Scan service
			refs, err := p.scanner.ScanService(ctx, svc)
			if err != nil {
				result.err = err
			} else {
				for _, ref := range refs {
					result.resources = append(result.resources, p.convertRefToResource(ref))
				}
			}
			
			resultChan <- result
		}(service)
	}
	
	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()
	
	// Collect results
	for result := range resultChan {
		if result.err != nil {
			errors = append(errors, fmt.Sprintf("Service %s: %v", result.service, result.err))
		} else {
			allResources = append(allResources, result.resources...)
		}
	}
	
	return allResources, errors
}

func (p *GCPProvider) attachRelationshipsToResources(resources []*pb.Resource, relationships []*pb.Relationship) {
	// Create a map for quick lookup
	resourceMap := make(map[string]*pb.Resource)
	for _, resource := range resources {
		resourceMap[resource.Id] = resource
	}
	
	// Attach relationships to resources
	for _, rel := range relationships {
		// Find source resource (relationships are stored on the source)
		if sourceID, ok := rel.Properties["source_id"]; ok {
			if resource, exists := resourceMap[sourceID]; exists {
				if resource.Relationships == nil {
					resource.Relationships = make([]*pb.Relationship, 0)
				}
				resource.Relationships = append(resource.Relationships, rel)
			}
		}
	}
}

// Note: serviceResult is defined in enhanced_scanners.go

// GetServiceInfo returns information about a specific GCP service
func (p *GCPProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	log.Printf("GetServiceInfo called for service: %s", req.Service)

	// Use discovery to get service information
	services, err := p.discovery.DiscoverServices(ctx, p.projectIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to discover services: %w", err)
	}

	// Find the requested service
	for _, service := range services {
		if service.Name == req.Service {
			// Extract supported resources from asset types
			assetTypes := p.mapServiceToAssetTypes(req.Service)
			supportedResources := make([]string, 0, len(assetTypes))
			for _, assetType := range assetTypes {
				// Extract resource type from asset type (e.g., "compute.googleapis.com/Instance" -> "Instance")
				parts := strings.Split(assetType, "/")
				if len(parts) > 1 {
					supportedResources = append(supportedResources, parts[1])
				}
			}

			return &pb.ServiceInfoResponse{
				ServiceName: service.Name,
				Version:     "v1", // Default version
				SupportedResources: supportedResources,
				RequiredPermissions: service.RequiredPermissions,
				Capabilities: map[string]string{
					"provider":     "gcp",
					"projects":     strings.Join(p.projectIDs, ","),
					"retrieved_at": time.Now().Format(time.RFC3339),
					"scan_method":  p.getScanMethod(),
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("service %s not found", req.Service)
}

// ScanService performs scanning for a specific GCP service
func (p *GCPProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	log.Printf("ScanService called for service: %s", req.Service)

	startTime := time.Now()

	// Map service to asset types
	assetTypes := p.mapServiceToAssetTypes(req.Service)
	if len(assetTypes) == 0 {
		return nil, fmt.Errorf("no asset types found for service: %s", req.Service)
	}

	// Use Asset Inventory or scanner to get resources
	var resources []*pb.Resource

	if p.assetInventory != nil {
		// Use Asset Inventory for efficient scanning
		resourceRefs, scanErr := p.assetInventory.QueryAssetsByType(ctx, assetTypes)
		if scanErr != nil {
			log.Printf("Failed to query assets: %v", scanErr)
		} else {
			// Convert ResourceRefs to Resources
			for _, ref := range resourceRefs {
				resource := &pb.Resource{
					Provider:     "gcp",
					Service:      ref.Service,
					Type:         ref.Type,
					Id:           ref.Id,
					Name:         ref.Name,
					Region:       ref.Region,
					Tags:         ref.BasicAttributes,
					DiscoveredAt: timestamppb.Now(),
				}
				resources = append(resources, resource)
			}
		}
	} else {
		// Fall back to scanner
		resourceRefs, err := p.scanner.ScanService(ctx, req.Service)
		if err != nil {
			return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
		}
		// Convert ResourceRefs to Resources
		for _, ref := range resourceRefs {
			resource := &pb.Resource{
				Provider:     "gcp",
				Service:      ref.Service,
				Type:         ref.Type,
				Id:           ref.Id,
				Name:         ref.Name,
				Region:       ref.Region,
				Tags:         ref.BasicAttributes,
				DiscoveredAt: timestamppb.Now(),
			}
			resources = append(resources, resource)
		}
	}

	// If relationships are requested, enrich resources
	if req.IncludeRelationships && p.relationships != nil {
		// Convert resources to ResourceRefs for relationship extraction
		var resourceRefs []*pb.ResourceRef
		for _, res := range resources {
			ref := &pb.ResourceRef{
				Id:              res.Id,
				Name:            res.Name,
				Type:            res.Type,
				Service:         res.Service,
				Region:          res.Region,
				BasicAttributes: res.Tags,
			}
			resourceRefs = append(resourceRefs, ref)
		}
		
		// Extract all relationships
		allRelationships := p.relationships.ExtractRelationships(resourceRefs)
		
		// Map relationships back to resources
		relationshipMap := make(map[string][]*pb.Relationship)
		for _, rel := range allRelationships {
			if sourceID, ok := rel.Properties["source_id"]; ok {
				relationshipMap[sourceID] = append(relationshipMap[sourceID], rel)
			}
		}
		
		// Assign relationships to resources
		for _, resource := range resources {
			if rels, ok := relationshipMap[resource.Id]; ok {
				resource.Relationships = rels
			}
		}
	}

	return &pb.ScanServiceResponse{
		Service:   req.Service,
		Resources: resources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(resources)),
			DurationMs:     time.Since(startTime).Milliseconds(),
			ResourceCounts: map[string]int32{
				req.Service: int32(len(resources)),
			},
			ServiceCounts: map[string]int32{
				req.Service: 1,
			},
		},
	}, nil
}

// StreamScanService streams resources as they are discovered for a specific GCP service
func (p *GCPProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}

	ctx := stream.Context()
	log.Printf("StreamScanService called for service: %s", req.Service)

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit exceeded: %w", err)
	}

	// Map service to asset types
	assetTypes := p.mapServiceToAssetTypes(req.Service)
	if len(assetTypes) == 0 {
		return fmt.Errorf("no asset types found for service: %s", req.Service)
	}

	// Stream resources as they're discovered
	for _, project := range p.projectIDs {
		if p.assetInventory != nil {
			// Stream using Asset Inventory
			resourceRefs, err := p.assetInventory.QueryAssetsByType(ctx, assetTypes)
			if err != nil {
				log.Printf("Failed to query assets for project %s: %v", project, err)
				continue
			}

			// Convert and stream each resource
			for _, ref := range resourceRefs {
				resource := &pb.Resource{
					Provider:     "gcp",
					Service:      ref.Service,
					Type:         ref.Type,
					Id:           ref.Id,
					Name:         ref.Name,
					Region:       ref.Region,
					Tags:         ref.BasicAttributes,
					DiscoveredAt: timestamppb.Now(),
				}

				// Enrich with relationships if requested
				if req.IncludeRelationships && p.relationships != nil {
					// Need to get relationships for this resource
					allRels := p.relationships.ExtractRelationships([]*pb.ResourceRef{ref})
					resource.Relationships = allRels
				}

				// Send the resource
				if err := stream.Send(resource); err != nil {
					return fmt.Errorf("failed to send resource: %w", err)
				}
			}
		} else {
			// Use scanner for streaming
			resourceRefs, err := p.scanner.ScanService(ctx, req.Service)
			if err != nil {
				return fmt.Errorf("scanner failed: %w", err)
			}
			
			// Stream each resource
			for _, ref := range resourceRefs {
				resource := &pb.Resource{
					Provider:     "gcp",
					Service:      ref.Service,
					Type:         ref.Type,
					Id:           ref.Id,
					Name:         ref.Name,
					Region:       ref.Region,
					Tags:         ref.BasicAttributes,
					DiscoveredAt: timestamppb.Now(),
				}
				
				// Enrich with relationships if requested
				if req.IncludeRelationships && p.relationships != nil {
					allRels := p.relationships.ExtractRelationships([]*pb.ResourceRef{ref})
					resource.Relationships = allRels
				}

				if err := stream.Send(resource); err != nil {
					return fmt.Errorf("failed to send resource: %w", err)
				}
			}
		}
	}

	return nil
}

// Service Account Management Methods (Phase 3) - TODO: implement when protobuf types are added

// SetupServiceAccount provides automated service account setup
// func (p *GCPProvider) SetupServiceAccount(ctx context.Context, req *pb.AutoSetupRequest) (*pb.AutoSetupResponse, error) {
//	if !p.initialized {
//		return nil, fmt.Errorf("provider not initialized")
//	}
//	
//	if p.serviceAccountIntegration == nil {
//		return &pb.AutoSetupResponse{
//			Success: false,
//			Error:   "Service account integration not available",
//		}, nil
//	}
//	
//	return p.serviceAccountIntegration.AutoSetupServiceAccount(ctx, req)
// }

// ValidateServiceAccount validates service account configuration
// func (p *GCPProvider) ValidateServiceAccount(ctx context.Context, req *pb.ValidateSetupRequest) (*pb.ValidateSetupResponse, error) {
//	if !p.initialized {
//		return nil, fmt.Errorf("provider not initialized")
//	}
//	
//	if p.serviceAccountIntegration == nil {
//		return &pb.ValidateSetupResponse{
//			Valid: false,
//			Error: "Service account integration not available",
//		}, nil
//	}
//	
//	return p.serviceAccountIntegration.ValidateServiceAccountSetup(ctx, req)
// }

// GenerateServiceAccountScripts generates deployment and validation scripts
// func (p *GCPProvider) GenerateServiceAccountScripts(ctx context.Context, req *pb.GenerateScriptRequest) (*pb.GenerateScriptResponse, error) {
//	if !p.initialized {
//		return nil, fmt.Errorf("provider not initialized")
//	}
//	
//	if p.serviceAccountIntegration == nil {
//		return &pb.GenerateScriptResponse{
//			Success: false,
//			Error:   "Service account integration not available",
//		}, nil
//	}
//	
//	return p.serviceAccountIntegration.GenerateServiceAccountScript(ctx, req)
// }

// GetServiceAccountRecommendations provides setup recommendations
// func (p *GCPProvider) GetServiceAccountRecommendations(ctx context.Context, req *pb.RecommendationsRequest) (*pb.RecommendationsResponse, error) {
//	if !p.initialized {
//		return nil, fmt.Errorf("provider not initialized")
//	}
//	
//	if p.serviceAccountIntegration == nil {
//		return &pb.RecommendationsResponse{
//			Success: false,
//			Error:   "Service account integration not available",
//		}, nil
//	}
//	
//	return p.serviceAccountIntegration.GetServiceAccountRecommendations(ctx, req)
// }

// GetServiceAccountStatus returns current service account status
// func (p *GCPProvider) GetServiceAccountStatus(ctx context.Context) (*pb.ServiceAccountStatus, error) {
//	if !p.initialized {
//		return nil, fmt.Errorf("provider not initialized")
//	}
//	
//	if p.serviceAccountIntegration == nil {
//		return &pb.ServiceAccountStatus{
//			Configured: false,
//			Error:      "Service account integration not available",
//		}, nil
//	}
//	
//	return p.serviceAccountIntegration.GetServiceAccountStatus(ctx)
// }
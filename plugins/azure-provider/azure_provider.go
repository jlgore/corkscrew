package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ResourceCache provides caching for discovered services
type ResourceCache struct {
	mu       sync.RWMutex
	services []*pb.ServiceInfo
	ttl      time.Duration
	lastSet  time.Time
}

func NewResourceCache(ttl time.Duration) *ResourceCache {
	return &ResourceCache{
		ttl: ttl,
	}
}

func (c *ResourceCache) GetServices() []*pb.ServiceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if time.Since(c.lastSet) > c.ttl {
		return nil
	}
	return c.services
}

func (c *ResourceCache) SetServices(services []*pb.ServiceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.services = services
	c.lastSet = time.Now()
}

// ErrorHandler provides error handling utilities
type ErrorHandler struct {
	mu sync.RWMutex
}

func NewErrorHandler() *ErrorHandler {
	return &ErrorHandler{}
}

// AzureProvider implements the CloudProvider interface for Azure
type AzureProvider struct {
	mu             sync.RWMutex
	initialized    bool
	credential     azcore.TokenCredential
	subscriptionID string
	tenantID       string

	// Core components
	discovery     *AzureServiceDiscovery
	scanner       *AzureResourceScanner
	schemaGen     *AzureSchemaGenerator
	clientFactory *AzureClientFactory
	resourceGraph *ResourceGraphClient

	// Database integration
	database *AzureDatabaseIntegration

	// Performance components
	cache        *ResourceCache
	rateLimiter  *rate.Limiter
	errorHandler *ErrorHandler

	// Configuration
	maxConcurrency  int
	enableStreaming bool
	cacheDir        string
}

// NewAzureProvider creates a new Azure provider instance
func NewAzureProvider() *AzureProvider {
	return &AzureProvider{
		cache:           NewResourceCache(24 * time.Hour),
		rateLimiter:     rate.NewLimiter(rate.Limit(100), 200), // 100 requests/sec, burst 200
		maxConcurrency:  10,
		enableStreaming: true,
		errorHandler:    NewErrorHandler(),
	}
}

// getAzureCliSubscription attempts to get the current Azure CLI subscription
func getAzureCliSubscription() string {
	// First check environment variable
	if sub := os.Getenv("AZURE_SUBSCRIPTION_ID"); sub != "" {
		return sub
	}
	
	// Try to get from Azure CLI
	cmd := exec.Command("az", "account", "show", "--query", "id", "-o", "tsv")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	
	return strings.TrimSpace(string(output))
}

// Initialize sets up the Azure provider with credentials and configuration
func (p *AzureProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Extract Azure-specific configuration
	subscriptionID := req.Config["subscription_id"]
	
	// If subscription_id not provided, try to get it from Azure CLI context
	if subscriptionID == "" {
		// Try to get subscription from Azure CLI
		if cliSub := getAzureCliSubscription(); cliSub != "" {
			subscriptionID = cliSub
			log.Printf("Using subscription from Azure CLI: %s", subscriptionID)
		}
	}
	
	log.Printf("Initializing Azure provider with subscription: %s", subscriptionID)
	
	if subscriptionID == "" {
		return &pb.InitializeResponse{
			Success: false,
			Error:   "subscription_id is required - set AZURE_SUBSCRIPTION_ID or use 'az login'",
		}, nil
	}

	tenantID := req.Config["tenant_id"] // Optional

	// Initialize Azure credentials using DefaultAzureCredential
	// This supports multiple auth methods: MSI, Azure CLI, Environment Variables, etc.
	cred, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{
		TenantID: tenantID,
	})
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create Azure credential: %v", err),
		}, nil
	}

	p.credential = cred
	p.subscriptionID = subscriptionID
	p.tenantID = tenantID
	p.cacheDir = req.CacheDir

	// Initialize components
	p.discovery = NewAzureServiceDiscovery(cred, subscriptionID)
	p.scanner = NewAzureResourceScanner(cred, subscriptionID)
	p.schemaGen = NewAzureSchemaGenerator()
	p.clientFactory = NewAzureClientFactory(cred, subscriptionID)

	// Initialize database integration
	database, err := NewAzureDatabaseIntegration()
	if err != nil {
		log.Printf("Warning: Failed to initialize database integration: %v", err)
		// Continue without database integration
	} else {
		p.database = database
		log.Printf("Azure database integration initialized successfully")
	}

	// Initialize Resource Graph client for efficient querying
	resourceGraph, err := NewResourceGraphClient(cred, []string{subscriptionID})
	if err != nil {
		log.Printf("Warning: Failed to initialize Resource Graph client: %v", err)
		// Continue without Resource Graph - fall back to ARM APIs
	} else {
		p.resourceGraph = resourceGraph
		log.Printf("Resource Graph client initialized successfully")
	}

	p.initialized = true

	return &pb.InitializeResponse{
		Success: true,
		Version: "1.0.0",
		Metadata: map[string]string{
			"subscription_id": subscriptionID,
			"tenant_id":       tenantID,
			"auth_method":     "DefaultAzureCredential",
			"resource_graph":  fmt.Sprintf("%t", p.resourceGraph != nil),
		},
	}, nil
}

// GetProviderInfo returns information about the Azure provider
func (p *AzureProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "azure",
		Version:     "1.0.0",
		Description: "Microsoft Azure cloud provider plugin with ARM integration and Resource Graph support",
		Capabilities: map[string]string{
			"discovery":        "true",
			"scanning":         "true",
			"streaming":        "true",
			"multi_region":     "true",
			"resource_graph":   "true",
			"change_tracking":  "true",
			"batch_operations": "true",
			"arm_integration":  "true",
		},
		SupportedServices: []string{
			"compute", "storage", "network", "keyvault", "sql", "cosmosdb",
			"appservice", "functions", "aks", "containerregistry", "monitor",
		},
	}, nil
}

// DiscoverServices discovers available Azure services (resource providers)
func (p *AzureProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Check cache unless force refresh is requested
	if !req.ForceRefresh {
		if cached := p.cache.GetServices(); cached != nil {
			return &pb.DiscoverServicesResponse{
				Services:     cached,
				DiscoveredAt: timestamppb.Now(),
				SdkVersion:   "azure-sdk-go-v1.0.0",
			}, nil
		}
	}

	// Discover Azure resource providers
	providerInfos, err := p.discovery.DiscoverProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover Azure providers: %w", err)
	}

	// Convert to service list
	services := make([]*pb.ServiceInfo, 0, len(providerInfos))
	for _, providerInfo := range providerInfos {
		// Convert Microsoft.Compute -> compute
		serviceName := p.normalizeServiceName(providerInfo.Namespace)

		// Skip if service is in exclude list
		if p.isServiceExcluded(serviceName, req.ExcludeServices) {
			continue
		}

		// Include only if in include list (if specified)
		if len(req.IncludeServices) > 0 && !p.isServiceIncluded(serviceName, req.IncludeServices) {
			continue
		}

		service := &pb.ServiceInfo{
			Name:        serviceName,
			DisplayName: strings.Title(serviceName),
			PackageName: fmt.Sprintf("github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/%s", serviceName),
			ClientType:  fmt.Sprintf("%sClient", strings.Title(serviceName)),
		}

		// Add resource types
		for _, rt := range providerInfo.ResourceTypes {
			resourceType := &pb.ResourceType{
				Name:              rt.ResourceType,
				TypeName:          fmt.Sprintf("%s/%s", providerInfo.Namespace, rt.ResourceType),
				ListOperation:     "List",
				DescribeOperation: "Get",
				GetOperation:      "Get",
				IdField:           "id",
				NameField:         "name",
				SupportsTags:      true,
				Paginated:         true,
			}
			service.ResourceTypes = append(service.ResourceTypes, resourceType)
		}

		services = append(services, service)
	}

	// Cache the results
	p.cache.SetServices(services)

	return &pb.DiscoverServicesResponse{
		Services:     services,
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   "azure-sdk-go-v1.0.0",
	}, nil
}

// ListResources lists resources for specified services
func (p *AzureProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	var resources []*pb.ResourceRef
	filters := p.parseFilters(req.Filters)

	if req.Service != "" {
		// List resources for a specific service
		serviceResources, err := p.scanner.ScanService(ctx, req.Service, filters)
		if err != nil {
			return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
		}
		resources = serviceResources
	} else {
		// List all resources - use Resource Graph for efficiency if available
		if p.resourceGraph != nil {
			allResources, err := p.resourceGraph.QueryAllResources(ctx)
			if err != nil {
				log.Printf("Resource Graph query failed, falling back to ARM: %v", err)
				// Fall back to ARM scanning
				allResources, err = p.scanner.ScanAllResources(ctx, filters)
				if err != nil {
					return nil, fmt.Errorf("failed to scan all resources: %w", err)
				}
			}
			resources = allResources
		} else {
			// Use ARM scanning
			allResources, err := p.scanner.ScanAllResources(ctx, filters)
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
func (p *AzureProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
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

	// Use Resource Graph for efficient batch scanning if available
	if p.resourceGraph != nil && len(req.Services) > 1 {
		resources, err := p.batchScanWithResourceGraph(ctx, req)
		if err != nil {
			log.Printf("Resource Graph batch scan failed, falling back to ARM: %v", err)
			// Fall back to ARM scanning
			resources, err = p.batchScanWithARM(ctx, req)
			if err != nil {
				errors = append(errors, fmt.Sprintf("Batch scan failed: %v", err))
			} else {
				allResources = resources
			}
		} else {
			allResources = resources
		}
	} else {
		// Use ARM scanning
		resources, err := p.batchScanWithARM(ctx, req)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Batch scan failed: %v", err))
		} else {
			allResources = resources
		}
	}

	// Store resources in database if available
	if p.database != nil && len(allResources) > 0 {
		log.Printf("Storing %d Azure resources in database", len(allResources))
		if err := p.database.StoreResources(allResources); err != nil {
			log.Printf("Warning: Failed to store resources in database: %v", err)
			errors = append(errors, fmt.Sprintf("Database storage failed: %v", err))
		} else {
			log.Printf("Successfully stored %d Azure resources in database", len(allResources))
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
func (p *AzureProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
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
func (p *AzureProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	schemas := make([]*pb.Schema, 0)
	
	// Generate the main DuckDB schemas
	dbSchemas := GenerateAzureDuckDBSchemas()
	
	// Always include core tables
	// Add the main azure_resources table
	schemas = append(schemas, &pb.Schema{
		Name:         "azure_resources",
		Service:      "core",
		ResourceType: "all",
		Sql:          dbSchemas.AzureResourcesTable,
		Description:  "Unified table for all Azure resources",
		Metadata: map[string]string{
			"provider":     "azure",
			"table_type":   "unified",
			"supports_json": "true",
		},
	})
	
	// Add the relationships table
	schemas = append(schemas, &pb.Schema{
		Name:         "azure_relationships",
		Service:      "core",
		ResourceType: "relationships",
		Sql:          dbSchemas.AzureRelationshipsTable,
		Description:  "Resource relationships and dependencies",
		Metadata: map[string]string{
			"provider":     "azure",
			"table_type":   "graph",
			"supports_json": "true",
		},
	})
	
	// Add scan metadata table
	schemas = append(schemas, &pb.Schema{
		Name:         "azure_scan_metadata",
		Service:      "core",
		ResourceType: "metadata",
		Sql:          dbSchemas.ScanMetadataTable,
		Description:  "Scan operation metadata and history",
		Metadata: map[string]string{
			"provider":   "azure",
			"table_type": "metadata",
		},
	})
	
	// Add API action metadata table
	schemas = append(schemas, &pb.Schema{
		Name:         "azure_api_action_metadata",
		Service:      "core",
		ResourceType: "api_metadata",
		Sql:          dbSchemas.APIActionMetadataTable,
		Description:  "Azure API call tracking and performance",
		Metadata: map[string]string{
			"provider":   "azure",
			"table_type": "telemetry",
		},
	})

	// If specific services requested, also add service-specific schemas
	if len(req.Services) > 0 {
		for _, service := range req.Services {
			// Add predefined service-specific tables
			switch service {
			case "storage":
				if sql, exists := dbSchemas.ServiceTables["storage_accounts"]; exists {
					schemas = append(schemas, &pb.Schema{
						Name:         "azure_storage_accounts",
						Service:      "storage",
						ResourceType: "Microsoft.Storage/storageAccounts",
						Sql:          sql,
						Description:  "Azure Storage Accounts with detailed properties",
						Metadata: map[string]string{
							"provider":      "azure",
							"table_type":    "service_specific",
							"supports_json": "true",
						},
					})
				}
			case "compute":
				if sql, exists := dbSchemas.ServiceTables["virtual_machines"]; exists {
					schemas = append(schemas, &pb.Schema{
						Name:         "azure_virtual_machines",
						Service:      "compute",
						ResourceType: "Microsoft.Compute/virtualMachines",
						Sql:          sql,
						Description:  "Azure Virtual Machines with detailed properties",
						Metadata: map[string]string{
							"provider":      "azure",
							"table_type":    "service_specific",
							"supports_json": "true",
						},
					})
				}
			case "network":
				if sql, exists := dbSchemas.ServiceTables["virtual_networks"]; exists {
					schemas = append(schemas, &pb.Schema{
						Name:         "azure_virtual_networks",
						Service:      "network",
						ResourceType: "Microsoft.Network/virtualNetworks",
						Sql:          sql,
						Description:  "Azure Virtual Networks with detailed properties",
						Metadata: map[string]string{
							"provider":      "azure",
							"table_type":    "service_specific",
							"supports_json": "true",
						},
					})
				}
			case "keyvault":
				if sql, exists := dbSchemas.ServiceTables["key_vaults"]; exists {
					schemas = append(schemas, &pb.Schema{
						Name:         "azure_key_vaults",
						Service:      "keyvault",
						ResourceType: "Microsoft.KeyVault/vaults",
						Sql:          sql,
						Description:  "Azure Key Vaults with detailed properties",
						Metadata: map[string]string{
							"provider":      "azure",
							"table_type":    "service_specific",
							"supports_json": "true",
						},
					})
				}
			}
			
			// Also try to get dynamic schemas for the service
			provider := p.discovery.GetProvider(p.denormalizeServiceName(service))
			if provider != nil {
				for _, resourceType := range provider.ResourceTypes {
					schema := p.schemaGen.GenerateSchema(resourceType)
					pbSchema := &pb.Schema{
						Name:         schema.TableName,
						Service:      service,
						ResourceType: resourceType.ResourceType,
						Sql:          p.generateCreateTableSQL(schema),
						Description:  fmt.Sprintf("Dynamic schema for %s resources", resourceType.ResourceType),
						Metadata: map[string]string{
							"provider":     "azure",
							"api_version":  resourceType.APIVersions[0],
							"column_count": fmt.Sprintf("%d", len(schema.Columns)),
							"has_indexes":  fmt.Sprintf("%t", len(schema.Indexes) > 0),
							"table_type":   "dynamic",
						},
					}
					schemas = append(schemas, pbSchema)
				}
			}
		}
	}

	return &pb.SchemaResponse{
		Schemas: schemas,
	}, nil
}

// DescribeResource provides detailed information about a specific resource
func (p *AzureProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
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

	// Add relationships if requested
	if req.IncludeRelationships && p.resourceGraph != nil {
		relationships, err := p.resourceGraph.QueryResourceRelationships(ctx, resourceRef.Id)
		if err != nil {
			log.Printf("Failed to query relationships for %s: %v", resourceRef.Id, err)
		} else {
			for _, rel := range relationships {
				resource.Relationships = append(resource.Relationships, &pb.Relationship{
					TargetId:         rel.TargetID,
					TargetType:       rel.RelationType,
					RelationshipType: rel.Direction,
				})
			}
		}
	}

	return &pb.DescribeResourceResponse{
		Resource: resource,
	}, nil
}

// Helper methods

func (p *AzureProvider) normalizeServiceName(namespace string) string {
	// Convert Microsoft.Compute -> compute
	return strings.ToLower(strings.TrimPrefix(namespace, "Microsoft."))
}

func (p *AzureProvider) denormalizeServiceName(service string) string {
	// Convert compute -> Microsoft.Compute
	return fmt.Sprintf("Microsoft.%s", strings.Title(service))
}

func (p *AzureProvider) parseFilters(filters map[string]string) map[string]string {
	// Azure-specific filter parsing
	result := make(map[string]string)
	for k, v := range filters {
		switch k {
		case "resource_group", "location", "tag":
			result[k] = v
		default:
			result[k] = v
		}
	}
	return result
}

func (p *AzureProvider) isServiceExcluded(service string, excludeList []string) bool {
	for _, excluded := range excludeList {
		if service == excluded {
			return true
		}
	}
	return false
}

func (p *AzureProvider) isServiceIncluded(service string, includeList []string) bool {
	for _, included := range includeList {
		if service == included {
			return true
		}
	}
	return false
}

func (p *AzureProvider) getScanMethod() string {
	if p.resourceGraph != nil {
		return "resource_graph"
	}
	return "arm_api"
}

func (p *AzureProvider) batchScanWithResourceGraph(ctx context.Context, req *pb.BatchScanRequest) ([]*pb.Resource, error) {
	// Use Resource Graph for efficient batch scanning
	filters := make(map[string]interface{})

	if len(req.Services) > 0 {
		// Map service names to actual Azure resource types
		serviceMapping := map[string][]string{
			"storage":         {"microsoft.storage/storageaccounts"},
			"compute":         {"microsoft.compute/virtualmachines", "microsoft.compute/disks", "microsoft.compute/virtualmachinescalesets"},
			"network":         {"microsoft.network/virtualnetworks", "microsoft.network/networkinterfaces", "microsoft.network/publicipaddresses", "microsoft.network/privatednszones", "microsoft.network/networksecuritygroups"},
			"keyvault":        {"microsoft.keyvault/vaults"},
			"sql":             {"microsoft.sql/servers", "microsoft.sql/servers/databases"},
			"cosmosdb":        {"microsoft.documentdb/databaseaccounts"},
			"appservice":      {"microsoft.web/sites", "microsoft.web/serverfarms"},
			"functions":       {"microsoft.web/sites"},
			"aks":             {"microsoft.containerservice/managedclusters"},
			"containerregistry": {"microsoft.containerregistry/registries"},
			"monitor":         {"microsoft.insights/components", "microsoft.operationalinsights/workspaces"},
			"eventhub":        {"microsoft.eventhub/namespaces"},
			"managedidentity": {"microsoft.managedidentity/userassignedidentities"},
		}
		
		var resourceTypes []string
		for _, service := range req.Services {
			if types, ok := serviceMapping[strings.ToLower(service)]; ok {
				resourceTypes = append(resourceTypes, types...)
			} else {
				// If not in mapping, try generic pattern
				resourceTypes = append(resourceTypes, fmt.Sprintf("microsoft.%s/*", strings.ToLower(service)))
			}
		}
		
		if len(resourceTypes) > 0 {
			filters["type"] = resourceTypes
		}
	}

	// Only add region filter if it's not the default AWS region
	if req.Region != "" && req.Region != "us-east-1" {
		filters["location"] = req.Region
	}

	// Add custom filters
	for k, v := range req.Filters {
		filters[k] = v
	}

	log.Printf("Resource Graph query filters: %+v", filters)
	resourceRefs, err := p.resourceGraph.QueryResourcesWithFilter(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("resource graph query failed: %w", err)
	}
	log.Printf("Resource Graph returned %d resources", len(resourceRefs))

	// Convert ResourceRef to Resource
	resources := make([]*pb.Resource, 0, len(resourceRefs))
	for _, ref := range resourceRefs {
		resource := &pb.Resource{
			Provider:     "azure",
			Service:      ref.Service,
			Type:         ref.Type,
			Id:           ref.Id,
			Name:         ref.Name,
			Region:       ref.Region,
			Tags:         make(map[string]string),
			DiscoveredAt: timestamppb.Now(),
		}

		// Extract metadata
		if ref.BasicAttributes != nil {
			if rg, ok := ref.BasicAttributes["resource_group"]; ok {
				resource.ParentId = rg
			}
			if subID, ok := ref.BasicAttributes["subscription_id"]; ok {
				resource.AccountId = subID
			}

			// Extract tags
			for k, v := range ref.BasicAttributes {
				if strings.HasPrefix(k, "tag_") {
					tagName := strings.TrimPrefix(k, "tag_")
					resource.Tags[tagName] = v
				}
			}

			// Store properties as raw data
			if props, ok := ref.BasicAttributes["properties"]; ok {
				resource.RawData = props
			}
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

func (p *AzureProvider) batchScanWithARM(ctx context.Context, req *pb.BatchScanRequest) ([]*pb.Resource, error) {
	// Fall back to ARM API scanning
	var allResources []*pb.Resource
	
	log.Printf("Batch scanning %d services: %v", len(req.Services), req.Services)

	// Clean up filters - remove AWS default region
	cleanFilters := make(map[string]string)
	for k, v := range req.Filters {
		if k == "region" && v == "us-east-1" {
			continue // Skip AWS default region
		}
		cleanFilters[k] = v
	}
	
	for _, service := range req.Services {
		log.Printf("Scanning service: %s", service)
		resources, err := p.scanner.ScanServiceForResources(ctx, service, cleanFilters)
		if err != nil {
			log.Printf("Failed to scan service %s: %v", service, err)
			continue
		}
		log.Printf("Found %d resources for service %s", len(resources), service)
		allResources = append(allResources, resources...)
	}

	log.Printf("Total resources found in batch scan: %d", len(allResources))
	return allResources, nil
}

func (p *AzureProvider) generateCreateTableSQL(schema *TableSchema) string {
	var columns []string
	for _, col := range schema.Columns {
		nullable := ""
		if col.Nullable {
			nullable = " NULL"
		} else {
			nullable = " NOT NULL"
		}

		pk := ""
		if col.PrimaryKey {
			pk = " PRIMARY KEY"
		}

		columns = append(columns, fmt.Sprintf("  %s %s%s%s", col.Name, col.Type, nullable, pk))
	}

	sql := fmt.Sprintf("CREATE TABLE %s (\n%s\n);", schema.TableName, strings.Join(columns, ",\n"))

	// Add indexes
	for _, index := range schema.Indexes {
		sql += fmt.Sprintf("\nCREATE INDEX %s ON %s (%s);", index, schema.TableName, index)
	}

	return sql
}

// ConfigureDiscovery configures the auto-discovery system
func (p *AzureProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Simple implementation for now
	return &pb.ConfigureDiscoveryResponse{
		Success: true,
	}, nil
}

// AnalyzeDiscoveredData analyzes discovered Azure resources and generates insights
func (p *AzureProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Simple implementation for now
	return &pb.AnalysisResponse{
		Success: true,
	}, nil
}

// GenerateFromAnalysis generates scanners or configurations based on analysis results
func (p *AzureProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Simple implementation for now
	return &pb.GenerateResponse{
		Success: true,
	}, nil
}

// ScanService performs scanning for a specific Azure service
func (p *AzureProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	log.Printf("ScanService called for Azure service: %s", req.Service)

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	// For now, convert to BatchScanRequest and use existing logic
	batchReq := &pb.BatchScanRequest{
		Services: []string{req.Service},
		Region:   req.Region,
		Filters:  req.Filters,
		IncludeRelationships: req.IncludeRelationships,
	}

	batchResponse, err := p.BatchScan(ctx, batchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Azure service %s: %w", req.Service, err)
	}

	return &pb.ScanServiceResponse{
		Service:   req.Service,
		Resources: batchResponse.Resources,
		Stats:     batchResponse.Stats,
		Errors:    batchResponse.Errors,
	}, nil
}

// GetServiceInfo returns information about a specific Azure service
func (p *AzureProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	log.Printf("GetServiceInfo called for Azure service: %s", req.Service)

	// Use discovery to get service information
	discoverReq := &pb.DiscoverServicesRequest{
		IncludeServices: []string{req.Service},
	}
	
	services, err := p.DiscoverServices(ctx, discoverReq)
	if err != nil {
		return nil, fmt.Errorf("failed to discover Azure services: %w", err)
	}

	// Find the requested service
	for _, service := range services.Services {
		if service.Name == req.Service {
			var resourceTypes []string
			for _, rt := range service.ResourceTypes {
				resourceTypes = append(resourceTypes, rt.Name)
			}
			
			return &pb.ServiceInfoResponse{
				ServiceName: service.Name,
				Version:     "v1", // Default version
				SupportedResources: resourceTypes,
				RequiredPermissions: service.RequiredPermissions,
				Capabilities: map[string]string{
					"provider":    "azure",
					"retrieved_at": time.Now().Format(time.RFC3339),
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("Azure service %s not found", req.Service)
}

// StreamScanService streams resources as they are discovered for a specific Azure service
func (p *AzureProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}

	ctx := stream.Context()
	log.Printf("StreamScanService called for Azure service: %s", req.Service)

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit exceeded: %w", err)
	}

	// Use ScanService to get resources, then stream them
	scanResponse, err := p.ScanService(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to scan Azure service %s: %w", req.Service, err)
	}

	// Stream each resource
	for _, resource := range scanResponse.Resources {
		if err := stream.Send(resource); err != nil {
			return fmt.Errorf("failed to send resource: %w", err)
		}
	}

	return nil
}

// GenerateServiceScanners generates service-specific scanners for Azure
func (p *AzureProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	// This would generate service-specific scanners similar to AWS
	// For now, return empty response
	return &pb.GenerateScannersResponse{
		Scanners:       []*pb.GeneratedScanner{},
		GeneratedCount: 0,
	}, nil
}

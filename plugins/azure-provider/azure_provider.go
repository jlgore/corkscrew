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

// ManagementGroupDiscoveryResponse represents the response from management group discovery
type ManagementGroupDiscoveryResponse struct {
	Scopes       []*ManagementGroupScope `json:"scopes"`
	TotalScopes  int                     `json:"total_scopes"`
	ScopeType    string                  `json:"scope_type"`
	DiscoveredAt time.Time               `json:"discovered_at"`
}

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

	// Management group and enterprise app support
	managementGroupClient *ManagementGroupClient
	entraIDAppDeployer    *EntraIDAppDeployer
	currentScopes         []*ManagementGroupScope
	scopeType             string // "subscription", "management_group", "tenant"

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

	// Initialize management group client
	managementGroupClient, err := NewManagementGroupClient(cred)
	if err != nil {
		log.Printf("Warning: Failed to initialize management group client: %v", err)
		// Continue without management group support
	} else {
		p.managementGroupClient = managementGroupClient
		log.Printf("Management group client initialized successfully")
	}

	// Initialize Entra ID app deployer
	entraIDAppDeployer, err := NewEntraIDAppDeployer(cred)
	if err != nil {
		log.Printf("Warning: Failed to initialize Entra ID app deployer: %v", err)
		// Continue without enterprise app deployment support
	} else {
		p.entraIDAppDeployer = entraIDAppDeployer
		log.Printf("Entra ID app deployer initialized successfully")
	}

	// Initialize database integration
	database, err := NewAzureDatabaseIntegration()
	if err != nil {
		log.Printf("Warning: Failed to initialize database integration: %v", err)
		// Continue without database integration
	} else {
		p.database = database
		log.Printf("Azure database integration initialized successfully")
	}

	// Discover management group scopes if available
	if p.managementGroupClient != nil {
		scopes, err := p.managementGroupClient.DiscoverManagementGroupHierarchy(ctx)
		if err != nil {
			log.Printf("Warning: Failed to discover management group hierarchy: %v", err)
			// Fall back to subscription-only scope
			p.currentScopes = []*ManagementGroupScope{{
				Type:          "subscription",
				ID:            subscriptionID,
				Name:          subscriptionID,
				Subscriptions: []string{subscriptionID},
			}}
			p.scopeType = "subscription"
		} else {
			p.currentScopes = scopes
			if len(scopes) > 0 && scopes[0].Type == "management_group" {
				p.scopeType = "management_group"
			} else {
				p.scopeType = "subscription"
			}
			log.Printf("Discovered %d management group scopes (type: %s)", len(scopes), p.scopeType)
		}
	} else {
		// No management group support, use subscription scope
		p.currentScopes = []*ManagementGroupScope{{
			Type:          "subscription",
			ID:            subscriptionID,
			Name:          subscriptionID,
			Subscriptions: []string{subscriptionID},
		}}
		p.scopeType = "subscription"
	}

	// Get all subscriptions in scope for Resource Graph
	subscriptions, _ := p.managementGroupClient.GetScopeForResourceGraph(p.currentScopes)
	if len(subscriptions) == 0 {
		subscriptions = []string{subscriptionID}
	}

	// Initialize Resource Graph client for efficient querying
	resourceGraph, err := NewResourceGraphClient(cred, subscriptions)
	if err != nil {
		log.Printf("Warning: Failed to initialize Resource Graph client: %v", err)
		// Continue without Resource Graph - fall back to ARM APIs
	} else {
		p.resourceGraph = resourceGraph
		log.Printf("Resource Graph client initialized successfully with %d subscriptions", len(subscriptions))
	}

	p.initialized = true

	return &pb.InitializeResponse{
		Success: true,
		Version: "1.0.0",
		Metadata: map[string]string{
			"subscription_id":      subscriptionID,
			"tenant_id":            tenantID,
			"auth_method":          "DefaultAzureCredential",
			"resource_graph":       fmt.Sprintf("%t", p.resourceGraph != nil),
			"management_groups":    fmt.Sprintf("%t", p.managementGroupClient != nil),
			"entraid_app_deployer": fmt.Sprintf("%t", p.entraIDAppDeployer != nil),
			"scope_type":           p.scopeType,
			"scopes_count":         fmt.Sprintf("%d", len(p.currentScopes)),
		},
	}, nil
}

// GetProviderInfo returns information about the Azure provider
func (p *AzureProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "azure",
		Version:     "1.0.0",
		Description: "Microsoft Azure cloud provider plugin with ARM integration, Resource Graph support, and Management Group scoping",
		Capabilities: map[string]string{
			"discovery":              "true",
			"scanning":               "true",
			"streaming":              "true",
			"multi_region":           "true",
			"resource_graph":         "true",
			"change_tracking":        "true",
			"batch_operations":       "true",
			"arm_integration":        "true",
			"management_groups":      "true",
			"entraid_app_deployment": "true",
			"tenant_wide_access":     "true",
			"hierarchical_scoping":   "true",
		},
		SupportedServices: []string{
			"compute", "storage", "network", "keyvault", "sql", "cosmosdb",
			"appservice", "functions", "aks", "containerregistry", "monitor",
			"managementgroups", "authorization", "policy", "security",
		},
	}, nil
}

// DiscoverServices discovers available Azure services using Resource Graph
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
				SdkVersion:   "azure-resource-graph-v1.0.0",
			}, nil
		}
	}

	var services []*pb.ServiceInfo
	var err error

	// Try Resource Graph discovery first (superior method)
	if p.resourceGraph != nil {
		log.Printf("Using Resource Graph for dynamic service discovery...")
		services, err = p.resourceGraph.DiscoverAllResourceTypes(ctx)
		if err != nil {
			log.Printf("Resource Graph discovery failed, falling back to provider discovery: %v", err)
			// Fall back to traditional provider discovery
			services, err = p.discoverServicesViaProviders(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("both Resource Graph and provider discovery failed: %w", err)
			}
		} else {
			log.Printf("Resource Graph discovered %d services dynamically", len(services))
		}
	} else {
		// Fall back to traditional provider discovery
		log.Printf("Resource Graph not available, using provider discovery...")
		services, err = p.discoverServicesViaProviders(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("provider discovery failed: %w", err)
		}
	}

	// Apply filters
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
	p.cache.SetServices(filteredServices)

	return &pb.DiscoverServicesResponse{
		Services:     filteredServices,
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   "azure-resource-graph-v1.0.0",
	}, nil
}

// discoverServicesViaProviders uses traditional provider discovery as fallback
func (p *AzureProvider) discoverServicesViaProviders(ctx context.Context, req *pb.DiscoverServicesRequest) ([]*pb.ServiceInfo, error) {
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

	return services, nil
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
			"provider":      "azure",
			"table_type":    "unified",
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
			"provider":      "azure",
			"table_type":    "graph",
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
			"storage":           {"microsoft.storage/storageaccounts"},
			"compute":           {"microsoft.compute/virtualmachines", "microsoft.compute/disks", "microsoft.compute/virtualmachinescalesets"},
			"network":           {"microsoft.network/virtualnetworks", "microsoft.network/networkinterfaces", "microsoft.network/publicipaddresses", "microsoft.network/privatednszones", "microsoft.network/networksecuritygroups"},
			"keyvault":          {"microsoft.keyvault/vaults"},
			"sql":               {"microsoft.sql/servers", "microsoft.sql/servers/databases"},
			"cosmosdb":          {"microsoft.documentdb/databaseaccounts"},
			"appservice":        {"microsoft.web/sites", "microsoft.web/serverfarms"},
			"functions":         {"microsoft.web/sites"},
			"aks":               {"microsoft.containerservice/managedclusters"},
			"containerregistry": {"microsoft.containerregistry/registries"},
			"monitor":           {"microsoft.insights/components", "microsoft.operationalinsights/workspaces"},
			"eventhub":          {"microsoft.eventhub/namespaces"},
			"managedidentity":   {"microsoft.managedidentity/userassignedidentities"},
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
	log.Printf("Resource Graph returned %d resources (VERSION 2)", len(resourceRefs))

	// Debug: Log the first few resources to see what data we have
	for i, ref := range resourceRefs {
		if i < 3 && ref.BasicAttributes != nil {
			keys := make([]string, 0, len(ref.BasicAttributes))
			for k := range ref.BasicAttributes {
				keys = append(keys, k)
			}
			log.Printf("DEBUG Resource %d: Name=%s, Type=%s, BasicAttributes keys=%v", 
				i, ref.Name, ref.Type, keys)
			if rawData, ok := ref.BasicAttributes["raw_data"]; ok {
				log.Printf("  raw_data length: %d", len(rawData))
			}
			if props, ok := ref.BasicAttributes["properties"]; ok {
				log.Printf("  properties length: %d", len(props))
			}
		}
	}

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

			// Store raw data - prefer full raw_data over just properties
			if rawData, ok := ref.BasicAttributes["raw_data"]; ok {
				resource.RawData = rawData
				log.Printf("DEBUG: Using raw_data for %s: %d bytes", ref.Name, len(rawData))
			} else if props, ok := ref.BasicAttributes["properties"]; ok {
				// Fallback to just properties if raw_data not available
				resource.RawData = props
				log.Printf("DEBUG: Using properties for %s: %d bytes", ref.Name, len(props))
			} else {
				log.Printf("DEBUG: No raw_data or properties for %s (type: %s)", ref.Name, ref.Type)
				
				// For resources without metadata, especially storage accounts, fetch full details
				if strings.Contains(strings.ToLower(ref.Type), "storage") || resource.RawData == "" {
					log.Printf("DEBUG: Fetching full details for %s via DescribeResource", ref.Name)
					if fullResource, err := p.scanner.DescribeResource(ctx, ref); err == nil {
						resource.RawData = fullResource.RawData
						log.Printf("DEBUG: Successfully fetched full details for %s, RawData length: %d", ref.Name, len(resource.RawData))
					} else {
						log.Printf("DEBUG: Failed to fetch full details for %s: %v", ref.Name, err)
					}
				}
			}
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

func (p *AzureProvider) batchScanWithARM(ctx context.Context, req *pb.BatchScanRequest) ([]*pb.Resource, error) {
	// Fall back to ARM API scanning
	var allResources []*pb.Resource

	log.Printf("Using ARM API - Batch scanning %d services: %v", len(req.Services), req.Services)

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

// GetServiceInfo returns information about a specific Azure service
func (p *AzureProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	log.Printf("GetServiceInfo called for service: %s", req.Service)

	// Service mapping for Azure
	serviceMapping := map[string][]string{
		"storage":           {"StorageAccount", "BlobContainer", "FileShare", "Queue", "Table"},
		"compute":           {"VirtualMachine", "Disk", "VirtualMachineScaleSet", "AvailabilitySet"},
		"network":           {"VirtualNetwork", "NetworkInterface", "PublicIPAddress", "NetworkSecurityGroup", "LoadBalancer"},
		"keyvault":          {"Vault", "Secret", "Key", "Certificate"},
		"sql":               {"Server", "Database", "ElasticPool", "ManagedInstance"},
		"cosmosdb":          {"DatabaseAccount", "SqlDatabase", "MongoDatabase", "CassandraKeyspace"},
		"appservice":        {"Site", "ServerFarm", "Certificate", "Domain"},
		"functions":         {"Site"},
		"aks":               {"ManagedCluster", "AgentPool"},
		"containerregistry": {"Registry"},
		"monitor":           {"Component", "Workspace", "ActionGroup", "MetricAlert"},
		"eventhub":          {"Namespace", "EventHub", "ConsumerGroup"},
		"managedidentity":   {"UserAssignedIdentity"},
	}

	// Check if service exists in our mapping
	supportedResources, exists := serviceMapping[strings.ToLower(req.Service)]
	if !exists {
		return nil, fmt.Errorf("service %s not found", req.Service)
	}

	// Get provider namespace for the service
	providerNamespace := fmt.Sprintf("Microsoft.%s", strings.Title(strings.ToLower(req.Service)))

	return &pb.ServiceInfoResponse{
		ServiceName:        req.Service,
		Version:            "2021-04-01", // Default Azure API version
		SupportedResources: supportedResources,
		RequiredPermissions: []string{
			"Microsoft.Resources/subscriptions/resourceGroups/read",
			fmt.Sprintf("%s/*/read", providerNamespace),
			fmt.Sprintf("%s/*/list", providerNamespace),
		},
		Capabilities: map[string]string{
			"provider":          "azure",
			"subscription":      p.subscriptionID,
			"scope_type":        p.scopeType,
			"management_groups": fmt.Sprintf("%t", p.managementGroupClient != nil),
			"retrieved_at":      time.Now().Format(time.RFC3339),
			"scan_method":       p.getScanMethod(),
		},
	}, nil
}

// DiscoverManagementGroups discovers the management group hierarchy
func (p *AzureProvider) DiscoverManagementGroups(ctx context.Context) (*ManagementGroupDiscoveryResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	if p.managementGroupClient == nil {
		return nil, fmt.Errorf("management group client not available")
	}

	scopes, err := p.managementGroupClient.DiscoverManagementGroupHierarchy(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover management group hierarchy: %w", err)
	}

	return &ManagementGroupDiscoveryResponse{
		Scopes:       scopes,
		TotalScopes:  len(scopes),
		ScopeType:    p.scopeType,
		DiscoveredAt: time.Now(),
	}, nil
}

// DeployEnterpriseApp deploys a Corkscrew enterprise application
func (p *AzureProvider) DeployEnterpriseApp(ctx context.Context, config *EntraIDAppConfig) (*EntraIDAppResult, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	if p.entraIDAppDeployer == nil {
		return nil, fmt.Errorf("Entra ID app deployer not available")
	}

	// Set default configuration if not provided
	if config.AppName == "" {
		config.AppName = "Corkscrew Cloud Scanner"
	}
	if config.Description == "" {
		config.Description = "Corkscrew enterprise application for cloud resource scanning and discovery"
	}
	if len(config.RequiredPermissions) == 0 {
		config.RequiredPermissions = []string{
			"Directory.Read.All",
			"User.Read",
		}
	}
	if len(config.RoleDefinitions) == 0 {
		config.RoleDefinitions = []string{"Reader", "Security Reader", "Monitoring Reader"}
	}

	// Use the root management group if available
	if config.ManagementGroupScope == "" && len(p.currentScopes) > 0 {
		for _, scope := range p.currentScopes {
			if scope.Type == "management_group" {
				config.ManagementGroupScope = scope.ID
				break
			}
		}
	}

	result, err := p.entraIDAppDeployer.DeployCorkscrewEnterpriseApp(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy enterprise app: %w", err)
	}

	return result, nil
}

// SetManagementGroupScope sets the scope for resource operations
func (p *AzureProvider) SetManagementGroupScope(ctx context.Context, scopeID string, scopeType string) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}

	if p.managementGroupClient == nil {
		return fmt.Errorf("management group client not available")
	}

	// Discover the specific scope
	var newScopes []*ManagementGroupScope

	switch scopeType {
	case "management_group":
		scopes, err := p.managementGroupClient.discoverManagementGroupRecursive(ctx, scopeID, 0)
		if err != nil {
			return fmt.Errorf("failed to discover management group %s: %w", scopeID, err)
		}
		newScopes = scopes

	case "subscription":
		newScopes = []*ManagementGroupScope{{
			Type:          "subscription",
			ID:            scopeID,
			Name:          scopeID,
			Subscriptions: []string{scopeID},
		}}

	default:
		return fmt.Errorf("unsupported scope type: %s", scopeType)
	}

	// Update the provider scope
	p.mu.Lock()
	p.currentScopes = newScopes
	p.scopeType = scopeType
	p.mu.Unlock()

	// Reinitialize Resource Graph with new scope
	subscriptions, _ := p.managementGroupClient.GetScopeForResourceGraph(newScopes)
	if len(subscriptions) > 0 {
		resourceGraph, err := NewResourceGraphClient(p.credential, subscriptions)
		if err != nil {
			log.Printf("Warning: Failed to reinitialize Resource Graph with new scope: %v", err)
		} else {
			p.resourceGraph = resourceGraph
			log.Printf("Resource Graph reinitialized with %d subscriptions in scope", len(subscriptions))
		}
	}

	return nil
}

// ScanService performs scanning for a specific Azure service
func (p *AzureProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	log.Printf("ScanService called for service: %s", req.Service)

	startTime := time.Now()
	var resources []*pb.Resource
	var err error

	// Use Resource Graph or ARM API
	if p.resourceGraph != nil {
		// Use Resource Graph for efficient scanning
		serviceMapping := map[string][]string{
			"storage":           {"microsoft.storage/storageaccounts"},
			"compute":           {"microsoft.compute/virtualmachines", "microsoft.compute/disks", "microsoft.compute/virtualmachinescalesets"},
			"network":           {"microsoft.network/virtualnetworks", "microsoft.network/networkinterfaces", "microsoft.network/publicipaddresses"},
			"keyvault":          {"microsoft.keyvault/vaults"},
			"sql":               {"microsoft.sql/servers", "microsoft.sql/servers/databases"},
			"cosmosdb":          {"microsoft.documentdb/databaseaccounts"},
			"appservice":        {"microsoft.web/sites", "microsoft.web/serverfarms"},
			"functions":         {"microsoft.web/sites"},
			"aks":               {"microsoft.containerservice/managedclusters"},
			"containerregistry": {"microsoft.containerregistry/registries"},
			"monitor":           {"microsoft.insights/components", "microsoft.operationalinsights/workspaces"},
			"eventhub":          {"microsoft.eventhub/namespaces"},
			"managedidentity":   {"microsoft.managedidentity/userassignedidentities"},
		}

		resourceTypes := []string{}
		if types, ok := serviceMapping[strings.ToLower(req.Service)]; ok {
			resourceTypes = types
		} else {
			// Generic pattern
			resourceTypes = []string{fmt.Sprintf("microsoft.%s/*", strings.ToLower(req.Service))}
		}

		filters := map[string]interface{}{
			"type": resourceTypes,
		}

		resourceRefs, err := p.resourceGraph.QueryResourcesWithFilter(ctx, filters)
		if err != nil {
			return nil, fmt.Errorf("resource graph query failed: %w", err)
		}

		// Convert ResourceRef to Resource
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
			}

			// If relationships are requested, enrich the resource
			if req.IncludeRelationships {
				// Extract relationships from resource properties
				// This is simplified - real implementation would parse resource properties
				resource.Relationships = []*pb.Relationship{}
			}

			resources = append(resources, resource)
		}
	} else {
		// Fall back to ARM API scanning
		resources, err = p.scanner.ScanServiceForResources(ctx, req.Service, map[string]string{})
		if err != nil {
			return nil, fmt.Errorf("failed to scan service %s: %w", req.Service, err)
		}
	}

	// Calculate stats
	resourceCounts := make(map[string]int32)
	for _, r := range resources {
		resourceCounts[r.Type]++
	}

	return &pb.ScanServiceResponse{
		Service:   req.Service,
		Resources: resources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(resources)),
			DurationMs:     time.Since(startTime).Milliseconds(),
			ResourceCounts: resourceCounts,
			ServiceCounts: map[string]int32{
				req.Service: 1,
			},
		},
	}, nil
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

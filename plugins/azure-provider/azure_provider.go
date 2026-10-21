package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
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
	mu sync.RWMutex
	// initialized is set last in Initialize. RPC handlers read it without p.mu, so it is atomic;
	// a true Load also publishes the fields Initialize wrote before it.
	initialized    atomic.Bool
	credential     azcore.TokenCredential
	subscriptionID string
	tenantID       string

	// Core components
	discovery     *AzureServiceDiscovery
	scanner       *AzureResourceScanner
	streamer      resourceStreamer // overrides scanner for StreamScan; nil in production
	schemaGen     *AzureSchemaGenerator
	clientFactory *AzureClientFactory
	// resourceGraph is replaced by SetManagementGroupScope while scans may be reading it.
	resourceGraph atomic.Pointer[ResourceGraphClient]

	// Management group and enterprise app support
	managementGroupClient *ManagementGroupClient
	entraIDAppDeployer    *EntraIDAppDeployer
	currentScopes         []*ManagementGroupScope
	scopeType             string // "subscription", "management_group", "tenant"
	// scopeErr is why the current scope may be narrower than the credential can see: a partly
	// failed hierarchy walk. Guarded by mu; reported by scopeScanErrors.
	scopeErr error

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

	tenantID := req.Config["tenant_id"] // Optional

	cred, subscriptionID, tenantID, authMethod, err := resolveAzureCredential(ctx, req.GetConfig(), subscriptionID, tenantID, nil)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create Azure credential: %v", err),
		}, nil
	}

	if subscriptionID == "" {
		return &pb.InitializeResponse{
			Success: false,
			Error:   "subscription_id is required - set AZURE_SUBSCRIPTION_ID or use 'az login'",
		}, nil
	}

	log.Printf("Initializing Azure provider with subscription: %s", subscriptionID)

	p.credential = cred
	p.subscriptionID = subscriptionID
	p.tenantID = tenantID
	p.cacheDir = req.CacheDir

	// Initialize components. A client that cannot be built fails initialization here instead
	// of panicking on first use.
	discovery, err := NewAzureServiceDiscovery(cred, subscriptionID)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("service discovery: %v", err),
		}, nil
	}
	scanner, err := NewAzureResourceScanner(cred, subscriptionID)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("resource scanner: %v", err),
		}, nil
	}
	p.discovery = discovery
	p.scanner = scanner
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

	p.discoverInitialScope(ctx, subscriptionID)

	// Get all subscriptions in scope for Resource Graph
	var subscriptions []string
	if p.managementGroupClient != nil {
		subscriptions, _ = p.managementGroupClient.GetScopeForResourceGraph(p.currentScopes)
	}
	if len(subscriptions) == 0 {
		subscriptions = []string{subscriptionID}
	}

	// Initialize Resource Graph client for efficient querying
	resourceGraph, err := NewResourceGraphClient(cred, subscriptions)
	if err != nil {
		log.Printf("Warning: Failed to initialize Resource Graph client: %v", err)
		// Continue without Resource Graph - fall back to ARM APIs
	} else {
		p.resourceGraph.Store(resourceGraph)
		log.Printf("Resource Graph client initialized successfully with %d subscriptions", len(subscriptions))
	}

	p.initialized.Store(true)

	return &pb.InitializeResponse{
		Success: true,
		Version: "1.0.0",
		Metadata: map[string]string{
			"subscription_id":      subscriptionID,
			"tenant_id":            tenantID,
			"auth_method":          authMethod,
			"resource_graph":       fmt.Sprintf("%t", p.resourceGraph.Load() != nil),
			"management_groups":    fmt.Sprintf("%t", p.managementGroupClient != nil),
			"entraid_app_deployer": fmt.Sprintf("%t", p.entraIDAppDeployer != nil),
			"scope_type":           p.scopeType,
			"scopes_count":         fmt.Sprintf("%d", len(p.currentScopes)),
		},
	}, nil
}

// discoverInitialScope sets the scan scope from the management group hierarchy, falling back to
// the configured subscription. Called from Initialize with p.mu held.
//
// A credential without management group access (403) has the subscription as its whole scope.
// Any other failure, including a walk that failed part way, leaves a scope that may be narrower
// than the credential can see, so it is kept in p.scopeErr and every scan reports it.
func (p *AzureProvider) discoverInitialScope(ctx context.Context, subscriptionID string) {
	subscriptionScope := []*ManagementGroupScope{{
		Type:          "subscription",
		ID:            subscriptionID,
		Name:          subscriptionID,
		Subscriptions: []string{subscriptionID},
	}}
	p.currentScopes = subscriptionScope
	p.scopeType = "subscription"
	p.scopeErr = nil
	if p.managementGroupClient == nil {
		return
	}

	scopes, err := p.managementGroupClient.DiscoverManagementGroupHierarchy(ctx)
	if err != nil && !isForbidden(err) {
		p.scopeErr = fmt.Errorf("management group discovery: %w", err)
		log.Printf("Warning: management group hierarchy incomplete: %v", err)
	}
	if len(scopes) == 0 {
		return
	}
	p.currentScopes = scopes
	if scopes[0].Type == "management_group" {
		p.scopeType = "management_group"
	}
	log.Printf("Discovered %d management group scopes (type: %s)", len(scopes), p.scopeType)
}

// scopeScanErrors returns an error entry when the scan scope may be narrower than it should be
// (see discoverInitialScope), so such a scan is partial and resolves no findings.
func (p *AzureProvider) scopeScanErrors() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.scopeErr == nil {
		return nil
	}
	return []string{shared.ServiceScanError("managementgroups", p.scopeErr)}
}

// GetProviderInfo returns information about the Azure provider
func (p *AzureProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "azure",
		Version:     "1.0.0",
		Description: "Microsoft Azure cloud provider plugin with ARM integration, Resource Graph support, and Management Group scoping",
		Capabilities: shared.WithOptionalCapabilities(map[string]string{
			"discovery":              "true",
			"scanning":               "true",
			"streaming":              "true",
			"multi_region":           "true",
			"resource_graph":         "true",
			"batch_operations":       "true",
			"arm_integration":        "true",
			"management_groups":      "true",
			"entraid_app_deployment": "true",
			"tenant_wide_access":     "true",
			"hierarchical_scoping":   "true",
			"vault_auth":             "true",
		}, map[string]bool{
			shared.OptionalGenerateServiceScanners: true,
			shared.OptionalConfigureDiscovery:      true,
			shared.OptionalAnalyzeDiscoveredData:   true,
			shared.OptionalGenerateFromAnalysis:    true,
		}),
		SupportedServices: []string{
			"compute", "storage", "network", "keyvault", "sql", "cosmosdb",
			"appservice", "functions", "aks", "containerregistry", "monitor",
			"managementgroups", "authorization", "policy", "security",
		},
	}, nil
}

// DiscoverServices discovers available Azure services using Resource Graph
func (p *AzureProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if !p.initialized.Load() {
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
	if p.resourceGraph.Load() != nil {
		log.Printf("Using Resource Graph for dynamic service discovery...")
		services, err = p.resourceGraph.Load().DiscoverAllResourceTypes(ctx)
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
	if !p.initialized.Load() {
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
		if p.resourceGraph.Load() != nil {
			allResources, err := p.resourceGraph.Load().QueryAllResources(ctx)
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
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}

	startTime := time.Now()
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
		ServiceCounts:  make(map[string]int32),
	}

	allResources, errors := p.scanBatch(ctx, req)
	errors = append(errors, p.scopeScanErrors()...)

	// Calculate statistics
	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(startTime).Milliseconds()

	for _, resource := range allResources {
		stats.ResourceCounts[resource.Type]++
		stats.ServiceCounts[resource.Service]++
	}
	enrichCorrelationEvidence(allResources)

	return &pb.BatchScanResponse{
		Resources: allResources,
		Stats:     stats,
		Errors:    errors,
	}, nil
}

// scanBatch runs the scan BatchScan asked for. It returns every resource observed and one error
// entry per failure that may have dropped resources, so the scan is partial rather than
// silently smaller (ADR 0023).
func (p *AzureProvider) scanBatch(
	ctx context.Context, req *pb.BatchScanRequest,
) ([]*pb.Resource, []string) {
	rg := p.resourceGraph.Load()
	if rg == nil || len(req.Services) <= 1 {
		return p.batchScanWithARM(ctx, req)
	}
	resources, scanErrors, err := p.batchScanWithResourceGraph(ctx, rg, req)
	if err == nil {
		return resources, scanErrors
	}

	log.Printf("Resource Graph batch scan failed, falling back to ARM: %v", err)
	armResources, armErrors := p.batchScanWithARM(ctx, req)
	scanErrors = append(scanErrors, armErrors...)
	// ARM lists only the provider's own subscription. When Resource Graph covered more, the
	// fallback cannot observe the whole scope, so the Resource Graph failure stays reported.
	if !rg.coversOnly(p.subscriptionID) {
		scanErrors = append(scanErrors, shared.ServiceScanError("resourcegraph", err))
	}
	return mergeResources(armResources, resources), scanErrors
}

// mergeResources returns primary plus every resource in extra whose ID primary lacks.
func mergeResources(primary, extra []*pb.Resource) []*pb.Resource {
	seen := make(map[string]bool, len(primary))
	for _, resource := range primary {
		seen[resource.Id] = true
	}
	for _, resource := range extra {
		if !seen[resource.Id] {
			primary = append(primary, resource)
		}
	}
	return primary
}

// StreamScan streams resources as they are discovered
func (p *AzureProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized.Load() {
		return fmt.Errorf("provider not initialized")
	}

	send := func(resource *pb.Resource) error {
		if err := stream.Send(resource); err != nil {
			return fmt.Errorf("failed to send resource: %w", err)
		}
		return nil
	}
	if err := streamResources(stream.Context(), p.streamSource(), req.Services, send); err != nil {
		return fmt.Errorf("stream scan: %w", err)
	}
	return nil
}

// streamSource returns the producer for StreamScan. Tests inject a fake via p.streamer.
func (p *AzureProvider) streamSource() resourceStreamer {
	if p.streamer != nil {
		return p.streamer
	}
	return p.scanner
}

// GetSchemas returns database schemas for resources
func (p *AzureProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized.Load() {
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
	if !p.initialized.Load() {
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
	if req.IncludeRelationships && p.resourceGraph.Load() != nil {
		relationships, err := p.resourceGraph.Load().QueryResourceRelationships(ctx, resourceRef.Id)
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
	if p.resourceGraph.Load() != nil {
		return "resource_graph"
	}
	return "arm_api"
}

// batchScanWithResourceGraph queries Resource Graph for the requested services. err means the
// query failed and resources holds only what arrived first; scanErrors reports resources whose
// full details could not be fetched.
func (p *AzureProvider) batchScanWithResourceGraph(
	ctx context.Context, rg *ResourceGraphClient, req *pb.BatchScanRequest,
) (resources []*pb.Resource, scanErrors []string, err error) {
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
	resourceRefs, queryErr := rg.QueryResourcesWithFilter(ctx, filters)
	log.Printf("Resource Graph returned %d resources", len(resourceRefs))

	describeErrs := newDescribeErrors()
	resources, errs := describeEach(ctx, resourceRefs, p.resourceFromGraphRef)
	for index, ref := range resourceRefs {
		if errs[index] != nil {
			describeErrs.add(ref.Service, errs[index])
		}
	}
	if queryErr != nil {
		queryErr = fmt.Errorf("resource graph query failed: %w", queryErr)
	}
	return resources, describeErrs.entries(), queryErr
}

// resourceFromGraphRef converts a Resource Graph row to a Resource. A row without properties is
// described through ARM; if that fails the resource is still returned, from the row alone,
// with the error, because its configuration was not observed.
func (p *AzureProvider) resourceFromGraphRef(
	ctx context.Context, ref *pb.ResourceRef,
) (*pb.Resource, error) {
	resource := basicResourceFromRef(ref)
	if resource.RawData != "" || ref.BasicAttributes == nil {
		return resource, nil
	}
	fullResource, err := p.scanner.DescribeResource(ctx, ref)
	if err != nil {
		return resource, err
	}
	resource.RawData = fullResource.RawData
	return resource, nil
}

// describeErrors summarizes describe failures per service: one entry with a count and the first
// error, rather than one entry per resource.
type describeErrors struct {
	count map[string]int
	first map[string]error
	order []string
}

func newDescribeErrors() *describeErrors {
	return &describeErrors{count: make(map[string]int), first: make(map[string]error)}
}

func (d *describeErrors) add(service string, err error) {
	if d.count[service] == 0 {
		d.first[service] = err
		d.order = append(d.order, service)
	}
	d.count[service]++
}

func (d *describeErrors) entries() []string {
	var entries []string
	for _, service := range d.order {
		err := fmt.Errorf("describe failed for %d resources, first: %w",
			d.count[service], d.first[service])
		entries = append(entries, shared.ServiceScanError(service, err))
	}
	return entries
}

// batchScanWithARM scans each requested service through ARM. A failed service keeps the
// resources it returned and adds an error entry.
func (p *AzureProvider) batchScanWithARM(
	ctx context.Context, req *pb.BatchScanRequest,
) ([]*pb.Resource, []string) {
	var allResources []*pb.Resource
	var scanErrors []string

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
			scanErrors = append(scanErrors, shared.ServiceScanError(service, err))
		}
		log.Printf("Found %d resources for service %s", len(resources), service)
		allResources = append(allResources, resources...)
	}

	log.Printf("Total resources found in batch scan: %d", len(allResources))
	return allResources, scanErrors
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
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Simple implementation for now
	return &pb.ConfigureDiscoveryResponse{
		Success: true,
	}, nil
}

// AnalyzeDiscoveredData analyzes discovered Azure resources and generates insights
func (p *AzureProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Simple implementation for now
	return &pb.AnalysisResponse{
		Success: true,
	}, nil
}

// GenerateFromAnalysis generates scanners or configurations based on analysis results
func (p *AzureProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Simple implementation for now
	return &pb.GenerateResponse{
		Success: true,
	}, nil
}

// GetServiceInfo returns information about a specific Azure service
func (p *AzureProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized.Load() {
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
	if !p.initialized.Load() {
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
	if !p.initialized.Load() {
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
	if !p.initialized.Load() {
		return fmt.Errorf("provider not initialized")
	}

	if p.managementGroupClient == nil {
		return fmt.Errorf("management group client not available")
	}

	// Discover the specific scope
	var newScopes []*ManagementGroupScope

	switch scopeType {
	case "management_group":
		visited := make(map[string]bool)
		scopes, err := p.managementGroupClient.discoverManagementGroupRecursive(
			ctx, scopeID, 0, visited)
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

	// Update the provider scope. An explicitly chosen scope replaces any narrowing that
	// automatic discovery reported.
	p.mu.Lock()
	p.currentScopes = newScopes
	p.scopeType = scopeType
	p.scopeErr = nil
	p.mu.Unlock()

	// Reinitialize Resource Graph with new scope
	subscriptions, _ := p.managementGroupClient.GetScopeForResourceGraph(newScopes)
	if len(subscriptions) > 0 {
		resourceGraph, err := NewResourceGraphClient(p.credential, subscriptions)
		if err != nil {
			log.Printf("Warning: Failed to reinitialize Resource Graph with new scope: %v", err)
		} else {
			p.resourceGraph.Store(resourceGraph)
			log.Printf("Resource Graph reinitialized with %d subscriptions in scope", len(subscriptions))
		}
	}

	return nil
}

// ScanService performs scanning for a specific Azure service
func (p *AzureProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Apply rate limiting
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	log.Printf("ScanService called for service: %s", req.Service)

	startTime := time.Now()
	resources, scanErr := p.scanServiceResources(ctx, req)
	scanErrors := p.scopeScanErrors()
	if scanErr != nil {
		scanErrors = append(scanErrors, shared.ServiceScanError(req.Service, scanErr))
	}

	// Calculate stats
	resourceCounts := make(map[string]int32)
	for _, r := range resources {
		resourceCounts[r.Type]++
	}

	return &pb.ScanServiceResponse{
		Service:   req.Service,
		Resources: resources,
		Errors:    scanErrors,
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

// scanServiceResources lists one service through Resource Graph, or ARM without it. It returns
// the resources observed even when it also returns an error.
func (p *AzureProvider) scanServiceResources(
	ctx context.Context, req *pb.ScanServiceRequest,
) ([]*pb.Resource, error) {
	rg := p.resourceGraph.Load()
	if rg == nil {
		return p.scanner.ScanServiceForResources(ctx, req.Service, map[string]string{})
	}

	resourceTypes := []string{fmt.Sprintf("microsoft.%s/*", strings.ToLower(req.Service))}
	if types, ok := scanServiceResourceTypes[strings.ToLower(req.Service)]; ok {
		resourceTypes = types
	}
	filters := map[string]interface{}{"type": resourceTypes}
	resourceRefs, err := rg.QueryResourcesWithFilter(ctx, filters)
	if err != nil {
		err = fmt.Errorf("resource graph query failed: %w", err)
	}

	resources := make([]*pb.Resource, 0, len(resourceRefs))
	for _, ref := range resourceRefs {
		resource := basicResourceFromRef(ref)
		if req.IncludeRelationships {
			resource.Relationships = []*pb.Relationship{}
		}
		resources = append(resources, resource)
	}
	return resources, err
}

// scanServiceResourceTypes maps service names to the Resource Graph types ScanService queries.
var scanServiceResourceTypes = map[string][]string{
	"storage": {"microsoft.storage/storageaccounts"},
	"compute": {
		"microsoft.compute/virtualmachines", "microsoft.compute/disks",
		"microsoft.compute/virtualmachinescalesets",
	},
	"network": {
		"microsoft.network/virtualnetworks", "microsoft.network/networkinterfaces",
		"microsoft.network/publicipaddresses",
	},
	"keyvault":          {"microsoft.keyvault/vaults"},
	"sql":               {"microsoft.sql/servers", "microsoft.sql/servers/databases"},
	"cosmosdb":          {"microsoft.documentdb/databaseaccounts"},
	"appservice":        {"microsoft.web/sites", "microsoft.web/serverfarms"},
	"functions":         {"microsoft.web/sites"},
	"aks":               {"microsoft.containerservice/managedclusters"},
	"containerregistry": {"microsoft.containerregistry/registries"},
	"monitor": {
		"microsoft.insights/components", "microsoft.operationalinsights/workspaces",
	},
	"eventhub":        {"microsoft.eventhub/namespaces"},
	"managedidentity": {"microsoft.managedidentity/userassignedidentities"},
}

// StreamScanService streams resources as they are discovered for a specific Azure service
func (p *AzureProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized.Load() {
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

	// Stream each resource, then report any failure so the stream does not end as complete.
	for _, resource := range scanResponse.Resources {
		if err := stream.Send(resource); err != nil {
			return fmt.Errorf("failed to send resource: %w", err)
		}
	}
	if len(scanResponse.Errors) > 0 {
		return fmt.Errorf("scan Azure service %s: %s",
			req.Service, strings.Join(scanResponse.Errors, "; "))
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// KubernetesProvider implements the CloudProvider interface for Kubernetes
type KubernetesProvider struct {
	mu          sync.RWMutex
	initialized bool

	// Kubernetes configuration
	kubeConfig    *rest.Config
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface

	// Core components (following cloud provider patterns)
	discovery     *APIDiscovery
	scanner       *ResourceScanner
	schemaGen     *K8sSchemaGenerator
	clientFactory *ClientFactory
	relationships *RelationshipMapper

	// Multi-cluster support
	clusters      map[string]*ClusterConnection
	activeCluster string

	// Performance components (matching cloud providers)
	rateLimiter    *rate.Limiter
	maxConcurrency int
	cache          *MultiLevelCache
	informerCache  *InformerCache

	// Configuration
	config *ProviderConfig
}

// ProviderConfig holds configuration for the Kubernetes provider
type ProviderConfig struct {
	KubeConfigPath  string            `json:"kubeconfig_path"`
	Contexts        []string          `json:"contexts"`        // Multiple cluster contexts
	AllNamespaces   bool              `json:"all_namespaces"`
	LabelSelectors  map[string]string `json:"label_selectors"`
	ResourceTypes   []string          `json:"resource_types"`
	ExcludeSystemNS bool              `json:"exclude_system_namespaces"`
	WatchMode       bool              `json:"watch_mode"`
}

// ClusterConnection represents a connection to a Kubernetes cluster
type ClusterConnection struct {
	Name          string
	Context       string
	Config        *rest.Config
	Clientset     kubernetes.Interface
	DynamicClient dynamic.Interface
	Healthy       bool
	LastCheck     time.Time
}

// NewKubernetesProvider creates a new Kubernetes provider instance
func NewKubernetesProvider() *KubernetesProvider {
	return &KubernetesProvider{
		rateLimiter:    rate.NewLimiter(rate.Limit(100), 200), // K8s API can handle high rates
		maxConcurrency: 20,                                     // Higher concurrency than cloud providers
		cache:          NewMultiLevelCache(24 * time.Hour),
		clusters:       make(map[string]*ClusterConnection),
	}
}

// Initialize sets up the Kubernetes provider with configuration
func (p *KubernetesProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Parse configuration
	config := &ProviderConfig{
		AllNamespaces:   true,
		ExcludeSystemNS: true,
	}

	// Get kubeconfig path
	if kubeconfigPath := req.Config["kubeconfig_path"]; kubeconfigPath != "" {
		config.KubeConfigPath = kubeconfigPath
	} else if home := homedir.HomeDir(); home != "" {
		config.KubeConfigPath = filepath.Join(home, ".kube", "config")
	}

	// Get contexts to use (for multi-cluster)
	if contexts := req.Config["contexts"]; contexts != "" {
		config.Contexts = strings.Split(contexts, ",")
	}

	// Parse other configurations
	if allNs := req.Config["all_namespaces"]; allNs == "false" {
		config.AllNamespaces = false
	}

	p.config = config

	// Initialize primary cluster connection
	if err := p.initializePrimaryCluster(ctx); err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to initialize Kubernetes client: %v", err),
		}, nil
	}

	// Initialize additional clusters if specified
	if len(config.Contexts) > 0 {
		p.initializeMultipleClusters(ctx, config.Contexts)
	}

	// Initialize components
	p.clientFactory = NewClientFactory(p.dynamicClient, p.clientset)
	p.discovery = NewAPIDiscovery(p.clientset.Discovery(), p.dynamicClient, p.kubeConfig)
	p.scanner = NewResourceScanner(p.clientFactory, p.rateLimiter)
	p.schemaGen = NewK8sSchemaGenerator()
	p.relationships = NewRelationshipMapper()
	p.informerCache = NewInformerCache(p.clientFactory)

	p.initialized = true

	return &pb.InitializeResponse{
		Success: true,
		Version: "1.0.0",
		Metadata: map[string]string{
			"cluster_count":    fmt.Sprintf("%d", len(p.clusters)),
			"primary_cluster":  p.activeCluster,
			"all_namespaces":   fmt.Sprintf("%v", config.AllNamespaces),
			"exclude_system":   fmt.Sprintf("%v", config.ExcludeSystemNS),
			"watch_mode":       fmt.Sprintf("%v", config.WatchMode),
		},
	}, nil
}

// initializePrimaryCluster sets up the connection to the primary Kubernetes cluster
func (p *KubernetesProvider) initializePrimaryCluster(ctx context.Context) error {
	// Build config from kubeconfig file
	config, err := clientcmd.BuildConfigFromFlags("", p.config.KubeConfigPath)
	if err != nil {
		// Try in-cluster config as fallback
		config, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to create Kubernetes config: %w", err)
		}
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	// Create dynamic client for CRDs
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Test connection
	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to connect to Kubernetes API: %w", err)
	}

	p.kubeConfig = config
	p.clientset = clientset
	p.dynamicClient = dynamicClient

	// Add to clusters map
	p.clusters["default"] = &ClusterConnection{
		Name:          "default",
		Context:       "current-context",
		Config:        config,
		Clientset:     clientset,
		DynamicClient: dynamicClient,
		Healthy:       true,
		LastCheck:     time.Now(),
	}
	p.activeCluster = "default"

	return nil
}

// initializeMultipleClusters sets up connections to multiple Kubernetes clusters
func (p *KubernetesProvider) initializeMultipleClusters(ctx context.Context, contexts []string) {
	for _, context := range contexts {
		go func(ctxName string) {
			if err := p.addCluster(ctx, ctxName); err != nil {
				// Log error but don't fail initialization
				fmt.Printf("Failed to add cluster %s: %v\n", ctxName, err)
			}
		}(context)
	}
}

// addCluster adds a new cluster connection
func (p *KubernetesProvider) addCluster(ctx context.Context, contextName string) error {
	// Load config with specific context
	configOverrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: p.config.KubeConfigPath},
		configOverrides,
	).ClientConfig()
	if err != nil {
		return err
	}

	// Create clients
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	// Test connection
	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.clusters[contextName] = &ClusterConnection{
		Name:          contextName,
		Context:       contextName,
		Config:        config,
		Clientset:     clientset,
		DynamicClient: dynamicClient,
		Healthy:       true,
		LastCheck:     time.Now(),
	}
	p.mu.Unlock()

	return nil
}

// GetProviderInfo returns provider metadata
func (p *KubernetesProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "kubernetes",
		Version:     "1.0.0",
		Description: "Kubernetes provider with auto-discovery for all resources including CRDs",
		Capabilities: map[string]string{
			"discovery":     "true",
			"scanning":      "true",
			"streaming":     "true",
			"schemas":       "true",
			"relationships": "true",
			"multi_cluster": "true",
			"watch_mode":    "true",
			"crd_support":   "true",
			"helm_support":  "true",
		},
	}, nil
}

// DiscoverServices discovers all available Kubernetes resources
func (p *KubernetesProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Check cache first
	if !req.ForceRefresh {
		if cached, found := p.cache.Get("discovered_services"); found {
			return cached.(*pb.DiscoverServicesResponse), nil
		}
	}

	// Discover all resources including CRDs
	resources, err := p.discovery.DiscoverAllResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover resources: %w", err)
	}

	// Convert to service info format (group resources by API group)
	serviceMap := make(map[string]*pb.ServiceInfo)
	for _, resource := range resources {
		groupName := resource.Group
		if groupName == "" {
			groupName = "core"
		}

		service, exists := serviceMap[groupName]
		if !exists {
			service = &pb.ServiceInfo{
				Name:        groupName,
				DisplayName: fmt.Sprintf("Kubernetes %s API", strings.Title(groupName)),
				PackageName: fmt.Sprintf("k8s.io/api/%s", groupName),
				ClientType:  "kubernetes",
			}
			serviceMap[groupName] = service
		}

		// Add resource type to service
		resourceType := &pb.ResourceType{
			Name:         resource.Kind,
			TypeName:     resource.Kind,
			SupportsTags: true,
			IdField:      "uid",
			NameField:    "name",
		}
		service.ResourceTypes = append(service.ResourceTypes, resourceType)
	}

	// Convert map to slice
	var services []*pb.ServiceInfo
	for _, service := range serviceMap {
		services = append(services, service)
	}

	response := &pb.DiscoverServicesResponse{
		Services:     services,
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   "kubernetes-go-client",
	}

	// Cache the response
	p.cache.Set("discovered_services", response, 1*time.Hour)

	return response, nil
}

// ListResources lists all resources of specified types
func (p *KubernetesProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	var allResources []*pb.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Determine which clusters to scan
	clusters := p.getClustersToScan(req)

	// Use semaphore for concurrency control
	semaphore := make(chan struct{}, p.maxConcurrency)

	for _, cluster := range clusters {
		wg.Add(1)
		go func(c *ClusterConnection) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			resources, err := p.scanClusterResources(ctx, c, req)
			if err != nil {
				// Log error but continue with other clusters
				fmt.Printf("Failed to scan cluster %s: %v\n", c.Name, err)
				return
			}

			mu.Lock()
			allResources = append(allResources, resources...)
			mu.Unlock()
		}(cluster)
	}

	wg.Wait()

	// Convert Resources to ResourceRefs
	var resourceRefs []*pb.ResourceRef
	for _, resource := range allResources {
		resourceRef := &pb.ResourceRef{
			Id:      resource.Id,
			Name:    resource.Name,
			Type:    resource.Type,
			Service: resource.Service,
			Region:  resource.Region,
			BasicAttributes: resource.Tags,
		}
		resourceRefs = append(resourceRefs, resourceRef)
	}

	return &pb.ListResourcesResponse{
		Resources:  resourceRefs,
		NextToken:  "", // Kubernetes doesn't use pagination tokens like cloud providers
		TotalCount: int32(len(allResources)),
		Metadata: map[string]string{
			"cluster_count":   fmt.Sprintf("%d", len(clusters)),
			"resource_count":  fmt.Sprintf("%d", len(allResources)),
			"scan_timestamp":  time.Now().Format(time.RFC3339),
		},
	}, nil
}

// getClustersToScan determines which clusters to scan based on the request
func (p *KubernetesProvider) getClustersToScan(req *pb.ListResourcesRequest) []*ClusterConnection {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// If specific clusters requested
	if clusterList := req.Filters["clusters"]; clusterList != "" {
		clusters := strings.Split(clusterList, ",")
		var result []*ClusterConnection
		for _, clusterName := range clusters {
			if cluster, exists := p.clusters[clusterName]; exists && cluster.Healthy {
				result = append(result, cluster)
			}
		}
		return result
	}

	// Otherwise, return all healthy clusters
	var result []*ClusterConnection
	for _, cluster := range p.clusters {
		if cluster.Healthy {
			result = append(result, cluster)
		}
	}
	return result
}

// scanClusterResources scans resources in a specific cluster
func (p *KubernetesProvider) scanClusterResources(ctx context.Context, cluster *ClusterConnection, req *pb.ListResourcesRequest) ([]*pb.Resource, error) {
	// Create scanner with cluster-specific clients
	scanner := NewResourceScannerForCluster(cluster)

	// Determine which resources to scan
	var resourcesToScan []string
	if req.Service != "" {
		// Service name in K8s context is the API group
		resourcesToScan = p.getResourcesForAPIGroup(req.Service)
	} else if resourceTypes := req.Filters["resource_types"]; resourceTypes != "" {
		resourcesToScan = strings.Split(resourceTypes, ",")
	} else {
		// Scan all resources
		resourcesToScan = []string{"all"}
	}

	// Determine namespaces
	namespaces := p.getNamespacesToScan(ctx, cluster, req)

	// Scan resources
	return scanner.ScanResources(ctx, resourcesToScan, namespaces)
}

// getNamespacesToScan determines which namespaces to scan
func (p *KubernetesProvider) getNamespacesToScan(ctx context.Context, cluster *ClusterConnection, req *pb.ListResourcesRequest) []string {
	if namespace := req.Filters["namespace"]; namespace != "" {
		return []string{namespace}
	}

	if !p.config.AllNamespaces {
		return []string{"default"}
	}

	// List all namespaces
	namespaceList, err := cluster.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return []string{"default"}
	}

	var namespaces []string
	for _, ns := range namespaceList.Items {
		// Optionally exclude system namespaces
		if p.config.ExcludeSystemNS && isSystemNamespace(ns.Name) {
			continue
		}
		namespaces = append(namespaces, ns.Name)
	}

	return namespaces
}

// DescribeResource gets detailed information about a specific resource
func (p *KubernetesProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Parse resource ID (format: cluster/namespace/kind/name)
	parts := strings.Split(req.ResourceRef.Id, "/")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid resource ID format")
	}

	clusterName, namespace, kind, name := parts[0], parts[1], parts[2], parts[3]

	// Get cluster connection
	cluster, exists := p.clusters[clusterName]
	if !exists {
		return nil, fmt.Errorf("cluster %s not found", clusterName)
	}

	// Get detailed resource information
	resource, err := p.scanner.GetResourceDetails(ctx, cluster, namespace, kind, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource details: %w", err)
	}

	// Note: Relationships would be extracted separately in a real implementation
	// For now, just return the resource
	
	return &pb.DescribeResourceResponse{
		Resource: resource,
	}, nil
}

// GenerateServiceScanners generates scanner code for discovered resources
func (p *KubernetesProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	// This is where auto-generation happens
	generator := NewScannerGenerator()
	
	var generatedScanners []*pb.GeneratedScanner
	
	for _, service := range req.Services {
		// Generate scanner for each resource type in the service
		_, err := generator.GenerateServiceScanner(service)
		if err != nil {
			continue
		}
		
		scanner := &pb.GeneratedScanner{
			Service:       service,
			FilePath:      fmt.Sprintf("%s_scanner.go", strings.ToLower(service)),
			ResourceTypes: []string{}, // Would populate with actual resource types
			GeneratedAt:   timestamppb.Now(),
		}
		generatedScanners = append(generatedScanners, scanner)
	}
	
	return &pb.GenerateScannersResponse{
		Scanners:       generatedScanners,
		GeneratedCount: int32(len(generatedScanners)),
	}, nil
}

// GetSchemas returns database schemas for Kubernetes resources
func (p *KubernetesProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Discover all resources
	resources, err := p.discovery.DiscoverAllResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover resources: %w", err)
	}

	// Generate schemas
	schemas := p.schemaGen.GenerateSchemas(resources)

	return &pb.SchemaResponse{
		Schemas: schemas,
	}, nil
}

// BatchScan performs concurrent scanning of multiple services
func (p *KubernetesProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	// Similar to ListResources but optimized for batch operations
	listReq := &pb.ListResourcesRequest{
		Filters: req.Filters,
	}

	// Add services as resource types filter
	if len(req.Services) > 0 {
		listReq.Filters["resource_types"] = strings.Join(req.Services, ",")
	}

	listResp, err := p.ListResources(ctx, listReq)
	if err != nil {
		return nil, err
	}

	// Convert ResourceRefs back to Resources (this is a bit awkward but matches the interface)
	var resources []*pb.Resource
	for _, resourceRef := range listResp.Resources {
		// This would ideally be done with a proper conversion
		resource := &pb.Resource{
			Provider: "kubernetes",
			Service:  resourceRef.Service,
			Type:     resourceRef.Type,
			Id:       resourceRef.Id,
			Name:     resourceRef.Name,
			Region:   resourceRef.Region,
			Tags:     resourceRef.BasicAttributes,
		}
		resources = append(resources, resource)
	}

	return &pb.BatchScanResponse{
		Resources: resources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(resources)),
			DurationMs:     0, // Would calculate actual duration
		},
		Errors: []string{}, // Any non-fatal errors
	}, nil
}

// StreamScan streams resources as they are discovered
func (p *KubernetesProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	ctx := stream.Context()

	// If watch mode is enabled, use informers for real-time updates
	if p.config.WatchMode {
		return p.streamWithInformers(ctx, req, stream)
	}

	// Otherwise, do regular scanning and stream results
	clusters := p.getClustersToScan(&pb.ListResourcesRequest{Filters: req.Filters})

	for _, cluster := range clusters {
		if err := p.streamClusterResources(ctx, cluster, req, stream); err != nil {
			return err
		}
	}

	return nil
}

// streamClusterResources streams resources from a specific cluster
func (p *KubernetesProvider) streamClusterResources(ctx context.Context, cluster *ClusterConnection, req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	scanner := NewResourceScannerForCluster(cluster)
	
	// Create a channel for streaming results
	resourceChan := make(chan *pb.Resource, 100)
	errChan := make(chan error, 1)

	// Start scanning in background
	go func() {
		defer close(resourceChan)
		err := scanner.StreamScanResources(ctx, req.Services, resourceChan)
		if err != nil {
			errChan <- err
		}
	}()

	// Stream resources as they arrive
	for {
		select {
		case resource, ok := <-resourceChan:
			if !ok {
				return nil // Channel closed, scanning complete
			}
			if err := stream.Send(resource); err != nil {
				return err
			}
		case err := <-errChan:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// streamWithInformers uses Kubernetes informers for real-time streaming
func (p *KubernetesProvider) streamWithInformers(ctx context.Context, req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	// Set up informers for requested resource types
	informers, err := p.informerCache.SetupInformers(ctx, req.Services)
	if err != nil {
		return fmt.Errorf("failed to setup informers: %w", err)
	}

	// Create event channel
	eventChan := make(chan *pb.Resource, 1000)

	// Start informers
	p.informerCache.StartInformers(ctx, informers, eventChan)

	// Stream events
	for {
		select {
		case resource := <-eventChan:
			if err := stream.Send(resource); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Helper functions

func isSystemNamespace(namespace string) bool {
	systemNamespaces := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default",
	}
	for _, sysNs := range systemNamespaces {
		if namespace == sysNs {
			return true
		}
	}
	return false
}

func (p *KubernetesProvider) getResourcesForAPIGroup(apiGroup string) []string {
	// This would be populated from discovery
	// For now, return common resources for the group
	switch apiGroup {
	case "core":
		return []string{"pods", "services", "configmaps", "secrets", "persistentvolumeclaims"}
	case "apps":
		return []string{"deployments", "replicasets", "statefulsets", "daemonsets"}
	case "networking.k8s.io":
		return []string{"ingresses", "networkpolicies"}
	default:
		return []string{}
	}
}

// GetServiceInfo returns information about a specific Kubernetes API group/service
func (p *KubernetesProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	// In Kubernetes context, "service" is an API group
	apiGroup := req.Service
	
	// Use discovery to get API group information
	groups, err := p.clientset.Discovery().ServerGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to discover API groups: %w", err)
	}

	// Find the requested API group
	for _, group := range groups.Groups {
		groupName := group.Name
		if groupName == "" {
			groupName = "core" // Core API group has empty name
		}
		
		if groupName == apiGroup {
			// Get resource types for this API group
			resourceTypes := p.getResourcesForAPIGroup(apiGroup)
			
			// Get preferred version
			preferredVersion := group.PreferredVersion.Version

			return &pb.ServiceInfoResponse{
				ServiceName:         apiGroup,
				Version:            preferredVersion,
				SupportedResources: resourceTypes,
				RequiredPermissions: []string{
					"get", "list", "watch", // Basic permissions
				},
				Capabilities: map[string]string{
					"provider":       "kubernetes",
					"cluster":        p.activeCluster,
					"api_versions":   p.getVersionsString(group.Versions),
					"retrieved_at":   time.Now().Format(time.RFC3339),
					"watch_enabled":  fmt.Sprintf("%v", p.config.WatchMode),
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("API group %s not found", apiGroup)
}

// ScanService scans resources for a specific Kubernetes API group
func (p *KubernetesProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}

	startTime := time.Now()
	
	// In Kubernetes, service is an API group
	apiGroup := req.Service
	
	// Get resource types for this API group
	resourceTypes := p.getResourcesForAPIGroup(apiGroup)
	if len(resourceTypes) == 0 {
		// Try dynamic discovery
		discovery := NewAPIDiscovery(p.clientset.Discovery(), p.dynamicClient, p.kubeConfig)
		discoveredResources, err := discovery.DiscoverResourcesForAPIGroup(ctx, apiGroup)
		if err != nil {
			return nil, fmt.Errorf("failed to discover resources for API group %s: %w", apiGroup, err)
		}
		
		// Convert discovered resources to resource type names
		for _, res := range discoveredResources {
			resourceTypes = append(resourceTypes, res.Name)
		}
		
		// If still no resources found, return empty response
		if len(resourceTypes) == 0 {
			return &pb.ScanServiceResponse{
				Service:   apiGroup,
				Resources: []*pb.Resource{},
				Stats: &pb.ScanStats{
					TotalResources: 0,
					DurationMs:     0,
					ResourceCounts: map[string]int32{},
					ServiceCounts: map[string]int32{
						apiGroup: 1,
					},
				},
			}, nil
		}
	}

	// Scan resources across all configured clusters
	var allResources []*pb.Resource
	clusters := p.getClustersToScan(&pb.ListResourcesRequest{})
	
	for _, cluster := range clusters {
		scanner := NewResourceScannerForCluster(cluster)
		namespaces := p.getNamespacesToScan(ctx, cluster, &pb.ListResourcesRequest{})
		
		resources, err := scanner.ScanResources(ctx, resourceTypes, namespaces)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resources for cluster %s: %w", cluster.Name, err)
		}
		
		// Enrich with relationships if requested
		if req.IncludeRelationships && p.relationships != nil {
			for _, resource := range resources {
				// Extract basic Kubernetes relationships
				resource.Relationships = p.extractBasicRelationships(resource)
			}
		}
		
		allResources = append(allResources, resources...)
	}

	// Calculate stats
	resourceCounts := make(map[string]int32)
	for _, r := range allResources {
		resourceCounts[r.Type]++
	}

	return &pb.ScanServiceResponse{
		Service:   apiGroup,
		Resources: allResources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(allResources)),
			DurationMs:     time.Since(startTime).Milliseconds(),
			ResourceCounts: resourceCounts,
			ServiceCounts: map[string]int32{
				apiGroup: 1,
			},
		},
	}, nil
}

// StreamScanService streams resources as they are discovered for a specific API group
func (p *KubernetesProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}

	ctx := stream.Context()
	apiGroup := req.Service
	
	// Get resource types for this API group
	resourceTypes := p.getResourcesForAPIGroup(apiGroup)
	if len(resourceTypes) == 0 {
		// No resources for this API group
		return nil
	}

	// If watch mode is enabled, use informers
	if p.config.WatchMode {
		return p.streamWithInformersForService(ctx, apiGroup, resourceTypes, req, stream)
	}

	// Otherwise, scan and stream from each cluster
	clusters := p.getClustersToScan(&pb.ListResourcesRequest{})
	
	for _, cluster := range clusters {
		scanner := NewResourceScannerForCluster(cluster)
		
		// Create a channel for streaming results
		resourceChan := make(chan *pb.Resource, 100)
		errChan := make(chan error, 1)
		
		// Start scanning in background
		go func() {
			defer close(resourceChan)
			err := scanner.StreamScanResources(ctx, resourceTypes, resourceChan)
			if err != nil {
				errChan <- err
			}
		}()
		
		// Stream resources as they arrive
		for {
			select {
			case resource, ok := <-resourceChan:
				if !ok {
					// Channel closed, move to next cluster
					break
				}
				
				// Enrich with relationships if requested
				if req.IncludeRelationships && p.relationships != nil {
					resource.Relationships = p.extractBasicRelationships(resource)
				}
				
				if err := stream.Send(resource); err != nil {
					return fmt.Errorf("failed to send resource: %w", err)
				}
			case err := <-errChan:
				return fmt.Errorf("scanning error for cluster %s: %w", cluster.Name, err)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// streamWithInformersForService uses informers to stream real-time updates for a specific API group
func (p *KubernetesProvider) streamWithInformersForService(ctx context.Context, apiGroup string, resourceTypes []string, req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	// Set up informers for the specific resource types
	informers, err := p.informerCache.SetupInformers(ctx, resourceTypes)
	if err != nil {
		return fmt.Errorf("failed to setup informers: %w", err)
	}

	// Create event channel
	eventChan := make(chan *pb.Resource, 1000)
	
	// Start informers
	p.informerCache.StartInformers(ctx, informers, eventChan)
	
	// Stream events
	for {
		select {
		case resource := <-eventChan:
			// Only send resources from the requested API group
			if p.getAPIGroupForResource(resource) == apiGroup {
				// Enrich with relationships if requested
				if req.IncludeRelationships && p.relationships != nil {
					resource.Relationships = p.extractBasicRelationships(resource)
				}
				
				if err := stream.Send(resource); err != nil {
					return err
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// getAPIGroupForResource extracts the API group from a resource
func (p *KubernetesProvider) getAPIGroupForResource(resource *pb.Resource) string {
	// Extract API group from service field
	if resource.Service != "" {
		return resource.Service
	}
	
	// Try to infer from type
	switch resource.Type {
	case "Pod", "Service", "ConfigMap", "Secret", "PersistentVolumeClaim":
		return "core"
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet":
		return "apps"
	case "Ingress", "NetworkPolicy":
		return "networking.k8s.io"
	default:
		return "unknown"
	}
}

// getVersionsString converts GroupVersionForDiscovery slice to comma-separated string
func (p *KubernetesProvider) getVersionsString(versions []metav1.GroupVersionForDiscovery) string {
	var versionStrings []string
	for _, v := range versions {
		versionStrings = append(versionStrings, v.Version)
	}
	return strings.Join(versionStrings, ",")
}

// extractBasicRelationships extracts basic Kubernetes relationships from a resource
func (p *KubernetesProvider) extractBasicRelationships(resource *pb.Resource) []*pb.Relationship {
	var relationships []*pb.Relationship
	
	// Parse raw data if available
	if resource.RawData == "" {
		return relationships
	}
	
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(resource.RawData), &obj); err != nil {
		return relationships
	}
	
	// Extract metadata
	metadata, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return relationships
	}
	
	namespace := ""
	if ns, ok := metadata["namespace"].(string); ok {
		namespace = ns
	}
	
	// 1. Owner References - most common relationship type
	if ownerRefs, ok := metadata["ownerReferences"].([]interface{}); ok {
		for _, ownerRef := range ownerRefs {
			if owner, ok := ownerRef.(map[string]interface{}); ok {
				ownerKind := owner["kind"].(string)
				ownerName := owner["name"].(string)
				ownerUID := owner["uid"].(string)
				
				targetID := fmt.Sprintf("default/%s/%s/%s", namespace, ownerKind, ownerName)
				
				relationships = append(relationships, &pb.Relationship{
					Type:       "OWNED_BY",
					SourceId:   resource.Id,
					TargetId:   targetID,
					Properties: map[string]string{
						"owner_kind": ownerKind,
						"owner_name": ownerName,
						"owner_uid":  ownerUID,
						"controller": fmt.Sprintf("%v", owner["controller"]),
					},
				})
			}
		}
	}
	
	// 2. Pod to Service relationships
	if resource.Type == "Pod" {
		// Extract labels from pod
		if labels, ok := metadata["labels"].(map[string]interface{}); ok {
			// Find services that might select this pod
			labelStr := p.labelsToString(labels)
			relationships = append(relationships, &pb.Relationship{
				Type:     "SELECTED_BY",
				SourceId: resource.Id,
				// TargetId would be filled by matching service selectors
				Properties: map[string]string{
					"pod_labels": labelStr,
					"namespace":  namespace,
				},
			})
		}
	}
	
	// 3. Service to Pod relationships (reverse of above)
	if resource.Type == "Service" {
		if spec, ok := obj["spec"].(map[string]interface{}); ok {
			if selector, ok := spec["selector"].(map[string]interface{}); ok {
				selectorStr := p.labelsToString(selector)
				relationships = append(relationships, &pb.Relationship{
					Type:       "SELECTS",
					SourceId:   resource.Id,
					Properties: map[string]string{
						"selector":  selectorStr,
						"namespace": namespace,
					},
				})
			}
		}
	}
	
	// 4. ConfigMap/Secret volume mounts
	if resource.Type == "Pod" {
		if spec, ok := obj["spec"].(map[string]interface{}); ok {
			// Check volumes
			if volumes, ok := spec["volumes"].([]interface{}); ok {
				for _, vol := range volumes {
					if volume, ok := vol.(map[string]interface{}); ok {
						// ConfigMap volumes
						if cm, ok := volume["configMap"].(map[string]interface{}); ok {
							if cmName, ok := cm["name"].(string); ok {
								targetID := fmt.Sprintf("default/%s/ConfigMap/%s", namespace, cmName)
								relationships = append(relationships, &pb.Relationship{
									Type:     "MOUNTS",
									SourceId: resource.Id,
									TargetId: targetID,
									Properties: map[string]string{
										"volume_name": volume["name"].(string),
										"mount_type":  "configMap",
									},
								})
							}
						}
						// Secret volumes
						if secret, ok := volume["secret"].(map[string]interface{}); ok {
							if secretName, ok := secret["secretName"].(string); ok {
								targetID := fmt.Sprintf("default/%s/Secret/%s", namespace, secretName)
								relationships = append(relationships, &pb.Relationship{
									Type:     "MOUNTS",
									SourceId: resource.Id,
									TargetId: targetID,
									Properties: map[string]string{
										"volume_name": volume["name"].(string),
										"mount_type":  "secret",
									},
								})
							}
						}
						// PVC volumes
						if pvc, ok := volume["persistentVolumeClaim"].(map[string]interface{}); ok {
							if pvcName, ok := pvc["claimName"].(string); ok {
								targetID := fmt.Sprintf("default/%s/PersistentVolumeClaim/%s", namespace, pvcName)
								relationships = append(relationships, &pb.Relationship{
									Type:     "MOUNTS",
									SourceId: resource.Id,
									TargetId: targetID,
									Properties: map[string]string{
										"volume_name": volume["name"].(string),
										"mount_type":  "pvc",
									},
								})
							}
						}
					}
				}
			}
		}
	}
	
	// 5. Ingress to Service relationships
	if resource.Type == "Ingress" {
		if spec, ok := obj["spec"].(map[string]interface{}); ok {
			// Check rules
			if rules, ok := spec["rules"].([]interface{}); ok {
				for _, rule := range rules {
					if r, ok := rule.(map[string]interface{}); ok {
						if http, ok := r["http"].(map[string]interface{}); ok {
							if paths, ok := http["paths"].([]interface{}); ok {
								for _, path := range paths {
									if p, ok := path.(map[string]interface{}); ok {
										if backend, ok := p["backend"].(map[string]interface{}); ok {
											if service, ok := backend["service"].(map[string]interface{}); ok {
												if serviceName, ok := service["name"].(string); ok {
													targetID := fmt.Sprintf("default/%s/Service/%s", namespace, serviceName)
													relationships = append(relationships, &pb.Relationship{
														Type:     "ROUTES_TO",
														SourceId: resource.Id,
														TargetId: targetID,
														Properties: map[string]string{
															"path": p["path"].(string),
															"host": r["host"].(string),
														},
													})
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	
	// 6. NetworkPolicy relationships
	if resource.Type == "NetworkPolicy" {
		if spec, ok := obj["spec"].(map[string]interface{}); ok {
			// Pod selector
			if podSelector, ok := spec["podSelector"].(map[string]interface{}); ok {
				if matchLabels, ok := podSelector["matchLabels"].(map[string]interface{}); ok {
					selectorStr := p.labelsToString(matchLabels)
					relationships = append(relationships, &pb.Relationship{
						Type:       "APPLIES_TO",
						SourceId:   resource.Id,
						Properties: map[string]string{
							"pod_selector": selectorStr,
							"namespace":    namespace,
						},
					})
				}
			}
		}
	}
	
	return relationships
}

// labelsToString converts a map of labels to a string representation
func (p *KubernetesProvider) labelsToString(labels map[string]interface{}) string {
	var labelPairs []string
	for k, v := range labels {
		labelPairs = append(labelPairs, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(labelPairs, ",")
}

// ConfigureDiscovery configures discovery sources for the Kubernetes provider
func (p *KubernetesProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	return &pb.ConfigureDiscoveryResponse{
		Success: true,
	}, nil
}

// AnalyzeDiscoveredData analyzes discovered Kubernetes data
func (p *KubernetesProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	return &pb.AnalysisResponse{
		Services: []*pb.ServiceAnalysis{},
		Success:  true,
	}, nil
}

// GenerateFromAnalysis generates code from analysis results
func (p *KubernetesProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	return &pb.GenerateResponse{
		Files: []*pb.GeneratedFile{},
		Stats: &pb.GenerationStats{},
	}, nil
}
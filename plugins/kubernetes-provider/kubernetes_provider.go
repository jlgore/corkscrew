package main

import (
	"context"
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
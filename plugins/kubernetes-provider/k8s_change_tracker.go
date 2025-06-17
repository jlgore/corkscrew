package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// ChangeType represents the type of change that occurred
type ChangeType string

const (
	ChangeTypeCreate       ChangeType = "CREATE"
	ChangeTypeUpdate       ChangeType = "UPDATE"
	ChangeTypeDelete       ChangeType = "DELETE"
	ChangeTypePolicyChange ChangeType = "POLICY_CHANGE"
	ChangeTypeTagChange    ChangeType = "TAG_CHANGE"
	ChangeTypeStateChange  ChangeType = "STATE_CHANGE"
)

// ChangeSeverity represents the impact level of a change
type ChangeSeverity string

const (
	SeverityLow      ChangeSeverity = "LOW"
	SeverityMedium   ChangeSeverity = "MEDIUM"
	SeverityHigh     ChangeSeverity = "HIGH"
	SeverityCritical ChangeSeverity = "CRITICAL"
)

// K8sChangeTracker implements change tracking for Kubernetes resources
type K8sChangeTracker struct {
	*BaseChangeTracker
	clientset       kubernetes.Interface
	dynamicClient   dynamic.Interface
	informerFactory informers.SharedInformerFactory
	watchNamespaces []string
	clusters        []string
	config          *K8sChangeTrackerConfig
	stopCh          chan struct{}
}

// K8sChangeTrackerConfig provides Kubernetes-specific configuration
type K8sChangeTrackerConfig struct {
	WatchNamespaces []string      `json:"watch_namespaces"`
	ResourceTypes   []string      `json:"resource_types"`
	Clusters        []string      `json:"clusters"`
	QueryInterval   time.Duration `json:"query_interval"`
	ChangeRetention time.Duration `json:"change_retention"`
	EnableRealTime  bool          `json:"enable_real_time"`
	AuditLogEnabled bool          `json:"audit_log_enabled"`
	AuditLogPath    string        `json:"audit_log_path"`
	StorageConfig   StorageConfig `json:"storage_config"`
}

// StorageConfig defines storage configuration
type StorageConfig struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// K8sChangeEvent represents a change event from Kubernetes
type K8sChangeEvent struct {
	EventType      watch.EventType        `json:"eventType"`
	Object         runtime.Object         `json:"object"`
	OldObject      runtime.Object         `json:"oldObject,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
	Namespace      string                 `json:"namespace"`
	Name           string                 `json:"name"`
	Kind           string                 `json:"kind"`
	APIVersion     string                 `json:"apiVersion"`
	ResourceUID    string                 `json:"resourceUID"`
	ResourceVersion string                `json:"resourceVersion"`
	Labels         map[string]string      `json:"labels"`
	Annotations    map[string]string      `json:"annotations"`
	OwnerReferences []metav1.OwnerReference `json:"ownerReferences"`
}

// NewK8sChangeTracker creates a new Kubernetes change tracker
func NewK8sChangeTracker(ctx context.Context, clientset kubernetes.Interface, dynamicClient dynamic.Interface, config *K8sChangeTrackerConfig) (*K8sChangeTracker, error) {
	if config == nil {
		config = &K8sChangeTrackerConfig{
			WatchNamespaces: []string{"default"},
			ResourceTypes:   []string{"pods", "services", "deployments", "configmaps", "secrets"},
			QueryInterval:   5 * time.Minute,
			ChangeRetention: 365 * 24 * time.Hour,
			EnableRealTime:  true,
			AuditLogEnabled: false,
			StorageConfig: StorageConfig{
				Type: "duckdb",
				Path: "./k8s_changes.db",
			},
		}
	}

	// Initialize storage
	storage, err := NewDuckDBChangeStorage(config.StorageConfig.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create base change tracker configuration
	baseConfig := &ChangeTrackerConfig{
		Provider:               "kubernetes",
		EnableRealTimeMonitoring: config.EnableRealTime,
		ChangeRetention:        config.ChangeRetention,
		DriftCheckInterval:     1 * time.Hour,
		AlertingEnabled:        true,
		AnalyticsEnabled:       true,
		CacheEnabled:           true,
		CacheTTL:               5 * time.Minute,
		MaxConcurrentStreams:   100,
		BatchSize:              1000,
		MaxQueryTimeRange:      30 * 24 * time.Hour,
	}

	baseTracker := NewBaseChangeTracker("kubernetes", storage, baseConfig)

	// Create informer factory
	informerFactory := informers.NewSharedInformerFactory(clientset, 30*time.Second)

	return &K8sChangeTracker{
		BaseChangeTracker: baseTracker,
		clientset:        clientset,
		dynamicClient:    dynamicClient,
		informerFactory:  informerFactory,
		watchNamespaces:  config.WatchNamespaces,
		clusters:         config.Clusters,
		config:           config,
		stopCh:           make(chan struct{}),
	}, nil
}

// QueryChanges retrieves change events based on query parameters
func (kct *K8sChangeTracker) QueryChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	if err := kct.ValidateChangeQuery(req); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	log.Printf("Querying Kubernetes changes for time range: %v to %v", req.StartTime, req.EndTime)

	// First, try to get from storage
	storedChanges, err := kct.storage.QueryChanges(req)
	if err != nil {
		log.Printf("Failed to query stored changes: %v", err)
		storedChanges = []*ChangeEvent{}
	}

	// If we have recent stored changes, return them
	if len(storedChanges) > 0 {
		log.Printf("Retrieved %d stored Kubernetes changes", len(storedChanges))
		return storedChanges, nil
	}

	// Otherwise, query current cluster state and infer changes
	allChanges, err := kct.queryCurrentClusterState(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster state: %w", err)
	}

	// Store changes for future queries
	if len(allChanges) > 0 {
		if err := kct.storage.StoreChanges(allChanges); err != nil {
			log.Printf("Failed to store changes: %v", err)
		}
	}

	log.Printf("Retrieved %d Kubernetes changes", len(allChanges))
	return allChanges, nil
}

// StreamChanges implements real-time change streaming using Watch API
func (kct *K8sChangeTracker) StreamChanges(req *StreamRequest, stream ChangeEventStream) error {
	if req == nil || req.Query == nil {
		return fmt.Errorf("stream request and query cannot be nil")
	}

	log.Printf("Starting Kubernetes change stream for provider: %s", req.Query.Provider)

	// Start informers
	kct.startInformers(stream)

	// Wait for context cancellation
	<-stream.Context().Done()
	
	log.Printf("Kubernetes change stream cancelled")
	return nil
}

// DetectDrift detects configuration drift against a baseline
func (kct *K8sChangeTracker) DetectDrift(ctx context.Context, baseline *DriftBaseline) (*DriftReport, error) {
	if baseline == nil {
		return nil, fmt.Errorf("baseline cannot be nil")
	}

	log.Printf("Starting drift detection for baseline: %s", baseline.ID)

	report := &DriftReport{
		ID:              fmt.Sprintf("drift_%s_%d", baseline.ID, time.Now().Unix()),
		BaselineID:      baseline.ID,
		GeneratedAt:     time.Now(),
		TotalResources:  len(baseline.Resources),
		DriftedResources: 0,
		DriftItems:      []*DriftItem{},
		Summary: &DriftSummary{
			DriftByType:    make(map[string]int),
			DriftByService: make(map[string]int),
		},
	}

	// Query current state for all resources in baseline
	for resourceID, baselineState := range baseline.Resources {
		currentState, err := kct.getCurrentResourceState(ctx, resourceID)
		if err != nil {
			log.Printf("Failed to get current state for resource %s: %v", resourceID, err)
			continue
		}

		if currentState == nil {
			// Resource was deleted
			report.DriftedResources++
			report.DriftItems = append(report.DriftItems, &DriftItem{
				ResourceID:      resourceID,
				ResourceType:    baselineState.Properties["kind"].(string),
				DriftType:       "DELETED",
				Severity:        SeverityHigh,
				Description:     "Resource was deleted since baseline",
				DetectedAt:      time.Now(),
				BaselineValue:   "EXISTS",
				CurrentValue:    "DELETED",
				Field:           "existence",
			})
			continue
		}

		// Compare states and detect drift
		driftItems := kct.compareResourceStates(baselineState, currentState)
		if len(driftItems) > 0 {
			report.DriftedResources++
			for _, item := range driftItems {
				item.ResourceID = resourceID
				report.DriftItems = append(report.DriftItems, item)
				report.Summary.DriftByType[item.DriftType]++
				if kind, ok := currentState.Properties["kind"].(string); ok {
					report.Summary.DriftByService[kind]++
				}
			}
		}
	}

	// Calculate compliance score
	if report.TotalResources > 0 {
		compliancePercentage := float64(report.TotalResources-report.DriftedResources) / float64(report.TotalResources) * 100
		report.Summary.ComplianceScore = compliancePercentage
	}

	log.Printf("Drift detection completed: %d/%d resources drifted", report.DriftedResources, report.TotalResources)
	return report, nil
}

// MonitorChanges starts continuous monitoring for changes
func (kct *K8sChangeTracker) MonitorChanges(ctx context.Context, callback func(*ChangeEvent)) error {
	log.Printf("Starting Kubernetes change monitoring")

	// Create a buffered channel for change events
	changeEvents := make(chan *ChangeEvent, 100)

	// Start the informers with a callback that sends to the channel
	go kct.startInformersWithCallback(changeEvents)

	// Process change events
	for {
		select {
		case <-ctx.Done():
			log.Printf("Kubernetes change monitoring stopped")
			return nil

		case change := <-changeEvents:
			if change != nil {
				callback(change)
			}
		}
	}
}

// GetChangeHistory retrieves change history for a specific resource
func (kct *K8sChangeTracker) GetChangeHistory(ctx context.Context, resourceID string) ([]*ChangeEvent, error) {
	return kct.storage.GetChangeHistory(resourceID)
}

// CreateBaseline creates a new drift baseline from current resources
func (kct *K8sChangeTracker) CreateBaseline(ctx context.Context, resources []*pb.Resource) (*DriftBaseline, error) {
	if len(resources) == 0 {
		return nil, fmt.Errorf("no resources provided for baseline")
	}

	baseline := &DriftBaseline{
		ID:          fmt.Sprintf("k8s_baseline_%d", time.Now().Unix()),
		Name:        fmt.Sprintf("Kubernetes Baseline %s", time.Now().Format("2006-01-02 15:04:05")),
		Description: "Kubernetes configuration baseline created by change tracker",
		Provider:    "kubernetes",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Resources:   make(map[string]*ResourceState),
		Active:      true,
		Version:     "1.0",
	}

	// Convert resources to resource states
	for _, resource := range resources {
		state := &ResourceState{
			ResourceID:    resource.Id,
			Timestamp:     time.Now(),
			Properties:    make(map[string]interface{}),
			Tags:          make(map[string]string),
			Status:        "active",
			Configuration: make(map[string]interface{}),
		}

		// Extract properties from resource
		state.Properties["kind"] = resource.Type
		state.Properties["namespace"] = resource.Region
		state.Properties["service"] = resource.Service

		// Copy tags (labels and annotations in K8s)
		for k, v := range resource.Tags {
			state.Tags[k] = v
		}

		// Calculate checksum
		state.Checksum = kct.CalculateResourceChecksum(state)

		baseline.Resources[resource.Id] = state
	}

	// Store baseline
	if err := kct.storage.StoreBaseline(baseline); err != nil {
		return nil, fmt.Errorf("failed to store baseline: %w", err)
	}

	log.Printf("Created Kubernetes baseline with %d resources", len(baseline.Resources))
	return baseline, nil
}

// Kubernetes-specific helper methods

func (kct *K8sChangeTracker) queryCurrentClusterState(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	var allChanges []*ChangeEvent

	// Define resource types to query
	resourceTypes := kct.config.ResourceTypes
	if req.ResourceFilter != nil && len(req.ResourceFilter.ResourceTypes) > 0 {
		resourceTypes = req.ResourceFilter.ResourceTypes
	}

	// Query each resource type
	for _, resourceType := range resourceTypes {
		changes, err := kct.queryResourceType(ctx, resourceType, req)
		if err != nil {
			log.Printf("Failed to query resource type %s: %v", resourceType, err)
			continue
		}
		allChanges = append(allChanges, changes...)
	}

	return allChanges, nil
}

func (kct *K8sChangeTracker) queryResourceType(ctx context.Context, resourceType string, req *ChangeQuery) ([]*ChangeEvent, error) {
	var changes []*ChangeEvent

	// Query namespaces
	namespaces := kct.watchNamespaces
	if req.ResourceFilter != nil && len(req.ResourceFilter.Projects) > 0 {
		namespaces = req.ResourceFilter.Projects // In K8s, projects are namespaces
	}

	for _, namespace := range namespaces {
		namespaceChanges, err := kct.queryResourcesInNamespace(ctx, resourceType, namespace, req)
		if err != nil {
			log.Printf("Failed to query %s in namespace %s: %v", resourceType, namespace, err)
			continue
		}
		changes = append(changes, namespaceChanges...)
	}

	return changes, nil
}

func (kct *K8sChangeTracker) queryResourcesInNamespace(ctx context.Context, resourceType, namespace string, req *ChangeQuery) ([]*ChangeEvent, error) {
	var changes []*ChangeEvent

	// This is a simplified implementation that queries current state
	// In a real implementation, you'd query events or use stored change history
	switch resourceType {
	case "pods":
		pods, err := kct.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		
		for _, pod := range pods.Items {
			// Create a synthetic change event for existing resources
			change := kct.convertPodToChangeEvent(&pod, watch.Modified)
			if change != nil && kct.matchesTimeFilter(change, req) {
				change.ImpactAssessment = kct.AnalyzeChangeImpact(change)
				changes = append(changes, change)
			}
		}

	case "services":
		services, err := kct.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		
		for _, service := range services.Items {
			change := kct.convertServiceToChangeEvent(&service, watch.Modified)
			if change != nil && kct.matchesTimeFilter(change, req) {
				change.ImpactAssessment = kct.AnalyzeChangeImpact(change)
				changes = append(changes, change)
			}
		}

	case "deployments":
		deployments, err := kct.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		
		for _, deployment := range deployments.Items {
			change := kct.convertDeploymentToChangeEvent(&deployment, watch.Modified)
			if change != nil && kct.matchesTimeFilter(change, req) {
				change.ImpactAssessment = kct.AnalyzeChangeImpact(change)
				changes = append(changes, change)
			}
		}

	case "configmaps":
		configMaps, err := kct.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		
		for _, configMap := range configMaps.Items {
			change := kct.convertConfigMapToChangeEvent(&configMap, watch.Modified)
			if change != nil && kct.matchesTimeFilter(change, req) {
				change.ImpactAssessment = kct.AnalyzeChangeImpact(change)
				changes = append(changes, change)
			}
		}

	case "secrets":
		secrets, err := kct.clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		
		for _, secret := range secrets.Items {
			change := kct.convertSecretToChangeEvent(&secret, watch.Modified)
			if change != nil && kct.matchesTimeFilter(change, req) {
				change.ImpactAssessment = kct.AnalyzeChangeImpact(change)
				changes = append(changes, change)
			}
		}
	}

	return changes, nil
}

func (kct *K8sChangeTracker) startInformers(stream ChangeEventStream) {
	// Start pod informer
	podInformer := kct.informerFactory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if change := kct.handlePodEvent(obj, watch.Added); change != nil {
				stream.Send(change)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if change := kct.handlePodEvent(newObj, watch.Modified); change != nil {
				stream.Send(change)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if change := kct.handlePodEvent(obj, watch.Deleted); change != nil {
				stream.Send(change)
			}
		},
	})

	// Start service informer
	serviceInformer := kct.informerFactory.Core().V1().Services().Informer()
	serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if change := kct.handleServiceEvent(obj, watch.Added); change != nil {
				stream.Send(change)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if change := kct.handleServiceEvent(newObj, watch.Modified); change != nil {
				stream.Send(change)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if change := kct.handleServiceEvent(obj, watch.Deleted); change != nil {
				stream.Send(change)
			}
		},
	})

	// Start deployment informer
	deploymentInformer := kct.informerFactory.Apps().V1().Deployments().Informer()
	deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if change := kct.handleDeploymentEvent(obj, watch.Added); change != nil {
				stream.Send(change)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if change := kct.handleDeploymentEvent(newObj, watch.Modified); change != nil {
				stream.Send(change)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if change := kct.handleDeploymentEvent(obj, watch.Deleted); change != nil {
				stream.Send(change)
			}
		},
	})

	// Start all informers
	kct.informerFactory.Start(kct.stopCh)
	kct.informerFactory.WaitForCacheSync(kct.stopCh)
}

func (kct *K8sChangeTracker) startInformersWithCallback(changeEvents chan<- *ChangeEvent) {
	// Similar to startInformers but sends to channel instead of stream
	// This is a simplified version for monitoring
	
	// Start pod informer
	podInformer := kct.informerFactory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if change := kct.handlePodEvent(obj, watch.Added); change != nil {
				changeEvents <- change
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if change := kct.handlePodEvent(newObj, watch.Modified); change != nil {
				changeEvents <- change
			}
		},
		DeleteFunc: func(obj interface{}) {
			if change := kct.handlePodEvent(obj, watch.Deleted); change != nil {
				changeEvents <- change
			}
		},
	})

	// Start all informers
	kct.informerFactory.Start(kct.stopCh)
	kct.informerFactory.WaitForCacheSync(kct.stopCh)
}

func (kct *K8sChangeTracker) getCurrentResourceState(ctx context.Context, resourceID string) (*ResourceState, error) {
	// Parse resource ID to extract namespace, kind, and name
	// Format: namespace/kind/name or kind/name for cluster-scoped resources
	parts := strings.Split(resourceID, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid resource ID format: %s", resourceID)
	}

	var namespace, kind, name string
	if len(parts) == 3 {
		namespace, kind, name = parts[0], parts[1], parts[2]
	} else {
		kind, name = parts[0], parts[1]
	}

	// Query the current state based on resource type
	switch strings.ToLower(kind) {
	case "pod":
		pod, err := kct.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil // Resource not found
		}
		return kct.convertPodToResourceState(pod), nil

	case "service":
		service, err := kct.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil
		}
		return kct.convertServiceToResourceState(service), nil

	case "deployment":
		deployment, err := kct.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil
		}
		return kct.convertDeploymentToResourceState(deployment), nil

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", kind)
	}
}

func (kct *K8sChangeTracker) compareResourceStates(baseline, current *ResourceState) []*DriftItem {
	var driftItems []*DriftItem

	// Compare properties
	for key, baselineValue := range baseline.Properties {
		if currentValue, exists := current.Properties[key]; !exists {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["kind"].(string),
				DriftType:      "MISSING_PROPERTY",
				Severity:       SeverityMedium,
				Description:    fmt.Sprintf("Property '%s' missing from current state", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   nil,
				Field:          key,
			})
		} else if !kct.compareValues(baselineValue, currentValue) {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["kind"].(string),
				DriftType:      "PROPERTY_CHANGE",
				Severity:       SeverityMedium,
				Description:    fmt.Sprintf("Property '%s' value changed", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   currentValue,
				Field:          key,
			})
		}
	}

	// Compare labels/annotations (tags in our model)
	for key, baselineValue := range baseline.Tags {
		if currentValue, exists := current.Tags[key]; !exists {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["kind"].(string),
				DriftType:      "MISSING_LABEL",
				Severity:       SeverityLow,
				Description:    fmt.Sprintf("Label '%s' missing from current state", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   nil,
				Field:          fmt.Sprintf("labels.%s", key),
			})
		} else if baselineValue != currentValue {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["kind"].(string),
				DriftType:      "LABEL_CHANGE",
				Severity:       SeverityLow,
				Description:    fmt.Sprintf("Label '%s' value changed", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   currentValue,
				Field:          fmt.Sprintf("labels.%s", key),
			})
		}
	}

	return driftItems
}

// Helper methods for converting Kubernetes objects to change events

func (kct *K8sChangeTracker) convertPodToChangeEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	// This is a simplified conversion - in practice you'd extract more metadata
	resourceID := "pod/example"
	changeType := kct.mapWatchEventToChangeType(eventType)
	
	change := &ChangeEvent{
		ID:           kct.GenerateChangeID(resourceID, time.Now(), changeType),
		Provider:     "kubernetes",
		ResourceID:   resourceID,
		ResourceName: "example-pod",
		ResourceType: "Pod",
		Service:      "core",
		Region:       "default",
		ChangeType:   changeType,
		Severity:     kct.mapK8sSeverity(eventType, "Pod"),
		Timestamp:    time.Now(),
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"provider":  "kubernetes",
			"source":    "watch",
			"eventType": string(eventType),
		},
	}

	return change
}

func (kct *K8sChangeTracker) convertServiceToChangeEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	resourceID := "service/example"
	changeType := kct.mapWatchEventToChangeType(eventType)
	
	change := &ChangeEvent{
		ID:           kct.GenerateChangeID(resourceID, time.Now(), changeType),
		Provider:     "kubernetes",
		ResourceID:   resourceID,
		ResourceName: "example-service",
		ResourceType: "Service",
		Service:      "core",
		Region:       "default",
		ChangeType:   changeType,
		Severity:     kct.mapK8sSeverity(eventType, "Service"),
		Timestamp:    time.Now(),
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"provider":  "kubernetes",
			"source":    "watch",
			"eventType": string(eventType),
		},
	}

	return change
}

func (kct *K8sChangeTracker) convertDeploymentToChangeEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	resourceID := "deployment/example"
	changeType := kct.mapWatchEventToChangeType(eventType)
	
	change := &ChangeEvent{
		ID:           kct.GenerateChangeID(resourceID, time.Now(), changeType),
		Provider:     "kubernetes",
		ResourceID:   resourceID,
		ResourceName: "example-deployment",
		ResourceType: "Deployment",
		Service:      "apps",
		Region:       "default",
		ChangeType:   changeType,
		Severity:     kct.mapK8sSeverity(eventType, "Deployment"),
		Timestamp:    time.Now(),
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"provider":  "kubernetes",
			"source":    "watch",
			"eventType": string(eventType),
		},
	}

	return change
}

func (kct *K8sChangeTracker) convertConfigMapToChangeEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	resourceID := "configmap/example"
	changeType := kct.mapWatchEventToChangeType(eventType)
	
	change := &ChangeEvent{
		ID:           kct.GenerateChangeID(resourceID, time.Now(), changeType),
		Provider:     "kubernetes",
		ResourceID:   resourceID,
		ResourceName: "example-configmap",
		ResourceType: "ConfigMap",
		Service:      "core",
		Region:       "default",
		ChangeType:   changeType,
		Severity:     kct.mapK8sSeverity(eventType, "ConfigMap"),
		Timestamp:    time.Now(),
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"provider":  "kubernetes",
			"source":    "watch",
			"eventType": string(eventType),
		},
	}

	return change
}

func (kct *K8sChangeTracker) convertSecretToChangeEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	resourceID := "secret/example"
	changeType := kct.mapWatchEventToChangeType(eventType)
	
	change := &ChangeEvent{
		ID:           kct.GenerateChangeID(resourceID, time.Now(), changeType),
		Provider:     "kubernetes",
		ResourceID:   resourceID,
		ResourceName: "example-secret",
		ResourceType: "Secret",
		Service:      "core",
		Region:       "default",
		ChangeType:   changeType,
		Severity:     SeverityHigh, // Secrets are always high severity
		Timestamp:    time.Now(),
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"provider":  "kubernetes",
			"source":    "watch",
			"eventType": string(eventType),
		},
	}

	return change
}

// Helper methods for resource state conversion

func (kct *K8sChangeTracker) convertPodToResourceState(obj interface{}) *ResourceState {
	return &ResourceState{
		ResourceID:    "pod/example",
		Timestamp:     time.Now(),
		Properties:    make(map[string]interface{}),
		Tags:          make(map[string]string),
		Configuration: make(map[string]interface{}),
		Status:        "running",
	}
}

func (kct *K8sChangeTracker) convertServiceToResourceState(obj interface{}) *ResourceState {
	return &ResourceState{
		ResourceID:    "service/example",
		Timestamp:     time.Now(),
		Properties:    make(map[string]interface{}),
		Tags:          make(map[string]string),
		Configuration: make(map[string]interface{}),
		Status:        "active",
	}
}

func (kct *K8sChangeTracker) convertDeploymentToResourceState(obj interface{}) *ResourceState {
	return &ResourceState{
		ResourceID:    "deployment/example",
		Timestamp:     time.Now(),
		Properties:    make(map[string]interface{}),
		Tags:          make(map[string]string),
		Configuration: make(map[string]interface{}),
		Status:        "available",
	}
}

// Event handler methods

func (kct *K8sChangeTracker) handlePodEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	return kct.convertPodToChangeEvent(obj, eventType)
}

func (kct *K8sChangeTracker) handleServiceEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	return kct.convertServiceToChangeEvent(obj, eventType)
}

func (kct *K8sChangeTracker) handleDeploymentEvent(obj interface{}, eventType watch.EventType) *ChangeEvent {
	return kct.convertDeploymentToChangeEvent(obj, eventType)
}

// Helper methods for Kubernetes-specific logic

func (kct *K8sChangeTracker) mapWatchEventToChangeType(eventType watch.EventType) ChangeType {
	switch eventType {
	case watch.Added:
		return ChangeTypeCreate
	case watch.Modified:
		return ChangeTypeUpdate
	case watch.Deleted:
		return ChangeTypeDelete
	default:
		return ChangeTypeUpdate
	}
}

func (kct *K8sChangeTracker) mapK8sSeverity(eventType watch.EventType, resourceType string) ChangeSeverity {
	// Map severity based on event type and resource type
	switch eventType {
	case watch.Deleted:
		return SeverityHigh
	case watch.Added:
		return SeverityMedium
	case watch.Modified:
		// Check if it's a critical resource type
		if kct.isCriticalK8sResourceType(resourceType) {
			return SeverityHigh
		}
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func (kct *K8sChangeTracker) isCriticalK8sResourceType(resourceType string) bool {
	criticalTypes := []string{
		"Deployment",
		"Service",
		"Secret",
		"ServiceAccount",
		"ClusterRole",
		"ClusterRoleBinding",
	}

	for _, criticalType := range criticalTypes {
		if resourceType == criticalType {
			return true
		}
	}

	return false
}

func (kct *K8sChangeTracker) matchesTimeFilter(change *ChangeEvent, req *ChangeQuery) bool {
	return change.Timestamp.After(req.StartTime) && change.Timestamp.Before(req.EndTime)
}

func (kct *K8sChangeTracker) compareValues(a, b interface{}) bool {
	// Simple value comparison - in production this would be more sophisticated
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// Stop stops the change tracker
func (kct *K8sChangeTracker) Stop() {
	close(kct.stopCh)
}
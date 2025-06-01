package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// InformerCache manages Kubernetes informers for efficient resource watching
type InformerCache struct {
	clientFactory     *ClientFactory
	informerFactory   dynamicinformer.DynamicSharedInformerFactory
	resourceInformers map[string]cache.SharedIndexInformer
	mu                sync.RWMutex
	stopCh            chan struct{}
}

// NewInformerCache creates a new informer cache
func NewInformerCache(clientFactory *ClientFactory) *InformerCache {
	return &InformerCache{
		clientFactory:     clientFactory,
		resourceInformers: make(map[string]cache.SharedIndexInformer),
		stopCh:            make(chan struct{}),
	}
}

// SetupInformers sets up informers for the specified resource types
func (ic *InformerCache) SetupInformers(ctx context.Context, resourceTypes []string) (map[string]cache.SharedIndexInformer, error) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	// Create informer factory if not exists
	if ic.informerFactory == nil {
		ic.informerFactory = dynamicinformer.NewDynamicSharedInformerFactory(
			ic.clientFactory.GetDynamicClient(),
			30*time.Second, // Resync period
		)
	}

	informers := make(map[string]cache.SharedIndexInformer)

	for _, resourceType := range resourceTypes {
		// Get resource definition
		resourceDef, err := ic.getResourceDefinition(resourceType)
		if err != nil {
			fmt.Printf("Failed to get definition for %s: %v\n", resourceType, err)
			continue
		}

		// Create GVR
		gvr := schema.GroupVersionResource{
			Group:    resourceDef.Group,
			Version:  resourceDef.Version,
			Resource: resourceDef.Name,
		}

		// Create informer
		informer := ic.informerFactory.ForResource(gvr).Informer()
		
		// Store informer
		key := fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
		ic.resourceInformers[key] = informer
		informers[resourceType] = informer
	}

	return informers, nil
}

// StartInformers starts the informers and begins streaming events
func (ic *InformerCache) StartInformers(ctx context.Context, informers map[string]cache.SharedIndexInformer, eventChan chan<- *pb.Resource) {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	// Add event handlers to each informer
	for resourceType, informer := range informers {
		rt := resourceType // Capture for closure
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				ic.handleEvent("ADDED", rt, obj, eventChan)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				ic.handleEvent("MODIFIED", rt, newObj, eventChan)
			},
			DeleteFunc: func(obj interface{}) {
				ic.handleEvent("DELETED", rt, obj, eventChan)
			},
		})
	}

	// Start the informer factory
	ic.informerFactory.Start(ic.stopCh)

	// Wait for caches to sync
	ic.informerFactory.WaitForCacheSync(ic.stopCh)
}

// handleEvent processes informer events and sends them to the event channel
func (ic *InformerCache) handleEvent(eventType, resourceType string, obj interface{}, eventChan chan<- *pb.Resource) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		// Try to handle DeletedFinalStateUnknown
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
	}

	resource := ic.convertToResource(unstructuredObj, resourceType)
	if resource.Tags == nil {
		resource.Tags = make(map[string]string)
	}
	resource.Tags["event_type"] = eventType
	resource.Tags["event_time"] = time.Now().Format(time.RFC3339)

	select {
	case eventChan <- resource:
	default:
		// Channel full, drop event
		fmt.Printf("Warning: dropping event for %s/%s\n", resourceType, unstructuredObj.GetName())
	}
}

// convertToResource converts an unstructured object to pb.Resource
func (ic *InformerCache) convertToResource(obj *unstructured.Unstructured, resourceType string) *pb.Resource {
	namespace := obj.GetNamespace()
	resourceID := fmt.Sprintf("default/%s/%s/%s", namespace, obj.GetKind(), obj.GetName())

	tags := obj.GetLabels()
	if tags == nil {
		tags = make(map[string]string)
	}
	tags["uid"] = string(obj.GetUID())
	tags["resourceVersion"] = obj.GetResourceVersion()
	tags["generation"] = fmt.Sprintf("%d", obj.GetGeneration())
	tags["creationTimestamp"] = obj.GetCreationTimestamp().Format(time.RFC3339)
	tags["informer_cache"] = "true"
	tags["api_version"] = obj.GetAPIVersion()
	tags["kind"] = obj.GetKind()

	resource := &pb.Resource{
		Provider:     "kubernetes",
		Service:      obj.GetAPIVersion(),
		Type:         obj.GetKind(),
		Id:           resourceID,
		Name:         obj.GetName(),
		Region:       namespace, // Using Region for namespace
		DiscoveredAt: timestamppb.Now(),
		Tags:         tags,
	}

	// Add annotations as tags
	for k, v := range obj.GetAnnotations() {
		resource.Tags["annotation."+k] = v
	}

	return resource
}

// GetCachedResources returns all cached resources of a specific type
func (ic *InformerCache) GetCachedResources(resourceType string) ([]*pb.Resource, error) {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	// Find the informer for this resource type
	var informer cache.SharedIndexInformer
	for key, inf := range ic.resourceInformers {
		if key == resourceType {
			informer = inf
			break
		}
	}

	if informer == nil {
		return nil, fmt.Errorf("no informer found for resource type %s", resourceType)
	}

	// Get all objects from the informer's store
	objects := informer.GetStore().List()
	
	var resources []*pb.Resource
	for _, obj := range objects {
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		resource := ic.convertToResource(unstructuredObj, resourceType)
		resources = append(resources, resource)
	}

	return resources, nil
}

// Stop stops all informers
func (ic *InformerCache) Stop() {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	close(ic.stopCh)
	ic.stopCh = make(chan struct{})
}

// getResourceDefinition gets the resource definition for a given type
// This is a simplified version - in production, this would use the discovery client
func (ic *InformerCache) getResourceDefinition(resourceType string) (*ResourceDefinition, error) {
	// Common resource mappings
	commonResources := map[string]*ResourceDefinition{
		"pods": {
			Group:      "",
			Version:    "v1",
			Kind:       "Pod",
			Name:       "pods",
			Namespaced: true,
		},
		"services": {
			Group:      "",
			Version:    "v1",
			Kind:       "Service",
			Name:       "services",
			Namespaced: true,
		},
		"deployments": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "Deployment",
			Name:       "deployments",
			Namespaced: true,
		},
		"replicasets": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "ReplicaSet",
			Name:       "replicasets",
			Namespaced: true,
		},
		"statefulsets": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "StatefulSet",
			Name:       "statefulsets",
			Namespaced: true,
		},
		"daemonsets": {
			Group:      "apps",
			Version:    "v1",
			Kind:       "DaemonSet",
			Name:       "daemonsets",
			Namespaced: true,
		},
		"configmaps": {
			Group:      "",
			Version:    "v1",
			Kind:       "ConfigMap",
			Name:       "configmaps",
			Namespaced: true,
		},
		"secrets": {
			Group:      "",
			Version:    "v1",
			Kind:       "Secret",
			Name:       "secrets",
			Namespaced: true,
		},
		"ingresses": {
			Group:      "networking.k8s.io",
			Version:    "v1",
			Kind:       "Ingress",
			Name:       "ingresses",
			Namespaced: true,
		},
	}

	if def, ok := commonResources[resourceType]; ok {
		return def, nil
	}

	return nil, fmt.Errorf("resource type %s not found", resourceType)
}

// WatchResource sets up a watch for a specific resource type
func (ic *InformerCache) WatchResource(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (<-chan *pb.Resource, error) {
	eventChan := make(chan *pb.Resource, 100)

	// Create a specific informer for this resource
	var informer cache.SharedIndexInformer
	if namespace != "" {
		// Namespace-scoped informer
		informer = ic.informerFactory.ForResource(gvr).Informer()
	} else {
		// Cluster-scoped informer
		informer = ic.informerFactory.ForResource(gvr).Informer()
	}

	// Add event handler
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if resource := ic.processWatchEvent("ADDED", obj); resource != nil {
				select {
				case eventChan <- resource:
				case <-ctx.Done():
					return
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if resource := ic.processWatchEvent("MODIFIED", newObj); resource != nil {
				select {
				case eventChan <- resource:
				case <-ctx.Done():
					return
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			if resource := ic.processWatchEvent("DELETED", obj); resource != nil {
				select {
				case eventChan <- resource:
				case <-ctx.Done():
					return
				}
			}
		},
	})

	// Start watching in background
	go func() {
		informer.Run(ctx.Done())
		close(eventChan)
	}()

	return eventChan, nil
}

// processWatchEvent processes a watch event and returns a resource
func (ic *InformerCache) processWatchEvent(eventType string, obj interface{}) *pb.Resource {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		// Handle tombstone
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return nil
		}
		unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return nil
		}
	}

	resource := ic.convertToResource(unstructuredObj, unstructuredObj.GetKind())
	if resource.Tags == nil {
		resource.Tags = make(map[string]string)
	}
	resource.Tags["watch_event"] = eventType
	resource.Tags["watch_time"] = time.Now().Format(time.RFC3339)

	return resource
}

// SyncStatus returns the sync status of all informers
func (ic *InformerCache) SyncStatus() map[string]bool {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	status := make(map[string]bool)
	for key, informer := range ic.resourceInformers {
		status[key] = informer.HasSynced()
	}

	return status
}
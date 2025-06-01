package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// ResourceScanner handles scanning of Kubernetes resources
type ResourceScanner struct {
	clientFactory *ClientFactory
	rateLimiter   *rate.Limiter
	discovery     *APIDiscovery
}

// NewResourceScanner creates a new resource scanner
func NewResourceScanner(clientFactory *ClientFactory, rateLimiter *rate.Limiter) *ResourceScanner {
	return &ResourceScanner{
		clientFactory: clientFactory,
		rateLimiter:   rateLimiter,
	}
}

// NewResourceScannerForCluster creates a scanner for a specific cluster
func NewResourceScannerForCluster(cluster *ClusterConnection) *ResourceScanner {
	clientFactory := &ClientFactory{
		dynamicClient: cluster.DynamicClient,
		clientset:     cluster.Clientset,
	}
	return &ResourceScanner{
		clientFactory: clientFactory,
		rateLimiter:   rate.NewLimiter(rate.Limit(100), 200),
		discovery:     NewAPIDiscovery(cluster.Clientset.Discovery(), cluster.DynamicClient, cluster.Config),
	}
}

// ScanResources scans the specified resource types across namespaces
func (s *ResourceScanner) ScanResources(ctx context.Context, resourceTypes []string, namespaces []string) ([]*pb.Resource, error) {
	var allResources []*pb.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup

	// If "all" is specified, discover all resource types
	if len(resourceTypes) == 1 && resourceTypes[0] == "all" {
		discovered, err := s.discovery.DiscoverAllResources(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to discover resources: %w", err)
		}
		resourceTypes = []string{}
		for _, res := range discovered {
			resourceTypes = append(resourceTypes, res.Name)
		}
	}

	// Scan each resource type
	for _, resourceType := range resourceTypes {
		wg.Add(1)
		go func(resType string) {
			defer wg.Done()

			resources, err := s.scanResourceType(ctx, resType, namespaces)
			if err != nil {
				fmt.Printf("Failed to scan %s: %v\n", resType, err)
				return
			}

			mu.Lock()
			allResources = append(allResources, resources...)
			mu.Unlock()
		}(resourceType)
	}

	wg.Wait()
	return allResources, nil
}

// scanResourceType scans a specific resource type
func (s *ResourceScanner) scanResourceType(ctx context.Context, resourceType string, namespaces []string) ([]*pb.Resource, error) {
	// Wait for rate limiter
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	// Get resource definition
	resourceDef, err := s.getResourceDefinition(ctx, resourceType)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource definition for %s: %w", resourceType, err)
	}

	// Get the dynamic client interface for this resource
	gvr := GetGVR(resourceDef)
	resourceClient := s.clientFactory.dynamicClient.Resource(gvr)

	var resources []*pb.Resource

	if resourceDef.Namespaced {
		// Scan each namespace
		for _, namespace := range namespaces {
			nsResources, err := s.scanNamespacedResource(ctx, resourceClient, namespace, resourceDef)
			if err != nil {
				fmt.Printf("Failed to scan %s in namespace %s: %v\n", resourceType, namespace, err)
				continue
			}
			resources = append(resources, nsResources...)
		}
	} else {
		// Scan cluster-scoped resource
		clusterResources, err := s.scanClusterScopedResource(ctx, resourceClient, resourceDef)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cluster-scoped %s: %w", resourceType, err)
		}
		resources = append(resources, clusterResources...)
	}

	return resources, nil
}

// scanNamespacedResource scans resources in a specific namespace
func (s *ResourceScanner) scanNamespacedResource(ctx context.Context, client dynamic.NamespaceableResourceInterface, namespace string, resourceDef *ResourceDefinition) ([]*pb.Resource, error) {
	list, err := client.Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return s.convertUnstructuredList(list, resourceDef.Kind), nil
}

// scanClusterScopedResource scans cluster-scoped resources
func (s *ResourceScanner) scanClusterScopedResource(ctx context.Context, client dynamic.NamespaceableResourceInterface, resourceDef *ResourceDefinition) ([]*pb.Resource, error) {
	list, err := client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return s.convertUnstructuredList(list, resourceDef.Kind), nil
}

// convertUnstructuredList converts unstructured list to pb.Resource slice
func (s *ResourceScanner) convertUnstructuredList(list *unstructured.UnstructuredList, kind string) []*pb.Resource {
	var resources []*pb.Resource

	for _, item := range list.Items {
		resource := s.convertToResource(&item, kind)
		resources = append(resources, resource)
	}

	return resources
}

// convertToResource converts an unstructured object to pb.Resource
func (s *ResourceScanner) convertToResource(obj *unstructured.Unstructured, kind string) *pb.Resource {
	// Extract metadata
	metadata := obj.Object["metadata"].(map[string]interface{})
	
	// Build resource ID
	namespace := ""
	if ns, ok := metadata["namespace"].(string); ok {
		namespace = ns
	}
	name := metadata["name"].(string)
	resourceID := fmt.Sprintf("default/%s/%s/%s", namespace, kind, name)

	// Extract common fields
	resource := &pb.Resource{
		Provider:     "kubernetes",
		Service:      obj.GetAPIVersion(),
		Type:         kind,
		Id:           resourceID,
		Name:         name,
		Region:       namespace, // Using Region for namespace
		DiscoveredAt: timestamppb.Now(),
		Tags: map[string]string{
			"api_version": obj.GetAPIVersion(),
			"kind":        kind,
		},
	}

	// Extract labels and annotations
	if labels := obj.GetLabels(); labels != nil {
		for k, v := range labels {
			resource.Tags[k] = v
		}
	}

	// Extract additional metadata as tags
	resource.Tags["uid"] = string(obj.GetUID())
	resource.Tags["resourceVersion"] = obj.GetResourceVersion()
	resource.Tags["generation"] = fmt.Sprintf("%d", obj.GetGeneration())
	resource.Tags["creationTimestamp"] = obj.GetCreationTimestamp().Format(time.RFC3339)

	// Add annotations as tags
	if annotations := obj.GetAnnotations(); annotations != nil {
		for k, v := range annotations {
			resource.Tags["annotation."+k] = v
		}
	}

	// Store the full object as raw data (JSON string)
	if rawBytes, err := obj.MarshalJSON(); err == nil {
		resource.RawData = string(rawBytes)
	}

	return resource
}

// GetResourceDetails gets detailed information about a specific resource
func (s *ResourceScanner) GetResourceDetails(ctx context.Context, cluster *ClusterConnection, namespace, kind, name string) (*pb.Resource, error) {
	// Find resource definition
	resourceDef, err := s.getResourceDefinition(ctx, strings.ToLower(kind))
	if err != nil {
		return nil, fmt.Errorf("failed to get resource definition: %w", err)
	}

	// Get the resource
	gvr := GetGVR(resourceDef)
	var obj *unstructured.Unstructured

	if resourceDef.Namespaced {
		obj, err = cluster.DynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = cluster.DynamicClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	return s.convertToResource(obj, kind), nil
}

// StreamScanResources scans resources and streams them through a channel
func (s *ResourceScanner) StreamScanResources(ctx context.Context, resourceTypes []string, resourceChan chan<- *pb.Resource) error {
	defer close(resourceChan)

	// If no specific types requested, scan all
	if len(resourceTypes) == 0 {
		discovered, err := s.discovery.DiscoverAllResources(ctx)
		if err != nil {
			return fmt.Errorf("failed to discover resources: %w", err)
		}
		for _, res := range discovered {
			resourceTypes = append(resourceTypes, res.Name)
		}
	}

	// Scan each resource type
	for _, resourceType := range resourceTypes {
		if err := s.streamResourceType(ctx, resourceType, resourceChan); err != nil {
			fmt.Printf("Failed to stream %s: %v\n", resourceType, err)
			// Continue with other resources
		}
	}

	return nil
}

// streamResourceType streams a specific resource type
func (s *ResourceScanner) streamResourceType(ctx context.Context, resourceType string, resourceChan chan<- *pb.Resource) error {
	// Get resource definition
	resourceDef, err := s.getResourceDefinition(ctx, resourceType)
	if err != nil {
		return err
	}

	// Get the dynamic client interface
	gvr := GetGVR(resourceDef)
	resourceClient := s.clientFactory.dynamicClient.Resource(gvr)

	// List with pagination
	continueToken := ""
	for {
		listOptions := metav1.ListOptions{
			Limit:    100, // Process in chunks
			Continue: continueToken,
		}

		var list *unstructured.UnstructuredList
		if resourceDef.Namespaced {
			list, err = resourceClient.List(ctx, listOptions)
		} else {
			list, err = resourceClient.List(ctx, listOptions)
		}

		if err != nil {
			return err
		}

		// Stream each resource
		for _, item := range list.Items {
			resource := s.convertToResource(&item, resourceDef.Kind)
			select {
			case resourceChan <- resource:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Check if there are more resources
		continueToken = list.GetContinue()
		if continueToken == "" {
			break
		}
	}

	return nil
}

// getResourceDefinition gets the resource definition for a given resource type name
func (s *ResourceScanner) getResourceDefinition(ctx context.Context, resourceType string) (*ResourceDefinition, error) {
	// This would typically use the discovery client to find the resource
	// For now, we'll use a simplified mapping
	
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
		"namespaces": {
			Group:      "",
			Version:    "v1",
			Kind:       "Namespace",
			Name:       "namespaces",
			Namespaced: false,
		},
		"nodes": {
			Group:      "",
			Version:    "v1",
			Kind:       "Node",
			Name:       "nodes",
			Namespaced: false,
		},
		"persistentvolumeclaims": {
			Group:      "",
			Version:    "v1",
			Kind:       "PersistentVolumeClaim",
			Name:       "persistentvolumeclaims",
			Namespaced: true,
		},
		"persistentvolumes": {
			Group:      "",
			Version:    "v1",
			Kind:       "PersistentVolume",
			Name:       "persistentvolumes",
			Namespaced: false,
		},
	}

	if def, ok := commonResources[resourceType]; ok {
		return def, nil
	}

	// For unknown resources, try to discover
	allResources, err := s.discovery.DiscoverAllResources(ctx)
	if err != nil {
		return nil, err
	}

	for _, res := range allResources {
		if res.Name == resourceType {
			return res, nil
		}
	}

	return nil, fmt.Errorf("resource type %s not found", resourceType)
}

// ScanWithLabelSelector scans resources matching label selectors
func (s *ResourceScanner) ScanWithLabelSelector(ctx context.Context, resourceType string, labelSelector string, namespaces []string) ([]*pb.Resource, error) {
	// Get resource definition
	resourceDef, err := s.getResourceDefinition(ctx, resourceType)
	if err != nil {
		return nil, err
	}

	// Get the dynamic client interface
	gvr := GetGVR(resourceDef)
	resourceClient := s.clientFactory.dynamicClient.Resource(gvr)

	var resources []*pb.Resource

	listOptions := metav1.ListOptions{
		LabelSelector: labelSelector,
	}

	if resourceDef.Namespaced {
		for _, namespace := range namespaces {
			list, err := resourceClient.Namespace(namespace).List(ctx, listOptions)
			if err != nil {
				continue
			}
			resources = append(resources, s.convertUnstructuredList(list, resourceDef.Kind)...)
		}
	} else {
		list, err := resourceClient.List(ctx, listOptions)
		if err != nil {
			return nil, err
		}
		resources = append(resources, s.convertUnstructuredList(list, resourceDef.Kind)...)
	}

	return resources, nil
}

// ScanWithFieldSelector scans resources matching field selectors
func (s *ResourceScanner) ScanWithFieldSelector(ctx context.Context, resourceType string, fieldSelector string, namespaces []string) ([]*pb.Resource, error) {
	// Similar to label selector but with field selectors
	resourceDef, err := s.getResourceDefinition(ctx, resourceType)
	if err != nil {
		return nil, err
	}

	gvr := GetGVR(resourceDef)
	resourceClient := s.clientFactory.dynamicClient.Resource(gvr)

	var resources []*pb.Resource

	listOptions := metav1.ListOptions{
		FieldSelector: fieldSelector,
	}

	if resourceDef.Namespaced {
		for _, namespace := range namespaces {
			list, err := resourceClient.Namespace(namespace).List(ctx, listOptions)
			if err != nil {
				continue
			}
			resources = append(resources, s.convertUnstructuredList(list, resourceDef.Kind)...)
		}
	} else {
		list, err := resourceClient.List(ctx, listOptions)
		if err != nil {
			return nil, err
		}
		resources = append(resources, s.convertUnstructuredList(list, resourceDef.Kind)...)
	}

	return resources, nil
}
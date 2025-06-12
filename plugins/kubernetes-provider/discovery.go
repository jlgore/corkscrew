package main

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ResourceDefinition represents a Kubernetes resource type
type ResourceDefinition struct {
	Group      string
	Version    string
	Kind       string
	Name       string // Plural name
	Namespaced bool
	Verbs      []string
	CRD        bool
	Schema     interface{} // OpenAPI schema
}

// APIDiscovery handles discovery of all Kubernetes resources
type APIDiscovery struct {
	discoveryClient discovery.DiscoveryInterface
	dynamicClient   dynamic.Interface
	crdClient       clientset.Interface
	restConfig      *rest.Config
}

// NewAPIDiscovery creates a new API discovery instance
func NewAPIDiscovery(discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface, restConfig *rest.Config) *APIDiscovery {
	return &APIDiscovery{
		discoveryClient: discoveryClient,
		dynamicClient:   dynamicClient,
		restConfig:      restConfig,
	}
}

// DiscoverAllResources discovers all available Kubernetes resources including CRDs
func (d *APIDiscovery) DiscoverAllResources(ctx context.Context) ([]*ResourceDefinition, error) {
	var allResources []*ResourceDefinition

	// 1. Discover core Kubernetes resources
	coreResources, err := d.discoverCoreResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover core resources: %w", err)
	}
	allResources = append(allResources, coreResources...)

	// 2. Discover CRDs
	crdResources, err := d.discoverCRDs(ctx)
	if err != nil {
		// Don't fail if CRDs can't be discovered (might not have permissions)
		fmt.Printf("Warning: failed to discover CRDs: %v\n", err)
	} else {
		allResources = append(allResources, crdResources...)
	}

	return allResources, nil
}

// discoverCoreResources discovers all built-in Kubernetes resources
func (d *APIDiscovery) discoverCoreResources(ctx context.Context) ([]*ResourceDefinition, error) {
	var resources []*ResourceDefinition

	// Get all API groups
	apiGroupList, err := d.discoveryClient.ServerGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to get API groups: %w", err)
	}

	// For each API group, discover resources
	for _, group := range apiGroupList.Groups {
		// Skip deprecated versions
		preferredVersion := group.PreferredVersion
		
		// Get resources for this group version
		apiResourceList, err := d.discoveryClient.ServerResourcesForGroupVersion(preferredVersion.GroupVersion)
		if err != nil {
			// Some resources might not be accessible, continue with others
			fmt.Printf("Warning: failed to get resources for %s: %v\n", preferredVersion.GroupVersion, err)
			continue
		}

		// Parse group and version
		groupName := group.Name
		version := preferredVersion.Version

		// Process each resource
		for _, apiResource := range apiResourceList.APIResources {
			// Skip sub-resources (e.g., pods/log, pods/status)
			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			// Only include resources that can be listed
			if !contains(apiResource.Verbs, "list") {
				continue
			}

			resource := &ResourceDefinition{
				Group:      groupName,
				Version:    version,
				Kind:       apiResource.Kind,
				Name:       apiResource.Name,
				Namespaced: apiResource.Namespaced,
				Verbs:      apiResource.Verbs,
				CRD:        false,
			}

			// Try to get OpenAPI schema
			if d.discoveryClient.OpenAPISchema != nil {
				// This would require OpenAPI v3 client
				// For now, we'll leave schema as nil
			}

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// discoverCRDs discovers all Custom Resource Definitions
func (d *APIDiscovery) discoverCRDs(ctx context.Context) ([]*ResourceDefinition, error) {
	var resources []*ResourceDefinition

	// Create CRD client
	crdClient, err := clientset.NewForConfig(d.restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create CRD client: %w", err)
	}

	// List all CRDs
	crdList, err := crdClient.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list CRDs: %w", err)
	}

	// Process each CRD
	for _, crd := range crdList.Items {
		// For each version of the CRD
		for _, version := range crd.Spec.Versions {
			if !version.Served {
				continue
			}

			resource := &ResourceDefinition{
				Group:      crd.Spec.Group,
				Version:    version.Name,
				Kind:       crd.Spec.Names.Kind,
				Name:       crd.Spec.Names.Plural,
				Namespaced: crd.Spec.Scope == "Namespaced",
				CRD:        true,
				Verbs:      []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			}

			// CRDs include their OpenAPI schema
			if version.Schema != nil && version.Schema.OpenAPIV3Schema != nil {
				resource.Schema = version.Schema.OpenAPIV3Schema
			}

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// DiscoverResourcesByGroup discovers resources for a specific API group
func (d *APIDiscovery) DiscoverResourcesByGroup(ctx context.Context, groupName string) ([]*ResourceDefinition, error) {
	allResources, err := d.DiscoverAllResources(ctx)
	if err != nil {
		return nil, err
	}

	var groupResources []*ResourceDefinition
	for _, resource := range allResources {
		if resource.Group == groupName || (groupName == "core" && resource.Group == "") {
			groupResources = append(groupResources, resource)
		}
	}

	return groupResources, nil
}

// DiscoverResourcesForAPIGroup is an alias for DiscoverResourcesByGroup to match the expected interface
func (d *APIDiscovery) DiscoverResourcesForAPIGroup(ctx context.Context, apiGroup string) ([]*ResourceDefinition, error) {
	return d.DiscoverResourcesByGroup(ctx, apiGroup)
}

// DiscoverNamespacedResources returns only namespaced resources
func (d *APIDiscovery) DiscoverNamespacedResources(ctx context.Context) ([]*ResourceDefinition, error) {
	allResources, err := d.DiscoverAllResources(ctx)
	if err != nil {
		return nil, err
	}

	var namespacedResources []*ResourceDefinition
	for _, resource := range allResources {
		if resource.Namespaced {
			namespacedResources = append(namespacedResources, resource)
		}
	}

	return namespacedResources, nil
}

// GetResourceDefinition gets the definition for a specific resource type
func (d *APIDiscovery) GetResourceDefinition(ctx context.Context, kind string) (*ResourceDefinition, error) {
	allResources, err := d.DiscoverAllResources(ctx)
	if err != nil {
		return nil, err
	}

	for _, resource := range allResources {
		if strings.EqualFold(resource.Kind, kind) {
			return resource, nil
		}
	}

	return nil, fmt.Errorf("resource kind %s not found", kind)
}

// GetGVR returns the GroupVersionResource for a resource definition
func GetGVR(resource *ResourceDefinition) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    resource.Group,
		Version:  resource.Version,
		Resource: resource.Name,
	}
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// DiscoverHelmReleases discovers Helm releases in the cluster
func (d *APIDiscovery) DiscoverHelmReleases(ctx context.Context) ([]*SimpleHelmRelease, error) {
	var releases []*SimpleHelmRelease

	// Helm 3 stores releases as secrets with specific labels
	secretGVR := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}

	// List secrets in all namespaces
	secretList, err := d.dynamicClient.Resource(secretGVR).List(ctx, metav1.ListOptions{
		LabelSelector: "owner=helm",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Helm secrets: %w", err)
	}

	// Process each Helm release secret
	for _, item := range secretList.Items {
		release := &SimpleHelmRelease{
			Name:      item.GetLabels()["name"],
			Namespace: item.GetNamespace(),
			Version:   item.GetLabels()["version"],
			Status:    item.GetLabels()["status"],
		}

		// Decode release data if needed
		// This would involve base64 decoding and ungzipping the release data

		releases = append(releases, release)
	}

	return releases, nil
}

// SimpleHelmRelease represents a simple Helm release (to avoid conflict with helm_discovery.go)
type SimpleHelmRelease struct {
	Name      string
	Namespace string
	Chart     string
	Version   string
	Status    string
	Resources []unstructured.Unstructured
}

// DiscoverOperators discovers installed operators in the cluster
func (d *APIDiscovery) DiscoverOperators(ctx context.Context) ([]*Operator, error) {
	var operators []*Operator

	// Check for OLM (Operator Lifecycle Manager) operators
	olmGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	csvList, err := d.dynamicClient.Resource(olmGVR).List(ctx, metav1.ListOptions{})
	if err == nil {
		for _, item := range csvList.Items {
			operator := &Operator{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
				Type:      "OLM",
			}
			operators = append(operators, operator)
		}
	}

	// Check for other common operator patterns
	// This could be extended to detect various operator frameworks

	return operators, nil
}

// Operator represents an installed operator
type Operator struct {
	Name      string
	Namespace string
	Type      string // OLM, Helm, Custom
	CRDs      []string
}
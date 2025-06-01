package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"compress/gzip"
	"io"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// HelmDiscovery handles discovery of Helm releases
type HelmDiscovery struct {
	clientFactory *ClientFactory
}

// NewHelmDiscovery creates a new Helm discovery instance
func NewHelmDiscovery(clientFactory *ClientFactory) *HelmDiscovery {
	return &HelmDiscovery{
		clientFactory: clientFactory,
	}
}

// HelmRelease represents a Helm release with detailed information
type HelmRelease struct {
	Name         string                         `json:"name"`
	Namespace    string                         `json:"namespace"`
	Revision     int                            `json:"revision"`
	Updated      string                         `json:"updated"`
	Status       string                         `json:"status"`
	Chart        string                         `json:"chart"`
	ChartVersion string                         `json:"chart_version"`
	AppVersion   string                         `json:"app_version"`
	Values       map[string]interface{}         `json:"values"`
	Manifest     string                         `json:"manifest"`
	Resources    []unstructured.Unstructured    `json:"resources"`
}

// DiscoverHelmReleases discovers all Helm releases in the cluster
func (h *HelmDiscovery) DiscoverHelmReleases(ctx context.Context) ([]*HelmRelease, error) {
	var releases []*HelmRelease

	// Helm 3 stores releases as secrets with specific labels
	secretGVR := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}

	// List secrets with Helm labels
	secretList, err := h.clientFactory.GetDynamicClient().Resource(secretGVR).List(ctx, metav1.ListOptions{
		LabelSelector: "owner=helm",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Helm secrets: %w", err)
	}

	// Process each Helm release secret
	for _, item := range secretList.Items {
		release, err := h.parseHelmSecret(&item)
		if err != nil {
			fmt.Printf("Failed to parse Helm secret %s: %v\n", item.GetName(), err)
			continue
		}
		
		// Parse resources from manifest
		release.Resources = h.parseManifestResources(release.Manifest)
		
		releases = append(releases, release)
	}

	// Also check for Helm 2 releases (ConfigMaps)
	helm2Releases, err := h.discoverHelm2Releases(ctx)
	if err == nil {
		releases = append(releases, helm2Releases...)
	}

	return releases, nil
}

// parseHelmSecret parses a Helm 3 release secret
func (h *HelmDiscovery) parseHelmSecret(secret *unstructured.Unstructured) (*HelmRelease, error) {
	// Extract data from secret
	encodedData, found, err := unstructured.NestedString(secret.Object, "data", "release")
	if !found || err != nil {
		return nil, fmt.Errorf("release data not found in secret")
	}

	decodedData, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	// Decompress gzip
	reader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

	uncompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}

	// Parse JSON
	var releaseData map[string]interface{}
	if err := json.Unmarshal(uncompressed, &releaseData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal release data: %w", err)
	}

	// Extract release information
	release := &HelmRelease{
		Name:      secret.GetLabels()["name"],
		Namespace: secret.GetNamespace(),
		Status:    secret.GetLabels()["status"],
	}

	// Extract version from labels
	if version, ok := secret.GetLabels()["version"]; ok {
		fmt.Sscanf(version, "%d", &release.Revision)
	}

	// Extract chart information
	if chart, ok := releaseData["chart"].(map[string]interface{}); ok {
		if metadata, ok := chart["metadata"].(map[string]interface{}); ok {
			release.Chart = metadata["name"].(string)
			release.ChartVersion = metadata["version"].(string)
			release.AppVersion, _ = metadata["appVersion"].(string)
		}
	}

	// Extract values
	if values, ok := releaseData["values"].(map[string]interface{}); ok {
		release.Values = values
	}

	// Extract manifest
	if manifest, ok := releaseData["manifest"].(string); ok {
		release.Manifest = manifest
	}

	return release, nil
}

// discoverHelm2Releases discovers Helm 2 releases stored as ConfigMaps
func (h *HelmDiscovery) discoverHelm2Releases(ctx context.Context) ([]*HelmRelease, error) {
	var releases []*HelmRelease

	// Helm 2 stores releases as ConfigMaps in kube-system
	cmGVR := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "configmaps",
	}

	// List ConfigMaps with Helm 2 labels
	cmList, err := h.clientFactory.GetDynamicClient().Resource(cmGVR).Namespace("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "OWNER=TILLER",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Helm 2 ConfigMaps: %w", err)
	}

	// Process each Helm 2 release
	for _, item := range cmList.Items {
		// Helm 2 parsing would be implemented here
		// For now, just create a basic release object
		release := &HelmRelease{
			Name:      item.GetLabels()["NAME"],
			Namespace: item.GetLabels()["NAMESPACE"],
			Status:    item.GetLabels()["STATUS"],
			Chart:     item.GetLabels()["CHART"],
		}
		releases = append(releases, release)
	}

	return releases, nil
}

// parseManifestResources parses Kubernetes resources from a Helm manifest
func (h *HelmDiscovery) parseManifestResources(manifest string) []unstructured.Unstructured {
	var resources []unstructured.Unstructured

	// Split manifest by YAML document separator
	docs := strings.Split(manifest, "\n---\n")

	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Parse YAML to unstructured
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON([]byte(doc)); err != nil {
			// Try YAML parsing if JSON fails
			// This would require a YAML parser
			continue
		}

		resources = append(resources, *obj)
	}

	return resources
}

// ConvertHelmReleasesToResources converts Helm releases to Corkscrew resources
func (h *HelmDiscovery) ConvertHelmReleasesToResources(releases []*HelmRelease) []*pb.Resource {
	var resources []*pb.Resource

	for _, release := range releases {
		resource := &pb.Resource{
			Provider:     "kubernetes",
			Service:      "helm",
			Type:         "HelmRelease",
			Id:           fmt.Sprintf("helm/%s/%s", release.Namespace, release.Name),
			Name:         release.Name,
			Region:       release.Namespace, // Using Region for namespace
			DiscoveredAt: timestamppb.Now(),
			Tags: map[string]string{
				"chart":         release.Chart,
				"chart_version": release.ChartVersion,
				"app_version":   release.AppVersion,
				"status":        release.Status,
				"revision":      fmt.Sprintf("%d", release.Revision),
				"updated":       release.Updated,
				"resource_count": fmt.Sprintf("%d", len(release.Resources)),
				"api_version":   "helm.sh/v3",
				"kind":          "HelmRelease",
			},
		}

		resources = append(resources, resource)
	}

	return resources
}

// GetHelmReleaseResources returns all Kubernetes resources managed by a Helm release
func (h *HelmDiscovery) GetHelmReleaseResources(ctx context.Context, releaseName, namespace string) ([]*pb.Resource, error) {
	// Find the Helm release
	releases, err := h.DiscoverHelmReleases(ctx)
	if err != nil {
		return nil, err
	}

	var targetRelease *HelmRelease
	for _, release := range releases {
		if release.Name == releaseName && release.Namespace == namespace {
			targetRelease = release
			break
		}
	}

	if targetRelease == nil {
		return nil, fmt.Errorf("Helm release %s not found in namespace %s", releaseName, namespace)
	}

	// Get all resources with Helm labels
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/managed-by": "Helm",
		"app.kubernetes.io/instance":   releaseName,
	})

	// Scan all resource types for matching labels
	var resources []*pb.Resource

	// This would scan various resource types
	// For brevity, showing just a few common ones
	resourceTypes := []schema.GroupVersionResource{
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "apps", Version: "v1", Resource: "statefulsets"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	}

	for _, gvr := range resourceTypes {
		list, err := h.clientFactory.GetDynamicClient().Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector.String(),
		})
		if err != nil {
			continue
		}

		for _, item := range list.Items {
			resource := h.convertToHelmManagedResource(&item, releaseName)
			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// convertToHelmManagedResource converts a Kubernetes resource to a Helm-managed resource
func (h *HelmDiscovery) convertToHelmManagedResource(obj *unstructured.Unstructured, releaseName string) *pb.Resource {
	namespace := obj.GetNamespace()
	resourceID := fmt.Sprintf("helm-managed/%s/%s/%s", namespace, obj.GetKind(), obj.GetName())

	resource := &pb.Resource{
		Provider:     "kubernetes",
		Service:      obj.GetAPIVersion(),
		Type:         obj.GetKind(),
		Id:           resourceID,
		Name:         obj.GetName(),
		Region:       namespace, // Using Region for namespace
		DiscoveredAt: timestamppb.Now(),
		Tags: func() map[string]string {
			tags := obj.GetLabels()
			if tags == nil {
				tags = make(map[string]string)
			}
			tags["uid"] = string(obj.GetUID())
			tags["resourceVersion"] = obj.GetResourceVersion()
			tags["helm_release"] = releaseName
			tags["helm_managed"] = "true"
			tags["api_version"] = obj.GetAPIVersion()
			tags["kind"] = obj.GetKind()
			return tags
		}(),
	}

	// Add Helm-specific labels
	if helmChart, ok := obj.GetLabels()["helm.sh/chart"]; ok {
		resource.Tags["helm_chart"] = helmChart
	}

	return resource
}

// AnalyzeHelmRelease analyzes a Helm release for issues
func (h *HelmDiscovery) AnalyzeHelmRelease(release *HelmRelease) map[string]interface{} {
	analysis := map[string]interface{}{
		"name":      release.Name,
		"namespace": release.Namespace,
		"issues":    []string{},
		"warnings":  []string{},
		"info":      []string{},
	}

	issues := []string{}
	warnings := []string{}
	info := []string{}

	// Check release status
	if release.Status != "deployed" {
		issues = append(issues, fmt.Sprintf("Release status is %s", release.Status))
	}

	// Check for outdated chart version
	// This would require checking against a chart repository
	info = append(info, fmt.Sprintf("Chart: %s version %s", release.Chart, release.ChartVersion))

	// Analyze resources
	resourceCount := len(release.Resources)
	info = append(info, fmt.Sprintf("Managing %d resources", resourceCount))

	// Check for common issues in values
	if release.Values != nil {
		// Check for missing required values
		// Check for insecure configurations
		// etc.
	}

	analysis["issues"] = issues
	analysis["warnings"] = warnings
	analysis["info"] = info
	analysis["resource_count"] = resourceCount

	return analysis
}
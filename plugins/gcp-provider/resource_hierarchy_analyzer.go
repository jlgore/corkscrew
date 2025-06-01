package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ResourceHierarchyAnalyzer analyzes GCP resource hierarchies and parent relationships
type ResourceHierarchyAnalyzer struct {
	serviceMappings map[string]*ServiceMapping
	hierarchyRules  map[string]*HierarchyRule
}

// HierarchyRule defines parent-child relationships and requirements
type HierarchyRule struct {
	ChildResource    string              `json:"child_resource"`
	ParentResource   string              `json:"parent_resource"`
	ParentRequired   bool                `json:"parent_required"`
	Scope           string              `json:"scope"`
	PathPattern     string              `json:"path_pattern"`
	RequiredParams  []string            `json:"required_params"`
	Examples        []string            `json:"examples"`
	Metadata        map[string]string   `json:"metadata"`
}

// ResourceNamePattern represents GCP resource name pattern analysis
type ResourceNamePattern struct {
	FullPattern     string            `json:"full_pattern"`
	Components      []string          `json:"components"`
	ParentPattern   string            `json:"parent_pattern"`
	ResourcePattern string            `json:"resource_pattern"`
	Scope          string            `json:"scope"`
	Examples       []string          `json:"examples"`
}

// NewResourceHierarchyAnalyzer creates a new hierarchy analyzer
func NewResourceHierarchyAnalyzer(serviceMappings map[string]*ServiceMapping) *ResourceHierarchyAnalyzer {
	rha := &ResourceHierarchyAnalyzer{
		serviceMappings: serviceMappings,
		hierarchyRules:  make(map[string]*HierarchyRule),
	}
	
	// Initialize built-in hierarchy rules
	rha.initializeHierarchyRules()
	
	return rha
}

// AnalyzeResourceHierarchies analyzes all resource hierarchies across services
func (rha *ResourceHierarchyAnalyzer) AnalyzeResourceHierarchies(ctx context.Context) error {
	log.Printf("Starting resource hierarchy analysis...")

	// Analyze each service's resource patterns
	for serviceName, mapping := range rha.serviceMappings {
		if err := rha.analyzeServiceHierarchy(serviceName, mapping); err != nil {
			log.Printf("Error analyzing hierarchy for service %s: %v", serviceName, err)
			continue
		}
	}

	// Analyze cross-service relationships
	rha.analyzeCrossServiceRelationships()

	log.Printf("Completed hierarchy analysis for %d services", len(rha.serviceMappings))
	return nil
}

// analyzeServiceHierarchy analyzes hierarchy within a specific service
func (rha *ResourceHierarchyAnalyzer) analyzeServiceHierarchy(serviceName string, mapping *ServiceMapping) error {
	log.Printf("Analyzing hierarchy for service: %s", serviceName)

	for _, pattern := range mapping.ResourcePatterns {
		// Analyze resource name patterns to detect parent relationships
		namePattern := rha.analyzeResourceNamePattern(serviceName, pattern)
		
		// Create parent relationship if detected
		if parentRel := rha.extractParentRelationship(namePattern, pattern); parentRel != nil {
			mapping.ParentHierarchy = append(mapping.ParentHierarchy, parentRel)
			
			// Store as hierarchy rule
			key := fmt.Sprintf("%s.%s", serviceName, pattern.ResourceType)
			rha.hierarchyRules[key] = &HierarchyRule{
				ChildResource:  pattern.ResourceType,
				ParentResource: parentRel.ParentResource,
				ParentRequired: true,
				Scope:         parentRel.Scope,
				RequiredParams: parentRel.RequiredParams,
				Metadata:      pattern.Metadata,
			}
		}
	}

	return nil
}

// analyzeResourceNamePattern analyzes GCP resource name patterns
func (rha *ResourceHierarchyAnalyzer) analyzeResourceNamePattern(serviceName string, pattern *ResourcePattern) *ResourceNamePattern {
	// GCP resource names follow specific patterns:
	// - projects/{project}/regions/{region}/instances/{instance}
	// - projects/{project}/zones/{zone}/disks/{disk}
	// - projects/{project}/buckets/{bucket}
	// - organizations/{org}/folders/{folder}

	namePattern := &ResourceNamePattern{
		Components: []string{},
		Examples:   []string{},
		Scope:     rha.detectScope(serviceName, pattern),
	}

	// Build pattern based on service and resource type
	switch serviceName {
	case "compute":
		namePattern = rha.analyzeComputeResourcePattern(pattern)
	case "storage":
		namePattern = rha.analyzeStorageResourcePattern(pattern)
	case "container":
		namePattern = rha.analyzeContainerResourcePattern(pattern)
	case "bigquery":
		namePattern = rha.analyzeBigQueryResourcePattern(pattern)
	case "pubsub":
		namePattern = rha.analyzePubSubResourcePattern(pattern)
	default:
		namePattern = rha.analyzeGenericResourcePattern(serviceName, pattern)
	}

	return namePattern
}

// analyzeComputeResourcePattern analyzes Compute Engine resource patterns
func (rha *ResourceHierarchyAnalyzer) analyzeComputeResourcePattern(pattern *ResourcePattern) *ResourceNamePattern {
	switch pattern.ResourceType {
	case "Instance":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/zones/{zone}/instances/{instance}",
			Components:      []string{"projects", "zones", "instances"},
			ParentPattern:   "projects/{project}/zones/{zone}",
			ResourcePattern: "{instance}",
			Scope:          "zonal",
			Examples:       []string{"projects/my-project/zones/us-central1-a/instances/my-instance"},
		}
	case "Disk":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/zones/{zone}/disks/{disk}",
			Components:      []string{"projects", "zones", "disks"},
			ParentPattern:   "projects/{project}/zones/{zone}",
			ResourcePattern: "{disk}",
			Scope:          "zonal",
			Examples:       []string{"projects/my-project/zones/us-central1-a/disks/my-disk"},
		}
	case "Network":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/global/networks/{network}",
			Components:      []string{"projects", "global", "networks"},
			ParentPattern:   "projects/{project}",
			ResourcePattern: "{network}",
			Scope:          "global",
			Examples:       []string{"projects/my-project/global/networks/default"},
		}
	case "Subnetwork":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/regions/{region}/subnetworks/{subnetwork}",
			Components:      []string{"projects", "regions", "subnetworks"},
			ParentPattern:   "projects/{project}/regions/{region}",
			ResourcePattern: "{subnetwork}",
			Scope:          "regional",
			Examples:       []string{"projects/my-project/regions/us-central1/subnetworks/default"},
		}
	default:
		return &ResourceNamePattern{
			FullPattern: fmt.Sprintf("projects/{project}/zones/{zone}/%ss/{%s}", 
				strings.ToLower(pattern.ResourceType), strings.ToLower(pattern.ResourceType)),
			Scope: "zonal",
		}
	}
}

// analyzeStorageResourcePattern analyzes Cloud Storage resource patterns
func (rha *ResourceHierarchyAnalyzer) analyzeStorageResourcePattern(pattern *ResourcePattern) *ResourceNamePattern {
	switch pattern.ResourceType {
	case "Bucket":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/buckets/{bucket}",
			Components:      []string{"projects", "buckets"},
			ParentPattern:   "projects/{project}",
			ResourcePattern: "{bucket}",
			Scope:          "global",
			Examples:       []string{"projects/my-project/buckets/my-bucket"},
		}
	case "Object":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/buckets/{bucket}/objects/{object}",
			Components:      []string{"projects", "buckets", "objects"},
			ParentPattern:   "projects/{project}/buckets/{bucket}",
			ResourcePattern: "{object}",
			Scope:          "bucket",
			Examples:       []string{"projects/my-project/buckets/my-bucket/objects/path/to/file.txt"},
		}
	default:
		return &ResourceNamePattern{
			FullPattern: fmt.Sprintf("projects/{project}/buckets/{bucket}/%ss/{%s}", 
				strings.ToLower(pattern.ResourceType), strings.ToLower(pattern.ResourceType)),
			Scope: "bucket",
		}
	}
}

// analyzeContainerResourcePattern analyzes Kubernetes Engine resource patterns  
func (rha *ResourceHierarchyAnalyzer) analyzeContainerResourcePattern(pattern *ResourcePattern) *ResourceNamePattern {
	switch pattern.ResourceType {
	case "Cluster":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/locations/{location}/clusters/{cluster}",
			Components:      []string{"projects", "locations", "clusters"},
			ParentPattern:   "projects/{project}/locations/{location}",
			ResourcePattern: "{cluster}",
			Scope:          "regional",
			Examples:       []string{"projects/my-project/locations/us-central1/clusters/my-cluster"},
		}
	case "NodePool":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/locations/{location}/clusters/{cluster}/nodePools/{nodepool}",
			Components:      []string{"projects", "locations", "clusters", "nodePools"},
			ParentPattern:   "projects/{project}/locations/{location}/clusters/{cluster}",
			ResourcePattern: "{nodepool}",
			Scope:          "cluster",
			Examples:       []string{"projects/my-project/locations/us-central1/clusters/my-cluster/nodePools/default-pool"},
		}
	default:
		return &ResourceNamePattern{
			FullPattern: fmt.Sprintf("projects/{project}/locations/{location}/clusters/{cluster}/%ss/{%s}",
				strings.ToLower(pattern.ResourceType), strings.ToLower(pattern.ResourceType)),
			Scope: "cluster",
		}
	}
}

// analyzeBigQueryResourcePattern analyzes BigQuery resource patterns
func (rha *ResourceHierarchyAnalyzer) analyzeBigQueryResourcePattern(pattern *ResourcePattern) *ResourceNamePattern {
	switch pattern.ResourceType {
	case "Dataset":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/datasets/{dataset}",
			Components:      []string{"projects", "datasets"},
			ParentPattern:   "projects/{project}",
			ResourcePattern: "{dataset}",
			Scope:          "project",
			Examples:       []string{"projects/my-project/datasets/my_dataset"},
		}
	case "Table":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/datasets/{dataset}/tables/{table}",
			Components:      []string{"projects", "datasets", "tables"},
			ParentPattern:   "projects/{project}/datasets/{dataset}",
			ResourcePattern: "{table}",
			Scope:          "dataset",
			Examples:       []string{"projects/my-project/datasets/my_dataset/tables/my_table"},
		}
	default:
		return &ResourceNamePattern{
			FullPattern: fmt.Sprintf("projects/{project}/datasets/{dataset}/%ss/{%s}",
				strings.ToLower(pattern.ResourceType), strings.ToLower(pattern.ResourceType)),
			Scope: "dataset",
		}
	}
}

// analyzePubSubResourcePattern analyzes Pub/Sub resource patterns
func (rha *ResourceHierarchyAnalyzer) analyzePubSubResourcePattern(pattern *ResourcePattern) *ResourceNamePattern {
	switch pattern.ResourceType {
	case "Topic":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/topics/{topic}",
			Components:      []string{"projects", "topics"},
			ParentPattern:   "projects/{project}",
			ResourcePattern: "{topic}",
			Scope:          "project",
			Examples:       []string{"projects/my-project/topics/my-topic"},
		}
	case "Subscription":
		return &ResourceNamePattern{
			FullPattern:     "projects/{project}/subscriptions/{subscription}",
			Components:      []string{"projects", "subscriptions"},
			ParentPattern:   "projects/{project}",
			ResourcePattern: "{subscription}",
			Scope:          "project",
			Examples:       []string{"projects/my-project/subscriptions/my-subscription"},
		}
	default:
		return &ResourceNamePattern{
			FullPattern: fmt.Sprintf("projects/{project}/%ss/{%s}",
				strings.ToLower(pattern.ResourceType), strings.ToLower(pattern.ResourceType)),
			Scope: "project",
		}
	}
}

// analyzeGenericResourcePattern provides fallback generic analysis
func (rha *ResourceHierarchyAnalyzer) analyzeGenericResourcePattern(serviceName string, pattern *ResourcePattern) *ResourceNamePattern {
	resourceType := strings.ToLower(pattern.ResourceType)
	
	// Most GCP resources are project-scoped
	return &ResourceNamePattern{
		FullPattern:     fmt.Sprintf("projects/{project}/%ss/{%s}", resourceType, resourceType),
		Components:      []string{"projects", resourceType + "s"},
		ParentPattern:   "projects/{project}",
		ResourcePattern: fmt.Sprintf("{%s}", resourceType),
		Scope:          "project",
		Examples:       []string{fmt.Sprintf("projects/my-project/%ss/my-%s", resourceType, resourceType)},
	}
}

// extractParentRelationship extracts parent relationship from name pattern
func (rha *ResourceHierarchyAnalyzer) extractParentRelationship(namePattern *ResourceNamePattern, resourcePattern *ResourcePattern) *ParentRelationship {
	if namePattern.ParentPattern == "" {
		return nil
	}

	// Extract parent resource type from parent pattern
	parentResource := rha.extractParentResourceType(namePattern.ParentPattern, namePattern.Scope)
	if parentResource == "" {
		return nil
	}

	// Extract required parameters
	requiredParams := rha.extractRequiredParameters(namePattern.FullPattern)

	return &ParentRelationship{
		ChildResource:  resourcePattern.ResourceType,
		ParentResource: parentResource,
		RequiredParams: requiredParams,
		Scope:         namePattern.Scope,
	}
}

// extractParentResourceType extracts parent resource type from pattern
func (rha *ResourceHierarchyAnalyzer) extractParentResourceType(parentPattern, scope string) string {
	switch scope {
	case "zonal":
		if strings.Contains(parentPattern, "/zones/") {
			return "Zone"
		}
	case "regional":
		if strings.Contains(parentPattern, "/regions/") {
			return "Region"
		}
		if strings.Contains(parentPattern, "/locations/") {
			return "Location"
		}
	case "global":
		return "Project"
	case "project":
		return "Project"
	case "cluster":
		return "Cluster"
	case "dataset":
		return "Dataset"
	case "bucket":
		return "Bucket"
	}

	// Fallback to project
	if strings.Contains(parentPattern, "projects/") {
		return "Project"
	}

	return ""
}

// extractRequiredParameters extracts required parameters from resource pattern
func (rha *ResourceHierarchyAnalyzer) extractRequiredParameters(pattern string) []string {
	var params []string
	
	// Find all {param} patterns
	re := regexp.MustCompile(`\{([^}]+)\}`)
	matches := re.FindAllStringSubmatch(pattern, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			params = append(params, match[1])
		}
	}
	
	return params
}

// detectScope detects the scope of a resource
func (rha *ResourceHierarchyAnalyzer) detectScope(serviceName string, pattern *ResourcePattern) string {
	// Use metadata if available
	if scope, ok := pattern.Metadata["scope"]; ok {
		return scope
	}

	// Detect from parameters
	for _, param := range pattern.Parameters {
		switch param {
		case "zone":
			return "zonal"
		case "region":
			return "regional"
		case "location":
			return "regional"
		case "cluster":
			return "cluster"
		case "dataset":
			return "dataset"
		case "bucket":
			return "bucket"
		}
	}

	// Service-specific defaults
	switch serviceName {
	case "compute":
		if strings.Contains(strings.ToLower(pattern.ResourceType), "network") {
			return "global"
		}
		return "zonal"
	case "storage":
		if pattern.ResourceType == "Bucket" {
			return "global"
		}
		return "bucket"
	case "container":
		if pattern.ResourceType == "Cluster" {
			return "regional"
		}
		return "cluster"
	default:
		return "project"
	}
}

// analyzeCrossServiceRelationships analyzes relationships across services
func (rha *ResourceHierarchyAnalyzer) analyzeCrossServiceRelationships() {
	log.Printf("Analyzing cross-service relationships...")

	// Common cross-service relationships in GCP:
	// - Compute instances use VPC networks
	// - Compute instances use Cloud Storage buckets
	// - GKE clusters use Compute networks
	// - BigQuery datasets access Cloud Storage buckets
	// - Pub/Sub topics deliver to Cloud Storage buckets

	crossServiceRules := []*HierarchyRule{
		{
			ChildResource:  "compute.Instance",
			ParentResource: "compute.Network",
			ParentRequired: true,
			Scope:         "network",
			Metadata:      map[string]string{"relationship_type": "uses"},
		},
		{
			ChildResource:  "compute.Instance", 
			ParentResource: "storage.Bucket",
			ParentRequired: false,
			Scope:         "access",
			Metadata:      map[string]string{"relationship_type": "accesses"},
		},
		{
			ChildResource:  "container.Cluster",
			ParentResource: "compute.Network",
			ParentRequired: true,
			Scope:         "network",
			Metadata:      map[string]string{"relationship_type": "uses"},
		},
		{
			ChildResource:  "container.NodePool",
			ParentResource: "container.Cluster",
			ParentRequired: true,
			Scope:         "cluster",
			Metadata:      map[string]string{"relationship_type": "belongs_to"},
		},
	}

	for _, rule := range crossServiceRules {
		key := fmt.Sprintf("cross_service.%s", rule.ChildResource)
		rha.hierarchyRules[key] = rule
	}
}

// initializeHierarchyRules initializes built-in hierarchy rules
func (rha *ResourceHierarchyAnalyzer) initializeHierarchyRules() {
	// Core GCP resource hierarchy rules
	rha.hierarchyRules = map[string]*HierarchyRule{
		"organization": {
			ChildResource:  "Organization",
			ParentResource: "",
			ParentRequired: false,
			Scope:         "global",
			PathPattern:   "organizations/{organization}",
			RequiredParams: []string{"organization"},
		},
		"folder": {
			ChildResource:  "Folder",
			ParentResource: "Organization",
			ParentRequired: false,
			Scope:         "organizational",
			PathPattern:   "folders/{folder}",
			RequiredParams: []string{"folder"},
		},
		"project": {
			ChildResource:  "Project",
			ParentResource: "Folder",
			ParentRequired: false,
			Scope:         "organizational",
			PathPattern:   "projects/{project}",
			RequiredParams: []string{"project"},
		},
	}
}

// GenerateHierarchyRelationships generates relationship data for the orchestrator
func (rha *ResourceHierarchyAnalyzer) GenerateHierarchyRelationships() []*pb.Relationship {
	var relationships []*pb.Relationship

	for _, rule := range rha.hierarchyRules {
		if rule.ParentResource == "" {
			continue // Skip root resources
		}

		relationship := &pb.Relationship{
			TargetId:         rule.ParentResource,
			RelationshipType: "parent_of",
			Properties: map[string]string{
				"child_resource":  rule.ChildResource,
				"scope":          rule.Scope,
				"parent_required": fmt.Sprintf("%t", rule.ParentRequired),
			},
		}

		// Add required parameters
		if len(rule.RequiredParams) > 0 {
			relationship.Properties["required_params"] = strings.Join(rule.RequiredParams, ",")
		}

		// Add metadata
		for k, v := range rule.Metadata {
			relationship.Properties[k] = v
		}

		relationships = append(relationships, relationship)
	}

	return relationships
}

// GetHierarchyRule gets hierarchy rule for a specific resource
func (rha *ResourceHierarchyAnalyzer) GetHierarchyRule(serviceName, resourceType string) *HierarchyRule {
	key := fmt.Sprintf("%s.%s", serviceName, resourceType)
	return rha.hierarchyRules[key]
}

// ValidateResourceHierarchy validates if a resource hierarchy is valid
func (rha *ResourceHierarchyAnalyzer) ValidateResourceHierarchy(resourcePath string) (bool, error) {
	// Parse the resource path and validate against hierarchy rules
	components := strings.Split(strings.Trim(resourcePath, "/"), "/")
	
	if len(components) < 2 {
		return false, fmt.Errorf("invalid resource path: %s", resourcePath)
	}

	// Extract service and resource type from path
	// This is a simplified validation - real implementation would be more complex
	return true, nil
}

// GetResourceParent determines the parent resource for a given resource
func (rha *ResourceHierarchyAnalyzer) GetResourceParent(serviceName, resourceType, resourcePath string) (string, error) {
	rule := rha.GetHierarchyRule(serviceName, resourceType)
	if rule == nil {
		return "", fmt.Errorf("no hierarchy rule found for %s.%s", serviceName, resourceType)
	}

	// Extract parent from resource path based on the rule
	if rule.PathPattern != "" {
		// Use path pattern to extract parent
		// This would involve more complex parsing in a real implementation
		return rule.ParentResource, nil
	}

	return "", fmt.Errorf("unable to determine parent for %s", resourcePath)
}
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/asset/apiv1/assetpb"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Page sizes requested from Cloud Asset Inventory; each is the API's documented maximum.
const (
	assetListPageSize   = 1000
	assetSearchPageSize = 500
)

// AssetInventoryClient provides efficient resource querying using Cloud Asset Inventory
type AssetInventoryClient struct {
	client     *asset.Client
	projectIDs []string // Multiple projects support
	orgID      string
	folderID   string
	scope      string // "projects", "folders", or "organizations"
}

// AssetChange represents a change to an asset
type AssetChange struct {
	Timestamp  time.Time
	ChangeType string
	Asset      *assetpb.Asset
}

// IAMPolicy represents a GCP IAM policy
type IAMPolicy struct {
	Bindings []IAMBinding `json:"bindings"`
	Version  int          `json:"version"`
}

// IAMBinding represents an IAM policy binding
type IAMBinding struct {
	Role    string   `json:"role"`
	Members []string `json:"members"`
}

// NewAssetInventoryClient creates a new Cloud Asset Inventory client
func NewAssetInventoryClient(ctx context.Context, opts ...option.ClientOption) (*AssetInventoryClient, error) {
	client, err := asset.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create asset client: %w", err)
	}

	return &AssetInventoryClient{
		client: client,
	}, nil
}

// Close releases the client's gRPC connection. It is idempotent.
func (aic *AssetInventoryClient) Close() error {
	if aic.client == nil {
		return nil
	}
	err := aic.client.Close()
	aic.client = nil
	return err
}

// SetScope configures the scanning scope
func (aic *AssetInventoryClient) SetScope(scope string, projectIDs []string, orgID, folderID string) {
	aic.scope = scope
	aic.projectIDs = projectIDs
	aic.orgID = orgID
	aic.folderID = folderID
}

// QueryAllAssets queries all assets across the configured scope. A failed project or page does
// not stop the others: the assets that were listed are returned together with the joined error,
// so callers keep what they observed and still report the scope as not fully observed.
func (aic *AssetInventoryClient) QueryAllAssets(ctx context.Context) ([]*pb.ResourceRef, error) {
	switch aic.scope {
	case "organizations":
		if aic.orgID == "" {
			return nil, fmt.Errorf("organization ID required for organization scope")
		}
		return aic.queryAssetsForParent(ctx, fmt.Sprintf("organizations/%s", aic.orgID))
	case "folders":
		if aic.folderID == "" {
			return nil, fmt.Errorf("folder ID required for folder scope")
		}
		return aic.queryAssetsForParent(ctx, fmt.Sprintf("folders/%s", aic.folderID))
	case "projects":
		var allResources []*pb.ResourceRef
		var errs []error
		for _, projectID := range aic.projectIDs {
			resources, err := aic.queryAssetsForParent(ctx, fmt.Sprintf("projects/%s", projectID))
			allResources = append(allResources, resources...)
			if err != nil {
				errs = append(errs, fmt.Errorf("project %s: %w", projectID, err))
			}
		}
		return allResources, errors.Join(errs...)
	}
	return nil, nil
}

// queryAssetsForParent queries assets for a specific parent (project/folder/org). It returns the
// assets listed before any failure along with the error.
func (aic *AssetInventoryClient) queryAssetsForParent(ctx context.Context, parent string) ([]*pb.ResourceRef, error) {
	req := &assetpb.ListAssetsRequest{
		Parent:      parent,
		ContentType: assetpb.ContentType_RESOURCE,
	}
	return aic.listAssets(ctx, req)
}

// listAssets runs one ListAssets request to completion, keeping the assets from pages that
// arrived before a failure.
func (aic *AssetInventoryClient) listAssets(
	ctx context.Context, req *assetpb.ListAssetsRequest,
) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	label := "list assets for " + req.Parent
	err := listPages(label, aic.client.ListAssets(ctx, req), assetListPageSize,
		func(asset *assetpb.Asset) {
			if resource := aic.convertAssetToResourceRef(asset); resource != nil {
				resources = append(resources, resource)
			}
		})
	if err != nil {
		return resources, fmt.Errorf("failed to list assets for %s: %w", req.Parent, err)
	}
	return resources, nil
}

// QueryAssetsByType queries assets of specific types. A failed parent does not stop the others;
// see QueryAllAssets for how partial results are returned.
func (aic *AssetInventoryClient) QueryAssetsByType(ctx context.Context, assetTypes []string) ([]*pb.ResourceRef, error) {
	var allResources []*pb.ResourceRef
	var errs []error
	for _, parent := range aic.getParents() {
		req := &assetpb.ListAssetsRequest{
			Parent:      parent,
			AssetTypes:  assetTypes,
			ContentType: assetpb.ContentType_RESOURCE,
		}
		resources, err := aic.listAssets(ctx, req)
		allResources = append(allResources, resources...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return allResources, errors.Join(errs...)
}

// SearchAssets performs advanced search using Cloud Asset Inventory search. A failed scope does
// not stop the others; see QueryAllAssets for how partial results are returned.
func (aic *AssetInventoryClient) SearchAssets(ctx context.Context, query string) ([]*pb.ResourceRef, error) {
	var allResources []*pb.ResourceRef
	var errs []error
	for _, scope := range aic.getSearchScopes() {
		req := &assetpb.SearchAllResourcesRequest{Scope: scope, Query: query}
		it := aic.client.SearchAllResources(ctx, req)
		err := listPages("search assets in "+scope, it, assetSearchPageSize,
			func(result *assetpb.ResourceSearchResult) {
				if resource := aic.convertSearchResultToResourceRef(result); resource != nil {
					allResources = append(allResources, resource)
				}
			})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to search assets in %s: %w", scope, err))
		}
	}
	return allResources, errors.Join(errs...)
}

// QueryAssetHistory queries the history of changes for specific assets
func (aic *AssetInventoryClient) QueryAssetHistory(ctx context.Context, assetName string, startTime, endTime time.Time) ([]*AssetChange, error) {
	// Get parent from asset name
	parent := aic.extractParentFromAssetName(assetName)

	req := &assetpb.BatchGetAssetsHistoryRequest{
		Parent:      parent,
		AssetNames:  []string{assetName},
		ContentType: assetpb.ContentType_RESOURCE,
		ReadTimeWindow: &assetpb.TimeWindow{
			StartTime: timestamppb.New(startTime),
			EndTime:   timestamppb.New(endTime),
		},
	}

	resp, err := aic.client.BatchGetAssetsHistory(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get asset history: %w", err)
	}

	return aic.parseAssetHistory(resp), nil
}

// QueryAssetRelationships discovers relationships between resources using IAM analysis
func (aic *AssetInventoryClient) QueryAssetRelationships(ctx context.Context) ([]*pb.Relationship, error) {
	var allRelationships []*pb.Relationship

	// For each parent, analyze IAM policies
	parents := aic.getParents()

	for _, parent := range parents {
		req := &assetpb.AnalyzeIamPolicyRequest{
			AnalysisQuery: &assetpb.IamPolicyAnalysisQuery{
				Scope: parent,
			},
		}

		resp, err := aic.client.AnalyzeIamPolicy(ctx, req)
		if err != nil {
			// Log error but continue with other parents
			fmt.Printf("Error analyzing IAM policy for %s: %v\n", parent, err)
			continue
		}

		relationships := aic.extractRelationshipsFromAnalysis(resp)
		allRelationships = append(allRelationships, relationships...)
	}

	return allRelationships, nil
}

// IsHealthy checks if Asset Inventory is functioning correctly
func (aic *AssetInventoryClient) IsHealthy(ctx context.Context) bool {
	// Try a simple query with one of the configured parents
	parents := aic.getParents()
	if len(parents) == 0 {
		return false
	}

	req := &assetpb.ListAssetsRequest{
		Parent:   parents[0],
		PageSize: 1,
	}

	it := aic.client.ListAssets(ctx, req)
	_, err := it.Next()

	// If we get iterator.Done, that's still healthy (just no assets)
	return err == nil || err == iterator.Done
}

// Helper methods

// getParents returns all parent resources based on configured scope
func (aic *AssetInventoryClient) getParents() []string {
	var parents []string

	switch aic.scope {
	case "organizations":
		if aic.orgID != "" {
			parents = append(parents, fmt.Sprintf("organizations/%s", aic.orgID))
		}
	case "folders":
		if aic.folderID != "" {
			parents = append(parents, fmt.Sprintf("folders/%s", aic.folderID))
		}
	case "projects":
		for _, projectID := range aic.projectIDs {
			parents = append(parents, fmt.Sprintf("projects/%s", projectID))
		}
	}

	return parents
}

// getSearchScopes returns search scopes based on configured scope
func (aic *AssetInventoryClient) getSearchScopes() []string {
	// Search scope is the same as parent for Cloud Asset Inventory
	return aic.getParents()
}

// convertAssetToResourceRef converts a Cloud Asset to ResourceRef
func (aic *AssetInventoryClient) convertAssetToResourceRef(asset *assetpb.Asset) *pb.ResourceRef {
	if asset == nil || asset.Resource == nil {
		return nil
	}

	ref := &pb.ResourceRef{
		Id:              asset.Name,
		Name:            aic.extractResourceName(asset.Name),
		Type:            asset.AssetType,
		Service:         aic.extractServiceFromType(asset.AssetType),
		BasicAttributes: make(map[string]string),
	}

	// Extract location from resource data
	if asset.Resource.Location != "" {
		ref.Region = asset.Resource.Location
	}

	// Extract parent information
	if asset.Resource.Parent != "" {
		ref.BasicAttributes["parent"] = asset.Resource.Parent
	}

	// Extract project ID from asset name
	if projectID := aic.extractProjectFromAssetName(asset.Name); projectID != "" {
		ref.BasicAttributes["project_id"] = projectID
	}

	// Extract resource data as JSON
	if asset.Resource.Data != nil {
		// Convert protobuf Struct to JSON string
		if jsonData, err := json.Marshal(asset.Resource.Data.AsMap()); err == nil {
			ref.BasicAttributes["resource_data"] = string(jsonData)
		}
	}

	// Extract labels
	if asset.Resource.Data != nil {
		if labels := aic.extractLabels(asset.Resource.Data.AsMap()); labels != nil {
			for k, v := range labels {
				ref.BasicAttributes["label_"+k] = v
			}
		}
	}

	return ref
}

// convertSearchResultToResourceRef converts a search result to ResourceRef
func (aic *AssetInventoryClient) convertSearchResultToResourceRef(result *assetpb.ResourceSearchResult) *pb.ResourceRef {
	if result == nil {
		return nil
	}

	ref := &pb.ResourceRef{
		Id:              result.Name,
		Name:            aic.extractResourceNameFromFullName(result.Name),
		Type:            result.AssetType,
		Service:         aic.extractServiceFromType(result.AssetType),
		BasicAttributes: make(map[string]string),
	}

	// Set location
	if result.Location != "" {
		ref.Region = result.Location
	}

	// Extract project
	if result.Project != "" {
		ref.BasicAttributes["project_id"] = strings.TrimPrefix(result.Project, "projects/")
	}

	// Store display name
	if result.DisplayName != "" {
		ref.BasicAttributes["display_name"] = result.DisplayName
	}

	// Extract labels
	if result.Labels != nil {
		for k, v := range result.Labels {
			ref.BasicAttributes["label_"+k] = v
		}
	}

	// Store additional attributes as JSON if present
	if result.AdditionalAttributes != nil {
		if jsonData, err := json.Marshal(result.AdditionalAttributes.AsMap()); err == nil {
			ref.BasicAttributes["additional_attributes"] = string(jsonData)
		}
	}

	return ref
}

// extractResourceName extracts resource name from asset name
func (aic *AssetInventoryClient) extractResourceName(assetName string) string {
	// Asset names are in format: //service.googleapis.com/projects/.../resource-type/resource-name
	parts := strings.Split(assetName, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return assetName
}

// extractResourceNameFromFullName extracts resource name from full resource name
func (aic *AssetInventoryClient) extractResourceNameFromFullName(fullName string) string {
	// Format: //service.googleapis.com/projects/PROJECT_ID/locations/LOCATION/resource-type/RESOURCE_NAME
	if strings.HasPrefix(fullName, "//") {
		parts := strings.Split(fullName, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	return fullName
}

// extractServiceFromType extracts service name from asset type
func (aic *AssetInventoryClient) extractServiceFromType(assetType string) string {
	// Asset types are in format: service.googleapis.com/ResourceType
	parts := strings.Split(assetType, "/")
	if len(parts) > 0 {
		servicePart := parts[0]
		return strings.TrimSuffix(servicePart, ".googleapis.com")
	}
	return "unknown"
}

// extractProjectFromAssetName extracts project ID from asset name
func (aic *AssetInventoryClient) extractProjectFromAssetName(assetName string) string {
	// Look for pattern: projects/PROJECT_ID/
	if idx := strings.Index(assetName, "projects/"); idx != -1 {
		substr := assetName[idx+9:] // Skip "projects/"
		if endIdx := strings.Index(substr, "/"); endIdx != -1 {
			return substr[:endIdx]
		}
	}
	return ""
}

// extractParentFromAssetName extracts parent from asset name
func (aic *AssetInventoryClient) extractParentFromAssetName(assetName string) string {
	// Extract project/folder/org from asset name
	if strings.Contains(assetName, "/projects/") {
		if idx := strings.Index(assetName, "projects/"); idx != -1 {
			substr := assetName[idx:]
			if endIdx := strings.Index(substr[9:], "/"); endIdx != -1 {
				return substr[:9+endIdx]
			}
		}
	} else if strings.Contains(assetName, "/folders/") {
		if idx := strings.Index(assetName, "folders/"); idx != -1 {
			substr := assetName[idx:]
			if endIdx := strings.Index(substr[8:], "/"); endIdx != -1 {
				return substr[:8+endIdx]
			}
		}
	} else if strings.Contains(assetName, "/organizations/") {
		if idx := strings.Index(assetName, "organizations/"); idx != -1 {
			substr := assetName[idx:]
			if endIdx := strings.Index(substr[14:], "/"); endIdx != -1 {
				return substr[:14+endIdx]
			}
		}
	}

	// Default to first project if we can't extract
	if len(aic.projectIDs) > 0 {
		return fmt.Sprintf("projects/%s", aic.projectIDs[0])
	}

	return ""
}

// extractLabels extracts labels from resource data
func (aic *AssetInventoryClient) extractLabels(data map[string]interface{}) map[string]string {
	labels := make(map[string]string)

	// Check for labels field
	if labelsData, ok := data["labels"]; ok {
		if labelsMap, ok := labelsData.(map[string]interface{}); ok {
			for k, v := range labelsMap {
				if str, ok := v.(string); ok {
					labels[k] = str
				}
			}
		}
	}

	return labels
}

// parseAssetHistory parses asset history response
func (aic *AssetInventoryClient) parseAssetHistory(resp *assetpb.BatchGetAssetsHistoryResponse) []*AssetChange {
	var changes []*AssetChange

	// The actual response structure may differ, this is a placeholder implementation
	// In practice, you would parse the actual response structure from the API
	for _, asset := range resp.Assets {
		change := &AssetChange{
			Timestamp:  time.Now(), // Placeholder
			ChangeType: "update",
			Asset:      asset.Asset, // TemporalAsset contains Asset field
		}
		changes = append(changes, change)
	}

	return changes
}

// extractRelationshipsFromAnalysis extracts relationships from IAM policy analysis
func (aic *AssetInventoryClient) extractRelationshipsFromAnalysis(resp *assetpb.AnalyzeIamPolicyResponse) []*pb.Relationship {
	var relationships []*pb.Relationship

	// Process analysis results
	for _, analysisResult := range resp.MainAnalysis.AnalysisResults {
		// Extract identity to resource relationships
		if analysisResult.IamBinding != nil && analysisResult.AttachedResourceFullName != "" {
			for _, member := range analysisResult.IamBinding.Members {
				rel := &pb.Relationship{
					TargetId:         analysisResult.AttachedResourceFullName,
					RelationshipType: "has_access_to",
					Properties: map[string]string{
						"source_identity": member,
						"role":            analysisResult.IamBinding.Role,
						"access_state":    "allowed",
					},
				}
				relationships = append(relationships, rel)
			}
		}
	}

	// Process service account impersonation analysis
	for _, serviceAccountResult := range resp.ServiceAccountImpersonationAnalysis {
		// Extract service account usage relationships
		for _, analysisResult := range serviceAccountResult.AnalysisResults {
			if analysisResult.IamBinding != nil {
				rel := &pb.Relationship{
					TargetId:         analysisResult.AttachedResourceFullName,
					RelationshipType: "can_impersonate",
					Properties: map[string]string{
						"source_identity": strings.Join(analysisResult.IamBinding.Members, ","),
						"role":            analysisResult.IamBinding.Role,
					},
				}
				relationships = append(relationships, rel)
			}
		}
	}

	return relationships
}

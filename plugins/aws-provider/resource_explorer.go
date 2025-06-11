package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2/types"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ResourceExplorer provides high-performance resource discovery using AWS Resource Explorer
type ResourceExplorer struct {
	client    *resourceexplorer2.Client
	viewArn   string
	accountID string
}

// NewResourceExplorer creates a new Resource Explorer client
func NewResourceExplorer(cfg aws.Config, viewArn, accountID string) *ResourceExplorer {
	return &ResourceExplorer{
		client:    resourceexplorer2.NewFromConfig(cfg),
		viewArn:   viewArn,
		accountID: accountID,
	}
}

// QueryAllResources queries all resources using Resource Explorer
func (re *ResourceExplorer) QueryAllResources(ctx context.Context) ([]*pb.ResourceRef, error) {
	return re.executeQuery(ctx, "*", nil)
}

// QueryByService queries resources for a specific service
func (re *ResourceExplorer) QueryByService(ctx context.Context, service string) ([]*pb.ResourceRef, error) {
	query := fmt.Sprintf("service:%s", service)
	return re.executeQuery(ctx, query, nil)
}

// QueryByResourceType queries resources by type
func (re *ResourceExplorer) QueryByResourceType(ctx context.Context, resourceType string) ([]*pb.ResourceRef, error) {
	query := fmt.Sprintf("resourcetype:%s", resourceType)
	return re.executeQuery(ctx, query, nil)
}

// QueryWithFilter queries resources with custom filters
func (re *ResourceExplorer) QueryWithFilter(ctx context.Context, filters map[string]string) ([]*pb.ResourceRef, error) {
	var queryParts []string
	
	for key, value := range filters {
		switch key {
		case "service":
			queryParts = append(queryParts, fmt.Sprintf("service:%s", value))
		case "region":
			queryParts = append(queryParts, fmt.Sprintf("region:%s", value))
		case "resource_type":
			queryParts = append(queryParts, fmt.Sprintf("resourcetype:%s", value))
		case "tag":
			// Handle tag filters
			queryParts = append(queryParts, fmt.Sprintf("tag:%s", value))
		default:
			// Generic filter
			queryParts = append(queryParts, fmt.Sprintf("%s:%s", key, value))
		}
	}
	
	query := strings.Join(queryParts, " AND ")
	if query == "" {
		query = "*"
	}
	
	return re.executeQuery(ctx, query, filters)
}

// executeQuery performs the actual Resource Explorer query
func (re *ResourceExplorer) executeQuery(ctx context.Context, query string, filters map[string]string) ([]*pb.ResourceRef, error) {
	var allResources []*pb.ResourceRef
	var nextToken *string

	for {
		input := &resourceexplorer2.SearchInput{
			QueryString: aws.String(query),
			ViewArn:     aws.String(re.viewArn),
			MaxResults:  aws.Int32(1000),
			NextToken:   nextToken,
		}

		result, err := re.client.Search(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("resource explorer search failed: %w", err)
		}

		resources := re.convertResults(result.Resources)
		allResources = append(allResources, resources...)

		if result.NextToken == nil {
			break
		}
		nextToken = result.NextToken
	}

	return allResources, nil
}

// convertResults converts Resource Explorer results to ResourceRef objects
func (re *ResourceExplorer) convertResults(resources []types.Resource) []*pb.ResourceRef {
	var resourceRefs []*pb.ResourceRef

	for _, resource := range resources {
		resourceRef := &pb.ResourceRef{
			AccountId:       re.accountID,
			BasicAttributes: make(map[string]string),
		}

		// Extract basic information
		if resource.Arn != nil {
			resourceRef.Id = *resource.Arn
			resourceRef.BasicAttributes["arn"] = *resource.Arn
			
			// Parse ARN to extract service and resource type
			if arnParts := re.parseARN(*resource.Arn); arnParts != nil {
				resourceRef.Service = arnParts["service"]
				resourceRef.Type = arnParts["resource_type"]
				resourceRef.Region = arnParts["region"]
				resourceRef.BasicAttributes["account_id"] = arnParts["account_id"]
			}
		}

		if resource.ResourceType != nil {
			if resourceRef.Type == "" {
				resourceRef.Type = *resource.ResourceType
			}
			resourceRef.BasicAttributes["resource_type"] = *resource.ResourceType
		}

		if resource.Region != nil {
			if resourceRef.Region == "" {
				resourceRef.Region = *resource.Region
			}
		}

		if resource.Service != nil {
			if resourceRef.Service == "" {
				resourceRef.Service = *resource.Service
			}
		}

		if resource.OwningAccountId != nil {
			resourceRef.BasicAttributes["account_id"] = *resource.OwningAccountId
		}

		// Extract properties
		if resource.Properties != nil {
			for _, prop := range resource.Properties {
				if prop.Name != nil && prop.Data != nil {
					key := strings.ToLower(*prop.Name)
					
					// Handle special properties
					switch key {
					case "name", "resourcename":
						resourceRef.Name = re.extractStringValue(prop.Data)
					case "tags":
						re.extractTags(prop.Data, resourceRef)
					default:
						resourceRef.BasicAttributes[key] = re.extractStringValue(prop.Data)
					}
				}
			}
		}

		// Set a default name if not found
		if resourceRef.Name == "" && resourceRef.Id != "" {
			resourceRef.Name = re.extractNameFromArn(resourceRef.Id)
		}

		resourceRefs = append(resourceRefs, resourceRef)
	}

	return resourceRefs
}

// parseARN parses an AWS ARN and returns its components
func (re *ResourceExplorer) parseARN(arn string) map[string]string {
	// ARN format: arn:partition:service:region:account-id:resource-type/resource-id
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return nil
	}

	result := map[string]string{
		"partition":   parts[1],
		"service":     parts[2],
		"region":      parts[3],
		"account_id":  parts[4],
	}

	// Handle resource part
	if len(parts) >= 6 {
		resourcePart := strings.Join(parts[5:], ":")
		if strings.Contains(resourcePart, "/") {
			resourceParts := strings.SplitN(resourcePart, "/", 2)
			result["resource_type"] = resourceParts[0]
			result["resource_id"] = resourceParts[1]
		} else {
			result["resource_type"] = resourcePart
		}
	}

	return result
}

// extractNameFromArn extracts a resource name from its ARN
func (re *ResourceExplorer) extractNameFromArn(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return arn
	}

	resourcePart := strings.Join(parts[5:], ":")
	if strings.Contains(resourcePart, "/") {
		resourceParts := strings.SplitN(resourcePart, "/", 2)
		return resourceParts[1]
	}

	return resourcePart
}

// extractStringValue safely extracts a string value from Resource Explorer data
func (re *ResourceExplorer) extractStringValue(data interface{}) string {
	if str, ok := data.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", data)
}

// extractTags extracts tags from Resource Explorer property data
func (re *ResourceExplorer) extractTags(data interface{}, resourceRef *pb.ResourceRef) {
	// This would parse tag data and add to BasicAttributes with "tag_" prefix
	// Implementation depends on the actual format returned by Resource Explorer
	if tagData, ok := data.(map[string]interface{}); ok {
		for key, value := range tagData {
			if str, ok := value.(string); ok {
				resourceRef.BasicAttributes["tag_"+key] = str
			}
		}
	}
}

// GetAvailableViews returns available Resource Explorer views
func (re *ResourceExplorer) GetAvailableViews(ctx context.Context) ([]string, error) {
	input := &resourceexplorer2.ListViewsInput{}
	
	result, err := re.client.ListViews(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list Resource Explorer views: %w", err)
	}

	var views []string
	for _, viewArn := range result.Views {
		views = append(views, viewArn)
	}

	return views, nil
}

// IsHealthy checks if Resource Explorer is functioning correctly
func (re *ResourceExplorer) IsHealthy(ctx context.Context) bool {
	// Perform a simple query to test connectivity
	input := &resourceexplorer2.SearchInput{
		QueryString: aws.String("service:s3"),
		ViewArn:     aws.String(re.viewArn),
		MaxResults:  aws.Int32(1),
	}

	_, err := re.client.Search(ctx, input)
	return err == nil
}
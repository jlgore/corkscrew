package discovery

import (
	"context"
	"testing"
)

// mockScanner implements ResourceScanner for testing
type mockScanner struct {
	region string
	debug  bool
}

func (m *mockScanner) DiscoverAndListServiceResources(ctx context.Context, serviceName string) ([]AWSResourceRef, error) {
	// Return mock data based on service
	switch serviceName {
	case "iam":
		return []AWSResourceRef{
			{ID: "user1", Name: "test-user-1", Type: "User", Service: "iam", Region: m.region},
			{ID: "user2", Name: "test-user-2", Type: "User", Service: "iam", Region: m.region},
			{ID: "role1", Name: "test-role-1", Type: "Role", Service: "iam", Region: m.region},
		}, nil
	case "s3":
		return []AWSResourceRef{
			{ID: "bucket1", Name: "test-bucket-1", Type: "Bucket", Service: "s3", Region: m.region},
		}, nil
	default:
		return []AWSResourceRef{}, nil
	}
}

func (m *mockScanner) ExecuteOperationWithParams(ctx context.Context, serviceName, operationName string, params map[string]interface{}) ([]AWSResourceRef, error) {
	// Mock parameter-based operations
	switch operationName {
	case "ListUserTags":
		if userName, ok := params["UserName"].(string); ok {
			return []AWSResourceRef{
				{ID: userName + "-tag1", Name: "Environment", Type: "Tag", Service: "iam", Region: m.region, Metadata: map[string]string{"Value": "Production"}},
			}, nil
		}
	case "ListUserPolicies":
		if userName, ok := params["UserName"].(string); ok {
			return []AWSResourceRef{
				{ID: userName + "-policy1", Name: userName + "-inline-policy", Type: "Policy", Service: "iam", Region: m.region},
			}, nil
		}
	}
	return []AWSResourceRef{}, nil
}

func (m *mockScanner) GetRegion() string {
	return m.region
}

func TestHierarchicalDiscoverer_IAMStrategy(t *testing.T) {
	scanner := &mockScanner{region: "us-east-1", debug: true}
	discoverer := NewHierarchicalDiscoverer(scanner, true)

	ctx := context.Background()
	resources, err := discoverer.DiscoverService(ctx, "iam")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(resources) == 0 {
		t.Fatal("Expected resources to be discovered")
	}

	// Check that we have both global and user-specific resources
	hasUsers := false
	hasUserTags := false

	for _, resource := range resources {
		if resource.Type == "User" {
			hasUsers = true
		}
		if resource.Type == "Tag" {
			hasUserTags = true
		}
	}

	if !hasUsers {
		t.Error("Expected to find User resources")
	}

	if !hasUserTags {
		t.Error("Expected to find user-specific Tag resources")
	}

	t.Logf("Successfully discovered %d resources with hierarchical strategy", len(resources))
}

func TestHierarchicalDiscoverer_FallbackToFlat(t *testing.T) {
	scanner := &mockScanner{region: "us-east-1", debug: true}
	discoverer := NewHierarchicalDiscoverer(scanner, true)

	ctx := context.Background()
	resources, err := discoverer.DiscoverService(ctx, "unknown-service")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should fall back to flat discovery and return empty results for unknown service
	if len(resources) != 0 {
		t.Errorf("Expected 0 resources for unknown service, got: %d", len(resources))
	}
}

func TestDiscoveryContext(t *testing.T) {
	ctx := &discoveryContext{
		discovered: map[string][]AWSResourceRef{
			"ListUsers": {
				{ID: "user1", Name: "test-user", Type: "User", Service: "iam"},
			},
			"ListRoles": {
				{ID: "role1", Name: "test-role", Type: "Role", Service: "iam"},
			},
		},
		region: "us-east-1",
	}

	// Test GetDiscoveredResourcesByOperation
	users := ctx.GetDiscoveredResourcesByOperation("ListUsers")
	if len(users) != 1 || users[0].Name != "test-user" {
		t.Error("GetDiscoveredResourcesByOperation failed")
	}

	// Test GetDiscoveredResources by type
	userResources := ctx.GetDiscoveredResources("User")
	if len(userResources) != 1 || userResources[0].Name != "test-user" {
		t.Error("GetDiscoveredResources failed")
	}

	// Test GetRegion
	if ctx.GetRegion() != "us-east-1" {
		t.Error("GetRegion failed")
	}
}

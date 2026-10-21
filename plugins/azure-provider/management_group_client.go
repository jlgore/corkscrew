package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/jlgore/corkscrew/internal/shared/paging"
)

// ManagementGroupScope represents the scope configuration for Azure resources
type ManagementGroupScope struct {
	Type           string   `json:"type"`            // "subscription", "management_group", "tenant"
	ID             string   `json:"id"`              // The actual ID
	Name           string   `json:"name"`            // Display name
	Subscriptions  []string `json:"subscriptions"`   // All subscriptions in scope
	ChildGroups    []string `json:"child_groups"`    // Child management groups
	AccessLevel    string   `json:"access_level"`    // "read", "contributor", "owner"
	HasPermissions bool     `json:"has_permissions"` // Whether we have access
}

// ManagementGroupClient handles management group discovery and scoping
type ManagementGroupClient struct {
	credential          azcore.TokenCredential
	mgClient            *armmanagementgroups.Client
	subscriptionsClient *armsubscriptions.Client
	cache               map[string]*ManagementGroupScope
	mu                  sync.RWMutex
}

// NewManagementGroupClient creates a new management group client
func NewManagementGroupClient(credential azcore.TokenCredential) (*ManagementGroupClient, error) {
	mgClient, err := armmanagementgroups.NewClient(credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create management groups client: %w", err)
	}

	subsClient, err := armsubscriptions.NewClient(credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscriptions client: %w", err)
	}

	return &ManagementGroupClient{
		credential:          credential,
		mgClient:            mgClient,
		subscriptionsClient: subsClient,
		cache:               make(map[string]*ManagementGroupScope),
	}, nil
}

// managementGroupDepthMax bounds the hierarchy walk. Azure allows six levels below the tenant
// root, so a deeper walk means a malformed or cyclic answer, not a real hierarchy.
const managementGroupDepthMax = 10

// DiscoverManagementGroupHierarchy discovers the full management group hierarchy from root.
//
// A failed root group, child group, or subscription listing does not stop the walk: the scopes
// that were discovered are returned together with the joined error, because a partial
// hierarchy is a narrower scan scope and the caller must report it. A 403 is not an error
// here: a group the credential cannot read is outside its scope.
func (c *ManagementGroupClient) DiscoverManagementGroupHierarchy(ctx context.Context) ([]*ManagementGroupScope, error) {
	log.Printf("Discovering management group hierarchy from root...")

	// First, try to get the root management group (tenant root)
	rootGroups, err := c.discoverRootManagementGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover root management groups: %w", err)
	}

	var allScopes []*ManagementGroupScope
	var errs []error
	visited := make(map[string]bool)
	for _, rootGroup := range rootGroups {
		scopes, err := c.discoverManagementGroupRecursive(ctx, rootGroup.ID, 0, visited)
		allScopes = append(allScopes, scopes...)
		if err != nil && !isForbidden(err) {
			errs = append(errs, fmt.Errorf("root group %s: %w", rootGroup.ID, err))
		}
	}

	// Also discover accessible subscriptions not in management groups
	orphanSubs, err := c.discoverOrphanSubscriptions(ctx, allScopes)
	allScopes = append(allScopes, orphanSubs...)
	if err != nil && !isForbidden(err) {
		errs = append(errs, fmt.Errorf("orphan subscriptions: %w", err))
	}

	log.Printf("Discovered %d management group scopes", len(allScopes))
	return allScopes, errors.Join(errs...)
}

// discoverRootManagementGroups discovers root-level management groups
func (c *ManagementGroupClient) discoverRootManagementGroups(ctx context.Context) ([]*ManagementGroupScope, error) {
	var rootGroups []*ManagementGroupScope

	// List all management groups the user has access to
	pager := c.mgClient.NewListPager(&armmanagementgroups.ClientListOptions{})
	guard := paging.New("list management groups", paging.PagesMax)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list management groups: %w", err)
		}
		if err := guardNextLink(guard, page.NextLink); err != nil {
			return nil, err
		}

		for _, mg := range page.Value {
			if mg.ID == nil || mg.Name == nil {
				continue
			}

			// Check if this is a root group (no parent)
			// For now, assume all listed management groups could be root groups
			// We'll determine hierarchy later through detailed queries
			isRoot := true

			if isRoot {
				// Convert ManagementGroupInfo to ManagementGroup for getDisplayName
				mgGroup := &armmanagementgroups.ManagementGroup{
					ID:   mg.ID,
					Name: mg.Name,
					Type: mg.Type,
					Properties: &armmanagementgroups.ManagementGroupProperties{
						DisplayName: mg.Properties.DisplayName,
					},
				}

				scope := &ManagementGroupScope{
					Type:           "management_group",
					ID:             *mg.Name, // Use name as ID for management groups
					Name:           getDisplayName(mgGroup),
					Subscriptions:  []string{},
					ChildGroups:    []string{},
					AccessLevel:    "read", // Will be determined later
					HasPermissions: true,   // If we can list it, we have some access
				}
				rootGroups = append(rootGroups, scope)
			}
		}
	}

	// If no root groups found, try to get tenant root group directly
	if len(rootGroups) == 0 {
		tenantRoot, err := c.getTenantRootGroup(ctx)
		if err == nil {
			rootGroups = append(rootGroups, tenantRoot)
		}
	}

	return rootGroups, nil
}

// discoverManagementGroupRecursive discovers mgID and its descendants. visited holds every group
// already entered on this walk, so a cyclic answer cannot loop; depth is bounded by
// managementGroupDepthMax. Child failures are joined into the returned error alongside the
// scopes that were discovered.
func (c *ManagementGroupClient) discoverManagementGroupRecursive(
	ctx context.Context, mgID string, depth int, visited map[string]bool,
) ([]*ManagementGroupScope, error) {
	if depth > managementGroupDepthMax {
		return nil, fmt.Errorf("maximum recursion depth reached for management group %s", mgID)
	}
	if visited[mgID] {
		return nil, nil
	}
	visited[mgID] = true

	// Check cache first
	c.mu.RLock()
	if cached, exists := c.cache[mgID]; exists {
		c.mu.RUnlock()
		return []*ManagementGroupScope{cached}, nil
	}
	c.mu.RUnlock()

	// Get detailed information about this management group
	result, err := c.mgClient.Get(ctx, mgID, &armmanagementgroups.ClientGetOptions{
		Expand:  to.Ptr(armmanagementgroups.ManagementGroupExpandTypeChildren),
		Recurse: to.Ptr(false), // We'll handle recursion manually
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get management group %s: %w", mgID, err)
	}

	scope := &ManagementGroupScope{
		Type:           "management_group",
		ID:             mgID,
		Name:           getDisplayName(&result.ManagementGroup),
		Subscriptions:  []string{},
		ChildGroups:    []string{},
		AccessLevel:    "read",
		HasPermissions: true,
	}
	allScopes := []*ManagementGroupScope{scope}

	var errs []error
	var children []*armmanagementgroups.ManagementGroupChildInfo
	if result.Properties != nil {
		children = result.Properties.Children
	}
	for _, child := range children {
		if child.ID == nil || child.Name == nil || child.Type == nil {
			continue
		}
		childName := *child.Name
		switch *child.Type {
		case "Microsoft.Management/managementGroups":
			scope.ChildGroups = append(scope.ChildGroups, childName)
			childScopes, err := c.discoverManagementGroupRecursive(ctx, childName, depth+1, visited)
			allScopes = append(allScopes, childScopes...)
			if err != nil && !isForbidden(err) {
				errs = append(errs, fmt.Errorf("child management group %s: %w", childName, err))
			}
		case "Microsoft.Subscription":
			if subID := extractSubscriptionID(*child.ID); subID != "" {
				scope.Subscriptions = append(scope.Subscriptions, subID)
			}
		}
	}

	// Cache only a complete answer, so a later walk retries the failed children.
	if len(errs) == 0 {
		c.mu.Lock()
		c.cache[mgID] = scope
		c.mu.Unlock()
	}
	return allScopes, errors.Join(errs...)
}

// discoverOrphanSubscriptions finds subscriptions not in any management group
func (c *ManagementGroupClient) discoverOrphanSubscriptions(ctx context.Context, existingScopes []*ManagementGroupScope) ([]*ManagementGroupScope, error) {
	// Get all subscriptions in management groups
	mgSubscriptions := make(map[string]bool)
	for _, scope := range existingScopes {
		for _, subID := range scope.Subscriptions {
			mgSubscriptions[subID] = true
		}
	}

	// List all accessible subscriptions
	var orphanScopes []*ManagementGroupScope
	pager := c.subscriptionsClient.NewListPager(nil)
	guard := paging.New("list subscriptions", paging.PagesMax)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return orphanScopes, fmt.Errorf("failed to list subscriptions: %w", err)
		}
		if err := guardNextLink(guard, page.NextLink); err != nil {
			return orphanScopes, err
		}

		for _, sub := range page.Value {
			if sub.SubscriptionID == nil {
				continue
			}

			subID := *sub.SubscriptionID
			if !mgSubscriptions[subID] {
				// This subscription is not in any management group
				scope := &ManagementGroupScope{
					Type:           "subscription",
					ID:             subID,
					Name:           getSubscriptionDisplayName(sub),
					Subscriptions:  []string{subID},
					ChildGroups:    []string{},
					AccessLevel:    "read",
					HasPermissions: true,
				}
				orphanScopes = append(orphanScopes, scope)
			}
		}
	}

	return orphanScopes, nil
}

// getTenantRootGroup attempts to get the tenant root management group
func (c *ManagementGroupClient) getTenantRootGroup(ctx context.Context) (*ManagementGroupScope, error) {
	// Try common tenant root group patterns
	tenantPatterns := []string{
		"Tenant Root Group",
		"Root Management Group",
		"Default Management Group",
	}

	for _, pattern := range tenantPatterns {
		result, err := c.mgClient.Get(ctx, pattern, &armmanagementgroups.ClientGetOptions{
			Expand: to.Ptr(armmanagementgroups.ManagementGroupExpandTypeChildren),
		})
		if err == nil {
			return &ManagementGroupScope{
				Type:           "management_group",
				ID:             pattern,
				Name:           getDisplayName(&result.ManagementGroup),
				Subscriptions:  []string{},
				ChildGroups:    []string{},
				AccessLevel:    "read",
				HasPermissions: true,
			}, nil
		}
	}

	return nil, fmt.Errorf("tenant root group not found")
}

// GetScopeForResourceGraph returns the appropriate scope for Resource Graph queries
func (c *ManagementGroupClient) GetScopeForResourceGraph(scopes []*ManagementGroupScope) ([]string, string) {
	var subscriptions []string
	var scopeType string

	// Collect all unique subscriptions
	subSet := make(map[string]bool)
	hasManagementGroups := false

	for _, scope := range scopes {
		if scope.Type == "management_group" {
			hasManagementGroups = true
		}
		for _, subID := range scope.Subscriptions {
			subSet[subID] = true
		}
	}

	// Convert to slice
	for subID := range subSet {
		subscriptions = append(subscriptions, subID)
	}

	if hasManagementGroups {
		scopeType = "management_group"
	} else {
		scopeType = "subscription"
	}

	return subscriptions, scopeType
}

// Helper functions
func getDisplayName(mg *armmanagementgroups.ManagementGroup) string {
	if mg.Properties != nil && mg.Properties.DisplayName != nil {
		return *mg.Properties.DisplayName
	}
	if mg.Name != nil {
		return *mg.Name
	}
	return "Unknown"
}

func getSubscriptionDisplayName(sub *armsubscriptions.Subscription) string {
	if sub.DisplayName != nil {
		return *sub.DisplayName
	}
	if sub.SubscriptionID != nil {
		return *sub.SubscriptionID
	}
	return "Unknown"
}

func extractSubscriptionID(fullID string) string {
	// Extract subscription ID from full ARM ID
	// Format: /subscriptions/{subscription-id}
	parts := strings.Split(fullID, "/")
	for i, part := range parts {
		if part == "subscriptions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

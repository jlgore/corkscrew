package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
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

// DiscoverManagementGroupHierarchy discovers the full management group hierarchy from root
func (c *ManagementGroupClient) DiscoverManagementGroupHierarchy(ctx context.Context) ([]*ManagementGroupScope, error) {
	log.Printf("Discovering management group hierarchy from root...")

	// First, try to get the root management group (tenant root)
	rootGroups, err := c.discoverRootManagementGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover root management groups: %w", err)
	}

	var allScopes []*ManagementGroupScope

	// For each root group, recursively discover the hierarchy
	for _, rootGroup := range rootGroups {
		scopes, err := c.discoverManagementGroupRecursive(ctx, rootGroup.ID, 0)
		if err != nil {
			log.Printf("Warning: Failed to discover hierarchy for root group %s: %v", rootGroup.ID, err)
			continue
		}
		allScopes = append(allScopes, scopes...)
	}

	// Also discover accessible subscriptions not in management groups
	orphanSubs, err := c.discoverOrphanSubscriptions(ctx, allScopes)
	if err != nil {
		log.Printf("Warning: Failed to discover orphan subscriptions: %v", err)
	} else {
		allScopes = append(allScopes, orphanSubs...)
	}

	log.Printf("Discovered %d management group scopes", len(allScopes))
	return allScopes, nil
}

// discoverRootManagementGroups discovers root-level management groups
func (c *ManagementGroupClient) discoverRootManagementGroups(ctx context.Context) ([]*ManagementGroupScope, error) {
	var rootGroups []*ManagementGroupScope

	// List all management groups the user has access to
	pager := c.mgClient.NewListPager(&armmanagementgroups.ClientListOptions{})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list management groups: %w", err)
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

// discoverManagementGroupRecursive recursively discovers management group hierarchy
func (c *ManagementGroupClient) discoverManagementGroupRecursive(ctx context.Context, mgID string, depth int) ([]*ManagementGroupScope, error) {
	// Prevent infinite recursion
	if depth > 10 {
		return nil, fmt.Errorf("maximum recursion depth reached for management group %s", mgID)
	}

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

	var allScopes []*ManagementGroupScope
	allScopes = append(allScopes, scope)

	// Process children
	if result.Properties != nil && result.Properties.Children != nil {
		for _, child := range result.Properties.Children {
			if child.ID == nil || child.Name == nil {
				continue
			}

			childName := *child.Name
			if child.Type != nil {
				switch *child.Type {
				case "Microsoft.Management/managementGroups":
					scope.ChildGroups = append(scope.ChildGroups, childName)
					// Recursively discover child management groups
					childScopes, err := c.discoverManagementGroupRecursive(ctx, childName, depth+1)
					if err != nil {
						log.Printf("Warning: Failed to discover child management group %s: %v", childName, err)
						continue
					}
					allScopes = append(allScopes, childScopes...)

				case "Microsoft.Subscription":
					// Extract subscription ID from the full ID
					subID := extractSubscriptionID(*child.ID)
					if subID != "" {
						scope.Subscriptions = append(scope.Subscriptions, subID)
					}
				}
			}
		}
	}

	// Cache the result
	c.mu.Lock()
	c.cache[mgID] = scope
	c.mu.Unlock()

	return allScopes, nil
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

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list subscriptions: %w", err)
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

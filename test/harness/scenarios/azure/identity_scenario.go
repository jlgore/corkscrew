package azure

import (
	"fmt"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/pulumi/pulumi-azure-native-sdk/authorization/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/managedidentity/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/storage/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// IdentityScenario creates Azure managed identities and role assignments
type IdentityScenario struct {
	expectedResources map[string]interface{}
}

// NewIdentityScenario creates a new Azure identity scenario
func NewIdentityScenario() automation.Scenario {
	return &IdentityScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *IdentityScenario) GetName() string {
	return "azure-identity"
}

// GetServices returns the Azure services this scenario tests
func (s *IdentityScenario) GetServices() []string {
	return []string{"managedidentity", "authorization", "storage", "resources"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *IdentityScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("azure-identity"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create resource group
	rg, err := resources.NewResourceGroup(ctx, "identity-rg", &resources.ResourceGroupArgs{
		ResourceGroupName: pulumi.String(fmt.Sprintf("corkscrew-identity-%s", testID)),
		Location:          pulumi.String("eastus"),
		Tags:              tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create resource group: %w", err)
	}

	// Create user-assigned managed identity
	identity, err := managedidentity.NewUserAssignedIdentity(ctx, "test-identity", &managedidentity.UserAssignedIdentityArgs{
		ResourceName:      pulumi.String(fmt.Sprintf("corkscrew-identity-%s", testID)),
		ResourceGroupName: rg.Name,
		Location:          rg.Location,
		Tags:              tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create managed identity: %w", err)
	}

	// Create storage account for testing permissions
	shortID := testID
	if len(testID) > 10 {
		shortID = testID[:10]
	}
	storageAccountName := fmt.Sprintf("corkscrewid%s", shortID)

	account, err := storage.NewStorageAccount(ctx, "identity-storage", &storage.StorageAccountArgs{
		AccountName:       pulumi.String(storageAccountName),
		ResourceGroupName: rg.Name,
		Location:          rg.Location,
		Sku: &storage.SkuArgs{
			Name: pulumi.String("Standard_LRS"),
		},
		Kind:                   pulumi.String("StorageV2"),
		AccessTier:             pulumi.String("Hot"),
		AllowBlobPublicAccess:  pulumi.Bool(false),
		EnableHttpsTrafficOnly: pulumi.Bool(true),
		MinimumTlsVersion:      pulumi.String("TLS1_2"),
		Tags:                   tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create storage account: %w", err)
	}

	// Create role assignment - Storage Blob Data Reader
	roleAssignment, err := authorization.NewRoleAssignment(ctx, "identity-role-assignment", &authorization.RoleAssignmentArgs{
		RoleAssignmentName: pulumi.String(fmt.Sprintf("%s-blob-reader", testID)),
		Scope:              account.ID(),
		PrincipalId:        identity.PrincipalId,
		RoleDefinitionId:   pulumi.String("/subscriptions/{subscription-id}/providers/Microsoft.Authorization/roleDefinitions/2a2b9908-6ea1-4ae2-8e65-a410df84e7d1"), // Storage Blob Data Reader
		PrincipalType:      pulumi.String("ServicePrincipal"),
	})
	if err != nil {
		return fmt.Errorf("failed to create role assignment: %w", err)
	}

	// Create another managed identity for comparison
	identity2, err := managedidentity.NewUserAssignedIdentity(ctx, "test-identity-2", &managedidentity.UserAssignedIdentityArgs{
		ResourceName:      pulumi.String(fmt.Sprintf("corkscrew-identity-2-%s", testID)),
		ResourceGroupName: rg.Name,
		Location:          rg.Location,
		Tags:              tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create second managed identity: %w", err)
	}

	// Create role assignment - Storage Blob Data Contributor for second identity
	roleAssignment2, err := authorization.NewRoleAssignment(ctx, "identity-role-assignment-2", &authorization.RoleAssignmentArgs{
		RoleAssignmentName: pulumi.String(fmt.Sprintf("%s-blob-contributor", testID)),
		Scope:              account.ID(),
		PrincipalId:        identity2.PrincipalId,
		RoleDefinitionId:   pulumi.String("/subscriptions/{subscription-id}/providers/Microsoft.Authorization/roleDefinitions/ba92f5b4-2d11-453d-a403-e96b0029c9fe"), // Storage Blob Data Contributor
		PrincipalType:      pulumi.String("ServicePrincipal"),
	})
	if err != nil {
		return fmt.Errorf("failed to create second role assignment: %w", err)
	}

	// Export resource details for verification
	ctx.Export("resourceGroupName", rg.Name)
	ctx.Export("identityName", identity.Name)
	ctx.Export("identityId", identity.ID())
	ctx.Export("identityPrincipalId", identity.PrincipalId)
	ctx.Export("identity2Name", identity2.Name)
	ctx.Export("identity2Id", identity2.ID())
	ctx.Export("identity2PrincipalId", identity2.PrincipalId)
	ctx.Export("storageAccountName", account.Name)
	ctx.Export("storageAccountId", account.ID())
	ctx.Export("roleAssignmentId", roleAssignment.ID())
	ctx.Export("roleAssignment2Id", roleAssignment2.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"managedidentity": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("UserAssignedIdentity"),
				"name": identity.Name,
				"id":   identity.ID(),
				"attributes": pulumi.Map{
					"location": pulumi.String("eastus"),
				},
				"tags": tags,
			},
			pulumi.Map{
				"type": pulumi.String("UserAssignedIdentity"),
				"name": identity2.Name,
				"id":   identity2.ID(),
				"attributes": pulumi.Map{
					"location": pulumi.String("eastus"),
				},
				"tags": tags,
			},
		},
		"authorization": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("RoleAssignment"),
				"id":   roleAssignment.ID(),
				"attributes": pulumi.Map{
					"role":           pulumi.String("Storage Blob Data Reader"),
					"principal_type": pulumi.String("ServicePrincipal"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("RoleAssignment"),
				"id":   roleAssignment2.ID(),
				"attributes": pulumi.Map{
					"role":           pulumi.String("Storage Blob Data Contributor"),
					"principal_type": pulumi.String("ServicePrincipal"),
				},
			},
		},
		"storage": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("StorageAccount"),
				"name": account.Name,
				"id":   account.ID(),
				"attributes": pulumi.Map{
					"sku_name":                  pulumi.String("Standard_LRS"),
					"kind":                      pulumi.String("StorageV2"),
					"access_tier":               pulumi.String("Hot"),
					"allow_blob_public_access":  pulumi.Bool(false),
					"enable_https_traffic_only": pulumi.Bool(true),
					"minimum_tls_version":       pulumi.String("TLS1_2"),
				},
				"tags": tags,
			},
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"managedidentity": []interface{}{
				map[string]interface{}{
					"type": "UserAssignedIdentity",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"location": "eastus",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "azure-identity",
						"CreatedBy":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "UserAssignedIdentity",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"location": "eastus",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "azure-identity",
						"CreatedBy":   "corkscrew-test",
					},
				},
			},
			"authorization": []interface{}{
				map[string]interface{}{
					"type": "RoleAssignment",
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"role":           "Storage Blob Data Reader",
						"principal_type": "ServicePrincipal",
					},
				},
				map[string]interface{}{
					"type": "RoleAssignment",
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"role":           "Storage Blob Data Contributor",
						"principal_type": "ServicePrincipal",
					},
				},
			},
			"storage": []interface{}{
				map[string]interface{}{
					"type": "StorageAccount",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"sku_name":                  "Standard_LRS",
						"kind":                      "StorageV2",
						"access_tier":               "Hot",
						"allow_blob_public_access":  false,
						"enable_https_traffic_only": true,
						"minimum_tls_version":       "TLS1_2",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "azure-identity",
						"CreatedBy":   "corkscrew-test",
					},
				},
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *IdentityScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}
//go:build integration

package azure

import (
	"fmt"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/storage/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ResourceGroupScenario creates a basic Azure resource group with storage account
type ResourceGroupScenario struct {
	expectedResources map[string]interface{}
}

// NewResourceGroupScenario creates a new Azure resource group scenario
func NewResourceGroupScenario() automation.Scenario {
	return &ResourceGroupScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *ResourceGroupScenario) GetName() string {
	return "azure-resource-group"
}

// GetServices returns the Azure services this scenario tests
func (s *ResourceGroupScenario) GetServices() []string {
	return []string{"resources", "storage"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *ResourceGroupScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("azure-resource-group"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create resource group
	rg, err := resources.NewResourceGroup(ctx, "test-rg", &resources.ResourceGroupArgs{
		ResourceGroupName: pulumi.String(fmt.Sprintf("corkscrew-test-%s", testID)),
		Location:          pulumi.String("eastus"),
		Tags:              tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create resource group: %w", err)
	}

	// Create storage account with unique name
	shortID := testID
	if len(testID) > 10 {
		shortID = testID[:10]
	}
	storageAccountName := fmt.Sprintf("corkscrewtest%s", shortID)

	account, err := storage.NewStorageAccount(ctx, "test-storage", &storage.StorageAccountArgs{
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

	// Export resource details for verification
	ctx.Export("resourceGroupName", rg.Name)
	ctx.Export("resourceGroupId", rg.ID())
	ctx.Export("storageAccountName", account.Name)
	ctx.Export("storageAccountId", account.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"resources": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("ResourceGroup"),
				"name": rg.Name,
				"id":   rg.ID(),
				"attributes": pulumi.Map{
					"location": pulumi.String("eastus"),
				},
				"tags": tags,
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
			"resources": []interface{}{
				map[string]interface{}{
					"type": "ResourceGroup",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"location": "eastus",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "azure-resource-group",
						"CreatedBy":   "corkscrew-test",
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
						"Scenario":    "azure-resource-group",
						"CreatedBy":   "corkscrew-test",
					},
				},
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *ResourceGroupScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

package azure

import (
	"fmt"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/pulumi/pulumi-azure-native-sdk/network/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// VNetScenario creates a complete Azure virtual network with subnets and NSGs
type VNetScenario struct {
	expectedResources map[string]interface{}
}

// NewVNetScenario creates a new Azure VNet scenario
func NewVNetScenario() automation.Scenario {
	return &VNetScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *VNetScenario) GetName() string {
	return "azure-vnet"
}

// GetServices returns the Azure services this scenario tests
func (s *VNetScenario) GetServices() []string {
	return []string{"network", "resources"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *VNetScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("azure-vnet"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create resource group
	rg, err := resources.NewResourceGroup(ctx, "vnet-rg", &resources.ResourceGroupArgs{
		ResourceGroupName: pulumi.String(fmt.Sprintf("corkscrew-vnet-%s", testID)),
		Location:          pulumi.String("eastus"),
		Tags:              tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create resource group: %w", err)
	}

	// Create virtual network
	vnet, err := network.NewVirtualNetwork(ctx, "test-vnet", &network.VirtualNetworkArgs{
		VirtualNetworkName: pulumi.String(fmt.Sprintf("corkscrew-vnet-%s", testID)),
		ResourceGroupName:  rg.Name,
		Location:           rg.Location,
		AddressSpace: &network.AddressSpaceArgs{
			AddressPrefixes: pulumi.StringArray{
				pulumi.String("10.0.0.0/16"),
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create virtual network: %w", err)
	}

	// Create public subnet
	publicSubnet, err := network.NewSubnet(ctx, "public-subnet", &network.SubnetArgs{
		SubnetName:         pulumi.String("public-subnet"),
		ResourceGroupName:  rg.Name,
		VirtualNetworkName: vnet.Name,
		AddressPrefix:      pulumi.String("10.0.1.0/24"),
	})
	if err != nil {
		return fmt.Errorf("failed to create public subnet: %w", err)
	}

	// Create private subnet
	privateSubnet, err := network.NewSubnet(ctx, "private-subnet", &network.SubnetArgs{
		SubnetName:         pulumi.String("private-subnet"),
		ResourceGroupName:  rg.Name,
		VirtualNetworkName: vnet.Name,
		AddressPrefix:      pulumi.String("10.0.2.0/24"),
	})
	if err != nil {
		return fmt.Errorf("failed to create private subnet: %w", err)
	}

	// Create Network Security Group for public subnet
	publicNsg, err := network.NewNetworkSecurityGroup(ctx, "public-nsg", &network.NetworkSecurityGroupArgs{
		NetworkSecurityGroupName: pulumi.String(fmt.Sprintf("public-nsg-%s", testID)),
		ResourceGroupName:        rg.Name,
		Location:                 rg.Location,
		SecurityRules: network.SecurityRuleArray{
			&network.SecurityRuleArgs{
				Name:                     pulumi.String("AllowHTTP"),
				Protocol:                 pulumi.String("Tcp"),
				SourcePortRange:          pulumi.String("*"),
				DestinationPortRange:     pulumi.String("80"),
				SourceAddressPrefix:      pulumi.String("*"),
				DestinationAddressPrefix: pulumi.String("*"),
				Access:                   pulumi.String("Allow"),
				Priority:                 pulumi.Int(100),
				Direction:                pulumi.String("Inbound"),
			},
			&network.SecurityRuleArgs{
				Name:                     pulumi.String("AllowHTTPS"),
				Protocol:                 pulumi.String("Tcp"),
				SourcePortRange:          pulumi.String("*"),
				DestinationPortRange:     pulumi.String("443"),
				SourceAddressPrefix:      pulumi.String("*"),
				DestinationAddressPrefix: pulumi.String("*"),
				Access:                   pulumi.String("Allow"),
				Priority:                 pulumi.Int(110),
				Direction:                pulumi.String("Inbound"),
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create public NSG: %w", err)
	}

	// Create Network Security Group for private subnet
	privateNsg, err := network.NewNetworkSecurityGroup(ctx, "private-nsg", &network.NetworkSecurityGroupArgs{
		NetworkSecurityGroupName: pulumi.String(fmt.Sprintf("private-nsg-%s", testID)),
		ResourceGroupName:        rg.Name,
		Location:                 rg.Location,
		SecurityRules: network.SecurityRuleArray{
			&network.SecurityRuleArgs{
				Name:                     pulumi.String("AllowVNetInbound"),
				Protocol:                 pulumi.String("*"),
				SourcePortRange:          pulumi.String("*"),
				DestinationPortRange:     pulumi.String("*"),
				SourceAddressPrefix:      pulumi.String("VirtualNetwork"),
				DestinationAddressPrefix: pulumi.String("VirtualNetwork"),
				Access:                   pulumi.String("Allow"),
				Priority:                 pulumi.Int(100),
				Direction:                pulumi.String("Inbound"),
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create private NSG: %w", err)
	}

	// Export resource details for verification
	ctx.Export("resourceGroupName", rg.Name)
	ctx.Export("vnetName", vnet.Name)
	ctx.Export("vnetId", vnet.ID())
	ctx.Export("publicSubnetName", publicSubnet.Name)
	ctx.Export("publicSubnetId", publicSubnet.ID())
	ctx.Export("privateSubnetName", privateSubnet.Name)
	ctx.Export("privateSubnetId", privateSubnet.ID())
	ctx.Export("publicNsgName", publicNsg.Name)
	ctx.Export("publicNsgId", publicNsg.ID())
	ctx.Export("privateNsgName", privateNsg.Name)
	ctx.Export("privateNsgId", privateNsg.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"network": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("VirtualNetwork"),
				"name": vnet.Name,
				"id":   vnet.ID(),
				"attributes": pulumi.Map{
					"address_space": pulumi.StringArray{pulumi.String("10.0.0.0/16")},
					"location":      pulumi.String("eastus"),
				},
				"tags": tags,
			},
			pulumi.Map{
				"type": pulumi.String("Subnet"),
				"name": publicSubnet.Name,
				"id":   publicSubnet.ID(),
				"attributes": pulumi.Map{
					"address_prefix": pulumi.String("10.0.1.0/24"),
					"subnet_type":    pulumi.String("public"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Subnet"),
				"name": privateSubnet.Name,
				"id":   privateSubnet.ID(),
				"attributes": pulumi.Map{
					"address_prefix": pulumi.String("10.0.2.0/24"),
					"subnet_type":    pulumi.String("private"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("NetworkSecurityGroup"),
				"name": publicNsg.Name,
				"id":   publicNsg.ID(),
				"attributes": pulumi.Map{
					"security_rules_count": pulumi.Int(2),
					"nsg_type":             pulumi.String("public"),
				},
				"tags": tags,
			},
			pulumi.Map{
				"type": pulumi.String("NetworkSecurityGroup"),
				"name": privateNsg.Name,
				"id":   privateNsg.ID(),
				"attributes": pulumi.Map{
					"security_rules_count": pulumi.Int(1),
					"nsg_type":             pulumi.String("private"),
				},
				"tags": tags,
			},
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"network": []interface{}{
				map[string]interface{}{
					"type": "VirtualNetwork",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"address_space": []string{"10.0.0.0/16"},
						"location":      "eastus",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "azure-vnet",
						"CreatedBy":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "Subnet",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"address_prefix": "10.0.1.0/24",
						"subnet_type":    "public",
					},
				},
				map[string]interface{}{
					"type": "Subnet",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"address_prefix": "10.0.2.0/24",
						"subnet_type":    "private",
					},
				},
				map[string]interface{}{
					"type": "NetworkSecurityGroup",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"security_rules_count": 2,
						"nsg_type":             "public",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "azure-vnet",
						"CreatedBy":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "NetworkSecurityGroup",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"security_rules_count": 1,
						"nsg_type":             "private",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "azure-vnet",
						"CreatedBy":   "corkscrew-test",
					},
				},
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *VNetScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}
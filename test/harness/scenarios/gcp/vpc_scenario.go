package gcp

import (
	"fmt"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/pulumi/pulumi-gcp/sdk/v7/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// VPCScenario creates a complete GCP VPC setup with subnets and firewall rules
type VPCScenario struct {
	expectedResources map[string]interface{}
}

// NewVPCScenario creates a new GCP VPC scenario
func NewVPCScenario() automation.Scenario {
	return &VPCScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *VPCScenario) GetName() string {
	return "gcp-vpc"
}

// GetServices returns the GCP services this scenario tests
func (s *VPCScenario) GetServices() []string {
	return []string{"compute"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *VPCScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common labels for all resources
	labels := pulumi.StringMap{
		"test-harness": pulumi.String("true"),
		"test-id":      pulumi.String(testID),
		"scenario":     pulumi.String("gcp-vpc"),
		"created-by":   pulumi.String("corkscrew-test"),
	}

	// Create custom VPC network
	network, err := compute.NewNetwork(ctx, "test-vpc", &compute.NetworkArgs{
		Name:                  pulumi.String(fmt.Sprintf("corkscrew-vpc-%s", testID)),
		AutoCreateSubnetworks: pulumi.Bool(false),
		Description:           pulumi.String("Corkscrew test VPC network"),
	})
	if err != nil {
		return fmt.Errorf("failed to create VPC network: %w", err)
	}

	// Create public subnet
	publicSubnet, err := compute.NewSubnetwork(ctx, "public-subnet", &compute.SubnetworkArgs{
		Name:        pulumi.String(fmt.Sprintf("public-subnet-%s", testID)),
		Network:     network.ID(),
		IpCidrRange: pulumi.String("10.0.1.0/24"),
		Region:      pulumi.String("us-central1"),
		Description: pulumi.String("Public subnet for Corkscrew test"),
	})
	if err != nil {
		return fmt.Errorf("failed to create public subnet: %w", err)
	}

	// Create private subnet
	privateSubnet, err := compute.NewSubnetwork(ctx, "private-subnet", &compute.SubnetworkArgs{
		Name:                  pulumi.String(fmt.Sprintf("private-subnet-%s", testID)),
		Network:               network.ID(),
		IpCidrRange:           pulumi.String("10.0.2.0/24"),
		Region:                pulumi.String("us-central1"),
		Description:           pulumi.String("Private subnet for Corkscrew test"),
		PrivateIpGoogleAccess: pulumi.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to create private subnet: %w", err)
	}

	// Create NAT gateway for private subnet
	router, err := compute.NewRouter(ctx, "test-router", &compute.RouterArgs{
		Name:    pulumi.String(fmt.Sprintf("corkscrew-router-%s", testID)),
		Region:  pulumi.String("us-central1"),
		Network: network.ID(),
	})
	if err != nil {
		return fmt.Errorf("failed to create router: %w", err)
	}

	// Create external IP for NAT gateway
	natIP, err := compute.NewAddress(ctx, "nat-ip", &compute.AddressArgs{
		Name:   pulumi.String(fmt.Sprintf("corkscrew-nat-ip-%s", testID)),
		Region: pulumi.String("us-central1"),
	})
	if err != nil {
		return fmt.Errorf("failed to create NAT IP: %w", err)
	}

	// Create NAT gateway
	nat, err := compute.NewRouterNat(ctx, "test-nat", &compute.RouterNatArgs{
		Name:                pulumi.String(fmt.Sprintf("corkscrew-nat-%s", testID)),
		Router:              router.Name,
		Region:              router.Region,
		NatIpAllocateOption: pulumi.String("MANUAL_ONLY"),
		NatIps: pulumi.StringArray{
			natIP.SelfLink,
		},
		SourceSubnetworkIpRangesToNat: pulumi.String("LIST_OF_SUBNETWORKS"),
		Subnetworks: compute.RouterNatSubnetworkArray{
			&compute.RouterNatSubnetworkArgs{
				Name: privateSubnet.ID(),
				SourceIpRangesToNats: pulumi.StringArray{
					pulumi.String("ALL_IP_RANGES"),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create NAT gateway: %w", err)
	}

	// Create firewall rule for internal traffic
	internalFirewall, err := compute.NewFirewall(ctx, "internal-firewall", &compute.FirewallArgs{
		Name:    pulumi.String(fmt.Sprintf("corkscrew-internal-%s", testID)),
		Network: network.Name,
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("icmp"),
			},
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports: pulumi.StringArray{
					pulumi.String("0-65535"),
				},
			},
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("udp"),
				Ports: pulumi.StringArray{
					pulumi.String("0-65535"),
				},
			},
		},
		SourceRanges: pulumi.StringArray{
			pulumi.String("10.0.0.0/16"),
		},
		Description: pulumi.String("Allow internal traffic within VPC"),
		Priority:    pulumi.Int(1000),
	})
	if err != nil {
		return fmt.Errorf("failed to create internal firewall rule: %w", err)
	}

	// Create firewall rule for SSH access
	sshFirewall, err := compute.NewFirewall(ctx, "ssh-firewall", &compute.FirewallArgs{
		Name:    pulumi.String(fmt.Sprintf("corkscrew-ssh-%s", testID)),
		Network: network.Name,
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports: pulumi.StringArray{
					pulumi.String("22"),
				},
			},
		},
		SourceRanges: pulumi.StringArray{
			pulumi.String("0.0.0.0/0"),
		},
		TargetTags: pulumi.StringArray{
			pulumi.String("ssh"),
		},
		Description: pulumi.String("Allow SSH access"),
		Priority:    pulumi.Int(1000),
	})
	if err != nil {
		return fmt.Errorf("failed to create SSH firewall rule: %w", err)
	}

	// Create firewall rule for HTTP/HTTPS access
	webFirewall, err := compute.NewFirewall(ctx, "web-firewall", &compute.FirewallArgs{
		Name:    pulumi.String(fmt.Sprintf("corkscrew-web-%s", testID)),
		Network: network.Name,
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports: pulumi.StringArray{
					pulumi.String("80"),
					pulumi.String("443"),
				},
			},
		},
		SourceRanges: pulumi.StringArray{
			pulumi.String("0.0.0.0/0"),
		},
		TargetTags: pulumi.StringArray{
			pulumi.String("http-server"),
			pulumi.String("https-server"),
		},
		Description: pulumi.String("Allow HTTP and HTTPS access"),
		Priority:    pulumi.Int(1000),
	})
	if err != nil {
		return fmt.Errorf("failed to create web firewall rule: %w", err)
	}

	// Export resource details for verification
	ctx.Export("networkName", network.Name)
	ctx.Export("networkId", network.ID())
	ctx.Export("networkSelfLink", network.SelfLink)
	ctx.Export("publicSubnetName", publicSubnet.Name)
	ctx.Export("publicSubnetId", publicSubnet.ID())
	ctx.Export("privateSubnetName", privateSubnet.Name)
	ctx.Export("privateSubnetId", privateSubnet.ID())
	ctx.Export("routerName", router.Name)
	ctx.Export("routerId", router.ID())
	ctx.Export("natName", nat.Name)
	ctx.Export("natId", nat.ID())
	ctx.Export("natIPAddress", natIP.Address)
	ctx.Export("internalFirewallName", internalFirewall.Name)
	ctx.Export("sshFirewallName", sshFirewall.Name)
	ctx.Export("webFirewallName", webFirewall.Name)

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"compute": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("Network"),
				"name": network.Name,
				"id":   network.ID(),
				"attributes": pulumi.Map{
					"auto_create_subnetworks": pulumi.Bool(false),
					"routing_mode":            pulumi.String("REGIONAL"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Subnetwork"),
				"name": publicSubnet.Name,
				"id":   publicSubnet.ID(),
				"attributes": pulumi.Map{
					"ip_cidr_range":            pulumi.String("10.0.1.0/24"),
					"region":                   pulumi.String("us-central1"),
					"private_ip_google_access": pulumi.Bool(false),
					"subnet_type":              pulumi.String("public"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Subnetwork"),
				"name": privateSubnet.Name,
				"id":   privateSubnet.ID(),
				"attributes": pulumi.Map{
					"ip_cidr_range":            pulumi.String("10.0.2.0/24"),
					"region":                   pulumi.String("us-central1"),
					"private_ip_google_access": pulumi.Bool(true),
					"subnet_type":              pulumi.String("private"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Router"),
				"name": router.Name,
				"id":   router.ID(),
				"attributes": pulumi.Map{
					"region": pulumi.String("us-central1"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("RouterNat"),
				"name": nat.Name,
				"id":   nat.ID(),
				"attributes": pulumi.Map{
					"nat_ip_allocate_option": pulumi.String("MANUAL_ONLY"),
					"region":                 pulumi.String("us-central1"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Address"),
				"name": natIP.Name,
				"id":   natIP.ID(),
				"attributes": pulumi.Map{
					"address_type": pulumi.String("EXTERNAL"),
					"region":       pulumi.String("us-central1"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Firewall"),
				"name": internalFirewall.Name,
				"id":   internalFirewall.ID(),
				"attributes": pulumi.Map{
					"direction":     pulumi.String("INGRESS"),
					"priority":      pulumi.Int(1000),
					"source_ranges": pulumi.StringArray{pulumi.String("10.0.0.0/16")},
					"firewall_type": pulumi.String("internal"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Firewall"),
				"name": sshFirewall.Name,
				"id":   sshFirewall.ID(),
				"attributes": pulumi.Map{
					"direction":     pulumi.String("INGRESS"),
					"priority":      pulumi.Int(1000),
					"source_ranges": pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					"target_tags":   pulumi.StringArray{pulumi.String("ssh")},
					"firewall_type": pulumi.String("ssh"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Firewall"),
				"name": webFirewall.Name,
				"id":   webFirewall.ID(),
				"attributes": pulumi.Map{
					"direction":     pulumi.String("INGRESS"),
					"priority":      pulumi.Int(1000),
					"source_ranges": pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					"target_tags":   pulumi.StringArray{pulumi.String("http-server"), pulumi.String("https-server")},
					"firewall_type": pulumi.String("web"),
				},
			},
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"compute": []interface{}{
				map[string]interface{}{
					"type": "Network",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"auto_create_subnetworks": false,
						"routing_mode":            "REGIONAL",
					},
				},
				map[string]interface{}{
					"type": "Subnetwork",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"ip_cidr_range":            "10.0.1.0/24",
						"region":                   "us-central1",
						"private_ip_google_access": false,
						"subnet_type":              "public",
					},
				},
				map[string]interface{}{
					"type": "Subnetwork",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"ip_cidr_range":            "10.0.2.0/24",
						"region":                   "us-central1",
						"private_ip_google_access": true,
						"subnet_type":              "private",
					},
				},
				map[string]interface{}{
					"type": "Router",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"region": "us-central1",
					},
				},
				map[string]interface{}{
					"type": "RouterNat",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"nat_ip_allocate_option": "MANUAL_ONLY",
						"region":                 "us-central1",
					},
				},
				map[string]interface{}{
					"type": "Address",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"address_type": "EXTERNAL",
						"region":       "us-central1",
					},
				},
				map[string]interface{}{
					"type": "Firewall",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"direction":     "INGRESS",
						"priority":      1000,
						"source_ranges": []string{"10.0.0.0/16"},
						"firewall_type": "internal",
					},
				},
				map[string]interface{}{
					"type": "Firewall",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"direction":     "INGRESS",
						"priority":      1000,
						"source_ranges": []string{"0.0.0.0/0"},
						"target_tags":   []string{"ssh"},
						"firewall_type": "ssh",
					},
				},
				map[string]interface{}{
					"type": "Firewall",
					"name": "", // Will be filled by output
					"id":   "", // Will be filled by output
					"attributes": map[string]interface{}{
						"direction":     "INGRESS",
						"priority":      1000,
						"source_ranges": []string{"0.0.0.0/0"},
						"target_tags":   []string{"http-server", "https-server"},
						"firewall_type": "web",
					},
				},
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *VPCScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

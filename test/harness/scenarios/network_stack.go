package scenarios

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NetworkStackScenario creates VPC, subnets, security groups, and NAT gateway
type NetworkStackScenario struct {
	expectedResources map[string]interface{}
}

// NewNetworkStackScenario creates a new network stack scenario
func NewNetworkStackScenario() *NetworkStackScenario {
	return &NetworkStackScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *NetworkStackScenario) GetName() string {
	return "network-stack"
}

// GetServices returns the AWS services this scenario tests
func (s *NetworkStackScenario) GetServices() []string {
	return []string{"ec2"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *NetworkStackScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("network-stack"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create VPC
	vpc, err := ec2.NewVpc(ctx, "test-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String("10.0.0.0/16"),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create VPC: %w", err)
	}

	// Create Internet Gateway
	igw, err := ec2.NewInternetGateway(ctx, "test-igw", &ec2.InternetGatewayArgs{
		VpcId: vpc.ID(),
		Tags:  tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create Internet Gateway: %w", err)
	}

	// Create public subnet
	publicSubnet, err := ec2.NewSubnet(ctx, "public-subnet", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.0.1.0/24"),
		AvailabilityZone:    pulumi.String("us-east-1a"),
		MapPublicIpOnLaunch: pulumi.Bool(true),
		Tags: pulumi.StringMap{
			"TestHarness": pulumi.String("true"),
			"TestID":      pulumi.String(testID),
			"Scenario":    pulumi.String("network-stack"),
			"CreatedBy":   pulumi.String("corkscrew-test"),
			"Type":        pulumi.String("public"),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create public subnet: %w", err)
	}

	// Create private subnet
	privateSubnet, err := ec2.NewSubnet(ctx, "private-subnet", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String("10.0.2.0/24"),
		AvailabilityZone: pulumi.String("us-east-1a"),
		Tags: pulumi.StringMap{
			"TestHarness": pulumi.String("true"),
			"TestID":      pulumi.String(testID),
			"Scenario":    pulumi.String("network-stack"),
			"CreatedBy":   pulumi.String("corkscrew-test"),
			"Type":        pulumi.String("private"),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create private subnet: %w", err)
	}

	// Create Elastic IP for NAT Gateway
	eip, err := ec2.NewEip(ctx, "nat-eip", &ec2.EipArgs{
		Domain: pulumi.String("vpc"),
		Tags:   tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create Elastic IP: %w", err)
	}

	// Create NAT Gateway
	natGw, err := ec2.NewNatGateway(ctx, "nat-gateway", &ec2.NatGatewayArgs{
		AllocationId: eip.ID(),
		SubnetId:     publicSubnet.ID(),
		Tags:         tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create NAT Gateway: %w", err)
	}

	// Create web security group
	webSg, err := ec2.NewSecurityGroup(ctx, "web-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Web tier security group"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("HTTP"),
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(80),
				ToPort:      pulumi.Int(80),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
			&ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("HTTPS"),
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(443),
				ToPort:      pulumi.Int(443),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Protocol:   pulumi.String("-1"),
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create web security group: %w", err)
	}

	// Create database security group
	dbSg, err := ec2.NewSecurityGroup(ctx, "db-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Database tier security group"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Description:    pulumi.String("MySQL/Aurora"),
				Protocol:       pulumi.String("tcp"),
				FromPort:       pulumi.Int(3306),
				ToPort:         pulumi.Int(3306),
				SecurityGroups: pulumi.StringArray{webSg.ID()},
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create database security group: %w", err)
	}

	// Create public route table
	publicRt, err := ec2.NewRouteTable(ctx, "public-rt", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock: pulumi.String("0.0.0.0/0"),
				GatewayId: igw.ID(),
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create public route table: %w", err)
	}

	// Create private route table
	privateRt, err := ec2.NewRouteTable(ctx, "private-rt", &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock:    pulumi.String("0.0.0.0/0"),
				NatGatewayId: natGw.ID(),
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create private route table: %w", err)
	}

	// Associate subnets with route tables
	_, err = ec2.NewRouteTableAssociation(ctx, "public-rta", &ec2.RouteTableAssociationArgs{
		SubnetId:     publicSubnet.ID(),
		RouteTableId: publicRt.ID(),
	})
	if err != nil {
		return fmt.Errorf("failed to associate public subnet with route table: %w", err)
	}

	_, err = ec2.NewRouteTableAssociation(ctx, "private-rta", &ec2.RouteTableAssociationArgs{
		SubnetId:     privateSubnet.ID(),
		RouteTableId: privateRt.ID(),
	})
	if err != nil {
		return fmt.Errorf("failed to associate private subnet with route table: %w", err)
	}

	// Export resource details for verification
	ctx.Export("vpcId", vpc.ID())
	ctx.Export("vpcCidr", vpc.CidrBlock)
	ctx.Export("publicSubnetId", publicSubnet.ID())
	ctx.Export("privateSubnetId", privateSubnet.ID())
	ctx.Export("webSgId", webSg.ID())
	ctx.Export("dbSgId", dbSg.ID())
	ctx.Export("natGatewayId", natGw.ID())
	ctx.Export("internetGatewayId", igw.ID())

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"ec2": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("VPC"),
				"id":   vpc.ID(),
				"attributes": pulumi.Map{
					"cidr_block":           pulumi.String("10.0.0.0/16"),
					"enable_dns_hostnames": pulumi.Bool(true),
					"enable_dns_support":   pulumi.Bool(true),
				},
				"tags": tags,
			},
			pulumi.Map{
				"type": pulumi.String("Subnet"),
				"id":   publicSubnet.ID(),
				"attributes": pulumi.Map{
					"cidr_block":              pulumi.String("10.0.1.0/24"),
					"map_public_ip_on_launch": pulumi.Bool(true),
					"availability_zone":       pulumi.String("us-east-1a"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Subnet"),
				"id":   privateSubnet.ID(),
				"attributes": pulumi.Map{
					"cidr_block":        pulumi.String("10.0.2.0/24"),
					"availability_zone": pulumi.String("us-east-1a"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("SecurityGroup"),
				"id":   webSg.ID(),
				"attributes": pulumi.Map{
					"description": pulumi.String("Web tier security group"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("SecurityGroup"),
				"id":   dbSg.ID(),
				"attributes": pulumi.Map{
					"description": pulumi.String("Database tier security group"),
				},
			},
		},
	})

	// Export relationships for verification
	ctx.Export("relationships", pulumi.Map{
		"vpc_to_subnets": pulumi.Map{
			"from": vpc.ID(),
			"to":   pulumi.Array{publicSubnet.ID(), privateSubnet.ID()},
			"type": pulumi.String("contains"),
		},
		"sg_to_sg": pulumi.Map{
			"from": dbSg.ID(),
			"to":   webSg.ID(),
			"type": pulumi.String("allows_access_from"),
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"ec2": []interface{}{
				map[string]interface{}{
					"type": "VPC",
					"attributes": map[string]interface{}{
						"cidr_block":           "10.0.0.0/16",
						"enable_dns_hostnames": true,
						"enable_dns_support":   true,
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "network-stack",
						"CreatedBy":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "Subnet",
					"attributes": map[string]interface{}{
						"cidr_block":              "10.0.1.0/24",
						"map_public_ip_on_launch": true,
						"availability_zone":       "us-east-1a",
					},
				},
				map[string]interface{}{
					"type": "Subnet",
					"attributes": map[string]interface{}{
						"cidr_block":        "10.0.2.0/24",
						"availability_zone": "us-east-1a",
					},
				},
				map[string]interface{}{
					"type": "SecurityGroup",
					"attributes": map[string]interface{}{
						"description": "Web tier security group",
					},
				},
				map[string]interface{}{
					"type": "SecurityGroup",
					"attributes": map[string]interface{}{
						"description": "Database tier security group",
					},
				},
			},
		},
		"relationships": map[string]interface{}{
			"vpc_to_subnets": map[string]interface{}{
				"type": "contains",
			},
			"sg_to_sg": map[string]interface{}{
				"type": "allows_access_from",
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *NetworkStackScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

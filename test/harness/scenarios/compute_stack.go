package scenarios

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/autoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/elb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ComputeStackScenario creates EC2 instances, auto-scaling group, and load balancer
type ComputeStackScenario struct {
	expectedResources map[string]interface{}
}

// NewComputeStackScenario creates a new compute stack scenario
func NewComputeStackScenario() *ComputeStackScenario {
	return &ComputeStackScenario{
		expectedResources: make(map[string]interface{}),
	}
}

// GetName returns the scenario name
func (s *ComputeStackScenario) GetName() string {
	return "compute-stack"
}

// GetServices returns the AWS services this scenario tests
func (s *ComputeStackScenario) GetServices() []string {
	return []string{"ec2", "elb", "autoscaling"}
}

// DefineResources creates the Pulumi resources for this scenario
func (s *ComputeStackScenario) DefineResources(ctx *pulumi.Context, testID string) error {
	// Common tags for all resources
	tags := pulumi.StringMap{
		"TestHarness": pulumi.String("true"),
		"TestID":      pulumi.String(testID),
		"Scenario":    pulumi.String("compute-stack"),
		"CreatedBy":   pulumi.String("corkscrew-test"),
	}

	// Create VPC (minimal for compute resources)
	vpc, err := ec2.NewVpc(ctx, "compute-vpc", &ec2.VpcArgs{
		CidrBlock: pulumi.String("10.1.0.0/16"),
		Tags:      tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create VPC: %w", err)
	}

	// Create Internet Gateway
	igw, err := ec2.NewInternetGateway(ctx, "compute-igw", &ec2.InternetGatewayArgs{
		VpcId: vpc.ID(),
		Tags:  tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create Internet Gateway: %w", err)
	}

	// Create subnet for compute resources
	subnet, err := ec2.NewSubnet(ctx, "compute-subnet", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String("10.1.1.0/24"),
		AvailabilityZone:    pulumi.String("us-east-1a"),
		MapPublicIpOnLaunch: pulumi.Bool(true),
		Tags:                tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w", err)
	}

	// Create security group for web servers
	webSg, err := ec2.NewSecurityGroup(ctx, "web-servers-sg", &ec2.SecurityGroupArgs{
		VpcId:       vpc.ID(),
		Description: pulumi.String("Security group for web servers"),
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("HTTP"),
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(80),
				ToPort:      pulumi.Int(80),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
			&ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("SSH"),
				Protocol:    pulumi.String("tcp"),
				FromPort:    pulumi.Int(22),
				ToPort:      pulumi.Int(22),
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
		return fmt.Errorf("failed to create security group: %w", err)
	}

	// Create route table
	rt, err := ec2.NewRouteTable(ctx, "compute-rt", &ec2.RouteTableArgs{
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
		return fmt.Errorf("failed to create route table: %w", err)
	}

	// Associate subnet with route table
	_, err = ec2.NewRouteTableAssociation(ctx, "compute-rta", &ec2.RouteTableAssociationArgs{
		SubnetId:     subnet.ID(),
		RouteTableId: rt.ID(),
	})
	if err != nil {
		return fmt.Errorf("failed to associate subnet with route table: %w", err)
	}

	// Get latest Amazon Linux 2 AMI
	amiId := pulumi.String("ami-0c55b159cbfafe1f0") // Amazon Linux 2

	// Create launch template for auto scaling group
	launchTemplate, err := ec2.NewLaunchTemplate(ctx, "web-launch-template", &ec2.LaunchTemplateArgs{
		ImageId:      amiId,
		InstanceType: pulumi.String("t2.micro"),
		VpcSecurityGroupIds: pulumi.StringArray{
			webSg.ID(),
		},
		UserData: pulumi.String(`#!/bin/bash
yum update -y
yum install -y httpd
systemctl start httpd
systemctl enable httpd
echo "<h1>Test Instance</h1>" > /var/www/html/index.html
`),
		TagSpecifications: ec2.LaunchTemplateTagSpecificationArray{
			&ec2.LaunchTemplateTagSpecificationArgs{
				ResourceType: pulumi.String("instance"),
				Tags:         tags,
			},
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create launch template: %w", err)
	}

	// Create standalone EC2 instance for testing
	instance, err := ec2.NewInstance(ctx, "test-instance", &ec2.InstanceArgs{
		Ami:                 amiId,
		InstanceType:        pulumi.String("t2.micro"),
		SubnetId:            subnet.ID(),
		VpcSecurityGroupIds: pulumi.StringArray{webSg.ID()},
		UserData: pulumi.String(`#!/bin/bash
yum update -y
yum install -y httpd
systemctl start httpd
systemctl enable httpd
echo "<h1>Standalone Test Instance</h1>" > /var/www/html/index.html
`),
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create EC2 instance: %w", err)
	}

	// Create Classic Load Balancer
	elb, err := elb.NewLoadBalancer(ctx, "web-elb", &elb.LoadBalancerArgs{
		Subnets:        pulumi.StringArray{subnet.ID()},
		SecurityGroups: pulumi.StringArray{webSg.ID()},
		Listeners: elb.LoadBalancerListenerArray{
			&elb.LoadBalancerListenerArgs{
				InstancePort:     pulumi.Int(80),
				InstanceProtocol: pulumi.String("http"),
				LbPort:           pulumi.Int(80),
				LbProtocol:       pulumi.String("http"),
			},
		},
		HealthCheck: &elb.LoadBalancerHealthCheckArgs{
			Target:             pulumi.String("HTTP:80/"),
			Interval:           pulumi.Int(30),
			Timeout:            pulumi.Int(5),
			HealthyThreshold:   pulumi.Int(2),
			UnhealthyThreshold: pulumi.Int(2),
		},
		Tags: tags,
	})
	if err != nil {
		return fmt.Errorf("failed to create load balancer: %w", err)
	}

	// Create Auto Scaling Group
	asg, err := autoscaling.NewGroup(ctx, "web-asg", &autoscaling.GroupArgs{
		MinSize:            pulumi.Int(1),
		MaxSize:            pulumi.Int(3),
		DesiredCapacity:    pulumi.Int(2),
		VpcZoneIdentifiers: pulumi.StringArray{subnet.ID()},
		LoadBalancers:      pulumi.StringArray{elb.Name},
		LaunchTemplate: &autoscaling.GroupLaunchTemplateArgs{
			Id:      launchTemplate.ID(),
			Version: pulumi.String("$Latest"),
		},
		Tags: autoscaling.GroupTagArray{
			&autoscaling.GroupTagArgs{
				Key:               pulumi.String("TestHarness"),
				Value:             pulumi.String("true"),
				PropagateAtLaunch: pulumi.Bool(true),
			},
			&autoscaling.GroupTagArgs{
				Key:               pulumi.String("TestID"),
				Value:             pulumi.String(testID),
				PropagateAtLaunch: pulumi.Bool(true),
			},
			&autoscaling.GroupTagArgs{
				Key:               pulumi.String("Scenario"),
				Value:             pulumi.String("compute-stack"),
				PropagateAtLaunch: pulumi.Bool(true),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create auto scaling group: %w", err)
	}

	// Export resource details for verification
	ctx.Export("vpcId", vpc.ID())
	ctx.Export("subnetId", subnet.ID())
	ctx.Export("securityGroupId", webSg.ID())
	ctx.Export("instanceId", instance.ID())
	ctx.Export("launchTemplateId", launchTemplate.ID())
	ctx.Export("loadBalancerName", elb.Name)
	ctx.Export("loadBalancerDnsName", elb.DnsName)
	ctx.Export("autoScalingGroupName", asg.Name)

	// Export expected resources for verification
	ctx.Export("expectedResources", pulumi.Map{
		"ec2": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("VPC"),
				"id":   vpc.ID(),
				"attributes": pulumi.Map{
					"cidr_block": pulumi.String("10.1.0.0/16"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("Instance"),
				"id":   instance.ID(),
				"attributes": pulumi.Map{
					"instance_type": pulumi.String("t2.micro"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("SecurityGroup"),
				"id":   webSg.ID(),
				"attributes": pulumi.Map{
					"description": pulumi.String("Security group for web servers"),
				},
			},
			pulumi.Map{
				"type": pulumi.String("LaunchTemplate"),
				"id":   launchTemplate.ID(),
				"attributes": pulumi.Map{
					"instance_type": pulumi.String("t2.micro"),
				},
			},
		},
		"elb": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("LoadBalancer"),
				"name": elb.Name,
				"attributes": pulumi.Map{
					"dns_name": elb.DnsName,
				},
			},
		},
		"autoscaling": pulumi.Array{
			pulumi.Map{
				"type": pulumi.String("AutoScalingGroup"),
				"name": asg.Name,
				"attributes": pulumi.Map{
					"min_size":         pulumi.Int(1),
					"max_size":         pulumi.Int(3),
					"desired_capacity": pulumi.Int(2),
				},
			},
		},
	})

	// Export relationships for verification
	ctx.Export("relationships", pulumi.Map{
		"instance_to_sg": pulumi.Map{
			"from": instance.ID(),
			"to":   webSg.ID(),
			"type": pulumi.String("protected_by"),
		},
		"asg_to_elb": pulumi.Map{
			"from": asg.Name,
			"to":   elb.Name,
			"type": pulumi.String("registers_with"),
		},
	})

	// Store expected resources for later verification
	s.expectedResources = map[string]interface{}{
		"expectedResources": map[string]interface{}{
			"ec2": []interface{}{
				map[string]interface{}{
					"type": "VPC",
					"attributes": map[string]interface{}{
						"cidr_block": "10.1.0.0/16",
					},
					"tags": map[string]string{
						"TestHarness": "true",
						"TestID":      testID,
						"Scenario":    "compute-stack",
						"CreatedBy":   "corkscrew-test",
					},
				},
				map[string]interface{}{
					"type": "Instance",
					"attributes": map[string]interface{}{
						"instance_type": "t2.micro",
					},
				},
				map[string]interface{}{
					"type": "SecurityGroup",
					"attributes": map[string]interface{}{
						"description": "Security group for web servers",
					},
				},
				map[string]interface{}{
					"type": "LaunchTemplate",
					"attributes": map[string]interface{}{
						"instance_type": "t2.micro",
					},
				},
			},
			"elb": []interface{}{
				map[string]interface{}{
					"type": "LoadBalancer",
				},
			},
			"autoscaling": []interface{}{
				map[string]interface{}{
					"type": "AutoScalingGroup",
					"attributes": map[string]interface{}{
						"min_size":         1,
						"max_size":         3,
						"desired_capacity": 2,
					},
				},
			},
		},
		"relationships": map[string]interface{}{
			"instance_to_sg": map[string]interface{}{
				"type": "protected_by",
			},
			"asg_to_elb": map[string]interface{}{
				"type": "registers_with",
			},
		},
	}

	return nil
}

// GetExpectedResources returns the expected resources for verification
func (s *ComputeStackScenario) GetExpectedResources() map[string]interface{} {
	return s.expectedResources
}

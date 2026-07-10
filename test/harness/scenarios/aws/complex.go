//go:build integration

package main

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Create VPC
		vpc, err := ec2.NewVpc(ctx, "complex-vpc", &ec2.VpcArgs{
			CidrBlock:          pulumi.String("10.0.0.0/16"),
			EnableDnsHostnames: pulumi.Bool(true),
			EnableDnsSupport:   pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name":        pulumi.String("complex-vpc"),
				"Environment": pulumi.String("test"),
				"Scenario":    pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Create Internet Gateway
		igw, err := ec2.NewInternetGateway(ctx, "complex-igw", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-igw"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Create public subnet
		publicSubnet, err := ec2.NewSubnet(ctx, "complex-public-subnet", &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String("10.0.1.0/24"),
			AvailabilityZone:    pulumi.String("us-east-1a"),
			MapPublicIpOnLaunch: pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-public-subnet"),
				"Type":     pulumi.String("public"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Create private subnet
		privateSubnet, err := ec2.NewSubnet(ctx, "complex-private-subnet", &ec2.SubnetArgs{
			VpcId:            vpc.ID(),
			CidrBlock:        pulumi.String("10.0.2.0/24"),
			AvailabilityZone: pulumi.String("us-east-1b"),
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-private-subnet"),
				"Type":     pulumi.String("private"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Create route table for public subnet
		publicRouteTable, err := ec2.NewRouteTable(ctx, "complex-public-rt", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Routes: ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				},
			},
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-public-rt"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Associate route table with public subnet
		_, err = ec2.NewRouteTableAssociation(ctx, "complex-public-rta", &ec2.RouteTableAssociationArgs{
			SubnetId:     publicSubnet.ID(),
			RouteTableId: publicRouteTable.ID(),
		})
		if err != nil {
			return err
		}

		// Create security group
		securityGroup, err := ec2.NewSecurityGroup(ctx, "complex-sg", &ec2.SecurityGroupArgs{
			Name:        pulumi.String("complex-security-group"),
			Description: pulumi.String("Security group for complex scenario"),
			VpcId:       vpc.ID(),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(22),
					ToPort:      pulumi.Int(22),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("SSH access"),
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(80),
					ToPort:      pulumi.Int(80),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("HTTP access"),
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(443),
					ToPort:      pulumi.Int(443),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("HTTPS access"),
				},
			},
			Egress: ec2.SecurityGroupEgressArray{
				&ec2.SecurityGroupEgressArgs{
					Protocol:    pulumi.String("-1"),
					FromPort:    pulumi.Int(0),
					ToPort:      pulumi.Int(0),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("Allow all outbound traffic"),
				},
			},
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-sg"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Create IAM role for EC2
		assumeRolePolicy, err := json.Marshal(map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Action": "sts:AssumeRole",
					"Principal": map[string]interface{}{
						"Service": "ec2.amazonaws.com",
					},
					"Effect": "Allow",
					"Sid":    "",
				},
			},
		})
		if err != nil {
			return err
		}

		ec2Role, err := iam.NewRole(ctx, "complex-ec2-role", &iam.RoleArgs{
			Name:             pulumi.String("complex-ec2-role"),
			AssumeRolePolicy: pulumi.String(assumeRolePolicy),
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-ec2-role"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Attach S3 read-only policy to role
		_, err = iam.NewRolePolicyAttachment(ctx, "complex-role-policy-attachment", &iam.RolePolicyAttachmentArgs{
			Role:      ec2Role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess"),
		})
		if err != nil {
			return err
		}

		// Create instance profile
		instanceProfile, err := iam.NewInstanceProfile(ctx, "complex-instance-profile", &iam.InstanceProfileArgs{
			Name: pulumi.String("complex-instance-profile"),
			Role: ec2Role.Name,
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-instance-profile"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Get latest Amazon Linux 2 AMI
		ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
			MostRecent: pulumi.BoolRef(true),
			Filters: []ec2.GetAmiFilter{
				{
					Name:   "name",
					Values: []string{"amzn2-ami-hvm-*-x86_64-ebs"},
				},
				{
					Name:   "virtualization-type",
					Values: []string{"hvm"},
				},
			},
			Owners: []string{"amazon"},
		})
		if err != nil {
			return err
		}

		// Create EC2 instance
		instance, err := ec2.NewInstance(ctx, "complex-instance", &ec2.InstanceArgs{
			InstanceType:        pulumi.String("t3.micro"),
			Ami:                 pulumi.String(ami.Id),
			SubnetId:            publicSubnet.ID(),
			VpcSecurityGroupIds: pulumi.StringArray{securityGroup.ID()},
			IamInstanceProfile:  instanceProfile.Name,
			UserData: pulumi.String(`#!/bin/bash
echo "Hello from complex scenario!" > /tmp/hello.txt
`),
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-instance"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Create S3 bucket
		bucket, err := s3.NewBucket(ctx, "complex-bucket", &s3.BucketArgs{
			Bucket: pulumi.String(fmt.Sprintf("complex-scenario-bucket-%s", ctx.Stack())),
			Tags: pulumi.StringMap{
				"Name":     pulumi.String("complex-bucket"),
				"Scenario": pulumi.String("complex"),
			},
		})
		if err != nil {
			return err
		}

		// Create bucket versioning
		_, err = s3.NewBucketVersioningV2(ctx, "complex-bucket-versioning", &s3.BucketVersioningV2Args{
			Bucket: bucket.ID(),
			VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
				Status: pulumi.String("Enabled"),
			},
		})
		if err != nil {
			return err
		}

		// Create bucket policy allowing EC2 role to read
		bucketPolicyDocument, err := json.Marshal(map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Sid":    "AllowEC2RoleRead",
					"Effect": "Allow",
					"Principal": map[string]interface{}{
						"AWS": ec2Role.Arn,
					},
					"Action": []string{
						"s3:GetObject",
						"s3:ListBucket",
					},
					"Resource": []interface{}{
						pulumi.Sprintf("arn:aws:s3:::%s", bucket.ID()),
						pulumi.Sprintf("arn:aws:s3:::%s/*", bucket.ID()),
					},
				},
			},
		})
		if err != nil {
			return err
		}

		_, err = s3.NewBucketPolicy(ctx, "complex-bucket-policy", &s3.BucketPolicyArgs{
			Bucket: bucket.ID(),
			Policy: pulumi.String(bucketPolicyDocument),
		})
		if err != nil {
			return err
		}

		// Export resource information
		ctx.Export("vpcId", vpc.ID())
		ctx.Export("vpcArn", vpc.Arn)
		ctx.Export("vpcCidrBlock", vpc.CidrBlock)

		ctx.Export("internetGatewayId", igw.ID())
		ctx.Export("internetGatewayArn", igw.Arn)

		ctx.Export("publicSubnetId", publicSubnet.ID())
		ctx.Export("publicSubnetArn", publicSubnet.Arn)
		ctx.Export("publicSubnetCidrBlock", publicSubnet.CidrBlock)

		ctx.Export("privateSubnetId", privateSubnet.ID())
		ctx.Export("privateSubnetArn", privateSubnet.Arn)
		ctx.Export("privateSubnetCidrBlock", privateSubnet.CidrBlock)

		ctx.Export("securityGroupId", securityGroup.ID())
		ctx.Export("securityGroupArn", securityGroup.Arn)
		ctx.Export("securityGroupName", securityGroup.Name)

		ctx.Export("instanceId", instance.ID())
		ctx.Export("instanceArn", instance.Arn)
		ctx.Export("instancePublicIp", instance.PublicIp)
		ctx.Export("instancePrivateIp", instance.PrivateIp)
		ctx.Export("instanceState", instance.InstanceState)

		ctx.Export("bucketId", bucket.ID())
		ctx.Export("bucketArn", bucket.Arn)
		ctx.Export("bucketDomainName", bucket.BucketDomainName)

		ctx.Export("iamRoleId", ec2Role.ID())
		ctx.Export("iamRoleArn", ec2Role.Arn)
		ctx.Export("iamRoleName", ec2Role.Name)

		ctx.Export("instanceProfileId", instanceProfile.ID())
		ctx.Export("instanceProfileArn", instanceProfile.Arn)

		// Export expected relationships for verification
		ctx.Export("expectedRelationships", pulumi.Map{
			"vpc_to_subnets": pulumi.StringArray{
				publicSubnet.ID(),
				privateSubnet.ID(),
			},
			"vpc_to_security_group":   securityGroup.ID(),
			"vpc_to_internet_gateway": igw.ID(),
			"subnet_to_instance": pulumi.Map{
				"subnet":   publicSubnet.ID(),
				"instance": instance.ID(),
			},
			"security_group_to_instance": pulumi.Map{
				"security_group": securityGroup.ID(),
				"instance":       instance.ID(),
			},
			"role_to_instance": pulumi.Map{
				"role":     ec2Role.ID(),
				"instance": instance.ID(),
			},
			"role_to_bucket_policy": pulumi.Map{
				"role":   ec2Role.ID(),
				"bucket": bucket.ID(),
			},
			"instance_profile_to_role": pulumi.Map{
				"profile": instanceProfile.ID(),
				"role":    ec2Role.ID(),
			},
			"route_table_to_subnet": pulumi.Map{
				"route_table": publicRouteTable.ID(),
				"subnet":      publicSubnet.ID(),
			},
		})

		// Export resource counts for verification
		ctx.Export("resourceCounts", pulumi.Map{
			"vpcs":              pulumi.Int(1),
			"subnets":           pulumi.Int(2),
			"security_groups":   pulumi.Int(1),
			"instances":         pulumi.Int(1),
			"buckets":           pulumi.Int(1),
			"iam_roles":         pulumi.Int(1),
			"instance_profiles": pulumi.Int(1),
			"internet_gateways": pulumi.Int(1),
			"route_tables":      pulumi.Int(1),
		})

		return nil
	})
}

package discovery

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestBuildResourceRelationshipGraph(t *testing.T) {
	// Create sample service analysis data
	services := map[string]*ServiceAnalysis{
		"ec2": {
			ServiceName: "ec2",
			ResourceTypes: []ResourceTypeAnalysis{
				{
					Name:            "Instance",
					GoTypeName:      "Instance",
					IdentifierField: "InstanceId",
					Fields: []FieldInfo{
						{Name: "InstanceId", GoType: "string", JSONTag: "instanceId"},
						{Name: "VpcId", GoType: "string", JSONTag: "vpcId"},
						{Name: "SubnetId", GoType: "string", JSONTag: "subnetId"},
						{Name: "SecurityGroupIds", GoType: "[]string", JSONTag: "securityGroupIds", IsList: true},
						{Name: "IamInstanceProfile", GoType: "string", JSONTag: "iamInstanceProfile"},
						{Name: "KeyName", GoType: "string", JSONTag: "keyName"},
					},
				},
				{
					Name:            "Vpc",
					GoTypeName:      "Vpc",
					IdentifierField: "VpcId",
					Fields: []FieldInfo{
						{Name: "VpcId", GoType: "string", JSONTag: "vpcId"},
						{Name: "CidrBlock", GoType: "string", JSONTag: "cidrBlock"},
					},
				},
				{
					Name:            "Subnet",
					GoTypeName:      "Subnet",
					IdentifierField: "SubnetId",
					Fields: []FieldInfo{
						{Name: "SubnetId", GoType: "string", JSONTag: "subnetId"},
						{Name: "VpcId", GoType: "string", JSONTag: "vpcId"},
						{Name: "AvailabilityZone", GoType: "string", JSONTag: "availabilityZone"},
					},
				},
				{
					Name:            "SecurityGroup",
					GoTypeName:      "SecurityGroup",
					IdentifierField: "GroupId",
					Fields: []FieldInfo{
						{Name: "GroupId", GoType: "string", JSONTag: "groupId"},
						{Name: "VpcId", GoType: "string", JSONTag: "vpcId"},
						{Name: "GroupName", GoType: "string", JSONTag: "groupName"},
					},
				},
			},
		},
		"iam": {
			ServiceName: "iam",
			ResourceTypes: []ResourceTypeAnalysis{
				{
					Name:            "Role",
					GoTypeName:      "Role",
					IdentifierField: "RoleName",
					Fields: []FieldInfo{
						{Name: "RoleName", GoType: "string", JSONTag: "roleName"},
						{Name: "Arn", GoType: "string", JSONTag: "arn"},
						{Name: "AssumeRolePolicyDocument", GoType: "string", JSONTag: "assumeRolePolicyDocument"},
					},
				},
				{
					Name:            "InstanceProfile",
					GoTypeName:      "InstanceProfile",
					IdentifierField: "InstanceProfileName",
					Fields: []FieldInfo{
						{Name: "InstanceProfileName", GoType: "string", JSONTag: "instanceProfileName"},
						{Name: "Arn", GoType: "string", JSONTag: "arn"},
						{Name: "Roles", GoType: "[]Role", JSONTag: "roles", IsList: true},
					},
				},
			},
		},
		"rds": {
			ServiceName: "rds",
			ResourceTypes: []ResourceTypeAnalysis{
				{
					Name:            "DBInstance",
					GoTypeName:      "DBInstance",
					IdentifierField: "DBInstanceIdentifier",
					Fields: []FieldInfo{
						{Name: "DBInstanceIdentifier", GoType: "string", JSONTag: "dbInstanceIdentifier"},
						{Name: "DBSubnetGroupName", GoType: "string", JSONTag: "dbSubnetGroupName"},
						{Name: "VpcSecurityGroupIds", GoType: "[]string", JSONTag: "vpcSecurityGroupIds", IsList: true},
					},
				},
				{
					Name:            "DBSubnetGroup",
					GoTypeName:      "DBSubnetGroup",
					IdentifierField: "DBSubnetGroupName",
					Fields: []FieldInfo{
						{Name: "DBSubnetGroupName", GoType: "string", JSONTag: "dbSubnetGroupName"},
						{Name: "SubnetIds", GoType: "[]string", JSONTag: "subnetIds", IsList: true},
						{Name: "VpcId", GoType: "string", JSONTag: "vpcId"},
					},
				},
			},
		},
		"lambda": {
			ServiceName: "lambda",
			ResourceTypes: []ResourceTypeAnalysis{
				{
					Name:            "Function",
					GoTypeName:      "Function",
					IdentifierField: "FunctionName",
					Fields: []FieldInfo{
						{Name: "FunctionName", GoType: "string", JSONTag: "functionName"},
						{Name: "FunctionArn", GoType: "string", JSONTag: "functionArn"},
						{Name: "Role", GoType: "string", JSONTag: "role"},
						{Name: "VpcConfig.SubnetIds", GoType: "[]string", JSONTag: "subnetIds", IsList: true},
						{Name: "VpcConfig.SecurityGroupIds", GoType: "[]string", JSONTag: "securityGroupIds", IsList: true},
					},
				},
			},
		},
	}

	// Build the graph
	graph := BuildResourceRelationshipGraph(services)

	// Test graph structure
	if graph == nil {
		t.Fatal("BuildResourceRelationshipGraph returned nil")
	}

	// Check that all resources are in the graph
	expectedResources := []string{
		"ec2::Instance",
		"ec2::Vpc",
		"ec2::Subnet",
		"ec2::SecurityGroup",
		"iam::Role",
		"iam::InstanceProfile",
		"rds::DBInstance",
		"rds::DBSubnetGroup",
		"lambda::Function",
	}

	for _, expected := range expectedResources {
		if _, exists := graph.Resources[expected]; !exists {
			t.Errorf("Expected resource %s not found in graph", expected)
		}
	}

	// Print relationships for debugging
	fmt.Println("\n=== Discovered Relationships ===")
	for _, rel := range graph.Relationships {
		fmt.Printf("%s::%s -[%s via %s]-> %s::%s (%s)\n",
			rel.SourceService, rel.SourceResource,
			rel.RelationType, rel.SourceField,
			rel.TargetService, rel.TargetResource,
			rel.Cardinality)
	}

	// Test specific relationships
	hasRelationship := func(source, target, field string) bool {
		for _, rel := range graph.Relationships {
			if fmt.Sprintf("%s::%s", rel.SourceService, rel.SourceResource) == source &&
				fmt.Sprintf("%s::%s", rel.TargetService, rel.TargetResource) == target &&
				rel.SourceField == field {
				return true
			}
		}
		return false
	}

	// EC2 Instance relationships
	if !hasRelationship("ec2::Instance", "ec2::Vpc", "VpcId") {
		t.Error("Missing relationship: EC2 Instance -> VPC")
	}
	if !hasRelationship("ec2::Instance", "ec2::Subnet", "SubnetId") {
		t.Error("Missing relationship: EC2 Instance -> Subnet")
	}
	if !hasRelationship("ec2::Instance", "ec2::SecurityGroup", "SecurityGroupIds") {
		t.Error("Missing relationship: EC2 Instance -> SecurityGroup")
	}

	// Cross-service relationships
	if !hasRelationship("ec2::Instance", "iam::InstanceProfile", "IamInstanceProfile") {
		t.Error("Missing relationship: EC2 Instance -> IAM InstanceProfile")
	}
	if !hasRelationship("lambda::Function", "iam::Role", "Role") {
		t.Error("Missing relationship: Lambda Function -> IAM Role")
	}

	// Generate Mermaid diagram
	mermaid := graph.GenerateMermaidDiagram()
	fmt.Println("\n=== Mermaid Diagram ===")
	fmt.Println(mermaid)

	// Generate JSON output
	jsonOutput := graph.ToJSON()
	jsonBytes, _ := json.MarshalIndent(jsonOutput, "", "  ")
	fmt.Println("\n=== JSON Output ===")
	fmt.Println(string(jsonBytes))

	// Test dependency order
	order := graph.GetDependencyOrder()
	fmt.Println("\n=== Dependency Order ===")
	for i, resource := range order {
		fmt.Printf("%d. %s\n", i+1, resource)
	}

	// Verify VPC comes before Instance in dependency order
	vpcIndex := -1
	instanceIndex := -1
	for i, res := range order {
		if res == "ec2::Vpc" {
			vpcIndex = i
		}
		if res == "ec2::Instance" {
			instanceIndex = i
		}
	}

	if vpcIndex > instanceIndex {
		t.Error("VPC should come before Instance in dependency order")
	}
}

func TestCrossServiceRelationships(t *testing.T) {
	// Test with minimal services to verify cross-service detection
	services := map[string]*ServiceAnalysis{
		"ec2": {
			ServiceName: "ec2",
			ResourceTypes: []ResourceTypeAnalysis{
				{Name: "Instance"},
				{Name: "SecurityGroup"},
				{Name: "Subnet"},
			},
		},
		"lambda": {
			ServiceName: "lambda",
			ResourceTypes: []ResourceTypeAnalysis{
				{Name: "Function"},
			},
		},
		"iam": {
			ServiceName: "iam",
			ResourceTypes: []ResourceTypeAnalysis{
				{Name: "Role"},
			},
		},
		"rds": {
			ServiceName: "rds",
			ResourceTypes: []ResourceTypeAnalysis{
				{Name: "DBInstance"},
				{Name: "DBSubnetGroup"},
			},
		},
	}

	graph := BuildResourceRelationshipGraph(services)

	// Count cross-service relationships
	crossServiceCount := 0
	for _, rel := range graph.Relationships {
		if rel.SourceService != rel.TargetService {
			crossServiceCount++
			fmt.Printf("Cross-service: %s::%s -> %s::%s\n",
				rel.SourceService, rel.SourceResource,
				rel.TargetService, rel.TargetResource)
		}
	}

	if crossServiceCount == 0 {
		t.Error("No cross-service relationships detected")
	}

	fmt.Printf("\nTotal cross-service relationships: %d\n", crossServiceCount)
}
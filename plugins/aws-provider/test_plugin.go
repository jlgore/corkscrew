package main

import (
	"context"
	"fmt"
	"log"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// Simple test function to verify the plugin works
func testPlugin() {
	fmt.Println("Testing AWS Provider v2...")
	
	// Create provider
	provider := NewAWSProvider()
	
	// Test initialization
	ctx := context.Background()
	initReq := &pb.InitializeRequest{
		Config:   make(map[string]string),
		CacheDir: "/tmp/corkscrew-cache",
	}
	
	initResp, err := provider.Initialize(ctx, initReq)
	if err != nil {
		log.Printf("Initialize failed: %v", err)
		return
	}
	
	if !initResp.Success {
		log.Printf("Initialize failed: %s", initResp.Error)
		return
	}
	
	fmt.Printf("✓ Initialize succeeded: version %s\n", initResp.Version)
	for k, v := range initResp.Metadata {
		fmt.Printf("  - %s: %s\n", k, v)
	}
	
	// Test provider info
	infoResp, err := provider.GetProviderInfo(ctx, &pb.Empty{})
	if err != nil {
		log.Printf("GetProviderInfo failed: %v", err)
		return
	}
	
	fmt.Printf("✓ Provider info: %s v%s\n", infoResp.Name, infoResp.Version)
	fmt.Printf("  - Description: %s\n", infoResp.Description)
	fmt.Printf("  - Capabilities: %d\n", len(infoResp.Capabilities))
	fmt.Printf("  - Supported services: %d\n", len(infoResp.SupportedServices))
	
	// Test service discovery (should work even without AWS creds)
	discoverReq := &pb.DiscoverServicesRequest{
		ForceRefresh: true,
	}
	
	discoverResp, err := provider.DiscoverServices(ctx, discoverReq)
	if err != nil {
		log.Printf("DiscoverServices failed: %v", err)
		return
	}
	
	fmt.Printf("✓ Discovered %d services\n", len(discoverResp.Services))
	for i, service := range discoverResp.Services {
		if i < 5 { // Show first 5 services
			fmt.Printf("  - %s (%s): %d resource types\n", 
				service.Name, service.DisplayName, len(service.ResourceTypes))
		}
	}
	if len(discoverResp.Services) > 5 {
		fmt.Printf("  ... and %d more\n", len(discoverResp.Services)-5)
	}
	
	// Test schema generation
	schemaReq := &pb.GetSchemasRequest{
		Services: []string{"s3", "ec2"},
	}
	
	schemaResp, err := provider.GetSchemas(ctx, schemaReq)
	if err != nil {
		log.Printf("GetSchemas failed: %v", err)
		return
	}
	
	fmt.Printf("✓ Generated %d schemas\n", len(schemaResp.Schemas))
	for _, schema := range schemaResp.Schemas {
		fmt.Printf("  - %s (%s)\n", schema.Name, schema.Service)
	}
	
	// Test relationship extraction
	fmt.Println("\n🔗 Testing relationship extraction...")
	
	// Create mock resource references for testing
	mockResources := []*pb.ResourceRef{
		{
			Id:      "i-1234567890abcdef0",
			Name:    "test-instance",
			Type:    "AWS::EC2::Instance",
			Service: "ec2",
			Region:  "us-east-1",
			BasicAttributes: map[string]string{
				"vpcid":              "vpc-12345678",
				"subnetid":           "subnet-12345678",
				"securitygroupids":   "sg-12345678",
			},
		},
		{
			Id:      "vol-1234567890abcdef0",
			Name:    "test-volume",
			Type:    "AWS::EC2::Volume",
			Service: "ec2",
			Region:  "us-east-1",
			BasicAttributes: map[string]string{
				"instanceid": "i-1234567890abcdef0",
			},
		},
		{
			Id:      "vpc-12345678",
			Name:    "test-vpc",
			Type:    "AWS::EC2::Vpc",
			Service: "ec2",
			Region:  "us-east-1",
			BasicAttributes: map[string]string{},
		},
	}
	
	// Extract relationships
	relationships := provider.scanner.ExtractRelationships(mockResources)
	fmt.Printf("✓ Extracted %d relationships from mock resources\n", len(relationships))
	
	for i, rel := range relationships {
		if i < 3 { // Show first 3 relationships
			sourceType := rel.Properties["source_type"]
			fmt.Printf("  - %s %s %s (%s)\n", 
				sourceType, rel.RelationshipType, rel.TargetType, rel.Properties["relationship_context"])
		}
	}
	if len(relationships) > 3 {
		fmt.Printf("  ... and %d more\n", len(relationships)-3)
	}
	
	fmt.Println("✅ All tests passed!")
}
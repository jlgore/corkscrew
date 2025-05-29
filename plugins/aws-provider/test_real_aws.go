package main

import (
	"context"
	"fmt"
	"log"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// testRealAWS tests the plugin against real AWS resources
func testRealAWS() {
	fmt.Println("🔍 Testing AWS Provider v2 with Real AWS Resources...")
	
	// Create provider
	provider := NewAWSProvider()
	
	// Test initialization with real AWS credentials
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
	
	fmt.Printf("✓ AWS Provider v2 initialized successfully\n")
	fmt.Printf("  - Region: %s\n", initResp.Metadata["region"])
	fmt.Printf("  - Resource Explorer: %s\n", initResp.Metadata["resource_explorer"])
	
	// Test S3 scanning
	fmt.Println("\n📦 Scanning S3 buckets...")
	s3Req := &pb.ListResourcesRequest{
		Service: "s3",
	}
	
	s3Resp, err := provider.ListResources(ctx, s3Req)
	if err != nil {
		log.Printf("S3 scan failed: %v", err)
	} else {
		fmt.Printf("✓ Found %d S3 buckets\n", len(s3Resp.Resources))
		for i, bucket := range s3Resp.Resources {
			if i < 5 { // Show first 5 buckets
				fmt.Printf("  - %s (%s)\n", bucket.Name, bucket.Id)
			}
		}
		if len(s3Resp.Resources) > 5 {
			fmt.Printf("  ... and %d more\n", len(s3Resp.Resources)-5)
		}
	}
	
	// Test DynamoDB scanning
	fmt.Println("\n🗃️  Scanning DynamoDB tables...")
	dynamoReq := &pb.ListResourcesRequest{
		Service: "dynamodb",
	}
	
	dynamoResp, err := provider.ListResources(ctx, dynamoReq)
	if err != nil {
		log.Printf("DynamoDB scan failed: %v", err)
	} else {
		fmt.Printf("✓ Found %d DynamoDB tables\n", len(dynamoResp.Resources))
		for i, table := range dynamoResp.Resources {
			if i < 5 { // Show first 5 tables
				fmt.Printf("  - %s (%s)\n", table.Name, table.Id)
			}
		}
		if len(dynamoResp.Resources) > 5 {
			fmt.Printf("  ... and %d more\n", len(dynamoResp.Resources)-5)
		}
	}
	
	// Test batch scanning of both services
	fmt.Println("\n🔄 Batch scanning S3 and DynamoDB...")
	batchReq := &pb.BatchScanRequest{
		Services: []string{"s3", "dynamodb"},
		IncludeRelationships: true,
	}
	
	batchResp, err := provider.BatchScan(ctx, batchReq)
	if err != nil {
		log.Printf("Batch scan failed: %v", err)
	} else {
		fmt.Printf("✓ Batch scan completed in %dms\n", batchResp.Stats.DurationMs)
		fmt.Printf("  - Total resources: %d\n", batchResp.Stats.TotalResources)
		fmt.Printf("  - S3 resources: %d\n", batchResp.Stats.ServiceCounts["s3"])
		fmt.Printf("  - DynamoDB resources: %d\n", batchResp.Stats.ServiceCounts["dynamodb"])
		
		// Count relationships
		relationshipCount := 0
		for _, resource := range batchResp.Resources {
			relationshipCount += len(resource.Relationships)
		}
		fmt.Printf("  - Relationships discovered: %d\n", relationshipCount)
		
		if len(batchResp.Errors) > 0 {
			fmt.Printf("  - Errors: %d\n", len(batchResp.Errors))
			for i, errMsg := range batchResp.Errors {
				if i < 3 { // Show first 3 errors
					fmt.Printf("    - %s\n", errMsg)
				}
			}
		}
	}
	
	// Test describing a specific resource if we found any
	if s3Resp != nil && len(s3Resp.Resources) > 0 {
		fmt.Println("\n🔍 Describing first S3 bucket...")
		descReq := &pb.DescribeResourceRequest{
			ResourceRef: s3Resp.Resources[0],
			IncludeRelationships: true,
		}
		
		descResp, err := provider.DescribeResource(ctx, descReq)
		if err != nil {
			log.Printf("Describe resource failed: %v", err)
		} else if descResp.Error != "" {
			log.Printf("Describe resource error: %s", descResp.Error)
		} else {
			resource := descResp.Resource
			fmt.Printf("✓ Resource details:\n")
			fmt.Printf("  - ID: %s\n", resource.Id)
			fmt.Printf("  - Name: %s\n", resource.Name)
			fmt.Printf("  - Type: %s\n", resource.Type)
			fmt.Printf("  - Region: %s\n", resource.Region)
			fmt.Printf("  - Tags: %d\n", len(resource.Tags))
			fmt.Printf("  - Relationships: %d\n", len(resource.Relationships))
		}
	}
	
	fmt.Println("\n✅ Real AWS testing completed!")
}
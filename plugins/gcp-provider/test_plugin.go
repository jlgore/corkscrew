package main

import (
	"context"
	"log"
	"os"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// testPlugin runs basic plugin functionality tests
func testPlugin() {
	log.Printf("🧪 Running GCP Plugin Tests")
	
	ctx := context.Background()
	provider := NewGCPProvider()
	
	// Test 1: Provider Info
	log.Printf("📋 Testing GetProviderInfo...")
	info, err := provider.GetProviderInfo(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("❌ GetProviderInfo failed: %v", err)
	}
	log.Printf("✅ Provider: %s v%s", info.Name, info.Version)
	log.Printf("   Description: %s", info.Description)
	log.Printf("   Capabilities: %d", len(info.Capabilities))
	
	// Test 2: Initialization (without real credentials)
	log.Printf("🔑 Testing Initialize (mock)...")
	initReq := &pb.InitializeRequest{
		Config: map[string]string{
			"project_ids": "test-project-1,test-project-2",
			"scope":       "projects",
		},
	}
	
	// This will likely fail without real credentials, which is expected
	initResp, err := provider.Initialize(ctx, initReq)
	if err != nil {
		log.Printf("⚠️  Initialize failed (expected without credentials): %v", err)
	} else if !initResp.Success {
		log.Printf("⚠️  Initialize failed: %s", initResp.Error)
	} else {
		log.Printf("✅ Initialize succeeded")
		log.Printf("   Metadata: %v", initResp.Metadata)
	}
	
	// Test 3: Schema Generation
	log.Printf("📊 Testing GetSchemas...")
	schemaReq := &pb.GetSchemasRequest{
		Services: []string{"compute", "storage"},
	}
	schemaResp, err := provider.GetSchemas(ctx, schemaReq)
	if err != nil {
		log.Printf("❌ GetSchemas failed: %v", err)
	} else {
		log.Printf("✅ Generated %d schemas", len(schemaResp.Schemas))
		for _, schema := range schemaResp.Schemas {
			log.Printf("   Table: %s", schema.Name)
		}
	}
	
	log.Printf("🎉 Plugin tests completed")
}

// testRealGCP tests with real GCP credentials and resources
func testRealGCP() {
	log.Printf("🌐 Running Real GCP Tests")
	log.Printf("⚠️  This requires valid GCP credentials and permissions")
	
	ctx := context.Background()
	provider := NewGCPProvider()
	
	// Test initialization with real credentials
	log.Printf("🔑 Testing real GCP initialization...")
	initReq := &pb.InitializeRequest{
		Config: map[string]string{
			// Will use Application Default Credentials
		},
	}
	
	initResp, err := provider.Initialize(ctx, initReq)
	if err != nil {
		log.Fatalf("❌ Initialize failed: %v", err)
	}
	if !initResp.Success {
		log.Fatalf("❌ Initialize failed: %s", initResp.Error)
	}
	
	log.Printf("✅ Initialize succeeded")
	log.Printf("   Version: %s", initResp.Version)
	for k, v := range initResp.Metadata {
		log.Printf("   %s: %s", k, v)
	}
	
	// Test service discovery
	log.Printf("🔍 Testing service discovery...")
	discoverReq := &pb.DiscoverServicesRequest{
		ForceRefresh: true,
	}
	
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	discoverResp, err := provider.DiscoverServices(ctx, discoverReq)
	if err != nil {
		log.Printf("❌ Service discovery failed: %v", err)
	} else {
		log.Printf("✅ Discovered %d services", len(discoverResp.Services))
		for i, service := range discoverResp.Services {
			if i < 10 { // Show first 10 services
				log.Printf("   %s: %s (%d resource types)", 
					service.Name, service.DisplayName, len(service.ResourceTypes))
			}
		}
		if len(discoverResp.Services) > 10 {
			log.Printf("   ... and %d more services", len(discoverResp.Services)-10)
		}
	}
	
	// Test resource listing for a simple service
	log.Printf("💾 Testing resource listing (storage buckets)...")
	listReq := &pb.ListResourcesRequest{
		Service: "storage",
	}
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()
	
	listResp, err := provider.ListResources(ctx2, listReq)
	if err != nil {
		log.Printf("❌ Resource listing failed: %v", err)
	} else {
		log.Printf("✅ Found %d storage resources", len(listResp.Resources))
		for i, resource := range listResp.Resources {
			if i < 5 { // Show first 5 resources
				log.Printf("   %s: %s (%s)", resource.Type, resource.Name, resource.Region)
			}
		}
		if len(listResp.Resources) > 5 {
			log.Printf("   ... and %d more resources", len(listResp.Resources)-5)
		}
		
		if len(listResp.Metadata) > 0 {
			log.Printf("   Metadata:")
			for k, v := range listResp.Metadata {
				log.Printf("     %s: %s", k, v)
			}
		}
	}
	
	log.Printf("🎉 Real GCP tests completed")
}

// testAssetInventorySetup tests Cloud Asset Inventory setup and permissions
func testAssetInventorySetup() {
	log.Printf("🗃️  Testing Cloud Asset Inventory Setup")
	
	ctx := context.Background()
	
	// Try to create Asset Inventory client
	log.Printf("🔧 Creating Asset Inventory client...")
	client, err := NewAssetInventoryClient(ctx)
	if err != nil {
		log.Fatalf("❌ Failed to create Asset Inventory client: %v", err)
	}
	
	log.Printf("✅ Asset Inventory client created")
	
	// Test with a sample project (will need to be provided)
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		log.Printf("⚠️  No GCP_PROJECT_ID environment variable set")
		log.Printf("   Please set GCP_PROJECT_ID to test with a specific project")
		log.Printf("   Example: export GCP_PROJECT_ID=my-gcp-project")
		return
	}
	
	log.Printf("🎯 Testing with project: %s", projectID)
	
	// Configure client for the project
	client.SetScope("projects", []string{projectID}, "", "")
	
	// Test health check
	log.Printf("🩺 Testing Asset Inventory health...")
	if client.IsHealthy(ctx) {
		log.Printf("✅ Asset Inventory is healthy and accessible")
	} else {
		log.Printf("❌ Asset Inventory health check failed")
		log.Printf("   This could mean:")
		log.Printf("   1. Cloud Asset Inventory API is not enabled")
		log.Printf("   2. Missing required IAM permissions")
		log.Printf("   3. Project doesn't exist or is not accessible")
		return
	}
	
	// Test a simple query
	log.Printf("🔍 Testing basic asset query...")
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	assets, err := client.QueryAllAssets(ctx)
	if err != nil {
		log.Printf("❌ Asset query failed: %v", err)
		log.Printf("   Common causes:")
		log.Printf("   1. Missing 'cloudasset.assets.listAssets' permission")
		log.Printf("   2. Cloud Asset Inventory API not enabled")
		log.Printf("   3. Invalid project ID or access denied")
		
		log.Printf("📋 Required IAM permissions:")
		log.Printf("   • cloudasset.assets.listAssets")
		log.Printf("   • cloudasset.assets.searchAllResources") 
		log.Printf("   • cloudasset.assets.analyzeIamPolicy")
		
		log.Printf("🔧 To enable Cloud Asset Inventory API:")
		log.Printf("   gcloud services enable cloudasset.googleapis.com --project=%s", projectID)
		
		return
	}
	
	log.Printf("✅ Found %d assets via Cloud Asset Inventory", len(assets))
	
	// Show sample of assets
	serviceCount := make(map[string]int)
	for i, asset := range assets {
		serviceCount[asset.Service]++
		if i < 5 {
			log.Printf("   %s: %s (%s)", asset.Type, asset.Name, asset.Service)
		}
	}
	
	if len(assets) > 5 {
		log.Printf("   ... and %d more assets", len(assets)-5)
	}
	
	log.Printf("📊 Assets by service:")
	for service, count := range serviceCount {
		log.Printf("   %s: %d", service, count)
	}
	
	log.Printf("🎉 Asset Inventory setup test completed successfully!")
	log.Printf("💡 Your GCP provider will use Cloud Asset Inventory for efficient scanning")
}
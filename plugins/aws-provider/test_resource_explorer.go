package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
)

// testResourceExplorerSetup provides detailed Resource Explorer setup information
func testResourceExplorerSetup() {
	fmt.Println("🔍 AWS Resource Explorer Setup Analysis...")
	
	ctx := context.Background()
	
	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("Failed to load AWS config: %v", err)
		return
	}
	
	explorer := resourceexplorer2.NewFromConfig(cfg)
	
	// Check for indexes
	fmt.Println("\n📊 Checking Resource Explorer Indexes...")
	indexResult, err := explorer.ListIndexes(ctx, &resourceexplorer2.ListIndexesInput{})
	if err != nil {
		fmt.Printf("❌ Failed to list indexes: %v\n", err)
		fmt.Println("\n💡 To set up Resource Explorer:")
		fmt.Println("   1. Go to AWS Resource Explorer in the console")
		fmt.Println("   2. Choose 'Turn on Resource Explorer'")
		fmt.Println("   3. Select regions to index")
		fmt.Println("   4. Create an aggregator index in your primary region")
		return
	}
	
	if len(indexResult.Indexes) == 0 {
		fmt.Println("❌ No Resource Explorer indexes found")
		fmt.Println("\n💡 To set up Resource Explorer:")
		fmt.Println("   1. Go to AWS Resource Explorer in the console")
		fmt.Println("   2. Choose 'Turn on Resource Explorer'")
		fmt.Println("   3. Select regions to index")
		fmt.Println("   4. Create an aggregator index in your primary region")
		return
	}
	
	fmt.Printf("✅ Found %d Resource Explorer indexes:\n", len(indexResult.Indexes))
	for _, index := range indexResult.Indexes {
		if index.Arn != nil {
			fmt.Printf("   - %s", *index.Arn)
			if index.Type != "" {
				fmt.Printf(" (Type: %s)", index.Type)
			}
			if index.Region != nil {
				fmt.Printf(" (Region: %s)", *index.Region)
			}
			fmt.Println()
		}
	}
	
	// Check for views
	fmt.Println("\n👁️  Checking Resource Explorer Views...")
	viewResult, err := explorer.ListViews(ctx, &resourceexplorer2.ListViewsInput{})
	if err != nil {
		fmt.Printf("❌ Failed to list views: %v\n", err)
		return
	}
	
	if len(viewResult.Views) == 0 {
		fmt.Println("❌ No Resource Explorer views found")
		fmt.Println("\n💡 To create a view:")
		fmt.Println("   1. Go to Resource Explorer > Views")
		fmt.Println("   2. Click 'Create view'")
		fmt.Println("   3. Name it 'corkscrew-view'")
		fmt.Println("   4. Leave filters empty for all resources")
		return
	}
	
	fmt.Printf("✅ Found %d Resource Explorer views:\n", len(viewResult.Views))
	for i, viewArn := range viewResult.Views {
		fmt.Printf("   %d. %s\n", i+1, viewArn)
		
		// Get view details
		viewDetails, err := explorer.GetView(ctx, &resourceexplorer2.GetViewInput{
			ViewArn: &viewArn,
		})
		if err != nil {
			fmt.Printf("      ❌ Failed to get view details: %v\n", err)
			continue
		}
		
		if viewDetails.View != nil {
			if viewDetails.View.Scope != nil {
				fmt.Printf("      Scope: %s\n", *viewDetails.View.Scope)
			}
			if viewDetails.View.IncludedProperties != nil {
				fmt.Printf("      Included Properties: %d\n", len(viewDetails.View.IncludedProperties))
			}
			if viewDetails.View.LastUpdatedAt != nil {
				fmt.Printf("      Last Updated: %s\n", viewDetails.View.LastUpdatedAt.Format("2006-01-02 15:04:05"))
			}
		}
	}
	
	// Test a simple search if views exist
	if len(viewResult.Views) > 0 {
		fmt.Println("\n🔍 Testing Resource Explorer Search...")
		searchResult, err := explorer.Search(ctx, &resourceexplorer2.SearchInput{
			QueryString: &[]string{"*"}[0],
			ViewArn:     &viewResult.Views[0],
			MaxResults:  &[]int32{5}[0],
		})
		if err != nil {
			fmt.Printf("❌ Search test failed: %v\n", err)
		} else {
			fmt.Printf("✅ Search test successful - found %d resources\n", len(searchResult.Resources))
			for i, resource := range searchResult.Resources {
				if i < 3 && resource.Arn != nil { // Show first 3
					fmt.Printf("   - %s\n", *resource.Arn)
				}
			}
			if len(searchResult.Resources) > 3 {
				fmt.Printf("   ... and %d more\n", len(searchResult.Resources)-3)
			}
		}
	}
	
	// Check account-level configuration
	fmt.Println("\n⚙️  Checking Account Configuration...")
	configResult, err := explorer.GetAccountLevelServiceConfiguration(ctx, &resourceexplorer2.GetAccountLevelServiceConfigurationInput{})
	if err != nil {
		fmt.Printf("❌ Failed to get account configuration: %v\n", err)
	} else if configResult.OrgConfiguration != nil {
		fmt.Printf("✅ Organization configuration found\n")
		if configResult.OrgConfiguration.AWSServiceAccessStatus != "" {
			fmt.Printf("   AWS Service Access: %s\n", configResult.OrgConfiguration.AWSServiceAccessStatus)
		}
	}
	
	fmt.Println("\n📋 Summary:")
	if len(indexResult.Indexes) > 0 && len(viewResult.Views) > 0 {
		fmt.Println("✅ Resource Explorer is properly configured and ready to use!")
		fmt.Println("   Corkscrew will use Resource Explorer for 100x faster resource discovery.")
	} else if len(indexResult.Indexes) > 0 {
		fmt.Println("⚠️  Resource Explorer indexes exist but no views found.")
		fmt.Println("   Create a view to enable fast resource discovery.")
	} else {
		fmt.Println("❌ Resource Explorer is not set up.")
		fmt.Println("   Enable it for dramatically faster resource discovery.")
	}
}
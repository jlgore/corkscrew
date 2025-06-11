package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
)

func main() {
	// Log that the generated factory has been imported
	log.Printf("AWS Provider starting with generated client factory")
	
	// If run with --test flag, run the test instead of serving plugin
	if len(os.Args) > 1 && os.Args[1] == "--test" {
		testPlugin()
		return
	}
	
	// If run with --test-aws flag, run real AWS testing
	if len(os.Args) > 1 && os.Args[1] == "--test-aws" {
		testRealAWS()
		return
	}
	
	// If run with --check-explorer flag, check Resource Explorer setup
	if len(os.Args) > 1 && os.Args[1] == "--check-explorer" {
		testResourceExplorerSetup()
		return
	}
	
	// If run with --demo-explorer flag, show Resource Explorer benefits
	if len(os.Args) > 1 && os.Args[1] == "--demo-explorer" {
		testWithResourceExplorerEnabled()
		return
	}
	
	// If run with --test-services flag, test all registered services
	if len(os.Args) > 1 && os.Args[1] == "--test-services" {
		testAllRegisteredServices()
		return
	}

	// Create the new AWS provider v2
	awsProvider := NewAWSProvider()

	// Serve the plugin
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: awsProvider},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

// Test function stubs - these should be replaced with actual implementations
func testPlugin() {
	fmt.Println("Testing plugin functionality")
	
	// Test the client registry functionality
	testClientCreation()
}

func testClientCreation() {
	fmt.Println("Testing client creation with registered constructors...")
	
	// This is a simplified test - in practice you'd use the real AWS config
	fmt.Printf("Registered services: %v\n", client.ListRegisteredServices())
	
	// Test creating a few clients
	testServices := []string{"s3", "ec2", "iam", "lambda"}
	for _, service := range testServices {
		constructor := client.GetConstructor(service)
		if constructor != nil {
			fmt.Printf("✓ Constructor found for %s\n", service)
		} else {
			fmt.Printf("✗ No constructor found for %s\n", service)
		}
	}
	
	// Test the reflection factory integration
	fmt.Println("\nTesting ReflectionClientFactory integration...")
	testReflectionFactoryIntegration()
}

func testReflectionFactoryIntegration() {
	// Create a simple AWS config for testing (no real credentials needed)
	cfg := aws.Config{
		Region: "us-east-1",
	}
	
	// Create a reflection factory
	factory := client.NewReflectionClientFactory(cfg)
	
	// Test that it can create clients using registered constructors
	testServices := []string{"s3", "ec2", "iam"}
	for _, service := range testServices {
		clientInstance := factory.GetClient(service)
		if clientInstance != nil {
			fmt.Printf("✓ ReflectionFactory created %s client successfully\n", service)
		} else {
			fmt.Printf("✗ ReflectionFactory failed to create %s client\n", service)
		}
	}
	
	// Test available services
	availableServices := factory.GetAvailableServices()
	fmt.Printf("Available services from ReflectionFactory: %v\n", availableServices)
}

func testRealAWS() {
	fmt.Println("Testing AWS Provider with real AWS credentials...")
	
	// Test client creation with real AWS config
	testRealClientCreation()
	
	// Test Phase 3: JSON registry integration
	fmt.Println("\n=== Testing Phase 3: JSON Registry Integration ===")
	testJSONRegistryIntegration()
}

func testRealClientCreation() {
	fmt.Println("Loading real AWS configuration...")
	
	// Load AWS configuration with real credentials
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		fmt.Printf("✗ Failed to load AWS config: %v\n", err)
		return
	}
	
	fmt.Printf("✓ AWS config loaded. Region: %s\n", cfg.Region)
	
	// Create reflection factory with real config
	factory := client.NewReflectionClientFactory(cfg)
	
	// Test creating real clients
	fmt.Println("\nTesting real client creation:")
	testServices := []string{"s3", "iam", "ec2"}
	
	for _, service := range testServices {
		fmt.Printf("Creating %s client... ", service)
		clientInstance := factory.GetClient(service)
		if clientInstance != nil {
			fmt.Printf("✓ Success\n")
			
			// Try to get some basic info to prove it's a real client
			switch service {
			case "s3":
				if s3Client, ok := clientInstance.(*s3.Client); ok {
					fmt.Printf("  - Got S3 client: %T\n", s3Client)
				}
			case "iam":
				if iamClient, ok := clientInstance.(*iam.Client); ok {
					fmt.Printf("  - Got IAM client: %T\n", iamClient)
				}
			case "ec2":
				if ec2Client, ok := clientInstance.(*ec2.Client); ok {
					fmt.Printf("  - Got EC2 client: %T\n", ec2Client)
				}
			}
		} else {
			fmt.Printf("✗ Failed\n")
		}
	}
	
	// Show available services
	availableServices := factory.GetAvailableServices()
	fmt.Printf("\nAvailable services: %v\n", availableServices)
}

func testResourceExplorerSetup() {
	fmt.Println("Resource Explorer setup testing not implemented")
}

func testWithResourceExplorerEnabled() {
	fmt.Println("Resource Explorer demo functionality not implemented")
}

func testJSONRegistryIntegration() {
	fmt.Println("Testing JSON Registry Integration...")
	
	// Create JSON registry and load services.json
	jsonRegistry := registry.NewJSONServiceRegistry()
	
	servicesPath := "./generated/services.json"
	fmt.Printf("Loading services from: %s\n", servicesPath)
	
	if err := jsonRegistry.LoadFromServicesJSON(servicesPath); err != nil {
		fmt.Printf("✗ Failed to load services.json: %v\n", err)
		return
	}
	
	// Show loaded services
	services := jsonRegistry.ListServices()
	fmt.Printf("✓ Loaded %d services from JSON: %v\n", len(services), services)
	
	// Show service definitions with package paths
	fmt.Println("\nService Definitions:")
	definitions := jsonRegistry.ListServiceDefinitions()
	for _, def := range definitions {
		fmt.Printf("  - %s: %s (Package: %s)\n", def.Name, def.DisplayName, def.PackagePath)
	}
	
	// Validate that package paths are present
	fmt.Println("\nValidating package paths:")
	errors := jsonRegistry.ValidateRegistry()
	if len(errors) == 0 {
		fmt.Println("✓ All services have valid package paths")
	} else {
		fmt.Printf("✗ Found %d validation errors:\n", len(errors))
		for _, err := range errors {
			fmt.Printf("  - %v\n", err)
		}
	}
}

func testAllRegisteredServices() {
	fmt.Println("Testing All Registered Services...")
	fmt.Println("================================")
	
	// Create a test AWS config
	cfg := aws.Config{
		Region: "us-east-1",
	}
	
	// List all registered services from the client registry
	services := client.ListRegisteredServices()
	
	fmt.Printf("\nFound %d registered services in client registry\n", len(services))
	fmt.Printf("Services: %v\n\n", services)
	
	// Test creating a client for each service
	successCount := 0
	failureCount := 0
	
	for i, service := range services {
		fmt.Printf("[%d/%d] Testing %s...", i+1, len(services), service)
		
		constructor := client.GetConstructor(service)
		if constructor == nil {
			fmt.Printf(" ❌ No constructor registered\n")
			failureCount++
		} else {
			// Try to create the client
			clientInstance := constructor(cfg)
			if clientInstance != nil {
				fmt.Printf(" ✅ Success\n")
				successCount++
			} else {
				fmt.Printf(" ❌ Constructor returned nil\n")
				failureCount++
			}
		}
	}
	
	fmt.Printf("\nSummary:\n")
	fmt.Printf("========\n")
	fmt.Printf("Total services: %d\n", len(services))
	fmt.Printf("Successful: %d\n", successCount)
	fmt.Printf("Failed: %d\n", failureCount)
	
	if failureCount == 0 && len(services) == 18 {
		fmt.Println("\n🎉 All 18 services are working correctly!")
	} else if len(services) != 18 {
		fmt.Printf("\n⚠️  Expected 18 services, but only found %d\n", len(services))
	} else {
		fmt.Printf("\n❌ %d services failed\n", failureCount)
	}
}
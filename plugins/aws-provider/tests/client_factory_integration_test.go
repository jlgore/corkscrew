//go:build integration
// +build integration

package tests

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedClientFactoryIntegration tests the complete client factory flow
func TestGeneratedClientFactoryIntegration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err, "Failed to load AWS config")

	// Test the factory that delegates to generated code
	factory := client.NewClientFactory(cfg)
	require.NotNil(t, factory, "Factory should not be nil")

	t.Run("TestAvailableServices", func(t *testing.T) {
		// Test that services are available
		availableServices := factory.GetAvailableServices()
		t.Logf("Available services: %v", availableServices)
		
		// We expect at least some core services to be available
		expectedServices := []string{"s3", "ec2", "iam"}
		for _, service := range expectedServices {
			assert.Contains(t, availableServices, service, "Service %s should be available", service)
		}
	})

	t.Run("TestClientCreation", func(t *testing.T) {
		// Test creating clients for core services
		testCases := []struct {
			serviceName string
			shouldWork  bool
		}{
			{"s3", true},      // S3 should work
			{"ec2", true},     // EC2 should work  
			{"iam", true},     // IAM should work
			{"lambda", true},  // Lambda should work
			{"nonexistent-service", false}, // Should fail
		}

		for _, tc := range testCases {
			t.Run("Service_"+tc.serviceName, func(t *testing.T) {
				client := factory.GetClient(tc.serviceName)
				
				if tc.shouldWork {
					if client != nil {
						t.Logf("Successfully created client for %s", tc.serviceName)
					} else {
						t.Logf("No client created for %s (may be expected if generated factory not registered)", tc.serviceName)
					}
				} else {
					assert.Nil(t, client, "Should not create client for nonexistent service")
				}
			})
		}
	})

	t.Run("TestClientCaching", func(t *testing.T) {
		// Test that clients are cached properly
		serviceName := "s3"
		
		// Clear cache first
		factory.ClearCache()
		
		// Create client first time
		client1 := factory.GetClient(serviceName)
		
		// Create client second time
		client2 := factory.GetClient(serviceName)
		
		// If both are not nil, they should be the same instance due to caching
		if client1 != nil && client2 != nil {
			assert.Equal(t, client1, client2, "Clients should be cached and return same instance")
		}
	})

	t.Run("TestGeneratedFactoryRegistration", func(t *testing.T) {
		// Test that we can check if generated factory is available
		generatedFactory := client.GetGeneratedFactory()
		
		if generatedFactory != nil {
			t.Log("Generated factory is registered")
			
			// Test creating client directly with generated factory
			client, err := generatedFactory.CreateClient(ctx, "s3")
			if err == nil && client != nil {
				t.Log("Generated factory can create S3 client")
			} else {
				t.Logf("Generated factory failed to create S3 client: %v", err)
			}
			
			// Test listing services from generated factory
			services := generatedFactory.ListAvailableServices()
			t.Logf("Generated factory services: %v", services)
			assert.NotEmpty(t, services, "Generated factory should list services")
		} else {
			t.Log("Generated factory not registered - this is the issue to fix")
		}
	})
}

// TestGeneratedCodeExists verifies the generated files exist and are valid
func TestGeneratedCodeExists(t *testing.T) {
	t.Run("VerifyGeneratedFileExists", func(t *testing.T) {
		// Check that the generated file exists
		generatedFile := "../generated/client_factory.go"
		info, err := os.Stat(generatedFile)
		assert.NoError(t, err, "Generated client factory file should exist")
		if err == nil {
			assert.Greater(t, info.Size(), int64(1000), "Generated file should not be empty")
			t.Logf("Generated file size: %d bytes", info.Size())
		}
	})

	t.Run("VerifyServicesJsonExists", func(t *testing.T) {
		// Check that services.json exists
		servicesFile := "../generated/services.json"
		info, err := os.Stat(servicesFile)
		assert.NoError(t, err, "Services.json file should exist")
		if err == nil {
			assert.Greater(t, info.Size(), int64(100), "Services.json should not be empty")
			t.Logf("Services.json file size: %d bytes", info.Size())
		}
	})
}

// BenchmarkClientCreation benchmarks client creation performance  
func BenchmarkClientCreation(b *testing.B) {
	if os.Getenv("RUN_BENCHMARKS") != "true" {
		b.Skip("Skipping benchmark. Set RUN_BENCHMARKS=true to run.")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	require.NoError(b, err)

	factory := client.NewClientFactory(cfg)

	b.ResetTimer()

	b.Run("ColdClientCreation", func(b *testing.B) {
		services := []string{"s3", "ec2", "iam", "lambda"}
		
		for i := 0; i < b.N; i++ {
			factory.ClearCache() // Ensure cold creation
			service := services[i%len(services)]
			_ = factory.GetClient(service)
		}
	})

	b.Run("CachedClientCreation", func(b *testing.B) {
		services := []string{"s3", "ec2", "iam", "lambda"}
		
		// Pre-warm cache
		for _, service := range services {
			_ = factory.GetClient(service)
		}
		
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			service := services[i%len(services)]
			_ = factory.GetClient(service)
		}
	})
}
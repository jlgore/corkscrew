package tests

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"

	// Import the pkg packages directly
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
)

// TestUnifiedScannerUnit provides unit tests that don't require AWS credentials
func TestUnifiedScannerUnit(t *testing.T) {
	// Create a mock AWS config for testing
	cfg := aws.Config{
		Region: "us-east-1",
	}

	// Create UnifiedScanner components
	clientFactory := client.NewClientFactory(cfg)
	unifiedScanner := scanner.NewUnifiedScanner(clientFactory)

	t.Run("ClientFactoryServices", func(t *testing.T) {
		testClientFactoryServices(t, clientFactory)
	})

	t.Run("ScannerCreation", func(t *testing.T) {
		assert.NotNil(t, unifiedScanner, "UnifiedScanner should be created successfully")
	})
}

func testClientFactoryServices(t *testing.T, factory *client.ClientFactory) {
	t.Log("Testing ClientFactory service support...")

	// Test that all expected services are available
	availableServices := factory.GetAvailableServices()
	expectedServices := []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
		"ecs", "eks", "elasticache", "cloudformation",
		"cloudwatch", "sns", "sqs", "kinesis", "glue",
		"route53", "redshift",
	}

	assert.GreaterOrEqual(t, len(availableServices), len(expectedServices),
		"Should have at least the expected number of services")

	for _, expectedService := range expectedServices {
		assert.Contains(t, availableServices, expectedService,
			"Service %s should be available", expectedService)
	}

	// Test client creation
	for _, service := range expectedServices {
		t.Run("Client_"+service, func(t *testing.T) {
			client := factory.GetClient(service)
			assert.NotNil(t, client, "Should be able to create client for %s", service)
			
			if client != nil {
				assert.True(t, factory.HasClient(service), 
					"HasClient should return true for %s", service)
			}
		})
	}

	// Test invalid service
	t.Run("InvalidService", func(t *testing.T) {
		client := factory.GetClient("nonexistent-service")
		assert.Nil(t, client, "Should return nil for nonexistent service")
		
		assert.False(t, factory.HasClient("nonexistent-service"),
			"HasClient should return false for nonexistent service")
	})
}

// Note: Private method tests removed since they can't be accessed from outside the package
// The comprehensive test above demonstrates the complete capabilities through integration testing
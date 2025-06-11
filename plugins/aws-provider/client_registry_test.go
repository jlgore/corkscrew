package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllServicesAvailable(t *testing.T) {
	// Expected list of all AWS services that should be registered
	expected := []string{
		"autoscaling", "cloudformation", "cloudwatch", "dynamodb",
		"ec2", "ecs", "eks", "elasticloadbalancing", "iam", "kms",
		"lambda", "rds", "route53", "s3", "secretsmanager", "sns", "sqs", "ssm",
	}

	// Get the list of registered services
	registered := client.ListRegisteredServices()

	// Verify we have the expected number of services
	assert.Equal(t, len(expected), len(registered), 
		"Should have %d services registered, but found %d", len(expected), len(registered))

	// Verify each expected service is registered
	for _, svc := range expected {
		assert.Contains(t, registered, svc, "Service %s should be registered", svc)
	}

	// Log any extra services that were registered but not expected
	for _, svc := range registered {
		found := false
		for _, exp := range expected {
			if svc == exp {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Warning: Service %s is registered but not in expected list", svc)
		}
	}
}

func TestServiceClientCreation(t *testing.T) {
	// Create a test AWS config
	cfg := aws.Config{
		Region: "us-east-1",
	}

	// List of all services to test
	services := []string{
		"autoscaling", "cloudformation", "cloudwatch", "dynamodb",
		"ec2", "ecs", "eks", "elasticloadbalancing", "iam", "kms",
		"lambda", "rds", "route53", "s3", "secretsmanager", "sns", "sqs", "ssm",
	}

	// Test that each service can create a client
	for _, svc := range services {
		t.Run(svc, func(t *testing.T) {
			constructor := client.GetConstructor(svc)
			require.NotNil(t, constructor, "Constructor for %s should not be nil", svc)

			// Create the client
			clientInstance := constructor(cfg)
			assert.NotNil(t, clientInstance, "Client instance for %s should not be nil", svc)
		})
	}
}

func TestGeneratedRegistryConsistency(t *testing.T) {
	// This test ensures that the generated registry is consistent
	// and all registered services have valid constructors
	
	registered := client.ListRegisteredServices()
	require.NotEmpty(t, registered, "Should have registered services")

	cfg := aws.Config{
		Region: "us-east-1",
	}

	for _, svc := range registered {
		constructor := client.GetConstructor(svc)
		assert.NotNil(t, constructor, "Service %s is registered but has no constructor", svc)
		
		if constructor != nil {
			clientInstance := constructor(cfg)
			assert.NotNil(t, clientInstance, "Service %s constructor returned nil", svc)
		}
	}
}

func BenchmarkClientCreation(b *testing.B) {
	cfg := aws.Config{
		Region: "us-east-1",
	}

	services := client.ListRegisteredServices()
	require.NotEmpty(b, services, "Should have registered services for benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, svc := range services {
			constructor := client.GetConstructor(svc)
			if constructor != nil {
				_ = constructor(cfg)
			}
		}
	}
}
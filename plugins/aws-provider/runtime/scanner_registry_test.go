package runtime

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// MockUnifiedScanner implements UnifiedScannerProvider for testing
type MockUnifiedScanner struct {
	resources []*pb.ResourceRef
	enriched  *pb.Resource
	scanError error
	descError error
}

func (m *MockUnifiedScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	if m.scanError != nil {
		return nil, m.scanError
	}
	return m.resources, nil
}

func (m *MockUnifiedScanner) DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	if m.descError != nil {
		return nil, m.descError
	}
	return m.enriched, nil
}

func TestScannerRegistry(t *testing.T) {
	registry := NewScannerRegistry()
	assert.NotNil(t, registry)
	
	// Test that registry starts empty
	services := registry.ListServices()
	assert.Empty(t, services)
}

func TestScannerRegistryWithUnifiedScanner(t *testing.T) {
	registry := NewScannerRegistry()
	
	// Create mock unified scanner
	mockResource := &pb.Resource{
		Provider: "aws",
		Service:  "s3", 
		Type:     "Bucket",
		Id:       "test-bucket",
		Name:     "test-bucket",
		Region:   "us-east-1",
	}
	
	mockRef := &pb.ResourceRef{
		Service: "s3",
		Type:    "Bucket", 
		Id:      "test-bucket",
		Name:    "test-bucket",
		Region:  "us-east-1",
	}
	
	mock := &MockUnifiedScanner{
		resources: []*pb.ResourceRef{mockRef},
		enriched:  mockResource,
	}
	
	// Set unified scanner
	registry.SetUnifiedScanner(mock)
	
	// Test scanning a service
	cfg := aws.Config{Region: "us-east-1"}
	resources, err := registry.ScanService(context.Background(), "s3", cfg, "us-east-1")
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "test-bucket", resources[0].Id)
}

func TestRateLimiting(t *testing.T) {
	registry := NewScannerRegistry()
	
	// Test default rate limiter
	limiter := registry.GetRateLimiter("unknown-service")
	assert.NotNil(t, limiter)
	
	// Test service metadata
	metadata := &ScannerMetadata{
		ServiceName: "test-service",
		RateLimit:   rate.Limit(5),
		BurstLimit:  10,
	}
	
	// Register metadata
	registry.mu.Lock()
	registry.metadata["test-service"] = metadata
	registry.limiters["test-service"] = rate.NewLimiter(metadata.RateLimit, metadata.BurstLimit)
	registry.mu.Unlock()
	
	// Get specific limiter
	limiter = registry.GetRateLimiter("test-service")
	assert.NotNil(t, limiter)
}

func TestScannerLoader(t *testing.T) {
	registry := NewScannerRegistry()
	loader := NewScannerLoader(registry)
	
	// Test loading (should work without errors)
	err := loader.LoadAll()
	assert.NoError(t, err)
	
	// Should have metadata for common services
	metadata, exists := registry.GetMetadata("s3")
	assert.True(t, exists)
	assert.NotNil(t, metadata)
}

func TestGetResourceTypesForService(t *testing.T) {
	tests := []struct {
		service  string
		expected []string
	}{
		{"s3", []string{"Bucket", "Object"}},
		{"ec2", []string{"Instance", "Volume", "Snapshot", "SecurityGroup", "VPC"}},
		{"lambda", []string{"Function", "Layer"}},
		{"unknown", []string{}},
	}
	
	for _, test := range tests {
		t.Run(test.service, func(t *testing.T) {
			types := getResourceTypesForService(test.service)
			assert.Equal(t, test.expected, types)
		})
	}
}

func TestGetPermissionsForService(t *testing.T) {
	tests := []struct {
		service  string
		contains []string
	}{
		{"s3", []string{"s3:ListBuckets", "s3:GetBucket*"}},
		{"ec2", []string{"ec2:Describe*", "ec2:List*"}},
		{"lambda", []string{"lambda:ListFunctions", "lambda:GetFunction*"}},
	}
	
	for _, test := range tests {
		t.Run(test.service, func(t *testing.T) {
			perms := getPermissionsForService(test.service)
			for _, expected := range test.contains {
				assert.Contains(t, perms, expected)
			}
		})
	}
}
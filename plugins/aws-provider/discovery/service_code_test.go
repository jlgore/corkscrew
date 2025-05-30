package discovery

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestServiceCodeFetching tests the ability to fetch and analyze AWS service code
func TestServiceCodeFetching(t *testing.T) {
	// Skip if no GitHub token is available
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		t.Skip("GITHUB_TOKEN not set, skipping service code fetching test")
	}

	sd := NewAWSServiceDiscovery(githubToken)
	ctx := context.Background()

	// Test with a well-known service
	testService := "s3"
	
	t.Run("FetchServiceCode", func(t *testing.T) {
		code, err := sd.FetchServiceCode(ctx, testService)
		if err != nil {
			t.Fatalf("Failed to fetch service code: %v", err)
		}

		if code.ServiceName != testService {
			t.Errorf("Expected service name %s, got %s", testService, code.ServiceName)
		}

		if code.ClientCode == "" {
			t.Error("Client code is empty")
		}

		if code.TypesCode == "" {
			t.Error("Types code is empty")
		}

		if len(code.OperationFiles) == 0 {
			t.Error("No operation files found")
		}

		t.Logf("Fetched %s service code: %d operation files", testService, len(code.OperationFiles))
		
		// Print some operations for verification
		count := 0
		for fileName := range code.OperationFiles {
			if count < 5 {
				t.Logf("  - %s", fileName)
				count++
			}
		}
	})

	t.Run("CacheServiceCode", func(t *testing.T) {
		// First fetch the code
		code, err := sd.FetchServiceCode(ctx, testService)
		if err != nil {
			t.Fatalf("Failed to fetch service code: %v", err)
		}

		// Cache it
		err = sd.CacheServiceCode(testService, code)
		if err != nil {
			t.Fatalf("Failed to cache service code: %v", err)
		}

		// Verify cache directory exists
		cachePath := sd.cachePath + "/" + testService
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			t.Error("Cache directory was not created")
		}
	})

	t.Run("LoadCachedServiceCode", func(t *testing.T) {
		// Load from cache
		cachedCode, err := sd.LoadCachedServiceCode(testService)
		if err != nil {
			t.Fatalf("Failed to load cached service code: %v", err)
		}

		if cachedCode.ServiceName != testService {
			t.Errorf("Expected cached service name %s, got %s", testService, cachedCode.ServiceName)
		}

		if cachedCode.ClientCode == "" {
			t.Error("Cached client code is empty")
		}

		if len(cachedCode.OperationFiles) == 0 {
			t.Error("No cached operation files found")
		}
	})

	t.Run("AnalyzeService", func(t *testing.T) {
		metadata, err := sd.AnalyzeService(ctx, testService)
		if err != nil {
			t.Fatalf("Failed to analyze service: %v", err)
		}

		if metadata.Name != testService {
			t.Errorf("Expected service name %s, got %s", testService, metadata.Name)
		}

		if len(metadata.Operations) == 0 {
			t.Error("No operations found")
		}

		if len(metadata.Resources) == 0 {
			t.Error("No resources found")
		}

		t.Logf("Analyzed %s service:", testService)
		t.Logf("  Operations: %d", metadata.OperationCount)
		t.Logf("  Resources: %d", metadata.ResourceCount)
		t.Logf("  Version: %s", metadata.Version)
		
		// Print sample operations
		t.Log("  Sample operations:")
		for i, op := range metadata.Operations {
			if i < 5 {
				t.Logf("    - %s", op)
			}
		}
		
		// Print sample resources
		t.Log("  Sample resources:")
		for i, res := range metadata.Resources {
			if i < 5 {
				t.Logf("    - %s", res)
			}
		}
	})
}

// TestServiceDiscoveryRateLimiting tests rate limiting behavior
func TestServiceDiscoveryRateLimiting(t *testing.T) {
	sd := NewAWSServiceDiscovery("")
	
	// Verify rate limiter is initialized
	if sd.rateLimiter == nil {
		t.Error("Rate limiter not initialized")
	}
	
	// Test rate limiting timing
	start := time.Now()
	for i := 0; i < 5; i++ {
		<-sd.rateLimiter.C
	}
	elapsed := time.Since(start)
	
	// Should take at least 400ms for 5 ticks at 10/second rate
	expectedMin := 400 * time.Millisecond
	if elapsed < expectedMin {
		t.Errorf("Rate limiting too fast: %v < %v", elapsed, expectedMin)
	}
}

// TestServiceAnalysisOffline tests the analysis logic without GitHub access
func TestServiceAnalysisOffline(t *testing.T) {
	sd := NewAWSServiceDiscovery("")
	
	// Create mock service code
	mockCode := &ServiceCode{
		ServiceName: "testservice",
		ClientCode: `package testservice

type Client struct {
	// client implementation
}`,
		TypesCode: `package testservice

type Bucket struct {
	Name string
	Region string
}

type BucketPolicy struct {
	Version string
	Statement []Statement
}

type ListBucketsInput struct {
	// input fields
}

type ListBucketsOutput struct {
	Buckets []Bucket
}

type InvalidBucketNameException struct {
	Message string
}`,
		OperationFiles: map[string]string{
			"api_op_ListBuckets.go": "// ListBuckets operation",
			"api_op_GetBucket.go": "// GetBucket operation",
			"api_op_CreateBucket.go": "// CreateBucket operation",
			"api_op_DeleteBucket.go": "// DeleteBucket operation",
		},
		DownloadedAt: time.Now(),
		Version: "test-version",
	}
	
	// Analyze the mock code
	operations, resources := sd.analyzeServiceCode(mockCode)
	
	// Verify operations
	expectedOps := []string{"CreateBucket", "DeleteBucket", "GetBucket", "ListBuckets"}
	if len(operations) != len(expectedOps) {
		t.Errorf("Expected %d operations, got %d", len(expectedOps), len(operations))
	}
	
	for i, op := range expectedOps {
		if i < len(operations) && operations[i] != op {
			t.Errorf("Expected operation %s, got %s", op, operations[i])
		}
	}
	
	// Verify resources
	if len(resources) == 0 {
		t.Error("No resources found")
	}
	
	// Should find Bucket and BucketPolicy
	foundBucket := false
	foundPolicy := false
	for _, res := range resources {
		if res == "Bucket" {
			foundBucket = true
		}
		if res == "BucketPolicy" {
			foundPolicy = true
		}
	}
	
	if !foundBucket {
		t.Error("Bucket resource not found")
	}
	if !foundPolicy {
		t.Error("BucketPolicy resource not found")
	}
	
	t.Logf("Found resources: %v", resources)
}

// Example usage function for documentation
func ExampleServiceDiscovery() {
	// Initialize with GitHub token for better rate limits
	githubToken := os.Getenv("GITHUB_TOKEN")
	sd := NewAWSServiceDiscovery(githubToken)
	
	ctx := context.Background()
	
	// Discover all AWS services
	services, err := sd.DiscoverAWSServices(ctx, false)
	if err != nil {
		fmt.Printf("Error discovering services: %v\n", err)
		return
	}
	
	fmt.Printf("Discovered %d AWS services\n", len(services))
	
	// Analyze a specific service in depth
	metadata, err := sd.AnalyzeService(ctx, "s3")
	if err != nil {
		fmt.Printf("Error analyzing S3 service: %v\n", err)
		return
	}
	
	fmt.Printf("S3 Service Analysis:\n")
	fmt.Printf("  Operations: %d\n", metadata.OperationCount)
	fmt.Printf("  Resources: %d\n", metadata.ResourceCount)
	fmt.Printf("  Version: %s\n", metadata.Version)
}
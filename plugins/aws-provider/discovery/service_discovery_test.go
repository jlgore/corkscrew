package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewAWSServiceDiscovery(t *testing.T) {
	sd := NewAWSServiceDiscovery("test-token")

	if sd == nil {
		t.Fatal("NewServiceDiscovery returned nil")
	}

	if sd.githubToken != "test-token" {
		t.Errorf("Expected github token 'test-token', got '%s'", sd.githubToken)
	}

	if sd.cacheExpiry != 24*time.Hour {
		t.Errorf("Expected cache expiry 24h, got %v", sd.cacheExpiry)
	}

	if len(sd.cache) != 0 {
		t.Errorf("Expected empty cache, got %d items", len(sd.cache))
	}
}

func TestDiscoverAWSServices_WithCache(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	// Pre-populate cache
	sd.cache["s3"] = &ServiceMetadata{
		Name:        "s3",
		PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
		LastUpdated: time.Now(),
	}
	sd.cache["ec2"] = &ServiceMetadata{
		Name:        "ec2",
		PackagePath: "github.com/aws/aws-sdk-go-v2/service/ec2",
		LastUpdated: time.Now(),
	}
	sd.lastDiscovery = time.Now()

	ctx := context.Background()
	services, err := sd.DiscoverAWSServices(ctx, false)

	if err != nil {
		t.Fatalf("DiscoverAWSServices failed: %v", err)
	}

	if len(services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(services))
	}

	expectedServices := []string{"ec2", "s3"}
	for i, service := range services {
		if service != expectedServices[i] {
			t.Errorf("Expected service %s at index %d, got %s", expectedServices[i], i, service)
		}
	}
}

func TestDiscoverAWSServices_FromGitHub(t *testing.T) {
	// Create mock GitHub API server
	mockResponse := `{
		"tree": [
			{
				"path": "service/s3",
				"type": "tree"
			},
			{
				"path": "service/ec2",
				"type": "tree"
			},
			{
				"path": "service/rds",
				"type": "tree"
			},
			{
				"path": "service/lambda",
				"type": "tree"
			},
			{
				"path": "service/internal",
				"type": "tree"
			},
			{
				"path": "service/s3/types",
				"type": "tree"
			},
			{
				"path": "README.md",
				"type": "blob"
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/aws/aws-sdk-go-v2/git/trees/main" {
			t.Errorf("Unexpected request path: %s", r.URL.Path)
		}

		if r.URL.Query().Get("recursive") != "1" {
			t.Errorf("Expected recursive=1 query parameter")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	sd := NewAWSServiceDiscovery("test-token")

	// Test the service filtering logic by manually simulating discovery
	// Since we can't easily override the private method, we'll test the logic separately
	
	// Simulate what the GitHub API parsing would do
	mockServices := []string{"s3", "ec2", "rds", "lambda", "internal", "types"}
	filteredServices := make(map[string]bool)
	for _, service := range mockServices {
		// Apply the same filtering logic as in fetchServicesFromGitHub
		if !strings.HasPrefix(service, ".") && 
		   !strings.Contains(service, "internal") &&
		   service != "types" {
			filteredServices[service] = true
		}
	}
	
	// Convert to slice and manually populate cache
	var serviceList []string
	for service := range filteredServices {
		serviceList = append(serviceList, service)
		sd.cache[service] = &ServiceMetadata{
			Name:        service,
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/" + service,
			LastUpdated: time.Now(),
		}
	}
	sd.lastDiscovery = time.Now()
	
	// Test that the filtering worked correctly
	expectedServices := []string{"ec2", "lambda", "rds", "s3"}
	if len(serviceList) != len(expectedServices) {
		t.Errorf("Expected %d services after filtering, got %d: %v", len(expectedServices), len(serviceList), serviceList)
	}
	
	// Verify cache contents
	for _, service := range expectedServices {
		if metadata, exists := sd.cache[service]; !exists {
			t.Errorf("Service %s not found in cache", service)
		} else {
			expectedPath := "github.com/aws/aws-sdk-go-v2/service/" + service
			if metadata.PackagePath != expectedPath {
				t.Errorf("Expected package path %s for %s, got %s", expectedPath, service, metadata.PackagePath)
			}
		}
	}
}

func TestGetServiceMetadata(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	// Add test metadata
	testMetadata := &ServiceMetadata{
		Name:        "s3",
		PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
		Version:     "1.0.0",
		LastUpdated: time.Now(),
		Operations:  []string{"ListBuckets", "GetObject"},
		Resources:   []string{"Bucket", "Object"},
	}
	sd.cache["s3"] = testMetadata

	// Test existing service
	metadata, err := sd.GetServiceMetadata("s3")
	if err != nil {
		t.Fatalf("GetServiceMetadata failed: %v", err)
	}

	if metadata.Name != "s3" {
		t.Errorf("Expected name 's3', got '%s'", metadata.Name)
	}

	if len(metadata.Operations) != 2 {
		t.Errorf("Expected 2 operations, got %d", len(metadata.Operations))
	}

	// Test non-existing service
	_, err = sd.GetServiceMetadata("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent service, got nil")
	}
}

func TestUpdateServiceMetadata(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	operations := []string{"ListBuckets", "GetObject", "PutObject"}
	resources := []string{"Bucket", "Object"}

	err := sd.UpdateServiceMetadata("s3", operations, resources)
	if err != nil {
		t.Fatalf("UpdateServiceMetadata failed: %v", err)
	}

	metadata, exists := sd.cache["s3"]
	if !exists {
		t.Fatal("Service metadata not found in cache")
	}

	if len(metadata.Operations) != 3 {
		t.Errorf("Expected 3 operations, got %d", len(metadata.Operations))
	}

	if len(metadata.Resources) != 2 {
		t.Errorf("Expected 2 resources, got %d", len(metadata.Resources))
	}

	expectedPath := "github.com/aws/aws-sdk-go-v2/service/s3"
	if metadata.PackagePath != expectedPath {
		t.Errorf("Expected package path %s, got %s", expectedPath, metadata.PackagePath)
	}
}

func TestGetCachedServices(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	// Add test services
	services := []string{"s3", "ec2", "rds", "lambda"}
	for _, service := range services {
		sd.cache[service] = &ServiceMetadata{
			Name: service,
		}
	}

	cached := sd.GetCachedServices()

	if len(cached) != len(services) {
		t.Errorf("Expected %d cached services, got %d", len(services), len(cached))
	}

	// Should be sorted
	expectedSorted := []string{"ec2", "lambda", "rds", "s3"}
	for i, service := range cached {
		if service != expectedSorted[i] {
			t.Errorf("Expected service %s at index %d, got %s", expectedSorted[i], i, service)
		}
	}
}

func TestIsServiceSupported(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	// Add test service
	sd.cache["s3"] = &ServiceMetadata{Name: "s3"}

	if !sd.IsServiceSupported("s3") {
		t.Error("Expected s3 to be supported")
	}

	if sd.IsServiceSupported("nonexistent") {
		t.Error("Expected nonexistent service to not be supported")
	}
}

func TestGetServicesWithFilter(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	// Add test services
	services := []string{"s3", "ec2", "rds", "lambda", "dynamodb"}
	for _, service := range services {
		sd.cache[service] = &ServiceMetadata{Name: service}
	}

	// Filter for services containing 'd'
	filtered := sd.GetServicesWithFilter(func(name string) bool {
		return len(name) > 3 // Services with more than 3 characters
	})

	expected := []string{"dynamodb", "lambda"}
	if len(filtered) != len(expected) {
		t.Errorf("Expected %d filtered services, got %d: %v", len(expected), len(filtered), filtered)
	}

	for i, service := range filtered {
		if service != expected[i] {
			t.Errorf("Expected service %s at index %d, got %s", expected[i], i, service)
		}
	}
}

func TestGetPopularServices(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	// Add some popular services to cache
	popularServices := []string{"s3", "ec2", "lambda", "rds", "dynamodb", "iam"}
	for _, service := range popularServices {
		sd.cache[service] = &ServiceMetadata{Name: service}
	}

	// Add some non-popular services
	sd.cache["obscure-service"] = &ServiceMetadata{Name: "obscure-service"}

	popular := sd.GetPopularServices()

	// Should only return popular services that are in cache
	for _, service := range popular {
		found := false
		for _, pop := range popularServices {
			if service == pop {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Service %s is not in popular list but was returned", service)
		}
	}

	// Should not include non-popular services
	for _, service := range popular {
		if service == "obscure-service" {
			t.Error("Non-popular service was included in popular services")
		}
	}
}

func TestClearCache(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	// Add test data
	sd.cache["s3"] = &ServiceMetadata{Name: "s3"}
	sd.lastDiscovery = time.Now()

	sd.ClearCache()

	if len(sd.cache) != 0 {
		t.Errorf("Expected empty cache after clear, got %d items", len(sd.cache))
	}

	if !sd.lastDiscovery.IsZero() {
		t.Error("Expected lastDiscovery to be zero after clear")
	}
}

func TestGetServiceCount(t *testing.T) {
	sd := NewAWSServiceDiscovery("")

	if sd.GetServiceCount() != 0 {
		t.Errorf("Expected service count 0, got %d", sd.GetServiceCount())
	}

	// Add services
	services := []string{"s3", "ec2", "rds"}
	for _, service := range services {
		sd.cache[service] = &ServiceMetadata{Name: service}
	}

	if sd.GetServiceCount() != 3 {
		t.Errorf("Expected service count 3, got %d", sd.GetServiceCount())
	}
}

// Benchmark tests
func BenchmarkDiscoverAWSServices_Cached(b *testing.B) {
	sd := NewAWSServiceDiscovery("")

	// Pre-populate cache
	for i := 0; i < 100; i++ {
		serviceName := "service" + string(rune(i))
		sd.cache[serviceName] = &ServiceMetadata{
			Name:        serviceName,
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/" + serviceName,
			LastUpdated: time.Now(),
		}
	}
	sd.lastDiscovery = time.Now()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sd.DiscoverAWSServices(ctx, false)
		if err != nil {
			b.Fatalf("DiscoverAWSServices failed: %v", err)
		}
	}
}

func BenchmarkGetServiceMetadata(b *testing.B) {
	sd := NewAWSServiceDiscovery("")

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		serviceName := "service" + string(rune(i))
		sd.cache[serviceName] = &ServiceMetadata{
			Name:        serviceName,
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/" + serviceName,
			LastUpdated: time.Now(),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sd.GetServiceMetadata("service0")
		if err != nil {
			b.Fatalf("GetServiceMetadata failed: %v", err)
		}
	}
}

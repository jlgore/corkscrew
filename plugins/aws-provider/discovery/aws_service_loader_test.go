package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewAWSServiceLoader(t *testing.T) {
	pluginDir := "/tmp/test-plugins"
	tempDir := "/tmp/test-temp"
	
	loader := NewAWSServiceLoader(pluginDir, tempDir)
	
	if loader == nil {
		t.Fatal("NewAWSServiceLoader returned nil")
	}
	
	if len(loader.loadedClients) != 0 {
		t.Errorf("Expected empty loadedClients, got %d items", len(loader.loadedClients))
	}
	
	if len(loader.loadedPlugins) != 0 {
		t.Errorf("Expected empty loadedPlugins, got %d items", len(loader.loadedPlugins))
	}
	
	if loader.pluginDir != pluginDir {
		t.Errorf("Expected pluginDir %s, got %s", pluginDir, loader.pluginDir)
	}
	
	if loader.tempDir != tempDir {
		t.Errorf("Expected tempDir %s, got %s", tempDir, loader.tempDir)
	}
	
	if loader.analyzer == nil {
		t.Error("Expected analyzer to be initialized")
	}
}

func TestGetLoadedServices_Empty(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	services := loader.GetLoadedServices()
	
	if len(services) != 0 {
		t.Errorf("Expected 0 loaded services, got %d", len(services))
	}
}

func TestIsServiceLoaded_False(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	if loader.IsServiceLoaded("s3") {
		t.Error("Expected s3 to not be loaded")
	}
}

func TestUnloadService_NotLoaded(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	err := loader.UnloadService("nonexistent")
	if err != nil {
		t.Errorf("UnloadService should not error for non-loaded service, got: %v", err)
	}
}

func TestUnloadAllServices(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	// Manually add some mock loaded services
	loader.loadedClients["s3"] = "mock-client"
	loader.loadedClients["ec2"] = "mock-client"
	
	if len(loader.GetLoadedServices()) != 2 {
		t.Fatal("Expected 2 services to be loaded before test")
	}
	
	loader.UnloadAllServices()
	
	if len(loader.GetLoadedServices()) != 0 {
		t.Errorf("Expected 0 loaded services after unloading all, got %d", len(loader.GetLoadedServices()))
	}
}

func TestGetServiceClient_NotLoaded(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	_, err := loader.GetServiceClient("nonexistent")
	if err == nil {
		t.Error("Expected error for non-loaded service, got nil")
	}
}

func TestGetServiceClient_Loaded(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	// Manually add a mock loaded service
	mockClient := "mock-s3-client"
	loader.loadedClients["s3"] = mockClient
	
	client, err := loader.GetServiceClient("s3")
	if err != nil {
		t.Fatalf("GetServiceClient failed: %v", err)
	}
	
	if client != mockClient {
		t.Errorf("Expected client %v, got %v", mockClient, client)
	}
}

func TestGetPluginPath(t *testing.T) {
	pluginDir := "/tmp/test-plugins"
	loader := NewAWSServiceLoader(pluginDir, "/tmp/temp")
	
	path := loader.GetPluginPath("s3")
	expected := filepath.Join(pluginDir, "aws-s3.so")
	
	if path != expected {
		t.Errorf("Expected plugin path %s, got %s", expected, path)
	}
}

func TestGeneratePluginSource(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	source := loader.generatePluginSource("s3")
	
	if source == "" {
		t.Error("Expected non-empty plugin source")
	}
	
	// Check that the source contains expected elements
	expectedStrings := []string{
		"package main",
		"NewS3Client",
		"github.com/aws/aws-sdk-go-v2/service/s3",
		"GetServiceName",
		"GetPackagePath",
	}
	
	for _, expected := range expectedStrings {
		if !contains(source, expected) {
			t.Errorf("Expected plugin source to contain %s", expected)
		}
	}
}

func TestGenerateGoMod(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	goMod := loader.generateGoMod("s3")
	
	if goMod == "" {
		t.Error("Expected non-empty go.mod content")
	}
	
	expectedStrings := []string{
		"module aws-s3-plugin",
		"go 1.24",
		"github.com/aws/aws-sdk-go-v2",
		"github.com/aws/aws-sdk-go-v2/service/s3",
	}
	
	for _, expected := range expectedStrings {
		if !contains(goMod, expected) {
			t.Errorf("Expected go.mod to contain %s", expected)
		}
	}
}

func TestCleanupTempFiles(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := "/tmp/test-cleanup-" + string(rune(time.Now().UnixNano()))
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test temp dir: %v", err)
	}
	
	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	loader := NewAWSServiceLoader("/tmp/plugins", tempDir)
	
	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("Test file should exist before cleanup")
	}
	
	// Cleanup
	err = loader.CleanupTempFiles()
	if err != nil {
		t.Fatalf("CleanupTempFiles failed: %v", err)
	}
	
	// Verify file is gone
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Test file should not exist after cleanup")
	}
}

func TestCleanupTempFiles_EmptyDir(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "")
	
	err := loader.CleanupTempFiles()
	if err != nil {
		t.Errorf("CleanupTempFiles should not error with empty tempDir, got: %v", err)
	}
}

func TestLoadMultipleServices_EmptyList(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	ctx := context.Background()
	results, errors := loader.LoadMultipleServices(ctx, []string{})
	
	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty service list, got %d", len(results))
	}
	
	if len(errors) != 0 {
		t.Errorf("Expected 0 errors for empty service list, got %d", len(errors))
	}
}

// Integration test that requires actual AWS SDK (may fail in test environment)
func TestLoadService_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	// Create temporary directories
	tempDir := "/tmp/corkscrew-test-" + string(rune(time.Now().UnixNano()))
	pluginDir := filepath.Join(tempDir, "plugins")
	workDir := filepath.Join(tempDir, "work")
	
	defer os.RemoveAll(tempDir)
	
	loader := NewAWSServiceLoader(pluginDir, workDir)
	
	ctx := context.Background()
	
	// This will likely fail in test environment due to missing AWS SDK dependencies
	// but we test that it fails gracefully
	_, err := loader.LoadService(ctx, "s3")
	if err != nil {
		t.Logf("LoadService failed as expected in test environment: %v", err)
	}
}

func TestValidatePluginEnvironment(t *testing.T) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	err := loader.ValidatePluginEnvironment()
	if err != nil {
		t.Logf("Plugin environment validation failed (may be expected): %v", err)
		// Don't fail the test as this depends on the build environment
	}
}

func TestPreloadPopularServices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	ctx := context.Background()
	err := loader.PreloadPopularServices(ctx)
	
	// This will likely fail in test environment, but we test that it doesn't panic
	if err != nil {
		t.Logf("PreloadPopularServices failed as expected in test environment: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > len(substr) && containsAt(s, substr, 0)))
}

func containsAt(s, substr string, start int) bool {
	if start+len(substr) > len(s) {
		return false
	}
	for i := 0; i < len(substr); i++ {
		if s[start+i] != substr[i] {
			if start+1 < len(s) {
				return containsAt(s, substr, start+1)
			}
			return false
		}
	}
	return true
}

// Benchmark tests
func BenchmarkGetLoadedServices(b *testing.B) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	// Pre-load some mock services
	for i := 0; i < 100; i++ {
		serviceName := "service" + string(rune(i))
		loader.loadedClients[serviceName] = "mock-client"
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loader.GetLoadedServices()
	}
}

func BenchmarkIsServiceLoaded(b *testing.B) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	// Pre-load some mock services
	for i := 0; i < 100; i++ {
		serviceName := "service" + string(rune(i))
		loader.loadedClients[serviceName] = "mock-client"
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loader.IsServiceLoaded("service0")
	}
}

func BenchmarkGetServiceClient(b *testing.B) {
	loader := NewAWSServiceLoader("/tmp/plugins", "/tmp/temp")
	
	// Pre-load a mock service
	loader.loadedClients["s3"] = "mock-s3-client"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := loader.GetServiceClient("s3")
		if err != nil {
			b.Fatalf("GetServiceClient failed: %v", err)
		}
	}
}

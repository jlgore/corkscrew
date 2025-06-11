package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverServicesFromGoMod(t *testing.T) {
	// Create a test go.mod file
	tmpDir := t.TempDir()
	goModContent := `module test

go 1.21

require (
	github.com/aws/aws-sdk-go-v2 v1.24.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.44.0
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.138.0
	github.com/aws/aws-sdk-go-v2/service/lambda v1.49.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.26.0
	github.com/other/package v1.0.0
)`

	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to create test go.mod: %v", err)
	}

	// Change to temp directory
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Test discovery
	services, err := discoverServicesFromGoMod()
	if err != nil {
		t.Errorf("discoverServicesFromGoMod() error: %v", err)
		return
	}

	// Check expected services
	expectedServices := []string{"s3", "ec2", "lambda", "dynamodb"}
	if len(services) != len(expectedServices) {
		t.Errorf("discoverServicesFromGoMod() got %d services, want %d", len(services), len(expectedServices))
		return
	}

	// Check each expected service is found
	serviceMap := make(map[string]bool)
	for _, svc := range services {
		serviceMap[svc] = true
	}

	for _, expected := range expectedServices {
		if !serviceMap[expected] {
			t.Errorf("discoverServicesFromGoMod() missing expected service: %s", expected)
		}
	}
}

func TestDiscoverServicesFromGoModNoFile(t *testing.T) {
	// Test in directory without go.mod
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	services, err := discoverServicesFromGoMod()
	if err == nil {
		t.Error("discoverServicesFromGoMod() expected error when no go.mod exists")
	}
	if len(services) > 0 {
		t.Errorf("discoverServicesFromGoMod() returned services when none expected: %v", services)
	}
}

func TestDiscoverServicesFromAWSSDK(t *testing.T) {
	// This test might not find actual SDK installations in CI
	// but should at least not crash
	services, err := discoverServicesFromAWSSDK()
	
	if err != nil {
		t.Errorf("discoverServicesFromAWSSDK() unexpected error: %v", err)
		return
	}
	
	// In CI or without SDK, it should return empty list without error
	t.Logf("discoverServicesFromAWSSDK() found %d services", len(services))
}

func TestDiscoverServices(t *testing.T) {
	// Test the main discovery function
	// It should combine results from multiple sources
	services, err := discoverServices()
	
	if err != nil {
		t.Errorf("discoverServices() error: %v", err)
		return
	}
	
	// Should return at least some services (even if just from fallback)
	if len(services) == 0 {
		t.Error("discoverServices() returned no services")
	}
	
	t.Logf("discoverServices() found %d services", len(services))
}

func TestDiscoverServicesFromGitHub(t *testing.T) {
	// Skip this test if we don't have internet access or in CI
	if os.Getenv("CI") == "true" || os.Getenv("SKIP_NETWORK_TESTS") == "true" {
		t.Skip("Skipping GitHub API test in CI")
	}
	
	services, err := discoverServicesFromGitHub()
	
	// GitHub API might fail due to rate limits or network issues
	if err != nil {
		t.Logf("discoverServicesFromGitHub() error (might be rate limited): %v", err)
		return
	}
	
	// If successful, should return many services
	if len(services) < 50 {
		t.Errorf("discoverServicesFromGitHub() returned only %d services, expected more", len(services))
	}
	
	// Check for some common services
	expectedServices := []string{"s3", "ec2", "lambda", "dynamodb", "iam"}
	serviceMap := make(map[string]bool)
	for _, svc := range services {
		serviceMap[svc] = true
	}
	
	for _, expected := range expectedServices {
		if !serviceMap[expected] {
			t.Errorf("discoverServicesFromGitHub() missing expected service: %s", expected)
		}
	}
}
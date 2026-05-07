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

go 1.24

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
	// It should combine results from local sources by default
	services, err := discoverServices()

	if err != nil {
		t.Errorf("discoverServices() error: %v", err)
		return
	}

	// Should return at least some services from local go.mod/module cache
	if len(services) == 0 {
		t.Error("discoverServices() returned no services")
	}

	t.Logf("discoverServices() found %d services", len(services))
}

func TestRemoteDiscoveryEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "unset", want: false},
		{name: "false", value: "false", want: false},
		{name: "zero", value: "0", want: false},
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "yes", value: "yes", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CORKSCREW_ENABLE_REMOTE_DISCOVERY", tt.value)

			if got := remoteDiscoveryEnabled(); got != tt.want {
				t.Fatalf("remoteDiscoveryEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiscoverServicesFromGitHub(t *testing.T) {
	if !remoteDiscoveryEnabled() {
		t.Skip("Skipping GitHub API test unless CORKSCREW_ENABLE_REMOTE_DISCOVERY is enabled")
	}

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

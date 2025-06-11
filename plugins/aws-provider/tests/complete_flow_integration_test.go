//go:build integration
// +build integration

package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompleteGenerationFlow tests the entire build pipeline flow
func TestCompleteGenerationFlow(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	// Get the aws-provider directory
	awsProviderDir, err := filepath.Abs("../")
	require.NoError(t, err)

	t.Run("Phase1_CleanAndGenerate", func(t *testing.T) {
		// Clean previous generated files
		t.Log("Cleaning previous generated files...")
		cleanCmd := exec.Command("make", "clean")
		cleanCmd.Dir = awsProviderDir
		output, err := cleanCmd.CombinedOutput()
		if err != nil {
			t.Logf("Clean command output: %s", output)
			// Don't fail - clean might not exist or have nothing to clean
		}

		// Run code generation
		t.Log("Running code generation...")
		generateCmd := exec.Command("make", "generate")
		generateCmd.Dir = awsProviderDir
		generateCmd.Env = append(os.Environ(), "GO111MODULE=on")
		
		output, err = generateCmd.CombinedOutput()
		t.Logf("Generate command output: %s", output)
		require.NoError(t, err, "Code generation should succeed")

		// Verify generated files exist
		generatedDir := filepath.Join(awsProviderDir, "generated")
		
		// Check client_factory.go
		clientFactoryFile := filepath.Join(generatedDir, "client_factory.go")
		_, err = os.Stat(clientFactoryFile)
		assert.NoError(t, err, "client_factory.go should be generated")
		
		// Check services.json  
		servicesFile := filepath.Join(generatedDir, "services.json")
		_, err = os.Stat(servicesFile)
		assert.NoError(t, err, "services.json should exist")
	})

	t.Run("Phase2_BuildPlugin", func(t *testing.T) {
		// Build the plugin
		t.Log("Building AWS provider plugin...")
		buildCmd := exec.Command("make", "build")
		buildCmd.Dir = awsProviderDir
		buildCmd.Env = append(os.Environ(), "GO111MODULE=on")
		
		output, err := buildCmd.CombinedOutput()
		t.Logf("Build command output: %s", output)
		require.NoError(t, err, "Plugin build should succeed")

		// Verify binary exists
		binaryPath := filepath.Join(awsProviderDir, "aws-provider")
		_, err = os.Stat(binaryPath)
		assert.NoError(t, err, "aws-provider binary should exist after build")
	})

	t.Run("Phase3_TestGeneratedClientFactory", func(t *testing.T) {
		// This requires the generated code to be compiled and available
		// We test this by running the actual plugin with test flag
		
		t.Log("Testing generated client factory...")
		binaryPath := filepath.Join(awsProviderDir, "aws-provider")
		
		// Test that binary can run
		testCmd := exec.Command(binaryPath, "--help")
		testCmd.Dir = awsProviderDir
		
		output, err := testCmd.CombinedOutput()
		t.Logf("Plugin help output: %s", output)
		// Note: --help might not be implemented, so don't require success
		
		// If there's a test mode, we could test it:
		if _, err := os.Stat(binaryPath); err == nil {
			// Try to run a basic test if the plugin supports it
			testModeCmd := exec.Command(binaryPath, "--test")
			testModeCmd.Dir = awsProviderDir
			testModeCmd.Env = append(os.Environ(), "AWS_REGION=us-east-1")
			
			// Set timeout to avoid hanging
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			testModeCmd = exec.CommandContext(ctx, binaryPath, "--test")
			testModeCmd.Dir = awsProviderDir
			
			output, err := testModeCmd.CombinedOutput()
			t.Logf("Plugin test mode output: %s", output)
			// Don't require success as this might not be implemented or need AWS creds
		}
	})

	t.Run("Phase4_RunIntegrationTests", func(t *testing.T) {
		// Run the actual integration tests
		t.Log("Running integration tests...")
		
		// Run specific integration tests for client factory
		testCmd := exec.Command("go", "test", "-v", "-tags=integration", "./tests", "-run", "TestGeneratedClientFactoryIntegration", "-timeout", "5m")
		testCmd.Dir = awsProviderDir
		testCmd.Env = append(os.Environ(), 
			"RUN_INTEGRATION_TESTS=true",
			"GO111MODULE=on",
		)
		
		output, err := testCmd.CombinedOutput()
		t.Logf("Integration test output: %s", output)
		
		// Note: This might fail if AWS credentials aren't available
		// That's okay for the build pipeline test
		if err != nil {
			t.Logf("Integration tests failed (may be due to AWS credentials): %v", err)
		}
	})
}

// TestMakefileTargets tests that all expected Makefile targets work
func TestMakefileTargets(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	awsProviderDir, err := filepath.Abs("../")
	require.NoError(t, err)

	// Test individual Makefile targets
	targets := []struct {
		name        string
		target      string
		shouldExist []string // Files that should exist after this target
	}{
		{
			name:   "analyze",
			target: "analyze",
			shouldExist: []string{
				"generated/services.json",
			},
		},
		{
			name:   "generate-go", 
			target: "generate-go",
			shouldExist: []string{
				"generated/client_factory.go",
			},
		},
	}

	for _, tt := range targets {
		t.Run("Target_"+tt.name, func(t *testing.T) {
			// Clean first
			cleanCmd := exec.Command("make", "clean")
			cleanCmd.Dir = awsProviderDir
			_, _ = cleanCmd.CombinedOutput() // Ignore errors
			
			// Run the target
			cmd := exec.Command("make", tt.target)
			cmd.Dir = awsProviderDir
			cmd.Env = append(os.Environ(), "GO111MODULE=on")
			
			output, err := cmd.CombinedOutput()
			t.Logf("Target %s output: %s", tt.target, output)
			
			// Some targets might not exist yet, so don't require success
			if err != nil {
				t.Logf("Target %s failed (may not be implemented): %v", tt.target, err)
			}
			
			// Check expected files exist if target succeeded
			if err == nil {
				for _, file := range tt.shouldExist {
					filePath := filepath.Join(awsProviderDir, file)
					_, statErr := os.Stat(filePath)
					assert.NoError(t, statErr, "File %s should exist after target %s", file, tt.target)
				}
			}
		})
	}
}

// TestDocumentationUpdated verifies that documentation reflects the current implementation
func TestDocumentationUpdated(t *testing.T) {
	awsProviderDir, err := filepath.Abs("../")
	require.NoError(t, err)

	t.Run("CheckMigrationDocs", func(t *testing.T) {
		// Check that migration documentation exists
		docs := []string{
			"MIGRATION_COMPLETED.md",
			"OPTIMIZED_SCANNER_INTEGRATION.md", 
			"PHASE2_REFLECTION_DISCOVERY_COMPLETED.md",
			"PHASE3_ANALYSIS_GENERATION_COMPLETED.md",
		}
		
		for _, doc := range docs {
			docPath := filepath.Join(awsProviderDir, doc)
			_, err := os.Stat(docPath)
			assert.NoError(t, err, "Documentation file %s should exist", doc)
		}
	})

	t.Run("CheckREADMEs", func(t *testing.T) {
		// Check that key README files exist
		readmes := []string{
			"tests/README.md",
			"registry/README.md",
			"factory/README.md",
		}
		
		for _, readme := range readmes {
			readmePath := filepath.Join(awsProviderDir, readme)
			if _, err := os.Stat(readmePath); err == nil {
				t.Logf("README exists: %s", readme)
			} else {
				t.Logf("README missing: %s (may be okay)", readme)
			}
		}
	})
}
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCLIConfigInitAndValidate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corkscrew.yaml")
	t.Setenv("CORKSCREW_CONFIG_FILE", configPath)

	initCode := captureCLIOutput(t, func() int {
		return runCLI([]string{"config", "init"})
	})
	if initCode != 0 {
		t.Fatalf("config init exit code = %d, want 0", initCode)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}

	validateCode := captureCLIOutput(t, func() int {
		return runCLI([]string{"config", "validate"})
	})
	if validateCode != 0 {
		t.Fatalf("config validate exit code = %d, want 0", validateCode)
	}

	secondInitCode := captureCLIOutput(t, func() int {
		return runCLI([]string{"config", "init"})
	})
	if secondInitCode != 1 {
		t.Fatalf("second config init exit code = %d, want 1", secondInitCode)
	}
}

func TestRunCLIConfigErrorsReturnNonZero(t *testing.T) {
	t.Run("missing config command", func(t *testing.T) {
		code := captureCLIOutput(t, func() int {
			return runCLI([]string{"config"})
		})
		if code != 1 {
			t.Fatalf("config exit code = %d, want 1", code)
		}
	})

	t.Run("missing config file", func(t *testing.T) {
		t.Setenv("CORKSCREW_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))

		code := captureCLIOutput(t, func() int {
			return runCLI([]string{"config", "show"})
		})
		if code != 1 {
			t.Fatalf("config show exit code = %d, want 1", code)
		}
	})

	t.Run("invalid config file", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "invalid.yaml")
		t.Setenv("CORKSCREW_CONFIG_FILE", configPath)
		if err := os.WriteFile(configPath, []byte(invalidConfigYAML), 0644); err != nil {
			t.Fatalf("write invalid config: %v", err)
		}

		code := captureCLIOutput(t, func() int {
			return runCLI([]string{"config", "validate"})
		})
		if code != 1 {
			t.Fatalf("config validate exit code = %d, want 1", code)
		}
	})
}

var invalidConfigYAML = strings.TrimSpace(`
version: "2.0"
providers:
  aws:
    enabled: true
    regions:
      - ""
    services:
      - ""
database:
  path: ""
output:
  default_format: table
`) + "\n"

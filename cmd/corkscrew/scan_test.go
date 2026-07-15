package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/jlgore/corkscrew/internal/config"
)

func TestRunScanEParseErrorsReturnError(t *testing.T) {
	var err error
	captureCLIOutput(t, func() int {
		err = runScanE([]string{"--not-a-real-flag"})
		if err != nil {
			return 1
		}
		return 0
	})

	if err == nil {
		t.Fatal("runScanE returned nil, want parse error")
	}
	if !strings.Contains(err.Error(), "parse scan flags") {
		t.Fatalf("runScanE error = %q, want parse scan flags", err)
	}
}

func TestRunScanEHelpReturnsNil(t *testing.T) {
	var err error
	code := captureCLIOutput(t, func() int {
		err = runScanE([]string{"-h"})
		if err != nil {
			return 1
		}
		return 0
	})

	if code != 0 {
		t.Fatalf("help exit code = %d, want 0", code)
	}
	if err != nil {
		t.Fatalf("runScanE help error = %v, want nil", err)
	}
}

func TestRunScanEValidationErrorsReturnError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corkscrew.yaml")
	if err := os.WriteFile(configPath, []byte(appconfig.DefaultCorkscrewYAML()), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var err error
	captureCLIOutput(t, func() int {
		err = runScanE([]string{"--provider", "digitalocean", "--config", configPath})
		if err != nil {
			return 1
		}
		return 0
	})

	if err == nil {
		t.Fatal("runScanE returned nil, want provider validation error")
	}
	if !strings.Contains(err.Error(), "provider digitalocean is not configured and enabled") {
		t.Fatalf("runScanE error = %q, want unconfigured provider", err)
	}
}

func TestRunScanEMissingConfigReturnsError(t *testing.T) {
	missingConfig := filepath.Join(t.TempDir(), "missing.yaml")

	var err error
	captureCLIOutput(t, func() int {
		err = runScanE([]string{"--config", missingConfig})
		if err != nil {
			return 1
		}
		return 0
	})

	if err == nil {
		t.Fatal("runScanE returned nil, want missing config error")
	}
	if !strings.Contains(err.Error(), "failed to load configuration") {
		t.Fatalf("runScanE error = %q, want configuration load error", err)
	}
}

func TestRunCLIScanErrorsReturnNonZero(t *testing.T) {
	missingConfig := filepath.Join(t.TempDir(), "missing.yaml")

	code := captureCLIOutput(t, func() int {
		return runCLI([]string{"scan", "--config", missingConfig})
	})
	if code != 1 {
		t.Fatalf("scan exit code = %d, want 1", code)
	}
}

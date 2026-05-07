package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindPluginUsesProviderBinaryName(t *testing.T) {
	tmpDir := t.TempDir()
	pluginPath := filepath.Join(tmpDir, "aws-provider")
	if err := os.WriteFile(pluginPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to create plugin fixture: %v", err)
	}

	pm := &PluginManager{pluginDirs: []string{tmpDir}}
	found, err := pm.FindPlugin("aws")
	if err != nil {
		t.Fatalf("FindPlugin() error = %v", err)
	}
	if found != pluginPath {
		t.Fatalf("FindPlugin() = %q, want %q", found, pluginPath)
	}
}

func TestFindPluginUsesNestedProviderBinaryName(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "aws-provider")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	pluginPath := filepath.Join(pluginDir, "aws-provider")
	if err := os.WriteFile(pluginPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to create plugin fixture: %v", err)
	}

	pm := &PluginManager{pluginDirs: []string{tmpDir}}
	found, err := pm.FindPlugin("aws")
	if err != nil {
		t.Fatalf("FindPlugin() error = %v", err)
	}
	if found != pluginPath {
		t.Fatalf("FindPlugin() = %q, want %q", found, pluginPath)
	}
}

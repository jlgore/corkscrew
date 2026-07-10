package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginSearchPathsUseProviderBinaryName(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	paths := PluginSearchPaths("aws", []string{
		filepath.Join(".", "build", "bin"),
		filepath.Join(".", "plugins"),
		filepath.Join("/home/tester", ".corkscrew", "plugins"),
		filepath.Join("/home/tester", ".corkscrew", "bin", "plugin"),
	})
	want := []string{
		filepath.Join(".", "build", "bin", "aws-provider"),
		filepath.Join(".", "build", "bin", "aws-provider", "aws-provider"),
		filepath.Join(".", "plugins", "aws-provider"),
		filepath.Join(".", "plugins", "aws-provider", "aws-provider"),
		filepath.Join("/home/tester", ".corkscrew", "plugins", "aws-provider"),
		filepath.Join("/home/tester", ".corkscrew", "plugins", "aws-provider", "aws-provider"),
		filepath.Join("/home/tester", ".corkscrew", "bin", "plugin", "aws-provider"),
		filepath.Join("/home/tester", ".corkscrew", "bin", "plugin", "aws-provider", "aws-provider"),
	}

	if len(paths) != len(want) {
		t.Fatalf("PluginSearchPaths() got %d paths, want %d", len(paths), len(want))
	}

	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("PluginSearchPaths()[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestDefaultPluginDirsPrependsEnvironmentOverride(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	t.Setenv("CORKSCREW_PLUGIN_DIR", "/custom/plugins")

	dirs := DefaultPluginDirs()
	if len(dirs) == 0 {
		t.Fatal("DefaultPluginDirs() returned no directories")
	}
	if dirs[0] != "/custom/plugins" {
		t.Fatalf("DefaultPluginDirs()[0] = %q, want environment override first", dirs[0])
	}
}

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

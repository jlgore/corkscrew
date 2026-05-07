package client

import (
	"path/filepath"
	"testing"
)

func TestPluginSearchPathsUseProviderBinaryName(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	paths := pluginSearchPaths("aws")
	want := []string{
		filepath.Join(".", "build", "bin", "aws-provider"),
		filepath.Join(".", "plugins", "aws-provider", "aws-provider"),
		filepath.Join("/home/tester", ".corkscrew", "plugins", "aws-provider"),
		filepath.Join("/home/tester", ".corkscrew", "bin", "plugin", "aws-provider"),
	}

	if len(paths) != len(want) {
		t.Fatalf("pluginSearchPaths() got %d paths, want %d", len(paths), len(want))
	}

	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("pluginSearchPaths()[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverDefaultInstallationsPrefersManagedOfficial(t *testing.T) {
	managed := t.TempDir()
	local := t.TempDir()
	writeDefaultManifest(t, managed, "aws", "managed")
	writeDefaultManifest(t, local, "aws", "local")
	writeDefaultManifest(t, local, "gcp", "local")

	installations, err := discoverDefaultInstallations([]Root{
		{Path: managed, Origin: OriginOfficial},
		{Path: local, Origin: OriginOfficial},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(installations) != 2 {
		t.Fatalf("installations = %#v, want managed aws and local gcp", installations)
	}
	versions := map[string]string{}
	for _, installation := range installations {
		versions[installation.Manifest.Manifest.Name] = installation.Manifest.Manifest.Version
	}
	if versions["aws"] != "managed" || versions["gcp"] != "local" {
		t.Fatalf("resolved versions = %v", versions)
	}
}

func writeDefaultManifest(t *testing.T, root, name, version string) {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schema_version":"1","name":"` + name + `","version":"` + version + `","protocol":2,"executable":"provider","capabilities":["batch_scan"],"default_scopes":["global"],"storage":{"mode":"table","table":"` + name + `_resources"}}`
	if err := os.WriteFile(filepath.Join(directory, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "provider"), []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
}

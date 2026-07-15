package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func TestRegistryDistinguishesOfficialAndCustomProviders(t *testing.T) {
	t.Parallel()

	official := []providercatalog.Provider{
		{Name: "aws", Description: "AWS", ResourceTable: "aws_resources"},
	}
	custom := ResolvedManifest{Manifest: Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Name:          "acme",
		Version:       "1.0.0",
		Protocol:      2,
		Executable:    "acme-provider",
		Capabilities:  []Capability{CapabilityBatchScan},
	}}

	registry, err := NewRegistry(official, []Installation{{Manifest: custom, Origin: OriginCustom}})
	if err != nil {
		t.Fatalf("NewRegistry(): %v", err)
	}
	providers := registry.List()
	if len(providers) != 2 {
		t.Fatalf("List() length = %d, want 2: %#v", len(providers), providers)
	}
	if providers[0].Name != "acme" || providers[0].Origin != OriginCustom || !providers[0].Installed {
		t.Fatalf("custom descriptor = %#v", providers[0])
	}
	if providers[1].Name != "aws" || providers[1].Origin != OriginOfficial || providers[1].Installed {
		t.Fatalf("official descriptor = %#v", providers[1])
	}

	custom.Manifest.Name = "aws"
	_, err = NewRegistry(official, []Installation{{Manifest: custom, Origin: OriginCustom}})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("custom official-name error = %v", err)
	}
}

func TestRegistryValidatesManifestStorageAgainstOfficialCatalog(t *testing.T) {
	t.Parallel()
	official := []providercatalog.Provider{{Name: "aws", Description: "AWS", ResourceTable: "aws_resources"}}
	manifest := ResolvedManifest{Manifest: Manifest{
		SchemaVersion: ManifestSchemaVersion, Name: "acme", Version: "1.0.0", Protocol: 2,
		Executable: "acme-provider", Capabilities: []Capability{CapabilityBatchScan},
		Storage: StorageManifest{Mode: "table", Table: "arbitrary_resources"},
	}}
	if _, err := NewRegistry(official, []Installation{{Manifest: manifest, Origin: OriginCustom}}); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("custom unregistered table error = %v", err)
	}

	manifest.Manifest.Storage.Table = "aws_resources"
	if _, err := NewRegistry(official, []Installation{{Manifest: manifest, Origin: OriginCustom}}); err != nil {
		t.Fatalf("custom registered table: %v", err)
	}

	manifest.Manifest.Name = "aws"
	manifest.Manifest.Storage.Table = "arbitrary_resources"
	if _, err := NewRegistry(official, []Installation{{Manifest: manifest, Origin: OriginOfficial}}); err == nil || !strings.Contains(err.Error(), "canonical table") {
		t.Fatalf("official mismatched table error = %v", err)
	}
}

func TestDiscoverInstallationsReadsManagedProviderDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schema_version":"1","name":"acme","version":"1.0.0","protocol":2,"executable":"acme-provider","capabilities":["batch_scan"]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acme-provider"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	installations, err := DiscoverInstallations([]Root{{Path: root, Origin: OriginCustom}})
	if err != nil {
		t.Fatalf("DiscoverInstallations(): %v", err)
	}
	if len(installations) != 1 || installations[0].Manifest.Manifest.Name != "acme" || installations[0].Origin != OriginCustom {
		t.Fatalf("installations = %#v", installations)
	}
}

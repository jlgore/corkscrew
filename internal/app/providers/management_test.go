package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func TestBuildAndInstallMakesManifestProviderRuntimeVisible(t *testing.T) {
	sourceRoot := t.TempDir()
	writeBuildFixture(t, sourceRoot, "fixture-cloud", "1.0.0")
	managedRoot := filepath.Join(t.TempDir(), "managed")

	result, err := BuildAndInstall(context.Background(), BuildRequest{
		Provider: "fixture-cloud", SourceRoot: sourceRoot, ManagedRoot: managedRoot,
	}, BuildDependencies{})
	if err != nil {
		t.Fatalf("BuildAndInstall(): %v", err)
	}
	if result.Manifest.Name != "fixture-cloud" || result.Origin != providerRuntime.OriginCustom {
		t.Fatalf("build result = %#v", result)
	}
	if filepath.Base(result.ManifestPath) != "plugin.json" {
		t.Fatalf("installed manifest = %q, want normalized plugin.json", result.ManifestPath)
	}

	installations, err := providerRuntime.DiscoverInstallations([]providerRuntime.Root{{Path: managedRoot, Origin: providerRuntime.OriginCustom}})
	if err != nil {
		t.Fatalf("discover managed installation: %v", err)
	}
	registry, err := providerRuntime.NewRegistry(providercatalog.Shipped(), installations)
	if err != nil {
		t.Fatalf("create runtime registry: %v", err)
	}
	descriptor, err := registry.Resolve("fixture-cloud")
	if err != nil {
		t.Fatalf("resolve built provider: %v", err)
	}
	if !descriptor.Installed || descriptor.Origin != providerRuntime.OriginCustom || descriptor.Version != "1.0.0" {
		t.Fatalf("runtime descriptor = %#v", descriptor)
	}
}

func TestBuildAndInstallOfficialProviderUsesOfficialProvenance(t *testing.T) {
	sourceRoot := t.TempDir()
	writeBuildFixture(t, sourceRoot, "aws", "9.0.0")
	managedRoot := filepath.Join(t.TempDir(), "managed")

	result, err := BuildAndInstall(context.Background(), BuildRequest{
		Provider: "aws", SourceRoot: sourceRoot, ManagedRoot: managedRoot,
	}, BuildDependencies{})
	if err != nil {
		t.Fatalf("BuildAndInstall(): %v", err)
	}
	if result.Origin != providerRuntime.OriginOfficial {
		t.Fatalf("origin = %q, want official", result.Origin)
	}
	installations, err := providerRuntime.DiscoverInstallations([]providerRuntime.Root{{Path: filepath.Join(managedRoot, "official"), Origin: providerRuntime.OriginOfficial}})
	if err != nil || len(installations) != 1 || installations[0].Origin != providerRuntime.OriginOfficial {
		t.Fatalf("official installations = %#v, %v", installations, err)
	}
}

func TestBuildFailurePreservesManagedInstallation(t *testing.T) {
	sourceRoot := t.TempDir()
	writeBuildFixture(t, sourceRoot, "fixture-cloud", "1.0.0")
	managedRoot := filepath.Join(t.TempDir(), "managed")
	if _, err := BuildAndInstall(context.Background(), BuildRequest{Provider: "fixture-cloud", SourceRoot: sourceRoot, ManagedRoot: managedRoot}, BuildDependencies{}); err != nil {
		t.Fatalf("install first version: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "fixture-cloud-provider", "main.go"), []byte("package main\nfunc broken("), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAndInstall(context.Background(), BuildRequest{Provider: "fixture-cloud", SourceRoot: sourceRoot, ManagedRoot: managedRoot}, BuildDependencies{}); err == nil {
		t.Fatal("broken build succeeded")
	}
	installed, err := providerRuntime.LoadManifestDirectory(filepath.Join(managedRoot, "fixture-cloud"))
	if err != nil || installed.Manifest.Version != "1.0.0" {
		t.Fatalf("preserved installation = %#v, %v", installed, err)
	}
}

func writeBuildFixture(t *testing.T, root, name, version string) {
	t.Helper()
	directory := filepath.Join(root, name+"-provider")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	storage := "storage:\n  mode: generic\n"
	if provider, ok := providercatalog.Lookup(name); ok {
		storage = "storage:\n  mode: table\n  table: " + provider.ResourceTable + "\n"
	}
	files := map[string]string{
		"go.mod":      "module example.test/" + name + "\n\ngo 1.24.0\n",
		"main.go":     "package main\nfunc main() {}\n",
		"plugin.yaml": "schema_version: \"1\"\nname: " + name + "\nversion: \"" + version + "\"\nprotocol: 2\nexecutable: " + name + "-provider\ncapabilities: [batch_scan]\ndefault_scopes: [global]\n" + storage,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

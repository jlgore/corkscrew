package provider

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func TestInstallCustomCopiesExecutableAndNormalizesManifest(t *testing.T) {
	t.Parallel()

	source := t.TempDir()
	executable := filepath.Join(source, "acme-provider")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(source, "plugin.yaml")
	manifest := `schema_version: "1"
name: acme
version: 1.0.0
protocol: 2
executable: acme-provider
capabilities: [batch_scan]
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	var warnings bytes.Buffer
	installed, err := InstallCustom(manifestPath, t.TempDir(), providercatalog.Shipped(), &warnings)
	if err != nil {
		t.Fatalf("InstallCustom(): %v", err)
	}
	if filepath.Base(installed.ManifestPath) != "plugin.json" {
		t.Fatalf("manifest path = %q, want normalized plugin.json", installed.ManifestPath)
	}
	data, err := os.ReadFile(installed.ExecutablePath)
	if err != nil || string(data) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("installed executable data = %q, err=%v", data, err)
	}
	if !strings.Contains(warnings.String(), "unsigned custom provider") {
		t.Fatalf("warning = %q, want unsigned custom provider warning", warnings.String())
	}
}

func TestInstallCustomReplacesManagedInstallation(t *testing.T) {
	source := t.TempDir()
	executable := filepath.Join(source, "acme-provider")
	if err := os.WriteFile(executable, []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(source, "plugin.json")
	writeManifest := func(version string) {
		t.Helper()
		manifest := `{"schema_version":"1","name":"acme","version":"` + version + `","protocol":2,"executable":"acme-provider","capabilities":["batch_scan"]}`
		if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	destination := t.TempDir()
	writeManifest("1.0.0")
	if _, err := InstallCustom(manifestPath, destination, providercatalog.Shipped(), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("version-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest("2.0.0")
	installed, err := InstallCustom(manifestPath, destination, providercatalog.Shipped(), nil)
	if err != nil {
		t.Fatalf("replace installation: %v", err)
	}
	data, err := os.ReadFile(installed.ExecutablePath)
	if err != nil || string(data) != "version-two" || installed.Manifest.Version != "2.0.0" {
		t.Fatalf("replacement = version %s data %q, err %v", installed.Manifest.Version, data, err)
	}
}

func TestInstallValidatesStorageRegistrationBeforeActivation(t *testing.T) {
	t.Parallel()

	writePackage := func(t *testing.T, name, storage string) string {
		t.Helper()
		source := t.TempDir()
		executable := filepath.Join(source, name+"-provider")
		if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(source, "plugin.yaml")
		manifest := "schema_version: \"1\"\nname: " + name + "\nversion: 1.0.0\nprotocol: 2\nexecutable: " + name + "-provider\ncapabilities: [batch_scan]\n" + storage
		if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		return manifestPath
	}

	custom := writePackage(t, "acme", "storage:\n  mode: table\n  table: arbitrary_resources\n")
	if _, err := InstallCustom(custom, t.TempDir(), providercatalog.Shipped(), nil); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("custom storage error = %v", err)
	}

	official := writePackage(t, "aws", "storage:\n  mode: generic\n")
	if _, err := InstallOfficial(official, t.TempDir(), providercatalog.Shipped()); err == nil || !strings.Contains(err.Error(), "canonical table") {
		t.Fatalf("official storage error = %v", err)
	}
}

package provider

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadManifestAcceptsEquivalentJSONAndYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "plugin.json")
	yamlPath := filepath.Join(dir, "plugin.yaml")
	jsonData := `{
  "schema_version": "1",
  "name": "acme",
  "version": "1.2.3",
  "protocol": 2,
  "executable": "acme-provider",
  "capabilities": ["batch_scan", "describe"],
  "default_scopes": ["global"]
}`
	yamlData := `schema_version: "1"
name: acme
version: 1.2.3
protocol: 2
executable: acme-provider
capabilities:
  - batch_scan
  - describe
default_scopes:
  - global
`
	if err := os.WriteFile(jsonPath, []byte(jsonData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yamlPath, []byte(yamlData), 0o600); err != nil {
		t.Fatal(err)
	}

	fromJSON, err := LoadManifest(jsonPath)
	if err != nil {
		t.Fatalf("LoadManifest(JSON): %v", err)
	}
	fromYAML, err := LoadManifest(yamlPath)
	if err != nil {
		t.Fatalf("LoadManifest(YAML): %v", err)
	}
	if !reflect.DeepEqual(fromJSON, fromYAML) {
		t.Fatalf("manifests differ:\nJSON: %#v\nYAML: %#v", fromJSON, fromYAML)
	}
}

func TestManifestValidateRejectsInvalidContract(t *testing.T) {
	t.Parallel()

	valid := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Name:          "acme",
		Version:       "1.2.3",
		Protocol:      2,
		Executable:    "acme-provider",
		Capabilities:  []Capability{CapabilityBatchScan},
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{name: "schema", mutate: func(m *Manifest) { m.SchemaVersion = "2" }, want: "schema_version"},
		{name: "name", mutate: func(m *Manifest) { m.Name = "Bad Name" }, want: "name"},
		{name: "version", mutate: func(m *Manifest) { m.Version = "" }, want: "version"},
		{name: "protocol", mutate: func(m *Manifest) { m.Protocol = 3 }, want: "protocol"},
		{name: "executable", mutate: func(m *Manifest) { m.Executable = "../escape" }, want: "executable"},
		{name: "capabilities", mutate: func(m *Manifest) { m.Capabilities = nil }, want: "capabilities"},
		{name: "unknown capability", mutate: func(m *Manifest) { m.Capabilities = []Capability{"teleport"} }, want: "capability"},
		{name: "specialized table", mutate: func(m *Manifest) { m.Storage = StorageManifest{Mode: "table"} }, want: "storage.table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := valid
			tt.mutate(&manifest)
			err := manifest.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadManifestDirectoryRequiresOneManifestAndExecutable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	executable := filepath.Join(dir, "acme-provider")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifestData := `{"schema_version":"1","name":"acme","version":"1.0.0","protocol":2,"executable":"acme-provider","capabilities":["batch_scan"]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifestData), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := LoadManifestDirectory(dir)
	if err != nil {
		t.Fatalf("LoadManifestDirectory(): %v", err)
	}
	if resolved.Manifest.Name != "acme" || resolved.ExecutablePath != executable {
		t.Fatalf("resolved manifest = %#v", resolved)
	}

	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifestData), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifestDirectory(dir); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("duplicate manifest error = %v, want multiple-manifest error", err)
	}
}

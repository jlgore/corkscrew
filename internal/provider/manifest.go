// Package provider owns runtime provider registration and process lifecycle.
package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const ManifestSchemaVersion = "1"

var (
	providerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	tableNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type Capability string

const (
	CapabilityDiscover   Capability = "discover"
	CapabilityList       Capability = "list"
	CapabilityDescribe   Capability = "describe"
	CapabilitySchemas    Capability = "schemas"
	CapabilityBatchScan  Capability = "batch_scan"
	CapabilityStreamScan Capability = "stream_scan"
)

type StorageManifest struct {
	Mode  string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Table string `json:"table,omitempty" yaml:"table,omitempty"`
}

type Manifest struct {
	SchemaVersion string          `json:"schema_version" yaml:"schema_version"`
	Name          string          `json:"name" yaml:"name"`
	DisplayName   string          `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Description   string          `json:"description,omitempty" yaml:"description,omitempty"`
	Version       string          `json:"version" yaml:"version"`
	Protocol      int             `json:"protocol" yaml:"protocol"`
	Executable    string          `json:"executable" yaml:"executable"`
	Capabilities  []Capability    `json:"capabilities" yaml:"capabilities"`
	DefaultScopes []string        `json:"default_scopes,omitempty" yaml:"default_scopes,omitempty"`
	Storage       StorageManifest `json:"storage,omitempty" yaml:"storage,omitempty"`
}

type ResolvedManifest struct {
	Manifest       Manifest
	ManifestPath   string
	ExecutablePath string
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("schema_version must be %q", ManifestSchemaVersion)
	}
	if !providerNamePattern.MatchString(m.Name) {
		return fmt.Errorf("name must contain lowercase letters, digits, and hyphens and start with a letter")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if m.Protocol != 2 {
		return fmt.Errorf("protocol must be 2")
	}
	if m.Executable == "" || filepath.Base(m.Executable) != m.Executable || m.Executable == "." {
		return fmt.Errorf("executable must be a filename relative to the manifest directory")
	}
	if len(m.Capabilities) == 0 {
		return fmt.Errorf("capabilities must not be empty")
	}
	known := map[Capability]bool{
		CapabilityDiscover: true, CapabilityList: true, CapabilityDescribe: true,
		CapabilitySchemas: true, CapabilityBatchScan: true, CapabilityStreamScan: true,
	}
	seen := make(map[Capability]bool, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if !known[capability] {
			return fmt.Errorf("unknown capability %q", capability)
		}
		if seen[capability] {
			return fmt.Errorf("duplicate capability %q", capability)
		}
		seen[capability] = true
	}
	mode := m.Storage.Mode
	if mode == "" {
		mode = "generic"
	}
	switch mode {
	case "generic":
		if m.Storage.Table != "" {
			return fmt.Errorf("storage.table is only valid when storage.mode is table")
		}
	case "table":
		if !tableNamePattern.MatchString(m.Storage.Table) {
			return fmt.Errorf("storage.table must be a valid table identifier")
		}
	default:
		return fmt.Errorf("storage.mode must be generic or table")
	}
	return nil
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read provider manifest: %w", err)
	}

	var manifest Manifest
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &manifest); err != nil {
			return Manifest{}, fmt.Errorf("parse JSON provider manifest: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return Manifest{}, fmt.Errorf("parse YAML provider manifest: %w", err)
		}
	default:
		return Manifest{}, fmt.Errorf("unsupported provider manifest format %q", filepath.Ext(path))
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate provider manifest: %w", err)
	}
	return manifest, nil
}

// manifestFilenames are the recognized plugin manifest names, in precedence order.
var manifestFilenames = []string{"plugin.json", "plugin.yaml", "plugin.yml"}

// manifestCandidates returns the plugin manifest files present in dir.
func manifestCandidates(dir string) []string {
	var paths []string
	for _, name := range manifestFilenames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			paths = append(paths, path)
		}
	}
	return paths
}

// FindManifestFile returns the single plugin manifest file in dir. It errors if
// the directory contains no manifest or more than one.
func FindManifestFile(dir string) (string, error) {
	paths := manifestCandidates(dir)
	if len(paths) == 0 {
		return "", fmt.Errorf("provider directory %s contains no plugin manifest", dir)
	}
	if len(paths) > 1 {
		return "", fmt.Errorf("provider directory %s contains multiple plugin manifests", dir)
	}
	return paths[0], nil
}

func LoadManifestDirectory(dir string) (ResolvedManifest, error) {
	manifestPath, err := FindManifestFile(dir)
	if err != nil {
		return ResolvedManifest{}, err
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return ResolvedManifest{}, err
	}
	executablePath := filepath.Join(dir, manifest.Executable)
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return ResolvedManifest{}, fmt.Errorf("resolve provider directory: %w", err)
	}
	resolvedExecutable, err := filepath.EvalSymlinks(executablePath)
	if err != nil {
		return ResolvedManifest{}, fmt.Errorf("resolve provider executable: %w", err)
	}
	relative, err := filepath.Rel(resolvedDir, resolvedExecutable)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ResolvedManifest{}, fmt.Errorf("provider executable escapes manifest directory")
	}
	info, err := os.Stat(resolvedExecutable)
	if err != nil {
		return ResolvedManifest{}, fmt.Errorf("stat provider executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ResolvedManifest{}, fmt.Errorf("provider executable must be a regular executable file")
	}
	return ResolvedManifest{
		Manifest:       manifest,
		ManifestPath:   manifestPath,
		ExecutablePath: resolvedExecutable,
	}, nil
}

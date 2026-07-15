package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

type Origin string

const (
	OriginOfficial Origin = "official"
	OriginCustom   Origin = "custom"
)

type Installation struct {
	Manifest ResolvedManifest
	Origin   Origin
}

type Root struct {
	Path   string
	Origin Origin
}

func DiscoverInstallations(roots []Root) ([]Installation, error) {
	var result []Installation
	for _, root := range roots {
		entries, err := os.ReadDir(root.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read provider root %s: %w", root.Path, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			providerDir := filepath.Join(root.Path, entry.Name())
			if !containsManifest(providerDir) {
				continue
			}
			resolved, err := LoadManifestDirectory(providerDir)
			if err != nil {
				return nil, fmt.Errorf("load provider directory %s: %w", entry.Name(), err)
			}
			if resolved.Manifest.Name != entry.Name() {
				return nil, fmt.Errorf("provider directory %q contains manifest for %q", entry.Name(), resolved.Manifest.Name)
			}
			result = append(result, Installation{Manifest: resolved, Origin: root.Origin})
		}
	}
	return result, nil
}

func containsManifest(dir string) bool {
	return len(manifestCandidates(dir)) > 0
}

type Descriptor struct {
	Name           string
	DisplayName    string
	Description    string
	Version        string
	Origin         Origin
	Installed      bool
	ExecutablePath string
	Capabilities   []Capability
	DefaultScopes  []string
	ResourceTable  string
}

func (d Descriptor) Supports(capability Capability) bool {
	for _, candidate := range d.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

type Registry struct {
	descriptors map[string]Descriptor
}

func NewRegistry(official []providercatalog.Provider, installations []Installation) (*Registry, error) {
	descriptors := make(map[string]Descriptor, len(official)+len(installations))
	officialNames := make(map[string]providercatalog.Provider, len(official))
	for _, provider := range official {
		if provider.ResourceTable == "" {
			return nil, fmt.Errorf("official provider %q has no canonical resource table", provider.Name)
		}
		officialNames[provider.Name] = provider
		descriptors[provider.Name] = Descriptor{
			Name:          provider.Name,
			DisplayName:   provider.Name,
			Description:   provider.Description,
			Origin:        OriginOfficial,
			ResourceTable: provider.ResourceTable,
		}
	}

	for _, installation := range installations {
		manifest := installation.Manifest.Manifest
		if err := manifest.Validate(); err != nil {
			return nil, fmt.Errorf("provider %q: %w", manifest.Name, err)
		}
		catalogProvider, reserved := officialNames[manifest.Name]
		if existing, exists := descriptors[manifest.Name]; exists && existing.Installed {
			return nil, fmt.Errorf("provider %q has multiple installations", manifest.Name)
		}

		// resourceTableForRegistration is the single owner of the reserved-name
		// and official-catalog validation; NewRegistry does not repeat it.
		resourceTable, err := resourceTableForRegistration(manifest, installation.Origin, official)
		if err != nil {
			return nil, err
		}
		description := manifest.Description
		if reserved {
			if description == "" {
				description = catalogProvider.Description
			}
		}
		displayName := manifest.DisplayName
		if displayName == "" {
			displayName = manifest.Name
		}
		descriptors[manifest.Name] = Descriptor{
			Name:           manifest.Name,
			DisplayName:    displayName,
			Description:    description,
			Version:        manifest.Version,
			Origin:         installation.Origin,
			Installed:      true,
			ExecutablePath: installation.Manifest.ExecutablePath,
			Capabilities:   append([]Capability(nil), manifest.Capabilities...),
			DefaultScopes:  append([]string(nil), manifest.DefaultScopes...),
			ResourceTable:  resourceTable,
		}
	}
	return &Registry{descriptors: descriptors}, nil
}

func resourceTableForRegistration(manifest Manifest, origin Origin, official []providercatalog.Provider) (string, error) {
	var catalogProvider providercatalog.Provider
	reserved := false
	registeredTables := make(map[string]bool, len(official))
	for _, provider := range official {
		registeredTables[provider.ResourceTable] = provider.ResourceTable != ""
		if provider.Name == manifest.Name {
			catalogProvider = provider
			reserved = true
		}
	}

	if origin == OriginOfficial {
		if !reserved {
			return "", fmt.Errorf("provider %q is not in the official catalog", manifest.Name)
		}
		if manifest.Storage.Mode != "table" || manifest.Storage.Table != catalogProvider.ResourceTable {
			return "", fmt.Errorf("official provider %q storage table must be canonical table %q", manifest.Name, catalogProvider.ResourceTable)
		}
		return catalogProvider.ResourceTable, nil
	}
	if reserved {
		return "", fmt.Errorf("provider name %q is reserved for an official provider", manifest.Name)
	}
	if manifest.Storage.Mode == "table" {
		if !registeredTables[manifest.Storage.Table] {
			return "", fmt.Errorf("custom provider %q storage table %q is not registered by the official catalog", manifest.Name, manifest.Storage.Table)
		}
		return manifest.Storage.Table, nil
	}
	return providercatalog.CustomResourceTable, nil
}

func (r *Registry) List() []Descriptor {
	result := make([]Descriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		result = append(result, descriptor)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *Registry) Resolve(name string) (Descriptor, error) {
	descriptor, ok := r.descriptors[name]
	if !ok {
		return Descriptor{}, fmt.Errorf("provider %q is not registered", name)
	}
	if !descriptor.Installed {
		return Descriptor{}, fmt.Errorf("provider %q is not installed", name)
	}
	return descriptor, nil
}

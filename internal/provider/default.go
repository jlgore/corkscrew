package provider

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func ManagedPluginRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".corkscrew", "plugins"), nil
}

func DefaultRoots() ([]Root, error) {
	managed, err := ManagedPluginRoot()
	if err != nil {
		return nil, err
	}
	localOfficial := filepath.Join("build", "bin", "plugins", "official")
	roots := []Root{{Path: filepath.Join(managed, "official"), Origin: OriginOfficial}}
	if info, statErr := os.Stat(localOfficial); statErr == nil && info.IsDir() {
		roots = append(roots, Root{Path: localOfficial, Origin: OriginOfficial})
	}
	if custom := strings.TrimSpace(os.Getenv("CORKSCREW_PLUGIN_DIR")); custom != "" {
		roots = append(roots, Root{Path: custom, Origin: OriginCustom})
	}
	roots = append(roots, Root{Path: managed, Origin: OriginCustom})
	return roots, nil
}

func discoverDefaultInstallations(roots []Root) ([]Installation, error) {
	var installations []Installation
	managedOfficial := make(map[string]bool)
	for _, root := range roots {
		discovered, err := DiscoverInstallations([]Root{root})
		if err != nil {
			return nil, err
		}
		for _, installation := range discovered {
			name := installation.Manifest.Manifest.Name
			if root.Origin == OriginOfficial {
				if managedOfficial[name] {
					continue
				}
				managedOfficial[name] = true
			}
			installations = append(installations, installation)
		}
	}
	return installations, nil
}

func NewDefaultRuntime(warnings io.Writer) (*Runtime, error) {
	roots, err := DefaultRoots()
	if err != nil {
		return nil, err
	}
	installations, err := discoverDefaultInstallations(roots)
	if err != nil {
		return nil, err
	}
	registry, err := NewRegistry(providercatalog.Shipped(), installations)
	if err != nil {
		return nil, err
	}
	return NewRuntime(registry, HashicorpLauncher{}, warnings), nil
}

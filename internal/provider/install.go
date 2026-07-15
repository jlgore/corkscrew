package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func InstallCustom(manifestPath, destinationRoot string, official []providercatalog.Provider, warnings io.Writer) (ResolvedManifest, error) {
	source, err := LoadManifestDirectory(filepath.Dir(manifestPath))
	if err != nil {
		return ResolvedManifest{}, err
	}
	if _, err := resourceTableForRegistration(source.Manifest, OriginCustom, official); err != nil {
		return ResolvedManifest{}, err
	}
	installed, err := installManaged(source, destinationRoot)
	if err != nil {
		return ResolvedManifest{}, err
	}
	if warnings != nil {
		fmt.Fprintf(warnings, "warning: installing unsigned custom provider %q from local executable %s\n", source.Manifest.Name, source.ExecutablePath)
	}
	return installed, nil
}

// InstallOfficial atomically activates a manifest package for a shipped
// provider under the official managed root.
func InstallOfficial(manifestPath, destinationRoot string, official []providercatalog.Provider) (ResolvedManifest, error) {
	source, err := LoadManifestDirectory(filepath.Dir(manifestPath))
	if err != nil {
		return ResolvedManifest{}, err
	}
	if _, err := resourceTableForRegistration(source.Manifest, OriginOfficial, official); err != nil {
		return ResolvedManifest{}, err
	}
	return installManaged(source, destinationRoot)
}

func installManaged(source ResolvedManifest, destinationRoot string) (ResolvedManifest, error) {
	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		return ResolvedManifest{}, fmt.Errorf("create managed provider directory: %w", err)
	}
	destination := filepath.Join(destinationRoot, source.Manifest.Name)
	destinationExists := false
	if info, err := os.Stat(destination); err == nil {
		if !info.IsDir() {
			return ResolvedManifest{}, fmt.Errorf("managed provider path %q is not a directory", destination)
		}
		destinationExists = true
	} else if !os.IsNotExist(err) {
		return ResolvedManifest{}, fmt.Errorf("inspect managed provider directory: %w", err)
	}

	temporary, err := os.MkdirTemp(destinationRoot, ".install-"+source.Manifest.Name+"-")
	if err != nil {
		return ResolvedManifest{}, fmt.Errorf("create temporary provider directory: %w", err)
	}
	defer os.RemoveAll(temporary)

	executableDestination := filepath.Join(temporary, source.Manifest.Executable)
	if err := copyExecutable(source.ExecutablePath, executableDestination); err != nil {
		return ResolvedManifest{}, err
	}
	manifestData, err := json.MarshalIndent(source.Manifest, "", "  ")
	if err != nil {
		return ResolvedManifest{}, fmt.Errorf("encode managed provider manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(filepath.Join(temporary, "plugin.json"), manifestData, 0o600); err != nil {
		return ResolvedManifest{}, fmt.Errorf("write managed provider manifest: %w", err)
	}
	backup := ""
	if destinationExists {
		backupDir, err := os.MkdirTemp(destinationRoot, ".replace-"+source.Manifest.Name+"-")
		if err != nil {
			return ResolvedManifest{}, fmt.Errorf("create provider replacement path: %w", err)
		}
		if err := os.Remove(backupDir); err != nil {
			return ResolvedManifest{}, fmt.Errorf("prepare provider replacement path: %w", err)
		}
		backup = backupDir
		if err := os.Rename(destination, backup); err != nil {
			return ResolvedManifest{}, fmt.Errorf("deactivate managed provider installation: %w", err)
		}
	}
	if err := os.Rename(temporary, destination); err != nil {
		if backup != "" {
			_ = os.Rename(backup, destination)
		}
		return ResolvedManifest{}, fmt.Errorf("activate managed provider installation: %w", err)
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return LoadManifestDirectory(destination)
}

func copyExecutable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open provider executable: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return fmt.Errorf("create managed provider executable: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy provider executable: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close managed provider executable: %w", err)
	}
	return nil
}

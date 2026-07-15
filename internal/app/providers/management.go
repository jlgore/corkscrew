package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

type BuildRequest struct {
	Provider    string
	SourceRoot  string
	ManagedRoot string
}

type BuildDependencies struct {
	RunBuild func(context.Context, string, string) (string, error)
	Warnings io.Writer
}

type BuildResult struct {
	providerRuntime.ResolvedManifest
	Origin      providerRuntime.Origin
	BuildOutput string
}

func ListManaged(warnings io.Writer) ([]providerRuntime.Descriptor, error) {
	runtime, err := providerRuntime.NewDefaultRuntime(warnings)
	if err != nil {
		return nil, err
	}
	defer runtime.Close()
	return runtime.List(), nil
}

func InstallCustomManifest(manifestPath string, warnings io.Writer) (providerRuntime.ResolvedManifest, error) {
	root, err := providerRuntime.ManagedPluginRoot()
	if err != nil {
		return providerRuntime.ResolvedManifest{}, err
	}
	return providerRuntime.InstallCustom(manifestPath, root, providercatalog.Shipped(), warnings)
}

// BuildAndInstall compiles a provider manifest package and atomically activates
// it in the managed runtime registry. A build is successful only after managed
// installation succeeds.
func BuildAndInstall(ctx context.Context, request BuildRequest, dependencies BuildDependencies) (BuildResult, error) {
	name := strings.TrimSpace(request.Provider)
	if name == "" {
		return BuildResult{}, fmt.Errorf("provider is required")
	}
	sourceRoot := strings.TrimSpace(request.SourceRoot)
	if sourceRoot == "" {
		sourceRoot = "plugins"
	}
	sourceDirectory := filepath.Join(sourceRoot, name+"-provider")
	manifestPath, manifest, err := sourceManifest(sourceDirectory)
	if err != nil {
		return BuildResult{}, err
	}
	if manifest.Name != name {
		return BuildResult{}, fmt.Errorf("provider source %q contains manifest for %q", name, manifest.Name)
	}

	managedRoot := strings.TrimSpace(request.ManagedRoot)
	if managedRoot == "" {
		managedRoot, err = providerRuntime.ManagedPluginRoot()
		if err != nil {
			return BuildResult{}, err
		}
	}
	staging, err := os.MkdirTemp("", "corkscrew-provider-build-")
	if err != nil {
		return BuildResult{}, fmt.Errorf("create provider build staging: %w", err)
	}
	defer os.RemoveAll(staging)

	executablePath := filepath.Join(staging, manifest.Executable)
	runner := dependencies.RunBuild
	if runner == nil {
		runner = runGoBuild
	}
	buildOutput, err := runner(ctx, sourceDirectory, executablePath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("build provider %q: %w%s", name, err, formattedBuildOutput(buildOutput))
	}
	if err := os.Chmod(executablePath, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("mark provider executable: %w", err)
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("read provider manifest package: %w", err)
	}
	stagedManifest := filepath.Join(staging, filepath.Base(manifestPath))
	if err := os.WriteFile(stagedManifest, manifestData, 0o600); err != nil {
		return BuildResult{}, fmt.Errorf("stage provider manifest: %w", err)
	}
	if _, err := providerRuntime.LoadManifestDirectory(staging); err != nil {
		return BuildResult{}, fmt.Errorf("validate built provider package: %w", err)
	}

	origin := providerRuntime.OriginCustom
	destination := managedRoot
	if isOfficialProvider(name) {
		origin = providerRuntime.OriginOfficial
		destination = filepath.Join(managedRoot, "official")
	}
	var installed providerRuntime.ResolvedManifest
	if origin == providerRuntime.OriginOfficial {
		installed, err = providerRuntime.InstallOfficial(stagedManifest, destination, providercatalog.Shipped())
	} else {
		installed, err = providerRuntime.InstallCustom(stagedManifest, destination, providercatalog.Shipped(), dependencies.Warnings)
	}
	if err != nil {
		return BuildResult{}, fmt.Errorf("install built provider %q: %w", name, err)
	}
	return BuildResult{ResolvedManifest: installed, Origin: origin, BuildOutput: buildOutput}, nil
}

func isOfficialProvider(name string) bool {
	for _, provider := range providercatalog.Shipped() {
		if provider.Name == name {
			return true
		}
	}
	return false
}

func sourceManifest(directory string) (string, providerRuntime.Manifest, error) {
	manifestPath, err := providerRuntime.FindManifestFile(directory)
	if err != nil {
		return "", providerRuntime.Manifest{}, err
	}
	manifest, err := providerRuntime.LoadManifest(manifestPath)
	if err != nil {
		return "", providerRuntime.Manifest{}, err
	}
	return manifestPath, manifest, nil
}

func runGoBuild(ctx context.Context, sourceDirectory, outputPath string) (string, error) {
	command := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", outputPath, ".")
	command.Dir = sourceDirectory
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.String(), err
}

func formattedBuildOutput(output string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	return ":\n" + strings.TrimSpace(output)
}

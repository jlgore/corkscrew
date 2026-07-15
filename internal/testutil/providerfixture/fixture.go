// Package providerfixture builds Corkscrew's hermetic protocol-v2 test provider.
package providerfixture

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const Name = "fixture-cloud"

type Fixture struct {
	ManifestPath   string
	ExecutablePath string
	StateDirectory string
}

// Build compiles the local fixture provider and packages it beside its YAML
// manifest. It never reads a user directory or contacts an external service.
func Build(t testing.TB, version string) Fixture {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate provider fixture source")
	}
	packageDirectory := filepath.Dir(filename)
	sourceDirectory := filepath.Join(packageDirectory, "testdata", "provider")
	destination := t.TempDir()
	executable := filepath.Join(destination, "fixture-provider")

	command := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", executable, ".")
	command.Dir = sourceDirectory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(os.TempDir(), "corkscrew-gocache"),
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fixture provider: %v\n%s", err, output)
	}

	manifest, err := os.ReadFile(filepath.Join(packageDirectory, "testdata", "plugin.yaml"))
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	manifest = []byte(strings.ReplaceAll(string(manifest), "VERSION", version))
	manifestPath := filepath.Join(destination, "plugin.yaml")
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}

	stateDirectory := filepath.Join(destination, "state")
	if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
		t.Fatalf("create fixture state directory: %v", err)
	}
	return Fixture{ManifestPath: manifestPath, ExecutablePath: executable, StateDirectory: stateDirectory}
}

func (f Fixture) Config(session string) map[string]string {
	return map[string]string{
		"session":    session,
		"state_dir":  f.StateDirectory,
		"fail_scope": "scope-fail",
	}
}

func (f Fixture) String() string {
	return fmt.Sprintf("%s (%s)", Name, f.ExecutablePath)
}

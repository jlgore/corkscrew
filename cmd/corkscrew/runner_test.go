package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
	"github.com/jlgore/corkscrew/internal/testutil/providerfixture"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func TestRunCLIReturnsTopLevelExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "no args", args: nil, want: 1},
		{name: "help", args: []string{"help"}, want: 0},
		{name: "version", args: []string{"--version"}, want: 0},
		{name: "unknown command", args: []string{"wat"}, want: 1},
		{name: "unavailable command", args: []string{"diagram"}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := captureCLIOutput(t, func() int {
				return runCLI(tt.args)
			})
			if got != tt.want {
				t.Fatalf("runCLI(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunCLIListsManagedCustomProvider(t *testing.T) {
	fixture := providerfixture.Build(t, "1.0.0")
	home := t.TempDir()
	t.Setenv("HOME", home)
	managedRoot := filepath.Join(home, ".corkscrew", "plugins")
	if _, err := providerRuntime.InstallCustom(fixture.ManifestPath, managedRoot, providercatalog.Shipped(), nil); err != nil {
		t.Fatalf("install fixture provider: %v", err)
	}

	code, output := captureCLIOutputText(t, func() int {
		return runCLI([]string{"plugin", "list"})
	})
	if code != 0 {
		t.Fatalf("plugin list exit code = %d, output = %q", code, output)
	}
	if !strings.Contains(output, "fixture-cloud") || !strings.Contains(output, "custom") || !strings.Contains(output, "Installed") {
		t.Fatalf("plugin list output = %q, want installed custom fixture provider", output)
	}
}

func TestRunCLIPluginBuildAutomaticallyInstallsRuntimeVisibleProvider(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "plugins", "fixture-cloud-provider")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":  "module example.test/fixture-cloud\n\ngo 1.24.0\n",
		"main.go": "package main\nfunc main() {}\n",
		"plugin.yaml": `schema_version: "1"
name: fixture-cloud
version: "2.0.0"
protocol: 2
executable: fixture-cloud-provider
capabilities: [batch_scan]
default_scopes: [global]
storage:
  mode: generic
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	home := t.TempDir()
	t.Setenv("HOME", home)

	code, output := captureCLIOutputText(t, func() int {
		return runCLI([]string{"plugin", "build", "fixture-cloud"})
	})
	if code != 0 || !strings.Contains(output, "Built and installed fixture-cloud 2.0.0 (custom)") {
		t.Fatalf("plugin build exit=%d output=%q", code, output)
	}
	code, output = captureCLIOutputText(t, func() int {
		return runCLI([]string{"plugin", "list"})
	})
	if code != 0 || !strings.Contains(output, "fixture-cloud") || !strings.Contains(output, "Installed") {
		t.Fatalf("plugin list after build exit=%d output=%q", code, output)
	}
}

func captureCLIOutput(t *testing.T, fn func() int) int {
	t.Helper()
	code, _ := captureCLIOutputText(t, fn)
	return code
}

func captureCLIOutputText(t *testing.T, fn func() int) (int, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	var wg sync.WaitGroup
	var stdoutData, stderrData []byte
	wg.Add(2)
	go func() {
		defer wg.Done()
		stdoutData, _ = io.ReadAll(stdoutR)
	}()
	go func() {
		defer wg.Done()
		stderrData, _ = io.ReadAll(stderrR)
	}()

	code := fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	wg.Wait()

	os.Stdout = oldStdout
	os.Stderr = oldStderr
	_ = stdoutR.Close()
	_ = stderrR.Close()

	return code, string(append(stdoutData, stderrData...))
}

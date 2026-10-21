package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/internal/archive"
	appconfig "github.com/jlgore/corkscrew/internal/config"
)

// InitConfig represents the initialization configuration
type InitConfig struct {
	CorkscrewDir  string
	BinDir        string
	PluginDir     string
	ConfigDir     string
	DepsDir       string
	ProtocVersion string
	DuckDBVersion string
}

type CorkscrewConfig = appconfig.CorkscrewConfig
type ProviderConfig = appconfig.CloudProviderConfig
type DependenciesConfig = appconfig.DependenciesConfig
type DependencyConfig = appconfig.DependencyConfig
type DatabaseConfig = appconfig.DatabaseConfig
type QueryConfig = appconfig.QueryConfig
type ComplianceConfig = appconfig.ComplianceConfig
type LoggingConfig = appconfig.LoggingConfig
type OutputConfig = appconfig.OutputConfig

// DependencyInfo represents a dependency to download
type DependencyInfo struct {
	Name        string
	Version     string
	Platform    string
	URL         string
	ArchiveName string
	BinaryName  string
	Size        int64
}

// ProgressTracker tracks download progress
type ProgressTracker struct {
	Name      string
	Total     int64
	Current   int64
	StartTime time.Time
}

// runInit takes the CLI root context: Ctrl-C cancels in-flight downloads and kills
// generator/build subprocesses instead of leaving them running after init exits.
func runInit(ctx context.Context, args []string) {
	// Check for help first
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			printInitUsage()
			return
		}
	}

	fmt.Println("🚀 Initializing Corkscrew v2.0.0...")
	fmt.Println()

	// Parse flags
	dryRun := false
	upgrade := false
	for _, arg := range args {
		if arg == "--dry-run" {
			dryRun = true
		}
		if arg == "--upgrade" {
			upgrade = true
		}
	}

	// Get current user
	usr, err := user.Current()
	if err != nil {
		fmt.Printf("❌ Failed to get current user: %v\n", err)
		os.Exit(1)
	}

	// Setup configuration
	config := &InitConfig{
		CorkscrewDir:  filepath.Join(usr.HomeDir, ".corkscrew"),
		ProtocVersion: "25.3",
		DuckDBVersion: "1.5.2", // Updated to v1.5.2 with enhanced duckpgq support
	}
	config.BinDir = filepath.Join(config.CorkscrewDir, "bin")
	config.PluginDir = filepath.Join(config.CorkscrewDir, "plugins")
	config.ConfigDir = filepath.Join(config.CorkscrewDir, "config")
	config.DepsDir = filepath.Join(config.CorkscrewDir, "deps")

	// Step 1: Create directory structure
	fmt.Println("📁 Creating directory structure...")
	if err := createDirectories(config); err != nil {
		fmt.Printf("❌ Failed to create directories: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  ✓ Created ~/.corkscrew directories")
	fmt.Println()

	// Step 2: Download dependencies
	if upgrade {
		fmt.Println("📦 Upgrading dependencies...")
	} else {
		fmt.Println("📦 Downloading dependencies...")
	}
	if dryRun {
		if upgrade {
			fmt.Println("  ✓ DRY RUN: Would force upgrade protoc v25.3 and duckdb v1.5.2")
		} else {
			fmt.Println("  ✓ DRY RUN: Would download protoc v25.3 and duckdb v1.5.2")
		}
	} else {
		if err := downloadDependencies(ctx, config, upgrade); err != nil {
			fmt.Printf("❌ Failed to download dependencies: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println()

	// Step 3: Read configuration
	fmt.Println("🔍 Reading configuration from ./corkscrew.yaml...")
	corkscrewConfig, err := readConfiguration()
	if err != nil {
		fmt.Printf("❌ Failed to read configuration: %v\n", err)
		os.Exit(1)
	}

	// Generation and builds run `go run` and `make` from a source tree, so they only run from a
	// directory that is verifiably the corkscrew repository, never from an arbitrary checkout.
	sourceRoot, rootErr := findPluginSourceRoot(".")
	if rootErr != nil {
		fmt.Printf("⚠️  Skipping code generation and plugin builds: %v\n\n", rootErr)
	} else {
		fmt.Printf("📂 Plugin source directory: %s\n\n", sourceRoot)
	}

	// Step 4: Generate code for enabled providers
	fmt.Println("⚙️  Generating scanner code for enabled providers...")
	if rootErr != nil {
		fmt.Println("  ⚠️  skipped: no corkscrew source directory")
	} else if dryRun {
		for provider, cfg := range corkscrewConfig.Providers {
			if cfg.Enabled {
				fmt.Printf("  ✓ DRY RUN: Would generate code for %s-provider (%d services)\n", provider, len(cfg.Services))
			}
		}
	} else {
		if err := generateProviderCode(ctx, sourceRoot, corkscrewConfig); err != nil {
			fmt.Printf("❌ Failed to generate provider code: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println()

	// Step 5: Build enabled plugins
	fmt.Println("🔨 Building enabled plugins...")
	if rootErr != nil {
		fmt.Println("  ⚠️  skipped: no corkscrew source directory")
	} else if dryRun {
		for provider, cfg := range corkscrewConfig.Providers {
			if cfg.Enabled {
				fmt.Printf("  ✓ DRY RUN: Would build %s-provider\n", provider)
			}
		}
	} else {
		if err := buildPluginsFromConfig(ctx, sourceRoot, corkscrewConfig); err != nil {
			fmt.Printf("❌ Failed to build plugins: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println()

	// Step 6: Success message
	fmt.Println("🎉 Corkscrew initialized successfully!")
	fmt.Println()

	// Step 7: PATH check / offer to install
	ensureBinDirOnPath(config.BinDir, dryRun)

	fmt.Printf("Run: %s/corkscrew scan --provider aws --services s3\n", config.BinDir)
}

// ensureBinDirOnPath checks whether binDir is on the user's $PATH and, if not,
// offers to append an export line to the appropriate shell rc file.
func ensureBinDirOnPath(binDir string, dryRun bool) {
	if isOnPath(binDir) {
		fmt.Printf("✅ %s is already on your PATH\n\n", binDir)
		return
	}

	rcFile, shellName := detectShellRC()
	exportLine := fmt.Sprintf("export PATH=\"%s:$PATH\"", binDir)
	if shellName == "fish" {
		exportLine = fmt.Sprintf("set -gx PATH %s $PATH", binDir)
	}

	fmt.Printf("⚠️  %s is not on your PATH.\n", binDir)

	if dryRun {
		if rcFile != "" {
			fmt.Printf("   DRY RUN: would offer to append to %s:\n     %s\n\n", rcFile, exportLine)
		} else {
			fmt.Printf("   DRY RUN: add manually: %s\n\n", exportLine)
		}
		return
	}

	if rcFile == "" || !isStdinTTY() {
		fmt.Printf("   Add this line to your shell rc file:\n     %s\n\n", exportLine)
		return
	}

	fmt.Printf("   Append to %s? [y/N]: ", rcFile)
	var resp string
	fmt.Scanln(&resp)
	resp = strings.ToLower(strings.TrimSpace(resp))
	if resp != "y" && resp != "yes" {
		fmt.Printf("   Skipped. Add manually when ready:\n     %s\n\n", exportLine)
		return
	}

	if err := appendToRC(rcFile, exportLine); err != nil {
		fmt.Printf("   ❌ Failed to update %s: %v\n", rcFile, err)
		fmt.Printf("   Add manually: %s\n\n", exportLine)
		return
	}
	fmt.Printf("   ✅ Added to %s. Run `source %s` or open a new shell.\n\n", rcFile, rcFile)
}

func isOnPath(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "" {
			continue
		}
		pAbs, err := filepath.Abs(p)
		if err != nil {
			pAbs = p
		}
		if pAbs == abs {
			return true
		}
	}
	return false
}

func detectShellRC() (rcFile, shellName string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	shell := os.Getenv("SHELL")
	switch {
	case strings.Contains(shell, "zsh"):
		return filepath.Join(home, ".zshrc"), "zsh"
	case strings.Contains(shell, "fish"):
		return filepath.Join(home, ".config", "fish", "config.fish"), "fish"
	case strings.Contains(shell, "bash"):
		// Prefer .bashrc on Linux, .bash_profile on macOS if it exists
		bashrc := filepath.Join(home, ".bashrc")
		if runtime.GOOS == "darwin" {
			bp := filepath.Join(home, ".bash_profile")
			if _, err := os.Stat(bp); err == nil {
				return bp, "bash"
			}
		}
		return bashrc, "bash"
	}
	// Fallback
	profile := filepath.Join(home, ".profile")
	return profile, "sh"
}

func isStdinTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func appendToRC(rcFile, exportLine string) error {
	// Avoid duplicate entries
	if existing, err := os.ReadFile(rcFile); err == nil {
		if strings.Contains(string(existing), exportLine) {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(rcFile), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	block := fmt.Sprintf("\n# Added by corkscrew init\n%s\n", exportLine)
	_, err = f.WriteString(block)
	return err
}

func createDirectories(config *InitConfig) error {
	dirs := []string{
		config.CorkscrewDir,
		config.BinDir,
		config.PluginDir,
		config.ConfigDir,
		config.DepsDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

func downloadDependencies(ctx context.Context, config *InitConfig, upgrade bool) error {
	// Detect platform
	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

	// Define dependencies
	deps := []DependencyInfo{
		{
			Name:        "protoc",
			Version:     config.ProtocVersion,
			Platform:    platform,
			URL:         getProtocURL(config.ProtocVersion, platform),
			ArchiveName: getProtocArchiveName(config.ProtocVersion, platform),
			BinaryName:  "protoc",
		},
		{
			Name:        "duckdb",
			Version:     config.DuckDBVersion,
			Platform:    platform,
			URL:         getDuckDBURL(config.DuckDBVersion, platform),
			ArchiveName: getDuckDBArchiveName(config.DuckDBVersion, platform),
			BinaryName:  "duckdb",
		},
	}

	for _, dep := range deps {
		// Check if already exists
		binPath := filepath.Join(config.BinDir, dep.BinaryName)
		if _, err := os.Stat(binPath); err == nil && !upgrade {
			fmt.Printf("  ✓ %s v%s already installed\n", dep.Name, dep.Version)
			continue
		}

		// Download and install (or upgrade)
		if upgrade && dep.Name != "" {
			fmt.Printf("  🔄 Upgrading %s v%s...\n", dep.Name, dep.Version)
		}
		if err := downloadAndInstallDependency(ctx, config, dep); err != nil {
			return fmt.Errorf("failed to install %s: %w", dep.Name, err)
		}
	}

	return nil
}

// dependencyDownloadTimeout bounds one archive download; the largest (DuckDB osx-universal) is
// about 33 MiB, so five minutes only trips on a stalled connection.
const dependencyDownloadTimeout = 5 * time.Minute

func downloadAndInstallDependency(ctx context.Context, config *InitConfig,
	dep DependencyInfo) error {
	// Resolve the pin before touching the network so an unpinned tuple fails without a download.
	wantSHA256, err := lookupDependencyPin(dep.Name, dep.Version, dep.Platform)
	if err != nil {
		return err
	}

	fmt.Printf("  ↓ %s v%s (%s)...", dep.Name, dep.Version, dep.Platform)

	tempFile := filepath.Join(config.DepsDir, dep.ArchiveName)
	// Remove the archive on every path so a rejected download does not linger in deps/. The
	// error is ignored because the file may never have been created.
	defer func() { _ = os.Remove(tempFile) }()

	n, err := downloadToFile(ctx, dep, tempFile)
	if err != nil {
		return err
	}
	if err := verifyFileSHA256(tempFile, wantSHA256); err != nil {
		return err
	}
	if err := extractDependency(config, dep, tempFile); err != nil {
		return fmt.Errorf("failed to extract: %w", err)
	}

	fmt.Printf(" ✓ (%.1f MB)\n", float64(n)/(1024*1024))
	return nil
}

// downloadToFile streams dep.URL into path and returns the bytes written. The request carries
// ctx so Ctrl-C aborts it. The response's own Content-Length replaces the former HEAD request.
func downloadToFile(ctx context.Context, dep DependencyInfo, path string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dep.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build request: %w", err)
	}
	client := &http.Client{Timeout: dependencyDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Chunked or compressed responses report -1; record 0 (unknown) rather than a negative
	// total that progress math would divide by.
	total := resp.ContentLength
	if total <= 0 {
		total = 0
	}
	tracker := &ProgressTracker{Name: dep.Name, Total: total, StartTime: time.Now()}

	out, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("failed to create temp file: %w", err)
	}
	body := io.LimitReader(&progressReader{resp.Body, tracker}, dependencyDownloadBytesMax+1)
	n, copyErr := io.Copy(out, body)
	if err := errors.Join(copyErr, out.Close()); err != nil {
		return 0, fmt.Errorf("failed to download file: %w", err)
	}
	if n > dependencyDownloadBytesMax {
		return 0, fmt.Errorf("download exceeds %d bytes", dependencyDownloadBytesMax)
	}
	return n, nil
}

// protoc and duckdb archives are tens of MiB; these limits leave headroom for growth while
// stopping a hostile mirror from filling the disk.
const (
	dependencyDownloadBytesMax = 512 << 20
	dependencyBytesMax         = 512 << 20
	dependencyEntryCountMax    = 10_000
	dependencyEntryBytesMax    = 512 << 20
)

func extractDependency(config *InitConfig, dep DependencyInfo, archivePath string) error {
	var format archive.Format
	switch {
	case strings.HasSuffix(dep.ArchiveName, ".zip"):
		format = archive.FormatZip
	case strings.HasSuffix(dep.ArchiveName, ".tar.gz"):
		format = archive.FormatTarGz
	default:
		return fmt.Errorf("unsupported archive format: %s", dep.ArchiveName)
	}

	binDir, err := filepath.Abs(config.BinDir)
	if err != nil {
		return err
	}
	destPath := filepath.Join(binDir, dep.BinaryName)
	if runtime.GOOS == "windows" {
		destPath += ".exe"
	}
	limits := archive.Limits{
		BytesMax:      dependencyBytesMax,
		EntryCountMax: dependencyEntryCountMax,
		EntryBytesMax: dependencyEntryBytesMax,
	}
	return archive.ExtractFile(format, archivePath, dep.BinaryName, destPath, limits)
}

// The URL builders derive the URL from the archive name so the asset that is downloaded is
// always the one whose name, and therefore pin, was looked up.
func getProtocURL(version, platform string) string {
	return fmt.Sprintf("https://github.com/protocolbuffers/protobuf/releases/download/v%s/%s",
		version, getProtocArchiveName(version, platform))
}

func getProtocArchiveName(version, platform string) string {
	osArch := platform
	switch platform {
	case "linux-amd64":
		osArch = "linux-x86_64"
	case "linux-arm64":
		osArch = "linux-aarch_64"
	case "darwin-amd64":
		osArch = "osx-x86_64"
	case "darwin-arm64":
		osArch = "osx-aarch_64"
	case "windows-amd64":
		osArch = "win64"
	case "windows-386":
		osArch = "win32"
	}
	return fmt.Sprintf("protoc-%s-%s.zip", version, osArch)
}

func getDuckDBURL(version, platform string) string {
	return fmt.Sprintf("https://github.com/duckdb/duckdb/releases/download/v%s/%s",
		version, getDuckDBArchiveName(version, platform))
}

// DuckDB names its CLI assets with Go-style suffixes (linux-arm64, windows-amd64) except on
// macOS, where one universal archive serves both architectures.
func getDuckDBArchiveName(version, platform string) string {
	osArch := platform
	switch platform {
	case "darwin-amd64", "darwin-arm64":
		osArch = "osx-universal"
	}
	return fmt.Sprintf("duckdb_cli-%s.zip", osArch)
}

func readConfiguration() (*CorkscrewConfig, error) {
	configFile := "./corkscrew.yaml"

	// Check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("  ⚠️  Configuration file not found, using defaults")
		return getDefaultConfig(), nil
	}

	config, err := appconfig.LoadCorkscrewConfig(configFile)
	if err != nil {
		return nil, err
	}

	fmt.Println("  ✓ Configuration file found and parsed")

	// Display provider status
	for provider, cfg := range config.Providers {
		if cfg.Enabled {
			fmt.Printf("  ✓ %s provider: enabled (%d services)\n", strings.ToUpper(provider), len(cfg.Services))
		} else {
			fmt.Printf("  ✗ %s provider: disabled\n", strings.ToUpper(provider))
		}
	}
	fmt.Println()

	return config, nil
}

func getDefaultConfig() *CorkscrewConfig {
	return appconfig.DefaultCorkscrewConfig()
}

// corkscrewModulePath identifies the repository whose Makefile and generators init may run.
const corkscrewModulePath = "github.com/jlgore/corkscrew"

// Subprocess deadlines. A cold `make build-<p>-plugin` compiles a large SDK dependency tree, and
// the Azure analyzer may download the SDK first; both are far below these on a working machine,
// so hitting one means a hang, not a slow build.
const (
	pluginBuildTimeout = 20 * time.Minute
	generatorTimeout   = 10 * time.Minute
	// subprocessWaitDelay stops CombinedOutput blocking after a kill: `go run` leaves a child
	// that still holds the output pipe once the go command itself is gone.
	subprocessWaitDelay = 5 * time.Second
)

// findPluginSourceRoot returns dir as an absolute path if it is the corkscrew repository: a
// go.mod declaring corkscrewModulePath next to a plugins/ directory. It does not search
// parents. This guards against running some unrelated checkout's Makefile by accident; a
// hostile fork can still claim the module path, which is why the directory and each command
// are printed before they run.
func findPluginSourceRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	gomod, err := os.ReadFile(filepath.Join(abs, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("%s is not the corkscrew repository (no go.mod): run init from "+
			"the repository root to build plugins", abs)
	}
	if module := goModulePath(gomod); module != corkscrewModulePath {
		return "", fmt.Errorf("%s is not the corkscrew repository (module %q, want %q)",
			abs, module, corkscrewModulePath)
	}
	info, err := os.Stat(filepath.Join(abs, "plugins"))
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s has no plugins/ directory", abs)
	}
	return abs, nil
}

// goModulePath returns the path from the module directive of a go.mod file, or "" if absent.
// A full modfile parser would be a new dependency for one line.
func goModulePath(gomod []byte) string {
	for _, line := range strings.Split(string(gomod), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

// runBounded runs one subprocess in dir with a deadline, printing the directory and command
// first so the user sees exactly what init executes on their behalf.
func runBounded(ctx context.Context, timeout time.Duration, dir, name string,
	args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Printf("\n    $ (cd %s && %s %s)", dir, name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = subprocessWaitDelay

	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output, fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return output, err
}

func generateProviderCode(ctx context.Context, sourceRoot string, config *CorkscrewConfig) error {
	for provider, cfg := range config.Providers {
		if !cfg.Enabled {
			continue
		}

		fmt.Printf("  ⚙️  Generating %s-provider code...", provider)

		// Check if plugin source exists
		pluginDir := filepath.Join(sourceRoot, "plugins", provider+"-provider")
		if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
			fmt.Printf(" ⚠️  source not found, skipping\n")
			continue
		}

		// Generate scanners for configured services
		if err := generateScannersForProvider(ctx, provider, cfg.Services, pluginDir); err != nil {
			fmt.Printf(" ❌ failed: %v\n", err)
			continue
		}

		// Generate analysis files for dynamic discovery
		if err := generateAnalysisFilesForProvider(provider, cfg.Services, pluginDir); err != nil {
			fmt.Printf(" ⚠️  analysis generation failed: %v\n", err)
			// Don't fail the whole process, just warn
		}

		fmt.Printf(" ✓ (%d services)\n", len(cfg.Services))
	}

	return nil
}

func generateScannersForProvider(ctx context.Context, provider string, services []string,
	pluginDir string) error {
	switch provider {
	case "aws":
		return generateAWSScannersForServices(ctx, services, pluginDir)
	case "azure":
		return generateAzureScannersForServices(ctx, services, pluginDir)
	case "gcp":
		return generateGCPScannersForServices(ctx, services, pluginDir)
	case "kubernetes":
		return generateKubernetesScannersForServices(ctx, services, pluginDir)
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}
}

func generateAWSScannersForServices(ctx context.Context, services []string,
	pluginDir string) error {
	// AWS uses analyzer + registry generator pattern
	// First, run the analyzer to discover services
	analyzerMainGo := filepath.Join("cmd", "analyzer", "main.go")
	analyzerFullPath := filepath.Join(pluginDir, analyzerMainGo)
	if _, err := os.Stat(analyzerFullPath); os.IsNotExist(err) {
		return fmt.Errorf("AWS analyzer not found at %s", analyzerFullPath)
	}

	// Create generated directory if it doesn't exist
	generatedDir := filepath.Join(pluginDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("failed to create generated directory: %w", err)
	}

	// Run analyzer to generate services.json
	output, err := runBounded(ctx, generatorTimeout, pluginDir, "go", "run", analyzerMainGo,
		"-output", "generated/services.json",
		"-services", strings.Join(services, ","))
	if err != nil {
		return fmt.Errorf("AWS analyzer failed: %w, output: %s", err, output)
	}

	return nil
}

func generateAzureScannersForServices(ctx context.Context, services []string,
	pluginDir string) error {
	// Azure uses analyzer + scanner generator pattern (like AWS)
	analyzerMainGo := filepath.Join("cmd", "analyze-azure-sdk", "main.go")
	analyzerFullPath := filepath.Join(pluginDir, analyzerMainGo)
	if _, err := os.Stat(analyzerFullPath); os.IsNotExist(err) {
		return fmt.Errorf("Azure analyzer not found at %s", analyzerFullPath)
	}

	// Create generated directory if it doesn't exist
	generatedDir := filepath.Join(pluginDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("failed to create generated directory: %w", err)
	}

	// First, run analyzer to generate service catalog
	catalogPath := "generated/azure-service-catalog.json"
	output, err := runBounded(ctx, generatorTimeout, pluginDir, "go", "run", analyzerMainGo,
		"-output", catalogPath,
		"-services", strings.Join(services, ","),
		"-update") // Auto-download Azure SDK if needed
	if err != nil {
		return fmt.Errorf("Azure analyzer failed: %w, output: %s", err, output)
	}

	// Then run scanner generator with the catalog
	generatorMainGo := filepath.Join("cmd", "scanner-generator", "main.go")
	generatorFullPath := filepath.Join(pluginDir, generatorMainGo)
	if _, err := os.Stat(generatorFullPath); os.IsNotExist(err) {
		return fmt.Errorf("Azure scanner generator not found at %s", generatorFullPath)
	}

	output, err = runBounded(ctx, generatorTimeout, pluginDir, "go", "run", generatorMainGo,
		"-catalog", catalogPath,
		"-services", strings.Join(services, ","),
		"-output", "./generated")
	if err != nil {
		return fmt.Errorf("Azure scanner generator failed: %w, output: %s", err, output)
	}

	return nil
}

func generateGCPScannersForServices(ctx context.Context, services []string,
	pluginDir string) error {
	// GCP uses gcp-scanner-generator
	generatorMainGo := filepath.Join("cmd", "gcp-scanner-generator", "main.go")
	generatorFullPath := filepath.Join(pluginDir, generatorMainGo)
	if _, err := os.Stat(generatorFullPath); os.IsNotExist(err) {
		return fmt.Errorf("GCP scanner generator not found at %s", generatorFullPath)
	}

	// Create generated directory if it doesn't exist
	generatedDir := filepath.Join(pluginDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("failed to create generated directory: %w", err)
	}

	// GCP generator - check if it expects comma-separated services
	output, err := runBounded(ctx, generatorTimeout, pluginDir, "go", "run", generatorMainGo,
		"-services", strings.Join(services, ","),
		"-output", "./generated")
	if err != nil {
		return fmt.Errorf("GCP generator failed: %w, output: %s", err, output)
	}

	return nil
}

func generateKubernetesScannersForServices(ctx context.Context, services []string,
	pluginDir string) error {
	// Kubernetes might not have explicit service generation
	// Check if there's a generator available
	generatorMainGo := filepath.Join("cmd", "scanner-generator", "main.go")
	generatorFullPath := filepath.Join(pluginDir, generatorMainGo)
	if _, err := os.Stat(generatorFullPath); os.IsNotExist(err) {
		// No generator found, assume static implementation
		return nil
	}

	// Create generated directory if it doesn't exist
	generatedDir := filepath.Join(pluginDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("failed to create generated directory: %w", err)
	}

	for _, service := range services {
		output, err := runBounded(ctx, generatorTimeout, pluginDir, "go", "run", generatorMainGo,
			"-service", service,
			"-output", "./generated")
		if err != nil {
			return fmt.Errorf("Kubernetes generator failed for service %s: %w, output: %s",
				service, err, output)
		}
	}

	return nil
}

// generateAnalysisFilesForProvider generates analysis files for enhanced discovery
func generateAnalysisFilesForProvider(provider string, services []string, pluginDir string) error {
	switch provider {
	case "aws":
		// AWS now uses integrated runtime analysis generation in UnifiedScanner
		// Skip static analysis file generation
		return nil
	case "azure":
		// Azure might use a different approach
		return nil
	case "gcp":
		// GCP might use a different approach
		return nil
	default:
		return nil // No analysis generation for this provider
	}
}

func buildPluginsFromConfig(ctx context.Context, sourceRoot string,
	corkscrewConfig *CorkscrewConfig) error {
	for provider, cfg := range corkscrewConfig.Providers {
		if !cfg.Enabled {
			continue
		}

		fmt.Printf("  🔨 Building %s-provider...", provider)

		// Check if plugin source exists
		pluginDir := filepath.Join(sourceRoot, "plugins", provider+"-provider")
		if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
			fmt.Printf(" ⚠️  source not found, skipping\n")
			continue
		}

		// Build the plugin
		if err := buildPlugin(ctx, sourceRoot, provider); err != nil {
			fmt.Printf(" ❌ failed: %v\n", err)
			continue
		}

		fmt.Printf(" ✓\n")
	}

	return nil
}

func buildPlugin(ctx context.Context, sourceRoot, provider string) error {
	// The Makefile at the repository root owns the per-provider build recipe.
	target := fmt.Sprintf("build-%s-plugin", provider)
	output, err := runBounded(ctx, pluginBuildTimeout, sourceRoot, "make", target)
	if err != nil {
		return fmt.Errorf("failed to build %s plugin: %v\nOutput: %s", provider, err, string(output))
	}

	return nil
}

// progressReader wraps an io.Reader to track progress
type progressReader struct {
	io.Reader
	tracker *ProgressTracker
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.tracker.Current += int64(n)
	return n, err
}

func printInitUsage() {
	fmt.Println("🚀 Corkscrew Init - Initialize Corkscrew with dependencies and plugins")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  corkscrew init [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --dry-run    Show what would be done without making changes")
	fmt.Println("  --upgrade    Force upgrade dependencies even if they exist")
	fmt.Println("  --help, -h   Show this help message")
	fmt.Println()
	fmt.Println("What it does:")
	fmt.Println("  1. Creates ~/.corkscrew directory structure")
	fmt.Println("  2. Downloads protoc v25.3 and duckdb v1.5.2")
	fmt.Println("  3. Reads configuration from ./corkscrew.yaml")
	fmt.Println("  4. Generates scanner code for enabled providers")
	fmt.Println("  5. Generates analysis files for enhanced discovery")
	fmt.Println("  6. Builds enabled provider plugins")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  corkscrew init                 # Standard initialization")
	fmt.Println("  corkscrew init --dry-run      # See what would happen")
	fmt.Println("  corkscrew init --upgrade      # Force upgrade all dependencies")
	fmt.Println("  corkscrew init --help         # Show this help")
}

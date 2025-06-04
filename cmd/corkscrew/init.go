package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
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

	"gopkg.in/yaml.v3"
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

// CorkscrewConfig represents the complete configuration from corkscrew.yaml
type CorkscrewConfig struct {
	Version   string                       `yaml:"version"`
	Providers map[string]ProviderConfig    `yaml:"providers"`
	Dependencies DependenciesConfig        `yaml:"dependencies"`
	Database  DatabaseConfig               `yaml:"database"`
	Query     QueryConfig                  `yaml:"query"`
	Compliance ComplianceConfig            `yaml:"compliance"`
	Logging   LoggingConfig                `yaml:"logging"`
	Output    OutputConfig                 `yaml:"output"`
}

// ProviderConfig represents configuration for a cloud provider
type ProviderConfig struct {
	Enabled       bool     `yaml:"enabled"`
	DefaultRegion string   `yaml:"default_region"`
	Services      []string `yaml:"services"`
}

// DependenciesConfig represents dependency configuration
type DependenciesConfig struct {
	Protoc DependencyConfig `yaml:"protoc"`
	DuckDB DependencyConfig `yaml:"duckdb"`
}

// DependencyConfig represents individual dependency configuration
type DependencyConfig struct {
	Version      string `yaml:"version"`
	AutoDownload bool   `yaml:"auto_download"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Path       string `yaml:"path"`
	AutoCreate bool   `yaml:"auto_create"`
}

// QueryConfig represents query engine configuration
type QueryConfig struct {
	Timeout            string `yaml:"timeout"`
	StreamingThreshold int    `yaml:"streaming_threshold"`
	MaxMemory          string `yaml:"max_memory"`
}

// ComplianceConfig represents compliance settings
type ComplianceConfig struct {
	PacksDir   string `yaml:"packs_dir"`
	AutoUpdate bool   `yaml:"auto_update"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// OutputConfig represents output configuration
type OutputConfig struct {
	DefaultFormat string `yaml:"default_format"`
	Colors        bool   `yaml:"colors"`
	ProgressBars  bool   `yaml:"progress_bars"`
}

// DependencyInfo represents a dependency to download
type DependencyInfo struct {
	Name        string
	Version     string
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

func runInit(args []string) {
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
		DuckDBVersion: "1.2.2", // Locked to tested version with duckpgq support
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
			fmt.Println("  ✓ DRY RUN: Would force upgrade protoc v25.3 and duckdb v1.2.2")
		} else {
			fmt.Println("  ✓ DRY RUN: Would download protoc v25.3 and duckdb v1.2.2")
		}
	} else {
		if err := downloadDependencies(config, upgrade); err != nil {
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
	
	// Step 4: Generate code for enabled providers
	fmt.Println("⚙️  Generating scanner code for enabled providers...")
	if dryRun {
		for provider, cfg := range corkscrewConfig.Providers {
			if cfg.Enabled {
				fmt.Printf("  ✓ DRY RUN: Would generate code for %s-provider (%d services)\n", provider, len(cfg.Services))
			}
		}
	} else {
		if err := generateProviderCode(corkscrewConfig); err != nil {
			fmt.Printf("❌ Failed to generate provider code: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println()
	
	// Step 5: Build enabled plugins
	fmt.Println("🔨 Building enabled plugins...")
	if dryRun {
		for provider, cfg := range corkscrewConfig.Providers {
			if cfg.Enabled {
				fmt.Printf("  ✓ DRY RUN: Would build %s-provider\n", provider)
			}
		}
	} else {
		if err := buildPluginsFromConfig(config, corkscrewConfig); err != nil {
			fmt.Printf("❌ Failed to build plugins: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println()

	// Step 6: Success message
	fmt.Println("🎉 Corkscrew initialized successfully!")
	fmt.Println()
	fmt.Printf("Add to your PATH: export PATH=\"%s:$PATH\"\n", config.BinDir)
	fmt.Printf("Or run directly: %s/corkscrew scan --provider aws --services s3\n", config.BinDir)
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

func downloadDependencies(config *InitConfig, upgrade bool) error {
	ctx := context.Background()
	
	// Detect platform
	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	
	// Define dependencies
	deps := []DependencyInfo{
		{
			Name:        "protoc",
			Version:     config.ProtocVersion,
			URL:         getProtocURL(config.ProtocVersion, platform),
			ArchiveName: getProtocArchiveName(config.ProtocVersion, platform),
			BinaryName:  "protoc",
		},
		{
			Name:        "duckdb",
			Version:     config.DuckDBVersion,
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

func downloadAndInstallDependency(ctx context.Context, config *InitConfig, dep DependencyInfo) error {
	// Create HTTP client with timeout
	client := &http.Client{Timeout: 5 * time.Minute}
	
	// Get file size first
	resp, err := client.Head(dep.URL)
	if err != nil {
		return fmt.Errorf("failed to get dependency info: %w", err)
	}
	resp.Body.Close()
	
	size := resp.ContentLength
	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	
	// Start download
	fmt.Printf("  ↓ %s v%s (%s)...", dep.Name, dep.Version, platform)
	
	resp, err = client.Get(dep.URL)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Download to temp file
	tempFile := filepath.Join(config.DepsDir, dep.ArchiveName)
	out, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	// Copy with progress
	tracker := &ProgressTracker{
		Name:      dep.Name,
		Total:     size,
		StartTime: time.Now(),
	}
	
	_, err = io.Copy(out, &progressReader{resp.Body, tracker})
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}

	// Extract and install
	if err := extractDependency(config, dep, tempFile); err != nil {
		return fmt.Errorf("failed to extract: %w", err)
	}

	// Cleanup temp file
	os.Remove(tempFile)
	
	// Calculate download size in MB
	sizeMB := float64(size) / (1024 * 1024)
	fmt.Printf(" ✓ (%.1f MB)\n", sizeMB)
	
	return nil
}

func extractDependency(config *InitConfig, dep DependencyInfo, archivePath string) error {
	if strings.HasSuffix(dep.ArchiveName, ".zip") {
		return extractZip(archivePath, config.BinDir, dep.BinaryName)
	} else if strings.HasSuffix(dep.ArchiveName, ".tar.gz") {
		return extractTarGz(archivePath, config.BinDir, dep.BinaryName)
	}
	return fmt.Errorf("unsupported archive format: %s", dep.ArchiveName)
}

func extractZip(archivePath, destDir, binaryName string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Look for the binary file (might be in subdirectory)
		if strings.HasSuffix(f.Name, binaryName) || strings.HasSuffix(f.Name, binaryName+".exe") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			destPath := filepath.Join(destDir, binaryName)
			if runtime.GOOS == "windows" {
				destPath += ".exe"
			}

			out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, rc)
			return err
		}
	}
	return fmt.Errorf("binary %s not found in archive", binaryName)
}

func extractTarGz(archivePath, destDir, binaryName string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Look for the binary file
		if header.Typeflag == tar.TypeReg && 
		   (strings.HasSuffix(header.Name, binaryName) || strings.HasSuffix(header.Name, binaryName+".exe")) {
			
			destPath := filepath.Join(destDir, binaryName)
			if runtime.GOOS == "windows" {
				destPath += ".exe"
			}

			out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, tr)
			return err
		}
	}
	return fmt.Errorf("binary %s not found in archive", binaryName)
}

// Platform-specific URL generators
func getProtocURL(version, platform string) string {
	archiveExt := "zip"
	osArch := platform
	
	// Map Go platform names to protoc naming
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

	return fmt.Sprintf("https://github.com/protocolbuffers/protobuf/releases/download/v%s/protoc-%s-%s.%s",
		version, version, osArch, archiveExt)
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
	ext := "zip"
	if strings.HasPrefix(platform, "linux") {
		ext = "zip"  // DuckDB provides zip for all platforms
	}
	
	osArch := platform
	switch platform {
	case "linux-amd64":
		osArch = "linux-amd64"
	case "linux-arm64":
		osArch = "linux-aarch64"
	case "darwin-amd64":
		osArch = "osx-universal"
	case "darwin-arm64":
		osArch = "osx-universal"
	case "windows-amd64":
		osArch = "win-amd64"
	}

	return fmt.Sprintf("https://github.com/duckdb/duckdb/releases/download/v%s/duckdb_cli-%s.%s",
		version, osArch, ext)
}

func getDuckDBArchiveName(version, platform string) string {
	osArch := platform
	switch platform {
	case "linux-amd64":
		osArch = "linux-amd64"
	case "linux-arm64":
		osArch = "linux-aarch64"
	case "darwin-amd64", "darwin-arm64":
		osArch = "osx-universal"
	case "windows-amd64":
		osArch = "win-amd64"
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

	// Read and parse YAML configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	var config CorkscrewConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse configuration file: %w", err)
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

	return &config, nil
}

func getDefaultConfig() *CorkscrewConfig {
	return &CorkscrewConfig{
		Version: "2.0",
		Providers: map[string]ProviderConfig{
			"aws": {
				Enabled:       true,
				DefaultRegion: "us-east-1",
				Services:      []string{"s3", "ec2", "lambda", "iam", "rds", "dynamodb"},
			},
			"azure": {
				Enabled:       true,
				DefaultRegion: "eastus",
				Services:      []string{"storage", "compute", "keyvault", "sql"},
			},
			"gcp": {
				Enabled:       false,
				DefaultRegion: "us-central1-a",
				Services:      []string{"storage", "compute", "bigquery"},
			},
			"kubernetes": {
				Enabled: false,
			},
		},
		Dependencies: DependenciesConfig{
			Protoc: DependencyConfig{Version: "25.3", AutoDownload: true},
			DuckDB: DependencyConfig{Version: "1.2.2", AutoDownload: true},
		},
	}
}

func generateProviderCode(config *CorkscrewConfig) error {
	for provider, cfg := range config.Providers {
		if !cfg.Enabled {
			continue
		}

		fmt.Printf("  ⚙️  Generating %s-provider code...", provider)
		
		// Check if plugin source exists
		pluginDir := fmt.Sprintf("./plugins/%s-provider", provider)
		if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
			fmt.Printf(" ⚠️  source not found, skipping\n")
			continue
		}

		// Generate scanners for configured services
		if err := generateScannersForProvider(provider, cfg.Services, pluginDir); err != nil {
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

func generateScannersForProvider(provider string, services []string, pluginDir string) error {
	switch provider {
	case "aws":
		return generateAWSScannersForServices(services, pluginDir)
	case "azure":
		return generateAzureScannersForServices(services, pluginDir)
	case "gcp":
		return generateGCPScannersForServices(services, pluginDir)
	case "kubernetes":
		return generateKubernetesScannersForServices(services, pluginDir)
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}
}

func generateAWSScannersForServices(services []string, pluginDir string) error {
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
	cmd := exec.Command("go", "run", analyzerMainGo, 
		"-output", "generated/services.json",
		"-services", strings.Join(services, ","))
	cmd.Dir = pluginDir
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("AWS analyzer failed: %w, output: %s", err, output)
	}
	
	// Run registry generator to create scanner registry
	registryGeneratorMainGo := filepath.Join("cmd", "registry-generator", "main.go")
	registryGeneratorFullPath := filepath.Join(pluginDir, registryGeneratorMainGo)
	if _, err := os.Stat(registryGeneratorFullPath); os.IsNotExist(err) {
		return fmt.Errorf("AWS registry generator not found at %s", registryGeneratorFullPath)
	}
	
	cmd = exec.Command("go", "run", registryGeneratorMainGo,
		"-services", "generated/services.json",
		"-output", "generated/scanner_registry.go",
		"-package", "generated")
	cmd.Dir = pluginDir
	
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("AWS registry generator failed: %w, output: %s", err, output)
	}
	
	return nil
}

func generateAzureScannersForServices(services []string, pluginDir string) error {
	// Azure uses scanner generator
	generatorMainGo := filepath.Join("cmd", "scanner-generator", "main.go")
	generatorFullPath := filepath.Join(pluginDir, generatorMainGo)
	if _, err := os.Stat(generatorFullPath); os.IsNotExist(err) {
		return fmt.Errorf("Azure scanner generator not found at %s", generatorFullPath)
	}
	
	// Create generated directory if it doesn't exist
	generatedDir := filepath.Join(pluginDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("failed to create generated directory: %w", err)
	}
	
	for _, service := range services {
		cmd := exec.Command("go", "run", generatorMainGo, 
			"-service", service,
			"-output", "./generated")
		cmd.Dir = pluginDir
		
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Azure generator failed for service %s: %w, output: %s", service, err, output)
		}
	}
	
	return nil
}

func generateGCPScannersForServices(services []string, pluginDir string) error {
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
	
	for _, service := range services {
		cmd := exec.Command("go", "run", generatorMainGo,
			"-service", service,
			"-output", "./generated")
		cmd.Dir = pluginDir
		
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("GCP generator failed for service %s: %w, output: %s", service, err, output)
		}
	}
	
	return nil
}

func generateKubernetesScannersForServices(services []string, pluginDir string) error {
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
		cmd := exec.Command("go", "run", generatorMainGo,
			"-service", service,
			"-output", "./generated")
		cmd.Dir = pluginDir
		
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Kubernetes generator failed for service %s: %w, output: %s", service, err, output)
		}
	}
	
	return nil
}

// generateAnalysisFilesForProvider generates analysis files for enhanced discovery
func generateAnalysisFilesForProvider(provider string, services []string, pluginDir string) error {
	switch provider {
	case "aws":
		return generateAWSAnalysisFiles(services, pluginDir)
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

func generateAWSAnalysisFiles(services []string, pluginDir string) error {
	// Check if analysis generator exists
	analyzerPath := filepath.Join(pluginDir, "cmd", "generate-analysis", "main.go")
	if _, err := os.Stat(analyzerPath); os.IsNotExist(err) {
		return fmt.Errorf("analysis generator not found at %s", analyzerPath)
	}
	
	// Create generated directory
	generatedDir := filepath.Join(pluginDir, "generated")
	if err := os.MkdirAll(generatedDir, 0755); err != nil {
		return fmt.Errorf("failed to create generated directory: %w", err)
	}
	
	// Run the analysis generator
	cmd := exec.Command("go", "run", analyzerPath, generatedDir)
	cmd.Dir = pluginDir
	cmd.Env = append(os.Environ(), 
		fmt.Sprintf("AWS_SERVICES=%s", strings.Join(services, ",")),
	)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("analysis generation failed: %w\nOutput: %s", err, output)
	}
	
	// Copy analysis files to runtime location
	homeDir, _ := os.UserHomeDir()
	runtimeGenDir := filepath.Join(homeDir, ".corkscrew", "plugins", 
		fmt.Sprintf("%s-provider", "aws"), "generated")
	
	if err := os.MkdirAll(runtimeGenDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime generated directory: %w", err)
	}
	
	// Copy all *_final.json files
	return copyAnalysisFiles(generatedDir, runtimeGenDir)
}

// copyAnalysisFiles copies analysis files from source to destination
func copyAnalysisFiles(srcDir, destDir string) error {
	files, err := filepath.Glob(filepath.Join(srcDir, "*_final.json"))
	if err != nil {
		return fmt.Errorf("failed to find analysis files: %w", err)
	}
	
	for _, file := range files {
		basename := filepath.Base(file)
		destFile := filepath.Join(destDir, basename)
		
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read analysis file %s: %w", file, err)
		}
		
		if err := os.WriteFile(destFile, data, 0644); err != nil {
			return fmt.Errorf("failed to copy analysis file to %s: %w", destFile, err)
		}
	}
	
	return nil
}

func buildPluginsFromConfig(config *InitConfig, corkscrewConfig *CorkscrewConfig) error {
	for provider, cfg := range corkscrewConfig.Providers {
		if !cfg.Enabled {
			continue
		}

		fmt.Printf("  🔨 Building %s-provider...", provider)
		
		// Check if plugin source exists
		pluginDir := fmt.Sprintf("./plugins/%s-provider", provider)
		if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
			fmt.Printf(" ⚠️  source not found, skipping\n")
			continue
		}

		// Build the plugin
		if err := buildPlugin(provider, config.PluginDir); err != nil {
			fmt.Printf(" ❌ failed: %v\n", err)
			continue
		}

		fmt.Printf(" ✓\n")
	}

	return nil
}

func buildPlugin(provider, pluginDir string) error {
	// Use the existing autoBuildPlugin function logic
	if err := autoBuildPlugin(provider, false); err != nil {
		return err
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
	fmt.Println("  2. Downloads protoc v25.3 and duckdb v1.2.2")
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
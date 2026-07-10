package plugins

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type PluginManager struct {
	pluginDirs []string
	sourceDir  string
}

func NewPluginManager() *PluginManager {
	return &PluginManager{
		pluginDirs: DefaultPluginDirs(),
		sourceDir:  "./plugins",
	}
}

func NewPluginManagerWithDirs(pluginDirs []string, sourceDir string) *PluginManager {
	if len(pluginDirs) == 0 {
		pluginDirs = DefaultPluginDirs()
	}
	if sourceDir == "" {
		sourceDir = "./plugins"
	}
	return &PluginManager{
		pluginDirs: pluginDirs,
		sourceDir:  sourceDir,
	}
}

func DefaultPluginDirs() []string {
	homeDir, _ := os.UserHomeDir()
	dirs := []string{}
	if envDir := strings.TrimSpace(os.Getenv("CORKSCREW_PLUGIN_DIR")); envDir != "" {
		dirs = append(dirs, envDir)
	}
	dirs = append(dirs,
		"./build/bin",
		"./plugins",
		filepath.Join(homeDir, ".corkscrew", "plugins"),
		filepath.Join(homeDir, ".corkscrew", "bin", "plugin"),
		"/usr/local/lib/corkscrew/plugins",
	)
	return dirs
}

func ProviderBinaryName(provider string) string {
	return fmt.Sprintf("%s-provider", provider)
}

func PluginSearchPaths(provider string, pluginDirs []string) []string {
	if len(pluginDirs) == 0 {
		pluginDirs = DefaultPluginDirs()
	}

	pluginName := ProviderBinaryName(provider)
	paths := make([]string, 0, len(pluginDirs)*2)
	for _, dir := range pluginDirs {
		paths = append(paths,
			filepath.Join(dir, pluginName),
			filepath.Join(dir, pluginName, pluginName),
		)
	}
	return paths
}

// FindPlugin looks for installed plugin binary
func (pm *PluginManager) FindPlugin(provider string) (string, error) {
	for _, path := range PluginSearchPaths(provider, pm.pluginDirs) {
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path, nil
		}
	}

	return "", fmt.Errorf("plugin not found: %s", provider)
}

// CanBuildPlugin checks if source exists for plugin
func (pm *PluginManager) CanBuildPlugin(provider string) bool {
	sourceDir := filepath.Join(pm.sourceDir, ProviderBinaryName(provider))
	_, err := os.Stat(sourceDir)
	return err == nil
}

// BuildPlugin builds a plugin from source
func (pm *PluginManager) BuildPlugin(provider string) error {
	fmt.Printf("🔨 Building %s plugin...\n", provider)

	script := filepath.Join("plugins", fmt.Sprintf("build-%s-plugin.sh", provider))
	if _, err := os.Stat(script); err == nil {
		// Use existing build script
		cmd := exec.Command("bash", script)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Fallback to direct go build
	sourceDir := filepath.Join(pm.sourceDir, ProviderBinaryName(provider))

	// Create plugins directory if it doesn't exist
	pluginsDir := filepath.Join("plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugins directory: %w", err)
	}

	outputPath := filepath.Join(pluginsDir, ProviderBinaryName(provider))

	cmd := exec.Command("go", "build", "-o", outputPath, ".")
	cmd.Dir = sourceDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Make executable
	if err := os.Chmod(outputPath, 0755); err != nil {
		return fmt.Errorf("failed to make plugin executable: %w", err)
	}

	fmt.Printf("✅ Built %s plugin successfully\n", provider)
	return nil
}

// PromptBuildPlugin prompts the user to build a plugin (for explicit plugin commands only)
func (pm *PluginManager) PromptBuildPlugin(provider string) (bool, error) {
	if !pm.CanBuildPlugin(provider) {
		return false, fmt.Errorf("no source available for %s plugin", provider)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Build %s plugin? [Y/n]: ", provider)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response == "" || response == "y" || response == "yes" {
		return true, pm.BuildPlugin(provider)
	}

	return false, nil
}

// GetPluginStatus returns the status of a plugin
func (pm *PluginManager) GetPluginStatus(provider string) string {
	if _, err := pm.FindPlugin(provider); err == nil {
		return "✅ Installed"
	}

	if pm.CanBuildPlugin(provider) {
		return "🔨 Can Build"
	}

	return "❌ Not Available"
}

// ListAvailablePlugins returns information about all plugins
func (pm *PluginManager) ListAvailablePlugins() map[string]PluginInfo {
	// Try to load from registry first
	if registry, err := pm.LoadRegistry(); err == nil {
		result := make(map[string]PluginInfo)
		for provider, pluginData := range registry.Plugins {
			info := PluginInfo{
				Name:        pluginData.Name,
				Description: pluginData.Description,
				Version:     pluginData.Version,
				Source:      pluginData.Source,
				Binary:      pluginData.Binary,
				Status:      pm.GetPluginStatus(provider),
			}
			result[provider] = info
		}
		return result
	}

	// Fallback to hardcoded list
	plugins := map[string]PluginInfo{
		"aws": {
			Name:        "aws-provider",
			Description: "Amazon Web Services provider",
			Version:     "2.0.0",
			Source:      "plugins/aws-provider",
			Binary:      "aws-provider",
			Status:      "stable",
		},
		"azure": {
			Name:        "azure-provider",
			Description: "Microsoft Azure provider",
			Version:     "2.0.0",
			Source:      "plugins/azure-provider",
			Binary:      "azure-provider",
			Status:      "stable",
		},
		"gcp": {
			Name:        "gcp-provider",
			Description: "Google Cloud Platform provider",
			Version:     "1.0.0",
			Source:      "plugins/gcp-provider",
			Binary:      "gcp-provider",
			Status:      "beta",
		},
		"kubernetes": {
			Name:        "kubernetes-provider",
			Description: "Kubernetes provider",
			Version:     "1.0.0",
			Source:      "plugins/kubernetes-provider",
			Binary:      "kubernetes-provider",
			Status:      "beta",
		},
		"github": {
			Name:        "github-provider",
			Description: "GitHub organization and repository posture provider",
			Version:     "0.1.0",
			Source:      "plugins/github-provider",
			Binary:      "github-provider",
			Status:      "alpha",
		},
		"cloudflare": {
			Name:        "cloudflare-provider",
			Description: "Cloudflare provider for edge, Workers, storage, and Zero Trust posture",
			Version:     "0.1.0",
			Source:      "plugins/cloudflare-provider",
			Binary:      "cloudflare-provider",
			Status:      "alpha",
		},
	}

	// Update status for each plugin
	for provider, info := range plugins {
		info.Status = pm.GetPluginStatus(provider)
		plugins[provider] = info
	}

	return plugins
}

// LoadRegistry loads the plugin registry from JSON file
func (pm *PluginManager) LoadRegistry() (*PluginRegistry, error) {
	registryPath := filepath.Join("plugins", "registry.json")

	data, err := os.ReadFile(registryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry: %w", err)
	}

	var registry PluginRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse registry: %w", err)
	}

	return &registry, nil
}

type PluginInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	Binary      string `json:"binary"`
	Status      string `json:"status"`
}

type PluginRegistry struct {
	Version string                    `json:"version"`
	Plugins map[string]PluginMetadata `json:"plugins"`
}

type PluginMetadata struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Version      string            `json:"version"`
	Source       string            `json:"source"`
	Binary       string            `json:"binary"`
	Releases     map[string]string `json:"releases"`
	Capabilities []string          `json:"capabilities"`
	Status       string            `json:"status"`
}

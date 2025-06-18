package autodiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
)

type PluginDiscovery struct {
	config          PluginDiscoveryConfig
	discoveredPlugins map[string]*PluginInfo
	pluginRegistry   *PluginRegistry
	mutex           sync.RWMutex
}

type PluginDiscoveryConfig struct {
	PluginDirectories    []string
	AutoInstallEnabled   bool
	RegistryURL         string
	CacheExpiration     time.Duration
	AllowedSources      []string
	SecurityValidation  bool
}

type PluginInfo struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	Provider        string                 `json:"provider"`
	Description     string                 `json:"description"`
	Author          string                 `json:"author"`
	Homepage        string                 `json:"homepage"`
	BinaryPath      string                 `json:"binary_path"`
	ConfigPath      string                 `json:"config_path"`
	Dependencies    []string               `json:"dependencies"`
	Capabilities    []string               `json:"capabilities"`
	SupportedRegions []string              `json:"supported_regions"`
	Metadata        map[string]interface{} `json:"metadata"`
	LastUpdated     time.Time              `json:"last_updated"`
	Installed       bool                   `json:"installed"`
	Active          bool                   `json:"active"`
}

type PluginManifest struct {
	APIVersion  string                 `json:"apiVersion"`
	Kind        string                 `json:"kind"`
	Metadata    PluginMetadata         `json:"metadata"`
	Spec        PluginSpec             `json:"spec"`
}

type PluginMetadata struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type PluginSpec struct {
	Provider         string            `json:"provider"`
	Binary           string            `json:"binary"`
	Config           string            `json:"config"`
	Dependencies     []string          `json:"dependencies"`
	Capabilities     []string          `json:"capabilities"`
	SupportedRegions []string          `json:"supportedRegions"`
	Requirements     PluginRequirements `json:"requirements"`
	Installation     InstallationSpec   `json:"installation"`
}

type PluginRequirements struct {
	MinVersion     string   `json:"minVersion"`
	MaxVersion     string   `json:"maxVersion"`
	Architecture   []string `json:"architecture"`
	OperatingSystem []string `json:"operatingSystem"`
}

type InstallationSpec struct {
	Method      string            `json:"method"`
	Source      string            `json:"source"`
	Checksum    string            `json:"checksum"`
	Environment map[string]string `json:"environment"`
}

func NewPluginDiscovery(config PluginDiscoveryConfig) *PluginDiscovery {
	return &PluginDiscovery{
		config:            config,
		discoveredPlugins: make(map[string]*PluginInfo),
		pluginRegistry:    NewPluginRegistry(config.RegistryURL),
	}
}

func (pd *PluginDiscovery) DiscoverPlugins(ctx context.Context) ([]*PluginInfo, error) {
	var allPlugins []*PluginInfo
	
	// Discover local plugins
	localPlugins, err := pd.discoverLocalPlugins(ctx)
	if err != nil {
		return nil, fmt.Errorf("local plugin discovery failed: %w", err)
	}
	allPlugins = append(allPlugins, localPlugins...)
	
	// Discover registry plugins
	registryPlugins, err := pd.discoverRegistryPlugins(ctx)
	if err != nil {
		// Don't fail if registry is unavailable, just log the error
		fmt.Printf("Registry plugin discovery failed: %v\n", err)
	} else {
		allPlugins = append(allPlugins, registryPlugins...)
	}
	
	// Update internal cache
	pd.mutex.Lock()
	for _, plugin := range allPlugins {
		pd.discoveredPlugins[plugin.Name] = plugin
	}
	pd.mutex.Unlock()
	
	return allPlugins, nil
}

func (pd *PluginDiscovery) discoverLocalPlugins(ctx context.Context) ([]*PluginInfo, error) {
	var plugins []*PluginInfo
	
	for _, directory := range pd.config.PluginDirectories {
		dirPlugins, err := pd.scanDirectory(directory)
		if err != nil {
			return nil, fmt.Errorf("failed to scan directory %s: %w", directory, err)
		}
		plugins = append(plugins, dirPlugins...)
	}
	
	return plugins, nil
}

func (pd *PluginDiscovery) scanDirectory(directory string) ([]*PluginInfo, error) {
	var plugins []*PluginInfo
	
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue walking even if some files are inaccessible
		}
		
		// Look for plugin manifests
		if strings.HasSuffix(info.Name(), "plugin.json") || strings.HasSuffix(info.Name(), "manifest.yaml") {
			plugin, err := pd.loadPluginFromManifest(path)
			if err != nil {
				fmt.Printf("Failed to load plugin from %s: %v\n", path, err)
				return nil // Continue processing other plugins
			}
			plugins = append(plugins, plugin)
		}
		
		// Look for binary plugins
		if pd.isBinaryPlugin(path, info) {
			plugin, err := pd.loadPluginFromBinary(path)
			if err != nil {
				fmt.Printf("Failed to load binary plugin from %s: %v\n", path, err)
				return nil
			}
			plugins = append(plugins, plugin)
		}
		
		return nil
	})
	
	return plugins, err
}

func (pd *PluginDiscovery) loadPluginFromManifest(manifestPath string) (*PluginInfo, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	
	var manifest PluginManifest
	if strings.HasSuffix(manifestPath, ".json") {
		err = json.Unmarshal(data, &manifest)
	} else {
		// For YAML files, you'd use yaml.Unmarshal
		// For now, assuming JSON format
		err = json.Unmarshal(data, &manifest)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	
	plugin := &PluginInfo{
		Name:            manifest.Metadata.Name,
		Version:         manifest.Metadata.Version,
		Provider:        manifest.Spec.Provider,
		Description:     manifest.Metadata.Description,
		BinaryPath:      pd.resolvePath(manifestPath, manifest.Spec.Binary),
		ConfigPath:      pd.resolvePath(manifestPath, manifest.Spec.Config),
		Dependencies:    manifest.Spec.Dependencies,
		Capabilities:    manifest.Spec.Capabilities,
		SupportedRegions: manifest.Spec.SupportedRegions,
		Metadata:        make(map[string]interface{}),
		LastUpdated:     time.Now(),
		Installed:       pd.isPluginInstalled(manifest.Spec.Binary),
		Active:          false,
	}
	
	// Add labels and annotations to metadata
	for k, v := range manifest.Metadata.Labels {
		plugin.Metadata["label:"+k] = v
	}
	for k, v := range manifest.Metadata.Annotations {
		plugin.Metadata["annotation:"+k] = v
	}
	
	return plugin, nil
}

func (pd *PluginDiscovery) loadPluginFromBinary(binaryPath string) (*PluginInfo, error) {
	// Try to load the plugin to extract metadata
	p, err := plugin.Open(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}
	
	// Look for standard plugin metadata functions
	getMetadata, err := p.Lookup("GetPluginMetadata")
	if err != nil {
		// Fallback to basic info extraction
		return pd.extractBasicPluginInfo(binaryPath), nil
	}
	
	metadataFunc, ok := getMetadata.(func() *PluginInfo)
	if !ok {
		return pd.extractBasicPluginInfo(binaryPath), nil
	}
	
	pluginInfo := metadataFunc()
	pluginInfo.BinaryPath = binaryPath
	pluginInfo.Installed = true
	pluginInfo.LastUpdated = time.Now()
	
	return pluginInfo, nil
}

func (pd *PluginDiscovery) extractBasicPluginInfo(binaryPath string) *PluginInfo {
	filename := filepath.Base(binaryPath)
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	
	// Try to extract provider from filename pattern
	provider := "unknown"
	if strings.Contains(name, "aws") {
		provider = "aws"
	} else if strings.Contains(name, "azure") {
		provider = "azure"
	} else if strings.Contains(name, "gcp") {
		provider = "gcp"
	} else if strings.Contains(name, "k8s") || strings.Contains(name, "kubernetes") {
		provider = "kubernetes"
	}
	
	return &PluginInfo{
		Name:        name,
		Version:     "unknown",
		Provider:    provider,
		Description: fmt.Sprintf("Auto-discovered plugin: %s", name),
		BinaryPath:  binaryPath,
		Metadata:    make(map[string]interface{}),
		LastUpdated: time.Now(),
		Installed:   true,
		Active:      false,
	}
}

func (pd *PluginDiscovery) discoverRegistryPlugins(ctx context.Context) ([]*PluginInfo, error) {
	if pd.config.RegistryURL == "" {
		return []*PluginInfo{}, nil
	}
	
	return pd.pluginRegistry.ListAvailablePlugins(ctx)
}

func (pd *PluginDiscovery) AutoInstallPlugin(ctx context.Context, pluginName string) error {
	if !pd.config.AutoInstallEnabled {
		return fmt.Errorf("auto-installation is disabled")
	}
	
	pd.mutex.RLock()
	plugin, exists := pd.discoveredPlugins[pluginName]
	pd.mutex.RUnlock()
	
	if !exists {
		return fmt.Errorf("plugin %s not found in discovery cache", pluginName)
	}
	
	if plugin.Installed {
		return fmt.Errorf("plugin %s is already installed", pluginName)
	}
	
	installer := NewPluginInstaller(pd.config)
	return installer.InstallPlugin(ctx, plugin)
}

func (pd *PluginDiscovery) GetInstalledPlugins() []*PluginInfo {
	pd.mutex.RLock()
	defer pd.mutex.RUnlock()
	
	var installed []*PluginInfo
	for _, plugin := range pd.discoveredPlugins {
		if plugin.Installed {
			installed = append(installed, plugin)
		}
	}
	
	return installed
}

func (pd *PluginDiscovery) GetAvailablePlugins() []*PluginInfo {
	pd.mutex.RLock()
	defer pd.mutex.RUnlock()
	
	var available []*PluginInfo
	for _, plugin := range pd.discoveredPlugins {
		if !plugin.Installed {
			available = append(available, plugin)
		}
	}
	
	return available
}

func (pd *PluginDiscovery) ValidatePlugin(plugin *PluginInfo) error {
	if !pd.config.SecurityValidation {
		return nil
	}
	
	// Basic security validation
	if plugin.BinaryPath != "" {
		if !pd.isPathSafe(plugin.BinaryPath) {
			return fmt.Errorf("plugin binary path is not safe: %s", plugin.BinaryPath)
		}
		
		if !pd.isSourceTrusted(plugin.BinaryPath) {
			return fmt.Errorf("plugin source is not trusted: %s", plugin.BinaryPath)
		}
	}
	
	return nil
}

func (pd *PluginDiscovery) isBinaryPlugin(path string, info os.FileInfo) bool {
	if info.IsDir() {
		return false
	}
	
	// Check for executable files
	if info.Mode()&0111 == 0 {
		return false
	}
	
	// Check for known plugin patterns
	name := strings.ToLower(info.Name())
	return strings.Contains(name, "plugin") || 
		   strings.Contains(name, "provider") ||
		   strings.HasPrefix(name, "corkscrew-")
}

func (pd *PluginDiscovery) isPluginInstalled(binaryPath string) bool {
	if binaryPath == "" {
		return false
	}
	
	resolvedPath := pd.resolvePath("", binaryPath)
	_, err := os.Stat(resolvedPath)
	return err == nil
}

func (pd *PluginDiscovery) resolvePath(basePath, relativePath string) string {
	if filepath.IsAbs(relativePath) {
		return relativePath
	}
	
	if basePath != "" {
		baseDir := filepath.Dir(basePath)
		return filepath.Join(baseDir, relativePath)
	}
	
	return relativePath
}

func (pd *PluginDiscovery) isPathSafe(path string) bool {
	// Basic path traversal protection
	cleanPath := filepath.Clean(path)
	return !strings.Contains(cleanPath, "..")
}

func (pd *PluginDiscovery) isSourceTrusted(path string) bool {
	if len(pd.config.AllowedSources) == 0 {
		return true // No restrictions
	}
	
	for _, allowedSource := range pd.config.AllowedSources {
		if strings.HasPrefix(path, allowedSource) {
			return true
		}
	}
	
	return false
}
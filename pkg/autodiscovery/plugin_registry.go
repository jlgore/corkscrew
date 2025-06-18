package autodiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type PluginRegistry struct {
	baseURL    string
	httpClient *http.Client
	cache      *RegistryCache
}

type RegistryCache struct {
	plugins     []*PluginInfo
	lastUpdated time.Time
	ttl         time.Duration
}

type RegistryResponse struct {
	Plugins []RegistryPlugin `json:"plugins"`
	Total   int              `json:"total"`
	Page    int              `json:"page"`
	PerPage int              `json:"per_page"`
}

type RegistryPlugin struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	Provider        string                 `json:"provider"`
	Description     string                 `json:"description"`
	Author          string                 `json:"author"`
	Homepage        string                 `json:"homepage"`
	DownloadURL     string                 `json:"download_url"`
	Checksum        string                 `json:"checksum"`
	Dependencies    []string               `json:"dependencies"`
	Capabilities    []string               `json:"capabilities"`
	SupportedRegions []string              `json:"supported_regions"`
	Metadata        map[string]interface{} `json:"metadata"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	Downloads       int                    `json:"downloads"`
	Rating          float64                `json:"rating"`
}

func NewPluginRegistry(baseURL string) *PluginRegistry {
	return &PluginRegistry{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: &RegistryCache{
			ttl: 1 * time.Hour,
		},
	}
}

func (pr *PluginRegistry) ListAvailablePlugins(ctx context.Context) ([]*PluginInfo, error) {
	// Check cache first
	if pr.cache.plugins != nil && time.Since(pr.cache.lastUpdated) < pr.cache.ttl {
		return pr.cache.plugins, nil
	}
	
	// Fetch from registry
	plugins, err := pr.fetchPluginsFromRegistry(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch plugins from registry: %w", err)
	}
	
	// Update cache
	pr.cache.plugins = plugins
	pr.cache.lastUpdated = time.Now()
	
	return plugins, nil
}

func (pr *PluginRegistry) fetchPluginsFromRegistry(ctx context.Context) ([]*PluginInfo, error) {
	if pr.baseURL == "" {
		return []*PluginInfo{}, nil
	}
	
	url := fmt.Sprintf("%s/api/v1/plugins", strings.TrimSuffix(pr.baseURL, "/"))
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "corkscrew-plugin-discovery/1.0")
	
	resp, err := pr.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch plugins: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var registryResp RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&registryResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	// Convert registry plugins to plugin info
	plugins := make([]*PluginInfo, len(registryResp.Plugins))
	for i, regPlugin := range registryResp.Plugins {
		plugins[i] = pr.convertRegistryPlugin(regPlugin)
	}
	
	return plugins, nil
}

func (pr *PluginRegistry) convertRegistryPlugin(regPlugin RegistryPlugin) *PluginInfo {
	plugin := &PluginInfo{
		Name:            regPlugin.Name,
		Version:         regPlugin.Version,
		Provider:        regPlugin.Provider,
		Description:     regPlugin.Description,
		Author:          regPlugin.Author,
		Homepage:        regPlugin.Homepage,
		Dependencies:    regPlugin.Dependencies,
		Capabilities:    regPlugin.Capabilities,
		SupportedRegions: regPlugin.SupportedRegions,
		Metadata:        regPlugin.Metadata,
		LastUpdated:     regPlugin.UpdatedAt,
		Installed:       false,
		Active:          false,
	}
	
	if plugin.Metadata == nil {
		plugin.Metadata = make(map[string]interface{})
	}
	
	// Add registry-specific metadata
	plugin.Metadata["registry_url"] = pr.baseURL
	plugin.Metadata["download_url"] = regPlugin.DownloadURL
	plugin.Metadata["checksum"] = regPlugin.Checksum
	plugin.Metadata["downloads"] = regPlugin.Downloads
	plugin.Metadata["rating"] = regPlugin.Rating
	plugin.Metadata["created_at"] = regPlugin.CreatedAt
	
	return plugin
}

func (pr *PluginRegistry) GetPluginDetails(ctx context.Context, pluginName, version string) (*PluginInfo, error) {
	url := fmt.Sprintf("%s/api/v1/plugins/%s", strings.TrimSuffix(pr.baseURL, "/"), pluginName)
	if version != "" {
		url += fmt.Sprintf("/%s", version)
	}
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "corkscrew-plugin-discovery/1.0")
	
	resp, err := pr.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch plugin details: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin %s not found in registry", pluginName)
	}
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var regPlugin RegistryPlugin
	if err := json.NewDecoder(resp.Body).Decode(&regPlugin); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return pr.convertRegistryPlugin(regPlugin), nil
}

func (pr *PluginRegistry) SearchPlugins(ctx context.Context, query string, provider string) ([]*PluginInfo, error) {
	url := fmt.Sprintf("%s/api/v1/plugins/search", strings.TrimSuffix(pr.baseURL, "/"))
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	q := req.URL.Query()
	if query != "" {
		q.Add("q", query)
	}
	if provider != "" {
		q.Add("provider", provider)
	}
	req.URL.RawQuery = q.Encode()
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "corkscrew-plugin-discovery/1.0")
	
	resp, err := pr.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search plugins: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var registryResp RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&registryResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	plugins := make([]*PluginInfo, len(registryResp.Plugins))
	for i, regPlugin := range registryResp.Plugins {
		plugins[i] = pr.convertRegistryPlugin(regPlugin)
	}
	
	return plugins, nil
}

func (pr *PluginRegistry) GetPluginVersions(ctx context.Context, pluginName string) ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/plugins/%s/versions", strings.TrimSuffix(pr.baseURL, "/"), pluginName)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "corkscrew-plugin-discovery/1.0")
	
	resp, err := pr.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch plugin versions: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin %s not found in registry", pluginName)
	}
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var versionsResp struct {
		Versions []string `json:"versions"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&versionsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return versionsResp.Versions, nil
}

func (pr *PluginRegistry) DownloadPlugin(ctx context.Context, plugin *PluginInfo, outputPath string) error {
	downloadURL, ok := plugin.Metadata["download_url"].(string)
	if !ok || downloadURL == "" {
		return fmt.Errorf("no download URL available for plugin %s", plugin.Name)
	}
	
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	
	req.Header.Set("User-Agent", "corkscrew-plugin-discovery/1.0")
	
	resp, err := pr.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download plugin: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	
	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()
	
	// Copy response body to file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write plugin file: %w", err)
	}
	
	return nil
}

func (pr *PluginRegistry) InvalidateCache() {
	pr.cache.plugins = nil
	pr.cache.lastUpdated = time.Time{}
}
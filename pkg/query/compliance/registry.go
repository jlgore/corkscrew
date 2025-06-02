package compliance

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// RegistryClient manages discovery, download, and installation of compliance packs
type RegistryClient struct {
	mu            sync.RWMutex
	registryCache *RegistryCache
	cachePath     string
	httpClient    *http.Client
	githubToken   string
	retryConfig   RetryConfig
	userAgent     string
	baseURLs      []string // Support multiple registry URLs
	offlineMode   bool
}

// RegistryCache represents the local registry cache structure
type RegistryCache struct {
	LastUpdated time.Time           `json:"last_updated"`
	TTL         time.Duration       `json:"ttl"`
	Registries  map[string]Registry `json:"registries"` // registry URL -> registry data
	Packs       map[string]PackInfo `json:"packs"`      // namespace/name -> pack info
	Version     string              `json:"version"`
}

// Registry represents a pack registry
type Registry struct {
	Name        string               `json:"name"`
	URL         string               `json:"url"`
	Type        string               `json:"type"` // github, gitlab, custom
	LastUpdated time.Time            `json:"last_updated"`
	Packs       map[string]PackInfo  `json:"packs"`
	Metadata    RegistryMetadata     `json:"metadata"`
}

// RegistryMetadata contains registry-level metadata
type RegistryMetadata struct {
	Description string            `json:"description"`
	Maintainers []string          `json:"maintainers"`
	Tags        []string          `json:"tags"`
	Categories  []string          `json:"categories"`
	Providers   []string          `json:"providers"`
	Frameworks  []string          `json:"frameworks"`
	Labels      map[string]string `json:"labels"`
}

// PackInfo represents information about a pack in the registry
type PackInfo struct {
	Name        string                 `json:"name"`
	Namespace   string                 `json:"namespace"`
	Description string                 `json:"description"`
	Versions    []RegistryPackVersion  `json:"versions"`
	LatestVersion string               `json:"latest_version"`
	Provider    string                 `json:"provider"`
	Frameworks  []string               `json:"frameworks"`
	Tags        []string               `json:"tags"`
	Categories  []string               `json:"categories"`
	Maintainers []string               `json:"maintainers"`
	Repository  PackRepository         `json:"repository"`
	Downloads   PackDownloadStats      `json:"downloads"`
	Metadata    map[string]interface{} `json:"metadata"`
	LastUpdated time.Time              `json:"last_updated"`
}

// RegistryPackVersion represents a specific version of a pack in the registry
type RegistryPackVersion struct {
	Version     string            `json:"version"`
	Tag         string            `json:"tag"`
	Commit      string            `json:"commit"`
	ReleaseDate time.Time         `json:"release_date"`
	Checksum    string            `json:"checksum"` // SHA256
	Size        int64             `json:"size"`
	DownloadURL string            `json:"download_url"`
	TarballURL  string            `json:"tarball_url"`
	Deprecated  bool              `json:"deprecated"`
	Metadata    map[string]string `json:"metadata"`
}

// PackRepository contains repository information
type PackRepository struct {
	Type      string `json:"type"` // github, gitlab
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	Path      string `json:"path"`      // Path within repository
	URL       string `json:"url"`       // Clone URL
	WebURL    string `json:"web_url"`   // Web interface URL
	APIURL    string `json:"api_url"`   // API URL
}

// PackDownloadStats tracks download statistics
type PackDownloadStats struct {
	Total      int64            `json:"total"`
	LastMonth  int64            `json:"last_month"`
	LastWeek   int64            `json:"last_week"`
	ByVersion  map[string]int64 `json:"by_version"`
	LastUpdate time.Time        `json:"last_update"`
}

// SearchCriteria defines search parameters for packs
type SearchCriteria struct {
	Query      string   `json:"query,omitempty"`
	Provider   string   `json:"provider,omitempty"`
	Framework  string   `json:"framework,omitempty"`
	Category   string   `json:"category,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Namespace  string   `json:"namespace,omitempty"`
	Sort       string   `json:"sort,omitempty"`       // name, downloads, updated
	Order      string   `json:"order,omitempty"`      // asc, desc
	Limit      int      `json:"limit,omitempty"`
	Offset     int      `json:"offset,omitempty"`
}

// SearchResult represents search results
type SearchResult struct {
	Packs      []PackInfo `json:"packs"`
	Total      int        `json:"total"`
	Limit      int        `json:"limit"`
	Offset     int        `json:"offset"`
	Query      string     `json:"query"`
	Duration   time.Duration `json:"duration"`
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxRetries int           `json:"max_retries"`
	RetryDelay time.Duration `json:"retry_delay"`
	Backoff    float64       `json:"backoff"` // Exponential backoff multiplier
}

// RegistryError represents registry-related errors
type RegistryError struct {
	Operation string
	URL       string
	Message   string
	Cause     error
}

func (e RegistryError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("registry %s failed for '%s': %s (caused by: %v)", e.Operation, e.URL, e.Message, e.Cause)
	}
	return fmt.Sprintf("registry %s failed for '%s': %s", e.Operation, e.URL, e.Message)
}

func (e RegistryError) Unwrap() error {
	return e.Cause
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName     string                 `json:"tag_name"`
	Name        string                 `json:"name"`
	PublishedAt time.Time              `json:"published_at"`
	TarballURL  string                 `json:"tarball_url"`
	ZipballURL  string                 `json:"zipball_url"`
	Assets      []GitHubReleaseAsset   `json:"assets"`
	Prerelease  bool                   `json:"prerelease"`
	Draft       bool                   `json:"draft"`
}

// GitHubReleaseAsset represents a release asset
type GitHubReleaseAsset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"browser_download_url"`
	ContentType string `json:"content_type"`
}

// GitHubRepository represents repository information from GitHub API
type GitHubRepository struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	CloneURL    string    `json:"clone_url"`
	HTMLURL     string    `json:"html_url"`
	UpdatedAt   time.Time `json:"updated_at"`
	Topics      []string  `json:"topics"`
}

// NewRegistryClient creates a new registry client
func NewRegistryClient() *RegistryClient {
	homeDir, _ := os.UserHomeDir()
	cachePath := filepath.Join(homeDir, ".corkscrew", "query", "registry.json")
	
	// Ensure cache directory exists
	os.MkdirAll(filepath.Dir(cachePath), 0755)
	
	client := &RegistryClient{
		cachePath: cachePath,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		retryConfig: RetryConfig{
			MaxRetries: 3,
			RetryDelay: time.Second,
			Backoff:    2.0,
		},
		userAgent: "Corkscrew-Registry-Client/1.0",
		baseURLs: []string{
			"https://api.github.com", // Default GitHub API
		},
	}
	
	// Load existing cache
	client.loadCache()
	
	return client
}

// WithGitHubToken configures GitHub authentication
func (c *RegistryClient) WithGitHubToken(token string) *RegistryClient {
	c.githubToken = token
	return c
}

// WithOfflineMode enables/disables offline mode
func (c *RegistryClient) WithOfflineMode(offline bool) *RegistryClient {
	c.offlineMode = offline
	return c
}

// WithRetryConfig sets retry configuration
func (c *RegistryClient) WithRetryConfig(config RetryConfig) *RegistryClient {
	c.retryConfig = config
	return c
}

// WithCacheTTL sets cache TTL
func (c *RegistryClient) WithCacheTTL(ttl time.Duration) *RegistryClient {
	if c.registryCache != nil {
		c.registryCache.TTL = ttl
	}
	return c
}

// UpdateRegistry updates the local registry cache from remote sources
func (c *RegistryClient) UpdateRegistry(ctx context.Context, forceRefresh bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.offlineMode {
		return nil // Skip update in offline mode
	}
	
	// Check if update is needed
	if !forceRefresh && c.registryCache != nil {
		if time.Since(c.registryCache.LastUpdated) < c.registryCache.TTL {
			return nil // Cache is still valid
		}
	}
	
	// Initialize cache if needed
	if c.registryCache == nil {
		c.registryCache = &RegistryCache{
			TTL:        24 * time.Hour, // Default 24 hour TTL
			Registries: make(map[string]Registry),
			Packs:      make(map[string]PackInfo),
			Version:    "1.0",
		}
	}
	
	// Update from GitHub-based registries
	for _, baseURL := range c.baseURLs {
		if strings.Contains(baseURL, "github.com") {
			if err := c.updateFromGitHub(ctx, baseURL); err != nil {
				// Log error but continue with other registries
				continue
			}
		}
	}
	
	c.registryCache.LastUpdated = time.Now()
	
	// Save cache to disk
	return c.saveCache()
}

// updateFromGitHub updates the cache with packs from GitHub
func (c *RegistryClient) updateFromGitHub(ctx context.Context, baseURL string) error {
	// Search for repositories with compliance pack topics
	topics := []string{
		"corkscrew-compliance-pack",
		"compliance-pack",
		"ccc-pack",
		"security-compliance",
	}
	
	for _, topic := range topics {
		if err := c.searchGitHubRepositories(ctx, topic); err != nil {
			// Continue with other topics
			continue
		}
	}
	
	return nil
}

// searchGitHubRepositories searches GitHub for repositories with compliance packs
func (c *RegistryClient) searchGitHubRepositories(ctx context.Context, topic string) error {
	url := fmt.Sprintf("https://api.github.com/search/repositories?q=topic:%s&sort=updated&order=desc&per_page=100", topic)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	
	// Add authentication if available
	if c.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", c.githubToken))
	}
	
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", c.userAgent)
	
	resp, err := c.doRequestWithRetry(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	var searchResult struct {
		TotalCount int                `json:"total_count"`
		Items      []GitHubRepository `json:"items"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return err
	}
	
	// Process each repository
	for _, repo := range searchResult.Items {
		if err := c.processGitHubRepository(ctx, repo); err != nil {
			// Continue with other repositories
			continue
		}
	}
	
	return nil
}

// processGitHubRepository processes a single GitHub repository
func (c *RegistryClient) processGitHubRepository(ctx context.Context, repo GitHubRepository) error {
	// Parse owner/name from full_name
	parts := strings.Split(repo.FullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository name: %s", repo.FullName)
	}
	
	owner, name := parts[0], parts[1]
	
	// Try to find pack metadata in the repository
	packInfo, err := c.extractPackInfo(ctx, owner, name, repo)
	if err != nil {
		return err
	}
	
	// Store in cache
	namespace := fmt.Sprintf("%s/%s", owner, name)
	c.registryCache.Packs[namespace] = *packInfo
	
	return nil
}

// extractPackInfo extracts pack information from a GitHub repository
func (c *RegistryClient) extractPackInfo(ctx context.Context, owner, name string, repo GitHubRepository) (*PackInfo, error) {
	// Try to fetch manifest.yaml or pack.yaml from the repository
	manifestURLs := []string{
		fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/manifest.yaml", owner, name),
		fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/pack.yaml", owner, name),
		fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/corkscrew-pack.yaml", owner, name),
	}
	
	var manifest *PackManifest
	for _, url := range manifestURLs {
		if m, err := c.fetchManifestFromGitHub(ctx, url); err == nil {
			manifest = m
			break
		}
	}
	
	// If no manifest found, create basic pack info from repository
	if manifest == nil {
		return c.createPackInfoFromRepo(repo), nil
	}
	
	// Create pack info from manifest
	packInfo := &PackInfo{
		Name:        manifest.Metadata.Name,
		Namespace:   fmt.Sprintf("%s/%s", owner, name),
		Description: manifest.Metadata.Description,
		Provider:    manifest.Metadata.Provider,
		Frameworks:  manifest.Metadata.Frameworks,
		Tags:        manifest.Metadata.Tags,
		Maintainers: manifest.Metadata.Maintainers,
		Repository: PackRepository{
			Type:   "github",
			Owner:  owner,
			Name:   name,
			URL:    repo.CloneURL,
			WebURL: repo.HTMLURL,
			APIURL: fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, name),
		},
		LastUpdated: repo.UpdatedAt,
	}
	
	// Fetch version information
	versions, err := c.fetchPackVersions(ctx, owner, name)
	if err == nil {
		packInfo.Versions = versions
		if len(versions) > 0 {
			packInfo.LatestVersion = versions[0].Version // Assuming sorted by date desc
		}
	}
	
	return packInfo, nil
}

// fetchManifestFromGitHub fetches a pack manifest from GitHub
func (c *RegistryClient) fetchManifestFromGitHub(ctx context.Context, url string) (*PackManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	if c.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", c.githubToken))
	}
	
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", c.userAgent)
	
	resp, err := c.doRequestWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}
	
	var fileInfo struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&fileInfo); err != nil {
		return nil, err
	}
	
	// Decode base64 content
	if fileInfo.Encoding != "base64" {
		return nil, fmt.Errorf("unsupported encoding: %s", fileInfo.Encoding)
	}
	
	content, err := hex.DecodeString(fileInfo.Content)
	if err != nil {
		return nil, err
	}
	
	// Parse YAML manifest
	var manifest PackManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, err
	}
	
	return &manifest, nil
}

// createPackInfoFromRepo creates basic pack info from repository metadata
func (c *RegistryClient) createPackInfoFromRepo(repo GitHubRepository) *PackInfo {
	parts := strings.Split(repo.FullName, "/")
	owner, name := parts[0], parts[1]
	
	return &PackInfo{
		Name:        name,
		Namespace:   repo.FullName,
		Description: repo.Description,
		Tags:        repo.Topics,
		Repository: PackRepository{
			Type:   "github",
			Owner:  owner,
			Name:   name,
			URL:    repo.CloneURL,
			WebURL: repo.HTMLURL,
			APIURL: fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, name),
		},
		LastUpdated: repo.UpdatedAt,
	}
}

// fetchPackVersions fetches version information from GitHub releases
func (c *RegistryClient) fetchPackVersions(ctx context.Context, owner, name string) ([]RegistryPackVersion, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, name)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	if c.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", c.githubToken))
	}
	
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", c.userAgent)
	
	resp, err := c.doRequestWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}
	
	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	
	var versions []RegistryPackVersion
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue // Skip drafts and prereleases
		}
		
		version := RegistryPackVersion{
			Version:     release.TagName,
			Tag:         release.TagName,
			ReleaseDate: release.PublishedAt,
			TarballURL:  release.TarballURL,
			DownloadURL: release.TarballURL,
		}
		
		versions = append(versions, version)
	}
	
	return versions, nil
}

// SearchPacks searches for packs based on criteria
func (c *RegistryClient) SearchPacks(ctx context.Context, criteria SearchCriteria) (*SearchResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	start := time.Now()
	
	// Ensure registry is updated
	if !c.offlineMode {
		c.mu.RUnlock()
		if err := c.UpdateRegistry(ctx, false); err != nil {
			c.mu.RLock()
			// Continue with cached data
		} else {
			c.mu.RLock()
		}
	}
	
	if c.registryCache == nil {
		return &SearchResult{
			Packs:    []PackInfo{},
			Total:    0,
			Duration: time.Since(start),
		}, nil
	}
	
	var matches []PackInfo
	
	// Filter packs based on criteria
	for _, pack := range c.registryCache.Packs {
		if c.matchesCriteria(pack, criteria) {
			matches = append(matches, pack)
		}
	}
	
	// Sort results
	c.sortPacks(matches, criteria.Sort, criteria.Order)
	
	// Apply pagination
	total := len(matches)
	if criteria.Limit > 0 {
		start := criteria.Offset
		end := start + criteria.Limit
		if start > len(matches) {
			matches = []PackInfo{}
		} else if end > len(matches) {
			matches = matches[start:]
		} else {
			matches = matches[start:end]
		}
	}
	
	return &SearchResult{
		Packs:    matches,
		Total:    total,
		Limit:    criteria.Limit,
		Offset:   criteria.Offset,
		Query:    criteria.Query,
		Duration: time.Since(start),
	}, nil
}

// matchesCriteria checks if a pack matches search criteria
func (c *RegistryClient) matchesCriteria(pack PackInfo, criteria SearchCriteria) bool {
	// Query match (name, description, tags)
	if criteria.Query != "" {
		query := strings.ToLower(criteria.Query)
		if !strings.Contains(strings.ToLower(pack.Name), query) &&
			!strings.Contains(strings.ToLower(pack.Description), query) &&
			!c.containsAny(pack.Tags, query) {
			return false
		}
	}
	
	// Provider match
	if criteria.Provider != "" && pack.Provider != criteria.Provider {
		return false
	}
	
	// Framework match
	if criteria.Framework != "" && !c.contains(pack.Frameworks, criteria.Framework) {
		return false
	}
	
	// Category match
	if criteria.Category != "" && !c.contains(pack.Categories, criteria.Category) {
		return false
	}
	
	// Namespace match
	if criteria.Namespace != "" && !strings.HasPrefix(pack.Namespace, criteria.Namespace) {
		return false
	}
	
	// Tags match (all specified tags must be present)
	if len(criteria.Tags) > 0 {
		for _, tag := range criteria.Tags {
			if !c.contains(pack.Tags, tag) {
				return false
			}
		}
	}
	
	return true
}

// sortPacks sorts packs based on sort criteria
func (c *RegistryClient) sortPacks(packs []PackInfo, sortBy, order string) {
	if sortBy == "" {
		sortBy = "name"
	}
	if order == "" {
		order = "asc"
	}
	
	sort.Slice(packs, func(i, j int) bool {
		var less bool
		
		switch sortBy {
		case "name":
			less = packs[i].Name < packs[j].Name
		case "downloads":
			less = packs[i].Downloads.Total < packs[j].Downloads.Total
		case "updated":
			less = packs[i].LastUpdated.Before(packs[j].LastUpdated)
		default:
			less = packs[i].Name < packs[j].Name
		}
		
		if order == "desc" {
			return !less
		}
		return less
	})
}

// DownloadPack downloads and extracts a pack
func (c *RegistryClient) DownloadPack(ctx context.Context, namespace, version string, destDir string) (*QueryPack, error) {
	c.mu.RLock()
	packInfo, exists := c.registryCache.Packs[namespace]
	c.mu.RUnlock()
	
	if !exists {
		return nil, RegistryError{
			Operation: "download",
			URL:       namespace,
			Message:   "pack not found in registry",
		}
	}
	
	// Find the specified version
	var packVersion *RegistryPackVersion
	if version == "" || version == "latest" {
		// Use latest version
		if len(packInfo.Versions) > 0 {
			packVersion = &packInfo.Versions[0]
		}
	} else {
		// Find specific version
		for _, v := range packInfo.Versions {
			if v.Version == version || v.Tag == version {
				packVersion = &v
				break
			}
		}
	}
	
	if packVersion == nil {
		return nil, RegistryError{
			Operation: "download",
			URL:       namespace,
			Message:   fmt.Sprintf("version %s not found", version),
		}
	}
	
	// Download and extract the pack
	return c.downloadAndExtract(ctx, packInfo, *packVersion, destDir)
}

// downloadAndExtract downloads and extracts a pack tarball
func (c *RegistryClient) downloadAndExtract(ctx context.Context, packInfo PackInfo, version RegistryPackVersion, destDir string) (*QueryPack, error) {
	// Create temporary file for download
	tmpFile, err := os.CreateTemp("", "corkscrew-pack-*.tar.gz")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()
	
	// Download the tarball
	if err := c.downloadFile(ctx, version.TarballURL, tmpFile); err != nil {
		return nil, RegistryError{
			Operation: "download",
			URL:       version.TarballURL,
			Message:   "failed to download tarball",
			Cause:     err,
		}
	}
	
	// Verify checksum if available
	if version.Checksum != "" {
		if err := c.verifyChecksum(tmpFile.Name(), version.Checksum); err != nil {
			return nil, RegistryError{
				Operation: "verify",
				URL:       version.TarballURL,
				Message:   "checksum verification failed",
				Cause:     err,
			}
		}
	}
	
	// Extract to destination
	extractDir := filepath.Join(destDir, packInfo.Namespace)
	if err := c.extractTarball(tmpFile.Name(), extractDir); err != nil {
		return nil, RegistryError{
			Operation: "extract",
			URL:       tmpFile.Name(),
			Message:   "failed to extract tarball",
			Cause:     err,
		}
	}
	
	// Load the pack using the pack loader
	loader := NewPackLoader().WithSearchPaths(destDir)
	return loader.LoadPack(ctx, packInfo.Namespace)
}

// downloadFile downloads a file from URL
func (c *RegistryClient) downloadFile(ctx context.Context, url string, dest *os.File) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	
	if c.githubToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("token %s", c.githubToken))
	}
	
	req.Header.Set("User-Agent", c.userAgent)
	
	resp, err := c.doRequestWithRetry(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	
	_, err = io.Copy(dest, resp.Body)
	return err
}

// verifyChecksum verifies file checksum
func (c *RegistryClient) verifyChecksum(filePath, expectedChecksum string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	
	actualChecksum := hex.EncodeToString(hash.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}
	
	return nil
}

// extractTarball extracts a gzipped tarball
func (c *RegistryClient) extractTarball(tarPath, destDir string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	
	file, err := os.Open(tarPath)
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
		
		// Skip root directory (GitHub tarballs have a root directory)
		pathParts := strings.Split(header.Name, "/")
		if len(pathParts) <= 1 {
			continue
		}
		
		// Remove the root directory from path
		relativePath := strings.Join(pathParts[1:], "/")
		if relativePath == "" {
			continue
		}
		
		targetPath := filepath.Join(destDir, relativePath)
		
		// Ensure we don't extract outside destination
		if !strings.HasPrefix(targetPath, destDir) {
			continue
		}
		
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}
	
	return nil
}

// doRequestWithRetry performs HTTP request with retry logic
func (c *RegistryClient) doRequestWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	delay := c.retryConfig.RetryDelay
	
	for attempt := 0; attempt <= c.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * c.retryConfig.Backoff)
		}
		
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		
		// Check for retryable status codes
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
			continue
		}
		
		return resp, nil
	}
	
	return nil, lastErr
}

// loadCache loads the registry cache from disk
func (c *RegistryClient) loadCache() error {
	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Cache doesn't exist, initialize empty cache
			c.registryCache = &RegistryCache{
				TTL:        24 * time.Hour,
				Registries: make(map[string]Registry),
				Packs:      make(map[string]PackInfo),
				Version:    "1.0",
			}
			return nil
		}
		return err
	}
	
	var cache RegistryCache
	if err := json.Unmarshal(data, &cache); err != nil {
		// If cache is corrupted, reinitialize
		c.registryCache = &RegistryCache{
			TTL:        24 * time.Hour,
			Registries: make(map[string]Registry),
			Packs:      make(map[string]PackInfo),
			Version:    "1.0",
		}
		return nil
	}
	
	c.registryCache = &cache
	return nil
}

// saveCache saves the registry cache to disk
func (c *RegistryClient) saveCache() error {
	data, err := json.MarshalIndent(c.registryCache, "", "  ")
	if err != nil {
		return err
	}
	
	// Write to temporary file first, then rename for atomic operation
	tempPath := c.cachePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}
	
	return os.Rename(tempPath, c.cachePath)
}

// GetCacheInfo returns information about the registry cache
func (c *RegistryClient) GetCacheInfo() (map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if c.registryCache == nil {
		return map[string]interface{}{
			"status": "empty",
		}, nil
	}
	
	return map[string]interface{}{
		"last_updated": c.registryCache.LastUpdated,
		"ttl":          c.registryCache.TTL,
		"version":      c.registryCache.Version,
		"pack_count":   len(c.registryCache.Packs),
		"registry_count": len(c.registryCache.Registries),
		"cache_path":   c.cachePath,
		"offline_mode": c.offlineMode,
	}, nil
}

// ClearCache clears the registry cache
func (c *RegistryClient) ClearCache() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.registryCache = &RegistryCache{
		TTL:        24 * time.Hour,
		Registries: make(map[string]Registry),
		Packs:      make(map[string]PackInfo),
		Version:    "1.0",
	}
	
	return c.saveCache()
}

// Helper functions

func (c *RegistryClient) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (c *RegistryClient) containsAny(slice []string, query string) bool {
	for _, s := range slice {
		if strings.Contains(strings.ToLower(s), query) {
			return true
		}
	}
	return false
}
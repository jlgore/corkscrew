package compliance

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed packs/*
var defaultPacks embed.FS

// PackLoader manages discovery, loading, and caching of query packs
type PackLoader struct {
	mu                 sync.RWMutex
	registry           *PackRegistry
	searchPaths        []string
	cache              map[string]*CachedPack
	dependencyGraph    map[string][]string // pack -> dependencies
	loadedPacks        map[string]*QueryPack
	manifestCache      map[string]*PackManifest
	sqlCache           map[string]string // file path -> SQL content
	enableEmbeddedPacks bool
	cacheExpiry        time.Duration
	maxRetries         int
	retryDelay         time.Duration
}

// CachedPack represents a cached pack with metadata
type CachedPack struct {
	Pack       *QueryPack
	LoadedAt   time.Time
	ModTime    time.Time
	FileHash   string
	FilePath   string
	Dependencies []string
}

// PackManifest represents the manifest.yaml file in a pack directory
type PackManifest struct {
	APIVersion   string                 `yaml:"apiVersion"`
	Kind         string                 `yaml:"kind"`
	Metadata     PackMetadata           `yaml:"metadata"`
	Spec         PackSpec               `yaml:"spec"`
	LoadedFrom   string                 `yaml:"-"`
	LoadedAt     time.Time              `yaml:"-"`
}

// PackSpec defines the specification for a query pack
type PackSpec struct {
	Queries     []QuerySpec     `yaml:"queries"`
	Parameters  []PackParameter `yaml:"parameters,omitempty"`
	Includes    []string        `yaml:"includes,omitempty"`
	DependsOn   []PackDependency `yaml:"depends_on,omitempty"`
	Resources   []string        `yaml:"resources,omitempty"`
}

// QuerySpec defines a query specification in the manifest
type QuerySpec struct {
	ID              string               `yaml:"id"`
	Title           string               `yaml:"title"`
	Description     string               `yaml:"description"`
	Objective       string               `yaml:"objective,omitempty"`
	Severity        string               `yaml:"severity"`
	Category        string               `yaml:"category"`
	ControlFamily   string               `yaml:"control_family,omitempty"`
	NISTCSF         string               `yaml:"nist_csf,omitempty"`
	Tags            []string             `yaml:"tags,omitempty"`
	QueryFile       string               `yaml:"query_file"`
	Parameters      []string             `yaml:"parameters,omitempty"`
	DependsOn       []string             `yaml:"depends_on,omitempty"`
	Threats         []string             `yaml:"threats,omitempty"`
	ControlMappings ControlMappings      `yaml:"control_mappings,omitempty"`
	TestRequirements []TestRequirement   `yaml:"test_requirements,omitempty"`
	Remediation     RemediationInfo      `yaml:"remediation,omitempty"`
	References      []Reference          `yaml:"references,omitempty"`
	Enabled         bool                 `yaml:"enabled"`
}

// PackDependency represents a dependency on another pack
type PackDependency struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace,omitempty"`
	Version   string `yaml:"version"`
	Required  bool   `yaml:"required"`
}

// LoaderError represents an error during pack loading
type LoaderError struct {
	PackName string
	Path     string
	Message  string
	Cause    error
}

func (e LoaderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("failed to load pack '%s' from '%s': %s (caused by: %v)", e.PackName, e.Path, e.Message, e.Cause)
	}
	return fmt.Sprintf("failed to load pack '%s' from '%s': %s", e.PackName, e.Path, e.Message)
}

func (e LoaderError) Unwrap() error {
	return e.Cause
}

// DependencyError represents a dependency resolution error
type DependencyError struct {
	PackName     string
	Dependencies []string
	Missing      []string
	Circular     []string
}

func (e DependencyError) Error() string {
	var parts []string
	if len(e.Missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing dependencies: %v", e.Missing))
	}
	if len(e.Circular) > 0 {
		parts = append(parts, fmt.Sprintf("circular dependencies: %v", e.Circular))
	}
	return fmt.Sprintf("dependency resolution failed for pack '%s': %s", e.PackName, strings.Join(parts, "; "))
}

// NewPackLoader creates a new pack loader with default configuration
func NewPackLoader() *PackLoader {
	homeDir, _ := os.UserHomeDir()
	
	return &PackLoader{
		registry:           NewPackRegistry(),
		searchPaths: []string{
			filepath.Join(homeDir, ".corkscrew", "query-packs"),
			"/etc/corkscrew/query-packs",
			"./query-packs",
		},
		cache:              make(map[string]*CachedPack),
		dependencyGraph:    make(map[string][]string),
		loadedPacks:        make(map[string]*QueryPack),
		manifestCache:      make(map[string]*PackManifest),
		sqlCache:           make(map[string]string),
		enableEmbeddedPacks: true,
		cacheExpiry:        30 * time.Minute,
		maxRetries:         3,
		retryDelay:         time.Second,
	}
}

// WithSearchPaths sets custom search paths for pack discovery
func (l *PackLoader) WithSearchPaths(paths ...string) *PackLoader {
	l.searchPaths = paths
	return l
}

// WithCacheExpiry sets the cache expiry duration
func (l *PackLoader) WithCacheExpiry(duration time.Duration) *PackLoader {
	l.cacheExpiry = duration
	return l
}

// WithEmbeddedPacks enables or disables embedded pack loading
func (l *PackLoader) WithEmbeddedPacks(enabled bool) *PackLoader {
	l.enableEmbeddedPacks = enabled
	return l
}

// DiscoverPacks discovers all available query packs in the search paths
func (l *PackLoader) DiscoverPacks(ctx context.Context) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var allPacks []string
	discovered := make(map[string]bool)

	// Discover from filesystem paths
	for _, searchPath := range l.searchPaths {
		packs, err := l.discoverFromPath(ctx, searchPath)
		if err != nil {
			// Log error but continue with other paths
			continue
		}
		
		for _, pack := range packs {
			if !discovered[pack] {
				allPacks = append(allPacks, pack)
				discovered[pack] = true
			}
		}
	}

	// Discover from embedded packs
	if l.enableEmbeddedPacks {
		packs, err := l.discoverFromEmbedded(ctx)
		if err == nil {
			for _, pack := range packs {
				if !discovered[pack] {
					allPacks = append(allPacks, pack)
					discovered[pack] = true
				}
			}
		}
	}

	sort.Strings(allPacks)
	return allPacks, nil
}

// LoadPack loads a query pack by namespace and name, resolving dependencies
func (l *PackLoader) LoadPack(ctx context.Context, namespace string) (*QueryPack, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.loadPackInternal(ctx, namespace, make(map[string]bool), make([]string, 0))
}

// loadPackInternal loads a pack with dependency tracking to prevent circular dependencies
func (l *PackLoader) loadPackInternal(ctx context.Context, namespace string, loading map[string]bool, loadPath []string) (*QueryPack, error) {
	// Check for circular dependencies
	if loading[namespace] {
		return nil, DependencyError{
			PackName: namespace,
			Circular: append(loadPath, namespace),
		}
	}

	// Check if already loaded
	if pack, exists := l.loadedPacks[namespace]; exists {
		if l.isCacheValid(namespace) {
			return pack, nil
		}
	}

	loading[namespace] = true
	loadPath = append(loadPath, namespace)
	defer func() {
		delete(loading, namespace)
	}()

	// Find the pack manifest
	manifest, manifestPath, err := l.findPackManifest(ctx, namespace)
	if err != nil {
		return nil, LoaderError{
			PackName: namespace,
			Message:  "manifest not found",
			Cause:    err,
		}
	}

	// Load dependencies first
	var loadedDependencies []*QueryPack
	for _, dep := range manifest.Spec.DependsOn {
		depNamespace := dep.Namespace
		if depNamespace == "" {
			depNamespace = dep.Name
		}

		depPack, err := l.loadPackInternal(ctx, depNamespace, loading, loadPath)
		if err != nil {
			if dep.Required {
				return nil, LoaderError{
					PackName: namespace,
					Message:  fmt.Sprintf("failed to load required dependency '%s'", depNamespace),
					Cause:    err,
				}
			}
			// Optional dependency failed - log but continue
			continue
		}
		loadedDependencies = append(loadedDependencies, depPack)
	}

	// Convert manifest to QueryPack
	pack, err := l.manifestToPack(ctx, manifest, manifestPath)
	if err != nil {
		return nil, LoaderError{
			PackName: namespace,
			Path:     manifestPath,
			Message:  "failed to convert manifest to pack",
			Cause:    err,
		}
	}

	// Validate the pack
	if err := l.registry.ValidatePack(pack); err != nil {
		return nil, LoaderError{
			PackName: namespace,
			Path:     manifestPath,
			Message:  "pack validation failed",
			Cause:    err,
		}
	}

	// Cache the pack
	l.cachePack(namespace, pack, manifestPath, loadedDependencies)

	return pack, nil
}

// findPackManifest finds and loads a pack manifest by namespace
func (l *PackLoader) findPackManifest(ctx context.Context, namespace string) (*PackManifest, string, error) {
	// Check manifest cache first
	if manifest, exists := l.manifestCache[namespace]; exists {
		return manifest, manifest.LoadedFrom, nil
	}

	// Search in filesystem paths
	for _, searchPath := range l.searchPaths {
		manifestPath := l.getManifestPath(searchPath, namespace)
		if manifest, err := l.loadManifestFromFile(manifestPath); err == nil {
			l.manifestCache[namespace] = manifest
			return manifest, manifestPath, nil
		}
	}

	// Search in embedded packs
	if l.enableEmbeddedPacks {
		embeddedPath := fmt.Sprintf("packs/%s/manifest.yaml", strings.ReplaceAll(namespace, "/", "/"))
		if manifest, err := l.loadManifestFromEmbedded(embeddedPath); err == nil {
			manifest.LoadedFrom = embeddedPath
			l.manifestCache[namespace] = manifest
			return manifest, embeddedPath, nil
		}
	}

	return nil, "", fmt.Errorf("manifest not found for namespace '%s'", namespace)
}

// getManifestPath constructs the expected manifest path for a namespace
func (l *PackLoader) getManifestPath(searchPath, namespace string) string {
	parts := strings.Split(namespace, "/")
	pathParts := append([]string{searchPath}, parts...)
	pathParts = append(pathParts, "manifest.yaml")
	return filepath.Join(pathParts...)
}

// loadManifestFromFile loads a manifest from a file path
func (l *PackLoader) loadManifestFromFile(path string) (*PackManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var manifest PackManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	manifest.LoadedFrom = path
	manifest.LoadedAt = time.Now()

	return &manifest, nil
}

// loadManifestFromEmbedded loads a manifest from embedded files
func (l *PackLoader) loadManifestFromEmbedded(path string) (*PackManifest, error) {
	data, err := defaultPacks.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var manifest PackManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse embedded manifest YAML: %w", err)
	}

	manifest.LoadedFrom = path
	manifest.LoadedAt = time.Now()

	return &manifest, nil
}

// manifestToPack converts a PackManifest to a QueryPack
func (l *PackLoader) manifestToPack(ctx context.Context, manifest *PackManifest, manifestPath string) (*QueryPack, error) {
	pack := &QueryPack{
		Metadata:   manifest.Metadata,
		Parameters: manifest.Spec.Parameters,
		Includes:   manifest.Spec.Includes,
		LoadedFrom: manifestPath,
		LoadedAt:   time.Now(),
	}

	// Set namespace from metadata or derive from path
	if manifest.Metadata.Namespace != "" {
		pack.Namespace = manifest.Metadata.Namespace
	}

	// Convert dependencies
	for _, dep := range manifest.Spec.DependsOn {
		depName := dep.Name
		if dep.Namespace != "" {
			depName = fmt.Sprintf("%s/%s", dep.Namespace, dep.Name)
		}
		pack.DependsOn = append(pack.DependsOn, depName)
	}

	// Convert queries and load SQL files
	baseDir := filepath.Dir(manifestPath)
	for _, querySpec := range manifest.Spec.Queries {
		query := ComplianceQuery{
			ID:              querySpec.ID,
			Title:           querySpec.Title,
			Description:     querySpec.Description,
			Objective:       querySpec.Objective,
			Severity:        querySpec.Severity,
			Category:        querySpec.Category,
			ControlFamily:   querySpec.ControlFamily,
			NISTCSF:         querySpec.NISTCSF,
			Tags:            querySpec.Tags,
			QueryFile:       querySpec.QueryFile,
			Parameters:      querySpec.Parameters,
			DependsOn:       querySpec.DependsOn,
			Threats:         querySpec.Threats,
			ControlMappings: querySpec.ControlMappings,
			TestRequirements: querySpec.TestRequirements,
			Remediation:     querySpec.Remediation,
			References:      querySpec.References,
			Enabled:         querySpec.Enabled,
		}

		// Load SQL content
		if querySpec.QueryFile != "" {
			sql, err := l.loadSQLFile(ctx, baseDir, querySpec.QueryFile, manifestPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load SQL file '%s' for query '%s': %w", querySpec.QueryFile, querySpec.ID, err)
			}
			query.SQL = sql
		}

		pack.Queries = append(pack.Queries, query)
	}

	return pack, nil
}

// loadSQLFile loads SQL content from a file with caching
func (l *PackLoader) loadSQLFile(ctx context.Context, baseDir, queryFile, manifestPath string) (string, error) {
	var sqlPath string
	var isEmbedded bool

	// Determine if this is from embedded files
	if strings.HasPrefix(manifestPath, "packs/") {
		sqlPath = filepath.Join(filepath.Dir(manifestPath), queryFile)
		isEmbedded = true
	} else {
		sqlPath = filepath.Join(baseDir, queryFile)
		isEmbedded = false
	}

	// Check cache first
	if sql, exists := l.sqlCache[sqlPath]; exists {
		return sql, nil
	}

	var data []byte
	var err error

	if isEmbedded {
		data, err = defaultPacks.ReadFile(sqlPath)
	} else {
		data, err = os.ReadFile(sqlPath)
	}

	if err != nil {
		return "", err
	}

	sql := string(data)
	l.sqlCache[sqlPath] = sql
	return sql, nil
}

// discoverFromPath discovers packs in a filesystem path
func (l *PackLoader) discoverFromPath(ctx context.Context, searchPath string) ([]string, error) {
	if _, err := os.Stat(searchPath); os.IsNotExist(err) {
		return nil, nil // Path doesn't exist, return empty list
	}

	var packs []string

	err := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip directories with errors
		}

		if d.IsDir() || d.Name() != "manifest.yaml" {
			return nil
		}

		// Calculate namespace from path
		relPath, err := filepath.Rel(searchPath, filepath.Dir(path))
		if err != nil {
			return nil
		}

		namespace := strings.ReplaceAll(relPath, string(filepath.Separator), "/")
		if namespace != "." {
			packs = append(packs, namespace)
		}

		return nil
	})

	return packs, err
}

// discoverFromEmbedded discovers packs from embedded files
func (l *PackLoader) discoverFromEmbedded(ctx context.Context) ([]string, error) {
	var packs []string

	err := fs.WalkDir(defaultPacks, "packs", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() || d.Name() != "manifest.yaml" {
			return nil
		}

		// Calculate namespace from embedded path
		relPath := strings.TrimPrefix(filepath.Dir(path), "packs/")
		if relPath != "" && relPath != "." {
			namespace := strings.ReplaceAll(relPath, "/", "/")
			packs = append(packs, namespace)
		}

		return nil
	})

	return packs, err
}

// cachePack caches a loaded pack with metadata
func (l *PackLoader) cachePack(namespace string, pack *QueryPack, manifestPath string, dependencies []*QueryPack) {
	var fileHash string
	var modTime time.Time

	// Calculate file hash and mod time for filesystem paths
	if !strings.HasPrefix(manifestPath, "packs/") {
		if info, err := os.Stat(manifestPath); err == nil {
			modTime = info.ModTime()
			if data, err := os.ReadFile(manifestPath); err == nil {
				hash := sha256.Sum256(data)
				fileHash = fmt.Sprintf("%x", hash)
			}
		}
	}

	// Extract dependency names
	var depNames []string
	for _, dep := range dependencies {
		depNames = append(depNames, dep.Namespace)
	}

	cached := &CachedPack{
		Pack:         pack,
		LoadedAt:     time.Now(),
		ModTime:      modTime,
		FileHash:     fileHash,
		FilePath:     manifestPath,
		Dependencies: depNames,
	}

	l.cache[namespace] = cached
	l.loadedPacks[namespace] = pack

	// Update dependency graph
	l.dependencyGraph[namespace] = depNames
}

// isCacheValid checks if a cached pack is still valid
func (l *PackLoader) isCacheValid(namespace string) bool {
	cached, exists := l.cache[namespace]
	if !exists {
		return false
	}

	// Check expiry
	if time.Since(cached.LoadedAt) > l.cacheExpiry {
		return false
	}

	// Check file modification time for filesystem paths
	if !strings.HasPrefix(cached.FilePath, "packs/") {
		if info, err := os.Stat(cached.FilePath); err == nil {
			if !info.ModTime().Equal(cached.ModTime) {
				return false
			}
			
			// Also check file hash for more accurate change detection
			if data, err := os.ReadFile(cached.FilePath); err == nil {
				hash := sha256.Sum256(data)
				newHash := fmt.Sprintf("%x", hash)
				if newHash != cached.FileHash {
					return false
				}
			}
		}
	}

	return true
}

// InvalidateCache invalidates the cache for a specific pack or all packs
func (l *PackLoader) InvalidateCache(namespace string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if namespace == "" {
		// Clear all caches
		l.cache = make(map[string]*CachedPack)
		l.loadedPacks = make(map[string]*QueryPack)
		l.manifestCache = make(map[string]*PackManifest)
		l.sqlCache = make(map[string]string)
		l.dependencyGraph = make(map[string][]string)
	} else {
		// Clear specific pack
		delete(l.cache, namespace)
		delete(l.loadedPacks, namespace)
		delete(l.manifestCache, namespace)
		delete(l.dependencyGraph, namespace)
		
		// Clear SQL cache entries for this pack
		for path := range l.sqlCache {
			if strings.Contains(path, namespace) {
				delete(l.sqlCache, path)
			}
		}
	}
}

// GetLoadedPacks returns all currently loaded packs
func (l *PackLoader) GetLoadedPacks() map[string]*QueryPack {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make(map[string]*QueryPack)
	for k, v := range l.loadedPacks {
		result[k] = v
	}
	return result
}

// GetDependencyGraph returns the dependency graph for loaded packs
func (l *PackLoader) GetDependencyGraph() map[string][]string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make(map[string][]string)
	for k, v := range l.dependencyGraph {
		deps := make([]string, len(v))
		copy(deps, v)
		result[k] = deps
	}
	return result
}

// ValidateManifest validates a pack manifest against the schema
func (l *PackLoader) ValidateManifest(manifest *PackManifest) error {
	if manifest.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}
	if manifest.Kind != "QueryPack" {
		return fmt.Errorf("kind must be 'QueryPack'")
	}
	if manifest.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if manifest.Metadata.Version == "" {
		return fmt.Errorf("metadata.version is required")
	}
	if manifest.Metadata.Provider == "" {
		return fmt.Errorf("metadata.provider is required")
	}

	// Validate version format
	if _, err := ParseVersion(manifest.Metadata.Version); err != nil {
		return fmt.Errorf("invalid version format: %w", err)
	}

	// Validate queries
	queryIDs := make(map[string]bool)
	for i, query := range manifest.Spec.Queries {
		if query.ID == "" {
			return fmt.Errorf("spec.queries[%d].id is required", i)
		}
		if queryIDs[query.ID] {
			return fmt.Errorf("spec.queries[%d].id '%s' is duplicate", i, query.ID)
		}
		queryIDs[query.ID] = true

		if query.Title == "" {
			return fmt.Errorf("spec.queries[%d].title is required", i)
		}
		if query.QueryFile == "" {
			return fmt.Errorf("spec.queries[%d].query_file is required", i)
		}
		if !isValidSeverity(query.Severity) {
			return fmt.Errorf("spec.queries[%d].severity '%s' is invalid", i, query.Severity)
		}
	}

	return nil
}
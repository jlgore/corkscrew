package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// PersistenceFormat represents the structure for persisting the registry
type PersistenceFormat struct {
	Version      string                       `json:"version"`
	Generated    time.Time                    `json:"generated"`
	Services     map[string]ServiceDefinition `json:"services"`
	Stats        RegistryStats                `json:"stats"`
	AuditLog     []auditEntry                 `json:"auditLog,omitempty"`
	Metadata     map[string]interface{}       `json:"metadata,omitempty"`
}

// ServiceDefinitionJSON is a JSON-serializable version of ServiceDefinition
type ServiceDefinitionJSON struct {
	ServiceDefinition
	RateLimitFloat float64 `json:"rateLimitFloat"` // For JSON serialization of rate.Limit
}

// PersistToFile saves the registry to a JSON file
func (r *serviceRegistry) PersistToFile(path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Prepare persistence format
	format := PersistenceFormat{
		Version:   "1.0",
		Generated: time.Now(),
		Services:  make(map[string]ServiceDefinition),
		Stats:     r.stats,
		Metadata: map[string]interface{}{
			"totalServices": len(r.services),
			"lastUpdated":   r.stats.LastUpdated,
		},
	}

	// Copy services (already locked)
	for name, service := range r.services {
		format.Services[name] = *service
	}

	// Include audit log if enabled
	if r.config.EnableAuditLog {
		format.AuditLog = r.auditLog
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(format, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	// Write to temporary file first
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("failed to rename file: %w", err)
	}

	r.lastPersisted = time.Now()
	return nil
}

// LoadFromFile loads the registry from a JSON file
func (r *serviceRegistry) LoadFromFile(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", path)
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Unmarshal JSON
	var format PersistenceFormat
	if err := json.Unmarshal(data, &format); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Validate version
	if format.Version != "1.0" {
		return fmt.Errorf("unsupported format version: %s", format.Version)
	}

	// Clear existing services
	r.services = make(map[string]*ServiceDefinition)

	// Load services
	for name, service := range format.Services {
		serviceCopy := service
		
		// Ensure rate limits are set
		if serviceCopy.RateLimit == 0 {
			serviceCopy.RateLimit = rate.Limit(10)
		}
		if serviceCopy.BurstLimit == 0 {
			serviceCopy.BurstLimit = 20
		}
		
		r.services[name] = &serviceCopy
	}

	// Update stats
	r.stats = format.Stats
	r.stats.LastUpdated = time.Now()

	// Load audit log if present
	if format.AuditLog != nil && r.config.EnableAuditLog {
		r.auditLog = format.AuditLog
	}

	// Clear cache
	r.cache = make(map[string]*cacheEntry)

	return nil
}

// LoadFromDirectory loads multiple registry files from a directory
func (r *serviceRegistry) LoadFromDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var loadErrors []error
	loadedCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process JSON files
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		
		// Create temporary registry to load file
		tempRegistry := &serviceRegistry{
			services: make(map[string]*ServiceDefinition),
			config:   r.config,
		}

		if err := tempRegistry.LoadFromFile(path); err != nil {
			loadErrors = append(loadErrors, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}

		// Merge services from temp registry
		for name, service := range tempRegistry.services {
			if err := r.RegisterService(*service); err != nil {
				loadErrors = append(loadErrors, fmt.Errorf("failed to register %s from %s: %w", name, entry.Name(), err))
			} else {
				loadedCount++
			}
		}
	}

	if len(loadErrors) > 0 {
		return fmt.Errorf("loaded %d services with %d errors: %v", loadedCount, len(loadErrors), loadErrors)
	}

	return nil
}

// ExportServices exports specific services to a file
func (r *serviceRegistry) ExportServices(services []string, path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Create export format
	format := PersistenceFormat{
		Version:   "1.0",
		Generated: time.Now(),
		Services:  make(map[string]ServiceDefinition),
		Metadata: map[string]interface{}{
			"exportedServices": services,
			"exportedAt":       time.Now(),
			"totalExported":    0,
		},
	}

	// Export requested services
	exported := 0
	var notFound []string

	for _, name := range services {
		name = strings.ToLower(name)
		if service, exists := r.services[name]; exists {
			format.Services[name] = *service
			exported++
		} else {
			notFound = append(notFound, name)
		}
	}

	format.Metadata["totalExported"] = exported
	if len(notFound) > 0 {
		format.Metadata["notFound"] = notFound
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(format, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal services: %w", err)
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if len(notFound) > 0 {
		return fmt.Errorf("exported %d services, not found: %v", exported, notFound)
	}

	return nil
}

// BackupRegistry creates a timestamped backup of the current registry
func BackupRegistry(registry DynamicServiceRegistry, backupDir string) (string, error) {
	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Generate backup filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("registry-backup-%s.json", timestamp)
	backupPath := filepath.Join(backupDir, filename)

	// Persist to backup file
	if err := registry.PersistToFile(backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	return backupPath, nil
}

// MergeRegistryFiles merges multiple registry files into one
func MergeRegistryFiles(files []string, outputPath string) error {
	mergedRegistry := &serviceRegistry{
		services: make(map[string]*ServiceDefinition),
		config: RegistryConfig{
			EnableValidation: true,
		},
	}

	// Load and merge each file
	for _, file := range files {
		tempRegistry := &serviceRegistry{
			services: make(map[string]*ServiceDefinition),
		}

		if err := tempRegistry.LoadFromFile(file); err != nil {
			return fmt.Errorf("failed to load %s: %w", file, err)
		}

		// Merge services
		for name, service := range tempRegistry.services {
			if existing, exists := mergedRegistry.services[name]; exists {
				// Merge with existing
				merged := mergeServiceDefinitions(existing, service)
				mergedRegistry.services[name] = merged
			} else {
				// Add new service
				mergedRegistry.services[name] = service
			}
		}
	}

	// Save merged registry
	return mergedRegistry.PersistToFile(outputPath)
}

// ValidateRegistryFile validates a registry file without loading it
func ValidateRegistryFile(path string) error {
	// Check file exists
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	// Check file size
	if info.Size() == 0 {
		return fmt.Errorf("file is empty")
	}

	// Read file
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Validate JSON structure
	decoder := json.NewDecoder(file)
	var format PersistenceFormat
	if err := decoder.Decode(&format); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Validate version
	if format.Version == "" {
		return fmt.Errorf("missing version field")
	}
	if format.Version != "1.0" {
		return fmt.Errorf("unsupported version: %s", format.Version)
	}

	// Validate services
	if len(format.Services) == 0 {
		return fmt.Errorf("no services found")
	}

	// Validate each service
	for name, service := range format.Services {
		if service.Name == "" {
			return fmt.Errorf("service at key %s has no name", name)
		}
		if service.Name != name {
			return fmt.Errorf("service name mismatch: key=%s, name=%s", name, service.Name)
		}
		if service.PackagePath == "" && service.DiscoverySource == "manual" {
			return fmt.Errorf("service %s missing package path", name)
		}
	}

	return nil
}

// CleanupBackups removes old backup files based on retention policy
func CleanupBackups(backupDir string, keepDays int) error {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	cutoffTime := time.Now().AddDate(0, 0, -keepDays)
	var removed int
	var errors []error

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Check if it's a backup file
		if !strings.HasPrefix(entry.Name(), "registry-backup-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to get info for %s: %w", entry.Name(), err))
			continue
		}

		// Remove if older than cutoff
		if info.ModTime().Before(cutoffTime) {
			path := filepath.Join(backupDir, entry.Name())
			if err := os.Remove(path); err != nil {
				errors = append(errors, fmt.Errorf("failed to remove %s: %w", entry.Name(), err))
			} else {
				removed++
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("removed %d backups with %d errors: %v", removed, len(errors), errors)
	}

	return nil
}

// Helper function to merge two service definitions
func mergeServiceDefinitions(existing, new *ServiceDefinition) *ServiceDefinition {
	merged := *existing

	// Update basic fields if newer
	if new.DiscoveredAt.After(existing.DiscoveredAt) {
		merged.DisplayName = new.DisplayName
		merged.Description = new.Description
		merged.PackagePath = new.PackagePath
		merged.ClientType = new.ClientType
	}

	// Merge resource types
	resourceMap := make(map[string]ResourceTypeDefinition)
	for _, rt := range existing.ResourceTypes {
		resourceMap[rt.Name] = rt
	}
	for _, rt := range new.ResourceTypes {
		if _, exists := resourceMap[rt.Name]; !exists {
			merged.ResourceTypes = append(merged.ResourceTypes, rt)
		}
	}

	// Merge operations
	opMap := make(map[string]OperationDefinition)
	for _, op := range existing.Operations {
		opMap[op.Name] = op
	}
	for _, op := range new.Operations {
		if _, exists := opMap[op.Name]; !exists {
			merged.Operations = append(merged.Operations, op)
		}
	}

	// Merge permissions
	permMap := make(map[string]bool)
	for _, perm := range existing.Permissions {
		permMap[perm] = true
	}
	for _, perm := range new.Permissions {
		if !permMap[perm] {
			merged.Permissions = append(merged.Permissions, perm)
		}
	}

	// Update metadata
	merged.LastValidated = time.Now()
	if new.RateLimit > existing.RateLimit {
		merged.RateLimit = new.RateLimit
	}
	if new.BurstLimit > existing.BurstLimit {
		merged.BurstLimit = new.BurstLimit
	}

	return &merged
}

// Custom JSON marshaling for rate.Limit
func (s ServiceDefinition) MarshalJSON() ([]byte, error) {
	type Alias ServiceDefinition
	return json.Marshal(&struct {
		RateLimitFloat float64 `json:"rateLimit"`
		*Alias
	}{
		RateLimitFloat: float64(s.RateLimit),
		Alias:          (*Alias)(&s),
	})
}

// Custom JSON unmarshaling for rate.Limit
func (s *ServiceDefinition) UnmarshalJSON(data []byte) error {
	type Alias ServiceDefinition
	aux := &struct {
		RateLimitFloat float64 `json:"rateLimit"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	s.RateLimit = rate.Limit(aux.RateLimitFloat)
	return nil
}

// StreamWriter provides streaming JSON writing for large registries
type StreamWriter struct {
	writer io.Writer
	encoder *json.Encoder
	started bool
}

// NewStreamWriter creates a new streaming writer
func NewStreamWriter(w io.Writer) *StreamWriter {
	return &StreamWriter{
		writer: w,
		encoder: json.NewEncoder(w),
	}
}

// WriteHeader writes the JSON header
func (sw *StreamWriter) WriteHeader(version string) error {
	if sw.started {
		return fmt.Errorf("stream already started")
	}
	
	header := map[string]interface{}{
		"version": version,
		"generated": time.Now(),
		"stream": true,
	}
	
	if _, err := sw.writer.Write([]byte(`{"header":`)); err != nil {
		return err
	}
	
	if err := sw.encoder.Encode(header); err != nil {
		return err
	}
	
	if _, err := sw.writer.Write([]byte(`,"services":[`)); err != nil {
		return err
	}
	
	sw.started = true
	return nil
}

// WriteService writes a single service definition
func (sw *StreamWriter) WriteService(service ServiceDefinition, isLast bool) error {
	if !sw.started {
		return fmt.Errorf("stream not started")
	}
	
	if err := sw.encoder.Encode(service); err != nil {
		return err
	}
	
	if !isLast {
		if _, err := sw.writer.Write([]byte(",")); err != nil {
			return err
		}
	}
	
	return nil
}

// Close closes the stream
func (sw *StreamWriter) Close() error {
	if !sw.started {
		return fmt.Errorf("stream not started")
	}
	
	_, err := sw.writer.Write([]byte("]}"))
	return err
}
package registry

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Persistence operations for UnifiedServiceRegistry

// PersistToFile saves the unified registry to a JSON file
func (r *UnifiedServiceRegistry) PersistToFile(path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Prepare data for persistence
	persistData := &UnifiedRegistryPersistence{
		Version:     "1.0",
		SavedAt:     time.Now(),
		Services:    make([]ServiceDefinition, 0, len(r.services)),
		Config:      r.config,
		Stats:       r.stats,
		AuditLog:    r.auditLog,
	}

	// Copy all services
	for _, service := range r.services {
		persistData.Services = append(persistData.Services, *service)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(persistData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	// Write atomically (write to temp file, then rename)
	tempPath := path + ".tmp"
	if err := ioutil.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	// Update persistence timestamp
	r.lastPersisted = time.Now()

	return nil
}

// LoadFromFile loads the unified registry from a JSON file
func (r *UnifiedServiceRegistry) LoadFromFile(path string) error {
	// Read file
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Parse JSON
	var persistData UnifiedRegistryPersistence
	if err := json.Unmarshal(data, &persistData); err != nil {
		return fmt.Errorf("failed to unmarshal registry: %w", err)
	}

	// Validate version compatibility
	if persistData.Version != "1.0" {
		return fmt.Errorf("unsupported registry version: %s", persistData.Version)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Load services
	for _, service := range persistData.Services {
		r.services[service.Name] = &service
		
		// Recreate rate limiter
		r.createRateLimiter(service.Name, service.RateLimit, service.BurstLimit)
	}

	// Load config (merge with current config)
	if persistData.Config.EnableCache {
		r.config.EnableCache = true
		r.config.CacheTTL = persistData.Config.CacheTTL
	}

	// Load audit log
	r.auditLog = persistData.AuditLog

	// Update stats
	r.updateStats()

	fmt.Printf("Loaded %d services from %s\n", len(persistData.Services), path)
	return nil
}

// LoadFromDirectory loads registry data from multiple JSON files in a directory
func (r *UnifiedServiceRegistry) LoadFromDirectory(dir string) error {
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var errors []error
	loadedCount := 0

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		if err := r.LoadFromFile(filePath); err != nil {
			errors = append(errors, fmt.Errorf("failed to load %s: %w", entry.Name(), err))
		} else {
			loadedCount++
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("loaded %d files with %d errors: %v", loadedCount, len(errors), errors)
	}

	return nil
}

// ExportServices exports specific services to a file
func (r *UnifiedServiceRegistry) ExportServices(serviceNames []string, path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	exportData := &UnifiedRegistryPersistence{
		Version: "1.0",
		SavedAt: time.Now(),
		Services: make([]ServiceDefinition, 0, len(serviceNames)),
	}

	for _, name := range serviceNames {
		if service, exists := r.services[name]; exists {
			exportData.Services = append(exportData.Services, *service)
		}
	}

	data, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal export data: %w", err)
	}

	if err := ioutil.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	fmt.Printf("Exported %d services to %s\n", len(exportData.Services), path)
	return nil
}

// UnifiedRegistryPersistence represents the persisted form of the unified registry
type UnifiedRegistryPersistence struct {
	Version  string              `json:"version"`
	SavedAt  time.Time           `json:"savedAt"`
	Services []ServiceDefinition `json:"services"`
	Config   RegistryConfig      `json:"config"`
	Stats    RegistryStats       `json:"stats"`
	AuditLog []auditEntry        `json:"auditLog,omitempty"`
}

// Auto-persistence support

func (r *UnifiedServiceRegistry) startAutoPersistence() {
	if r.config.AutoPersist && r.config.PersistenceInterval > 0 {
		go func() {
			ticker := time.NewTicker(r.config.PersistenceInterval)
			defer ticker.Stop()

			for range ticker.C {
				if err := r.PersistToFile(r.persistencePath); err != nil {
					fmt.Printf("Auto-persistence failed: %v\n", err)
				}
			}
		}()
	}
}

// Backup and versioning support

// CreateBackup creates a timestamped backup of the current registry
func (r *UnifiedServiceRegistry) CreateBackup() error {
	if r.persistencePath == "" {
		return fmt.Errorf("no persistence path configured")
	}

	backupPath := fmt.Sprintf("%s.backup.%s", r.persistencePath, time.Now().Format("20060102-150405"))
	return r.PersistToFile(backupPath)
}

// ListBackups returns available backup files
func (r *UnifiedServiceRegistry) ListBackups() ([]string, error) {
	if r.persistencePath == "" {
		return nil, fmt.Errorf("no persistence path configured")
	}

	dir := filepath.Dir(r.persistencePath)
	base := filepath.Base(r.persistencePath)
	
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		name := entry.Name()
		if len(name) > len(base)+8 && name[:len(base)+8] == base+".backup" {
			backups = append(backups, filepath.Join(dir, name))
		}
	}

	return backups, nil
}

// RestoreFromBackup restores the registry from a backup file
func (r *UnifiedServiceRegistry) RestoreFromBackup(backupPath string) error {
	// Create backup of current state first
	if err := r.CreateBackup(); err != nil {
		fmt.Printf("Warning: failed to backup current state: %v\n", err)
	}

	// Clear current registry
	r.mu.Lock()
	r.services = make(map[string]*ServiceDefinition)
	r.rateLimiters = make(map[string]*rate.Limiter)
	r.clientCache = sync.Map{}
	r.cache = make(map[string]*cacheEntry)
	r.mu.Unlock()

	// Load from backup
	return r.LoadFromFile(backupPath)
}
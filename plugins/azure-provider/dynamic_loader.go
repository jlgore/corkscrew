package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// Scanner interface defines the contract for all generated scanners
type Scanner interface {
	ScanResources(ctx context.Context, credential interface{}, subscriptionID string, filters map[string]string) ([]*pb.Resource, error)
	GetSupportedServices() []string
	GetServiceName() string
	GetVersion() string
}

// DynamicScannerLoader manages dynamic loading and hot-reload of Azure service scanners
type DynamicScannerLoader struct {
	scanners     map[string]Scanner    // service name -> scanner instance
	plugins      map[string]*plugin.Plugin // service name -> plugin instance
	pluginPaths  map[string]string     // service name -> file path
	mu           sync.RWMutex
	watcher      *fsnotify.Watcher
	watchDir     string
	reloadChan   chan string          // for reload notifications
	stopChan     chan struct{}        // for shutdown
	registry     *ScannerRegistry     // registry for metadata
}

// ScannerRegistry maintains metadata about loaded scanners
type ScannerRegistry struct {
	services     map[string]ScannerMetadata
	mu           sync.RWMutex
}

// ScannerMetadata contains information about a loaded scanner
type ScannerMetadata struct {
	ServiceName   string    `json:"service_name"`
	Version       string    `json:"version"`
	FilePath      string    `json:"file_path"`
	LoadedAt      time.Time `json:"loaded_at"`
	LastReload    time.Time `json:"last_reload"`
	ReloadCount   int       `json:"reload_count"`
	ScanCount     int64     `json:"scan_count"`
	LastScanAt    time.Time `json:"last_scan_at"`
	IsActive      bool      `json:"is_active"`
	ErrorMessage  string    `json:"error_message,omitempty"`
}

// NewDynamicScannerLoader creates a new dynamic scanner loader
func NewDynamicScannerLoader(watchDir string) (*DynamicScannerLoader, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	loader := &DynamicScannerLoader{
		scanners:    make(map[string]Scanner),
		plugins:     make(map[string]*plugin.Plugin),
		pluginPaths: make(map[string]string),
		watcher:     watcher,
		watchDir:    watchDir,
		reloadChan:  make(chan string, 100),
		stopChan:    make(chan struct{}),
		registry: &ScannerRegistry{
			services: make(map[string]ScannerMetadata),
		},
	}

	// Start the reload handler goroutine
	go loader.handleReloads()

	return loader, nil
}

// LoadScanners loads all scanner plugins from the specified pattern
func (d *DynamicScannerLoader) LoadScanners(pattern string) error {
	log.Printf("Loading scanners from pattern: %s", pattern)
	
	// Find all .so files matching the pattern
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
	}

	if len(files) == 0 {
		log.Printf("No scanner files found matching pattern: %s", pattern)
		return nil
	}

	// Load each scanner
	var loadErrors []error
	successCount := 0

	for _, filePath := range files {
		if err := d.loadScannerFile(filePath); err != nil {
			log.Printf("Failed to load scanner %s: %v", filePath, err)
			loadErrors = append(loadErrors, fmt.Errorf("failed to load %s: %w", filePath, err))
		} else {
			successCount++
		}
	}

	log.Printf("Loaded %d scanners successfully, %d failed", successCount, len(loadErrors))

	// Return error if no scanners were loaded successfully
	if successCount == 0 && len(loadErrors) > 0 {
		return fmt.Errorf("failed to load any scanners: %v", loadErrors)
	}

	return nil
}

// loadScannerFile loads a single scanner plugin file
func (d *DynamicScannerLoader) loadScannerFile(filePath string) error {
	// Open the plugin
	p, err := plugin.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open plugin %s: %w", filePath, err)
	}

	// Look for the NewScanner function
	newScannerSymbol, err := p.Lookup("NewScanner")
	if err != nil {
		return fmt.Errorf("plugin %s does not export NewScanner function: %w", filePath, err)
	}

	// Cast to the expected function signature
	newScanner, ok := newScannerSymbol.(func() Scanner)
	if !ok {
		return fmt.Errorf("plugin %s NewScanner function has incorrect signature", filePath)
	}

	// Create the scanner instance
	scanner := newScanner()
	if scanner == nil {
		return fmt.Errorf("plugin %s NewScanner returned nil", filePath)
	}

	// Get service information
	serviceName := scanner.GetServiceName()
	if serviceName == "" {
		return fmt.Errorf("plugin %s scanner returned empty service name", filePath)
	}

	// Register the scanner
	d.mu.Lock()
	defer d.mu.Unlock()

	// Unload existing scanner if present
	if _, exists := d.scanners[serviceName]; exists {
		log.Printf("Replacing existing scanner for service: %s", serviceName)
		d.unloadScannerUnsafe(serviceName)
	}

	// Store the new scanner
	d.scanners[serviceName] = scanner
	d.plugins[serviceName] = p
	d.pluginPaths[serviceName] = filePath

	// Update registry
	d.registry.mu.Lock()
	metadata := ScannerMetadata{
		ServiceName: serviceName,
		Version:     scanner.GetVersion(),
		FilePath:    filePath,
		LoadedAt:    time.Now(),
		LastReload:  time.Now(),
		IsActive:    true,
	}
	
	if existing, exists := d.registry.services[serviceName]; exists {
		metadata.ReloadCount = existing.ReloadCount + 1
		metadata.ScanCount = existing.ScanCount
		metadata.LastScanAt = existing.LastScanAt
	}
	
	d.registry.services[serviceName] = metadata
	d.registry.mu.Unlock()

	log.Printf("Successfully loaded scanner for service: %s (version: %s)", serviceName, scanner.GetVersion())
	return nil
}

// WatchForUpdates starts watching for file changes in the specified directory
func (d *DynamicScannerLoader) WatchForUpdates(dir string) error {
	log.Printf("Starting file watcher for directory: %s", dir)
	
	err := d.watcher.Add(dir)
	if err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}

	// Start the watcher goroutine
	go d.watchFiles()

	return nil
}

// watchFiles handles file system events
func (d *DynamicScannerLoader) watchFiles() {
	for {
		select {
		case event, ok := <-d.watcher.Events:
			if !ok {
				log.Printf("File watcher closed")
				return
			}

			// Only handle .so files
			if !strings.HasSuffix(event.Name, ".so") {
				continue
			}

			// Handle write events (file updated) or create events (new file)
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				log.Printf("Detected change in scanner file: %s", event.Name)
				// Send reload signal with a small delay to ensure file write is complete
				go func(filename string) {
					time.Sleep(100 * time.Millisecond)
					select {
					case d.reloadChan <- filename:
					case <-d.stopChan:
					}
				}(event.Name)
			}

		case err, ok := <-d.watcher.Errors:
			if !ok {
				log.Printf("File watcher error channel closed")
				return
			}
			log.Printf("File watcher error: %v", err)

		case <-d.stopChan:
			log.Printf("File watcher stopping")
			return
		}
	}
}

// handleReloads processes reload requests
func (d *DynamicScannerLoader) handleReloads() {
	for {
		select {
		case filename := <-d.reloadChan:
			if err := d.ReloadScanner(filename); err != nil {
				log.Printf("Failed to reload scanner %s: %v", filename, err)
			}

		case <-d.stopChan:
			log.Printf("Reload handler stopping")
			return
		}
	}
}

// ReloadScanner reloads a specific scanner by filename
func (d *DynamicScannerLoader) ReloadScanner(filename string) error {
	log.Printf("Reloading scanner from file: %s", filename)

	// Find the service name for this file
	d.mu.RLock()
	var serviceName string
	for service, path := range d.pluginPaths {
		if path == filename {
			serviceName = service
			break
		}
	}
	d.mu.RUnlock()

	if serviceName == "" {
		// This might be a new scanner, try to load it
		return d.loadScannerFile(filename)
	}

	// Reload the existing scanner
	d.mu.Lock()
	defer d.mu.Unlock()

	// Unload the current scanner
	d.unloadScannerUnsafe(serviceName)

	// Load the new version
	if err := d.loadScannerFile(filename); err != nil {
		// Mark as inactive in registry
		d.registry.mu.Lock()
		if metadata, exists := d.registry.services[serviceName]; exists {
			metadata.IsActive = false
			metadata.ErrorMessage = err.Error()
			d.registry.services[serviceName] = metadata
		}
		d.registry.mu.Unlock()
		
		return fmt.Errorf("failed to reload scanner %s: %w", serviceName, err)
	}

	log.Printf("Successfully reloaded scanner for service: %s", serviceName)
	return nil
}

// unloadScannerUnsafe removes a scanner (must be called with d.mu locked)
func (d *DynamicScannerLoader) unloadScannerUnsafe(serviceName string) {
	// Remove from maps
	delete(d.scanners, serviceName)
	delete(d.plugins, serviceName)
	delete(d.pluginPaths, serviceName)

	// Note: We cannot actually unload Go plugins due to limitations in the plugin package
	// The plugin will remain in memory, but we remove our references to it
}

// GetScanner returns a scanner for the specified service
func (d *DynamicScannerLoader) GetScanner(serviceName string) (Scanner, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	scanner, exists := d.scanners[serviceName]
	if !exists {
		return nil, fmt.Errorf("no scanner found for service: %s", serviceName)
	}

	// Update scan statistics
	d.registry.mu.Lock()
	if metadata, exists := d.registry.services[serviceName]; exists {
		metadata.ScanCount++
		metadata.LastScanAt = time.Now()
		d.registry.services[serviceName] = metadata
	}
	d.registry.mu.Unlock()

	return scanner, nil
}

// GetLoadedServices returns a list of all loaded service names
func (d *DynamicScannerLoader) GetLoadedServices() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	services := make([]string, 0, len(d.scanners))
	for serviceName := range d.scanners {
		services = append(services, serviceName)
	}
	return services
}

// GetScannerMetadata returns metadata for a specific scanner
func (d *DynamicScannerLoader) GetScannerMetadata(serviceName string) (ScannerMetadata, error) {
	d.registry.mu.RLock()
	defer d.registry.mu.RUnlock()

	metadata, exists := d.registry.services[serviceName]
	if !exists {
		return ScannerMetadata{}, fmt.Errorf("no metadata found for service: %s", serviceName)
	}

	return metadata, nil
}

// GetAllScannerMetadata returns metadata for all loaded scanners
func (d *DynamicScannerLoader) GetAllScannerMetadata() map[string]ScannerMetadata {
	d.registry.mu.RLock()
	defer d.registry.mu.RUnlock()

	metadata := make(map[string]ScannerMetadata)
	for service, meta := range d.registry.services {
		metadata[service] = meta
	}
	return metadata
}

// ScanService uses the appropriate scanner to scan a specific service
func (d *DynamicScannerLoader) ScanService(ctx context.Context, serviceName string, credential interface{}, subscriptionID string, filters map[string]string) ([]*pb.Resource, error) {
	scanner, err := d.GetScanner(serviceName)
	if err != nil {
		return nil, err
	}

	return scanner.ScanResources(ctx, credential, subscriptionID, filters)
}

// ScanAllServices scans all loaded services
func (d *DynamicScannerLoader) ScanAllServices(ctx context.Context, credential interface{}, subscriptionID string, filters map[string]string) (map[string][]*pb.Resource, error) {
	services := d.GetLoadedServices()
	results := make(map[string][]*pb.Resource)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var errors []error

	// Limit concurrency to avoid overwhelming Azure APIs
	semaphore := make(chan struct{}, 5)

	for _, serviceName := range services {
		wg.Add(1)
		go func(service string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			resources, err := d.ScanService(ctx, service, credential, subscriptionID, filters)
			
			mu.Lock()
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to scan %s: %w", service, err))
			} else {
				results[service] = resources
				log.Printf("Scanned %d resources for service: %s", len(resources), service)
			}
			mu.Unlock()
		}(serviceName)
	}

	wg.Wait()

	if len(errors) > 0 {
		log.Printf("Scanning completed with %d errors out of %d services", len(errors), len(services))
		// Return partial results with error information
	}

	return results, nil
}

// RefreshScanners reloads all currently loaded scanners
func (d *DynamicScannerLoader) RefreshScanners() error {
	log.Printf("Refreshing all loaded scanners")
	
	d.mu.RLock()
	filePaths := make([]string, 0, len(d.pluginPaths))
	for _, path := range d.pluginPaths {
		filePaths = append(filePaths, path)
	}
	d.mu.RUnlock()

	var errors []error
	for _, filePath := range filePaths {
		if err := d.ReloadScanner(filePath); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to refresh some scanners: %v", errors)
	}

	log.Printf("Successfully refreshed %d scanners", len(filePaths))
	return nil
}

// Stop gracefully shuts down the dynamic loader
func (d *DynamicScannerLoader) Stop() error {
	log.Printf("Stopping dynamic scanner loader")
	
	// Signal all goroutines to stop
	close(d.stopChan)

	// Close the file watcher
	if d.watcher != nil {
		err := d.watcher.Close()
		if err != nil {
			log.Printf("Error closing file watcher: %v", err)
		}
	}

	// Clear all scanners
	d.mu.Lock()
	d.scanners = make(map[string]Scanner)
	d.plugins = make(map[string]*plugin.Plugin)
	d.pluginPaths = make(map[string]string)
	d.mu.Unlock()

	// Clear registry
	d.registry.mu.Lock()
	d.registry.services = make(map[string]ScannerMetadata)
	d.registry.mu.Unlock()

	log.Printf("Dynamic scanner loader stopped")
	return nil
}

// ValidateScanner validates that a scanner plugin conforms to the expected interface
func (d *DynamicScannerLoader) ValidateScanner(filePath string) error {
	// Try to open the plugin
	p, err := plugin.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open plugin: %w", err)
	}

	// Check for required symbols
	newScannerSymbol, err := p.Lookup("NewScanner")
	if err != nil {
		return fmt.Errorf("plugin does not export NewScanner function: %w", err)
	}

	// Validate function signature
	newScanner, ok := newScannerSymbol.(func() Scanner)
	if !ok {
		return fmt.Errorf("NewScanner function has incorrect signature")
	}

	// Test scanner creation
	scanner := newScanner()
	if scanner == nil {
		return fmt.Errorf("NewScanner returned nil")
	}

	// Validate required methods
	serviceName := scanner.GetServiceName()
	if serviceName == "" {
		return fmt.Errorf("scanner returned empty service name")
	}

	version := scanner.GetVersion()
	if version == "" {
		return fmt.Errorf("scanner returned empty version")
	}

	supportedServices := scanner.GetSupportedServices()
	if len(supportedServices) == 0 {
		return fmt.Errorf("scanner returned no supported services")
	}

	log.Printf("Scanner validation successful for %s (service: %s, version: %s)", filePath, serviceName, version)
	return nil
}

// GetHealthStatus returns the health status of the dynamic loader
func (d *DynamicScannerLoader) GetHealthStatus() map[string]interface{} {
	d.mu.RLock()
	loadedCount := len(d.scanners)
	d.mu.RUnlock()

	d.registry.mu.RLock()
	activeCount := 0
	errorCount := 0
	for _, metadata := range d.registry.services {
		if metadata.IsActive {
			activeCount++
		}
		if metadata.ErrorMessage != "" {
			errorCount++
		}
	}
	d.registry.mu.RUnlock()

	return map[string]interface{}{
		"loaded_scanners":  loadedCount,
		"active_scanners":  activeCount,
		"scanners_with_errors": errorCount,
		"watch_directory":  d.watchDir,
		"is_watching":      d.watcher != nil,
	}
}
package registry

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// DynamicServiceRegistry manages AWS service definitions dynamically
type DynamicServiceRegistry interface {
	// Core registry operations
	RegisterService(def ServiceDefinition) error
	GetService(name string) (*ServiceDefinition, bool)
	ListServices() []string
	ListServiceDefinitions() []ServiceDefinition
	UpdateService(name string, updates func(*ServiceDefinition) error) error
	RemoveService(name string) error

	// Bulk operations
	RegisterServices(defs []ServiceDefinition) error
	GetServices(names []string) ([]*ServiceDefinition, error)
	FilterServices(filter ServiceFilter) []ServiceDefinition

	// Discovery integration
	PopulateFromDiscovery(discovered []*pb.ServiceInfo) error
	PopulateFromReflection(serviceClients map[string]interface{}) error
	MergeWithExisting(def ServiceDefinition) error

	// Persistence operations
	PersistToFile(path string) error
	LoadFromFile(path string) error
	LoadFromDirectory(dir string) error
	ExportServices(services []string, path string) error

	// Statistics and monitoring
	GetStats() RegistryStats
	ValidateRegistry() []error
	GetServiceHealth(name string) error

	// Configuration
	GetConfig() RegistryConfig
	UpdateConfig(config RegistryConfig) error
}

// serviceRegistry is the default implementation of DynamicServiceRegistry
type serviceRegistry struct {
	mu       sync.RWMutex
	services map[string]*ServiceDefinition
	config   RegistryConfig
	stats    RegistryStats

	// Persistence management
	persistencePath   string
	lastPersisted     time.Time
	persistenceTimer  *time.Timer
	persistenceMutex  sync.Mutex

	// Cache management
	cache           map[string]*cacheEntry
	cacheMutex      sync.RWMutex
	cacheHits       int64
	cacheMisses     int64

	// Discovery state
	lastDiscovery   time.Time
	discoveryErrors []error

	// Audit log
	auditLog []auditEntry
}

// cacheEntry represents a cached service definition
type cacheEntry struct {
	service    *ServiceDefinition
	expiry     time.Time
	accessTime time.Time
	hitCount   int
}

// auditEntry represents a change to the registry
type auditEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	Action      string                 `json:"action"`
	ServiceName string                 `json:"serviceName"`
	Details     map[string]interface{} `json:"details"`
	User        string                 `json:"user,omitempty"`
}

// NewServiceRegistry creates a new dynamic service registry
func NewServiceRegistry(config RegistryConfig) DynamicServiceRegistry {
	registry := &serviceRegistry{
		services:        make(map[string]*ServiceDefinition),
		config:          config,
		cache:           make(map[string]*cacheEntry),
		auditLog:        make([]auditEntry, 0),
		persistencePath: config.PersistencePath,
		stats: RegistryStats{
			ServicesBySource: make(map[string]int),
			ServicesByStatus: make(map[string]int),
			CustomMetrics:    make(map[string]interface{}),
			LastUpdated:      time.Now(),
		},
	}

	// Load from persistence if configured
	if config.PersistencePath != "" && fileExists(config.PersistencePath) {
		if err := registry.LoadFromFile(config.PersistencePath); err != nil {
			// Log error but continue with empty registry
			fmt.Printf("Warning: Failed to load registry from %s: %v\n", config.PersistencePath, err)
		}
	}

	// Start auto-persistence if configured
	if config.AutoPersist && config.PersistenceInterval > 0 {
		registry.startAutoPersistence()
	}

	// Initialize with fallback services if configured
	if config.UseFallbackServices {
		registry.loadFallbackServices()
	}

	return registry
}

// RegisterService adds or updates a service definition
func (r *serviceRegistry) RegisterService(def ServiceDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate the service definition
	if err := r.validateServiceDefinition(&def); err != nil {
		return fmt.Errorf("invalid service definition: %w", err)
	}

	// Normalize service name
	def.Name = strings.ToLower(def.Name)

	// Set metadata if not present
	if def.DiscoveredAt.IsZero() {
		def.DiscoveredAt = time.Now()
	}
	if def.DiscoverySource == "" {
		def.DiscoverySource = "manual"
	}
	def.LastValidated = time.Now()
	def.ValidationStatus = "valid"

	// Check if service already exists
	_, exists := r.services[def.Name]

	// Store the service
	r.services[def.Name] = &def

	// Update statistics
	r.updateStats()

	// Add audit log entry
	if r.config.EnableAuditLog {
		r.addAuditEntry(auditEntry{
			Timestamp:   time.Now(),
			Action:      map[bool]string{true: "update", false: "create"}[exists],
			ServiceName: def.Name,
			Details: map[string]interface{}{
				"resourceTypes": len(def.ResourceTypes),
				"operations":    len(def.Operations),
			},
		})
	}

	// Invalidate cache for this service
	r.invalidateCache(def.Name)

	// Trigger persistence if auto-persist is enabled
	if r.config.AutoPersist {
		r.schedulePersistence()
	}

	return nil
}

// GetService retrieves a service definition by name
func (r *serviceRegistry) GetService(name string) (*ServiceDefinition, bool) {
	name = strings.ToLower(name)

	// Check cache first if enabled
	if r.config.EnableCache {
		if cached := r.getFromCache(name); cached != nil {
			return cached, true
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	service, exists := r.services[name]
	if !exists {
		r.cacheMisses++
		return nil, false
	}

	// Make a copy to prevent external modifications
	serviceCopy := *service

	// Update cache if enabled
	if r.config.EnableCache {
		r.updateCache(name, &serviceCopy)
	}

	return &serviceCopy, true
}

// ListServices returns all registered service names
func (r *serviceRegistry) ListServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	return names
}

// ListServiceDefinitions returns all service definitions
func (r *serviceRegistry) ListServiceDefinitions() []ServiceDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ServiceDefinition, 0, len(r.services))
	for _, service := range r.services {
		defs = append(defs, *service)
	}
	return defs
}

// UpdateService updates an existing service definition
func (r *serviceRegistry) UpdateService(name string, updates func(*ServiceDefinition) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name = strings.ToLower(name)
	service, exists := r.services[name]
	if !exists {
		return fmt.Errorf("service %s not found", name)
	}

	// Create a copy for updates
	updatedService := *service

	// Apply updates
	if err := updates(&updatedService); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	// Validate updated definition
	if err := r.validateServiceDefinition(&updatedService); err != nil {
		return fmt.Errorf("invalid updated definition: %w", err)
	}

	// Update metadata
	updatedService.LastValidated = time.Now()

	// Store updated service
	r.services[name] = &updatedService

	// Update statistics
	r.updateStats()

	// Add audit log entry
	if r.config.EnableAuditLog {
		r.addAuditEntry(auditEntry{
			Timestamp:   time.Now(),
			Action:      "update",
			ServiceName: name,
			Details: map[string]interface{}{
				"modified": true,
			},
		})
	}

	// Invalidate cache
	r.invalidateCache(name)

	// Trigger persistence
	if r.config.AutoPersist {
		r.schedulePersistence()
	}

	return nil
}

// RemoveService removes a service from the registry
func (r *serviceRegistry) RemoveService(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name = strings.ToLower(name)
	if _, exists := r.services[name]; !exists {
		return fmt.Errorf("service %s not found", name)
	}

	delete(r.services, name)

	// Update statistics
	r.updateStats()

	// Add audit log entry
	if r.config.EnableAuditLog {
		r.addAuditEntry(auditEntry{
			Timestamp:   time.Now(),
			Action:      "delete",
			ServiceName: name,
		})
	}

	// Invalidate cache
	r.invalidateCache(name)

	// Trigger persistence
	if r.config.AutoPersist {
		r.schedulePersistence()
	}

	return nil
}

// RegisterServices registers multiple services at once
func (r *serviceRegistry) RegisterServices(defs []ServiceDefinition) error {
	var errors []error
	for _, def := range defs {
		if err := r.RegisterService(def); err != nil {
			errors = append(errors, fmt.Errorf("failed to register %s: %w", def.Name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("registration errors: %v", errors)
	}
	return nil
}

// GetServices retrieves multiple services by name
func (r *serviceRegistry) GetServices(names []string) ([]*ServiceDefinition, error) {
	services := make([]*ServiceDefinition, 0, len(names))
	var notFound []string

	for _, name := range names {
		if service, exists := r.GetService(name); exists {
			services = append(services, service)
		} else {
			notFound = append(notFound, name)
		}
	}

	if len(notFound) > 0 {
		return services, fmt.Errorf("services not found: %v", notFound)
	}
	return services, nil
}

// FilterServices returns services matching the given filter
func (r *serviceRegistry) FilterServices(filter ServiceFilter) []ServiceDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []ServiceDefinition

	for _, service := range r.services {
		if r.matchesFilter(service, filter) {
			results = append(results, *service)
		}
	}

	return results
}

// PopulateFromDiscovery populates the registry from discovered service info
func (r *serviceRegistry) PopulateFromDiscovery(discovered []*pb.ServiceInfo) error {
	r.stats.DiscoverySuccess = 0
	r.stats.DiscoveryFailures = 0

	for _, info := range discovered {
		def := r.convertDiscoveredService(info)
		if err := r.RegisterService(def); err != nil {
			r.stats.DiscoveryFailures++
			r.discoveryErrors = append(r.discoveryErrors, fmt.Errorf("failed to register %s: %w", info.Name, err))
		} else {
			r.stats.DiscoverySuccess++
		}
	}

	r.lastDiscovery = time.Now()

	if r.stats.DiscoveryFailures > 0 {
		return fmt.Errorf("discovered %d services with %d failures", r.stats.DiscoverySuccess, r.stats.DiscoveryFailures)
	}
	return nil
}

// PopulateFromReflection populates the registry using reflection on service clients
func (r *serviceRegistry) PopulateFromReflection(serviceClients map[string]interface{}) error {
	for serviceName, client := range serviceClients {
		def := r.reflectServiceDefinition(serviceName, client)
		if err := r.RegisterService(def); err != nil {
			return fmt.Errorf("failed to register %s from reflection: %w", serviceName, err)
		}
	}
	return nil
}

// MergeWithExisting merges a new definition with an existing one
func (r *serviceRegistry) MergeWithExisting(def ServiceDefinition) error {
	return r.UpdateService(def.Name, func(existing *ServiceDefinition) error {
		// Merge operations
		existingOps := make(map[string]bool)
		for _, op := range existing.Operations {
			existingOps[op.Name] = true
		}
		for _, op := range def.Operations {
			if !existingOps[op.Name] {
				existing.Operations = append(existing.Operations, op)
			}
		}

		// Merge resource types
		existingResources := make(map[string]bool)
		for _, rt := range existing.ResourceTypes {
			existingResources[rt.Name] = true
		}
		for _, rt := range def.ResourceTypes {
			if !existingResources[rt.Name] {
				existing.ResourceTypes = append(existing.ResourceTypes, rt)
			}
		}

		// Update metadata
		if def.RateLimit > 0 {
			existing.RateLimit = def.RateLimit
		}
		if def.BurstLimit > 0 {
			existing.BurstLimit = def.BurstLimit
		}

		// Merge permissions
		permMap := make(map[string]bool)
		for _, perm := range existing.Permissions {
			permMap[perm] = true
		}
		for _, perm := range def.Permissions {
			if !permMap[perm] {
				existing.Permissions = append(existing.Permissions, perm)
			}
		}

		return nil
	})
}

// GetStats returns registry statistics
func (r *serviceRegistry) GetStats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := r.stats
	stats.TotalServices = len(r.services)
	
	// Calculate cache hit rate
	total := float64(r.cacheHits + r.cacheMisses)
	if total > 0 {
		stats.CacheHitRate = float64(r.cacheHits) / total
	}

	// Count resources and operations
	stats.TotalResourceTypes = 0
	stats.TotalOperations = 0
	for _, service := range r.services {
		stats.TotalResourceTypes += len(service.ResourceTypes)
		stats.TotalOperations += len(service.Operations)
	}

	return stats
}

// ValidateRegistry validates all service definitions
func (r *serviceRegistry) ValidateRegistry() []error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var errors []error
	for name, service := range r.services {
		if err := r.validateServiceDefinition(service); err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors
}

// GetServiceHealth checks the health of a specific service
func (r *serviceRegistry) GetServiceHealth(name string) error {
	service, exists := r.GetService(name)
	if !exists {
		return fmt.Errorf("service %s not found", name)
	}

	// Check validation status
	if service.ValidationStatus != "valid" {
		return fmt.Errorf("service validation status: %s", service.ValidationStatus)
	}

	// Check last validated time
	if time.Since(service.LastValidated) > 24*time.Hour {
		return fmt.Errorf("service not validated in 24 hours")
	}

	return nil
}

// GetConfig returns the current registry configuration
func (r *serviceRegistry) GetConfig() RegistryConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

// UpdateConfig updates the registry configuration
func (r *serviceRegistry) UpdateConfig(config RegistryConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	oldConfig := r.config
	r.config = config

	// Handle configuration changes
	if config.AutoPersist && !oldConfig.AutoPersist {
		r.startAutoPersistence()
	} else if !config.AutoPersist && oldConfig.AutoPersist {
		r.stopAutoPersistence()
	}

	if config.PersistencePath != oldConfig.PersistencePath {
		r.persistencePath = config.PersistencePath
	}

	return nil
}

// Helper methods

func (r *serviceRegistry) validateServiceDefinition(def *ServiceDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("service name is required")
	}
	if def.PackagePath == "" && def.DiscoverySource == "manual" {
		return fmt.Errorf("package path is required for manually defined services")
	}
	if def.RateLimit == 0 {
		def.RateLimit = rate.Limit(10) // Default rate limit
	}
	if def.BurstLimit == 0 {
		def.BurstLimit = 20 // Default burst limit
	}
	return nil
}

func (r *serviceRegistry) updateStats() {
	r.stats.LastUpdated = time.Now()
	
	// Reset counters
	r.stats.ServicesBySource = make(map[string]int)
	r.stats.ServicesByStatus = make(map[string]int)
	
	// Count by source and status
	for _, service := range r.services {
		r.stats.ServicesBySource[service.DiscoverySource]++
		r.stats.ServicesByStatus[service.ValidationStatus]++
	}
}

func (r *serviceRegistry) matchesFilter(service *ServiceDefinition, filter ServiceFilter) bool {
	// Filter by names
	if len(filter.Names) > 0 {
		found := false
		for _, name := range filter.Names {
			if service.Name == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by resource types
	if len(filter.ResourceTypes) > 0 {
		found := false
		for _, filterRT := range filter.ResourceTypes {
			for _, serviceRT := range service.ResourceTypes {
				if serviceRT.Name == filterRT || serviceRT.ResourceType == filterRT {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by required operations
	if len(filter.RequiredOperations) > 0 {
		opMap := make(map[string]bool)
		for _, op := range service.Operations {
			opMap[op.Name] = true
		}
		for _, reqOp := range filter.RequiredOperations {
			if !opMap[reqOp] {
				return false
			}
		}
	}

	// Filter by capabilities
	if filter.RequiresPagination != nil && service.SupportsPagination != *filter.RequiresPagination {
		return false
	}
	if filter.RequiresResourceExplorer != nil && service.SupportsResourceExplorer != *filter.RequiresResourceExplorer {
		return false
	}
	if filter.RequiresGlobalService != nil && service.GlobalService != *filter.RequiresGlobalService {
		return false
	}

	// Filter by discovery source
	if filter.DiscoverySource != "" && service.DiscoverySource != filter.DiscoverySource {
		return false
	}

	// Filter by discovery time
	if !filter.DiscoveredAfter.IsZero() && service.DiscoveredAt.Before(filter.DiscoveredAfter) {
		return false
	}
	if !filter.DiscoveredBefore.IsZero() && service.DiscoveredAt.After(filter.DiscoveredBefore) {
		return false
	}

	// Filter by validation status
	if len(filter.ValidationStatus) > 0 {
		found := false
		for _, status := range filter.ValidationStatus {
			if service.ValidationStatus == status {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by validation time
	if !filter.ValidatedAfter.IsZero() && service.LastValidated.Before(filter.ValidatedAfter) {
		return false
	}

	return true
}

func (r *serviceRegistry) convertDiscoveredService(info *pb.ServiceInfo) ServiceDefinition {
	def := ServiceDefinition{
		Name:             strings.ToLower(info.Name),
		DisplayName:      info.DisplayName,
		// Description:      info.Description, // TODO: Check proto definition
		DiscoverySource:  "resource-explorer",
		DiscoveredAt:     time.Now(),
		ValidationStatus: "discovered",
		RateLimit:        rate.Limit(10), // Default, will be updated
		BurstLimit:       20,              // Default, will be updated
	}

	// Convert resource types
	for _, rt := range info.ResourceTypes {
		def.ResourceTypes = append(def.ResourceTypes, ResourceTypeDefinition{
			Name:         rt.Name,
			// ResourceType: rt.Type, // TODO: Check proto definition
			Paginated:    rt.Paginated,
		})
	}

	// Convert operations (TODO: Check if info.Operations field exists)
	// for _, op := range info.Operations {
	// 	def.Operations = append(def.Operations, OperationDefinition{
	// 		Name:          op.Name,
	// 		OperationType: op.Type,
	// 		Paginated:     op.Paginated,
	// 	})
	// }

	return def
}

func (r *serviceRegistry) reflectServiceDefinition(serviceName string, client interface{}) ServiceDefinition {
	clientType := reflect.TypeOf(client)
	
	def := ServiceDefinition{
		Name:             strings.ToLower(serviceName),
		DisplayName:      formatServiceName(serviceName),
		ClientType:       clientType.String(),
		ClientRef:        clientType,
		DiscoverySource:  "reflection",
		DiscoveredAt:     time.Now(),
		ValidationStatus: "reflected",
		RateLimit:        rate.Limit(10), // Will be updated based on service
		BurstLimit:       20,              // Will be updated based on service
	}

	// Use reflection to discover operations
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		if isListOperation(method.Name) {
			def.Operations = append(def.Operations, OperationDefinition{
				Name:          method.Name,
				OperationType: "List",
			})
		}
	}

	return def
}

func (r *serviceRegistry) addAuditEntry(entry auditEntry) {
	r.auditLog = append(r.auditLog, entry)
	
	// Limit audit log size
	maxEntries := 10000
	if len(r.auditLog) > maxEntries {
		r.auditLog = r.auditLog[len(r.auditLog)-maxEntries:]
	}
}

// Cache management helpers

func (r *serviceRegistry) getFromCache(name string) *ServiceDefinition {
	r.cacheMutex.RLock()
	defer r.cacheMutex.RUnlock()

	entry, exists := r.cache[name]
	if !exists || time.Now().After(entry.expiry) {
		return nil
	}

	r.cacheHits++
	entry.hitCount++
	entry.accessTime = time.Now()
	return entry.service
}

func (r *serviceRegistry) updateCache(name string, service *ServiceDefinition) {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()

	// Enforce max cache size
	if len(r.cache) >= r.config.MaxCacheSize && r.config.MaxCacheSize > 0 {
		r.evictOldestCache()
	}

	r.cache[name] = &cacheEntry{
		service:    service,
		expiry:     time.Now().Add(r.config.CacheTTL),
		accessTime: time.Now(),
		hitCount:   0,
	}
}

func (r *serviceRegistry) invalidateCache(name string) {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()
	delete(r.cache, name)
}

func (r *serviceRegistry) evictOldestCache() {
	var oldest string
	var oldestTime time.Time

	for name, entry := range r.cache {
		if oldest == "" || entry.accessTime.Before(oldestTime) {
			oldest = name
			oldestTime = entry.accessTime
		}
	}

	if oldest != "" {
		delete(r.cache, oldest)
	}
}

// Persistence helpers

func (r *serviceRegistry) startAutoPersistence() {
	if r.persistenceTimer != nil {
		r.persistenceTimer.Stop()
	}

	r.persistenceTimer = time.AfterFunc(r.config.PersistenceInterval, func() {
		r.persistenceMutex.Lock()
		defer r.persistenceMutex.Unlock()

		if err := r.PersistToFile(r.persistencePath); err != nil {
			fmt.Printf("Auto-persistence failed: %v\n", err)
		}

		// Reschedule
		r.startAutoPersistence()
	})
}

func (r *serviceRegistry) stopAutoPersistence() {
	if r.persistenceTimer != nil {
		r.persistenceTimer.Stop()
		r.persistenceTimer = nil
	}
}

func (r *serviceRegistry) schedulePersistence() {
	// Debounce persistence to avoid too frequent writes
	r.persistenceMutex.Lock()
	defer r.persistenceMutex.Unlock()

	if time.Since(r.lastPersisted) < 5*time.Second {
		return // Skip if persisted recently
	}

	go func() {
		time.Sleep(1 * time.Second) // Small delay to batch changes
		if err := r.PersistToFile(r.persistencePath); err != nil {
			fmt.Printf("Scheduled persistence failed: %v\n", err)
		}
	}()
}

func (r *serviceRegistry) loadFallbackServices() {
	// This will be populated with known service definitions
	// For now, we'll add a few core services as examples
	fallbackServices := []ServiceDefinition{
		{
			Name:        "s3",
			DisplayName: "Amazon S3",
			Description: "Simple Storage Service",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
			ClientType:  "*s3.Client",
			RateLimit:   rate.Limit(100),
			BurstLimit:  200,
			GlobalService: true,
			ResourceTypes: []ResourceTypeDefinition{
				{
					Name:              "Bucket",
					ResourceType:      "AWS::S3::Bucket",
					ListOperation:     "ListBuckets",
					IsGlobalResource:  true,
					SupportsTags:      true,
				},
			},
			Permissions: []string{"s3:ListAllMyBuckets", "s3:GetBucketLocation"},
			DiscoverySource: "fallback",
			ValidationStatus: "valid",
		},
		{
			Name:        "ec2",
			DisplayName: "Amazon EC2",
			Description: "Elastic Compute Cloud",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/ec2",
			ClientType:  "*ec2.Client",
			RateLimit:   rate.Limit(20),
			BurstLimit:  40,
			RequiresRegion: true,
			ResourceTypes: []ResourceTypeDefinition{
				{
					Name:              "Instance",
					ResourceType:      "AWS::EC2::Instance",
					ListOperation:     "DescribeInstances",
					DescribeOperation: "DescribeInstances",
					SupportsTags:      true,
					Paginated:         true,
				},
			},
			Permissions: []string{"ec2:DescribeInstances", "ec2:DescribeRegions"},
			DiscoverySource: "fallback",
			ValidationStatus: "valid",
		},
	}

	for _, service := range fallbackServices {
		if r.config.FallbackServiceList == nil || contains(r.config.FallbackServiceList, service.Name) {
			r.services[service.Name] = &service
		}
	}
}

// Utility functions

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func formatServiceName(name string) string {
	// Convert "s3" to "Amazon S3", "ec2" to "Amazon EC2", etc.
	return "Amazon " + strings.ToUpper(name)
}

func isListOperation(name string) bool {
	return strings.HasPrefix(name, "List") || strings.HasPrefix(name, "Describe")
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
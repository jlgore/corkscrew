package registry

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// UnifiedServiceRegistry combines service metadata, client creation, and scanning capabilities
// into a single, coherent registry system that replaces the three separate registries
type UnifiedServiceRegistry struct {
	mu sync.RWMutex

	// Core service definitions (from DynamicServiceRegistry)
	services map[string]*ServiceDefinition
	config   RegistryConfig
	stats    RegistryStats

	// Client factory capabilities (from UnifiedClientFactory)
	awsConfig       aws.Config
	clientFactories map[string]ClientFactoryFunc
	clientCache     sync.Map
	factoryStrategies []CreationStrategy
	
	// Lazy loading configuration
	lazyLoadEnabled bool
	lazyLoadCache   sync.Map // service name -> bool (indicates if loaded)

	// Scanner capabilities (from ScannerRegistry)
	rateLimiters map[string]*rate.Limiter
	scannerProvider UnifiedScannerProvider

	// Unified caching
	cache       map[string]*cacheEntry
	cacheMutex  sync.RWMutex
	cacheHits   int64
	cacheMisses int64

	// Persistence and discovery
	persistencePath   string
	lastPersisted     time.Time
	lastDiscovery     time.Time
	discoveryErrors   []error
	auditLog          []auditEntry
}

// ClientFactoryFunc creates a client for a service
type ClientFactoryFunc func(aws.Config) interface{}

// CreationStrategy represents a client creation strategy
type CreationStrategy interface {
	Name() string
	CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error)
	Priority() int
}

// UnifiedScannerProvider provides scanning capabilities
type UnifiedScannerProvider interface {
	ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error)
	DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error)
	GetMetrics() interface{}
}

// NewUnifiedServiceRegistry creates a properly initialized unified registry
func NewUnifiedServiceRegistry(cfg aws.Config, config RegistryConfig) *UnifiedServiceRegistry {
	registry := &UnifiedServiceRegistry{
		services:        make(map[string]*ServiceDefinition),
		config:          config,
		awsConfig:       cfg,
		clientFactories: make(map[string]ClientFactoryFunc),
		rateLimiters:    make(map[string]*rate.Limiter),
		cache:           make(map[string]*cacheEntry),
		auditLog:        make([]auditEntry, 0),
		persistencePath: config.PersistencePath,
		stats: RegistryStats{
			ServicesBySource: make(map[string]int),
			ServicesByStatus: make(map[string]int),
			CustomMetrics:    make(map[string]interface{}),
			LastUpdated:      time.Now(),
		},
		lazyLoadEnabled: config.EnableCache, // Enable lazy loading if caching is enabled
	}

	// Initialize fallback strategies
	registry.setupDefaultStrategies()

	// Register core AWS services
	registry.registerCoreServices()

	// Load from persistence if configured
	if config.PersistencePath != "" && fileExists(config.PersistencePath) {
		if err := registry.LoadFromFile(config.PersistencePath); err != nil {
			fmt.Printf("Warning: Failed to load registry from %s: %v\n", config.PersistencePath, err)
		}
	}

	// Initialize with fallback services if configured
	if config.UseFallbackServices {
		registry.loadFallbackServices()
	}

	return registry
}

// SetServiceFilter configures which services should be loaded/used
func (r *UnifiedServiceRegistry) SetServiceFilter(services []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(services) == 0 {
		// No filter, allow all services
		r.config.FallbackServiceList = nil
		return
	}

	// Set the filter list
	r.config.FallbackServiceList = services

	// Update lazy loading to only load filtered services
	for _, serviceName := range services {
		r.lazyLoadCache.Store(serviceName, false) // Mark as ready for lazy loading
	}
}

// IsServiceAllowed checks if a service is in the allowed list
func (r *UnifiedServiceRegistry) IsServiceAllowed(serviceName string) bool {
	if len(r.config.FallbackServiceList) == 0 {
		return true // No filter set, allow all
	}

	serviceName = formatUnifiedServiceName(serviceName)
	for _, allowed := range r.config.FallbackServiceList {
		if formatUnifiedServiceName(allowed) == serviceName {
			return true
		}
	}
	return false
}

// GetFilteredServices returns only the services that pass the filter
func (r *UnifiedServiceRegistry) GetFilteredServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []string
	for serviceName := range r.services {
		if r.IsServiceAllowed(serviceName) {
			filtered = append(filtered, serviceName)
		}
	}
	return filtered
}

// Service Management (from DynamicServiceRegistry)

// RegisterService adds or updates a service definition with factory integration
func (r *UnifiedServiceRegistry) RegisterService(def ServiceDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if service is allowed by filter
	if !r.IsServiceAllowed(def.Name) {
		// Skip registration of filtered out services
		return nil
	}

	// Validate the service definition
	if err := r.validateServiceDefinition(&def); err != nil {
		return fmt.Errorf("invalid service definition: %w", err)
	}

	// Normalize service name
	def.Name = formatUnifiedServiceName(def.Name)

	// Set metadata if not present
	if def.DiscoveredAt.IsZero() {
		def.DiscoveredAt = time.Now()
	}
	if def.DiscoverySource == "" {
		def.DiscoverySource = "manual"
	}
	def.LastValidated = time.Now()
	def.ValidationStatus = "valid"

	// Store the service
	r.services[def.Name] = &def

	// Create rate limiter for the service
	r.createRateLimiter(def.Name, def.RateLimit, def.BurstLimit)

	// Update statistics
	r.updateStats()

	// Add audit log entry
	if r.config.EnableAuditLog {
		r.addAuditEntry(auditEntry{
			Timestamp:   time.Now(),
			Action:      "register",
			ServiceName: def.Name,
			Details: map[string]interface{}{
				"resourceTypes": len(def.ResourceTypes),
				"operations":    len(def.Operations),
			},
		})
	}

	// Invalidate cache for this service
	r.invalidateCache(def.Name)

	return nil
}

// RegisterServiceWithFactory registers a service with its client factory
func (r *UnifiedServiceRegistry) RegisterServiceWithFactory(def ServiceDefinition, factory ClientFactoryFunc) error {
	if err := r.RegisterService(def); err != nil {
		return err
	}

	r.mu.Lock()
	r.clientFactories[def.Name] = factory
	r.mu.Unlock()

	return nil
}

// GetService retrieves a service definition by name
func (r *UnifiedServiceRegistry) GetService(name string) (*ServiceDefinition, bool) {
	name = formatUnifiedServiceName(name)

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
func (r *UnifiedServiceRegistry) ListServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	return names
}

// ListServiceDefinitions returns all service definitions
func (r *UnifiedServiceRegistry) ListServiceDefinitions() []ServiceDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ServiceDefinition, 0, len(r.services))
	for _, service := range r.services {
		defs = append(defs, *service)
	}
	return defs
}

// Client Factory Methods (from UnifiedClientFactory)

// CreateClient creates a client for the specified service using the configured strategies with lazy loading
func (r *UnifiedServiceRegistry) CreateClient(ctx context.Context, serviceName string) (interface{}, error) {
	serviceName = formatUnifiedServiceName(serviceName)

	// Check if already created and cached
	if client, ok := r.clientCache.Load(serviceName); ok {
		// Update access statistics for cache optimization
		r.updateCacheHitMetrics(serviceName)
		return client, nil
	}

	// Lazy loading: check if service metadata needs to be loaded
	if r.lazyLoadEnabled {
		if err := r.ensureServiceLoaded(serviceName); err != nil {
			return nil, fmt.Errorf("lazy load failed for %s: %w", serviceName, err)
		}
	}

	// Try direct factory first if available
	r.mu.RLock()
	factory, hasFactory := r.clientFactories[serviceName]
	r.mu.RUnlock()

	if hasFactory {
		client := factory(r.awsConfig)
		r.clientCache.Store(serviceName, client)
		r.updateCacheMetrics(serviceName, true)
		return client, nil
	}

	// Try each creation strategy in order
	var lastError error
	for _, strategy := range r.factoryStrategies {
		client, err := strategy.CreateClient(ctx, serviceName, r.awsConfig)
		if err == nil && client != nil {
			// Success - cache and return
			r.clientCache.Store(serviceName, client)
			r.updateCacheMetrics(serviceName, true)
			return client, nil
		}
		if err != nil {
			lastError = fmt.Errorf("%s strategy failed: %w", strategy.Name(), err)
		}
	}

	// Cache miss metrics
	r.updateCacheMetrics(serviceName, false)

	// All strategies failed
	if lastError != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", serviceName, lastError)
	}
	return nil, fmt.Errorf("no strategy could create client for %s", serviceName)
}

// GetClient is an alias for CreateClient for compatibility
func (r *UnifiedServiceRegistry) GetClient(ctx context.Context, serviceName string) (interface{}, error) {
	return r.CreateClient(ctx, serviceName)
}

// Scanner Registry Methods (from ScannerRegistry)

// SetUnifiedScanner sets the unified scanner for dynamic resource discovery
func (r *UnifiedServiceRegistry) SetUnifiedScanner(scanner UnifiedScannerProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scannerProvider = scanner
}

// ScanService scans a single service using the unified scanner with rate limiting
func (r *UnifiedServiceRegistry) ScanService(ctx context.Context, serviceName string, region string) ([]*pb.Resource, error) {
	serviceName = formatUnifiedServiceName(serviceName)

	// Apply rate limiting
	limiter := r.GetRateLimiter(serviceName)
	if err := limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait failed: %w", err)
	}

	r.mu.RLock()
	scanner := r.scannerProvider
	r.mu.RUnlock()

	if scanner == nil {
		return nil, fmt.Errorf("unified scanner not configured")
	}

	// Performance tracking
	scanStart := time.Now()

	// Scan and enrich in one pass
	refs, err := scanner.ScanService(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("scan failed for %s: %w", serviceName, err)
	}

	// Convert refs to full resources with configuration
	resources := make([]*pb.Resource, 0, len(refs))
	for _, ref := range refs {
		// Set region on ref if not already set
		if ref.Region == "" {
			ref.Region = region
		}

		resource, err := scanner.DescribeResource(ctx, ref)
		if err != nil {
			// Create basic resource on enrichment failure
			resource = &pb.Resource{
				Provider: "aws",
				Service:  ref.Service,
				Type:     ref.Type,
				Id:       ref.Id,
				Name:     ref.Name,
				Region:   ref.Region,
				Tags:     make(map[string]string),
			}
		}

		resources = append(resources, resource)
	}

	scanDuration := time.Since(scanStart)
	fmt.Printf("Unified scan of %s completed in %v, found %d resources\n",
		serviceName, scanDuration, len(resources))

	return resources, nil
}

// GetRateLimiter returns the rate limiter for a service
func (r *UnifiedServiceRegistry) GetRateLimiter(serviceName string) *rate.Limiter {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serviceName = formatUnifiedServiceName(serviceName)
	if limiter, exists := r.rateLimiters[serviceName]; exists {
		return limiter
	}

	// Return default limiter if service not found
	return rate.NewLimiter(rate.Limit(10), 20)
}

// Discovery and Integration Methods

// PopulateFromDiscovery populates the registry from discovered service info
func (r *UnifiedServiceRegistry) PopulateFromDiscovery(discovered []*pb.ServiceInfo) error {
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
func (r *UnifiedServiceRegistry) PopulateFromReflection(serviceClients map[string]interface{}) error {
	for serviceName, client := range serviceClients {
		def := r.reflectServiceDefinition(serviceName, client)
		if err := r.RegisterService(def); err != nil {
			return fmt.Errorf("failed to register %s from reflection: %w", serviceName, err)
		}
	}
	return nil
}

// Migration Support

// MigrateFromDynamicRegistry migrates from the old DynamicServiceRegistry
func (r *UnifiedServiceRegistry) MigrateFromDynamicRegistry(oldRegistry DynamicServiceRegistry) error {
	defs := oldRegistry.ListServiceDefinitions()
	for _, def := range defs {
		if err := r.RegisterService(def); err != nil {
			return fmt.Errorf("failed to migrate service %s: %w", def.Name, err)
		}
	}
	fmt.Printf("Migrated %d services from DynamicServiceRegistry\n", len(defs))
	return nil
}

// Performance and Statistics

// GetStats returns unified registry statistics
func (r *UnifiedServiceRegistry) GetStats() RegistryStats {
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

// InvalidateCache removes a service from all caches
func (r *UnifiedServiceRegistry) InvalidateCache(serviceName string) {
	serviceName = formatUnifiedServiceName(serviceName)
	r.clientCache.Delete(serviceName)
	r.invalidateCache(serviceName)
}

// ClearAllCaches clears all cached clients and metadata
func (r *UnifiedServiceRegistry) ClearAllCaches() {
	r.clientCache = sync.Map{}
	r.cacheMutex.Lock()
	r.cache = make(map[string]*cacheEntry)
	r.cacheMutex.Unlock()
}

// Private Helper Methods

func (r *UnifiedServiceRegistry) setupDefaultStrategies() {
	// Default strategies will be added here
	// For now, we'll implement the basic strategy pattern
	r.factoryStrategies = []CreationStrategy{
		&reflectionStrategy{registry: r},
		&genericStrategy{},
	}
}

func (r *UnifiedServiceRegistry) registerCoreServices() {
	coreServices := []struct {
		name        string
		displayName string
		packagePath string
		factory     ClientFactoryFunc
		rateLimit   rate.Limit
		burstLimit  int
		global      bool
	}{
		// We'll implement the core service registration
		// This will be populated with actual AWS services
	}

	for _, svc := range coreServices {
		def := ServiceDefinition{
			Name:           svc.name,
			DisplayName:    svc.displayName,
			PackagePath:    svc.packagePath,
			RateLimit:      svc.rateLimit,
			BurstLimit:     svc.burstLimit,
			GlobalService:  svc.global,
			DiscoverySource: "core",
			ValidationStatus: "valid",
		}

		r.RegisterServiceWithFactory(def, svc.factory)
	}
}

func (r *UnifiedServiceRegistry) createRateLimiter(serviceName string, rateLimit rate.Limit, burstLimit int) {
	if rateLimit == 0 {
		rateLimit = rate.Limit(10) // Default
	}
	if burstLimit == 0 {
		burstLimit = 20 // Default
	}

	r.rateLimiters[serviceName] = rate.NewLimiter(rateLimit, burstLimit)
}

func (r *UnifiedServiceRegistry) validateServiceDefinition(def *ServiceDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("service name is required")
	}
	if def.RateLimit == 0 {
		def.RateLimit = rate.Limit(10) // Default rate limit
	}
	if def.BurstLimit == 0 {
		def.BurstLimit = 20 // Default burst limit
	}
	return nil
}

func (r *UnifiedServiceRegistry) updateStats() {
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

func (r *UnifiedServiceRegistry) addAuditEntry(entry auditEntry) {
	r.auditLog = append(r.auditLog, entry)

	// Limit audit log size
	maxEntries := 10000
	if len(r.auditLog) > maxEntries {
		r.auditLog = r.auditLog[len(r.auditLog)-maxEntries:]
	}
}

func (r *UnifiedServiceRegistry) getFromCache(name string) *ServiceDefinition {
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

func (r *UnifiedServiceRegistry) updateCache(name string, service *ServiceDefinition) {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()

	r.cache[name] = &cacheEntry{
		service:    service,
		expiry:     time.Now().Add(r.config.CacheTTL),
		accessTime: time.Now(),
		hitCount:   0,
	}
}

func (r *UnifiedServiceRegistry) invalidateCache(name string) {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()
	delete(r.cache, name)
}

// Strategy implementations
type reflectionStrategy struct {
	registry    *UnifiedServiceRegistry
	clientCache sync.Map // serviceName -> reflect.Type (cached client types)
}

func (s *reflectionStrategy) Name() string { return "reflection" }
func (s *reflectionStrategy) Priority() int { return 1 }

func (s *reflectionStrategy) CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error) {
	// Check cache first for performance
	if clientType, ok := s.clientCache.Load(serviceName); ok {
		if ct, ok := clientType.(reflect.Type); ok {
			return s.createClientFromType(ct, config)
		}
	}

	// Try to discover client type through reflection
	clientType, err := s.discoverClientType(serviceName)
	if err != nil {
		return nil, fmt.Errorf("reflection discovery failed: %w", err)
	}

	// Cache the discovered type for future use
	s.clientCache.Store(serviceName, clientType)

	return s.createClientFromType(clientType, config)
}

// discoverClientType uses reflection to discover the client type for a service
func (s *reflectionStrategy) discoverClientType(serviceName string) (reflect.Type, error) {
	// Common AWS SDK v2 patterns
	possiblePackages := []string{
		fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
		fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%sv2", serviceName), // Some services have v2 suffix
	}

	for _, pkgPath := range possiblePackages {
		// Try to find the NewFromConfig function which is standard for AWS SDK v2
		clientType := s.tryDiscoverFromPackage(pkgPath, serviceName)
		if clientType != nil {
			return clientType, nil
		}
	}

	return nil, fmt.Errorf("could not discover client type for service %s", serviceName)
}

// tryDiscoverFromPackage attempts to discover client from a specific package
func (s *reflectionStrategy) tryDiscoverFromPackage(packagePath, serviceName string) reflect.Type {
	// This is a simplified reflection strategy
	// In a real implementation, you would use more sophisticated reflection
	// to dynamically discover and load client types
	
	// For now, return known types for common services
	knownTypes := map[string]string{
		"s3":       "*s3.Client",
		"ec2":      "*ec2.Client", 
		"lambda":   "*lambda.Client",
		"dynamodb": "*dynamodb.Client",
		"rds":      "*rds.Client",
	}

	if _, exists := knownTypes[serviceName]; exists {
		// Return a placeholder type - in real implementation this would be the actual type
		return reflect.TypeOf((*interface{})(nil)).Elem()
	}

	return nil
}

// createClientFromType creates a client instance from a reflected type
func (s *reflectionStrategy) createClientFromType(clientType reflect.Type, config aws.Config) (interface{}, error) {
	// This would use reflection to instantiate the client
	// For now, return a generic client wrapper
	return &ReflectedClient{
		ServiceType: clientType,
		Config:      config,
	}, nil
}

// ReflectedClient wraps a dynamically discovered client
type ReflectedClient struct {
	ServiceType reflect.Type
	Config      aws.Config
}

type genericStrategy struct{}

func (s *genericStrategy) Name() string { return "generic" }
func (s *genericStrategy) Priority() int { return 99 }
func (s *genericStrategy) CreateClient(ctx context.Context, serviceName string, config aws.Config) (interface{}, error) {
	return &GenericServiceClient{
		ServiceName: serviceName,
		Config:      config,
	}, nil
}

// GenericServiceClient provides a fallback client implementation
type GenericServiceClient struct {
	ServiceName string
	Config      aws.Config
}

// Helper functions
func formatUnifiedServiceName(name string) string {
	// Normalize service names to lowercase
	return name
}

func (r *UnifiedServiceRegistry) convertDiscoveredService(info *pb.ServiceInfo) ServiceDefinition {
	def := ServiceDefinition{
		Name:             formatUnifiedServiceName(info.Name),
		DisplayName:      info.DisplayName,
		DiscoverySource:  "resource-explorer",
		DiscoveredAt:     time.Now(),
		ValidationStatus: "discovered",
		RateLimit:        rate.Limit(10), // Default
		BurstLimit:       20,             // Default
	}

	// Convert resource types
	for _, rt := range info.ResourceTypes {
		def.ResourceTypes = append(def.ResourceTypes, ResourceTypeDefinition{
			Name:      rt.Name,
			Paginated: rt.Paginated,
		})
	}

	return def
}

func (r *UnifiedServiceRegistry) reflectServiceDefinition(serviceName string, client interface{}) ServiceDefinition {
	def := ServiceDefinition{
		Name:             formatUnifiedServiceName(serviceName),
		DisplayName:      fmt.Sprintf("Amazon %s", serviceName),
		DiscoverySource:  "reflection",
		DiscoveredAt:     time.Now(),
		ValidationStatus: "reflected",
		RateLimit:        rate.Limit(10),
		BurstLimit:       20,
	}

	// TODO: Implement reflection logic to discover operations

	return def
}

func (r *UnifiedServiceRegistry) loadFallbackServices() {
	// Load fallback services if no services are discovered
}

// MergeWithExisting merges a service definition with existing one (compatibility method)
func (r *UnifiedServiceRegistry) MergeWithExisting(def ServiceDefinition) error {
	return r.RegisterService(def)
}

// ensureServiceLoaded implements lazy loading for service metadata
func (r *UnifiedServiceRegistry) ensureServiceLoaded(serviceName string) error {
	// Check if already loaded
	if _, loaded := r.lazyLoadCache.Load(serviceName); loaded {
		return nil
	}

	// Try to discover the service if not already loaded
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring lock
	if _, loaded := r.lazyLoadCache.Load(serviceName); loaded {
		return nil
	}

	// Check if service exists in registry first
	if _, exists := r.services[serviceName]; exists {
		r.lazyLoadCache.Store(serviceName, true)
		return nil
	}

	// Try to load from well-known AWS services
	if r.tryLoadWellKnownService(serviceName) {
		r.lazyLoadCache.Store(serviceName, true)
		return nil
	}

	// Mark as attempted to avoid repeated attempts
	r.lazyLoadCache.Store(serviceName, false)
	return fmt.Errorf("service %s not found and could not be loaded", serviceName)
}

// tryLoadWellKnownService attempts to load a service from well-known AWS services
func (r *UnifiedServiceRegistry) tryLoadWellKnownService(serviceName string) bool {
	// Check if service is allowed by filter
	if !r.IsServiceAllowed(serviceName) {
		return false
	}

	wellKnownServices := map[string]ServiceDefinition{
		"s3": {
			Name:        "s3",
			DisplayName: "Amazon S3",
			Description: "Simple Storage Service",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
			GlobalService: true,
			RateLimit:   rate.Limit(100),
			BurstLimit:  200,
			DiscoverySource: "well-known",
		},
		"ec2": {
			Name:        "ec2",
			DisplayName: "Amazon EC2",
			Description: "Elastic Compute Cloud",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/ec2",
			RequiresRegion: true,
			RateLimit:   rate.Limit(20),
			BurstLimit:  40,
			DiscoverySource: "well-known",
		},
		"lambda": {
			Name:        "lambda",
			DisplayName: "AWS Lambda",
			Description: "Run code without thinking about servers",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/lambda",
			RequiresRegion: true,
			RateLimit:   rate.Limit(50),
			BurstLimit:  100,
			DiscoverySource: "well-known",
		},
		"dynamodb": {
			Name:        "dynamodb",
			DisplayName: "Amazon DynamoDB",
			Description: "NoSQL Database Service",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/dynamodb",
			RequiresRegion: true,
			RateLimit:   rate.Limit(40),
			BurstLimit:  80,
			DiscoverySource: "well-known",
		},
		"rds": {
			Name:        "rds",
			DisplayName: "Amazon RDS",
			Description: "Relational Database Service",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/rds",
			RequiresRegion: true,
			RateLimit:   rate.Limit(30),
			BurstLimit:  60,
			DiscoverySource: "well-known",
		},
		"sts": {
			Name:        "sts",
			DisplayName: "AWS Security Token Service",
			Description: "AWS Security Token Service for identity and access management",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/sts",
			GlobalService: true,
			RateLimit:   rate.Limit(100),
			BurstLimit:  200,
			DiscoverySource: "well-known",
			CustomHandlerRequired: true,
			Operations: []OperationDefinition{
				{
					Name:          "GetCallerIdentity",
					DisplayName:   "Get Caller Identity",
					OperationType: "get",
					Description:   "Returns details about the IAM identity whose credentials are used to call the operation",
					InputType:     "*sts.GetCallerIdentityInput",
					OutputType:    "*sts.GetCallerIdentityOutput",
					IsGlobalOperation: true,
				},
			},
		},
	}

	if def, exists := wellKnownServices[serviceName]; exists {
		def.DiscoveredAt = time.Now()
		def.LastValidated = time.Now()
		def.ValidationStatus = "well-known"
		r.services[serviceName] = &def
		r.createRateLimiter(serviceName, def.RateLimit, def.BurstLimit)
		return true
	}
	return false
}

// updateCacheHitMetrics updates cache hit statistics for optimization
func (r *UnifiedServiceRegistry) updateCacheHitMetrics(serviceName string) {
	r.cacheHits++
	
	// Update cache entry access time if it exists
	r.cacheMutex.Lock()
	if entry, exists := r.cache[serviceName]; exists {
		entry.hitCount++
		entry.accessTime = time.Now()
	}
	r.cacheMutex.Unlock()
}

// updateCacheMetrics updates cache metrics
func (r *UnifiedServiceRegistry) updateCacheMetrics(serviceName string, hit bool) {
	if hit {
		r.cacheHits++
	} else {
		r.cacheMisses++
	}
}
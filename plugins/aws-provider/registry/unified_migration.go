package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"golang.org/x/time/rate"
)

// Migration utilities for transitioning from the three separate registries to UnifiedServiceRegistry

// MigrationReport tracks the results of a migration operation
type MigrationReport struct {
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	ServicesTotal   int
	ServicesSuccess int
	ServicesSkipped int
	ServicesFailed  int
	Errors          []MigrationError
	Summary         string
}

// MigrationError represents an error during migration
type MigrationError struct {
	ServiceName string
	Component   string // "registry", "factory", "scanner"
	Error       string
	Timestamp   time.Time
}

// MigrationOptions configures migration behavior
type MigrationOptions struct {
	SkipExisting       bool // Skip services that already exist
	OverwriteExisting  bool // Overwrite existing services
	ValidateAfter      bool // Validate registry after migration
	CreateBackup       bool // Create backup before migration
	DryRun            bool // Don't actually perform migration
}

// LegacyClientFactory interface to avoid import cycles
type LegacyClientFactory interface {
	ListAvailableServices() []string
	GetServiceInfo(serviceName string) (*LegacyServiceInfo, error)
}

// LegacyServiceInfo represents service info from the legacy factory
type LegacyServiceInfo struct {
	Name                    string
	DisplayName             string
	Description             string
	PackagePath             string
	ClientType              string
	RateLimit               float64
	BurstLimit              int
	GlobalService           bool
	RequiresRegion          bool
	SupportsPagination      bool
	SupportsResourceExplorer bool
}

// LegacyScannerRegistry interface to avoid import cycles
type LegacyScannerRegistry interface {
	ListServices() []string
	GetMetadata(serviceName string) (*LegacyScannerMetadata, bool)
}

// LegacyScannerMetadata represents scanner metadata from legacy registry
type LegacyScannerMetadata struct {
	ServiceName          string
	ResourceTypes        []string
	SupportsPagination   bool
	SupportsParallelScan bool
	RequiredPermissions  []string
	RateLimit           rate.Limit
	BurstLimit          int
}

// MigrateFromLegacyRegistries migrates from all three legacy registry systems
func (r *UnifiedServiceRegistry) MigrateFromLegacyRegistries(
	dynamicRegistry DynamicServiceRegistry,
	clientFactory LegacyClientFactory,
	scannerRegistry LegacyScannerRegistry,
	options MigrationOptions,
) (*MigrationReport, error) {

	report := &MigrationReport{
		StartTime: time.Now(),
		Errors:    make([]MigrationError, 0),
	}

	fmt.Println("Starting migration from legacy registry systems...")

	// Create backup if requested
	if options.CreateBackup && !options.DryRun {
		if err := r.CreateBackup(); err != nil {
			fmt.Printf("Warning: Failed to create backup: %v\n", err)
		}
	}

	// Step 1: Migrate from DynamicServiceRegistry
	if dynamicRegistry != nil {
		fmt.Println("Migrating from DynamicServiceRegistry...")
		if err := r.migrateDynamicRegistry(dynamicRegistry, options, report); err != nil {
			return report, fmt.Errorf("failed to migrate DynamicServiceRegistry: %w", err)
		}
	}

	// Step 2: Migrate client factories
	if clientFactory != nil {
		fmt.Println("Migrating client factory information...")
		if err := r.migrateClientFactory(clientFactory, options, report); err != nil {
			return report, fmt.Errorf("failed to migrate client factory: %w", err)
		}
	}

	// Step 3: Migrate scanner metadata
	if scannerRegistry != nil {
		fmt.Println("Migrating scanner registry metadata...")
		if err := r.migrateScannerRegistry(scannerRegistry, options, report); err != nil {
			return report, fmt.Errorf("failed to migrate scanner registry: %w", err)
		}
	}

	// Validate if requested
	if options.ValidateAfter && !options.DryRun {
		fmt.Println("Validating migrated registry...")
		if errors := r.ValidateRegistry(); len(errors) > 0 {
			for _, err := range errors {
				report.Errors = append(report.Errors, MigrationError{
					Component: "validation",
					Error:     err.Error(),
					Timestamp: time.Now(),
				})
			}
		}
	}

	// Finalize report
	report.EndTime = time.Now()
	report.Duration = report.EndTime.Sub(report.StartTime)
	report.Summary = fmt.Sprintf("Migrated %d/%d services successfully (%d failed, %d skipped)",
		report.ServicesSuccess, report.ServicesTotal, report.ServicesFailed, report.ServicesSkipped)

	fmt.Printf("Migration completed: %s\n", report.Summary)
	return report, nil
}

// migrateDynamicRegistry migrates service definitions from DynamicServiceRegistry
func (r *UnifiedServiceRegistry) migrateDynamicRegistry(
	oldRegistry DynamicServiceRegistry,
	options MigrationOptions,
	report *MigrationReport,
) error {

	definitions := oldRegistry.ListServiceDefinitions()
	report.ServicesTotal = len(definitions)

	for _, def := range definitions {
		// Check if service already exists
		if _, exists := r.GetService(def.Name); exists {
			if options.SkipExisting && !options.OverwriteExisting {
				report.ServicesSkipped++
				continue
			}
		}

		// Perform migration
		if !options.DryRun {
			if err := r.RegisterService(def); err != nil {
				report.ServicesFailed++
				report.Errors = append(report.Errors, MigrationError{
					ServiceName: def.Name,
					Component:   "registry",
					Error:       err.Error(),
					Timestamp:   time.Now(),
				})
				continue
			}
		}

		report.ServicesSuccess++
		fmt.Printf("  Migrated service: %s\n", def.Name)
	}

	return nil
}

// migrateClientFactory extracts factory information from LegacyClientFactory
func (r *UnifiedServiceRegistry) migrateClientFactory(
	clientFactory LegacyClientFactory,
	options MigrationOptions,
	report *MigrationReport,
) error {

	// Get available services from factory
	services := clientFactory.ListAvailableServices()

	for _, serviceName := range services {
		// Get service info from factory
		serviceInfo, err := clientFactory.GetServiceInfo(serviceName)
		if err != nil {
			report.Errors = append(report.Errors, MigrationError{
				ServiceName: serviceName,
				Component:   "factory",
				Error:       fmt.Sprintf("failed to get service info: %v", err),
				Timestamp:   time.Now(),
			})
			continue
		}

		// Create or update service definition with factory information
		if !options.DryRun {
			if err := r.updateServiceFromFactory(serviceName, serviceInfo, options); err != nil {
				report.Errors = append(report.Errors, MigrationError{
					ServiceName: serviceName,
					Component:   "factory",
					Error:       err.Error(),
					Timestamp:   time.Now(),
				})
			}
		}

		fmt.Printf("  Migrated factory info for: %s\n", serviceName)
	}

	return nil
}

// migrateScannerRegistry extracts scanner metadata from LegacyScannerRegistry
func (r *UnifiedServiceRegistry) migrateScannerRegistry(
	scannerRegistry LegacyScannerRegistry,
	options MigrationOptions,
	report *MigrationReport,
) error {

	services := scannerRegistry.ListServices()

	for _, serviceName := range services {
		metadata, exists := scannerRegistry.GetMetadata(serviceName)
		if !exists {
			continue
		}

		// Update service with scanner metadata
		if !options.DryRun {
			if err := r.updateServiceFromScanner(serviceName, metadata, options); err != nil {
				report.Errors = append(report.Errors, MigrationError{
					ServiceName: serviceName,
					Component:   "scanner",
					Error:       err.Error(),
					Timestamp:   time.Now(),
				})
			}
		}

		fmt.Printf("  Migrated scanner metadata for: %s\n", serviceName)
	}

	return nil
}

// Helper methods for migration

func (r *UnifiedServiceRegistry) updateServiceFromFactory(
	serviceName string,
	serviceInfo *LegacyServiceInfo,
	options MigrationOptions,
) error {

	// Get existing service or create new one
	var def ServiceDefinition
	if existing, exists := r.GetService(serviceName); exists && !options.OverwriteExisting {
		def = *existing
	} else {
		def = ServiceDefinition{
			Name:             serviceName,
			DiscoverySource:  "factory_migration",
			DiscoveredAt:     time.Now(),
			ValidationStatus: "migrated",
		}
	}

	// Update with factory information
	def.DisplayName = serviceInfo.DisplayName
	def.Description = serviceInfo.Description
	def.PackagePath = serviceInfo.PackagePath
	def.ClientType = serviceInfo.ClientType
	def.RateLimit = rate.Limit(serviceInfo.RateLimit)
	def.BurstLimit = serviceInfo.BurstLimit
	def.GlobalService = serviceInfo.GlobalService
	def.RequiresRegion = serviceInfo.RequiresRegion
	def.SupportsPagination = serviceInfo.SupportsPagination
	def.SupportsResourceExplorer = serviceInfo.SupportsResourceExplorer

	return r.RegisterService(def)
}

func (r *UnifiedServiceRegistry) updateServiceFromScanner(
	serviceName string,
	metadata *LegacyScannerMetadata,
	options MigrationOptions,
) error {

	// Get existing service or create new one
	var def ServiceDefinition
	if existing, exists := r.GetService(serviceName); exists && !options.OverwriteExisting {
		def = *existing
	} else {
		def = ServiceDefinition{
			Name:             serviceName,
			DiscoverySource:  "scanner_migration",
			DiscoveredAt:     time.Now(),
			ValidationStatus: "migrated",
		}
	}

	// Update with scanner metadata
	def.RateLimit = metadata.RateLimit
	def.BurstLimit = metadata.BurstLimit
	def.SupportsPagination = metadata.SupportsPagination
	def.SupportsParallelScan = metadata.SupportsParallelScan
	def.Permissions = metadata.RequiredPermissions

	// Convert resource types
	for _, rtName := range metadata.ResourceTypes {
		def.ResourceTypes = append(def.ResourceTypes, ResourceTypeDefinition{
			Name: rtName,
		})
	}

	return r.RegisterService(def)
}

// ValidateRegistry validates all service definitions in the unified registry
func (r *UnifiedServiceRegistry) ValidateRegistry() []error {
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

// CreateMigrationScript generates a script to migrate to the unified registry
func CreateMigrationScript(
	awsConfig aws.Config,
	dynamicRegistry DynamicServiceRegistry,
	clientFactory LegacyClientFactory,
	scannerRegistry LegacyScannerRegistry,
) (*UnifiedServiceRegistry, error) {

	fmt.Println("Creating UnifiedServiceRegistry migration...")

	// Create unified registry with default configuration
	config := RegistryConfig{
		EnableCache:      true,
		CacheTTL:         15 * time.Minute,
		MaxCacheSize:     1000,
		AutoPersist:      true,
		PersistenceInterval: 5 * time.Minute,
		PersistencePath:  "/tmp/corkscrew-unified-registry.json",
		EnableMetrics:    true,
		EnableAuditLog:   true,
	}

	unifiedRegistry := NewUnifiedServiceRegistry(awsConfig, config)

	// Set scanner if available
	if scannerRegistry != nil {
		// Note: We need to adapt this to the actual scanner interface
		fmt.Println("Scanner registry integration pending - needs UnifiedScannerProvider implementation")
	}

	// Migrate from legacy systems
	options := MigrationOptions{
		SkipExisting:      false,
		OverwriteExisting: true,
		ValidateAfter:     true,
		CreateBackup:      true,
		DryRun:           false,
	}

	report, err := unifiedRegistry.MigrateFromLegacyRegistries(
		dynamicRegistry,
		clientFactory,
		scannerRegistry,
		options,
	)

	if err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	fmt.Printf("Migration completed successfully: %s\n", report.Summary)
	if len(report.Errors) > 0 {
		fmt.Printf("Migration had %d errors:\n", len(report.Errors))
		for _, migErr := range report.Errors {
			fmt.Printf("  - %s (%s): %s\n", migErr.ServiceName, migErr.Component, migErr.Error)
		}
	}

	return unifiedRegistry, nil
}

// DemoMigration demonstrates the migration process
func DemoMigration(ctx context.Context, awsConfig aws.Config) error {
	fmt.Println("=== UnifiedServiceRegistry Migration Demo ===")

	// Create the existing registries (these would be your actual instances)
	registryConfig := RegistryConfig{
		EnableCache:  true,
		CacheTTL:     5 * time.Minute,
		AutoPersist:  false,
	}
	
	// Create AWS config for unified registry (using passed awsConfig)
	dynamicRegistry := NewUnifiedServiceRegistry(awsConfig, registryConfig)
	// clientFactory := factory.NewUnifiedClientFactory(awsConfig, dynamicRegistry)
	// scannerRegistry := runtime.NewScannerRegistry()

	// For demonstration, let's add some test services to the dynamic registry
	testServices := []ServiceDefinition{
		{
			Name:        "s3",
			DisplayName: "Amazon S3",
			Description: "Simple Storage Service",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
			RateLimit:   rate.Limit(100),
			BurstLimit:  200,
			GlobalService: true,
			ResourceTypes: []ResourceTypeDefinition{
				{Name: "Bucket", ResourceType: "AWS::S3::Bucket"},
			},
			DiscoverySource: "test",
		},
		{
			Name:        "ec2",
			DisplayName: "Amazon EC2",
			Description: "Elastic Compute Cloud",
			PackagePath: "github.com/aws/aws-sdk-go-v2/service/ec2",
			RateLimit:   rate.Limit(20),
			BurstLimit:  40,
			RequiresRegion: true,
			ResourceTypes: []ResourceTypeDefinition{
				{Name: "Instance", ResourceType: "AWS::EC2::Instance"},
			},
			DiscoverySource: "test",
		},
	}

	for _, service := range testServices {
		if err := dynamicRegistry.RegisterService(service); err != nil {
			return fmt.Errorf("failed to register test service %s: %w", service.Name, err)
		}
	}

	fmt.Printf("Created test dynamic registry with %d services\n", len(testServices))

	// Create unified registry and migrate
	unified, err := CreateMigrationScript(awsConfig, dynamicRegistry, nil, nil)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Demonstrate unified registry capabilities
	fmt.Println("\n=== Unified Registry Capabilities ===")
	
	services := unified.ListServices()
	fmt.Printf("Available services: %v\n", services)

	// Get service information
	for _, serviceName := range services {
		if service, exists := unified.GetService(serviceName); exists {
			fmt.Printf("Service %s: %s (Rate: %.0f/s, Burst: %d)\n",
				service.Name, service.DisplayName, float64(service.RateLimit), service.BurstLimit)
		}
	}

	// Test client creation (this would create actual clients in real usage)
	for _, serviceName := range services {
		client, err := unified.CreateClient(ctx, serviceName)
		if err != nil {
			fmt.Printf("Failed to create client for %s: %v\n", serviceName, err)
		} else {
			fmt.Printf("Created client for %s: %T\n", serviceName, client)
		}
	}

	// Show statistics
	stats := unified.GetStats()
	fmt.Printf("\nRegistry Statistics:\n")
	fmt.Printf("  Total Services: %d\n", stats.TotalServices)
	fmt.Printf("  Total Resource Types: %d\n", stats.TotalResourceTypes)
	fmt.Printf("  Cache Hit Rate: %.2f%%\n", stats.CacheHitRate*100)

	fmt.Println("\n=== Migration Demo Completed Successfully ===")
	return nil
}
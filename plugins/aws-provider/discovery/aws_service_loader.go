package discovery

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/jlgore/corkscrew/internal/pool"
	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// ServiceLoader handles dynamic loading of AWS service clients
type ServiceLoader struct {
	mu            sync.RWMutex
	loadedClients map[string]interface{}
	loadedPlugins map[string]*plugin.Plugin
	pluginDir     string
	tempDir       string
	analyzer      *generator.AWSAnalyzer
	clientPool    *pool.MultiServicePool
}

// LoadedService represents a dynamically loaded AWS service
type LoadedService struct {
	Name     string
	Client   interface{}
	Plugin   *plugin.Plugin
	Metadata *generator.AWSServiceInfo
	LoadedAt time.Time
}

// NewAWSServiceLoader creates a new AWS service loader
func NewAWSServiceLoader(pluginDir, tempDir string) *ServiceLoader {
	sl := &ServiceLoader{
		loadedClients: make(map[string]interface{}),
		loadedPlugins: make(map[string]*plugin.Plugin),
		pluginDir:     pluginDir,
		tempDir:       tempDir,
		analyzer:      generator.NewAWSAnalyzer(),
		clientPool:    pool.NewMultiServicePool(10, 30*time.Minute), // 10 clients per service, 30 min idle timeout
	}

	// Start client pool cleanup routines
	ctx := context.Background()
	sl.clientPool.StartCleanupRoutines(ctx, 5*time.Minute) // Cleanup every 5 minutes

	return sl
}

// LoadService dynamically loads an AWS service client
func (sl *ServiceLoader) LoadService(ctx context.Context, serviceName string) (*LoadedService, error) {
	return sl.LoadServiceWithRegion(ctx, serviceName, "")
}

// LoadServiceWithRegion dynamically loads an AWS service client with a specific region using the client pool
func (sl *ServiceLoader) LoadServiceWithRegion(ctx context.Context, serviceName, region string) (*LoadedService, error) {
	// Create AWS config for this service and region
	cfg, err := sl.createAWSConfig(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS config: %w", err)
	}

	// Create client key for pooling
	clientKey := pool.ClientKey{
		Service: serviceName,
		Region:  region,
		Profile: os.Getenv("AWS_PROFILE"),
		RoleARN: "", // TODO: Add role ARN support if needed
	}

	// Create client factory for this service
	factory := func(ctx context.Context, key pool.ClientKey, cfg aws.Config) (interface{}, error) {
		return sl.createServiceClient(ctx, key.Service, cfg)
	}

	// Get client from pool (or create new one)
	client, err := sl.clientPool.GetClient(ctx, clientKey, cfg, factory)
	if err != nil {
		return nil, fmt.Errorf("failed to get pooled client for service %s: %w", serviceName, err)
	}

	// Analyze the service
	metadata, err := sl.analyzer.AnalyzeServiceFromReflection(serviceName, client)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze service %s: %w", serviceName, err)
	}

	// Get plugin instance if available
	var pluginInstance *plugin.Plugin
	sl.mu.RLock()
	if plugin, exists := sl.loadedPlugins[serviceName]; exists {
		pluginInstance = plugin
	}
	sl.mu.RUnlock()

	return &LoadedService{
		Name:     serviceName,
		Client:   client,
		Plugin:   pluginInstance,
		Metadata: metadata,
		LoadedAt: time.Now(),
	}, nil
}

// loadServicePlugin loads a service as a Go plugin
func (sl *ServiceLoader) loadServicePlugin(ctx context.Context, serviceName string) (interface{}, *plugin.Plugin, error) {
	return sl.loadServicePluginWithRegion(ctx, serviceName, "")
}

// loadServicePluginWithRegion loads a service as a Go plugin with region support
func (sl *ServiceLoader) loadServicePluginWithRegion(ctx context.Context, serviceName, region string) (interface{}, *plugin.Plugin, error) {
	// First try to load from existing plugin
	pluginPath := filepath.Join(sl.pluginDir, fmt.Sprintf("aws-%s.so", serviceName))
	if _, err := os.Stat(pluginPath); err == nil {
		return sl.loadExistingPluginWithRegion(pluginPath, serviceName, region)
	}

	// Generate and compile the plugin
	if err := sl.generateServicePlugin(ctx, serviceName); err != nil {
		return nil, nil, fmt.Errorf("failed to generate plugin: %w", err)
	}

	// Load the newly created plugin
	return sl.loadExistingPluginWithRegion(pluginPath, serviceName, region)
}

// loadExistingPlugin loads an existing plugin file
func (sl *ServiceLoader) loadExistingPlugin(pluginPath, serviceName string) (interface{}, *plugin.Plugin, error) {
	// Check if plugin is already loaded to avoid "plugin already loaded" error
	if existingPlugin, exists := sl.loadedPlugins[serviceName]; exists {
		// Plugin already loaded, just create a new client instance
		factorySymbol, err := existingPlugin.Lookup(fmt.Sprintf("New%sClient", strings.Title(serviceName)))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to find client factory in existing plugin: %w", err)
		}

		factory, ok := factorySymbol.(func() interface{})
		if !ok {
			return nil, nil, fmt.Errorf("invalid client factory signature")
		}

		client := factory()
		return client, existingPlugin, nil
	}

	// Load the plugin for the first time
	p, err := plugin.Open(pluginPath)
	if err != nil {
		// If plugin.Open fails with "already loaded", try to find it in our cache
		if strings.Contains(err.Error(), "plugin already loaded") {
			// This shouldn't happen if our caching is working correctly,
			// but let's handle it gracefully
			return nil, nil, fmt.Errorf("plugin %s already loaded but not in cache - this indicates a bug", pluginPath)
		}
		return nil, nil, fmt.Errorf("failed to open plugin %s: %w", pluginPath, err)
	}

	// Look for the client factory function
	factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClient", strings.Title(serviceName)))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find client factory in plugin: %w", err)
	}

	factory, ok := factorySymbol.(func() interface{})
	if !ok {
		return nil, nil, fmt.Errorf("invalid client factory signature")
	}

	client := factory()
	return client, p, nil
}

// loadExistingPluginWithRegion loads an existing plugin file with region support
func (sl *ServiceLoader) loadExistingPluginWithRegion(pluginPath, serviceName, region string) (interface{}, *plugin.Plugin, error) {
	// Check if plugin is already loaded to avoid "plugin already loaded" error
	if existingPlugin, exists := sl.loadedPlugins[serviceName]; exists {
		// Plugin already loaded, just create a new client instance

		// Try region-based factory first if region is provided
		if region != "" {
			factorySymbol, err := existingPlugin.Lookup(fmt.Sprintf("New%sClientWithRegion", strings.Title(serviceName)))
			if err == nil {
				factory, ok := factorySymbol.(func(string) interface{})
				if ok {
					client := factory(region)
					return client, existingPlugin, nil
				}
			}
		}

		// Fallback to original factory
		factorySymbol, err := existingPlugin.Lookup(fmt.Sprintf("New%sClient", strings.Title(serviceName)))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to find client factory in existing plugin: %w", err)
		}

		factory, ok := factorySymbol.(func() interface{})
		if !ok {
			return nil, nil, fmt.Errorf("invalid client factory signature")
		}

		client := factory()
		return client, existingPlugin, nil
	}

	// Load the plugin for the first time
	p, err := plugin.Open(pluginPath)
	if err != nil {
		// If plugin.Open fails with "already loaded", try to find it in our cache
		if strings.Contains(err.Error(), "plugin already loaded") {
			// This shouldn't happen if our caching is working correctly,
			// but let's handle it gracefully
			return nil, nil, fmt.Errorf("plugin %s already loaded but not in cache - this indicates a bug", pluginPath)
		}
		return nil, nil, fmt.Errorf("failed to open plugin %s: %w", pluginPath, err)
	}

	// Try region-based factory first if region is provided
	if region != "" {
		factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClientWithRegion", strings.Title(serviceName)))
		if err == nil {
			factory, ok := factorySymbol.(func(string) interface{})
			if ok {
				client := factory(region)
				return client, p, nil
			}
		}
	}

	// Fallback to original factory
	factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClient", strings.Title(serviceName)))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find client factory in plugin: %w", err)
	}

	factory, ok := factorySymbol.(func() interface{})
	if !ok {
		return nil, nil, fmt.Errorf("invalid client factory signature")
	}

	client := factory()
	return client, p, nil
}

// generateServicePlugin generates a Go plugin for the specified service
func (sl *ServiceLoader) generateServicePlugin(ctx context.Context, serviceName string) error {
	// Create temporary directory for plugin generation
	serviceDir := filepath.Join(sl.tempDir, serviceName)
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	// Generate plugin source code
	pluginCode := sl.generatePluginSource(serviceName)

	// Write plugin source
	sourceFile := filepath.Join(serviceDir, "main.go")
	if err := os.WriteFile(sourceFile, []byte(pluginCode), 0644); err != nil {
		return fmt.Errorf("failed to write plugin source: %w", err)
	}

	// Generate go.mod
	goModContent := sl.generateGoMod(serviceName)
	goModFile := filepath.Join(serviceDir, "go.mod")
	if err := os.WriteFile(goModFile, []byte(goModContent), 0644); err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	// Build the plugin
	pluginPath := filepath.Join(sl.pluginDir, fmt.Sprintf("aws-%s.so", serviceName))
	if err := sl.buildPlugin(ctx, serviceDir, pluginPath); err != nil {
		return fmt.Errorf("failed to build plugin: %w", err)
	}

	return nil
}

// generatePluginSource generates the Go source code for a service plugin
func (sl *ServiceLoader) generatePluginSource(serviceName string) string {
	titleCase := strings.Title(serviceName)

	return fmt.Sprintf(`package main

import (
	"context"
	"os"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/%s"
)

// New%sClient creates a new %s client (legacy - for backward compatibility)
func New%sClient() interface{} {
	// This will be called with proper AWS config in the actual implementation
	// For now, return a client that can be analyzed via reflection
	cfg := aws.Config{}
	return %s.NewFromConfig(cfg)
}

// New%sClientWithConfig creates a new %s client with provided config
func New%sClientWithConfig(cfg aws.Config) interface{} {
	return %s.NewFromConfig(cfg)
}

// New%sClientWithRegion creates a new %s client with specified region
func New%sClientWithRegion(region string) interface{} {
	// Try to load default config with the specified region and profile support
	cfg, err := config.LoadDefaultConfig(context.Background(), 
		config.WithRegion(region),
		config.WithSharedConfigProfile(os.Getenv("AWS_PROFILE")),
	)
	if err != nil {
		// Fallback to basic config with region if LoadDefaultConfig fails
		cfg = aws.Config{Region: region}
	}
	return %s.NewFromConfig(cfg)
}

// GetServiceName returns the service name
func GetServiceName() string {
	return "%s"
}

// GetPackagePath returns the package path
func GetPackagePath() string {
	return "github.com/aws/aws-sdk-go-v2/service/%s"
}
`, serviceName, titleCase, serviceName, titleCase, serviceName, titleCase, serviceName, titleCase, serviceName, titleCase, serviceName, titleCase, serviceName, serviceName, serviceName)
}

// generateGoMod generates go.mod content for the plugin
func (sl *ServiceLoader) generateGoMod(serviceName string) string {
	return fmt.Sprintf(`module aws-%s-plugin

go 1.24

require (
	github.com/aws/aws-sdk-go-v2 v1.36.3
	github.com/aws/aws-sdk-go-v2/config v1.29.14
	github.com/aws/aws-sdk-go-v2/service/%s latest
)
`, serviceName, serviceName)
}

// buildPlugin compiles the plugin
func (sl *ServiceLoader) buildPlugin(ctx context.Context, sourceDir, outputPath string) error {
	// Ensure plugin directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// First, run go mod tidy to resolve dependencies
	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = sourceDir
	tidyCmd.Env = append(os.Environ(), "CGO_ENABLED=1")

	tidyOutput, err := tidyCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run go mod tidy: %w\nOutput: %s", err, string(tidyOutput))
	}

	// Build the plugin
	cmd := exec.CommandContext(ctx, "go", "build", "-buildmode=plugin", "-o", outputPath, ".")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1") // Required for plugins

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build plugin: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// GetLoadedServices returns all currently loaded services
func (sl *ServiceLoader) GetLoadedServices() []string {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	services := make([]string, 0, len(sl.loadedClients))
	for name := range sl.loadedClients {
		services = append(services, name)
	}
	return services
}

// UnloadService unloads a service and frees resources
func (sl *ServiceLoader) UnloadService(serviceName string) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	delete(sl.loadedClients, serviceName)
	delete(sl.loadedPlugins, serviceName)

	return nil
}

// UnloadAllServices unloads all services
func (sl *ServiceLoader) UnloadAllServices() {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	sl.loadedClients = make(map[string]interface{})
	sl.loadedPlugins = make(map[string]*plugin.Plugin)
}

// IsServiceLoaded checks if a service is currently loaded
func (sl *ServiceLoader) IsServiceLoaded(serviceName string) bool {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	_, exists := sl.loadedClients[serviceName]
	return exists
}

// LoadMultipleServices loads multiple services concurrently
func (sl *ServiceLoader) LoadMultipleServices(ctx context.Context, serviceNames []string) (map[string]*LoadedService, []error) {
	results := make(map[string]*LoadedService)
	errors := make([]error, 0)

	// Use a channel to collect results
	type result struct {
		name    string
		service *LoadedService
		err     error
	}

	resultChan := make(chan result, len(serviceNames))

	// Load services concurrently
	for _, serviceName := range serviceNames {
		go func(name string) {
			service, err := sl.LoadService(ctx, name)
			resultChan <- result{name: name, service: service, err: err}
		}(serviceName)
	}

	// Collect results
	for i := 0; i < len(serviceNames); i++ {
		res := <-resultChan
		if res.err != nil {
			errors = append(errors, fmt.Errorf("failed to load %s: %w", res.name, res.err))
		} else {
			results[res.name] = res.service
		}
	}

	return results, errors
}

// GetServiceClient returns the client for a loaded service
func (sl *ServiceLoader) GetServiceClient(serviceName string) (interface{}, error) {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	client, exists := sl.loadedClients[serviceName]
	if !exists {
		return nil, fmt.Errorf("service %s not loaded", serviceName)
	}

	return client, nil
}

// AnalyzeLoadedService analyzes a loaded service and returns its metadata
func (sl *ServiceLoader) AnalyzeLoadedService(serviceName string) (*generator.AWSServiceInfo, error) {
	client, err := sl.GetServiceClient(serviceName)
	if err != nil {
		return nil, err
	}

	return sl.analyzer.AnalyzeServiceFromReflection(serviceName, client)
}

// CleanupTempFiles removes temporary files created during plugin generation
func (sl *ServiceLoader) CleanupTempFiles() error {
	if sl.tempDir == "" {
		return nil
	}

	return os.RemoveAll(sl.tempDir)
}

// GetPluginPath returns the path where a service plugin would be stored
func (sl *ServiceLoader) GetPluginPath(serviceName string) string {
	return filepath.Join(sl.pluginDir, fmt.Sprintf("aws-%s.so", serviceName))
}

// GetClientPoolStats returns statistics for all client pools
func (sl *ServiceLoader) GetClientPoolStats() map[string]pool.PoolStats {
	return sl.clientPool.GetAllStats()
}

// ClearClientPools clears all client pools (useful for testing)
func (sl *ServiceLoader) ClearClientPools() {
	sl.clientPool.ClearAll()
}

// PreloadPopularServices preloads commonly used AWS services
func (sl *ServiceLoader) PreloadPopularServices(ctx context.Context) error {
	popularServices := []string{
		"s3", "ec2", "lambda", "rds", "dynamodb", "iam",
	}

	log.Printf("Preloading %d popular AWS services...", len(popularServices))

	results, errors := sl.LoadMultipleServices(ctx, popularServices)

	log.Printf("Successfully preloaded %d services", len(results))

	if len(errors) > 0 {
		log.Printf("Failed to preload %d services:", len(errors))
		for _, err := range errors {
			log.Printf("  - %v", err)
		}
		return fmt.Errorf("failed to preload some services: %d errors", len(errors))
	}

	return nil
}

// AWSConfigOptions represents options for AWS configuration
type AWSConfigOptions struct {
	Region             string
	Profile            string
	AssumeRoleARN      string
	ExternalID         string
	SessionName        string
	EndpointURL        string
	MaxRetries         int
	Timeout            time.Duration
	InsecureSkipVerify bool
}

// createAWSConfig creates a proper AWS configuration with region and credentials
func (sl *ServiceLoader) createAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	opts := AWSConfigOptions{
		Region:  region,
		Profile: os.Getenv("AWS_PROFILE"),
	}
	return sl.CreateAWSConfigWithOptions(ctx, opts)
}

// createServiceClient creates an AWS service client using the plugin system or direct SDK
func (sl *ServiceLoader) createServiceClient(ctx context.Context, serviceName string, cfg aws.Config) (interface{}, error) {
	// First try to use existing plugin if available
	sl.mu.RLock()
	if plugin, exists := sl.loadedPlugins[serviceName]; exists {
		sl.mu.RUnlock()

		// Try config-based factory first
		factoryName := fmt.Sprintf("New%sClientWithConfig", strings.Title(serviceName))
		if factorySymbol, err := plugin.Lookup(factoryName); err == nil {
			if factory, ok := factorySymbol.(func(aws.Config) interface{}); ok {
				return factory(cfg), nil
			}
		}

		// Try region-based factory
		factoryName = fmt.Sprintf("New%sClientWithRegion", strings.Title(serviceName))
		if factorySymbol, err := plugin.Lookup(factoryName); err == nil {
			if factory, ok := factorySymbol.(func(string) interface{}); ok {
				return factory(cfg.Region), nil
			}
		}

		// Fallback to basic factory
		factoryName = fmt.Sprintf("New%sClient", strings.Title(serviceName))
		if factorySymbol, err := plugin.Lookup(factoryName); err == nil {
			if factory, ok := factorySymbol.(func() interface{}); ok {
				return factory(), nil
			}
		}
	} else {
		sl.mu.RUnlock()
	}

	// If no plugin available, try to create one
	if err := sl.generateServicePlugin(ctx, serviceName); err != nil {
		return nil, fmt.Errorf("failed to generate plugin for service %s: %w", serviceName, err)
	}

	// Load the newly created plugin
	pluginPath := sl.GetPluginPath(serviceName)
	client, plugin, err := sl.loadExistingPluginWithRegion(pluginPath, serviceName, cfg.Region)
	if err != nil {
		return nil, fmt.Errorf("failed to load generated plugin: %w", err)
	}

	// Cache the plugin
	sl.mu.Lock()
	sl.loadedPlugins[serviceName] = plugin
	sl.mu.Unlock()

	return client, nil
}

// CreateAWSConfigWithOptions creates AWS configuration with advanced options
func (sl *ServiceLoader) CreateAWSConfigWithOptions(ctx context.Context, opts AWSConfigOptions) (aws.Config, error) {
	var configOpts []func(*config.LoadOptions) error

	// Set profile if specified
	if opts.Profile != "" {
		configOpts = append(configOpts, config.WithSharedConfigProfile(opts.Profile))
	}

	// Set region if specified
	if opts.Region != "" {
		configOpts = append(configOpts, config.WithRegion(opts.Region))
	}

	// Set custom endpoint if specified (for LocalStack, etc.)
	if opts.EndpointURL != "" {
		customResolver := aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               opts.EndpointURL,
					HostnameImmutable: true,
					PartitionID:       "aws",
					SigningRegion:     region,
				}, nil
			})
		configOpts = append(configOpts, config.WithEndpointResolverWithOptions(customResolver))
	}

	// Set retry options
	if opts.MaxRetries > 0 {
		configOpts = append(configOpts, config.WithRetryMode(aws.RetryModeAdaptive))
		configOpts = append(configOpts, config.WithRetryMaxAttempts(opts.MaxRetries))
	}

	// Load the base config
	cfg, err := config.LoadDefaultConfig(ctx, configOpts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Handle role assumption if specified
	if opts.AssumeRoleARN != "" {
		cfg, err = sl.assumeRole(ctx, cfg, opts)
		if err != nil {
			return aws.Config{}, fmt.Errorf("failed to assume role: %w", err)
		}
	}

	// Set timeout if specified
	if opts.Timeout > 0 {
		// AWS SDK v2 doesn't have NewBuildableHTTPClient
		// Timeout is handled at the service client level
	}

	// Note: InsecureSkipVerify would need to be handled differently in AWS SDK v2
	// For now, we'll skip this feature

	return cfg, nil
}

// assumeRole assumes an IAM role and returns updated config
func (sl *ServiceLoader) assumeRole(ctx context.Context, cfg aws.Config, opts AWSConfigOptions) (aws.Config, error) {
	// Import required for STS
	// Note: This requires adding github.com/aws/aws-sdk-go-v2/service/sts to imports
	stsClient := sts.NewFromConfig(cfg)

	// Create assume role provider options
	assumeRoleOpts := func(o *stscreds.AssumeRoleOptions) {
		if opts.ExternalID != "" {
			o.ExternalID = &opts.ExternalID
		}
		if opts.SessionName != "" {
			o.RoleSessionName = opts.SessionName
		} else {
			o.RoleSessionName = fmt.Sprintf("corkscrew-%d", time.Now().Unix())
		}
	}

	// Create credentials provider
	creds := stscreds.NewAssumeRoleProvider(stsClient, opts.AssumeRoleARN, assumeRoleOpts)

	// Update config with new credentials
	cfg.Credentials = aws.NewCredentialsCache(creds)

	return cfg, nil
}

// ValidateAWSCredentials validates AWS credentials are working
func (sl *ServiceLoader) ValidateAWSCredentials(ctx context.Context, cfg aws.Config) error {
	// Import required for STS
	stsClient := sts.NewFromConfig(cfg)

	// Try to get caller identity
	_, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("credential validation failed: %w", err)
	}

	return nil
}

// ValidatePluginEnvironment checks if the environment supports Go plugins
func (sl *ServiceLoader) ValidatePluginEnvironment() error {
	// Check if CGO is available
	cmd := exec.Command("go", "env", "CGO_ENABLED")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check CGO status: %w", err)
	}

	if strings.TrimSpace(string(output)) != "1" {
		return fmt.Errorf("CGO is not enabled, required for Go plugins")
	}

	// Check if we can build plugins
	testDir := filepath.Join(sl.tempDir, "plugin-test")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return fmt.Errorf("failed to create test directory: %w", err)
	}
	defer os.RemoveAll(testDir)

	testSource := `package main
func TestFunc() string { return "test" }
`
	testFile := filepath.Join(testDir, "test.go")
	if err := os.WriteFile(testFile, []byte(testSource), 0644); err != nil {
		return fmt.Errorf("failed to write test file: %w", err)
	}

	testPlugin := filepath.Join(testDir, "test.so")
	cmd = exec.Command("go", "build", "-buildmode=plugin", "-o", testPlugin, testFile)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build test plugin: %w", err)
	}

	return nil
}

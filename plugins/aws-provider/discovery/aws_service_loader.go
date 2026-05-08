package discovery

import (
	"context"
	"fmt"
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

// ServiceLoader handles dynamic loading of AWS service clients via the
// build-mode=plugin pipeline. Clients are pooled per (service, region, profile).
type ServiceLoader struct {
	mu            sync.RWMutex
	loadedPlugins map[string]*plugin.Plugin
	pluginDir     string
	tempDir       string
	analyzer      *generator.AWSAnalyzer
	clientPool    *pool.MultiServicePool
}

// LoadedService represents a dynamically loaded AWS service.
type LoadedService struct {
	Name     string
	Client   interface{}
	Plugin   *plugin.Plugin
	Metadata *generator.AWSServiceInfo
	LoadedAt time.Time
}

// AWSConfigOptions captures the AWS config knobs CreateAWSConfigWithOptions accepts.
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

func NewAWSServiceLoader(pluginDir, tempDir string) *ServiceLoader {
	sl := &ServiceLoader{
		loadedPlugins: make(map[string]*plugin.Plugin),
		pluginDir:     pluginDir,
		tempDir:       tempDir,
		analyzer:      generator.NewAWSAnalyzer(),
		clientPool:    pool.NewMultiServicePool(10, 30*time.Minute),
	}

	ctx := context.Background()
	sl.clientPool.StartCleanupRoutines(ctx, 5*time.Minute)

	return sl
}

// LoadServiceWithRegion loads (or pulls from the pool) a client for the given
// service+region, generating a build-mode=plugin shim on demand if one is not
// already cached at pluginDir.
func (sl *ServiceLoader) LoadServiceWithRegion(ctx context.Context, serviceName, region string) (*LoadedService, error) {
	cfg, err := sl.createAWSConfig(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS config: %w", err)
	}

	clientKey := pool.ClientKey{
		Service: serviceName,
		Region:  region,
		Profile: os.Getenv("AWS_PROFILE"),
	}

	factory := func(ctx context.Context, key pool.ClientKey, cfg aws.Config) (interface{}, error) {
		return sl.createServiceClient(ctx, key.Service, cfg)
	}

	client, err := sl.clientPool.GetClient(ctx, clientKey, cfg, factory)
	if err != nil {
		return nil, fmt.Errorf("failed to get pooled client for service %s: %w", serviceName, err)
	}

	metadata, err := sl.analyzer.AnalyzeServiceFromReflection(serviceName, client)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze service %s: %w", serviceName, err)
	}

	var pluginInstance *plugin.Plugin
	sl.mu.RLock()
	if p, exists := sl.loadedPlugins[serviceName]; exists {
		pluginInstance = p
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

func (sl *ServiceLoader) loadExistingPluginWithRegion(pluginPath, serviceName, region string) (interface{}, *plugin.Plugin, error) {
	if existingPlugin, exists := sl.loadedPlugins[serviceName]; exists {
		if region != "" {
			factorySymbol, err := existingPlugin.Lookup(fmt.Sprintf("New%sClientWithRegion", strings.Title(serviceName)))
			if err == nil {
				if factory, ok := factorySymbol.(func(string) interface{}); ok {
					return factory(region), existingPlugin, nil
				}
			}
		}
		factorySymbol, err := existingPlugin.Lookup(fmt.Sprintf("New%sClient", strings.Title(serviceName)))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to find client factory in existing plugin: %w", err)
		}
		factory, ok := factorySymbol.(func() interface{})
		if !ok {
			return nil, nil, fmt.Errorf("invalid client factory signature")
		}
		return factory(), existingPlugin, nil
	}

	p, err := plugin.Open(pluginPath)
	if err != nil {
		if strings.Contains(err.Error(), "plugin already loaded") {
			return nil, nil, fmt.Errorf("plugin %s already loaded but not in cache - this indicates a bug", pluginPath)
		}
		return nil, nil, fmt.Errorf("failed to open plugin %s: %w", pluginPath, err)
	}

	if region != "" {
		factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClientWithRegion", strings.Title(serviceName)))
		if err == nil {
			if factory, ok := factorySymbol.(func(string) interface{}); ok {
				return factory(region), p, nil
			}
		}
	}
	factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClient", strings.Title(serviceName)))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find client factory in plugin: %w", err)
	}
	factory, ok := factorySymbol.(func() interface{})
	if !ok {
		return nil, nil, fmt.Errorf("invalid client factory signature")
	}
	return factory(), p, nil
}

func (sl *ServiceLoader) generateServicePlugin(ctx context.Context, serviceName string) error {
	serviceDir := filepath.Join(sl.tempDir, serviceName)
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	pluginCode := sl.generatePluginSource(serviceName)
	sourceFile := filepath.Join(serviceDir, "main.go")
	if err := os.WriteFile(sourceFile, []byte(pluginCode), 0644); err != nil {
		return fmt.Errorf("failed to write plugin source: %w", err)
	}

	goModContent := sl.generateGoMod(serviceName)
	goModFile := filepath.Join(serviceDir, "go.mod")
	if err := os.WriteFile(goModFile, []byte(goModContent), 0644); err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	pluginPath := filepath.Join(sl.pluginDir, fmt.Sprintf("aws-%s.so", serviceName))
	if err := sl.buildPlugin(ctx, serviceDir, pluginPath); err != nil {
		return fmt.Errorf("failed to build plugin: %w", err)
	}

	return nil
}

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

func New%sClient() interface{} {
	cfg := aws.Config{}
	return %s.NewFromConfig(cfg)
}

func New%sClientWithConfig(cfg aws.Config) interface{} {
	return %s.NewFromConfig(cfg)
}

func New%sClientWithRegion(region string) interface{} {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithSharedConfigProfile(os.Getenv("AWS_PROFILE")),
	)
	if err != nil {
		cfg = aws.Config{Region: region}
	}
	return %s.NewFromConfig(cfg)
}

func GetServiceName() string {
	return "%s"
}

func GetPackagePath() string {
	return "github.com/aws/aws-sdk-go-v2/service/%s"
}
`, serviceName, titleCase, serviceName, titleCase, serviceName, titleCase, serviceName, serviceName, serviceName)
}

func (sl *ServiceLoader) generateGoMod(serviceName string) string {
	return fmt.Sprintf(`module aws-%s-plugin

go 1.26.2

require (
	github.com/aws/aws-sdk-go-v2 v1.36.3
	github.com/aws/aws-sdk-go-v2/config v1.29.14
	github.com/aws/aws-sdk-go-v2/service/%s latest
)
`, serviceName, serviceName)
}

func (sl *ServiceLoader) buildPlugin(ctx context.Context, sourceDir, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = sourceDir
	tidyCmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	if tidyOutput, err := tidyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run go mod tidy: %w\nOutput: %s", err, string(tidyOutput))
	}

	cmd := exec.CommandContext(ctx, "go", "build", "-buildmode=plugin", "-o", outputPath, ".")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build plugin: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// CleanupTempFiles removes the temp directory used for plugin generation.
func (sl *ServiceLoader) CleanupTempFiles() error {
	if sl.tempDir == "" {
		return nil
	}
	return os.RemoveAll(sl.tempDir)
}

func (sl *ServiceLoader) getPluginPath(serviceName string) string {
	return filepath.Join(sl.pluginDir, fmt.Sprintf("aws-%s.so", serviceName))
}

func (sl *ServiceLoader) createAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	return sl.createAWSConfigWithOptions(ctx, AWSConfigOptions{
		Region:  region,
		Profile: os.Getenv("AWS_PROFILE"),
	})
}

func (sl *ServiceLoader) createServiceClient(ctx context.Context, serviceName string, cfg aws.Config) (interface{}, error) {
	sl.mu.RLock()
	if p, exists := sl.loadedPlugins[serviceName]; exists {
		sl.mu.RUnlock()

		if factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClientWithConfig", strings.Title(serviceName))); err == nil {
			if factory, ok := factorySymbol.(func(aws.Config) interface{}); ok {
				return factory(cfg), nil
			}
		}
		if factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClientWithRegion", strings.Title(serviceName))); err == nil {
			if factory, ok := factorySymbol.(func(string) interface{}); ok {
				return factory(cfg.Region), nil
			}
		}
		if factorySymbol, err := p.Lookup(fmt.Sprintf("New%sClient", strings.Title(serviceName))); err == nil {
			if factory, ok := factorySymbol.(func() interface{}); ok {
				return factory(), nil
			}
		}
	} else {
		sl.mu.RUnlock()
	}

	if err := sl.generateServicePlugin(ctx, serviceName); err != nil {
		return nil, fmt.Errorf("failed to generate plugin for service %s: %w", serviceName, err)
	}

	pluginPath := sl.getPluginPath(serviceName)
	client, p, err := sl.loadExistingPluginWithRegion(pluginPath, serviceName, cfg.Region)
	if err != nil {
		return nil, fmt.Errorf("failed to load generated plugin: %w", err)
	}

	sl.mu.Lock()
	sl.loadedPlugins[serviceName] = p
	sl.mu.Unlock()

	return client, nil
}

func (sl *ServiceLoader) createAWSConfigWithOptions(ctx context.Context, opts AWSConfigOptions) (aws.Config, error) {
	var configOpts []func(*config.LoadOptions) error

	if opts.Profile != "" {
		configOpts = append(configOpts, config.WithSharedConfigProfile(opts.Profile))
	}
	if opts.Region != "" {
		configOpts = append(configOpts, config.WithRegion(opts.Region))
	}
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
	if opts.MaxRetries > 0 {
		configOpts = append(configOpts, config.WithRetryMode(aws.RetryModeAdaptive))
		configOpts = append(configOpts, config.WithRetryMaxAttempts(opts.MaxRetries))
	}

	cfg, err := config.LoadDefaultConfig(ctx, configOpts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	if opts.AssumeRoleARN != "" {
		cfg, err = sl.assumeRole(ctx, cfg, opts)
		if err != nil {
			return aws.Config{}, fmt.Errorf("failed to assume role: %w", err)
		}
	}

	return cfg, nil
}

func (sl *ServiceLoader) assumeRole(ctx context.Context, cfg aws.Config, opts AWSConfigOptions) (aws.Config, error) {
	stsClient := sts.NewFromConfig(cfg)

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

	creds := stscreds.NewAssumeRoleProvider(stsClient, opts.AssumeRoleARN, assumeRoleOpts)
	cfg.Credentials = aws.NewCredentialsCache(creds)
	return cfg, nil
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ServiceConfig struct {
	Version   string                    `yaml:"version"`
	Providers map[string]ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	DiscoveryMode string              `yaml:"discovery_mode"` // manual, auto, hybrid
	Services      ServicesConfig      `yaml:"services"`
	ServiceGroups map[string][]string `yaml:"service_groups"`
	Analysis      AnalysisConfig      `yaml:"analysis"`
}

type ServicesConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type AnalysisConfig struct {
	SkipEmpty    bool   `yaml:"skip_empty"`
	Workers      int    `yaml:"workers"`
	CacheEnabled bool   `yaml:"cache_enabled"`
	CacheTTL     string `yaml:"cache_ttl"`
}

// LoadServiceConfig loads configuration from various sources
func LoadServiceConfig() (*ServiceConfig, error) {
	// Priority order: CLI args > env vars > config file > defaults
	
	// 1. Try to load from config file
	config, err := loadFromFile()
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}
	
	if config == nil {
		config = getDefaultConfig()
	}
	
	// 2. Override with environment variables
	applyEnvOverrides(config)
	
	// 3. Validate configuration
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	
	return config, nil
}

func loadFromFile() (*ServiceConfig, error) {
	// Check if custom config file is specified
	if configFile := os.Getenv("CORKSCREW_CONFIG_FILE"); configFile != "" {
		return loadConfigFile(configFile)
	}
	
	// Look for config file in standard locations
	locations := []string{
		"corkscrew.yaml",
		"corkscrew.yml",
		".corkscrew.yaml",
		".corkscrew.yml",
		filepath.Join(os.Getenv("HOME"), ".corkscrew", "config.yaml"),
	}
	
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loadConfigFile(loc)
		}
	}
	
	return nil, os.ErrNotExist
}

func loadConfigFile(path string) (*ServiceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	
	var config ServiceConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	
	return &config, nil
}

func getDefaultConfig() *ServiceConfig {
	return &ServiceConfig{
		Version: "1.0",
		Providers: map[string]ProviderConfig{
			"aws": {
				DiscoveryMode: "manual",
				Services: ServicesConfig{
					Include: []string{
						"ec2", "s3", "lambda", "rds", "dynamodb", "iam",
						"sqs", "sns", "ecs", "eks", "cloudformation",
						"cloudwatch", "route53", "elasticloadbalancing",
						"autoscaling", "kms", "secretsmanager", "ssm",
					},
				},
				Analysis: AnalysisConfig{
					SkipEmpty:    true,
					Workers:      4,
					CacheEnabled: true,
					CacheTTL:     "24h",
				},
			},
		},
	}
}

func applyEnvOverrides(config *ServiceConfig) {
	// Override services from environment
	if services := os.Getenv("CORKSCREW_AWS_SERVICES"); services != "" {
		serviceList := strings.Split(services, ",")
		for i, svc := range serviceList {
			serviceList[i] = strings.TrimSpace(svc)
		}
		if awsProv, ok := config.Providers["aws"]; ok {
			awsProv.Services.Include = serviceList
			config.Providers["aws"] = awsProv
		}
	}
	
	// Override discovery mode
	if mode := os.Getenv("CORKSCREW_DISCOVERY_MODE"); mode != "" {
		if awsProv, ok := config.Providers["aws"]; ok {
			awsProv.DiscoveryMode = mode
			config.Providers["aws"] = awsProv
		}
	}
}

// GetServicesForProvider returns the final list of services to analyze
func (c *ServiceConfig) GetServicesForProvider(provider string) ([]string, error) {
	prov, ok := c.Providers[provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not configured", provider)
	}
	
	services := make(map[string]bool)
	
	switch prov.DiscoveryMode {
	case "manual":
		// Only use explicitly included services
		for _, svc := range prov.Services.Include {
			services[svc] = true
		}
		
	case "auto":
		// Auto-discover from go.mod and AWS SDK
		discovered, err := discoverServices()
		if err != nil {
			return nil, fmt.Errorf("failed to auto-discover services: %w", err)
		}
		for _, svc := range discovered {
			services[svc] = true
		}
		
	case "hybrid":
		// Start with manual list, add auto-discovered
		for _, svc := range prov.Services.Include {
			services[svc] = true
		}
		discovered, _ := discoverServices()
		for _, svc := range discovered {
			services[svc] = true
		}
		
	default:
		return nil, fmt.Errorf("unknown discovery mode: %s", prov.DiscoveryMode)
	}
	
	// Apply exclusions
	for _, svc := range prov.Services.Exclude {
		delete(services, svc)
	}
	
	// Convert to slice
	result := make([]string, 0, len(services))
	for svc := range services {
		result = append(result, svc)
	}
	
	return result, nil
}

// GetServiceGroup returns services in a named group
func (c *ServiceConfig) GetServiceGroup(provider, group string) ([]string, error) {
	prov, ok := c.Providers[provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not configured", provider)
	}
	
	services, ok := prov.ServiceGroups[group]
	if !ok {
		return nil, fmt.Errorf("service group %s not found", group)
	}
	
	return services, nil
}

func validateConfig(config *ServiceConfig) error {
	if config.Version == "" {
		config.Version = "1.0"
	}
	
	// Validate provider configurations
	for name, prov := range config.Providers {
		// Validate discovery mode
		validModes := map[string]bool{"manual": true, "auto": true, "hybrid": true}
		if !validModes[prov.DiscoveryMode] {
			return fmt.Errorf("invalid discovery mode '%s' for provider %s", prov.DiscoveryMode, name)
		}
		
		// Ensure analysis config has sensible defaults
		if prov.Analysis.Workers <= 0 {
			prov.Analysis.Workers = 4
		}
		if prov.Analysis.CacheTTL == "" {
			prov.Analysis.CacheTTL = "24h"
		}
		
		// Validate cache TTL format
		if _, err := time.ParseDuration(prov.Analysis.CacheTTL); err != nil {
			return fmt.Errorf("invalid cache TTL format: %w", err)
		}
		
		config.Providers[name] = prov
	}
	
	return nil
}

// InitializeConfigFile creates a default configuration file
func InitializeConfigFile() error {
	// Check if config already exists
	locations := []string{
		"corkscrew.yaml",
		"corkscrew.yml",
		".corkscrew.yaml",
		".corkscrew.yml",
	}
	
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return fmt.Errorf("configuration file already exists at %s", loc)
		}
	}
	
	// Create example configuration
	exampleConfig := `# Corkscrew Configuration
version: "1.0"

providers:
  aws:
    # Service discovery mode: manual, auto, hybrid
    discovery_mode: hybrid
    
    # Manually specified services
    services:
      include:
        # Core services (current hardcoded list)
        - ec2
        - s3
        - iam
        - lambda
        - rds
        - dynamodb
        - sqs
        - sns
        - ecs
        - eks
        - cloudformation
        - cloudwatch
        - route53
        - elasticloadbalancing
        - autoscaling
        - kms
        - secretsmanager
        - ssm
        
        # Additional commonly used services
        - athena
        - glue
        - kinesis
        - elasticache
        - apigateway
        - cloudtrail
        - eventbridge
        - stepfunctions
        - sagemaker
        - cloudfront
        
      # Services to exclude (useful in auto mode)
      exclude: []
        # - chatbot  # Rarely used
        # - gamelift # Gaming specific
    
    # Service groups for convenience
    service_groups:
      core:
        - ec2
        - s3
        - iam
        - vpc
        
      compute:
        - lambda
        - ecs
        - eks
        - batch
        - lightsail
        
      data:
        - rds
        - dynamodb
        - elasticache
        - redshift
        - athena
        - glue
        
      networking:
        - vpc
        - route53
        - cloudfront
        - elasticloadbalancing
        - apigateway
        
      security:
        - iam
        - kms
        - secretsmanager
        - guardduty
        - shield
        
      monitoring:
        - cloudwatch
        - cloudtrail
        - xray
        - cloudwatchlogs
    
    # Analysis configuration
    analysis:
      # Skip services with no resources
      skip_empty: true
      
      # Parallel analysis workers
      workers: 4
      
      # Cache analysis results
      cache_enabled: true
      cache_ttl: 24h
`
	
	// Write to corkscrew.yaml
	if err := os.WriteFile("corkscrew.yaml", []byte(exampleConfig), 0644); err != nil {
		return fmt.Errorf("failed to write configuration file: %w", err)
	}
	
	fmt.Println("Created corkscrew.yaml with example configuration")
	return nil
}
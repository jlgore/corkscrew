package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultConfigFileName = "corkscrew.yaml"

type CorkscrewConfig struct {
	Version      string                         `yaml:"version"`
	Providers    map[string]CloudProviderConfig `yaml:"providers"`
	Dependencies DependenciesConfig             `yaml:"dependencies"`
	Database     DatabaseConfig                 `yaml:"database"`
	Query        QueryConfig                    `yaml:"query"`
	Compliance   ComplianceConfig               `yaml:"compliance"`
	Logging      LoggingConfig                  `yaml:"logging"`
	Output       OutputConfig                   `yaml:"output"`
}

type CloudProviderConfig struct {
	Enabled       bool     `yaml:"enabled"`
	DefaultRegion string   `yaml:"default_region,omitempty"`
	Regions       []string `yaml:"regions,omitempty"`
	Services      []string `yaml:"services"`
}

type DependenciesConfig struct {
	Protoc DependencyConfig `yaml:"protoc"`
	DuckDB DependencyConfig `yaml:"duckdb"`
}

type DependencyConfig struct {
	Version      string `yaml:"version"`
	AutoDownload bool   `yaml:"auto_download"`
}

type DatabaseConfig struct {
	Path       string `yaml:"path"`
	AutoCreate bool   `yaml:"auto_create,omitempty"`
}

type QueryConfig struct {
	Timeout            string `yaml:"timeout,omitempty"`
	StreamingThreshold int    `yaml:"streaming_threshold,omitempty"`
	MaxMemory          string `yaml:"max_memory,omitempty"`
}

type ComplianceConfig struct {
	PacksDir   string `yaml:"packs_dir,omitempty"`
	AutoUpdate bool   `yaml:"auto_update,omitempty"`
}

type LoggingConfig struct {
	Level  string `yaml:"level,omitempty"`
	Format string `yaml:"format,omitempty"`
}

type OutputConfig struct {
	DefaultFormat     string `yaml:"default_format"`
	Colors            bool   `yaml:"colors"`
	ProgressBars      bool   `yaml:"progress_bars"`
	HideEmptyRegions  bool   `yaml:"hide_empty_regions"`
	HideEmptyServices bool   `yaml:"hide_empty_services"`
}

const defaultConfigYAML = `# Corkscrew Configuration File
version: "2.0"

providers:
  aws:
    enabled: true
    regions:
      - "us-east-1"
      - "us-west-2"
    services:
      - s3
      - ec2
      - lambda
      - iam
      - rds
      - dynamodb

  azure:
    enabled: false
    regions:
      - "eastus"
    services:
      - storage
      - compute

  gcp:
    enabled: false
    regions:
      - "us-central1-a"
    services:
      - storage
      - compute

  kubernetes:
    enabled: false
    regions:
      - "default"
    services:
      - pods
      - services

dependencies:
  protoc:
    version: "25.3"
    auto_download: true
  duckdb:
    version: "1.5.2"
    auto_download: true

database:
  path: "~/.corkscrew/db/corkscrew.duckdb"
  auto_create: true

output:
  default_format: "table"
  colors: true
  progress_bars: true
  hide_empty_regions: true
  hide_empty_services: true
`

func DefaultCorkscrewYAML() string {
	return defaultConfigYAML
}

func DefaultCorkscrewConfig() *CorkscrewConfig {
	var cfg CorkscrewConfig
	if err := yaml.Unmarshal([]byte(defaultConfigYAML), &cfg); err != nil {
		panic(err)
	}
	cfg.ApplyDefaults()
	return &cfg
}

func ResolveCorkscrewConfigPath() (string, error) {
	if configFile := strings.TrimSpace(os.Getenv("CORKSCREW_CONFIG_FILE")); configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			return configFile, nil
		}
		return "", fmt.Errorf("config file set by CORKSCREW_CONFIG_FILE not found: %s", configFile)
	}

	homeDir, _ := os.UserHomeDir()
	candidates := []string{
		"corkscrew.yaml",
		"corkscrew.yml",
		".corkscrew.yaml",
		".corkscrew.yml",
		filepath.Join(homeDir, ".corkscrew", "config.yaml"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no configuration file found (looked for corkscrew.yaml, corkscrew.yml, .corkscrew.yaml, .corkscrew.yml, ~/.corkscrew/config.yaml)")
}

func LoadCorkscrewConfig(configPath string) (*CorkscrewConfig, error) {
	if strings.TrimSpace(configPath) == "" {
		resolved, err := ResolveCorkscrewConfigPath()
		if err != nil {
			return nil, err
		}
		configPath = resolved
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var cfg CorkscrewConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}
	cfg.ApplyDefaults()
	return &cfg, nil
}

func WriteDefaultCorkscrewConfig(path string) error {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigFileName
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory %s: %w", dir, err)
		}
	}
	return os.WriteFile(path, []byte(defaultConfigYAML), 0644)
}

func (c *CorkscrewConfig) ApplyDefaults() {
	if c.Version == "" {
		c.Version = "2.0"
	}
	if c.Providers == nil {
		c.Providers = DefaultCorkscrewConfig().Providers
	}
	for provider, providerConfig := range c.Providers {
		if len(providerConfig.Regions) == 0 && providerConfig.DefaultRegion != "" {
			providerConfig.Regions = []string{providerConfig.DefaultRegion}
		}
		if providerConfig.DefaultRegion == "" && len(providerConfig.Regions) > 0 {
			providerConfig.DefaultRegion = providerConfig.Regions[0]
		}
		c.Providers[provider] = providerConfig
	}
	if c.Dependencies.Protoc.Version == "" {
		c.Dependencies.Protoc = DependencyConfig{Version: "25.3", AutoDownload: true}
	}
	if c.Dependencies.DuckDB.Version == "" {
		c.Dependencies.DuckDB = DependencyConfig{Version: "1.5.2", AutoDownload: true}
	}
	if c.Output.DefaultFormat == "" {
		c.Output.DefaultFormat = "table"
	}
	if c.Database.Path == "" {
		c.Database.Path = "~/.corkscrew/db/corkscrew.duckdb"
	}
}

func (c *CorkscrewConfig) GetRegionsForProvider(provider string) ([]string, error) {
	providerConfig, exists := c.Providers[provider]
	if !exists {
		return nil, fmt.Errorf("provider %s not found in configuration", provider)
	}
	if !providerConfig.Enabled {
		return nil, fmt.Errorf("provider %s is disabled", provider)
	}
	if len(providerConfig.Regions) == 0 {
		return DefaultRegionsForProvider(provider), nil
	}
	return providerConfig.Regions, nil
}

func (c *CorkscrewConfig) GetServicesForProvider(provider string) ([]string, error) {
	providerConfig, exists := c.Providers[provider]
	if !exists {
		return nil, fmt.Errorf("provider %s not found in configuration", provider)
	}
	if !providerConfig.Enabled {
		return nil, fmt.Errorf("provider %s is disabled", provider)
	}
	return providerConfig.Services, nil
}

func (c *CorkscrewConfig) IsProviderEnabled(provider string) bool {
	providerConfig, exists := c.Providers[provider]
	return exists && providerConfig.Enabled
}

func (c *CorkscrewConfig) ShouldHideEmptyRegions() bool {
	return c.Output.HideEmptyRegions
}

func (c *CorkscrewConfig) ShouldHideEmptyServices() bool {
	return c.Output.HideEmptyServices
}

func ValidateProviderName(provider string) error {
	validProviders := map[string]bool{
		"aws":        true,
		"azure":      true,
		"gcp":        true,
		"kubernetes": true,
	}
	if validProviders[provider] {
		return nil
	}
	return fmt.Errorf("unsupported provider: %s. Valid providers: %v", provider, []string{"aws", "azure", "gcp", "kubernetes"})
}

func DefaultRegionsForProvider(provider string) []string {
	switch provider {
	case "aws":
		return []string{"us-east-1", "us-west-2"}
	case "azure":
		return []string{"eastus", "westus2"}
	case "gcp":
		return []string{"us-central1-a", "us-west1-a"}
	case "kubernetes":
		return []string{"default"}
	default:
		return []string{}
	}
}

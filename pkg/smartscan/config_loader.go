package smartscan

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type SmartScanConfiguration struct {
	Version   string                        `yaml:"version"`
	Providers map[string]ProviderConfig     `yaml:"providers"`
	Output    OutputConfig                  `yaml:"output"`
}

type ProviderConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Regions  []string `yaml:"regions"`
	Services []string `yaml:"services"`
}

type OutputConfig struct {
	DefaultFormat     string `yaml:"default_format"`
	Colors            bool   `yaml:"colors"`
	ProgressBars      bool   `yaml:"progress_bars"`
	HideEmptyRegions  bool   `yaml:"hide_empty_regions"`
	HideEmptyServices bool   `yaml:"hide_empty_services"`
}

func LoadSmartScanConfig(configPath string) (*SmartScanConfiguration, error) {
	if configPath == "" {
		// Try to find config in common locations
		candidates := []string{
			"corkscrew.yaml",
			"corkscrew.yml",
			filepath.Join(os.Getenv("HOME"), ".corkscrew", "config.yaml"),
			filepath.Join(".", "corkscrew.yaml"),
		}

		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				configPath = candidate
				break
			}
		}

		if configPath == "" {
			return nil, fmt.Errorf("no configuration file found in standard locations")
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config SmartScanConfiguration
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	// Apply defaults
	config.applyDefaults()

	return &config, nil
}

func (c *SmartScanConfiguration) applyDefaults() {
	// Apply default output settings if not specified
	if c.Output.DefaultFormat == "" {
		c.Output.DefaultFormat = "table"
	}
}

func (c *SmartScanConfiguration) GetRegionsForProvider(provider string) ([]string, error) {
	providerConfig, exists := c.Providers[provider]
	if !exists {
		return nil, fmt.Errorf("provider %s not found in configuration", provider)
	}

	if !providerConfig.Enabled {
		return nil, fmt.Errorf("provider %s is disabled", provider)
	}

	if len(providerConfig.Regions) == 0 {
		// Return default regions based on provider
		return c.getDefaultRegions(provider), nil
	}

	return providerConfig.Regions, nil
}

func (c *SmartScanConfiguration) GetServicesForProvider(provider string) ([]string, error) {
	providerConfig, exists := c.Providers[provider]
	if !exists {
		return nil, fmt.Errorf("provider %s not found in configuration", provider)
	}

	if !providerConfig.Enabled {
		return nil, fmt.Errorf("provider %s is disabled", provider)
	}

	return providerConfig.Services, nil
}

func (c *SmartScanConfiguration) getDefaultRegions(provider string) []string {
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

func (c *SmartScanConfiguration) IsProviderEnabled(provider string) bool {
	providerConfig, exists := c.Providers[provider]
	if !exists {
		return false
	}
	return providerConfig.Enabled
}

func (c *SmartScanConfiguration) ShouldHideEmptyRegions() bool {
	return c.Output.HideEmptyRegions
}

func (c *SmartScanConfiguration) ShouldHideEmptyServices() bool {
	return c.Output.HideEmptyServices
}

func (c *SmartScanConfiguration) GetSmartScanConfig(provider string) *SmartScanConfig {
	regions, _ := c.GetRegionsForProvider(provider)
	
	return &SmartScanConfig{
		HideEmptyRegions:  c.ShouldHideEmptyRegions(),
		HideEmptyServices: c.ShouldHideEmptyServices(),
		MaxConcurrency:    3, // Could be made configurable
		PreferredRegions:  c.getPreferredRegions(provider, regions),
	}
}

func (c *SmartScanConfiguration) getPreferredRegions(provider string, configuredRegions []string) []string {
	// If regions are explicitly configured, use first few as preferred
	if len(configuredRegions) > 0 && configuredRegions[0] != "all" {
		// Use first 2-3 regions as preferred
		preferred := make([]string, 0, 3)
		for i, region := range configuredRegions {
			if i >= 3 {
				break
			}
			preferred = append(preferred, region)
		}
		return preferred
	}

	// Use common/primary regions as preferred
	switch provider {
	case "aws":
		return []string{"us-east-1", "us-west-2", "eu-west-1"}
	case "azure":
		return []string{"eastus", "westus2", "westeurope"}
	case "gcp":
		return []string{"us-central1", "us-west1", "europe-west1"}
	default:
		return []string{}
	}
}

func (c *SmartScanConfiguration) ValidateProvider(provider string) error {
	validProviders := []string{"aws", "azure", "gcp", "kubernetes"}
	
	for _, valid := range validProviders {
		if provider == valid {
			return nil
		}
	}
	
	return fmt.Errorf("unsupported provider: %s. Valid providers: %v", provider, validProviders)
}

func (c *SmartScanConfiguration) PrintConfig() {
	fmt.Printf("📋 Configuration Summary:\n")
	fmt.Printf("Version: %s\n\n", c.Version)
	
	for provider, config := range c.Providers {
		status := "❌ disabled"
		if config.Enabled {
			status = "✅ enabled"
		}
		
		fmt.Printf("Provider %s: %s\n", provider, status)
		if config.Enabled {
			fmt.Printf("  Regions: %v\n", config.Regions)
			fmt.Printf("  Services: %d configured\n", len(config.Services))
		}
	}
	
	fmt.Printf("\nOutput Settings:\n")
	fmt.Printf("  Format: %s\n", c.Output.DefaultFormat)
	fmt.Printf("  Hide empty regions: %t\n", c.Output.HideEmptyRegions)
	fmt.Printf("  Hide empty services: %t\n", c.Output.HideEmptyServices)
}
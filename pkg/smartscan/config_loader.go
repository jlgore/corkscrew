package smartscan

import (
	"fmt"
	"time"

	appconfig "github.com/jlgore/corkscrew/internal/config"
)

type SmartScanConfiguration struct {
	*appconfig.CorkscrewConfig
}

type ProviderConfig = appconfig.CloudProviderConfig
type OutputConfig = appconfig.OutputConfig
type DatabaseConfig = appconfig.DatabaseConfig

func LoadSmartScanConfig(configPath string) (*SmartScanConfiguration, error) {
	cfg, err := appconfig.LoadCorkscrewConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &SmartScanConfiguration{CorkscrewConfig: cfg}, nil
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
		return appconfig.DefaultRegionsForProvider(provider), nil
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
		MaxConcurrency:    3,               // Could be made configurable
		RegionTimeout:     5 * time.Minute, // Set proper timeout for region scanning
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
	return appconfig.ValidateProviderName(provider)
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

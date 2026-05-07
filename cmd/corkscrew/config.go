package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jlgore/corkscrew/pkg/smartscan"
)

// runConfig handles configuration management commands
func runConfig(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew config <command>")
		fmt.Println("Commands: init, show, validate")
		return
	}

	command := args[0]
	switch command {
	case "init":
		runConfigInit()
	case "show":
		runConfigShow()
	case "validate":
		runConfigValidate()
	default:
		fmt.Printf("Unknown config command: %s\n", command)
		fmt.Println("Available commands: init, show, validate")
	}
}

const defaultSmartScanConfigYAML = `# Corkscrew Configuration File
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

database:
  path: "~/.corkscrew/db/corkscrew.duckdb"

output:
  default_format: "table"
  colors: true
  progress_bars: true
  hide_empty_regions: true
  hide_empty_services: true
`

func resolveSmartScanConfigPath() (string, error) {
	if configFile := strings.TrimSpace(os.Getenv("CORKSCREW_CONFIG_FILE")); configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			return configFile, nil
		}
		return "", fmt.Errorf("config file set by CORKSCREW_CONFIG_FILE not found: %s", configFile)
	}

	candidates := []string{
		"corkscrew.yaml",
		"corkscrew.yml",
		".corkscrew.yaml",
		".corkscrew.yml",
		filepath.Join(os.Getenv("HOME"), ".corkscrew", "config.yaml"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no configuration file found (looked for corkscrew.yaml, corkscrew.yml, .corkscrew.yaml, .corkscrew.yml, ~/.corkscrew/config.yaml)")
}

func runConfigInit() {
	fmt.Println("🔧 Initializing Corkscrew configuration...")

	if existingPath, err := resolveSmartScanConfigPath(); err == nil {
		log.Fatalf("Configuration file already exists at %s", existingPath)
	}

	targetPath := "corkscrew.yaml"
	if override := strings.TrimSpace(os.Getenv("CORKSCREW_CONFIG_FILE")); override != "" {
		targetPath = override
	}

	if dir := filepath.Dir(targetPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create config directory %s: %v", dir, err)
		}
	}

	if err := os.WriteFile(targetPath, []byte(defaultSmartScanConfigYAML), 0644); err != nil {
		log.Fatalf("Failed to write configuration file: %v", err)
	}

	fmt.Printf("✅ Configuration file created: %s\n", targetPath)
	fmt.Println("\nYou can now:")
	fmt.Println("  - Edit provider regions/services in the config file")
	fmt.Println("  - Run 'corkscrew config validate' to check your configuration")
	fmt.Println("  - Run 'corkscrew config show' to view the current configuration")
}

func runConfigShow() {
	configPath, err := resolveSmartScanConfigPath()
	if err != nil {
		log.Fatalf("Failed to locate configuration: %v", err)
	}

	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to read configuration: %v", err)
	}

	cfg, err := smartscan.LoadSmartScanConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to parse configuration: %v", err)
	}

	fmt.Printf("📋 Current Configuration (%s):\n\n", configPath)
	fmt.Println(string(rawConfig))

	fmt.Println("🔍 Resolved Provider Summary:")
	providers := make([]string, 0, len(cfg.Providers))
	for provider := range cfg.Providers {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	for _, provider := range providers {
		providerConfig := cfg.Providers[provider]
		status := "disabled"
		if providerConfig.Enabled {
			status = "enabled"
		}
		fmt.Printf("  - %s: %s, %d region(s), %d service(s)\n",
			provider, status, len(providerConfig.Regions), len(providerConfig.Services))
	}

	dbPath := cfg.Database.Path
	if strings.TrimSpace(dbPath) == "" {
		dbPath = defaultDatabasePath()
	}
	fmt.Printf("\n💾 Database path: %s\n", dbPath)
}

func runConfigValidate() {
	fmt.Println("🔍 Validating configuration...")

	configPath, err := resolveSmartScanConfigPath()
	if err != nil {
		log.Fatalf("❌ Configuration is invalid: %v", err)
	}

	cfg, err := smartscan.LoadSmartScanConfig(configPath)
	if err != nil {
		log.Fatalf("❌ Configuration is invalid: %v", err)
	}

	providers := make([]string, 0, len(cfg.Providers))
	for provider := range cfg.Providers {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	var validationErrors []string
	enabledProviders := 0
	for _, provider := range providers {
		if err := cfg.ValidateProvider(provider); err != nil {
			validationErrors = append(validationErrors, err.Error())
			continue
		}

		providerConfig := cfg.Providers[provider]
		if providerConfig.Enabled {
			enabledProviders++
		}

		for _, region := range providerConfig.Regions {
			if strings.TrimSpace(region) == "" {
				validationErrors = append(validationErrors, fmt.Sprintf("provider %s has an empty region entry", provider))
			}
		}

		for _, service := range providerConfig.Services {
			if strings.TrimSpace(service) == "" {
				validationErrors = append(validationErrors, fmt.Sprintf("provider %s has an empty service entry", provider))
			}
		}
	}

	if len(validationErrors) > 0 {
		fmt.Println("❌ Configuration has validation errors:")
		for _, validationError := range validationErrors {
			fmt.Printf("  - %s\n", validationError)
		}
		os.Exit(1)
	}

	fmt.Println("✅ Configuration is valid")
	fmt.Printf("📄 Source: %s\n", configPath)
	fmt.Printf("📦 Providers: %d total, %d enabled\n", len(providers), enabledProviders)

	for _, provider := range providers {
		providerConfig := cfg.Providers[provider]
		status := "disabled"
		if providerConfig.Enabled {
			status = "enabled"
		}
		fmt.Printf("  - %s: %s (%d region(s), %d service(s))\n",
			provider, status, len(providerConfig.Regions), len(providerConfig.Services))
	}

	dbPath := cfg.Database.Path
	if strings.TrimSpace(dbPath) == "" {
		dbPath = defaultDatabasePath()
	}
	fmt.Printf("💾 Database path: %s\n", dbPath)
}

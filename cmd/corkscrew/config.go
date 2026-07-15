package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	providerapp "github.com/jlgore/corkscrew/internal/app/providers"
	appconfig "github.com/jlgore/corkscrew/internal/config"
)

// runConfig handles configuration management commands
func runConfigE(args []string) error {
	if len(args) == 0 {
		printConfigUsage(os.Stderr)
		return fmt.Errorf("missing config command")
	}

	command := args[0]
	switch command {
	case "init":
		return runConfigInitE()
	case "show":
		return runConfigShowE()
	case "validate":
		return runConfigValidateE()
	default:
		printConfigUsage(os.Stderr)
		return fmt.Errorf("unknown config command: %s", command)
	}
}

func printConfigUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: corkscrew config <command>")
	fmt.Fprintln(w, "Commands: init, show, validate")
}

func resolveSmartScanConfigPath() (string, error) {
	return appconfig.ResolveCorkscrewConfigPath()
}

func runConfigInitE() error {
	fmt.Println("🔧 Initializing Corkscrew configuration...")

	if existingPath, err := resolveSmartScanConfigPath(); err == nil {
		return fmt.Errorf("configuration file already exists at %s", existingPath)
	}

	targetPath := appconfig.DefaultConfigFileName
	if override := strings.TrimSpace(os.Getenv("CORKSCREW_CONFIG_FILE")); override != "" {
		targetPath = override
	}

	if dir := filepath.Dir(targetPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory %s: %w", dir, err)
		}
	}

	if err := appconfig.WriteDefaultCorkscrewConfig(targetPath); err != nil {
		return fmt.Errorf("failed to write configuration file: %w", err)
	}

	fmt.Printf("✅ Configuration file created: %s\n", targetPath)
	fmt.Println("\nYou can now:")
	fmt.Println("  - Edit provider regions/services in the config file")
	fmt.Println("  - Run 'corkscrew config validate' to check your configuration")
	fmt.Println("  - Run 'corkscrew config show' to view the current configuration")
	return nil
}

func runConfigShowE() error {
	configPath, err := resolveSmartScanConfigPath()
	if err != nil {
		return fmt.Errorf("failed to locate configuration: %w", err)
	}

	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read configuration: %w", err)
	}

	cfg, err := appconfig.LoadCorkscrewConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
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
	return nil
}

func runConfigValidateE() error {
	fmt.Println("🔍 Validating configuration...")

	configPath, err := resolveSmartScanConfigPath()
	if err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}

	cfg, err := appconfig.LoadCorkscrewConfig(configPath)
	if err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}

	providers := make([]string, 0, len(cfg.Providers))
	for provider := range cfg.Providers {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	var validationErrors []string
	enabledProviders := 0
	application, runtimeErr := providerapp.OpenDefault(os.Stderr)
	if runtimeErr != nil {
		validationErrors = append(validationErrors, runtimeErr.Error())
	} else {
		defer application.Close()
		validation := application.ValidateConfig(cfg)
		validationErrors = append(validationErrors, validation.Errors...)
		enabledProviders = validation.EnabledProviders
	}

	if len(validationErrors) > 0 {
		fmt.Println("❌ Configuration has validation errors:")
		for _, validationError := range validationErrors {
			fmt.Printf("  - %s\n", validationError)
		}
		return fmt.Errorf("configuration has validation errors")
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
	return nil
}

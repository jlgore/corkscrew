// This file contains all feature flags that were removed during UnifiedScanner migration
// Keep this for reference only - these flags should NOT be reintroduced

package archive

import (
	"log"
	"os"
	"strconv"
)

// ARCHIVED: Feature flags for migration control from aws_provider.go:27-34
const (
	// AWS_PROVIDER_MIGRATION_ENABLED controls whether to use the new dynamic discovery system
	EnvMigrationEnabled = "AWS_PROVIDER_MIGRATION_ENABLED"
	// AWS_PROVIDER_FALLBACK_MODE controls fallback behavior
	EnvFallbackMode = "AWS_PROVIDER_FALLBACK_MODE"
	// AWS_PROVIDER_MONITORING_ENABLED enables additional monitoring
	EnvMonitoringEnabled = "AWS_PROVIDER_MONITORING_ENABLED"
)

// ARCHIVED: isMigrationEnabled checks if the migration to dynamic discovery is enabled
// Originally from aws_provider.go:36-48
func isMigrationEnabled() bool {
	value := os.Getenv(EnvMigrationEnabled)
	if value == "" {
		return true // Default to enabled
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("Warning: Invalid value for %s: %s, defaulting to true", EnvMigrationEnabled, value)
		return true
	}
	return enabled
}

// ARCHIVED: isFallbackModeEnabled checks if fallback mode is enabled
// Originally from aws_provider.go:50-62
func isFallbackModeEnabled() bool {
	value := os.Getenv(EnvFallbackMode)
	if value == "" {
		return false // Default to disabled
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("Warning: Invalid value for %s: %s, defaulting to false", EnvFallbackMode, value)
		return false
	}
	return enabled
}

// ARCHIVED: isMonitoringEnabled checks if enhanced monitoring is enabled
// Originally from aws_provider.go:64-76
func isMonitoringEnabled() bool {
	value := os.Getenv(EnvMonitoringEnabled)
	if value == "" {
		return false // Default to disabled
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("Warning: Invalid value for %s: %s, defaulting to false", EnvMonitoringEnabled, value)
		return false
	}
	return enabled
}
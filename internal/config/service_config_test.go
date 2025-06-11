package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadServiceConfig(t *testing.T) {
	tests := []struct {
		name         string
		configFile   string
		envVars      map[string]string
		wantError    bool
		wantServices int
	}{
		{
			name:         "Default config",
			wantError:    false,
			wantServices: 18, // Default 18 services
		},
		{
			name:       "Custom config file",
			configFile: "testdata/custom.yaml",
			wantError:  false,
			wantServices: 25,
		},
		{
			name: "Environment override",
			envVars: map[string]string{
				"CORKSCREW_AWS_SERVICES": "s3,ec2,lambda",
			},
			wantError:    false,
			wantServices: 3,
		},
		{
			name: "Discovery mode override",
			envVars: map[string]string{
				"CORKSCREW_DISCOVERY_MODE": "auto",
			},
			wantError:    false,
			wantServices: 18, // Will still use defaults in test
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean environment
			os.Unsetenv("CORKSCREW_CONFIG_FILE")
			os.Unsetenv("CORKSCREW_AWS_SERVICES")
			os.Unsetenv("CORKSCREW_DISCOVERY_MODE")
			
			// Set up environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}
			
			if tt.configFile != "" {
				os.Setenv("CORKSCREW_CONFIG_FILE", tt.configFile)
				defer os.Unsetenv("CORKSCREW_CONFIG_FILE")
			}
			
			// Load config
			cfg, err := LoadServiceConfig()
			
			if tt.wantError {
				if err == nil {
					t.Errorf("LoadServiceConfig() expected error, got nil")
				}
				return
			}
			
			if err != nil {
				t.Errorf("LoadServiceConfig() unexpected error: %v", err)
				return
			}
			
			// Check services
			services, err := cfg.GetServicesForProvider("aws")
			if err != nil {
				t.Errorf("GetServicesForProvider() error: %v", err)
				return
			}
			
			if len(services) != tt.wantServices {
				t.Errorf("GetServicesForProvider() got %d services, want %d", len(services), tt.wantServices)
			}
		})
	}
}

func TestServiceGroups(t *testing.T) {
	cfg := getDefaultConfig()
	
	// Add test groups
	cfg.Providers["aws"].ServiceGroups = map[string][]string{
		"test": {"s3", "ec2", "lambda"},
		"data": {"rds", "dynamodb", "athena"},
	}
	
	tests := []struct {
		name      string
		provider  string
		group     string
		wantError bool
		wantSvcs  []string
	}{
		{
			name:      "Valid group",
			provider:  "aws",
			group:     "test",
			wantError: false,
			wantSvcs:  []string{"s3", "ec2", "lambda"},
		},
		{
			name:      "Non-existent group",
			provider:  "aws",
			group:     "nonexistent",
			wantError: true,
		},
		{
			name:      "Non-existent provider",
			provider:  "gcp",
			group:     "test",
			wantError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			services, err := cfg.GetServiceGroup(tt.provider, tt.group)
			
			if tt.wantError {
				if err == nil {
					t.Errorf("GetServiceGroup() expected error, got nil")
				}
				return
			}
			
			if err != nil {
				t.Errorf("GetServiceGroup() unexpected error: %v", err)
				return
			}
			
			if len(services) != len(tt.wantSvcs) {
				t.Errorf("GetServiceGroup() got %d services, want %d", len(services), len(tt.wantSvcs))
				return
			}
			
			for i, svc := range services {
				if svc != tt.wantSvcs[i] {
					t.Errorf("GetServiceGroup() service[%d] = %s, want %s", i, svc, tt.wantSvcs[i])
				}
			}
		})
	}
}

func TestDiscoveryModes(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		include      []string
		exclude      []string
		expectError  bool
		minServices  int
	}{
		{
			name:        "Manual mode",
			mode:        "manual",
			include:     []string{"s3", "ec2", "lambda"},
			exclude:     []string{"ec2"},
			minServices: 2, // s3, lambda (ec2 excluded)
		},
		{
			name:        "Invalid mode",
			mode:        "invalid",
			expectError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ServiceConfig{
				Version: "1.0",
				Providers: map[string]ProviderConfig{
					"aws": {
						DiscoveryMode: tt.mode,
						Services: ServicesConfig{
							Include: tt.include,
							Exclude: tt.exclude,
						},
					},
				},
			}
			
			services, err := cfg.GetServicesForProvider("aws")
			
			if tt.expectError {
				if err == nil {
					t.Errorf("GetServicesForProvider() expected error for mode %s", tt.mode)
				}
				return
			}
			
			if err != nil {
				t.Errorf("GetServicesForProvider() unexpected error: %v", err)
				return
			}
			
			if len(services) < tt.minServices {
				t.Errorf("GetServicesForProvider() got %d services, want at least %d", len(services), tt.minServices)
			}
			
			// Check exclusions work
			for _, svc := range services {
				for _, excluded := range tt.exclude {
					if svc == excluded {
						t.Errorf("GetServicesForProvider() returned excluded service: %s", excluded)
					}
				}
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *ServiceConfig
		wantError   bool
		errorMsg    string
	}{
		{
			name: "Valid config",
			config: &ServiceConfig{
				Version: "1.0",
				Providers: map[string]ProviderConfig{
					"aws": {
						DiscoveryMode: "manual",
						Analysis: AnalysisConfig{
							Workers:  4,
							CacheTTL: "24h",
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "Invalid discovery mode",
			config: &ServiceConfig{
				Providers: map[string]ProviderConfig{
					"aws": {
						DiscoveryMode: "invalid",
					},
				},
			},
			wantError: true,
			errorMsg:  "invalid discovery mode",
		},
		{
			name: "Invalid cache TTL",
			config: &ServiceConfig{
				Providers: map[string]ProviderConfig{
					"aws": {
						DiscoveryMode: "manual",
						Analysis: AnalysisConfig{
							CacheTTL: "invalid",
						},
					},
				},
			},
			wantError: true,
			errorMsg:  "invalid cache TTL",
		},
		{
			name: "Zero workers gets default",
			config: &ServiceConfig{
				Providers: map[string]ProviderConfig{
					"aws": {
						DiscoveryMode: "manual",
						Analysis: AnalysisConfig{
							Workers: 0,
						},
					},
				},
			},
			wantError: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			
			if tt.wantError {
				if err == nil {
					t.Errorf("validateConfig() expected error containing '%s', got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateConfig() error = %v, want error containing '%s'", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateConfig() unexpected error: %v", err)
				}
				
				// Check defaults were applied
				if tt.name == "Zero workers gets default" {
					if tt.config.Providers["aws"].Analysis.Workers != 4 {
						t.Errorf("validateConfig() didn't set default workers, got %d", tt.config.Providers["aws"].Analysis.Workers)
					}
				}
			}
		})
	}
}

func TestInitializeConfigFile(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)
	
	// Test creating new config
	err := InitializeConfigFile()
	if err != nil {
		t.Errorf("InitializeConfigFile() error: %v", err)
		return
	}
	
	// Check file exists
	if _, err := os.Stat("corkscrew.yaml"); os.IsNotExist(err) {
		t.Error("InitializeConfigFile() didn't create corkscrew.yaml")
		return
	}
	
	// Test error when file already exists
	err = InitializeConfigFile()
	if err == nil {
		t.Error("InitializeConfigFile() should error when file exists")
	}
}

func TestCacheTTLParsing(t *testing.T) {
	tests := []struct {
		ttl   string
		valid bool
	}{
		{"24h", true},
		{"1h30m", true},
		{"30m", true},
		{"invalid", false},
		{"", false},
		{"24", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.ttl, func(t *testing.T) {
			_, err := time.ParseDuration(tt.ttl)
			isValid := err == nil
			
			if isValid != tt.valid {
				t.Errorf("ParseDuration(%s) valid = %v, want %v", tt.ttl, isValid, tt.valid)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > 0 && len(substr) > 0 && strings.Contains(s, substr)))
}

// Import strings for the helper
var strings = struct {
	Contains func(string, string) bool
}{
	Contains: func(s, substr string) bool {
		if len(substr) > len(s) {
			return false
		}
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	},
}
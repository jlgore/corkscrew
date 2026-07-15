package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadCorkscrewConfigPreservesProviderInitializationConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corkscrew.yaml")
	configYAML := `version: "2.0"
providers:
  aws:
    enabled: true
    regions: [us-east-1]
    services: [s3]
    config:
      auth.secret.provider: vault
      auth.secret.mount: secret
      auth.secret.path: aws/prod
      auth.secret.allow_fallback: true
database:
  path: test.duckdb
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadCorkscrewConfig(configPath)
	if err != nil {
		t.Fatalf("LoadCorkscrewConfig() error = %v", err)
	}

	got := cfg.ProviderInitializationConfig("aws")
	want := map[string]string{
		"auth.secret.provider":       "vault",
		"auth.secret.mount":          "secret",
		"auth.secret.path":           "aws/prod",
		"auth.secret.allow_fallback": "true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProviderInitializationConfig() = %#v, want %#v", got, want)
	}

	got["auth.secret.path"] = "mutated"
	if cfg.Providers["aws"].Config["auth.secret.path"] != "aws/prod" {
		t.Fatal("ProviderInitializationConfig() returned the mutable config map")
	}
}

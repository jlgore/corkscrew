package cloudflareauth

import (
	"reflect"
	"testing"
)

func TestParseCSVSortsAndDeduplicates(t *testing.T) {
	got := ParseCSV(" workers ,dns,workers, zones ,dns ")
	want := []string{"dns", "workers", "zones"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCSV() = %#v, want %#v", got, want)
	}
}

func TestLoadConfigDefaultsToAPIToken(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_API_KEY", "")
	t.Setenv("CLOUDFLARE_EMAIL", "")
	t.Setenv("CLOUDFLARE_BASE_URL", "")

	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Auth.Method != AuthMethodAPIToken {
		t.Fatalf("cfg.Auth.Method = %q, want %q", cfg.Auth.Method, AuthMethodAPIToken)
	}
	if cfg.Auth.Profile != DefaultProfileName {
		t.Fatalf("cfg.Auth.Profile = %q, want %q", cfg.Auth.Profile, DefaultProfileName)
	}
}

func TestLoadConfigPrefersAPIKeyWhenBothEnvVarsSet(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_KEY", "key")
	t.Setenv("CLOUDFLARE_EMAIL", "user@example.com")

	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Auth.Method != AuthMethodAPIKey {
		t.Fatalf("cfg.Auth.Method = %q, want %q", cfg.Auth.Method, AuthMethodAPIKey)
	}
}

func TestLoadConfigOverrideWins(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_KEY", "key")
	t.Setenv("CLOUDFLARE_EMAIL", "user@example.com")

	cfg, err := LoadConfig(map[string]string{
		"auth.method":         "oauth",
		"auth.profile":        "prod",
		"scope.account_ids":   "acc-b,acc-a",
		"scope.include_zones": "example.com,api.example.com",
		"scan.services":       "workers,dns,workers",
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Auth.Method != AuthMethodOAuth {
		t.Fatalf("cfg.Auth.Method = %q, want %q", cfg.Auth.Method, AuthMethodOAuth)
	}
	if cfg.Auth.Profile != "prod" {
		t.Fatalf("cfg.Auth.Profile = %q, want %q", cfg.Auth.Profile, "prod")
	}
	if !reflect.DeepEqual(cfg.Scope.AccountIDs, []string{"acc-a", "acc-b"}) {
		t.Fatalf("cfg.Scope.AccountIDs = %#v", cfg.Scope.AccountIDs)
	}
	if !reflect.DeepEqual(cfg.Scan.Services, []string{"dns", "workers"}) {
		t.Fatalf("cfg.Scan.Services = %#v", cfg.Scan.Services)
	}
}

func TestLoadConfigRejectsUnsupportedAuthMethod(t *testing.T) {
	_, err := LoadConfig(map[string]string{"auth.method": "magic"})
	if err == nil {
		t.Fatal("expected error for unsupported auth method")
	}
}

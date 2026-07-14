package cloudflareauth

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jlgore/corkscrew/internal/secrets"
)

func LoadConfig(override map[string]string) (*CloudflareConfig, error) {
	if override == nil {
		override = map[string]string{}
	}

	cfg := &CloudflareConfig{
		Auth: AuthConfig{
			Method:          AuthMethodAPIToken,
			Profile:         DefaultProfileName,
			TokenEnv:        "CLOUDFLARE_API_TOKEN",
			APIKeyEnv:       "CLOUDFLARE_API_KEY",
			EmailEnv:        "CLOUDFLARE_EMAIL",
			UseRefreshToken: true,
			BaseURL:         os.Getenv("CLOUDFLARE_BASE_URL"),
			Secret:          secrets.DefaultCredentialSource(),
		},
	}

	if hasNonEmptyEnv("CLOUDFLARE_API_KEY") && hasNonEmptyEnv("CLOUDFLARE_EMAIL") {
		cfg.Auth.Method = AuthMethodAPIKey
	}

	if value := firstNonEmpty(override["auth.method"], override["method"]); value != "" {
		cfg.Auth.Method = AuthMethod(value)
	}
	if value := firstNonEmpty(override["auth.profile"], override["profile"]); value != "" {
		cfg.Auth.Profile = value
	}
	if value := firstNonEmpty(override["auth.token"], override["token"]); value != "" {
		cfg.Auth.Token = value
	}
	if value := firstNonEmpty(override["auth.token_env"], override["token_env"]); value != "" {
		cfg.Auth.TokenEnv = value
	}
	if value := firstNonEmpty(override["auth.api_key"], override["api_key"]); value != "" {
		cfg.Auth.APIKey = value
	}
	if value := firstNonEmpty(override["auth.api_key_env"], override["api_key_env"]); value != "" {
		cfg.Auth.APIKeyEnv = value
	}
	if value := firstNonEmpty(override["auth.email"], override["email"]); value != "" {
		cfg.Auth.Email = value
	}
	if value := firstNonEmpty(override["auth.email_env"], override["email_env"]); value != "" {
		cfg.Auth.EmailEnv = value
	}
	if value := firstNonEmpty(override["auth.base_url"], override["base_url"]); value != "" {
		cfg.Auth.BaseURL = value
	}
	if value := firstNonEmpty(override["auth.oauth_scopes"], override["oauth_scopes"]); value != "" {
		cfg.Auth.OAuthScopes = ParseCSV(value)
	}
	secret, err := secrets.CredentialSourceFromConfig(override)
	if err != nil {
		return nil, err
	}
	cfg.Auth.Secret = secret

	cfg.Scope.AccountIDs = ParseCSV(firstNonEmpty(override["scope.account_ids"], override["account_ids"]))
	cfg.Scope.ZoneIDs = ParseCSV(firstNonEmpty(override["scope.zone_ids"], override["zone_ids"]))
	cfg.Scope.IncludeZones = ParseCSV(firstNonEmpty(override["scope.include_zones"], override["include_zones"]))
	cfg.Scope.ExcludeZones = ParseCSV(firstNonEmpty(override["scope.exclude_zones"], override["exclude_zones"]))
	cfg.Scan.Services = ParseCSV(firstNonEmpty(override["scan.services"], override["services"]))

	if cfg.Auth.Profile == "" {
		cfg.Auth.Profile = DefaultProfileName
	}

	if cfg.Auth.Method != AuthMethodOAuth && cfg.Auth.Method != AuthMethodAPIToken && cfg.Auth.Method != AuthMethodAPIKey {
		return nil, fmt.Errorf("unsupported auth method %q", cfg.Auth.Method)
	}
	if cfg.Auth.Secret.Kind != "" && AuthMethod(cfg.Auth.Secret.Kind) != AuthMethodOAuth && AuthMethod(cfg.Auth.Secret.Kind) != AuthMethodAPIToken && AuthMethod(cfg.Auth.Secret.Kind) != AuthMethodAPIKey {
		return nil, fmt.Errorf("unsupported secret auth method %q", cfg.Auth.Secret.Kind)
	}

	return cfg, nil
}

func ParseCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		values = append(values, part)
	}
	sort.Strings(values)
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func hasNonEmptyEnv(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}

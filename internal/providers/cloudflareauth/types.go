package cloudflareauth

import "time"

type AuthMethod string

const (
	AuthMethodOAuth    AuthMethod = "oauth"
	AuthMethodAPIToken AuthMethod = "api_token"
	AuthMethodAPIKey   AuthMethod = "api_key"

	DefaultProfileName = "default"
)

var CanonicalServices = []string{
	"accounts",
	"zones",
	"dns",
	"rulesets",
	"workers",
	"storage",
	"data",
	"pages",
	"zero_trust_access",
	"zero_trust_network",
	"zero_trust_devices",
	"load_balancing",
	"edge_certs",
	"delivery",
	"observability",
	"media",
	"security",
	"email",
	"apps_ai",
	"network_services",
	"specialized",
}

type CloudflareConfig struct {
	Auth  AuthConfig
	Scope ScopeConfig
	Scan  ScanConfig
}

type AuthConfig struct {
	Method          AuthMethod
	Profile         string
	Token           string
	TokenEnv        string
	APIKey          string
	APIKeyEnv       string
	Email           string
	EmailEnv        string
	OAuthScopes     []string
	UseRefreshToken bool
	BaseURL         string
}

type ScopeConfig struct {
	AccountIDs   []string
	ZoneIDs      []string
	IncludeZones []string
	ExcludeZones []string
}

type ScanConfig struct {
	Services []string
}

type OAuthProfile struct {
	Profile      string    `json:"profile"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	Scopes       []string  `json:"scopes"`
	AccountHints []string  `json:"account_hints"`
	BaseURL      string    `json:"base_url"`
}

type ResolvedAuth struct {
	Method       AuthMethod
	AccessToken  string
	APIKey       string
	Email        string
	Scopes       []string
	AccountHints []string
	BaseURL      string
	Source       string
}

type PermissionBundle struct {
	Name        string
	Services    []string
	Scopes      []string
	Description string
}

type PermissionPlan struct {
	Services []string
	Bundles  []PermissionBundle
	Scopes   []string
}

type VerifyResult struct {
	Method           AuthMethod
	Source           string
	RequiredBundles  []PermissionBundle
	RequiredScopes   []string
	GrantedScopes    []string
	MissingScopes    []string
	ScopeCheckStrict bool
}

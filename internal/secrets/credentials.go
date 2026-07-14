package secrets

import (
	"context"
	"fmt"
	"path"
	"strings"
)

const (
	KindAPIToken              = "api_token"
	KindAPIKey                = "api_key"
	KindOAuth                 = "oauth"
	KindAWSStatic             = "aws_static"
	KindAWSRole               = "aws_role"
	KindAzureServicePrincipal = "azure_service_principal"
	KindGCPServiceAccount     = "gcp_service_account"
)

type CredentialSource struct {
	Provider  string
	Engine    string
	Address   string
	Token     string
	TokenEnv  string
	Namespace string
	Mount     string
	Path      string
	Version   int

	Kind       string
	KindField  string
	TokenField string

	AccessTokenField        string
	RefreshTokenField       string
	APIKeyField             string
	EmailField              string
	BaseURLField            string
	ScopesField             string
	AccountHintsField       string
	ClientIDField           string
	ClientSecretField       string
	TenantIDField           string
	SubscriptionIDField     string
	AccessKeyIDField        string
	SecretAccessKeyField    string
	SessionTokenField       string
	RoleARNField            string
	ExternalIDField         string
	RegionField             string
	ProjectIDField          string
	ServiceAccountJSONField string
	ClientEmailField        string
	PrivateKeyField         string

	AllowFallback bool
}

func (s CredentialSource) Configured() bool {
	return firstNonEmpty(s.Provider, s.Address, s.Token, s.Namespace, s.Mount, s.Path) != ""
}

func (s CredentialSource) Reference() Reference {
	return Reference{
		Provider:  firstNonEmpty(s.Provider, ProviderVault),
		Engine:    s.Engine,
		Address:   s.Address,
		Token:     s.Token,
		TokenEnv:  s.TokenEnv,
		Namespace: s.Namespace,
		Mount:     s.Mount,
		Path:      s.Path,
		Version:   s.Version,
	}
}

func (s CredentialSource) SourceString() string {
	provider := firstNonEmpty(s.Provider, ProviderVault)
	secretPath := strings.Trim(path.Join(strings.Trim(s.Mount, "/"), strings.Trim(s.Path, "/")), "/")
	if secretPath == "" {
		return "secret:" + provider
	}
	return "secret:" + provider + ":" + secretPath
}

type Credential struct {
	Kind               string
	Token              string
	AccessToken        string
	RefreshToken       string
	APIKey             string
	Email              string
	BaseURL            string
	Scopes             []string
	AccountHints       []string
	ClientID           string
	ClientSecret       string
	TenantID           string
	SubscriptionID     string
	AccessKeyID        string
	SecretAccessKey    string
	SessionToken       string
	RoleARN            string
	ExternalID         string
	Region             string
	ProjectID          string
	ServiceAccountJSON string
	ClientEmail        string
	PrivateKey         string
	Source             string
	Raw                map[string]string
}

type CredentialResolver struct {
	Reader Reader
}

type CredentialRequest struct {
	Source      CredentialSource
	DefaultKind string
}

func (r *CredentialResolver) ResolveCredential(ctx context.Context, req CredentialRequest) (*Credential, error) {
	if !req.Source.Configured() {
		return nil, fmt.Errorf("credential source not configured")
	}

	reader := r.Reader
	if reader == nil {
		reader = &VaultReader{}
	}

	secret, err := reader.ReadSecret(ctx, req.Source.Reference())
	if err != nil {
		return nil, err
	}
	return CredentialFromSecret(secret, req.Source, req.DefaultKind), nil
}

func CredentialFromSecret(secret map[string]string, source CredentialSource, defaultKind string) *Credential {
	kind := firstNonEmpty(source.Kind, secretField(secret, source.KindField, "kind", "method", "auth_method"), defaultKind)
	raw := make(map[string]string, len(secret))
	for key, value := range secret {
		raw[key] = value
	}

	return &Credential{
		Kind:               kind,
		Token:              secretField(secret, source.TokenField, "api_token", "token", "access_token"),
		AccessToken:        secretField(secret, source.AccessTokenField, "access_token", "api_token", "token"),
		RefreshToken:       secretField(secret, source.RefreshTokenField, "refresh_token"),
		APIKey:             secretField(secret, source.APIKeyField, "api_key", "key"),
		Email:              secretField(secret, source.EmailField, "email", "api_email", "client_email"),
		BaseURL:            secretField(secret, source.BaseURLField, "base_url", "endpoint", "url"),
		Scopes:             ParseList(secretField(secret, source.ScopesField, "scopes", "oauth_scopes")),
		AccountHints:       ParseList(secretField(secret, source.AccountHintsField, "account_hints", "account_ids")),
		ClientID:           secretField(secret, source.ClientIDField, "client_id", "application_id", "app_id"),
		ClientSecret:       secretField(secret, source.ClientSecretField, "client_secret", "secret"),
		TenantID:           secretField(secret, source.TenantIDField, "tenant_id", "directory_id"),
		SubscriptionID:     secretField(secret, source.SubscriptionIDField, "subscription_id"),
		AccessKeyID:        secretField(secret, source.AccessKeyIDField, "access_key_id", "aws_access_key_id"),
		SecretAccessKey:    secretField(secret, source.SecretAccessKeyField, "secret_access_key", "aws_secret_access_key"),
		SessionToken:       secretField(secret, source.SessionTokenField, "session_token", "aws_session_token"),
		RoleARN:            secretField(secret, source.RoleARNField, "role_arn", "aws_role_arn"),
		ExternalID:         secretField(secret, source.ExternalIDField, "external_id"),
		Region:             secretField(secret, source.RegionField, "region", "aws_region"),
		ProjectID:          secretField(secret, source.ProjectIDField, "project_id"),
		ServiceAccountJSON: secretField(secret, source.ServiceAccountJSONField, "service_account_json", "credentials_json"),
		ClientEmail:        secretField(secret, source.ClientEmailField, "client_email"),
		PrivateKey:         secretField(secret, source.PrivateKeyField, "private_key"),
		Source:             source.SourceString(),
		Raw:                raw,
	}
}

func ParseList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
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
	return values
}

func secretField(secret map[string]string, primary string, aliases ...string) string {
	keys := make([]string, 0, len(aliases)+1)
	if strings.TrimSpace(primary) != "" {
		keys = append(keys, strings.TrimSpace(primary))
	}
	keys = append(keys, aliases...)
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if value := strings.TrimSpace(secret[key]); value != "" {
			return value
		}
	}
	return ""
}

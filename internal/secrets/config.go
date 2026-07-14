package secrets

import (
	"fmt"
	"strconv"
	"strings"
)

func DefaultCredentialSource() CredentialSource {
	return CredentialSource{
		Engine:       EngineKVV2,
		TokenEnv:     "VAULT_TOKEN",
		KindField:    "kind",
		TokenField:   "api_token",
		APIKeyField:  "api_key",
		EmailField:   "email",
		BaseURLField: "base_url",
		ScopesField:  "scopes",
	}
}

func CredentialSourceFromConfig(config map[string]string) (CredentialSource, error) {
	source := DefaultCredentialSource()
	if config == nil {
		return source, nil
	}

	setString(config, &source.Provider, "auth.secret.provider", "secret.provider", "auth.vault.provider", "vault.provider", "auth.secret_provider", "secret_provider")
	setString(config, &source.Engine, "auth.secret.engine", "secret.engine", "auth.vault.engine", "vault.engine")
	setString(config, &source.Address, "auth.secret.address", "secret.address", "auth.vault.address", "vault.address")
	setString(config, &source.Token, "auth.secret.token", "secret.token", "auth.vault.token", "vault.token")
	setString(config, &source.TokenEnv, "auth.secret.token_env", "secret.token_env", "auth.vault.token_env", "vault.token_env")
	setString(config, &source.Namespace, "auth.secret.namespace", "secret.namespace", "auth.vault.namespace", "vault.namespace")
	setString(config, &source.Mount, "auth.secret.mount", "secret.mount", "auth.vault.mount", "vault.mount")
	setString(config, &source.Path, "auth.secret.path", "secret.path", "auth.vault.path", "vault.path", "auth.secret_ref", "secret_ref")

	if value := firstConfigValue(config, "auth.secret.version", "secret.version", "auth.vault.version", "vault.version"); value != "" {
		version, err := strconv.Atoi(value)
		if err != nil || version < 1 {
			return source, fmt.Errorf("invalid auth.secret.version %q", value)
		}
		source.Version = version
	}

	setString(config, &source.Kind, "auth.secret.kind", "secret.kind", "auth.secret.method", "secret.method")
	setString(config, &source.KindField, "auth.secret.kind_field", "secret.kind_field", "auth.secret.method_field", "secret.method_field")
	setString(config, &source.TokenField, "auth.secret.token_field", "secret.token_field", "auth.secret.api_token_field", "secret.api_token_field")
	setString(config, &source.AccessTokenField, "auth.secret.access_token_field", "secret.access_token_field")
	setString(config, &source.RefreshTokenField, "auth.secret.refresh_token_field", "secret.refresh_token_field")
	setString(config, &source.APIKeyField, "auth.secret.api_key_field", "secret.api_key_field")
	setString(config, &source.EmailField, "auth.secret.email_field", "secret.email_field")
	setString(config, &source.BaseURLField, "auth.secret.base_url_field", "secret.base_url_field")
	setString(config, &source.ScopesField, "auth.secret.oauth_scopes_field", "secret.oauth_scopes_field", "auth.secret.scopes_field", "secret.scopes_field")
	setString(config, &source.AccountHintsField, "auth.secret.account_hints_field", "secret.account_hints_field", "auth.secret.account_ids_field", "secret.account_ids_field")
	setString(config, &source.ClientIDField, "auth.secret.client_id_field", "secret.client_id_field")
	setString(config, &source.ClientSecretField, "auth.secret.client_secret_field", "secret.client_secret_field")
	setString(config, &source.TenantIDField, "auth.secret.tenant_id_field", "secret.tenant_id_field")
	setString(config, &source.SubscriptionIDField, "auth.secret.subscription_id_field", "secret.subscription_id_field")
	setString(config, &source.AccessKeyIDField, "auth.secret.access_key_id_field", "secret.access_key_id_field", "auth.secret.aws_access_key_id_field", "secret.aws_access_key_id_field")
	setString(config, &source.SecretAccessKeyField, "auth.secret.secret_access_key_field", "secret.secret_access_key_field", "auth.secret.aws_secret_access_key_field", "secret.aws_secret_access_key_field")
	setString(config, &source.SessionTokenField, "auth.secret.session_token_field", "secret.session_token_field", "auth.secret.aws_session_token_field", "secret.aws_session_token_field")
	setString(config, &source.RoleARNField, "auth.secret.role_arn_field", "secret.role_arn_field", "auth.secret.aws_role_arn_field", "secret.aws_role_arn_field")
	setString(config, &source.ExternalIDField, "auth.secret.external_id_field", "secret.external_id_field")
	setString(config, &source.RegionField, "auth.secret.region_field", "secret.region_field")
	setString(config, &source.ProjectIDField, "auth.secret.project_id_field", "secret.project_id_field")
	setString(config, &source.ServiceAccountJSONField, "auth.secret.service_account_json_field", "secret.service_account_json_field", "auth.secret.credentials_json_field", "secret.credentials_json_field")
	setString(config, &source.ClientEmailField, "auth.secret.client_email_field", "secret.client_email_field")
	setString(config, &source.PrivateKeyField, "auth.secret.private_key_field", "secret.private_key_field")

	if value := firstConfigValue(config, "auth.secret.allow_fallback", "secret.allow_fallback"); value != "" {
		allowFallback, err := strconv.ParseBool(value)
		if err != nil {
			return source, fmt.Errorf("invalid auth.secret.allow_fallback %q", value)
		}
		source.AllowFallback = allowFallback
	}

	if source.Provider == "" && firstNonEmpty(source.Address, source.Token, source.Namespace, source.Mount, source.Path) != "" {
		source.Provider = ProviderVault
	}
	return source, nil
}

func setString(config map[string]string, target *string, keys ...string) {
	if value := firstConfigValue(config, keys...); value != "" {
		*target = value
	}
}

func firstConfigValue(config map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(config[key]); value != "" {
			return value
		}
	}
	return ""
}

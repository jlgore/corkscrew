package secrets

import "testing"

func TestCredentialSourceFromConfigParsesVaultSource(t *testing.T) {
	source, err := CredentialSourceFromConfig(map[string]string{
		"auth.secret.address":                 "https://vault.example.com",
		"auth.secret.engine":                  "kv-v1",
		"auth.secret.mount":                   "kv",
		"auth.secret.path":                    "aws/prod",
		"auth.secret.version":                 "7",
		"auth.secret.kind":                    KindAWSRole,
		"auth.secret.access_key_id_field":     "access",
		"auth.secret.secret_access_key_field": "secret",
		"auth.secret.allow_fallback":          "true",
	})
	if err != nil {
		t.Fatalf("CredentialSourceFromConfig() error = %v", err)
	}
	if source.Provider != ProviderVault {
		t.Fatalf("Provider = %q, want %q", source.Provider, ProviderVault)
	}
	if source.Engine != EngineKVV1 || source.Mount != "kv" || source.Path != "aws/prod" {
		t.Fatalf("unexpected source: %#v", source)
	}
	if source.Version != 7 || source.Kind != KindAWSRole {
		t.Fatalf("unexpected version/kind: %#v", source)
	}
	if source.AccessKeyIDField != "access" || source.SecretAccessKeyField != "secret" {
		t.Fatalf("unexpected field overrides: %#v", source)
	}
	if !source.AllowFallback {
		t.Fatal("AllowFallback = false, want true")
	}
}

func TestCredentialSourceFromConfigRejectsBadVersion(t *testing.T) {
	_, err := CredentialSourceFromConfig(map[string]string{"auth.secret.version": "zero"})
	if err == nil {
		t.Fatal("expected error")
	}
}

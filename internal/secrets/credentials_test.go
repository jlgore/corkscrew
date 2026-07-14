package secrets

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeReader struct {
	secret map[string]string
	ref    Reference
	err    error
}

func (f *fakeReader) ReadSecret(_ context.Context, ref Reference) (map[string]string, error) {
	f.ref = ref
	if f.err != nil {
		return nil, f.err
	}
	return f.secret, nil
}

func TestCredentialResolverMapsAPIToken(t *testing.T) {
	reader := &fakeReader{secret: map[string]string{
		"method":    KindAPIToken,
		"api_token": "tok",
		"scopes":    "Zone:Read,DNS:Read,Zone:Read",
	}}
	resolver := &CredentialResolver{Reader: reader}
	credential, err := resolver.ResolveCredential(context.Background(), CredentialRequest{
		Source: CredentialSource{
			Provider: ProviderVault,
			Mount:    "secret",
			Path:     "cloudflare/prod",
		},
	})
	if err != nil {
		t.Fatalf("ResolveCredential() error = %v", err)
	}
	if credential.Kind != KindAPIToken || credential.Token != "tok" || credential.AccessToken != "tok" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
	if !reflect.DeepEqual(credential.Scopes, []string{"Zone:Read", "DNS:Read"}) {
		t.Fatalf("Scopes = %#v", credential.Scopes)
	}
	if credential.Source != "secret:vault:secret/cloudflare/prod" {
		t.Fatalf("Source = %q", credential.Source)
	}
	if reader.ref.Provider != ProviderVault || reader.ref.Mount != "secret" || reader.ref.Path != "cloudflare/prod" {
		t.Fatalf("unexpected reference: %#v", reader.ref)
	}
}

func TestCredentialFromSecretMapsAzureServicePrincipal(t *testing.T) {
	credential := CredentialFromSecret(map[string]string{
		"client_id":       "client",
		"client_secret":   "secret",
		"tenant_id":       "tenant",
		"subscription_id": "sub",
	}, CredentialSource{Kind: KindAzureServicePrincipal}, "")

	if credential.Kind != KindAzureServicePrincipal ||
		credential.ClientID != "client" ||
		credential.ClientSecret != "secret" ||
		credential.TenantID != "tenant" ||
		credential.SubscriptionID != "sub" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
}

func TestCredentialFromSecretMapsAWSMaterial(t *testing.T) {
	credential := CredentialFromSecret(map[string]string{
		"aws_access_key_id":     "access",
		"aws_secret_access_key": "secret",
		"aws_session_token":     "session",
		"aws_role_arn":          "arn:aws:iam::123456789012:role/ReadOnly",
		"external_id":           "external",
		"aws_region":            "us-east-1",
	}, CredentialSource{Kind: KindAWSStatic}, "")

	if credential.AccessKeyID != "access" ||
		credential.SecretAccessKey != "secret" ||
		credential.SessionToken != "session" ||
		credential.RoleARN != "arn:aws:iam::123456789012:role/ReadOnly" ||
		credential.ExternalID != "external" ||
		credential.Region != "us-east-1" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
}

func TestCredentialResolverReturnsReaderError(t *testing.T) {
	resolver := &CredentialResolver{Reader: &fakeReader{err: errors.New("boom")}}
	_, err := resolver.ResolveCredential(context.Background(), CredentialRequest{
		Source: CredentialSource{Provider: ProviderVault, Mount: "secret", Path: "cloudflare/prod"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

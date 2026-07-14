package main

import (
	"context"
	"testing"

	"github.com/jlgore/corkscrew/internal/secrets"
)

type fakeSecretReader struct {
	secret map[string]string
	err    error
}

func (f *fakeSecretReader) ReadSecret(_ context.Context, _ secrets.Reference) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.secret, nil
}

func TestResolveAzureCredentialUsesVaultServicePrincipal(t *testing.T) {
	cred, subscriptionID, tenantID, authMethod, err := resolveAzureCredential(context.Background(), map[string]string{
		"auth.secret.mount": "secret",
		"auth.secret.path":  "azure/prod",
	}, "", "", &fakeSecretReader{secret: map[string]string{
		"client_id":       "client-id",
		"client_secret":   "client-secret",
		"tenant_id":       "tenant-id",
		"subscription_id": "subscription-id",
	}})
	if err != nil {
		t.Fatalf("resolveAzureCredential() error = %v", err)
	}
	if cred == nil {
		t.Fatal("credential is nil")
	}
	if subscriptionID != "subscription-id" || tenantID != "tenant-id" {
		t.Fatalf("subscriptionID/tenantID = %q/%q", subscriptionID, tenantID)
	}
	if authMethod != "secret:vault:secret/azure/prod" {
		t.Fatalf("authMethod = %q", authMethod)
	}
}

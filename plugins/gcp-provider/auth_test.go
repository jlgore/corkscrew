package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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

func TestResolveGCPCredentialsUsesVaultServiceAccountJSON(t *testing.T) {
	keyPEM := testPrivateKeyPEM(t)
	material, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   "project-a",
		"private_key":  keyPEM,
		"client_email": "scanner@project-a.iam.gserviceaccount.com",
		"token_uri":    "https://oauth2.googleapis.com/token",
	})
	if err != nil {
		t.Fatalf("marshal service account: %v", err)
	}

	creds, authMethod, err := resolveGCPCredentials(context.Background(), map[string]string{
		"auth.secret.mount": "secret",
		"auth.secret.path":  "gcp/prod",
	}, &fakeSecretReader{secret: map[string]string{
		"service_account_json": string(material),
	}})
	if err != nil {
		t.Fatalf("resolveGCPCredentials() error = %v", err)
	}
	if creds.ProjectID != "project-a" {
		t.Fatalf("ProjectID = %q, want project-a", creds.ProjectID)
	}
	if authMethod != "secret:vault:secret/gcp/prod" {
		t.Fatalf("authMethod = %q", authMethod)
	}
}

func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
}

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

func TestLoadAWSConfigUsesVaultStaticCredentials(t *testing.T) {
	cfg, authMethod, err := loadAWSConfig(context.Background(), map[string]string{
		"region":            "us-east-1",
		"auth.secret.mount": "secret",
		"auth.secret.path":  "aws/prod",
	}, &fakeSecretReader{secret: map[string]string{
		"aws_access_key_id":     "AKIAEXAMPLE",
		"aws_secret_access_key": "secret",
		"aws_session_token":     "session",
	}})
	if err != nil {
		t.Fatalf("loadAWSConfig() error = %v", err)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("Region = %q, want us-east-1", cfg.Region)
	}
	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if creds.AccessKeyID != "AKIAEXAMPLE" || creds.SecretAccessKey != "secret" || creds.SessionToken != "session" {
		t.Fatalf("unexpected credentials: %#v", creds)
	}
	if authMethod != "secret:vault:secret/aws/prod" {
		t.Fatalf("authMethod = %q", authMethod)
	}
}

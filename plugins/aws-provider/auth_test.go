package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jlgore/corkscrew/internal/secrets"
)

// The retryer is set in code, so an environment override cannot change a scan's bounds.
func TestLoadAWSConfigSetsExplicitRetryer(t *testing.T) {
	t.Setenv("AWS_MAX_ATTEMPTS", "1")
	t.Setenv("AWS_RETRY_MODE", "adaptive")
	configMap := map[string]string{"region": "us-east-1"}
	cfg, _, err := loadAWSConfig(context.Background(), configMap, nil)
	if err != nil {
		t.Fatalf("loadAWSConfig() error = %v", err)
	}
	if cfg.Retryer == nil {
		t.Fatal("Retryer is nil")
	}
	retryer := cfg.Retryer()
	if got := retryer.MaxAttempts(); got != retryAttemptsMax {
		t.Fatalf("MaxAttempts = %d, want %d", got, retryAttemptsMax)
	}
	for attempt := 1; attempt <= 10; attempt++ {
		delay, err := retryer.RetryDelay(attempt, errors.New("throttled"))
		if err != nil {
			t.Fatalf("RetryDelay(%d) error = %v", attempt, err)
		}
		if delay > retryBackoffMax {
			t.Fatalf("RetryDelay(%d) = %s, want at most %s", attempt, delay, retryBackoffMax)
		}
	}
}

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

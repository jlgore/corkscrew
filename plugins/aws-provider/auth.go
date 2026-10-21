package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/jlgore/corkscrew/internal/secrets"
)

// The SDK retryer is set explicitly rather than taken from the environment
// (AWS_MAX_ATTEMPTS, AWS_RETRY_MODE) so every scan has the same bounds. Cloud
// Control throttles often under 20 concurrent describes, so allow 5 attempts
// instead of the SDK's 3, but cap one backoff at 5s instead of 20s. The SDK's
// backoff after failed attempt n is up to 2^n s, then the cap, so the worst
// total for one call is 2+4+5+5 = 16s. GetResource's 45s per-call deadline and
// the host's 5-minute scope deadline both cover that. Every client built from
// this config, including copies for other regions and assumed-role accounts,
// inherits it.
const (
	retryAttemptsMax = 5
	retryBackoffMax  = 5 * time.Second
)

func newRetryer() aws.Retryer {
	return retry.NewStandard(func(o *retry.StandardOptions) {
		o.MaxAttempts = retryAttemptsMax
		o.MaxBackoff = retryBackoffMax
	})
}

func loadAWSConfig(ctx context.Context, configMap map[string]string, reader secrets.Reader) (aws.Config, string, error) {
	opts := []func(*config.LoadOptions) error{config.WithRetryer(newRetryer)}
	if region := firstConfig(configMap, "region", "aws.region"); region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, "", fmt.Errorf("failed to load AWS config: %w", err)
	}

	source, err := secrets.CredentialSourceFromConfig(configMap)
	if err != nil {
		return aws.Config{}, "", err
	}
	if !source.Configured() {
		return cfg, "DefaultCredentialChain", nil
	}

	credential, err := (&secrets.CredentialResolver{Reader: reader}).ResolveCredential(ctx, secrets.CredentialRequest{
		Source:      source,
		DefaultKind: secrets.KindAWSStatic,
	})
	if err != nil {
		if source.AllowFallback {
			return cfg, "DefaultCredentialChain:fallback", nil
		}
		return aws.Config{}, "", fmt.Errorf("read AWS auth secret: %w", err)
	}

	if credential.Region != "" {
		cfg.Region = credential.Region
	}

	if credential.AccessKeyID != "" || credential.SecretAccessKey != "" || credential.SessionToken != "" {
		if credential.AccessKeyID == "" || credential.SecretAccessKey == "" {
			return aws.Config{}, "", fmt.Errorf("AWS auth secret requires access_key_id and secret_access_key")
		}
		cfg.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			credential.AccessKeyID,
			credential.SecretAccessKey,
			credential.SessionToken,
		))
	}

	authMethod := credential.Source
	if credential.RoleARN != "" {
		provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), credential.RoleARN, func(options *stscreds.AssumeRoleOptions) {
			options.RoleSessionName = "CorkscrewVaultSession"
			if credential.ExternalID != "" {
				options.ExternalID = aws.String(credential.ExternalID)
			}
		})
		cfg.Credentials = aws.NewCredentialsCache(provider)
		authMethod += ":assume_role"
	}

	if credential.AccessKeyID == "" && credential.RoleARN == "" {
		return aws.Config{}, "", fmt.Errorf("AWS auth secret requires access_key_id/secret_access_key or role_arn")
	}
	return cfg, authMethod, nil
}

func firstConfig(configMap map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(configMap[key]); value != "" {
			return value
		}
	}
	return ""
}

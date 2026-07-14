package cloudflareauth

import (
	"context"
	"fmt"
	"strings"

	"github.com/jlgore/corkscrew/internal/secrets"
)

func (r *DefaultAuthResolver) resolveSecret(ctx context.Context, req ResolveAuthRequest) (*ResolvedAuth, error) {
	cfg := req.Config.Auth.Secret
	if !cfg.Configured() {
		return nil, fmt.Errorf("cloudflare auth secret not configured")
	}

	credential, err := (&secrets.CredentialResolver{Reader: r.SecretReader}).ResolveCredential(ctx, secrets.CredentialRequest{
		Source:      cfg,
		DefaultKind: string(req.Config.Auth.Method),
	})
	if err != nil {
		return nil, fmt.Errorf("read cloudflare auth secret: %w", err)
	}

	method := AuthMethod(credential.Kind)
	if method == "" {
		method = AuthMethodAPIToken
	}

	baseURL := firstNonEmpty(req.Config.Auth.BaseURL, credential.BaseURL)

	switch method {
	case AuthMethodAPIToken:
		token := firstNonEmpty(credential.Token, credential.AccessToken)
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("cloudflare api token field %q not found in auth secret", firstNonEmpty(cfg.TokenField, "api_token"))
		}
		return &ResolvedAuth{
			Method:      AuthMethodAPIToken,
			AccessToken: strings.TrimSpace(token),
			BaseURL:     baseURL,
			Source:      credential.Source,
		}, nil

	case AuthMethodAPIKey:
		apiKey := credential.APIKey
		email := credential.Email
		if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(email) == "" {
			return nil, fmt.Errorf("cloudflare api key/email fields %q/%q not found in auth secret", firstNonEmpty(cfg.APIKeyField, "api_key"), firstNonEmpty(cfg.EmailField, "email"))
		}
		return &ResolvedAuth{
			Method:  AuthMethodAPIKey,
			APIKey:  strings.TrimSpace(apiKey),
			Email:   strings.TrimSpace(email),
			BaseURL: baseURL,
			Source:  credential.Source,
		}, nil

	case AuthMethodOAuth:
		token := firstNonEmpty(credential.AccessToken, credential.Token)
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("cloudflare oauth token field %q not found in auth secret", firstNonEmpty(cfg.TokenField, "api_token"))
		}
		return &ResolvedAuth{
			Method:       AuthMethodOAuth,
			AccessToken:  strings.TrimSpace(token),
			Scopes:       append([]string(nil), credential.Scopes...),
			AccountHints: append([]string(nil), credential.AccountHints...),
			BaseURL:      baseURL,
			Source:       credential.Source,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported secret auth method %q", method)
	}
}

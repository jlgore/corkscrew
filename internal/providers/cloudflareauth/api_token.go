package cloudflareauth

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func (r *DefaultAuthResolver) resolveAPIToken(_ context.Context, req ResolveAuthRequest) (*ResolvedAuth, error) {
	cfg := req.Config.Auth
	token := cfg.Token
	if token == "" {
		env := cfg.TokenEnv
		if env == "" {
			env = "CLOUDFLARE_API_TOKEN"
		}
		token = os.Getenv(env)
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("cloudflare api token not configured")
	}
	return &ResolvedAuth{
		Method:      AuthMethodAPIToken,
		AccessToken: strings.TrimSpace(token),
		BaseURL:     cfg.BaseURL,
		Source:      "api_token",
	}, nil
}

func (r *DefaultAuthResolver) resolveAPIKey(_ context.Context, req ResolveAuthRequest) (*ResolvedAuth, error) {
	cfg := req.Config.Auth
	apiKey := cfg.APIKey
	if apiKey == "" {
		env := cfg.APIKeyEnv
		if env == "" {
			env = "CLOUDFLARE_API_KEY"
		}
		apiKey = os.Getenv(env)
	}
	email := cfg.Email
	if email == "" {
		env := cfg.EmailEnv
		if env == "" {
			env = "CLOUDFLARE_EMAIL"
		}
		email = os.Getenv(env)
	}
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(email) == "" {
		return nil, fmt.Errorf("cloudflare api key/email not configured")
	}
	return &ResolvedAuth{
		Method:  AuthMethodAPIKey,
		APIKey:  strings.TrimSpace(apiKey),
		Email:   strings.TrimSpace(email),
		BaseURL: cfg.BaseURL,
		Source:  "api_key",
	}, nil
}

package cloudflareauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TokenValidator verifies that resolved credentials are valid with Cloudflare's API.
type TokenValidator interface {
	ValidateToken(ctx context.Context, auth *ResolvedAuth) (*ValidationResult, error)
}

// ValidationResult carries the outcome of a credential validation call.
type ValidationResult struct {
	Valid     bool
	GrantType string // e.g. "user", "service"
	AccountID string // primary account when available
	ExpiresIn time.Duration
	Errors    []string
}

// CloudflareTokenValidator performs a lightweight HTTP call to Cloudflare to verify credentials.
type CloudflareTokenValidator struct {
	HTTPClient *http.Client
	BaseURL    string
}

func (v *CloudflareTokenValidator) httpClient() *http.Client {
	if v.HTTPClient != nil {
		return v.HTTPClient
	}
	return http.DefaultClient
}

func (v *CloudflareTokenValidator) baseURL() string {
	if v.BaseURL != "" {
		return v.BaseURL
	}
	return "https://api.cloudflare.com/client/v4"
}

// ValidateToken checks credentials against the Cloudflare API.
// For API tokens it uses /user/tokens/verify.
// For API keys and OAuth it uses a lightweight /zones?per_page=1 call.
func (v *CloudflareTokenValidator) ValidateToken(ctx context.Context, auth *ResolvedAuth) (*ValidationResult, error) {
	switch auth.Method {
	case AuthMethodAPIToken:
		return v.validateAPIToken(ctx, auth.AccessToken)
	case AuthMethodAPIKey:
		return v.validateAPIKey(ctx, auth.APIKey, auth.Email)
	case AuthMethodOAuth:
		// OAuth tokens use the same transport as API tokens; validate with a lightweight call.
		return v.validateAPIToken(ctx, auth.AccessToken)
	default:
		return nil, fmt.Errorf("unknown auth method %q", auth.Method)
	}
}

func (v *CloudflareTokenValidator) validateAPIToken(ctx context.Context, token string) (*ValidationResult, error) {
	result := &ValidationResult{Valid: false}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL()+"/user/tokens/verify", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := v.httpClient().Do(req)
	if err != nil {
		result.Errors = append(result.Errors, "validation request failed: "+err.Error())
		return result, nil
	}
	defer resp.Body.Close()

	var payload struct {
		Success bool `json:"success"`
		Result  struct {
			ID      string   `json:"id"`
			Status  string   `json:"status"`
			Expires string   `json:"expires_on"`
			Scopes  []string `json:"scopes"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		result.Errors = append(result.Errors, "decode failed: "+err.Error())
		return result, nil
	}
	result.Valid = payload.Success && resp.StatusCode == http.StatusOK && payload.Result.Status == "active"
	for _, e := range payload.Errors {
		result.Errors = append(result.Errors, e.Message)
	}
	if payload.Result.Expires != "" && payload.Result.Expires != "never" {
		if t, err := time.Parse(time.RFC3339, payload.Result.Expires); err == nil {
			result.ExpiresIn = time.Until(t)
		}
	}
	return result, nil
}

func (v *CloudflareTokenValidator) validateAPIKey(ctx context.Context, apiKey, email string) (*ValidationResult, error) {
	result := &ValidationResult{Valid: false}

	// Use a lightweight zones list with per_page=1 to minimize data transfer.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL()+"/zones?per_page=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Auth-Key", apiKey)
	req.Header.Set("X-Auth-Email", email)

	resp, err := v.httpClient().Do(req)
	if err != nil {
		result.Errors = append(result.Errors, "validation request failed: "+err.Error())
		return result, nil
	}
	defer resp.Body.Close()

	var payload struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		result.Errors = append(result.Errors, "decode failed: "+err.Error())
		return result, nil
	}
	result.Valid = payload.Success && resp.StatusCode == http.StatusOK
	for _, e := range payload.Errors {
		result.Errors = append(result.Errors, e.Message)
	}
	return result, nil
}

// NoopTokenValidator always returns valid for tests.
type NoopTokenValidator struct{}

func (n *NoopTokenValidator) ValidateToken(_ context.Context, _ *ResolvedAuth) (*ValidationResult, error) {
	return &ValidationResult{Valid: true}, nil
}

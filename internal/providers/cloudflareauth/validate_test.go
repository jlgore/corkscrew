package cloudflareauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCloudflareTokenValidatorAPIToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/tokens/verify" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "errors": []map[string]string{{"message": "invalid token"}}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result": map[string]interface{}{
				"id":         "01",
				"status":     "active",
				"expires_on": time.Now().Add(time.Hour).Format(time.RFC3339),
				"scopes":     []string{"zone:read"},
			},
		})
	}))
	defer ts.Close()

	v := &CloudflareTokenValidator{BaseURL: ts.URL}

	// Good token
	result, err := v.ValidateToken(context.Background(), &ResolvedAuth{Method: AuthMethodAPIToken, AccessToken: "good-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatal("expected valid token")
	}
	if result.ExpiresIn <= 0 {
		t.Fatal("expected positive expires_in")
	}

	// Bad token
	result, err = v.ValidateToken(context.Background(), &ResolvedAuth{Method: AuthMethodAPIToken, AccessToken: "bad-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid token")
	}
}

func TestCloudflareTokenValidatorAPIKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Auth-Key") != "key" || r.Header.Get("X-Auth-Email") != "me@example.com" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "result": []string{}})
	}))
	defer ts.Close()

	v := &CloudflareTokenValidator{BaseURL: ts.URL}
	result, err := v.ValidateToken(context.Background(), &ResolvedAuth{Method: AuthMethodAPIKey, APIKey: "key", Email: "me@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatal("expected valid api key")
	}
}

func TestNoopValidatorAlwaysValid(t *testing.T) {
	v := &NoopTokenValidator{}
	result, err := v.ValidateToken(context.Background(), &ResolvedAuth{Method: AuthMethodAPIToken})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatal("expected noop validator to always return valid")
	}
}

func TestNoopRefresherReturnsOriginal(t *testing.T) {
	original := &OAuthProfile{AccessToken: "orig"}
	r := &NoopTokenRefresher{}
	got, err := r.RefreshToken(context.Background(), original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != original {
		t.Fatal("expected noop refresher to return original profile")
	}
}

package secrets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestVaultReaderReadsKVV2Secret(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/cloudflare/prod" {
			t.Fatalf("path = %q, want /v1/secret/data/cloudflare/prod", r.URL.Path)
		}
		if r.URL.Query().Get("version") != "3" {
			t.Fatalf("version = %q, want 3", r.URL.Query().Get("version"))
		}
		if r.Header.Get("X-Vault-Token") != "vault-token" {
			t.Fatalf("vault token header mismatch")
		}
		if r.Header.Get("X-Vault-Namespace") != "admin" {
			t.Fatalf("vault namespace header mismatch")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"api_token": "cf-token",
					"scopes":    []string{"Zone:Read", "DNS:Read"},
					"enabled":   true,
					"count":     12,
				},
			},
		})
	}))
	defer ts.Close()

	reader := &VaultReader{HTTPClient: ts.Client()}
	got, err := reader.ReadSecret(context.Background(), Reference{
		Provider:  ProviderVault,
		Engine:    EngineKVV2,
		Address:   ts.URL,
		Token:     "vault-token",
		Namespace: "admin",
		Mount:     "secret",
		Path:      "cloudflare/prod",
		Version:   3,
	})
	if err != nil {
		t.Fatalf("ReadSecret() error = %v", err)
	}
	want := map[string]string{
		"api_token": "cf-token",
		"scopes":    "Zone:Read,DNS:Read",
		"enabled":   "true",
		"count":     "12",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadSecret() = %#v, want %#v", got, want)
	}
}

func TestVaultReaderReadsKVV1Secret(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kv/cloudflare/prod" {
			t.Fatalf("path = %q, want /v1/kv/cloudflare/prod", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"api_key": "key",
				"email":   "user@example.com",
			},
		})
	}))
	defer ts.Close()

	reader := &VaultReader{HTTPClient: ts.Client()}
	got, err := reader.ReadSecret(context.Background(), Reference{
		Provider: ProviderVault,
		Engine:   EngineKVV1,
		Address:  ts.URL,
		Token:    "vault-token",
		Mount:    "kv",
		Path:     "cloudflare/prod",
	})
	if err != nil {
		t.Fatalf("ReadSecret() error = %v", err)
	}
	want := map[string]string{"api_key": "key", "email": "user@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadSecret() = %#v, want %#v", got, want)
	}
}

func TestVaultReaderReturnsVaultErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{"permission denied"},
		})
	}))
	defer ts.Close()

	reader := &VaultReader{HTTPClient: ts.Client()}
	_, err := reader.ReadSecret(context.Background(), Reference{
		Provider: ProviderVault,
		Address:  ts.URL,
		Token:    "vault-token",
		Mount:    "secret",
		Path:     "cloudflare/prod",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

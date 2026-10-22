package main

import (
	"strings"
	"testing"
)

func TestClassifyBindingValue(t *testing.T) {
	tests := []struct {
		value  string
		reason string // empty: not a credential
	}{
		// AWS's documented example key: real-shaped, never valid.
		{"AKIAIOSFODNN7EXAMPLE", "prefix:aws_access_key"},
		{"ghp_" + strings.Repeat("a1B2", 9), "prefix:github_token"},
		{"github_pat_" + strings.Repeat("Ab1_", 6), "prefix:github_pat"},
		{"sk_live_" + strings.Repeat("Xy9", 6), "prefix:stripe_live_key"},
		{"xoxb-1234567890-abcdefghij", "prefix:slack_token"},
		{"glpat-" + strings.Repeat("aB3-", 5), "prefix:gitlab_token"},
		{"AIza" + strings.Repeat("Sy9_x", 7), "prefix:google_api_key"},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIE...", "prefix:private_key"},
		{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.c2lnbmF0dXJlc2lnbmF0dXJl", "prefix:jwt"},
		{"postgres://app:hunter2@db.example.com:5432/app", "prefix:url_credentials"},
		{"q8Zr2LmX0vTn5KpW3sYb7HdJ4cQe", "entropy"},

		// Values that must stay negative.
		{"true", ""},
		{"production", ""},
		{"1.24.3", ""},
		{"https://api.example.com/v1/users", ""},
		{"db.internal.example.com", ""},
		{"2025-09-01T10:00:00Z", ""},
		{"c5a6d4b0-6b39-4f62-9b7d-2f7a1a2c3d4e", ""},     // UUID
		{"0f2ac74b498b48028cb68387c421e279", ""},         // 32-hex ID
		{"ea95132c15732412d22c1476fa83f27aea95132c", ""}, // 40-hex hash
		{"api/v1/users/list/all/active", ""},
		{"SomeLongCamelCaseIdentifierName", ""}, // no digits
		{"Short1Ab", ""},
	}
	for _, tt := range tests {
		looks, reason := classifyBindingValue(tt.value)
		if looks != (tt.reason != "") || reason != tt.reason {
			t.Errorf("classifyBindingValue(%q) = %t, %q; want %q", tt.value, looks, reason, tt.reason)
		}
	}
}

func TestClassifyJSONValueReportsPathNotValue(t *testing.T) {
	value := map[string]any{
		"region": "us",
		"upstreams": []any{
			map[string]any{"url": "https://cdn.example.com"},
			map[string]any{"url": "postgres://app:hunter2@db.example.com/app"},
		},
	}
	looks, reason := classifyJSONValue(value)
	if !looks || reason != "json:$.upstreams[1].url:prefix:url_credentials" {
		t.Fatalf("classifyJSONValue = %t, %q", looks, reason)
	}
	if strings.Contains(reason, "hunter2") {
		t.Fatalf("reason %q repeats the value", reason)
	}
}

func TestClassifyJSONValueRedactsCredentialKeys(t *testing.T) {
	key := "AKIAIOSFODNN7EXAMPLE"
	looks, reason := classifyJSONValue(map[string]any{key: map[string]any{"secret": "q8Zr2LmX0vTn5KpW3sYb7HdJ4cQe"}})
	if !looks || strings.Contains(reason, key) || !strings.Contains(reason, "[redacted]") {
		t.Fatalf("classifyJSONValue = %t, %q; want the key redacted", looks, reason)
	}
}

func TestClassifyJSONValueIsBounded(t *testing.T) {
	// Deeper than classifyJSONDepthMax: the credential at the bottom is not reached.
	var nested any = "AKIAIOSFODNN7EXAMPLE"
	for range classifyJSONDepthMax + 2 {
		nested = map[string]any{"a": nested}
	}
	if looks, _ := classifyJSONValue(nested); looks {
		t.Error("classifier walked past its depth bound")
	}
	// More string leaves than classifyJSONLeavesMax before the credential.
	leaves := make([]any, classifyJSONLeavesMax+1)
	for i := range leaves {
		leaves[i] = "x"
	}
	leaves = append(leaves, "AKIAIOSFODNN7EXAMPLE")
	if looks, _ := classifyJSONValue(leaves); looks {
		t.Error("classifier read past its leaf bound")
	}
}

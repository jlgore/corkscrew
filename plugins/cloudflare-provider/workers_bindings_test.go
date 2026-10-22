package main

import (
	"context"
	"strings"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/cloudflare-provider/internal/fakecf"
	"google.golang.org/protobuf/encoding/protojson"
)

func bindingAttrs(t *testing.T, resp *pb.ScanServiceResponse) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for _, r := range resp.Resources {
		if r.Type == "worker_binding" {
			attrs := decodeAttrs(r.Attributes)
			out[attrs["script"]+"/"+r.Name] = attrs
			if r.ParentId != workerScriptID(fixtureAccountID, attrs["script"]) {
				t.Errorf("%s parent = %q", r.Id, r.ParentId)
			}
		}
	}
	return out
}

func TestScanWorkersBindingsFromBaseline(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	resp := scanService(t, p, "workers")
	if len(resp.Errors) > 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	const store = "023e105f4ecef8ad9ca31a8372d0c356"
	want := map[string]map[string]string{
		"api-gateway/SESSIONS":            {"kind": "kv_namespace", "target_ref": "0f2ac74b498b48028cb68387c421e279"},
		"api-gateway/UPLOADS":             {"kind": "r2_bucket", "target_ref": "uploads"},
		"api-gateway/DB":                  {"kind": "d1", "target_ref": "c5a6d4b0-6b39-4f62-9b7d-2f7a1a2c3d4e"},
		"api-gateway/AUDIT":               {"kind": "queue", "target_ref": "audit-events"},
		"api-gateway/RATE_LIMITER":        {"kind": "durable_object_namespace", "target_ref": "api-gateway#RateLimiter"},
		"api-gateway/AUTH":                {"kind": "service", "target_ref": "internal-auth", "environment": "production"},
		"api-gateway/STRIPE":              {"kind": "secrets_store_secret", "target_ref": store + "/STRIPE_KEY"},
		"api-gateway/SESSION_SIGNING_KEY": {"kind": "secret_text"},
		"api-gateway/API_BASE_URL": {
			"kind": "plain_text", "looks_like_credential": "false", "credential_reason": "", "value_length": "26",
		},
		"api-gateway/LEGACY_AWS_ACCESS_KEY": {
			"kind": "plain_text", "looks_like_credential": "true", "credential_reason": "prefix:aws_access_key",
		},
		"api-gateway/UPSTREAMS": {
			"kind": "json", "looks_like_credential": "true",
			"credential_reason": "json:$.primary.url:prefix:url_credentials",
		},
		"api-gateway/NOTIFY":          {"kind": "send_email", "destination_restricted": "false"},
		"internal-auth/SIGNING_TOKEN": {"looks_like_credential": "true", "credential_reason": "entropy"},
		"internal-auth/DEBUG":         {"looks_like_credential": "false"},
		"internal-auth/WEBHOOK":       {"target_ref": store + "/LEGACY_WEBHOOK_SECRET"},
		"billing-cron/REPORTS":        {"kind": "send_email", "destination_restricted": "true"},
		"billing-cron/STAGING_CACHE":  {"kind": "kv_namespace"},
	}
	got := bindingAttrs(t, resp)
	if len(got) != 24 {
		t.Errorf("%d bindings, want 24", len(got))
	}
	for binding, attrs := range want {
		if got[binding] == nil {
			t.Errorf("no binding %s", binding)
			continue
		}
		for key, value := range attrs {
			if got[binding][key] != value {
				t.Errorf("%s %s = %q, want %q", binding, key, got[binding][key], value)
			}
		}
	}
}

// No binding value may leave the plugin: not in attributes, not in raw data, not in any field
// of the scan response. Every value-bearing field carries a sentinel, and the whole response is
// searched for it.
func TestBindingValuesNeverLeaveThePlugin(t *testing.T) {
	const sentinel = "SENTINELq8Zr2LmX0vTn5KpW3sYb"
	srv := fakecf.New(t)
	srv.SetList("GET /accounts/{account_id}/workers/scripts", fakecf.Params{"account_id": fixtureAccountID},
		map[string]any{"id": "leaky"})
	srv.SetResult(subdomainOp, scriptParams("leaky"), map[string]any{"enabled": false, "previews_enabled": false})
	srv.SetResult(settingsOp, scriptParams("leaky"), map[string]any{"bindings": []any{
		map[string]any{"type": "plain_text", "name": "A", "text": sentinel},
		map[string]any{"type": "secret_text", "name": "B", "text": sentinel},
		map[string]any{"type": "json", "name": "C", "json": map[string]any{"nested": []any{sentinel}, sentinel: 1}},
		map[string]any{"type": "secret_key", "name": "D", "algorithm": map[string]any{"name": "HMAC"},
			"format": "raw", "usages": []any{"sign"}, "key_base64": sentinel},
		map[string]any{"type": "secret_key", "name": "E", "algorithm": map[string]any{"name": "HMAC"},
			"format": "jwk", "usages": []any{"sign"}, "key_jwk": map[string]any{"k": sentinel}},
	}})
	p := newTestProvider(t, srv)
	p.config.Scope.AccountIDs = []string{fixtureAccountID}

	resp := scanService(t, p, "workers")
	encoded, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), sentinel) {
		t.Fatalf("a binding value left the plugin:\n%s", encoded)
	}
	if n := len(bindingAttrs(t, resp)); n != 5 {
		t.Fatalf("%d bindings, want 5", n)
	}
}

func TestDescribeWorkerBinding(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	id := workerBindingID(fixtureAccountID, "api-gateway", "UPLOADS")
	got, err := p.describeByType(context.Background(), &pb.ResourceRef{Type: "worker_binding", Id: id})
	if err != nil {
		t.Fatal(err)
	}
	if got.Id != id || decodeAttrs(got.Attributes)["target_ref"] != "uploads" {
		t.Errorf("describe = %s %s", got.Id, got.Attributes)
	}
	if _, err := p.describeByType(context.Background(), &pb.ResourceRef{Type: "worker_binding", Id: "nope"}); err == nil {
		t.Error("describe accepted a malformed binding ID")
	}
}

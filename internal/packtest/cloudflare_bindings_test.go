package packtest

import "testing"

func binding(script, name string) string {
	return cloudflareAccount + "/workers/" + script + "/bindings/" + name
}

const (
	stripeKey      = "3fd85f74b32742f1bff64a85009dda07"
	legacyWebhook  = "3fd85f74b32742f1bff64a85009dda08"
	sessionsKV     = "0f2ac74b498b48028cb68387c421e279"
	stagingKV      = "2c1d5e8f9a0b4c3d8e7f6a5b4c3d2e1f"
	billingD1      = "c5a6d4b0-6b39-4f62-9b7d-2f7a1a2c3d4e"
	bindingsPack   = "cloudflare/workers-bindings"
	approvedReport = "billing-cron:send_email"
)

func TestCloudflareWorkersBindingsPack(t *testing.T) {
	params := map[string]any{"privileged_binding_allowlist": []any{approvedReport}}
	outcomes := Run(t, cloudflareTarget(t), bindingsPack, params)
	outcomes.Check(t, []Expect{
		// Privileged kinds: unapproved email and mTLS / approved email.
		{"CF.BND.01", binding("api-gateway", "NOTIFY"), "FAIL"},
		{"CF.BND.01", binding("api-gateway", "UPSTREAM_CERT"), "FAIL"},
		{"CF.BND.01", binding("billing-cron", "REPORTS"), "PASS"},
		// Email with no destination restriction / with one.
		{"CF.BND.02", binding("api-gateway", "NOTIFY"), "FAIL"},
		{"CF.BND.02", binding("billing-cron", "REPORTS"), "PASS"},
		// Staging Worker on the production store's secret / on a staging namespace / a
		// production-ish Worker on the production secret.
		{"CF.BND.03", binding("checkout-staging", "STRIPE"), "FAIL"},
		{"CF.BND.03", binding("checkout-staging", "SESSIONS"), "PASS"},
		{"CF.BND.03", binding("api-gateway", "STRIPE"), "PASS"},
		// STRIPE_KEY is bound by 3 Workers (secret limit 2); sessions by 2 (storage limit 3).
		{"CF.BND.04", stripeKey, "FAIL"},
		{"CF.BND.04", legacyWebhook, "PASS"},
		{"CF.BND.04", sessionsKV, "PASS"},
		{"CF.BND.04", billingD1, "PASS"},
		// Internal Workers behind service bindings: on workers.dev / off it.
		{"CF.BND.05", script("internal-auth"), "FAIL"},
		{"CF.BND.05", script("billing-cron"), "PASS"},
		// A binding to a KV namespace that doesn't exist / to one that does.
		{"CF.BND.06", binding("billing-cron", "STAGING_CACHE"), "FAIL"},
		{"CF.BND.06", binding("api-gateway", "SESSIONS"), "PASS"},
	})
	// Bindings Corkscrew can't resolve are never called dangling.
	for _, b := range []string{binding("api-gateway", "NOTIFY"), binding("api-gateway", "API_BASE_URL")} {
		if _, ok := outcomes["CF.BND.06"][b]; ok {
			t.Errorf("CF.BND.06 evaluated %s, whose target can't be checked", b)
		}
	}
	// Workers reached through a route aren't internal.
	if _, ok := outcomes["CF.BND.05"][script("api-gateway")]; ok {
		t.Error("CF.BND.05 evaluated api-gateway, which has a route")
	}
}

// Raising the secret limit clears the fan-in finding; the defaults are only defaults.
func TestCloudflareWorkersBindingsFanInThreshold(t *testing.T) {
	params := map[string]any{"max_secret_fan_in": 3}
	outcomes := Run(t, cloudflareTarget(t), bindingsPack, params)
	if got := outcomes["CF.BND.04"][stripeKey]; got != "PASS" {
		t.Errorf("STRIPE_KEY with max_secret_fan_in 3 = %s, want PASS", got)
	}
}

func TestCloudflareWorkersBindingsEmptyInventory(t *testing.T) {
	target := Persist(t, "cloudflare", Golden{Services: []string{"workers"}})
	for _, result := range Results(t, target, bindingsPack, nil) {
		if result.Error != nil || len(result.Rows) != 0 {
			t.Errorf("%s on an empty inventory: %d rows, error %v", result.ControlID, len(result.Rows), result.Error)
		}
	}
}

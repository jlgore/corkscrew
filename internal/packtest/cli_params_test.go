package packtest

import "testing"

// The CLI's --param parses a number as a string ("30"), and a single list item as a plain
// string. Thresholds and allowlists must work in both forms.
func TestCloudflarePacksAcceptCLIParameterForms(t *testing.T) {
	target := cloudflareTarget(t)

	secrets := Run(
		t,
		target,
		secretsPack,
		map[string]any{"as_of_date": "2025-10-01", "max_secret_age_days": "30"},
	)
	// STRIPE_KEY was modified 61 days before as_of_date: over a 30-day limit.
	if got := secrets["CF.SEC.03"][stripeKey]; got != "FAIL" {
		t.Errorf("CF.SEC.03 STRIPE_KEY with max_secret_age_days \"30\" = %s, want FAIL", got)
	}

	bindings := Run(t, target, bindingsPack, map[string]any{
		"privileged_binding_allowlist": approvedReport, // one item: a string, not a list
		"max_secret_fan_in":            "3",
	})
	if got := bindings["CF.BND.01"][binding("billing-cron", "REPORTS")]; got != "PASS" {
		t.Errorf("CF.BND.01 with a one-item allowlist string = %s, want PASS", got)
	}
	if got := bindings["CF.BND.04"][stripeKey]; got != "PASS" {
		t.Errorf("CF.BND.04 with max_secret_fan_in \"3\" = %s, want PASS", got)
	}

	baseline := Run(
		t,
		target,
		"cloudflare/workers-baseline",
		map[string]any{"as_of_date": "2026-01-01", "max_compat_age_days": "100"},
	)
	// api-gateway's 2025-09-01 is 122 days before 2026-01-01.
	if got := baseline["CF.WRK.04"][script("api-gateway")]; got != "FAIL" {
		t.Errorf("CF.WRK.04 api-gateway with max_compat_age_days \"100\" = %s, want FAIL", got)
	}
}

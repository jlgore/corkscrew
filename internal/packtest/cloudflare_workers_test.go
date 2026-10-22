package packtest

import "testing"

func script(name string) string { return cloudflareAccount + "/workers/" + name }

// The golden scan's compatibility dates are fixed, so ages are measured to a fixed date.
var baselineParams = map[string]any{"as_of_date": "2026-01-01"}

func TestCloudflareWorkersBaselinePack(t *testing.T) {
	outcomes := Run(t, cloudflareTarget(t), "cloudflare/workers-baseline", baselineParams)
	outcomes.Check(t, []Expect{
		// Routed and on workers.dev / routed and off it.
		{"CF.WRK.01", script("api-gateway"), "FAIL"},
		{"CF.WRK.01", script("www-site"), "PASS"},
		// Routed with Preview URLs on / off.
		{"CF.WRK.02", script("www-site"), "FAIL"},
		{"CF.WRK.02", script("api-gateway"), "PASS"},
		// No observability at all / logs explicitly off / logs on / logs on by default.
		{"CF.WRK.03", script("internal-auth"), "FAIL"},
		{"CF.WRK.03", script("billing-cron"), "FAIL"},
		{"CF.WRK.03", script("api-gateway"), "PASS"},
		{"CF.WRK.03", script("www-site"), "PASS"},
		// 2023-05-18 is 958 days before 2026-01-01; the rest are under 365.
		{"CF.WRK.04", script("internal-auth"), "FAIL"},
		{"CF.WRK.04", script("api-gateway"), "PASS"},
		{"CF.WRK.04", script("billing-cron"), "PASS"},
		// example.com/old/* names legacy-redirect, which doesn't exist.
		{"CF.WRK.05", "023e105f4ecef8ad9ca31a8372d0c358", "FAIL"},
		{"CF.WRK.05", "023e105f4ecef8ad9ca31a8372d0c354", "PASS"},
	})
	// Scripts without a route or domain aren't evaluated by the exposure controls.
	for _, control := range []string{"CF.WRK.01", "CF.WRK.02"} {
		if _, ok := outcomes[control][script("internal-auth")]; ok {
			t.Errorf("%s evaluated internal-auth, which has no route or domain", control)
		}
	}
}

func TestCloudflareWorkersBaselineAllowlist(t *testing.T) {
	params := map[string]any{"as_of_date": "2026-01-01", "workers_dev_allowlist": []any{"api-gateway"}}
	outcomes := Run(t, cloudflareTarget(t), "cloudflare/workers-baseline", params)
	if got := outcomes["CF.WRK.01"][script("api-gateway")]; got != "PASS" {
		t.Errorf("allowlisted api-gateway = %s, want PASS", got)
	}
}

// With no parameters and nothing scanned, every control runs and returns nothing.
func TestCloudflareWorkersBaselineEmptyInventory(t *testing.T) {
	target := Persist(t, "cloudflare", Golden{Services: []string{"workers"}})
	for _, result := range Results(t, target, "cloudflare/workers-baseline", nil) {
		if result.Error != nil || len(result.Rows) != 0 {
			t.Errorf("%s on an empty inventory: %d rows, error %v", result.ControlID, len(result.Rows), result.Error)
		}
	}
}

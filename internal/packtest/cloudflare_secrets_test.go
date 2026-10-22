package packtest

import (
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

const (
	secretsPack        = "cloudflare/workers-secrets"
	oldSendgridKey     = "3fd85f74b32742f1bff64a85009dda09"
	rotatingSigningKey = "3fd85f74b32742f1bff64a85009dda0a"
)

// Secret ages are measured to a fixed date against the golden's fixed modification times.
var secretsParams = map[string]any{"as_of_date": "2025-10-01"}

func TestCloudflareWorkersSecretsPack(t *testing.T) {
	outcomes := Run(t, cloudflareTarget(t), secretsPack, secretsParams)
	outcomes.Check(t, []Expect{
		// AWS's documented example key, a URL with a password inside JSON, a random token /
		// a URL and a flag.
		{"CF.SEC.01", binding("api-gateway", "LEGACY_AWS_ACCESS_KEY"), "FAIL"},
		{"CF.SEC.01", binding("api-gateway", "UPSTREAMS"), "FAIL"},
		{"CF.SEC.01", binding("internal-auth", "SIGNING_TOKEN"), "FAIL"},
		{"CF.SEC.01", binding("api-gateway", "API_BASE_URL"), "PASS"},
		{"CF.SEC.01", binding("internal-auth", "DEBUG"), "PASS"},
		// Plaintext variables named like secrets / not.
		{"CF.SEC.02", binding("api-gateway", "LEGACY_AWS_ACCESS_KEY"), "FAIL"},
		{"CF.SEC.02", binding("internal-auth", "SIGNING_TOKEN"), "FAIL"},
		{"CF.SEC.02", binding("api-gateway", "API_BASE_URL"), "PASS"},
		// Modified 2023-01-01 and 2024-02-01 / 2025-08-01 and 2025-09-20, against 2025-10-01.
		{"CF.SEC.03", legacyWebhook, "FAIL"},
		{"CF.SEC.03", oldSendgridKey, "FAIL"},
		{"CF.SEC.03", stripeKey, "PASS"},
		{"CF.SEC.03", rotatingSigningKey, "PASS"},
		// Scoped to Workers and bound by none / bound.
		{"CF.SEC.04", oldSendgridKey, "FAIL"},
		{"CF.SEC.04", stripeKey, "PASS"},
		{"CF.SEC.04", legacyWebhook, "PASS"},
		// SENDGRID_API_KEY is set in 3 Workers / SESSION_SIGNING_KEY in 1.
		{"CF.SEC.05", binding("api-gateway", "SENDGRID_API_KEY"), "FAIL"},
		{"CF.SEC.05", binding("billing-cron", "SENDGRID_API_KEY"), "FAIL"},
		{"CF.SEC.05", binding("api-gateway", "SESSION_SIGNING_KEY"), "PASS"},
		// Also scoped to ai_gateway / Workers only.
		{"CF.SEC.06", legacyWebhook, "FAIL"},
		{"CF.SEC.06", stripeKey, "PASS"},
		// Bound while pending / bound and active.
		{"CF.SEC.07", binding("billing-cron", "SIGNING"), "FAIL"},
		{"CF.SEC.07", binding("api-gateway", "STRIPE"), "PASS"},
	})
}

func TestCloudflareWorkersSecretsAllowedScopes(t *testing.T) {
	params := map[string]any{"as_of_date": "2025-10-01", "allowed_secret_scopes": []any{"workers", "ai_gateway"}}
	outcomes := Run(t, cloudflareTarget(t), secretsPack, params)
	if got := outcomes["CF.SEC.06"][legacyWebhook]; got != "PASS" {
		t.Errorf("LEGACY_WEBHOOK_SECRET with ai_gateway allowed = %s, want PASS", got)
	}
}

// Without the workers service in the scan, no secret can look unused.
func TestCloudflareWorkersSecretsUnusedNeedsWorkers(t *testing.T) {
	golden := LoadGolden(t, cloudflareGolden)
	var data []*pb.Resource
	for _, r := range golden.Resources {
		if r.Service == "data" {
			data = append(data, r)
		}
	}
	target := Persist(t, "cloudflare", Golden{Services: []string{"data"}, Resources: data})
	for _, result := range Results(t, target, secretsPack, secretsParams) {
		if result.ControlID == "CF.SEC.04" && len(result.Rows) != 0 {
			t.Errorf("CF.SEC.04 evaluated %d secrets without a workers scan", len(result.Rows))
		}
	}
}

func TestCloudflareWorkersSecretsEmptyInventory(t *testing.T) {
	target := Persist(t, "cloudflare", Golden{Services: []string{"workers", "data"}})
	for _, result := range Results(t, target, secretsPack, nil) {
		if result.Error != nil || len(result.Rows) != 0 {
			t.Errorf("%s on an empty inventory: %d rows, error %v", result.ControlID, len(result.Rows), result.Error)
		}
	}
}

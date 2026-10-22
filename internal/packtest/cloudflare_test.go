package packtest

import (
	"context"
	"slices"
	"testing"

	"github.com/jlgore/corkscrew/internal/app/findings"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
)

const (
	cloudflareGolden  = "testdata/cloudflare/golden-scan.json"
	cloudflareAccount = "9a7806061c88ada191ed06f989cc3dac"
)

func cloudflareTarget(t *testing.T) string {
	t.Helper()
	return Persist(t, "cloudflare", LoadGolden(t, cloudflareGolden))
}

func TestCloudflareGoldenPersists(t *testing.T) {
	target := cloudflareTarget(t)
	session, err := dataaccess.OpenSession(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	var bindings int
	if err := session.QueryRowContext(context.Background(),
		`SELECT count(*) FROM cloudflare_resources WHERE type = 'worker_binding'`,
	).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 28 {
		t.Errorf("%d worker_binding rows, want 28", bindings)
	}

	rows, err := session.QueryContext(context.Background(),
		`SELECT from_id, to_resource_type FROM cloud_relationships
		 WHERE provider = 'cloudflare' AND relationship_type = 'binds' ORDER BY from_id, to_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	perScript := map[string][]string{}
	for rows.Next() {
		var from, toType string
		if err := rows.Scan(&from, &toType); err != nil {
			t.Fatal(err)
		}
		perScript[from] = append(perScript[from], toType)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	billing := perScript[cloudflareAccount+"/workers/billing-cron"]
	if len(perScript[cloudflareAccount+"/workers/api-gateway"]) != 7 ||
		!slices.Contains(billing, "d1_database") {
		t.Errorf("binds edges in cloud_relationships: %v", perScript)
	}
}

func TestCloudflareSmokePack(t *testing.T) {
	outcomes := Run(t, cloudflareTarget(t), "test/cloudflare-smoke", nil)
	outcomes.Check(t, []Expect{
		{"SMOKE.01", cloudflareAccount + "/workers/api-gateway", "FAIL"},
		{"SMOKE.01", cloudflareAccount + "/workers/internal-auth", "FAIL"},
		{"SMOKE.01", cloudflareAccount + "/workers/billing-cron", "PASS"},
	})
}

// Finding evaluation, end to end: the smoke pack's failures open findings against the golden
// scan, through the same snapshot hydration a real `corkscrew findings evaluate` uses.
func TestCloudflareSmokePackOpensFindings(t *testing.T) {
	target := cloudflareTarget(t)
	t.Setenv("HOME", t.TempDir())
	evaluation, err := findings.Evaluate(context.Background(), findings.EvaluateRequest{
		Target: target, ScanID: ScanID, PackName: "test/cloudflare-smoke",
	})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Opened != 2 || evaluation.Resolved != 0 {
		t.Fatalf(
			"opened %d, resolved %d; want the 2 workers.dev scripts opened",
			evaluation.Opened,
			evaluation.Resolved,
		)
	}
}

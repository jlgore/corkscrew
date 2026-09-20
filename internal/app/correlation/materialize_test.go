package correlation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dataaccess "github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
	contract "github.com/jlgore/corkscrew/pkg/correlation"
)

func TestRefreshMaterializesEvidenceAndPreservesExternalRows(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "correlation.duckdb")
	attributes, err := contract.Attach("", contract.Evidence{ID: "public", Kind: contract.KindIP, Confidence: .9,
		Method: "fixture", Values: map[string]any{"ip_address": "203.0.113.8", "ip_version": "ipv4", "ip_type": "public"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := session.PersistScanOutcome(ctx, dataaccess.ScanOutcome{ID: "scan", Provider: "acme", Services: []string{"network"}, Scopes: []string{"global"},
		Status: dataaccess.ScanStatusCompleted, StartedAt: now, EndedAt: now, Resources: []*pb.Resource{{Provider: "acme", Id: "resource", Name: "r", Type: "ip", Region: "global", Attributes: attributes}}}, dataaccess.PersistScanOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = session.Close()
	database, err := db.OpenDuckDB(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO cross_cloud_ip_addresses(id, ip_address, ip_version, ip_type, resource_id, resource_type, provider, region)
VALUES ('external', '198.51.100.2', 'ipv4', 'public', 'external-r', 'ip', 'other', 'global')`); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	result, err := Refresh(ctx, Request{Target: target, Kinds: []contract.Kind{contract.KindIP}})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowCount != 1 {
		t.Fatalf("rows = %d", result.RowCount)
	}
	database, _ = db.OpenDuckDB(ctx, target)
	defer database.Close()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM cross_cloud_ip_addresses`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("IP rows = %d, want generated + external", count)
	}
}

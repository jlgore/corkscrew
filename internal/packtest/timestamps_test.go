package packtest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jlgore/corkscrew/internal/app/findings"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
)

// A resource's own created and modified times are stored as created_at and modified_at, and NULL
// when the provider didn't report them; never the scan time.
func TestResourceTimesPersist(t *testing.T) {
	target := cloudflareTarget(t)
	session, err := dataaccess.OpenSession(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	day := func(s string) time.Time {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	tests := []struct {
		id                string
		created, modified *time.Time
	}{
		{legacyWebhook, ptr(day("2023-01-01T10:00:00Z")), ptr(day("2023-01-01T10:00:00Z"))},
		{stripeKey, ptr(day("2024-04-01T10:00:00Z")), ptr(day("2025-08-01T10:00:00Z"))},
		{sessionsKV, nil, nil}, // the KV listing reports no times
	}
	for _, tt := range tests {
		var created, modified sql.NullTime
		err := session.QueryRowContext(context.Background(),
			`SELECT created_at, modified_at FROM cloudflare_resources WHERE id = ?`, tt.id,
		).Scan(&created, &modified)
		if err != nil {
			t.Fatalf("%s: %v", tt.id, err)
		}
		checkTime(t, tt.id+" created_at", created, tt.created)
		checkTime(t, tt.id+" modified_at", modified, tt.modified)
	}
}

// Finding evaluation hydrates a snapshot from the scan's observations. Resource times must
// survive that too, or a pack reading modified_at sees nothing old.
func TestResourceTimesSurviveSnapshotHydration(t *testing.T) {
	target := cloudflareTarget(t)
	t.Setenv("HOME", t.TempDir())
	evaluation, err := findings.Evaluate(context.Background(), findings.EvaluateRequest{
		Target: target, ScanID: ScanID, PackName: "test/timestamps",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only LEGACY_WEBHOOK_SECRET was last modified before 2024.
	if evaluation.Opened != 1 {
		t.Fatalf("opened %d findings, want 1 (LEGACY_WEBHOOK_SECRET)", evaluation.Opened)
	}
}

func ptr(t time.Time) *time.Time { return &t }

func checkTime(t *testing.T, what string, got sql.NullTime, want *time.Time) {
	t.Helper()
	switch {
	case want == nil && got.Valid:
		t.Errorf("%s = %s, want NULL", what, got.Time)
	case want != nil && !got.Valid:
		t.Errorf("%s is NULL, want %s", what, want)
	case want != nil && !got.Time.Equal(*want):
		t.Errorf("%s = %s, want %s", what, got.Time, want)
	}
}

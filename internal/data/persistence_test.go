package data

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// fakeScanWriter records the outcomes handed to the write seam so persistence
// behavior can be exercised without opening a database.
type fakeScanWriter struct {
	outcomes []ScanOutcome
	options  []PersistScanOptions
	err      error
}

func (f *fakeScanWriter) StoreScanOutcome(_ context.Context, outcome ScanOutcome, options PersistScanOptions) error {
	f.outcomes = append(f.outcomes, outcome)
	f.options = append(f.options, options)
	return f.err
}

func TestPersistScanOutcomeDelegatesToScanWriter(t *testing.T) {
	fake := &fakeScanWriter{}
	session := &Session{scanWriter: fake}

	if err := session.PersistScanOutcome(context.Background(), fixtureScanOutcome(), PersistScanOptions{ProviderTableOverride: "custom_table"}); err != nil {
		t.Fatalf("persist scan outcome: %v", err)
	}

	if len(fake.outcomes) != 1 {
		t.Fatalf("scan writer received %d outcomes, want 1", len(fake.outcomes))
	}
	if fake.outcomes[0].ID != "fixture-scan" {
		t.Fatalf("scan writer got outcome %q, want fixture-scan", fake.outcomes[0].ID)
	}
	if fake.options[0].ProviderTableOverride != "custom_table" {
		t.Fatalf("scan writer got override %q, want custom_table", fake.options[0].ProviderTableOverride)
	}
}

func TestPersistScanOutcomePropagatesWriterError(t *testing.T) {
	fake := &fakeScanWriter{err: errors.New("storage unavailable")}
	session := &Session{scanWriter: fake}

	if err := session.PersistScanOutcome(context.Background(), fixtureScanOutcome(), PersistScanOptions{}); err == nil {
		t.Fatal("persist scan outcome succeeded; want propagated writer error")
	}
}

func TestOpenSessionUsesInjectedScanWriter(t *testing.T) {
	fake := &fakeScanWriter{}
	session, err := OpenSession(context.Background(), filepath.Join(t.TempDir(), "injected.duckdb"), WithScanWriter(fake))
	if err != nil {
		t.Fatalf("open data session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if err := session.PersistScanOutcome(context.Background(), fixtureScanOutcome(), PersistScanOptions{}); err != nil {
		t.Fatalf("persist scan outcome: %v", err)
	}
	if len(fake.outcomes) != 1 {
		t.Fatalf("injected writer received %d outcomes, want 1", len(fake.outcomes))
	}
	// The default graph store never ran, so nothing was written to storage.
	assertEmptyScanOutcome(t, session)
}

func TestPersistScanOutcomeCommitsResourcesRelationshipsAndMetadata(t *testing.T) {
	session := openPersistenceTestSession(t)
	outcome := fixtureScanOutcome()

	if err := session.PersistScanOutcome(context.Background(), outcome, PersistScanOptions{}); err != nil {
		t.Fatalf("persist scan outcome: %v", err)
	}
	if err := session.PersistScanOutcome(context.Background(), outcome, PersistScanOptions{}); err != nil {
		t.Fatalf("retry scan outcome: %v", err)
	}

	assertSessionCount(t, session, `SELECT COUNT(*) FROM custom_provider_resources WHERE provider = 'fixture-cloud'`, 2)
	assertSessionCount(t, session, `SELECT COUNT(*) FROM cloud_relationships WHERE provider = 'fixture-cloud'`, 1)
	assertSessionCount(t, session, `SELECT COUNT(*) FROM scan_metadata WHERE id = 'fixture-scan' AND status = 'partial'`, 1)

	count, err := session.Inventory().Count(context.Background(), InventoryFilter{Provider: "fixture-cloud"})
	if err != nil || count != 2 {
		t.Fatalf("normalized inventory count = %d, %v; want 2", count, err)
	}
	relationships, err := session.Relationships().List(context.Background(), RelationshipFilter{Provider: "fixture-cloud"}, Page{Limit: 10})
	if err != nil || len(relationships) != 1 {
		t.Fatalf("normalized relationships = %#v, %v; want one", relationships, err)
	}
}

func TestPersistScanOutcomeRollsBackWhenAResourceFails(t *testing.T) {
	session := openPersistenceTestSession(t)
	outcome := fixtureScanOutcome()
	outcome.Resources = append(outcome.Resources, &pb.Resource{Id: "missing-provider", Type: "broken"})

	if err := session.PersistScanOutcome(context.Background(), outcome, PersistScanOptions{}); err == nil {
		t.Fatal("persist scan outcome succeeded; want resource failure")
	}

	assertEmptyScanOutcome(t, session)
}

func TestHermeticRemotePersistenceHasLocalAtomicityContract(t *testing.T) {
	t.Run("commit", func(t *testing.T) {
		session := openHermeticRemotePersistenceSession(t)
		if err := session.PersistScanOutcome(context.Background(), fixtureScanOutcome(), PersistScanOptions{}); err != nil {
			t.Fatal(err)
		}
		assertSessionCount(t, session, `SELECT COUNT(*) FROM custom_provider_resources WHERE provider = 'fixture-cloud'`, 2)
		assertSessionCount(t, session, `SELECT COUNT(*) FROM cloud_relationships WHERE provider = 'fixture-cloud'`, 1)
		assertSessionCount(t, session, `SELECT COUNT(*) FROM scan_metadata WHERE id = 'fixture-scan'`, 1)
	})

	t.Run("rollback", func(t *testing.T) {
		session := openHermeticRemotePersistenceSession(t)
		outcome := fixtureScanOutcome()
		outcome.Resources = append(outcome.Resources, &pb.Resource{Id: "missing-provider", Type: "broken"})
		if err := session.PersistScanOutcome(context.Background(), outcome, PersistScanOptions{}); err == nil {
			t.Fatal("remote persistence succeeded; want rollback")
		}
		assertEmptyScanOutcome(t, session)
	})
}

func openPersistenceTestSession(t *testing.T) *Session {
	t.Helper()
	session, err := OpenSession(context.Background(), filepath.Join(t.TempDir(), "outcome.duckdb"))
	if err != nil {
		t.Fatalf("open data session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func openHermeticRemotePersistenceSession(t *testing.T) *Session {
	t.Helper()
	localTarget := filepath.Join(t.TempDir(), "quack-adapter.duckdb")
	session, err := openSessionWith(context.Background(), "quack:fixture:9494", func(ctx context.Context, _ string, _ ...db.Option) (*sql.DB, error) {
		return db.OpenDuckDB(ctx, localTarget)
	})
	if err != nil {
		t.Fatalf("open hermetic remote session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func fixtureScanOutcome() ScanOutcome {
	started := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	return ScanOutcome{
		ID:           "fixture-scan",
		Provider:     "fixture-cloud",
		Services:     []string{"widgets"},
		Scopes:       []string{"scope-a", "scope-fail"},
		FailedScopes: []string{"scope-fail"},
		Status:       ScanStatusPartial,
		StartedAt:    started,
		EndedAt:      started.Add(2 * time.Second),
		Resources: []*pb.Resource{
			{
				Provider: "fixture-cloud", Id: "fixture://one", Name: "one", Type: "widget", Service: "widgets", Region: "scope-a",
				Relationships: []*pb.Relationship{{TargetId: "fixture://shared", RelationshipType: "uses"}},
			},
			{Provider: "fixture-cloud", Id: "fixture://shared", Name: "shared", Type: "widget", Service: "widgets", Region: "scope-a"},
		},
	}
}

func assertEmptyScanOutcome(t *testing.T, session *Session) {
	t.Helper()
	assertSessionCount(t, session, `SELECT COUNT(*) FROM custom_provider_resources WHERE provider = 'fixture-cloud'`, 0)
	assertSessionCount(t, session, `SELECT COUNT(*) FROM cloud_relationships WHERE provider = 'fixture-cloud'`, 0)
	assertSessionCount(t, session, `SELECT COUNT(*) FROM scan_metadata WHERE id = 'fixture-scan'`, 0)
}

func assertSessionCount(t *testing.T, session *Session, query string, want int) {
	t.Helper()
	var got int
	if err := session.database.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q count = %d, want %d", query, got, want)
	}
}

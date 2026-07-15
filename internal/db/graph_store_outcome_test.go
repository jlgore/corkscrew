package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestStoreScanOutcomeRollsBackAtEveryPostResourceStage(t *testing.T) {
	for _, test := range []struct {
		name     string
		failSQL  string
		wantName string
	}{
		{name: "relationship", failSQL: "INSERT INTO cloud_relationships", wantName: "relationship write failed"},
		{name: "metadata", failSQL: "INSERT INTO scan_metadata", wantName: "metadata write failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, store := openTestGraphStore(t)
			store.decorateScanWriter = func(next scanStatementExecutor) scanStatementExecutor {
				return failingScanWriter{next: next, contains: test.failSQL, err: errors.New(test.wantName)}
			}

			err := store.StoreScanOutcome(context.Background(), atomicFixtureResources(), StoreScanResourcesOptions{}, atomicFixtureMetadata())
			if err == nil || !strings.Contains(err.Error(), test.wantName) {
				t.Fatalf("StoreScanOutcome() error = %v, want %q", err, test.wantName)
			}
			assertGraphStoreCount(t, database, `SELECT COUNT(*) FROM custom_provider_resources WHERE provider = 'fixture-cloud'`, 0)
			assertGraphStoreCount(t, database, `SELECT COUNT(*) FROM cloud_relationships WHERE provider = 'fixture-cloud'`, 0)
			assertGraphStoreCount(t, database, `SELECT COUNT(*) FROM scan_metadata WHERE id = 'fixture-scan'`, 0)
		})
	}
}

func TestStoreScanOutcomeFailedReplacementPreservesExistingRows(t *testing.T) {
	database, store := openTestGraphStore(t)
	seed := atomicFixtureResources()
	seed[0].Name = "original"
	if err := store.StoreScanOutcome(context.Background(), seed, StoreScanResourcesOptions{}, atomicFixtureMetadata()); err != nil {
		t.Fatalf("seed outcome: %v", err)
	}

	store.decorateScanWriter = func(next scanStatementExecutor) scanStatementExecutor {
		return failingScanWriter{next: next, contains: "INSERT INTO scan_metadata", err: errors.New("metadata write failed")}
	}
	replacement := atomicFixtureResources()
	replacement[0].Name = "replacement"
	metadata := atomicFixtureMetadata()
	metadata.ID = "replacement-scan"
	if err := store.StoreScanOutcome(context.Background(), replacement, StoreScanResourcesOptions{}, metadata); err == nil {
		t.Fatal("replacement succeeded; want metadata failure")
	}

	var name string
	if err := database.QueryRow(`SELECT name FROM custom_provider_resources WHERE provider = 'fixture-cloud' AND id = 'fixture://one'`).Scan(&name); err != nil {
		t.Fatalf("read preserved resource: %v", err)
	}
	if name != "original" {
		t.Fatalf("resource name = %q, want original", name)
	}
	assertGraphStoreCount(t, database, `SELECT COUNT(*) FROM scan_metadata WHERE id = 'replacement-scan'`, 0)
}

type failingScanWriter struct {
	next     scanStatementExecutor
	contains string
	err      error
}

func (w failingScanWriter) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if strings.Contains(query, w.contains) {
		return nil, w.err
	}
	return w.next.ExecContext(ctx, query, args...)
}

func atomicFixtureResources() []*pb.Resource {
	return []*pb.Resource{
		{
			Provider: "fixture-cloud", Id: "fixture://one", Name: "one", Type: "widget", Service: "widgets", Region: "scope-a",
			Relationships: []*pb.Relationship{{TargetId: "fixture://shared", RelationshipType: "uses"}},
		},
		{Provider: "fixture-cloud", Id: "fixture://shared", Name: "shared", Type: "widget", Service: "widgets", Region: "scope-a"},
	}
}

func atomicFixtureMetadata() ScanOutcomeMetadata {
	started := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	return ScanOutcomeMetadata{
		ID: "fixture-scan", Provider: "fixture-cloud",
		Services: []string{"widgets"}, Regions: []string{"scope-a", "scope-fail"},
		TotalResources: 2, FailedResources: 1,
		StartedAt: started, EndedAt: started.Add(2 * time.Second), DurationMS: 2000,
		Metadata: map[string]interface{}{"failed_scopes": []string{"scope-fail"}}, Status: "partial",
	}
}

func assertGraphStoreCount(t *testing.T, database *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := database.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q count = %d, want %d", query, got, want)
	}
}

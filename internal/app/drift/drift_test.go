package drift

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dataaccess "github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestRunComparesCompletedSnapshots(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "drift.duckdb")
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	persist := func(id string, at time.Time, resources ...*pb.Resource) {
		t.Helper()
		err := session.PersistScanOutcome(ctx, dataaccess.ScanOutcome{ID: id, Provider: "acme", Services: []string{"widgets"},
			Scopes: []string{"global"}, Status: dataaccess.ScanStatusCompleted, StartedAt: at, EndedAt: at.Add(time.Second), Resources: resources}, dataaccess.PersistScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	persist("one", started,
		&pb.Resource{Provider: "acme", Id: "same", Type: "widget", Name: "old", RawData: `{"b":2,"a":1}`},
		&pb.Resource{Provider: "acme", Id: "removed", Type: "widget"})
	persist("two", started.Add(time.Hour),
		&pb.Resource{Provider: "acme", Id: "same", Type: "widget", Name: "new", RawData: `{"a":1,"b":2}`},
		&pb.Resource{Provider: "acme", Id: "added", Type: "widget"})
	_ = session.Close()

	result, err := Run(ctx, Request{Target: target, ToScan: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Comparable || result.Incomplete {
		t.Fatalf("comparison flags = comparable %t incomplete %t", result.Comparable, result.Incomplete)
	}
	if len(result.Changes) != 3 {
		t.Fatalf("changes = %#v", result.Changes)
	}
}

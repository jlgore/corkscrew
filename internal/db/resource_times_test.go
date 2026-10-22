package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	resourceCreated  = time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC)
	resourceModified = time.Date(2024, 6, 7, 8, 9, 10, 0, time.UTC)
	resourceScanned  = timestamppb.New(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
)

// timedResource is a resource whose provider reported its own times, scanned later.
func timedResource(provider, id, typ string) *pb.Resource {
	return &pb.Resource{
		Provider: provider, Id: id, Name: id, Type: typ, Service: "storage", RawData: `{}`,
		CreatedAt:    timestamppb.New(resourceCreated),
		ModifiedAt:   timestamppb.New(resourceModified),
		DiscoveredAt: resourceScanned,
	}
}

func readTimes(
	t *testing.T, database *sql.DB, query string, args ...any,
) (sql.NullTime, sql.NullTime) {
	t.Helper()
	var created, modified sql.NullTime
	if err := database.QueryRowContext(context.Background(), query, args...).
		Scan(&created, &modified); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return created, modified
}

// Every writer stores a resource's own times as created_at and modified_at, NULL when the
// provider didn't report them; never the scan time.
func TestStoreScanResourcesWritesResourceTimes(t *testing.T) {
	database, store := openTestGraphStore(t)
	untimed := &pb.Resource{Provider: "aws", Id: "aws-untimed", Name: "b", Type: "AWS::S3::Bucket",
		Service: "s3", DiscoveredAt: resourceScanned}
	resources := []*pb.Resource{
		timedResource("aws", "aws-timed", "AWS::S3::Bucket"),
		untimed,
		timedResource("gcp", "gcp-timed", "storage.googleapis.com/Bucket"),
		// No createdTime or changedTime in raw data: the resource's own times are the fallback.
		timedResource("azure", "azure-timed", "Microsoft.Storage/storageAccounts"),
	}
	ctx := context.Background()
	if err := store.StoreScanResources(ctx, resources, StoreScanResourcesOptions{}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		table, id string
		wantTimes bool
	}{
		{"aws_resources", "aws-timed", true},
		{"aws_resources", "aws-untimed", false},
		{"gcp_resources", "gcp-timed", true},
		{"azure_resources", "azure-timed", true},
	}
	for _, tt := range tests {
		query := `SELECT created_at, modified_at FROM ` + tt.table + ` WHERE id = ?`
		created, modified := readTimes(t, database, query, tt.id)
		switch {
		case !tt.wantTimes && (created.Valid || modified.Valid):
			t.Errorf("%s: created %v modified %v, want NULL", tt.id, created, modified)
		case tt.wantTimes && (!created.Time.Equal(resourceCreated) ||
			!modified.Time.Equal(resourceModified)):
			t.Errorf("%s: created %v modified %v, want %s and %s",
				tt.id, created.Time, modified.Time, resourceCreated, resourceModified)
		}
	}
}

// Version 7 adds resource times to an existing database's observations; observations recorded
// before it keep NULL.
func TestEnsureSchemaV7AddsObservationTimes(t *testing.T) {
	database := openLifecycleTestDB(t)
	ctx := context.Background()
	if err := EnsureSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	// Roll back to version 6 and record an observation the way version 6 did. DuckDB can't drop
	// a column from an indexed table, so the indexes go first; this test doesn't need them.
	for _, statement := range []string{
		`DROP INDEX idx_resource_observations_scan`,
		`DROP INDEX idx_resource_observations_identity`,
		`DROP INDEX idx_resource_observations_hash`,
		`ALTER TABLE resource_observations DROP COLUMN created_at`,
		`ALTER TABLE resource_observations DROP COLUMN modified_at`,
		`DELETE FROM corkscrew_schema_migrations WHERE version = 7`,
		`INSERT INTO resource_observations
		   (scan_id, provider, resource_id, type, semantic_hash, observed_at)
		 VALUES ('old-scan', 'aws', 'old', 'AWS::S3::Bucket', 'hash', TIMESTAMP '2026-01-01')`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	if err := EnsureSchema(ctx, database); err != nil {
		t.Fatalf("upgrade to version 7: %v", err)
	}
	if version, err := CurrentSchemaVersion(ctx, database); err != nil || version != 7 {
		t.Fatalf("schema version = %d, %v; want 7", version, err)
	}
	created, modified := readTimes(t, database,
		`SELECT created_at, modified_at FROM resource_observations WHERE resource_id = 'old'`)
	if created.Valid || modified.Valid {
		t.Errorf("a version 6 observation got times %v %v, want NULL", created, modified)
	}
}

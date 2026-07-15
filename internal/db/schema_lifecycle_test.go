package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func openLifecycleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "schema.duckdb"))
	if err != nil {
		t.Fatalf("open DuckDB: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestEnsureSchemaFreshDatabase(t *testing.T) {
	database := openLifecycleTestDB(t)
	ctx := context.Background()

	if err := EnsureSchema(ctx, database); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := EnsureSchema(ctx, database); err != nil {
		t.Fatalf("ensure schema a second time: %v", err)
	}
	version, err := CurrentSchemaVersion(ctx, database)
	if err != nil {
		t.Fatalf("current schema version: %v", err)
	}
	if version != LatestSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, LatestSchemaVersion)
	}

	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM corkscrew_schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != LatestSchemaVersion {
		t.Fatalf("migration count = %d, want %d", migrationCount, LatestSchemaVersion)
	}

	for _, provider := range providercatalog.Names() {
		for _, object := range []struct {
			name string
			kind string
		}{
			{name: provider + "_resources", kind: "BASE TABLE"},
			{name: provider + "_relationships", kind: "VIEW"},
		} {
			var kind string
			if err := database.QueryRow(`SELECT table_type FROM information_schema.tables WHERE table_name = ?`, object.name).Scan(&kind); err != nil {
				t.Fatalf("inspect %s: %v", object.name, err)
			}
			if kind != object.kind {
				t.Fatalf("%s type = %q, want %q", object.name, kind, object.kind)
			}
		}

		var graphColumnCount int
		if err := database.QueryRow(`
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = ?
			  AND column_name IN ('id', 'type', 'name', 'region', 'account_id', 'arn', 'tags')
		`, provider+"_resources").Scan(&graphColumnCount); err != nil {
			t.Fatalf("inspect graph columns for %s: %v", provider, err)
		}
		if graphColumnCount != 7 {
			t.Fatalf("%s graph column count = %d, want 7", provider, graphColumnCount)
		}
	}
}

func TestEnsureSchemaCreatesNormalizedCustomProviderStorage(t *testing.T) {
	database := openLifecycleTestDB(t)
	ctx := context.Background()

	if err := EnsureSchema(ctx, database); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO custom_provider_resources
		(provider, id, type, name, service, region, account_id, arn, tags)
		VALUES ('acme', 'resource-1', 'widget', 'Widget', 'inventory', 'global', 'acct', 'acme:resource-1', '{}')
	`); err != nil {
		t.Fatalf("insert custom resource: %v", err)
	}
	var provider, id string
	if err := database.QueryRowContext(ctx, `
		SELECT provider, id FROM all_cloud_resources
		WHERE provider = 'acme' AND id = 'resource-1'
	`).Scan(&provider, &id); err != nil {
		t.Fatalf("query normalized custom resource: %v", err)
	}
	if provider != "acme" || id != "resource-1" {
		t.Fatalf("normalized custom resource = %q/%q", provider, id)
	}
}

func TestEnsureSchemaMigratesLegacyRelationships(t *testing.T) {
	database := openLifecycleTestDB(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE github_relationships (
			from_id VARCHAR, to_id VARCHAR, relationship_type VARCHAR, properties JSON
		);
		INSERT INTO github_relationships VALUES
			('repo:one', 'team:platform', 'owned_by', '{"source":"legacy"}')
	`); err != nil {
		t.Fatalf("seed legacy relationships: %v", err)
	}

	if err := EnsureSchema(ctx, database); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	var archiveCount, viewCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM github_relationships_legacy_v0`).Scan(&archiveCount); err != nil {
		t.Fatalf("query relationship archive: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM github_relationships WHERE from_id = 'repo:one'`).Scan(&viewCount); err != nil {
		t.Fatalf("query compatibility view: %v", err)
	}
	if archiveCount != 1 || viewCount != 1 {
		t.Fatalf("archive/view counts = %d/%d, want 1/1", archiveCount, viewCount)
	}

	var provider string
	if err := database.QueryRow(`SELECT provider FROM cloud_relationships WHERE from_id = 'repo:one'`).Scan(&provider); err != nil {
		t.Fatalf("query canonical relationship: %v", err)
	}
	if provider != "github" {
		t.Fatalf("canonical provider = %q, want github", provider)
	}
}

func TestEnsureSchemaQuackIntegration(t *testing.T) {
	target := os.Getenv("CORKSCREW_TEST_QUACK_URL")
	if target == "" {
		t.Skip("set CORKSCREW_TEST_QUACK_URL to run against a disposable Quack database")
	}

	config, err := initializeTestUnifiedDatabase(target)
	if err != nil {
		t.Fatalf("initialize Quack database: %v", err)
	}
	t.Cleanup(func() { _ = config.Close() })

	version, err := CurrentSchemaVersion(context.Background(), config.DB)
	if err != nil {
		t.Fatalf("current Quack schema version: %v", err)
	}
	if version != LatestSchemaVersion {
		t.Fatalf("Quack schema version = %d, want %d", version, LatestSchemaVersion)
	}
}

package data

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jlgore/corkscrew/internal/db"
)

func TestOpenSessionEstablishesStorageSchemaForFreshTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	session, err := OpenSession(ctx, filepath.Join(t.TempDir(), "fresh.duckdb"))
	if err != nil {
		t.Fatalf("OpenSession(): %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	version, err := db.CurrentSchemaVersion(ctx, session.database)
	if err != nil {
		t.Fatalf("CurrentSchemaVersion(): %v", err)
	}
	if version != db.LatestSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, db.LatestSchemaVersion)
	}
	count, err := session.Inventory().Count(ctx, InventoryFilter{})
	if err != nil || count != 0 {
		t.Fatalf("fresh normalized inventory count = %d, %v; want 0", count, err)
	}
}

func TestOpenSessionExpandsHomeDirectoryTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	session, err := OpenSession(context.Background(), "~/.corkscrew/test.duckdb")
	if err != nil {
		t.Fatalf("OpenSession(): %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	want := filepath.Join(home, ".corkscrew", "test.duckdb")
	if got := session.Target(); got != want {
		t.Fatalf("session target = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expanded database target: %v", err)
	}
}

func TestResolveLocalTargetRejectsNamedHome(t *testing.T) {
	if _, err := resolveLocalTarget("~someone/corkscrew.duckdb"); err == nil {
		t.Fatal("resolveLocalTarget accepted named-home syntax")
	}
}

func TestOpenSessionAppliesLifecycleThroughHermeticRemoteAdapter(t *testing.T) {
	ctx := context.Background()
	localTarget := filepath.Join(t.TempDir(), "remote-adapter.duckdb")
	var openedTarget string
	session, err := openSessionWith(ctx, "quack:fixture:9494", func(ctx context.Context, target string, _ ...db.Option) (*sql.DB, error) {
		openedTarget = target
		return db.OpenDuckDB(ctx, localTarget)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if openedTarget != "quack:fixture:9494" || !session.IsRemote() {
		t.Fatalf("opened target = %q, session remote = %t", openedTarget, session.IsRemote())
	}
	version, err := db.CurrentSchemaVersion(ctx, session.database)
	if err != nil || version != db.LatestSchemaVersion {
		t.Fatalf("remote adapter schema version = %d, %v", version, err)
	}
}

func TestOpenSessionClosesDatabaseWhenCapabilityInitializationFails(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "failed-session.duckdb")
	var opened *sql.DB
	_, err := openSessionWith(ctx, target, func(ctx context.Context, target string, _ ...db.Option) (*sql.DB, error) {
		var openErr error
		opened, openErr = db.OpenDuckDB(ctx, target)
		return opened, openErr
	}, WithGraphExtension(filepath.Join(t.TempDir(), "missing.duckdb_extension")))
	if err == nil {
		t.Fatal("OpenSession succeeded with missing graph extension")
	}
	if opened == nil {
		t.Fatal("database opener was not called")
	}
	if pingErr := opened.PingContext(ctx); pingErr == nil || !errors.Is(pingErr, sql.ErrConnDone) && pingErr.Error() != "sql: database is closed" {
		t.Fatalf("database remains usable after failed session construction: %v", pingErr)
	}
}

func TestOpenSessionUpgradesExistingStorageBeforeUse(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "older.duckdb")
	database, err := db.OpenDuckDB(ctx, target)
	if err != nil {
		t.Fatalf("open older target: %v", err)
	}
	if err := db.EnsureSchema(ctx, database); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	if _, err := database.Exec(`DROP MACRO safe_json_extract; DELETE FROM corkscrew_schema_migrations WHERE version = 3`); err != nil {
		t.Fatalf("downgrade fixture: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close older target: %v", err)
	}

	session, err := OpenSession(ctx, target)
	if err != nil {
		t.Fatalf("open upgraded session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, err := session.ReadOnly(ctx, `SELECT safe_json_extract('{}', '$.missing', 'upgraded')`)
	if err != nil {
		t.Fatalf("use upgraded query helper: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "upgraded" {
		t.Fatalf("upgraded helper result = %#v", result.Rows)
	}
}

func TestValidateReadOnlyStatement(t *testing.T) {
	for _, statement := range []string{
		"SELECT 1",
		"WITH resources AS (SELECT 1 AS id) SELECT * FROM resources;",
		"EXPLAIN SELECT * FROM all_cloud_resources",
	} {
		if err := ValidateReadOnlyStatement(statement); err != nil {
			t.Errorf("ValidateReadOnlyStatement(%q): %v", statement, err)
		}
	}

	for _, statement := range []string{
		"",
		"CREATE TABLE nope(id INTEGER)",
		"SELECT 1; SELECT 2",
		"/* harmless-looking */ DELETE FROM resources",
		"WITH deleted AS (DELETE FROM resources RETURNING *) SELECT * FROM deleted",
	} {
		if err := ValidateReadOnlyStatement(statement); err == nil {
			t.Errorf("ValidateReadOnlyStatement(%q) succeeded, want rejection", statement)
		}
	}
}

func TestSessionExecutesOnlyReadOnlyQueries(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "inventory.duckdb")
	fixture, err := sql.Open("duckdb", target)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	if _, err := fixture.ExecContext(ctx, "CREATE TABLE resources(id INTEGER); INSERT INTO resources VALUES (1), (2)"); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}

	session, err := OpenSession(ctx, target)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer session.Close()

	result, err := session.ReadOnly(ctx, "SELECT id FROM resources ORDER BY id")
	if err != nil {
		t.Fatalf("ReadOnly: %v", err)
	}
	if len(result.Columns) != 1 || result.Columns[0].Name != "id" || len(result.Rows) != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}

	if _, err := session.ReadOnly(ctx, "DELETE FROM resources"); err == nil {
		t.Fatal("mutation unexpectedly succeeded")
	}
	var count int
	if err := session.QueryRowContext(ctx, "SELECT COUNT(*) FROM resources").Scan(&count); err != nil {
		t.Fatalf("count fixture: %v", err)
	}
	if count != 2 {
		t.Fatalf("resource count = %d, want 2", count)
	}
}

func TestResolveGraphExtension(t *testing.T) {
	extension := filepath.Join(t.TempDir(), "corkscrew_graph.duckdb_extension")
	if err := os.WriteFile(extension, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveGraphExtension(extension)
	if err != nil {
		t.Fatalf("ResolveGraphExtension: %v", err)
	}
	if resolved != extension {
		t.Fatalf("resolved = %q, want %q", resolved, extension)
	}
}

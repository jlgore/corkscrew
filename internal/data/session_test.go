package data

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestSandboxBlocksHostFilesAndLocksConfiguration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	secret := filepath.Join(dir, "credentials")
	if err := os.WriteFile(secret, []byte("aws_secret_access_key=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenSession(ctx, filepath.Join(dir, "sandboxed.duckdb"), WithSandbox())
	if err != nil {
		t.Fatalf("OpenSession(WithSandbox): %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// The database the session already has open still answers queries.
	if _, err := session.ReadOnly(ctx, "SELECT COUNT(*) FROM all_cloud_resources"); err != nil {
		t.Fatalf("query attached database under sandbox: %v", err)
	}
	for _, statement := range []string{
		"SELECT * FROM read_text('" + secret + "')",
		"SELECT * FROM read_csv('" + secret + "')",
		"SELECT * FROM read_text('https://example.com/exfiltrate')",
	} {
		_, err := session.ReadOnly(ctx, statement)
		if err == nil || !strings.Contains(err.Error(), "Permission Error") {
			t.Fatalf("%s: error = %v, want a DuckDB permission error", statement, err)
		}
	}

	// ReadOnly rejects SET lexically, so go under it: the lock must hold even
	// for SQL that reaches DuckDB directly, on any pooled connection.
	connection, err := session.Connection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	for _, statement := range []string{
		"SET enable_external_access = true",
		"RESET enable_external_access",
		"SET lock_configuration = false",
	} {
		_, err := connection.ExecContext(ctx, statement)
		if err == nil || !strings.Contains(err.Error(), "configuration has been locked") {
			t.Fatalf("%s: error = %v, want locked configuration", statement, err)
		}
	}
}

func TestSessionWithoutSandboxReadsHostFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	local := filepath.Join(dir, "local.txt")
	if err := os.WriteFile(local, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := OpenSession(ctx, filepath.Join(dir, "open.duckdb"))
	if err != nil {
		t.Fatalf("OpenSession(): %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, err := session.ReadOnly(ctx, "SELECT content FROM read_text('"+local+"')")
	if err != nil {
		t.Fatalf("read_text without sandbox: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0] != "hello" {
		t.Fatalf("rows = %#v, want [[hello]]", result.Rows)
	}
}

// A checkout can ship a binary at any path; resolution must not look there.
func TestResolveGraphExtensionIgnoresWorkingDirectory(t *testing.T) {
	checkout := t.TempDir()
	for _, relative := range []string{
		"extensions/corkscrew-graph/build/" + graphExtensionFile,
		"extensions/corkscrew-graph/target/release/" + graphExtensionFile,
		"build/" + graphExtensionFile,
		graphExtensionFile,
	} {
		path := filepath.Join(checkout, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("planted"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(graphExtensionEnv, "")
	t.Chdir(checkout)

	if resolved, err := ResolveGraphExtension(""); err == nil {
		t.Fatalf("resolved %q from the working directory, want an error", resolved)
	}

	t.Setenv(graphExtensionEnv, "build/"+graphExtensionFile)
	_, err := ResolveGraphExtension("")
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative %s: error = %v, want absolute-path error", graphExtensionEnv, err)
	}
	t.Setenv(graphExtensionEnv, "")

	installed := filepath.Join(home, ".corkscrew", "extensions", graphExtensionFile)
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("installed"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveGraphExtension("")
	if err != nil {
		t.Fatalf("ResolveGraphExtension with installed extension: %v", err)
	}
	if resolved != installed {
		t.Fatalf("resolved = %q, want %q", resolved, installed)
	}
}

func TestReadOnlyRejectsResultsOverRowLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "rows.duckdb")
	session, err := OpenSession(ctx, target, withMaxRows(3))
	if err != nil {
		t.Fatalf("OpenSession(): %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.ReadOnly(ctx, "SELECT * FROM range(3)")
	if err != nil || len(result.Rows) != 3 {
		t.Fatalf("rows at the limit = %v, %v; want 3 rows", result, err)
	}
	_, err = session.ReadOnly(ctx, "SELECT * FROM range(4)")
	if !errors.Is(err, ErrRowLimitExceeded) {
		t.Fatalf("rows over the limit error = %v, want ErrRowLimitExceeded", err)
	}
}

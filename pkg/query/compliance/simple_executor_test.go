package compliance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jlgore/corkscrew/pkg/query"
)

type fakeComplianceEngine struct {
	lastQuery  string
	lastParams map[string]interface{}
	result     *query.QueryResult
}

func (f *fakeComplianceEngine) Execute(ctx context.Context, sql string) (*query.QueryResult, error) {
	return f.ExecuteWithParams(ctx, sql, nil)
}

func (f *fakeComplianceEngine) ExecuteWithParams(_ context.Context, sql string, params map[string]interface{}) (*query.QueryResult, error) {
	f.lastQuery = sql
	f.lastParams = params
	return f.result, nil
}

func (f *fakeComplianceEngine) ExecuteStreaming(context.Context, string) (<-chan query.StreamingRow, error) {
	return nil, nil
}

func (f *fakeComplianceEngine) ExecuteStreamingWithParams(context.Context, string, map[string]interface{}) (<-chan query.StreamingRow, error) {
	return nil, nil
}

func (f *fakeComplianceEngine) Validate(string) error {
	return nil
}

func (f *fakeComplianceEngine) Close() error {
	return nil
}

// writeSingleQueryPack writes pack test/sandbox with one control, TEST.S01,
// whose SQL is controlSQL, and returns the pack search path.
func writeSingleQueryPack(t *testing.T, controlSQL string) string {
	t.Helper()
	searchPath := t.TempDir()
	packDir := filepath.Join(searchPath, "test", "sandbox")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("create pack dir: %v", err)
	}
	manifest := `apiVersion: v1
kind: QueryPack
metadata:
  name: sandbox
  namespace: test/sandbox
  version: 1.0.0
  description: Sandbox test pack
  provider: aws
spec:
  queries:
    - id: TEST.S01
      title: Sandbox control
      description: Runs the SQL under test
      severity: HIGH
      category: security
      query_file: control.sql
      enabled: true
`
	files := map[string]string{"manifest.yaml": manifest, "control.sql": controlSQL}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(packDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return searchPath
}

// newPackExecutor opens a real sandboxed executor on a fresh database and
// points it at searchPath only.
func newPackExecutor(t *testing.T, searchPath string) *Executor {
	t.Helper()
	executor, err := NewExecutor(filepath.Join(t.TempDir(), "compliance.duckdb"))
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	t.Cleanup(func() { _ = executor.Close() })
	executor.loader = NewPackLoader().WithSearchPaths(searchPath).WithEmbeddedPacks(false)
	return executor
}

// A pack from an external source must not read host files.
func TestExecutorSandboxesPackSQL(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(secret, []byte("aws_secret_access_key=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	controlSQL := "SELECT 'FAIL' AS status, content AS resource_id FROM read_text('" + secret + "')"
	executor := newPackExecutor(t, writeSingleQueryPack(t, controlSQL))

	results, err := executor.Execute(context.Background(), ExecuteOptions{PackName: "test/sandbox"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Error == nil || !strings.Contains(results[0].Error.Error(), "Permission Error") {
		t.Fatalf("control error = %v, want a DuckDB permission error", results[0].Error)
	}
	if results[0].Passed || len(results[0].Rows) != 0 {
		t.Fatalf("sandboxed control leaked rows or passed: %#v", results[0])
	}
}

func TestExecutorCancelsQueryAtTimeout(t *testing.T) {
	// Far more rows than the timeout allows: only cancellation ends it.
	controlSQL := "SELECT 'PASS' AS status, CAST(COUNT(*) AS VARCHAR) AS resource_id " +
		"FROM range(1000000000) AS a, range(1000000000) AS b"
	executor := newPackExecutor(t, writeSingleQueryPack(t, controlSQL))

	started := time.Now()
	results, err := executor.Execute(context.Background(), ExecuteOptions{
		PackName:     "test/sandbox",
		QueryTimeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 || results[0].Error == nil {
		t.Fatalf("results = %#v, want one control that failed on the timeout", results)
	}
	if results[0].Passed {
		t.Fatal("timed-out control reported as passed")
	}
	if !errors.Is(results[0].Error, context.DeadlineExceeded) {
		t.Fatalf("control error = %v, want context.DeadlineExceeded", results[0].Error)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("query ran for %s; the timeout did not cancel it", elapsed)
	}
}

func TestExecutorExecuteRunsPackQueries(t *testing.T) {
	tempDir := t.TempDir()
	packDir := filepath.Join(tempDir, "test", "pack")
	if err := os.MkdirAll(packDir, 0755); err != nil {
		t.Fatalf("failed to create pack dir: %v", err)
	}

	manifest := `apiVersion: v1
kind: QueryPack
metadata:
  name: pack
  namespace: test/pack
  version: 1.0.0
  description: Test pack
  provider: aws
spec:
  parameters:
    - name: trusted_keys
      description: Trusted keys
      type: list
      required: true
  queries:
    - id: TEST.C01
      title: Test Control
      description: A real control query
      severity: HIGH
      category: security
      tags:
        - storage
      query_file: control.sql
      parameters:
        - trusted_keys
      enabled: true
`
	if err := os.WriteFile(filepath.Join(packDir, "manifest.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "control.sql"), []byte("SELECT :trusted_keys AS keys"), 0644); err != nil {
		t.Fatalf("failed to write sql: %v", err)
	}

	engine := &fakeComplianceEngine{
		result: &query.QueryResult{
			Rows: []map[string]interface{}{
				{
					"status":            "FAIL",
					"resource_id":       "bucket-1",
					"bucket_name":       "critical-bucket",
					"severity":          "HIGH",
					"issue_description": "Bucket failed the control",
				},
			},
		},
	}

	executor := &Executor{
		engine:     engine,
		loader:     NewPackLoader().WithSearchPaths(tempDir).WithEmbeddedPacks(false),
		compliance: NewComplianceExecutor(engine),
	}

	results, err := executor.Execute(context.Background(), ExecuteOptions{
		PackName: "test/pack",
		Tags:     []string{"storage"},
		Parameters: map[string]interface{}{
			"trusted_keys": []interface{}{"key-a", "key-b"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Fatalf("expected failed compliance result")
	}
	if results[0].ResourceCount != 1 {
		t.Fatalf("expected 1 resource, got %d", results[0].ResourceCount)
	}
	if got := results[0].FailedResources[0]; got != "critical-bucket (bucket-1)" {
		t.Fatalf("unexpected failed resource: %s", got)
	}
	if engine.lastQuery != "SELECT :trusted_keys AS keys" {
		t.Fatalf("unexpected query: %s", engine.lastQuery)
	}
	if got := engine.lastParams["trusted_keys"]; got != "key-a,key-b" {
		t.Fatalf("expected comma-normalized list parameter, got %#v", got)
	}
}

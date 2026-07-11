package compliance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

	results, err := executor.Execute(ExecuteOptions{
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

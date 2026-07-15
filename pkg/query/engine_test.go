package query

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNewDuckDBQueryEngine(t *testing.T) {
	engine, err := newTestQueryEngine(t)
	if err != nil {
		t.Fatalf("Failed to create query engine: %v", err)
	}
	defer engine.Close()

	if engine.GetDatabasePath() == "" {
		t.Error("Database path should not be empty")
	}
}

func TestQueryEngineOpensCurrentStorageSchema(t *testing.T) {
	engine, err := newTestQueryEngine(t)
	if err != nil {
		t.Fatalf("create query engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	result, err := engine.Execute(context.Background(), "SELECT COUNT(*) AS resources FROM all_cloud_resources")
	if err != nil {
		t.Fatalf("query normalized inventory: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["resources"] != int64(0) {
		t.Fatalf("normalized inventory result = %#v", result.Rows)
	}
	helpers, err := engine.Execute(context.Background(), `SELECT safe_json_extract('{"name":"fixture"}', '$.name', 'missing') AS name`)
	if err != nil {
		t.Fatalf("query storage JSON helper: %v", err)
	}
	if len(helpers.Rows) != 1 || helpers.Rows[0]["name"] != "fixture" {
		t.Fatalf("JSON helper result = %#v", helpers.Rows)
	}
}

func TestQueryEngineValidation(t *testing.T) {
	engine, err := newTestQueryEngine(t)
	if err != nil {
		t.Fatalf("Failed to create query engine: %v", err)
	}
	defer engine.Close()

	// Test valid query
	err = engine.Validate("SELECT 1 as test")
	if err != nil {
		t.Errorf("Valid query should pass validation: %v", err)
	}

	// Test empty query
	err = engine.Validate("")
	if err == nil {
		t.Error("Empty query should fail validation")
	}

	// Test dangerous query
	err = engine.Validate("DROP TABLE test")
	if err == nil {
		t.Error("Dangerous query should fail validation")
	}

	if _, err := engine.Execute(context.Background(), "SELECT 1; SELECT 2"); err == nil {
		t.Error("multiple statements should fail execution")
	}
	if _, err := engine.Execute(context.Background(), "WITH changed AS (DELETE FROM test RETURNING *) SELECT * FROM changed"); err == nil {
		t.Error("mutation hidden in a CTE should fail execution")
	}
}

func TestQueryExecution(t *testing.T) {
	engine, err := newTestQueryEngine(t)
	if err != nil {
		t.Fatalf("Failed to create query engine: %v", err)
	}
	defer engine.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test simple query
	result, err := engine.Execute(ctx, "SELECT 42 as answer, 'hello' as greeting")
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if len(result.Columns) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(result.Columns))
	}

	if len(result.Rows) != 1 {
		t.Errorf("Expected 1 row, got %d", len(result.Rows))
	}

	// DuckDB may return different integer types, check the actual value
	answer := result.Rows[0]["answer"]
	switch v := answer.(type) {
	case int64:
		if v != 42 {
			t.Errorf("Expected answer=42, got %v", v)
		}
	case int32:
		if v != 42 {
			t.Errorf("Expected answer=42, got %v", v)
		}
	case int:
		if v != 42 {
			t.Errorf("Expected answer=42, got %v", v)
		}
	default:
		t.Errorf("Expected answer=42 (int/int32/int64), got %v (%T)", answer, answer)
	}

	if result.Rows[0]["greeting"] != "hello" {
		t.Errorf("Expected greeting='hello', got %v", result.Rows[0]["greeting"])
	}

	if result.Stats.Duration <= 0 {
		t.Error("Query duration should be positive")
	}
}

func TestQueryEngineWithParams(t *testing.T) {
	engine, err := newTestQueryEngine(t)
	if err != nil {
		t.Fatalf("Failed to create query engine: %v", err)
	}
	defer engine.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test parameterized query
	params := map[string]interface{}{
		"value": 100,
		"name":  "test",
	}

	result, err := engine.ExecuteWithParams(ctx, "SELECT :value as num, :name as text", params)
	if err != nil {
		t.Fatalf("Failed to execute parameterized query: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("Expected 1 row, got %d", len(result.Rows))
	}

	// DuckDB may return different integer types, check the actual value
	num := result.Rows[0]["num"]
	switch v := num.(type) {
	case int64:
		if v != 100 {
			t.Errorf("Expected num=100, got %v", v)
		}
	case int32:
		if v != 100 {
			t.Errorf("Expected num=100, got %v", v)
		}
	case int:
		if v != 100 {
			t.Errorf("Expected num=100, got %v", v)
		}
	default:
		t.Errorf("Expected num=100 (int/int32/int64), got %v (%T)", num, num)
	}

	if result.Rows[0]["text"] != "test" {
		t.Errorf("Expected text='test', got %v", result.Rows[0]["text"])
	}
}

func TestConvertNamedParamsPreservesSQLOrderAndRepeats(t *testing.T) {
	engine := &DuckDBQueryEngine{}

	query, args := engine.convertNamedParams(
		"SELECT :second as second, :first as first, :second as second_again",
		map[string]interface{}{
			"first":  "one",
			"second": "two",
		},
	)

	if query != "SELECT ? as second, ? as first, ? as second_again" {
		t.Fatalf("query = %q, want positional placeholders in SQL order", query)
	}
	wantArgs := []interface{}{"two", "one", "two"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestPing(t *testing.T) {
	engine, err := newTestQueryEngine(t)
	if err != nil {
		t.Fatalf("Failed to create query engine: %v", err)
	}
	defer engine.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = engine.Ping(ctx)
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestCloseEngine(t *testing.T) {
	engine, err := newTestQueryEngine(t)
	if err != nil {
		t.Fatalf("Failed to create query engine: %v", err)
	}

	err = engine.Close()
	if err != nil {
		t.Errorf("Failed to close engine: %v", err)
	}

	// Test that operations fail after close
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = engine.Execute(ctx, "SELECT 1")
	if err == nil {
		t.Error("Execute should fail after close")
	}

	err = engine.Ping(ctx)
	if err == nil {
		t.Error("Ping should fail after close")
	}
}

func newTestQueryEngine(t *testing.T) (*DuckDBQueryEngine, error) {
	t.Helper()
	return NewDuckDBQueryEngineForTarget(filepath.Join(t.TempDir(), "query.duckdb"))
}

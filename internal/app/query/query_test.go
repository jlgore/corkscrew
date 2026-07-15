package query

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestRunExecutesReadQuery(t *testing.T) {
	target := filepath.Join(t.TempDir(), "query.duckdb")
	result, err := Run(context.Background(), Request{Target: target, SQL: "SELECT 42 AS answer"})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "answer" {
		t.Fatalf("columns = %#v, want [answer]", result.Columns)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		t.Fatalf("rows = %#v, want one row with one value", result.Rows)
	}
}

func TestRunClassifiesMissingTable(t *testing.T) {
	target := filepath.Join(t.TempDir(), "query.duckdb")
	_, err := Run(context.Background(), Request{Target: target, SQL: "SELECT * FROM zzz_not_a_real_table"})
	if err == nil {
		t.Fatal("expected error for missing table")
	}
	var queryErr *Error
	if !errors.As(err, &queryErr) {
		t.Fatalf("error is %T, want *query.Error", err)
	}
	if queryErr.Kind != ErrorTableNotFound {
		t.Fatalf("kind = %q, want %q", queryErr.Kind, ErrorTableNotFound)
	}
	if queryErr.MissingTable != "zzz_not_a_real_table" {
		t.Fatalf("missing table = %q, want zzz_not_a_real_table", queryErr.MissingTable)
	}
}

func TestSuggestTableNamesFindsCloseMatches(t *testing.T) {
	suggestions := suggestTableNames("all_cloud_resource")
	found := false
	for _, suggestion := range suggestions {
		if suggestion == "all_cloud_resources" {
			found = true
		}
	}
	if !found {
		t.Fatalf("suggestions %v do not include all_cloud_resources", suggestions)
	}
}

func TestExtractErrorPosition(t *testing.T) {
	if got := extractErrorPosition("Parser Error: syntax error at or near position 12"); got != 12 {
		t.Fatalf("position = %d, want 12", got)
	}
	if got := extractErrorPosition("some other error"); got != 0 {
		t.Fatalf("position = %d, want 0", got)
	}
}

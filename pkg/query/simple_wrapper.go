package query

import (
	"context"

	"github.com/jlgore/corkscrew/internal/db"
)

// NewEngine creates a new query engine for the specified target, which may be a
// local DuckDB file path or a remote Quack server URI (e.g. "quack:host:9494").
// An empty target resolves to the default unified database path.
func NewEngine(target string) (QueryEngine, error) {
	return NewDuckDBQueryEngineForTarget(target)
}

// NewEngineWithOptions creates a query engine for the specified target with
// connection options (such as db.WithToken for remote Quack authentication).
func NewEngineWithOptions(target string, opts ...db.Option) (QueryEngine, error) {
	return NewDuckDBQueryEngineForTarget(target, opts...)
}

// Engine is an alias for QueryEngine for backward compatibility
type Engine = QueryEngine

// ExecuteQuery is a simple wrapper that returns rows and columns for backward compatibility
func ExecuteQuery(engine QueryEngine, query string) ([][]interface{}, []string, error) {
	ctx := context.Background()
	result, err := engine.Execute(ctx, query)
	if err != nil {
		return nil, nil, err
	}

	// Extract column names
	var columns []string
	for _, col := range result.Columns {
		columns = append(columns, col.Name)
	}

	// Convert rows from []map[string]interface{} to [][]interface{}
	var rows [][]interface{}
	for _, rowMap := range result.Rows {
		row := make([]interface{}, len(columns))
		for i, col := range columns {
			row[i] = rowMap[col]
		}
		rows = append(rows, row)
	}

	return rows, columns, nil
}

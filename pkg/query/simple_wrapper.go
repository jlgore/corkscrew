package query

import "context"

// NewEngine creates a new query engine with the specified database path
// This is a wrapper for compatibility with existing code
func NewEngine(dbPath string) (QueryEngine, error) {
	return NewDuckDBQueryEngine()
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
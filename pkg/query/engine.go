package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/internal/db"
)

var namedParamPattern = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)

// QueryEngine defines the interface for executing SQL queries against the Corkscrew database
type QueryEngine interface {
	// Execute runs a SQL query and returns structured results
	Execute(ctx context.Context, query string) (*QueryResult, error)

	// ExecuteWithParams runs a SQL query with parameters and returns structured results
	ExecuteWithParams(ctx context.Context, query string, params map[string]interface{}) (*QueryResult, error)

	// ExecuteStreaming runs a SQL query and returns a channel of rows for large result sets
	ExecuteStreaming(ctx context.Context, query string) (<-chan StreamingRow, error)

	// ExecuteStreamingWithParams runs a SQL query with parameters and returns a channel of rows
	ExecuteStreamingWithParams(ctx context.Context, query string, params map[string]interface{}) (<-chan StreamingRow, error)

	// Validate checks if a SQL query is syntactically valid
	Validate(query string) error

	// Close closes the query engine and any underlying connections
	Close() error
}

// ColumnInfo represents metadata about a result column
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// QueryStats contains execution statistics for a query
type QueryStats struct {
	Duration      time.Duration `json:"duration"`
	RowsAffected  int64         `json:"rows_affected"`
	RowsReturned  int           `json:"rows_returned"`
	ExecutionTime time.Time     `json:"execution_time"`
}

// QueryResult contains the complete result of a query execution
type QueryResult struct {
	Columns []ColumnInfo             `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
	Stats   QueryStats               `json:"stats"`
}

// StreamingRow represents a single row in a streaming result set
type StreamingRow struct {
	Data    map[string]interface{} `json:"data"`
	Columns []ColumnInfo           `json:"columns,omitempty"` // Only included in first row
	Error   error                  `json:"error,omitempty"`   // Non-nil if error occurred
	EOF     bool                   `json:"eof,omitempty"`     // True for the final row
	Stats   *QueryStats            `json:"stats,omitempty"`   // Only included in final row
}

// DuckDBQueryEngine implements QueryEngine using DuckDB
type DuckDBQueryEngine struct {
	session *dataaccess.Session
	dbPath  string
	mutex   sync.RWMutex
}

// NewDuckDBQueryEngine creates a new DuckDB-based query engine against the
// default unified database.
func NewDuckDBQueryEngine() (*DuckDBQueryEngine, error) {
	return NewDuckDBQueryEngineForTarget("")
}

// NewDuckDBQueryEngineForTarget creates a query engine against an explicit
// target. The target may be a local DuckDB file path or a remote Quack server
// URI (e.g. "quack:host:9494"). An empty target resolves to the default unified
// database path. Options (such as db.WithToken) apply only to remote targets.
func NewDuckDBQueryEngineForTarget(target string, opts ...db.Option) (*DuckDBQueryEngine, error) {
	session, err := dataaccess.OpenSession(
		context.Background(),
		target,
		dataaccess.WithDatabaseOptions(opts...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open data session: %w", err)
	}

	return &DuckDBQueryEngine{
		session: session,
		dbPath:  session.Target(),
	}, nil
}

// Execute runs a SQL query and returns structured results
func (e *DuckDBQueryEngine) Execute(ctx context.Context, query string) (*QueryResult, error) {
	return e.ExecuteWithParams(ctx, query, nil)
}

// ExecuteWithParams runs a SQL query with parameters and returns structured results
func (e *DuckDBQueryEngine) ExecuteWithParams(ctx context.Context, query string, params map[string]interface{}) (*QueryResult, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.session == nil {
		return nil, fmt.Errorf("query engine is closed")
	}

	startTime := time.Now()

	// Prepare parameters if provided
	var args []interface{}
	if params != nil {
		// Convert named parameters to positional parameters
		query, args = e.convertNamedParams(query, params)
	}

	if err := dataaccess.ValidateReadOnlyStatement(query); err != nil {
		return nil, fmt.Errorf("query validation failed: %w", err)
	}
	result, err := e.session.ReadOnly(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	columnInfo := make([]ColumnInfo, len(result.Columns))
	for index, column := range result.Columns {
		columnInfo[index] = ColumnInfo{Name: column.Name, Type: column.Type}
	}
	results := make([]map[string]interface{}, 0, len(result.Rows))
	for _, values := range result.Rows {
		row := make(map[string]interface{}, len(result.Columns))
		for index, column := range result.Columns {
			row[column.Name] = values[index]
		}
		results = append(results, row)
	}

	duration := time.Since(startTime)

	return &QueryResult{
		Columns: columnInfo,
		Rows:    results,
		Stats: QueryStats{
			Duration:      duration,
			RowsReturned:  len(results),
			ExecutionTime: startTime,
		},
	}, nil
}

// ExecuteStreaming runs a SQL query and returns a channel of rows for large result sets
func (e *DuckDBQueryEngine) ExecuteStreaming(ctx context.Context, query string) (<-chan StreamingRow, error) {
	return e.ExecuteStreamingWithParams(ctx, query, nil)
}

// ExecuteStreamingWithParams runs a SQL query with parameters and returns a channel of rows
func (e *DuckDBQueryEngine) ExecuteStreamingWithParams(ctx context.Context, query string, params map[string]interface{}) (<-chan StreamingRow, error) {
	if err := dataaccess.ValidateReadOnlyStatement(query); err != nil {
		return nil, fmt.Errorf("query validation failed: %w", err)
	}
	resultChan := make(chan StreamingRow, 100)
	go func() {
		defer close(resultChan)
		result, err := e.ExecuteWithParams(ctx, query, params)
		if err != nil {
			resultChan <- StreamingRow{Error: err}
			return
		}
		for index, row := range result.Rows {
			select {
			case <-ctx.Done():
				resultChan <- StreamingRow{Error: ctx.Err()}
				return
			default:
			}
			streamingRow := StreamingRow{Data: row}
			if index == 0 {
				streamingRow.Columns = result.Columns
			}
			resultChan <- streamingRow
		}
		resultChan <- StreamingRow{EOF: true, Stats: &result.Stats}
	}()
	return resultChan, nil
}

// Validate checks if a SQL query is syntactically valid (for public API)
func (e *DuckDBQueryEngine) Validate(query string) error {
	if err := dataaccess.ValidateReadOnlyStatement(query); err != nil {
		return err
	}
	return e.validateSyntax(query)
}

// validateSyntax performs syntax validation using EXPLAIN
func (e *DuckDBQueryEngine) validateSyntax(query string) error {
	// Replace parameters with dummy values for validation
	validationQuery := query
	if strings.Contains(query, "?") {
		validationQuery = e.replacePlaceholdersForValidation(query)
	}

	// Use EXPLAIN to validate syntax without execution
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.session == nil {
		return fmt.Errorf("query engine is closed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := e.session.ReadOnly(ctx, "EXPLAIN "+validationQuery)
	if err != nil {
		return fmt.Errorf("query syntax validation failed: %w", err)
	}

	return nil
}

// replacePlaceholdersForValidation replaces ? placeholders with dummy values for syntax validation
func (e *DuckDBQueryEngine) replacePlaceholdersForValidation(query string) string {
	// Count placeholders and replace with appropriate dummy values
	placeholderCount := strings.Count(query, "?")
	result := query

	for i := 0; i < placeholderCount; i++ {
		// Use different dummy values to test various data types
		var dummy string
		switch i % 4 {
		case 0:
			dummy = "'dummy_string'" // String type
		case 1:
			dummy = "1" // Integer type
		case 2:
			dummy = "true" // Boolean type
		case 3:
			dummy = "2023-01-01" // Date type
		}
		result = strings.Replace(result, "?", dummy, 1)
	}

	return result
}

// convertNamedParams converts named parameters to positional parameters
func (e *DuckDBQueryEngine) convertNamedParams(query string, params map[string]interface{}) (string, []interface{}) {
	var args []interface{}

	converted := namedParamPattern.ReplaceAllStringFunc(query, func(match string) string {
		value, ok := params[match[1:]]
		if !ok {
			return match
		}
		args = append(args, value)
		return "?"
	})

	return converted, args
}

// Close closes the query engine and any underlying connections
func (e *DuckDBQueryEngine) Close() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.session != nil {
		err := e.session.Close()
		e.session = nil
		return err
	}
	return nil
}

// GetDatabasePath returns the path to the DuckDB database file
func (e *DuckDBQueryEngine) GetDatabasePath() string {
	return e.dbPath
}

// Ping tests the database connection
func (e *DuckDBQueryEngine) Ping(ctx context.Context) error {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.session == nil {
		return fmt.Errorf("query engine is closed")
	}

	return e.session.Ping(ctx)
}

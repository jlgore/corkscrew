package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/internal/db"
	_ "github.com/marcboeker/go-duckdb"
)

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
	Columns []ColumnInfo               `json:"columns"`
	Rows    []map[string]interface{}   `json:"rows"`
	Stats   QueryStats                 `json:"stats"`
}

// StreamingRow represents a single row in a streaming result set
type StreamingRow struct {
	Data    map[string]interface{} `json:"data"`
	Columns []ColumnInfo          `json:"columns,omitempty"` // Only included in first row
	Error   error                 `json:"error,omitempty"`   // Non-nil if error occurred
	EOF     bool                  `json:"eof,omitempty"`     // True for the final row
	Stats   *QueryStats           `json:"stats,omitempty"`   // Only included in final row
}

// DuckDBQueryEngine implements QueryEngine using DuckDB
type DuckDBQueryEngine struct {
	db     *sql.DB
	dbPath string
	mutex  sync.RWMutex
}

// NewDuckDBQueryEngine creates a new DuckDB-based query engine
func NewDuckDBQueryEngine() (*DuckDBQueryEngine, error) {
	dbPath, err := db.GetUnifiedDatabasePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get database path: %w", err)
	}

	database, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool settings
	database.SetMaxOpenConns(10)
	database.SetMaxIdleConns(2)
	database.SetConnMaxLifetime(30 * time.Minute)

	// Initialize database with required extensions
	if err := initializeQueryEngine(database); err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to initialize query engine: %w", err)
	}

	// Register JSON helper functions
	ctx := context.Background()
	if err := InitializeJSONHelpers(ctx, database); err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to initialize JSON helpers: %w", err)
	}

	return &DuckDBQueryEngine{
		db:     database,
		dbPath: dbPath,
	}, nil
}

// initializeQueryEngine sets up the database with required extensions and settings
func initializeQueryEngine(db *sql.DB) error {
	// Install and load JSON extension
	if _, err := db.Exec("INSTALL json; LOAD json;"); err != nil {
		return fmt.Errorf("failed to load JSON extension: %w", err)
	}

	// Set autoinstall and autoload for known extensions
	if _, err := db.Exec("SET autoinstall_known_extensions=1;"); err != nil {
		return fmt.Errorf("failed to set autoinstall: %w", err)
	}

	if _, err := db.Exec("SET autoload_known_extensions=1;"); err != nil {
		return fmt.Errorf("failed to set autoload: %w", err)
	}

	return nil
}

// Execute runs a SQL query and returns structured results
func (e *DuckDBQueryEngine) Execute(ctx context.Context, query string) (*QueryResult, error) {
	return e.ExecuteWithParams(ctx, query, nil)
}

// ExecuteWithParams runs a SQL query with parameters and returns structured results
func (e *DuckDBQueryEngine) ExecuteWithParams(ctx context.Context, query string, params map[string]interface{}) (*QueryResult, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.db == nil {
		return nil, fmt.Errorf("query engine is closed")
	}

	startTime := time.Now()

	// Prepare parameters if provided
	var args []interface{}
	if params != nil {
		// Convert named parameters to positional parameters
		query, args = e.convertNamedParams(query, params)
	}

	// Validate query after parameter conversion
	if err := e.validateSyntax(query); err != nil {
		return nil, fmt.Errorf("query validation failed: %w", err)
	}

	// Execute the query
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column info: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// Build column metadata
	columnInfo := make([]ColumnInfo, len(columns))
	for i, col := range columns {
		columnInfo[i] = ColumnInfo{
			Name:     col,
			Type:     columnTypes[i].DatabaseTypeName(),
			Nullable: func() bool {
				nullable, ok := columnTypes[i].Nullable()
				return ok && nullable
			}(),
		}
	}

	// Scan all rows
	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			// Convert byte slices to strings for JSON compatibility
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error while iterating rows: %w", err)
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
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.db == nil {
		return nil, fmt.Errorf("query engine is closed")
	}

	// Prepare parameters if provided
	var args []interface{}
	if params != nil {
		// Convert named parameters to positional parameters
		query, args = e.convertNamedParams(query, params)
	}

	// Validate query after parameter conversion
	if err := e.validateSyntax(query); err != nil {
		return nil, fmt.Errorf("query validation failed: %w", err)
	}

	// Create result channel
	resultChan := make(chan StreamingRow, 100) // Buffer for performance

	// Start streaming in a goroutine
	go func() {
		defer close(resultChan)
		
		startTime := time.Now()
		rowCount := 0

		// Execute the query
		rows, err := e.db.QueryContext(ctx, query, args...)
		if err != nil {
			resultChan <- StreamingRow{Error: fmt.Errorf("query execution failed: %w", err)}
			return
		}
		defer rows.Close()

		// Get column information
		columns, err := rows.Columns()
		if err != nil {
			resultChan <- StreamingRow{Error: fmt.Errorf("failed to get column info: %w", err)}
			return
		}

		columnTypes, err := rows.ColumnTypes()
		if err != nil {
			resultChan <- StreamingRow{Error: fmt.Errorf("failed to get column types: %w", err)}
			return
		}

		// Build column metadata
		columnInfo := make([]ColumnInfo, len(columns))
		for i, col := range columns {
			columnInfo[i] = ColumnInfo{
				Name:     col,
				Type:     columnTypes[i].DatabaseTypeName(),
				Nullable: func() bool {
					nullable, ok := columnTypes[i].Nullable()
					return ok && nullable
				}(),
			}
		}

		// Stream rows one by one
		firstRow := true
		for rows.Next() {
			select {
			case <-ctx.Done():
				resultChan <- StreamingRow{Error: ctx.Err()}
				return
			default:
			}

			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				resultChan <- StreamingRow{Error: fmt.Errorf("failed to scan row: %w", err)}
				return
			}

			row := make(map[string]interface{})
			for i, col := range columns {
				val := values[i]
				// Convert byte slices to strings for JSON compatibility
				if b, ok := val.([]byte); ok {
					row[col] = string(b)
				} else {
					row[col] = val
				}
			}

			streamingRow := StreamingRow{Data: row}
			
			// Include column info in first row
			if firstRow {
				streamingRow.Columns = columnInfo
				firstRow = false
			}

			resultChan <- streamingRow
			rowCount++
		}

		if err := rows.Err(); err != nil {
			resultChan <- StreamingRow{Error: fmt.Errorf("error while iterating rows: %w", err)}
			return
		}

		// Send final row with statistics
		duration := time.Since(startTime)
		stats := QueryStats{
			Duration:      duration,
			RowsReturned:  rowCount,
			ExecutionTime: startTime,
		}

		resultChan <- StreamingRow{
			EOF:   true,
			Stats: &stats,
		}
	}()

	return resultChan, nil
}

// Validate checks if a SQL query is syntactically valid (for public API)
func (e *DuckDBQueryEngine) Validate(query string) error {
	// Basic validation - check for empty query
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("query cannot be empty")
	}

	// Check for dangerous operations
	upperQuery := strings.ToUpper(query)
	dangerous := []string{"DROP", "DELETE", "UPDATE", "INSERT", "ALTER", "CREATE", "TRUNCATE"}
	
	for _, op := range dangerous {
		if strings.Contains(upperQuery, op) {
			return fmt.Errorf("query contains potentially dangerous operation: %s", op)
		}
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

	if e.db == nil {
		return fmt.Errorf("query engine is closed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := e.db.QueryContext(ctx, "EXPLAIN "+validationQuery)
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
			dummy = "'dummy_string'"  // String type
		case 1:
			dummy = "1"              // Integer type
		case 2:
			dummy = "true"           // Boolean type
		case 3:
			dummy = "2023-01-01"     // Date type
		}
		result = strings.Replace(result, "?", dummy, 1)
	}
	
	return result
}

// convertNamedParams converts named parameters to positional parameters
func (e *DuckDBQueryEngine) convertNamedParams(query string, params map[string]interface{}) (string, []interface{}) {
	var args []interface{}
	paramIndex := 1

	// Simple implementation - replace :paramName with ? for DuckDB
	for name, value := range params {
		placeholder := ":" + name
		if strings.Contains(query, placeholder) {
			query = strings.ReplaceAll(query, placeholder, "?")
			args = append(args, value)
			paramIndex++
		}
	}

	return query, args
}

// Close closes the query engine and any underlying connections
func (e *DuckDBQueryEngine) Close() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.db != nil {
		err := e.db.Close()
		e.db = nil
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

	if e.db == nil {
		return fmt.Errorf("query engine is closed")
	}

	return e.db.PingContext(ctx)
}
// Package query owns the read-query and compliance execution workflows. CLI and
// other adapters supply a resolved target and SQL, and render the returned
// transport-neutral results and classified errors.
package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jlgore/corkscrew/internal/db"
	pkgquery "github.com/jlgore/corkscrew/pkg/query"
	"github.com/jlgore/corkscrew/pkg/query/compliance"
)

// Request executes one read query against a resolved database target.
type Request struct {
	Target  string
	Options []db.Option
	SQL     string
}

// Result is the transport-neutral output of a successful query.
type Result struct {
	Columns []string
	Rows    [][]interface{}
}

// Run executes the query and returns rows and columns. On failure it returns a
// *Error classifying the failure and carrying repair hints.
func Run(_ context.Context, request Request) (Result, error) {
	engine, err := pkgquery.NewEngineWithOptions(request.Target, request.Options...)
	if err != nil {
		return Result{}, fmt.Errorf("create query engine: %w", err)
	}
	defer engine.Close()

	rows, columns, err := pkgquery.ExecuteQuery(engine, request.SQL)
	if err != nil {
		return Result{}, classifyQueryError(engine, request.SQL, err)
	}
	return Result{Columns: columns, Rows: rows}, nil
}

// ErrorKind classifies why a query failed.
type ErrorKind string

const (
	ErrorTableNotFound ErrorKind = "table_not_found"
	ErrorSyntax        ErrorKind = "syntax"
	ErrorOther         ErrorKind = "other"
)

// Error is a classified query failure. Adapters render its fields as hints.
type Error struct {
	SQL             string
	Kind            ErrorKind
	Cause           error
	MissingTable    string
	Suggestions     []string
	AvailableTables []string
	Position        int // 1-based character position for syntax errors, 0 if unknown
}

func (e *Error) Error() string { return e.Cause.Error() }
func (e *Error) Unwrap() error { return e.Cause }

var (
	tableNotFoundPattern = regexp.MustCompile(`Table with name ([a-zA-Z_][a-zA-Z0-9_]*) does not exist`)
	errorPositionPattern = regexp.MustCompile(`at or near position (\d+)`)
)

func classifyQueryError(engine pkgquery.QueryEngine, sql string, cause error) *Error {
	message := cause.Error()
	result := &Error{SQL: sql, Cause: cause, Kind: ErrorOther}

	switch {
	case strings.Contains(message, "no such table") || strings.Contains(message, "does not exist"):
		result.Kind = ErrorTableNotFound
		result.AvailableTables = availableTables(engine)
		if matches := tableNotFoundPattern.FindStringSubmatch(message); len(matches) > 1 {
			result.MissingTable = matches[1]
			result.Suggestions = suggestTableNames(matches[1])
		}
	case strings.Contains(message, "syntax error"):
		result.Kind = ErrorSyntax
		result.Position = extractErrorPosition(message)
	}
	return result
}

func availableTables(engine pkgquery.QueryEngine) []string {
	rows, _, err := pkgquery.ExecuteQuery(engine, "SELECT DISTINCT table_name FROM information_schema.tables WHERE table_schema = 'main'")
	if err != nil {
		return nil
	}
	var tables []string
	for _, row := range rows {
		if name, ok := row[0].(string); ok {
			tables = append(tables, name)
		}
	}
	return tables
}

// commonTables are the core corkscrew tables used for typo suggestions.
var commonTables = []string{
	"all_cloud_resources",
	"cloud_relationships",
	"resource_counts_by_provider",
	"scan_metadata",
}

func suggestTableNames(tableName string) []string {
	lowerInput := strings.ToLower(tableName)
	var suggestions []string
	for _, table := range commonTables {
		lowerTable := strings.ToLower(table)
		if strings.Contains(lowerTable, lowerInput) || strings.Contains(lowerInput, lowerTable) {
			suggestions = append(suggestions, table)
			continue
		}
		if levenshteinDistance(lowerInput, lowerTable) <= 3 {
			suggestions = append(suggestions, table)
		}
	}
	return suggestions
}

func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}
	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}
			matrix[i][j] = min(matrix[i-1][j]+1, matrix[i][j-1]+1, matrix[i-1][j-1]+cost)
		}
	}
	return matrix[len(s1)][len(s2)]
}

func extractErrorPosition(message string) int {
	matches := errorPositionPattern.FindStringSubmatch(message)
	if len(matches) < 2 {
		return 0
	}
	var position int
	if _, err := fmt.Sscanf(matches[1], "%d", &position); err != nil {
		return 0
	}
	return position
}

// ComplianceRequest runs compliance controls or packs against a target.
type ComplianceRequest struct {
	Target     string
	ControlID  string
	PackName   string
	Tags       []string
	Parameters map[string]interface{}
	DryRun     bool
}

// ComplianceResult is one control's transport-neutral outcome.
type ComplianceResult struct {
	ControlID       string
	Title           string
	Description     string
	Passed          bool
	ResourceCount   int
	FailedResources []string
	Error           error
}

// RunCompliance executes the requested compliance controls and returns their
// results.
func RunCompliance(_ context.Context, request ComplianceRequest) ([]ComplianceResult, error) {
	executor, err := compliance.NewExecutor(request.Target)
	if err != nil {
		return nil, fmt.Errorf("create compliance executor: %w", err)
	}
	defer executor.Close()

	results, err := executor.Execute(compliance.ExecuteOptions{
		ControlID:  request.ControlID,
		PackName:   request.PackName,
		Tags:       request.Tags,
		Parameters: request.Parameters,
		DryRun:     request.DryRun,
	})
	if err != nil {
		return nil, err
	}

	converted := make([]ComplianceResult, len(results))
	for index, result := range results {
		converted[index] = ComplianceResult{
			ControlID:       result.ControlID,
			Title:           result.Title,
			Description:     result.Description,
			Passed:          result.Passed,
			ResourceCount:   result.ResourceCount,
			FailedResources: result.FailedResources,
			Error:           result.Error,
		}
	}
	return converted, nil
}

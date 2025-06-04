package compliance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/pkg/query"
)

// ComplianceExecutor executes compliance queries with standardized results
type ComplianceExecutor struct {
	engine   query.QueryEngine
	registry *PackRegistry
}

// ComplianceResult represents a standardized compliance check result
type ComplianceResult struct {
	ResourceID   string `json:"resource_id"`
	ResourceName string `json:"resource_name"`
	ResourceType string `json:"resource_type"`
	Region       string `json:"region,omitempty"`
	ControlID    string `json:"control_id"`
	ControlName  string `json:"control_name"`
	Status       string `json:"status"` // PASS, FAIL, WARNING, ERROR
	Severity     string `json:"severity"` // CRITICAL, HIGH, MEDIUM, LOW, INFO
	Details      string `json:"details"`
	Remediation  *string `json:"remediation,omitempty"` // NULL if passing
	Timestamp    time.Time `json:"timestamp"`
}

// ProgressEvent represents progress during pack execution
type ProgressEvent struct {
	Type     string             `json:"type"` // "start", "query", "complete", "error"
	PackName string             `json:"pack_name"`
	QueryID  string             `json:"query_id,omitempty"`
	Current  int                `json:"current"`
	Total    int                `json:"total"`
	Message  string             `json:"message,omitempty"`
	Error    error              `json:"error,omitempty"`
	Result   []ComplianceResult `json:"result,omitempty"` // For "query" events
}

// QueryError represents an error during query execution
type QueryError struct {
	QueryID    string                 `json:"query_id"`
	ErrorType  string                 `json:"error_type"` // "syntax", "parameter", "execution", "timeout"
	Message    string                 `json:"message"`
	SQL        string                 `json:"sql,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// PackExecutionResult represents the complete result of pack execution
type PackExecutionResult struct {
	PackName          string             `json:"pack_name"`
	ExecutionID       string             `json:"execution_id"`
	StartTime         time.Time          `json:"start_time"`
	EndTime           time.Time          `json:"end_time"`
	TotalQueries      int                `json:"total_queries"`
	SuccessfulQueries int                `json:"successful_queries"`
	FailedQueries     []QueryError       `json:"failed_queries"`
	Results           []ComplianceResult `json:"results"`
	PartialSuccess    bool               `json:"partial_success"`
}

// QueryValidation represents dry-run validation results
type QueryValidation struct {
	QueryID    string                 `json:"query_id"`
	SQL        string                 `json:"sql"` // After parameter substitution
	IsValid    bool                   `json:"is_valid"`
	Error      error                  `json:"error,omitempty"`
	Plan       string                 `json:"plan,omitempty"` // EXPLAIN output
	Parameters map[string]interface{} `json:"parameters"`
}

// DryRunResult represents the result of dry-run validation
type DryRunResult struct {
	Valid   bool              `json:"valid"`
	Queries []QueryValidation `json:"queries"`
}

// ExecutionOptions configures query execution behavior
type ExecutionOptions struct {
	DryRun           bool                   `json:"dry_run"`
	Timeout          time.Duration          `json:"timeout"`
	Parameters       map[string]interface{} `json:"parameters"`
	ContinueOnError  bool                   `json:"continue_on_error"`
	MaxConcurrency   int                    `json:"max_concurrency"`
}

// NewComplianceExecutor creates a new compliance query executor
func NewComplianceExecutor(engine query.QueryEngine) *ComplianceExecutor {
	return &ComplianceExecutor{
		engine:   engine,
		registry: NewPackRegistry(),
	}
}

// ExecuteQuery executes a single compliance query with parameter substitution
func (e *ComplianceExecutor) ExecuteQuery(ctx context.Context, query *ComplianceQuery, parameters map[string]interface{}) ([]ComplianceResult, error) {
	// Validate required columns
	if err := e.validateQueryColumns(query.SQL); err != nil {
		return nil, fmt.Errorf("query validation failed: %w", err)
	}

	// Substitute parameters
	substitutedSQL, sqlParams, err := e.substituteParameters(query.SQL, query.Parameters, parameters)
	if err != nil {
		return nil, fmt.Errorf("parameter substitution failed: %w", err)
	}

	// Execute query
	result, err := e.engine.ExecuteWithParams(ctx, substitutedSQL, sqlParams)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// Convert to compliance results
	complianceResults, err := e.convertToComplianceResults(result)
	if err != nil {
		return nil, fmt.Errorf("result conversion failed: %w", err)
	}

	return complianceResults, nil
}

// ExecutePack executes all queries in a pack with progress reporting
func (e *ComplianceExecutor) ExecutePack(ctx context.Context, pack *QueryPack, options ExecutionOptions) (<-chan ProgressEvent, error) {
	// Validate pack parameters
	if err := e.validatePackParameters(pack, options.Parameters); err != nil {
		return nil, fmt.Errorf("pack parameter validation failed: %w", err)
	}

	// Handle dry-run mode
	if options.DryRun {
		return e.dryRunPack(ctx, pack, options)
	}

	progress := make(chan ProgressEvent, 100)

	go func() {
		defer close(progress)

		startTime := time.Now()
		executionID := fmt.Sprintf("%s-%d", pack.Metadata.Name, startTime.Unix())

		// Send start event
		progress <- ProgressEvent{
			Type:     "start",
			PackName: pack.Metadata.Name,
			Total:    len(pack.Queries),
			Message:  fmt.Sprintf("Starting execution of pack %s", pack.Metadata.Name),
		}

		var allResults []ComplianceResult
		var failedQueries []QueryError
		successCount := 0

		// Execute each query
		for i, query := range pack.Queries {
			if !query.Enabled {
				continue
			}

			// Create query context with timeout
			queryCtx := ctx
			if options.Timeout > 0 {
				var cancel context.CancelFunc
				queryCtx, cancel = context.WithTimeout(ctx, options.Timeout)
				defer cancel()
			}

			// Execute query
			results, err := e.ExecuteQuery(queryCtx, &query, options.Parameters)
			if err != nil {
				queryError := QueryError{
					QueryID:    query.ID,
					ErrorType:  e.categorizeError(err),
					Message:    err.Error(),
					Parameters: options.Parameters,
				}

				failedQueries = append(failedQueries, queryError)

				// Send error event
				progress <- ProgressEvent{
					Type:     "error",
					PackName: pack.Metadata.Name,
					QueryID:  query.ID,
					Current:  i + 1,
					Total:    len(pack.Queries),
					Message:  fmt.Sprintf("Query %s failed: %s", query.ID, err.Error()),
					Error:    err,
				}

				// Continue or fail based on options
				if !options.ContinueOnError {
					break
				}
				continue
			}

			// Success
			successCount++
			allResults = append(allResults, results...)

			// Send progress event
			progress <- ProgressEvent{
				Type:     "query",
				PackName: pack.Metadata.Name,
				QueryID:  query.ID,
				Current:  i + 1,
				Total:    len(pack.Queries),
				Message:  fmt.Sprintf("Completed query %s (%d results)", query.ID, len(results)),
				Result:   results,
			}
		}

		// Send completion event
		endTime := time.Now()
		_ = PackExecutionResult{
			PackName:          pack.Metadata.Name,
			ExecutionID:       executionID,
			StartTime:         startTime,
			EndTime:           endTime,
			TotalQueries:      len(pack.Queries),
			SuccessfulQueries: successCount,
			FailedQueries:     failedQueries,
			Results:           allResults,
			PartialSuccess:    len(failedQueries) > 0 && successCount > 0,
		}

		progress <- ProgressEvent{
			Type:     "complete",
			PackName: pack.Metadata.Name,
			Current:  len(pack.Queries),
			Total:    len(pack.Queries),
			Message:  fmt.Sprintf("Pack execution completed: %d/%d queries successful", successCount, len(pack.Queries)),
		}
	}()

	return progress, nil
}

// DryRun validates a pack without executing queries
func (e *ComplianceExecutor) DryRun(ctx context.Context, pack *QueryPack, parameters map[string]interface{}) (*DryRunResult, error) {
	// Validate pack parameters
	if err := e.validatePackParameters(pack, parameters); err != nil {
		return &DryRunResult{Valid: false}, err
	}

	var validations []QueryValidation
	allValid := true

	for _, query := range pack.Queries {
		validation := QueryValidation{
			QueryID:    query.ID,
			Parameters: parameters,
		}

		// Validate required columns
		if err := e.validateQueryColumns(query.SQL); err != nil {
			validation.IsValid = false
			validation.Error = err
			allValid = false
			validations = append(validations, validation)
			continue
		}

		// Substitute parameters
		substitutedSQL, sqlParams, err := e.substituteParameters(query.SQL, query.Parameters, parameters)
		if err != nil {
			validation.IsValid = false
			validation.Error = fmt.Errorf("parameter substitution failed: %w", err)
			allValid = false
			validations = append(validations, validation)
			continue
		}

		validation.SQL = substitutedSQL

		// Validate syntax using EXPLAIN
		if err := e.engine.Validate(substitutedSQL); err != nil {
			validation.IsValid = false
			validation.Error = fmt.Errorf("SQL validation failed: %w", err)
			allValid = false
		} else {
			validation.IsValid = true
			// Try to get execution plan
			planResult, err := e.engine.ExecuteWithParams(ctx, "EXPLAIN "+substitutedSQL, sqlParams)
			if err == nil && len(planResult.Rows) > 0 {
				if plan, ok := planResult.Rows[0]["explain_value"].(string); ok {
					validation.Plan = plan
				}
			}
		}

		validations = append(validations, validation)
	}

	return &DryRunResult{
		Valid:   allValid,
		Queries: validations,
	}, nil
}

// dryRunPack executes dry-run mode via progress channel
func (e *ComplianceExecutor) dryRunPack(ctx context.Context, pack *QueryPack, options ExecutionOptions) (<-chan ProgressEvent, error) {
	progress := make(chan ProgressEvent, 100)

	go func() {
		defer close(progress)

		progress <- ProgressEvent{
			Type:     "start",
			PackName: pack.Metadata.Name,
			Total:    len(pack.Queries),
			Message:  "Starting dry-run validation",
		}

		dryRunResult, err := e.DryRun(ctx, pack, options.Parameters)
		if err != nil {
			progress <- ProgressEvent{
				Type:     "error",
				PackName: pack.Metadata.Name,
				Message:  "Dry-run validation failed",
				Error:    err,
			}
			return
		}

		// Report each query validation
		for i, validation := range dryRunResult.Queries {
			message := fmt.Sprintf("Query %s validation passed", validation.QueryID)
			
			if !validation.IsValid {
				message = fmt.Sprintf("Query %s validation failed: %s", validation.QueryID, validation.Error.Error())
			}

			progress <- ProgressEvent{
				Type:     "query",
				PackName: pack.Metadata.Name,
				QueryID:  validation.QueryID,
				Current:  i + 1,
				Total:    len(pack.Queries),
				Message:  message,
				Error:    validation.Error,
			}
		}

		// Send completion
		message := "Dry-run validation completed successfully"
		if !dryRunResult.Valid {
			message = "Dry-run validation completed with errors"
		}

		progress <- ProgressEvent{
			Type:     "complete",
			PackName: pack.Metadata.Name,
			Current:  len(pack.Queries),
			Total:    len(pack.Queries),
			Message:  message,
		}
	}()

	return progress, nil
}

// validateQueryColumns validates that a query returns required compliance columns
func (e *ComplianceExecutor) validateQueryColumns(sqlQuery string) error {
	requiredColumns := []string{
		"resource_id", "resource_name", "resource_type", "control_id", 
		"control_name", "status", "severity", "details",
	}

	// Simple validation - check if query contains the required column names
	// This is a basic check; real validation would parse the SQL AST
	upperSQL := strings.ToUpper(sqlQuery)
	
	for _, column := range requiredColumns {
		if !strings.Contains(upperSQL, strings.ToUpper(column)) {
			return fmt.Errorf("query must return required column: %s", column)
		}
	}

	// Check for valid status values in query
	if !strings.Contains(upperSQL, "'PASS'") && !strings.Contains(upperSQL, "'FAIL'") &&
		!strings.Contains(upperSQL, "'WARNING'") && !strings.Contains(upperSQL, "'ERROR'") {
		return fmt.Errorf("query must return valid status values: PASS, FAIL, WARNING, or ERROR")
	}

	return nil
}

// substituteParameters performs parameter substitution with support for lists
func (e *ComplianceExecutor) substituteParameters(sqlQuery string, queryParams []string, providedParams map[string]interface{}) (string, map[string]interface{}, error) {
	result := sqlQuery
	sqlParams := make(map[string]interface{})

	// Handle each required parameter
	for _, paramName := range queryParams {
		placeholder := ":" + paramName
		
		if !strings.Contains(result, placeholder) {
			continue // Parameter not used in this query
		}

		value, exists := providedParams[paramName]
		if !exists {
			return "", nil, fmt.Errorf("required parameter '%s' not provided", paramName)
		}

		// Handle list parameters (IN clauses)
		if strings.Contains(result, "("+placeholder+")") {
			if listValue, ok := value.([]interface{}); ok {
				placeholders := make([]string, len(listValue))
				for i, item := range listValue {
					paramKey := fmt.Sprintf("%s_%d", paramName, i)
					placeholders[i] = ":" + paramKey
					sqlParams[paramKey] = item
				}
				listPlaceholder := "(" + placeholder + ")"
				listReplacement := "(" + strings.Join(placeholders, ", ") + ")"
				result = strings.ReplaceAll(result, listPlaceholder, listReplacement)
			} else {
				return "", nil, fmt.Errorf("parameter '%s' must be a list for IN clause", paramName)
			}
		} else {
			// Regular parameter substitution
			sqlParams[paramName] = value
		}
	}

	return result, sqlParams, nil
}

// validatePackParameters validates all parameters required by a pack
func (e *ComplianceExecutor) validatePackParameters(pack *QueryPack, providedParams map[string]interface{}) error {
	// Collect all required parameters from pack and queries
	requiredParams := make(map[string]*PackParameter)
	
	// Add pack-level parameters
	for _, param := range pack.Parameters {
		if param.Required {
			requiredParams[param.Name] = &param
		}
	}

	// Add query-level parameters
	for _, query := range pack.Queries {
		for _, paramName := range query.Parameters {
			// Find parameter definition
			var paramDef *PackParameter
			for _, p := range pack.Parameters {
				if p.Name == paramName {
					paramDef = &p
					break
				}
			}
			
			if paramDef != nil && paramDef.Required {
				requiredParams[paramName] = paramDef
			}
		}
	}

	// Validate each required parameter
	for paramName, paramDef := range requiredParams {
		value, exists := providedParams[paramName]
		if !exists {
			// Check for default value
			if paramDef.Default != nil {
				providedParams[paramName] = paramDef.Default
				continue
			}
			return fmt.Errorf("required parameter '%s' not provided", paramName)
		}

		// Validate parameter value
		if err := paramDef.ValidateParameter(value); err != nil {
			return fmt.Errorf("parameter '%s' validation failed: %w", paramName, err)
		}
	}

	return nil
}

// convertToComplianceResults converts raw query results to standardized compliance format
func (e *ComplianceExecutor) convertToComplianceResults(result *query.QueryResult) ([]ComplianceResult, error) {
	var complianceResults []ComplianceResult

	for _, row := range result.Rows {
		complianceResult := ComplianceResult{
			Timestamp: time.Now(),
		}

		// Map required fields
		if val, ok := row["resource_id"]; ok && val != nil {
			complianceResult.ResourceID = fmt.Sprintf("%v", val)
		} else {
			return nil, fmt.Errorf("missing required field: resource_id")
		}

		if val, ok := row["resource_name"]; ok && val != nil {
			complianceResult.ResourceName = fmt.Sprintf("%v", val)
		}

		if val, ok := row["resource_type"]; ok && val != nil {
			complianceResult.ResourceType = fmt.Sprintf("%v", val)
		} else {
			return nil, fmt.Errorf("missing required field: resource_type")
		}

		if val, ok := row["region"]; ok && val != nil {
			complianceResult.Region = fmt.Sprintf("%v", val)
		}

		if val, ok := row["control_id"]; ok && val != nil {
			complianceResult.ControlID = fmt.Sprintf("%v", val)
		} else {
			return nil, fmt.Errorf("missing required field: control_id")
		}

		if val, ok := row["control_name"]; ok && val != nil {
			complianceResult.ControlName = fmt.Sprintf("%v", val)
		} else {
			return nil, fmt.Errorf("missing required field: control_name")
		}

		if val, ok := row["status"]; ok && val != nil {
			status := fmt.Sprintf("%v", val)
			if !isValidComplianceStatus(status) {
				return nil, fmt.Errorf("invalid status value: %s (must be PASS, FAIL, WARNING, or ERROR)", status)
			}
			complianceResult.Status = status
		} else {
			return nil, fmt.Errorf("missing required field: status")
		}

		if val, ok := row["severity"]; ok && val != nil {
			severity := fmt.Sprintf("%v", val)
			if !isValidSeverity(severity) {
				return nil, fmt.Errorf("invalid severity value: %s", severity)
			}
			complianceResult.Severity = severity
		} else {
			return nil, fmt.Errorf("missing required field: severity")
		}

		if val, ok := row["details"]; ok && val != nil {
			complianceResult.Details = fmt.Sprintf("%v", val)
		} else {
			return nil, fmt.Errorf("missing required field: details")
		}

		// Optional remediation field (NULL for passing checks)
		if val, ok := row["remediation"]; ok && val != nil {
			remediation := fmt.Sprintf("%v", val)
			complianceResult.Remediation = &remediation
		}

		complianceResults = append(complianceResults, complianceResult)
	}

	return complianceResults, nil
}

// categorizeError categorizes an error for better reporting
func (e *ComplianceExecutor) categorizeError(err error) string {
	errMsg := err.Error()
	
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "context deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(errMsg, "parameter") {
		return "parameter"
	}
	if strings.Contains(errMsg, "syntax") || strings.Contains(errMsg, "SQL") {
		return "syntax"
	}
	
	return "execution"
}

// Helper functions

func isValidComplianceStatus(status string) bool {
	validStatuses := []string{"PASS", "FAIL", "WARNING", "ERROR"}
	for _, valid := range validStatuses {
		if status == valid {
			return true
		}
	}
	return false
}
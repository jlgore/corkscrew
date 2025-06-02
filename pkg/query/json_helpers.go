package query

import (
	"context"
	"database/sql"
	"fmt"
)

// JSONHelpers contains SQL helper functions for JSON operations
type JSONHelpers struct {
	db *sql.DB
}

// NewJSONHelpers creates a new JSONHelpers instance
func NewJSONHelpers(db *sql.DB) *JSONHelpers {
	return &JSONHelpers{db: db}
}

// RegisterJSONFunctions registers all custom JSON helper functions with DuckDB
func (j *JSONHelpers) RegisterJSONFunctions(ctx context.Context) error {
	functions := []struct {
		name string
		sql  string
	}{
		{
			name: "extract_json",
			sql: `CREATE OR REPLACE MACRO extract_json(json_data, json_path) AS 
				CASE 
					WHEN json_data IS NULL OR json_path IS NULL THEN NULL
					WHEN json_data = '' OR json_data = 'null' THEN NULL
					WHEN NOT json_valid(json_data) THEN NULL
					ELSE json_extract_string(json_data, json_path)
				END`,
		},
		{
			name: "json_path",
			sql: `CREATE OR REPLACE MACRO json_path(json_data, json_path) AS 
				CASE 
					WHEN json_data IS NULL OR json_path IS NULL THEN NULL
					WHEN json_data = '' OR json_data = 'null' THEN NULL
					WHEN NOT json_valid(json_data) THEN NULL
					ELSE json_extract(json_data, json_path)
				END`,
		},
		{
			name: "has_tag",
			sql: `CREATE OR REPLACE MACRO has_tag(tags_data, tag_key, tag_value) AS 
				CASE 
					WHEN tags_data IS NULL OR tag_key IS NULL THEN FALSE
					WHEN tags_data = '' OR tags_data = 'null' OR tags_data = '{}' THEN FALSE
					WHEN NOT json_valid(tags_data) THEN FALSE
					WHEN tag_value IS NULL THEN 
						COALESCE(json_extract_string(tags_data, '$.' || tag_key) IS NOT NULL, FALSE)
					ELSE 
						COALESCE(json_extract_string(tags_data, '$.' || tag_key) = tag_value, FALSE)
				END`,
		},
	}

	for _, fn := range functions {
		if _, err := j.db.ExecContext(ctx, fn.sql); err != nil {
			return fmt.Errorf("failed to register function %s: %w", fn.name, err)
		}
	}

	return nil
}

// RegisterAdvancedJSONFunctions registers additional helper functions for complex JSON operations
func (j *JSONHelpers) RegisterAdvancedJSONFunctions(ctx context.Context) error {
	advancedFunctions := []struct {
		name string
		sql  string
	}{
		{
			name: "count_tags",
			sql: `CREATE OR REPLACE MACRO count_tags(tags_data) AS 
				CASE 
					WHEN tags_data IS NULL OR tags_data = '' OR tags_data = 'null' THEN 0
					WHEN NOT json_valid(tags_data) THEN 0
					ELSE 
						COALESCE(array_length(json_keys(tags_data)), 0)
				END`,
		},
		{
			name: "safe_json_extract",
			sql: `CREATE OR REPLACE MACRO safe_json_extract(json_data, json_path, default_value) AS 
				CASE 
					WHEN json_data IS NULL OR json_path IS NULL THEN default_value
					WHEN json_data = '' OR json_data = 'null' THEN default_value
					WHEN NOT json_valid(json_data) THEN default_value
					ELSE 
						COALESCE(json_extract_string(json_data, json_path), default_value)
				END`,
		},
	}

	for _, fn := range advancedFunctions {
		if _, err := j.db.ExecContext(ctx, fn.sql); err != nil {
			return fmt.Errorf("failed to register advanced function %s: %w", fn.name, err)
		}
	}

	return nil
}

// ValidateJSONFunctions tests that all registered functions work correctly
func (j *JSONHelpers) ValidateJSONFunctions(ctx context.Context) error {
	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "extract_json with valid JSON",
			query: `SELECT extract_json('{"key": "value"}', '$.key') as result`,
		},
		{
			name:  "extract_json with NULL",
			query: `SELECT extract_json(NULL, '$.key') as result`,
		},
		{
			name:  "json_path with nested object",
			query: `SELECT json_path('{"nested": {"key": "value"}}', '$.nested') as result`,
		},
		{
			name:  "has_tag with valid tags",
			query: `SELECT has_tag('{"Environment": "production"}', 'Environment', 'production') as result`,
		},
		{
			name:  "has_tag key existence",
			query: `SELECT has_tag('{"Environment": "production"}', 'Environment', NULL) as result`,
		},
		{
			name:  "safe_json_extract with default",
			query: `SELECT safe_json_extract('{}', '$.missing', 'default') as result`,
		},
		{
			name:  "count_tags",
			query: `SELECT count_tags('{"Environment": "production", "Team": "platform"}') as result`,
		},
	}

	for _, tc := range testCases {
		rows, err := j.db.QueryContext(ctx, tc.query)
		if err != nil {
			return fmt.Errorf("validation failed for %s: %w", tc.name, err)
		}
		rows.Close()
	}

	return nil
}

// GetFunctionDocumentation returns documentation for all JSON helper functions
func (j *JSONHelpers) GetFunctionDocumentation() map[string]string {
	return map[string]string{
		"extract_json": `extract_json(json_data, json_path) -> VARCHAR
Safely extracts a string value from JSON data using JSONPath notation.
Returns NULL if json_data is NULL, empty, invalid JSON, or if the path doesn't exist.
Performance optimized with NULL checks and JSON validation.

Example: extract_json(raw_data, '$.BucketPolicy.Statement[0].Effect')`,

		"json_path": `json_path(json_data, json_path) -> JSON
Safely extracts a JSON object/array from JSON data using JSONPath notation.
Returns NULL if json_data is NULL, empty, invalid JSON, or if the path doesn't exist.
Use this when you need to extract complex nested structures.

Example: json_path(raw_data, '$.BucketPolicy.Statement')`,

		"has_tag": `has_tag(tags_data, tag_key, tag_value) -> BOOLEAN
Checks if a tag exists with an optional value match.
- If tag_value is NULL, checks if the tag_key exists
- If tag_value is provided, checks if tag_key exists with that exact value
Returns FALSE for NULL, empty, or invalid JSON input.

Example: has_tag(tags, 'Environment', 'production')`,

		"count_tags": `count_tags(tags_data) -> INTEGER
Returns the number of tags in a JSON tags object.
Returns 0 for NULL, empty, or invalid JSON input.

Example: count_tags(tags) to count total tags`,

		"safe_json_extract": `safe_json_extract(json_data, json_path, default_value) -> VARCHAR
Safely extracts a string value with a fallback default value.
Returns default_value if json_data is NULL, empty, invalid JSON, or if the path doesn't exist.

Example: safe_json_extract(raw_data, '$.Region', 'us-east-1')`,
	}
}

// InitializeJSONHelpers registers all JSON helper functions with the DuckDB engine
func InitializeJSONHelpers(ctx context.Context, db *sql.DB) error {
	helpers := NewJSONHelpers(db)
	
	// Register core functions
	if err := helpers.RegisterJSONFunctions(ctx); err != nil {
		return fmt.Errorf("failed to register core JSON functions: %w", err)
	}
	
	// Register advanced functions
	if err := helpers.RegisterAdvancedJSONFunctions(ctx); err != nil {
		return fmt.Errorf("failed to register advanced JSON functions: %w", err)
	}
	
	// Validate all functions work
	if err := helpers.ValidateJSONFunctions(ctx); err != nil {
		return fmt.Errorf("JSON function validation failed: %w", err)
	}
	
	return nil
}

// ListRegisteredFunctions returns a list of all registered JSON helper function names
func ListRegisteredJSONFunctions() []string {
	return []string{
		"extract_json",
		"json_path", 
		"has_tag",
		"count_tags",
		"safe_json_extract",
	}
}
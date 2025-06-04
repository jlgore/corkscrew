package query

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/marcboeker/go-duckdb"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Load JSON extension
	if _, err := db.Exec("INSTALL json; LOAD json;"); err != nil {
		t.Fatalf("Failed to load JSON extension: %v", err)
	}

	return db
}

func TestJSONHelperFunctions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Initialize JSON helpers
	if err := InitializeJSONHelpers(ctx, db); err != nil {
		t.Fatalf("Failed to initialize JSON helpers: %v", err)
	}

	// Create test table with sample data
	_, err := db.Exec(`
		CREATE TABLE test_resources (
			id INTEGER,
			name VARCHAR,
			raw_data VARCHAR,
			attributes VARCHAR,
			tags VARCHAR
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert test data
	testData := []struct {
		id         int
		name       string
		raw_data   string
		attributes string
		tags       string
	}{
		{
			id:   1,
			name: "test-bucket",
			raw_data: `{
				"BucketPolicy": {
					"Statement": [
						{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject"}
					]
				},
				"Region": "us-east-1",
				"SecurityGroups": ["sg-12345", "sg-67890"]
			}`,
			attributes: `{"PublicRead": true, "Versioning": false}`,
			tags:       `{"Environment": "production", "Team": "platform", "CostCenter": "12345"}`,
		},
		{
			id:         2,
			name:       "empty-resource",
			raw_data:   `{}`,
			attributes: `null`,
			tags:       `{}`,
		},
		{
			id:         3,
			name:       "null-resource",
			raw_data:   `null`,
			attributes: `null`,
			tags:       `null`,
		},
	}

	for _, data := range testData {
		_, err := db.Exec(`
			INSERT INTO test_resources (id, name, raw_data, attributes, tags)
			VALUES (?, ?, ?, ?, ?)
		`, data.id, data.name, data.raw_data, data.attributes, data.tags)
		if err != nil {
			t.Fatalf("Failed to insert test data: %v", err)
		}
	}

	// Test extract_json function
	t.Run("extract_json", func(t *testing.T) {
		tests := []struct {
			name     string
			query    string
			expected string
		}{
			{
				name:     "valid_json_path",
				query:    "SELECT extract_json(raw_data, '$.Region') as result FROM test_resources WHERE id = 1",
				expected: "us-east-1",
			},
			{
				name:     "nested_json_path",
				query:    "SELECT extract_json(raw_data, '$.BucketPolicy.Statement[0].Effect') as result FROM test_resources WHERE id = 1",
				expected: "Allow",
			},
			{
				name:     "empty_json",
				query:    "SELECT extract_json(raw_data, '$.Region') as result FROM test_resources WHERE id = 2",
				expected: "",
			},
			{
				name:     "null_json",
				query:    "SELECT extract_json(raw_data, '$.Region') as result FROM test_resources WHERE id = 3",
				expected: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var result sql.NullString
				err := db.QueryRow(tt.query).Scan(&result)
				if err != nil {
					t.Errorf("Query failed: %v", err)
					return
				}

				if tt.expected == "" && result.Valid {
					t.Errorf("Expected NULL, got %v", result.String)
				} else if tt.expected != "" && (!result.Valid || result.String != tt.expected) {
					t.Errorf("Expected %s, got %v", tt.expected, result)
				}
			})
		}
	})

	// Test json_path function
	t.Run("json_path", func(t *testing.T) {
		query := "SELECT CAST(json_path(raw_data, '$.BucketPolicy.Statement') AS VARCHAR) as result FROM test_resources WHERE id = 1"
		var result sql.NullString
		err := db.QueryRow(query).Scan(&result)
		if err != nil {
			t.Errorf("Query failed: %v", err)
			return
		}

		if !result.Valid {
			t.Error("Expected valid JSON result, got NULL")
		}
	})

	// Test has_tag function
	t.Run("has_tag", func(t *testing.T) {
		tests := []struct {
			name     string
			query    string
			expected bool
		}{
			{
				name:     "tag_exists_with_value",
				query:    "SELECT has_tag(tags, 'Environment', 'production') as result FROM test_resources WHERE id = 1",
				expected: true,
			},
			{
				name:     "tag_exists_wrong_value",
				query:    "SELECT has_tag(tags, 'Environment', 'staging') as result FROM test_resources WHERE id = 1",
				expected: false,
			},
			{
				name:     "tag_key_exists",
				query:    "SELECT has_tag(tags, 'Team', NULL) as result FROM test_resources WHERE id = 1",
				expected: true,
			},
			{
				name:     "tag_key_missing",
				query:    "SELECT has_tag(tags, 'Missing', NULL) as result FROM test_resources WHERE id = 1",
				expected: false,
			},
			{
				name:     "empty_tags",
				query:    "SELECT has_tag(tags, 'Environment', 'production') as result FROM test_resources WHERE id = 2",
				expected: false,
			},
			{
				name:     "null_tags",
				query:    "SELECT has_tag(tags, 'Environment', 'production') as result FROM test_resources WHERE id = 3",
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var result bool
				err := db.QueryRow(tt.query).Scan(&result)
				if err != nil {
					t.Errorf("Query failed: %v", err)
					return
				}

				if result != tt.expected {
					t.Errorf("Expected %v, got %v", tt.expected, result)
				}
			})
		}
	})

	// Test advanced functions

	t.Run("count_tags", func(t *testing.T) {
		query := "SELECT count_tags(tags) as result FROM test_resources WHERE id = 1"
		var result int
		err := db.QueryRow(query).Scan(&result)
		if err != nil {
			t.Errorf("Query failed: %v", err)
			return
		}

		if result != 3 {
			t.Errorf("Expected 3 tags, got %d", result)
		}
	})

	t.Run("safe_json_extract", func(t *testing.T) {
		query := "SELECT safe_json_extract(raw_data, '$.MissingField', 'default_value') as result FROM test_resources WHERE id = 1"
		var result string
		err := db.QueryRow(query).Scan(&result)
		if err != nil {
			t.Errorf("Query failed: %v", err)
			return
		}

		if result != "default_value" {
			t.Errorf("Expected 'default_value', got %s", result)
		}
	})
}

func TestComplexQueryExample(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Initialize JSON helpers
	if err := InitializeJSONHelpers(ctx, db); err != nil {
		t.Fatalf("Failed to initialize JSON helpers: %v", err)
	}

	// Create AWS resources table similar to the example
	_, err := db.Exec(`
		CREATE TABLE aws_resources (
			id INTEGER,
			name VARCHAR,
			type VARCHAR,
			raw_data VARCHAR,
			tags VARCHAR
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert sample bucket data
	_, err = db.Exec(`
		INSERT INTO aws_resources (id, name, type, raw_data, tags)
		VALUES (?, ?, ?, ?, ?)
	`, 1, "my-bucket", "Bucket", `{
		"BucketPolicy": {
			"Statement": [
				{"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject"}
			]
		}
	}`, `{"Environment": "production", "Team": "platform"}`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Test the example query from the requirements
	query := `
		SELECT 
			name,
			extract_json(raw_data, '$.BucketPolicy.Statement[0].Effect') as policy_effect,
			has_tag(tags, 'Environment', 'production') as is_prod
		FROM aws_resources
		WHERE type = 'Bucket'
	`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("Example query failed: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("Expected at least one row")
	}

	var name, policyEffect string
	var isProd bool
	err = rows.Scan(&name, &policyEffect, &isProd)
	if err != nil {
		t.Fatalf("Failed to scan result: %v", err)
	}

	if name != "my-bucket" {
		t.Errorf("Expected name 'my-bucket', got %s", name)
	}
	if policyEffect != "Allow" {
		t.Errorf("Expected policy_effect 'Allow', got %s", policyEffect)
	}
	if !isProd {
		t.Error("Expected is_prod to be true")
	}
}

func TestFunctionDocumentation(t *testing.T) {
	helpers := &JSONHelpers{}
	docs := helpers.GetFunctionDocumentation()

	expectedFunctions := []string{
		"extract_json",
		"json_path",
		"has_tag",
		"count_tags",
		"safe_json_extract",
	}

	for _, fn := range expectedFunctions {
		if _, exists := docs[fn]; !exists {
			t.Errorf("Missing documentation for function: %s", fn)
		}
	}
}

func TestListRegisteredJSONFunctions(t *testing.T) {
	functions := ListRegisteredJSONFunctions()
	
	expected := 5 // Number of functions we register
	if len(functions) != expected {
		t.Errorf("Expected %d functions, got %d", expected, len(functions))
	}

	// Check core functions are included
	coreFunctions := []string{"extract_json", "json_path", "has_tag"}
	for _, core := range coreFunctions {
		found := false
		for _, fn := range functions {
			if fn == core {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Core function %s not found in registered functions", core)
		}
	}
}
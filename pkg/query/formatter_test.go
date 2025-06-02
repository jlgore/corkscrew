package query

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Helper function to create test data
func createTestQueryResult() *QueryResult {
	return &QueryResult{
		Columns: []ColumnInfo{
			{Name: "id", Type: "INTEGER", Nullable: false},
			{Name: "name", Type: "VARCHAR", Nullable: true},
			{Name: "value", Type: "DOUBLE", Nullable: true},
			{Name: "active", Type: "BOOLEAN", Nullable: false},
			{Name: "data", Type: "JSON", Nullable: true},
		},
		Rows: []map[string]interface{}{
			{"id": 1, "name": "Alice", "value": 123.45, "active": true, "data": `{"key": "value1"}`},
			{"id": 2, "name": nil, "value": nil, "active": false, "data": nil},
			{"id": 3, "name": "", "value": 0.0, "active": true, "data": `{}`},
			{"id": 4, "name": "Bob with \"quotes\" and, commas", "value": -999.99, "active": false, "data": `[1,2,3]`},
		},
		Stats: QueryStats{
			Duration:      time.Millisecond * 150,
			RowsAffected:  0,
			RowsReturned:  4,
			ExecutionTime: time.Now(),
		},
	}
}

func TestFormatterOptions(t *testing.T) {
	// Test default options
	opts := DefaultFormatterOptions()
	if opts.CSVDelimiter != ',' {
		t.Errorf("Expected CSV delimiter ',', got %c", opts.CSVDelimiter)
	}
	if opts.JSONPretty != true {
		t.Error("Expected JSON pretty printing to be enabled by default")
	}
	if opts.NullValue != "NULL" {
		t.Errorf("Expected NULL value 'NULL', got %s", opts.NullValue)
	}

	// Test terminal width detection with environment variable
	os.Setenv("COLUMNS", "100")
	defer os.Unsetenv("COLUMNS")
	opts = DefaultFormatterOptions()
	if opts.TableMaxWidth != 100 {
		t.Errorf("Expected table max width 100, got %d", opts.TableMaxWidth)
	}
}

func TestFormatValue(t *testing.T) {
	opts := DefaultFormatterOptions()

	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"nil value", nil, "NULL"},
		{"empty string", "", ""},
		{"regular string", "hello", "hello"},
		{"boolean true", true, "true"},
		{"boolean false", false, "false"},
		{"integer", 42, "42"},
		{"float", 3.14159, "3.14159"},
		{"byte slice", []byte("bytes"), "bytes"},
		{"empty byte slice", []byte{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValue(tt.input, opts)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}

	// Test truncation
	opts.MaxValueLength = 10
	opts.TruncateValues = true
	longString := "this is a very long string that should be truncated"
	result := formatValue(longString, opts)
	if len(result) != 10 || !strings.HasSuffix(result, "...") {
		t.Errorf("Expected truncated string with '...', got %q", result)
	}
}

func TestCSVFormatter(t *testing.T) {
	result := createTestQueryResult()
	formatter := NewCSVFormatter(nil)

	var buf bytes.Buffer
	err := formatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("CSV formatting failed: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Check header
	if lines[0] != "id,name,value,active,data" {
		t.Errorf("Unexpected CSV header: %s", lines[0])
	}

	// Check first data row
	expected := "1,Alice,123.45,true,\"{\"\"key\"\": \"\"value1\"\"}\""
	if lines[1] != expected {
		t.Errorf("Expected CSV row: %s, got: %s", expected, lines[1])
	}

	// Check row with NULL values
	expected = "2,NULL,NULL,false,NULL"
	if lines[2] != expected {
		t.Errorf("Expected CSV row with NULLs: %s, got: %s", expected, lines[2])
	}

	// Check row with special characters (quotes and commas)
	if !strings.Contains(lines[4], "\"Bob with \"\"quotes\"\" and, commas\"") {
		t.Errorf("CSV should properly escape quotes and commas: %s", lines[4])
	}
}

func TestCSVFormatterCustomOptions(t *testing.T) {
	result := createTestQueryResult()
	opts := DefaultFormatterOptions()
	opts.CSVDelimiter = ';'
	opts.NullValue = "N/A"
	formatter := NewCSVFormatter(opts)

	var buf bytes.Buffer
	err := formatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("CSV formatting failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, ";") {
		t.Error("Expected semicolon delimiter in CSV output")
	}
	if !strings.Contains(output, "N/A") {
		t.Error("Expected custom NULL value 'N/A' in CSV output")
	}
}

func TestJSONFormatter(t *testing.T) {
	result := createTestQueryResult()
	formatter := NewJSONFormatter(nil)

	var buf bytes.Buffer
	err := formatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("JSON formatting failed: %v", err)
	}

	// Parse the JSON to verify it's valid
	var output map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	// Check structure
	if _, ok := output["columns"]; !ok {
		t.Error("JSON output missing 'columns' field")
	}
	if _, ok := output["rows"]; !ok {
		t.Error("JSON output missing 'rows' field")
	}
	if _, ok := output["stats"]; !ok {
		t.Error("JSON output missing 'stats' field")
	}

	// Check that rows are preserved
	rows, ok := output["rows"].([]interface{})
	if !ok || len(rows) != 4 {
		t.Errorf("Expected 4 rows in JSON output, got %d", len(rows))
	}
}

func TestJSONFormatterCompact(t *testing.T) {
	result := createTestQueryResult()
	opts := DefaultFormatterOptions()
	opts.JSONPretty = false
	formatter := NewJSONFormatter(opts)

	var buf bytes.Buffer
	err := formatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("JSON formatting failed: %v", err)
	}

	output := buf.String()
	// Compact JSON should not have extra whitespace
	if strings.Contains(output, "\n  ") {
		t.Error("Expected compact JSON without indentation")
	}
}

func TestTableFormatter(t *testing.T) {
	result := createTestQueryResult()
	formatter := NewTableFormatter(nil)

	var buf bytes.Buffer
	err := formatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("Table formatting failed: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have header, separator, and data rows
	if len(lines) < 6 { // header + separator + 4 data rows
		t.Errorf("Expected at least 6 lines, got %d", len(lines))
	}

	// Check that header contains column names
	headerLine := lines[0]
	for _, col := range result.Columns {
		if !strings.Contains(headerLine, col.Name) {
			t.Errorf("Header missing column %s: %s", col.Name, headerLine)
		}
	}

	// Check that separator line contains dashes
	separatorLine := lines[1]
	if !strings.Contains(separatorLine, "---") {
		t.Errorf("Expected separator line with dashes: %s", separatorLine)
	}

	// Check that NULL values are displayed correctly
	nullRowFound := false
	for _, line := range lines[2:] {
		if strings.Contains(line, "NULL") {
			nullRowFound = true
			break
		}
	}
	if !nullRowFound {
		t.Error("Expected to find NULL values in table output")
	}
}

func TestTableFormatterEmptyResult(t *testing.T) {
	result := &QueryResult{
		Columns: []ColumnInfo{
			{Name: "id", Type: "INTEGER", Nullable: false},
		},
		Rows:  []map[string]interface{}{},
		Stats: QueryStats{},
	}

	formatter := NewTableFormatter(nil)
	var buf bytes.Buffer
	err := formatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("Table formatting failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No rows returned") {
		t.Error("Expected 'No rows returned' message for empty result")
	}
}

func TestTableFormatterWidth(t *testing.T) {
	// Create result with very long values
	result := &QueryResult{
		Columns: []ColumnInfo{
			{Name: "short", Type: "VARCHAR", Nullable: false},
			{Name: "very_long_column_name", Type: "VARCHAR", Nullable: false},
		},
		Rows: []map[string]interface{}{
			{"short": "a", "very_long_column_name": "this is a very long value that should be truncated"},
		},
		Stats: QueryStats{},
	}

	opts := DefaultFormatterOptions()
	opts.TableMaxWidth = 50 // Force narrow table
	formatter := NewTableFormatter(opts)

	var buf bytes.Buffer
	err := formatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("Table formatting failed: %v", err)
	}

	output := buf.String()
	// Check that long values are truncated
	if strings.Contains(output, "this is a very long value that should be truncated") {
		t.Error("Expected long values to be truncated")
	}
}

func TestStreamingCSV(t *testing.T) {
	columns := []ColumnInfo{
		{Name: "id", Type: "INTEGER", Nullable: false},
		{Name: "name", Type: "VARCHAR", Nullable: true},
	}

	// Create a channel for streaming data
	rowChan := make(chan Row, 3)
	rowChan <- Row{"id": 1, "name": "Alice"}
	rowChan <- Row{"id": 2, "name": nil}
	rowChan <- Row{"id": 3, "name": "Bob"}
	close(rowChan)

	formatter := NewCSVFormatter(nil)
	var buf bytes.Buffer
	err := formatter.FormatStream(rowChan, columns, &buf)
	if err != nil {
		t.Fatalf("CSV streaming failed: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have header + 3 data rows
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines, got %d", len(lines))
	}

	// Check header
	if lines[0] != "id,name" {
		t.Errorf("Unexpected header: %s", lines[0])
	}

	// Check that NULL is properly formatted
	if !strings.Contains(output, "NULL") {
		t.Error("Expected NULL value in streaming output")
	}
}

func TestStreamingJSON(t *testing.T) {
	columns := []ColumnInfo{
		{Name: "id", Type: "INTEGER", Nullable: false},
		{Name: "name", Type: "VARCHAR", Nullable: true},
	}

	// Create a channel for streaming data
	rowChan := make(chan Row, 2)
	rowChan <- Row{"id": 1, "name": "Alice"}
	rowChan <- Row{"id": 2, "name": nil}
	close(rowChan)

	formatter := NewJSONFormatter(nil)
	var buf bytes.Buffer
	err := formatter.FormatStream(rowChan, columns, &buf)
	if err != nil {
		t.Fatalf("JSON streaming failed: %v", err)
	}

	// Parse the JSON to verify it's valid
	var output map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	// Check structure
	if _, ok := output["columns"]; !ok {
		t.Error("JSON output missing 'columns' field")
	}
	if _, ok := output["rows"]; !ok {
		t.Error("JSON output missing 'rows' field")
	}

	rows, ok := output["rows"].([]interface{})
	if !ok || len(rows) != 2 {
		t.Errorf("Expected 2 rows in JSON streaming output, got %d", len(rows))
	}
}

func TestStreamingTable(t *testing.T) {
	columns := []ColumnInfo{
		{Name: "id", Type: "INTEGER", Nullable: false},
		{Name: "name", Type: "VARCHAR", Nullable: true},
	}

	// Create a channel for streaming data
	rowChan := make(chan Row, 2)
	rowChan <- Row{"id": 1, "name": "Alice"}
	rowChan <- Row{"id": 2, "name": nil}
	close(rowChan)

	formatter := NewTableFormatter(nil)
	var buf bytes.Buffer
	err := formatter.FormatStream(rowChan, columns, &buf)
	if err != nil {
		t.Fatalf("Table streaming failed: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have header, separator, and data rows
	if len(lines) < 4 {
		t.Errorf("Expected at least 4 lines, got %d", len(lines))
	}

	// Check that NULL values are handled in streaming
	if !strings.Contains(output, "NULL") {
		t.Error("Expected NULL value in streaming table output")
	}
}

func TestFormatterFactory(t *testing.T) {
	factory := &FormatterFactory{}

	// Test supported formats
	supportedFormats := factory.GetSupportedFormats()
	expectedFormats := []string{"csv", "json", "table"}
	
	if len(supportedFormats) != len(expectedFormats) {
		t.Errorf("Expected %d supported formats, got %d", len(expectedFormats), len(supportedFormats))
	}

	for _, format := range expectedFormats {
		found := false
		for _, supported := range supportedFormats {
			if supported == format {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected format %s to be supported", format)
		}
	}

	// Test creating formatters
	csvFormatter, err := factory.CreateFormatter("csv", nil)
	if err != nil {
		t.Errorf("Failed to create CSV formatter: %v", err)
	}
	if _, ok := csvFormatter.(*CSVFormatter); !ok {
		t.Error("Expected CSVFormatter instance")
	}

	jsonFormatter, err := factory.CreateFormatter("json", nil)
	if err != nil {
		t.Errorf("Failed to create JSON formatter: %v", err)
	}
	if _, ok := jsonFormatter.(*JSONFormatter); !ok {
		t.Error("Expected JSONFormatter instance")
	}

	tableFormatter, err := factory.CreateFormatter("table", nil)
	if err != nil {
		t.Errorf("Failed to create table formatter: %v", err)
	}
	if _, ok := tableFormatter.(*TableFormatter); !ok {
		t.Error("Expected TableFormatter instance")
	}

	// Test case insensitivity
	csvFormatter2, err := factory.CreateFormatter("CSV", nil)
	if err != nil {
		t.Errorf("Failed to create CSV formatter with uppercase: %v", err)
	}
	if _, ok := csvFormatter2.(*CSVFormatter); !ok {
		t.Error("Expected case-insensitive format creation")
	}

	// Test unsupported format
	_, err = factory.CreateFormatter("xml", nil)
	if err == nil {
		t.Error("Expected error for unsupported format 'xml'")
	}
}

func TestFormatterWithCustomNullValues(t *testing.T) {
	result := &QueryResult{
		Columns: []ColumnInfo{
			{Name: "col1", Type: "VARCHAR", Nullable: true},
		},
		Rows: []map[string]interface{}{
			{"col1": nil},
			{"col1": ""},
		},
		Stats: QueryStats{},
	}

	opts := DefaultFormatterOptions()
	opts.NullValue = "N/A"
	opts.EmptyValue = "<empty>"

	// Test CSV
	csvFormatter := NewCSVFormatter(opts)
	var buf bytes.Buffer
	err := csvFormatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("CSV formatting failed: %v", err)
	}
	
	output := buf.String()
	if !strings.Contains(output, "N/A") {
		t.Error("Expected custom NULL value 'N/A' in CSV output")
	}
	if !strings.Contains(output, "<empty>") {
		t.Error("Expected custom empty value '<empty>' in CSV output")
	}

	// Test Table
	tableFormatter := NewTableFormatter(opts)
	buf.Reset()
	err = tableFormatter.Format(result, &buf)
	if err != nil {
		t.Fatalf("Table formatting failed: %v", err)
	}
	
	output = buf.String()
	if !strings.Contains(output, "N/A") {
		t.Error("Expected custom NULL value 'N/A' in table output")
	}
	if !strings.Contains(output, "<empty>") {
		t.Error("Expected custom empty value '<empty>' in table output")
	}
}

func BenchmarkCSVFormatter(b *testing.B) {
	result := createTestQueryResult()
	formatter := NewCSVFormatter(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		err := formatter.Format(result, &buf)
		if err != nil {
			b.Fatalf("CSV formatting failed: %v", err)
		}
	}
}

func BenchmarkJSONFormatter(b *testing.B) {
	result := createTestQueryResult()
	formatter := NewJSONFormatter(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		err := formatter.Format(result, &buf)
		if err != nil {
			b.Fatalf("JSON formatting failed: %v", err)
		}
	}
}

func BenchmarkTableFormatter(b *testing.B) {
	result := createTestQueryResult()
	formatter := NewTableFormatter(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		err := formatter.Format(result, &buf)
		if err != nil {
			b.Fatalf("Table formatting failed: %v", err)
		}
	}
}
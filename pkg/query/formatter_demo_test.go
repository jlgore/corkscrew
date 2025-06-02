package query

import (
	"fmt"
	"os"
	"time"
)

// ExampleCSVFormatter demonstrates CSV output formatting
func ExampleCSVFormatter() {
	// Create sample data
	result := &QueryResult{
		Columns: []ColumnInfo{
			{Name: "id", Type: "INTEGER", Nullable: false},
			{Name: "name", Type: "VARCHAR", Nullable: true},
			{Name: "active", Type: "BOOLEAN", Nullable: false},
		},
		Rows: []map[string]interface{}{
			{"id": 1, "name": "Alice", "active": true},
			{"id": 2, "name": nil, "active": false},
			{"id": 3, "name": "Bob, Jr.", "active": true},
		},
		Stats: QueryStats{Duration: time.Millisecond * 150, RowsReturned: 3},
	}

	// Create CSV formatter
	formatter := NewCSVFormatter(nil)
	
	// Format to stdout
	err := formatter.Format(result, os.Stdout)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	
	// Output:
	// id,name,active
	// 1,Alice,true
	// 2,NULL,false
	// 3,"Bob, Jr.",true
}

// ExampleJSONFormatter demonstrates JSON output formatting
func ExampleJSONFormatter() {
	// Create sample data
	result := &QueryResult{
		Columns: []ColumnInfo{
			{Name: "id", Type: "INTEGER", Nullable: false},
			{Name: "value", Type: "DOUBLE", Nullable: true},
		},
		Rows: []map[string]interface{}{
			{"id": 1, "value": 123.45},
			{"id": 2, "value": nil},
		},
		Stats: QueryStats{Duration: time.Millisecond * 75, RowsReturned: 2},
	}

	// Create compact JSON formatter
	opts := DefaultFormatterOptions()
	opts.JSONPretty = false
	formatter := NewJSONFormatter(opts)
	
	// Format to stdout
	err := formatter.Format(result, os.Stdout)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// ExampleTableFormatter demonstrates table output formatting  
func ExampleTableFormatter() {
	// Create sample data
	result := &QueryResult{
		Columns: []ColumnInfo{
			{Name: "id", Type: "INTEGER", Nullable: false},
			{Name: "name", Type: "VARCHAR", Nullable: true},
			{Name: "status", Type: "VARCHAR", Nullable: false},
		},
		Rows: []map[string]interface{}{
			{"id": 1, "name": "Alice", "status": "active"},
			{"id": 2, "name": nil, "status": "inactive"},
			{"id": 3, "name": "Bob", "status": "pending"},
		},
		Stats: QueryStats{Duration: time.Millisecond * 200, RowsReturned: 3},
	}

	// Create table formatter
	formatter := NewTableFormatter(nil)
	
	// Format to stdout
	err := formatter.Format(result, os.Stdout)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	
	// Output will be a nicely formatted table with headers and aligned columns
}

// ExampleFormatterFactory demonstrates using the factory pattern
func ExampleFormatterFactory() {
	factory := &FormatterFactory{}
	
	// List supported formats
	formats := factory.GetSupportedFormats()
	fmt.Printf("Supported formats: %v\n", formats)
	
	// Create formatter dynamically
	formatter, err := factory.CreateFormatter("csv", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	
	// Use the formatter...
	_ = formatter
	
	// Output:
	// Supported formats: [csv json table]
}

// ExampleStreamingCSV demonstrates streaming CSV output
func ExampleCSVFormatter_FormatStream() {
	columns := []ColumnInfo{
		{Name: "id", Type: "INTEGER", Nullable: false},
		{Name: "message", Type: "VARCHAR", Nullable: true},
	}

	// Create a channel for streaming data
	rowChan := make(chan Row, 2)
	rowChan <- Row{"id": 1, "message": "Hello"}
	rowChan <- Row{"id": 2, "message": "World"}
	close(rowChan)

	// Create CSV formatter
	formatter := NewCSVFormatter(nil)
	
	// Format streaming data to stdout
	err := formatter.FormatStream(rowChan, columns, os.Stdout)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	
	// Output:
	// id,message
	// 1,Hello
	// 2,World
}

// ExampleFormatterOptions demonstrates custom formatting options
func ExampleFormatterOptions() {
	// Create custom options
	opts := DefaultFormatterOptions()
	opts.NullValue = "N/A"
	opts.CSVDelimiter = ';'
	opts.TableMaxWidth = 60
	
	result := &QueryResult{
		Columns: []ColumnInfo{
			{Name: "name", Type: "VARCHAR", Nullable: true},
		},
		Rows: []map[string]interface{}{
			{"name": "Alice"},
			{"name": nil},
		},
		Stats: QueryStats{},
	}

	// Use custom options with CSV formatter
	formatter := NewCSVFormatter(opts)
	err := formatter.Format(result, os.Stdout)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	
	// Output:
	// name
	// Alice
	// N/A
}
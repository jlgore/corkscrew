package query

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// Row represents a single row of data for streaming
type Row map[string]interface{}

// Formatter interface for converting QueryResult to different output formats
type Formatter interface {
	Format(result *QueryResult, writer io.Writer) error
	FormatStream(resultChan <-chan Row, columns []ColumnInfo, writer io.Writer) error
}

// FormatterOptions contains configuration options for formatters
type FormatterOptions struct {
	// CSV options
	CSVDelimiter rune
	CSVQuote     rune
	
	// JSON options
	JSONPretty bool
	JSONIndent string
	
	// Table options
	TableMaxWidth    int
	TableMinColWidth int
	TableMaxColWidth int
	TablePadding     int
	
	// Common options
	NullValue      string
	EmptyValue     string
	TruncateValues bool
	MaxValueLength int
}

// getTerminalWidth tries to determine terminal width from environment, defaults to 80
func getTerminalWidth() int {
	// Try to get from environment variables
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if width, err := strconv.Atoi(cols); err == nil && width > 0 {
			return width
		}
	}
	
	// Check TERM environment for common cases
	if term := os.Getenv("TERM"); term != "" {
		// If we're in a known wide terminal, use a wider default
		switch term {
		case "xterm-256color", "screen-256color":
			return 120
		}
	}

	// Sensible default
	return 80
}

// DefaultFormatterOptions returns sensible default options
func DefaultFormatterOptions() *FormatterOptions {
	terminalWidth := getTerminalWidth()

	return &FormatterOptions{
		CSVDelimiter:     ',',
		CSVQuote:         '"',
		JSONPretty:       true,
		JSONIndent:       "  ",
		TableMaxWidth:    terminalWidth,
		TableMinColWidth: 8,
		TableMaxColWidth: 30,
		TablePadding:     1,
		NullValue:        "NULL",
		EmptyValue:       "",
		TruncateValues:   true,
		MaxValueLength:   100,
	}
}

// formatValue consistently formats values according to options
func formatValue(value interface{}, opts *FormatterOptions) string {
	if value == nil {
		return opts.NullValue
	}

	var str string
	switch v := value.(type) {
	case string:
		if v == "" {
			return opts.EmptyValue
		}
		str = v
	case []byte:
		if len(v) == 0 {
			return opts.EmptyValue
		}
		str = string(v)
	case bool:
		str = strconv.FormatBool(v)
	case int, int8, int16, int32, int64:
		str = fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		str = fmt.Sprintf("%d", v)
	case float32, float64:
		str = fmt.Sprintf("%g", v)
	default:
		str = fmt.Sprintf("%v", v)
	}

	if opts.TruncateValues && len(str) > opts.MaxValueLength {
		if utf8.ValidString(str) {
			// Truncate at character boundary
			runes := []rune(str)
			if len(runes) > opts.MaxValueLength-3 {
				str = string(runes[:opts.MaxValueLength-3]) + "..."
			}
		} else {
			// Truncate bytes
			if len(str) > opts.MaxValueLength-3 {
				str = str[:opts.MaxValueLength-3] + "..."
			}
		}
	}

	return str
}

// CSVFormatter implements CSV output formatting
type CSVFormatter struct {
	Options *FormatterOptions
}

// NewCSVFormatter creates a new CSV formatter with options
func NewCSVFormatter(opts *FormatterOptions) *CSVFormatter {
	if opts == nil {
		opts = DefaultFormatterOptions()
	}
	return &CSVFormatter{Options: opts}
}

// Format formats a QueryResult as CSV
func (f *CSVFormatter) Format(result *QueryResult, writer io.Writer) error {
	csvWriter := csv.NewWriter(writer)
	csvWriter.Comma = f.Options.CSVDelimiter

	// Write header
	headers := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		headers[i] = col.Name
	}
	if err := csvWriter.Write(headers); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write rows
	for _, row := range result.Rows {
		record := make([]string, len(result.Columns))
		for i, col := range result.Columns {
			record[i] = formatValue(row[col.Name], f.Options)
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	csvWriter.Flush()
	return csvWriter.Error()
}

// FormatStream formats streaming data as CSV
func (f *CSVFormatter) FormatStream(resultChan <-chan Row, columns []ColumnInfo, writer io.Writer) error {
	csvWriter := csv.NewWriter(writer)
	csvWriter.Comma = f.Options.CSVDelimiter

	// Write header
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = col.Name
	}
	if err := csvWriter.Write(headers); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write rows as they come in
	for row := range resultChan {
		record := make([]string, len(columns))
		for i, col := range columns {
			record[i] = formatValue(row[col.Name], f.Options)
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
		csvWriter.Flush() // Flush immediately for streaming
	}

	return csvWriter.Error()
}

// JSONFormatter implements JSON output formatting
type JSONFormatter struct {
	Options *FormatterOptions
}

// NewJSONFormatter creates a new JSON formatter with options
func NewJSONFormatter(opts *FormatterOptions) *JSONFormatter {
	if opts == nil {
		opts = DefaultFormatterOptions()
	}
	return &JSONFormatter{Options: opts}
}

// Format formats a QueryResult as JSON
func (f *JSONFormatter) Format(result *QueryResult, writer io.Writer) error {
	// Convert to JSON-friendly format
	output := map[string]interface{}{
		"columns": result.Columns,
		"rows":    result.Rows,
		"stats":   result.Stats,
	}

	encoder := json.NewEncoder(writer)
	if f.Options.JSONPretty {
		encoder.SetIndent("", f.Options.JSONIndent)
	}

	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

// FormatStream formats streaming data as JSON
func (f *JSONFormatter) FormatStream(resultChan <-chan Row, columns []ColumnInfo, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	if f.Options.JSONPretty {
		encoder.SetIndent("", f.Options.JSONIndent)
	}

	// Start JSON structure
	if _, err := writer.Write([]byte(`{"columns":`)); err != nil {
		return err
	}
	if err := encoder.Encode(columns); err != nil {
		return fmt.Errorf("failed to encode columns: %w", err)
	}

	if _, err := writer.Write([]byte(`,"rows":[`)); err != nil {
		return err
	}

	first := true
	for row := range resultChan {
		if !first {
			if _, err := writer.Write([]byte(",")); err != nil {
				return err
			}
		}
		first = false

		if f.Options.JSONPretty {
			if _, err := writer.Write([]byte("\n" + f.Options.JSONIndent)); err != nil {
				return err
			}
		}

		if err := encoder.Encode(row); err != nil {
			return fmt.Errorf("failed to encode row: %w", err)
		}
	}

	if f.Options.JSONPretty {
		if _, err := writer.Write([]byte("\n]}\n")); err != nil {
			return err
		}
	} else {
		if _, err := writer.Write([]byte("]}")); err != nil {
			return err
		}
	}

	return nil
}

// TableFormatter implements table output formatting
type TableFormatter struct {
	Options *FormatterOptions
}

// NewTableFormatter creates a new table formatter with options
func NewTableFormatter(opts *FormatterOptions) *TableFormatter {
	if opts == nil {
		opts = DefaultFormatterOptions()
	}
	return &TableFormatter{Options: opts}
}

// calculateColumnWidths determines optimal column widths based on content and constraints
func (f *TableFormatter) calculateColumnWidths(columns []ColumnInfo, rows []map[string]interface{}) map[string]int {
	widths := make(map[string]int)
	
	// Initialize with header widths
	for _, col := range columns {
		widths[col.Name] = len(col.Name)
	}

	// Check content widths
	for _, row := range rows {
		for _, col := range columns {
			value := formatValue(row[col.Name], f.Options)
			if len(value) > widths[col.Name] {
				widths[col.Name] = len(value)
			}
		}
	}

	// Apply constraints
	totalUsed := 0
	for name, width := range widths {
		if width < f.Options.TableMinColWidth {
			widths[name] = f.Options.TableMinColWidth
		} else if width > f.Options.TableMaxColWidth {
			widths[name] = f.Options.TableMaxColWidth
		}
		totalUsed += widths[name]
	}

	// If we exceed terminal width, proportionally reduce column widths
	separators := len(columns) - 1
	totalPadding := len(columns) * f.Options.TablePadding * 2
	totalAvailable := f.Options.TableMaxWidth - separators - totalPadding
	
	if totalUsed > totalAvailable && totalAvailable > 0 {
		ratio := float64(totalAvailable) / float64(totalUsed)
		for name, width := range widths {
			newWidth := int(float64(width) * ratio)
			if newWidth < f.Options.TableMinColWidth {
				newWidth = f.Options.TableMinColWidth
			}
			widths[name] = newWidth
		}
	}

	return widths
}

// Format formats a QueryResult as a table
func (f *TableFormatter) Format(result *QueryResult, writer io.Writer) error {
	if len(result.Rows) == 0 {
		fmt.Fprintf(writer, "No rows returned.\n")
		return nil
	}

	widths := f.calculateColumnWidths(result.Columns, result.Rows)

	// Create tabwriter for aligned output
	tw := tabwriter.NewWriter(writer, f.Options.TableMinColWidth, 0, f.Options.TablePadding, ' ', 0)

	// Write header
	headerParts := make([]string, len(result.Columns))
	separatorParts := make([]string, len(result.Columns))
	
	for i, col := range result.Columns {
		width := widths[col.Name]
		headerParts[i] = padOrTruncate(col.Name, width)
		separatorParts[i] = strings.Repeat("-", width)
	}

	fmt.Fprintf(tw, "%s\n", strings.Join(headerParts, "\t"))
	fmt.Fprintf(tw, "%s\n", strings.Join(separatorParts, "\t"))

	// Write rows
	for _, row := range result.Rows {
		rowParts := make([]string, len(result.Columns))
		for i, col := range result.Columns {
			value := formatValue(row[col.Name], f.Options)
			width := widths[col.Name]
			rowParts[i] = padOrTruncate(value, width)
		}
		fmt.Fprintf(tw, "%s\n", strings.Join(rowParts, "\t"))
	}

	return tw.Flush()
}

// FormatStream formats streaming data as a table
func (f *TableFormatter) FormatStream(resultChan <-chan Row, columns []ColumnInfo, writer io.Writer) error {
	// For streaming, we can't pre-calculate optimal widths, so use fixed widths
	tw := tabwriter.NewWriter(writer, f.Options.TableMinColWidth, 0, f.Options.TablePadding, ' ', 0)

	// Write header
	headerParts := make([]string, len(columns))
	separatorParts := make([]string, len(columns))
	
	for i, col := range columns {
		width := f.Options.TableMaxColWidth
		if len(col.Name) > width {
			width = len(col.Name)
		}
		headerParts[i] = padOrTruncate(col.Name, width)
		separatorParts[i] = strings.Repeat("-", width)
	}

	fmt.Fprintf(tw, "%s\n", strings.Join(headerParts, "\t"))
	fmt.Fprintf(tw, "%s\n", strings.Join(separatorParts, "\t"))
	tw.Flush()

	// Write rows as they come in
	for row := range resultChan {
		rowParts := make([]string, len(columns))
		for i, col := range columns {
			value := formatValue(row[col.Name], f.Options)
			width := f.Options.TableMaxColWidth
			if len(col.Name) > width {
				width = len(col.Name)
			}
			rowParts[i] = padOrTruncate(value, width)
		}
		fmt.Fprintf(tw, "%s\n", strings.Join(rowParts, "\t"))
		tw.Flush() // Flush immediately for streaming
	}

	return nil
}

// padOrTruncate pads or truncates a string to the specified width
func padOrTruncate(str string, width int) string {
	if len(str) > width {
		if utf8.ValidString(str) {
			runes := []rune(str)
			if len(runes) > width-3 {
				return string(runes[:width-3]) + "..."
			}
		} else {
			if len(str) > width-3 {
				return str[:width-3] + "..."
			}
		}
	}
	return fmt.Sprintf("%-*s", width, str)
}

// FormatterFactory creates formatters based on format type
type FormatterFactory struct{}

// CreateFormatter creates a formatter for the specified format
func (ff *FormatterFactory) CreateFormatter(format string, opts *FormatterOptions) (Formatter, error) {
	if opts == nil {
		opts = DefaultFormatterOptions()
	}

	switch strings.ToLower(format) {
	case "csv":
		return NewCSVFormatter(opts), nil
	case "json":
		return NewJSONFormatter(opts), nil
	case "table", "txt":
		return NewTableFormatter(opts), nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// GetSupportedFormats returns a list of supported output formats
func (ff *FormatterFactory) GetSupportedFormats() []string {
	return []string{"csv", "json", "table"}
}
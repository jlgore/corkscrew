package main

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// TypeMapping maps Go types to DuckDB types
type TypeMapping struct {
	GoType      reflect.Type
	DuckDBType  string
	Nullable    bool
	IndexType   string // "" for no index, "btree", "hash"
}

// FieldAnalysis contains analysis results for a struct field
type FieldAnalysis struct {
	Name        string
	DuckDBType  string
	Nullable    bool
	IsPrimary   bool
	ShouldIndex bool
	IndexType   string
	Comment     string
}

// DynamicSchemaGenerator generates DuckDB schemas from Go struct types
type DynamicSchemaGenerator struct {
	typeMappings map[reflect.Type]TypeMapping
	schemaCache  map[string]*pb.Schema
}

// NewDynamicSchemaGenerator creates a new dynamic schema generator
func NewDynamicSchemaGenerator() *DynamicSchemaGenerator {
	generator := &DynamicSchemaGenerator{
		typeMappings: make(map[reflect.Type]TypeMapping),
		schemaCache:  make(map[string]*pb.Schema),
	}
	
	generator.initializeTypeMappings()
	return generator
}

// initializeTypeMappings sets up the default Go type to DuckDB type mappings
func (dsg *DynamicSchemaGenerator) initializeTypeMappings() {
	// String types
	dsg.typeMappings[reflect.TypeOf("")] = TypeMapping{
		GoType:     reflect.TypeOf(""),
		DuckDBType: "VARCHAR",
		Nullable:   false,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf((*string)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*string)(nil)).Elem(),
		DuckDBType: "VARCHAR",
		Nullable:   true,
		IndexType:  "btree",
	}

	// Integer types
	dsg.typeMappings[reflect.TypeOf(int(0))] = TypeMapping{
		GoType:     reflect.TypeOf(int(0)),
		DuckDBType: "BIGINT",
		Nullable:   false,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf(int32(0))] = TypeMapping{
		GoType:     reflect.TypeOf(int32(0)),
		DuckDBType: "INTEGER",
		Nullable:   false,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf(int64(0))] = TypeMapping{
		GoType:     reflect.TypeOf(int64(0)),
		DuckDBType: "BIGINT",
		Nullable:   false,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf((*int)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*int)(nil)).Elem(),
		DuckDBType: "BIGINT",
		Nullable:   true,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf((*int32)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*int32)(nil)).Elem(),
		DuckDBType: "INTEGER",
		Nullable:   true,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf((*int64)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*int64)(nil)).Elem(),
		DuckDBType: "BIGINT",
		Nullable:   true,
		IndexType:  "btree",
	}

	// Float types
	dsg.typeMappings[reflect.TypeOf(float32(0))] = TypeMapping{
		GoType:     reflect.TypeOf(float32(0)),
		DuckDBType: "REAL",
		Nullable:   false,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf(float64(0))] = TypeMapping{
		GoType:     reflect.TypeOf(float64(0)),
		DuckDBType: "DOUBLE",
		Nullable:   false,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf((*float32)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*float32)(nil)).Elem(),
		DuckDBType: "REAL",
		Nullable:   true,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf((*float64)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*float64)(nil)).Elem(),
		DuckDBType: "DOUBLE",
		Nullable:   true,
		IndexType:  "btree",
	}

	// Boolean types
	dsg.typeMappings[reflect.TypeOf(true)] = TypeMapping{
		GoType:     reflect.TypeOf(true),
		DuckDBType: "BOOLEAN",
		Nullable:   false,
		IndexType:  "",
	}
	dsg.typeMappings[reflect.TypeOf((*bool)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*bool)(nil)).Elem(),
		DuckDBType: "BOOLEAN",
		Nullable:   true,
		IndexType:  "",
	}

	// Time types
	dsg.typeMappings[reflect.TypeOf(time.Time{})] = TypeMapping{
		GoType:     reflect.TypeOf(time.Time{}),
		DuckDBType: "TIMESTAMP",
		Nullable:   false,
		IndexType:  "btree",
	}
	dsg.typeMappings[reflect.TypeOf((*time.Time)(nil)).Elem()] = TypeMapping{
		GoType:     reflect.TypeOf((*time.Time)(nil)).Elem(),
		DuckDBType: "TIMESTAMP",
		Nullable:   true,
		IndexType:  "btree",
	}
}

// AnalyzeStruct analyzes a Go struct and returns field analysis results
func (dsg *DynamicSchemaGenerator) AnalyzeStruct(structType reflect.Type) ([]FieldAnalysis, error) {
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}
	
	if structType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct type, got %s", structType.Kind())
	}

	var fields []FieldAnalysis
	
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		analysis := dsg.analyzeField(field)
		if analysis != nil {
			fields = append(fields, *analysis)
		}
	}

	return fields, nil
}

// analyzeField analyzes a single struct field
func (dsg *DynamicSchemaGenerator) analyzeField(field reflect.StructField) *FieldAnalysis {
	// Skip unexported fields
	if !field.IsExported() {
		return nil
	}

	fieldName := dsg.getFieldName(field)
	fieldType := field.Type
	
	// Handle pointer types
	nullable := false
	if fieldType.Kind() == reflect.Ptr {
		nullable = true
		fieldType = fieldType.Elem()
	}

	// Check if this is a primary key field
	isPrimary := dsg.isPrimaryKeyField(fieldName, field)
	
	// Determine DuckDB type
	duckDBType, shouldIndex, indexType := dsg.mapGoTypeToDuckDB(fieldType, fieldName)
	
	// Override nullable for primary keys
	if isPrimary {
		nullable = false
	}

	return &FieldAnalysis{
		Name:        fieldName,
		DuckDBType:  duckDBType,
		Nullable:    nullable,
		IsPrimary:   isPrimary,
		ShouldIndex: shouldIndex,
		IndexType:   indexType,
		Comment:     dsg.generateFieldComment(field),
	}
}

// getFieldName extracts the field name, considering JSON tags
func (dsg *DynamicSchemaGenerator) getFieldName(field reflect.StructField) string {
	// Check for JSON tag first
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "" && parts[0] != "-" {
			return dsg.toSnakeCase(parts[0])
		}
	}
	
	// Use field name converted to snake_case
	return dsg.toSnakeCase(field.Name)
}

// toSnakeCase converts CamelCase to snake_case
func (dsg *DynamicSchemaGenerator) toSnakeCase(str string) string {
	var result []rune
	for i, r := range str {
		if i > 0 && 'A' <= r && r <= 'Z' {
			result = append(result, '_')
		}
		result = append(result, rune(strings.ToLower(string(r))[0]))
	}
	return string(result)
}

// isPrimaryKeyField determines if a field should be a primary key
func (dsg *DynamicSchemaGenerator) isPrimaryKeyField(fieldName string, field reflect.StructField) bool {
	// Check common primary key field names
	primaryKeyNames := []string{
		"id", "instance_id", "resource_id", "arn", "user_id", 
		"function_name", "bucket_name", "table_name", "db_instance_identifier",
	}
	
	for _, pkName := range primaryKeyNames {
		if strings.EqualFold(fieldName, pkName) {
			return true
		}
	}
	
	// Check struct tags
	if dbTag := field.Tag.Get("db"); strings.Contains(dbTag, "primary_key") {
		return true
	}
	
	return false
}

// mapGoTypeToDuckDB maps a Go type to a DuckDB type
func (dsg *DynamicSchemaGenerator) mapGoTypeToDuckDB(fieldType reflect.Type, fieldName string) (string, bool, string) {
	// Check direct type mapping first
	if mapping, exists := dsg.typeMappings[fieldType]; exists {
		return mapping.DuckDBType, mapping.IndexType != "", mapping.IndexType
	}
	
	// Handle special cases
	switch fieldType.Kind() {
	case reflect.Slice, reflect.Array:
		// Arrays and slices are stored as JSON
		shouldIndex := dsg.shouldIndexJSONField(fieldName)
		return "JSON", shouldIndex, ""
		
	case reflect.Map:
		// Maps are stored as JSON
		shouldIndex := dsg.shouldIndexJSONField(fieldName)
		return "JSON", shouldIndex, ""
		
	case reflect.Struct:
		// Nested structs are stored as JSON unless it's a known type
		if fieldType == reflect.TypeOf(time.Time{}) {
			return "TIMESTAMP", true, "btree"
		}
		shouldIndex := dsg.shouldIndexJSONField(fieldName)
		return "JSON", shouldIndex, ""
		
	case reflect.Interface:
		// Interfaces are stored as JSON
		return "JSON", false, ""
		
	default:
		// Default to VARCHAR for unknown types
		return "VARCHAR", true, "btree"
	}
}

// shouldIndexJSONField determines if a JSON field should be indexed
func (dsg *DynamicSchemaGenerator) shouldIndexJSONField(fieldName string) bool {
	// Common JSON fields that benefit from indexing
	indexableJSONFields := []string{
		"tags", "metadata", "properties", "configuration", "state", "status",
	}
	
	for _, indexField := range indexableJSONFields {
		if strings.Contains(strings.ToLower(fieldName), indexField) {
			return true
		}
	}
	
	return false
}

// generateFieldComment generates a comment for the field
func (dsg *DynamicSchemaGenerator) generateFieldComment(field reflect.StructField) string {
	if comment := field.Tag.Get("comment"); comment != "" {
		return comment
	}
	
	if desc := field.Tag.Get("description"); desc != "" {
		return desc
	}
	
	// Generate basic comment based on field name and type
	return fmt.Sprintf("%s field of type %s", field.Name, field.Type.String())
}

// GenerateSchemaFromStruct generates a complete DuckDB schema from a Go struct
func (dsg *DynamicSchemaGenerator) GenerateSchemaFromStruct(structType reflect.Type, tableName, service, resourceType string) (*pb.Schema, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%s_%s_%s", service, resourceType, tableName)
	if cached, exists := dsg.schemaCache[cacheKey]; exists {
		return cached, nil
	}

	// Analyze the struct
	fields, err := dsg.AnalyzeStruct(structType)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze struct: %w", err)
	}

	// Generate SQL
	sql := dsg.generateCreateTableSQL(tableName, fields)
	
	// Add indexes
	indexSQL := dsg.generateIndexSQL(tableName, fields)
	if indexSQL != "" {
		sql += "\n\n" + indexSQL
	}
	
	// Add views
	viewSQL := dsg.generateCommonViews(tableName, service, fields)
	if viewSQL != "" {
		sql += "\n\n" + viewSQL
	}

	schema := &pb.Schema{
		Name:         tableName,
		Service:      service,
		ResourceType: resourceType,
		Sql:          sql,
		Description:  fmt.Sprintf("Auto-generated schema for %s resources", resourceType),
		Metadata: map[string]string{
			"provider":       "aws",
			"table_type":     "dynamic",
			"supports_json":  "true",
			"generated_from": structType.String(),
			"field_count":    fmt.Sprintf("%d", len(fields)),
		},
	}

	// Cache the result
	dsg.schemaCache[cacheKey] = schema
	
	return schema, nil
}

// generateCreateTableSQL generates the CREATE TABLE SQL statement
func (dsg *DynamicSchemaGenerator) generateCreateTableSQL(tableName string, fields []FieldAnalysis) string {
	var columns []string
	var primaryKeys []string
	
	// Add analyzed fields
	for _, field := range fields {
		nullable := ""
		if field.Nullable {
			nullable = " NULL"
		} else {
			nullable = " NOT NULL"
		}
		
		column := fmt.Sprintf("    %s %s%s", field.Name, field.DuckDBType, nullable)
		columns = append(columns, column)
		
		if field.IsPrimary {
			primaryKeys = append(primaryKeys, field.Name)
		}
	}
	
	// Add standard metadata columns
	columns = append(columns,
		"    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL",
		"    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL",
		"    raw_data JSON",
	)
	
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s", tableName, strings.Join(columns, ",\n"))
	
	// Add primary key constraint
	if len(primaryKeys) > 0 {
		sql += fmt.Sprintf(",\n    PRIMARY KEY (%s)", strings.Join(primaryKeys, ", "))
	}
	
	sql += "\n);"
	
	return sql
}

// generateIndexSQL generates index creation SQL statements
func (dsg *DynamicSchemaGenerator) generateIndexSQL(tableName string, fields []FieldAnalysis) string {
	var indexes []string
	
	for _, field := range fields {
		if field.ShouldIndex && !field.IsPrimary {
			indexName := fmt.Sprintf("idx_%s_%s", strings.ReplaceAll(tableName, "aws_", ""), field.Name)
			
			var indexSQL string
			if field.DuckDBType == "JSON" {
				// For JSON fields, create GIN index if supported, otherwise skip
				indexSQL = fmt.Sprintf("-- JSON index for %s.%s (manual optimization recommended)", tableName, field.Name)
			} else {
				indexSQL = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s(%s);", indexName, tableName, field.Name)
			}
			indexes = append(indexes, indexSQL)
		}
	}
	
	// Always add standard indexes
	standardIndexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_discovered_at ON %s(discovered_at);", strings.ReplaceAll(tableName, "aws_", ""), tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_last_seen ON %s(last_seen);", strings.ReplaceAll(tableName, "aws_", ""), tableName),
	}
	indexes = append(indexes, standardIndexes...)
	
	return strings.Join(indexes, "\n")
}

// generateCommonViews generates common views for the table
func (dsg *DynamicSchemaGenerator) generateCommonViews(tableName, service string, fields []FieldAnalysis) string {
	var views []string
	
	// Recent resources view
	recentViewName := fmt.Sprintf("%s_recent", tableName)
	recentView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT *
FROM %s
WHERE discovered_at >= CURRENT_TIMESTAMP - INTERVAL '24 hours'
ORDER BY discovered_at DESC;`, recentViewName, tableName)
	views = append(views, recentView)
	
	// Summary view
	summaryViewName := fmt.Sprintf("%s_summary", tableName)
	summaryView := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT 
    COUNT(*) as total_resources,
    COUNT(DISTINCT discovered_at::DATE) as discovery_days,
    MIN(discovered_at) as first_discovered,
    MAX(last_seen) as last_updated
FROM %s;`, summaryViewName, tableName)
	views = append(views, summaryView)
	
	return strings.Join(views, "\n\n")
}

// GenerateMigrationScript generates a migration script for schema changes
func (dsg *DynamicSchemaGenerator) GenerateMigrationScript(oldSchema, newSchema *pb.Schema) string {
	return fmt.Sprintf(`-- Migration script for %s
-- Generated: %s
-- From: %s
-- To: %s

-- Note: This is a basic migration template
-- Manual review and testing required before execution

-- TODO: Add specific migration steps
-- TODO: Backup existing data
-- TODO: Test migration on development environment

BEGIN TRANSACTION;

-- Placeholder for migration steps
-- ALTER TABLE %s ADD COLUMN new_column VARCHAR;
-- UPDATE %s SET new_column = 'default_value';

COMMIT;`,
		newSchema.Name,
		time.Now().Format(time.RFC3339),
		oldSchema.Metadata["generated_from"],
		newSchema.Metadata["generated_from"],
		newSchema.Name,
		newSchema.Name,
	)
}

// ClearCache clears the schema cache
func (dsg *DynamicSchemaGenerator) ClearCache() {
	dsg.schemaCache = make(map[string]*pb.Schema)
}

// GetCachedSchemas returns all cached schemas
func (dsg *DynamicSchemaGenerator) GetCachedSchemas() map[string]*pb.Schema {
	return dsg.schemaCache
}
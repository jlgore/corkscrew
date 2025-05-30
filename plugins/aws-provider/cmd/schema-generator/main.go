package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/plugins/aws-provider"
)

var (
	servicesFile = flag.String("services", "generated/services.json", "Path to services.json from analyzer")
	outputDir    = flag.String("output-dir", "generated/schemas", "Output directory for schemas")
	format       = flag.String("format", "sql", "Output format: sql, json, or both")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	dryRun       = flag.Bool("dry-run", false, "Perform dry run without writing files")
)

// SchemaGeneratorConfig contains configuration for schema generation
type SchemaGeneratorConfig struct {
	ServicesFile string
	OutputDir    string
	Format       string
	Verbose      bool
	DryRun       bool
}

// SchemaOutput represents generated schema output
type SchemaOutput struct {
	Service       string    `json:"service"`
	ResourceType  string    `json:"resource_type"`
	TableName     string    `json:"table_name"`
	SQL           string    `json:"sql"`
	JSONSchema    string    `json:"json_schema,omitempty"`
	GeneratedAt   time.Time `json:"generated_at"`
	FieldCount    int       `json:"field_count"`
	IndexCount    int       `json:"index_count"`
	ViewCount     int       `json:"view_count"`
}

func main() {
	flag.Parse()

	config := &SchemaGeneratorConfig{
		ServicesFile: *servicesFile,
		OutputDir:    *outputDir,
		Format:       *format,
		Verbose:      *verbose,
		DryRun:       *dryRun,
	}

	if err := run(config); err != nil {
		log.Fatal(err)
	}
}

func run(config *SchemaGeneratorConfig) error {
	// Load services from analyzer output
	services, err := loadServices(config.ServicesFile)
	if err != nil {
		return fmt.Errorf("failed to load services: %w", err)
	}

	if config.Verbose {
		log.Printf("Loaded %d services from %s", len(services.Services), config.ServicesFile)
	}

	// Create output directory
	if !config.DryRun {
		if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Initialize schema generator
	dynamicGenerator := main.NewDynamicSchemaGenerator()
	standardGenerator := main.NewSchemaGenerator()

	// Track all generated schemas
	allSchemas := make([]*SchemaOutput, 0)
	schemaIndex := make(map[string][]*SchemaOutput)

	// Generate schemas for each service
	for _, service := range services.Services {
		if config.Verbose {
			log.Printf("Generating schemas for service: %s", service.Name)
		}

		serviceSchemas, err := generateServiceSchemas(service, dynamicGenerator, standardGenerator, config)
		if err != nil {
			log.Printf("Error generating schemas for %s: %v", service.Name, err)
			continue
		}

		allSchemas = append(allSchemas, serviceSchemas...)
		schemaIndex[service.Name] = serviceSchemas
	}

	// Write output files
	if !config.DryRun {
		if err := writeOutput(allSchemas, schemaIndex, config); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}

	// Print summary
	fmt.Printf("\nSchema Generation Summary:\n")
	fmt.Printf("  Services processed: %d\n", len(services.Services))
	fmt.Printf("  Total schemas generated: %d\n", len(allSchemas))
	fmt.Printf("  Output directory: %s\n", config.OutputDir)
	fmt.Printf("  Format: %s\n", config.Format)

	if config.DryRun {
		fmt.Println("\n[DRY RUN] No files were written.")
	}

	return nil
}

func loadServices(path string) (*main.AnalysisResult, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var result main.AnalysisResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func generateServiceSchemas(service main.ServiceInfo, dynamicGen *main.DynamicSchemaGenerator, standardGen *main.SchemaGenerator, config *SchemaGeneratorConfig) ([]*SchemaOutput, error) {
	var schemas []*SchemaOutput

	// Generate schemas for each resource type
	for _, resourceType := range service.ResourceTypes {
		tableName := fmt.Sprintf("aws_%s_%s", service.Name, strings.ToLower(resourceType.Name))

		// Try to get Go type for dynamic generation
		goType := getGoTypeForResource(service.Name, resourceType.Name)
		
		var sqlSchema string
		var fieldCount, indexCount, viewCount int

		if goType != nil {
			// Use dynamic generator
			schema, err := dynamicGen.GenerateSchemaFromStruct(
				goType,
				tableName,
				service.Name,
				fmt.Sprintf("AWS::%s::%s", strings.Title(service.Name), resourceType.Name),
			)
			if err != nil {
				if config.Verbose {
					log.Printf("Failed to generate dynamic schema for %s.%s: %v", service.Name, resourceType.Name, err)
				}
			} else {
				sqlSchema = schema.Sql
				fieldCount = len(resourceType.Fields)
				indexCount = strings.Count(sqlSchema, "CREATE INDEX")
				viewCount = strings.Count(sqlSchema, "CREATE OR REPLACE VIEW")
			}
		}

		// Fallback to standard generation if needed
		if sqlSchema == "" {
			sqlSchema = generateStandardSchema(tableName, service.Name, resourceType)
			fieldCount = len(resourceType.Fields)
			indexCount = 4 // Standard indexes
			viewCount = 0
		}

		output := &SchemaOutput{
			Service:      service.Name,
			ResourceType: resourceType.Name,
			TableName:    tableName,
			SQL:          sqlSchema,
			GeneratedAt:  time.Now(),
			FieldCount:   fieldCount,
			IndexCount:   indexCount,
			ViewCount:    viewCount,
		}

		// Generate JSON schema if requested
		if config.Format == "json" || config.Format == "both" {
			output.JSONSchema = generateJSONSchema(resourceType)
		}

		schemas = append(schemas, output)
	}

	// Also generate service-level aggregation views
	if len(schemas) > 0 {
		aggregationSQL := generateServiceAggregationViews(service.Name, schemas)
		if aggregationSQL != "" {
			schemas = append(schemas, &SchemaOutput{
				Service:      service.Name,
				ResourceType: "_aggregations",
				TableName:    fmt.Sprintf("aws_%s_aggregations", service.Name),
				SQL:          aggregationSQL,
				GeneratedAt:  time.Now(),
				ViewCount:    strings.Count(aggregationSQL, "CREATE OR REPLACE VIEW"),
			})
		}
	}

	return schemas, nil
}

func getGoTypeForResource(serviceName, resourceTypeName string) reflect.Type {
	// This maps service and resource names to Go types
	// In production, this would be loaded from generated mappings
	
	knownTypes := map[string]reflect.Type{
		"ec2.Instance":     reflect.TypeOf(main.EC2Instance{}),
		"s3.Bucket":        reflect.TypeOf(main.S3Bucket{}),
		"lambda.Function":  reflect.TypeOf(main.LambdaFunction{}),
	}

	key := fmt.Sprintf("%s.%s", serviceName, resourceTypeName)
	return knownTypes[key]
}

func generateStandardSchema(tableName, serviceName string, resourceType main.ResourceTypeInfo) string {
	var columns []string
	
	// Add resource type fields
	for fieldName, fieldType := range resourceType.Fields {
		columnName := toSnakeCase(fieldName)
		columnType := mapGoTypeToDuckDB(fieldType)
		nullable := "NULL"
		
		if fieldName == resourceType.PrimaryKey {
			nullable = "NOT NULL"
		}
		
		columns = append(columns, fmt.Sprintf("    %s %s %s", columnName, columnType, nullable))
	}

	// Add standard metadata columns
	columns = append(columns,
		"    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL",
		"    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL",
		"    raw_data JSON",
	)

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s", tableName, strings.Join(columns, ",\n"))

	// Add primary key if specified
	if resourceType.PrimaryKey != "" {
		sql += fmt.Sprintf(",\n    PRIMARY KEY (%s)", toSnakeCase(resourceType.PrimaryKey))
	}

	sql += "\n);\n\n"

	// Add standard indexes
	tablePrefix := strings.ReplaceAll(tableName, "aws_", "")
	sql += fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_discovered_at ON %s(discovered_at);\n", tablePrefix, tableName)
	sql += fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_last_seen ON %s(last_seen);\n", tablePrefix, tableName)
	
	// Add field-specific indexes for common patterns
	for fieldName := range resourceType.Fields {
		columnName := toSnakeCase(fieldName)
		if shouldCreateIndex(fieldName) {
			sql += fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_%s ON %s(%s);\n", 
				tablePrefix, columnName, tableName, columnName)
		}
	}

	return sql
}

func mapGoTypeToDuckDB(goType string) string {
	// Remove pointer prefix
	goType = strings.TrimPrefix(goType, "*")
	
	switch goType {
	case "string":
		return "VARCHAR"
	case "int", "int32", "int64":
		return "BIGINT"
	case "float32":
		return "REAL"
	case "float64":
		return "DOUBLE"
	case "bool":
		return "BOOLEAN"
	case "time.Time":
		return "TIMESTAMP"
	default:
		if strings.HasPrefix(goType, "[]") || strings.HasPrefix(goType, "map[") {
			return "JSON"
		}
		// Complex types stored as JSON
		return "JSON"
	}
}

func shouldCreateIndex(fieldName string) bool {
	indexableFields := []string{
		"Name", "Type", "State", "Status", "Region", "AccountId",
		"VpcId", "SubnetId", "InstanceType", "Runtime",
	}
	
	for _, field := range indexableFields {
		if fieldName == field {
			return true
		}
	}
	return false
}

func toSnakeCase(str string) string {
	var result []rune
	for i, r := range str {
		if i > 0 && 'A' <= r && r <= 'Z' {
			result = append(result, '_')
		}
		result = append(result, []rune(strings.ToLower(string(r)))[0])
	}
	return string(result)
}

func generateJSONSchema(resourceType main.ResourceTypeInfo) string {
	// Generate JSON Schema for the resource type
	schema := map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"title":   resourceType.Name,
		"properties": make(map[string]interface{}),
	}

	properties := schema["properties"].(map[string]interface{})
	
	for fieldName, fieldType := range resourceType.Fields {
		properties[toSnakeCase(fieldName)] = map[string]interface{}{
			"type": mapGoTypeToJSONSchema(fieldType),
		}
	}

	// Add metadata properties
	properties["discovered_at"] = map[string]interface{}{
		"type":   "string",
		"format": "date-time",
	}
	properties["last_seen"] = map[string]interface{}{
		"type":   "string",
		"format": "date-time",
	}
	properties["raw_data"] = map[string]interface{}{
		"type": "object",
	}

	jsonBytes, _ := json.MarshalIndent(schema, "", "  ")
	return string(jsonBytes)
}

func mapGoTypeToJSONSchema(goType string) string {
	goType = strings.TrimPrefix(goType, "*")
	
	switch goType {
	case "string":
		return "string"
	case "int", "int32", "int64":
		return "integer"
	case "float32", "float64":
		return "number"
	case "bool":
		return "boolean"
	default:
		if strings.HasPrefix(goType, "[]") {
			return "array"
		}
		return "object"
	}
}

func generateServiceAggregationViews(serviceName string, schemas []*SchemaOutput) string {
	if len(schemas) == 0 {
		return ""
	}

	var views []string

	// Service summary view
	summaryView := fmt.Sprintf(`CREATE OR REPLACE VIEW aws_%s_summary AS
SELECT 
    '%s' as service_name,
    COUNT(DISTINCT table_name) as resource_types,
    SUM(resource_count) as total_resources,
    MAX(last_updated) as last_scan
FROM (`, serviceName, serviceName)

	var unions []string
	for _, schema := range schemas {
		if schema.ResourceType != "_aggregations" {
			unions = append(unions, fmt.Sprintf(`
    SELECT '%s' as table_name, COUNT(*) as resource_count, MAX(discovered_at) as last_updated
    FROM %s`, schema.TableName, schema.TableName))
		}
	}
	
	summaryView += strings.Join(unions, "\n    UNION ALL") + "\n) t;"
	views = append(views, summaryView)

	// Cross-resource relationships view
	if serviceName == "ec2" {
		relationshipView := `CREATE OR REPLACE VIEW aws_ec2_relationships AS
SELECT 
    i.instance_id,
    i.vpc_id,
    i.subnet_id,
    v.cidr_block as vpc_cidr,
    s.cidr_block as subnet_cidr,
    sg.group_name as security_group
FROM aws_ec2_instances i
LEFT JOIN aws_ec2_vpcs v ON i.vpc_id = v.vpc_id
LEFT JOIN aws_ec2_subnets s ON i.subnet_id = s.subnet_id
LEFT JOIN aws_ec2_security_groups sg ON sg.group_id = ANY(i.security_groups);`
		views = append(views, relationshipView)
	}

	return strings.Join(views, "\n\n")
}

func writeOutput(schemas []*SchemaOutput, schemaIndex map[string][]*SchemaOutput, config *SchemaGeneratorConfig) error {
	// Write individual SQL files
	if config.Format == "sql" || config.Format == "both" {
		for _, schema := range schemas {
			filename := fmt.Sprintf("%s_%s.sql", schema.Service, strings.ToLower(schema.ResourceType))
			path := filepath.Join(config.OutputDir, filename)
			
			if err := ioutil.WriteFile(path, []byte(schema.SQL), 0644); err != nil {
				return fmt.Errorf("failed to write SQL file %s: %w", path, err)
			}
			
			if config.Verbose {
				log.Printf("Wrote SQL schema: %s", path)
			}
		}

		// Write master SQL file
		masterSQL := generateMasterSQL(schemas)
		masterPath := filepath.Join(config.OutputDir, "all_schemas.sql")
		if err := ioutil.WriteFile(masterPath, []byte(masterSQL), 0644); err != nil {
			return fmt.Errorf("failed to write master SQL file: %w", err)
		}
	}

	// Write JSON schemas
	if config.Format == "json" || config.Format == "both" {
		for _, schema := range schemas {
			if schema.JSONSchema != "" {
				filename := fmt.Sprintf("%s_%s_schema.json", schema.Service, strings.ToLower(schema.ResourceType))
				path := filepath.Join(config.OutputDir, filename)
				
				if err := ioutil.WriteFile(path, []byte(schema.JSONSchema), 0644); err != nil {
					return fmt.Errorf("failed to write JSON schema file %s: %w", path, err)
				}
			}
		}
	}

	// Write index file
	indexPath := filepath.Join(config.OutputDir, "schema_index.json")
	indexData, _ := json.MarshalIndent(schemaIndex, "", "  ")
	if err := ioutil.WriteFile(indexPath, indexData, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

func generateMasterSQL(schemas []*SchemaOutput) string {
	var parts []string
	
	parts = append(parts, "-- AWS Resource Schemas")
	parts = append(parts, fmt.Sprintf("-- Generated: %s", time.Now().Format(time.RFC3339)))
	parts = append(parts, "-- Total schemas: "+fmt.Sprintf("%d", len(schemas)))
	parts = append(parts, "")
	
	// Group by service
	serviceMap := make(map[string][]*SchemaOutput)
	for _, schema := range schemas {
		serviceMap[schema.Service] = append(serviceMap[schema.Service], schema)
	}

	for service, serviceSchemas := range serviceMap {
		parts = append(parts, fmt.Sprintf("\n-- Service: %s", strings.ToUpper(service)))
		parts = append(parts, strings.Repeat("-", 50))
		
		for _, schema := range serviceSchemas {
			parts = append(parts, fmt.Sprintf("\n-- Resource: %s", schema.ResourceType))
			parts = append(parts, schema.SQL)
		}
	}

	return strings.Join(parts, "\n")
}
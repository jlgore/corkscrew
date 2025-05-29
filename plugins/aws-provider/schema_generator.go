package main

import (
	"fmt"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// SchemaGenerator generates database schemas for AWS resources
type SchemaGenerator struct {
	// Cache for generated schemas
	schemaCache map[string]*pb.Schema
}

// NewSchemaGenerator creates a new schema generator
func NewSchemaGenerator() *SchemaGenerator {
	return &SchemaGenerator{
		schemaCache: make(map[string]*pb.Schema),
	}
}

// GenerateSchemas generates database schemas for specified services
func (sg *SchemaGenerator) GenerateSchemas(services []string) *pb.SchemaResponse {
	var schemas []*pb.Schema

	// Always include the core AWS resources table
	schemas = append(schemas, sg.generateCoreSchema())

	// Add the relationships table
	schemas = append(schemas, sg.generateRelationshipsSchema())

	// Add scan metadata table
	schemas = append(schemas, sg.generateScanMetadataSchema())

	// Add service-specific schemas if requested
	if len(services) > 0 {
		for _, service := range services {
			serviceSchemas := sg.generateServiceSchemas(service)
			schemas = append(schemas, serviceSchemas...)
		}
	}

	return &pb.SchemaResponse{
		Schemas: schemas,
	}
}

// generateCoreSchema generates the main AWS resources table schema
func (sg *SchemaGenerator) generateCoreSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_resources (
    id TEXT PRIMARY KEY,
    name TEXT,
    type TEXT NOT NULL,
    service TEXT NOT NULL,
    region TEXT,
    account_id TEXT,
    arn TEXT,
    tags JSON,
    attributes JSON,
    raw_data JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_aws_resources_type ON aws_resources(type);
CREATE INDEX IF NOT EXISTS idx_aws_resources_service ON aws_resources(service);
CREATE INDEX IF NOT EXISTS idx_aws_resources_region ON aws_resources(region);
CREATE INDEX IF NOT EXISTS idx_aws_resources_name ON aws_resources(name);
CREATE INDEX IF NOT EXISTS idx_aws_resources_account_id ON aws_resources(account_id);
CREATE INDEX IF NOT EXISTS idx_aws_resources_discovered_at ON aws_resources(discovered_at);`

	return &pb.Schema{
		Name:         "aws_resources",
		Service:      "core",
		ResourceType: "all",
		Sql:          sql,
		Description:  "Unified table for all AWS resources",
		Metadata: map[string]string{
			"provider":       "aws",
			"table_type":     "unified",
			"supports_json":  "true",
			"supports_graph": "true",
		},
	}
}

// generateRelationshipsSchema generates the relationships table schema
func (sg *SchemaGenerator) generateRelationshipsSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_relationships (
    source_id TEXT NOT NULL,
    source_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    properties JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    PRIMARY KEY (source_id, target_id, relationship_type)
);

-- Indexes for relationship queries
CREATE INDEX IF NOT EXISTS idx_aws_relationships_source ON aws_relationships(source_id);
CREATE INDEX IF NOT EXISTS idx_aws_relationships_target ON aws_relationships(target_id);
CREATE INDEX IF NOT EXISTS idx_aws_relationships_type ON aws_relationships(relationship_type);`

	return &pb.Schema{
		Name:         "aws_relationships",
		Service:      "core",
		ResourceType: "relationships",
		Sql:          sql,
		Description:  "Resource relationships and dependencies for AWS resources",
		Metadata: map[string]string{
			"provider":     "aws",
			"table_type":   "graph",
			"supports_pgq": "true",
		},
	}
}

// generateScanMetadataSchema generates the scan metadata table schema
func (sg *SchemaGenerator) generateScanMetadataSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_scan_metadata (
    scan_id TEXT PRIMARY KEY,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    services_scanned JSON,
    resource_count INTEGER,
    error_count INTEGER,
    status TEXT,
    metadata JSON,
    
    INDEX idx_scan_metadata_start_time (start_time),
    INDEX idx_scan_metadata_status (status)
);`

	return &pb.Schema{
		Name:         "aws_scan_metadata",
		Service:      "core",
		ResourceType: "metadata",
		Sql:          sql,
		Description:  "Scan operation metadata and history for AWS",
		Metadata: map[string]string{
			"provider":   "aws",
			"table_type": "metadata",
		},
	}
}

// generateServiceSchemas generates service-specific schemas
func (sg *SchemaGenerator) generateServiceSchemas(service string) []*pb.Schema {
	var schemas []*pb.Schema

	// Check cache first
	cacheKey := fmt.Sprintf("service_%s", service)
	if cached, exists := sg.schemaCache[cacheKey]; exists {
		return []*pb.Schema{cached}
	}

	// Generate service-specific schema based on known patterns
	switch service {
	case "s3":
		schemas = append(schemas, sg.generateS3Schema())
	case "ec2":
		schemas = append(schemas, sg.generateEC2Schema())
	case "lambda":
		schemas = append(schemas, sg.generateLambdaSchema())
	case "rds":
		schemas = append(schemas, sg.generateRDSSchema())
	case "dynamodb":
		schemas = append(schemas, sg.generateDynamoDBSchema())
	case "iam":
		schemas = append(schemas, sg.generateIAMSchema())
	default:
		// Generate generic service schema
		schemas = append(schemas, sg.generateGenericServiceSchema(service))
	}

	// Cache the result
	if len(schemas) > 0 {
		sg.schemaCache[cacheKey] = schemas[0]
	}

	return schemas
}

// generateS3Schema generates S3-specific schema
func (sg *SchemaGenerator) generateS3Schema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_s3_buckets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT,
    creation_date TIMESTAMP,
    versioning_status TEXT,
    encryption_enabled BOOLEAN,
    public_access_blocked BOOLEAN,
    lifecycle_configuration JSON,
    tags JSON,
    attributes JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_s3_name (name),
    INDEX idx_s3_region (region),
    INDEX idx_s3_creation_date (creation_date)
);`

	return &pb.Schema{
		Name:         "aws_s3_buckets",
		Service:      "s3",
		ResourceType: "AWS::S3::Bucket",
		Sql:          sql,
		Description:  "S3 buckets with detailed properties",
		Metadata: map[string]string{
			"provider":      "aws",
			"table_type":    "service_specific",
			"supports_json": "true",
		},
	}
}

// generateEC2Schema generates EC2-specific schema
func (sg *SchemaGenerator) generateEC2Schema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_ec2_instances (
    id TEXT PRIMARY KEY,
    name TEXT,
    instance_type TEXT,
    state TEXT,
    vpc_id TEXT,
    subnet_id TEXT,
    security_groups JSON,
    key_name TEXT,
    launch_time TIMESTAMP,
    availability_zone TEXT,
    private_ip_address TEXT,
    public_ip_address TEXT,
    tags JSON,
    attributes JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_ec2_instance_type (instance_type),
    INDEX idx_ec2_state (state),
    INDEX idx_ec2_vpc_id (vpc_id),
    INDEX idx_ec2_availability_zone (availability_zone),
    INDEX idx_ec2_launch_time (launch_time)
);`

	return &pb.Schema{
		Name:         "aws_ec2_instances",
		Service:      "ec2",
		ResourceType: "AWS::EC2::Instance",
		Sql:          sql,
		Description:  "EC2 instances with detailed properties",
		Metadata: map[string]string{
			"provider":      "aws",
			"table_type":    "service_specific",
			"supports_json": "true",
		},
	}
}

// generateLambdaSchema generates Lambda-specific schema
func (sg *SchemaGenerator) generateLambdaSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_lambda_functions (
    id TEXT PRIMARY KEY,
    function_name TEXT NOT NULL,
    runtime TEXT,
    handler TEXT,
    role TEXT,
    code_size BIGINT,
    description TEXT,
    timeout INTEGER,
    memory_size INTEGER,
    last_modified TIMESTAMP,
    version TEXT,
    vpc_config JSON,
    environment JSON,
    tags JSON,
    attributes JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_lambda_function_name (function_name),
    INDEX idx_lambda_runtime (runtime),
    INDEX idx_lambda_last_modified (last_modified)
);`

	return &pb.Schema{
		Name:         "aws_lambda_functions",
		Service:      "lambda",
		ResourceType: "AWS::Lambda::Function",
		Sql:          sql,
		Description:  "Lambda functions with detailed properties",
		Metadata: map[string]string{
			"provider":      "aws",
			"table_type":    "service_specific",
			"supports_json": "true",
		},
	}
}

// generateRDSSchema generates RDS-specific schema
func (sg *SchemaGenerator) generateRDSSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_rds_instances (
    id TEXT PRIMARY KEY,
    db_instance_identifier TEXT NOT NULL,
    db_name TEXT,
    engine TEXT,
    engine_version TEXT,
    instance_class TEXT,
    status TEXT,
    allocated_storage INTEGER,
    vpc_id TEXT,
    subnet_group TEXT,
    security_groups JSON,
    endpoint_address TEXT,
    endpoint_port INTEGER,
    creation_time TIMESTAMP,
    backup_retention_period INTEGER,
    multi_az BOOLEAN,
    publicly_accessible BOOLEAN,
    encrypted BOOLEAN,
    tags JSON,
    attributes JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_rds_identifier (db_instance_identifier),
    INDEX idx_rds_engine (engine),
    INDEX idx_rds_status (status),
    INDEX idx_rds_instance_class (instance_class)
);`

	return &pb.Schema{
		Name:         "aws_rds_instances",
		Service:      "rds",
		ResourceType: "AWS::RDS::DBInstance",
		Sql:          sql,
		Description:  "RDS database instances with detailed properties",
		Metadata: map[string]string{
			"provider":      "aws",
			"table_type":    "service_specific",
			"supports_json": "true",
		},
	}
}

// generateDynamoDBSchema generates DynamoDB-specific schema
func (sg *SchemaGenerator) generateDynamoDBSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_dynamodb_tables (
    id TEXT PRIMARY KEY,
    table_name TEXT NOT NULL,
    status TEXT,
    creation_date_time TIMESTAMP,
    item_count BIGINT,
    table_size_bytes BIGINT,
    billing_mode TEXT,
    provisioned_throughput JSON,
    global_secondary_indexes JSON,
    local_secondary_indexes JSON,
    stream_specification JSON,
    sse_description JSON,
    tags JSON,
    attributes JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_dynamodb_table_name (table_name),
    INDEX idx_dynamodb_status (status),
    INDEX idx_dynamodb_billing_mode (billing_mode)
);`

	return &pb.Schema{
		Name:         "aws_dynamodb_tables",
		Service:      "dynamodb",
		ResourceType: "AWS::DynamoDB::Table",
		Sql:          sql,
		Description:  "DynamoDB tables with detailed properties",
		Metadata: map[string]string{
			"provider":      "aws",
			"table_type":    "service_specific",
			"supports_json": "true",
		},
	}
}

// generateIAMSchema generates IAM-specific schema
func (sg *SchemaGenerator) generateIAMSchema() *pb.Schema {
	sql := `CREATE TABLE IF NOT EXISTS aws_iam_users (
    id TEXT PRIMARY KEY,
    user_name TEXT NOT NULL,
    path TEXT,
    create_date TIMESTAMP,
    password_last_used TIMESTAMP,
    mfa_enabled BOOLEAN,
    access_keys JSON,
    attached_policies JSON,
    inline_policies JSON,
    groups JSON,
    tags JSON,
    attributes JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_iam_user_name (user_name),
    INDEX idx_iam_path (path),
    INDEX idx_iam_create_date (create_date)
);`

	return &pb.Schema{
		Name:         "aws_iam_users",
		Service:      "iam",
		ResourceType: "AWS::IAM::User",
		Sql:          sql,
		Description:  "IAM users with detailed properties",
		Metadata: map[string]string{
			"provider":      "aws",
			"table_type":    "service_specific",
			"supports_json": "true",
		},
	}
}

// generateGenericServiceSchema generates a generic schema for unknown services
func (sg *SchemaGenerator) generateGenericServiceSchema(service string) *pb.Schema {
	tableName := fmt.Sprintf("aws_%s_resources", strings.ReplaceAll(service, "-", "_"))
	
	sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    id TEXT PRIMARY KEY,
    name TEXT,
    type TEXT,
    region TEXT,
    status TEXT,
    tags JSON,
    attributes JSON,
    raw_data JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_%s_name (name),
    INDEX idx_%s_type (type),
    INDEX idx_%s_region (region),
    INDEX idx_%s_status (status)
);`, tableName, strings.ReplaceAll(service, "-", "_"), strings.ReplaceAll(service, "-", "_"), strings.ReplaceAll(service, "-", "_"), strings.ReplaceAll(service, "-", "_"))

	return &pb.Schema{
		Name:         tableName,
		Service:      service,
		ResourceType: fmt.Sprintf("AWS::%s::*", strings.Title(service)),
		Sql:          sql,
		Description:  fmt.Sprintf("Generic schema for %s service resources", service),
		Metadata: map[string]string{
			"provider":      "aws",
			"table_type":    "generic",
			"supports_json": "true",
		},
	}
}

// GetCachedSchema returns a cached schema if available
func (sg *SchemaGenerator) GetCachedSchema(key string) (*pb.Schema, bool) {
	schema, exists := sg.schemaCache[key]
	return schema, exists
}

// ClearCache clears the schema cache
func (sg *SchemaGenerator) ClearCache() {
	sg.schemaCache = make(map[string]*pb.Schema)
}
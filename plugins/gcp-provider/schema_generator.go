package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// GCPSchemaGenerator generates DuckDB schemas from GCP protobuf definitions and client library types
type GCPSchemaGenerator struct {
	// Schema definitions for known resource types
	resourceSchemas map[string]*pb.Schema
	// Type analyzer for parsing Go protobuf types
	typeAnalyzer *ProtoTypeAnalyzer
	// Views for cross-resource analytics
	crossResourceViews map[string]string
}

// ProtoTypeAnalyzer analyzes Go types generated from protobuf definitions
type ProtoTypeAnalyzer struct {
	fileSet      *token.FileSet
	parsedFiles  map[string]*ast.File
	typeCache    map[string]*TypeInfo
	gcpPatterns  *GCPPatterns
}

// TypeInfo contains analyzed type information
type TypeInfo struct {
	Name         string
	Package      string
	Fields       []*FieldInfo
	IsProtoType  bool
	GCPResource  *GCPResourceInfo
}

// FieldInfo contains field analysis
type FieldInfo struct {
	Name         string
	Type         string
	JsonTag      string
	ProtoTag     string
	IsRepeated   bool
	IsMap        bool
	KeyType      string
	ValueType    string
	IsOptional   bool
	Documentation string
}

// GCPResourceInfo contains GCP-specific resource metadata
type GCPResourceInfo struct {
	ServiceName    string
	ResourceType   string
	HasProject     bool
	HasZone        bool
	HasRegion      bool
	HasLabels      bool
	HasMetadata    bool
	Hierarchical   bool
	IAMEnabled     bool
	AssetType      string
}

// GCPPatterns defines patterns for GCP resource identification
type GCPPatterns struct {
	ProjectRegex    *regexp.Regexp
	ZoneRegex       *regexp.Regexp
	RegionRegex     *regexp.Regexp
	ServiceRegex    *regexp.Regexp
	ResourceRegex   *regexp.Regexp
}

// NewGCPSchemaGenerator creates a new GCP schema generator
func NewGCPSchemaGenerator() *GCPSchemaGenerator {
	analyzer := &ProtoTypeAnalyzer{
		fileSet:     token.NewFileSet(),
		parsedFiles: make(map[string]*ast.File),
		typeCache:   make(map[string]*TypeInfo),
		gcpPatterns: initGCPPatterns(),
	}
	
	sg := &GCPSchemaGenerator{
		resourceSchemas:    make(map[string]*pb.Schema),
		typeAnalyzer:      analyzer,
		crossResourceViews: make(map[string]string),
	}
	
	// Initialize predefined schemas and views
	sg.initializeSchemas()
	sg.initializeCrossResourceViews()
	
	return sg
}

// NewSchemaGenerator creates a legacy schema generator (for compatibility)
func NewSchemaGenerator() *GCPSchemaGenerator {
	return NewGCPSchemaGenerator()
}

// initGCPPatterns initializes regex patterns for GCP resource identification
func initGCPPatterns() *GCPPatterns {
	return &GCPPatterns{
		ProjectRegex:  regexp.MustCompile(`(?i)(project|projects)[_-]?(id|name)?`),
		ZoneRegex:     regexp.MustCompile(`(?i)zone`),
		RegionRegex:   regexp.MustCompile(`(?i)region`),
		ServiceRegex:  regexp.MustCompile(`(?i)(compute|storage|container|bigquery|sql|iam|dns|monitoring)`),
		ResourceRegex: regexp.MustCompile(`(?i)(instance|disk|bucket|cluster|dataset|table|user|role|policy)`),
	}
}

// GenerateSchemas generates DuckDB schemas for the specified services
func (sg *GCPSchemaGenerator) GenerateSchemas(services []string) *pb.SchemaResponse {
	schemas := make([]*pb.Schema, 0)
	
	// If no specific services requested, return schemas for all known services
	if len(services) == 0 {
		for _, schema := range sg.resourceSchemas {
			schemas = append(schemas, schema)
		}
	} else {
		// Generate schemas for requested services
		for _, service := range services {
			serviceSchemas := sg.getSchemasForService(service)
			schemas = append(schemas, serviceSchemas...)
		}
	}
	
	// Always include the unified resources table
	schemas = append(schemas, sg.generateUnifiedResourcesSchema())
	
	// Add cross-resource views
	for viewName, viewSQL := range sg.crossResourceViews {
		schemas = append(schemas, &pb.Schema{
			Name:        viewName,
			Service:     "analytics",
			ResourceType: "view",
			Sql:         viewSQL,
			Description: "Cross-resource analytics view",
		})
	}
	
	return &pb.SchemaResponse{
		Schemas: schemas,
	}
}

// GenerateSchemaFromProtoType generates a schema from a Go protobuf type
func (sg *GCPSchemaGenerator) GenerateSchemaFromProtoType(typeName string, protoType interface{}) *pb.Schema {
	typeInfo := sg.typeAnalyzer.AnalyzeType(typeName, protoType)
	if typeInfo == nil {
		return nil
	}
	
	tableName := sg.generateTableName(typeInfo)
	columns := sg.generateColumnsFromTypeInfo(typeInfo)
	indexes := sg.generateGCPIndexes(typeInfo)
	
	sql := sg.buildCreateTableSQL(tableName, columns, indexes)
	
	return &pb.Schema{
		Name:        tableName,
		Service:     typeInfo.GCPResource.ServiceName,
		ResourceType: typeInfo.GCPResource.ResourceType,
		Sql:         sql,
		Description: fmt.Sprintf("Schema for %s generated from protobuf type", typeName),
		Metadata: map[string]string{
			"provider":      "gcp",
			"proto_type":    typeName,
			"asset_type":    typeInfo.GCPResource.AssetType,
			"hierarchical":  strconv.FormatBool(typeInfo.GCPResource.Hierarchical),
			"iam_enabled":   strconv.FormatBool(typeInfo.GCPResource.IAMEnabled),
		},
	}
}

// initializeSchemas initializes predefined schemas for known resource types
func (sg *GCPSchemaGenerator) initializeSchemas() {
	// Compute Engine Instances - Enhanced with GCP patterns
	sg.resourceSchemas["compute_instances"] = &pb.Schema{
		Name: "gcp_compute_instances",
		Service: "compute",
		ResourceType: "Instance",
		Sql: `CREATE TABLE gcp_compute_instances (
			-- Resource identifiers
			id VARCHAR PRIMARY KEY,  -- Full resource name
			name VARCHAR NOT NULL,
			self_link VARCHAR,
			
			-- GCP-specific fields
			project_id VARCHAR NOT NULL,
			zone VARCHAR NOT NULL,
			region VARCHAR GENERATED ALWAYS AS (regexp_extract(zone, '(.*)-.', 1)) STORED,
			
			-- Resource properties
			machine_type VARCHAR,
			status VARCHAR,
			creation_timestamp TIMESTAMP,
			
			-- Network configuration
			network_interfaces JSON,
			internal_ip VARCHAR GENERATED ALWAYS AS (json_extract_scalar(network_interfaces, '$[0].networkIP')) STORED,
			external_ip VARCHAR GENERATED ALWAYS AS (json_extract_scalar(network_interfaces, '$[0].accessConfigs[0].natIP')) STORED,
			
			-- Storage configuration
			disks JSON,
			boot_disk_type VARCHAR GENERATED ALWAYS AS (json_extract_scalar(disks, '$[0].type')) STORED,
			boot_disk_size_gb INTEGER GENERATED ALWAYS AS (CAST(json_extract_scalar(disks, '$[0].diskSizeGb') AS INTEGER)) STORED,
			
			-- Complex fields
			service_accounts JSON,
			scheduling JSON,
			
			-- GCP uses labels, not tags
			labels JSON,
			
			-- Metadata
			metadata JSON,
			fingerprint VARCHAR,
			discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			asset_inventory_data JSON,
			
			-- Full resource data
			raw_data JSON
		);

		-- GCP-specific indexes
		CREATE INDEX idx_project_zone ON gcp_compute_instances(project_id, zone);
		CREATE INDEX idx_machine_type ON gcp_compute_instances(machine_type);
		CREATE INDEX idx_status ON gcp_compute_instances(status);
		CREATE INDEX idx_region ON gcp_compute_instances(region);
		CREATE INDEX idx_labels_env ON gcp_compute_instances USING GIN ((labels->>'env'));
		CREATE INDEX idx_labels_app ON gcp_compute_instances USING GIN ((labels->>'app'));
		CREATE INDEX idx_creation_timestamp ON gcp_compute_instances(creation_timestamp);`,
		Description: "Google Compute Engine virtual machine instances with GCP-optimized schema",
		Metadata: map[string]string{
			"provider":      "gcp",
			"asset_type":    "compute.googleapis.com/Instance",
			"hierarchical":  "true",
			"iam_enabled":   "true",
		},
	}
	
	// Compute Engine Disks
	sg.resourceSchemas["compute_disks"] = &pb.Schema{
		Name: "gcp_compute_disks",
		Service: "compute",
		ResourceType: "Disk",
		Sql: `CREATE TABLE gcp_compute_disks (
			id VARCHAR NOT NULL,
			name VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			zone VARCHAR NOT NULL,
			size_gb BIGINT,
			type VARCHAR,
			status VARCHAR,
			source_image VARCHAR,
			source_snapshot VARCHAR,
			encrypted BOOLEAN,
			labels JSON,
			users JSON,
			discovered_at TIMESTAMP NOT NULL,
			raw_data JSON
		)`,
		Description: "Google Compute Engine persistent disks",
	}
	
	// Cloud Storage Buckets
	sg.resourceSchemas["storage_buckets"] = &pb.Schema{
		Name: "gcp_storage_buckets",
		Service: "storage",
		ResourceType: "Bucket",
		Sql: `CREATE TABLE gcp_storage_buckets (
			id VARCHAR NOT NULL,
			name VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			location VARCHAR NOT NULL,
			location_type VARCHAR,
			storage_class VARCHAR,
			versioning_enabled BOOLEAN,
			public_access_prevention VARCHAR,
			uniform_bucket_level_access BOOLEAN,
			encryption_type VARCHAR,
			kms_key_name VARCHAR,
			labels JSON,
			lifecycle_rules JSON,
			cors_configuration JSON,
			discovered_at TIMESTAMP NOT NULL,
			raw_data JSON
		)`,
		Description: "Google Cloud Storage buckets",
	}
	
	// Kubernetes Engine Clusters
	sg.resourceSchemas["container_clusters"] = &pb.Schema{
		Name: "gcp_container_clusters",
		Service: "container",
		ResourceType: "Cluster",
		Sql: `CREATE TABLE gcp_container_clusters (
			id VARCHAR NOT NULL,
			name VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			location VARCHAR NOT NULL,
			location_type VARCHAR,
			status VARCHAR,
			endpoint VARCHAR,
			master_version VARCHAR,
			node_version VARCHAR,
			current_node_count INTEGER,
			network VARCHAR,
			subnetwork VARCHAR,
			cluster_ipv4_cidr VARCHAR,
			services_ipv4_cidr VARCHAR,
			autopilot_enabled BOOLEAN,
			private_cluster BOOLEAN,
			labels JSON,
			node_pools JSON,
			addons_config JSON,
			discovered_at TIMESTAMP NOT NULL,
			raw_data JSON
		)`,
		Description: "Google Kubernetes Engine clusters",
	}
	
	// BigQuery Datasets
	sg.resourceSchemas["bigquery_datasets"] = &pb.Schema{
		Name: "gcp_bigquery_datasets",
		Service: "bigquery",
		ResourceType: "Dataset",
		Sql: `CREATE TABLE gcp_bigquery_datasets (
			id VARCHAR NOT NULL,
			dataset_id VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			location VARCHAR,
			description VARCHAR,
			creation_time TIMESTAMP,
			last_modified_time TIMESTAMP,
			default_table_expiration_ms BIGINT,
			default_partition_expiration_ms BIGINT,
			labels JSON,
			access_entries JSON,
			discovered_at TIMESTAMP NOT NULL,
			raw_data JSON
		)`,
		Description: "Google BigQuery datasets",
	}
	
	// Cloud SQL Instances
	sg.resourceSchemas["cloudsql_instances"] = &pb.Schema{
		Name: "gcp_cloudsql_instances",
		Service: "cloudsql",
		ResourceType: "Instance",
		Sql: `CREATE TABLE gcp_cloudsql_instances (
			id VARCHAR NOT NULL,
			name VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			region VARCHAR NOT NULL,
			database_version VARCHAR,
			tier VARCHAR,
			state VARCHAR,
			backend_type VARCHAR,
			instance_type VARCHAR,
			connection_name VARCHAR,
			public_ip_address VARCHAR,
			private_ip_address VARCHAR,
			maintenance_window JSON,
			backup_configuration JSON,
			database_flags JSON,
			discovered_at TIMESTAMP NOT NULL,
			raw_data JSON
		)`,
		Description: "Google Cloud SQL instances",
	}
}

// getSchemasForService returns schemas for a specific service
func (sg *GCPSchemaGenerator) getSchemasForService(service string) []*pb.Schema {
	var schemas []*pb.Schema
	
	// Map service names to schema keys
	serviceSchemaMap := map[string][]string{
		"compute": {"compute_instances", "compute_disks"},
		"storage": {"storage_buckets"},
		"container": {"container_clusters"},
		"bigquery": {"bigquery_datasets"},
		"cloudsql": {"cloudsql_instances"},
	}
	
	if schemaKeys, ok := serviceSchemaMap[service]; ok {
		for _, key := range schemaKeys {
			if schema, exists := sg.resourceSchemas[key]; exists {
				schemas = append(schemas, schema)
			}
		}
	}
	
	// If no predefined schemas, generate a generic one
	if len(schemas) == 0 {
		schemas = append(schemas, sg.generateGenericServiceSchema(service))
	}
	
	return schemas
}

// generateGenericServiceSchema generates a generic schema for unknown services
func (sg *GCPSchemaGenerator) generateGenericServiceSchema(service string) *pb.Schema {
	tableName := fmt.Sprintf("gcp_%s_resources", strings.ReplaceAll(service, "-", "_"))
	
	return &pb.Schema{
		Name: tableName,
		Service: service,
		ResourceType: "Resource",
		Sql: fmt.Sprintf(`CREATE TABLE %s (
			id VARCHAR NOT NULL,
			name VARCHAR NOT NULL,
			type VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			region VARCHAR,
			labels JSON,
			discovered_at TIMESTAMP NOT NULL,
			raw_data JSON
		)`, tableName),
		Description: fmt.Sprintf("Google Cloud %s resources", service),
	}
}

// generateUnifiedResourcesSchema generates the unified resources table schema
func (sg *GCPSchemaGenerator) generateUnifiedResourcesSchema() *pb.Schema {
	return &pb.Schema{
		Name: "gcp_resources",
		Service: "all",
		ResourceType: "Resource",
		Sql: `CREATE TABLE gcp_resources (
			id VARCHAR NOT NULL,
			name VARCHAR NOT NULL,
			type VARCHAR NOT NULL,
			service VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			region VARCHAR,
			labels JSON,
			relationships JSON,
			discovered_at TIMESTAMP NOT NULL,
			raw_data JSON
		)`,
		Description: "Unified table containing all GCP resources across services",
	}
}

// AddCustomSchema adds a custom schema definition
func (sg *GCPSchemaGenerator) AddCustomSchema(key string, schema *pb.Schema) {
	sg.resourceSchemas[key] = schema
}

// GetAvailableSchemas returns all available schema keys
func (sg *GCPSchemaGenerator) GetAvailableSchemas() []string {
	var keys []string
	for key := range sg.resourceSchemas {
		keys = append(keys, key)
	}
	return keys
}

// initializeCrossResourceViews creates analytics views for GCP resources
func (sg *GCPSchemaGenerator) initializeCrossResourceViews() {
	// GCP Compute Summary View
	sg.crossResourceViews["gcp_compute_summary"] = `
		CREATE VIEW gcp_compute_summary AS
		SELECT 
			project_id,
			zone,
			region,
			machine_type,
			status,
			COUNT(*) as instance_count,
			COUNT(DISTINCT (labels->>'app')) as app_count,
			COUNT(DISTINCT (labels->>'env')) as env_count,
			SUM(CASE WHEN external_ip IS NOT NULL THEN 1 ELSE 0 END) as public_instances,
			AVG(boot_disk_size_gb) as avg_disk_size_gb
		FROM gcp_compute_instances
		GROUP BY project_id, zone, region, machine_type, status;
	`

	// GCP Resource Distribution by Project
	sg.crossResourceViews["gcp_project_distribution"] = `
		CREATE VIEW gcp_project_distribution AS
		SELECT 
			project_id,
			service,
			type as resource_type,
			region,
			COUNT(*) as resource_count,
			COUNT(DISTINCT CASE WHEN labels IS NOT NULL THEN (labels->>'env') END) as env_count
		FROM gcp_resources
		GROUP BY project_id, service, type, region;
	`

	// GCP Cost Optimization View
	sg.crossResourceViews["gcp_cost_optimization"] = `
		CREATE VIEW gcp_cost_optimization AS
		SELECT 
			ci.project_id,
			ci.zone,
			ci.machine_type,
			ci.status,
			COUNT(*) as instance_count,
			-- Identify potentially oversized instances
			SUM(CASE WHEN ci.status = 'RUNNING' AND ci.machine_type LIKE '%large%' THEN 1 ELSE 0 END) as large_running_instances,
			-- Identify unattached disks
			(SELECT COUNT(*) FROM gcp_compute_disks cd 
			 WHERE cd.project_id = ci.project_id AND cd.zone = ci.zone 
			 AND json_array_length(cd.users) = 0) as unattached_disks,
			-- Storage waste
			SUM(ci.boot_disk_size_gb) as total_boot_disk_gb
		FROM gcp_compute_instances ci
		GROUP BY ci.project_id, ci.zone, ci.machine_type, ci.status;
	`

	// GCP Security Analysis View
	sg.crossResourceViews["gcp_security_analysis"] = `
		CREATE VIEW gcp_security_analysis AS
		SELECT 
			project_id,
			zone,
			-- Public instances without proper labels
			SUM(CASE WHEN external_ip IS NOT NULL AND (labels->>'environment' IS NULL OR labels->>'team' IS NULL) THEN 1 ELSE 0 END) as unlabeled_public_instances,
			-- Instances with default service accounts
			SUM(CASE WHEN json_extract_scalar(service_accounts, '$[0].email') LIKE '%-compute@developer.gserviceaccount.com' THEN 1 ELSE 0 END) as default_service_account_instances,
			-- Total instances
			COUNT(*) as total_instances
		FROM gcp_compute_instances
		GROUP BY project_id, zone;
	`

	// IAM Bindings View (for IAM-enabled resources)
	sg.crossResourceViews["gcp_iam_bindings"] = `
		CREATE TABLE gcp_iam_bindings (
			resource_id VARCHAR NOT NULL,
			resource_type VARCHAR NOT NULL,
			project_id VARCHAR NOT NULL,
			role VARCHAR NOT NULL,
			member VARCHAR NOT NULL,
			member_type VARCHAR GENERATED ALWAYS AS (
				CASE 
					WHEN member LIKE 'user:%' THEN 'user'
					WHEN member LIKE 'serviceAccount:%' THEN 'serviceAccount'
					WHEN member LIKE 'group:%' THEN 'group'
					WHEN member LIKE 'domain:%' THEN 'domain'
					ELSE 'unknown'
				END
			) STORED,
			condition JSON,
			discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			
			-- Indexes for IAM queries
			INDEX idx_iam_resource (resource_id),
			INDEX idx_iam_role (role),
			INDEX idx_iam_member (member),
			INDEX idx_iam_member_type (member_type),
			INDEX idx_iam_project (project_id)
		);
	`
}

// AnalyzeType analyzes a Go type and extracts field information
func (pta *ProtoTypeAnalyzer) AnalyzeType(typeName string, protoType interface{}) *TypeInfo {
	if cached, exists := pta.typeCache[typeName]; exists {
		return cached
	}

	typeInfo := &TypeInfo{
		Name:        typeName,
		Fields:      []*FieldInfo{},
		IsProtoType: pta.isProtoType(protoType),
		GCPResource: pta.analyzeGCPResource(typeName, protoType),
	}

	// Use reflection to analyze the type
	reflectType := reflect.TypeOf(protoType)
	if reflectType.Kind() == reflect.Ptr {
		reflectType = reflectType.Elem()
	}

	if reflectType.Kind() == reflect.Struct {
		for i := 0; i < reflectType.NumField(); i++ {
			field := reflectType.Field(i)
			fieldInfo := pta.analyzeField(field)
			typeInfo.Fields = append(typeInfo.Fields, fieldInfo)
		}
	}

	pta.typeCache[typeName] = typeInfo
	return typeInfo
}

// analyzeField analyzes a struct field and extracts metadata
func (pta *ProtoTypeAnalyzer) analyzeField(field reflect.StructField) *FieldInfo {
	fieldInfo := &FieldInfo{
		Name:      field.Name,
		Type:      field.Type.String(),
		JsonTag:   field.Tag.Get("json"),
		ProtoTag:  field.Tag.Get("protobuf"),
	}

	// Analyze field type
	if field.Type.Kind() == reflect.Slice {
		fieldInfo.IsRepeated = true
		fieldInfo.ValueType = field.Type.Elem().String()
	} else if field.Type.Kind() == reflect.Map {
		fieldInfo.IsMap = true
		fieldInfo.KeyType = field.Type.Key().String()
		fieldInfo.ValueType = field.Type.Elem().String()
	}

	// Check if field is optional (pointer type)
	if field.Type.Kind() == reflect.Ptr {
		fieldInfo.IsOptional = true
	}

	return fieldInfo
}

// isProtoType determines if a type is generated from protobuf
func (pta *ProtoTypeAnalyzer) isProtoType(protoType interface{}) bool {
	reflectType := reflect.TypeOf(protoType)
	if reflectType.Kind() == reflect.Ptr {
		reflectType = reflectType.Elem()
	}

	// Check if type implements proto.Message interface or has protobuf tags
	for i := 0; i < reflectType.NumField(); i++ {
		field := reflectType.Field(i)
		if field.Tag.Get("protobuf") != "" {
			return true
		}
	}
	return false
}

// analyzeGCPResource extracts GCP-specific resource information
func (pta *ProtoTypeAnalyzer) analyzeGCPResource(typeName string, protoType interface{}) *GCPResourceInfo {
	resource := &GCPResourceInfo{
		ResourceType: typeName,
	}

	// Extract service name from type name or infer from resource type
	if pta.gcpPatterns.ServiceRegex.MatchString(typeName) {
		matches := pta.gcpPatterns.ServiceRegex.FindStringSubmatch(typeName)
		if len(matches) > 1 {
			resource.ServiceName = matches[1]
		}
	} else {
		// Infer service from resource type
		resource.ServiceName = pta.inferServiceFromResourceType(typeName)
	}

	// Analyze fields for GCP patterns
	reflectType := reflect.TypeOf(protoType)
	if reflectType.Kind() == reflect.Ptr {
		reflectType = reflectType.Elem()
	}

	if reflectType.Kind() == reflect.Struct {
		for i := 0; i < reflectType.NumField(); i++ {
			field := reflectType.Field(i)
			fieldName := strings.ToLower(field.Name)

			if pta.gcpPatterns.ProjectRegex.MatchString(fieldName) {
				resource.HasProject = true
			}
			if pta.gcpPatterns.ZoneRegex.MatchString(fieldName) {
				resource.HasZone = true
			}
			if pta.gcpPatterns.RegionRegex.MatchString(fieldName) {
				resource.HasRegion = true
			}
			if fieldName == "labels" {
				resource.HasLabels = true
			}
			if fieldName == "metadata" {
				resource.HasMetadata = true
			}
		}
	}

	// Set hierarchical based on having project organization
	resource.Hierarchical = resource.HasProject && (resource.HasZone || resource.HasRegion)

	// Most GCP resources support IAM
	resource.IAMEnabled = true

	// Generate asset type
	if resource.ServiceName != "" {
		resource.AssetType = fmt.Sprintf("%s.googleapis.com/%s", resource.ServiceName, typeName)
	}

	return resource
}

// generateTableName creates a GCP table name from type info
func (sg *GCPSchemaGenerator) generateTableName(typeInfo *TypeInfo) string {
	service := "unknown"
	if typeInfo.GCPResource.ServiceName != "" {
		service = typeInfo.GCPResource.ServiceName
	}

	resourceType := strings.ToLower(typeInfo.Name)
	resourceType = strings.ReplaceAll(resourceType, ".", "_")
	
	return fmt.Sprintf("gcp_%s_%ss", service, resourceType)
}

// generateColumnsFromTypeInfo creates column definitions from type analysis
func (sg *GCPSchemaGenerator) generateColumnsFromTypeInfo(typeInfo *TypeInfo) []string {
	var columns []string

	// Standard GCP resource columns
	columns = append(columns, "id VARCHAR PRIMARY KEY")
	columns = append(columns, "name VARCHAR NOT NULL")
	columns = append(columns, "self_link VARCHAR")

	// GCP hierarchy columns
	if typeInfo.GCPResource.HasProject {
		columns = append(columns, "project_id VARCHAR NOT NULL")
	}
	if typeInfo.GCPResource.HasZone {
		columns = append(columns, "zone VARCHAR NOT NULL")
		columns = append(columns, "region VARCHAR GENERATED ALWAYS AS (regexp_extract(zone, '(.*)-.', 1)) STORED")
	} else if typeInfo.GCPResource.HasRegion {
		columns = append(columns, "region VARCHAR NOT NULL")
	}

	// Add columns based on analyzed fields
	for _, field := range typeInfo.Fields {
		if sg.shouldSkipField(field) {
			continue
		}

		columnType := sg.mapFieldTypeToSQL(field)
		columnName := sg.convertFieldNameToSQL(field.Name)

		if field.IsOptional {
			columns = append(columns, fmt.Sprintf("%s %s", columnName, columnType))
		} else {
			columns = append(columns, fmt.Sprintf("%s %s NOT NULL", columnName, columnType))
		}
	}

	// Standard GCP metadata columns
	if typeInfo.GCPResource.HasLabels {
		columns = append(columns, "labels JSON")
	}
	columns = append(columns, "creation_timestamp TIMESTAMP")
	columns = append(columns, "fingerprint VARCHAR")
	columns = append(columns, "discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP")
	columns = append(columns, "asset_inventory_data JSON")
	columns = append(columns, "raw_data JSON")

	return columns
}

// generateGCPIndexes creates GCP-optimized indexes
func (sg *GCPSchemaGenerator) generateGCPIndexes(typeInfo *TypeInfo) []string {
	var indexes []string
	tableName := sg.generateTableName(typeInfo)

	// Standard GCP indexes
	if typeInfo.GCPResource.HasProject && typeInfo.GCPResource.HasZone {
		indexes = append(indexes, fmt.Sprintf("CREATE INDEX idx_%s_project_zone ON %s(project_id, zone)", strings.ReplaceAll(tableName, "gcp_", ""), tableName))
	}
	if typeInfo.GCPResource.HasProject && typeInfo.GCPResource.HasRegion {
		indexes = append(indexes, fmt.Sprintf("CREATE INDEX idx_%s_project_region ON %s(project_id, region)", strings.ReplaceAll(tableName, "gcp_", ""), tableName))
	}
	if typeInfo.GCPResource.HasLabels {
		indexes = append(indexes, fmt.Sprintf("CREATE INDEX idx_%s_labels_env ON %s USING GIN ((labels->>'env'))", strings.ReplaceAll(tableName, "gcp_", ""), tableName))
		indexes = append(indexes, fmt.Sprintf("CREATE INDEX idx_%s_labels_app ON %s USING GIN ((labels->>'app'))", strings.ReplaceAll(tableName, "gcp_", ""), tableName))
	}

	indexes = append(indexes, fmt.Sprintf("CREATE INDEX idx_%s_creation_timestamp ON %s(creation_timestamp)", strings.ReplaceAll(tableName, "gcp_", ""), tableName))

	return indexes
}

// buildCreateTableSQL builds the complete CREATE TABLE statement
func (sg *GCPSchemaGenerator) buildCreateTableSQL(tableName string, columns []string, indexes []string) string {
	sql := fmt.Sprintf("CREATE TABLE %s (\n\t%s\n);\n\n", tableName, strings.Join(columns, ",\n\t"))
	
	for _, index := range indexes {
		sql += index + ";\n"
	}

	return sql
}

// Helper methods
func (sg *GCPSchemaGenerator) shouldSkipField(field *FieldInfo) bool {
	// Skip internal protobuf fields
	if strings.HasPrefix(field.Name, "XXX_") {
		return true
	}
	// Skip fields already handled as standard columns
	standardFields := []string{"Id", "Name", "SelfLink", "ProjectId", "Zone", "Region", "Labels", "CreationTimestamp", "Fingerprint"}
	for _, std := range standardFields {
		if field.Name == std {
			return true
		}
	}
	return false
}

func (sg *GCPSchemaGenerator) mapFieldTypeToSQL(field *FieldInfo) string {
	if field.IsRepeated || field.IsMap {
		return "JSON"
	}

	switch {
	case strings.Contains(field.Type, "string"):
		return "VARCHAR"
	case strings.Contains(field.Type, "int64"), strings.Contains(field.Type, "uint64"):
		return "BIGINT"
	case strings.Contains(field.Type, "int32"), strings.Contains(field.Type, "uint32"):
		return "INTEGER"
	case strings.Contains(field.Type, "bool"):
		return "BOOLEAN"
	case strings.Contains(field.Type, "float"), strings.Contains(field.Type, "double"):
		return "DOUBLE"
	case strings.Contains(field.Type, "time.Time"), strings.Contains(field.Type, "timestamppb"):
		return "TIMESTAMP"
	default:
		return "JSON"
	}
}

func (sg *GCPSchemaGenerator) convertFieldNameToSQL(fieldName string) string {
	// Convert CamelCase to snake_case
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	snake := re.ReplaceAllString(fieldName, "${1}_${2}")
	return strings.ToLower(snake)
}

// inferServiceFromResourceType infers the GCP service from resource type name
func (pta *ProtoTypeAnalyzer) inferServiceFromResourceType(typeName string) string {
	typeNameLower := strings.ToLower(typeName)
	
	// Common GCP resource mappings
	serviceMap := map[string]string{
		"instance":       "compute",
		"disk":          "compute", 
		"network":       "compute",
		"firewall":      "compute",
		"router":        "compute",
		"subnetwork":    "compute",
		"bucket":        "storage",
		"cluster":       "container",
		"nodepool":      "container",
		"dataset":       "bigquery",
		"table":         "bigquery",
		"job":           "bigquery",
		"function":      "cloudfunctions",
		"topic":         "pubsub",
		"subscription":  "pubsub",
		"database":      "sql",
		"user":          "iam",
		"role":          "iam",
		"policy":        "iam",
		"serviceaccount": "iam",
		"zone":          "dns",
		"recordset":     "dns",
	}
	
	// Check for direct matches
	if service, exists := serviceMap[typeNameLower]; exists {
		return service
	}
	
	// Check for partial matches
	for resourceType, service := range serviceMap {
		if strings.Contains(typeNameLower, resourceType) {
			return service
		}
	}
	
	return "unknown"
}
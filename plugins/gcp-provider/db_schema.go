package main

import (
	"fmt"
	"strings"
)

// DBSchema represents the complete database schema for GCP resources
type DBSchema struct {
	CoreTables     map[string]string
	ServiceTables  map[string]string
	Views          map[string]string
	Indexes        []string
}

// NewGCPDBSchema creates a new GCP database schema
func NewGCPDBSchema() *DBSchema {
	schema := &DBSchema{
		CoreTables:    make(map[string]string),
		ServiceTables: make(map[string]string),
		Views:         make(map[string]string),
		Indexes:       []string{},
	}
	
	// Define core tables
	schema.defineCoreSchema()
	
	// Define service-specific tables
	schema.defineServiceSchemas()
	
	// Define views
	schema.defineViews()
	
	// Define indexes
	schema.defineIndexes()
	
	return schema
}

// defineCoreSchema defines core tables used across all services
func (schema *DBSchema) defineCoreSchema() {
	// Main resources table
	schema.CoreTables["gcp_resources"] = `
CREATE TABLE IF NOT EXISTS gcp_resources (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    type VARCHAR NOT NULL,
    service VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    location VARCHAR,
    org_id VARCHAR,
    folder_id VARCHAR,
    tags JSON,
    labels JSON,
    raw_data JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    scan_id VARCHAR
);`

	// Relationships table
	schema.CoreTables["gcp_relationships"] = `
CREATE TABLE IF NOT EXISTS gcp_relationships (
    id VARCHAR PRIMARY KEY,
    source_id VARCHAR NOT NULL,
    target_id VARCHAR NOT NULL,
    relationship_type VARCHAR NOT NULL,
    properties JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (source_id) REFERENCES gcp_resources(id) ON DELETE CASCADE,
    FOREIGN KEY (target_id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// Projects table
	schema.CoreTables["gcp_projects"] = `
CREATE TABLE IF NOT EXISTS gcp_projects (
    project_id VARCHAR PRIMARY KEY,
    project_number VARCHAR UNIQUE,
    name VARCHAR NOT NULL,
    display_name VARCHAR,
    state VARCHAR,
    parent_type VARCHAR,
    parent_id VARCHAR,
    create_time TIMESTAMP,
    labels JSON,
    lifecycle_state VARCHAR
);`

	// Organizations table
	schema.CoreTables["gcp_organizations"] = `
CREATE TABLE IF NOT EXISTS gcp_organizations (
    org_id VARCHAR PRIMARY KEY,
    display_name VARCHAR,
    domain VARCHAR,
    state VARCHAR,
    create_time TIMESTAMP,
    update_time TIMESTAMP,
    owner_email VARCHAR
);`

	// Folders table
	schema.CoreTables["gcp_folders"] = `
CREATE TABLE IF NOT EXISTS gcp_folders (
    folder_id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    display_name VARCHAR,
    parent_type VARCHAR,
    parent_id VARCHAR,
    state VARCHAR,
    create_time TIMESTAMP,
    update_time TIMESTAMP
);`

	// Scan metadata table
	schema.CoreTables["gcp_scan_metadata"] = `
CREATE TABLE IF NOT EXISTS gcp_scan_metadata (
    scan_id VARCHAR PRIMARY KEY,
    started_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    resource_count INTEGER DEFAULT 0,
    status VARCHAR NOT NULL,
    scan_type VARCHAR,
    error_message VARCHAR,
    scan_scope JSON,
    duration_seconds INTEGER
);`

	// Resource changes table
	schema.CoreTables["gcp_resource_changes"] = `
CREATE TABLE IF NOT EXISTS gcp_resource_changes (
    change_id VARCHAR PRIMARY KEY,
    resource_id VARCHAR NOT NULL,
    change_type VARCHAR NOT NULL,
    change_time TIMESTAMP NOT NULL,
    previous_state JSON,
    current_state JSON,
    changed_fields JSON,
    scan_id VARCHAR,
    FOREIGN KEY (resource_id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`
}

// defineServiceSchemas defines service-specific tables
func (schema *DBSchema) defineServiceSchemas() {
	// Compute Engine
	schema.ServiceTables["gcp_compute_instances"] = `
CREATE TABLE IF NOT EXISTS gcp_compute_instances (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    machine_type VARCHAR,
    status VARCHAR,
    status_message VARCHAR,
    zone VARCHAR,
    project_id VARCHAR,
    cpu_platform VARCHAR,
    network_interfaces JSON,
    disks JSON,
    metadata JSON,
    labels JSON,
    scheduling JSON,
    service_accounts JSON,
    tags JSON,
    created_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	schema.ServiceTables["gcp_compute_disks"] = `
CREATE TABLE IF NOT EXISTS gcp_compute_disks (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    size_gb BIGINT,
    type VARCHAR,
    status VARCHAR,
    zone VARCHAR,
    project_id VARCHAR,
    source_image VARCHAR,
    source_snapshot VARCHAR,
    users JSON,
    labels JSON,
    created_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	schema.ServiceTables["gcp_compute_networks"] = `
CREATE TABLE IF NOT EXISTS gcp_compute_networks (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    description VARCHAR,
    ipv4_range VARCHAR,
    gateway_ipv4 VARCHAR,
    auto_create_subnetworks BOOLEAN,
    subnetworks JSON,
    peerings JSON,
    routing_config JSON,
    firewall_rules JSON,
    created_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	schema.ServiceTables["gcp_compute_subnetworks"] = `
CREATE TABLE IF NOT EXISTS gcp_compute_subnetworks (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    network VARCHAR,
    ip_cidr_range VARCHAR,
    gateway_address VARCHAR,
    region VARCHAR,
    private_ip_google_access BOOLEAN,
    secondary_ip_ranges JSON,
    created_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// Cloud Storage
	schema.ServiceTables["gcp_storage_buckets"] = `
CREATE TABLE IF NOT EXISTS gcp_storage_buckets (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL UNIQUE,
    location VARCHAR,
    location_type VARCHAR,
    storage_class VARCHAR,
    versioning_enabled BOOLEAN,
    lifecycle_rules JSON,
    cors JSON,
    website JSON,
    logging JSON,
    encryption JSON,
    iam_configuration JSON,
    retention_policy JSON,
    labels JSON,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// BigQuery
	schema.ServiceTables["gcp_bigquery_datasets"] = `
CREATE TABLE IF NOT EXISTS gcp_bigquery_datasets (
    id VARCHAR PRIMARY KEY,
    dataset_id VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    friendly_name VARCHAR,
    description VARCHAR,
    location VARCHAR,
    default_table_expiration_ms BIGINT,
    default_partition_expiration_ms BIGINT,
    labels JSON,
    access JSON,
    encryption_configuration JSON,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	schema.ServiceTables["gcp_bigquery_tables"] = `
CREATE TABLE IF NOT EXISTS gcp_bigquery_tables (
    id VARCHAR PRIMARY KEY,
    table_id VARCHAR NOT NULL,
    dataset_id VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    friendly_name VARCHAR,
    description VARCHAR,
    type VARCHAR,
    schema JSON,
    num_bytes BIGINT,
    num_rows BIGINT,
    partitioning JSON,
    clustering JSON,
    labels JSON,
    encryption_configuration JSON,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// Pub/Sub
	schema.ServiceTables["gcp_pubsub_topics"] = `
CREATE TABLE IF NOT EXISTS gcp_pubsub_topics (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    kms_key_name VARCHAR,
    message_retention_duration VARCHAR,
    schema_settings JSON,
    message_storage_policy JSON,
    labels JSON,
    created_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	schema.ServiceTables["gcp_pubsub_subscriptions"] = `
CREATE TABLE IF NOT EXISTS gcp_pubsub_subscriptions (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    topic VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    ack_deadline_seconds INTEGER,
    retain_acked_messages BOOLEAN,
    message_retention_duration VARCHAR,
    push_config JSON,
    bigquery_config JSON,
    dead_letter_policy JSON,
    retry_policy JSON,
    labels JSON,
    created_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// Cloud Functions
	schema.ServiceTables["gcp_cloudfunctions_functions"] = `
CREATE TABLE IF NOT EXISTS gcp_cloudfunctions_functions (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    region VARCHAR,
    entry_point VARCHAR,
    runtime VARCHAR,
    timeout VARCHAR,
    available_memory_mb INTEGER,
    max_instances INTEGER,
    min_instances INTEGER,
    trigger JSON,
    event_trigger JSON,
    https_trigger JSON,
    environment_variables JSON,
    vpc_connector VARCHAR,
    vpc_connector_egress_settings VARCHAR,
    ingress_settings VARCHAR,
    service_account_email VARCHAR,
    labels JSON,
    source_archive_url VARCHAR,
    source_repository JSON,
    status VARCHAR,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// Cloud Run
	schema.ServiceTables["gcp_cloudrun_services"] = `
CREATE TABLE IF NOT EXISTS gcp_cloudrun_services (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    region VARCHAR,
    platform VARCHAR,
    spec JSON,
    status JSON,
    traffic JSON,
    annotations JSON,
    labels JSON,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// GKE
	schema.ServiceTables["gcp_gke_clusters"] = `
CREATE TABLE IF NOT EXISTS gcp_gke_clusters (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    location VARCHAR,
    master_version VARCHAR,
    node_version VARCHAR,
    status VARCHAR,
    status_message VARCHAR,
    node_pools JSON,
    network VARCHAR,
    subnetwork VARCHAR,
    cluster_ipv4_cidr VARCHAR,
    services_ipv4_cidr VARCHAR,
    network_config JSON,
    addons_config JSON,
    master_auth JSON,
    private_cluster_config JSON,
    workload_identity_config JSON,
    labels JSON,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	// IAM
	schema.ServiceTables["gcp_iam_service_accounts"] = `
CREATE TABLE IF NOT EXISTS gcp_iam_service_accounts (
    id VARCHAR PRIMARY KEY,
    email VARCHAR NOT NULL UNIQUE,
    name VARCHAR NOT NULL,
    project_id VARCHAR NOT NULL,
    display_name VARCHAR,
    description VARCHAR,
    oauth2_client_id VARCHAR,
    unique_id VARCHAR UNIQUE,
    disabled BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`

	schema.ServiceTables["gcp_iam_roles"] = `
CREATE TABLE IF NOT EXISTS gcp_iam_roles (
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    title VARCHAR,
    description VARCHAR,
    included_permissions JSON,
    stage VARCHAR,
    deleted BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (id) REFERENCES gcp_resources(id) ON DELETE CASCADE
);`
}

// defineViews defines analytical views
func (schema *DBSchema) defineViews() {
	// Resource distribution view
	schema.Views["gcp_resource_distribution"] = `
CREATE OR REPLACE VIEW gcp_resource_distribution AS
SELECT 
    service,
    type,
    location,
    project_id,
    COUNT(*) as resource_count,
    COUNT(DISTINCT project_id) as project_count,
    COUNT(DISTINCT location) as location_count
FROM gcp_resources
GROUP BY service, type, location, project_id
ORDER BY resource_count DESC;`

	// Cross-service relationships
	schema.Views["gcp_cross_service_relationships"] = `
CREATE OR REPLACE VIEW gcp_cross_service_relationships AS
SELECT 
    r1.service as source_service,
    r1.type as source_type,
    rel.relationship_type,
    r2.service as target_service,
    r2.type as target_type,
    COUNT(*) as relationship_count
FROM gcp_relationships rel
JOIN gcp_resources r1 ON rel.source_id = r1.id
JOIN gcp_resources r2 ON rel.target_id = r2.id
WHERE r1.service != r2.service
GROUP BY r1.service, r1.type, rel.relationship_type, r2.service, r2.type;`

	// Compute instance details
	schema.Views["gcp_compute_instance_details"] = `
CREATE OR REPLACE VIEW gcp_compute_instance_details AS
SELECT 
    ci.*,
    r.project_id,
    r.labels as resource_labels,
    r.location,
    p.display_name as project_name,
    p.parent_type,
    p.parent_id
FROM gcp_compute_instances ci
JOIN gcp_resources r ON ci.id = r.id
LEFT JOIN gcp_projects p ON r.project_id = p.project_id;`

	// Storage bucket security view
	schema.Views["gcp_storage_bucket_security"] = `
CREATE OR REPLACE VIEW gcp_storage_bucket_security AS
SELECT 
    b.name as bucket_name,
    b.location,
    b.storage_class,
    b.versioning_enabled,
    b.iam_configuration->>'uniformBucketLevelAccess' as uniform_access,
    b.encryption->>'defaultKmsKeyName' as kms_key,
    b.retention_policy->>'retentionPeriod' as retention_period,
    r.project_id,
    r.labels
FROM gcp_storage_buckets b
JOIN gcp_resources r ON b.id = r.id;`

	// Resource cost estimation view
	schema.Views["gcp_resource_cost_estimation"] = `
CREATE OR REPLACE VIEW gcp_resource_cost_estimation AS
SELECT 
    r.service,
    r.type,
    r.location,
    r.project_id,
    COUNT(*) as resource_count,
    CASE 
        WHEN r.service = 'compute' AND r.type = 'Instance' THEN 'High'
        WHEN r.service = 'storage' AND r.type = 'Bucket' THEN 'Medium'
        WHEN r.service = 'bigquery' AND r.type = 'Dataset' THEN 'Variable'
        ELSE 'Low'
    END as estimated_cost_tier
FROM gcp_resources r
GROUP BY r.service, r.type, r.location, r.project_id;`

	// IAM analysis view
	schema.Views["gcp_iam_analysis"] = `
CREATE OR REPLACE VIEW gcp_iam_analysis AS
SELECT 
    sa.email as service_account,
    sa.project_id,
    COUNT(DISTINCT rel.source_id) as attached_resources,
    array_agg(DISTINCT r.service || ':' || r.type) as resource_types
FROM gcp_iam_service_accounts sa
LEFT JOIN gcp_relationships rel ON rel.target_id = sa.id
LEFT JOIN gcp_resources r ON rel.source_id = r.id
GROUP BY sa.email, sa.project_id;`

	// Network topology view
	schema.Views["gcp_network_topology"] = `
CREATE OR REPLACE VIEW gcp_network_topology AS
SELECT 
    n.name as network_name,
    n.ipv4_range,
    COUNT(DISTINCT s.id) as subnet_count,
    COUNT(DISTINCT i.id) as instance_count,
    array_agg(DISTINCT s.region) as regions
FROM gcp_compute_networks n
LEFT JOIN gcp_compute_subnetworks s ON s.network = n.id
LEFT JOIN gcp_compute_instances i ON i.network_interfaces::text LIKE '%' || n.name || '%'
GROUP BY n.name, n.ipv4_range;`
}

// defineIndexes defines database indexes
func (schema *DBSchema) defineIndexes() {
	schema.Indexes = []string{
		// Resources table indexes
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_type ON gcp_resources(type);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_service ON gcp_resources(service);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_project ON gcp_resources(project_id);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_location ON gcp_resources(location);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_scan ON gcp_resources(scan_id);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_org ON gcp_resources(org_id);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_folder ON gcp_resources(folder_id);",
		
		// Relationships table indexes
		"CREATE INDEX IF NOT EXISTS idx_gcp_relationships_source ON gcp_relationships(source_id);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_relationships_target ON gcp_relationships(target_id);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_relationships_type ON gcp_relationships(relationship_type);",
		
		// Service-specific indexes
		"CREATE INDEX IF NOT EXISTS idx_gcp_compute_instances_zone ON gcp_compute_instances(zone);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_compute_instances_status ON gcp_compute_instances(status);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_storage_buckets_location ON gcp_storage_buckets(location);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_bigquery_datasets_location ON gcp_bigquery_datasets(location);",
		
		// Change tracking indexes
		"CREATE INDEX IF NOT EXISTS idx_gcp_resource_changes_resource ON gcp_resource_changes(resource_id);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resource_changes_time ON gcp_resource_changes(change_time);",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resource_changes_type ON gcp_resource_changes(change_type);",
	}
}

// GenerateAllDDL generates all DDL statements
func (schema *DBSchema) GenerateAllDDL() []string {
	var ddlStatements []string
	
	// Add core tables
	for _, ddl := range schema.CoreTables {
		ddlStatements = append(ddlStatements, ddl)
	}
	
	// Add service tables
	for _, ddl := range schema.ServiceTables {
		ddlStatements = append(ddlStatements, ddl)
	}
	
	// Add views
	for _, ddl := range schema.Views {
		ddlStatements = append(ddlStatements, ddl)
	}
	
	// Add indexes
	ddlStatements = append(ddlStatements, schema.Indexes...)
	
	return ddlStatements
}

// GetTableName returns the table name for a service and resource type
func GetTableName(service, resourceType string) string {
	return fmt.Sprintf("gcp_%s_%s", 
		strings.ToLower(service), 
		pluralizeResourceType(strings.ToLower(resourceType)))
}

// pluralizeResourceType pluralizes a resource type name
func pluralizeResourceType(resourceType string) string {
	// Handle special cases
	specialCases := map[string]string{
		"instance":     "instances",
		"disk":         "disks",
		"network":      "networks",
		"subnetwork":   "subnetworks",
		"bucket":       "buckets",
		"dataset":      "datasets",
		"table":        "tables",
		"topic":        "topics",
		"subscription": "subscriptions",
		"function":     "functions",
		"service":      "services",
		"cluster":      "clusters",
	}
	
	if plural, exists := specialCases[resourceType]; exists {
		return plural
	}
	
	// Default pluralization
	if strings.HasSuffix(resourceType, "y") {
		return resourceType[:len(resourceType)-1] + "ies"
	}
	if strings.HasSuffix(resourceType, "s") || strings.HasSuffix(resourceType, "x") || 
	   strings.HasSuffix(resourceType, "ch") || strings.HasSuffix(resourceType, "sh") {
		return resourceType + "es"
	}
	return resourceType + "s"
}
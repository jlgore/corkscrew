package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jlgore/corkscrew/pkg/models"
)

// CrossCloudCorrelation represents a correlation between resources (local type to avoid circular import)
type CrossCloudCorrelation struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type"`
	Source          *models.Resource       `json:"source"`
	Target          *models.Resource       `json:"target"`
	ConfidenceScore float64                `json:"confidence_score"`
	Description     string                 `json:"description"`
	Properties      map[string]interface{} `json:"properties,omitempty"`
	SourceProvider  string                 `json:"source_provider"`
	TargetProvider  string                 `json:"target_provider"`
	DiscoveredAt    string                 `json:"discovered_at"`
}

// UnifiedDatabaseConfig holds configuration for the unified cloud database
type UnifiedDatabaseConfig struct {
	DatabasePath string
	DB           *sql.DB
	schemaRunner schemaExecer
}

// schemaExecer is the subset shared by *sql.DB and *sql.Tx that schema
// creation needs. Migrations install a transaction here so every DDL statement
// participates in the same commit or rollback.
type schemaExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func (c *UnifiedDatabaseConfig) schemaExec(query string, args ...interface{}) (sql.Result, error) {
	if c.schemaRunner != nil {
		return c.schemaRunner.Exec(query, args...)
	}
	return c.DB.Exec(query, args...)
}

// GetUnifiedDatabasePath returns the standardized path for the unified cloud database
// If customPath is provided, it will be used instead of the default location
func GetUnifiedDatabasePath(customPath ...string) (string, error) {
	// Use custom path if provided
	if len(customPath) > 0 && customPath[0] != "" {
		dbPath := customPath[0]

		// Create directory if it doesn't exist
		dbDir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create database directory %s: %w", dbDir, err)
		}

		return dbPath, nil
	}

	// Default behavior
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Create ~/.corkscrew/db directory if it doesn't exist
	dbDir := filepath.Join(homeDir, ".corkscrew", "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create database directory: %w", err)
	}

	return filepath.Join(dbDir, "corkscrew.duckdb"), nil
}

// InitializeUnifiedDatabase creates and initializes the unified cloud database.
// If customPath is provided, it will be used instead of the default location.
// A `quack:` URI connects to a remote Quack server instead of a local file.
func InitializeUnifiedDatabase(customPath ...string) (*UnifiedDatabaseConfig, error) {
	target := ""
	if len(customPath) > 0 {
		target = customPath[0]
	}
	return InitializeUnifiedDatabaseWithOptions(target)
}

// InitializeUnifiedDatabaseWithOptions is like InitializeUnifiedDatabase but
// accepts connection options (such as WithToken for remote Quack authentication).
func InitializeUnifiedDatabaseWithOptions(target string, opts ...Option) (*UnifiedDatabaseConfig, error) {
	dbPath := target
	if !IsRemoteTarget(target) {
		resolved, err := GetUnifiedDatabasePath(target)
		if err != nil {
			return nil, err
		}
		dbPath = resolved
	}

	db, err := OpenDuckDB(context.Background(), dbPath, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Install and load JSON extension for DuckDB
	if _, err := db.Exec("INSTALL json; LOAD json;"); err != nil {
		return nil, fmt.Errorf("failed to load JSON extension: %w", err)
	}

	if err := EnsureSchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ensure database schema: %w", err)
	}

	return &UnifiedDatabaseConfig{DatabasePath: dbPath, DB: db}, nil
}

// createUnifiedTables creates all the tables for different cloud providers
func (c *UnifiedDatabaseConfig) createUnifiedTables() error {
	// Create AWS tables
	if err := c.createAWSTable(); err != nil {
		return fmt.Errorf("failed to create AWS tables: %w", err)
	}

	// Create Azure tables
	if err := c.createAzureTables(); err != nil {
		return fmt.Errorf("failed to create Azure tables: %w", err)
	}

	// Create Kubernetes tables
	if err := c.createKubernetesTables(); err != nil {
		return fmt.Errorf("failed to create Kubernetes tables: %w", err)
	}

	// Create GCP tables
	if err := c.createGCPTables(); err != nil {
		return fmt.Errorf("failed to create GCP tables: %w", err)
	}

	if err := c.createGitHubTable(); err != nil {
		return fmt.Errorf("failed to create GitHub tables: %w", err)
	}

	if err := c.createCloudflareTable(); err != nil {
		return fmt.Errorf("failed to create Cloudflare tables: %w", err)
	}

	// Create unified relationships table
	if err := c.createUnifiedRelationshipsTable(); err != nil {
		return fmt.Errorf("failed to create relationships table: %w", err)
	}

	// Create scan metadata table
	if err := c.createScanMetadataTable(); err != nil {
		return fmt.Errorf("failed to create scan metadata table: %w", err)
	}

	// Create API action metadata table
	if err := c.createAPIActionMetadataTable(); err != nil {
		return fmt.Errorf("failed to create API action metadata table: %w", err)
	}

	// Create cross-cloud specific tables
	if err := c.createCrossCloudTables(); err != nil {
		return fmt.Errorf("failed to create cross-cloud tables: %w", err)
	}

	// Create security tables for Phase 3
	if err := c.createSecurityTables(); err != nil {
		return fmt.Errorf("failed to create security tables: %w", err)
	}

	return nil
}

// createKubernetesTables creates the Kubernetes resources table
func (c *UnifiedDatabaseConfig) createKubernetesTables() error {
	k8sTableSQL := `
CREATE TABLE IF NOT EXISTS kubernetes_resources (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Resource ID (e.g., cluster/namespace/Kind/name)
    arn VARCHAR,                               -- Not used for Kubernetes (reserved for compatibility)
    name VARCHAR NOT NULL,                     -- Resource name
    type VARCHAR NOT NULL,                     -- Resource kind (e.g., Pod, Deployment)

    -- Kubernetes-specific identifiers
    service VARCHAR,                           -- API group/version (e.g., v1, apps)
    region VARCHAR,                            -- Namespace
    account_id VARCHAR,                        -- Not used for Kubernetes (reserved for compatibility)
    parent_id VARCHAR,                         -- Parent resource ID (if any)

    -- Metadata
    tags JSON,                                 -- Labels/annotations and basic attributes
    attributes JSON,                           -- Additional attributes
    raw_data JSON,                             -- Raw Kubernetes resource JSON

    -- State information
    state VARCHAR,

    -- Timestamps
    created_at TIMESTAMP,
    modified_at TIMESTAMP,
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := c.schemaExec(k8sTableSQL); err != nil {
		return err
	}

	// Indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_k8s_type ON kubernetes_resources(type)",
		"CREATE INDEX IF NOT EXISTS idx_k8s_service ON kubernetes_resources(service)",
		"CREATE INDEX IF NOT EXISTS idx_k8s_region ON kubernetes_resources(region)",
		"CREATE INDEX IF NOT EXISTS idx_k8s_parent_id ON kubernetes_resources(parent_id)",
		"CREATE INDEX IF NOT EXISTS idx_k8s_scanned_at ON kubernetes_resources(scanned_at)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createAWSTable creates the AWS resources table
func (c *UnifiedDatabaseConfig) createAWSTable() error {
	awsTableSQL := `
CREATE TABLE IF NOT EXISTS aws_resources (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- AWS Resource ID/ARN
    arn VARCHAR,                               -- AWS ARN (unique identifier)
    name VARCHAR NOT NULL,                     -- Resource name
    type VARCHAR NOT NULL,                     -- Resource type (e.g., AWS::S3::Bucket)
    
    -- AWS-specific identifiers
    service VARCHAR,                           -- AWS service (e.g., s3, ec2)
    region VARCHAR,                            -- AWS region
    account_id VARCHAR,                        -- AWS account ID
    
    -- Hierarchy and relationships
    parent_id VARCHAR,                         -- Parent resource ID
    
    -- Metadata
    tags JSON,                                 -- Resource tags
    attributes JSON,                           -- AWS-specific attributes
    raw_data JSON,                             -- Complete raw resource data
    
    -- State information
    state VARCHAR,                             -- Resource state
    
    -- Timestamps
    created_at TIMESTAMP,                      -- Resource creation time
    modified_at TIMESTAMP,                     -- Last modification time
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP  -- When we discovered this resource
);`

	if _, err := c.schemaExec(awsTableSQL); err != nil {
		return err
	}

	// Create indexes separately
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_aws_type ON aws_resources(type)",
		"CREATE INDEX IF NOT EXISTS idx_aws_service ON aws_resources(service)",
		"CREATE INDEX IF NOT EXISTS idx_aws_region ON aws_resources(region)",
		"CREATE INDEX IF NOT EXISTS idx_aws_account_id ON aws_resources(account_id)",
		"CREATE INDEX IF NOT EXISTS idx_aws_parent_id ON aws_resources(parent_id)",
		"CREATE INDEX IF NOT EXISTS idx_aws_scanned_at ON aws_resources(scanned_at)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createAzureTables creates the Azure resources table
func (c *UnifiedDatabaseConfig) createAzureTables() error {
	azureTableSQL := `
CREATE TABLE IF NOT EXISTS azure_resources (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Azure Resource ID (full path)
    name VARCHAR NOT NULL,                     -- Resource name
    type VARCHAR NOT NULL,                     -- Resource type (e.g., Microsoft.Storage/storageAccounts)
    
    -- Azure-specific identifiers
    resource_id VARCHAR,                       -- Short resource ID
    subscription_id VARCHAR NOT NULL,          -- Azure subscription ID
    resource_group VARCHAR NOT NULL,           -- Resource group name
    
    -- Location and hierarchy
    location VARCHAR NOT NULL,                 -- Azure region (e.g., centralus)
    parent_id VARCHAR,                         -- Parent resource ID for hierarchical resources
    managed_by VARCHAR,                        -- ID of resource managing this resource
    
    -- Service information
    service VARCHAR,                           -- Service name (e.g., storage, compute)
    kind VARCHAR,                              -- Resource kind (e.g., StorageV2)
    
    -- SKU information
    sku_name VARCHAR,                          -- SKU name (e.g., Standard_LRS)
    sku_tier VARCHAR,                          -- SKU tier (e.g., Standard)
    sku_size VARCHAR,                          -- SKU size
    sku_family VARCHAR,                        -- SKU family
    sku_capacity INTEGER,                      -- SKU capacity
    
    -- Metadata
    tags JSON,                                 -- Resource tags
    properties JSON,                           -- Resource-specific properties
    raw_data JSON,                             -- Complete raw resource data
    
    -- State information
    provisioning_state VARCHAR,                -- Current provisioning state
    power_state VARCHAR,                       -- Power state (for VMs)
    
    -- Timestamps
    created_time TIMESTAMP,                    -- Resource creation time
    changed_time TIMESTAMP,                    -- Last modification time
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,  -- When we discovered this resource
    
    -- Additional metadata
    etag VARCHAR,                              -- Entity tag for optimistic concurrency
    api_version VARCHAR,                       -- API version used to fetch this resource
    
);`

	if _, err := c.schemaExec(azureTableSQL); err != nil {
		return err
	}

	// Create indexes separately
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_azure_type ON azure_resources(type)",
		"CREATE INDEX IF NOT EXISTS idx_azure_service ON azure_resources(service)",
		"CREATE INDEX IF NOT EXISTS idx_azure_resource_group ON azure_resources(resource_group)",
		"CREATE INDEX IF NOT EXISTS idx_azure_location ON azure_resources(location)",
		"CREATE INDEX IF NOT EXISTS idx_azure_subscription_id ON azure_resources(subscription_id)",
		"CREATE INDEX IF NOT EXISTS idx_azure_parent_id ON azure_resources(parent_id)",
		"CREATE INDEX IF NOT EXISTS idx_azure_provisioning_state ON azure_resources(provisioning_state)",
		"CREATE INDEX IF NOT EXISTS idx_azure_scanned_at ON azure_resources(scanned_at)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createGCPTables creates the GCP resources table
func (c *UnifiedDatabaseConfig) createGCPTables() error {
	gcpTableSQL := `
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

	if _, err := c.schemaExec(gcpTableSQL); err != nil {
		return err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_type ON gcp_resources(type)",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_service ON gcp_resources(service)",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_project ON gcp_resources(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_location ON gcp_resources(location)",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_scan ON gcp_resources(scan_id)",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_org ON gcp_resources(org_id)",
		"CREATE INDEX IF NOT EXISTS idx_gcp_resources_folder ON gcp_resources(folder_id)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

func (c *UnifiedDatabaseConfig) createGitHubTable() error {
	_, err := c.schemaExec(`
CREATE TABLE IF NOT EXISTS github_resources (
    id VARCHAR PRIMARY KEY,
    org VARCHAR,
    name VARCHAR NOT NULL,
    type VARCHAR NOT NULL,
    service VARCHAR,
    region VARCHAR,
    account_id VARCHAR,
    arn VARCHAR,
    parent_id VARCHAR,
    tags JSON,
    attributes JSON,
    raw_data JSON,
    state VARCHAR DEFAULT 'active',
    created_at TIMESTAMP,
    modified_at TIMESTAMP,
    discovered_at TIMESTAMP,
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`)
	return err
}

func (c *UnifiedDatabaseConfig) createCloudflareTable() error {
	_, err := c.schemaExec(`
CREATE TABLE IF NOT EXISTS cloudflare_resources (
    id VARCHAR PRIMARY KEY,
    provider VARCHAR DEFAULT 'cloudflare',
    service VARCHAR,
    type VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    region VARCHAR,
    account_id VARCHAR,
    parent_id VARCHAR,
    arn VARCHAR,
    tags JSON,
    relationships JSON,
    raw_data JSON,
    attributes JSON,
    state VARCHAR DEFAULT 'active',
    created_at TIMESTAMP,
    modified_at TIMESTAMP,
    discovered_at TIMESTAMP,
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`)
	return err
}

// createUnifiedRelationshipsTable creates a unified relationships table for all cloud providers
func (c *UnifiedDatabaseConfig) createUnifiedRelationshipsTable() error {
	relationshipsSQL := `
CREATE TABLE IF NOT EXISTS cloud_relationships (
    -- Relationship identifiers
    from_id VARCHAR NOT NULL,                  -- Source resource ID
    to_id VARCHAR NOT NULL,                    -- Target resource ID
    relationship_type VARCHAR NOT NULL,        -- Type of relationship
    
    -- Cloud provider context
    provider VARCHAR NOT NULL,                 -- Cloud provider (aws, azure, gcp, etc.)
    
    -- Relationship metadata
    relationship_subtype VARCHAR,              -- More specific relationship type
    properties JSON,                           -- Additional relationship properties
    
    -- Resource type context
    from_resource_type VARCHAR,                -- Source resource type
    to_resource_type VARCHAR,                  -- Target resource type
    direction VARCHAR DEFAULT 'outbound',      -- Relationship direction
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    PRIMARY KEY (from_id, to_id, relationship_type, provider)
);`

	if _, err := c.schemaExec(relationshipsSQL); err != nil {
		return err
	}

	// Create indexes separately
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_rel_from_id ON cloud_relationships(from_id)",
		"CREATE INDEX IF NOT EXISTS idx_rel_to_id ON cloud_relationships(to_id)",
		"CREATE INDEX IF NOT EXISTS idx_rel_type ON cloud_relationships(relationship_type)",
		"CREATE INDEX IF NOT EXISTS idx_rel_provider ON cloud_relationships(provider)",
		"CREATE INDEX IF NOT EXISTS idx_rel_from_type ON cloud_relationships(from_resource_type)",
		"CREATE INDEX IF NOT EXISTS idx_rel_to_type ON cloud_relationships(to_resource_type)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createScanMetadataTable creates a unified scan metadata table
func (c *UnifiedDatabaseConfig) createScanMetadataTable() error {
	scanMetadataSQL := `
CREATE TABLE IF NOT EXISTS scan_metadata (
    -- Scan identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique scan ID
    provider VARCHAR NOT NULL,                 -- Cloud provider (aws, azure, etc.)
    scan_type VARCHAR NOT NULL,                -- Type of scan (full, incremental, service)
    
    -- Scan scope
    services JSON,                             -- List of services scanned
    regions JSON,                              -- Regions scanned
    accounts JSON,                             -- Accounts/subscriptions scanned
    
    -- Scan results
    total_resources INTEGER DEFAULT 0,         -- Total resources found
    new_resources INTEGER DEFAULT 0,           -- New resources discovered
    updated_resources INTEGER DEFAULT 0,       -- Updated resources
    deleted_resources INTEGER DEFAULT 0,       -- Resources no longer found
    failed_resources INTEGER DEFAULT 0,        -- Resources that failed to scan
    
    -- Performance metrics
    scan_start_time TIMESTAMP NOT NULL,        -- When scan started
    scan_end_time TIMESTAMP,                   -- When scan completed
    duration_ms BIGINT,                        -- Total duration in milliseconds
    
    -- Scan metadata
    initiated_by VARCHAR,                      -- User or system that initiated scan
    scan_reason VARCHAR,                       -- Reason for scan
    error_messages JSON,                       -- Any errors encountered
    warnings JSON,                             -- Any warnings
    metadata JSON,                             -- Additional scan metadata
    
    -- Status
    status VARCHAR DEFAULT 'running'           -- Scan status (running, completed, failed)
);`

	if _, err := c.schemaExec(scanMetadataSQL); err != nil {
		return err
	}

	// Create indexes separately
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_scan_provider ON scan_metadata(provider)",
		"CREATE INDEX IF NOT EXISTS idx_scan_type ON scan_metadata(scan_type)",
		"CREATE INDEX IF NOT EXISTS idx_scan_start_time ON scan_metadata(scan_start_time)",
		"CREATE INDEX IF NOT EXISTS idx_scan_status ON scan_metadata(status)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createAPIActionMetadataTable creates a unified API action metadata table
func (c *UnifiedDatabaseConfig) createAPIActionMetadataTable() error {
	apiMetadataSQL := `
CREATE TABLE IF NOT EXISTS api_action_metadata (
    -- Action identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique action ID
    provider VARCHAR NOT NULL,                 -- Cloud provider (aws, azure, etc.)
    correlation_id VARCHAR,                    -- Correlation ID for request tracking
    
    -- API details
    service VARCHAR NOT NULL,                  -- Cloud service (e.g., s3, storage)
    operation_name VARCHAR NOT NULL,           -- API operation name
    operation_type VARCHAR,                    -- Operation type (List, Get, etc.)
    api_version VARCHAR,                       -- API version used
    
    -- Execution details
    execution_time TIMESTAMP NOT NULL,         -- When the API call was made
    region VARCHAR,                            -- Cloud region
    account_id VARCHAR,                        -- Account/subscription ID
    
    -- Results
    success BOOLEAN NOT NULL,                  -- Whether the operation succeeded
    status_code INTEGER,                       -- HTTP status code
    duration_ms BIGINT,                        -- Duration in milliseconds
    resource_count INTEGER DEFAULT 0,          -- Number of resources returned
    
    -- Request details
    request_method VARCHAR,                    -- HTTP method (GET, POST, etc.)
    request_path VARCHAR,                      -- API path
    request_headers JSON,                      -- Request headers (sanitized)
    request_body_size INTEGER,                 -- Size of request body
    
    -- Response details
    response_headers JSON,                     -- Response headers (sanitized)
    response_body_size INTEGER,                -- Size of response body
    
    -- Error information
    error_code VARCHAR,                        -- Cloud provider error code
    error_message VARCHAR,                     -- Error message
    error_details JSON,                        -- Detailed error information
    
    -- Rate limiting
    rate_limit_remaining INTEGER,              -- Remaining API calls
    rate_limit_reset_time TIMESTAMP,           -- When rate limit resets
    
    -- Additional metadata
    client_request_id VARCHAR,                 -- Client request ID
    request_charge DOUBLE,                     -- Request units consumed
    metadata JSON,                             -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := c.schemaExec(apiMetadataSQL); err != nil {
		return err
	}

	// Create indexes separately
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_api_provider ON api_action_metadata(provider)",
		"CREATE INDEX IF NOT EXISTS idx_api_service ON api_action_metadata(service)",
		"CREATE INDEX IF NOT EXISTS idx_api_operation_name ON api_action_metadata(operation_name)",
		"CREATE INDEX IF NOT EXISTS idx_api_execution_time ON api_action_metadata(execution_time)",
		"CREATE INDEX IF NOT EXISTS idx_api_success ON api_action_metadata(success)",
		"CREATE INDEX IF NOT EXISTS idx_api_account_id ON api_action_metadata(account_id)",
		"CREATE INDEX IF NOT EXISTS idx_api_correlation_id ON api_action_metadata(correlation_id)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// GetDB returns the database connection
func (c *UnifiedDatabaseConfig) GetDB() *sql.DB {
	return c.DB
}

// Close closes the database connection
func (c *UnifiedDatabaseConfig) Close() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}

// CreateCrossCloudViews creates views for cross-cloud resource queries
func (c *UnifiedDatabaseConfig) CreateCrossCloudViews() error {
	// Create a unified view of all cloud resources
	unifiedResourcesView := `
CREATE OR REPLACE VIEW all_cloud_resources AS
SELECT 'aws' AS provider, id, name, type, arn AS resource_identifier,
       service, region AS location, account_id, parent_id, tags, raw_data, scanned_at
FROM aws_resources
UNION ALL
SELECT 'azure', id, name, type, arn, service, region, account_id, parent_id, tags, raw_data, scanned_at
FROM azure_resources
UNION ALL
SELECT 'gcp', id, name, type, arn, service, region, account_id, NULL, tags, raw_data, scanned_at
FROM gcp_resources
UNION ALL
SELECT 'kubernetes', id, name, type, arn, service, region, account_id, parent_id, tags, raw_data, scanned_at
FROM kubernetes_resources
UNION ALL
SELECT 'github', id, name, type, arn, service, region, account_id, parent_id, tags, raw_data, scanned_at
FROM github_resources
UNION ALL
SELECT 'cloudflare', id, name, type, arn, service, region, account_id, parent_id, tags, raw_data, scanned_at
FROM cloudflare_resources;`

	if _, err := c.schemaExec(unifiedResourcesView); err != nil {
		return fmt.Errorf("failed to create unified resources view: %w", err)
	}

	// Create a view for resource counts by provider
	resourceCountsView := `
CREATE OR REPLACE VIEW resource_counts_by_provider AS
SELECT 
    provider,
    COUNT(*) as total_resources,
    COUNT(DISTINCT service) as unique_services,
    COUNT(DISTINCT location) as unique_locations,
    COUNT(DISTINCT account_id) as unique_accounts,
    MIN(scanned_at) as first_scan,
    MAX(scanned_at) as last_scan
FROM all_cloud_resources
GROUP BY provider;`

	if _, err := c.schemaExec(resourceCountsView); err != nil {
		return fmt.Errorf("failed to create resource counts view: %w", err)
	}

	return nil
}

// createCrossCloudTables creates tables for cross-cloud correlation and analysis
func (c *UnifiedDatabaseConfig) createCrossCloudTables() error {
	// Create IP address correlation table
	if err := c.createIPAddressTable(); err != nil {
		return fmt.Errorf("failed to create IP address table: %w", err)
	}

	// Create DNS correlation table
	if err := c.createDNSTable(); err != nil {
		return fmt.Errorf("failed to create DNS table: %w", err)
	}

	// Create cross-cloud correlation table
	if err := c.createCrossCloudCorrelationTable(); err != nil {
		return fmt.Errorf("failed to create cross-cloud correlation table: %w", err)
	}

	// Create network topology table
	if err := c.createNetworkTopologyTable(); err != nil {
		return fmt.Errorf("failed to create network topology table: %w", err)
	}

	return nil
}

// createIPAddressTable creates a table for IP address correlation across clouds
func (c *UnifiedDatabaseConfig) createIPAddressTable() error {
	ipAddressSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_ip_addresses (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique IP address record ID
    ip_address VARCHAR NOT NULL,               -- The IP address
    
    -- IP address metadata
    ip_version VARCHAR NOT NULL,               -- ipv4 or ipv6
    ip_type VARCHAR NOT NULL,                  -- public, private, elastic, reserved
    ip_scope VARCHAR,                          -- global, regional, local
    
    -- Resource association
    resource_id VARCHAR NOT NULL,              -- Associated resource ID
    resource_type VARCHAR NOT NULL,            -- Resource type
    resource_name VARCHAR,                     -- Resource name
    provider VARCHAR NOT NULL,                 -- Cloud provider
    region VARCHAR NOT NULL,                   -- Region
    account_id VARCHAR,                        -- Account/subscription ID
    
    -- Network context
    vpc_id VARCHAR,                            -- VPC/VNet ID
    subnet_id VARCHAR,                         -- Subnet ID
    network_interface_id VARCHAR,              -- Network interface ID
    
    -- Additional metadata
    allocation_id VARCHAR,                     -- Allocation ID (for elastic IPs)
    domain VARCHAR,                            -- Domain (vpc, standard, etc.)
    tags JSON,                                 -- Tags
    metadata JSON,                             -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP,                      -- When IP was allocated
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := c.schemaExec(ipAddressSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_ip_address ON cross_cloud_ip_addresses(ip_address)",
		"CREATE INDEX IF NOT EXISTS idx_ip_resource_id ON cross_cloud_ip_addresses(resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_ip_provider ON cross_cloud_ip_addresses(provider)",
		"CREATE INDEX IF NOT EXISTS idx_ip_region ON cross_cloud_ip_addresses(region)",
		"CREATE INDEX IF NOT EXISTS idx_ip_type ON cross_cloud_ip_addresses(ip_type)",
		"CREATE INDEX IF NOT EXISTS idx_ip_vpc ON cross_cloud_ip_addresses(vpc_id)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createDNSTable creates a table for DNS correlation across clouds
func (c *UnifiedDatabaseConfig) createDNSTable() error {
	dnsSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_dns_records (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique DNS record ID
    dns_name VARCHAR NOT NULL,                 -- DNS name
    
    -- DNS record details
    record_type VARCHAR NOT NULL,              -- A, AAAA, CNAME, MX, etc.
    record_values JSON NOT NULL,               -- Array of record values
    ttl INTEGER,                               -- Time to live
    
    -- Resource association
    resource_id VARCHAR,                       -- Associated resource ID (if any)
    resource_type VARCHAR,                     -- Resource type
    resource_name VARCHAR,                     -- Resource name
    provider VARCHAR NOT NULL,                 -- Cloud provider
    region VARCHAR,                            -- Region (if applicable)
    account_id VARCHAR,                        -- Account/subscription ID
    
    -- DNS service metadata
    dns_service VARCHAR,                       -- DNS service (Route53, Azure DNS, etc.)
    zone_id VARCHAR,                           -- DNS zone ID
    zone_name VARCHAR,                         -- DNS zone name
    
    -- Health check and routing
    health_check_id VARCHAR,                   -- Health check ID (if any)
    routing_policy VARCHAR,                    -- Routing policy type
    routing_policy_config JSON,                -- Routing policy configuration
    
    -- Additional metadata
    tags JSON,                                 -- Tags
    metadata JSON,                             -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP,                      -- When DNS record was created
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := c.schemaExec(dnsSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_dns_name ON cross_cloud_dns_records(dns_name)",
		"CREATE INDEX IF NOT EXISTS idx_dns_type ON cross_cloud_dns_records(record_type)",
		"CREATE INDEX IF NOT EXISTS idx_dns_resource_id ON cross_cloud_dns_records(resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_dns_provider ON cross_cloud_dns_records(provider)",
		"CREATE INDEX IF NOT EXISTS idx_dns_zone ON cross_cloud_dns_records(zone_id)",
		"CREATE INDEX IF NOT EXISTS idx_dns_service ON cross_cloud_dns_records(dns_service)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createCrossCloudCorrelationTable creates a table for tracking cross-cloud relationships
func (c *UnifiedDatabaseConfig) createCrossCloudCorrelationTable() error {
	correlationSQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_correlations (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique correlation ID
    
    -- Source resource
    source_resource_id VARCHAR NOT NULL,       -- Source resource ID
    source_provider VARCHAR NOT NULL,          -- Source provider
    source_region VARCHAR,                     -- Source region
    source_account_id VARCHAR,                 -- Source account
    source_resource_type VARCHAR,              -- Source resource type
    
    -- Target resource
    target_resource_id VARCHAR NOT NULL,       -- Target resource ID
    target_provider VARCHAR NOT NULL,          -- Target provider
    target_region VARCHAR,                     -- Target region
    target_account_id VARCHAR,                 -- Target account
    target_resource_type VARCHAR,              -- Target resource type
    
    -- Correlation details
    correlation_type VARCHAR NOT NULL,         -- Type of correlation
    correlation_subtype VARCHAR,               -- Subtype of correlation
    correlation_method VARCHAR NOT NULL,       -- How correlation was discovered
    confidence_score DOUBLE NOT NULL,          -- Confidence in correlation (0-1)
    
    -- Correlation evidence
    evidence JSON,                             -- Evidence supporting correlation
    matching_attributes JSON,                  -- Attributes that match
    
    -- Metadata
    description VARCHAR,                       -- Human-readable description
    tags JSON,                                 -- Tags
    metadata JSON,                             -- Additional metadata
    
    -- Status
    status VARCHAR DEFAULT 'active',           -- active, inactive, pending_verification
    verified BOOLEAN DEFAULT FALSE,            -- Whether correlation is verified
    verification_method VARCHAR,               -- How it was verified
    
    -- Timestamps
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_verified_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := c.schemaExec(correlationSQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_cc_source_resource ON cross_cloud_correlations(source_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_cc_target_resource ON cross_cloud_correlations(target_resource_id)",
		"CREATE INDEX IF NOT EXISTS idx_cc_source_provider ON cross_cloud_correlations(source_provider)",
		"CREATE INDEX IF NOT EXISTS idx_cc_target_provider ON cross_cloud_correlations(target_provider)",
		"CREATE INDEX IF NOT EXISTS idx_cc_correlation_type ON cross_cloud_correlations(correlation_type)",
		"CREATE INDEX IF NOT EXISTS idx_cc_confidence ON cross_cloud_correlations(confidence_score)",
		"CREATE INDEX IF NOT EXISTS idx_cc_status ON cross_cloud_correlations(status)",
		"CREATE INDEX IF NOT EXISTS idx_cc_providers ON cross_cloud_correlations(source_provider, target_provider)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createNetworkTopologyTable creates a table for network topology mapping
func (c *UnifiedDatabaseConfig) createNetworkTopologyTable() error {
	topologySQL := `
CREATE TABLE IF NOT EXISTS cross_cloud_network_topology (
    -- Primary identifiers
    id VARCHAR PRIMARY KEY,                    -- Unique topology record ID
    
    -- Network connection details
    connection_type VARCHAR NOT NULL,          -- vpn, peering, direct_connect, etc.
    connection_id VARCHAR NOT NULL,            -- Connection resource ID
    connection_name VARCHAR,                   -- Connection name
    
    -- Source network
    source_network_id VARCHAR NOT NULL,       -- Source network ID (VPC/VNet)
    source_network_name VARCHAR,              -- Source network name
    source_provider VARCHAR NOT NULL,         -- Source provider
    source_region VARCHAR NOT NULL,           -- Source region
    source_account_id VARCHAR,                -- Source account
    source_cidr_blocks JSON,                  -- Source CIDR blocks
    
    -- Target network
    target_network_id VARCHAR NOT NULL,       -- Target network ID
    target_network_name VARCHAR,              -- Target network name
    target_provider VARCHAR NOT NULL,         -- Target provider
    target_region VARCHAR NOT NULL,           -- Target region
    target_account_id VARCHAR,                -- Target account
    target_cidr_blocks JSON,                  -- Target CIDR blocks
    
    -- Connection properties
    status VARCHAR NOT NULL,                   -- Connection status
    bandwidth VARCHAR,                         -- Bandwidth
    encryption BOOLEAN,                        -- Is encrypted
    redundancy VARCHAR,                        -- Redundancy level
    
    -- Routing information
    routing_tables JSON,                       -- Associated routing tables
    route_propagation BOOLEAN,                 -- Route propagation enabled
    
    -- Gateway information
    source_gateway_id VARCHAR,                 -- Source gateway ID
    target_gateway_id VARCHAR,                 -- Target gateway ID
    
    -- Additional metadata
    tags JSON,                                 -- Tags
    metadata JSON,                             -- Additional metadata
    
    -- Timestamps
    created_at TIMESTAMP,                      -- When connection was created
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	if _, err := c.schemaExec(topologySQL); err != nil {
		return err
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_topo_connection_type ON cross_cloud_network_topology(connection_type)",
		"CREATE INDEX IF NOT EXISTS idx_topo_connection_id ON cross_cloud_network_topology(connection_id)",
		"CREATE INDEX IF NOT EXISTS idx_topo_source_network ON cross_cloud_network_topology(source_network_id)",
		"CREATE INDEX IF NOT EXISTS idx_topo_target_network ON cross_cloud_network_topology(target_network_id)",
		"CREATE INDEX IF NOT EXISTS idx_topo_source_provider ON cross_cloud_network_topology(source_provider)",
		"CREATE INDEX IF NOT EXISTS idx_topo_target_provider ON cross_cloud_network_topology(target_provider)",
		"CREATE INDEX IF NOT EXISTS idx_topo_status ON cross_cloud_network_topology(status)",
		"CREATE INDEX IF NOT EXISTS idx_topo_providers ON cross_cloud_network_topology(source_provider, target_provider)",
	}

	for _, idx := range indexes {
		if _, err := c.schemaExec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// QueryContext executes a query that returns rows, typically a SELECT
func (c *UnifiedDatabaseConfig) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return c.DB.QueryContext(ctx, query, args...)
}

// BeginTx starts a transaction
func (c *UnifiedDatabaseConfig) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.DB.BeginTx(ctx, opts)
}

func (c *UnifiedDatabaseConfig) graphStore() *GraphStore {
	return NewGraphStore(c.DB)
}

// StoreResources stores resource data
func (c *UnifiedDatabaseConfig) StoreResources(resources []*models.Resource) error {
	return c.graphStore().StoreResources(resources)
}

// StoreIPAddresses stores IP address data
func (c *UnifiedDatabaseConfig) StoreIPAddresses(addresses []*models.IPAddress) error {
	return c.graphStore().StoreIPAddresses(addresses)
}

// StoreDNSRecords stores DNS record data
func (c *UnifiedDatabaseConfig) StoreDNSRecords(records []*models.DNSRecord) error {
	return c.graphStore().StoreDNSRecords(records)
}

// StoreCorrelations stores correlation data
func (c *UnifiedDatabaseConfig) StoreCorrelations(correlations interface{}) error {
	return c.graphStore().StoreCorrelations(correlations)
}

// GetResourcesByProvider retrieves resources by provider
func (c *UnifiedDatabaseConfig) GetResourcesByProvider(provider string) ([]*models.Resource, error) {
	return c.graphStore().GetResourcesByProvider(provider)
}

// GetIPAddressesByProvider retrieves IP addresses by provider
func (c *UnifiedDatabaseConfig) GetIPAddressesByProvider(provider string) ([]*models.IPAddress, error) {
	return c.graphStore().GetIPAddressesByProvider(provider)
}

// GetDNSRecordsByProvider retrieves DNS records by provider
func (c *UnifiedDatabaseConfig) GetDNSRecordsByProvider(provider string) ([]*models.DNSRecord, error) {
	return c.graphStore().GetDNSRecordsByProvider(provider)
}

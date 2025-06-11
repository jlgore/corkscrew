package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// UnifiedDatabaseConfig holds configuration for the unified cloud database
type UnifiedDatabaseConfig struct {
	DatabasePath string
	DB           *sql.DB
}

// GetUnifiedDatabasePath returns the standardized path for the unified cloud database
func GetUnifiedDatabasePath() (string, error) {
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

// InitializeUnifiedDatabase creates and initializes the unified cloud database
func InitializeUnifiedDatabase() (*UnifiedDatabaseConfig, error) {
	dbPath, err := GetUnifiedDatabasePath()
	if err != nil {
		return nil, err
	}
	
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	
	// Install and load JSON extension for DuckDB
	if _, err := db.Exec("INSTALL json; LOAD json;"); err != nil {
		return nil, fmt.Errorf("failed to load JSON extension: %w", err)
	}
	
	config := &UnifiedDatabaseConfig{
		DatabasePath: dbPath,
		DB:           db,
	}
	
	// Initialize all cloud provider tables
	if err := config.createUnifiedTables(); err != nil {
		return nil, fmt.Errorf("failed to create unified tables: %w", err)
	}
	
	return config, nil
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

	if _, err := c.DB.Exec(awsTableSQL); err != nil {
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
		if _, err := c.DB.Exec(idx); err != nil {
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

	if _, err := c.DB.Exec(azureTableSQL); err != nil {
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
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}
	
	return nil
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

	if _, err := c.DB.Exec(relationshipsSQL); err != nil {
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
		if _, err := c.DB.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}
	
	return nil
}

// createScanMetadataTable creates a unified scan metadata table
func (c *UnifiedDatabaseConfig) createScanMetadataTable() error {
	// Drop the table if it exists to ensure clean schema
	if _, err := c.DB.Exec("DROP TABLE IF EXISTS scan_metadata"); err != nil {
		// Log but don't fail - table might not exist
		fmt.Printf("Note: Could not drop scan_metadata table: %v\n", err)
	}
	
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

	if _, err := c.DB.Exec(scanMetadataSQL); err != nil {
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
		if _, err := c.DB.Exec(idx); err != nil {
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

	if _, err := c.DB.Exec(apiMetadataSQL); err != nil {
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
		if _, err := c.DB.Exec(idx); err != nil {
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
SELECT 
    'aws' as provider,
    id,
    name,
    type,
    arn as resource_identifier,
    service,
    region as location,
    account_id,
    parent_id,
    tags,
    raw_data,
    scanned_at
FROM aws_resources
UNION ALL
SELECT 
    'azure' as provider,
    id,
    name,
    type,
    id as resource_identifier,
    service,
    location,
    subscription_id as account_id,
    parent_id,
    tags,
    raw_data,
    scanned_at
FROM azure_resources;`

	if _, err := c.DB.Exec(unifiedResourcesView); err != nil {
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

	if _, err := c.DB.Exec(resourceCountsView); err != nil {
		return fmt.Errorf("failed to create resource counts view: %w", err)
	}

	return nil
}
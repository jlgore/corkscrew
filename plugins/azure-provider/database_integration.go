package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb"  // DuckDB driver
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AzureDatabaseIntegration handles database operations for Azure resources
type AzureDatabaseIntegration struct {
	db             *sql.DB
	dbPath         string
	insertResource *sql.Stmt
	insertRelation *sql.Stmt
}

// NewAzureDatabaseIntegration creates a new Azure database integration
func NewAzureDatabaseIntegration() (*AzureDatabaseIntegration, error) {
	// Use unified database path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	
	dbPath := filepath.Join(homeDir, ".corkscrew", "db", "corkscrew.duckdb")
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}
	
	// Open database connection
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	
	// Install and load JSON extension
	if _, err := db.Exec("INSTALL json; LOAD json;"); err != nil {
		return nil, fmt.Errorf("failed to load JSON extension: %w", err)
	}
	
	integration := &AzureDatabaseIntegration{
		db:     db,
		dbPath: dbPath,
	}
	
	// Initialize Azure tables
	if err := integration.initializeTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}
	
	// Prepare statements
	if err := integration.prepareStatements(); err != nil {
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}
	
	log.Printf("Azure database integration initialized at: %s", dbPath)
	return integration, nil
}

// initializeTables creates the Azure resources table if it doesn't exist
func (d *AzureDatabaseIntegration) initializeTables() error {
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
    api_version VARCHAR                        -- API version used to fetch this resource
);`

	if _, err := d.db.Exec(azureTableSQL); err != nil {
		return fmt.Errorf("failed to create azure_resources table: %w", err)
	}

	// Create indexes
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

	for _, indexSQL := range indexes {
		if _, err := d.db.Exec(indexSQL); err != nil {
			log.Printf("Warning: Failed to create index: %v", err)
		}
	}

	// Create unified relationships table if it doesn't exist
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

	if _, err := d.db.Exec(relationshipsSQL); err != nil {
		return fmt.Errorf("failed to create cloud_relationships table: %w", err)
	}

	// Create relationship indexes
	relIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_rel_from_id ON cloud_relationships(from_id)",
		"CREATE INDEX IF NOT EXISTS idx_rel_to_id ON cloud_relationships(to_id)",
		"CREATE INDEX IF NOT EXISTS idx_rel_type ON cloud_relationships(relationship_type)",
		"CREATE INDEX IF NOT EXISTS idx_rel_provider ON cloud_relationships(provider)",
		"CREATE INDEX IF NOT EXISTS idx_rel_from_type ON cloud_relationships(from_resource_type)",
		"CREATE INDEX IF NOT EXISTS idx_rel_to_type ON cloud_relationships(to_resource_type)",
	}

	for _, indexSQL := range relIndexes {
		if _, err := d.db.Exec(indexSQL); err != nil {
			log.Printf("Warning: Failed to create relationship index: %v", err)
		}
	}

	return nil
}

// prepareStatements prepares commonly used SQL statements
func (d *AzureDatabaseIntegration) prepareStatements() error {
	// Prepare resource insert statement
	insertResourceSQL := `
INSERT OR REPLACE INTO azure_resources (
    id, name, type, resource_id, subscription_id, resource_group,
    location, parent_id, managed_by, service, kind,
    sku_name, sku_tier, sku_size, sku_family, sku_capacity,
    tags, properties, raw_data, provisioning_state, power_state,
    created_time, changed_time, etag, api_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var err error
	d.insertResource, err = d.db.Prepare(insertResourceSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert resource statement: %w", err)
	}

	// Prepare relationship insert statement
	insertRelationSQL := `
INSERT OR REPLACE INTO cloud_relationships (
    from_id, to_id, relationship_type, provider, relationship_subtype,
    properties, from_resource_type, to_resource_type, direction
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	d.insertRelation, err = d.db.Prepare(insertRelationSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert relationship statement: %w", err)
	}

	return nil
}

// StoreResource stores an Azure resource in the database
func (d *AzureDatabaseIntegration) StoreResource(resource *pb.Resource) error {
	if d.insertResource == nil {
		return fmt.Errorf("database not properly initialized")
	}

	// Convert tags to JSON
	tagsJSON, err := json.Marshal(resource.Tags)
	if err != nil || resource.Tags == nil {
		tagsJSON = []byte("{}")
	}

	// Parse raw data to extract properties and other fields
	var properties map[string]interface{}
	var createdTime, changedTime *time.Time
	var etag, apiVersion string
	var skuInfo map[string]interface{}

	if resource.RawData != "" {
		var rawDataMap map[string]interface{}
		if err := json.Unmarshal([]byte(resource.RawData), &rawDataMap); err == nil {
			// Extract properties
			if props, ok := rawDataMap["properties"].(map[string]interface{}); ok {
				properties = props
			}

			// Extract timestamps
			if ct, ok := rawDataMap["createdTime"].(string); ok {
				if parsed, err := time.Parse(time.RFC3339, ct); err == nil {
					createdTime = &parsed
				}
			}
			if ct, ok := rawDataMap["changedTime"].(string); ok {
				if parsed, err := time.Parse(time.RFC3339, ct); err == nil {
					changedTime = &parsed
				}
			}

			// Extract etag and API version
			if e, ok := rawDataMap["etag"].(string); ok {
				etag = e
			}
			if av, ok := rawDataMap["apiVersion"].(string); ok {
				apiVersion = av
			}

			// Extract SKU information
			if sku, ok := rawDataMap["sku"].(map[string]interface{}); ok {
				skuInfo = sku
			}
		}
	}

	// Convert properties to JSON
	propertiesJSON, err := json.Marshal(properties)
	if err != nil || properties == nil {
		propertiesJSON = []byte("{}")
	}

	// Extract SKU fields
	var skuName, skuTier, skuSize, skuFamily string
	var skuCapacity *int
	if skuInfo != nil {
		if name, ok := skuInfo["name"].(string); ok {
			skuName = name
		}
		if tier, ok := skuInfo["tier"].(string); ok {
			skuTier = tier
		}
		if size, ok := skuInfo["size"].(string); ok {
			skuSize = size
		}
		if family, ok := skuInfo["family"].(string); ok {
			skuFamily = family
		}
		if capacity, ok := skuInfo["capacity"].(float64); ok {
			cap := int(capacity)
			skuCapacity = &cap
		}
	}

	// Handle empty RawData
	rawData := resource.RawData
	if rawData == "" {
		rawData = "{}"
	}

	// Extract resource group from resource ID
	resourceGroup := extractResourceGroupFromID(resource.Id)
	if resourceGroup == "" {
		resourceGroup = resource.ParentId
	}

	// Execute insert
	_, err = d.insertResource.Exec(
		resource.Id,                    // id
		resource.Name,                  // name
		resource.Type,                  // type
		resource.Id,                    // resource_id (same as id for now)
		resource.AccountId,             // subscription_id
		resourceGroup,                  // resource_group
		resource.Region,                // location
		resource.ParentId,              // parent_id
		nil,                           // managed_by
		resource.Service,               // service
		nil,                           // kind
		skuName,                       // sku_name
		skuTier,                       // sku_tier
		skuSize,                       // sku_size
		skuFamily,                     // sku_family
		skuCapacity,                   // sku_capacity
		string(tagsJSON),              // tags
		string(propertiesJSON),        // properties
		rawData,                       // raw_data
		nil,                           // provisioning_state
		nil,                           // power_state
		createdTime,                   // created_time
		changedTime,                   // changed_time
		etag,                          // etag
		apiVersion,                    // api_version
	)

	if err != nil {
		return fmt.Errorf("failed to store resource %s: %w", resource.Id, err)
	}

	// Store relationships
	for _, rel := range resource.Relationships {
		if err := d.StoreRelationship(resource.Id, rel); err != nil {
			log.Printf("Warning: Failed to store relationship from %s to %s: %v", resource.Id, rel.TargetId, err)
		}
	}

	return nil
}

// StoreRelationship stores a relationship in the database
func (d *AzureDatabaseIntegration) StoreRelationship(fromID string, relationship *pb.Relationship) error {
	if d.insertRelation == nil {
		return fmt.Errorf("database not properly initialized")
	}

	// Convert properties to JSON if any
	propertiesJSON := "{}"
	if len(relationship.TargetType) > 0 {
		props := map[string]string{
			"target_type": relationship.TargetType,
		}
		if b, err := json.Marshal(props); err == nil {
			propertiesJSON = string(b)
		}
	}

	_, err := d.insertRelation.Exec(
		fromID,                          // from_id
		relationship.TargetId,           // to_id
		relationship.RelationshipType,   // relationship_type
		"azure",                         // provider
		"",                              // relationship_subtype
		propertiesJSON,                  // properties
		"",                              // from_resource_type (to be filled)
		relationship.TargetType,         // to_resource_type
		"outbound",                      // direction
	)

	return err
}

// StoreResources stores multiple resources in a transaction
func (d *AzureDatabaseIntegration) StoreResources(resources []*pb.Resource) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, resource := range resources {
		if err := d.StoreResource(resource); err != nil {
			return fmt.Errorf("failed to store resource in transaction: %w", err)
		}
	}

	return tx.Commit()
}

// QueryResources queries Azure resources from the database
func (d *AzureDatabaseIntegration) QueryResources(query string, args ...interface{}) ([]*pb.Resource, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var resources []*pb.Resource
	for rows.Next() {
		resource := &pb.Resource{}
		var tagsJSON, propertiesJSON string
		var createdTime, changedTime *time.Time
		var skuCapacity *int

		err := rows.Scan(
			&resource.Id,
			&resource.Name,
			&resource.Type,
			&resource.AccountId,
			&resource.ParentId,
			&resource.Region,
			&resource.Service,
			&tagsJSON,
			&propertiesJSON,
			&resource.RawData,
			&createdTime,
			&changedTime,
			&skuCapacity,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Parse tags
		if err := json.Unmarshal([]byte(tagsJSON), &resource.Tags); err != nil {
			resource.Tags = make(map[string]string)
		}

		// Set timestamps
		if createdTime != nil {
			resource.DiscoveredAt = timestamppb.New(*createdTime)
		} else {
			resource.DiscoveredAt = timestamppb.Now()
		}

		resource.Provider = "azure"
		resources = append(resources, resource)
	}

	return resources, rows.Err()
}

// GetResourceCount returns the count of Azure resources
func (d *AzureDatabaseIntegration) GetResourceCount() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM azure_resources").Scan(&count)
	return count, err
}

// GetResourcesByService returns resources for a specific service
func (d *AzureDatabaseIntegration) GetResourcesByService(service string) ([]*pb.Resource, error) {
	query := `
SELECT id, name, type, subscription_id, resource_group, location, service,
       tags, properties, raw_data, created_time, changed_time, sku_capacity
FROM azure_resources 
WHERE service = ?
ORDER BY scanned_at DESC`

	return d.QueryResources(query, service)
}

// Close closes the database connection
func (d *AzureDatabaseIntegration) Close() error {
	if d.insertResource != nil {
		d.insertResource.Close()
	}
	if d.insertRelation != nil {
		d.insertRelation.Close()
	}
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// GetDB returns the underlying database connection
func (d *AzureDatabaseIntegration) GetDB() *sql.DB {
	return d.db
}

// extractResourceGroupFromID extracts resource group name from Azure resource ID
func extractResourceGroupFromID(resourceID string) string {
	// Format: /subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
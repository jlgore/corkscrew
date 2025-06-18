package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	
	_ "github.com/marcboeker/go-duckdb"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// GCPDatabaseIntegration provides DuckDB storage for GCP resources
type GCPDatabaseIntegration struct {
	db              *sql.DB
	schemaGenerator *GCPSchemaGenerator
	dbPath          string
}

// DuckDBSchemas contains all database schema definitions
type DuckDBSchemas struct {
	GCPResourcesTable     string
	GCPRelationshipsTable string
	ScanMetadataTable     string
	ServiceTables         map[string]string
}

// NewGCPDatabaseIntegration creates a new database integration
func NewGCPDatabaseIntegration(dbPath string) (*GCPDatabaseIntegration, error) {
	if dbPath == "" {
		dbPath = "gcp_resources.db"
	}
	
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	
	integration := &GCPDatabaseIntegration{
		db:              db,
		schemaGenerator: NewGCPSchemaGenerator(),
		dbPath:          dbPath,
	}
	
	// Initialize schemas
	if err := integration.InitializeSchemas(); err != nil {
		return nil, fmt.Errorf("failed to initialize schemas: %w", err)
	}
	
	return integration, nil
}

// InitializeSchemas creates the necessary database schemas
func (gdi *GCPDatabaseIntegration) InitializeSchemas() error {
	schemas := gdi.GenerateDuckDBSchemas()
	
	// Create unified resources table
	if err := gdi.executeSchema(schemas.GCPResourcesTable); err != nil {
		return fmt.Errorf("failed to create resources table: %w", err)
	}
	
	// Create relationships table
	if err := gdi.executeSchema(schemas.GCPRelationshipsTable); err != nil {
		return fmt.Errorf("failed to create relationships table: %w", err)
	}
	
	// Create scan metadata table
	if err := gdi.executeSchema(schemas.ScanMetadataTable); err != nil {
		return fmt.Errorf("failed to create scan metadata table: %w", err)
	}
	
	// Create service-specific tables
	for service, schema := range schemas.ServiceTables {
		if err := gdi.executeSchema(schema); err != nil {
			log.Printf("Warning: failed to create %s table: %v", service, err)
		}
	}
	
	// Create indexes
	if err := gdi.createIndexes(); err != nil {
		log.Printf("Warning: failed to create indexes: %v", err)
	}
	
	// Create views
	if err := gdi.createAnalyticsViews(); err != nil {
		log.Printf("Warning: failed to create views: %v", err)
	}
	
	return nil
}

// StoreResources stores resources in the database
func (gdi *GCPDatabaseIntegration) StoreResources(resources []*pb.Resource) error {
	tx, err := gdi.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	
	// Prepare bulk insert statement
	stmt, err := tx.Prepare(`
		INSERT INTO gcp_resources (
			id, name, type, service, project_id, location, 
			org_id, folder_id, tags, labels, raw_data, 
			discovered_at, scan_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			type = EXCLUDED.type,
			service = EXCLUDED.service,
			location = EXCLUDED.location,
			tags = EXCLUDED.tags,
			labels = EXCLUDED.labels,
			raw_data = EXCLUDED.raw_data,
			discovered_at = EXCLUDED.discovered_at,
			scan_id = EXCLUDED.scan_id
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()
	
	scanID := generateScanID()
	
	for _, resource := range resources {
		// Extract GCP-specific fields
		projectID := extractProjectID(resource.Id)
		orgID := extractOrgID(resource.Id)
		folderID := extractFolderID(resource.Id)
		
		// Parse attributes from JSON string
		var attrs map[string]interface{}
		if err := json.Unmarshal([]byte(resource.Attributes), &attrs); err != nil {
			attrs = make(map[string]interface{})
		}
		
		// Convert tags and labels to JSON
		tagsJSON, _ := json.Marshal(resource.Tags)
		labelsJSON, _ := json.Marshal(attrs["labels"])
		
		_, err = stmt.Exec(
			resource.Id,
			resource.Name,
			resource.Type,
			resource.Service,
			projectID,
			resource.Region,
			orgID,
			folderID,
			string(tagsJSON),
			string(labelsJSON),
			resource.RawData,
			resource.DiscoveredAt.AsTime(),
			scanID,
		)
		if err != nil {
			log.Printf("Failed to insert resource %s: %v", resource.Id, err)
			continue
		}
		
		// Store in service-specific table if applicable
		if err := gdi.storeServiceSpecificData(tx, resource); err != nil {
			log.Printf("Failed to store service-specific data for %s: %v", resource.Id, err)
		}
	}
	
	// Store relationships
	if err := gdi.storeRelationships(tx, resources); err != nil {
		log.Printf("Failed to store relationships: %v", err)
	}
	
	// Store scan metadata
	if err := gdi.storeScanMetadata(tx, scanID, len(resources)); err != nil {
		log.Printf("Failed to store scan metadata: %v", err)
	}
	
	return tx.Commit()
}

// storeRelationships stores resource relationships
func (gdi *GCPDatabaseIntegration) storeRelationships(tx *sql.Tx, resources []*pb.Resource) error {
	stmt, err := tx.Prepare(`
		INSERT INTO gcp_relationships (
			id, source_id, target_id, relationship_type, 
			properties, discovered_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			properties = EXCLUDED.properties,
			discovered_at = EXCLUDED.discovered_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	
	for _, resource := range resources {
		if resource.Relationships == nil {
			continue
		}
		
		for _, rel := range resource.Relationships {
			relID := fmt.Sprintf("%s-%s-%s", resource.Id, rel.RelationshipType, rel.TargetId)
			propertiesJSON, _ := json.Marshal(rel.Properties)
			
			_, err = stmt.Exec(
				relID,
				resource.Id,
				rel.TargetId,
				rel.RelationshipType,
				string(propertiesJSON),
				time.Now(),
			)
			if err != nil {
				log.Printf("Failed to insert relationship %s: %v", relID, err)
			}
		}
	}
	
	return nil
}

// storeServiceSpecificData stores data in service-specific tables
func (gdi *GCPDatabaseIntegration) storeServiceSpecificData(tx *sql.Tx, resource *pb.Resource) error {
	switch resource.Service {
	case "compute":
		return gdi.storeComputeInstance(tx, resource)
	case "storage":
		return gdi.storeStorageBucket(tx, resource)
	case "bigquery":
		return gdi.storeBigQueryDataset(tx, resource)
	// Add more services as needed
	default:
		// No service-specific storage
		return nil
	}
}

// storeComputeInstance stores compute instance data
func (gdi *GCPDatabaseIntegration) storeComputeInstance(tx *sql.Tx, resource *pb.Resource) error {
	if resource.Type != "Instance" {
		return nil
	}
	
	stmt, err := tx.Prepare(`
		INSERT INTO gcp_compute_instances (
			id, name, machine_type, status, zone, project_id,
			network_interfaces, disks, metadata, labels, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			machine_type = EXCLUDED.machine_type,
			status = EXCLUDED.status,
			network_interfaces = EXCLUDED.network_interfaces,
			disks = EXCLUDED.disks,
			metadata = EXCLUDED.metadata,
			labels = EXCLUDED.labels
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	
	// Parse attributes from JSON string
	var attrs map[string]interface{}
	if err := json.Unmarshal([]byte(resource.Attributes), &attrs); err != nil {
		attrs = make(map[string]interface{})
	}
	
	// Extract fields from attributes
	machineType, _ := attrs["machineType"].(string)
	status, _ := attrs["status"].(string)
	zone, _ := attrs["zone"].(string)
	
	networkInterfacesJSON, _ := json.Marshal(attrs["networkInterfaces"])
	disksJSON, _ := json.Marshal(attrs["disks"])
	metadataJSON, _ := json.Marshal(attrs["metadata"])
	labelsJSON, _ := json.Marshal(attrs["labels"])
	
	createdAt := resource.CreatedAt.AsTime()
	
	_, err = stmt.Exec(
		resource.Id,
		resource.Name,
		machineType,
		status,
		zone,
		extractProjectID(resource.Id),
		string(networkInterfacesJSON),
		string(disksJSON),
		string(metadataJSON),
		string(labelsJSON),
		createdAt,
	)
	
	return err
}

// storeStorageBucket stores storage bucket data
func (gdi *GCPDatabaseIntegration) storeStorageBucket(tx *sql.Tx, resource *pb.Resource) error {
	if resource.Type != "Bucket" {
		return nil
	}
	
	stmt, err := tx.Prepare(`
		INSERT INTO gcp_storage_buckets (
			id, name, location, storage_class, versioning_enabled,
			lifecycle_rules, iam_configuration, labels, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			location = EXCLUDED.location,
			storage_class = EXCLUDED.storage_class,
			versioning_enabled = EXCLUDED.versioning_enabled,
			lifecycle_rules = EXCLUDED.lifecycle_rules,
			iam_configuration = EXCLUDED.iam_configuration,
			labels = EXCLUDED.labels
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	
	// Parse attributes from JSON string
	var attrs map[string]interface{}
	if err := json.Unmarshal([]byte(resource.Attributes), &attrs); err != nil {
		attrs = make(map[string]interface{})
	}
	
	// Extract fields
	location, _ := attrs["location"].(string)
	storageClass, _ := attrs["storageClass"].(string)
	versioningEnabled, _ := attrs["versioningEnabled"].(bool)
	
	lifecycleRulesJSON, _ := json.Marshal(attrs["lifecycleRules"])
	iamConfigJSON, _ := json.Marshal(attrs["iamConfiguration"])
	labelsJSON, _ := json.Marshal(attrs["labels"])
	
	createdAt := resource.CreatedAt.AsTime()
	
	_, err = stmt.Exec(
		resource.Id,
		resource.Name,
		location,
		storageClass,
		versioningEnabled,
		string(lifecycleRulesJSON),
		string(iamConfigJSON),
		string(labelsJSON),
		createdAt,
	)
	
	return err
}

// storeBigQueryDataset stores BigQuery dataset data
func (gdi *GCPDatabaseIntegration) storeBigQueryDataset(tx *sql.Tx, resource *pb.Resource) error {
	// Implementation similar to above
	return nil
}

// storeScanMetadata stores scan metadata
func (gdi *GCPDatabaseIntegration) storeScanMetadata(tx *sql.Tx, scanID string, resourceCount int) error {
	_, err := tx.Exec(`
		INSERT INTO gcp_scan_metadata (
			scan_id, started_at, completed_at, resource_count,
			status, scan_type
		) VALUES (?, ?, ?, ?, ?, ?)
	`, scanID, time.Now(), time.Now(), resourceCount, "completed", "full")
	
	return err
}

// GenerateDuckDBSchemas generates GCP-specific DuckDB schemas
func (gdi *GCPDatabaseIntegration) GenerateDuckDBSchemas() *DuckDBSchemas {
	return &DuckDBSchemas{
		GCPResourcesTable: `
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
				discovered_at TIMESTAMP,
				scan_id VARCHAR
			);
		`,
		
		GCPRelationshipsTable: `
			CREATE TABLE IF NOT EXISTS gcp_relationships (
				id VARCHAR PRIMARY KEY,
				source_id VARCHAR NOT NULL,
				target_id VARCHAR NOT NULL,
				relationship_type VARCHAR NOT NULL,
				properties JSON,
				discovered_at TIMESTAMP,
				FOREIGN KEY (source_id) REFERENCES gcp_resources(id),
				FOREIGN KEY (target_id) REFERENCES gcp_resources(id)
			);
		`,
		
		ScanMetadataTable: `
			CREATE TABLE IF NOT EXISTS gcp_scan_metadata (
				scan_id VARCHAR PRIMARY KEY,
				started_at TIMESTAMP,
				completed_at TIMESTAMP,
				resource_count INTEGER,
				status VARCHAR,
				scan_type VARCHAR,
				error_message VARCHAR
			);
		`,
		
		ServiceTables: map[string]string{
			"compute_instances": `
				CREATE TABLE IF NOT EXISTS gcp_compute_instances (
					id VARCHAR PRIMARY KEY,
					name VARCHAR NOT NULL,
					machine_type VARCHAR,
					status VARCHAR,
					zone VARCHAR,
					project_id VARCHAR,
					network_interfaces JSON,
					disks JSON,
					metadata JSON,
					labels JSON,
					created_at TIMESTAMP,
					FOREIGN KEY (id) REFERENCES gcp_resources(id)
				);
			`,
			"storage_buckets": `
				CREATE TABLE IF NOT EXISTS gcp_storage_buckets (
					id VARCHAR PRIMARY KEY,
					name VARCHAR NOT NULL UNIQUE,
					location VARCHAR,
					storage_class VARCHAR,
					versioning_enabled BOOLEAN,
					lifecycle_rules JSON,
					iam_configuration JSON,
					labels JSON,
					created_at TIMESTAMP,
					FOREIGN KEY (id) REFERENCES gcp_resources(id)
				);
			`,
			"bigquery_datasets": `
				CREATE TABLE IF NOT EXISTS gcp_bigquery_datasets (
					id VARCHAR PRIMARY KEY,
					name VARCHAR NOT NULL,
					location VARCHAR,
					project_id VARCHAR,
					default_table_expiration_ms BIGINT,
					description VARCHAR,
					labels JSON,
					access JSON,
					created_at TIMESTAMP,
					FOREIGN KEY (id) REFERENCES gcp_resources(id)
				);
			`,
		},
	}
}

// createIndexes creates database indexes for performance
func (gdi *GCPDatabaseIntegration) createIndexes() error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_resources_type ON gcp_resources(type);",
		"CREATE INDEX IF NOT EXISTS idx_resources_service ON gcp_resources(service);",
		"CREATE INDEX IF NOT EXISTS idx_resources_project ON gcp_resources(project_id);",
		"CREATE INDEX IF NOT EXISTS idx_resources_location ON gcp_resources(location);",
		"CREATE INDEX IF NOT EXISTS idx_resources_scan ON gcp_resources(scan_id);",
		"CREATE INDEX IF NOT EXISTS idx_relationships_source ON gcp_relationships(source_id);",
		"CREATE INDEX IF NOT EXISTS idx_relationships_target ON gcp_relationships(target_id);",
		"CREATE INDEX IF NOT EXISTS idx_relationships_type ON gcp_relationships(relationship_type);",
	}
	
	for _, index := range indexes {
		if _, err := gdi.db.Exec(index); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}
	
	return nil
}

// createAnalyticsViews creates analytics views
func (gdi *GCPDatabaseIntegration) createAnalyticsViews() error {
	views := map[string]string{
		"resource_distribution": `
			CREATE OR REPLACE VIEW gcp_resource_distribution AS
			SELECT 
				service,
				type,
				location,
				project_id,
				COUNT(*) as resource_count
			FROM gcp_resources
			GROUP BY service, type, location, project_id;
		`,
		"resource_relationships_graph": `
			CREATE OR REPLACE VIEW gcp_resource_relationships_graph AS
			SELECT 
				r1.name as source_name,
				r1.type as source_type,
				rel.relationship_type,
				r2.name as target_name,
				r2.type as target_type
			FROM gcp_relationships rel
			JOIN gcp_resources r1 ON rel.source_id = r1.id
			JOIN gcp_resources r2 ON rel.target_id = r2.id;
		`,
		"compute_instances_summary": `
			CREATE OR REPLACE VIEW gcp_compute_instances_summary AS
			SELECT 
				ci.zone,
				ci.machine_type,
				ci.status,
				COUNT(*) as instance_count,
				r.project_id
			FROM gcp_compute_instances ci
			JOIN gcp_resources r ON ci.id = r.id
			GROUP BY ci.zone, ci.machine_type, ci.status, r.project_id;
		`,
	}
	
	for name, query := range views {
		if _, err := gdi.db.Exec(query); err != nil {
			log.Printf("Failed to create view %s: %v", name, err)
		}
	}
	
	return nil
}

// Query methods
func (gdi *GCPDatabaseIntegration) QueryResources(query string, args ...interface{}) ([]*pb.Resource, error) {
	rows, err := gdi.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var resources []*pb.Resource
	for rows.Next() {
		resource := &pb.Resource{}
		var tagsJSON, labelsJSON, rawDataJSON string
		var discoveredAt time.Time
		
		err := rows.Scan(
			&resource.Id,
			&resource.Name,
			&resource.Type,
			&resource.Service,
			&resource.Region,
			&tagsJSON,
			&labelsJSON,
			&rawDataJSON,
			&discoveredAt,
		)
		if err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}
		
		// Parse JSON fields
		json.Unmarshal([]byte(tagsJSON), &resource.Tags)
		json.Unmarshal([]byte(rawDataJSON), &resource.RawData)
		
		resources = append(resources, resource)
	}
	
	return resources, nil
}

// Helper functions
func (gdi *GCPDatabaseIntegration) executeSchema(schema string) error {
	_, err := gdi.db.Exec(schema)
	return err
}

func generateScanID() string {
	return fmt.Sprintf("scan-%d", time.Now().Unix())
}

func extractProjectID(resourceID string) string {
	// Extract project ID from resource ID
	// Format: projects/PROJECT_ID/...
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if part == "projects" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractOrgID(resourceID string) string {
	// Extract org ID from resource ID
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if part == "organizations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractFolderID(resourceID string) string {
	// Extract folder ID from resource ID
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if part == "folders" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// Close closes the database connection
func (gdi *GCPDatabaseIntegration) Close() error {
	return gdi.db.Close()
}
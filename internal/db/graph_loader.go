package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	pb "github.com/jlgore/corkscrew/internal/proto"
	_ "github.com/marcboeker/go-duckdb"
)

type GraphLoader struct {
	db *sql.DB
}

func NewGraphLoader(dbPath string) (*GraphLoader, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, err
	}

	if err := dbInit(db); err != nil {
		db.Close()
		return nil, err
	}

	// Initialize schema
	if err := initializeGraphSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &GraphLoader{db: db}, nil
}

func dbInit(db *sql.DB) error {
	// Install and load the JSON extension
	if _, err := db.Exec(`INSTALL json;`); err != nil {
		// Ignore "already installed" errors if you want
		fmt.Printf("Warning: INSTALL json: %v\n", err)
	}
	if _, err := db.Exec(`LOAD json;`); err != nil {
		return fmt.Errorf("failed to load json extension: %w", err)
	}
	// Optionally set autoinstall/autoload for future extensions
	if _, err := db.Exec(`SET autoinstall_known_extensions=1;`); err != nil {
		fmt.Printf("Warning: SET autoinstall_known_extensions: %v\n", err)
	}
	if _, err := db.Exec(`SET autoload_known_extensions=1;`); err != nil {
		fmt.Printf("Warning: SET autoload_known_extensions: %v\n", err)
	}
	return nil
}

func initializeGraphSchema(db *sql.DB) error {
	// Create resource vertex table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS aws_resources (
			id VARCHAR PRIMARY KEY,
			type VARCHAR NOT NULL,
			service VARCHAR,
			arn VARCHAR,
			name VARCHAR,
			region VARCHAR,
			account_id VARCHAR,
			parent_id VARCHAR,
			raw_data JSON,
			attributes JSON,
			tags JSON,
			created_at TIMESTAMP,
			modified_at TIMESTAMP,
			scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// Create relationships edge table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS aws_relationships (
			from_id VARCHAR NOT NULL,
			to_id VARCHAR NOT NULL,
			relationship_type VARCHAR NOT NULL,
			properties JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (from_id, to_id, relationship_type)
		)
	`)
	if err != nil {
		return err
	}

	// Create scan metadata table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS scan_metadata (
			id VARCHAR PRIMARY KEY,
			service VARCHAR NOT NULL,
			region VARCHAR NOT NULL,
			scan_time TIMESTAMP NOT NULL,
			total_resources INTEGER,
			failed_resources INTEGER,
			duration_ms BIGINT,
			metadata JSON
		)
	`)
	if err != nil {
		return err
	}

	// Create API action metadata table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS api_action_metadata (
			id VARCHAR PRIMARY KEY,
			service VARCHAR NOT NULL,
			operation_name VARCHAR NOT NULL,
			operation_type VARCHAR, -- List, Describe, Get, etc.
			execution_time TIMESTAMP NOT NULL,
			region VARCHAR,
			success BOOLEAN NOT NULL,
			duration_ms BIGINT,
			resource_count INTEGER DEFAULT 0,
			error_message VARCHAR,
			request_id VARCHAR,
			metadata JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// Create indexes
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_resources_type ON aws_resources(type);
		CREATE INDEX IF NOT EXISTS idx_resources_service ON aws_resources(service);
		CREATE INDEX IF NOT EXISTS idx_resources_region ON aws_resources(region);
		CREATE INDEX IF NOT EXISTS idx_resources_parent ON aws_resources(parent_id);
		CREATE INDEX IF NOT EXISTS idx_relationships_type ON aws_relationships(relationship_type);
		CREATE INDEX IF NOT EXISTS idx_relationships_from ON aws_relationships(from_id);
		CREATE INDEX IF NOT EXISTS idx_relationships_to ON aws_relationships(to_id);
		CREATE INDEX IF NOT EXISTS idx_api_actions_service ON api_action_metadata(service);
		CREATE INDEX IF NOT EXISTS idx_api_actions_operation ON api_action_metadata(operation_name);
		CREATE INDEX IF NOT EXISTS idx_api_actions_time ON api_action_metadata(execution_time);
		CREATE INDEX IF NOT EXISTS idx_api_actions_success ON api_action_metadata(success);
	`)

	return err
}

func (gl *GraphLoader) LoadResources(ctx context.Context, resources []*pb.Resource) error {
	tx, err := gl.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Prepare statements
	resourceStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO aws_resources 
		(id, type, service, arn, name, region, account_id, parent_id, raw_data, attributes, tags, created_at, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer resourceStmt.Close()

	relationshipStmt, err := tx.Prepare(`
		INSERT INTO aws_relationships 
		(from_id, to_id, relationship_type, properties)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(from_id, to_id, relationship_type) DO UPDATE SET
			properties=excluded.properties
	`)
	if err != nil {
		return err
	}
	defer relationshipStmt.Close()

	// Load resources
	for _, resource := range resources {
		tagsJSON, _ := json.Marshal(resource.Tags)

		var createdAt, modifiedAt interface{}
		if resource.CreatedAt != nil {
			createdAt = resource.CreatedAt.AsTime()
		}
		if resource.ModifiedAt != nil {
			modifiedAt = resource.ModifiedAt.AsTime()
		}

		// Handle JSON fields properly - convert to string or NULL
		var rawDataStr, attributesStr interface{}
		if len(resource.RawData) > 0 {
			rawDataStr = string(resource.RawData)
		}
		if len(resource.Attributes) > 0 {
			attributesStr = string(resource.Attributes)
		}

		_, err = resourceStmt.Exec(
			resource.Id,
			resource.Type,
			resource.Service,
			resource.Arn,
			resource.Name,
			resource.Region,
			resource.AccountId,
			resource.ParentId,
			rawDataStr,
			attributesStr,
			string(tagsJSON),
			createdAt,
			modifiedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert resource %s: %w", resource.Id, err)
		}

		// Load relationships
		for _, rel := range resource.Relationships {
			propsJSON, _ := json.Marshal(rel.Properties)

			_, err = relationshipStmt.Exec(
				resource.Id,
				rel.TargetId,
				rel.RelationshipType,
				string(propsJSON),
			)
			if err != nil {
				// Log but don't fail - target might not exist yet
				fmt.Printf("Warning: failed to insert relationship %s -> %s: %v\n",
					resource.Id, rel.TargetId, err)
			}
		}
	}

	return tx.Commit()
}

func (gl *GraphLoader) LoadScanMetadata(ctx context.Context, service, region string, stats *pb.ScanStats, metadata map[string]string) error {
	metadataJSON, _ := json.Marshal(metadata)

	id := uuid.New().String()

	_, err := gl.db.ExecContext(ctx, `
		INSERT INTO scan_metadata (id, service, region, scan_time, total_resources, failed_resources, duration_ms, metadata)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?)
	`, id, service, region, stats.TotalResources, stats.FailedResources, stats.DurationMs, string(metadataJSON))

	return err
}

func (gl *GraphLoader) CreatePropertyGraph(ctx context.Context) error {
	// Note: This is a simplified version - actual PGQ syntax may vary
	_, err := gl.db.ExecContext(ctx, `
		CREATE OR REPLACE PROPERTY GRAPH aws_infrastructure
		VERTEX TABLES (
			aws_resources
		)
		EDGE TABLES (
			aws_relationships 
			SOURCE KEY (from_id) REFERENCES aws_resources (id)
			DESTINATION KEY (to_id) REFERENCES aws_resources (id)
		)
	`)
	return err
}

// Query methods for common use cases

func (gl *GraphLoader) GetResourcesByType(ctx context.Context, resourceType string) ([]map[string]interface{}, error) {
	rows, err := gl.db.QueryContext(ctx, `
		SELECT id, name, region, arn, tags, created_at, modified_at
		FROM aws_resources 
		WHERE type = ?
		ORDER BY name
	`, resourceType)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return gl.scanRowsToMaps(rows)
}

func (gl *GraphLoader) GetResourceDependencies(ctx context.Context, resourceID string) ([]map[string]interface{}, error) {
	rows, err := gl.db.QueryContext(ctx, `
		SELECT 
			r.id as target_id,
			r.type as target_type,
			r.name as target_name,
			r.region as target_region,
			rel.relationship_type as relationship,
			rel.properties
		FROM aws_relationships rel
		JOIN aws_resources r ON rel.to_id = r.id
		WHERE rel.from_id = ?
		ORDER BY rel.relationship_type, r.name
	`, resourceID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return gl.scanRowsToMaps(rows)
}

func (gl *GraphLoader) GetResourceDependents(ctx context.Context, resourceID string) ([]map[string]interface{}, error) {
	rows, err := gl.db.QueryContext(ctx, `
		SELECT 
			r.id as source_id,
			r.type as source_type,
			r.name as source_name,
			r.region as source_region,
			rel.relationship_type as relationship,
			rel.properties
		FROM aws_relationships rel
		JOIN aws_resources r ON rel.from_id = r.id
		WHERE rel.to_id = ?
		ORDER BY rel.relationship_type, r.name
	`, resourceID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return gl.scanRowsToMaps(rows)
}

func (gl *GraphLoader) GetResourcesByRegion(ctx context.Context, region string) ([]map[string]interface{}, error) {
	rows, err := gl.db.QueryContext(ctx, `
		SELECT type, COUNT(*) as count
		FROM aws_resources 
		WHERE region = ?
		GROUP BY type
		ORDER BY count DESC
	`, region)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return gl.scanRowsToMaps(rows)
}

func (gl *GraphLoader) GetScanHistory(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := gl.db.QueryContext(ctx, `
		SELECT 
			service,
			region,
			scan_time,
			total_resources,
			failed_resources,
			duration_ms
		FROM scan_metadata
		ORDER BY scan_time DESC
		LIMIT 50
	`)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return gl.scanRowsToMaps(rows)
}

// Advanced graph queries (these would use PGQ syntax in a real implementation)

func (gl *GraphLoader) FindResourcePath(ctx context.Context, fromID, toID string) ([]map[string]interface{}, error) {
	// This is a simplified path finding query
	// In a real PGQ implementation, you'd use graph traversal syntax
	rows, err := gl.db.QueryContext(ctx, `
		WITH RECURSIVE resource_path AS (
			SELECT from_id, to_id, relationship_type, 1 as depth, 
				   ARRAY[from_id] as path
			FROM aws_relationships
			WHERE from_id = ?
			
			UNION ALL
			
			SELECT r.from_id, r.to_id, r.relationship_type, rp.depth + 1,
				   array_append(rp.path, r.from_id)
			FROM aws_relationships r
			JOIN resource_path rp ON r.from_id = rp.to_id
			WHERE rp.depth < 10 AND NOT (r.from_id = ANY(rp.path))
		)
		SELECT path, depth, relationship_type
		FROM resource_path
		WHERE to_id = ?
		ORDER BY depth
		LIMIT 1
	`, fromID, toID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return gl.scanRowsToMaps(rows)
}

func (gl *GraphLoader) GetResourceNeighborhood(ctx context.Context, resourceID string, depth int) ([]map[string]interface{}, error) {
	rows, err := gl.db.QueryContext(ctx, `
		WITH RECURSIVE neighborhood AS (
			SELECT id, type, name, 0 as distance
			FROM aws_resources
			WHERE id = ?
			
			UNION ALL
			
			SELECT r.id, r.type, r.name, n.distance + 1
			FROM aws_resources r
			JOIN aws_relationships rel ON (r.id = rel.to_id OR r.id = rel.from_id)
			JOIN neighborhood n ON (
				(rel.from_id = n.id AND r.id = rel.to_id) OR
				(rel.to_id = n.id AND r.id = rel.from_id)
			)
			WHERE n.distance < ? AND r.id != ?
		)
		SELECT DISTINCT id, type, name, distance
		FROM neighborhood
		ORDER BY distance, type, name
	`, resourceID, depth, resourceID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return gl.scanRowsToMaps(rows)
}

func (gl *GraphLoader) scanRowsToMaps(rows *sql.Rows) ([]map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	return results, rows.Err()
}

// APIActionRecord represents an API action execution record
type APIActionRecord struct {
	ID            string                 `json:"id"`
	Service       string                 `json:"service"`
	OperationName string                 `json:"operation_name"`
	OperationType string                 `json:"operation_type"`
	ExecutionTime string                 `json:"execution_time"`
	Region        string                 `json:"region"`
	Success       bool                   `json:"success"`
	DurationMs    int64                  `json:"duration_ms"`
	ResourceCount int                    `json:"resource_count"`
	ErrorMessage  string                 `json:"error_message,omitempty"`
	RequestID     string                 `json:"request_id,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// LogAPIAction logs an API action execution to DuckDB
func (gl *GraphLoader) LogAPIAction(ctx context.Context, record APIActionRecord) error {
	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = gl.db.ExecContext(ctx, `
		INSERT INTO api_action_metadata 
		(id, service, operation_name, operation_type, execution_time, region, success, 
		 duration_ms, resource_count, error_message, request_id, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.Service, record.OperationName, record.OperationType,
		record.ExecutionTime, record.Region, record.Success, record.DurationMs,
		record.ResourceCount, record.ErrorMessage, record.RequestID, string(metadataJSON))

	return err
}

// GetAPIActionStats returns aggregated statistics about API actions
func (gl *GraphLoader) GetAPIActionStats(ctx context.Context, service string, hours int) (map[string]interface{}, error) {
	query := `
		SELECT 
			service,
			operation_name,
			operation_type,
			COUNT(*) as total_calls,
			SUM(CASE WHEN success THEN 1 ELSE 0 END) as successful_calls,
			AVG(duration_ms) as avg_duration_ms,
			SUM(resource_count) as total_resources,
			MAX(execution_time) as last_execution
		FROM api_action_metadata 
		WHERE execution_time >= NOW() - INTERVAL ? HOUR
	`
	args := []interface{}{hours}

	if service != "" {
		query += " AND service = ?"
		args = append(args, service)
	}

	query += `
		GROUP BY service, operation_name, operation_type
		ORDER BY total_calls DESC
	`

	rows, err := gl.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []map[string]interface{}
	for rows.Next() {
		var svc, opName, opType, lastExec string
		var totalCalls, successfulCalls, totalResources int
		var avgDuration float64

		err := rows.Scan(&svc, &opName, &opType, &totalCalls, &successfulCalls, 
			&avgDuration, &totalResources, &lastExec)
		if err != nil {
			return nil, err
		}

		stats = append(stats, map[string]interface{}{
			"service":          svc,
			"operation_name":   opName,
			"operation_type":   opType,
			"total_calls":      totalCalls,
			"successful_calls": successfulCalls,
			"success_rate":     float64(successfulCalls) / float64(totalCalls),
			"avg_duration_ms":  avgDuration,
			"total_resources":  totalResources,
			"last_execution":   lastExec,
		})
	}

	return map[string]interface{}{
		"stats":        stats,
		"total_operations": len(stats),
	}, rows.Err()
}

// ExecRaw executes a raw SQL statement with parameters
func (gl *GraphLoader) ExecRaw(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return gl.db.ExecContext(ctx, query, args...)
}

// Query executes a query and returns results as a slice of maps
func (gl *GraphLoader) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := gl.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	return results, rows.Err()
}

func (gl *GraphLoader) Close() error {
	return gl.db.Close()
}

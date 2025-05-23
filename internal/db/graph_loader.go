package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/marcboeker/go-duckdb"
	pb "github.com/jlgore/corkscrew-generator/internal/proto"
)

type GraphLoader struct {
	db *sql.DB
}

func NewGraphLoader(dbPath string) (*GraphLoader, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, err
	}

	// Initialize schema
	if err := initializeGraphSchema(db); err != nil {
		return nil, err
	}

	return &GraphLoader{db: db}, nil
}

func initializeGraphSchema(db *sql.DB) error {
	// Create resource vertex table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS aws_resources (
			id VARCHAR PRIMARY KEY,
			type VARCHAR NOT NULL,
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
			id INTEGER PRIMARY KEY,
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

	// Create indexes
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_resources_type ON aws_resources(type);
		CREATE INDEX IF NOT EXISTS idx_resources_region ON aws_resources(region);
		CREATE INDEX IF NOT EXISTS idx_resources_parent ON aws_resources(parent_id);
		CREATE INDEX IF NOT EXISTS idx_relationships_type ON aws_relationships(relationship_type);
		CREATE INDEX IF NOT EXISTS idx_relationships_from ON aws_relationships(from_id);
		CREATE INDEX IF NOT EXISTS idx_relationships_to ON aws_relationships(to_id);
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
		(id, type, arn, name, region, account_id, parent_id, raw_data, attributes, tags, created_at, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer resourceStmt.Close()

	relationshipStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO aws_relationships 
		(from_id, to_id, relationship_type, properties)
		VALUES (?, ?, ?, ?)
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

		_, err = resourceStmt.Exec(
			resource.Id,
			resource.Type,
			resource.Arn,
			resource.Name,
			resource.Region,
			resource.AccountId,
			resource.ParentId,
			resource.RawData,
			resource.Attributes,
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

	_, err := gl.db.ExecContext(ctx, `
		INSERT INTO scan_metadata (service, region, scan_time, total_resources, failed_resources, duration_ms, metadata)
		VALUES (?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?)
	`, service, region, stats.TotalResources, stats.FailedResources, stats.DurationMs, string(metadataJSON))

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

func (gl *GraphLoader) Close() error {
	return gl.db.Close()
}

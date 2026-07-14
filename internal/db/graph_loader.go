package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/duckdb/duckdb-go/v2"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/pkg/models"
)

type GraphLoader struct {
	db *sql.DB
	*GraphStore
}

// NewGraphLoader opens a graph loader against the given target, which may be a
// local DuckDB file path or a remote Quack server URI (e.g. "quack:host:9494").
// Options (such as WithToken) apply only to remote targets.
func NewGraphLoader(dbPath string, opts ...Option) (*GraphLoader, error) {
	db, err := OpenDuckDB(context.Background(), dbPath, opts...)
	if err != nil {
		return nil, err
	}

	if err := dbInit(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := EnsureSchema(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}

	return &GraphLoader{db: db, GraphStore: NewGraphStore(db)}, nil
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

	// Install and load the DuckPGQ extension for graph queries
	if _, err := db.Exec(`INSTALL duckpgq;`); err != nil {
		fmt.Printf("Warning: INSTALL duckpgq: %v\n", err)
	}
	if _, err := db.Exec(`LOAD duckpgq;`); err != nil {
		fmt.Printf("Warning: LOAD duckpgq: %v\n", err)
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

func (gl *GraphLoader) LoadResources(ctx context.Context, resources []*pb.Resource) error {
	return gl.GraphStore.StoreProtoResources(ctx, "aws", resources)
}

func (gl *GraphLoader) LoadScanMetadata(ctx context.Context, service, region string, stats *pb.ScanStats, metadata map[string]string) error {
	return gl.GraphStore.StoreScanMetadata(ctx, "", service, region, stats, metadata)
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
		"stats":            stats,
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

// CrossCloud DatabaseInterface Implementation
// These methods implement the interface required by CrossCloudOrchestrator

// StoreResources stores cross-cloud resources
func (gl *GraphLoader) StoreResources(resources []*models.Resource) error {
	return gl.GraphStore.StoreResources(resources)
}

// StoreIPAddresses stores IP addresses
func (gl *GraphLoader) StoreIPAddresses(addresses []*models.IPAddress) error {
	return gl.GraphStore.StoreIPAddresses(addresses)
}

// StoreDNSRecords stores DNS records
func (gl *GraphLoader) StoreDNSRecords(records []*models.DNSRecord) error {
	return gl.GraphStore.StoreDNSRecords(records)
}

// StoreCorrelations stores cross-cloud correlations
func (gl *GraphLoader) StoreCorrelations(correlations interface{}) error {
	return gl.GraphStore.StoreCorrelations(correlations)
}

func (gl *GraphLoader) storeResourceCorrelations(correlations []*models.ResourceCorrelation) error {
	return gl.GraphStore.storeResourceCorrelations(correlations)
}

func (gl *GraphLoader) storeGenericCorrelations(correlations interface{}) error {
	return gl.GraphStore.storeGenericCorrelations(correlations)
}

// GetResourcesByProvider retrieves resources for a specific provider
func (gl *GraphLoader) GetResourcesByProvider(provider string) ([]*models.Resource, error) {
	return gl.GraphStore.GetResourcesByProvider(provider)
}

// GetIPAddressesByProvider retrieves IP addresses for a specific provider
func (gl *GraphLoader) GetIPAddressesByProvider(provider string) ([]*models.IPAddress, error) {
	return gl.GraphStore.GetIPAddressesByProvider(provider)
}

// GetDNSRecordsByProvider retrieves DNS records for a specific provider
func (gl *GraphLoader) GetDNSRecordsByProvider(provider string) ([]*models.DNSRecord, error) {
	return gl.GraphStore.GetDNSRecordsByProvider(provider)
}

// QueryContext executes a query and returns raw SQL rows (required by CrossCloud interface)
func (gl *GraphLoader) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return gl.db.QueryContext(ctx, query, args...)
}

// BeginTx begins a transaction (required by CrossCloud interface)
func (gl *GraphLoader) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return gl.db.BeginTx(ctx, opts)
}

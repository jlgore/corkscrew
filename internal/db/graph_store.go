package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/pkg/models"
)

// GraphStore owns cross-cloud graph persistence over an existing database
// connection. It is shared by GraphLoader and UnifiedDatabaseConfig so callers
// do not get different behavior depending on which database entry point they
// were handed.
type GraphStore struct {
	db *sql.DB
}

// StoreScanResourcesOptions controls how scanner protobuf resources are
// persisted into provider resource tables.
type StoreScanResourcesOptions struct {
	ProviderTableOverride string
}

type scanResourceAdapterKind int

const (
	scanResourceAdapterGeneric scanResourceAdapterKind = iota
	scanResourceAdapterAzure
	scanResourceAdapterGCP
)

var scanResourceTableAdapters = map[string]scanResourceAdapterKind{
	"aws_resources":        scanResourceAdapterGeneric,
	"kubernetes_resources": scanResourceAdapterGeneric,
	"azure_resources":      scanResourceAdapterAzure,
	"gcp_resources":        scanResourceAdapterGCP,
}

type scanResourceDestination struct {
	table string
	store func(context.Context, *pb.Resource) error
}

func NewGraphStore(db *sql.DB) *GraphStore {
	return &GraphStore{db: db}
}

func graphJSONScanString(value interface{}) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		return v, v != ""
	case []byte:
		return string(v), len(v) > 0
	case sql.NullString:
		return v.String, v.Valid && v.String != ""
	case map[string]interface{}, []interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(data), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}

// StoreProtoResources stores scanner protobuf resources in the AWS graph tables
// and writes their relationships to the unified relationship table.
func (gs *GraphStore) StoreProtoResources(ctx context.Context, provider string, resources []*pb.Resource) error {
	if len(resources) == 0 {
		return nil
	}
	if provider == "" {
		provider = "aws"
	}

	if err := gs.ensureAWSResourcesTable(ctx); err != nil {
		return err
	}
	if err := gs.ensureCloudRelationshipsTable(ctx); err != nil {
		return err
	}

	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	resourceStmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO aws_resources
		(id, arn, name, type, service, region, account_id, parent_id, tags, attributes, raw_data, created_at, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer resourceStmt.Close()

	relationshipStmt, err := tx.Prepare(`
		INSERT INTO cloud_relationships
		(from_id, to_id, relationship_type, provider, properties)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(from_id, to_id, relationship_type, provider) DO UPDATE SET
			properties=excluded.properties
	`)
	if err != nil {
		return err
	}
	defer relationshipStmt.Close()

	for _, resource := range resources {
		arn := resource.Arn
		if resource.Id == resource.Arn && strings.HasPrefix(resource.Id, "arn:") {
			arn = ""
		}

		tagsJSON, _ := json.Marshal(resource.Tags)

		var createdAt, modifiedAt interface{}
		if resource.CreatedAt != nil {
			createdAt = resource.CreatedAt.AsTime()
		}
		if resource.ModifiedAt != nil {
			modifiedAt = resource.ModifiedAt.AsTime()
		}

		var rawDataStr, attributesStr interface{}
		if len(resource.RawData) > 0 {
			rawDataStr = string(resource.RawData)
		}
		if len(resource.Attributes) > 0 {
			attributesStr = string(resource.Attributes)
		}

		_, err = resourceStmt.Exec(
			resource.Id,
			arn,
			resource.Name,
			resource.Type,
			resource.Service,
			resource.Region,
			resource.AccountId,
			resource.ParentId,
			string(tagsJSON),
			attributesStr,
			rawDataStr,
			createdAt,
			modifiedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert resource %s: %w", resource.Id, err)
		}

		for _, rel := range resource.Relationships {
			propsJSON, _ := json.Marshal(rel.Properties)
			if _, err = relationshipStmt.Exec(resource.Id, rel.TargetId, rel.RelationshipType, provider, string(propsJSON)); err != nil {
				return fmt.Errorf("failed to insert relationship %s -> %s: %w", resource.Id, rel.TargetId, err)
			}
		}
	}

	return tx.Commit()
}

// StoreScanResources stores scanner protobuf resources in the unified provider
// resource tables and writes their relationships to cloud_relationships.
func (gs *GraphStore) StoreScanResources(ctx context.Context, resources []*pb.Resource, opts StoreScanResourcesOptions) error {
	if len(resources) == 0 {
		return nil
	}
	if err := gs.ensureCloudRelationshipsTable(ctx); err != nil {
		return err
	}

	seen := make(map[string]struct{})
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		destination, err := gs.scanResourceDestination(resource, opts.ProviderTableOverride)
		if err != nil {
			return fmt.Errorf("failed to select resource adapter for %s: %w", resource.Id, err)
		}
		key := destination.table + "|" + resource.Id
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if err := destination.store(ctx, resource); err != nil {
			return fmt.Errorf("failed to store resource %s: %w", resource.Id, err)
		}
		if err := gs.storeScanRelationships(ctx, resource); err != nil {
			return fmt.Errorf("failed to store relationships for %s: %w", resource.Id, err)
		}
	}

	return nil
}

func (gs *GraphStore) scanResourceDestination(resource *pb.Resource, override string) (scanResourceDestination, error) {
	table, err := scanResourceTable(resource, override)
	if err != nil {
		return scanResourceDestination{}, err
	}

	switch scanResourceTableAdapters[table] {
	case scanResourceAdapterAzure:
		return scanResourceDestination{table: table, store: gs.storeAzureScanResource}, nil
	case scanResourceAdapterGCP:
		return scanResourceDestination{table: table, store: gs.storeGCPScanResource}, nil
	default:
		return scanResourceDestination{
			table: table,
			store: func(ctx context.Context, resource *pb.Resource) error {
				return gs.storeGenericScanResource(ctx, table, resource)
			},
		}, nil
	}
}

func (gs *GraphStore) storeGenericScanResource(ctx context.Context, table string, resource *pb.Resource) error {
	tagsParam := scanTagsJSONOrNil(resource.Tags)
	attrsParam := scanStringJSONOrNil(resource.Attributes)
	rawParam := scanStringJSONOrNil(resource.RawData)

	arnValue := resource.Arn
	if strings.TrimSpace(arnValue) == "" {
		arnValue = resource.Id
	}

	var discoveredAt interface{}
	if resource.DiscoveredAt != nil {
		discoveredAt = resource.DiscoveredAt.AsTime()
	}

	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), resource.Id); err != nil {
		return fmt.Errorf("delete existing: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			id, arn, name, type, service, region, account_id,
			tags, attributes, raw_data, state,
			created_at, modified_at, scanned_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			try_cast(? AS JSON), try_cast(? AS JSON), try_cast(? AS JSON), ?,
			?, ?, CURRENT_TIMESTAMP
		)
	`, table),
		resource.Id,
		arnValue,
		scanStringOrFallback(resource.Name, resource.Id),
		scanStringOrFallback(resource.Type, "unknown"),
		scanStringOrFallback(resource.Service, "unknown"),
		resource.Region,
		resource.AccountId,
		tagsParam,
		attrsParam,
		rawParam,
		"active",
		discoveredAt,
		discoveredAt,
	); err != nil {
		return fmt.Errorf("insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (gs *GraphStore) storeAzureScanResource(ctx context.Context, resource *pb.Resource) error {
	tagsJSON := scanTagsJSONOrDefault(resource.Tags, "{}")
	rawData := scanStringOrFallback(resource.RawData, "{}")
	rawObject := scanJSONObject(rawData)

	propertiesJSON := "{}"
	if properties, ok := rawObject["properties"].(map[string]interface{}); ok {
		if data, err := json.Marshal(properties); err == nil {
			propertiesJSON = string(data)
		}
	}

	var createdTime, changedTime interface{}
	if value, ok := rawObject["createdTime"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			createdTime = parsed
		}
	}
	if value, ok := rawObject["changedTime"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			changedTime = parsed
		}
	}

	sku := scanMap(rawObject["sku"])
	resourceGroup := scanAzureResourceGroupFromID(resource.Id)
	if resourceGroup == "" {
		resourceGroup = resource.ParentId
	}

	_, err := gs.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO azure_resources (
			id, name, type, resource_id, subscription_id, resource_group,
			location, parent_id, managed_by, service, kind,
			sku_name, sku_tier, sku_size, sku_family, sku_capacity,
			tags, properties, raw_data, provisioning_state, power_state,
			created_time, changed_time, etag, api_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		resource.Id,
		scanStringOrFallback(resource.Name, resource.Id),
		scanStringOrFallback(resource.Type, "unknown"),
		resource.Id,
		resource.AccountId,
		resourceGroup,
		resource.Region,
		resource.ParentId,
		nil,
		scanStringOrFallback(resource.Service, "unknown"),
		nil,
		scanStringValue(sku["name"]),
		scanStringValue(sku["tier"]),
		scanStringValue(sku["size"]),
		scanStringValue(sku["family"]),
		scanIntValue(sku["capacity"]),
		tagsJSON,
		propertiesJSON,
		rawData,
		nil,
		nil,
		createdTime,
		changedTime,
		scanStringValue(rawObject["etag"]),
		scanStringValue(rawObject["apiVersion"]),
	)
	if err != nil {
		return fmt.Errorf("insert azure resource: %w", err)
	}
	return nil
}

func (gs *GraphStore) storeGCPScanResource(ctx context.Context, resource *pb.Resource) error {
	attrs := scanJSONObject(resource.Attributes)
	labelsJSON := scanAnyJSONOrDefault(attrs["labels"], "{}")
	tagsJSON := scanTagsJSONOrDefault(resource.Tags, "{}")
	scanID := scanStringOrFallback(scanStringValue(attrs["scan_id"]), fmt.Sprintf("scan-%d", time.Now().Unix()))

	discoveredAt := time.Now()
	if resource.DiscoveredAt != nil {
		discoveredAt = resource.DiscoveredAt.AsTime()
	}

	_, err := gs.db.ExecContext(ctx, `
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
			org_id = EXCLUDED.org_id,
			folder_id = EXCLUDED.folder_id,
			tags = EXCLUDED.tags,
			labels = EXCLUDED.labels,
			raw_data = EXCLUDED.raw_data,
			discovered_at = EXCLUDED.discovered_at,
			updated_at = EXCLUDED.discovered_at,
			scan_id = EXCLUDED.scan_id
	`,
		resource.Id,
		scanStringOrFallback(resource.Name, resource.Id),
		scanStringOrFallback(resource.Type, "unknown"),
		scanStringOrFallback(resource.Service, "unknown"),
		scanStringOrFallback(scanGCPProjectID(resource.Id), scanStringOrFallback(resource.AccountId, "unknown")),
		resource.Region,
		scanGCPOrgID(resource.Id),
		scanGCPFolderID(resource.Id),
		tagsJSON,
		labelsJSON,
		scanStringOrFallback(resource.RawData, "{}"),
		discoveredAt,
		scanID,
	)
	if err != nil {
		return fmt.Errorf("insert gcp resource: %w", err)
	}
	return nil
}

func (gs *GraphStore) storeScanRelationships(ctx context.Context, resource *pb.Resource) error {
	if len(resource.Relationships) == 0 {
		return nil
	}

	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	seen := make(map[string]struct{})
	for _, rel := range resource.Relationships {
		if rel == nil || rel.TargetId == "" || rel.RelationshipType == "" {
			continue
		}
		key := resource.Id + "|" + rel.TargetId + "|" + rel.RelationshipType
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO cloud_relationships (
				from_id, to_id, relationship_type, provider,
				relationship_subtype, properties,
				from_resource_type, to_resource_type, direction
			) VALUES (
				?, ?, ?, ?, ?, try_cast(? AS JSON), ?, ?, ?
			)
			ON CONFLICT(from_id, to_id, relationship_type, provider) DO UPDATE SET
				relationship_subtype = excluded.relationship_subtype,
				properties = excluded.properties,
				from_resource_type = excluded.from_resource_type,
				to_resource_type = excluded.to_resource_type,
				direction = excluded.direction`,
			resource.Id,
			rel.TargetId,
			rel.RelationshipType,
			strings.ToLower(resource.Provider),
			"",
			scanRelationshipPropertiesJSONOrNil(rel.Properties),
			resource.Type,
			scanRelationshipTargetType(rel),
			"outbound",
		); err != nil {
			return fmt.Errorf("insert rel: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rels: %w", err)
	}
	return nil
}

func scanResourceTable(resource *pb.Resource, override string) (string, error) {
	if table := strings.TrimSpace(override); table != "" {
		return scanValidatedResourceTable(table, "provider table override")
	}
	if table := scanResourceTableOverride(resource); table != "" {
		return scanValidatedResourceTable(table, "resource _table attribute")
	}
	if table := scanResourceTableForProvider(resource.Provider); table != "" {
		return table, nil
	}
	return "aws_resources", nil
}

func scanResourceTableForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "aws":
		return "aws_resources"
	case "azure":
		return "azure_resources"
	case "kubernetes":
		return "kubernetes_resources"
	case "gcp":
		return "gcp_resources"
	default:
		return ""
	}
}

func scanResourceTableOverride(resource *pb.Resource) string {
	if resource == nil || strings.TrimSpace(resource.Attributes) == "" {
		return ""
	}
	var attrs map[string]interface{}
	if err := json.Unmarshal([]byte(resource.Attributes), &attrs); err != nil {
		return ""
	}
	if value, ok := attrs["_table"]; ok {
		if table, ok := value.(string); ok && strings.TrimSpace(table) != "" {
			return strings.TrimSpace(table)
		}
	}
	return ""
}

func scanValidatedResourceTable(table string, source string) (string, error) {
	table = strings.TrimSpace(table)
	if !scanValidTableIdentifier(table) {
		return "", fmt.Errorf("invalid %s %q: table names must contain only letters, digits, and underscores, and must not start with a digit", source, table)
	}
	return table, nil
}

func scanValidTableIdentifier(table string) bool {
	if table == "" {
		return false
	}
	for i, r := range table {
		valid := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if !valid {
			return false
		}
	}
	first := rune(table[0])
	return first == '_' || (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
}

func scanTagsJSONOrNil(tags map[string]string) interface{} {
	if len(tags) == 0 {
		return nil
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return nil
	}
	return string(data)
}

func scanStringJSONOrNil(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func scanRelationshipPropertiesJSONOrNil(properties map[string]string) interface{} {
	if len(properties) == 0 {
		return nil
	}
	data, err := json.Marshal(properties)
	if err != nil {
		return nil
	}
	return string(data)
}

func scanRelationshipTargetType(rel *pb.Relationship) string {
	if strings.TrimSpace(rel.TargetType) != "" {
		return rel.TargetType
	}
	parts := strings.Split(rel.TargetId, "/")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

func scanTagsJSONOrDefault(tags map[string]string, fallback string) string {
	if len(tags) == 0 {
		return fallback
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return fallback
	}
	return string(data)
}

func scanAnyJSONOrDefault(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func scanJSONObject(value string) map[string]interface{} {
	if strings.TrimSpace(value) == "" {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func scanMap(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	return map[string]interface{}{}
}

func scanStringValue(value interface{}) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func scanStringOrFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func scanIntValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	}
	return nil
}

func scanAzureResourceGroupFromID(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func scanGCPProjectID(resourceID string) string {
	return scanPathSegmentAfter(resourceID, "projects")
}

func scanGCPOrgID(resourceID string) string {
	return scanPathSegmentAfter(resourceID, "organizations")
}

func scanGCPFolderID(resourceID string) string {
	return scanPathSegmentAfter(resourceID, "folders")
}

func scanPathSegmentAfter(resourceID, marker string) string {
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if part == marker && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// StoreScanMetadata stores scan metadata against either the unified scan schema
// or the older graph-loader schema.
func (gs *GraphStore) StoreScanMetadata(ctx context.Context, provider, service, region string, stats *pb.ScanStats, metadata map[string]string) error {
	if metadata == nil {
		metadata = map[string]string{}
	}
	if provider == "" {
		provider = metadata["provider"]
	}
	if provider == "" {
		provider = "aws"
	}
	if stats == nil {
		stats = &pb.ScanStats{}
	}

	columns, err := gs.scanMetadataColumns(ctx)
	if err != nil {
		if err := gs.ensureUnifiedScanMetadataTable(ctx); err != nil {
			return err
		}
		columns, err = gs.scanMetadataColumns(ctx)
		if err != nil {
			return err
		}
	}

	metadataJSON, _ := json.Marshal(metadata)
	id := uuid.New().String()

	if columns["scan_start_time"] && columns["services"] && columns["regions"] {
		servicesJSON, _ := json.Marshal([]string{service})
		regionsJSON, _ := json.Marshal([]string{region})

		_, err := gs.db.ExecContext(ctx, `
			INSERT INTO scan_metadata (
				id, provider, scan_type, services, regions,
				total_resources, failed_resources,
				scan_start_time, scan_end_time, duration_ms,
				metadata
			)
			VALUES (?, ?, 'service', ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?)
		`, id, provider, string(servicesJSON), string(regionsJSON),
			stats.TotalResources, stats.FailedResources, stats.DurationMs, string(metadataJSON))
		return err
	}

	if columns["scan_time"] && columns["service"] && columns["region"] {
		_, err := gs.db.ExecContext(ctx, `
			INSERT INTO scan_metadata (
				id, service, region, scan_time, total_resources, failed_resources, duration_ms, metadata
			)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?)
		`, id, service, region, stats.TotalResources, stats.FailedResources, stats.DurationMs, string(metadataJSON))
		return err
	}

	return fmt.Errorf("scan_metadata table has unsupported schema")
}

func (gs *GraphStore) ensureAWSResourcesTable(ctx context.Context) error {
	_, err := gs.db.ExecContext(ctx, `
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
	return err
}

func (gs *GraphStore) ensureCloudRelationshipsTable(ctx context.Context) error {
	_, err := gs.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cloud_relationships (
			from_id VARCHAR NOT NULL,
			to_id VARCHAR NOT NULL,
			relationship_type VARCHAR NOT NULL,
			provider VARCHAR NOT NULL,
			relationship_subtype VARCHAR,
			properties JSON,
			from_resource_type VARCHAR,
			to_resource_type VARCHAR,
			direction VARCHAR DEFAULT 'outbound',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (from_id, to_id, relationship_type, provider)
		)
	`)
	return err
}

func (gs *GraphStore) ensureUnifiedScanMetadataTable(ctx context.Context) error {
	_, err := gs.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS scan_metadata (
			id VARCHAR PRIMARY KEY,
			provider VARCHAR NOT NULL,
			scan_type VARCHAR NOT NULL,
			services JSON,
			regions JSON,
			accounts JSON,
			total_resources INTEGER DEFAULT 0,
			new_resources INTEGER DEFAULT 0,
			updated_resources INTEGER DEFAULT 0,
			deleted_resources INTEGER DEFAULT 0,
			failed_resources INTEGER DEFAULT 0,
			scan_start_time TIMESTAMP NOT NULL,
			scan_end_time TIMESTAMP,
			duration_ms BIGINT,
			initiated_by VARCHAR,
			scan_reason VARCHAR,
			error_messages JSON,
			warnings JSON,
			metadata JSON,
			status VARCHAR DEFAULT 'running'
		)
	`)
	return err
}

func (gs *GraphStore) scanMetadataColumns(ctx context.Context) (map[string]bool, error) {
	rows, err := gs.db.QueryContext(ctx, "SELECT * FROM scan_metadata LIMIT 0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := make(map[string]bool, len(columns))
	for _, column := range columns {
		result[column] = true
	}
	return result, nil
}

// StoreResources stores cross-cloud resources.
func (gs *GraphStore) StoreResources(resources []*models.Resource) error {
	if len(resources) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_resources (
			id VARCHAR PRIMARY KEY,
			name VARCHAR,
			type VARCHAR,
			service VARCHAR,
			provider VARCHAR,
			region VARCHAR,
			arn VARCHAR,
			status VARCHAR,
			created_at TIMESTAMP,
			modified_at TIMESTAMP,
			scanned_at TIMESTAMP,
			tags JSON,
			attributes JSON,
			metadata JSON,
			raw_data JSON,
			cross_cloud_id VARCHAR
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_resources table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_resources 
		(id, name, type, service, provider, region, arn, status, created_at, modified_at, scanned_at, tags, attributes, metadata, raw_data, cross_cloud_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, resource := range resources {
		tags, _ := json.Marshal(resource.Tags)
		attributes, _ := json.Marshal(resource.Attributes)
		metadata, _ := json.Marshal(resource.Metadata)
		rawData, _ := json.Marshal(resource.RawData)

		_, err := stmt.Exec(
			resource.ID, resource.Name, resource.Type, resource.Service,
			resource.Provider, resource.Region, resource.ARN, resource.Status,
			resource.CreatedAt, resource.ModifiedAt, resource.ScannedAt,
			string(tags), string(attributes), string(metadata), string(rawData),
			resource.CrossCloudID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert resource %s: %w", resource.ID, err)
		}
	}

	return nil
}

// StoreIPAddresses stores IP addresses.
func (gs *GraphStore) StoreIPAddresses(addresses []*models.IPAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_ip_addresses (
			address VARCHAR,
			type VARCHAR,
			version VARCHAR,
			provider VARCHAR,
			region VARCHAR,
			resource_id VARCHAR,
			scope VARCHAR,
			PRIMARY KEY (address, provider, resource_id)
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_ip_addresses table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_ip_addresses 
		(address, type, version, provider, region, resource_id, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, addr := range addresses {
		_, err := stmt.Exec(addr.Address, addr.Type, addr.Version, addr.Provider, addr.Region, addr.ResourceID, addr.Scope)
		if err != nil {
			return fmt.Errorf("failed to insert IP address %s: %w", addr.Address, err)
		}
	}

	return nil
}

// StoreDNSRecords stores DNS records.
func (gs *GraphStore) StoreDNSRecords(records []*models.DNSRecord) error {
	if len(records) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_dns_records (
			name VARCHAR,
			type VARCHAR,
			values JSON,
			ttl INTEGER,
			provider VARCHAR,
			zone VARCHAR,
			resource_id VARCHAR,
			PRIMARY KEY (name, type, provider, resource_id)
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_dns_records table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_dns_records 
		(name, type, values, ttl, provider, zone, resource_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, record := range records {
		values, _ := json.Marshal(record.Values)
		_, err := stmt.Exec(record.Name, record.Type, string(values), record.TTL, record.Provider, record.Zone, record.ResourceID)
		if err != nil {
			return fmt.Errorf("failed to insert DNS record %s: %w", record.Name, err)
		}
	}

	return nil
}

// StoreCorrelations stores cross-cloud correlations.
func (gs *GraphStore) StoreCorrelations(correlations interface{}) error {
	switch corrs := correlations.(type) {
	case []*models.ResourceCorrelation:
		return gs.storeResourceCorrelations(corrs)
	default:
		return gs.storeGenericCorrelations(correlations)
	}
}

func (gs *GraphStore) storeResourceCorrelations(correlations []*models.ResourceCorrelation) error {
	if len(correlations) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_correlations (
			id VARCHAR PRIMARY KEY,
			source_id VARCHAR,
			target_id VARCHAR,
			type VARCHAR,
			relation_type VARCHAR,
			strength DOUBLE,
			confidence DOUBLE,
			description VARCHAR,
			metadata JSON,
			discovered_at TIMESTAMP
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_correlations table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_correlations 
		(id, source_id, target_id, type, relation_type, strength, confidence, description, metadata, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, corr := range correlations {
		metadata, _ := json.Marshal(corr.Metadata)
		_, err := stmt.Exec(
			corr.ID, corr.SourceID, corr.TargetID, corr.Type, corr.RelationType,
			corr.Strength, corr.Confidence, corr.Description, string(metadata), corr.DiscoveredAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert correlation %s: %w", corr.ID, err)
		}
	}

	return nil
}

func (gs *GraphStore) storeGenericCorrelations(correlations interface{}) error {
	correlationsJSON, err := json.Marshal(correlations)
	if err != nil {
		return fmt.Errorf("failed to marshal correlations: %w", err)
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_generic_correlations (
			id VARCHAR PRIMARY KEY,
			correlation_data JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_generic_correlations table: %w", err)
	}

	id := uuid.New().String()
	insertSQL := `INSERT INTO crosscloud_generic_correlations (id, correlation_data) VALUES (?, ?)`
	_, err = gs.db.Exec(insertSQL, id, string(correlationsJSON))
	if err != nil {
		return fmt.Errorf("failed to store correlations: %w", err)
	}

	return nil
}

// GetResourcesByProvider retrieves resources for a specific provider.
func (gs *GraphStore) GetResourcesByProvider(provider string) ([]*models.Resource, error) {
	query := `
		SELECT id, name, type, service, provider, region, arn, status, 
		       created_at, modified_at, scanned_at, tags, attributes, metadata, raw_data, cross_cloud_id
		FROM crosscloud_resources 
		WHERE provider = ?`

	rows, err := gs.db.Query(query, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query resources: %w", err)
	}
	defer rows.Close()

	var resources []*models.Resource
	for rows.Next() {
		resource := &models.Resource{}
		var tags, attributes, metadata, rawData interface{}

		err := rows.Scan(
			&resource.ID, &resource.Name, &resource.Type, &resource.Service,
			&resource.Provider, &resource.Region, &resource.ARN, &resource.Status,
			&resource.CreatedAt, &resource.ModifiedAt, &resource.ScannedAt,
			&tags, &attributes, &metadata, &rawData, &resource.CrossCloudID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}

		if jsonValue, ok := graphJSONScanString(tags); ok {
			json.Unmarshal([]byte(jsonValue), &resource.Tags)
		}
		if jsonValue, ok := graphJSONScanString(attributes); ok {
			json.Unmarshal([]byte(jsonValue), &resource.Attributes)
		}
		if jsonValue, ok := graphJSONScanString(metadata); ok {
			json.Unmarshal([]byte(jsonValue), &resource.Metadata)
		}
		if jsonValue, ok := graphJSONScanString(rawData); ok {
			json.Unmarshal([]byte(jsonValue), &resource.RawData)
		}

		resources = append(resources, resource)
	}

	return resources, rows.Err()
}

// GetIPAddressesByProvider retrieves IP addresses for a specific provider.
func (gs *GraphStore) GetIPAddressesByProvider(provider string) ([]*models.IPAddress, error) {
	query := `
		SELECT address, type, version, provider, region, resource_id, scope
		FROM crosscloud_ip_addresses 
		WHERE provider = ?`

	rows, err := gs.db.Query(query, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query IP addresses: %w", err)
	}
	defer rows.Close()

	var addresses []*models.IPAddress
	for rows.Next() {
		addr := &models.IPAddress{}
		err := rows.Scan(&addr.Address, &addr.Type, &addr.Version, &addr.Provider, &addr.Region, &addr.ResourceID, &addr.Scope)
		if err != nil {
			return nil, fmt.Errorf("failed to scan IP address: %w", err)
		}
		addresses = append(addresses, addr)
	}

	return addresses, rows.Err()
}

// GetDNSRecordsByProvider retrieves DNS records for a specific provider.
func (gs *GraphStore) GetDNSRecordsByProvider(provider string) ([]*models.DNSRecord, error) {
	query := `
		SELECT name, type, values, ttl, provider, zone, resource_id
		FROM crosscloud_dns_records 
		WHERE provider = ?`

	rows, err := gs.db.Query(query, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query DNS records: %w", err)
	}
	defer rows.Close()

	var records []*models.DNSRecord
	for rows.Next() {
		record := &models.DNSRecord{}
		var valuesJSON interface{}
		err := rows.Scan(&record.Name, &record.Type, &valuesJSON, &record.TTL, &record.Provider, &record.Zone, &record.ResourceID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan DNS record: %w", err)
		}

		if jsonValue, ok := graphJSONScanString(valuesJSON); ok {
			json.Unmarshal([]byte(jsonValue), &record.Values)
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

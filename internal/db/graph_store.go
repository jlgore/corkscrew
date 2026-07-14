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
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
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
	"github_resources":     scanResourceAdapterGeneric,
	"cloudflare_resources": scanResourceAdapterGeneric,
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
			created_time, changed_time, etag, api_version,
			region, account_id, arn, attributes, state, created_at, modified_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		resource.Region,
		resource.AccountId,
		resource.Id,
		scanStringJSONOrNil(resource.Attributes),
		"active",
		createdTime,
		changedTime,
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
			discovered_at, scan_id, region, account_id, arn,
			attributes, state, created_at, modified_at, scanned_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			scan_id = EXCLUDED.scan_id,
			region = EXCLUDED.region,
			account_id = EXCLUDED.account_id,
			arn = EXCLUDED.arn,
			attributes = EXCLUDED.attributes,
			state = EXCLUDED.state,
			modified_at = EXCLUDED.modified_at,
			scanned_at = EXCLUDED.scanned_at
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
		resource.Region,
		resource.AccountId,
		resource.Id,
		scanStringJSONOrNil(resource.Attributes),
		"active",
		discoveredAt,
		discoveredAt,
		discoveredAt,
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
	return "", fmt.Errorf("unsupported provider %q: no resource table is registered", resource.Provider)
}

func scanResourceTableForProvider(provider string) string {
	if catalogProvider, ok := providercatalog.Lookup(provider); ok {
		return catalogProvider.ResourceTable
	}
	return ""
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

// StoreScanMetadata stores scan metadata against the canonical schema. Legacy
// shapes are upgraded by EnsureSchema before a GraphStore is constructed.
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

	metadataJSON, _ := json.Marshal(metadata)
	id := uuid.New().String()
	servicesJSON, _ := json.Marshal([]string{service})
	regionsJSON, _ := json.Marshal([]string{region})

	_, err := gs.db.ExecContext(ctx, `
		INSERT INTO scan_metadata (
			id, provider, scan_type, services, regions,
			total_resources, failed_resources,
			scan_start_time, scan_end_time, duration_ms,
			metadata, status
		)
		VALUES (?, ?, 'service', ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, 'completed')
	`, id, provider, string(servicesJSON), string(regionsJSON),
		stats.TotalResources, stats.FailedResources, stats.DurationMs, string(metadataJSON))
	return err
}

// StoreResources stores cross-cloud resources.
func (gs *GraphStore) StoreResources(resources []*models.Resource) error {
	if len(resources) == 0 {
		return nil
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

	insertSQL := `
			INSERT OR REPLACE INTO cross_cloud_ip_addresses
			(id, ip_address, ip_type, ip_version, provider, region, resource_id, resource_type, ip_scope)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'unknown', ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, addr := range addresses {
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{addr.Provider, addr.ResourceID, addr.Address}, "|"))).String()
		region := scanStringOrFallback(addr.Region, "global")
		_, err := stmt.Exec(id, addr.Address, scanStringOrFallback(addr.Type, "unknown"), scanStringOrFallback(addr.Version, "unknown"), addr.Provider, region, addr.ResourceID, addr.Scope)
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

	insertSQL := `
			INSERT OR REPLACE INTO cross_cloud_dns_records
			(id, dns_name, record_type, record_values, ttl, provider, zone_name, resource_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, record := range records {
		values, _ := json.Marshal(record.Values)
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{record.Provider, record.ResourceID, record.Name, record.Type}, "|"))).String()
		_, err := stmt.Exec(id, record.Name, record.Type, string(values), record.TTL, record.Provider, record.Zone, record.ResourceID)
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

	insertSQL := `
			INSERT OR REPLACE INTO cross_cloud_correlations
			(id, source_resource_id, source_provider, source_region, source_resource_type,
			 target_resource_id, target_provider, target_region, target_resource_type,
			 correlation_type, correlation_subtype, correlation_method, confidence_score,
			 description, metadata, discovered_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'legacy_store', ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, corr := range correlations {
		metadata, _ := json.Marshal(corr.Metadata)
		_, err := stmt.Exec(
			corr.ID,
			corr.SourceID, scanStringOrFallback(corr.SourceResource.Provider, "unknown"), corr.SourceResource.Region, corr.SourceResource.Type,
			corr.TargetID, scanStringOrFallback(corr.TargetResource.Provider, "unknown"), corr.TargetResource.Region, corr.TargetResource.Type,
			scanStringOrFallback(corr.Type, "legacy"), corr.RelationType, corr.Confidence,
			corr.Description, string(metadata), corr.DiscoveredAt,
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
		SELECT ip_address, ip_type, ip_version, provider, region, resource_id, ip_scope
		FROM cross_cloud_ip_addresses
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
		SELECT dns_name, record_type, record_values, ttl, provider, zone_name, resource_id
		FROM cross_cloud_dns_records
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

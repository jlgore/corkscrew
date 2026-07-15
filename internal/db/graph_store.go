package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	pb "github.com/jlgore/corkscrew/internal/proto"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

// GraphStore owns scan persistence over an existing database connection: it
// writes provider resources, their canonical relationships, and scan metadata.
// Data sessions use it so scan persistence has one implementation.
type GraphStore struct {
	db                 *sql.DB
	scanWriter         scanStatementExecutor
	decorateScanWriter func(scanStatementExecutor) scanStatementExecutor
}

type scanStatementExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

// StoreScanResourcesOptions controls how scanner protobuf resources are
// persisted into provider resource tables.
type StoreScanResourcesOptions struct {
	ProviderTableOverride string
}

// ScanOutcomeMetadata is the canonical metadata committed with one resource
// and relationship outcome.
type ScanOutcomeMetadata struct {
	ID              string
	Provider        string
	Services        []string
	Regions         []string
	TotalResources  int
	FailedResources int
	StartedAt       time.Time
	EndedAt         time.Time
	DurationMS      int64
	Metadata        map[string]interface{}
	Status          string
}

type scanResourceAdapterKind int

const (
	scanResourceAdapterGeneric scanResourceAdapterKind = iota
	scanResourceAdapterAzure
	scanResourceAdapterGCP
	scanResourceAdapterCustom
)

type scanResourceDestination struct {
	table string
	store func(context.Context, *pb.Resource) error
}

func NewGraphStore(db *sql.DB) *GraphStore {
	return &GraphStore{db: db, scanWriter: db}
}

func (gs *GraphStore) withScanWriter(writer scanStatementExecutor) *GraphStore {
	return &GraphStore{db: gs.db, scanWriter: writer, decorateScanWriter: gs.decorateScanWriter}
}

func (gs *GraphStore) scanExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	writer := gs.scanWriter
	if writer == nil {
		writer = gs.db
	}
	return writer.ExecContext(ctx, query, args...)
}

// StoreScanResources stores scanner protobuf resources in the unified provider
// resource tables and writes their relationships to cloud_relationships.
func (gs *GraphStore) StoreScanResources(ctx context.Context, resources []*pb.Resource, opts StoreScanResourcesOptions) error {
	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scan resource transaction: %w", err)
	}
	defer tx.Rollback()

	if err := gs.withScanWriter(tx).storeScanResources(ctx, resources, opts); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit scan resources: %w", err)
	}
	return nil
}

// StoreScanOutcome commits resources, their canonical relationships, and scan
// metadata as one storage transaction.
func (gs *GraphStore) StoreScanOutcome(ctx context.Context, resources []*pb.Resource, resourceOptions StoreScanResourcesOptions, metadata ScanOutcomeMetadata) error {
	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scan outcome transaction: %w", err)
	}
	defer tx.Rollback()

	writer := scanStatementExecutor(tx)
	if gs.decorateScanWriter != nil {
		writer = gs.decorateScanWriter(writer)
	}
	transactionStore := gs.withScanWriter(writer)
	if err := transactionStore.storeScanResources(ctx, resources, resourceOptions); err != nil {
		return err
	}
	if err := transactionStore.storeScanOutcomeMetadata(ctx, metadata); err != nil {
		return fmt.Errorf("store scan metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit scan outcome: %w", err)
	}
	return nil
}

func (gs *GraphStore) storeScanResources(ctx context.Context, resources []*pb.Resource, opts StoreScanResourcesOptions) error {
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
		key := destination.table + "|" + resource.Provider + "|" + resource.Id
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

	adapter := scanResourceAdapterGeneric
	if table == providercatalog.CustomResourceTable {
		adapter = scanResourceAdapterCustom
	} else if provider, ok := providercatalog.LookupByResourceTable(table); ok {
		switch provider.Name {
		case "azure":
			adapter = scanResourceAdapterAzure
		case "gcp":
			adapter = scanResourceAdapterGCP
		}
	}
	switch adapter {
	case scanResourceAdapterAzure:
		return scanResourceDestination{table: table, store: gs.storeAzureScanResource}, nil
	case scanResourceAdapterGCP:
		return scanResourceDestination{table: table, store: gs.storeGCPScanResource}, nil
	case scanResourceAdapterCustom:
		return scanResourceDestination{table: table, store: gs.storeCustomScanResource}, nil
	default:
		return scanResourceDestination{
			table: table,
			store: func(ctx context.Context, resource *pb.Resource) error {
				return gs.storeGenericScanResource(ctx, table, resource)
			},
		}, nil
	}
}

func (gs *GraphStore) storeCustomScanResource(ctx context.Context, resource *pb.Resource) error {
	if strings.TrimSpace(resource.Provider) == "" {
		return fmt.Errorf("custom resource provider is required")
	}
	tags := scanTagsJSONOrNil(resource.Tags)
	attributes := scanStringJSONOrNil(resource.Attributes)
	rawData := scanStringJSONOrNil(resource.RawData)
	arn := scanStringOrFallback(resource.Arn, resource.Id)
	var createdAt, modifiedAt interface{}
	if resource.CreatedAt != nil {
		createdAt = resource.CreatedAt.AsTime()
	}
	if resource.ModifiedAt != nil {
		modifiedAt = resource.ModifiedAt.AsTime()
	}
	_, err := gs.scanExecContext(ctx, `
		INSERT INTO custom_provider_resources (
			provider, id, name, type, service, region, account_id, arn,
			parent_id, tags, attributes, raw_data, state,
			created_at, modified_at, scanned_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, try_cast(? AS JSON),
		          try_cast(? AS JSON), try_cast(? AS JSON), 'active', ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(provider, id) DO UPDATE SET
			name = excluded.name, type = excluded.type, service = excluded.service,
			region = excluded.region, account_id = excluded.account_id, arn = excluded.arn,
			parent_id = excluded.parent_id, tags = excluded.tags,
			attributes = excluded.attributes, raw_data = excluded.raw_data,
			state = excluded.state, created_at = excluded.created_at,
			modified_at = excluded.modified_at, scanned_at = excluded.scanned_at
	`, resource.Provider, resource.Id, scanStringOrFallback(resource.Name, resource.Id),
		scanStringOrFallback(resource.Type, "unknown"), resource.Service, resource.Region,
		resource.AccountId, arn, resource.ParentId, tags, attributes, rawData, createdAt, modifiedAt)
	if err != nil {
		return fmt.Errorf("insert custom provider resource: %w", err)
	}
	return nil
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

	if _, err := gs.scanExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), resource.Id); err != nil {
		return fmt.Errorf("delete existing: %w", err)
	}
	if _, err := gs.scanExecContext(ctx, fmt.Sprintf(`
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

	_, err := gs.scanExecContext(ctx, `
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

	_, err := gs.scanExecContext(ctx, `
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

		if _, err := gs.scanExecContext(ctx,
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

	return nil
}

func scanResourceTable(resource *pb.Resource, override string) (string, error) {
	if table := strings.TrimSpace(override); table != "" {
		return scanValidatedResourceTable(table, "provider table override")
	}
	if table := scanResourceTableForProvider(resource.Provider); table != "" {
		return table, nil
	}
	if strings.TrimSpace(resource.Provider) == "" {
		return "", fmt.Errorf("resource provider is required")
	}
	return providercatalog.CustomResourceTable, nil
}

func scanResourceTableForProvider(provider string) string {
	if catalogProvider, ok := providercatalog.Lookup(provider); ok {
		return catalogProvider.ResourceTable
	}
	return ""
}

func scanValidatedResourceTable(table string, source string) (string, error) {
	table = strings.TrimSpace(table)
	if !scanValidTableIdentifier(table) {
		return "", fmt.Errorf("invalid %s %q: table names must contain only letters, digits, and underscores, and must not start with a digit", source, table)
	}
	if !providercatalog.IsRegisteredResourceTable(table) {
		return "", fmt.Errorf("invalid %s %q: table is not registered by the official provider catalog", source, table)
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

	convertedMetadata := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		convertedMetadata[key] = value
	}
	return gs.storeScanOutcomeMetadata(ctx, ScanOutcomeMetadata{
		ID: uuid.New().String(), Provider: provider,
		Services: []string{service}, Regions: []string{region},
		TotalResources: int(stats.TotalResources), FailedResources: int(stats.FailedResources),
		DurationMS: stats.DurationMs, Metadata: convertedMetadata, Status: "completed",
	})
}

func (gs *GraphStore) storeScanOutcomeMetadata(ctx context.Context, metadata ScanOutcomeMetadata) error {
	if metadata.ID == "" {
		metadata.ID = uuid.New().String()
	}
	if metadata.Provider == "" {
		metadata.Provider = "unknown"
	}
	if metadata.Status == "" {
		metadata.Status = "completed"
	}
	if metadata.StartedAt.IsZero() {
		metadata.StartedAt = time.Now()
	}
	if metadata.EndedAt.IsZero() {
		metadata.EndedAt = metadata.StartedAt
	}
	if metadata.DurationMS == 0 {
		metadata.DurationMS = metadata.EndedAt.Sub(metadata.StartedAt).Milliseconds()
	}
	metadataJSON, _ := json.Marshal(metadata.Metadata)
	servicesJSON, _ := json.Marshal(metadata.Services)
	regionsJSON, _ := json.Marshal(metadata.Regions)

	_, err := gs.scanExecContext(ctx, `
		INSERT INTO scan_metadata (
			id, provider, scan_type, services, regions,
			total_resources, failed_resources,
			scan_start_time, scan_end_time, duration_ms,
			metadata, status
		)
		VALUES (?, ?, 'multi_scope', ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider = excluded.provider, services = excluded.services,
			regions = excluded.regions, total_resources = excluded.total_resources,
			failed_resources = excluded.failed_resources,
			scan_start_time = excluded.scan_start_time,
			scan_end_time = excluded.scan_end_time,
			duration_ms = excluded.duration_ms, metadata = excluded.metadata,
			status = excluded.status
	`, metadata.ID, metadata.Provider, string(servicesJSON), string(regionsJSON),
		metadata.TotalResources, metadata.FailedResources,
		metadata.StartedAt, metadata.EndedAt, metadata.DurationMS,
		string(metadataJSON), metadata.Status)
	return err
}

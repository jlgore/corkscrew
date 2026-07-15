package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

const LatestSchemaVersion = 3

var schemaIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EnsureSchema upgrades db to the schema understood by this binary. Each
// version is applied atomically and recorded only after all DDL and data moves
// for that version succeed.
func EnsureSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS corkscrew_schema_migrations (
    version INTEGER PRIMARY KEY,
    name VARCHAR NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		return fmt.Errorf("create schema migration table: %w", err)
	}

	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM corkscrew_schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > LatestSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, LatestSchemaVersion)
	}

	if version < 1 {
		if err := migrateSchemaV1(ctx, db, tx); err != nil {
			return fmt.Errorf("migrate schema to version 1: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO corkscrew_schema_migrations(version, name) VALUES (1, 'authoritative_schema_lifecycle')`); err != nil {
			return fmt.Errorf("record schema version 1: %w", err)
		}
		version = 1
	}

	if version < 2 {
		if err := migrateSchemaV2(ctx, db, tx); err != nil {
			return fmt.Errorf("migrate schema to version 2: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO corkscrew_schema_migrations(version, name) VALUES (2, 'custom_provider_resources')`); err != nil {
			return fmt.Errorf("record schema version 2: %w", err)
		}
		version = 2
	}

	if version < 3 {
		if err := migrateSchemaV3(ctx, tx); err != nil {
			return fmt.Errorf("migrate schema to version 3: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO corkscrew_schema_migrations(version, name) VALUES (3, 'normalized_query_helpers')`); err != nil {
			return fmt.Errorf("record schema version 3: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

func migrateSchemaV3(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
CREATE OR REPLACE MACRO extract_json(json_data, json_path) AS
    CASE
        WHEN json_data IS NULL OR json_path IS NULL THEN NULL
        WHEN json_data = '' OR json_data = 'null' THEN NULL
        WHEN NOT json_valid(json_data) THEN NULL
        ELSE json_extract_string(json_data, json_path)
    END;
CREATE OR REPLACE MACRO json_path(json_data, json_path) AS
    CASE
        WHEN json_data IS NULL OR json_path IS NULL THEN NULL
        WHEN json_data = '' OR json_data = 'null' THEN NULL
        WHEN NOT json_valid(json_data) THEN NULL
        ELSE json_extract(json_data, json_path)
    END;
CREATE OR REPLACE MACRO has_tag(tags_data, tag_key, tag_value) AS
    CASE
        WHEN tags_data IS NULL OR tag_key IS NULL THEN FALSE
        WHEN tags_data = '' OR tags_data = 'null' OR tags_data = '{}' THEN FALSE
        WHEN NOT json_valid(tags_data) THEN FALSE
        WHEN tag_value IS NULL THEN
            COALESCE(json_extract_string(tags_data, '$.' || tag_key) IS NOT NULL, FALSE)
        ELSE
            COALESCE(json_extract_string(tags_data, '$.' || tag_key) = tag_value, FALSE)
    END;
CREATE OR REPLACE MACRO count_tags(tags_data) AS
    CASE
        WHEN tags_data IS NULL OR tags_data = '' OR tags_data = 'null' THEN 0
        WHEN NOT json_valid(tags_data) THEN 0
        ELSE COALESCE(array_length(json_keys(tags_data)), 0)
    END;
CREATE OR REPLACE MACRO safe_json_extract(json_data, json_path, default_value) AS
    CASE
        WHEN json_data IS NULL OR json_path IS NULL THEN default_value
        WHEN json_data = '' OR json_data = 'null' THEN default_value
        WHEN NOT json_valid(json_data) THEN default_value
        ELSE COALESCE(json_extract_string(json_data, json_path), default_value)
    END;
`); err != nil {
		return fmt.Errorf("create normalized query helpers: %w", err)
	}
	return nil
}

func migrateSchemaV2(ctx context.Context, db *sql.DB, tx *sql.Tx) error {
	if err := createCustomProviderResources(ctx, tx); err != nil {
		return err
	}
	config := &UnifiedDatabaseConfig{DB: db, schemaRunner: tx}
	if err := config.CreateCrossCloudViews(); err != nil {
		return err
	}
	return nil
}

func createCustomProviderResources(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS custom_provider_resources (
    provider VARCHAR NOT NULL,
    id VARCHAR NOT NULL,
    name VARCHAR,
    type VARCHAR NOT NULL,
    service VARCHAR,
    region VARCHAR,
    account_id VARCHAR,
    arn VARCHAR,
    parent_id VARCHAR,
    tags JSON,
    attributes JSON,
    raw_data JSON,
    state VARCHAR,
    created_at TIMESTAMP,
    modified_at TIMESTAMP,
    scanned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (provider, id)
);
CREATE INDEX IF NOT EXISTS idx_custom_provider_resources_provider ON custom_provider_resources(provider);
CREATE INDEX IF NOT EXISTS idx_custom_provider_resources_type ON custom_provider_resources(type);
CREATE INDEX IF NOT EXISTS idx_custom_provider_resources_region ON custom_provider_resources(region);
`); err != nil {
		return fmt.Errorf("create custom provider resources: %w", err)
	}
	return nil
}

// CurrentSchemaVersion reports the highest migration applied to db.
func CurrentSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database is required")
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) > 0 FROM information_schema.tables
WHERE table_name = 'corkscrew_schema_migrations'`).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM corkscrew_schema_migrations`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func migrateSchemaV1(ctx context.Context, db *sql.DB, tx *sql.Tx) error {
	legacyScan, err := archiveLegacyTable(ctx, tx, "scan_metadata", "scan_metadata_legacy_v0", "provider")
	if err != nil {
		return err
	}
	legacyAPI, err := archiveLegacyTable(ctx, tx, "api_action_metadata", "api_action_metadata_legacy_v0", "provider")
	if err != nil {
		return err
	}

	providers := providercatalog.Names()
	legacyRelationships := make([]string, 0, len(providers))
	for _, provider := range providers {
		name := provider + "_relationships"
		kind, err := schemaObjectKind(ctx, tx, name)
		if err != nil {
			return err
		}
		if kind == "BASE TABLE" {
			archive := name + "_legacy_v0"
			if existing, err := schemaObjectKind(ctx, tx, archive); err != nil {
				return err
			} else if existing != "" {
				return fmt.Errorf("cannot archive %s: %s already exists", name, archive)
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteSchemaIdentifier(name), quoteSchemaIdentifier(archive))); err != nil {
				return fmt.Errorf("archive %s: %w", name, err)
			}
			legacyRelationships = append(legacyRelationships, provider)
		}
	}

	config := &UnifiedDatabaseConfig{DB: db, schemaRunner: tx}
	if err := config.createUnifiedTables(); err != nil {
		return err
	}
	if err := createCompatibilityAggregateTables(ctx, tx); err != nil {
		return err
	}
	if err := ensureProviderGraphColumns(ctx, tx); err != nil {
		return err
	}
	// Fresh databases create the normalized inventory view during version 1.
	// Add the version-2 table before that view; existing version-1 databases
	// receive the same table transactionally in migrateSchemaV2.
	if err := createCustomProviderResources(ctx, tx); err != nil {
		return err
	}

	network := newNetworkSchemaExtensionsWithRunner(db, tx)
	if err := network.CreateNetworkTopologyExtensions(); err != nil {
		return err
	}
	if err := config.CreateCrossCloudViews(); err != nil {
		return err
	}
	if err := config.CreateSecurityViews(); err != nil {
		return err
	}
	if err := network.CreateNetworkAnalyticsViews(); err != nil {
		return err
	}

	if legacyScan {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO scan_metadata (
    id, provider, scan_type, services, regions, total_resources,
    failed_resources, scan_start_time, scan_end_time, duration_ms,
    metadata, status
)
SELECT id,
       COALESCE(json_extract_string(try_cast(metadata AS JSON), '$.provider'), 'unknown'),
       'legacy', json_array(service), json_array(region), total_resources,
       failed_resources, scan_time, scan_time, duration_ms,
       try_cast(metadata AS JSON), 'completed'
FROM scan_metadata_legacy_v0
ON CONFLICT(id) DO NOTHING`); err != nil {
			return fmt.Errorf("copy legacy scan metadata: %w", err)
		}
	}

	if legacyAPI {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO api_action_metadata (
    id, provider, service, operation_name, operation_type, execution_time,
    region, success, duration_ms, resource_count, error_message,
    client_request_id, metadata
)
SELECT id,
       COALESCE(json_extract_string(try_cast(metadata AS JSON), '$.provider'), 'unknown'),
       service, operation_name, operation_type, execution_time,
       region, success, duration_ms, resource_count, error_message,
       request_id, try_cast(metadata AS JSON)
FROM api_action_metadata_legacy_v0
ON CONFLICT(id) DO NOTHING`); err != nil {
			return fmt.Errorf("copy legacy API action metadata: %w", err)
		}
	}

	for _, provider := range legacyRelationships {
		archive := provider + "_relationships_legacy_v0"
		query := fmt.Sprintf(`
INSERT INTO cloud_relationships(from_id, to_id, relationship_type, provider, properties)
SELECT from_id, to_id, relationship_type, ?, try_cast(properties AS JSON)
FROM %s
ON CONFLICT(from_id, to_id, relationship_type, provider) DO UPDATE SET
    properties = excluded.properties`, quoteSchemaIdentifier(archive))
		if _, err := tx.ExecContext(ctx, query, provider); err != nil {
			return fmt.Errorf("copy legacy %s relationships: %w", provider, err)
		}
	}

	if err := migrateLegacyCrossCloudRows(ctx, tx); err != nil {
		return err
	}

	for _, provider := range providers {
		name := provider + "_relationships"
		query := fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS
SELECT from_id, to_id, relationship_type, properties
FROM cloud_relationships WHERE lower(provider) = '%s'`, quoteSchemaIdentifier(name), provider)
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("create %s relationship view: %w", provider, err)
		}
	}

	return nil
}

func createCompatibilityAggregateTables(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS crosscloud_resources (
            id VARCHAR PRIMARY KEY, name VARCHAR, type VARCHAR, service VARCHAR,
            provider VARCHAR, region VARCHAR, arn VARCHAR, status VARCHAR,
            created_at TIMESTAMP, modified_at TIMESTAMP, scanned_at TIMESTAMP,
            tags JSON, attributes JSON, metadata JSON, raw_data JSON,
            cross_cloud_id VARCHAR
        )`,
		`CREATE TABLE IF NOT EXISTS crosscloud_generic_correlations (
            id VARCHAR PRIMARY KEY, correlation_data JSON,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create compatibility aggregate table: %w", err)
		}
	}
	return nil
}

func archiveLegacyTable(ctx context.Context, tx *sql.Tx, table, archive, canonicalColumn string) (bool, error) {
	kind, err := schemaObjectKind(ctx, tx, table)
	if err != nil || kind == "" {
		return false, err
	}
	if kind != "BASE TABLE" {
		return false, fmt.Errorf("expected %s to be a table, found %s", table, kind)
	}
	columns, err := schemaColumns(ctx, tx, table)
	if err != nil {
		return false, err
	}
	if columns[canonicalColumn] {
		return false, nil
	}
	if existing, err := schemaObjectKind(ctx, tx, archive); err != nil {
		return false, err
	} else if existing != "" {
		return false, fmt.Errorf("cannot archive %s: %s already exists", table, archive)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteSchemaIdentifier(table), quoteSchemaIdentifier(archive))); err != nil {
		return false, fmt.Errorf("archive %s: %w", table, err)
	}
	return true, nil
}

func schemaObjectKind(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	var kind string
	err := tx.QueryRowContext(ctx, `SELECT table_type FROM information_schema.tables WHERE table_name = ? LIMIT 1`, name).Scan(&kind)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect schema object %s: %w", name, err)
	}
	return strings.ToUpper(kind), nil
}

func schemaColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_name = ?`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}

func quoteSchemaIdentifier(name string) string {
	if !schemaIdentifier.MatchString(name) {
		panic("unsafe schema identifier: " + name)
	}
	return `"` + name + `"`
}

func ensureProviderGraphColumns(ctx context.Context, tx *sql.Tx) error {
	columns := []struct{ name, dataType string }{
		{"service", "VARCHAR"}, {"region", "VARCHAR"}, {"account_id", "VARCHAR"},
		{"arn", "VARCHAR"}, {"tags", "JSON"}, {"attributes", "JSON"},
		{"raw_data", "JSON"}, {"state", "VARCHAR"}, {"created_at", "TIMESTAMP"},
		{"modified_at", "TIMESTAMP"}, {"scanned_at", "TIMESTAMP"},
	}
	for _, provider := range providercatalog.Names() {
		table := provider + "_resources"
		for _, column := range columns {
			query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", quoteSchemaIdentifier(table), quoteSchemaIdentifier(column.name), column.dataType)
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return fmt.Errorf("add graph column %s.%s: %w", table, column.name, err)
			}
		}
	}

	backfills := []string{
		`UPDATE aws_resources SET arn = COALESCE(arn, id), state = COALESCE(state, 'active'), scanned_at = COALESCE(scanned_at, CURRENT_TIMESTAMP)`,
		`UPDATE azure_resources SET region = COALESCE(region, location), account_id = COALESCE(account_id, subscription_id), arn = COALESCE(arn, resource_id, id), state = COALESCE(state, provisioning_state, 'active'), created_at = COALESCE(created_at, created_time), modified_at = COALESCE(modified_at, changed_time), scanned_at = COALESCE(scanned_at, CURRENT_TIMESTAMP)`,
		`UPDATE gcp_resources SET region = COALESCE(region, location), account_id = COALESCE(account_id, project_id), arn = COALESCE(arn, id), state = COALESCE(state, 'active'), scanned_at = COALESCE(scanned_at, discovered_at, CURRENT_TIMESTAMP)`,
		`UPDATE kubernetes_resources SET arn = COALESCE(arn, id), state = COALESCE(state, 'active'), scanned_at = COALESCE(scanned_at, CURRENT_TIMESTAMP)`,
		`UPDATE github_resources SET region = COALESCE(region, 'global'), account_id = COALESCE(account_id, org), arn = COALESCE(arn, id), state = COALESCE(state, 'active'), scanned_at = COALESCE(scanned_at, discovered_at, CURRENT_TIMESTAMP)`,
		`UPDATE cloudflare_resources SET region = COALESCE(region, 'global'), arn = COALESCE(arn, id), state = COALESCE(state, 'active'), scanned_at = COALESCE(scanned_at, discovered_at, CURRENT_TIMESTAMP)`,
	}
	for _, query := range backfills {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("backfill provider graph columns: %w", err)
		}
	}
	return nil
}

func migrateLegacyCrossCloudRows(ctx context.Context, tx *sql.Tx) error {
	legacyIP, err := archiveLegacyNamedTable(ctx, tx, "crosscloud_ip_addresses")
	if err != nil {
		return err
	}
	if legacyIP {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO cross_cloud_ip_addresses (
    id, ip_address, ip_version, ip_type, ip_scope, resource_id,
    resource_type, provider, region
)
SELECT md5(concat_ws('|', provider, resource_id, address)), address,
       COALESCE(version, 'unknown'), COALESCE(type, 'unknown'), scope,
       resource_id, 'unknown', provider, COALESCE(region, 'global')
FROM crosscloud_ip_addresses_legacy_v0 ON CONFLICT(id) DO NOTHING`); err != nil {
			return fmt.Errorf("copy legacy IP addresses: %w", err)
		}
	}

	legacyDNS, err := archiveLegacyNamedTable(ctx, tx, "crosscloud_dns_records")
	if err != nil {
		return err
	}
	if legacyDNS {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO cross_cloud_dns_records (
    id, dns_name, record_type, record_values, ttl, resource_id,
    provider, zone_name
)
SELECT md5(concat_ws('|', provider, resource_id, name, type)), name, type,
       try_cast(values AS JSON), ttl, resource_id, provider, zone
FROM crosscloud_dns_records_legacy_v0 ON CONFLICT(id) DO NOTHING`); err != nil {
			return fmt.Errorf("copy legacy DNS records: %w", err)
		}
	}

	legacyCorrelations, err := archiveLegacyNamedTable(ctx, tx, "crosscloud_correlations")
	if err != nil {
		return err
	}
	if legacyCorrelations {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO cross_cloud_correlations (
    id, source_resource_id, source_provider,
    target_resource_id, target_provider,
    correlation_type, correlation_subtype, correlation_method,
    confidence_score, evidence, description, metadata,
    discovered_at, created_at, updated_at
)
SELECT id, source_id,
       CASE WHEN strpos(source_id, ':') > 0 THEN lower(split_part(source_id, ':', 1)) ELSE 'unknown' END,
       target_id,
       CASE WHEN strpos(target_id, ':') > 0 THEN lower(split_part(target_id, ':', 1)) ELSE 'unknown' END,
       COALESCE(NULLIF(type, ''), 'legacy'), relation_type, 'legacy_graph_store',
       COALESCE(confidence, strength, 0), try_cast(metadata AS JSON), description,
       try_cast(metadata AS JSON), discovered_at,
       COALESCE(discovered_at, CURRENT_TIMESTAMP), COALESCE(discovered_at, CURRENT_TIMESTAMP)
FROM crosscloud_correlations_legacy_v0
ON CONFLICT(id) DO NOTHING`); err != nil {
			return fmt.Errorf("copy legacy correlations: %w", err)
		}
	}
	return nil
}

func archiveLegacyNamedTable(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	kind, err := schemaObjectKind(ctx, tx, table)
	if err != nil || kind == "" {
		return false, err
	}
	if kind != "BASE TABLE" {
		return false, fmt.Errorf("expected %s to be a table, found %s", table, kind)
	}
	archive := table + "_legacy_v0"
	if existing, err := schemaObjectKind(ctx, tx, archive); err != nil {
		return false, err
	} else if existing != "" {
		return false, fmt.Errorf("cannot archive %s: %s already exists", table, archive)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteSchemaIdentifier(table), quoteSchemaIdentifier(archive))); err != nil {
		return false, fmt.Errorf("archive %s: %w", table, err)
	}
	return true, nil
}

package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type migrationInvariant struct {
	name   string
	before string
	after  string
}

type migrationArchive struct {
	name string
	rows int
}

type migrationFixture struct {
	name       string
	invariants []migrationInvariant
	archives   []migrationArchive
}

func loadMigrationFixture(t *testing.T, database *sql.DB, name string) {
	t.Helper()
	path := filepath.Join("testdata", "migrations", name+".sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration fixture %s: %v", name, err)
	}
	if _, err := database.ExecContext(context.Background(), string(contents)); err != nil {
		t.Fatalf("apply migration fixture %s: %v", name, err)
	}
}

func migrationScalar(t *testing.T, database *sql.DB, query string) string {
	t.Helper()
	var value any
	if err := database.QueryRowContext(context.Background(), query).Scan(&value); err != nil {
		t.Fatalf("query migration invariant %q: %v", query, err)
	}
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func TestVersionZeroMigrationFixtures(t *testing.T) {
	fixtures := []migrationFixture{
		{
			name: "v0_minimal",
			invariants: []migrationInvariant{
				{
					name:   "unknown table",
					before: `SELECT note FROM legacy_fixture_marker WHERE id = 1`,
					after:  `SELECT note FROM legacy_fixture_marker WHERE id = 1`,
				},
			},
		},
		{
			name: "v0_populated",
			invariants: []migrationInvariant{
				{
					name: "AWS resource fields",
					before: `SELECT concat_ws('|', id, type, name, region, account_id,
					          json_extract_string(tags, '$.env')) FROM aws_resources`,
					after: `SELECT concat_ws('|', id, type, name, region, account_id,
					         json_extract_string(tags, '$.env')) FROM aws_resources`,
				},
				{
					name:   "scan row totals",
					before: `SELECT concat_ws('|', count(*), sum(total_resources), sum(failed_resources), sum(duration_ms)) FROM scan_metadata`,
					after:  `SELECT concat_ws('|', count(*), sum(total_resources), sum(failed_resources), sum(duration_ms)) FROM scan_metadata`,
				},
				{
					name:   "API action fields",
					before: `SELECT concat_ws('|', id, service, operation_name, success, request_id, resource_count) FROM api_action_metadata`,
					after:  `SELECT concat_ws('|', id, service, operation_name, success, client_request_id, resource_count) FROM api_action_metadata`,
				},
				{
					name:   "provider relationship",
					before: `SELECT concat_ws('|', from_id, to_id, relationship_type, json_extract_string(properties, '$.source')) FROM aws_relationships`,
					after:  `SELECT concat_ws('|', from_id, to_id, relationship_type, json_extract_string(properties, '$.source')) FROM cloud_relationships WHERE provider = 'aws'`,
				},
				{
					name:   "aggregate resource",
					before: `SELECT concat_ws('|', id, provider, cross_cloud_id, json_extract_string(attributes, '$.encrypted')) FROM crosscloud_resources`,
					after:  `SELECT concat_ws('|', id, provider, cross_cloud_id, json_extract_string(attributes, '$.encrypted')) FROM crosscloud_resources`,
				},
				{
					name:   "IP addresses",
					before: `SELECT string_agg(concat_ws('|', address, type, version, provider, region, resource_id, scope), ',' ORDER BY address) FROM crosscloud_ip_addresses`,
					after:  `SELECT string_agg(concat_ws('|', ip_address, ip_type, ip_version, provider, region, resource_id, ip_scope), ',' ORDER BY ip_address) FROM cross_cloud_ip_addresses`,
				},
				{
					name:   "DNS records",
					before: `SELECT string_agg(concat_ws('|', name, type, provider, zone, resource_id, json_extract_string(values, '$[0]')), ',' ORDER BY name) FROM crosscloud_dns_records`,
					after:  `SELECT string_agg(concat_ws('|', dns_name, record_type, provider, zone_name, resource_id, json_extract_string(record_values, '$[0]')), ',' ORDER BY dns_name) FROM cross_cloud_dns_records`,
				},
				{
					name:   "typed correlation",
					before: `SELECT concat_ws('|', id, source_id, target_id, type, relation_type, confidence, description) FROM crosscloud_correlations`,
					after:  `SELECT concat_ws('|', id, source_resource_id, target_resource_id, correlation_type, correlation_subtype, confidence_score, description) FROM cross_cloud_correlations`,
				},
				{
					name:   "generic correlation",
					before: `SELECT concat_ws('|', id, json_extract_string(correlation_data, '$.kind'), json_extract_string(correlation_data, '$.count')) FROM crosscloud_generic_correlations`,
					after:  `SELECT concat_ws('|', id, json_extract_string(correlation_data, '$.kind'), json_extract_string(correlation_data, '$.count')) FROM crosscloud_generic_correlations`,
				},
			},
			archives: []migrationArchive{
				{name: "scan_metadata_legacy_v0", rows: 2},
				{name: "api_action_metadata_legacy_v0", rows: 1},
				{name: "aws_relationships_legacy_v0", rows: 1},
				{name: "crosscloud_ip_addresses_legacy_v0", rows: 2},
				{name: "crosscloud_dns_records_legacy_v0", rows: 2},
				{name: "crosscloud_correlations_legacy_v0", rows: 1},
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+".duckdb")
			database, err := sql.Open("duckdb", path)
			if err != nil {
				t.Fatalf("open fixture database: %v", err)
			}
			loadMigrationFixture(t, database, fixture.name)

			before := make(map[string]string, len(fixture.invariants))
			for _, invariant := range fixture.invariants {
				before[invariant.name] = migrationScalar(t, database, invariant.before)
			}
			if err := database.Close(); err != nil {
				t.Fatalf("close seeded fixture: %v", err)
			}

			// Use the production entry point rather than invoking EnsureSchema
			// directly. This verifies automatic migration during normal opens.
			config, err := InitializeUnifiedDatabase(path)
			if err != nil {
				t.Fatalf("open and migrate fixture: %v", err)
			}
			t.Cleanup(func() { _ = config.Close() })

			for _, invariant := range fixture.invariants {
				if got := migrationScalar(t, config.DB, invariant.after); got != before[invariant.name] {
					t.Errorf("%s changed: before=%q after=%q", invariant.name, before[invariant.name], got)
				}
			}
			for _, archive := range fixture.archives {
				var count int
				if err := config.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ? AND table_type = 'BASE TABLE'`, archive.name).Scan(&count); err != nil {
					t.Fatalf("inspect archive %s: %v", archive.name, err)
				}
				if count != 1 {
					t.Errorf("archive %s object count = %d, want 1", archive.name, count)
					continue
				}
				if got := migrationScalar(t, config.DB, `SELECT COUNT(*) FROM `+archive.name); got != fmt.Sprint(archive.rows) {
					t.Errorf("archive %s row count = %s, want %d", archive.name, got, archive.rows)
				}
			}

			version, err := CurrentSchemaVersion(context.Background(), config.DB)
			if err != nil || version != LatestSchemaVersion {
				t.Fatalf("schema version = %d, %v; want %d", version, err, LatestSchemaVersion)
			}
			if err := EnsureSchema(context.Background(), config.DB); err != nil {
				t.Fatalf("second migration pass: %v", err)
			}
			var migrationRows int
			if err := config.DB.QueryRow(`SELECT COUNT(*) FROM corkscrew_schema_migrations`).Scan(&migrationRows); err != nil {
				t.Fatalf("count migration rows: %v", err)
			}
			if migrationRows != 1 {
				t.Fatalf("migration rows = %d, want 1", migrationRows)
			}

			if err := config.Close(); err != nil {
				t.Fatalf("close migrated fixture: %v", err)
			}
			config, err = InitializeUnifiedDatabase(path)
			if err != nil {
				t.Fatalf("reopen migrated fixture: %v", err)
			}
			for _, invariant := range fixture.invariants {
				if got := migrationScalar(t, config.DB, invariant.after); got != before[invariant.name] {
					t.Errorf("%s changed after reopen: before=%q after=%q", invariant.name, before[invariant.name], got)
				}
			}
			if err := config.DB.QueryRow(`SELECT COUNT(*) FROM corkscrew_schema_migrations`).Scan(&migrationRows); err != nil {
				t.Fatalf("count migration rows after reopen: %v", err)
			}
			if migrationRows != 1 {
				t.Fatalf("migration rows after reopen = %d, want 1", migrationRows)
			}
		})
	}
}

func TestVersionZeroConflictFixtureRollsBack(t *testing.T) {
	database := openLifecycleTestDB(t)
	loadMigrationFixture(t, database, "v0_conflict")

	err := EnsureSchema(context.Background(), database)
	if err == nil {
		t.Fatal("EnsureSchema succeeded, want archive conflict")
	}

	if got := migrationScalar(t, database, `SELECT id FROM scan_metadata`); got != "must-survive-rollback" {
		t.Fatalf("original row after rollback = %q", got)
	}
	version, versionErr := CurrentSchemaVersion(context.Background(), database)
	if versionErr != nil || version != 0 {
		t.Fatalf("schema version after rollback = %d, %v; want 0", version, versionErr)
	}
	var newObjectCount int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_name IN ('aws_resources', 'cloud_relationships', 'corkscrew_schema_migrations')
	`).Scan(&newObjectCount); err != nil {
		t.Fatalf("inspect objects after rollback: %v", err)
	}
	if newObjectCount != 0 {
		t.Fatalf("new schema objects after rollback = %d, want 0", newObjectCount)
	}
}

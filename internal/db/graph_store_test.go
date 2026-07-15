package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func openTestGraphStore(t *testing.T) (*sql.DB, *GraphStore) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "graph-store.duckdb")
	database, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	if err := EnsureSchema(context.Background(), database); err != nil {
		_ = database.Close()
		t.Fatalf("ensure schema: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	return database, NewGraphStore(database)
}

func TestGraphStoreScanResources(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "scan-resources.duckdb")
	cfg, err := initializeTestUnifiedDatabase(dbPath)
	if err != nil {
		t.Fatalf("initialize unified database: %v", err)
	}
	t.Cleanup(func() {
		_ = cfg.DB.Close()
	})

	store := NewGraphStore(cfg.DB)
	discoveredAt := timestamppb.New(time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC))
	resources := []*pb.Resource{
		{
			Provider:     "aws",
			Service:      "s3",
			Type:         "AWS::S3::Bucket",
			Id:           "bucket-1",
			Name:         "bucket-1",
			Region:       "us-east-1",
			AccountId:    "123456789012",
			Tags:         map[string]string{"env": "test"},
			Attributes:   `{"versioning":true}`,
			RawData:      `{"name":"bucket-1"}`,
			DiscoveredAt: discoveredAt,
			Relationships: []*pb.Relationship{
				{
					TargetId:         "cluster/default/Secret/db-creds",
					RelationshipType: "uses",
					Properties:       map[string]string{"reason": "encryption"},
				},
				{
					TargetId:         "cluster/default/Secret/db-creds",
					RelationshipType: "uses",
					Properties:       map[string]string{"reason": "duplicate"},
				},
			},
		},
		{
			Provider:     "aws",
			Service:      "s3",
			Type:         "AWS::S3::Bucket",
			Id:           "bucket-1",
			Name:         "duplicate-bucket-1",
			Region:       "us-east-1",
			DiscoveredAt: discoveredAt,
		},
		{
			Provider:     "custom",
			Service:      "inventory",
			Type:         "Custom::Inventory",
			Id:           "custom-1",
			Name:         "custom-1",
			Attributes:   `{"_table":"custom_scan_resources","kind":"inventory"}`,
			RawData:      `{"name":"custom-1"}`,
			DiscoveredAt: discoveredAt,
		},
	}

	if err := store.StoreScanResources(context.Background(), resources, StoreScanResourcesOptions{}); err != nil {
		t.Fatalf("store scan resources: %v", err)
	}

	var arn, state string
	if err := cfg.DB.QueryRow(`SELECT arn, state FROM aws_resources WHERE id = ?`, "bucket-1").Scan(&arn, &state); err != nil {
		t.Fatalf("query aws resource: %v", err)
	}
	if arn != "bucket-1" {
		t.Fatalf("expected blank ARN to fall back to id, got %q", arn)
	}
	if state != "active" {
		t.Fatalf("expected active state, got %q", state)
	}

	var awsCount int
	if err := cfg.DB.QueryRow(`SELECT COUNT(*) FROM aws_resources WHERE id = ?`, "bucket-1").Scan(&awsCount); err != nil {
		t.Fatalf("count aws resources: %v", err)
	}
	if awsCount != 1 {
		t.Fatalf("expected duplicate aws resource to be suppressed, got %d", awsCount)
	}

	var customCount int
	if err := cfg.DB.QueryRow(`SELECT COUNT(*) FROM custom_provider_resources WHERE provider = ? AND id = ?`, "custom", "custom-1").Scan(&customCount); err != nil {
		t.Fatalf("count custom resources: %v", err)
	}
	if customCount != 1 {
		t.Fatalf("expected generic custom resource count 1, got %d", customCount)
	}

	var relationshipCount int
	if err := cfg.DB.QueryRow(`
		SELECT COUNT(*)
		FROM cloud_relationships
		WHERE from_id = ? AND to_id = ? AND relationship_type = ? AND provider = ? AND to_resource_type = ?
	`, "bucket-1", "cluster/default/Secret/db-creds", "uses", "aws", "Secret").Scan(&relationshipCount); err != nil {
		t.Fatalf("query scan relationship: %v", err)
	}
	if relationshipCount != 1 {
		t.Fatalf("expected deduped relationship count 1, got %d", relationshipCount)
	}

	if err := store.StoreScanResources(context.Background(), resources[:1], StoreScanResourcesOptions{}); err != nil {
		t.Fatalf("store scan resources second time: %v", err)
	}
	if err := cfg.DB.QueryRow(`
		SELECT COUNT(*)
		FROM cloud_relationships
		WHERE from_id = ? AND to_id = ? AND relationship_type = ? AND provider = ?
	`, "bucket-1", "cluster/default/Secret/db-creds", "uses", "aws").Scan(&relationshipCount); err != nil {
		t.Fatalf("query scan relationship after second store: %v", err)
	}
	if relationshipCount != 1 {
		t.Fatalf("expected relationship upsert count 1, got %d", relationshipCount)
	}
}

func TestGraphStorePersistsEveryShippedProvider(t *testing.T) {
	database, store := openTestGraphStore(t)
	ctx := context.Background()
	resources := []*pb.Resource{
		{Provider: "aws", Id: "aws:bucket:one", Name: "one", Type: "AWS::S3::Bucket", Service: "s3", Region: "us-east-1", AccountId: "111"},
		{Provider: "azure", Id: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/one", Name: "one", Type: "Microsoft.Compute/virtualMachines", Service: "compute", Region: "eastus", AccountId: "sub"},
		{Provider: "gcp", Id: "//compute.googleapis.com/projects/project/zones/us-central1-a/instances/one", Name: "one", Type: "compute.googleapis.com/Instance", Service: "compute", Region: "us-central1-a", AccountId: "project"},
		{Provider: "kubernetes", Id: "cluster/default/Pod/one", Name: "one", Type: "Pod", Service: "v1", Region: "default", AccountId: "cluster"},
		{Provider: "github", Id: "github:repo:one", Name: "one", Type: "repository", Service: "repos", Region: "global", AccountId: "example"},
		{Provider: "cloudflare", Id: "cloudflare:worker:one", Name: "one", Type: "worker", Service: "workers", Region: "global", AccountId: "account"},
	}
	if err := store.StoreScanResources(ctx, resources, StoreScanResourcesOptions{}); err != nil {
		t.Fatalf("store resources: %v", err)
	}

	var total int
	if err := database.QueryRow(`SELECT COUNT(*) FROM all_cloud_resources`).Scan(&total); err != nil {
		t.Fatalf("query unified resources: %v", err)
	}
	if total != len(resources) {
		t.Fatalf("unified resource count = %d, want %d", total, len(resources))
	}

	for _, resource := range resources {
		var count int
		table := scanResourceTableForProvider(resource.Provider)
		if err := database.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id = ?`, resource.Id).Scan(&count); err != nil {
			t.Fatalf("query %s: %v", resource.Provider, err)
		}
		if count != 1 {
			t.Fatalf("%s resource count = %d, want 1", resource.Provider, count)
		}
	}

	custom := []*pb.Resource{
		{Provider: "third-party", Id: "shared-id", Name: "one", Type: "widget", Service: "inventory", Region: "global"},
		{Provider: "another-party", Id: "shared-id", Name: "two", Type: "widget", Service: "inventory", Region: "global"},
	}
	if err := store.StoreScanResources(ctx, custom, StoreScanResourcesOptions{}); err != nil {
		t.Fatalf("store custom resources: %v", err)
	}
	var customCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM custom_provider_resources WHERE id = 'shared-id'`).Scan(&customCount); err != nil {
		t.Fatalf("query custom resources: %v", err)
	}
	if customCount != 2 {
		t.Fatalf("custom resource count = %d, want provider-isolated count 2", customCount)
	}
}

func TestGraphStoreScanResourcesProviderAdapters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "provider-adapters.duckdb")
	cfg, err := initializeTestUnifiedDatabase(dbPath)
	if err != nil {
		t.Fatalf("initialize unified database: %v", err)
	}
	t.Cleanup(func() {
		_ = cfg.DB.Close()
	})

	store := NewGraphStore(cfg.DB)
	discoveredAt := timestamppb.New(time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC))
	resources := []*pb.Resource{
		{
			Provider:  "azure",
			Service:   "storage",
			Type:      "Microsoft.Storage/storageAccounts",
			Id:        "/subscriptions/sub-1/resourceGroups/rg-prod/providers/Microsoft.Storage/storageAccounts/acct1",
			Name:      "acct1",
			Region:    "eastus",
			AccountId: "sub-1",
			RawData: `{
				"properties": {"provisioningState": "Succeeded"},
				"sku": {"name": "Standard_LRS", "tier": "Standard", "capacity": 2},
				"etag": "etag-1",
				"apiVersion": "2024-01-01",
				"createdTime": "2026-07-11T12:00:00Z",
				"changedTime": "2026-07-11T12:05:00Z"
			}`,
			Tags:         map[string]string{"env": "prod"},
			DiscoveredAt: discoveredAt,
		},
		{
			Provider:     "gcp",
			Service:      "compute",
			Type:         "compute.googleapis.com/Instance",
			Id:           "projects/proj-1/zones/us-central1-a/instances/vm-1",
			Name:         "vm-1",
			Region:       "us-central1-a",
			Tags:         map[string]string{"env": "prod"},
			Attributes:   `{"labels":{"team":"platform"},"scan_id":"scan-test"}`,
			RawData:      `{"name":"vm-1"}`,
			DiscoveredAt: discoveredAt,
		},
	}

	if err := store.StoreScanResources(context.Background(), resources, StoreScanResourcesOptions{}); err != nil {
		t.Fatalf("store provider scan resources: %v", err)
	}

	var subscriptionID, resourceGroup, skuName, etag, apiVersion string
	var skuCapacity int
	if err := cfg.DB.QueryRow(`
		SELECT subscription_id, resource_group, sku_name, sku_capacity, etag, api_version
		FROM azure_resources
		WHERE id = ?
	`, resources[0].Id).Scan(&subscriptionID, &resourceGroup, &skuName, &skuCapacity, &etag, &apiVersion); err != nil {
		t.Fatalf("query azure resource: %v", err)
	}
	if subscriptionID != "sub-1" || resourceGroup != "rg-prod" || skuName != "Standard_LRS" || skuCapacity != 2 || etag != "etag-1" || apiVersion != "2024-01-01" {
		t.Fatalf("unexpected azure row: subscription=%q group=%q sku=%q capacity=%d etag=%q api=%q",
			subscriptionID, resourceGroup, skuName, skuCapacity, etag, apiVersion)
	}

	var projectID, location, scanID string
	if err := cfg.DB.QueryRow(`
		SELECT project_id, location, scan_id
		FROM gcp_resources
		WHERE id = ?
	`, resources[1].Id).Scan(&projectID, &location, &scanID); err != nil {
		t.Fatalf("query gcp resource: %v", err)
	}
	if projectID != "proj-1" || location != "us-central1-a" || scanID != "scan-test" {
		t.Fatalf("unexpected gcp row: project=%q location=%q scan=%q", projectID, location, scanID)
	}
}

func TestGraphStoreScanResourcesRejectsInvalidTableOverrides(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "invalid-overrides.duckdb")
	cfg, err := initializeTestUnifiedDatabase(dbPath)
	if err != nil {
		t.Fatalf("initialize unified database: %v", err)
	}
	t.Cleanup(func() {
		_ = cfg.DB.Close()
	})

	store := NewGraphStore(cfg.DB)
	resource := &pb.Resource{
		Provider: "aws",
		Service:  "s3",
		Type:     "AWS::S3::Bucket",
		Id:       "bucket-1",
		Name:     "bucket-1",
	}

	err = store.StoreScanResources(context.Background(), []*pb.Resource{resource}, StoreScanResourcesOptions{
		ProviderTableOverride: "aws_resources; DROP TABLE aws_resources",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid provider table override") {
		t.Fatalf("expected invalid provider table override error, got %v", err)
	}

	err = store.StoreScanResources(context.Background(), []*pb.Resource{resource}, StoreScanResourcesOptions{
		ProviderTableOverride: "arbitrary_resources",
	})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected unregistered table override error, got %v", err)
	}

	var count int
	if err := cfg.DB.QueryRow(`SELECT COUNT(*) FROM aws_resources WHERE id = ?`, resource.Id).Scan(&count); err != nil {
		t.Fatalf("query aws resources after rejected overrides: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected rejected overrides to store no rows, got %d", count)
	}
}

func TestGraphStoreScanMetadataSupportsLegacySchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-metadata.duckdb")
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE scan_metadata (
			id VARCHAR PRIMARY KEY,
			service VARCHAR NOT NULL,
			region VARCHAR NOT NULL,
			scan_time TIMESTAMP NOT NULL,
			total_resources INTEGER,
			failed_resources INTEGER,
			duration_ms BIGINT,
			metadata JSON
		)
	`); err != nil {
		t.Fatalf("create legacy scan metadata table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scan_metadata VALUES
		('legacy-scan', 's3', 'us-east-1', CURRENT_TIMESTAMP, 5, 0, 42, '{"provider":"aws"}')
	`); err != nil {
		t.Fatalf("seed legacy scan metadata: %v", err)
	}
	if err := EnsureSchema(ctx, db); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	store := NewGraphStore(db)

	stats := &pb.ScanStats{TotalResources: 7, FailedResources: 1, DurationMs: 1234}
	if err := store.StoreScanMetadata(ctx, "", "s3", "us-east-1", stats, map[string]string{"provider": "aws"}); err != nil {
		t.Fatalf("store legacy scan metadata: %v", err)
	}

	var provider string
	var totalResources int
	if err := db.QueryRow(`
		SELECT provider, total_resources
		FROM scan_metadata
		WHERE id = 'legacy-scan'
	`).Scan(&provider, &totalResources); err != nil {
		t.Fatalf("query legacy scan metadata: %v", err)
	}
	if provider != "aws" || totalResources != 5 {
		t.Fatalf("unexpected migrated metadata: provider=%q total=%d", provider, totalResources)
	}
	var archiveCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM scan_metadata_legacy_v0`).Scan(&archiveCount); err != nil {
		t.Fatalf("query legacy archive: %v", err)
	}
	if archiveCount != 1 {
		t.Fatalf("legacy archive count = %d, want 1", archiveCount)
	}
}

func TestGraphStoreScanMetadataCreatesUnifiedSchema(t *testing.T) {
	db, store := openTestGraphStore(t)
	ctx := context.Background()

	stats := &pb.ScanStats{TotalResources: 3, FailedResources: 0, DurationMs: 99}
	if err := store.StoreScanMetadata(ctx, "azure", "compute", "eastus", stats, nil); err != nil {
		t.Fatalf("store unified scan metadata: %v", err)
	}

	var provider string
	var totalResources int
	if err := db.QueryRow(`
		SELECT provider, total_resources
		FROM scan_metadata
		LIMIT 1
	`).Scan(&provider, &totalResources); err != nil {
		t.Fatalf("query unified scan metadata: %v", err)
	}
	if provider != "azure" || totalResources != 3 {
		t.Fatalf("unexpected unified scan metadata: provider=%q total=%d", provider, totalResources)
	}
}

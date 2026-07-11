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
	"github.com/jlgore/corkscrew/pkg/models"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func openTestGraphStore(t *testing.T) (*sql.DB, *GraphStore) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "graph-store.duckdb")
	database, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	return database, NewGraphStore(database)
}

func TestGraphStoreResourceRoundTrip(t *testing.T) {
	_, store := openTestGraphStore(t)

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	resources := []*models.Resource{
		{
			ID:           "aws:bucket:one",
			Name:         "bucket-one",
			Type:         "AWS::S3::Bucket",
			Service:      "s3",
			Provider:     "aws",
			Region:       "us-east-1",
			ARN:          "arn:aws:s3:::bucket-one",
			Status:       "active",
			CreatedAt:    &now,
			ModifiedAt:   &now,
			ScannedAt:    &now,
			Tags:         map[string]string{"env": "test"},
			Attributes:   map[string]interface{}{"versioning": true},
			Metadata:     map[string]interface{}{"source": "unit"},
			RawData:      map[string]interface{}{"name": "bucket-one"},
			CrossCloudID: "cc:bucket-one",
		},
	}

	if err := store.StoreResources(resources); err != nil {
		t.Fatalf("store resources: %v", err)
	}

	got, err := store.GetResourcesByProvider("aws")
	if err != nil {
		t.Fatalf("get resources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(got))
	}
	if got[0].ID != resources[0].ID || got[0].Provider != "aws" || got[0].CrossCloudID != "cc:bucket-one" {
		t.Fatalf("unexpected resource: %#v", got[0])
	}
	if got[0].Tags["env"] != "test" {
		t.Fatalf("expected tags to round-trip, got %#v", got[0].Tags)
	}
	if got[0].Attributes["versioning"] != true {
		t.Fatalf("expected attributes to round-trip, got %#v", got[0].Attributes)
	}
}

func TestGraphStoreProtoResources(t *testing.T) {
	db, store := openTestGraphStore(t)

	resources := []*pb.Resource{
		{
			Id:         "arn:aws:s3:::bucket-one",
			Arn:        "arn:aws:s3:::bucket-one",
			Name:       "bucket-one",
			Type:       "AWS::S3::Bucket",
			Service:    "s3",
			Region:     "us-east-1",
			AccountId:  "123456789012",
			Tags:       map[string]string{"env": "test"},
			RawData:    `{"name":"bucket-one"}`,
			Attributes: `{"versioning":true}`,
			Relationships: []*pb.Relationship{
				{
					TargetId:         "aws:kms:key-one",
					RelationshipType: "uses",
					Properties:       map[string]string{"reason": "encryption"},
				},
			},
		},
	}

	if err := store.StoreProtoResources(context.Background(), "aws", resources); err != nil {
		t.Fatalf("store proto resources: %v", err)
	}

	var arn string
	if err := db.QueryRow(`SELECT arn FROM aws_resources WHERE id = ?`, resources[0].Id).Scan(&arn); err != nil {
		t.Fatalf("query stored proto resource: %v", err)
	}
	if arn != "" {
		t.Fatalf("expected duplicated ARN field to be blank, got %q", arn)
	}

	var relationshipCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM cloud_relationships
		WHERE from_id = ? AND to_id = ? AND relationship_type = ? AND provider = ?
	`, resources[0].Id, "aws:kms:key-one", "uses", "aws").Scan(&relationshipCount); err != nil {
		t.Fatalf("query stored relationship: %v", err)
	}
	if relationshipCount != 1 {
		t.Fatalf("expected stored relationship count 1, got %d", relationshipCount)
	}
}

func TestGraphStoreScanResources(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "scan-resources.duckdb")
	cfg, err := InitializeUnifiedDatabase(dbPath)
	if err != nil {
		t.Fatalf("initialize unified database: %v", err)
	}
	t.Cleanup(func() {
		_ = cfg.DB.Close()
	})

	if _, err := cfg.DB.Exec(`
		CREATE TABLE custom_scan_resources (
			id VARCHAR PRIMARY KEY,
			arn VARCHAR,
			name VARCHAR,
			type VARCHAR,
			service VARCHAR,
			region VARCHAR,
			account_id VARCHAR,
			tags JSON,
			attributes JSON,
			raw_data JSON,
			state VARCHAR,
			created_at TIMESTAMP,
			modified_at TIMESTAMP,
			scanned_at TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create custom scan table: %v", err)
	}

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
	if err := cfg.DB.QueryRow(`SELECT COUNT(*) FROM custom_scan_resources WHERE id = ?`, "custom-1").Scan(&customCount); err != nil {
		t.Fatalf("count custom resources: %v", err)
	}
	if customCount != 1 {
		t.Fatalf("expected custom table resource count 1, got %d", customCount)
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

func TestGraphStoreScanResourcesProviderAdapters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "provider-adapters.duckdb")
	cfg, err := InitializeUnifiedDatabase(dbPath)
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
	cfg, err := InitializeUnifiedDatabase(dbPath)
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

	resource.Attributes = `{"_table":"bad-table"}`
	err = store.StoreScanResources(context.Background(), []*pb.Resource{resource}, StoreScanResourcesOptions{})
	if err == nil || !strings.Contains(err.Error(), "invalid resource _table attribute") {
		t.Fatalf("expected invalid _table attribute error, got %v", err)
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
	db, store := openTestGraphStore(t)
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

	stats := &pb.ScanStats{TotalResources: 7, FailedResources: 1, DurationMs: 1234}
	if err := store.StoreScanMetadata(ctx, "", "s3", "us-east-1", stats, map[string]string{"provider": "aws"}); err != nil {
		t.Fatalf("store legacy scan metadata: %v", err)
	}

	var service, region string
	var totalResources int
	if err := db.QueryRow(`
		SELECT service, region, total_resources
		FROM scan_metadata
		LIMIT 1
	`).Scan(&service, &region, &totalResources); err != nil {
		t.Fatalf("query legacy scan metadata: %v", err)
	}
	if service != "s3" || region != "us-east-1" || totalResources != 7 {
		t.Fatalf("unexpected legacy scan metadata: service=%q region=%q total=%d", service, region, totalResources)
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

func TestGraphStoreNetworkMetadataRoundTrip(t *testing.T) {
	_, store := openTestGraphStore(t)

	ips := []*models.IPAddress{
		{Address: "203.0.113.10", Type: "public", Version: "ipv4", Provider: "aws", Region: "us-east-1", ResourceID: "aws:lb:one", Scope: "regional"},
	}
	dns := []*models.DNSRecord{
		{Name: "app.example.com", Type: "A", Values: []string{"203.0.113.10"}, TTL: 300, Provider: "aws", Zone: "Z123", ResourceID: "aws:lb:one"},
	}

	if err := store.StoreIPAddresses(ips); err != nil {
		t.Fatalf("store IP addresses: %v", err)
	}
	if err := store.StoreDNSRecords(dns); err != nil {
		t.Fatalf("store DNS records: %v", err)
	}

	gotIPs, err := store.GetIPAddressesByProvider("aws")
	if err != nil {
		t.Fatalf("get IP addresses: %v", err)
	}
	if len(gotIPs) != 1 || gotIPs[0].Address != "203.0.113.10" || gotIPs[0].Scope != "regional" {
		t.Fatalf("unexpected IP addresses: %#v", gotIPs)
	}

	gotDNS, err := store.GetDNSRecordsByProvider("aws")
	if err != nil {
		t.Fatalf("get DNS records: %v", err)
	}
	if len(gotDNS) != 1 || gotDNS[0].Name != "app.example.com" || len(gotDNS[0].Values) != 1 || gotDNS[0].Values[0] != "203.0.113.10" {
		t.Fatalf("unexpected DNS records: %#v", gotDNS)
	}
}

func TestGraphStoreCorrelations(t *testing.T) {
	db, store := openTestGraphStore(t)

	correlations := []*models.ResourceCorrelation{
		{
			ID:           "corr-1",
			SourceID:     "aws:lb:one",
			TargetID:     "azure:dns:one",
			Type:         "cross_cloud",
			RelationType: "dns_target",
			Strength:     0.9,
			Confidence:   0.8,
			Description:  "DNS record points at load balancer",
			Metadata:     map[string]interface{}{"method": "dns"},
			DiscoveredAt: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
		},
	}

	if err := store.StoreCorrelations(correlations); err != nil {
		t.Fatalf("store typed correlations: %v", err)
	}

	var typedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM crosscloud_correlations WHERE id = ?`, "corr-1").Scan(&typedCount); err != nil {
		t.Fatalf("query typed correlations: %v", err)
	}
	if typedCount != 1 {
		t.Fatalf("expected typed correlation count 1, got %d", typedCount)
	}

	if err := store.StoreCorrelations(map[string]interface{}{"kind": "generic", "count": 1}); err != nil {
		t.Fatalf("store generic correlations: %v", err)
	}

	var genericCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM crosscloud_generic_correlations`).Scan(&genericCount); err != nil {
		t.Fatalf("query generic correlations: %v", err)
	}
	if genericCount != 1 {
		t.Fatalf("expected generic correlation count 1, got %d", genericCount)
	}
}

func TestUnifiedDatabaseConfigUsesGraphStore(t *testing.T) {
	db, _ := openTestGraphStore(t)
	cfg := &UnifiedDatabaseConfig{DatabasePath: "test", DB: db}

	resource := &models.Resource{
		ID:       "gcp:bucket:one",
		Name:     "bucket-one",
		Type:     "storage.googleapis.com/Bucket",
		Service:  "storage",
		Provider: "gcp",
		Region:   "global",
		Status:   "active",
	}

	if err := cfg.StoreResources([]*models.Resource{resource}); err != nil {
		t.Fatalf("store resources through unified config: %v", err)
	}

	got, err := cfg.GetResourcesByProvider("gcp")
	if err != nil {
		t.Fatalf("get resources through unified config: %v", err)
	}
	if len(got) != 1 || got[0].ID != resource.ID {
		t.Fatalf("unexpected resources through unified config: %#v", got)
	}
}

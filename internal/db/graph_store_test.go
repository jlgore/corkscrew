package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/jlgore/corkscrew/pkg/models"
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

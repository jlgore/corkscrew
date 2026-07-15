package data

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/jlgore/corkscrew/internal/db"
)

func TestInventoryListsOfficialAndCustomResourcesThroughNormalizedView(t *testing.T) {
	t.Parallel()

	database, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "inventory.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.EnsureSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO aws_resources(id, type, name, service, region) VALUES ('aws-1', 'bucket', 'one', 's3', 'us-east-1');
		INSERT INTO custom_provider_resources(provider, id, type, name, service, region) VALUES ('acme', 'custom-1', 'widget', 'two', 'inventory', 'global');
	`); err != nil {
		t.Fatal(err)
	}

	resources, err := NewInventory(database).List(context.Background(), InventoryFilter{}, Page{Limit: 10})
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(resources) != 2 || resources[0].Provider != "acme" || resources[1].Provider != "aws" {
		t.Fatalf("resources = %#v", resources)
	}
	count, err := NewInventory(database).Count(context.Background(), InventoryFilter{})
	if err != nil || count != 2 {
		t.Fatalf("Count() = %d, %v; want 2", count, err)
	}
}

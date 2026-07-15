package data

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/jlgore/corkscrew/internal/db"
)

func TestRelationshipsListAcrossProviders(t *testing.T) {
	database, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "relationships.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.EnsureSchema(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO cloud_relationships(from_id, to_id, relationship_type, provider, properties)
		VALUES ('a', 'b', 'uses', 'aws', '{"weight": 1}'),
		       ('a', 'c', 'owns', 'acme', NULL)
	`); err != nil {
		t.Fatal(err)
	}

	repository := NewRelationships(database)
	rows, err := repository.List(context.Background(), RelationshipFilter{ResourceID: "a", Direction: DirectionOutbound}, Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Provider != "acme" || rows[1].Provider != "aws" {
		t.Fatalf("unexpected relationships: %#v", rows)
	}
}

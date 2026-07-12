package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

func TestResultsViewLoadsResourcesFromProviderTables(t *testing.T) {
	database := openMainMenuTestDB(t)
	mustExec(t, database, `
		CREATE TABLE aws_resources (
			id VARCHAR PRIMARY KEY,
			name VARCHAR,
			type VARCHAR,
			service VARCHAR,
			region VARCHAR,
			scanned_at TIMESTAMP
		)
	`)
	mustExec(t, database, `
		INSERT INTO aws_resources (id, name, type, service, region, scanned_at)
		VALUES ('bucket-1', 'bucket-one', 'AWS::S3::Bucket', 's3', 'us-east-1', TIMESTAMP '2026-07-12 12:00:00')
	`)
	mustExec(t, database, `
		CREATE TABLE gcp_resources (
			id VARCHAR PRIMARY KEY,
			name VARCHAR,
			type VARCHAR,
			service VARCHAR,
			location VARCHAR,
			discovered_at TIMESTAMP
		)
	`)
	mustExec(t, database, `
		INSERT INTO gcp_resources (id, name, type, service, location, discovered_at)
		VALUES ('instance-1', 'instance-one', 'compute.googleapis.com/Instance', 'compute', 'us-central1', TIMESTAMP '2026-07-12 12:01:00')
	`)

	model := NewResultsViewModel()
	model.SetDatabase(database)

	msg := model.Init()()
	loaded, ok := msg.(types.ResourcesLoadedMsg)
	if !ok {
		t.Fatalf("Init returned %T, want ResourcesLoadedMsg", msg)
	}
	if loaded.Error != nil {
		t.Fatalf("load resources error: %v", loaded.Error)
	}
	if loaded.Total != 2 {
		t.Fatalf("loaded total = %d, want 2", loaded.Total)
	}

	updated, _ := model.Update(loaded)
	model = updated.(*ResultsViewModel)

	view := model.View()
	for _, want := range []string{"bucket-one", "instance-one", "aws", "gcp"} {
		if !strings.Contains(view, want) {
			t.Fatalf("results view missing %q:\n%s", want, view)
		}
	}
}

func TestResultsViewHandlesMissingDatabase(t *testing.T) {
	model := NewResultsViewModel()

	msg := model.Init()()
	loaded, ok := msg.(types.ResourcesLoadedMsg)
	if !ok {
		t.Fatalf("Init returned %T, want ResourcesLoadedMsg", msg)
	}
	if loaded.Error != nil {
		t.Fatalf("missing database should not be fatal: %v", loaded.Error)
	}
	if loaded.Total != 0 {
		t.Fatalf("loaded total = %d, want 0", loaded.Total)
	}

	updated, _ := model.Update(loaded)
	model = updated.(*ResultsViewModel)
	if view := model.View(); !strings.Contains(view, "No resources found") {
		t.Fatalf("empty results view missing empty state:\n%s", view)
	}
}

func TestResultsViewNavigation(t *testing.T) {
	model := NewResultsViewModel()
	resources := make([]types.Resource, 0, 5)
	for i := 0; i < 5; i++ {
		resources = append(resources, types.Resource{ID: string(rune('a' + i)), Name: "resource"})
	}
	model.Update(types.ResourcesLoadedMsg{Resources: resources, Total: len(resources)})

	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.selected != 1 {
		t.Fatalf("selected = %d, want 1", model.selected)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if model.selected != len(resources)-1 {
		t.Fatalf("selected = %d, want %d", model.selected, len(resources)-1)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyHome})
	if model.selected != 0 {
		t.Fatalf("selected = %d, want 0", model.selected)
	}
}

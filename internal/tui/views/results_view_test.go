package views

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

func TestResultsViewLoadsResourcesFromNormalizedInventory(t *testing.T) {
	session := openViewTestSession(t)
	startedAt := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	seedScanOutcome(t, session, dataaccess.ScanOutcome{
		ID: "results-scan", Provider: "aws", Services: []string{"s3"}, Scopes: []string{"us-east-1"},
		Status: dataaccess.ScanStatusCompleted, StartedAt: startedAt, EndedAt: startedAt.Add(time.Second),
		Resources: []*pb.Resource{
			{Provider: "aws", Id: "bucket-1", Name: "bucket-one", Type: "AWS::S3::Bucket", Service: "s3", Region: "us-east-1"},
			{Provider: "acme", Id: "widget-1", Name: "widget-one", Type: "Widget", Service: "inventory", Region: "lab-1"},
		},
	})

	model := NewResultsViewModel()
	model.SetDatabase(session)

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
	for _, want := range []string{"bucket-one", "widget-one", "aws", "acme"} {
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

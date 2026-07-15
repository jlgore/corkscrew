package views

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appconfig "github.com/jlgore/corkscrew/internal/config"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

func TestMainMenuQueryShortcutUsesNonQuitKey(t *testing.T) {
	model := NewMainMenuModel()

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if cmd == nil {
		t.Fatal("expected query shortcut to return a command")
	}

	msg := cmd()
	switch msg := msg.(type) {
	case types.SwitchViewMsg:
		if msg.View != types.ViewQuery {
			t.Fatalf("shortcut switched to %v, want %v", msg.View, types.ViewQuery)
		}
	default:
		t.Fatalf("shortcut returned %T, want SwitchViewMsg", msg)
	}
}

func TestMainMenuQIsReservedForGlobalQuit(t *testing.T) {
	model := NewMainMenuModel()

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil {
		t.Fatal("expected q to be unhandled by main menu")
	}
}

func TestMainMenuLoadsLiveSystemStatus(t *testing.T) {
	session := openViewTestSession(t)
	startedAt := time.Date(2026, 7, 12, 10, 30, 0, 0, time.UTC)
	seedScanOutcome(t, session, dataaccess.ScanOutcome{
		ID: "scan-1", Provider: "aws", Services: []string{"s3"}, Scopes: []string{"us-east-1"},
		Status: dataaccess.ScanStatusCompleted, StartedAt: startedAt, EndedAt: startedAt.Add(1500 * time.Millisecond),
		Resources: []*pb.Resource{
			{Provider: "aws", Id: "bucket-1", Name: "bucket-one", Type: "AWS::S3::Bucket", Service: "s3", Region: "us-east-1"},
			{Provider: "aws", Id: "bucket-2", Name: "bucket-two", Type: "AWS::S3::Bucket", Service: "s3", Region: "us-east-1"},
		},
	})

	config := &appconfig.CorkscrewConfig{
		Providers: map[string]appconfig.CloudProviderConfig{
			"aws":   {Enabled: true},
			"azure": {Enabled: false},
			"gcp":   {Enabled: true},
		},
	}

	model := NewMainMenuModel()
	model.SetDatabase(session)
	model.SetConfig(config)

	msg := model.loadSystemStatus()()
	statusMsg, ok := msg.(SystemStatusLoadedMsg)
	if !ok {
		t.Fatalf("loadSystemStatus returned %T, want SystemStatusLoadedMsg", msg)
	}

	if !statusMsg.Status.DatabaseConnected {
		t.Fatal("expected database to be connected")
	}
	if statusMsg.Status.TotalResources != 2 {
		t.Fatalf("total resources = %d, want 2", statusMsg.Status.TotalResources)
	}
	if statusMsg.Status.ProvidersConfigured != 2 {
		t.Fatalf("configured providers = %d, want 2", statusMsg.Status.ProvidersConfigured)
	}
	if statusMsg.Status.LastScanTime != "2026-07-12 10:30" {
		t.Fatalf("last scan time = %q, want %q", statusMsg.Status.LastScanTime, "2026-07-12 10:30")
	}
}

func TestMainMenuLoadsRecentScansFromMetadata(t *testing.T) {
	session := openViewTestSession(t)
	startedAt := time.Date(2026, 7, 12, 11, 45, 0, 0, time.UTC)
	resources := make([]*pb.Resource, 0, 17)
	for i := 0; i < 17; i++ {
		resources = append(resources, &pb.Resource{
			Provider: "aws", Id: "res-" + string(rune('a'+i)), Name: "res", Type: "AWS::S3::Bucket", Service: "s3", Region: "us-east-1",
		})
	}
	seedScanOutcome(t, session, dataaccess.ScanOutcome{
		ID: "scan-2", Provider: "aws", Services: []string{"s3"}, Scopes: []string{"us-east-1"},
		Status: dataaccess.ScanStatusCompleted, StartedAt: startedAt, EndedAt: startedAt.Add(2500 * time.Millisecond),
		Resources: resources,
	})

	model := NewMainMenuModel()
	model.SetDatabase(session)

	msg := model.loadRecentScans()()
	scansMsg, ok := msg.(RecentScansLoadedMsg)
	if !ok {
		t.Fatalf("loadRecentScans returned %T, want RecentScansLoadedMsg", msg)
	}
	if len(scansMsg.Scans) != 1 {
		t.Fatalf("recent scans = %d, want 1", len(scansMsg.Scans))
	}
	scan := scansMsg.Scans[0]
	if scan.ID != "scan-2" || scan.Resources != 17 || scan.Timestamp != "2026-07-12 11:45" || scan.Duration != (2500*time.Millisecond).String() {
		t.Fatalf("unexpected scan: %#v", scan)
	}
	if len(scan.Providers) != 1 || scan.Providers[0] != "aws" {
		t.Fatalf("providers = %#v, want [aws]", scan.Providers)
	}
}

func openViewTestSession(t *testing.T) *dataaccess.Session {
	t.Helper()
	session, err := dataaccess.OpenSession(context.Background(), filepath.Join(t.TempDir(), "views.duckdb"))
	if err != nil {
		t.Fatalf("open data session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func seedScanOutcome(t *testing.T, session *dataaccess.Session, outcome dataaccess.ScanOutcome) {
	t.Helper()
	if err := session.PersistScanOutcome(context.Background(), outcome, dataaccess.PersistScanOptions{}); err != nil {
		t.Fatalf("seed scan outcome: %v", err)
	}
}

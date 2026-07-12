package views

import (
	"database/sql"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	_ "github.com/duckdb/duckdb-go/v2"
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
	database := openMainMenuTestDB(t)
	mustExec(t, database, `
		CREATE TABLE aws_resources (
			id VARCHAR PRIMARY KEY
		)
	`)
	mustExec(t, database, `
		INSERT INTO aws_resources (id)
		VALUES ('bucket-1'), ('bucket-2')
	`)
	mustExec(t, database, `
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
	`)
	mustExec(t, database, `
		INSERT INTO scan_metadata (id, service, region, scan_time, total_resources, failed_resources, duration_ms)
		VALUES ('scan-1', 's3', 'us-east-1', TIMESTAMP '2026-07-12 10:30:00', 2, 0, 1500)
	`)

	config := testMenuConfig{
		Providers: map[string]testMenuProvider{
			"aws":   {Enabled: true},
			"azure": {Enabled: false},
			"gcp":   {Enabled: true},
		},
	}

	model := NewMainMenuModel()
	model.SetDatabase(database)
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

func TestMainMenuLoadsRecentScansFromUnifiedMetadata(t *testing.T) {
	database := openMainMenuTestDB(t)
	mustExec(t, database, `
		CREATE TABLE scan_metadata (
			id VARCHAR PRIMARY KEY,
			provider VARCHAR NOT NULL,
			scan_type VARCHAR NOT NULL,
			services JSON,
			regions JSON,
			total_resources INTEGER DEFAULT 0,
			failed_resources INTEGER DEFAULT 0,
			scan_start_time TIMESTAMP NOT NULL,
			scan_end_time TIMESTAMP,
			duration_ms BIGINT,
			metadata JSON,
			status VARCHAR DEFAULT 'completed'
		)
	`)
	mustExec(t, database, `
		INSERT INTO scan_metadata (
			id, provider, scan_type, total_resources, scan_start_time, duration_ms
		)
		VALUES ('scan-2', 'aws', 'service', 17, TIMESTAMP '2026-07-12 11:45:00', 2500)
	`)

	model := NewMainMenuModel()
	model.SetDatabase(database)

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

type testMenuConfig struct {
	Providers map[string]testMenuProvider
}

type testMenuProvider struct {
	Enabled bool
}

func openMainMenuTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func mustExec(t *testing.T, database *sql.DB, query string) {
	t.Helper()

	if _, err := database.Exec(query); err != nil {
		t.Fatalf("exec query: %v\n%s", err, query)
	}
}

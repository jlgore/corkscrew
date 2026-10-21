package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
	appconfig "github.com/jlgore/corkscrew/internal/config"
)

func TestWindowSizeUpdateDoesNotReemitWindowSize(t *testing.T) {
	app := NewCorkscrewApp()
	_, cmd := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Expand batches with an explicit, bounded stack rather than recursion.
	const maxCmds = 64
	stack := []tea.Cmd{cmd}
	for ran := 0; len(stack) > 0; ran++ {
		if ran >= maxCmds {
			t.Fatalf("ran more than %d commands from one WindowSizeMsg", maxCmds)
		}
		next := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case tea.WindowSizeMsg:
			t.Fatalf("Update re-emitted %#v", msg)
		case tea.BatchMsg:
			stack = append(stack, msg...)
		}
	}
}

func TestRouterHistoryIsCapped(t *testing.T) {
	router := NewViewRouter()
	destinations := []ViewType{ViewScan, ViewResults, ViewConfig, ViewMain}
	for i := 0; i < 100; i++ {
		router.SwitchView(destinations[i%len(destinations)], nil)
	}
	history := router.GetHistory()
	if len(history) > maxRouterHistory {
		t.Fatalf("len(history) = %d, want <= %d", len(history), maxRouterHistory)
	}
	// The cap drops the oldest entry, so the newest is the view switched away from last.
	if want := destinations[98%len(destinations)]; history[len(history)-1] != want {
		t.Fatalf("newest history entry = %v, want %v", history[len(history)-1], want)
	}
}

func TestRouterRejectsUnknownView(t *testing.T) {
	router := NewViewRouter()
	cmd := router.SwitchView(ViewType(999), nil)
	if cmd == nil {
		t.Fatal("expected an error command for an unknown view")
	}
	if _, ok := cmd().(ErrorMsg); !ok {
		t.Fatal("expected ErrorMsg for an unknown view")
	}
	if router.GetCurrentViewType() != ViewMain || len(router.GetHistory()) != 0 {
		t.Fatalf("router moved to %v, history %v", router.GetCurrentViewType(), router.GetHistory())
	}
}

func TestQuitCancelsQuickScanContext(t *testing.T) {
	config := &appconfig.CorkscrewConfig{Providers: map[string]appconfig.CloudProviderConfig{
		"aws": {Enabled: true},
	}}
	runner := ScanRunFunc(func(ctx context.Context, _ scanapp.Request) (scanapp.Outcome, error) {
		<-ctx.Done()
		return scanapp.Outcome{}, ctx.Err()
	})
	app := NewCorkscrewApp()
	app.SetDependencies(nil, config, runner)

	scan := app.beginQuickScan()
	app.Update(QuitMsg{})
	msg, ok := scan().(providerScanCompleteMsg)
	if !ok || !errors.Is(msg.err, context.Canceled) {
		t.Fatalf("scan message = %#v, want context.Canceled", msg)
	}
}

func TestQuickScanIgnoresDuplicateCompletion(t *testing.T) {
	config := &appconfig.CorkscrewConfig{Providers: map[string]appconfig.CloudProviderConfig{
		"alpha": {Enabled: true}, "beta": {Enabled: true},
	}}
	app := NewCorkscrewApp()
	app.SetDependencies(nil, config, func(context.Context, scanapp.Request) (scanapp.Outcome, error) {
		return scanapp.Outcome{}, nil
	})

	first, ok := app.beginQuickScan()().(providerScanCompleteMsg)
	if !ok {
		t.Fatal("expected the first provider scan to run")
	}
	if app.handleProviderScanComplete(first) == nil {
		t.Fatal("expected the next provider to start")
	}
	if cmd := app.handleProviderScanComplete(first); cmd != nil {
		t.Fatal("duplicate completion started more work")
	}
	if app.quickScanCompleted != 1 || app.scanStatus.Progress != 0.5 {
		t.Fatalf("completed=%d progress=%v", app.quickScanCompleted, app.scanStatus.Progress)
	}
	if restart, ok := app.beginQuickScan()().(ErrorMsg); !ok || restart.Error == nil {
		t.Fatal("expected restart during a running quick scan to fail")
	}
}

package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
	appconfig "github.com/jlgore/corkscrew/internal/config"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestQuickScanRunsEveryEnabledProviderInSortedOrder(t *testing.T) {
	config := &appconfig.CorkscrewConfig{Providers: map[string]appconfig.CloudProviderConfig{
		"zeta": {Enabled: true}, "disabled": {Enabled: false}, "alpha": {Enabled: true},
	}}
	var called []string
	runner := ScanRunFunc(func(_ context.Context, request scanapp.Request) (scanapp.Outcome, error) {
		called = append(called, request.Provider)
		if request.Provider == "zeta" {
			return scanapp.Outcome{}, errors.New("provider failed")
		}
		return scanapp.Outcome{Resources: []*pb.Resource{{Id: "one"}}}, nil
	})
	app := NewCorkscrewApp()
	app.SetDependencies(nil, config, runner)

	first := app.beginQuickScan()().(providerScanCompleteMsg)
	secondCommand := app.handleProviderScanComplete(first)
	second := secondCommand().(providerScanCompleteMsg)
	complete := app.handleProviderScanComplete(second)().(ScanCompleteMsg)

	if !reflect.DeepEqual(called, []string{"alpha", "zeta"}) {
		t.Fatalf("providers called = %v", called)
	}
	if complete.Resources != 1 || complete.Error == nil || !strings.Contains(complete.Error.Error(), "zeta") {
		t.Fatalf("completion = %#v", complete)
	}
	app.handleScanComplete(complete)
	if app.IsScanning() || app.lastError == nil {
		t.Fatalf("final app state scanning=%t error=%v", app.IsScanning(), app.lastError)
	}
}

func TestQuickScanRequiresAnEnabledProvider(t *testing.T) {
	app := NewCorkscrewApp()
	app.SetDependencies(nil, &appconfig.CorkscrewConfig{}, func(context.Context, scanapp.Request) (scanapp.Outcome, error) {
		return scanapp.Outcome{}, nil
	})
	message := app.beginQuickScan()()
	errorMessage, ok := message.(ErrorMsg)
	if !ok || errorMessage.Error == nil || !strings.Contains(errorMessage.Error.Error(), "no providers") {
		t.Fatalf("message = %#v", message)
	}
}

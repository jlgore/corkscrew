package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
	pb "github.com/jlgore/corkscrew/internal/proto"
	packing "github.com/jlgore/corkscrew/internal/scanexec"
)

func TestRenderScanOutcomeJSONKeepsMachineOutputClean(t *testing.T) {
	outcome := renderFixtureOutcome()
	var stdout, stderr bytes.Buffer
	if err := renderScanOutcome(&stdout, &stderr, scanapp.Request{OutputFormat: "json"}, outcome); err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not one JSON value: %v\n%s", err, stdout.String())
	}
	for _, field := range []string{"ID", "Provider", "Status", "Resources", "Scopes", "Warnings", "Persisted"} {
		if _, ok := payload[field]; !ok {
			t.Errorf("JSON output missing Outcome field %q", field)
		}
	}
	if strings.Contains(stdout.String(), "Results stored") || !strings.Contains(stderr.String(), "Results stored") {
		t.Fatalf("machine/human output split = stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func TestRenderScanOutcomeCSVContract(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := renderScanOutcome(&stdout, &stderr, scanapp.Request{OutputFormat: "csv"}, renderFixtureOutcome()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || lines[0] != "Provider,Scope,Service,Type,Name,ID" || lines[1] != "acme,scope-a,widgets,widget,one,widget-1" {
		t.Fatalf("CSV output = %q", stdout.String())
	}
}

func TestSaveScanOutcomeUsesOutcomeJSONContract(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "scan.json")
	if err := saveScanOutcome(filename, renderFixtureOutcome()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["Resources"]; !ok {
		t.Fatalf("saved Outcome fields = %v", payload)
	}
}

func renderFixtureOutcome() scanapp.Outcome {
	return scanapp.Outcome{
		ID: "scan-1", Provider: "acme", Status: packing.StatusComplete,
		Resources: []*pb.Resource{{Provider: "acme", Region: "scope-a", Service: "widgets", Type: "widget", Name: "one", Id: "widget-1"}},
		Duration:  time.Second, Persisted: true,
		Expansions: []scanapp.ServiceGroupExpansion{{Name: "common", Services: []string{"widgets"}}},
	}
}

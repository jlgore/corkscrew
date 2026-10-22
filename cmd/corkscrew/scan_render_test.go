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

func TestRenderScanOutcomeTablePrintsScanIDAndEvaluateHint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	request := scanapp.Request{OutputFormat: "table"}
	if err := renderScanOutcome(&stdout, &stderr, request, renderFixtureOutcome()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Scan ID: scan-1",
		"corkscrew findings evaluate --scan scan-1 --pack <pack>",
		"corkscrew query --list-packs",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("table output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRenderScanOutcomeTableSkipsEvaluateHintUnlessEvaluable(t *testing.T) {
	cases := map[string]func(*scanapp.Outcome){
		"partial":       func(o *scanapp.Outcome) { o.Status = packing.StatusPartial },
		"failed":        func(o *scanapp.Outcome) { o.Status = packing.StatusFailed },
		"not persisted": func(o *scanapp.Outcome) { o.Persisted = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			outcome := renderFixtureOutcome()
			mutate(&outcome)
			var stdout, stderr bytes.Buffer
			request := scanapp.Request{OutputFormat: "table"}
			if err := renderScanOutcome(&stdout, &stderr, request, outcome); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), "Scan ID: scan-1") {
				t.Errorf("table output missing scan ID:\n%s", stdout.String())
			}
			if strings.Contains(stdout.String(), "findings evaluate") {
				t.Errorf("table output suggests evaluating:\n%s", stdout.String())
			}
		})
	}
}

func TestRenderScanOutcomeTableExplainsWhyPartialCannotBeEvaluated(t *testing.T) {
	outcome := renderFixtureOutcome()
	outcome.Status = packing.StatusPartial
	var stdout, stderr bytes.Buffer
	request := scanapp.Request{OutputFormat: "table"}
	if err := renderScanOutcome(&stdout, &stderr, request, outcome); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "only completed scans can be evaluated") {
		t.Errorf("partial scan output lacks the reason:\n%s", stdout.String())
	}
}

func TestRenderScanOutcomeMachineModesSendEvaluateHintToWarnings(t *testing.T) {
	for _, format := range []string{"json", "csv"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			request := scanapp.Request{OutputFormat: format}
			err := renderScanOutcome(&stdout, &stderr, request, renderFixtureOutcome())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stdout.String(), "findings evaluate") {
				t.Errorf("%s stdout carries the hint:\n%s", format, stdout.String())
			}
			if !strings.Contains(stderr.String(), "--scan scan-1") {
				t.Errorf("%s warnings lack the hint:\n%s", format, stderr.String())
			}
		})
	}
}

func TestRenderScanOutcomeTableCountsResourcesByServiceAndType(t *testing.T) {
	outcome := renderFixtureOutcome()
	outcome.Resources = []*pb.Resource{
		{Region: "scope-b", Service: "widgets", Type: "widget", Name: "two"},
		{Region: "scope-a", Service: "widgets", Type: "widget", Name: "one"},
		nil,
		{Region: "scope-a", Service: "gadgets", Type: "gadget", Name: "three"},
	}
	var stdout, stderr bytes.Buffer
	request := scanapp.Request{OutputFormat: "table"}
	if err := renderScanOutcome(&stdout, &stderr, request, outcome); err != nil {
		t.Fatal(err)
	}
	got := stdout.String()
	want := "SERVICE  TYPE    COUNT\ngadgets  gadget  1\nwidgets  widget  2\n"
	if !strings.Contains(got, want) {
		t.Errorf("table output lacks sorted counts %q:\n%s", want, got)
	}
	if strings.Contains(got, "NAME") || strings.Contains(got, "three") {
		t.Errorf("table output lists resources without --list-resources:\n%s", got)
	}
	if !strings.Contains(got, "--list-resources") {
		t.Errorf("table output doesn't say how to list resources:\n%s", got)
	}
}

func TestRenderScanOutcomeTableListsResourcesOnRequest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	request := scanapp.Request{OutputFormat: "table", ListResources: true}
	if err := renderScanOutcome(&stdout, &stderr, request, renderFixtureOutcome()); err != nil {
		t.Fatal(err)
	}
	want := "SCOPE    SERVICE  TYPE    NAME\nscope-a  widgets  widget  one\n"
	if !strings.Contains(stdout.String(), want) {
		t.Errorf("table output lacks the resource rows:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "COUNT") {
		t.Errorf("table output shows counts with --list-resources:\n%s", stdout.String())
	}
}

func TestRenderScanOutcomeHintNamesANonDefaultDatabase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	outcome := renderFixtureOutcome()
	outcome.Database = "/srv/my scans.duckdb"
	got := renderTable(t, scanapp.Request{}, outcome)
	want := "--scan scan-1 --pack <pack> --db '/srv/my scans.duckdb'\n"
	if !strings.Contains(got, want) {
		t.Errorf("hint lacks %q:\n%s", want, got)
	}

	outcome.Database = defaultDatabasePath()
	if got := renderTable(t, scanapp.Request{}, outcome); strings.Contains(got, "--db") {
		t.Errorf("hint names the default database:\n%s", got)
	}
}

func TestRenderScanOutcomePartialNamesTheFailedScopes(t *testing.T) {
	outcome := renderFixtureOutcome()
	outcome.Status = packing.StatusPartial
	outcome.Scopes = []scanapp.ScopeResult{
		{Scope: "acct-ok"}, {Scope: "acct-bad", Errors: []string{"boom"}},
	}
	got := renderTable(t, scanapp.Request{}, outcome)
	if !strings.Contains(got, "Failed scopes: acct-bad\n") || strings.Contains(got, "acct-ok") {
		t.Errorf("partial output doesn't name only the failed scope:\n%s", got)
	}
	if strings.Contains(got, "warnings above") {
		t.Errorf("partial output points at warnings on another stream:\n%s", got)
	}
}

func TestRenderScanOutcomeSaysWhenTheScanWasNotStored(t *testing.T) {
	outcome := renderFixtureOutcome()
	outcome.Persisted = false
	got := renderTable(t, scanapp.Request{}, outcome)
	if !strings.Contains(got, "Scan ID: scan-1 (not stored, so it can't be queried or evaluated)") {
		t.Errorf("output doesn't say the scan wasn't stored:\n%s", got)
	}
}

func TestRenderScanOutcomeTableWithNoResources(t *testing.T) {
	outcome := renderFixtureOutcome()
	outcome.Resources = []*pb.Resource{nil}
	got := renderTable(t, scanapp.Request{}, outcome)
	if !strings.Contains(got, "Scan complete: 0 resources") {
		t.Errorf("summary counts nil entries:\n%s", got)
	}
	if strings.Contains(got, "COUNT") || strings.Contains(got, "--list-resources") {
		t.Errorf("empty scan prints a count table:\n%s", got)
	}
}

func TestRenderScanOutcomeEndsWithNextSteps(t *testing.T) {
	t.Chdir(t.TempDir())
	got := renderTable(t, scanapp.Request{SaveToFile: true}, renderFixtureOutcome())
	if !strings.HasSuffix(got, "corkscrew query --list-packs\n") {
		t.Errorf("output doesn't end with the next steps:\n%s", got)
	}
}

func renderTable(t *testing.T, request scanapp.Request, outcome scanapp.Outcome) string {
	t.Helper()
	request.OutputFormat = "table"
	var stdout, stderr bytes.Buffer
	if err := renderScanOutcome(&stdout, &stderr, request, outcome); err != nil {
		t.Fatal(err)
	}
	return stdout.String()
}

func renderFixtureOutcome() scanapp.Outcome {
	return scanapp.Outcome{
		ID: "scan-1", Provider: "acme", Status: packing.StatusComplete,
		Resources: []*pb.Resource{{Provider: "acme", Region: "scope-a", Service: "widgets", Type: "widget", Name: "one", Id: "widget-1"}},
		Duration:  time.Second, Persisted: true,
		Expansions: []scanapp.ServiceGroupExpansion{{Name: "common", Services: []string{"widgets"}}},
	}
}

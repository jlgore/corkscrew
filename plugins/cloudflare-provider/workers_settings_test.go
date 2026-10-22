package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/plugins/cloudflare-provider/internal/fakecf"
)

const (
	settingsOp  = "GET /accounts/{account_id}/workers/scripts/{script_name}/settings"
	subdomainOp = "GET /accounts/{account_id}/workers/scripts/{script_name}/subdomain"
)

func scriptParams(script string) fakecf.Params {
	return fakecf.Params{"account_id": fixtureAccountID, "script_name": script}
}

// scriptAttrs scans workers and returns each worker_script's attributes by script name.
func scriptAttrs(t *testing.T, resp *pb.ScanServiceResponse) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for _, r := range resp.Resources {
		if r.Type == "worker_script" {
			out[r.Name] = decodeAttrs(r.Attributes)
		}
	}
	return out
}

func TestScanWorkersScriptSettingsAndExposure(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	resp := scanService(t, p, "workers")
	if len(resp.Errors) > 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	want := map[string]map[string]string{
		"api-gateway": {
			"workers_dev_enabled": "true", "previews_enabled": "false",
			"logpush": "true", "observability_enabled": "true", "observability_logs_enabled": "true",
			"tail_consumers": "log-shipper", "placement_mode": "smart", "binding_count": "12",
			"has_route": "true", "has_custom_domain": "true",
		},
		"internal-auth": {
			"workers_dev_enabled": "true", "previews_enabled": "true",
			"logpush": "false", "observability_enabled": "false", "observability_logs_enabled": "false",
			"tail_consumers": "", "has_route": "false", "has_custom_domain": "false",
		},
		// Observability on with no logs object: logs default to on.
		"www-site": {
			"workers_dev_enabled": "false", "previews_enabled": "true",
			"observability_enabled": "true", "observability_logs_enabled": "true",
			"has_route": "true", "has_custom_domain": "false",
		},
		"billing-cron": {
			"workers_dev_enabled": "false", "previews_enabled": "false",
			"observability_enabled": "true", "observability_logs_enabled": "false",
			"has_route": "false", "has_custom_domain": "false",
		},
	}
	got := scriptAttrs(t, resp)
	for script, attrs := range want {
		for key, value := range attrs {
			if got[script][key] != value {
				t.Errorf("%s %s = %q, want %q", script, key, got[script][key], value)
			}
		}
	}
}

// A failed settings request makes the scan partial but keeps the script, with what the list and
// the other requests found.
func TestScanWorkersSettingsFailureIsPartial(t *testing.T) {
	srv := newFakeAPI(t)
	srv.Fail(settingsOp, scriptParams("internal-auth"), fakecf.Fault{Status: http.StatusInternalServerError})
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "workers")
	if len(resp.Errors) != 1 || shared.IsScanNotice(resp.Errors[0]) ||
		!strings.Contains(resp.Errors[0], "settings failed for "+fixtureAccountID+"/internal-auth") {
		t.Fatalf("errors = %v, want one settings error", resp.Errors)
	}
	got := scriptAttrs(t, resp)
	auth := got["internal-auth"]
	if auth == nil {
		t.Fatal("internal-auth was dropped")
	}
	if _, ok := auth["binding_count"]; ok {
		t.Errorf("internal-auth has binding_count %q without settings", auth["binding_count"])
	}
	if auth["workers_dev_enabled"] != "true" || auth["compatibility_date"] != "2023-05-18" {
		t.Errorf("internal-auth lost the list and subdomain attributes: %v", auth)
	}
	if got["api-gateway"]["binding_count"] != "12" {
		t.Errorf("api-gateway attrs = %v, want full settings", got["api-gateway"])
	}
}

// A 403 is the credential's scope, not a broken scan: a coverage notice (ADR 0023).
func TestScanWorkersSubdomainForbiddenIsNotice(t *testing.T) {
	srv := newFakeAPI(t)
	srv.Fail(subdomainOp, scriptParams("billing-cron"), fakecf.Fault{Status: http.StatusForbidden})
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "workers")
	if len(resp.Errors) != 1 || !shared.IsCoverageNotice(resp.Errors[0]) {
		t.Fatalf("errors = %v, want one coverage notice", resp.Errors)
	}
	if _, ok := scriptAttrs(t, resp)["billing-cron"]["workers_dev_enabled"]; ok {
		t.Error("billing-cron has workers_dev_enabled without a subdomain response")
	}
}

// Without every route listed, "no route" can't be told from "not seen", so it isn't claimed.
func TestScanWorkersExposureUnknownWhenRoutesFail(t *testing.T) {
	srv := newFakeAPI(t)
	srv.Fail("GET /zones/{zone_id}/workers/routes", fakecf.Params{"zone_id": fixtureZoneID},
		fakecf.Fault{Status: http.StatusInternalServerError})
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "workers")
	for script, attrs := range scriptAttrs(t, resp) {
		if _, ok := attrs["has_route"]; ok {
			t.Errorf("%s has_route = %q after a failed route listing", script, attrs["has_route"])
		}
	}
}

func TestScanWorkersScriptDetailConcurrencyIsBounded(t *testing.T) {
	srv := fakecf.New(t)
	account := fakecf.Params{"account_id": fixtureAccountID}
	scripts := make([]any, 0, 12)
	for i := range 12 {
		name := fmt.Sprintf("worker-%02d", i)
		scripts = append(scripts, map[string]any{"id": name})
		srv.SetResult(settingsOp, scriptParams(name), map[string]any{})
		srv.SetResult(subdomainOp, scriptParams(name), map[string]any{"enabled": false, "previews_enabled": false})
		srv.Fail(settingsOp, scriptParams(name), fakecf.Fault{Delay: 20 * time.Millisecond})
	}
	srv.SetList("GET /accounts/{account_id}/workers/scripts", account, scripts...)
	p := newTestProvider(t, srv)
	p.config.Scope.AccountIDs = []string{fixtureAccountID}

	resp := scanService(t, p, "workers")
	if len(resp.Errors) > 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	if got := srv.MaxInFlight(settingsOp); got > scriptDetailConcurrency || got < 2 {
		t.Errorf("settings requests in flight peaked at %d, want 2..%d", got, scriptDetailConcurrency)
	}
	if got := srv.Requests(settingsOp); got != 12 {
		t.Errorf("settings requests = %d, want one per script", got)
	}
}

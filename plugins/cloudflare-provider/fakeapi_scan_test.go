package main

import (
	"context"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/cloudflare-provider/internal/fakecf"
)

func scanService(t *testing.T, p *CloudflareProvider, service string) *pb.ScanServiceResponse {
	t.Helper()
	resp, err := p.ScanService(context.Background(), &pb.ScanServiceRequest{Service: service})
	if err != nil {
		t.Fatalf("ScanService(%s): %v", service, err)
	}
	return resp
}

// resourceKeys lists each resource as "type id", sorted.
func resourceKeys(resources []*pb.Resource) []string {
	keys := make([]string, len(resources))
	for i, r := range resources {
		keys[i] = r.Type + " " + r.Id
	}
	sort.Strings(keys)
	return keys
}

// TestScanServicesAgainstBaseline pins what each scanner emits for the baseline account.
func TestScanServicesAgainstBaseline(t *testing.T) {
	tests := []struct {
		service string
		want    []string
	}{
		{"accounts", []string{
			"account " + fixtureAccountID,
		}},
		{"zones", []string{
			"zone " + fixtureZoneID,
		}},
		{"dns", []string{
			"dns_record 372e67954025e0ba6aaa6d586b9e0b59",
			"dns_record 372e67954025e0ba6aaa6d586b9e0b60",
		}},
		{"workers", []string{
			"worker_domain dbe10b4bc17c295377eabd600e1787fd",
			"worker_route 023e105f4ecef8ad9ca31a8372d0c354",
			"worker_script " + fixtureAccountID + "/workers/api-gateway",
			"worker_script " + fixtureAccountID + "/workers/internal-auth",
		}},
		{"storage", []string{
			"kv_namespace 0f2ac74b498b48028cb68387c421e279",
			"queue 023e105f4ecef8ad9ca31a8372d0c355",
			"r2_bucket " + fixtureAccountID + "/r2/uploads",
		}},
		{"data", []string{
			"d1_database c5a6d4b0-6b39-4f62-9b7d-2f7a1a2c3d4e",
			"durable_object fe7803fc55b964e09d94666545aab688d360c6bda69ba349ced1e5f28d2fc2c8",
			"durable_object_namespace 5fd1cafff895419c8bcc647fc64ab8f0",
			"secret_store 023e105f4ecef8ad9ca31a8372d0c356",
			"secret_store_secret 3fd85f74b32742f1bff64a85009dda07",
			"secret_store_secret 3fd85f74b32742f1bff64a85009dda08",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			p := newTestProvider(t, newFakeAPI(t))
			resp := scanService(t, p, tt.service)
			if len(resp.Errors) > 0 {
				t.Errorf("errors = %v, want none", resp.Errors)
			}
			if got := resourceKeys(resp.Resources); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resources:\n got %v\nwant %v", got, tt.want)
			}
			for _, r := range resp.Resources {
				if r.Provider != "cloudflare" || r.Service != tt.service {
					t.Errorf("%s %s: provider/service = %s/%s", r.Type, r.Id, r.Provider, r.Service)
				}
			}
		})
	}
}

func TestScanWorkersAttributesFromBaseline(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	resp := scanService(t, p, "workers")
	byID := map[string]map[string]string{}
	for _, r := range resp.Resources {
		byID[r.Id] = decodeAttrs(r.Attributes)
	}
	script := byID[fixtureAccountID+"/workers/internal-auth"]
	if script["account_id"] != fixtureAccountID || script["script_name"] != "internal-auth" ||
		script["compatibility_date"] != "2023-05-18" {
		t.Errorf("internal-auth attrs = %v", script)
	}
	route := byID["023e105f4ecef8ad9ca31a8372d0c354"]
	if route["script"] != "api-gateway" || route["zone_name"] != "example.com" {
		t.Errorf("route attrs = %v", route)
	}
	domain := byID["dbe10b4bc17c295377eabd600e1787fd"]
	if domain["hostname"] != "api.example.com" || domain["service"] != "api-gateway" {
		t.Errorf("domain attrs = %v", domain)
	}
}

// A 403 on one listing keeps everything else the scan observed and reports the gap.
func TestScanStorageForbiddenKVKeepsOtherResources(t *testing.T) {
	srv := newFakeAPI(t)
	srv.Fail("GET /accounts/{account_id}/storage/kv/namespaces",
		fakecf.Params{"account_id": fixtureAccountID}, fakecf.Fault{Status: http.StatusForbidden})
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "storage")
	want := []string{
		"queue 023e105f4ecef8ad9ca31a8372d0c355",
		"r2_bucket " + fixtureAccountID + "/r2/uploads",
	}
	if got := resourceKeys(resp.Resources); !reflect.DeepEqual(got, want) {
		t.Errorf("resources:\n got %v\nwant %v", got, want)
	}
	if len(resp.Errors) != 1 || !strings.Contains(resp.Errors[0], "kv namespaces list failed") {
		t.Errorf("errors = %v, want one kv entry", resp.Errors)
	}
}

// A transient 5xx is retried by the SDK, and the scan completes cleanly.
func TestScanZonesRetriesTransientFailure(t *testing.T) {
	srv := newFakeAPI(t)
	srv.Fail("GET /zones", nil, fakecf.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "zones")
	if len(resp.Errors) > 0 || len(resp.Resources) != 1 {
		t.Errorf("errors %v, %d resources; want a clean scan of 1 zone", resp.Errors, len(resp.Resources))
	}
	// The failure, its retry, and the empty page 2 that ends the listing.
	if got := srv.Requests("GET /zones"); got != 3 {
		t.Errorf("zone list requests = %d, want 3", got)
	}
}

// Two accounts may each have a worker with the same name; their IDs must differ.
func TestWorkerScriptIDsAreAccountScoped(t *testing.T) {
	srv := newFakeAPI(t)
	const otherAccount = "1b1ba6e3c4b54e2e8f4c5a6b7c8d9e0f"
	srv.SetList("GET /accounts", nil, map[string]any{"id": otherAccount, "name": "Acme Staging", "type": "standard"})
	srv.SetList("GET /accounts/{account_id}/workers/scripts", fakecf.Params{"account_id": otherAccount},
		map[string]any{"id": "api-gateway", "compatibility_date": "2025-09-01"})
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "workers")
	names := map[string]int{}
	ids := map[string]bool{}
	for _, r := range resp.Resources {
		if r.Type != "worker_script" {
			continue
		}
		names[r.Name]++
		ids[r.Id] = true
	}
	if names["api-gateway"] != 2 {
		t.Fatalf("api-gateway scripts = %d, want one per account", names["api-gateway"])
	}
	for _, want := range []string{
		fixtureAccountID + "/workers/api-gateway",
		otherAccount + "/workers/api-gateway",
	} {
		if !ids[want] {
			t.Errorf("no worker_script %s in %v", want, ids)
		}
	}
}

// Describe finds a script from its account-scoped ID, and from a bare name (the old ID form).
func TestDescribeWorkerScriptByIDForms(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	for _, id := range []string{fixtureAccountID + "/workers/internal-auth", "internal-auth"} {
		got, err := p.describeByType(context.Background(), &pb.ResourceRef{
			Type: "worker_script", Id: id, AccountId: fixtureAccountID,
		})
		if err != nil {
			t.Fatalf("describe %s: %v", id, err)
		}
		if got.Id != fixtureAccountID+"/workers/internal-auth" || got.Name != "internal-auth" {
			t.Errorf("describe %s = %s (%s)", id, got.Id, got.Name)
		}
	}
}

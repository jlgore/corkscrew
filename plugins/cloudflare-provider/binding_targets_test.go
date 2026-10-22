package main

import (
	"context"
	"net/http"
	"reflect"
	"sort"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/plugins/cloudflare-provider/internal/fakecf"
)

const (
	kvListOp     = "GET /accounts/{account_id}/storage/kv/namespaces"
	fixtureStore = "023e105f4ecef8ad9ca31a8372d0c356"
)

func TestBindingTargetsResolveAgainstBaseline(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	resp := scanService(t, p, "workers")
	if len(resp.Errors) > 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	got := bindingAttrs(t, resp)
	tests := []struct {
		binding, resolved, id string
	}{
		{"api-gateway/SESSIONS", "true", "0f2ac74b498b48028cb68387c421e279"},
		{"api-gateway/UPLOADS", "true", fixtureAccountID + "/r2/uploads"},
		{"api-gateway/DB", "true", "c5a6d4b0-6b39-4f62-9b7d-2f7a1a2c3d4e"},
		{"api-gateway/AUDIT", "true", "023e105f4ecef8ad9ca31a8372d0c355"},
		{"api-gateway/RATE_LIMITER", "true", "5fd1cafff895419c8bcc647fc64ab8f0"},
		{"api-gateway/AUTH", "true", fixtureAccountID + "/workers/internal-auth"},
		{"api-gateway/STRIPE", "true", "3fd85f74b32742f1bff64a85009dda07"},
		{"internal-auth/WEBHOOK", "true", "3fd85f74b32742f1bff64a85009dda08"},
		{"billing-cron/STAGING_CACHE", "false", ""}, // a KV namespace that doesn't exist
	}
	for _, tt := range tests {
		attrs := got[tt.binding]
		if attrs["target_resolved"] != tt.resolved || attrs["target_id"] != tt.id {
			t.Errorf("%s: target_resolved=%q target_id=%q, want %q %q",
				tt.binding, attrs["target_resolved"], attrs["target_id"], tt.resolved, tt.id)
		}
	}
	// Kinds Corkscrew doesn't inventory are never resolved, so they can't read as dangling.
	for _, binding := range []string{
		"api-gateway/API_BASE_URL", "api-gateway/NOTIFY", "api-gateway/SESSION_SIGNING_KEY",
	} {
		if _, ok := got[binding]["target_resolved"]; ok {
			t.Errorf("%s has target_resolved %q", binding, got[binding]["target_resolved"])
		}
	}
	if got["billing-cron/STAGING_CACHE"]["target_type"] != "kv_namespace" {
		t.Errorf("STAGING_CACHE target_type = %q", got["billing-cron/STAGING_CACHE"]["target_type"])
	}
}

func scriptEdges(resp *pb.ScanServiceResponse) map[string][]string {
	edges := map[string][]string{}
	for _, r := range resp.Resources {
		for _, rel := range r.Relationships {
			edges[r.Name] = append(
				edges[r.Name],
				rel.RelationshipType+" "+rel.TargetType+" "+rel.TargetId,
			)
		}
		sort.Strings(edges[r.Name])
	}
	return edges
}

func TestBindingEdgesOnlyWhenRequested(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	if edges := scriptEdges(scanService(t, p, "workers")); len(edges) > 0 {
		t.Fatalf("edges without IncludeRelationships: %v", edges)
	}

	resp, err := p.ScanService(
		context.Background(),
		&pb.ScanServiceRequest{Service: "workers", IncludeRelationships: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	edges := scriptEdges(resp)
	want := map[string][]string{
		"api-gateway": {
			"binds d1_database c5a6d4b0-6b39-4f62-9b7d-2f7a1a2c3d4e",
			"binds queue 023e105f4ecef8ad9ca31a8372d0c355",
			"binds kv_namespace 0f2ac74b498b48028cb68387c421e279",
			"binds secret_store_secret 3fd85f74b32742f1bff64a85009dda07",
			"binds durable_object_namespace 5fd1cafff895419c8bcc647fc64ab8f0",
			"binds worker_script " + fixtureAccountID + "/workers/internal-auth",
			"binds r2_bucket " + fixtureAccountID + "/r2/uploads",
		},
		"internal-auth": {
			"binds kv_namespace 0f2ac74b498b48028cb68387c421e279",
			"binds secret_store_secret 3fd85f74b32742f1bff64a85009dda08",
		},
		"billing-cron": {
			"binds d1_database c5a6d4b0-6b39-4f62-9b7d-2f7a1a2c3d4e",
			"binds secret_store_secret 3fd85f74b32742f1bff64a85009dda0a",
		},
		"www-site": {
			"binds secret_store_secret 3fd85f74b32742f1bff64a85009dda07",
			"binds worker_script " + fixtureAccountID + "/workers/billing-cron",
		},
		"checkout-staging": {
			"binds secret_store_secret 3fd85f74b32742f1bff64a85009dda07",
			"binds kv_namespace 2c1d5e8f9a0b4c3d8e7f6a5b4c3d2e1f",
		},
	}
	for _, list := range want {
		sort.Strings(list)
	}
	if !reflect.DeepEqual(edges, want) {
		t.Errorf("edges:\n got %v\nwant %v", edges, want)
	}
}

// A denied target listing leaves those bindings unknown, not dangling, and is a coverage
// notice rather than a scan error.
func TestBindingTargetsForbiddenListingIsUnknown(t *testing.T) {
	srv := newFakeAPI(t)
	srv.Fail(
		kvListOp,
		fakecf.Params{"account_id": fixtureAccountID},
		fakecf.Fault{Status: http.StatusForbidden},
	)
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "workers")
	if len(resp.Errors) != 1 || !shared.IsCoverageNotice(resp.Errors[0]) {
		t.Fatalf("errors = %v, want one coverage notice", resp.Errors)
	}
	got := bindingAttrs(t, resp)
	for _, binding := range []string{
		"api-gateway/SESSIONS", "internal-auth/SESSIONS", "billing-cron/STAGING_CACHE",
	} {
		if v, ok := got[binding]["target_resolved"]; ok {
			t.Errorf("%s target_resolved = %q after a denied listing", binding, v)
		}
	}
	if got["api-gateway/DB"]["target_resolved"] != "true" {
		t.Errorf("other kinds stopped resolving: %v", got["api-gateway/DB"])
	}
}

// A batch scan lists each target kind once, for both binding resolution and the storage and
// data scans, and passes IncludeRelationships through to each service.
func TestBatchScanSharesListingsAndRelationships(t *testing.T) {
	srv := newFakeAPI(t)
	p := newTestProvider(t, srv)
	resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{
		Services: []string{"workers", "storage", "data"}, IncludeRelationships: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("errors = %v", resp.Errors)
	}
	// Once means one listing: the page-style lists also ask for the empty page 2 that ends
	// them. Without sharing, every count would double.
	for op, want := range map[string]int{
		kvListOp:                                 2,
		"GET /accounts/{account_id}/d1/database": 2,
		"GET /accounts/{account_id}/workers/durable_objects/namespaces": 2,
		"GET /accounts/{account_id}/secrets_store/stores":               2,
		"GET /accounts/{account_id}/r2/buckets":                         1,
		"GET /accounts/{account_id}/queues":                             1,
	} {
		if got := srv.Requests(op); got != want {
			t.Errorf("%s: %d requests, want %d", op, got, want)
		}
	}
	edges := 0
	for _, r := range resp.Resources {
		edges += len(r.Relationships)
	}
	if edges != 15 {
		t.Errorf("%d edges in the batch, want 15", edges)
	}
}

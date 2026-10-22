package fakecf

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// recorder is a testing.TB that records failures instead of failing the test, so tests can
// assert on what the fake reports. Fatalf panics with errFatal to stop the caller.
type recorder struct {
	testing.TB
	mu     sync.Mutex
	failed []string
}

var errFatal = fmt.Errorf("fatal")

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	panic(errFatal)
}

func (r *recorder) messages() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.failed, "\n")
}

// fatal runs fn and reports whether it called Fatalf.
func fatal(fn func()) (called bool) {
	defer func() {
		if p := recover(); p != nil {
			if p != errFatal {
				panic(p)
			}
			called = true
		}
	}()
	fn()
	return false
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
	return resp.StatusCode, body
}

func TestSchemaClassifiesOperations(t *testing.T) {
	s, err := loadSchema()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		op     string
		list   bool
		paging string
	}{
		{"GET /accounts", true, pagingPage},
		{"GET /accounts/{account_id}/workers/scripts", true, pagingNone},
		{
			"GET /accounts/{account_id}/workers/durable_objects/namespaces/{id}/objects",
			true,
			pagingCursor,
		},
		{"GET /accounts/{account_id}/r2/buckets", false, pagingNone},
		{"GET /accounts/{account_id}/workers/scripts/{script_name}/settings", false, pagingNone},
	}
	for _, tt := range tests {
		op, ok := s.byKey[tt.op]
		if !ok {
			t.Errorf("%s: not in the schema", tt.op)
			continue
		}
		if op.list != tt.list || op.paging != tt.paging {
			t.Errorf("%s: list=%t paging=%s, want list=%t paging=%s",
				tt.op, op.list, op.paging, tt.list, tt.paging)
		}
	}
}

func TestSeedThatBreaksTheSpecFails(t *testing.T) {
	rec := &recorder{TB: t}
	srv := New(rec)
	const op = "GET /accounts/{account_id}/workers/scripts/{script_name}/subdomain"
	called := fatal(func() {
		srv.SetResult(op, Params{"account_id": "a", "script_name": "w"}, map[string]any{
			"enabled": "yes-please",
			"extra":   1,
		})
	})
	if !called {
		t.Fatal("SetResult accepted a result that breaks the spec")
	}
	got := rec.messages()
	for _, want := range []string{
		op,
		"$.enabled: \"yes-please\" is not a boolean",
		"$.previews_enabled: required property is missing",
		"$.extra: property is not in the schema",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("failure %q\ndoesn't mention %q", got, want)
		}
	}
}

func TestSeedNamesUnknownOperation(t *testing.T) {
	rec := &recorder{TB: t}
	srv := New(rec)
	if !fatal(func() { srv.SetList("GET /nope", nil) }) {
		t.Fatal("SetList accepted an unknown operation")
	}
	if !strings.Contains(rec.messages(), "operations.ts") {
		t.Errorf("failure %q doesn't say where to add the operation", rec.messages())
	}
}

func account(i int) map[string]any {
	return map[string]any{
		"id":   fmt.Sprintf("%032x", i),
		"name": fmt.Sprintf("Account %d", i),
		"type": "standard",
	}
}

func TestListPagesUntilEmpty(t *testing.T) {
	srv := New(t)
	srv.SetList("GET /accounts", nil, account(1), account(2), account(3))

	status, body := get(t, srv.URL()+"/accounts?per_page=2")
	if status != http.StatusOK || body["success"] != true {
		t.Fatalf("page 1: status %d body %v", status, body)
	}
	if n := len(body["result"].([]any)); n != 2 {
		t.Fatalf("page 1 has %d items, want 2", n)
	}
	info := body["result_info"].(map[string]any)
	if info["total_count"] != float64(3) || info["page"] != float64(1) {
		t.Errorf("page 1 result_info = %v", info)
	}
	if _, ok := info["total_pages"]; ok {
		t.Errorf("result_info has total_pages, which iam_result_info doesn't declare: %v", info)
	}

	_, body = get(t, srv.URL()+"/accounts?per_page=2&page=2")
	if n := len(body["result"].([]any)); n != 1 {
		t.Fatalf("page 2 has %d items, want 1", n)
	}
	_, body = get(t, srv.URL()+"/accounts?per_page=2&page=3")
	if n := len(body["result"].([]any)); n != 0 {
		t.Fatalf("page 3 has %d items, want 0", n)
	}
	if got := srv.Requests("GET /accounts"); got != 3 {
		t.Errorf("Requests = %d, want 3", got)
	}
}

func TestUnseededListIsEmptyAndUnseededGetIs404(t *testing.T) {
	srv := New(t)
	status, body := get(t, srv.URL()+"/zones/z1/workers/routes")
	if status != http.StatusOK || len(body["result"].([]any)) != 0 {
		t.Errorf("unseeded list: status %d body %v", status, body)
	}
	status, body = get(t, srv.URL()+"/accounts/a1/r2/buckets")
	if status != http.StatusOK || len(body["result"].(map[string]any)["buckets"].([]any)) != 0 {
		t.Errorf("unseeded collection object: status %d body %v", status, body)
	}
	status, body = get(t, srv.URL()+"/accounts/a1/r2/buckets/b1")
	if status != http.StatusNotFound || body["success"] != false {
		t.Errorf("unseeded get: status %d body %v", status, body)
	}
}

func TestUnmatchedRequestsFailTheTest(t *testing.T) {
	rec := &recorder{TB: t}
	srv := New(rec)
	status, _ := get(t, srv.URL()+"/accounts/a1/workers/nope")
	if status != http.StatusNotFound {
		t.Errorf("status %d, want 404", status)
	}
	status, _ = get(t, srv.URL()+"/accounts?bogus=1")
	if status != http.StatusBadRequest {
		t.Errorf("status %d, want 400", status)
	}
	got := rec.messages()
	for _, want := range []string{"/accounts/a1/workers/nope matches no operation", "[bogus]"} {
		if !strings.Contains(got, want) {
			t.Errorf("failures %q\ndon't mention %q", got, want)
		}
	}
}

func TestFaultAppliesTimesThenClears(t *testing.T) {
	srv := New(t)
	params := Params{"account_id": "a1"}
	srv.SetList("GET /accounts/{account_id}/storage/kv/namespaces", params,
		map[string]any{"id": "0f2ac74b498b48028cb68387c421e279", "title": "cache"})
	srv.Fail(
		"GET /accounts/{account_id}/storage/kv/namespaces",
		params,
		Fault{Status: http.StatusForbidden, Times: 1},
	)

	status, body := get(t, srv.URL()+"/accounts/a1/storage/kv/namespaces")
	if status != http.StatusForbidden || body["success"] != false {
		t.Fatalf("first request: status %d body %v", status, body)
	}
	status, body = get(t, srv.URL()+"/accounts/a1/storage/kv/namespaces")
	if status != http.StatusOK || len(body["result"].([]any)) != 1 {
		t.Fatalf("second request: status %d body %v", status, body)
	}
}

func TestRepeatFaultServesTheSamePage(t *testing.T) {
	srv := New(t)
	srv.SetList("GET /accounts", nil, account(1))
	srv.Fail("GET /accounts", nil, Fault{Repeat: true})
	for page := 1; page <= 3; page++ {
		_, body := get(t, fmt.Sprintf("%s/accounts?page=%d", srv.URL(), page))
		if n := len(body["result"].([]any)); n != 1 {
			t.Fatalf("page %d has %d items, want the repeated 1", page, n)
		}
	}
}

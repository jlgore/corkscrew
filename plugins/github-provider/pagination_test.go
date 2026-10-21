package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared/paging"
	"github.com/shurcooL/githubv4"
)

// newTestProvider returns a provider whose REST client talks to an httptest server running h.
// The server URL is returned so handlers can build absolute Link headers.
func newTestProvider(t *testing.T, h http.Handler) (*GitHubProvider, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	client := github.NewClient(srv.Client())
	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client.BaseURL = base
	p := &GitHubProvider{initialized: true, org: "acme", client: client, concurrency: 2}
	return p, srv.URL
}

// withinTimeout fails the test if fn has not returned after d, so a pagination loop that never
// terminates fails fast instead of hanging the suite.
func withinTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not return within %s", d)
	}
}

func TestDependabotAlertsFollowsEveryLinkPage(t *testing.T) {
	var base string
	pages := map[string]struct {
		body string
		next string
	}{
		"":   {`[{"number":1},{"number":2}]`, "c1"},
		"c1": {`[{"number":3},{"number":4}]`, "c2"},
		"c2": {`[{"number":5}]`, ""},
	}
	mux := http.NewServeMux()
	const alertsPath = "/repos/acme/app/dependabot/alerts"
	mux.HandleFunc(alertsPath, func(w http.ResponseWriter, r *http.Request) {
		page, ok := pages[r.URL.Query().Get("after")]
		if !ok {
			http.Error(w, "unknown cursor", http.StatusBadRequest)
			return
		}
		if page.next != "" {
			next := base + alertsPath + "?state=open&per_page=100&after=" + page.next
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next))
		}
		fmt.Fprint(w, page.body)
	})
	p, serverURL := newTestProvider(t, mux)
	base = serverURL

	var errs []string
	items := p.dependabotAlerts(context.Background(), "acme", "app", &errs)
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	if len(items) != 5 {
		t.Fatalf("got %d alerts across 3 pages, want 5", len(items))
	}
}

func TestRESTPagingStopsOnRepeatedLink(t *testing.T) {
	var base string
	var requests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/rulesets", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// A broken server or proxy: every response points at the same next page.
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/app/rulesets?page=2>; rel="next"`, base))
		fmt.Fprint(w, `[{"id":1,"name":"main"}]`)
	})
	p, serverURL := newTestProvider(t, mux)
	base = serverURL
	repo := &github.Repository{Name: github.Ptr("app"), FullName: github.Ptr("acme/app"),
		Owner: &github.User{Login: github.Ptr("acme")}}

	var errs []string
	var resources []*pb.Resource
	withinTimeout(t, 5*time.Second, func() {
		resources = p.rulesetResources(context.Background(), repo, &errs)
	})
	if len(errs) != 1 || !strings.Contains(errs[0], paging.ErrTokenRepeated.Error()) {
		t.Fatalf("errors = %v, want one token-repeated entry", errs)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	// The pages that did arrive are kept; the error entry marks the scope partial.
	if len(resources) != 2 {
		t.Fatalf("resources = %d, want the 2 observed", len(resources))
	}
}

func TestRESTPagingRefusesLinkToOtherHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/rulesets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://attacker.example/steal?page=2>; rel="next"`)
		fmt.Fprint(w, `[]`)
	})
	p, _ := newTestProvider(t, mux)

	_, err := p.getRESTMaps(context.Background(), "rulesets", "repos/acme/app/rulesets")
	if err == nil || !strings.Contains(err.Error(), "leaves API host") {
		t.Fatalf("err = %v, want refusal to follow a foreign host", err)
	}
}

func TestLinkNext(t *testing.T) {
	const page2 = "https://api.github.com/x?page=2"
	cases := map[string]string{
		"": "",
		`<` + page2 + `>; rel="next", <https://api.github.com/x?page=9>; rel="last"`: page2,
		`<https://api.github.com/x?page=1>; rel="prev"`:                              "",
		`garbage; rel="next"`: "",
	}
	for header, want := range cases {
		if got := linkNext(header); got != want {
			t.Errorf("linkNext(%q) = %q, want %q", header, got, want)
		}
	}
}

func newGraphQLTestProvider(t *testing.T, endCursor string) (*GitHubProvider, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"organization":{"repositories":{`+
			`"pageInfo":{"endCursor":%q,"hasNextPage":true},`+
			`"nodes":[{"id":"R_1","name":"app","nameWithOwner":"acme/app",`+
			`"owner":{"login":"acme"}}]}}}}`, endCursor)
	}))
	t.Cleanup(srv.Close)
	p := &GitHubProvider{
		initialized: true,
		org:         "acme",
		gqlClient:   githubv4.NewEnterpriseClient(srv.URL+"/graphql", srv.Client()),
	}
	return p, &requests
}

func TestListReposGraphQLRepeatedCursorIsError(t *testing.T) {
	p, requests := newGraphQLTestProvider(t, "c1")
	var err error
	withinTimeout(t, 5*time.Second, func() {
		_, err = p.listReposGraphQL(context.Background())
	})
	if !errors.Is(err, paging.ErrTokenRepeated) {
		t.Fatalf("err = %v, want ErrTokenRepeated", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestListReposGraphQLEmptyCursorIsError(t *testing.T) {
	p, requests := newGraphQLTestProvider(t, "")
	var err error
	withinTimeout(t, 5*time.Second, func() {
		_, err = p.listReposGraphQL(context.Background())
	})
	if err == nil || !strings.Contains(err.Error(), "without endCursor") {
		t.Fatalf("err = %v, want an empty-cursor error", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

// securityServer lists one repository and fails every alert endpoint with a 500.
func securityServer(scans *atomic.Int32) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/acme/repos", func(w http.ResponseWriter, r *http.Request) {
		scans.Add(1)
		fmt.Fprint(w, `[{"name":"app","full_name":"acme/app","owner":{"login":"acme"}}]`)
	})
	mux.HandleFunc("/repos/acme/app/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	return mux
}

func TestListResourcesReportsScanErrors(t *testing.T) {
	var scans atomic.Int32
	p, _ := newTestProvider(t, securityServer(&scans))
	req := &pb.ListResourcesRequest{Service: "security"}

	resp, err := p.ListResources(context.Background(), req)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if resp.Metadata["partial"] != "true" {
		t.Fatalf("metadata = %v, want partial=true", resp.Metadata)
	}
	alertKinds := []string{"dependabot alerts", "secret scanning alerts", "code scanning alerts"}
	for _, want := range alertKinds {
		if !strings.Contains(resp.Metadata["errors"], want) {
			t.Errorf("errors %q missing %q", resp.Metadata["errors"], want)
		}
	}

	// A fresh listing retries instead of replaying the cached failure.
	if _, err := p.ListResources(context.Background(), req); err != nil {
		t.Fatalf("second ListResources: %v", err)
	}
	if got := scans.Load(); got != 2 {
		t.Fatalf("scans = %d, want a rescan for the second first-page request", got)
	}
}

func TestListResourcesDefaultPageSize(t *testing.T) {
	refs := make([]*pb.ResourceRef, listPageSizeDefault+5)
	for i := range refs {
		refs[i] = &pb.ResourceRef{Id: fmt.Sprintf("r%d", i)}
	}
	p := &GitHubProvider{initialized: true, org: "acme", client: github.NewClient(nil)}
	key := listCacheKey("repos", nil)
	p.listCache.Store(key, &listEntry{refs: refs, expiresAt: time.Now().Add(time.Minute)})

	resp, err := p.ListResources(context.Background(), &pb.ListResourcesRequest{Service: "repos"})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resp.Resources) != listPageSizeDefault || resp.NextToken == "" {
		t.Fatalf("page = %d refs, next %q; want %d and a token",
			len(resp.Resources), resp.NextToken, listPageSizeDefault)
	}
}

func TestBranchProtectionTransientErrorNotCached(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	const protectionPath = "/repos/acme/app/branches/main/protection"
	mux.HandleFunc(protectionPath, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, `{"message":"unavailable"}`, http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"url":"https://api.github.com/repos/acme/app/branches/main/protection"}`)
	})
	p, _ := newTestProvider(t, mux)
	repo := &github.Repository{Name: github.Ptr("app"), FullName: github.Ptr("acme/app"),
		DefaultBranch: github.Ptr("main"), Owner: &github.User{Login: github.Ptr("acme")}}
	ctx := withScanCache(context.Background())

	var errs []string
	if got := p.checkBranchProtection(ctx, repo, &errs).status; got != bpUnknown {
		t.Fatalf("first status = %v, want bpUnknown", got)
	}
	if got := p.checkBranchProtection(ctx, repo, &errs).status; got != bpProtected {
		t.Fatalf("second status = %v, want bpProtected after the transient error", got)
	}
	if got := p.checkBranchProtection(ctx, repo, &errs).status; got != bpProtected {
		t.Fatalf("third status = %v, want cached bpProtected", got)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("protection requests = %d, want 2 (unknown not cached, protected cached)", got)
	}
	// A new scan gets its own cache rather than inheriting this one.
	fresh := withScanCache(context.Background())
	if protectionCacheFrom(fresh) == protectionCacheFrom(ctx) {
		t.Fatal("separate scans share a protection cache")
	}
}

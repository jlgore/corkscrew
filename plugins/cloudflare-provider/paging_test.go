package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/cloudflare-provider/internal/fakecf"
)

// setListItemsMax lowers the listing cap for one test.
func setListItemsMax(t *testing.T, itemsMax int) {
	t.Helper()
	previous := listItemsMax
	listItemsMax = itemsMax
	t.Cleanup(func() { listItemsMax = previous })
}

type slicePager struct {
	items []int
	index int
}

func (s *slicePager) Next() bool {
	s.index++
	return s.index <= len(s.items)
}
func (s *slicePager) Current() int { return s.items[s.index-1] }
func (s *slicePager) Err() error   { return nil }

func TestCollectAutoPagingCap(t *testing.T) {
	setListItemsMax(t, 3)

	exact, err := collectAutoPaging[int](&slicePager{items: []int{1, 2, 3}})
	if err != nil || len(exact) != 3 {
		t.Fatalf("exactly the cap: got %v, %v; want 3 items and no error", exact, err)
	}
	over, err := collectAutoPaging[int](&slicePager{items: []int{1, 2, 3, 4}})
	if !errors.Is(err, errListItemCap) {
		t.Fatalf("over the cap: err = %v, want errListItemCap", err)
	}
	if len(over) != 3 {
		t.Fatalf("over the cap: kept %d items, want the 3 collected", len(over))
	}
}

// repeatingPager serves its items as one page, then the same page again for every later page,
// like a server that ignores the page number.
type repeatingPager struct {
	items    []string
	index    int
	requests int
}

func (r *repeatingPager) Next() bool {
	if r.index%len(r.items) == 0 {
		r.requests++
	}
	r.index++
	return r.requests <= 1000
}
func (r *repeatingPager) Current() string { return r.items[(r.index-1)%len(r.items)] }
func (r *repeatingPager) Err() error      { return nil }

func TestCollectAutoPagingStopsOnRepeatedItems(t *testing.T) {
	// A one-page listing served again for every page number is complete.
	small := &repeatingPager{items: []string{"a", "b"}}
	got, err := collectAutoPaging[string](small)
	if err != nil || len(got) != 2 || small.requests != 2 {
		t.Fatalf("small repeat: %v, %v after %d pages; want 2 items, no error, 2 pages",
			got, err, small.requests)
	}

	// A full page repeated may hide later pages: keep the items and report it.
	full := make([]string, repeatCompleteItemsMax)
	for i := range full {
		full[i] = fmt.Sprintf("item-%d", i)
	}
	got, err = collectAutoPaging[string](&repeatingPager{items: full})
	if !errors.Is(err, errListRepeated) || len(got) != repeatCompleteItemsMax {
		t.Fatalf("full repeat: %d items, %v; want %d and errListRepeated",
			len(got), err, repeatCompleteItemsMax)
	}
}

// The live failure: the accounts listing returned the same account for every page number, so the
// SDK paged until the scan's deadline. It now stops after the first repeat.
func TestScanAccountsRepeatedPageCompletes(t *testing.T) {
	srv := newFakeAPI(t)
	srv.Fail("GET /accounts", nil, fakecf.Fault{Repeat: true})
	p := newTestProvider(t, srv)

	resp := scanService(t, p, "accounts")
	if len(resp.Errors) > 0 || len(resp.Resources) != 1 {
		t.Fatalf("errors %v, %d resources; want the one account and no error",
			resp.Errors, len(resp.Resources))
	}
	if got := srv.Requests("GET /accounts"); got != 2 {
		t.Errorf("accounts list requests = %d, want 2 (the page and its repeat)", got)
	}
}

// With account_ids configured, accounts are fetched directly, not listed.
func TestScanAccountsScopedGetsDirectly(t *testing.T) {
	srv := newFakeAPI(t)
	srv.SetResult("GET /accounts/{account_id}", fakecf.Params{"account_id": fixtureAccountID},
		map[string]any{"id": fixtureAccountID, "name": "Acme Production", "type": "standard"})
	p := newTestProvider(t, srv)
	p.config.Scope.AccountIDs = []string{fixtureAccountID}

	resp := scanService(t, p, "accounts")
	if len(resp.Errors) > 0 || len(resp.Resources) != 1 ||
		resp.Resources[0].Id != fixtureAccountID {
		t.Fatalf("errors %v, resources %v; want the configured account",
			resp.Errors, resp.Resources)
	}
	if got := srv.Requests("GET /accounts"); got != 0 {
		t.Errorf("accounts list requests = %d, want 0", got)
	}
}

func TestListResourcesDefaultPageSizeAndPartial(t *testing.T) {
	refs := make([]*pb.ResourceRef, listPageSizeDefault+1)
	for i := range refs {
		refs[i] = &pb.ResourceRef{Id: fmt.Sprintf("r%d", i)}
	}
	p := &CloudflareProvider{initialized: true, client: cloudflare.NewClient()}
	p.listCache.Store(listCacheKey("zones", nil), &listEntry{
		refs:      refs,
		errors:    []string{"zones list failed: boom"},
		expiresAt: time.Now().Add(time.Minute),
	})

	// A continuation page is served from the cached partial listing.
	resp, err := p.ListResources(context.Background(), &pb.ListResourcesRequest{
		Service: "zones", NextToken: "0",
	})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resp.Resources) != listPageSizeDefault || resp.NextToken == "" {
		t.Fatalf("page = %d refs, next %q; want %d and a token",
			len(resp.Resources), resp.NextToken, listPageSizeDefault)
	}
	if resp.Metadata["partial"] != "true" || !strings.Contains(resp.Metadata["errors"], "boom") {
		t.Fatalf("metadata = %v, want the cached scan error", resp.Metadata)
	}
}

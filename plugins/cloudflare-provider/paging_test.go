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

// TestScanStorageItemCapIsErrorEntry runs the real KV pager against a fake that returns the
// same non-empty page forever. The SDK pager only stops on an empty page, so without the cap
// the scan would run until its deadline.
func TestScanStorageItemCapIsErrorEntry(t *testing.T) {
	setListItemsMax(t, 5)
	srv := fakecf.New(t)
	const kvList = "GET /accounts/{account_id}/storage/kv/namespaces"
	account := fakecf.Params{"account_id": fixtureAccountID}
	srv.SetList(kvList, account,
		map[string]any{"id": "0f2ac74b498b48028cb68387c421e27a", "title": "a"},
		map[string]any{"id": "0f2ac74b498b48028cb68387c421e27b", "title": "b"})
	srv.Fail(kvList, account, fakecf.Fault{Repeat: true})
	p := newTestProvider(t, srv)
	p.config.Scope.AccountIDs = []string{fixtureAccountID}

	var resp *pb.ScanServiceResponse
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err = p.ScanService(context.Background(), &pb.ScanServiceRequest{Service: "storage"})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("storage scan did not stop at the item cap")
	}
	if err != nil {
		t.Fatalf("ScanService: %v", err)
	}
	if len(resp.Errors) != 1 || !strings.Contains(resp.Errors[0], errListItemCap.Error()) {
		t.Fatalf("errors = %v, want one item-cap entry", resp.Errors)
	}
	// The namespaces collected before the cap are kept; the error entry marks the scope partial.
	if len(resp.Resources) != 5 {
		t.Fatalf("resources = %d, want the 5 collected", len(resp.Resources))
	}
	if got := srv.Requests(kvList); got != 3 {
		t.Fatalf("kv requests = %d, want 3 (pages of 2 up to a cap of 5)", got)
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

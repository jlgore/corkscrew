package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/internal/shared/paging"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakePage is one response from a fake list call.
type fakePage struct {
	items []string
	token string
	err   error
}

// fakeIterator is an iterator.Pageable over scripted pages. Once the script runs out it keeps
// serving the last page, which lets a test model a server that never stops paging.
type fakeIterator struct {
	pages    []fakePage
	calls    int
	buf      []string
	pageInfo *iterator.PageInfo
}

func newFakeIterator(pages ...fakePage) *fakeIterator {
	it := &fakeIterator{pages: pages}
	it.pageInfo, _ = iterator.NewPageInfo(it.fetch,
		func() int { return len(it.buf) },
		func() interface{} { b := it.buf; it.buf = nil; return b })
	return it
}

func (it *fakeIterator) PageInfo() *iterator.PageInfo { return it.pageInfo }

func (it *fakeIterator) fetch(_ int, _ string) (string, error) {
	page := it.pages[min(it.calls, len(it.pages)-1)]
	it.calls++
	if page.err != nil {
		return "", page.err
	}
	it.buf = append(it.buf, page.items...)
	return page.token, nil
}

func TestListPagesStopsOnRepeatedToken(t *testing.T) {
	it := newFakeIterator(fakePage{items: []string{"a"}, token: "same"})

	var seen []string
	err := listPages("test", it, 1, func(item string) { seen = append(seen, item) })
	if !errors.Is(err, paging.ErrTokenRepeated) {
		t.Fatalf("listPages error = %v, want ErrTokenRepeated", err)
	}
	if it.calls > 3 {
		t.Fatalf("fetched %d pages for a token that never advanced", it.calls)
	}
	if len(seen) == 0 {
		t.Fatal("items received before the repeat were dropped")
	}
}

func TestListPagesKeepsItemsBeforeFailedPage(t *testing.T) {
	pageErr := errors.New("page 2 failed")
	it := newFakeIterator(
		fakePage{items: []string{"a", "b"}, token: "t1"},
		fakePage{err: pageErr},
	)

	var seen []string
	err := listPages("test", it, 2, func(item string) { seen = append(seen, item) })
	if !errors.Is(err, pageErr) {
		t.Fatalf("listPages error = %v, want %v", err, pageErr)
	}
	if strings.Join(seen, ",") != "a,b" {
		t.Fatalf("seen = %v, want [a b]", seen)
	}
}

// fakeOrgIterator serves organizations; listPages needs the buffer typed as the item type.
type fakeOrgIterator struct {
	err      error
	pageInfo *iterator.PageInfo
}

func (it *fakeOrgIterator) PageInfo() *iterator.PageInfo { return it.pageInfo }

func TestCollectOrgIDsReturnsPermissionDenied(t *testing.T) {
	denied := status.Error(codes.PermissionDenied, "caller lacks permission")
	it := &fakeOrgIterator{err: denied}
	var buf []*resourcemanagerpb.Organization
	it.pageInfo, _ = iterator.NewPageInfo(
		func(int, string) (string, error) { return "", it.err },
		func() int { return len(buf) },
		func() interface{} { b := buf; buf = nil; return b })

	orgs, err := collectOrgIDs(it)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("collectOrgIDs error = %v, want PermissionDenied (it must not be swallowed)", err)
	}
	if len(orgs) != 0 {
		t.Fatalf("orgs = %v, want none", orgs)
	}
}

// fakeServiceScanner returns count refs for its service and then err.
type fakeServiceScanner struct {
	service string
	count   int
	err     error
}

func (f *fakeServiceScanner) Scan(context.Context) ([]*pb.ResourceRef, error) {
	refs := make([]*pb.ResourceRef, 0, f.count)
	for i := 0; i < f.count; i++ {
		refs = append(refs, &pb.ResourceRef{
			Id:      fmt.Sprintf("%s-%d", f.service, i),
			Service: f.service,
			Type:    f.service + ".googleapis.com/Thing",
		})
	}
	return refs, f.err
}

func (f *fakeServiceScanner) DescribeResource(context.Context, string) (*pb.Resource, error) {
	return nil, errors.New("not implemented")
}

func newScanErrorTestProvider(scanners map[string]ServiceScanner) *GCPProvider {
	p := NewGCPProvider()
	p.enhancedScanner = &EnhancedResourceScanner{scanners: scanners, concurrency: 2}
	p.relationships = NewRelationshipExtractor()
	p.initialized.Store(true)
	return p
}

func TestBatchScanReportsFailedServiceAndKeepsResources(t *testing.T) {
	denied := status.Error(codes.PermissionDenied, "storage.buckets.list denied")
	p := newScanErrorTestProvider(map[string]ServiceScanner{
		"compute": &fakeServiceScanner{service: "compute", count: 2},
		"storage": &fakeServiceScanner{service: "storage", count: 1, err: denied},
	})

	resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{})
	if err != nil {
		t.Fatalf("BatchScan: %v", err)
	}
	if len(resp.Resources) != 3 {
		t.Fatalf("got %d resources, want 3 (every observed resource)", len(resp.Resources))
	}
	errs, notices := shared.SplitScanErrors(resp.Errors)
	if len(errs) != 1 || len(notices) != 0 {
		t.Fatalf("Errors = %v, want one error entry and no notices", resp.Errors)
	}
	if shared.IsScanNotice(errs[0]) || !strings.Contains(errs[0], "storage") {
		t.Fatalf("entry %q must be an error naming the storage service", errs[0])
	}
}

func TestBatchScanReportsNarrowedScope(t *testing.T) {
	scanners := map[string]ServiceScanner{
		"compute": &fakeServiceScanner{service: "compute", count: 2},
	}
	tests := []struct {
		name       string
		orgErr     error
		wantErrors int
	}{
		{"no org access is the whole scope", status.Error(codes.PermissionDenied, "denied"), 0},
		{"outage narrows the scope", status.Error(codes.Unavailable, "try again"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newScanErrorTestProvider(scanners)
			p.orgDiscoveryErr = tt.orgErr

			resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{})
			if err != nil {
				t.Fatalf("BatchScan: %v", err)
			}
			errs, _ := shared.SplitScanErrors(resp.Errors)
			if len(errs) != tt.wantErrors {
				t.Fatalf("Errors = %v, want %d error entries", resp.Errors, tt.wantErrors)
			}
			if len(resp.Resources) != 2 {
				t.Fatalf("got %d resources, want 2", len(resp.Resources))
			}
		})
	}
}

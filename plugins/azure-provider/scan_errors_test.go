package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/internal/shared/paging"
)

const (
	testSubscription = "00000000-0000-0000-0000-000000000001"
	testStorageID    = "/subscriptions/" + testSubscription +
		"/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/acct"
	testVMID = "/subscriptions/" + testSubscription +
		"/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"
)

// fakeARM serves scripted list pages and fails GetByID for the IDs in describeFail.
type fakeARM struct {
	pages        []armresources.ClientListResponse
	fetches      int
	describeFail map[string]error
}

func (f *fakeARM) NewListPager(
	*armresources.ClientListOptions,
) *runtime.Pager[armresources.ClientListResponse] {
	return runtime.NewPager(runtime.PagingHandler[armresources.ClientListResponse]{
		More: func(page armresources.ClientListResponse) bool {
			return page.NextLink != nil && *page.NextLink != ""
		},
		Fetcher: func(
			context.Context, *armresources.ClientListResponse,
		) (armresources.ClientListResponse, error) {
			// Past the end of the script, keep serving the last page: a server that never stops.
			page := f.pages[min(f.fetches, len(f.pages)-1)]
			f.fetches++
			return page, nil
		},
	})
}

func (f *fakeARM) GetByID(
	_ context.Context, resourceID, _ string, _ *armresources.ClientGetByIDOptions,
) (armresources.ClientGetByIDResponse, error) {
	if err := f.describeFail[resourceID]; err != nil {
		return armresources.ClientGetByIDResponse{}, err
	}
	var resp armresources.ClientGetByIDResponse
	resp.ID = to.Ptr(resourceID)
	return resp, nil
}

func listPage(nextLink string, ids ...string) armresources.ClientListResponse {
	var page armresources.ClientListResponse
	for _, id := range ids {
		parts := strings.Split(id, "/")
		page.Value = append(page.Value, &armresources.GenericResourceExpanded{
			ID:   to.Ptr(id),
			Name: to.Ptr(parts[len(parts)-1]),
			Type: to.Ptr(parts[6] + "/" + parts[7]),
		})
	}
	if nextLink != "" {
		page.NextLink = to.Ptr(nextLink)
	}
	return page
}

func newScanErrorTestProvider(arm *fakeARM) *AzureProvider {
	p := NewAzureProvider()
	p.subscriptionID = testSubscription
	p.scanner = &AzureResourceScanner{subscriptionID: testSubscription, resourcesClient: arm}
	p.initialized.Store(true)
	return p
}

func TestBatchScanReportsFailedDescribeAndKeepsResources(t *testing.T) {
	arm := &fakeARM{
		pages:        []armresources.ClientListResponse{listPage("", testStorageID, testVMID)},
		describeFail: map[string]error{testStorageID: errors.New("throttled")},
	}
	p := newScanErrorTestProvider(arm)

	resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{
		Services: []string{"storage", "compute"},
	})
	if err != nil {
		t.Fatalf("BatchScan: %v", err)
	}
	if len(resp.Resources) != 2 {
		t.Fatalf("got %d resources, want 2 (the undescribed one is kept)", len(resp.Resources))
	}
	errs, notices := shared.SplitScanErrors(resp.Errors)
	if len(errs) != 1 || len(notices) != 0 {
		t.Fatalf("Errors = %v, want exactly one error entry", resp.Errors)
	}
	if shared.IsScanNotice(errs[0]) || !strings.Contains(errs[0], "storage") {
		t.Fatalf("entry %q must be an error naming the storage service", errs[0])
	}
}

func TestScanWithFilterStopsOnRepeatedNextLink(t *testing.T) {
	arm := &fakeARM{pages: []armresources.ClientListResponse{listPage("same", testVMID)}}
	s := &AzureResourceScanner{subscriptionID: testSubscription, resourcesClient: arm}

	resources, err := s.scanWithFilter(context.Background(), "")
	if !errors.Is(err, paging.ErrTokenRepeated) {
		t.Fatalf("scanWithFilter error = %v, want ErrTokenRepeated", err)
	}
	if arm.fetches > 3 {
		t.Fatalf("fetched %d pages for a link that never advanced", arm.fetches)
	}
	if len(resources) == 0 {
		t.Fatal("resources from pages before the repeat were dropped")
	}
}

// fakeGraph serves Resource Graph pages: one resource each, with skipTokens[i] as page i's token.
type fakeGraph struct {
	skipTokens []string
	failAt     int // page index that fails; -1 for none
	calls      int
}

func (f *fakeGraph) Resources(
	context.Context, armresourcegraph.QueryRequest, *armresourcegraph.ClientResourcesOptions,
) (armresourcegraph.ClientResourcesResponse, error) {
	call := f.calls
	f.calls++
	if call == f.failAt {
		return armresourcegraph.ClientResourcesResponse{}, errors.New("graph unavailable")
	}
	var resp armresourcegraph.ClientResourcesResponse
	resp.Data = []interface{}{map[string]interface{}{
		"id":   fmt.Sprintf("%s-%d", testVMID, call),
		"name": "vm",
		"type": "microsoft.compute/virtualmachines",
	}}
	if token := f.skipTokens[min(call, len(f.skipTokens)-1)]; token != "" {
		resp.SkipToken = to.Ptr(token)
	}
	return resp, nil
}

func TestExecuteQueryBoundsPagination(t *testing.T) {
	t.Run("repeated skip token", func(t *testing.T) {
		graph := &fakeGraph{skipTokens: []string{"same"}, failAt: -1}
		c := &ResourceGraphClient{client: graph}
		resources, err := c.executeQuery(context.Background(), "Resources")
		if !errors.Is(err, paging.ErrTokenRepeated) {
			t.Fatalf("executeQuery error = %v, want ErrTokenRepeated", err)
		}
		if graph.calls > 3 || len(resources) == 0 {
			t.Fatalf("calls = %d, resources = %d; want a bounded loop that kept its pages",
				graph.calls, len(resources))
		}
	})
	t.Run("failed page keeps earlier pages", func(t *testing.T) {
		graph := &fakeGraph{skipTokens: []string{"t1", "t2"}, failAt: 1}
		c := &ResourceGraphClient{client: graph}
		resources, err := c.executeQuery(context.Background(), "Resources")
		if err == nil || len(resources) != 1 {
			t.Fatalf("got %d resources, err %v; want 1 resource and an error", len(resources), err)
		}
	})
}

func TestInitializeFailsWhenResourcesClientCannotBeBuilt(t *testing.T) {
	buildErr := errors.New("bad client options")
	restore := newResourcesClient
	newResourcesClient = func(string, azcore.TokenCredential) (armResourcesAPI, error) {
		return nil, buildErr
	}
	t.Cleanup(func() { newResourcesClient = restore })

	p := NewAzureProvider()
	resp, err := p.Initialize(context.Background(), &pb.InitializeRequest{
		Config: map[string]string{"subscription_id": testSubscription, "tenant_id": "tenant"},
	})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if resp.Success || !strings.Contains(resp.Error, buildErr.Error()) {
		t.Fatalf("Initialize = success %t, error %q; want failure naming %q",
			resp.Success, resp.Error, buildErr)
	}
	if p.initialized.Load() || p.scanner != nil {
		t.Fatal("a failed Initialize must not leave a usable provider with a nil client")
	}
}

// nestedMap returns a map nested depth levels deep with leaf as the innermost value.
func nestedMap(depth int, leaf interface{}) map[string]interface{} {
	root := map[string]interface{}{}
	current := root
	for i := 1; i < depth; i++ {
		next := map[string]interface{}{}
		current["n"] = next
		current = next
	}
	current["n"] = leaf
	return root
}

func TestJSONRecursionIsDepthBounded(t *testing.T) {
	c := &ResourceGraphClient{}
	if !c.containsReference(nestedMap(5, testVMID), testVMID, 0) {
		t.Fatal("a reference at depth 5 must be found")
	}
	if c.containsReference(nestedMap(100, testVMID), testVMID, 0) {
		t.Fatal("a reference at depth 100 was found, so the walk went past maxJSONDepth")
	}

	propertyTypes := make(map[string]map[string]bool)
	c.analyzeProperties(nestedMap(100, "leaf"), "", propertyTypes, 0)
	for key := range propertyTypes {
		if levels := strings.Count(key, ".") + 1; levels > maxJSONDepth+1 {
			t.Fatalf("analyzed key %d levels deep, past maxJSONDepth %d", levels, maxJSONDepth)
		}
	}
	if len(propertyTypes) == 0 {
		t.Fatal("no properties analyzed")
	}
}

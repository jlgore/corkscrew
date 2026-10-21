package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// countingARM wraps fakeARM and records how many GetByID calls run at once.
// Each call holds its slot briefly so that overlap, or its absence, shows.
type countingARM struct {
	*fakeARM
	inFlight     atomic.Int32
	inFlightPeak atomic.Int32
	calls        atomic.Int32
}

func (c *countingARM) GetByID(
	ctx context.Context, resourceID, apiVersion string,
	options *armresources.ClientGetByIDOptions,
) (armresources.ClientGetByIDResponse, error) {
	c.calls.Add(1)
	current := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	for {
		peak := c.inFlightPeak.Load()
		if current <= peak || c.inFlightPeak.CompareAndSwap(peak, current) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	return c.fakeARM.GetByID(ctx, resourceID, apiVersion, options)
}

func storageIDs(count int) []string {
	ids := make([]string, 0, count)
	for index := 0; index < count; index++ {
		ids = append(ids, fmt.Sprintf("/subscriptions/%s/resourceGroups/rg/providers/"+
			"Microsoft.Storage/storageAccounts/acct%d", testSubscription, index))
	}
	return ids
}

// ScanServiceForResources used to issue one ARM GET per resource in sequence.
// It must now overlap them, never beyond describeConcurrency, keep every
// resource in listing order, and still report every failed describe.
func TestScanServiceForResourcesDescribesConcurrently(t *testing.T) {
	ids := storageIDs(describeConcurrency * 3)
	arm := &countingARM{fakeARM: &fakeARM{
		pages: []armresources.ClientListResponse{listPage("", ids...)},
		describeFail: map[string]error{
			ids[1]: errors.New("throttled"),
			ids[7]: errors.New("throttled"),
		},
	}}
	scanner := testScanner(arm)

	resources, err := scanner.ScanServiceForResources(context.Background(), "storage", nil)
	if len(resources) != len(ids) {
		t.Fatalf("got %d resources, want %d", len(resources), len(ids))
	}
	for index, resource := range resources {
		if resource.Id != ids[index] {
			t.Fatalf("resource %d = %s, want %s (listing order)", index, resource.Id, ids[index])
		}
	}
	if err == nil || !strings.Contains(err.Error(), "describe failed for 2 of") {
		t.Fatalf("err = %v, want both describe failures counted", err)
	}
	if calls := arm.calls.Load(); int(calls) != len(ids) {
		t.Fatalf("GetByID calls = %d, want %d: a failure must not cancel the rest", calls,
			len(ids))
	}
	peak := arm.inFlightPeak.Load()
	if peak < 2 {
		t.Fatalf("peak in-flight GETs = %d, want them to overlap", peak)
	}
	if peak > describeConcurrency {
		t.Fatalf("peak in-flight GETs = %d, want at most %d", peak, describeConcurrency)
	}
}

// describeEach is shared by the ARM and Resource Graph paths; it returns one
// result per ref, in ref order, with each ref's own error.
func TestDescribeEachKeepsOrderAndErrors(t *testing.T) {
	refs := make([]*pb.ResourceRef, 0, 40)
	for index := 0; index < 40; index++ {
		refs = append(refs, &pb.ResourceRef{Id: fmt.Sprintf("r%d", index)})
	}
	failure := errors.New("denied")
	resources, errs := describeEach(context.Background(), refs,
		func(_ context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
			if ref.Id == "r5" {
				return &pb.Resource{Id: ref.Id}, failure
			}
			return &pb.Resource{Id: ref.Id}, nil
		})
	if len(resources) != len(refs) || len(errs) != len(refs) {
		t.Fatalf("got %d resources and %d errors, want %d", len(resources), len(errs), len(refs))
	}
	for index, ref := range refs {
		if resources[index].Id != ref.Id {
			t.Fatalf("resource %d = %s, want %s", index, resources[index].Id, ref.Id)
		}
		if (errs[index] != nil) != (ref.Id == "r5") {
			t.Fatalf("error %d = %v", index, errs[index])
		}
	}
}

// The DEBUG logs used to print for every resource; they now need
// CORKSCREW_DEBUG set to a true value.
func TestDebugEnabledParsesFlag(t *testing.T) {
	if debugEnabled("") {
		t.Fatal("empty CORKSCREW_DEBUG enables debug logging")
	}
	if debugEnabled("0") || debugEnabled("false") {
		t.Fatal("CORKSCREW_DEBUG=0/false enables debug logging")
	}
	if !debugEnabled("1") || !debugEnabled("true") {
		t.Fatal("CORKSCREW_DEBUG=1/true does not enable debug logging")
	}
}

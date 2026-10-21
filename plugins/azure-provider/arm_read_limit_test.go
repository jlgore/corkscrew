package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"golang.org/x/time/rate"
)

// countingWaiter records every Wait and fails them all when err is set.
type countingWaiter struct {
	waits atomic.Int32
	err   error
}

func (c *countingWaiter) Wait(context.Context) error {
	c.waits.Add(1)
	return c.err
}

// testScanner builds a scanner whose ARM reads are not throttled, so tests stay fast.
func testScanner(arm armResourcesAPI) *AzureResourceScanner {
	return &AzureResourceScanner{
		subscriptionID:  testSubscription,
		resourcesClient: arm,
		armReadLimiter:  rate.NewLimiter(rate.Inf, 0),
	}
}

// The concurrency cap alone let 16 GETs of ~100 ms run at ~160 requests per second, past ARM's
// refill, so large services exhausted their retries on 429s. Every GET must now take a token
// from the scanner's shared read limiter first.
func TestDescribeWaitsOnARMReadLimiterPerGet(t *testing.T) {
	ids := storageIDs(describeConcurrency * 2)
	arm := &countingARM{fakeARM: &fakeARM{
		pages: []armresources.ClientListResponse{listPage("", ids...)},
	}}
	waiter := &countingWaiter{}
	scanner := testScanner(arm)
	scanner.armReadLimiter = waiter

	resources, err := scanner.ScanServiceForResources(context.Background(), "storage", nil)
	if err != nil {
		t.Fatalf("ScanServiceForResources: %v", err)
	}
	if len(resources) != len(ids) {
		t.Fatalf("got %d resources, want %d", len(resources), len(ids))
	}
	if waits := waiter.waits.Load(); int(waits) != len(ids) {
		t.Fatalf("limiter waits = %d, want one per GET (%d)", waits, len(ids))
	}
	if calls := arm.calls.Load(); int(calls) != len(ids) {
		t.Fatalf("GetByID calls = %d, want %d", calls, len(ids))
	}
}

// A failed Wait (the scan's ctx ended) must skip the GET and surface as a describe failure,
// so the scan is reported partial rather than complete.
func TestDescribeSkipsGetWhenLimiterWaitFails(t *testing.T) {
	ids := storageIDs(3)
	arm := &countingARM{fakeARM: &fakeARM{
		pages: []armresources.ClientListResponse{listPage("", ids...)},
	}}
	scanner := testScanner(arm)
	scanner.armReadLimiter = &countingWaiter{err: context.Canceled}

	resources, err := scanner.ScanServiceForResources(context.Background(), "storage", nil)
	if len(resources) != len(ids) {
		t.Fatalf("got %d resources, want %d kept from the listing", len(resources), len(ids))
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "describe failed for 3 of 3") {
		t.Fatalf("err = %v, want every describe counted as failed", err)
	}
	if calls := arm.calls.Load(); calls != 0 {
		t.Fatalf("GetByID calls = %d, want none after the limiter refused", calls)
	}
}

// The provider's scanner must be built with a read limiter at the ARM-safe rate.
func TestNewScannerLimitsARMReads(t *testing.T) {
	original := newResourcesClient
	defer func() { newResourcesClient = original }()
	newResourcesClient = func(string, azcore.TokenCredential) (armResourcesAPI, error) {
		return &fakeARM{}, nil
	}
	scanner, err := NewAzureResourceScanner(nil, testSubscription)
	if err != nil {
		t.Fatalf("NewAzureResourceScanner: %v", err)
	}
	limiter, ok := scanner.armReadLimiter.(*rate.Limiter)
	if !ok {
		t.Fatalf("armReadLimiter = %T, want *rate.Limiter", scanner.armReadLimiter)
	}
	if limiter.Limit() != armReadRatePerSecond || limiter.Burst() != armReadBurst {
		t.Fatalf("limiter = %v/s burst %d, want %v/s burst %d", limiter.Limit(),
			limiter.Burst(), armReadRatePerSecond, armReadBurst)
	}
}

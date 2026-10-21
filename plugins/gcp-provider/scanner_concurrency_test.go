package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// countingScanner records how many Scan calls run at once. Each call holds
// its slot briefly so that an unbounded fan-out would overlap visibly.
type countingScanner struct {
	service      string
	inFlight     *atomic.Int32
	inFlightPeak *atomic.Int32
	err          error
}

func (c *countingScanner) Scan(context.Context) ([]*pb.ResourceRef, error) {
	current := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	for {
		peak := c.inFlightPeak.Load()
		if current <= peak || c.inFlightPeak.CompareAndSwap(peak, current) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	return []*pb.ResourceRef{{Id: c.service + "-0", Service: c.service}}, c.err
}

func (c *countingScanner) DescribeResource(context.Context, string) (*pb.Resource, error) {
	return nil, errors.New("not implemented")
}

// ScanAllServices used to start one goroutine per scanner. It must now keep
// at most concurrency scans in flight, and still return every resource and
// every service error.
func TestScanAllServicesBoundsConcurrency(t *testing.T) {
	var inFlight, inFlightPeak atomic.Int32
	scanners := map[string]ServiceScanner{}
	for index := 0; index < 8; index++ {
		service := fmt.Sprintf("service-%d", index)
		scanner := &countingScanner{service: service, inFlight: &inFlight,
			inFlightPeak: &inFlightPeak}
		if index == 3 {
			scanner.err = errors.New("denied")
		}
		scanners[service] = scanner
	}
	rs := &ResourceScanner{scanners: scanners, concurrency: 2}

	resources, err := rs.ScanAllServices(context.Background())
	if len(resources) != 8 {
		t.Fatalf("got %d resources, want 8", len(resources))
	}
	if err == nil {
		t.Fatal("want the service-3 error, got nil")
	}
	if peak := inFlightPeak.Load(); peak != 2 {
		t.Fatalf("peak in-flight scans = %d, want 2", peak)
	}
}

// cancellingScanner cancels the scan's context from inside its own Scan, so
// the cancellation lands while ScanAllServices is still launching services.
type cancellingScanner struct {
	service string
	calls   *atomic.Int32
	cancel  context.CancelFunc
}

func (c *cancellingScanner) Scan(context.Context) ([]*pb.ResourceRef, error) {
	c.calls.Add(1)
	c.cancel()
	return []*pb.ResourceRef{{Id: c.service + "-0", Service: c.service}}, nil
}

func (c *cancellingScanner) DescribeResource(context.Context, string) (*pb.Resource, error) {
	return nil, errors.New("not implemented")
}

// ScanAllServices used to block on a free slot without watching ctx, so after
// cancellation it kept launching every remaining service. It must stop
// launching, keep what the launched scan observed, and report each unlaunched
// service as a cancellation error so the scan is partial (ADR 0023).
func TestScanAllServicesStopsLaunchingAfterCancel(t *testing.T) {
	const serviceCount = 6
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	scanners := map[string]ServiceScanner{}
	for index := 0; index < serviceCount; index++ {
		service := fmt.Sprintf("service-%d", index)
		scanners[service] = &cancellingScanner{service: service, calls: &calls, cancel: cancel}
	}
	rs := &ResourceScanner{scanners: scanners, concurrency: 1}

	resources, err := rs.ScanAllServices(ctx)
	if got := calls.Load(); got != 1 {
		t.Fatalf("Scan calls = %d, want 1: no scan may start after cancellation", got)
	}
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want the 1 the launched scan observed", len(resources))
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := strings.Count(err.Error(), "not scanned"); got != serviceCount-1 {
		t.Fatalf("err reports %d unscanned services, want %d: %v", got, serviceCount-1, err)
	}
}

func TestNewResourceScannerUsesProviderConcurrency(t *testing.T) {
	rs := NewResourceScanner(nil, 7)
	if rs.concurrency != 7 {
		t.Fatalf("concurrency = %d, want 7", rs.concurrency)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
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

func TestNewResourceScannerUsesProviderConcurrency(t *testing.T) {
	rs := NewResourceScanner(nil, 7)
	if rs.concurrency != 7 {
		t.Fatalf("concurrency = %d, want 7", rs.concurrency)
	}
}

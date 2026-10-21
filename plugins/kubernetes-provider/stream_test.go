package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/grpc"
)

// streamTestCount exceeds streamBufferSize so the producer must block on the consumer.
const streamTestCount = streamBufferSize*2 + 7

// fakeStreamer emits count resources, closes the channel it owns, and returns err.
type fakeStreamer struct {
	count int
	err   error
}

func (f *fakeStreamer) StreamScanResources(
	ctx context.Context, _ []string, resourceChan chan<- *pb.Resource,
) error {
	defer close(resourceChan)
	for i := 0; i < f.count; i++ {
		select {
		case resourceChan <- &pb.Resource{Id: fmt.Sprintf("r-%d", i)}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

// fakeStream records sent resources and fails every Send once sendErr is set.
type fakeStream struct {
	grpc.ServerStream
	ctx     context.Context
	sendErr error
	sent    []*pb.Resource
}

func (s *fakeStream) Context() context.Context { return s.ctx }

func (s *fakeStream) Send(resource *pb.Resource) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, resource)
	return nil
}

// newStreamTestProvider wires two healthy clusters to fake streamers, so the stream
// loops must move on from the first cluster's closed channel to the second.
func newStreamTestProvider(count int, err error) *KubernetesProvider {
	return &KubernetesProvider{
		initialized: true,
		config:      &ProviderConfig{},
		clusters: map[string]*ClusterConnection{
			"a": {Name: "a", Healthy: true},
			"b": {Name: "b", Healthy: true},
		},
		newStreamer: func(*ClusterConnection) resourceStreamer {
			return &fakeStreamer{count: count, err: err}
		},
	}
}

func TestStreamScanSendsAllResources(t *testing.T) {
	p := newStreamTestProvider(streamTestCount, nil)
	stream := &fakeStream{ctx: context.Background()}

	if err := p.StreamScan(&pb.StreamScanRequest{}, stream); err != nil {
		t.Fatalf("StreamScan: %v", err)
	}
	if len(stream.sent) != 2*streamTestCount {
		t.Fatalf("sent %d resources, want %d", len(stream.sent), 2*streamTestCount)
	}
}

// Before the fix, the break inside select left StreamScanService spinning on the closed
// channel of the first cluster, so this test would hang.
func TestStreamScanServiceSendsAllResources(t *testing.T) {
	p := newStreamTestProvider(streamTestCount, nil)
	stream := &fakeStream{ctx: context.Background()}

	if err := p.StreamScanService(&pb.ScanServiceRequest{Service: "apps"}, stream); err != nil {
		t.Fatalf("StreamScanService: %v", err)
	}
	if len(stream.sent) != 2*streamTestCount {
		t.Fatalf("sent %d resources, want %d", len(stream.sent), 2*streamTestCount)
	}
}

// Run with -count=100: the old select loop dropped this error about half the time.
func TestStreamScanReturnsScannerError(t *testing.T) {
	scanErr := errors.New("scan failed")
	p := newStreamTestProvider(5, scanErr)
	stream := &fakeStream{ctx: context.Background()}

	err := p.StreamScan(&pb.StreamScanRequest{}, stream)
	if !errors.Is(err, scanErr) {
		t.Fatalf("StreamScan error = %v, want %v", err, scanErr)
	}
	// The first cluster's error stops the scan before the second cluster starts.
	if len(stream.sent) != 5 {
		t.Fatalf("sent %d resources, want 5", len(stream.sent))
	}
}

// Run with -count=100, for the same reason as TestStreamScanReturnsScannerError.
func TestStreamScanServiceReturnsScannerError(t *testing.T) {
	scanErr := errors.New("scan failed")
	p := newStreamTestProvider(5, scanErr)
	stream := &fakeStream{ctx: context.Background()}

	err := p.StreamScanService(&pb.ScanServiceRequest{Service: "apps"}, stream)
	if !errors.Is(err, scanErr) {
		t.Fatalf("StreamScanService error = %v, want %v", err, scanErr)
	}
}

func TestStreamScanStopsProducerOnSendError(t *testing.T) {
	sendErr := errors.New("client gone")
	p := newStreamTestProvider(streamTestCount, nil)
	stream := &fakeStream{ctx: context.Background(), sendErr: sendErr}

	err := p.StreamScan(&pb.StreamScanRequest{}, stream)
	if !errors.Is(err, sendErr) {
		t.Fatalf("StreamScan error = %v, want %v", err, sendErr)
	}
}

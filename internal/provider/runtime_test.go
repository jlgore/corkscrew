package provider

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

type fakeRuntimeProvider struct {
	shared.CloudProvider
	initializations atomic.Int32
}

func (p *fakeRuntimeProvider) Initialize(context.Context, *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.initializations.Add(1)
	return &pb.InitializeResponse{Success: true}, nil
}

type fakeLauncher struct {
	provider *fakeRuntimeProvider
	launches atomic.Int32
	closes   atomic.Int32
}

func (l *fakeLauncher) Launch(string) (shared.CloudProvider, io.Closer, error) {
	l.launches.Add(1)
	return l.provider, closerFunc(func() error { l.closes.Add(1); return nil }), nil
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestRuntimeCachesSessionsAndGatesCapabilities(t *testing.T) {
	t.Parallel()

	registry := &Registry{descriptors: map[string]Descriptor{
		"acme": {
			Name:           "acme",
			Origin:         OriginCustom,
			Installed:      true,
			ExecutablePath: "/plugins/acme-provider",
			Capabilities:   []Capability{CapabilityBatchScan},
		},
	}}
	launched := &fakeRuntimeProvider{}
	launcher := &fakeLauncher{provider: launched}
	runtime := NewRuntime(registry, launcher, io.Discard)
	t.Cleanup(func() { _ = runtime.Close() })

	first, err := runtime.Open(context.Background(), "acme", map[string]string{"token": "secret"})
	if err != nil {
		t.Fatalf("Open(first): %v", err)
	}
	second, err := runtime.Open(context.Background(), "acme", map[string]string{"token": "secret"})
	if err != nil {
		t.Fatalf("Open(second): %v", err)
	}
	if first != second || launcher.launches.Load() != 1 || launched.initializations.Load() != 1 {
		t.Fatalf("session cache: first=%p second=%p launches=%d initializations=%d", first, second, launcher.launches.Load(), launched.initializations.Load())
	}
	if err := first.Require(CapabilityBatchScan); err != nil {
		t.Fatalf("Require(batch_scan): %v", err)
	}
	if err := first.Require(CapabilityStreamScan); err == nil {
		t.Fatal("Require(stream_scan) error = nil, want unsupported capability")
	}
}

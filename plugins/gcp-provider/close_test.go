package main

import (
	"context"
	"errors"
	"testing"

	asset "cloud.google.com/go/asset/apiv1"
	"google.golang.org/api/option"
)

type countingCloser struct {
	closes int
	err    error
}

func (c *countingCloser) Close() error {
	c.closes++
	return c.err
}

func TestClientFactoryCloseClosesConnectionHoldingClients(t *testing.T) {
	ok := &countingCloser{}
	failing := &countingCloser{err: errors.New("boom")}
	cf := NewClientFactory()
	cf.clients["grpc-ok"] = ok
	cf.clients["grpc-failing"] = failing
	cf.clients["rest"] = struct{}{} // a REST service has no Close

	err := cf.Close()
	if !errors.Is(err, failing.err) {
		t.Fatalf("Close error = %v, want it to wrap %v", err, failing.err)
	}
	if ok.closes != 1 || failing.closes != 1 {
		t.Fatalf("closes = %d, %d; want 1, 1", ok.closes, failing.closes)
	}
	if len(cf.clients) != 0 {
		t.Fatalf("cache holds %d clients after Close, want 0", len(cf.clients))
	}
	if err := cf.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
	if ok.closes != 1 {
		t.Fatalf("second Close closed a client again (%d closes)", ok.closes)
	}
}

// newOfflineAssetClient builds a real Asset Inventory client that never dials: gRPC connects
// lazily, and the endpoint is unroutable.
func newOfflineAssetClient(t *testing.T) *asset.Client {
	t.Helper()
	client, err := asset.NewClient(context.Background(),
		option.WithoutAuthentication(), option.WithEndpoint("127.0.0.1:1"))
	if err != nil {
		t.Fatalf("asset.NewClient: %v", err)
	}
	return client
}

func TestProviderCloseReleasesClients(t *testing.T) {
	raw := newOfflineAssetClient(t)
	closer := &countingCloser{}
	p := NewGCPProvider()
	p.assetInventory = &AssetInventoryClient{client: raw}
	p.clientFactory = NewClientFactory()
	p.clientFactory.clients["grpc"] = closer
	p.initialized.Store(true)

	p.Close()

	if p.assetInventory != nil || p.clientFactory != nil {
		t.Fatalf("Close kept clients: asset=%v factory=%v", p.assetInventory, p.clientFactory)
	}
	if p.initialized.Load() {
		t.Fatal("provider still reports initialized after Close")
	}
	if closer.closes != 1 {
		t.Fatalf("factory client closes = %d, want 1", closer.closes)
	}
	if err := raw.Close(); err == nil {
		t.Fatal("Asset Inventory connection was still open after provider Close")
	}

	p.Close() // idempotent
}

func TestAssetInventoryClientCloseIsIdempotent(t *testing.T) {
	aic := &AssetInventoryClient{client: newOfflineAssetClient(t)}
	if err := aic.Close(); err != nil {
		t.Fatalf("first Close = %v", err)
	}
	if err := aic.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
}

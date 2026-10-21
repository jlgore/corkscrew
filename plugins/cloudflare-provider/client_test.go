package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/accounts"
	"github.com/jlgore/corkscrew/internal/providers/cloudflareauth"
)

func testAuth(baseURL string) *cloudflareauth.ResolvedAuth {
	return &cloudflareauth.ResolvedAuth{
		Method:      cloudflareauth.AuthMethodAPIToken,
		AccessToken: "test",
		BaseURL:     baseURL + "/",
	}
}

// A server that never answers must fail one attempt at the request timeout, not hang until the
// scope deadline. The SDK does not retry a timed-out attempt, so exactly one request arrives.
func TestClientOptionsRequestTimeout(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	client := cloudflare.NewClient(clientOptions(testAuth(srv.URL), 50*time.Millisecond)...)
	start := time.Now()
	_, err := client.Accounts.List(context.Background(), accounts.AccountListParams{})
	if err == nil {
		t.Fatal("List succeeded against a server that never answers")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %s, want the 50ms request timeout to fire", elapsed)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

// The SDK default is 10 retries; the client must use retriesMax.
func TestClientOptionsMaxRetries(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		// Retry-After-Ms: 0 skips the SDK backoff so the test runs in milliseconds.
		w.Header().Set("Retry-After-Ms", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := newCloudflareClient(testAuth(srv.URL))
	_, err := client.Accounts.List(context.Background(), accounts.AccountListParams{})
	if err == nil {
		t.Fatal("List succeeded against a server that always returns 503")
	}
	if got, want := requests.Load(), int32(retriesMax+1); got != want {
		t.Fatalf("requests = %d, want %d (one attempt plus retriesMax)", got, want)
	}
}

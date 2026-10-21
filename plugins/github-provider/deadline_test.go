package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-github/v71/github"
	"github.com/jlgore/corkscrew/internal/shared"
)

// noSleepTransport is a test transport whose sleeper fails the test: a wait over the cap must
// return an error, never sleep.
func noSleepTransport(t *testing.T, rt http.RoundTripper) *rateLimitTransport {
	t.Helper()
	tr := newTestTransport(rt)
	tr.maxWait = retryWaitMax
	tr.sleeper = func(context.Context, time.Duration) error {
		t.Error("transport slept for a wait over the cap")
		return nil
	}
	return tr
}

func TestTransport_PrimaryResetBeyondCapFailsPromptly(t *testing.T) {
	reset := time.Now().Add(time.Hour).Unix()
	first := newResp(403, map[string]string{
		"X-RateLimit-Remaining": "0",
		"X-RateLimit-Reset":     strconv.FormatInt(reset, 10),
	}, "rate limited")
	rt := &fakeRT{steps: []rtStep{{resp: first}}}
	tr := noSleepTransport(t, rt)

	start := time.Now()
	resp, err := roundTrip(t, tr)
	if !errors.Is(err, errRateLimitWaitTooLong) {
		t.Fatalf("err = %v, want errRateLimitWaitTooLong", err)
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil with the error", resp)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s, want an immediate error", elapsed)
	}
	if rt.calls != 1 {
		t.Errorf("calls = %d, want 1", rt.calls)
	}
	if closes := first.Body.(*closeCounter).closes; closes != 1 {
		t.Errorf("body closes = %d, want 1", closes)
	}
}

func TestTransport_RetryAfterBeyondCapFailsPromptly(t *testing.T) {
	retryAfter := strconv.Itoa(int((retryWaitMax + time.Minute) / time.Second))
	rt := &fakeRT{steps: []rtStep{
		{resp: newResp(429, map[string]string{"Retry-After": retryAfter}, "")},
	}}
	_, err := roundTrip(t, noSleepTransport(t, rt))
	if !errors.Is(err, errRateLimitWaitTooLong) {
		t.Fatalf("err = %v, want errRateLimitWaitTooLong", err)
	}
}

// The default wait budget must stay under the host's 5-minute scope deadline and under the
// client timeout that wraps the whole retry loop.
func TestTransport_DefaultWaitBudget(t *testing.T) {
	tr := newRateLimitTransport(nil).(*rateLimitTransport)
	if tr.maxWait != retryWaitMax {
		t.Fatalf("maxWait = %s, want %s", tr.maxWait, retryWaitMax)
	}
	budget := time.Duration(tr.maxRetries) * tr.maxWait
	if budget >= httpClientTimeout || httpClientTimeout >= 5*time.Minute {
		t.Errorf("retry budget %s, client timeout %s: want budget < timeout < 5m",
			budget, httpClientTimeout)
	}
}

func TestSleepCtx_CancelAbortsWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s, want an immediate return", elapsed)
	}
}

// A far-off reset seen through go-github must land as a scan error, not as the
// access-denied notice a plain 403 becomes, so the scan is partial (ADR 0023).
func TestRateLimitBeyondCapIsScanError(t *testing.T) {
	reset := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", reset)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer server.Close()

	client := github.NewClient(newHTTPClient(newRateLimitTransport(nil)))
	client, err := client.WithEnterpriseURLs(server.URL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.Repositories.ListByOrg(context.Background(), "acme", nil)
	if !errors.Is(err, errRateLimitWaitTooLong) {
		t.Fatalf("err = %v, want errRateLimitWaitTooLong", err)
	}
	var entries []string
	appendAPIError(&entries, "list repos", "acme", err)
	if len(entries) != 1 || shared.IsScanNotice(entries[0]) {
		t.Errorf("entries = %q, want one non-notice error", entries)
	}
}

func TestNewGitHubClient_HTTPClientHasTimeout(t *testing.T) {
	t.Run("token", func(t *testing.T) {
		_, httpClient, err := newGitHubClient(context.Background(), &providerConfig{Token: "x"})
		if err != nil {
			t.Fatal(err)
		}
		assertClientTimeout(t, httpClient)
	})
	t.Run("app installation", func(t *testing.T) {
		cfg := &providerConfig{AppID: 1, InstallationID: 2, PrivateKeyPath: writeTestKey(t)}
		_, httpClient, err := newGitHubClient(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		assertClientTimeout(t, httpClient)
	})
}

func assertClientTimeout(t *testing.T, client *http.Client) {
	t.Helper()
	if client == nil {
		t.Fatal("http client is nil")
	}
	if client.Timeout != httpClientTimeout {
		t.Errorf("Timeout = %s, want %s", client.Timeout, httpClientTimeout)
	}
}

func writeTestKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	path := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

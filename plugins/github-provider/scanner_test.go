package main

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

// TestParallelReposCancelWithFullSemaphore cancels the context while the loop
// is blocked waiting for a semaphore slot. The loop must stop without starting
// a goroutine that never acquired a slot, since that goroutine's release would
// block forever and hang wg.Wait.
func TestParallelReposCancelWithFullSemaphore(t *testing.T) {
	p := &GitHubProvider{concurrency: 1}
	repos := make([]*github.Repository, 3)
	for i := range repos {
		repos[i] = &github.Repository{Name: github.Ptr("r")}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var calls atomic.Int32
	fn := func(ctx context.Context, _ *github.Repository) ([]*pb.Resource, []string) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		// Hold the only slot past the cancel so the loop's select sees
		// ctx.Done rather than a freed slot.
		time.Sleep(50 * time.Millisecond)
		return nil, nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.parallelRepos(ctx, repos, fn)
	}()

	<-started
	// Give the loop time to block on the full semaphore before cancelling.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("parallelRepos did not return within 1s after cancel")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fn called %d times after cancel, want 1", got)
	}
}

// TestBatchScanListsReposAndAlertsOncePerScan runs the default BatchScan (repos, security,
// findings) against a fake API that counts requests. The repository list and each alert
// listing must be fetched once per scan, and a second scan must fetch them again: the cache
// lasts one scan, never across scans.
func TestBatchScanListsReposAndAlertsOncePerScan(t *testing.T) {
	countPaths := []string{
		"/orgs/acme/repos",
		"/repos/acme/app/dependabot/alerts",
		"/repos/acme/app/secret-scanning/alerts",
		"/repos/acme/app/code-scanning/alerts",
	}
	counts := map[string]*atomic.Int32{}
	mux := http.NewServeMux()
	for _, path := range countPaths {
		counter := &atomic.Int32{}
		counts[path] = counter
		body := `[]`
		if path == "/orgs/acme/repos" {
			body = `[{"name":"app","full_name":"acme/app","default_branch":"main",` +
				`"owner":{"login":"acme"}}]`
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			counter.Add(1)
			fmt.Fprint(w, body)
		})
	}
	p, _ := newTestProvider(t, mux)

	for scan := int32(1); scan <= 2; scan++ {
		resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{})
		if err != nil {
			t.Fatalf("scan %d: BatchScan: %v", scan, err)
		}
		if resp == nil {
			t.Fatalf("scan %d: nil response", scan)
		}
		for _, path := range countPaths {
			if got := counts[path].Load(); got != scan {
				t.Fatalf("scan %d: %s requested %d times in total, want %d",
					scan, path, got, scan)
			}
		}
	}
}

// TestAlertCacheRetriesErrorsAndKeepsNotices checks which alert listings a scan caches. A
// notice (403/404: the feature is off or out of scope) is the scan's answer for that repository
// and is replayed from the cache, so every service still reports it. Any other failure is not
// cached, so the next service retries instead of repeating a transient error.
func TestAlertCacheRetriesErrorsAndKeepsNotices(t *testing.T) {
	var secretCalls, codeCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/secret-scanning/alerts", func(w http.ResponseWriter,
		_ *http.Request) {
		secretCalls.Add(1)
		http.Error(w, `{"message":"disabled"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/repos/acme/app/code-scanning/alerts", func(w http.ResponseWriter,
		_ *http.Request) {
		if codeCalls.Add(1) == 1 {
			http.Error(w, `{"message":"unavailable"}`, http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `[]`)
	})
	p, _ := newTestProvider(t, mux)
	ctx := withScanCache(context.Background())

	for round := 1; round <= 3; round++ {
		var errs []string
		p.secretScanningAlerts(ctx, "acme", "app", &errs)
		if len(errs) != 1 || !shared.IsScanNotice(errs[0]) {
			t.Fatalf("round %d: secret scanning entries = %v, want one notice", round, errs)
		}
	}
	if got := secretCalls.Load(); got != 1 {
		t.Fatalf("secret scanning requests = %d, want 1 (notice cached)", got)
	}

	var first, second []string
	p.codeScanningAlerts(ctx, "acme", "app", &first)
	p.codeScanningAlerts(ctx, "acme", "app", &second)
	p.codeScanningAlerts(ctx, "acme", "app", &second)
	if len(first) != 1 || shared.IsScanNotice(first[0]) {
		t.Fatalf("first code scanning entries = %v, want one error", first)
	}
	if len(second) != 0 {
		t.Fatalf("later code scanning entries = %v, want none after the retry", second)
	}
	if got := codeCalls.Load(); got != 2 {
		t.Fatalf("code scanning requests = %d, want 2 (error retried, success cached)", got)
	}
}

package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
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

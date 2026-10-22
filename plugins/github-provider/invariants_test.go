package main

import (
	"context"
	"testing"

	"github.com/google/go-github/v71/github"
	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

// Only a successful listing fills the repository cache, and it sets reposListed at the same
// time, so repositories without the flag mean the cache was written somewhere else.
func TestListReposAssertsCacheOnlyHoldsListedRepos(t *testing.T) {
	ctx := withScanCache(context.Background())
	scanCacheFrom(ctx).repos = []*github.Repository{{}}
	p := &GitHubProvider{initialized: true, org: "acme", client: github.NewClient(nil)}
	asserttest.FailsWith(t, "repositories it listed", func() { p.listRepos(ctx) })
}

func TestCachedAlertsAssertsEndpoint(t *testing.T) {
	p := &GitHubProvider{initialized: true, org: "acme", client: github.NewClient(nil)}
	var errors []string
	asserttest.FailsWith(t, "endpoint", func() {
		p.cachedAlerts(context.Background(), alertSource{}, "acme", "repo", &errors)
	})
}

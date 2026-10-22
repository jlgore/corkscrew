package scanexec

import (
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// A worker that drops a job leaves the channel short. Treating the missing slot's zero value
// as a clean scope would let the scan claim completeness for a region it never scanned.
func TestCollectScopeResultsAssertsEveryScopeHasAResult(t *testing.T) {
	results := make(chan indexedScopeResult, 2)
	results <- indexedScopeResult{index: 0, ScopeResult: ScopeResult{Scope: "a"}}
	close(results)
	asserttest.FailsWith(t, "every scope has a result", func() {
		collectScopeResults(results, []string{"a", "b"})
	})
}

// A result filed under the wrong index would attribute one scope's resources to another.
func TestCollectScopeResultsAssertsResultsMatchTheirScope(t *testing.T) {
	results := make(chan indexedScopeResult, 2)
	results <- indexedScopeResult{index: 0, ScopeResult: ScopeResult{Scope: "b"}}
	results <- indexedScopeResult{index: 1, ScopeResult: ScopeResult{Scope: "a"}}
	close(results)
	asserttest.FailsWith(t, "result for scope", func() {
		collectScopeResults(results, []string{"a", "b"})
	})
}

func TestCollectScopeResultsKeepsRequestedOrder(t *testing.T) {
	results := make(chan indexedScopeResult, 2)
	results <- indexedScopeResult{index: 1, ScopeResult: ScopeResult{Scope: "b"}}
	results <- indexedScopeResult{index: 0, ScopeResult: ScopeResult{Scope: "a"}}
	close(results)
	scopes := collectScopeResults(results, []string{"a", "b"})
	if scopes[0].Scope != "a" || scopes[1].Scope != "b" {
		t.Fatalf("scopes = %+v, want requested order", scopes)
	}
}

// The merged list must hold every scope's resources before dedupe; a shortfall means
// aggregation dropped resources that the providers returned.
func TestAssertMergedTotalCatchesDroppedResources(t *testing.T) {
	scopes := []ScopeResult{
		{Scope: "a", Resources: []*pb.Resource{widget("a")}},
		{Scope: "b", Resources: []*pb.Resource{widget("b")}},
	}
	asserttest.FailsWith(t, "before dedupe", func() {
		assertMergedTotal(scopes, []*pb.Resource{widget("a")})
	})
	assertMergedTotal(scopes, []*pb.Resource{widget("a"), widget("b")})
}

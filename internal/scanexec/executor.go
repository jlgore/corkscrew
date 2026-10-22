// Package scanexec owns provider-neutral scan scheduling and aggregation.
package scanexec

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/internal/assert"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

type BatchScanner interface {
	BatchScan(context.Context, *pb.BatchScanRequest) (*pb.BatchScanResponse, error)
}

type Plan struct {
	Provider             string
	Scopes               []string
	Services             []string
	Filters              map[string]string
	IncludeRelationships bool
	MaxConcurrency       int
	ScopeTimeout         time.Duration
}

type Status string

const (
	StatusComplete Status = "complete"
	StatusPartial  Status = "partial"
	StatusFailed   Status = "failed"
)

type ScopeResult struct {
	Scope     string
	Resources []*pb.Resource
	Stats     *pb.ScanStats
	// Errors mean the scope was not fully observed; any entry makes the scan partial.
	Errors []string
	// Notices are informational provider entries (see shared.IsScanNotice).
	Notices []string
	// Unreachable is set when the provider returned no response for the scope at all.
	Unreachable bool
	Duration    time.Duration
}

type Outcome struct {
	Status    Status
	Resources []*pb.Resource
	Scopes    []ScopeResult
	Stats     *pb.ScanStats
	Duration  time.Duration
}

type EventKind string

const (
	EventScopeStarted   EventKind = "scope_started"
	EventScopeCompleted EventKind = "scope_completed"
	EventScopeFailed    EventKind = "scope_failed"
)

type Event struct {
	Kind      EventKind
	Provider  string
	Scope     string
	Completed int
	Total     int
	Message   string
}

type Observer func(Event)

type PartialError struct{ FailedScopes []string }

func (e *PartialError) Error() string {
	return fmt.Sprintf("scan completed with %d failed scope(s): %s", len(e.FailedScopes), strings.Join(e.FailedScopes, ", "))
}

type indexedScopeResult struct {
	index int
	ScopeResult
}

func Execute(ctx context.Context, scanner BatchScanner, plan Plan, observer Observer) (Outcome, error) {
	started := time.Now()
	if scanner == nil {
		return Outcome{Status: StatusFailed}, fmt.Errorf("batch scanner is required")
	}
	if len(plan.Scopes) == 0 {
		return Outcome{Status: StatusFailed}, fmt.Errorf("scan plan has no scopes")
	}
	concurrency := plan.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(plan.Scopes) {
		concurrency = len(plan.Scopes)
	}
	timeout := plan.ScopeTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	results := dispatchScopes(ctx, scanner, plan, concurrency, timeout, observer)
	outcome := Outcome{
		Scopes: collectScopeResults(results, plan.Scopes),
		Stats:  &pb.ScanStats{ServiceCounts: map[string]int32{}},
	}
	failed, unreachable := aggregateScopes(&outcome, plan, observer)
	outcome.Duration = time.Since(started)
	if ctx.Err() != nil {
		outcome.Status = StatusFailed
		return outcome, ctx.Err()
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		// Failed means nothing was observed. A scope that answered with some errors still
		// contributed resources, so the scan is partial: persisted, but never comparable.
		if unreachable == len(plan.Scopes) {
			outcome.Status = StatusFailed
		} else {
			outcome.Status = StatusPartial
		}
		return outcome, &PartialError{FailedScopes: failed}
	}
	outcome.Status = StatusComplete
	return outcome, nil
}

// dispatchScopes scans every scope of plan on concurrency workers. The returned channel
// receives exactly one result per scope and is closed once every worker has finished.
func dispatchScopes(ctx context.Context, scanner BatchScanner, plan Plan, concurrency int,
	timeout time.Duration, observer Observer) <-chan indexedScopeResult {
	assert.That(concurrency > 0, "scope concurrency is positive")
	assert.That(concurrency <= len(plan.Scopes), "scope concurrency is at most the scope count")
	jobs := make(chan int)
	results := make(chan indexedScopeResult, len(plan.Scopes))
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				result := scanScope(ctx, scanner, plan, plan.Scopes[index], concurrency, timeout)
				results <- indexedScopeResult{index: index, ScopeResult: result}
			}
		}()
	}
	go func() {
		for index, scope := range plan.Scopes {
			if observer != nil {
				observer(Event{Kind: EventScopeStarted, Provider: plan.Provider, Scope: scope,
					Total: len(plan.Scopes)})
			}
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()
	return results
}

// scanScope runs one scope's BatchScan. A transport error or an empty response marks the
// scope unreachable; provider-reported errors are split into errors and notices.
func scanScope(ctx context.Context, scanner BatchScanner, plan Plan, scope string,
	concurrency int, timeout time.Duration) ScopeResult {
	scopeStarted := time.Now()
	scopeCtx, cancel := context.WithTimeout(ctx, timeout)
	response, err := scanner.BatchScan(scopeCtx, &pb.BatchScanRequest{
		Services:             append([]string(nil), plan.Services...),
		Region:               scope,
		Filters:              cloneFilters(plan.Filters),
		IncludeRelationships: plan.IncludeRelationships,
		Concurrency:          int32(concurrency),
	})
	cancel()
	result := ScopeResult{Scope: scope, Duration: time.Since(scopeStarted)}
	if err != nil {
		result.Errors = []string{err.Error()}
		result.Unreachable = true
	} else if response == nil {
		result.Errors = []string{"provider returned an empty scan response"}
		result.Unreachable = true
	} else {
		result.Resources = response.Resources
		result.Stats = response.Stats
		result.Errors, result.Notices = shared.SplitScanErrors(response.Errors)
	}
	return result
}

// collectScopeResults drains results into requested scope order. Every scope is dispatched
// exactly once and every worker reports each job it takes, so a missing or misfiled result is
// a scheduler bug. Treating its zero value as a clean scope would let the scan claim
// completeness for a region it never scanned.
func collectScopeResults(results <-chan indexedScopeResult, scopes []string) []ScopeResult {
	collected := make([]ScopeResult, len(scopes))
	received := 0
	for result := range results {
		assert.That(result.index >= 0, "scope result index is not negative")
		assert.That(result.index < len(scopes), "scope result index is within the plan")
		collected[result.index] = result.ScopeResult
		received++
	}
	assert.Thatf(received == len(scopes),
		"every scope has a result: received %d results for %d scopes", received, len(scopes))
	for index, result := range collected {
		assert.Thatf(result.Scope == scopes[index],
			"result for scope %q is filed under scope %q", result.Scope, scopes[index])
	}
	return collected
}

// aggregateScopes merges the scopes into outcome in requested scope order rather than
// worker-completion order, and returns the failed scopes and the unreachable count. When
// providers return conflicting duplicates, the earliest requested scope wins, followed by the
// provider's response order within that scope.
func aggregateScopes(outcome *Outcome, plan Plan, observer Observer) ([]string, int) {
	var failed []string
	unreachable := 0
	for index, result := range outcome.Scopes {
		if result.Unreachable {
			unreachable++
		}
		outcome.Resources = append(outcome.Resources, result.Resources...)
		if result.Stats != nil {
			outcome.Stats.FailedResources += result.Stats.FailedResources
			outcome.Stats.DurationMs += result.Stats.DurationMs
			for service, count := range result.Stats.ServiceCounts {
				outcome.Stats.ServiceCounts[service] += count
			}
		}
		kind := EventScopeCompleted
		message := ""
		if len(result.Errors) > 0 {
			kind = EventScopeFailed
			failed = append(failed, result.Scope)
			message = strings.Join(result.Errors, "; ")
		}
		if observer != nil {
			observer(Event{Kind: kind, Provider: plan.Provider, Scope: result.Scope,
				Completed: index + 1, Total: len(plan.Scopes), Message: message})
		}
	}
	assertMergedTotal(outcome.Scopes, outcome.Resources)
	merged := len(outcome.Resources)
	outcome.Resources = dedupeResources(outcome.Resources)
	assert.That(len(outcome.Resources) <= merged, "dedupe never adds resources")
	outcome.Stats.TotalResources = int32(len(outcome.Resources))
	assert.That(unreachable <= len(failed), "every unreachable scope is a failed scope")
	return failed, unreachable
}

// assertMergedTotal checks that merged holds every scope's resources before dedupe. The
// providers' own Stats.TotalResources is not compared: a plugin is an external process and
// its counts are data, so a disagreement there is not a Corkscrew bug.
func assertMergedTotal(scopes []ScopeResult, merged []*pb.Resource) {
	scopeResourceCount := 0
	for _, scope := range scopes {
		scopeResourceCount += len(scope.Resources)
	}
	assert.Thatf(scopeResourceCount == len(merged),
		"merged resources equal the scopes' resources before dedupe: %d merged, %d from scopes",
		len(merged), scopeResourceCount)
}

func cloneFilters(filters map[string]string) map[string]string {
	result := make(map[string]string, len(filters))
	for key, value := range filters {
		result[key] = value
	}
	return result
}

func dedupeResources(resources []*pb.Resource) []*pb.Resource {
	seen := make(map[string]bool, len(resources))
	result := make([]*pb.Resource, 0, len(resources))
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		key := strings.Join([]string{resource.Provider, resource.AccountId, resource.Type, resource.Id}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, resource)
	}
	return result
}

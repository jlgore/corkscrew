// Package scanexec owns provider-neutral scan scheduling and aggregation.
package scanexec

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
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
	Errors    []string
	Duration  time.Duration
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

	jobs := make(chan int)
	results := make(chan indexedScopeResult, len(plan.Scopes))
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				scope := plan.Scopes[index]
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
				} else if response == nil {
					result.Errors = []string{"provider returned an empty scan response"}
				} else {
					result.Resources = response.Resources
					result.Stats = response.Stats
					result.Errors = append(result.Errors, response.Errors...)
				}
				results <- indexedScopeResult{index: index, ScopeResult: result}
			}
		}()
	}
	go func() {
		for index, scope := range plan.Scopes {
			if observer != nil {
				observer(Event{Kind: EventScopeStarted, Provider: plan.Provider, Scope: scope, Total: len(plan.Scopes)})
			}
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	outcome := Outcome{Scopes: make([]ScopeResult, len(plan.Scopes)), Stats: &pb.ScanStats{ServiceCounts: map[string]int32{}}}
	for result := range results {
		outcome.Scopes[result.index] = result.ScopeResult
	}

	// Aggregate in requested scope order rather than worker-completion order.
	// When providers return conflicting duplicates, the earliest requested scope
	// wins, followed by the provider's response order within that scope.
	var failed []string
	for index, result := range outcome.Scopes {
		outcome.Resources = append(outcome.Resources, result.Resources...)
		if result.Stats != nil {
			outcome.Stats.TotalResources += result.Stats.TotalResources
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
			observer(Event{Kind: kind, Provider: plan.Provider, Scope: result.Scope, Completed: index + 1, Total: len(plan.Scopes), Message: message})
		}
	}
	outcome.Resources = dedupeResources(outcome.Resources)
	outcome.Stats.TotalResources = int32(len(outcome.Resources))
	outcome.Duration = time.Since(started)
	if ctx.Err() != nil {
		outcome.Status = StatusFailed
		return outcome, ctx.Err()
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		if len(failed) == len(plan.Scopes) {
			outcome.Status = StatusFailed
		} else {
			outcome.Status = StatusPartial
		}
		return outcome, &PartialError{FailedScopes: failed}
	}
	outcome.Status = StatusComplete
	return outcome, nil
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

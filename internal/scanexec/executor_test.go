package scanexec

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

type batchScannerFunc func(context.Context, *pb.BatchScanRequest) (*pb.BatchScanResponse, error)

func (f batchScannerFunc) BatchScan(ctx context.Context, request *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	return f(ctx, request)
}

func TestExecuteDeduplicatesInRequestedScopeOrder(t *testing.T) {
	t.Parallel()

	scanner := batchScannerFunc(func(_ context.Context, request *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
		if request.Region == "first" {
			time.Sleep(25 * time.Millisecond)
		}
		return &pb.BatchScanResponse{Resources: []*pb.Resource{{
			Provider: "acme", AccountId: "account", Type: "widget", Id: "same",
			Region: request.Region, Name: request.Region,
		}}}, nil
	})
	var events []Event
	outcome, err := Execute(context.Background(), scanner, Plan{
		Provider: "acme", Scopes: []string{"first", "second"}, MaxConcurrency: 2,
	}, func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Resources) != 1 || outcome.Resources[0].Name != "first" {
		t.Fatalf("deduplicated resources = %#v, want first requested scope", outcome.Resources)
	}
	var completed []string
	for _, event := range events {
		if event.Kind == EventScopeCompleted {
			completed = append(completed, event.Scope)
		}
	}
	if !reflect.DeepEqual(completed, []string{"first", "second"}) {
		t.Fatalf("completion events = %v, want requested scope order", completed)
	}
}

func TestExecuteReturnsResourcesAndPartialErrorWhenOneScopeFails(t *testing.T) {
	t.Parallel()

	scanner := batchScannerFunc(func(_ context.Context, request *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
		if request.Region == "broken" {
			return nil, errors.New("access denied")
		}
		return &pb.BatchScanResponse{Resources: []*pb.Resource{{
			Provider: "acme", AccountId: "account", Type: "widget", Id: "one", Region: request.Region,
		}}}, nil
	})
	outcome, err := Execute(context.Background(), scanner, Plan{
		Provider:       "acme",
		Scopes:         []string{"global", "broken"},
		MaxConcurrency: 2,
		ScopeTimeout:   time.Second,
	}, nil)
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("Execute() error = %v, want PartialError", err)
	}
	if outcome.Status != StatusPartial || len(outcome.Resources) != 1 || len(outcome.Scopes) != 2 {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func scopeOutcome(t *testing.T, scopes []string, respond func(scope string) (*pb.BatchScanResponse, error)) (Outcome, error) {
	t.Helper()
	scanner := batchScannerFunc(func(_ context.Context, request *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
		return respond(request.Region)
	})
	return Execute(context.Background(), scanner, Plan{
		Provider: "acme", Scopes: scopes, MaxConcurrency: 2, ScopeTimeout: time.Second,
	}, nil)
}

func widget(scope string) *pb.Resource {
	return &pb.Resource{Provider: "acme", AccountId: "account", Type: "widget", Id: scope, Region: scope}
}

// A notice says how a scope was observed, not that it was incomplete, so it must not make
// the scan partial: a partial scan never resolves findings.
func TestExecuteTreatsNoticesAsComplete(t *testing.T) {
	t.Parallel()

	outcome, err := scopeOutcome(t, []string{"a", "b"}, func(scope string) (*pb.BatchScanResponse, error) {
		return &pb.BatchScanResponse{
			Resources: []*pb.Resource{widget(scope)},
			Errors:    []string{shared.UnsupportedTypeNotice("AWS::S3::AccessGrant", "unsupported")},
		}, nil
	})
	if err != nil || outcome.Status != StatusComplete {
		t.Fatalf("status = %s, err = %v; want complete", outcome.Status, err)
	}
	if len(outcome.Scopes[0].Errors) != 0 || len(outcome.Scopes[0].Notices) != 1 {
		t.Fatalf("scope = %#v, want one notice and no errors", outcome.Scopes[0])
	}
}

// A service error inside an otherwise successful scope means resources may be missing.
func TestExecuteMarksServiceErrorsPartial(t *testing.T) {
	t.Parallel()

	outcome, err := scopeOutcome(t, []string{"a", "b"}, func(scope string) (*pb.BatchScanResponse, error) {
		response := &pb.BatchScanResponse{Resources: []*pb.Resource{widget(scope)}}
		if scope == "b" {
			response.Errors = []string{shared.ServiceScanError("ec2", errors.New("throttled"))}
		}
		return response, nil
	})
	var partial *PartialError
	if !errors.As(err, &partial) || outcome.Status != StatusPartial {
		t.Fatalf("status = %s, err = %v; want partial", outcome.Status, err)
	}
	if len(outcome.Resources) != 2 {
		t.Fatalf("resources = %d, want both scopes' resources kept", len(outcome.Resources))
	}
}

// Every scope answering with some errors still observed resources, so the scan is partial;
// failed is reserved for a scan where no scope answered at all.
func TestExecuteFailsOnlyWhenNoScopeAnswers(t *testing.T) {
	t.Parallel()

	outcome, _ := scopeOutcome(t, []string{"a", "b"}, func(scope string) (*pb.BatchScanResponse, error) {
		return &pb.BatchScanResponse{
			Resources: []*pb.Resource{widget(scope)},
			Errors:    []string{shared.ServiceScanError("ec2", errors.New("throttled"))},
		}, nil
	})
	if outcome.Status != StatusPartial {
		t.Fatalf("every scope with errors: status = %s, want partial", outcome.Status)
	}

	outcome, _ = scopeOutcome(t, []string{"a", "b"}, func(string) (*pb.BatchScanResponse, error) {
		return nil, errors.New("plugin exited")
	})
	if outcome.Status != StatusFailed {
		t.Fatalf("no scope answered: status = %s, want failed", outcome.Status)
	}
}

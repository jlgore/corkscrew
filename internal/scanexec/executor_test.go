package scanexec

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
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

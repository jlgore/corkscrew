package main

import (
	"context"
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// Every describe handed to describeEach falls back to the listing data on failure, so a nil
// resource means a caller forgot the fallback and the resource would vanish from the scan.
func TestDescribeEachAssertsEveryRefHasAResource(t *testing.T) {
	refs := []*pb.ResourceRef{{Id: "a"}, {Id: "b"}}
	asserttest.FailsWith(t, "returns a resource for b", func() {
		describeEach(context.Background(), refs,
			func(_ context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
				if ref.Id == "b" {
					return nil, nil
				}
				return &pb.Resource{Id: ref.Id}, nil
			})
	})
}

func TestDescribeAssertsARMReadLimiter(t *testing.T) {
	scanner := testScanner(nil)
	scanner.armReadLimiter = nil
	asserttest.FailsWith(t, "ARM read limiter", func() {
		_, _ = scanner.DescribeResource(context.Background(),
			&pb.ResourceRef{Id: "/subscriptions/x/resourceGroups/rg/providers/A.B/c/d"})
	})
}

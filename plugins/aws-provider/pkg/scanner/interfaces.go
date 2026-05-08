package scanner

import (
	"context"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ResourceExplorer is the subset of the AWS Resource Explorer client surface
// CloudControlScanner depends on. Implemented by the main package's
// ResourceExplorer wrapper.
type ResourceExplorer interface {
	QueryAllResources(ctx context.Context) ([]*pb.ResourceRef, error)
	QueryByService(ctx context.Context, service string) ([]*pb.ResourceRef, error)
	QueryByResourceType(ctx context.Context, resourceType string) ([]*pb.ResourceRef, error)
	IsHealthy(ctx context.Context) bool
}

package scanner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudcontrol"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// blockingCloudControl's GetResource never answers: it returns only when its ctx is done, as a
// hung Cloud Control handler would.
type blockingCloudControl struct {
	fakeCloudControl
}

func (*blockingCloudControl) GetResource(
	ctx context.Context, _ *cloudcontrol.GetResourceInput, _ ...func(*cloudcontrol.Options),
) (*cloudcontrol.GetResourceOutput, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func describeBlocked(t *testing.T, ref *pb.ResourceRef) (*pb.Resource, error) {
	t.Helper()
	s := &CloudControlScanner{
		client:                     &blockingCloudControl{},
		getResourceTimeoutOverride: 20 * time.Millisecond,
	}
	start := time.Now()
	resource, err := s.DescribeResource(context.Background(), ref)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("DescribeResource took %s, want the per-call timeout to fire", elapsed)
	}
	if resource == nil {
		t.Fatal("DescribeResource returned no resource")
	}
	return resource, err
}

func TestDescribeResourceTimesOut(t *testing.T) {
	ref := &pb.ResourceRef{Service: "s3", Type: "AWS::S3::Bucket", Id: "bucket-1"}
	_, err := describeBlocked(t, ref)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

// With ListResources properties cached on the ref, a timed-out describe keeps them.
func TestDescribeResourceTimeoutFallsBackToListProperties(t *testing.T) {
	ref := &pb.ResourceRef{
		Service: "s3", Type: "AWS::S3::Bucket", Id: "bucket-1",
		BasicAttributes: map[string]string{"properties": `{"BucketName":"bucket-1"}`},
	}
	resource, err := describeBlocked(t, ref)
	if err != nil {
		t.Fatalf("err = %v, want the cached properties", err)
	}
	if resource.RawData != `{"BucketName":"bucket-1"}` {
		t.Fatalf("RawData = %q, want the ListResources properties", resource.RawData)
	}
}

func TestDescribeTimeoutDefault(t *testing.T) {
	if got := (&CloudControlScanner{}).describeTimeout(); got != getResourceTimeout {
		t.Fatalf("describeTimeout = %s, want %s", got, getResourceTimeout)
	}
	if getResourceTimeout <= 0 || getResourceTimeout >= 5*time.Minute {
		t.Fatalf("getResourceTimeout = %s, want within the 5-minute scope", getResourceTimeout)
	}
}

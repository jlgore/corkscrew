package scanner

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/cloudcontrol"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// failingCloudControl's GetResource always fails with err.
type failingCloudControl struct {
	fakeCloudControl
	err error
}

func (f *failingCloudControl) GetResource(
	context.Context, *cloudcontrol.GetResourceInput, ...func(*cloudcontrol.Options),
) (*cloudcontrol.GetResourceOutput, error) {
	return nil, f.err
}

func describeFailing(t *testing.T, err error) error {
	t.Helper()
	s := &CloudControlScanner{client: &failingCloudControl{err: err}}
	ref := &pb.ResourceRef{Service: "kms", Type: "AWS::KMS::Key", Id: "key-1"}
	resource, describeErr := s.DescribeResource(context.Background(), ref)
	if resource == nil {
		t.Fatal("DescribeResource returned no resource")
	}
	if describeErr == nil {
		t.Fatal("DescribeResource succeeded; want the GetResource failure")
	}
	return describeErr
}

// A denied GetResource is the credential's scope: the provider reports it as an
// access_denied notice, so the scanner returns it as an *AccessDeniedError naming the type.
func TestDescribeResourceDeniedIsAccessDeniedError(t *testing.T) {
	err := describeFailing(t, errors.New("AccessDeniedException: User is not authorized "+
		"to perform: kms:DescribeKey"))
	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("err = %v, want an *AccessDeniedError", err)
	}
	if denied.Type != "AWS::KMS::Key" {
		t.Fatalf("denied type = %q, want AWS::KMS::Key", denied.Type)
	}
}

// Any other GetResource failure may have dropped configuration and must stay an error.
func TestDescribeResourceThrottleIsNotAccessDenied(t *testing.T) {
	err := describeFailing(t, errors.New("ThrottlingException: Rate exceeded"))
	var denied *AccessDeniedError
	if errors.As(err, &denied) {
		t.Fatalf("err = %v is an *AccessDeniedError; a throttle is not a denial", err)
	}
}

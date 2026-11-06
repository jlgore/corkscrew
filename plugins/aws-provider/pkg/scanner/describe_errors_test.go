package scanner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cctypes "github.com/aws/aws-sdk-go-v2/service/cloudcontrol/types"

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

// handlerThrottled answers GetResource with a handler throttle for the first failures calls.
type handlerThrottled struct {
	fakeCloudControl
	mu       sync.Mutex
	failures int
	calls    int
}

const handlerThrottle = "operation error CloudControl: GetResource, https response error " +
	"StatusCode: 400, GeneralServiceException: AWS::CloudFormation::Stack Handler returned " +
	"status FAILED: Rate exceeded (Service: CloudFormation, Status Code: 400) " +
	"(HandlerErrorCode: GeneralServiceException)"

func (f *handlerThrottled) GetResource(
	context.Context, *cloudcontrol.GetResourceInput, ...func(*cloudcontrol.Options),
) (*cloudcontrol.GetResourceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failures {
		return nil, errors.New(handlerThrottle)
	}
	return &cloudcontrol.GetResourceOutput{ResourceDescription: &cctypes.ResourceDescription{
		Properties: aws.String(`{"StackName":"app"}`),
	}}, nil
}

func describeStack(cc *handlerThrottled) (*pb.Resource, error) {
	s := &CloudControlScanner{client: cc, handlerThrottleBackoffOverride: time.Millisecond}
	ref := &pb.ResourceRef{Service: "cloudformation", Type: "AWS::CloudFormation::Stack",
		Id: "arn:aws:cloudformation:us-east-1:111111111111:stack/app/1b2c"}
	return s.DescribeResource(context.Background(), ref)
}

// A handler throttled by the service behind it fails with a GeneralServiceException the SDK
// does not retry; the scanner retries it, so a busy account does not lose configuration.
func TestDescribeResourceRetriesHandlerThrottle(t *testing.T) {
	cc := &handlerThrottled{failures: handlerThrottleRetries}
	res, err := describeStack(cc)
	if err != nil {
		t.Fatalf("DescribeResource: %v", err)
	}
	if res.RawData == "" || cc.calls != handlerThrottleRetries+1 {
		t.Fatalf("RawData %q after %d calls, want configuration after %d", res.RawData,
			cc.calls, handlerThrottleRetries+1)
	}
}

// The retries are bounded: a handler that stays throttled fails the describe.
func TestDescribeResourceGivesUpOnPersistentHandlerThrottle(t *testing.T) {
	cc := &handlerThrottled{failures: handlerThrottleRetries + 1}
	if _, err := describeStack(cc); err == nil {
		t.Fatal("DescribeResource succeeded; want the throttle")
	}
	if cc.calls != handlerThrottleRetries+1 {
		t.Fatalf("%d calls, want %d", cc.calls, handlerThrottleRetries+1)
	}
}

// Other failures are not retried.
func TestDescribeResourceDoesNotRetryNotFound(t *testing.T) {
	if isHandlerThrottleErr(errors.New("ResourceNotFoundException: AWS::EC2::KeyPair " +
		"Handler returned status FAILED: NotFound (HandlerErrorCode: NotFound)")) {
		t.Fatal("NotFound is not a throttle")
	}
	if !isHandlerThrottleErr(errors.New("GeneralServiceException: x (HandlerErrorCode: " +
		"Throttling)")) {
		t.Fatal("HandlerErrorCode Throttling is a throttle")
	}
}

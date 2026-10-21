package scanner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudcontrol"
	cctypes "github.com/aws/aws-sdk-go-v2/service/cloudcontrol/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/jlgore/corkscrew/internal/shared/paging"
)

// fakeCloudControl serves one page per type with the given identifiers.
// errs fails a type's listing; repeatToken makes a type return the same
// continuation token on every page.
type fakeCloudControl struct {
	ids         map[string][]string
	errs        map[string]error
	repeatToken map[string]bool
}

func (f *fakeCloudControl) ListResources(
	_ context.Context, in *cloudcontrol.ListResourcesInput, _ ...func(*cloudcontrol.Options),
) (*cloudcontrol.ListResourcesOutput, error) {
	typeName := aws.ToString(in.TypeName)
	if err := f.errs[typeName]; err != nil {
		return nil, err
	}
	out := &cloudcontrol.ListResourcesOutput{}
	for _, id := range f.ids[typeName] {
		out.ResourceDescriptions = append(out.ResourceDescriptions,
			cctypes.ResourceDescription{Identifier: aws.String(id)})
	}
	if f.repeatToken[typeName] {
		out.NextToken = aws.String("same-token")
	}
	return out, nil
}

func (f *fakeCloudControl) GetResource(
	context.Context, *cloudcontrol.GetResourceInput, ...func(*cloudcontrol.Options),
) (*cloudcontrol.GetResourceOutput, error) {
	return nil, errors.New("GetResource not faked")
}

type fakeIdentity struct {
	account string
	err     error
}

func (f fakeIdentity) GetCallerIdentity(
	context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options),
) (*sts.GetCallerIdentityOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &sts.GetCallerIdentityOutput{Account: aws.String(f.account)}, nil
}

// fakeTypeRegistry fails ListTypes with err, or returns one type. It fails
// with ctx's error when ctx is done, as the SDK does.
type fakeTypeRegistry struct {
	err   error
	calls int
}

func (f *fakeTypeRegistry) ListTypes(
	ctx context.Context, in *cloudformation.ListTypesInput, _ ...func(*cloudformation.Options),
) (*cloudformation.ListTypesOutput, error) {
	f.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	if in.ProvisioningType != cfntypes.ProvisioningTypeFullyMutable {
		return &cloudformation.ListTypesOutput{}, nil
	}
	return &cloudformation.ListTypesOutput{TypeSummaries: []cfntypes.TypeSummary{
		{TypeName: aws.String("AWS::KMS::Key")},
	}}, nil
}

// resetDiscovery clears the process-wide discovery cache so each test starts
// from the curated map, and restores it to that state afterwards.
func resetDiscovery(t *testing.T) {
	t.Helper()
	wipe := func() {
		discovery.mu.Lock()
		defer discovery.mu.Unlock()
		discovery.ok, discovery.types, discovery.reverse = false, nil, nil
		discovery.err, discovery.failedAt = nil, time.Time{}
	}
	wipe()
	t.Cleanup(wipe)
}

func testScanner(cc *fakeCloudControl) *CloudControlScanner {
	return &CloudControlScanner{
		client:   cc,
		identity: fakeIdentity{account: "111111111111"},
		cfg:      aws.Config{Region: "us-east-1"},
	}
}

// A denied type may hold resources the scan never saw, so it must be an
// error naming the type, not an unsupported-type notice. A type Cloud
// Control cannot list is a notice. Both keep the other types' refs.
func TestScanServiceReportsAccessDeniedAsErrorAndUnsupportedAsNotice(t *testing.T) {
	resetDiscovery(t)
	s := testScanner(&fakeCloudControl{
		ids: map[string][]string{"AWS::EC2::Subnet": {"subnet-1"}},
		errs: map[string]error{
			"AWS::EC2::Instance": errors.New("AccessDeniedException: not allowed"),
			"AWS::EC2::VPC":      errors.New("TypeNotFoundException: no such type"),
		},
	})

	refs, err := s.ScanService(context.Background(), "ec2")
	if len(refs) != 1 || refs[0].Id != "subnet-1" || refs[0].AccountId != "111111111111" {
		t.Fatalf("refs = %v, want subnet-1 in account 111111111111", refs)
	}
	leaves := FlattenErrors(err)
	const wantDenied = "access_denied:AWS::EC2::Instance"
	if len(leaves) != 1 || !strings.Contains(leaves[0].Error(), wantDenied) {
		t.Fatalf("errors = %v, want one access_denied:AWS::EC2::Instance", leaves)
	}
	unsupported := s.UnsupportedTypes()
	if _, ok := unsupported["AWS::EC2::Instance"]; ok {
		t.Fatal("a denied type was reported as unsupported")
	}
	if _, ok := unsupported["AWS::EC2::VPC"]; !ok || len(unsupported) != 1 {
		t.Fatalf("unsupported = %v, want only AWS::EC2::VPC", unsupported)
	}
}

// Without the account ID every ref would be stored under account "".
func TestScanServiceFailsWhenAccountIDCannotBeResolved(t *testing.T) {
	resetDiscovery(t)
	s := testScanner(&fakeCloudControl{ids: map[string][]string{"AWS::KMS::Key": {"k"}}})
	s.identity = fakeIdentity{err: errors.New("ExpiredToken")}

	refs, err := s.ScanService(context.Background(), "kms")
	if err == nil || !strings.Contains(err.Error(), "resolve account id") {
		t.Fatalf("err = %v, want an account id error", err)
	}
	if len(refs) != 0 {
		t.Fatalf("got %d refs without an account id", len(refs))
	}
}

// A server that repeats its continuation token must end the listing with an
// error, keeping the page it did return, instead of looping until the
// deadline and appending duplicates.
func TestListTypeStopsOnRepeatedToken(t *testing.T) {
	s := testScanner(&fakeCloudControl{
		ids:         map[string][]string{"AWS::KMS::Key": {"k"}},
		repeatToken: map[string]bool{"AWS::KMS::Key": true},
	})

	refs, err := s.listType(context.Background(), "kms", "AWS::KMS::Key")
	if !errors.Is(err, paging.ErrTokenRepeated) {
		t.Fatalf("err = %v, want ErrTokenRepeated", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want the 2 pages before the repeat was seen", len(refs))
	}
}

// A failed discovery must be reported while the curated map is in use, and
// must not be cached for the life of the process: a later run that succeeds
// replaces it. The run is detached from the caller, so a cancelled caller
// does not fail it.
func TestDiscoveryReportsFailureAndRetries(t *testing.T) {
	resetDiscovery(t)
	registry := &fakeTypeRegistry{err: errors.New("Throttling")}
	s := &CloudControlScanner{cfn: registry}

	s.ensureDiscovery(context.Background())
	if err := s.DiscoveryErr(); err == nil || !strings.Contains(err.Error(), "Throttling") {
		t.Fatalf("DiscoveryErr = %v, want the ListTypes failure", err)
	}
	if _, ok := s.activeTypeMap()["ec2"]; !ok {
		t.Fatal("failed discovery did not fall back to the curated map")
	}
	s.ensureDiscovery(context.Background())
	if registry.calls != 1 {
		t.Fatalf("ListTypes called %d times, want no retry before discoveryRetryAfter",
			registry.calls)
	}

	discovery.mu.Lock()
	discovery.failedAt = time.Now().Add(-discoveryRetryAfter)
	discovery.mu.Unlock()
	registry.err = nil
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	s.ensureDiscovery(cancelled)
	if err := s.DiscoveryErr(); err != nil {
		t.Fatalf("DiscoveryErr after a successful retry = %v", err)
	}
	if got := s.activeTypeMap()["kms"]; len(got) != 1 || got[0] != "AWS::KMS::Key" {
		t.Fatalf("discovered kms types = %v", got)
	}
}

func TestFlattenErrorsExpandsJoins(t *testing.T) {
	a, b, c := errors.New("a"), errors.New("b"), errors.New("c")
	got := FlattenErrors(errors.Join(a, errors.Join(b, nil, c)))
	if len(got) != 3 || got[0] != a || got[1] != b || got[2] != c {
		t.Fatalf("FlattenErrors = %v, want [a b c]", got)
	}
	if FlattenErrors(nil) != nil {
		t.Fatal("FlattenErrors(nil) != nil")
	}
}

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2/types"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// fakeScanner discovers the refs it is given and fails every describe,
// returning the partial resource CloudControlScanner returns on error.
type fakeScanner struct {
	refs []*pb.ResourceRef
}

func (f *fakeScanner) SupportedServices() []string { return []string{"kms"} }

func (f *fakeScanner) ScanService(context.Context, string) ([]*pb.ResourceRef, error) {
	return f.refs, nil
}

func (f *fakeScanner) DescribeResource(_ context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	return &pb.Resource{Provider: "aws", Type: ref.Type, Id: ref.Id, Region: ref.Region}, errors.New("GetResource failed")
}

func (f *fakeScanner) UnsupportedTypes() map[string]string { return nil }

func testProvider(base activeScanner, wired *[]string) *AWSProvider {
	p := &AWSProvider{
		config:      aws.Config{Region: "ap-east-1"},
		scanner:     base,
		regional:    map[string]activeScanner{},
		rateLimiter: rate.NewLimiter(rate.Inf, 1),
		wireExplorer: func(_ context.Context, cfg aws.Config, _ activeScanner) *ResourceExplorer {
			*wired = append(*wired, cfg.Region)
			return nil
		},
	}
	p.initialized.Store(true)
	return p
}

// mustScannerFor fails the test when scannerFor returns an error.
func mustScannerFor(t *testing.T, p *AWSProvider, region string) activeScanner {
	t.Helper()
	s, err := p.scannerFor(context.Background(), region)
	if err != nil {
		t.Fatalf("scannerFor(%q): %v", region, err)
	}
	return s
}

// A multi-region scan sends one BatchScanRequest per region. Each must be
// served by a scanner configured for that region, with that region's
// Resource Explorer view, rather than by the one built at Initialize.
func TestScannerForBuildsOneScannerPerRegion(t *testing.T) {
	var wired []string
	base := &fakeScanner{}
	p := testProvider(base, &wired)

	if got := mustScannerFor(t, p, ""); got != base {
		t.Fatalf("empty region: got %T, want the Initialize scanner", got)
	}
	if got := mustScannerFor(t, p, "ap-east-1"); got != base {
		t.Fatalf("configured region: got %T, want the Initialize scanner", got)
	}

	east := mustScannerFor(t, p, "us-east-1")
	if east == base {
		t.Fatal("us-east-1 was served by the ap-east-1 scanner")
	}
	if again := mustScannerFor(t, p, "us-east-1"); again != east {
		t.Fatal("us-east-1 scanner was rebuilt instead of reused")
	}
	if west := mustScannerFor(t, p, "eu-west-1"); west == east || west == base {
		t.Fatal("eu-west-1 shares another region's scanner")
	}

	want := []string{"us-east-1", "eu-west-1"}
	if len(wired) != len(want) || wired[0] != want[0] || wired[1] != want[1] {
		t.Fatalf("Resource Explorer wired for %v, want %v", wired, want)
	}
}

// An RPC can pass the initialized check just before Cleanup runs. Its
// scannerFor call must then fail instead of writing to the nil regional map.
func TestScannerForAfterCleanupReturnsError(t *testing.T) {
	var wired []string
	p := testProvider(&fakeScanner{}, &wired)
	if err := p.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	for _, region := range []string{"", "us-east-1"} {
		if _, err := p.scannerFor(context.Background(), region); !errors.Is(err, errNotInitialized) {
			t.Fatalf("scannerFor(%q) after Cleanup: err = %v, want errNotInitialized", region, err)
		}
	}
}

// A cancelled context must stop describeAll from starting describes, so a
// cancelled scan does not keep calling GetResource for every remaining ref.
func TestDescribeAllStopsOnCancel(t *testing.T) {
	refs := make([]*pb.ResourceRef, describeConcurrency*3)
	for i := range refs {
		refs[i] = &pb.ResourceRef{Type: "kms:key", Id: "k"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := describeAll(ctx, &fakeScanner{}, refs)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// select picks randomly among ready cases, so a few describes may start
	// before ctx.Done wins; it must not be every ref.
	if len(got) >= len(refs) {
		t.Fatalf("described %d of %d refs after cancel", len(got), len(refs))
	}
}

// A resource discovery found must survive a failed GetResource: dropping
// it leaves the inventory silently incomplete.
func TestBatchScanKeepsResourcesWhoseDescribeFails(t *testing.T) {
	arn := "arn:aws:kms:ap-east-1:123456789012:key/0c722e12-fa65-4f4f-af22-7361f6845317"
	var wired []string
	p := testProvider(&fakeScanner{refs: []*pb.ResourceRef{
		{Service: "kms", Type: "kms:key", Id: arn, Region: "ap-east-1"},
	}}, &wired)

	resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{Services: []string{"kms"}})
	if err != nil {
		t.Fatalf("BatchScan: %v", err)
	}
	if len(resp.Resources) != 1 {
		t.Fatalf("got %d resources, want the 1 discovered", len(resp.Resources))
	}
	if got := resp.Resources[0]; got.Id != arn || got.Arn != arn || got.Type != "kms:key" {
		t.Fatalf("kept resource = {Id:%q Arn:%q Type:%q}", got.Id, got.Arn, got.Type)
	}
}

func TestUnenrichedResourceFallsBackToTheRef(t *testing.T) {
	ref := &pb.ResourceRef{Service: "s3", Type: "s3:bucket", Id: "my-bucket", Region: "us-east-1", AccountId: "123456789012"}
	res := unenrichedResource(ref, nil)
	if res.Id != "my-bucket" || res.Region != "us-east-1" || res.AccountId != "123456789012" {
		t.Fatalf("resource from ref = %+v", res)
	}
	if res.Arn != "" {
		t.Fatalf("a bare id is not an ARN, got Arn %q", res.Arn)
	}
}

// Resource Explorer's "service:resource" type is what scanServiceViaRE
// maps to a CFN type; the segment parsed out of the ARN ("key") must not
// replace it.
func TestConvertResultsKeepsResourceExplorerType(t *testing.T) {
	re := &ResourceExplorer{accountID: "123456789012"}
	refs := re.convertResults([]types.Resource{{
		Arn:          aws.String("arn:aws:kms:us-east-1:123456789012:key/0c722e12-fa65-4f4f-af22-7361f6845317"),
		ResourceType: aws.String("kms:key"),
		Region:       aws.String("us-east-1"),
		Service:      aws.String("kms"),
	}})
	if len(refs) != 1 {
		t.Fatalf("got %d refs", len(refs))
	}
	if refs[0].Type != "kms:key" {
		t.Fatalf("Type = %q, want Resource Explorer's %q", refs[0].Type, "kms:key")
	}
	if refs[0].Region != "us-east-1" {
		t.Fatalf("Region = %q", refs[0].Region)
	}
}

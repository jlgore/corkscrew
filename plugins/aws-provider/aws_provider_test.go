package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2/types"
	"github.com/jlgore/corkscrew/internal/assert/asserttest"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/plugins/aws-provider/orgscan"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"golang.org/x/time/rate"
)

// fakeScanner discovers the refs it is given. A describe of a ref whose Id is in
// describeErrs fails with that error, returning the partial resource
// CloudControlScanner returns on error; failAll fails every describe. scanErrs makes
// ScanService of a service also return that error, and unsupported and discoveryErr are
// reported as given.
type fakeScanner struct {
	refs         []*pb.ResourceRef
	scanErrs     map[string]error
	describeErrs map[string]error
	failAll      bool
	unsupported  map[string]string
	discoveryErr error
}

func (f *fakeScanner) SupportedServices() []string { return []string{"kms"} }

// ScanService returns the refs of serviceName, plus its error if any.
func (f *fakeScanner) ScanService(
	_ context.Context, serviceName string,
) ([]*pb.ResourceRef, error) {
	var refs []*pb.ResourceRef
	for _, r := range f.refs {
		if r.Service == "" || r.Service == serviceName {
			refs = append(refs, r)
		}
	}
	return refs, f.scanErrs[serviceName]
}

func (f *fakeScanner) DescribeResource(_ context.Context, ref *pb.ResourceRef) (*pb.Resource, error) {
	res := &pb.Resource{Provider: "aws", Type: ref.Type, Id: ref.Id, Region: ref.Region}
	if err := f.describeErrs[ref.Id]; err != nil {
		return res, err
	}
	if f.failAll {
		return res, errors.New("GetResource failed")
	}
	res.RawData = "{}"
	return res, nil
}

func (f *fakeScanner) UnsupportedTypes() map[string]string { return f.unsupported }

func (f *fakeScanner) DiscoveryErr() error { return f.discoveryErr }

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

	got, _, err := describeAll(ctx, &fakeScanner{failAll: true}, refs)
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
	p := testProvider(&fakeScanner{failAll: true, refs: []*pb.ResourceRef{
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

// batchScan runs BatchScan for services against s, failing on an RPC error.
func batchScan(t *testing.T, s activeScanner, services ...string) *pb.BatchScanResponse {
	t.Helper()
	var wired []string
	resp, err := testProvider(s, &wired).BatchScan(context.Background(),
		&pb.BatchScanRequest{Services: services})
	if err != nil {
		t.Fatalf("BatchScan: %v", err)
	}
	return resp
}

// One failed service must not drop the others, and must be reported as an
// error, not a notice, so the scan is partial and resolves no findings
// (ADR 0023). An access denial and an unsupported type are notices: they
// describe the credential's coverage, not a broken scan.
func TestBatchScanReportsFailedServiceAndKeepsTheRest(t *testing.T) {
	s := &fakeScanner{
		refs: []*pb.ResourceRef{
			{Service: "kms", Type: "AWS::KMS::Key", Id: "key-1"},
			{Service: "ec2", Type: "AWS::EC2::Subnet", Id: "subnet-1"},
		},
		scanErrs: map[string]error{"ec2": errors.Join(
			&scanner.AccessDeniedError{Type: "AWS::EC2::Instance", Err: errors.New("AccessDenied")},
			errors.New("AWS::EC2::VPC: Throttling"),
		)},
		unsupported: map[string]string{"AWS::EC2::Thing": "TypeNotFoundException"},
	}
	resp := batchScan(t, s, "kms", "ec2", "s3")

	if len(resp.Resources) != 2 || resp.Stats.TotalResources != 2 {
		t.Fatalf("got %d resources, want kms and the partial ec2", len(resp.Resources))
	}
	errs, notices := shared.SplitScanErrors(resp.Errors)
	if len(errs) != 1 || !strings.HasPrefix(errs[0], "service:ec2: ") ||
		!strings.Contains(errs[0], "AWS::EC2::VPC") {
		t.Fatalf("errors = %v, want only the throttled ec2 type", errs)
	}
	var denied, unsupported int
	for _, n := range notices {
		if shared.IsCoverageNotice(n) && strings.Contains(n, "AWS::EC2::Instance") {
			denied++
		}
		if !shared.IsCoverageNotice(n) && strings.Contains(n, "AWS::EC2::Thing") {
			unsupported++
		}
	}
	if len(notices) != 2 || denied != 1 || unsupported != 1 {
		t.Fatalf("notices = %v, want the access denial and the unsupported type", notices)
	}
}

// The curated fallback misses most types, so a failed discovery is an error.
func TestBatchScanReportsDiscoveryFailure(t *testing.T) {
	s := &fakeScanner{discoveryErr: errors.New("ListTypes: Throttling")}
	resp := batchScan(t, s, "kms")

	errs, _ := shared.SplitScanErrors(resp.Errors)
	if len(errs) != 1 || !strings.HasPrefix(errs[0], "service:discovery: ") {
		t.Fatalf("errors = %v, want one discovery error", errs)
	}
}

// A service named twice must still count each resource once in every total.
func TestBatchScanCountsAgreeForRepeatedService(t *testing.T) {
	s := &fakeScanner{refs: []*pb.ResourceRef{{Service: "kms", Type: "AWS::KMS::Key", Id: "k"}}}
	resp := batchScan(t, s, "kms", "kms")
	if resp.Stats.TotalResources != 2 || resp.Stats.ServiceCounts["kms"] != 2 {
		t.Fatalf("stats = %+v", resp.Stats)
	}
}

func TestAssertScanStatsPanicsOnMismatch(t *testing.T) {
	asserttest.FailsWith(t, "scan stats agree", func() {
		assertScanStats(&pb.ScanStats{TotalResources: 1, ServiceCounts: map[string]int32{"kms": 2}})
	})
}

// Every launched describe yields exactly one resource, failed describes included, so a
// launched slot without one has lost a resource discovery found.
func TestLaunchedResourcesAssertsOneResultPerLaunchedDescribe(t *testing.T) {
	asserttest.FailsWith(t, "every launched describe yields a resource", func() {
		launchedResources([]*pb.Resource{{Id: "a"}, nil}, 2)
	})
	asserttest.FailsWith(t, "no resource past the launched describes", func() {
		launchedResources([]*pb.Resource{{Id: "a"}, {Id: "b"}}, 1)
	})
	if got := launchedResources([]*pb.Resource{{Id: "a"}, {Id: "b"}, nil}, 2); len(got) != 2 {
		t.Fatalf("got %d resources, want 2", len(got))
	}
}

// Without cancellation every ref is described: len(results) == len(refs).
func TestDescribeAllReturnsOneResourcePerRef(t *testing.T) {
	refs := []*pb.ResourceRef{
		{Service: "kms", Type: "AWS::KMS::Key", Id: "k1"},
		{Service: "kms", Type: "AWS::KMS::Key", Id: "k2"},
		{Service: "kms", Type: "AWS::KMS::Key", Id: "k3"},
	}
	got, errs, err := describeAll(context.Background(), &fakeScanner{}, refs)
	if err != nil || len(got) != len(refs) || len(errs) != len(refs) {
		t.Fatalf("describeAll = %d resources, err %v; want %d", len(got), err, len(refs))
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

// A scan in org mode must use a per-scan view, which lists the organization's
// accounts once for all services; a single-account scanner passes through.
func TestForScanScopesOrgScanner(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1"}
	if _, ok := forScan(orgscan.New(cfg, orgscan.Config{})).(*orgscan.Scan); !ok {
		t.Fatal("forScan(OrgScanner) is not an orgscan.Scan")
	}
	single := scanner.NewCloudControlScanner(cfg)
	if got := forScan(single); got != activeScanner(single) {
		t.Fatalf("forScan(single account) = %T, want the scanner itself", got)
	}
}

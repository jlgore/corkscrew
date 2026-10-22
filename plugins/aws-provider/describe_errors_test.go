package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"google.golang.org/grpc"
)

// describeFixture discovers three KMS keys, two of which fail to describe, and two EC2
// instances, one of which is denied. The KMS failures drop configuration, so they are one
// error naming the type and the count; the EC2 denial is the credential's scope, a notice.
func describeFixture() *fakeScanner {
	denied := &scanner.AccessDeniedError{Type: "AWS::EC2::Instance",
		Err: errors.New("AccessDeniedException: not authorized")}
	return &fakeScanner{
		refs: []*pb.ResourceRef{
			{Service: "kms", Type: "AWS::KMS::Key", Id: "key-1"},
			{Service: "kms", Type: "AWS::KMS::Key", Id: "key-2"},
			{Service: "kms", Type: "AWS::KMS::Key", Id: "key-3"},
			{Service: "ec2", Type: "AWS::EC2::Instance", Id: "i-1"},
			{Service: "ec2", Type: "AWS::EC2::Instance", Id: "i-2"},
		},
		describeErrs: map[string]error{
			"key-1": errors.New("GetResource(AWS::KMS::Key, key-1): Throttling"),
			"key-3": errors.New("GetResource(AWS::KMS::Key, key-3): Throttling"),
			"i-2":   fmt.Errorf("GetResource(AWS::EC2::Instance, i-2): %w", denied),
		},
	}
}

// checkDescribeEntries asserts the Errors entries for describeFixture: exactly one
// error, for the KMS keys, and exactly one coverage notice, for the denied instance.
func checkDescribeEntries(t *testing.T, entries []string) {
	t.Helper()
	errs, notices := shared.SplitScanErrors(entries)
	if len(errs) != 1 {
		t.Fatalf("errors = %q, want one for the failed KMS describes", errs)
	}
	want := "service:kms: describe failed for 2 of 3 AWS::KMS::Key, first: "
	if !strings.HasPrefix(errs[0], want) || !strings.Contains(errs[0], "key-1") {
		t.Fatalf("error = %q, want prefix %q naming the first failure", errs[0], want)
	}
	if len(notices) != 1 || !shared.IsCoverageNotice(notices[0]) {
		t.Fatalf("notices = %q, want one access_denied notice", notices)
	}
	if !strings.HasPrefix(notices[0], "access_denied:AWS::EC2::Instance: ") ||
		!strings.Contains(notices[0], "1 of 2") {
		t.Fatalf("notice = %q, want the denied instance type and its count", notices[0])
	}
}

// A failed describe drops the resource's configuration, so the scan is partial (ADR 0023),
// while the resource is still emitted from what discovery found.
func TestBatchScanReportsFailedDescribesPerType(t *testing.T) {
	resp := batchScan(t, describeFixture(), "kms", "ec2")
	if len(resp.Resources) != 5 {
		t.Fatalf("got %d resources, want all 5 discovered, failed describes included",
			len(resp.Resources))
	}
	checkDescribeEntries(t, resp.Errors)
}

// Only access denials: the scan stays complete and reports coverage notices.
func TestBatchScanDescribeDenialIsANotice(t *testing.T) {
	s := describeFixture()
	delete(s.describeErrs, "key-1")
	delete(s.describeErrs, "key-3")
	resp := batchScan(t, s, "kms", "ec2")

	errs, notices := shared.SplitScanErrors(resp.Errors)
	if len(errs) != 0 || len(notices) != 1 {
		t.Fatalf("errors = %q, notices = %q; want no errors and one notice", errs, notices)
	}
}

// fakeStream collects what StreamScan sends.
type fakeStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*pb.Resource
}

func (f *fakeStream) Context() context.Context { return f.ctx }

func (f *fakeStream) Send(res *pb.Resource) error {
	f.sent = append(f.sent, res)
	return nil
}

// A stream has no Errors field, so failed describes must end it with an error that carries
// the same entries BatchScan reports, after every resource has been sent.
func TestStreamScanReportsFailedDescribes(t *testing.T) {
	var wired []string
	p := testProvider(describeFixture(), &wired)
	stream := &fakeStream{ctx: context.Background()}

	err := p.StreamScan(&pb.StreamScanRequest{Services: []string{"kms", "ec2"}}, stream)
	if len(stream.sent) != 5 {
		t.Fatalf("sent %d resources, want all 5", len(stream.sent))
	}
	if err == nil {
		t.Fatal("StreamScan ended cleanly after failed describes; want an incomplete error")
	}
	if !strings.Contains(err.Error(), "describe failed for 2 of 3 AWS::KMS::Key") {
		t.Fatalf("err = %v, want the KMS describe failures", err)
	}
}

// A stream whose only describe failures are denials is complete: denials are notices.
func TestStreamScanDescribeDenialEndsCleanly(t *testing.T) {
	s := describeFixture()
	delete(s.describeErrs, "key-1")
	delete(s.describeErrs, "key-3")
	var wired []string
	stream := &fakeStream{ctx: context.Background()}

	err := testProvider(s, &wired).StreamScan(
		&pb.StreamScanRequest{Services: []string{"kms", "ec2"}}, stream)
	if err != nil {
		t.Fatalf("StreamScan: %v, want a clean end", err)
	}
	if len(stream.sent) != 5 {
		t.Fatalf("sent %d resources, want 5", len(stream.sent))
	}
}

// The tally keys a ref by its CloudFormation type when Resource Explorer gave it one.
func TestDescribeTallyUsesCFNType(t *testing.T) {
	tally := newDescribeTally()
	ref := &pb.ResourceRef{Type: "kms:key", BasicAttributes: map[string]string{
		"cfn_type": "AWS::KMS::Key"}}
	tally.add(ref, errors.New("boom"))
	tally.add(&pb.ResourceRef{Type: "kms:key"}, nil)

	entries := tally.entries("kms")
	if len(entries) != 1 || !strings.Contains(entries[0], "1 of 1 AWS::KMS::Key") {
		t.Fatalf("entries = %q, want the failure under AWS::KMS::Key", entries)
	}
}

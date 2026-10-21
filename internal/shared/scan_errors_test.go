package shared

import (
	"errors"
	"io"
	"reflect"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestSplitScanErrorsKeepsUnknownEntriesAsErrors(t *testing.T) {
	entries := []string{
		UnsupportedTypeNotice("AWS::S3::AccessGrant", "not supported in region"),
		AccessDeniedNotice("AWS::EC2::Instance", errors.New("AccessDenied")),
		NotEnabledNotice("acme/web code scanning", "404 Not Found"),
		ServiceScanError("ec2", errors.New("throttled")),
		"Service s3: legacy format",
	}
	errs, notices := SplitScanErrors(entries)
	if want := entries[3:]; !reflect.DeepEqual(errs, want) {
		t.Fatalf("errors = %v, want %v", errs, want)
	}
	if want := entries[:3]; !reflect.DeepEqual(notices, want) {
		t.Fatalf("notices = %v, want %v", notices, want)
	}
	if IsCoverageNotice(entries[0]) || !IsCoverageNotice(entries[1]) || !IsCoverageNotice(entries[2]) {
		t.Fatal("only access_denied and not_enabled are coverage notices")
	}
}

type fakeSource struct {
	resources []*pb.Resource
	end       error
}

func (f *fakeSource) Recv() (*pb.Resource, error) {
	if len(f.resources) == 0 {
		return nil, f.end
	}
	next := f.resources[0]
	f.resources = f.resources[1:]
	return next, nil
}

type fakeSink struct{ sent int }

func (f *fakeSink) Send(*pb.Resource) error {
	f.sent++
	return nil
}

func TestForwardStreamOnlyTreatsEOFAsComplete(t *testing.T) {
	two := func() []*pb.Resource { return []*pb.Resource{{Id: "a"}, {Id: "b"}} }

	sink := &fakeSink{}
	if err := forwardStream(&fakeSource{resources: two(), end: io.EOF}, sink); err != nil {
		t.Fatalf("EOF: err = %v, want nil", err)
	}
	if sink.sent != 2 {
		t.Fatalf("EOF: sent %d, want 2", sink.sent)
	}

	// A plugin crash mid-stream surfaces as a non-EOF Recv error.
	crash := errors.New("transport is closing")
	sink = &fakeSink{}
	err := forwardStream(&fakeSource{resources: two(), end: crash}, sink)
	if !errors.Is(err, crash) {
		t.Fatalf("crash: err = %v, want %v", err, crash)
	}
	if sink.sent != 2 {
		t.Fatalf("crash: sent %d, want the 2 received before the failure", sink.sent)
	}
}

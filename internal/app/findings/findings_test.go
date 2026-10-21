package findings

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	dataaccess "github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestEvaluateOpensSuppressesAndResolvesFinding(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "findings.duckdb")
	started := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	persistBucketScan(t, ctx, target, "bad", started, `{"PublicAccessBlockConfiguration":{"BlockPublicAcls":false,"BlockPublicPolicy":false,"IgnorePublicAcls":false,"RestrictPublicBuckets":false}}`)
	evaluation, err := Evaluate(ctx, EvaluateRequest{Target: target, ScanID: "bad", ControlID: "CCC.ObjStor.C02"})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Opened != 1 || len(evaluation.Findings) != 1 {
		t.Fatalf("evaluation = %#v", evaluation)
	}
	fingerprint := evaluation.Findings[0].Fingerprint
	if err := Suppress(ctx, target, fingerprint, "accepted for test", "tester", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := List(ctx, target, ListFilter{Status: "suppressed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("suppressed findings = %#v", rows)
	}

	persistBucketScan(t, ctx, target, "good", started.Add(time.Hour), `{"PublicAccessBlockConfiguration":{"BlockPublicAcls":true,"BlockPublicPolicy":true,"IgnorePublicAcls":true,"RestrictPublicBuckets":true}}`)
	evaluation, err = Evaluate(ctx, EvaluateRequest{Target: target, ScanID: "good", ControlID: "CCC.ObjStor.C02"})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Resolved != 1 {
		t.Fatalf("resolved = %d", evaluation.Resolved)
	}
	rows, err = List(ctx, target, ListFilter{Status: "resolved"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("resolved findings = %#v", rows)
	}
}

// Evaluate counts a finding as recurred or opened from findingExists. A
// failed existence query must surface as an error rather than read as "new",
// which would count a recurring finding as opened. Forcing only that query to
// fail inside a full Evaluate is impractical, so this exercises the seam
// Evaluate calls with a connection that has already been closed.
func TestFindingExistsReturnsQueryError(t *testing.T) {
	ctx := context.Background()
	session, err := dataaccess.OpenSession(ctx, filepath.Join(t.TempDir(), "findings.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	connection, err := session.Connection(ctx)
	if err != nil {
		t.Fatal(err)
	}

	existed, err := findingExists(ctx, connection, "fp")
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	if existed {
		t.Fatal("empty database reports the finding as existing")
	}

	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := findingExists(ctx, connection, "fp"); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed connection: err = %v, want sql.ErrConnDone", err)
	}
}

func TestDecodeJSONColumn(t *testing.T) {
	var services []string
	if err := decodeJSONColumn("", &services); err != nil || services != nil {
		t.Fatalf("empty column: services=%v err=%v", services, err)
	}
	if err := decodeJSONColumn(`["s3"]`, &services); err != nil || len(services) != 1 {
		t.Fatalf("valid column: services=%v err=%v", services, err)
	}
	if err := decodeJSONColumn(`["s3"`, &services); err == nil {
		t.Fatal("corrupt column: want error")
	}
}

func persistBucketScan(t *testing.T, ctx context.Context, target, id string, started time.Time, raw string) {
	t.Helper()
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	resource := &pb.Resource{Provider: "aws", Id: "arn:aws:s3:::fixture", Arn: "arn:aws:s3:::fixture", Name: "fixture",
		Type: "AWS::S3::Bucket", Service: "s3", Region: "us-east-1", AccountId: "123456789012", RawData: raw}
	if err := session.PersistScanOutcome(ctx, dataaccess.ScanOutcome{ID: id, Provider: "aws", Services: []string{"s3"}, Scopes: []string{"us-east-1"},
		Status: dataaccess.ScanStatusCompleted, StartedAt: started, EndedAt: started.Add(time.Second), Resources: []*pb.Resource{resource}}, dataaccess.PersistScanOptions{}); err != nil {
		t.Fatal(err)
	}
}

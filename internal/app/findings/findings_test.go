package findings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	dataaccess "github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/pkg/query/compliance"
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

// Evaluate counts a finding as recurred or opened from existingFingerprints.
// A failed existence query must surface as an error rather than read as
// "new", which would count a recurring finding as opened. Forcing only that
// query to fail inside a full Evaluate is impractical, so this exercises the
// seam Evaluate calls with a connection that has already been closed.
func TestExistingFingerprintsReturnsQueryError(t *testing.T) {
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

	existing := map[string]bool{}
	if err := existingFingerprints(ctx, connection, []string{"fp"}, existing); err != nil {
		t.Fatalf("open connection: %v", err)
	}
	if existing["fp"] {
		t.Fatal("empty database reports the finding as existing")
	}

	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	err = existingFingerprints(ctx, connection, []string{"fp"}, existing)
	if !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed connection: err = %v, want sql.ErrConnDone", err)
	}
}

// recordResult replaced one SELECT, one upsert and one INSERT per failing row
// with batched statements. This drives more rows than several batches hold,
// with duplicate rows and rows already stored by an earlier evaluation, and
// checks the counts and stored rows the per-row version produced.
func TestRecordResultBatchesWithPerRowSemantics(t *testing.T) {
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
	defer connection.Close()

	started := time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	resourceCount := findingBatchCountMax*2 + 88
	first := recordFixture(t, ctx, connection, "scan-1", started, 0, resourceCount)
	if first.Opened != resourceCount || first.Recurred != 5 || len(first.Findings) != resourceCount+5 {
		t.Fatalf("first: opened=%d recurred=%d findings=%d", first.Opened, first.Recurred,
			len(first.Findings))
	}
	checkFinding(t, connection, "resource-0", 6, "duplicate 4")
	checkOccurrence(t, connection, first.ID, "resource-0", "row resource-0")
	checkCount(t, connection, `SELECT COUNT(*) FROM finding_occurrences`, resourceCount)

	half := resourceCount / 2
	second := recordFixture(t, ctx, connection, "scan-2", started.Add(time.Hour), half, resourceCount)
	if second.Opened != half || second.Recurred != resourceCount-half+5 {
		t.Fatalf("second: opened=%d recurred=%d", second.Opened, second.Recurred)
	}
	checkFinding(t, connection, "resource-0", 6, "duplicate 4")
	checkFinding(t, connection, fmt.Sprintf("resource-%d", half), 7, "duplicate 4")
	checkFinding(t, connection, fmt.Sprintf("resource-%d", half+1), 2, "row resource-"+
		fmt.Sprint(half+1))
	checkCount(t, connection, `SELECT COUNT(*) FROM findings`, resourceCount+half)
}

// recordFixture records resources [from, from+count) as FAIL, five extra
// FAIL rows that duplicate the first resource, and a PASS row that must be
// ignored, inside one committed transaction.
func recordFixture(t *testing.T, ctx context.Context, connection *sql.Conn, scanID string,
	started time.Time, from, count int) Evaluation {
	t.Helper()
	rows := make([]compliance.ComplianceResult, 0, count+6)
	for index := from; index < from+count; index++ {
		id := fmt.Sprintf("resource-%d", index)
		rows = append(rows, compliance.ComplianceResult{ResourceID: id, ControlID: "C1",
			ControlName: "control", Status: "fail", Severity: "high", Details: "row " + id})
	}
	for duplicate := 0; duplicate < 5; duplicate++ {
		rows = append(rows, compliance.ComplianceResult{ResourceID: fmt.Sprintf("resource-%d", from),
			ControlID: "C1", ControlName: "control", Status: "WARNING", Severity: "low",
			Details: fmt.Sprintf("duplicate %d", duplicate)})
	}
	rows = append(rows, compliance.ComplianceResult{ResourceID: "passing", ControlID: "C1",
		Status: "PASS", Severity: "low"})

	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	scan := dataaccess.ScanRecord{ID: scanID, Provider: "aws", ScopeKey: "scope", StartedAt: started}
	evaluation := Evaluation{ID: "evaluation-" + scanID}
	result := compliance.SimpleQueryResult{PackRef: "pack", ControlID: "C1", Rows: rows}
	observed, err := recordResult(ctx, tx, &evaluation, scan, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != count {
		t.Fatalf("observed = %d, want %d", len(observed), count)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return evaluation
}

func checkFinding(t *testing.T, connection *sql.Conn, resourceID string, occurrences int,
	details string) {
	t.Helper()
	var count int
	var stored string
	err := connection.QueryRowContext(context.Background(), `SELECT occurrence_count, details
FROM findings WHERE resource_id = ?`, resourceID).Scan(&count, &stored)
	if err != nil {
		t.Fatal(err)
	}
	if count != occurrences || stored != details {
		t.Fatalf("%s: occurrences=%d details=%q, want %d %q", resourceID, count, stored,
			occurrences, details)
	}
}

func checkOccurrence(t *testing.T, connection *sql.Conn, evaluationID, resourceID,
	details string) {
	t.Helper()
	var stored string
	err := connection.QueryRowContext(context.Background(), `SELECT o.details
FROM finding_occurrences o JOIN findings f ON f.fingerprint = o.fingerprint
WHERE o.evaluation_id = ? AND f.resource_id = ?`, evaluationID, resourceID).Scan(&stored)
	if err != nil {
		t.Fatal(err)
	}
	if stored != details {
		t.Fatalf("%s occurrence details = %q, want %q", resourceID, stored, details)
	}
}

func checkCount(t *testing.T, connection *sql.Conn, query string, want int) {
	t.Helper()
	var count int
	if err := connection.QueryRowContext(context.Background(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s = %d, want %d", query, count, want)
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

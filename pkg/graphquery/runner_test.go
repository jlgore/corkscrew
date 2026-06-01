package graphquery

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/jlgore/corkscrew/internal/db"
)

func TestRenderTraverseDOT(t *testing.T) {
	rows := []traverseRow{
		{
			NodeID:            "r2",
			NodeType:          "aws::ec2::Instance",
			NodeName:          "i-2",
			PathIDs:           []string{"r1", "r2"},
			RelationshipTypes: []string{"peer"},
		},
		{
			NodeID:            "r3",
			NodeType:          "aws::s3::Bucket",
			NodeName:          "bucket-1",
			PathIDs:           []string{"r1", "r3"},
			RelationshipTypes: []string{"reads"},
		},
	}

	dot := RenderTraverseDOT("r1", rows)
	for _, want := range []string{
		"digraph corkscrew_graph",
		`"r1" [label="r1"`,
		`"r1" -> "r2" [label="peer"]`,
		`"r1" -> "r3" [label="reads"]`,
	} {
		if !strings.Contains(dot, want) {
			t.Fatalf("DOT output missing %q:\n%s", want, dot)
		}
	}
}

func TestTraverseRunnerAgainstFixtureDB(t *testing.T) {
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb CLI not available")
	}

	extensionPath, err := resolveExtensionPath("")
	if err != nil {
		t.Skipf("graph extension not available: %v", err)
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "fixture.duckdb")
	setupGraphFixture(t, dbPath)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := NewRunner(stdout, stderr)
	exitCode := runner.Run([]string{"traverse", "r1", "--db", dbPath, "--extension", extensionPath, "--output", "csv", "--no-header"})
	if exitCode != 0 {
		t.Fatalf("runner failed with code %d: %s", exitCode, stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{"r2", "r3"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = runner.Run([]string{"traverse", "r1", "--db", dbPath, "--extension", extensionPath, "--output", "dot"})
	if exitCode != 0 {
		t.Fatalf("dot runner failed with code %d: %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"r1" -> "r2"`) {
		t.Fatalf("expected DOT output, got:\n%s", stdout.String())
	}
}

func setupGraphFixture(t *testing.T, dbPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	database, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer database.Close()

	config, err := db.InitializeUnifiedDatabase(dbPath)
	if err != nil {
		t.Fatalf("initialize unified db failed: %v", err)
	}
	defer config.DB.Close()

	if _, err := config.DB.Exec(`
		INSERT INTO aws_resources (id, type, name, region, account_id, service, tags, attributes, raw_data)
		VALUES
			('r1', 'aws::ec2::Instance', 'i-1', 'us-east-1', '123', 'ec2', NULL, NULL, NULL),
			('r2', 'aws::ec2::Instance', 'i-2', 'us-east-1', '123', 'ec2', NULL, NULL, NULL),
			('r3', 'aws::s3::Bucket', 'b-1', 'us-east-1', '123', 's3', NULL, NULL, NULL);
		INSERT INTO aws_relationships (from_id, to_id, relationship_type, properties)
		VALUES
			('r1', 'r2', 'peer', NULL),
			('r1', 'r3', 'reads', NULL);
	`); err != nil {
		t.Fatalf("insert fixture failed: %v", err)
	}
}

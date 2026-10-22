// Package packtest runs compliance packs against golden scans in a real DuckDB. A golden scan is
// persisted with the data session's PersistScanOutcome and queried with compliance.NewExecutor,
// the same two steps finding evaluation takes (internal/app/findings), so pack SQL is tested
// against the tables it really runs on.
//
// Golden scans are written by provider tests: plugins/cloudflare-provider's TestGoldenScan
// writes testdata/cloudflare/golden-scan.json from its fake Cloudflare API.
package packtest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	dataaccess "github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/pkg/query/compliance"
	"google.golang.org/protobuf/encoding/protojson"
)

// ScanID is the ID Persist gives the golden scan.
const ScanID = "golden"

// Outcomes are a pack run's statuses: control ID -> resource ID -> PASS, FAIL, WARNING or ERROR.
type Outcomes map[string]map[string]string

// Golden is a golden scan file: the services scanned and the resources found.
type Golden struct {
	Services  []string
	Resources []*pb.Resource
}

// LoadGolden reads a golden scan file.
func LoadGolden(t testing.TB, path string) Golden {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("packtest: %v", err)
	}
	var file struct {
		Services  []string          `json:"services"`
		Resources []json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("packtest: %s: %v", path, err)
	}
	golden := Golden{Services: file.Services}
	for i, raw := range file.Resources {
		resource := &pb.Resource{}
		if err := protojson.Unmarshal(raw, resource); err != nil {
			t.Fatalf("packtest: %s resource %d: %v", path, i, err)
		}
		golden.Resources = append(golden.Resources, resource)
	}
	return golden
}

// Persist writes golden as one completed scan of provider into a new database and returns the
// database's path.
func Persist(t testing.TB, provider string, golden Golden) string {
	t.Helper()
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "golden.duckdb")
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		t.Fatalf("packtest: %v", err)
	}
	defer session.Close()
	started := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if err := session.PersistScanOutcome(ctx, dataaccess.ScanOutcome{
		ID: ScanID, Provider: provider, Services: golden.Services, Scopes: []string{"global"},
		Status:    dataaccess.ScanStatusCompleted,
		StartedAt: started, EndedAt: started.Add(time.Minute),
		Resources: golden.Resources,
	}, dataaccess.PersistScanOptions{}); err != nil {
		t.Fatalf("packtest: persist golden scan: %v", err)
	}
	return target
}

// Results runs every control of pack against the database at target, as finding evaluation
// does. Only the embedded packs and ./query-packs (beside the test) are visible: HOME is moved
// to an empty directory so a pack installed on the machine can't shadow the one under test.
func Results(
	t *testing.T,
	target, pack string,
	params map[string]any,
) []compliance.SimpleQueryResult {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	executor, err := compliance.NewExecutor(target)
	if err != nil {
		t.Fatalf("packtest: %v", err)
	}
	defer executor.Close()
	results, err := executor.Execute(
		context.Background(),
		compliance.ExecuteOptions{PackName: pack, Parameters: params},
	)
	if err != nil {
		t.Fatalf("packtest: run %s: %v", pack, err)
	}
	return results
}

// Run runs pack and returns its outcomes. It fails the test when a control errors, or returns
// no rows: a control that evaluates nothing against a golden scan built to exercise it is broken.
func Run(t *testing.T, target, pack string, params map[string]any) Outcomes {
	t.Helper()
	outcomes := Outcomes{}
	for _, result := range Results(t, target, pack, params) {
		if result.Error != nil {
			t.Errorf("packtest: %s %s: %v", pack, result.ControlID, result.Error)
			continue
		}
		if len(result.Rows) == 0 {
			t.Errorf("packtest: %s %s returned no rows", pack, result.ControlID)
			continue
		}
		statuses := map[string]string{}
		for _, row := range result.Rows {
			if previous, ok := statuses[row.ResourceID]; ok && previous != row.Status {
				t.Errorf(
					"packtest: %s %s gives %s both %s and %s",
					pack,
					result.ControlID,
					row.ResourceID,
					previous,
					row.Status,
				)
			}
			statuses[row.ResourceID] = strings.ToUpper(row.Status)
		}
		outcomes[result.ControlID] = statuses
	}
	return outcomes
}

// Expect is one expected outcome: control gives resource the status want.
type Expect struct {
	Control, Resource, Want string
}

// Check compares outcomes with each expectation, and fails for every control in outcomes that no
// expectation covers with both a PASS and a FAIL: each control needs a fixture of each kind.
func (o Outcomes) Check(t *testing.T, expectations []Expect) {
	t.Helper()
	covered := map[string]map[string]bool{}
	for _, e := range expectations {
		got, ok := o[e.Control][e.Resource]
		switch {
		case !ok:
			t.Errorf("%s: no row for %s; rows: %v", e.Control, e.Resource, sortedKeys(o[e.Control]))
		case got != e.Want:
			t.Errorf("%s %s = %s, want %s", e.Control, e.Resource, got, e.Want)
		}
		if covered[e.Control] == nil {
			covered[e.Control] = map[string]bool{}
		}
		covered[e.Control][e.Want] = true
	}
	for _, control := range sortedKeys(o) {
		if !covered[control]["PASS"] || !covered[control]["FAIL"] {
			t.Errorf("%s needs an expected PASS and an expected FAIL", control)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

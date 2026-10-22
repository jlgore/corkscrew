package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/encoding/protojson"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden scan from the fake API")

// goldenScanPath is the golden scan internal/packtest loads into DuckDB to run the Cloudflare
// packs. The provider is its own module, so this file is the hand-off between the two.
const goldenScanPath = "../../internal/packtest/testdata/cloudflare/golden-scan.json"

// goldenServices are the services the golden scan covers, in the order a full scan runs them.
var goldenServices = []string{"accounts", "zones", "dns", "workers", "storage", "data"}

// goldenScan is the file's shape: the services scanned and every resource, with relationships.
type goldenScan struct {
	Services  []string          `json:"services"`
	Resources []json.RawMessage `json:"resources"`
}

// TestGoldenScan scans the baseline account and compares the result with the golden file. Run
// `go test -run TestGoldenScan -update` after a deliberate change to the scanners or fixtures.
func TestGoldenScan(t *testing.T) {
	p := newTestProvider(t, newFakeAPI(t))
	resp, err := p.BatchScan(context.Background(), &pb.BatchScanRequest{
		Services: goldenServices, IncludeRelationships: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("the baseline scan must be clean: %v", resp.Errors)
	}
	got := encodeGolden(t, resp.Resources)

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenScanPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenScanPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenScanPath)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the scan differs from %s; if the change is intended, run `go test -run TestGoldenScan -update` and review the diff", goldenScanPath)
	}
}

// encodeGolden writes resources deterministically: sorted by type and ID, relationships sorted,
// the scan-time timestamp cleared, and JSON whitespace normalized (protojson varies it on
// purpose).
func encodeGolden(t *testing.T, resources []*pb.Resource) []byte {
	t.Helper()
	sorted := append([]*pb.Resource(nil), resources...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Type != sorted[j].Type {
			return sorted[i].Type < sorted[j].Type
		}
		return sorted[i].Id < sorted[j].Id
	})
	scan := goldenScan{Services: goldenServices}
	for _, resource := range sorted {
		resource.DiscoveredAt = nil
		sort.Slice(resource.Relationships, func(i, j int) bool {
			return resource.Relationships[i].TargetId < resource.Relationships[j].TargetId
		})
		encoded, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resource)
		if err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, encoded); err != nil {
			t.Fatal(err)
		}
		scan.Resources = append(scan.Resources, compact.Bytes())
	}
	out, err := json.MarshalIndent(scan, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

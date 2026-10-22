package compliance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

// writeChainPack writes pack chain/p<index>, which requires chain/p<index+1> unless it is last.
func writeChainPack(t *testing.T, root string, index, last int) {
	t.Helper()
	dir := filepath.Join(root, "chain", fmt.Sprintf("p%d", index))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dependsOn := ""
	if index < last {
		dependsOn = fmt.Sprintf(`  depends_on:
    - name: p%d
      namespace: chain/p%d
      required: true
`, index+1, index+1)
	}
	manifest := fmt.Sprintf(`apiVersion: v1
kind: QueryPack
metadata:
  name: p%d
  namespace: chain/p%d
  version: 1.0.0
  description: Chain pack
  provider: aws
spec:
%s  queries:
    - id: q%d
      title: Query
      description: A query
      severity: LOW
      category: governance
      query_file: q.sql
      enabled: true
`, index, index, dependsOn, index)
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "q.sql"), []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chainLoader(t *testing.T, packCount int) *PackLoader {
	t.Helper()
	root := t.TempDir()
	for index := 0; index < packCount; index++ {
		writeChainPack(t, root, index, packCount-1)
	}
	return NewPackLoader().WithSearchPaths(root).WithEmbeddedPacks(false)
}

// Pack manifests are user files, so an over-deep dependency chain is an operating error.
func TestLoadPackRejectsDependencyChainDeeperThanPackDepthMax(t *testing.T) {
	loader := chainLoader(t, packDepthMax+1)
	_, err := loader.LoadPack(context.Background(), "chain/p0")
	if err == nil || !strings.Contains(err.Error(), "dependency chain deeper than") {
		t.Fatalf("err = %v, want a dependency depth error", err)
	}
}

func TestLoadPackAcceptsDependencyChainAtPackDepthMax(t *testing.T) {
	loader := chainLoader(t, packDepthMax)
	if _, err := loader.LoadPack(context.Background(), "chain/p0"); err != nil {
		t.Fatalf("chain of %d packs: %v", packDepthMax, err)
	}
}

// The depth check rejects a chain before it grows past packDepthMax, so a load path that is
// already too long can only come from a caller bug.
func TestLoadPackInternalAssertsLoadPathWithinPackDepthMax(t *testing.T) {
	loader := chainLoader(t, 1)
	loadPath := make([]string, packDepthMax+1)
	asserttest.FailsWith(t, "packDepthMax", func() {
		_, _ = loader.loadPackInternal(context.Background(), "chain/p0", map[string]bool{},
			loadPath)
	})
}

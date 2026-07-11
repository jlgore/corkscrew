package smartscan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestSaveResultsToTimestampedFile(t *testing.T) {
	tempDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	results := &AggregatedResults{
		AllResources: []*pb.Resource{
			{
				Provider: "aws",
				Service:  "s3",
				Type:     "Bucket",
				Id:       "bucket-1",
				Name:     "bucket-1",
			},
		},
		Summary: &ScanSummary{TotalResources: 1},
	}

	filename, err := saveResultsToTimestampedFile(results, "aws")
	if err != nil {
		t.Fatalf("saveResultsToTimestampedFile returned error: %v", err)
	}
	if !strings.HasPrefix(filename, "enhanced-scan-aws-") || !strings.HasSuffix(filename, ".json") {
		t.Fatalf("filename = %q, want timestamped aws JSON filename", filename)
	}

	data, err := os.ReadFile(filepath.Join(tempDir, filename))
	if err != nil {
		t.Fatalf("read saved results: %v", err)
	}

	var saved AggregatedResults
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("saved results are not valid JSON: %v", err)
	}
	if len(saved.AllResources) != 1 || saved.AllResources[0].Id != "bucket-1" {
		t.Fatalf("saved resources = %#v, want bucket-1", saved.AllResources)
	}
	if saved.Summary == nil || saved.Summary.TotalResources != 1 {
		t.Fatalf("saved summary = %#v, want total resources 1", saved.Summary)
	}
}

package changestore

import (
	"testing"
	"time"
)

func TestDuckDBStoreStoresAndQueriesChanges(t *testing.T) {
	store, err := NewDuckDBStore(":memory:", "unused.db", "")
	if err != nil {
		t.Fatalf("NewDuckDBStore returned error: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	change := &Event{
		ID:             "change-1",
		Provider:       "aws",
		ResourceID:     "resource-1",
		ResourceName:   "bucket",
		ResourceType:   "AWS::S3::Bucket",
		Service:        "s3",
		Project:        "account-1",
		Region:         "us-east-1",
		ChangeType:     "UPDATE",
		Severity:       "HIGH",
		Timestamp:      now,
		DetectedAt:     now,
		ChangedFields:  []string{"policy"},
		ChangeMetadata: map[string]interface{}{"source": "test"},
		RelatedChanges: []string{"change-0"},
	}

	if err := store.StoreChange(change); err != nil {
		t.Fatalf("StoreChange returned error: %v", err)
	}

	results, err := store.QueryChanges(&Query{
		Provider:    "aws",
		ChangeTypes: []string{"UPDATE"},
		ResourceFilter: &ResourceFilter{
			Services: []string{"s3"},
		},
	})
	if err != nil {
		t.Fatalf("QueryChanges returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != change.ID {
		t.Fatalf("expected change ID %s, got %s", change.ID, results[0].ID)
	}
	if results[0].ChangeMetadata["source"] != "test" {
		t.Fatalf("expected metadata to round trip, got %#v", results[0].ChangeMetadata)
	}
}

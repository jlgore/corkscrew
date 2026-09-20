package drift

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"

	dataaccess "github.com/jlgore/corkscrew/internal/data"
)

type Request struct {
	Target   string
	Provider string
	FromScan string
	ToScan   string
}

type ChangeType string

const (
	Added    ChangeType = "added"
	Modified ChangeType = "modified"
	Removed  ChangeType = "removed"
)

type Change struct {
	Type          ChangeType              `json:"change_type"`
	Provider      string                  `json:"provider"`
	ResourceID    string                  `json:"resource_id"`
	ChangedFields []string                `json:"changed_fields,omitempty"`
	Before        *dataaccess.Observation `json:"before,omitempty"`
	After         *dataaccess.Observation `json:"after,omitempty"`
}

type Result struct {
	From       dataaccess.ScanRecord `json:"from"`
	To         dataaccess.ScanRecord `json:"to"`
	Comparable bool                  `json:"comparable"`
	Incomplete bool                  `json:"incomplete"`
	Changes    []Change              `json:"changes"`
}

func Run(ctx context.Context, request Request) (Result, error) {
	session, err := dataaccess.OpenSession(ctx, request.Target)
	if err != nil {
		return Result{}, err
	}
	defer session.Close()

	var to dataaccess.ScanRecord
	if request.ToScan != "" {
		to, err = session.GetScan(ctx, request.ToScan)
	} else if request.Provider != "" {
		to, err = session.LatestCompleteScan(ctx, request.Provider)
	} else {
		return Result{}, fmt.Errorf("--to or --provider is required")
	}
	if err != nil {
		return Result{}, fmt.Errorf("resolve target scan: %w", err)
	}

	var from dataaccess.ScanRecord
	if request.FromScan != "" {
		from, err = session.GetScan(ctx, request.FromScan)
	} else {
		from, err = session.PreviousComparableScan(ctx, to)
	}
	if err == sql.ErrNoRows {
		return Result{}, fmt.Errorf("scan %s has no previous comparable completed scan", to.ID)
	}
	if err != nil {
		return Result{}, fmt.Errorf("resolve baseline scan: %w", err)
	}

	before, err := session.Observations(ctx, from.ID)
	if err != nil {
		return Result{}, err
	}
	after, err := session.Observations(ctx, to.ID)
	if err != nil {
		return Result{}, err
	}
	result := Result{From: from, To: to}
	result.Comparable = from.Provider == to.Provider && from.ScopeKey != "" && from.ScopeKey == to.ScopeKey
	result.Incomplete = !result.Comparable || !from.SnapshotComplete || !to.SnapshotComplete
	result.Changes = compare(before, after)
	return result, nil
}

func compare(before, after []dataaccess.Observation) []Change {
	key := func(item dataaccess.Observation) string { return item.Provider + "\x00" + item.ResourceID }
	left := make(map[string]dataaccess.Observation, len(before))
	right := make(map[string]dataaccess.Observation, len(after))
	for _, item := range before {
		left[key(item)] = item
	}
	for _, item := range after {
		right[key(item)] = item
	}
	var changes []Change
	for identity, old := range left {
		current, ok := right[identity]
		if !ok {
			copy := old
			changes = append(changes, Change{Type: Removed, Provider: old.Provider, ResourceID: old.ResourceID, Before: &copy})
			continue
		}
		if old.SemanticHash != current.SemanticHash {
			oldCopy, currentCopy := old, current
			changes = append(changes, Change{Type: Modified, Provider: old.Provider, ResourceID: old.ResourceID,
				ChangedFields: changedFields(old, current), Before: &oldCopy, After: &currentCopy})
		}
	}
	for identity, current := range right {
		if _, ok := left[identity]; ok {
			continue
		}
		copy := current
		changes = append(changes, Change{Type: Added, Provider: current.Provider, ResourceID: current.ResourceID, After: &copy})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Provider != changes[j].Provider {
			return changes[i].Provider < changes[j].Provider
		}
		if changes[i].Type != changes[j].Type {
			return changes[i].Type < changes[j].Type
		}
		return changes[i].ResourceID < changes[j].ResourceID
	})
	return changes
}

func changedFields(before, after dataaccess.Observation) []string {
	fields := []struct {
		name          string
		before, after any
	}{
		{"name", before.Name, after.Name}, {"type", before.Type, after.Type},
		{"service", before.Service, after.Service}, {"location", before.Location, after.Location},
		{"account_id", before.AccountID, after.AccountID}, {"arn", before.ARN, after.ARN},
		{"parent_id", before.ParentID, after.ParentID}, {"tags", before.Tags, after.Tags},
		{"attributes", before.Attributes, after.Attributes}, {"raw_data", before.RawData, after.RawData},
	}
	var result []string
	for _, field := range fields {
		if !reflect.DeepEqual(field.before, field.after) {
			result = append(result, field.name)
		}
	}
	return result
}

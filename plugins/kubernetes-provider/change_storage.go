package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jlgore/corkscrew/internal/changestore"
)

// DuckDBChangeStorage adapts the shared DuckDB change store to Kubernetes provider types.
type DuckDBChangeStorage struct {
	store *changestore.DuckDBStore
}

// NewDuckDBChangeStorage creates a new DuckDB-based change storage.
func NewDuckDBChangeStorage(dbPath string) (*DuckDBChangeStorage, error) {
	store, err := changestore.NewDuckDBStore(dbPath, "k8s_change_tracking.db", "Kubernetes")
	if err != nil {
		return nil, err
	}
	return &DuckDBChangeStorage{store: store}, nil
}

func (dcs *DuckDBChangeStorage) StoreChange(change *ChangeEvent) error {
	return dcs.store.StoreChange(toStoreEvent(change))
}

func (dcs *DuckDBChangeStorage) StoreChanges(changes []*ChangeEvent) error {
	events := make([]*changestore.Event, 0, len(changes))
	for _, change := range changes {
		if change != nil {
			events = append(events, toStoreEvent(change))
		}
	}
	return dcs.store.StoreChanges(events)
}

func (dcs *DuckDBChangeStorage) QueryChanges(query *ChangeQuery) ([]*ChangeEvent, error) {
	events, err := dcs.store.QueryChanges(toStoreQuery(query))
	if err != nil {
		return nil, err
	}
	return fromStoreEvents(events)
}

func (dcs *DuckDBChangeStorage) GetChangeHistory(resourceID string) ([]*ChangeEvent, error) {
	events, err := dcs.store.GetChangeHistory(resourceID)
	if err != nil {
		return nil, err
	}
	return fromStoreEvents(events)
}

func (dcs *DuckDBChangeStorage) GetChange(changeID string) (*ChangeEvent, error) {
	event, err := dcs.store.GetChange(changeID)
	if err != nil {
		return nil, err
	}
	return fromStoreEvent(event)
}

func (dcs *DuckDBChangeStorage) DeleteChanges(olderThan time.Time) error {
	return dcs.store.DeleteChanges(olderThan)
}

func (dcs *DuckDBChangeStorage) StoreBaseline(baseline *DriftBaseline) error {
	return dcs.store.StoreBaseline(toStoreBaseline(baseline))
}

func (dcs *DuckDBChangeStorage) GetBaseline(baselineID string) (*DriftBaseline, error) {
	baseline, err := dcs.store.GetBaseline(baselineID)
	if err != nil {
		return nil, err
	}
	return fromStoreBaseline(baseline)
}

func (dcs *DuckDBChangeStorage) ListBaselines(provider string) ([]*DriftBaseline, error) {
	baselines, err := dcs.store.ListBaselines(provider)
	if err != nil {
		return nil, err
	}
	result := make([]*DriftBaseline, 0, len(baselines))
	for _, baseline := range baselines {
		converted, err := fromStoreBaseline(baseline)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func (dcs *DuckDBChangeStorage) UpdateBaseline(baseline *DriftBaseline) error {
	return dcs.store.UpdateBaseline(toStoreBaseline(baseline))
}

func (dcs *DuckDBChangeStorage) DeleteBaseline(baselineID string) error {
	return dcs.store.DeleteBaseline(baselineID)
}

func (dcs *DuckDBChangeStorage) Close() error {
	return dcs.store.Close()
}

func toStoreEvent(change *ChangeEvent) *changestore.Event {
	if change == nil {
		return nil
	}
	return &changestore.Event{
		ID:               change.ID,
		Provider:         change.Provider,
		ResourceID:       change.ResourceID,
		ResourceName:     change.ResourceName,
		ResourceType:     change.ResourceType,
		Service:          change.Service,
		Project:          change.Project,
		Region:           change.Region,
		ChangeType:       string(change.ChangeType),
		Severity:         string(change.Severity),
		Timestamp:        change.Timestamp,
		DetectedAt:       change.DetectedAt,
		PreviousState:    change.PreviousState,
		CurrentState:     change.CurrentState,
		ChangedFields:    change.ChangedFields,
		ChangeMetadata:   change.ChangeMetadata,
		ImpactAssessment: change.ImpactAssessment,
		ComplianceImpact: change.ComplianceImpact,
		RelatedChanges:   change.RelatedChanges,
	}
}

func fromStoreEvents(events []*changestore.Event) ([]*ChangeEvent, error) {
	changes := make([]*ChangeEvent, 0, len(events))
	for _, event := range events {
		change, err := fromStoreEvent(event)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func fromStoreEvent(event *changestore.Event) (*ChangeEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("change event cannot be nil")
	}

	change := &ChangeEvent{
		ID:             event.ID,
		Provider:       event.Provider,
		ResourceID:     event.ResourceID,
		ResourceName:   event.ResourceName,
		ResourceType:   event.ResourceType,
		Service:        event.Service,
		Project:        event.Project,
		Region:         event.Region,
		ChangeType:     ChangeType(event.ChangeType),
		Severity:       ChangeSeverity(event.Severity),
		Timestamp:      event.Timestamp,
		DetectedAt:     event.DetectedAt,
		ChangedFields:  event.ChangedFields,
		ChangeMetadata: event.ChangeMetadata,
		RelatedChanges: event.RelatedChanges,
	}

	if event.PreviousState != nil {
		var state ResourceState
		if err := decodeStoreValue(event.PreviousState, &state); err != nil {
			return nil, err
		}
		change.PreviousState = &state
	}
	if event.CurrentState != nil {
		var state ResourceState
		if err := decodeStoreValue(event.CurrentState, &state); err != nil {
			return nil, err
		}
		change.CurrentState = &state
	}
	if event.ImpactAssessment != nil {
		var impact ImpactAssessment
		if err := decodeStoreValue(event.ImpactAssessment, &impact); err != nil {
			return nil, err
		}
		change.ImpactAssessment = &impact
	}
	if event.ComplianceImpact != nil {
		var impact ComplianceImpact
		if err := decodeStoreValue(event.ComplianceImpact, &impact); err != nil {
			return nil, err
		}
		change.ComplianceImpact = &impact
	}

	return change, nil
}

func toStoreQuery(query *ChangeQuery) *changestore.Query {
	if query == nil {
		return nil
	}
	storeQuery := &changestore.Query{
		Provider:    query.Provider,
		ChangeTypes: changeTypesToStrings(query.ChangeTypes),
		Severities:  severitiesToStrings(query.Severities),
		StartTime:   query.StartTime,
		EndTime:     query.EndTime,
		Limit:       query.Limit,
		Offset:      query.Offset,
		SortBy:      query.SortBy,
		SortOrder:   query.SortOrder,
	}
	if query.ResourceFilter != nil {
		storeQuery.ResourceFilter = &changestore.ResourceFilter{
			ResourceIDs:   query.ResourceFilter.ResourceIDs,
			ResourceTypes: query.ResourceFilter.ResourceTypes,
			Services:      query.ResourceFilter.Services,
			Projects:      query.ResourceFilter.Projects,
			Regions:       query.ResourceFilter.Regions,
		}
	}
	return storeQuery
}

func toStoreBaseline(baseline *DriftBaseline) *changestore.Baseline {
	if baseline == nil {
		return nil
	}
	return &changestore.Baseline{
		ID:          baseline.ID,
		Name:        baseline.Name,
		Description: baseline.Description,
		Provider:    baseline.Provider,
		CreatedAt:   baseline.CreatedAt,
		UpdatedAt:   baseline.UpdatedAt,
		Resources:   baseline.Resources,
		Policies:    baseline.Policies,
		Tags:        baseline.Tags,
		Version:     baseline.Version,
		Active:      baseline.Active,
	}
}

func fromStoreBaseline(baseline *changestore.Baseline) (*DriftBaseline, error) {
	if baseline == nil {
		return nil, fmt.Errorf("baseline cannot be nil")
	}

	result := &DriftBaseline{
		ID:          baseline.ID,
		Name:        baseline.Name,
		Description: baseline.Description,
		Provider:    baseline.Provider,
		CreatedAt:   baseline.CreatedAt,
		UpdatedAt:   baseline.UpdatedAt,
		Tags:        baseline.Tags,
		Version:     baseline.Version,
		Active:      baseline.Active,
	}
	if baseline.Resources != nil {
		if err := decodeStoreValue(baseline.Resources, &result.Resources); err != nil {
			return nil, err
		}
	}
	if baseline.Policies != nil {
		if err := decodeStoreValue(baseline.Policies, &result.Policies); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func changeTypesToStrings(changeTypes []ChangeType) []string {
	values := make([]string, 0, len(changeTypes))
	for _, changeType := range changeTypes {
		values = append(values, string(changeType))
	}
	return values
}

func severitiesToStrings(severities []ChangeSeverity) []string {
	values := make([]string, 0, len(severities))
	for _, severity := range severities {
		values = append(values, string(severity))
	}
	return values
}

func decodeStoreValue(value interface{}, target interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to encode stored change value: %w", err)
	}
	if string(data) == "null" {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to decode stored change value: %w", err)
	}
	return nil
}

package data

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ScanRecord is the durable identity and coverage of one provider scan.
type ScanRecord struct {
	ID               string    `json:"id"`
	Provider         string    `json:"provider"`
	ServicesJSON     string    `json:"services"`
	ScopesJSON       string    `json:"scopes"`
	ScopeKey         string    `json:"scope_key"`
	Status           string    `json:"status"`
	SnapshotComplete bool      `json:"snapshot_complete"`
	StartedAt        time.Time `json:"started_at"`
	EndedAt          time.Time `json:"ended_at"`
	TotalResources   int       `json:"total_resources"`
	NewResources     int       `json:"new_resources"`
	UpdatedResources int       `json:"updated_resources"`
	DeletedResources int       `json:"deleted_resources"`
}

// Observation is a normalized resource state captured by one scan.
type Observation struct {
	ScanID       string    `json:"scan_id"`
	Provider     string    `json:"provider"`
	ResourceID   string    `json:"resource_id"`
	Name         string    `json:"name,omitempty"`
	Type         string    `json:"type"`
	Service      string    `json:"service,omitempty"`
	Location     string    `json:"location,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	ARN          string    `json:"arn,omitempty"`
	ParentID     string    `json:"parent_id,omitempty"`
	Tags         string    `json:"tags,omitempty"`
	Attributes   string    `json:"attributes,omitempty"`
	RawData      string    `json:"raw_data,omitempty"`
	SemanticHash string    `json:"semantic_hash"`
	ObservedAt   time.Time `json:"observed_at"`
}

type RelationshipObservation struct {
	ScanID, Provider, FromID, ToID, Type, Subtype, Properties, FromType, ToType, Direction string
}

func (s *Session) ListScans(ctx context.Context, provider string, limit int) ([]ScanRecord, error) {
	if s == nil || s.database == nil {
		return nil, fmt.Errorf("data session is closed")
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.database.QueryContext(ctx, `
SELECT id, provider, CAST(services AS VARCHAR), CAST(regions AS VARCHAR),
       COALESCE(scope_key, ''), status, COALESCE(snapshot_complete, FALSE),
       scan_start_time, COALESCE(scan_end_time, scan_start_time), total_resources,
       new_resources, updated_resources, deleted_resources
FROM scan_metadata WHERE (? = '' OR provider = ?)
ORDER BY scan_start_time DESC, id DESC LIMIT ?`, provider, provider, limit)
	if err != nil {
		return nil, fmt.Errorf("list scans: %w", err)
	}
	defer rows.Close()
	var result []ScanRecord
	for rows.Next() {
		var record ScanRecord
		if err := rows.Scan(&record.ID, &record.Provider, &record.ServicesJSON, &record.ScopesJSON,
			&record.ScopeKey, &record.Status, &record.SnapshotComplete, &record.StartedAt,
			&record.EndedAt, &record.TotalResources, &record.NewResources,
			&record.UpdatedResources, &record.DeletedResources); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (s *Session) GetScan(ctx context.Context, id string) (ScanRecord, error) {
	rows, err := s.ListScans(ctx, "", 1000000)
	if err != nil {
		return ScanRecord{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return ScanRecord{}, sql.ErrNoRows
}

func (s *Session) LatestCompleteScan(ctx context.Context, provider string) (ScanRecord, error) {
	var id string
	err := s.database.QueryRowContext(ctx, `
SELECT id FROM scan_metadata
WHERE provider = ? AND snapshot_complete = TRUE AND status = 'completed'
ORDER BY scan_start_time DESC, id DESC LIMIT 1`, provider).Scan(&id)
	if err != nil {
		return ScanRecord{}, err
	}
	return s.GetScan(ctx, id)
}

func (s *Session) PreviousComparableScan(ctx context.Context, scan ScanRecord) (ScanRecord, error) {
	var id string
	err := s.database.QueryRowContext(ctx, `
SELECT id FROM scan_metadata
WHERE provider = ? AND scope_key = ? AND snapshot_complete = TRUE
  AND status = 'completed' AND id <> ? AND scan_start_time <= ?
ORDER BY scan_start_time DESC, id DESC LIMIT 1`, scan.Provider, scan.ScopeKey, scan.ID, scan.StartedAt).Scan(&id)
	if err != nil {
		return ScanRecord{}, err
	}
	return s.GetScan(ctx, id)
}

func (s *Session) Observations(ctx context.Context, scanID string) ([]Observation, error) {
	rows, err := s.database.QueryContext(ctx, `
SELECT scan_id, provider, resource_id, COALESCE(name, ''), type,
       COALESCE(service, ''), COALESCE(location, ''), COALESCE(account_id, ''),
       COALESCE(arn, ''), COALESCE(parent_id, ''), COALESCE(CAST(tags AS VARCHAR), ''),
       COALESCE(CAST(attributes AS VARCHAR), ''), COALESCE(CAST(raw_data AS VARCHAR), ''),
       semantic_hash, observed_at
FROM resource_observations WHERE scan_id = ?
ORDER BY provider, resource_id`, scanID)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	defer rows.Close()
	var result []Observation
	for rows.Next() {
		var observation Observation
		if err := rows.Scan(&observation.ScanID, &observation.Provider, &observation.ResourceID,
			&observation.Name, &observation.Type, &observation.Service, &observation.Location,
			&observation.AccountID, &observation.ARN, &observation.ParentID, &observation.Tags,
			&observation.Attributes, &observation.RawData, &observation.SemanticHash,
			&observation.ObservedAt); err != nil {
			return nil, err
		}
		result = append(result, observation)
	}
	return result, rows.Err()
}

func (s *Session) RelationshipObservations(ctx context.Context, scanID string) ([]RelationshipObservation, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT scan_id, provider, from_id, to_id,
relationship_type, COALESCE(relationship_subtype, ''), COALESCE(CAST(properties AS VARCHAR), ''),
COALESCE(from_resource_type, ''), COALESCE(to_resource_type, ''), COALESCE(direction, '')
FROM relationship_observations WHERE scan_id = ? ORDER BY provider, from_id, to_id`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RelationshipObservation
	for rows.Next() {
		var item RelationshipObservation
		if err := rows.Scan(&item.ScanID, &item.Provider, &item.FromID, &item.ToID, &item.Type,
			&item.Subtype, &item.Properties, &item.FromType, &item.ToType, &item.Direction); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

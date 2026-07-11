package changestore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

type Event struct {
	ID               string
	Provider         string
	ResourceID       string
	ResourceName     string
	ResourceType     string
	Service          string
	Project          string
	Region           string
	ChangeType       string
	Severity         string
	Timestamp        time.Time
	DetectedAt       time.Time
	PreviousState    interface{}
	CurrentState     interface{}
	ChangedFields    []string
	ChangeMetadata   map[string]interface{}
	ImpactAssessment interface{}
	ComplianceImpact interface{}
	RelatedChanges   []string
}

type Query struct {
	Provider       string
	ResourceFilter *ResourceFilter
	ChangeTypes    []string
	Severities     []string
	StartTime      time.Time
	EndTime        time.Time
	Limit          int
	Offset         int
	SortBy         string
	SortOrder      string
}

type ResourceFilter struct {
	ResourceIDs   []string
	ResourceTypes []string
	Services      []string
	Projects      []string
	Regions       []string
}

type Baseline struct {
	ID          string
	Name        string
	Description string
	Provider    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Resources   interface{}
	Policies    interface{}
	Tags        map[string]string
	Version     string
	Active      bool
}

type DuckDBStore struct {
	db      *sql.DB
	logName string
}

func NewDuckDBStore(dbPath, defaultPath, logName string) (*DuckDBStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = defaultPath
	}
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &DuckDBStore{db: db, logName: logName}
	if err := store.initializeSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return store, nil
}

func (s *DuckDBStore) initializeSchema() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS change_events (
			id VARCHAR PRIMARY KEY,
			provider VARCHAR NOT NULL,
			resource_id VARCHAR NOT NULL,
			resource_name VARCHAR,
			resource_type VARCHAR NOT NULL,
			service VARCHAR NOT NULL,
			project VARCHAR,
			region VARCHAR,
			change_type VARCHAR NOT NULL,
			severity VARCHAR NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			detected_at TIMESTAMP NOT NULL,
			previous_state JSON,
			current_state JSON,
			changed_fields JSON,
			change_metadata JSON,
			impact_assessment JSON,
			compliance_impact JSON,
			related_changes JSON
		);`,
		`CREATE TABLE IF NOT EXISTS drift_baselines (
			id VARCHAR PRIMARY KEY,
			name VARCHAR NOT NULL,
			description TEXT,
			provider VARCHAR NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			resources JSON,
			policies JSON,
			tags JSON,
			version VARCHAR,
			active BOOLEAN DEFAULT true
		);`,
	}

	for _, schema := range schemas {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	if s.logName != "" {
		log.Printf("%s change tracking database schema initialized successfully", s.logName)
	}
	return nil
}

func (s *DuckDBStore) StoreChange(change *Event) error {
	if change == nil {
		return fmt.Errorf("change event cannot be nil")
	}

	args, err := eventInsertArgs(change)
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(eventInsertSQL, args...); err != nil {
		return fmt.Errorf("failed to store change event: %w", err)
	}
	return nil
}

func (s *DuckDBStore) StoreChanges(changes []*Event) error {
	if len(changes) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(eventInsertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	stored := 0
	for _, change := range changes {
		if change == nil {
			continue
		}
		args, err := eventInsertArgs(change)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(args...); err != nil {
			return fmt.Errorf("failed to store change event %s: %w", change.ID, err)
		}
		stored++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	log.Printf("Stored %d change events successfully", stored)
	return nil
}

func (s *DuckDBStore) QueryChanges(query *Query) ([]*Event, error) {
	if query == nil {
		return nil, fmt.Errorf("query cannot be nil")
	}

	sqlQuery, args := buildChangeQuery(query)
	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var changes []*Event
	for rows.Next() {
		change, err := scanChangeEvent(rows)
		if err != nil {
			log.Printf("Failed to scan change event: %v", err)
			continue
		}
		changes = append(changes, change)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading query results: %w", err)
	}
	return changes, nil
}

func (s *DuckDBStore) GetChangeHistory(resourceID string) ([]*Event, error) {
	rows, err := s.db.Query(selectEventsSQL+" WHERE resource_id = ? ORDER BY timestamp DESC LIMIT 1000", resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query change history: %w", err)
	}
	defer rows.Close()

	var changes []*Event
	for rows.Next() {
		change, err := scanChangeEvent(rows)
		if err != nil {
			log.Printf("Failed to scan change event: %v", err)
			continue
		}
		changes = append(changes, change)
	}
	return changes, rows.Err()
}

func (s *DuckDBStore) GetChange(changeID string) (*Event, error) {
	row := s.db.QueryRow(selectEventsSQL+" WHERE id = ?", changeID)
	return scanChangeEvent(row)
}

func (s *DuckDBStore) DeleteChanges(olderThan time.Time) error {
	result, err := s.db.Exec(`DELETE FROM change_events WHERE timestamp < ?`, olderThan)
	if err != nil {
		return fmt.Errorf("failed to delete old changes: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	log.Printf("Deleted %d change events older than %v", rowsAffected, olderThan)
	return nil
}

func (s *DuckDBStore) StoreBaseline(baseline *Baseline) error {
	if baseline == nil {
		return fmt.Errorf("baseline cannot be nil")
	}

	resourcesJSON, err := json.Marshal(baseline.Resources)
	if err != nil {
		return fmt.Errorf("failed to encode baseline resources: %w", err)
	}
	policiesJSON, err := json.Marshal(baseline.Policies)
	if err != nil {
		return fmt.Errorf("failed to encode baseline policies: %w", err)
	}
	tagsJSON, err := json.Marshal(baseline.Tags)
	if err != nil {
		return fmt.Errorf("failed to encode baseline tags: %w", err)
	}

	_, err = s.db.Exec(`INSERT INTO drift_baselines (
		id, name, description, provider, created_at, updated_at,
		resources, policies, tags, version, active
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		baseline.ID,
		baseline.Name,
		baseline.Description,
		baseline.Provider,
		baseline.CreatedAt,
		baseline.UpdatedAt,
		string(resourcesJSON),
		string(policiesJSON),
		string(tagsJSON),
		baseline.Version,
		baseline.Active,
	)
	if err != nil {
		return fmt.Errorf("failed to store baseline: %w", err)
	}
	return nil
}

func (s *DuckDBStore) GetBaseline(baselineID string) (*Baseline, error) {
	row := s.db.QueryRow(`SELECT id, name, description, provider, created_at, updated_at,
		resources, policies, tags, version, active FROM drift_baselines WHERE id = ?`, baselineID)
	return scanBaseline(row, fmt.Sprintf("baseline not found: %s", baselineID))
}

func (s *DuckDBStore) ListBaselines(provider string) ([]*Baseline, error) {
	rows, err := s.db.Query(`SELECT id, name, description, provider, created_at, updated_at,
		resources, policies, tags, version, active FROM drift_baselines WHERE provider = ? ORDER BY created_at DESC`, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query baselines: %w", err)
	}
	defer rows.Close()

	var baselines []*Baseline
	for rows.Next() {
		baseline, err := scanBaseline(rows, "")
		if err != nil {
			log.Printf("Failed to scan baseline: %v", err)
			continue
		}
		baselines = append(baselines, baseline)
	}
	return baselines, rows.Err()
}

func (s *DuckDBStore) UpdateBaseline(baseline *Baseline) error {
	if baseline == nil {
		return fmt.Errorf("baseline cannot be nil")
	}

	baseline.UpdatedAt = time.Now()
	resourcesJSON, err := json.Marshal(baseline.Resources)
	if err != nil {
		return fmt.Errorf("failed to encode baseline resources: %w", err)
	}
	policiesJSON, err := json.Marshal(baseline.Policies)
	if err != nil {
		return fmt.Errorf("failed to encode baseline policies: %w", err)
	}
	tagsJSON, err := json.Marshal(baseline.Tags)
	if err != nil {
		return fmt.Errorf("failed to encode baseline tags: %w", err)
	}

	_, err = s.db.Exec(`UPDATE drift_baselines SET
		name = ?, description = ?, updated_at = ?,
		resources = ?, policies = ?, tags = ?,
		version = ?, active = ?
		WHERE id = ?`,
		baseline.Name,
		baseline.Description,
		baseline.UpdatedAt,
		string(resourcesJSON),
		string(policiesJSON),
		string(tagsJSON),
		baseline.Version,
		baseline.Active,
		baseline.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update baseline: %w", err)
	}
	return nil
}

func (s *DuckDBStore) DeleteBaseline(baselineID string) error {
	if _, err := s.db.Exec(`DELETE FROM drift_baselines WHERE id = ?`, baselineID); err != nil {
		return fmt.Errorf("failed to delete baseline: %w", err)
	}
	return nil
}

func (s *DuckDBStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

const eventInsertSQL = `INSERT INTO change_events (
	id, provider, resource_id, resource_name, resource_type, service,
	project, region, change_type, severity, timestamp, detected_at,
	previous_state, current_state, changed_fields, change_metadata,
	impact_assessment, compliance_impact, related_changes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const selectEventsSQL = `SELECT id, provider, resource_id, resource_name, resource_type, service,
	project, region, change_type, severity, timestamp, detected_at,
	previous_state, current_state, changed_fields, change_metadata,
	impact_assessment, compliance_impact, related_changes FROM change_events`

func eventInsertArgs(change *Event) ([]interface{}, error) {
	previousStateJSON, err := json.Marshal(change.PreviousState)
	if err != nil {
		return nil, fmt.Errorf("failed to encode previous state for %s: %w", change.ID, err)
	}
	currentStateJSON, err := json.Marshal(change.CurrentState)
	if err != nil {
		return nil, fmt.Errorf("failed to encode current state for %s: %w", change.ID, err)
	}
	changedFieldsJSON, err := json.Marshal(change.ChangedFields)
	if err != nil {
		return nil, fmt.Errorf("failed to encode changed fields for %s: %w", change.ID, err)
	}
	changeMetadataJSON, err := json.Marshal(change.ChangeMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to encode change metadata for %s: %w", change.ID, err)
	}
	impactAssessmentJSON, err := json.Marshal(change.ImpactAssessment)
	if err != nil {
		return nil, fmt.Errorf("failed to encode impact assessment for %s: %w", change.ID, err)
	}
	complianceImpactJSON, err := json.Marshal(change.ComplianceImpact)
	if err != nil {
		return nil, fmt.Errorf("failed to encode compliance impact for %s: %w", change.ID, err)
	}
	relatedChangesJSON, err := json.Marshal(change.RelatedChanges)
	if err != nil {
		return nil, fmt.Errorf("failed to encode related changes for %s: %w", change.ID, err)
	}

	return []interface{}{
		change.ID,
		change.Provider,
		change.ResourceID,
		change.ResourceName,
		change.ResourceType,
		change.Service,
		change.Project,
		change.Region,
		change.ChangeType,
		change.Severity,
		change.Timestamp,
		change.DetectedAt,
		string(previousStateJSON),
		string(currentStateJSON),
		string(changedFieldsJSON),
		string(changeMetadataJSON),
		string(impactAssessmentJSON),
		string(complianceImpactJSON),
		string(relatedChangesJSON),
	}, nil
}

func buildChangeQuery(query *Query) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	sqlQuery := selectEventsSQL + " WHERE 1=1"
	if query.Provider != "" {
		conditions = append(conditions, "provider = ?")
		args = append(args, query.Provider)
	}
	if !query.StartTime.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, query.StartTime)
	}
	if !query.EndTime.IsZero() {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, query.EndTime)
	}
	if len(query.ChangeTypes) > 0 {
		conditions = appendInCondition(conditions, "change_type", query.ChangeTypes, &args)
	}
	if len(query.Severities) > 0 {
		conditions = appendInCondition(conditions, "severity", query.Severities, &args)
	}
	if query.ResourceFilter != nil {
		conditions = appendInCondition(conditions, "resource_id", query.ResourceFilter.ResourceIDs, &args)
		conditions = appendInCondition(conditions, "resource_type", query.ResourceFilter.ResourceTypes, &args)
		conditions = appendInCondition(conditions, "service", query.ResourceFilter.Services, &args)
		conditions = appendInCondition(conditions, "project", query.ResourceFilter.Projects, &args)
		conditions = appendInCondition(conditions, "region", query.ResourceFilter.Regions, &args)
	}
	if len(conditions) > 0 {
		sqlQuery += " AND " + strings.Join(conditions, " AND ")
	}

	sqlQuery += " ORDER BY " + safeSortBy(query.SortBy)
	if sortOrder := safeSortOrder(query.SortOrder); sortOrder != "" {
		sqlQuery += " " + sortOrder
	} else {
		sqlQuery += " DESC"
	}
	if query.Limit > 0 {
		sqlQuery += " LIMIT ?"
		args = append(args, query.Limit)
		if query.Offset > 0 {
			sqlQuery += " OFFSET ?"
			args = append(args, query.Offset)
		}
	}
	return sqlQuery, args
}

func appendInCondition(conditions []string, column string, values []string, args *[]interface{}) []string {
	if len(values) == 0 {
		return conditions
	}
	placeholders := make([]string, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		*args = append(*args, value)
	}
	return append(conditions, fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ",")))
}

func safeSortBy(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "detected_at", "severity", "change_type", "service", "resource_type", "resource_name":
		return strings.ToLower(strings.TrimSpace(sortBy))
	default:
		return "timestamp"
	}
}

func safeSortOrder(sortOrder string) string {
	switch strings.ToUpper(strings.TrimSpace(sortOrder)) {
	case "ASC", "DESC":
		return strings.ToUpper(strings.TrimSpace(sortOrder))
	default:
		return ""
	}
}

func scanChangeEvent(scanner interface {
	Scan(dest ...interface{}) error
}) (*Event, error) {
	var change Event
	var previousStateJSON, currentStateJSON, changedFieldsJSON, changeMetadataJSON interface{}
	var impactAssessmentJSON, complianceImpactJSON, relatedChangesJSON interface{}

	err := scanner.Scan(
		&change.ID,
		&change.Provider,
		&change.ResourceID,
		&change.ResourceName,
		&change.ResourceType,
		&change.Service,
		&change.Project,
		&change.Region,
		&change.ChangeType,
		&change.Severity,
		&change.Timestamp,
		&change.DetectedAt,
		&previousStateJSON,
		&currentStateJSON,
		&changedFieldsJSON,
		&changeMetadataJSON,
		&impactAssessmentJSON,
		&complianceImpactJSON,
		&relatedChangesJSON,
	)
	if err != nil {
		return nil, err
	}

	decodeJSONValue(previousStateJSON, &change.PreviousState)
	decodeJSONValue(currentStateJSON, &change.CurrentState)
	decodeJSONValue(changedFieldsJSON, &change.ChangedFields)
	decodeJSONValue(changeMetadataJSON, &change.ChangeMetadata)
	decodeJSONValue(impactAssessmentJSON, &change.ImpactAssessment)
	decodeJSONValue(complianceImpactJSON, &change.ComplianceImpact)
	decodeJSONValue(relatedChangesJSON, &change.RelatedChanges)
	return &change, nil
}

func scanBaseline(scanner interface {
	Scan(dest ...interface{}) error
}, notFoundMessage string) (*Baseline, error) {
	var baseline Baseline
	var resourcesJSON, policiesJSON, tagsJSON interface{}
	err := scanner.Scan(
		&baseline.ID,
		&baseline.Name,
		&baseline.Description,
		&baseline.Provider,
		&baseline.CreatedAt,
		&baseline.UpdatedAt,
		&resourcesJSON,
		&policiesJSON,
		&tagsJSON,
		&baseline.Version,
		&baseline.Active,
	)
	if err != nil {
		if err == sql.ErrNoRows && notFoundMessage != "" {
			return nil, errors.New(notFoundMessage)
		}
		return nil, fmt.Errorf("failed to scan baseline: %w", err)
	}

	decodeJSONValue(resourcesJSON, &baseline.Resources)
	decodeJSONValue(policiesJSON, &baseline.Policies)
	decodeJSONValue(tagsJSON, &baseline.Tags)
	return &baseline, nil
}

func decodeJSONValue(raw interface{}, target interface{}) {
	if raw == nil {
		return
	}
	switch value := raw.(type) {
	case string:
		decodeJSON(value, target)
	case []byte:
		decodeJSON(string(value), target)
	default:
		data, err := json.Marshal(value)
		if err != nil {
			log.Printf("Failed to marshal change tracking JSON value: %v", err)
			return
		}
		decodeJSON(string(data), target)
	}
}

func decodeJSON(raw string, target interface{}) {
	if raw == "" || raw == "null" {
		return
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		log.Printf("Failed to unmarshal change tracking JSON: %v", err)
	}
}

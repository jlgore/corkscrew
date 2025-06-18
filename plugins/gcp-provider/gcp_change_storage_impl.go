package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

// GCPChangeStorage implements change tracking storage for GCP
type GCPChangeStorage interface {
	StoreChange(change *ChangeEvent) error
	StoreChanges(changes []*ChangeEvent) error
	QueryChanges(query *ChangeQuery) ([]*ChangeEvent, error)
	GetChangeHistory(resourceID string) ([]*ChangeEvent, error)
	GetChange(changeID string) (*ChangeEvent, error)
	DeleteChanges(olderThan time.Time) error
	StoreBaseline(baseline *DriftBaseline) error
	GetBaseline(baselineID string) (*DriftBaseline, error)
	ListBaselines(projectID string) ([]*DriftBaseline, error)
	UpdateBaseline(baseline *DriftBaseline) error
	DeleteBaseline(baselineID string) error
	Close() error
}

// DuckDBGCPChangeStorage implements GCPChangeStorage using DuckDB
type DuckDBGCPChangeStorage struct {
	db *sql.DB
}

// NewGCPChangeStorage creates a new GCP change storage instance
func NewGCPChangeStorage(dbPath string) (GCPChangeStorage, error) {
	if dbPath == "" {
		dbPath = "gcp_change_tracking.db"
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &DuckDBGCPChangeStorage{db: db}

	// Initialize database schema
	if err := storage.initializeSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

// initializeSchema creates the necessary tables for GCP change tracking
func (gcs *DuckDBGCPChangeStorage) initializeSchema() error {
	schemas := []string{
		// Main change events table
		`CREATE TABLE IF NOT EXISTS gcp_change_events (
			id VARCHAR PRIMARY KEY,
			provider VARCHAR NOT NULL DEFAULT 'gcp',
			project_id VARCHAR NOT NULL,
			resource_id VARCHAR NOT NULL,
			resource_name VARCHAR,
			resource_type VARCHAR NOT NULL,
			service VARCHAR NOT NULL,
			region VARCHAR,
			zone VARCHAR,
			change_type VARCHAR NOT NULL,
			severity VARCHAR NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			detected_at TIMESTAMP NOT NULL,
			previous_state JSON,
			current_state JSON,
			changed_fields JSON,
			change_metadata JSON,
			asset_type VARCHAR,
			asset_state VARCHAR,
			change_source VARCHAR,
			cloud_logging_data JSON,
			impact_assessment JSON,
			compliance_impact JSON,
			related_changes JSON
		);`,

		// Asset history table for Cloud Asset Inventory data
		`CREATE TABLE IF NOT EXISTS gcp_asset_history (
			id VARCHAR PRIMARY KEY,
			project_id VARCHAR NOT NULL,
			asset_name VARCHAR NOT NULL,
			asset_type VARCHAR NOT NULL,
			operation_type VARCHAR NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			window_start_time TIMESTAMP,
			window_end_time TIMESTAMP,
			deleted BOOLEAN DEFAULT FALSE,
			prior_asset_state VARCHAR,
			current_asset_state VARCHAR,
			resource_data JSON,
			iam_policy_data JSON,
			org_policy_data JSON,
			access_policy_data JSON
		);`,

		// Drift baselines table
		`CREATE TABLE IF NOT EXISTS gcp_drift_baselines (
			id VARCHAR PRIMARY KEY,
			name VARCHAR NOT NULL,
			description TEXT,
			project_id VARCHAR NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			resources JSON,
			policies JSON,
			tags JSON,
			version VARCHAR,
			active BOOLEAN DEFAULT true,
			baseline_metadata JSON
		);`,

		// Compliance violations table
		`CREATE TABLE IF NOT EXISTS gcp_compliance_violations (
			id VARCHAR PRIMARY KEY,
			project_id VARCHAR NOT NULL,
			resource_id VARCHAR NOT NULL,
			framework VARCHAR NOT NULL,
			rule_id VARCHAR NOT NULL,
			severity VARCHAR NOT NULL,
			violation_type VARCHAR NOT NULL,
			description TEXT,
			detected_at TIMESTAMP NOT NULL,
			resolved_at TIMESTAMP,
			remediation_status VARCHAR,
			remediation_data JSON,
			metadata JSON
		);`,

		// Change analytics aggregation table
		`CREATE TABLE IF NOT EXISTS gcp_change_analytics (
			id VARCHAR PRIMARY KEY,
			project_id VARCHAR NOT NULL,
			aggregation_period VARCHAR NOT NULL,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP NOT NULL,
			total_changes INTEGER,
			changes_by_type JSON,
			changes_by_service JSON,
			changes_by_severity JSON,
			security_changes INTEGER,
			compliance_violations INTEGER,
			drift_items INTEGER,
			created_at TIMESTAMP NOT NULL
		);`,

		// Indexes for performance
		`CREATE INDEX IF NOT EXISTS idx_gcp_change_events_project_timestamp 
		 ON gcp_change_events(project_id, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_gcp_change_events_resource 
		 ON gcp_change_events(resource_id, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_gcp_asset_history_asset 
		 ON gcp_asset_history(asset_name, timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_gcp_compliance_violations_project 
		 ON gcp_compliance_violations(project_id, detected_at);`,
	}

	for _, schema := range schemas {
		if _, err := gcs.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	log.Printf("GCP change tracking database schema initialized successfully")
	return nil
}

// StoreChange stores a single change event
func (gcs *DuckDBGCPChangeStorage) StoreChange(change *ChangeEvent) error {
	if change == nil {
		return fmt.Errorf("change event cannot be nil")
	}

	// Convert complex fields to JSON
	previousStateJSON, _ := json.Marshal(change.PreviousState)
	currentStateJSON, _ := json.Marshal(change.CurrentState)
	changedFieldsJSON, _ := json.Marshal(change.ChangedFields)
	changeMetadataJSON, _ := json.Marshal(change.ChangeMetadata)
	impactAssessmentJSON, _ := json.Marshal(change.ImpactAssessment)
	complianceImpactJSON, _ := json.Marshal(change.ComplianceImpact)
	relatedChangesJSON, _ := json.Marshal(change.RelatedChanges)

	query := `INSERT INTO gcp_change_events (
		id, provider, project_id, resource_id, resource_name, resource_type, service, 
		region, change_type, severity, timestamp, detected_at,
		previous_state, current_state, changed_fields, change_metadata,
		impact_assessment, compliance_impact, related_changes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := gcs.db.Exec(query,
		change.ID,
		change.Provider,
		change.Project,
		change.ResourceID,
		change.ResourceName,
		change.ResourceType,
		change.Service,
		change.Region,
		string(change.ChangeType),
		string(change.Severity),
		change.Timestamp,
		change.DetectedAt,
		string(previousStateJSON),
		string(currentStateJSON),
		string(changedFieldsJSON),
		string(changeMetadataJSON),
		string(impactAssessmentJSON),
		string(complianceImpactJSON),
		string(relatedChangesJSON),
	)

	if err != nil {
		return fmt.Errorf("failed to store change event: %w", err)
	}

	return nil
}

// StoreChanges stores multiple change events in a batch
func (gcs *DuckDBGCPChangeStorage) StoreChanges(changes []*ChangeEvent) error {
	if len(changes) == 0 {
		return nil
	}

	tx, err := gcs.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO gcp_change_events (
		id, provider, project_id, resource_id, resource_name, resource_type, service, 
		region, change_type, severity, timestamp, detected_at,
		previous_state, current_state, changed_fields, change_metadata,
		impact_assessment, compliance_impact, related_changes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, change := range changes {
		if change == nil {
			continue
		}

		// Convert complex fields to JSON
		previousStateJSON, _ := json.Marshal(change.PreviousState)
		currentStateJSON, _ := json.Marshal(change.CurrentState)
		changedFieldsJSON, _ := json.Marshal(change.ChangedFields)
		changeMetadataJSON, _ := json.Marshal(change.ChangeMetadata)
		impactAssessmentJSON, _ := json.Marshal(change.ImpactAssessment)
		complianceImpactJSON, _ := json.Marshal(change.ComplianceImpact)
		relatedChangesJSON, _ := json.Marshal(change.RelatedChanges)

		_, err = stmt.Exec(
			change.ID,
			change.Provider,
			change.Project,
			change.ResourceID,
			change.ResourceName,
			change.ResourceType,
			change.Service,
			change.Region,
			string(change.ChangeType),
			string(change.Severity),
			change.Timestamp,
			change.DetectedAt,
			string(previousStateJSON),
			string(currentStateJSON),
			string(changedFieldsJSON),
			string(changeMetadataJSON),
			string(impactAssessmentJSON),
			string(complianceImpactJSON),
			string(relatedChangesJSON),
		)

		if err != nil {
			return fmt.Errorf("failed to store change event %s: %w", change.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Stored %d GCP change events successfully", len(changes))
	return nil
}

// QueryChanges retrieves change events based on query parameters
func (gcs *DuckDBGCPChangeStorage) QueryChanges(query *ChangeQuery) ([]*ChangeEvent, error) {
	if query == nil {
		return nil, fmt.Errorf("query cannot be nil")
	}

	// Build SQL query
	sqlQuery, args := gcs.buildChangeQuery(query)

	rows, err := gcs.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var changes []*ChangeEvent
	for rows.Next() {
		change, err := gcs.scanChangeEvent(rows)
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

// GetChangeHistory retrieves all changes for a specific resource
func (gcs *DuckDBGCPChangeStorage) GetChangeHistory(resourceID string) ([]*ChangeEvent, error) {
	query := `SELECT * FROM gcp_change_events 
			  WHERE resource_id = ? 
			  ORDER BY timestamp DESC 
			  LIMIT 1000`

	rows, err := gcs.db.Query(query, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query change history: %w", err)
	}
	defer rows.Close()

	var changes []*ChangeEvent
	for rows.Next() {
		change, err := gcs.scanChangeEvent(rows)
		if err != nil {
			log.Printf("Failed to scan change event: %v", err)
			continue
		}
		changes = append(changes, change)
	}

	return changes, nil
}

// GetChange retrieves a specific change event by ID
func (gcs *DuckDBGCPChangeStorage) GetChange(changeID string) (*ChangeEvent, error) {
	query := `SELECT * FROM gcp_change_events WHERE id = ?`

	row := gcs.db.QueryRow(query, changeID)
	return gcs.scanChangeEvent(row)
}

// DeleteChanges removes change events older than the specified time
func (gcs *DuckDBGCPChangeStorage) DeleteChanges(olderThan time.Time) error {
	query := `DELETE FROM gcp_change_events WHERE timestamp < ?`

	result, err := gcs.db.Exec(query, olderThan)
	if err != nil {
		return fmt.Errorf("failed to delete old changes: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("Deleted %d GCP change events older than %v", rowsAffected, olderThan)

	return nil
}

// StoreBaseline stores a drift baseline
func (gcs *DuckDBGCPChangeStorage) StoreBaseline(baseline *DriftBaseline) error {
	if baseline == nil {
		return fmt.Errorf("baseline cannot be nil")
	}

	resourcesJSON, _ := json.Marshal(baseline.Resources)
	policiesJSON, _ := json.Marshal(baseline.Policies)
	tagsJSON, _ := json.Marshal(baseline.Tags)

	query := `INSERT INTO gcp_drift_baselines (
		id, name, description, project_id, created_at, updated_at,
		resources, policies, tags, version, active
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := gcs.db.Exec(query,
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

// GetBaseline retrieves a baseline by ID
func (gcs *DuckDBGCPChangeStorage) GetBaseline(baselineID string) (*DriftBaseline, error) {
	query := `SELECT * FROM gcp_drift_baselines WHERE id = ?`

	row := gcs.db.QueryRow(query, baselineID)
	
	var baseline DriftBaseline
	var resourcesJSON, policiesJSON, tagsJSON string

	err := row.Scan(
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
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("baseline not found: %s", baselineID)
		}
		return nil, fmt.Errorf("failed to scan baseline: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal([]byte(resourcesJSON), &baseline.Resources); err != nil {
		log.Printf("Failed to unmarshal baseline resources: %v", err)
	}

	if err := json.Unmarshal([]byte(policiesJSON), &baseline.Policies); err != nil {
		log.Printf("Failed to unmarshal baseline policies: %v", err)
	}

	if err := json.Unmarshal([]byte(tagsJSON), &baseline.Tags); err != nil {
		log.Printf("Failed to unmarshal baseline tags: %v", err)
	}

	return &baseline, nil
}

// ListBaselines retrieves all baselines for a project
func (gcs *DuckDBGCPChangeStorage) ListBaselines(projectID string) ([]*DriftBaseline, error) {
	query := `SELECT * FROM gcp_drift_baselines WHERE project_id = ? ORDER BY created_at DESC`

	rows, err := gcs.db.Query(query, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to query baselines: %w", err)
	}
	defer rows.Close()

	var baselines []*DriftBaseline
	for rows.Next() {
		var baseline DriftBaseline
		var resourcesJSON, policiesJSON, tagsJSON string

		err := rows.Scan(
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
			log.Printf("Failed to scan baseline: %v", err)
			continue
		}

		// Unmarshal JSON fields
		json.Unmarshal([]byte(resourcesJSON), &baseline.Resources)
		json.Unmarshal([]byte(policiesJSON), &baseline.Policies)
		json.Unmarshal([]byte(tagsJSON), &baseline.Tags)

		baselines = append(baselines, &baseline)
	}

	return baselines, nil
}

// UpdateBaseline updates an existing baseline
func (gcs *DuckDBGCPChangeStorage) UpdateBaseline(baseline *DriftBaseline) error {
	if baseline == nil {
		return fmt.Errorf("baseline cannot be nil")
	}

	baseline.UpdatedAt = time.Now()

	resourcesJSON, _ := json.Marshal(baseline.Resources)
	policiesJSON, _ := json.Marshal(baseline.Policies)
	tagsJSON, _ := json.Marshal(baseline.Tags)

	query := `UPDATE gcp_drift_baselines SET 
		name = ?, description = ?, updated_at = ?,
		resources = ?, policies = ?, tags = ?, 
		version = ?, active = ?
		WHERE id = ?`

	_, err := gcs.db.Exec(query,
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

// DeleteBaseline removes a baseline
func (gcs *DuckDBGCPChangeStorage) DeleteBaseline(baselineID string) error {
	query := `DELETE FROM gcp_drift_baselines WHERE id = ?`

	_, err := gcs.db.Exec(query, baselineID)
	if err != nil {
		return fmt.Errorf("failed to delete baseline: %w", err)
	}

	return nil
}

// Close closes the database connection
func (gcs *DuckDBGCPChangeStorage) Close() error {
	if gcs.db != nil {
		return gcs.db.Close()
	}
	return nil
}

// Helper methods

func (gcs *DuckDBGCPChangeStorage) buildChangeQuery(query *ChangeQuery) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	sql := `SELECT * FROM gcp_change_events WHERE 1=1`

	// Provider filter
	if query.Provider != "" {
		conditions = append(conditions, "provider = ?")
		args = append(args, query.Provider)
	}

	// Time range filter
	if !query.StartTime.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, query.StartTime)
	}

	if !query.EndTime.IsZero() {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, query.EndTime)
	}

	// Change type filter
	if len(query.ChangeTypes) > 0 {
		placeholders := make([]string, len(query.ChangeTypes))
		for i, changeType := range query.ChangeTypes {
			placeholders[i] = "?"
			args = append(args, string(changeType))
		}
		conditions = append(conditions, fmt.Sprintf("change_type IN (%s)", strings.Join(placeholders, ",")))
	}

	// Resource filter
	if query.ResourceFilter != nil {
		rf := query.ResourceFilter

		if len(rf.ResourceIDs) > 0 {
			placeholders := make([]string, len(rf.ResourceIDs))
			for i, id := range rf.ResourceIDs {
				placeholders[i] = "?"
				args = append(args, id)
			}
			conditions = append(conditions, fmt.Sprintf("resource_id IN (%s)", strings.Join(placeholders, ",")))
		}

		if len(rf.ResourceTypes) > 0 {
			placeholders := make([]string, len(rf.ResourceTypes))
			for i, resourceType := range rf.ResourceTypes {
				placeholders[i] = "?"
				args = append(args, resourceType)
			}
			conditions = append(conditions, fmt.Sprintf("resource_type IN (%s)", strings.Join(placeholders, ",")))
		}
	}

	// Add conditions to SQL
	if len(conditions) > 0 {
		sql += " AND " + strings.Join(conditions, " AND ")
	}

	// Add ordering
	if query.SortBy != "" {
		sql += fmt.Sprintf(" ORDER BY %s", query.SortBy)
		if query.SortOrder != "" {
			sql += fmt.Sprintf(" %s", strings.ToUpper(query.SortOrder))
		}
	} else {
		sql += " ORDER BY timestamp DESC"
	}

	// Add limit and offset
	if query.Limit > 0 {
		sql += " LIMIT ?"
		args = append(args, query.Limit)

		if query.Offset > 0 {
			sql += " OFFSET ?"
			args = append(args, query.Offset)
		}
	}

	return sql, args
}

func (gcs *DuckDBGCPChangeStorage) scanChangeEvent(scanner interface {
	Scan(dest ...interface{}) error
}) (*ChangeEvent, error) {
	var change ChangeEvent
	var previousStateJSON, currentStateJSON, changedFieldsJSON, changeMetadataJSON string
	var impactAssessmentJSON, complianceImpactJSON, relatedChangesJSON string
	var changeTypeStr, severityStr string

	err := scanner.Scan(
		&change.ID,
		&change.Provider,
		&change.Project,
		&change.ResourceID,
		&change.ResourceName,
		&change.ResourceType,
		&change.Service,
		&change.Region,
		&changeTypeStr,
		&severityStr,
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

	// Convert string enums back to types
	change.ChangeType = ChangeType(changeTypeStr)
	change.Severity = ChangeSeverity(severityStr)

	// Unmarshal JSON fields
	if previousStateJSON != "" {
		json.Unmarshal([]byte(previousStateJSON), &change.PreviousState)
	}

	if currentStateJSON != "" {
		json.Unmarshal([]byte(currentStateJSON), &change.CurrentState)
	}

	if changedFieldsJSON != "" {
		json.Unmarshal([]byte(changedFieldsJSON), &change.ChangedFields)
	}

	if changeMetadataJSON != "" {
		json.Unmarshal([]byte(changeMetadataJSON), &change.ChangeMetadata)
	}

	if impactAssessmentJSON != "" {
		json.Unmarshal([]byte(impactAssessmentJSON), &change.ImpactAssessment)
	}

	if complianceImpactJSON != "" {
		json.Unmarshal([]byte(complianceImpactJSON), &change.ComplianceImpact)
	}

	if relatedChangesJSON != "" {
		json.Unmarshal([]byte(relatedChangesJSON), &change.RelatedChanges)
	}

	return &change, nil
}
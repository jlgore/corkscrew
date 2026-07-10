package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jlgore/corkscrew/pkg/models"
)

// GraphStore owns cross-cloud graph persistence over an existing database
// connection. It is shared by GraphLoader and UnifiedDatabaseConfig so callers
// do not get different behavior depending on which database entry point they
// were handed.
type GraphStore struct {
	db *sql.DB
}

func NewGraphStore(db *sql.DB) *GraphStore {
	return &GraphStore{db: db}
}

func graphJSONScanString(value interface{}) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		return v, v != ""
	case []byte:
		return string(v), len(v) > 0
	case sql.NullString:
		return v.String, v.Valid && v.String != ""
	case map[string]interface{}, []interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(data), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}

// StoreResources stores cross-cloud resources.
func (gs *GraphStore) StoreResources(resources []*models.Resource) error {
	if len(resources) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_resources (
			id VARCHAR PRIMARY KEY,
			name VARCHAR,
			type VARCHAR,
			service VARCHAR,
			provider VARCHAR,
			region VARCHAR,
			arn VARCHAR,
			status VARCHAR,
			created_at TIMESTAMP,
			modified_at TIMESTAMP,
			scanned_at TIMESTAMP,
			tags JSON,
			attributes JSON,
			metadata JSON,
			raw_data JSON,
			cross_cloud_id VARCHAR
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_resources table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_resources 
		(id, name, type, service, provider, region, arn, status, created_at, modified_at, scanned_at, tags, attributes, metadata, raw_data, cross_cloud_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, resource := range resources {
		tags, _ := json.Marshal(resource.Tags)
		attributes, _ := json.Marshal(resource.Attributes)
		metadata, _ := json.Marshal(resource.Metadata)
		rawData, _ := json.Marshal(resource.RawData)

		_, err := stmt.Exec(
			resource.ID, resource.Name, resource.Type, resource.Service,
			resource.Provider, resource.Region, resource.ARN, resource.Status,
			resource.CreatedAt, resource.ModifiedAt, resource.ScannedAt,
			string(tags), string(attributes), string(metadata), string(rawData),
			resource.CrossCloudID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert resource %s: %w", resource.ID, err)
		}
	}

	return nil
}

// StoreIPAddresses stores IP addresses.
func (gs *GraphStore) StoreIPAddresses(addresses []*models.IPAddress) error {
	if len(addresses) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_ip_addresses (
			address VARCHAR,
			type VARCHAR,
			version VARCHAR,
			provider VARCHAR,
			region VARCHAR,
			resource_id VARCHAR,
			scope VARCHAR,
			PRIMARY KEY (address, provider, resource_id)
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_ip_addresses table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_ip_addresses 
		(address, type, version, provider, region, resource_id, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, addr := range addresses {
		_, err := stmt.Exec(addr.Address, addr.Type, addr.Version, addr.Provider, addr.Region, addr.ResourceID, addr.Scope)
		if err != nil {
			return fmt.Errorf("failed to insert IP address %s: %w", addr.Address, err)
		}
	}

	return nil
}

// StoreDNSRecords stores DNS records.
func (gs *GraphStore) StoreDNSRecords(records []*models.DNSRecord) error {
	if len(records) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_dns_records (
			name VARCHAR,
			type VARCHAR,
			values JSON,
			ttl INTEGER,
			provider VARCHAR,
			zone VARCHAR,
			resource_id VARCHAR,
			PRIMARY KEY (name, type, provider, resource_id)
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_dns_records table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_dns_records 
		(name, type, values, ttl, provider, zone, resource_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, record := range records {
		values, _ := json.Marshal(record.Values)
		_, err := stmt.Exec(record.Name, record.Type, string(values), record.TTL, record.Provider, record.Zone, record.ResourceID)
		if err != nil {
			return fmt.Errorf("failed to insert DNS record %s: %w", record.Name, err)
		}
	}

	return nil
}

// StoreCorrelations stores cross-cloud correlations.
func (gs *GraphStore) StoreCorrelations(correlations interface{}) error {
	switch corrs := correlations.(type) {
	case []*models.ResourceCorrelation:
		return gs.storeResourceCorrelations(corrs)
	default:
		return gs.storeGenericCorrelations(correlations)
	}
}

func (gs *GraphStore) storeResourceCorrelations(correlations []*models.ResourceCorrelation) error {
	if len(correlations) == 0 {
		return nil
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_correlations (
			id VARCHAR PRIMARY KEY,
			source_id VARCHAR,
			target_id VARCHAR,
			type VARCHAR,
			relation_type VARCHAR,
			strength DOUBLE,
			confidence DOUBLE,
			description VARCHAR,
			metadata JSON,
			discovered_at TIMESTAMP
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_correlations table: %w", err)
	}

	insertSQL := `
		INSERT OR REPLACE INTO crosscloud_correlations 
		(id, source_id, target_id, type, relation_type, strength, confidence, description, metadata, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := gs.db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, corr := range correlations {
		metadata, _ := json.Marshal(corr.Metadata)
		_, err := stmt.Exec(
			corr.ID, corr.SourceID, corr.TargetID, corr.Type, corr.RelationType,
			corr.Strength, corr.Confidence, corr.Description, string(metadata), corr.DiscoveredAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert correlation %s: %w", corr.ID, err)
		}
	}

	return nil
}

func (gs *GraphStore) storeGenericCorrelations(correlations interface{}) error {
	correlationsJSON, err := json.Marshal(correlations)
	if err != nil {
		return fmt.Errorf("failed to marshal correlations: %w", err)
	}

	createTableSQL := `
		CREATE TABLE IF NOT EXISTS crosscloud_generic_correlations (
			id VARCHAR PRIMARY KEY,
			correlation_data JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`
	if _, err := gs.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create crosscloud_generic_correlations table: %w", err)
	}

	id := uuid.New().String()
	insertSQL := `INSERT INTO crosscloud_generic_correlations (id, correlation_data) VALUES (?, ?)`
	_, err = gs.db.Exec(insertSQL, id, string(correlationsJSON))
	if err != nil {
		return fmt.Errorf("failed to store correlations: %w", err)
	}

	return nil
}

// GetResourcesByProvider retrieves resources for a specific provider.
func (gs *GraphStore) GetResourcesByProvider(provider string) ([]*models.Resource, error) {
	query := `
		SELECT id, name, type, service, provider, region, arn, status, 
		       created_at, modified_at, scanned_at, tags, attributes, metadata, raw_data, cross_cloud_id
		FROM crosscloud_resources 
		WHERE provider = ?`

	rows, err := gs.db.Query(query, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query resources: %w", err)
	}
	defer rows.Close()

	var resources []*models.Resource
	for rows.Next() {
		resource := &models.Resource{}
		var tags, attributes, metadata, rawData interface{}

		err := rows.Scan(
			&resource.ID, &resource.Name, &resource.Type, &resource.Service,
			&resource.Provider, &resource.Region, &resource.ARN, &resource.Status,
			&resource.CreatedAt, &resource.ModifiedAt, &resource.ScannedAt,
			&tags, &attributes, &metadata, &rawData, &resource.CrossCloudID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}

		if jsonValue, ok := graphJSONScanString(tags); ok {
			json.Unmarshal([]byte(jsonValue), &resource.Tags)
		}
		if jsonValue, ok := graphJSONScanString(attributes); ok {
			json.Unmarshal([]byte(jsonValue), &resource.Attributes)
		}
		if jsonValue, ok := graphJSONScanString(metadata); ok {
			json.Unmarshal([]byte(jsonValue), &resource.Metadata)
		}
		if jsonValue, ok := graphJSONScanString(rawData); ok {
			json.Unmarshal([]byte(jsonValue), &resource.RawData)
		}

		resources = append(resources, resource)
	}

	return resources, rows.Err()
}

// GetIPAddressesByProvider retrieves IP addresses for a specific provider.
func (gs *GraphStore) GetIPAddressesByProvider(provider string) ([]*models.IPAddress, error) {
	query := `
		SELECT address, type, version, provider, region, resource_id, scope
		FROM crosscloud_ip_addresses 
		WHERE provider = ?`

	rows, err := gs.db.Query(query, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query IP addresses: %w", err)
	}
	defer rows.Close()

	var addresses []*models.IPAddress
	for rows.Next() {
		addr := &models.IPAddress{}
		err := rows.Scan(&addr.Address, &addr.Type, &addr.Version, &addr.Provider, &addr.Region, &addr.ResourceID, &addr.Scope)
		if err != nil {
			return nil, fmt.Errorf("failed to scan IP address: %w", err)
		}
		addresses = append(addresses, addr)
	}

	return addresses, rows.Err()
}

// GetDNSRecordsByProvider retrieves DNS records for a specific provider.
func (gs *GraphStore) GetDNSRecordsByProvider(provider string) ([]*models.DNSRecord, error) {
	query := `
		SELECT name, type, values, ttl, provider, zone, resource_id
		FROM crosscloud_dns_records 
		WHERE provider = ?`

	rows, err := gs.db.Query(query, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query DNS records: %w", err)
	}
	defer rows.Close()

	var records []*models.DNSRecord
	for rows.Next() {
		record := &models.DNSRecord{}
		var valuesJSON interface{}
		err := rows.Scan(&record.Name, &record.Type, &valuesJSON, &record.TTL, &record.Provider, &record.Zone, &record.ResourceID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan DNS record: %w", err)
		}

		if jsonValue, ok := graphJSONScanString(valuesJSON); ok {
			json.Unmarshal([]byte(jsonValue), &record.Values)
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

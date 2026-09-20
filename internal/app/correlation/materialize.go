package correlation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	contract "github.com/jlgore/corkscrew/pkg/correlation"
)

type Request struct {
	Target string
	Kinds  []contract.Kind
}

type Result struct {
	RunID         string          `json:"run_id"`
	Status        string          `json:"status"`
	Kinds         []contract.Kind `json:"kinds"`
	EvidenceCount int             `json:"evidence_count"`
	RejectedCount int             `json:"rejected_count"`
	RowCount      int             `json:"row_count"`
	Coverage      map[string]int  `json:"coverage"`
	StartedAt     time.Time       `json:"started_at"`
	EndedAt       time.Time       `json:"ended_at"`
	Error         string          `json:"error,omitempty"`
}

type sourceResource struct {
	Provider, ID, Name, Type, Service, Region, AccountID, Tags, Attributes string
}

type candidate struct {
	Kind   contract.Kind
	Table  string
	RowID  string
	Values map[string]any
}

var kindTables = map[contract.Kind][]string{
	contract.KindIP:           {"cross_cloud_ip_addresses"},
	contract.KindDNS:          {"cross_cloud_dns_records"},
	contract.KindNetwork:      {"cross_cloud_network_topology"},
	contract.KindLoadBalancer: {"cross_cloud_loadbalancer_topology"},
	contract.KindConnectivity: {"cross_cloud_vpn_connections", "cross_cloud_network_peering", "cross_cloud_direct_connections"},
	contract.KindSecurity:     {"cross_cloud_security_correlations"},
	contract.KindDomain:       {"certificate_correlations"},
	contract.KindIdentity:     {"identity_federation_relationships", "security_role_relationships"},
	contract.KindPolicy:       {"policy_similarity_analysis"},
	contract.KindSecret:       {"shared_secrets_correlation"},
}

func Refresh(ctx context.Context, request Request) (Result, error) {
	result := Result{RunID: uuid.NewString(), Status: "running", StartedAt: time.Now().UTC(), Coverage: map[string]int{}}
	result.Kinds = normalizeKinds(request.Kinds)
	session, err := dataaccess.OpenSession(ctx, request.Target)
	if err != nil {
		return result, err
	}
	defer session.Close()
	connection, err := session.Connection(ctx)
	if err != nil {
		return result, err
	}
	defer connection.Close()

	resources, err := loadEvidenceResources(ctx, connection)
	if err != nil {
		return result, err
	}
	selected := map[contract.Kind]bool{}
	for _, kind := range result.Kinds {
		selected[kind] = true
	}
	var candidates []candidate
	for _, resource := range resources {
		envelope, parseErr := contract.ParseAttributes(resource.Attributes)
		if parseErr != nil {
			result.RejectedCount++
			return recordFailure(ctx, connection, result, parseErr)
		}
		for _, evidence := range envelope.Evidence {
			if !selected[evidence.Kind] {
				continue
			}
			result.EvidenceCount++
			result.Coverage[string(evidence.Kind)+":"+resource.Provider]++
			item, candidateErr := buildCandidate(resource, evidence)
			if candidateErr != nil {
				result.RejectedCount++
				return recordFailure(ctx, connection, result, candidateErr)
			}
			candidates = append(candidates, item)
		}
	}

	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	for _, kind := range result.Kinds {
		for _, table := range kindTables[kind] {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id IN (
SELECT row_id FROM correlation_materialized_rows WHERE kind = ? AND table_name = ?)`, table), string(kind), table); err != nil {
				_ = tx.Rollback()
				return recordFailure(ctx, connection, result, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM correlation_materialized_rows WHERE kind = ?`, string(kind)); err != nil {
			_ = tx.Rollback()
			return recordFailure(ctx, connection, result, err)
		}
	}
	for _, item := range candidates {
		if err := insertCandidate(ctx, tx, item); err != nil {
			result.RejectedCount++
			_ = tx.Rollback()
			return recordFailure(ctx, connection, result, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO correlation_materialized_rows(run_id, kind, table_name, row_id) VALUES (?, ?, ?, ?)`,
			result.RunID, string(item.Kind), item.Table, item.RowID); err != nil {
			_ = tx.Rollback()
			return recordFailure(ctx, connection, result, err)
		}
		result.RowCount++
	}
	result.Status = "completed"
	result.EndedAt = time.Now().UTC()
	if err := insertRun(ctx, tx, result); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func Latest(ctx context.Context, target string) (Result, error) {
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		return Result{}, err
	}
	defer session.Close()
	var result Result
	var kinds, coverage, errorMessage sql.NullString
	err = session.QueryRowContext(ctx, `SELECT id, status, CAST(kinds AS VARCHAR), evidence_count,
rejected_count, row_count, CAST(coverage AS VARCHAR), started_at, COALESCE(ended_at, started_at), error_message
FROM correlation_materialization_runs ORDER BY started_at DESC LIMIT 1`).Scan(&result.RunID, &result.Status, &kinds,
		&result.EvidenceCount, &result.RejectedCount, &result.RowCount, &coverage, &result.StartedAt, &result.EndedAt, &errorMessage)
	if err != nil {
		return Result{}, err
	}
	_ = json.Unmarshal([]byte(kinds.String), &result.Kinds)
	_ = json.Unmarshal([]byte(coverage.String), &result.Coverage)
	result.Error = errorMessage.String
	return result, nil
}

func normalizeKinds(kinds []contract.Kind) []contract.Kind {
	if len(kinds) == 0 {
		return contract.AllKinds()
	}
	seen := map[contract.Kind]bool{}
	var result []contract.Kind
	for _, kind := range kinds {
		if _, ok := kindTables[kind]; ok && !seen[kind] {
			seen[kind] = true
			result = append(result, kind)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func loadEvidenceResources(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]sourceResource, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT provider, resource_id, COALESCE(name, ''), type, COALESCE(service, ''),
       COALESCE(location, ''), COALESCE(account_id, ''), COALESCE(CAST(tags AS VARCHAR), ''),
       COALESCE(CAST(attributes AS VARCHAR), '')
FROM resource_observations
QUALIFY row_number() OVER (PARTITION BY provider, resource_id ORDER BY observed_at DESC, scan_id DESC) = 1
ORDER BY provider, resource_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []sourceResource
	for rows.Next() {
		var resource sourceResource
		if err := rows.Scan(&resource.Provider, &resource.ID, &resource.Name, &resource.Type, &resource.Service,
			&resource.Region, &resource.AccountID, &resource.Tags, &resource.Attributes); err != nil {
			return nil, err
		}
		result = append(result, resource)
	}
	return result, rows.Err()
}

func buildCandidate(resource sourceResource, evidence contract.Evidence) (candidate, error) {
	table, err := evidenceTable(evidence)
	if err != nil {
		return candidate{}, err
	}
	sum := sha256.Sum256([]byte(resource.Provider + "\x00" + resource.ID + "\x00" + string(evidence.Kind) + "\x00" + evidence.ID))
	rowID := "cs:v1:" + hex.EncodeToString(sum[:])
	values := map[string]any{}
	for key, value := range evidence.Values {
		values[key] = value
	}
	values["id"] = rowID
	defaults := map[string]any{
		"provider": resource.Provider, "cloud_provider": resource.Provider,
		"resource_id": resource.ID, "resource_type": resource.Type, "resource_name": resource.Name,
		"region": resource.Region, "account_id": resource.AccountID, "service_name": resource.Service,
		"tags": json.RawMessage(resource.Tags), "correlation_confidence": evidence.Confidence,
		"confidence_score": evidence.Confidence, "correlation_method": evidence.Method,
	}
	for key, value := range defaults {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	return candidate{Kind: evidence.Kind, Table: table, RowID: rowID, Values: values}, nil
}

func evidenceTable(evidence contract.Evidence) (string, error) {
	switch evidence.Kind {
	case contract.KindConnectivity:
		switch evidence.Subtype {
		case "vpn":
			return "cross_cloud_vpn_connections", nil
		case "peering":
			return "cross_cloud_network_peering", nil
		case "direct":
			return "cross_cloud_direct_connections", nil
		}
	case contract.KindIdentity:
		if evidence.Subtype == "role" {
			return "security_role_relationships", nil
		}
		if evidence.Subtype == "federation" {
			return "identity_federation_relationships", nil
		}
	default:
		if tables := kindTables[evidence.Kind]; len(tables) == 1 {
			return tables[0], nil
		}
	}
	return "", fmt.Errorf("evidence %s/%s requires a supported subtype", evidence.Kind, evidence.Subtype)
}

type tableColumn struct {
	Name, Type string
	NotNull    bool
	Default    sql.NullString
}

func insertCandidate(ctx context.Context, tx *sql.Tx, item candidate) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info('%s')", item.Table))
	if err != nil {
		return err
	}
	var columns []tableColumn
	for rows.Next() {
		var cid int
		var column tableColumn
		var primary bool
		if err := rows.Scan(&cid, &column.Name, &column.Type, &column.NotNull, &column.Default, &primary); err != nil {
			rows.Close()
			return err
		}
		columns = append(columns, column)
	}
	rows.Close()
	var names, expressions []string
	var args []any
	for _, column := range columns {
		value, ok := item.Values[column.Name]
		if !ok || value == nil || value == "" {
			if column.NotNull && !column.Default.Valid {
				return fmt.Errorf("%s evidence missing required field %s", item.Kind, column.Name)
			}
			continue
		}
		names = append(names, column.Name)
		expressions = append(expressions, "?")
		if strings.EqualFold(column.Type, "JSON") {
			expressions[len(expressions)-1] = "try_cast(? AS JSON)"
			switch typed := value.(type) {
			case string:
				args = append(args, typed)
			case json.RawMessage:
				args = append(args, string(typed))
			default:
				encoded, _ := json.Marshal(value)
				args = append(args, string(encoded))
			}
		} else {
			args = append(args, value)
		}
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", item.Table, strings.Join(names, ","), strings.Join(expressions, ","))
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert %s evidence: %w", item.Kind, err)
	}
	return nil
}

func insertRun(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, result Result) error {
	kinds, _ := json.Marshal(result.Kinds)
	coverage, _ := json.Marshal(result.Coverage)
	_, err := executor.ExecContext(ctx, `INSERT INTO correlation_materialization_runs
(id, started_at, ended_at, status, kinds, evidence_count, rejected_count, row_count, coverage, error_message)
VALUES (?, ?, ?, ?, try_cast(? AS JSON), ?, ?, ?, try_cast(? AS JSON), ?)`, result.RunID, result.StartedAt,
		result.EndedAt, result.Status, string(kinds), result.EvidenceCount, result.RejectedCount, result.RowCount, string(coverage), nullable(result.Error))
	return err
}

func recordFailure(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, result Result, cause error) (Result, error) {
	result.Status = "failed"
	result.EndedAt = time.Now().UTC()
	result.Error = cause.Error()
	_ = insertRun(ctx, executor, result)
	return result, cause
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

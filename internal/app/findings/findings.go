package findings

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/pkg/query/compliance"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type EvaluateRequest struct {
	Target, ScanID, PackName, ControlID string
	Tags                                []string
	Parameters                          map[string]any
}

type Evaluation struct {
	ID       string    `json:"id"`
	ScanID   string    `json:"scan_id"`
	Status   string    `json:"status"`
	Error    string    `json:"error,omitempty"`
	Opened   int       `json:"opened"`
	Recurred int       `json:"recurred"`
	Resolved int       `json:"resolved"`
	Findings []Finding `json:"findings,omitempty"`
}

type Finding struct {
	Fingerprint     string     `json:"fingerprint"`
	PackRef         string     `json:"pack_ref"`
	ControlID       string     `json:"control_id"`
	Provider        string     `json:"provider"`
	ResourceID      string     `json:"resource_id"`
	ScopeKey        string     `json:"scope_key"`
	Title           string     `json:"title"`
	Severity        string     `json:"severity"`
	Details         string     `json:"details,omitempty"`
	Remediation     string     `json:"remediation,omitempty"`
	Status          string     `json:"status"`
	FirstSeenScan   string     `json:"first_seen_scan"`
	LastSeenScan    string     `json:"last_seen_scan"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	OccurrenceCount int        `json:"occurrence_count"`
}

type ListFilter struct{ Status, Provider, ControlID, MinSeverity, ScanID, PackRef string }

func Evaluate(ctx context.Context, request EvaluateRequest) (Evaluation, error) {
	mainSession, err := dataaccess.OpenSession(ctx, request.Target)
	if err != nil {
		return Evaluation{}, err
	}
	scan, err := mainSession.GetScan(ctx, request.ScanID)
	if err != nil {
		mainSession.Close()
		return Evaluation{}, fmt.Errorf("load scan: %w", err)
	}
	if !scan.SnapshotComplete || scan.Status != "completed" {
		mainSession.Close()
		return Evaluation{}, fmt.Errorf("scan %s is not a completed snapshot", scan.ID)
	}
	observations, err := mainSession.Observations(ctx, scan.ID)
	if err != nil {
		mainSession.Close()
		return Evaluation{}, err
	}
	relationships, err := mainSession.RelationshipObservations(ctx, scan.ID)
	_ = mainSession.Close()
	if err != nil {
		return Evaluation{}, err
	}

	temp, err := os.CreateTemp("", "corkscrew-findings-*.duckdb")
	if err != nil {
		return Evaluation{}, err
	}
	tempPath := temp.Name()
	_ = temp.Close()
	_ = os.Remove(tempPath)
	defer os.Remove(tempPath)
	if err := hydrateSnapshot(ctx, tempPath, scan, observations, relationships); err != nil {
		return Evaluation{}, err
	}

	executor, err := compliance.NewExecutor(tempPath)
	if err != nil {
		return Evaluation{}, err
	}
	results, executeErr := executor.Execute(compliance.ExecuteOptions{ControlID: request.ControlID, PackName: request.PackName, Tags: request.Tags, Parameters: request.Parameters})
	_ = executor.Close()
	if executeErr != nil {
		return Evaluation{}, executeErr
	}

	writeSession, err := dataaccess.OpenSession(ctx, request.Target)
	if err != nil {
		return Evaluation{}, err
	}
	defer writeSession.Close()
	connection, err := writeSession.Connection(ctx)
	if err != nil {
		return Evaluation{}, err
	}
	defer connection.Close()
	tx, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return Evaluation{}, err
	}
	defer tx.Rollback()

	evaluation := Evaluation{ID: uuid.NewString(), ScanID: scan.ID, Status: "completed"}
	started := time.Now().UTC()
	successfulControls := 0
	failedControls := 0
	for _, result := range results {
		if result.Error != nil {
			failedControls++
			evaluation.Error = result.Error.Error()
			continue
		}
		successfulControls++
		observed := map[string]bool{}
		for _, row := range result.Rows {
			status := strings.ToUpper(row.Status)
			if status != "FAIL" && status != "WARNING" {
				continue
			}
			fingerprint := findingFingerprint(result.PackRef, row.ControlID, scan.Provider, row.ResourceID, scan.ScopeKey)
			observed[fingerprint] = true
			finding := Finding{Fingerprint: fingerprint, PackRef: result.PackRef, ControlID: row.ControlID,
				Provider: scan.Provider, ResourceID: row.ResourceID, ScopeKey: scan.ScopeKey, Title: row.ControlName,
				Severity: strings.ToUpper(row.Severity), Details: row.Details, Status: "open",
				FirstSeenScan: scan.ID, LastSeenScan: scan.ID, FirstSeenAt: scan.StartedAt, LastSeenAt: scan.StartedAt}
			if row.Remediation != nil {
				finding.Remediation = *row.Remediation
			}
			var existed bool
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM findings WHERE fingerprint = ?`, fingerprint).Scan(&existed)
			if existed {
				evaluation.Recurred++
			} else {
				evaluation.Opened++
			}
			if err := upsertFinding(ctx, tx, finding); err != nil {
				return Evaluation{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO finding_occurrences
(evaluation_id, fingerprint, scan_id, result_status, severity, details, observed_at)
VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(evaluation_id, fingerprint) DO NOTHING`, evaluation.ID, fingerprint,
				scan.ID, status, finding.Severity, finding.Details, scan.StartedAt); err != nil {
				return Evaluation{}, err
			}
			evaluation.Findings = append(evaluation.Findings, finding)
		}
		resolved, err := resolveAbsent(ctx, tx, result.PackRef, result.ControlID, scan, observed)
		if err != nil {
			return Evaluation{}, err
		}
		evaluation.Resolved += resolved
	}
	if failedControls > 0 {
		if successfulControls == 0 {
			evaluation.Status = "failed"
		} else {
			evaluation.Status = "partial"
		}
	}
	ended := time.Now().UTC()
	packRef := request.PackName
	if packRef == "" && len(results) > 0 {
		packRef = results[0].PackRef
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO finding_evaluations
(id, scan_id, pack_ref, control_selector, scope_key, started_at, ended_at, status, error_message)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, evaluation.ID, scan.ID, packRef, request.ControlID, scan.ScopeKey,
		started, ended, evaluation.Status, nullString(evaluation.Error)); err != nil {
		return Evaluation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Evaluation{}, err
	}
	sort.Slice(evaluation.Findings, func(i, j int) bool { return evaluation.Findings[i].Fingerprint < evaluation.Findings[j].Fingerprint })
	return evaluation, nil
}

func hydrateSnapshot(ctx context.Context, target string, scan dataaccess.ScanRecord, observations []dataaccess.Observation, relationships []dataaccess.RelationshipObservation) error {
	bySource := map[string][]*pb.Relationship{}
	for _, relationship := range relationships {
		properties := map[string]string{}
		var decoded map[string]any
		if json.Unmarshal([]byte(relationship.Properties), &decoded) == nil {
			for key, value := range decoded {
				properties[key] = fmt.Sprint(value)
			}
		}
		bySource[relationship.Provider+"\x00"+relationship.FromID] = append(bySource[relationship.Provider+"\x00"+relationship.FromID], &pb.Relationship{
			TargetId: relationship.ToID, TargetType: relationship.ToType, RelationshipType: relationship.Type, Properties: properties})
	}
	resources := make([]*pb.Resource, 0, len(observations))
	for _, observation := range observations {
		tags := map[string]string{}
		_ = json.Unmarshal([]byte(observation.Tags), &tags)
		resources = append(resources, &pb.Resource{Provider: observation.Provider, Id: observation.ResourceID, Name: observation.Name,
			Type: observation.Type, Service: observation.Service, Region: observation.Location, AccountId: observation.AccountID,
			Arn: observation.ARN, ParentId: observation.ParentID, Tags: tags, Attributes: observation.Attributes,
			RawData: observation.RawData, DiscoveredAt: timestamppb.New(observation.ObservedAt),
			Relationships: bySource[observation.Provider+"\x00"+observation.ResourceID]})
	}
	var services, scopes []string
	_ = json.Unmarshal([]byte(scan.ServicesJSON), &services)
	_ = json.Unmarshal([]byte(scan.ScopesJSON), &scopes)
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		return err
	}
	defer session.Close()
	return session.PersistScanOutcome(ctx, dataaccess.ScanOutcome{ID: scan.ID, Provider: scan.Provider, Services: services,
		Scopes: scopes, Status: dataaccess.ScanStatusCompleted, StartedAt: scan.StartedAt, EndedAt: scan.EndedAt, Resources: resources}, dataaccess.PersistScanOptions{})
}

func upsertFinding(ctx context.Context, tx *sql.Tx, finding Finding) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO findings
(fingerprint, pack_ref, control_id, provider, resource_id, scope_key, title, severity, details, remediation,
 status, first_seen_scan, last_seen_scan, first_seen_at, last_seen_at, occurrence_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?, ?, ?, 1)
ON CONFLICT(fingerprint) DO UPDATE SET title = excluded.title, severity = excluded.severity,
details = excluded.details, remediation = excluded.remediation, status = 'open', last_seen_scan = excluded.last_seen_scan,
last_seen_at = excluded.last_seen_at, resolved_at = NULL, occurrence_count = findings.occurrence_count + 1
WHERE excluded.last_seen_at >= findings.last_seen_at`, finding.Fingerprint, finding.PackRef, finding.ControlID,
		finding.Provider, finding.ResourceID, finding.ScopeKey, finding.Title, finding.Severity, finding.Details,
		nullString(finding.Remediation), finding.FirstSeenScan, finding.LastSeenScan, finding.FirstSeenAt, finding.LastSeenAt)
	return err
}

func resolveAbsent(ctx context.Context, tx *sql.Tx, packRef, controlID string, scan dataaccess.ScanRecord, observed map[string]bool) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT fingerprint FROM findings WHERE pack_ref = ? AND control_id = ?
AND scope_key = ? AND status = 'open' AND last_seen_at <= ?`, packRef, controlID, scan.ScopeKey, scan.StartedAt)
	if err != nil {
		return 0, err
	}
	var candidates []string
	for rows.Next() {
		var fingerprint string
		if err := rows.Scan(&fingerprint); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, fingerprint)
	}
	rows.Close()
	resolved := 0
	for _, fingerprint := range candidates {
		if observed[fingerprint] {
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE findings SET status = 'resolved', resolved_at = ? WHERE fingerprint = ?`, scan.StartedAt, fingerprint)
		if err != nil {
			return resolved, err
		}
		count, _ := result.RowsAffected()
		resolved += int(count)
	}
	return resolved, nil
}

func List(ctx context.Context, target string, filter ListFilter) ([]Finding, error) {
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	rows, err := session.QueryContext(ctx, `SELECT f.fingerprint, f.pack_ref, f.control_id, f.provider, f.resource_id,
f.scope_key, COALESCE(f.title, ''), f.severity, COALESCE(f.details, ''), COALESCE(f.remediation, ''),
CASE WHEN f.status = 'open' AND EXISTS (SELECT 1 FROM finding_suppressions s WHERE s.fingerprint = f.fingerprint
 AND s.revoked_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > CURRENT_TIMESTAMP)) THEN 'suppressed' ELSE f.status END,
f.first_seen_scan, f.last_seen_scan, f.first_seen_at, f.last_seen_at, f.resolved_at, f.occurrence_count
FROM findings f WHERE (? = '' OR f.provider = ?) AND (? = '' OR f.control_id = ?)
AND (? = '' OR f.last_seen_scan = ?) AND (? = '' OR f.pack_ref = ?)
ORDER BY CASE f.severity WHEN 'CRITICAL' THEN 4 WHEN 'HIGH' THEN 3 WHEN 'MEDIUM' THEN 2 WHEN 'LOW' THEN 1 ELSE 0 END DESC,
f.provider, f.resource_id`, filter.Provider, filter.Provider, filter.ControlID, filter.ControlID,
		filter.ScanID, filter.ScanID, filter.PackRef, filter.PackRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Finding
	for rows.Next() {
		var item Finding
		var resolved sql.NullTime
		if err := rows.Scan(&item.Fingerprint, &item.PackRef, &item.ControlID, &item.Provider, &item.ResourceID,
			&item.ScopeKey, &item.Title, &item.Severity, &item.Details, &item.Remediation, &item.Status,
			&item.FirstSeenScan, &item.LastSeenScan, &item.FirstSeenAt, &item.LastSeenAt, &resolved, &item.OccurrenceCount); err != nil {
			return nil, err
		}
		if resolved.Valid {
			item.ResolvedAt = &resolved.Time
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.MinSeverity != "" && severityRank(item.Severity) < severityRank(filter.MinSeverity) {
			continue
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func Suppress(ctx context.Context, target, fingerprint, reason, actor string, until *time.Time) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("suppression reason is required")
	}
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		return err
	}
	defer session.Close()
	connection, err := session.Connection(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	var exists bool
	if err := connection.QueryRowContext(ctx, `SELECT COUNT(*) > 0 FROM findings WHERE fingerprint = ?`, fingerprint).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("finding %s not found", fingerprint)
	}
	_, err = connection.ExecContext(ctx, `INSERT INTO finding_suppressions(id, fingerprint, reason, actor, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?)`, uuid.NewString(), fingerprint, reason, actor, time.Now().UTC(), until)
	return err
}

func Unsuppress(ctx context.Context, target, fingerprint string) error {
	session, err := dataaccess.OpenSession(ctx, target)
	if err != nil {
		return err
	}
	defer session.Close()
	connection, err := session.Connection(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	_, err = connection.ExecContext(ctx, `UPDATE finding_suppressions SET revoked_at = ? WHERE fingerprint = ? AND revoked_at IS NULL`, time.Now().UTC(), fingerprint)
	return err
}

func findingFingerprint(packRef, controlID, provider, resourceID, scopeKey string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{packRef, controlID, provider, resourceID, scopeKey}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func severityRank(value string) int {
	switch strings.ToUpper(value) {
	case "CRITICAL":
		return 4
	case "HIGH":
		return 3
	case "MEDIUM":
		return 2
	case "LOW":
		return 1
	default:
		return 0
	}
}
func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

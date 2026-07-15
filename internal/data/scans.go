package data

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ScanSummary is one recent scan as stored in scan_metadata, in transport-neutral
// form. Adapters format the timestamp and duration for display.
type ScanSummary struct {
	ID        string
	Label     string // provider name (unified schema) or "service region" (legacy)
	StartedAt time.Time
	Resources int
	Duration  time.Duration
}

// RecentScans returns the most recent scans, newest first. It hides the
// unified/legacy scan_metadata column difference so adapters never sniff the
// storage schema themselves.
func (s *Session) RecentScans(ctx context.Context, limit int) ([]ScanSummary, error) {
	if s == nil || s.database == nil {
		return nil, fmt.Errorf("data session is closed")
	}
	if limit <= 0 {
		limit = 5
	}

	columns, err := s.scanMetadataColumns(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case columns["scan_start_time"]:
		return s.recentUnifiedScans(ctx, limit)
	case columns["scan_time"]:
		return s.recentLegacyScans(ctx, limit)
	default:
		return nil, nil
	}
}

func (s *Session) scanMetadataColumns(ctx context.Context) (map[string]bool, error) {
	rows, err := s.database.QueryContext(ctx, "SELECT * FROM scan_metadata LIMIT 0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	columns := make(map[string]bool, len(names))
	for _, name := range names {
		columns[name] = true
	}
	return columns, nil
}

func (s *Session) recentUnifiedScans(ctx context.Context, limit int) ([]ScanSummary, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, provider, scan_start_time, total_resources, duration_ms
		FROM scan_metadata
		ORDER BY scan_start_time DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scans []ScanSummary
	for rows.Next() {
		var id, provider string
		var startedAt time.Time
		var resources int
		var duration sql.NullInt64
		if err := rows.Scan(&id, &provider, &startedAt, &resources, &duration); err != nil {
			return nil, err
		}
		scans = append(scans, ScanSummary{
			ID: id, Label: provider, StartedAt: startedAt,
			Resources: resources, Duration: durationFromMillis(duration),
		})
	}
	return scans, rows.Err()
}

func (s *Session) recentLegacyScans(ctx context.Context, limit int) ([]ScanSummary, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, service, region, scan_time, total_resources, duration_ms
		FROM scan_metadata
		ORDER BY scan_time DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scans []ScanSummary
	for rows.Next() {
		var id, service, region string
		var startedAt time.Time
		var resources int
		var duration sql.NullInt64
		if err := rows.Scan(&id, &service, &region, &startedAt, &resources, &duration); err != nil {
			return nil, err
		}
		scans = append(scans, ScanSummary{
			ID: id, Label: strings.TrimSpace(service + " " + region), StartedAt: startedAt,
			Resources: resources, Duration: durationFromMillis(duration),
		})
	}
	return scans, rows.Err()
}

func durationFromMillis(millis sql.NullInt64) time.Duration {
	if !millis.Valid || millis.Int64 <= 0 {
		return 0
	}
	return time.Duration(millis.Int64) * time.Millisecond
}

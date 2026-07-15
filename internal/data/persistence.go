package data

import (
	"context"
	"fmt"
	"time"

	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

type ScanStatus string

const (
	ScanStatusCompleted ScanStatus = "completed"
	ScanStatusPartial   ScanStatus = "partial"
	ScanStatusFailed    ScanStatus = "failed"
)

// ScanOutcome contains every durable part of one provider scan.
type ScanOutcome struct {
	ID           string
	Provider     string
	Services     []string
	Scopes       []string
	FailedScopes []string
	Status       ScanStatus
	StartedAt    time.Time
	EndedAt      time.Time
	Resources    []*pb.Resource
}

type PersistScanOptions struct {
	ProviderTableOverride string
}

// ScanWriter commits a completed scan through a storage adapter. It is the
// write-side seam symmetric to Queryer: the session owns a default DuckDB-backed
// adapter, and tests substitute a fake that speaks these same data types.
type ScanWriter interface {
	StoreScanOutcome(ctx context.Context, outcome ScanOutcome, options PersistScanOptions) error
}

// graphStoreWriter is the default ScanWriter. It is the only place data's
// vocabulary is translated into the internal/db graph-store contract.
type graphStoreWriter struct {
	store *db.GraphStore
}

func (w graphStoreWriter) StoreScanOutcome(ctx context.Context, outcome ScanOutcome, options PersistScanOptions) error {
	failedScopes := append([]string(nil), outcome.FailedScopes...)
	metadata := map[string]interface{}{"failed_scopes": failedScopes}
	return w.store.StoreScanOutcome(
		ctx,
		outcome.Resources,
		db.StoreScanResourcesOptions{ProviderTableOverride: options.ProviderTableOverride},
		db.ScanOutcomeMetadata{
			ID: outcome.ID, Provider: outcome.Provider,
			Services: outcome.Services, Regions: outcome.Scopes,
			TotalResources: len(outcome.Resources), FailedResources: len(failedScopes),
			StartedAt: outcome.StartedAt, EndedAt: outcome.EndedAt,
			Metadata: metadata, Status: string(outcome.Status),
		},
	)
}

// PersistScanOutcome atomically commits resources, relationships, and scan
// metadata through the ScanWriter owned by this initialized session.
func (s *Session) PersistScanOutcome(ctx context.Context, outcome ScanOutcome, options PersistScanOptions) error {
	if s == nil || s.scanWriter == nil {
		return fmt.Errorf("data session is closed")
	}
	return s.scanWriter.StoreScanOutcome(ctx, outcome, options)
}

package main

// GCPChangeStorage preserves the enhanced GCP change-tracking storage contract
// while using the shared provider change storage implementation.
type GCPChangeStorage interface {
	ChangeStorage
	Close() error
}

// NewGCPChangeStorage creates a GCP change storage instance backed by the
// shared DuckDB change store.
func NewGCPChangeStorage(dbPath string) (GCPChangeStorage, error) {
	return NewDuckDBChangeStorage(dbPath)
}

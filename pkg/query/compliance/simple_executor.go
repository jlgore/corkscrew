package compliance

import (
	"context"
	"fmt"
	"github.com/jlgore/corkscrew/pkg/query"
)

// Loader is a wrapper around PackLoader for backward compatibility
type Loader struct {
	*PackLoader
}

// NewLoader creates a new pack loader
func NewLoader(path string) *Loader {
	return &Loader{
		PackLoader: NewPackLoader(),
	}
}

// ListPacks lists all installed packs
func (l *Loader) ListPacks() ([]*QueryPack, error) {
	packNames, err := l.DiscoverPacks(context.Background())
	if err != nil {
		return nil, err
	}
	
	var packs []*QueryPack
	for _, name := range packNames {
		pack, err := l.PackLoader.LoadPack(context.Background(), name)
		if err != nil {
			continue // Skip packs that fail to load
		}
		packs = append(packs, pack)
	}
	
	return packs, nil
}

// InstallPack installs a pack from the specified source
func (l *Loader) InstallPack(packName string) error {
	// For now, just return nil as packs are loaded from embedded resources
	return nil
}

// LoadPack loads a pack by name (wrapper for backward compatibility)
func (l *Loader) LoadPack(packName string) (*QueryPack, error) {
	return l.PackLoader.LoadPack(context.Background(), packName)
}

// Executor is a simple compliance executor wrapper
type Executor struct {
	engine query.QueryEngine
	loader *PackLoader
}

// NewExecutor creates a new compliance executor
func NewExecutor(dbPath string) (*Executor, error) {
	engine, err := query.NewEngine(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create query engine: %w", err)
	}
	
	return &Executor{
		engine: engine,
		loader: NewPackLoader(),
	}, nil
}

// Close closes the executor
func (e *Executor) Close() error {
	if e.engine != nil {
		return e.engine.Close()
	}
	return nil
}

// ExecuteOptions defines options for compliance execution
type ExecuteOptions struct {
	ControlID  string
	PackName   string
	Tags       []string
	Parameters map[string]interface{}
	DryRun     bool
}

// SimpleQueryResult represents a compliance query result for the CLI
type SimpleQueryResult struct {
	ControlID       string
	Title           string
	Description     string
	Passed          bool
	ResourceCount   int
	FailedResources []string
	Error           error
}

// Execute runs compliance checks based on the options - returns SimpleQueryResult for CLI compatibility
func (e *Executor) Execute(options ExecuteOptions) ([]SimpleQueryResult, error) {
	var results []SimpleQueryResult
	
	// If specific control ID is provided
	if options.ControlID != "" {
		// Parse control ID (format: pack/control)
		// For now, just create a simple result
		result := SimpleQueryResult{
			ControlID:     options.ControlID,
			Title:         "Control Check",
			Description:   "Checking control " + options.ControlID,
			Passed:        true,
			ResourceCount: 0,
		}
		
		// TODO: Actually execute the control query
		results = append(results, result)
		return results, nil
	}
	
	// If pack name is provided
	if options.PackName != "" {
		pack, err := e.loader.LoadPack(context.Background(), options.PackName)
		if err != nil {
			return nil, fmt.Errorf("failed to load pack %s: %w", options.PackName, err)
		}
		
		// Execute each query in the pack
		for _, query := range pack.Queries {
			result := SimpleQueryResult{
				ControlID:     query.ID,
				Title:         query.Title,
				Description:   query.Description,
				Passed:        true,
				ResourceCount: 0,
			}
			
			// TODO: Actually execute the query
			// For now, just add to results
			results = append(results, result)
		}
		
		return results, nil
	}
	
	// If tags are provided
	if len(options.Tags) > 0 {
		// TODO: Find and execute queries matching tags
		return results, fmt.Errorf("tag-based execution not yet implemented")
	}
	
	return results, fmt.Errorf("no control ID, pack name, or tags provided")
}
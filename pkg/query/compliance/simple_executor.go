package compliance

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	engine     query.QueryEngine
	loader     *PackLoader
	compliance *ComplianceExecutor
}

// NewExecutor creates a new compliance executor
func NewExecutor(dbPath string) (*Executor, error) {
	// Packs can come from external git or OCI sources, so their SQL never gets
	// host file or network access.
	engine, err := query.NewSandboxedEngine(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create query engine: %w", err)
	}

	return &Executor{
		engine:     engine,
		loader:     NewPackLoader(),
		compliance: NewComplianceExecutor(engine),
	}, nil
}

// Close closes the executor
func (e *Executor) Close() error {
	if e.engine != nil {
		return e.engine.Close()
	}
	return nil
}

// queryTimeout bounds each compliance query, so one pathological pack query
// (an accidental cross join, say) cannot hang a whole compliance run.
const queryTimeout = 2 * time.Minute

// ExecuteOptions defines options for compliance execution
type ExecuteOptions struct {
	ControlID  string
	PackName   string
	Tags       []string
	Parameters map[string]interface{}
	DryRun     bool
	// QueryTimeout overrides queryTimeout for each query when positive.
	QueryTimeout time.Duration
}

// SimpleQueryResult represents a compliance query result for the CLI
type SimpleQueryResult struct {
	PackRef         string
	ControlID       string
	Title           string
	Description     string
	Passed          bool
	ResourceCount   int
	FailedResources []string
	Rows            []ComplianceResult
	Error           error
}

// Execute runs compliance checks based on the options - returns SimpleQueryResult for CLI compatibility
func (e *Executor) Execute(
	ctx context.Context, options ExecuteOptions,
) ([]SimpleQueryResult, error) {
	selections, err := e.selectQueries(ctx, options)
	if err != nil {
		return nil, err
	}
	if len(selections) == 0 {
		return nil, fmt.Errorf("no compliance queries matched the requested options")
	}
	timeout := queryTimeout
	if options.QueryTimeout > 0 {
		timeout = options.QueryTimeout
	}

	results := make([]SimpleQueryResult, 0, len(selections))
	for _, selection := range selections {
		result := SimpleQueryResult{
			PackRef:     canonicalPackRef(selection.pack),
			ControlID:   selection.query.ID,
			Title:       selection.query.Title,
			Description: selection.query.Description,
			Passed:      true,
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("compliance run stopped before %s: %w", result.ControlID, err)
		}
		parameters := parametersWithDefaults(selection.pack, options.Parameters)
		if options.DryRun {
			if err := e.dryRunQuery(ctx, selection.query, parameters); err != nil {
				result.Passed = false
				result.Error = err
			}
			results = append(results, result)
			continue
		}

		queryCtx, cancel := context.WithTimeout(ctx, timeout)
		complianceResults, err := e.compliance.ExecuteQuery(queryCtx, &selection.query, parameters)
		cancel()
		if err != nil {
			result.Passed = false
			result.Error = err
			results = append(results, result)
			continue
		}
		recordComplianceRows(&result, complianceResults)
		results = append(results, result)
	}

	return results, nil
}

// recordComplianceRows fills result from the query's rows: any row whose
// status is not PASS fails the control and names its resource.
func recordComplianceRows(result *SimpleQueryResult, rows []ComplianceResult) {
	result.Rows = append(result.Rows, rows...)
	result.ResourceCount = len(rows)
	for _, row := range rows {
		if strings.ToUpper(row.Status) == "PASS" {
			continue
		}
		result.Passed = false
		resource := row.ResourceID
		if row.ResourceName != "" && row.ResourceName != row.ResourceID {
			resource = fmt.Sprintf("%s (%s)", row.ResourceName, row.ResourceID)
		}
		result.FailedResources = append(result.FailedResources, resource)
	}
}

func canonicalPackRef(pack *QueryPack) string {
	if pack == nil {
		return ""
	}
	namespace := strings.Trim(pack.Metadata.Namespace, "/")
	if namespace == "" {
		return pack.Metadata.Name
	}
	return namespace + "/" + pack.Metadata.Name
}

type querySelection struct {
	pack  *QueryPack
	query ComplianceQuery
}

func (e *Executor) selectQueries(ctx context.Context, options ExecuteOptions) ([]querySelection, error) {
	if options.ControlID != "" {
		return e.selectControl(ctx, options)
	}

	var packNames []string
	if options.PackName != "" {
		packNames = []string{options.PackName}
	} else {
		discovered, err := e.loader.DiscoverPacks(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to discover packs: %w", err)
		}
		packNames = discovered
	}

	var selections []querySelection
	for _, packName := range packNames {
		pack, err := e.loader.LoadPack(ctx, packName)
		if err != nil {
			return nil, fmt.Errorf("failed to load pack %s: %w", packName, err)
		}
		for _, complianceQuery := range pack.Queries {
			if !complianceQuery.Enabled || !matchesTags(pack, complianceQuery, options.Tags) {
				continue
			}
			selections = append(selections, querySelection{pack: pack, query: complianceQuery})
		}
	}

	return selections, nil
}

func (e *Executor) selectControl(ctx context.Context, options ExecuteOptions) ([]querySelection, error) {
	if options.PackName != "" {
		pack, err := e.loader.LoadPack(ctx, options.PackName)
		if err != nil {
			return nil, fmt.Errorf("failed to load pack %s: %w", options.PackName, err)
		}
		query, err := findQueryInPack(pack, options.ControlID)
		if err != nil {
			return nil, err
		}
		return []querySelection{{pack: pack, query: *query}}, nil
	}

	if packName, controlID, ok := splitControlRef(options.ControlID); ok {
		pack, err := e.loader.LoadPack(ctx, packName)
		if err == nil {
			if complianceQuery, err := findQueryInPack(pack, controlID); err == nil {
				return []querySelection{{pack: pack, query: *complianceQuery}}, nil
			}
		}
	}

	packNames, err := e.loader.DiscoverPacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover packs: %w", err)
	}
	for _, packName := range packNames {
		pack, err := e.loader.LoadPack(ctx, packName)
		if err != nil {
			continue
		}
		if complianceQuery, err := findQueryInPack(pack, options.ControlID); err == nil {
			return []querySelection{{pack: pack, query: *complianceQuery}}, nil
		}
	}

	return nil, fmt.Errorf("control %s not found", options.ControlID)
}

func findQueryInPack(pack *QueryPack, controlID string) (*ComplianceQuery, error) {
	for i := range pack.Queries {
		if queryMatchesControl(pack.Queries[i], controlID) {
			return &pack.Queries[i], nil
		}
	}
	return nil, fmt.Errorf("control %s not found in pack %s", controlID, pack.Metadata.Name)
}

func queryMatchesControl(query ComplianceQuery, controlID string) bool {
	if query.ID == controlID {
		return true
	}
	return strings.HasSuffix(controlID, "/"+query.ID)
}

func splitControlRef(controlRef string) (string, string, bool) {
	parts := strings.Split(controlRef, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1], true
}

func matchesTags(pack *QueryPack, query ComplianceQuery, tags []string) bool {
	if len(tags) == 0 {
		return true
	}

	available := make(map[string]bool)
	for _, tag := range pack.Metadata.Tags {
		available[strings.ToLower(strings.TrimSpace(tag))] = true
	}
	for _, tag := range query.Tags {
		available[strings.ToLower(strings.TrimSpace(tag))] = true
	}

	for _, tag := range tags {
		if !available[strings.ToLower(strings.TrimSpace(tag))] {
			return false
		}
	}
	return true
}

func parametersWithDefaults(pack *QueryPack, provided map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	for key, value := range provided {
		merged[key] = value
	}
	for _, parameter := range pack.Parameters {
		if _, exists := merged[parameter.Name]; !exists && parameter.Default != nil {
			merged[parameter.Name] = parameter.Default
		}
	}
	return merged
}

func (e *Executor) dryRunQuery(ctx context.Context, complianceQuery ComplianceQuery, parameters map[string]interface{}) error {
	_ = ctx

	substitutedSQL, _, err := e.compliance.substituteParameters(complianceQuery.SQL, complianceQuery.Parameters, parameters)
	if err != nil {
		return err
	}

	return e.engine.Validate(substitutedSQL)
}

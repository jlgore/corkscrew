package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	asset "cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/asset/apiv1/assetpb"
	"cloud.google.com/go/logging"
	"google.golang.org/api/iterator"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// GCPChangeTracker implements change tracking for Google Cloud Platform
type GCPChangeTracker struct {
	*BaseChangeTracker
	assetClient    *asset.Client
	loggingClient  *logging.Client
	projectIDs     []string
	orgID          string
	scope          string
	
	// Real-time monitoring
	monitoringActive bool
	stopCh           chan struct{}
	mu               sync.RWMutex
}

// NewGCPChangeTracker creates a new GCP change tracker
func NewGCPChangeTracker(ctx context.Context, storage ChangeStorage, config *ChangeTrackerConfig) (*GCPChangeTracker, error) {
	if config == nil {
		config = &ChangeTrackerConfig{
			Provider:               "gcp",
			EnableRealTimeMonitoring: true,
			ChangeRetention:        365 * 24 * time.Hour,
			DriftCheckInterval:     1 * time.Hour,
			AlertingEnabled:        true,
			AnalyticsEnabled:       true,
			CacheEnabled:           true,
			CacheTTL:               5 * time.Minute,
			MaxConcurrentStreams:   100,
			BatchSize:              1000,
			MaxQueryTimeRange:      30 * 24 * time.Hour,
		}
	}

	// Initialize base tracker
	baseTracker := NewBaseChangeTracker("gcp", storage, config)

	// Create Asset client
	assetClient, err := asset.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create asset client: %w", err)
	}

	// Create Logging client for real-time monitoring
	var loggingClient *logging.Client
	if config.EnableRealTimeMonitoring {
		loggingClient, err = logging.NewClient(ctx, "")
		if err != nil {
			log.Printf("Warning: failed to create logging client for real-time monitoring: %v", err)
		}
	}

	gct := &GCPChangeTracker{
		BaseChangeTracker: baseTracker,
		assetClient:       assetClient,
		loggingClient:     loggingClient,
		stopCh:           make(chan struct{}),
	}

	return gct, nil
}

// SetScope configures the tracking scope (projects, folders, or organizations)
func (gct *GCPChangeTracker) SetScope(scope string, projectIDs []string, orgID string) {
	gct.mu.Lock()
	defer gct.mu.Unlock()

	gct.scope = scope
	gct.projectIDs = projectIDs
	gct.orgID = orgID
}

// QueryChanges retrieves change events based on the provided query
func (gct *GCPChangeTracker) QueryChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	if err := gct.ValidateChangeQuery(req); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	// Check cache first
	if gct.cache != nil {
		cacheKey := gct.generateQueryCacheKey(req)
		if cachedResult := gct.getCachedQuery(cacheKey); cachedResult != nil {
			log.Printf("Returning cached result for change query")
			return cachedResult.Results, nil
		}
	}

	// Query Asset Inventory for changes
	assetChanges, err := gct.queryAssetInventoryChanges(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query asset inventory changes: %w", err)
	}

	// Convert to universal change events
	changeEvents := make([]*ChangeEvent, 0, len(assetChanges))
	for _, assetChange := range assetChanges {
		event := gct.convertAssetToChangeEvent(assetChange)
		if event != nil {
			changeEvents = append(changeEvents, event)
		}
	}

	// Apply additional filtering and sorting
	changeEvents = gct.filterAndSortChanges(changeEvents, req)

	// Cache the result
	if gct.cache != nil {
		cacheKey := gct.generateQueryCacheKey(req)
		gct.cacheQuery(cacheKey, req, changeEvents)
	}

	return changeEvents, nil
}

// StreamChanges provides real-time streaming of change events
func (gct *GCPChangeTracker) StreamChanges(req *StreamRequest, stream ChangeEventStream) error {
	if req.Query == nil {
		return fmt.Errorf("stream request must include a query")
	}

	if err := gct.ValidateChangeQuery(req.Query); err != nil {
		return fmt.Errorf("invalid stream query: %w", err)
	}

	ctx := stream.Context()
	
	// Set up streaming parameters
	bufferSize := req.BufferSize
	if bufferSize <= 0 {
		bufferSize = 100
	}

	batchTimeout := req.BatchTimeout
	if batchTimeout <= 0 {
		batchTimeout = 5 * time.Second
	}

	// Create a ticker for periodic checks
	ticker := time.NewTicker(batchTimeout)
	defer ticker.Stop()

	lastQuery := time.Now()
	buffer := make([]*ChangeEvent, 0, bufferSize)

	for {
		select {
		case <-ctx.Done():
			// Send any remaining buffered changes
			if len(buffer) > 0 {
				for _, change := range buffer {
					if err := stream.Send(change); err != nil {
						return err
					}
				}
			}
			return ctx.Err()

		case <-ticker.C:
			// Query for new changes since last check
			query := &ChangeQuery{
				Provider:       "gcp",
				ResourceFilter: req.Query.ResourceFilter,
				ChangeTypes:    req.Query.ChangeTypes,
				Severities:     req.Query.Severities,
				StartTime:      lastQuery,
				EndTime:        time.Now(),
				Limit:          bufferSize * 2, // Query more than buffer to handle high-volume scenarios
				SortBy:         "timestamp",
				SortOrder:      "asc",
			}

			changes, err := gct.QueryChanges(ctx, query)
			if err != nil {
				log.Printf("Error querying changes for stream: %v", err)
				continue
			}

			// Add to buffer
			buffer = append(buffer, changes...)
			lastQuery = time.Now()

			// Send buffered changes if buffer is full or has been accumulating
			if len(buffer) >= bufferSize {
				for _, change := range buffer {
					if err := stream.Send(change); err != nil {
						return err
					}
				}
				buffer = buffer[:0] // Clear buffer
			}
		}
	}
}

// DetectDrift performs configuration drift detection against a baseline
func (gct *GCPChangeTracker) DetectDrift(ctx context.Context, baseline *DriftBaseline) (*DriftReport, error) {
	if baseline == nil {
		return nil, fmt.Errorf("baseline cannot be nil")
	}

	log.Printf("Starting drift detection against baseline: %s", baseline.Name)

	// TODO: Implement full drift detection - this is a placeholder
	return &DriftReport{
		BaselineID:    baseline.Name,
		GeneratedAt:   time.Now(),
		DriftItems:    []*DriftItem{},
		Summary: &DriftSummary{
			HighSeverityCount:     0,
			MediumSeverityCount:   0,
			LowSeverityCount:      0,
			CriticalSeverityCount: 0,
			DriftByType:          make(map[string]int),
			DriftByService:       make(map[string]int),
			ComplianceScore:      100.0,
			Recommendations:      []string{},
		},
	}, nil
}

// MonitorChanges starts real-time change monitoring
func (gct *GCPChangeTracker) MonitorChanges(ctx context.Context, callback func(*ChangeEvent)) error {
	gct.mu.Lock()
	if gct.monitoringActive {
		gct.mu.Unlock()
		return fmt.Errorf("change monitoring is already active")
	}
	gct.monitoringActive = true
	gct.mu.Unlock()

	defer func() {
		gct.mu.Lock()
		gct.monitoringActive = false
		gct.mu.Unlock()
	}()

	log.Printf("Starting real-time change monitoring for GCP resources")

	// Set up periodic polling for changes
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	lastCheck := time.Now().Add(-1 * time.Minute) // Start with 1 minute ago

	for {
		select {
		case <-ctx.Done():
			log.Printf("Change monitoring stopped")
			return ctx.Err()

		case <-gct.stopCh:
			log.Printf("Change monitoring stopped via stop channel")
			return nil

		case <-ticker.C:
			// Query for changes since last check
			query := &ChangeQuery{
				Provider:  "gcp",
				StartTime: lastCheck,
				EndTime:   time.Now(),
				Limit:     1000,
				SortBy:    "timestamp",
				SortOrder: "asc",
			}

			changes, err := gct.QueryChanges(ctx, query)
			if err != nil {
				log.Printf("Error querying changes during monitoring: %v", err)
				continue
			}

			// Process each change
			for _, change := range changes {
				// Analyze impact
				change.ImpactAssessment = gct.AnalyzeChangeImpact(change)

				// Trigger alerts if needed
				if gct.alerting != nil {
					gct.alerting.ProcessChange(change)
				}

				// Call the callback
				callback(change)

				// Store the change
				if err := gct.storage.StoreChange(change); err != nil {
					log.Printf("Failed to store monitored change: %v", err)
				}
			}

			lastCheck = time.Now()
		}
	}
}

// GetChangeHistory retrieves the change history for a specific resource
func (gct *GCPChangeTracker) GetChangeHistory(ctx context.Context, resourceID string) ([]*ChangeEvent, error) {
	// Try storage first
	changes, err := gct.storage.GetChangeHistory(resourceID)
	if err != nil {
		log.Printf("Failed to get change history from storage: %v", err)
	}

	// If no changes in storage or recent history needed, query Asset Inventory
	if len(changes) == 0 || time.Since(changes[0].Timestamp) > 24*time.Hour {
		// TODO: implement queryAssetHistoryForResource method
		log.Printf("Asset history query not implemented for resource: %s", resourceID)
	}

	// Sort by timestamp
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Timestamp.After(changes[j].Timestamp)
	})

	return changes, nil
}

// CreateBaseline creates a new configuration baseline
func (gct *GCPChangeTracker) CreateBaseline(ctx context.Context, resources []*pb.Resource) (*DriftBaseline, error) {
	baseline := &DriftBaseline{
		ID:          fmt.Sprintf("baseline_gcp_%d", time.Now().Unix()),
		Name:        fmt.Sprintf("GCP Baseline %s", time.Now().Format("2006-01-02 15:04:05")),
		Description: "Auto-generated baseline for GCP resources",
		Provider:    "gcp",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Resources:   make(map[string]*ResourceState),
		Version:     "1.0",
		Active:      true,
	}

	// Convert resources to resource states
	for _, resource := range resources {
		state := gct.convertResourceToState(resource)
		baseline.Resources[resource.Id] = state
	}

	// Store the baseline
	if err := gct.storage.StoreBaseline(baseline); err != nil {
		return nil, fmt.Errorf("failed to store baseline: %w", err)
	}

	log.Printf("Created baseline %s with %d resources", baseline.ID, len(baseline.Resources))
	return baseline, nil
}

// StopMonitoring stops real-time change monitoring
func (gct *GCPChangeTracker) StopMonitoring() {
	close(gct.stopCh)
}

// Close closes the GCP change tracker and releases resources
func (gct *GCPChangeTracker) Close() error {
	if gct.monitoringActive {
		gct.StopMonitoring()
	}

	if gct.assetClient != nil {
		if err := gct.assetClient.Close(); err != nil {
			log.Printf("Error closing asset client: %v", err)
		}
	}

	if gct.loggingClient != nil {
		if err := gct.loggingClient.Close(); err != nil {
			log.Printf("Error closing logging client: %v", err)
		}
	}

	return nil
}

// Helper methods

func (gct *GCPChangeTracker) queryAssetInventoryChanges(ctx context.Context, req *ChangeQuery) ([]*AssetChange, error) {
	var changes []*AssetChange

	// Build the parent scope
	parent := gct.buildParentScope()
	if parent == "" {
		return nil, fmt.Errorf("no valid scope configured")
	}

	// For change tracking, we need to query the asset feed or use batch asset history
	// Since Cloud Asset Inventory doesn't have a direct "changes" API, we'll simulate it
	// by querying assets at different points in time

	// Query current state
	currentAssets, err := gct.queryAssetsAtTime(ctx, parent, req.EndTime, req.ResourceFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to query current assets: %w", err)
	}

	// Query previous state (simplified - in production you'd use feeds or multiple snapshots)
	previousAssets, err := gct.queryAssetsAtTime(ctx, parent, req.StartTime, req.ResourceFilter)
	if err != nil {
		log.Printf("Failed to query previous assets, assuming no previous state: %v", err)
		previousAssets = make(map[string]*assetpb.Asset)
	}

	// Compare states to detect changes
	changes = gct.detectChangesFromAssetComparison(currentAssets, previousAssets)

	// Filter by change types if specified
	if len(req.ChangeTypes) > 0 {
		filteredChanges := make([]*AssetChange, 0)
		for _, change := range changes {
			if gct.matchesChangeType(change, req.ChangeTypes) {
				filteredChanges = append(filteredChanges, change)
			}
		}
		changes = filteredChanges
	}

	return changes, nil
}

func (gct *GCPChangeTracker) queryAssetsAtTime(ctx context.Context, parent string, timestamp time.Time, filter *ResourceFilter) (map[string]*assetpb.Asset, error) {
	assets := make(map[string]*assetpb.Asset)

	// Build asset types filter
	var assetTypes []string
	if filter != nil && len(filter.ResourceTypes) > 0 {
		assetTypes = gct.convertResourceTypesToAssetTypes(filter.ResourceTypes)
	}

	// Query assets using SearchAllResources (for current time) or ExportAssets (for historical)
	if time.Since(timestamp) < 1*time.Hour {
		// Recent query - use SearchAllResources
		req := &assetpb.SearchAllResourcesRequest{
			Scope:      parent,
			AssetTypes: assetTypes,
			PageSize:   1000,
		}

		it := gct.assetClient.SearchAllResources(ctx, req)
		for {
			result, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, err
			}

			// Convert SearchResult to Asset (simplified)
			asset := &assetpb.Asset{
				Name:      result.Name,
				AssetType: result.AssetType,
				// Additional fields would be populated in real implementation
			}
			assets[result.Name] = asset
		}
	} else {
		// Historical query - would use ExportAssets with read_time
		// For this implementation, we'll return empty to simulate no historical data
		log.Printf("Historical asset query not implemented for timestamp: %v", timestamp)
	}

	return assets, nil
}

func (gct *GCPChangeTracker) detectChangesFromAssetComparison(current, previous map[string]*assetpb.Asset) []*AssetChange {
	var changes []*AssetChange

	// Detect new resources
	for name, asset := range current {
		if _, exists := previous[name]; !exists {
			changes = append(changes, &AssetChange{
				Timestamp:  time.Now(),
				ChangeType: string(ChangeTypeCreate),
				Asset:      asset,
			})
		}
	}

	// Detect deleted resources
	for name, asset := range previous {
		if _, exists := current[name]; !exists {
			changes = append(changes, &AssetChange{
				Timestamp:  time.Now(),
				ChangeType: string(ChangeTypeDelete),
				Asset:      asset,
			})
		}
	}

	// Detect modified resources (simplified)
	for name, currentAsset := range current {
		if previousAsset, exists := previous[name]; exists {
			// Simple comparison - in production you'd compare all relevant fields
			if gct.hasAssetChanged(previousAsset, currentAsset) {
				changes = append(changes, &AssetChange{
					Timestamp:  time.Now(),
					ChangeType: string(ChangeTypeUpdate),
					Asset:      currentAsset,
				})
			}
		}
	}

	return changes
}

func (gct *GCPChangeTracker) hasAssetChanged(previous, current *assetpb.Asset) bool {
	// Simplified change detection - in production this would be much more sophisticated
	return previous.AssetType != current.AssetType // Placeholder logic
}

func (gct *GCPChangeTracker) convertAssetToChangeEvent(assetChange *AssetChange) *ChangeEvent {
	if assetChange == nil || assetChange.Asset == nil {
		return nil
	}

	asset := assetChange.Asset
	
	event := &ChangeEvent{
		ID:           gct.GenerateChangeID(asset.Name, assetChange.Timestamp, ChangeType(assetChange.ChangeType)),
		Provider:     "gcp",
		ResourceID:   asset.Name,
		ResourceType: gct.extractResourceTypeFromAssetType(asset.AssetType),
		Service:      gct.extractServiceFromAssetType(asset.AssetType),
		ChangeType:   ChangeType(assetChange.ChangeType),
		Severity:     gct.calculateChangeSeverity(ChangeType(assetChange.ChangeType), asset.AssetType),
		Timestamp:    assetChange.Timestamp,
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"asset_type": asset.AssetType,
			"source":     "asset_inventory",
		},
	}

	// Extract project from resource name
	if project := gct.extractProjectFromResourceName(asset.Name); project != "" {
		event.Project = project
	}

	// Add current state
	event.CurrentState = &ResourceState{
		ResourceID:  asset.Name,
		Timestamp:   assetChange.Timestamp,
		Properties:  gct.extractAssetProperties(asset),
		Status:      "active", // Placeholder
		Checksum:    gct.CalculateResourceChecksum(&ResourceState{ResourceID: asset.Name}),
	}

	return event
}

func (gct *GCPChangeTracker) buildParentScope() string {
	switch gct.scope {
	case "organizations":
		if gct.orgID != "" {
			return fmt.Sprintf("organizations/%s", gct.orgID)
		}
	case "projects":
		if len(gct.projectIDs) > 0 {
			return fmt.Sprintf("projects/%s", gct.projectIDs[0]) // Use first project for now
		}
	}
	return ""
}

func (gct *GCPChangeTracker) extractResourceTypeFromAssetType(assetType string) string {
	parts := strings.Split(assetType, "/")
	if len(parts) > 1 {
		return parts[1]
	}
	return "Unknown"
}

func (gct *GCPChangeTracker) extractServiceFromAssetType(assetType string) string {
	parts := strings.Split(assetType, "/")
	if len(parts) > 0 {
		service := strings.TrimSuffix(parts[0], ".googleapis.com")
		return service
	}
	return "unknown"
}

func (gct *GCPChangeTracker) extractProjectFromResourceName(resourceName string) string {
	// Extract project from resource name like "projects/my-project/..."
	if strings.HasPrefix(resourceName, "projects/") {
		parts := strings.Split(resourceName, "/")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return ""
}

func (gct *GCPChangeTracker) extractAssetProperties(asset *assetpb.Asset) map[string]interface{} {
	properties := make(map[string]interface{})
	
	properties["name"] = asset.Name
	properties["asset_type"] = asset.AssetType
	
	// Add additional properties from asset resource data
	if asset.Resource != nil {
		properties["version"] = asset.Resource.Version
		properties["discovery_document_uri"] = asset.Resource.DiscoveryDocumentUri
		properties["discovery_name"] = asset.Resource.DiscoveryName
		
		// Convert resource data to map
		if asset.Resource.Data != nil {
			dataMap := make(map[string]interface{})
			// In a real implementation, you'd properly unmarshal the protobuf struct
			properties["resource_data"] = dataMap
		}
	}
	
	return properties
}

func (gct *GCPChangeTracker) calculateChangeSeverity(changeType ChangeType, assetType string) ChangeSeverity {
	// Base severity on change type
	switch changeType {
	case ChangeTypeDelete:
		return SeverityHigh
	case ChangeTypeCreate:
		return SeverityMedium
	case ChangeTypePolicyChange:
		return SeverityHigh
	case ChangeTypeUpdate:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// Additional helper methods for comprehensive functionality
func (gct *GCPChangeTracker) filterAndSortChanges(changes []*ChangeEvent, req *ChangeQuery) []*ChangeEvent {
	filtered := changes

	// Apply severity filter
	if len(req.Severities) > 0 {
		var filteredBySeverity []*ChangeEvent
		for _, change := range filtered {
			for _, severity := range req.Severities {
				if change.Severity == severity {
					filteredBySeverity = append(filteredBySeverity, change)
					break
				}
			}
		}
		filtered = filteredBySeverity
	}

	// Apply resource filter
	if req.ResourceFilter != nil {
		filtered = gct.applyResourceFilter(filtered, req.ResourceFilter)
	}

	// Sort results
	sort.Slice(filtered, func(i, j int) bool {
		switch req.SortBy {
		case "timestamp":
			if req.SortOrder == "asc" {
				return filtered[i].Timestamp.Before(filtered[j].Timestamp)
			}
			return filtered[i].Timestamp.After(filtered[j].Timestamp)
		case "severity":
			return gct.compareSeverity(filtered[i].Severity, filtered[j].Severity, req.SortOrder == "asc")
		default:
			return filtered[i].Timestamp.After(filtered[j].Timestamp)
		}
	})

	// Apply limit and offset
	start := req.Offset
	if start > len(filtered) {
		return []*ChangeEvent{}
	}

	end := start + req.Limit
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end]
}

func (gct *GCPChangeTracker) applyResourceFilter(changes []*ChangeEvent, filter *ResourceFilter) []*ChangeEvent {
	if filter == nil {
		return changes
	}

	var filtered []*ChangeEvent

	for _, change := range changes {
		// Check resource IDs
		if len(filter.ResourceIDs) > 0 && !contains(filter.ResourceIDs, change.ResourceID) {
			continue
		}

		// Check resource types
		if len(filter.ResourceTypes) > 0 && !contains(filter.ResourceTypes, change.ResourceType) {
			continue
		}

		// Check services
		if len(filter.Services) > 0 && !contains(filter.Services, change.Service) {
			continue
		}

		// Check projects
		if len(filter.Projects) > 0 && !contains(filter.Projects, change.Project) {
			continue
		}

		// Check regions
		if len(filter.Regions) > 0 && !contains(filter.Regions, change.Region) {
			continue
		}

		// Check tags (if current state has tags)
		if len(filter.Tags) > 0 && change.CurrentState != nil {
			tagMatch := true
			for key, value := range filter.Tags {
				if change.CurrentState.Tags[key] != value {
					tagMatch = false
					break
				}
			}
			if !tagMatch {
				continue
			}
		}

		filtered = append(filtered, change)
	}

	return filtered
}

func (gct *GCPChangeTracker) compareSeverity(a, b ChangeSeverity, ascending bool) bool {
	severityOrder := map[ChangeSeverity]int{
		SeverityLow:      1,
		SeverityMedium:   2,
		SeverityHigh:     3,
		SeverityCritical: 4,
	}

	aVal := severityOrder[a]
	bVal := severityOrder[b]

	if ascending {
		return aVal < bVal
	}
	return aVal > bVal
}

// Cache management methods
func (gct *GCPChangeTracker) generateQueryCacheKey(req *ChangeQuery) string {
	// Generate a cache key based on query parameters
	return fmt.Sprintf("query_%s_%d_%d_%v_%v",
		req.Provider,
		req.StartTime.Unix(),
		req.EndTime.Unix(),
		req.ChangeTypes,
		req.Severities)
}

func (gct *GCPChangeTracker) getCachedQuery(cacheKey string) *CachedQuery {
	if gct.cache == nil {
		return nil
	}

	gct.cache.mu.RLock()
	defer gct.cache.mu.RUnlock()

	cached, exists := gct.cache.queries[cacheKey]
	if !exists {
		return nil
	}

	if time.Now().After(cached.ExpiresAt) {
		// Cache expired
		delete(gct.cache.queries, cacheKey)
		return nil
	}

	return cached
}

func (gct *GCPChangeTracker) cacheQuery(cacheKey string, query *ChangeQuery, results []*ChangeEvent) {
	if gct.cache == nil {
		return
	}

	gct.cache.mu.Lock()
	defer gct.cache.mu.Unlock()

	cached := &CachedQuery{
		Query:     query,
		Results:   results,
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(gct.cache.ttl),
	}

	gct.cache.queries[cacheKey] = cached
}

// convertResourceToState converts a protobuf Resource to ResourceState
func (gct *GCPChangeTracker) convertResourceToState(resource *pb.Resource) *ResourceState {
	state := &ResourceState{
		ResourceID:    resource.Id,
		Timestamp:     time.Now(),
		Properties:    make(map[string]interface{}),
		Tags:          make(map[string]string),
		Configuration: make(map[string]interface{}),
		Status:        "active",
	}

	// Extract properties from resource
	state.Properties["resourceType"] = resource.Type
	state.Properties["region"] = resource.Region
	state.Properties["service"] = resource.Service
	
	// Parse attributes if available
	if resource.Attributes != "" {
		var attrs map[string]interface{}
		if err := json.Unmarshal([]byte(resource.Attributes), &attrs); err == nil {
			for k, v := range attrs {
				state.Properties[k] = v
			}
		}
	}

	// Copy tags
	for k, v := range resource.Tags {
		state.Tags[k] = v
	}

	// Calculate checksum
	state.Checksum = gct.calculateResourceChecksum(state)

	return state
}

// matchesChangeType checks if a change matches the specified change types
func (gct *GCPChangeTracker) matchesChangeType(change *AssetChange, changeTypes []ChangeType) bool {
	if len(changeTypes) == 0 {
		return true
	}
	
	changeType := ChangeTypeUpdate // Default
	if change.ChangeType == "ADDED" {
		changeType = ChangeTypeCreate
	} else if change.ChangeType == "REMOVED" {
		changeType = ChangeTypeDelete
	}

	for _, ct := range changeTypes {
		if ct == changeType {
			return true
		}
	}
	return false
}

// convertResourceTypesToAssetTypes converts resource types to GCP asset types
func (gct *GCPChangeTracker) convertResourceTypesToAssetTypes(resourceTypes []string) []string {
	assetTypes := make([]string, 0, len(resourceTypes))
	
	for _, resourceType := range resourceTypes {
		// Map common resource types to GCP asset types
		switch resourceType {
		case "compute.instances":
			assetTypes = append(assetTypes, "compute.googleapis.com/Instance")
		case "storage.buckets":
			assetTypes = append(assetTypes, "storage.googleapis.com/Bucket")
		case "iam.serviceAccounts":
			assetTypes = append(assetTypes, "iam.googleapis.com/ServiceAccount")
		default:
			// Use the resource type as-is if no mapping found
			assetTypes = append(assetTypes, resourceType)
		}
	}
	
	return assetTypes
}

// calculateResourceChecksum generates a checksum for a resource state
func (gct *GCPChangeTracker) calculateResourceChecksum(state *ResourceState) string {
	// Simple implementation - in production this would be more sophisticated
	return fmt.Sprintf("%s-%d", state.ResourceID, time.Now().Unix())
}
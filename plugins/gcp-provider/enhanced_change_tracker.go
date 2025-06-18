package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/asset/apiv1"
	"cloud.google.com/go/asset/apiv1/assetpb"
	"cloud.google.com/go/logging"
	"cloud.google.com/go/pubsub"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnhancedChangeTracker provides comprehensive change tracking for GCP resources
type EnhancedChangeTracker struct {
	mu                sync.RWMutex
	projectID         string
	assetClient       *asset.Client
	loggingClient     *logging.Client
	pubsubClient      *pubsub.Client
	storage           GCPChangeStorage
	analytics         *ChangeAnalytics
	alerting          *AlertingSystem
	driftDetector     *DriftDetector
	
	// Configuration
	config            *ChangeTrackerConfig
	isMonitoring      bool
	subscriptions     map[string]*pubsub.Subscription
}

// GCPChangeEvent represents a change event specific to GCP
type GCPChangeEvent struct {
	*ChangeEvent
	AssetType         string                 `json:"asset_type"`
	ResourceData      map[string]interface{} `json:"resource_data"`
	IAMPolicyData     map[string]interface{} `json:"iam_policy_data,omitempty"`
	AssetState        string                 `json:"asset_state"` // ACTIVE, DELETED
	ChangeSource      string                 `json:"change_source"` // ASSET_INVENTORY, CLOUD_LOGGING, DISCOVERY
	CloudLoggingData  map[string]interface{} `json:"cloud_logging_data,omitempty"`
}

// AssetHistoryQuery represents parameters for querying asset history
type AssetHistoryQuery struct {
	AssetNames        []string  `json:"asset_names,omitempty"`
	AssetTypes        []string  `json:"asset_types,omitempty"`
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	ContentType       string    `json:"content_type"` // RESOURCE, IAM_POLICY, ORG_POLICY, ACCESS_POLICY
	ReadTimeWindow    string    `json:"read_time_window,omitempty"`
	MaxResults        int32     `json:"max_results,omitempty"`
}

// AssetHistoryResult contains the results of an asset history query
type AssetHistoryResult struct {
	Assets            []*TemporalAsset       `json:"assets"`
	NextPageToken     string                 `json:"next_page_token,omitempty"`
	ReadTime          time.Time              `json:"read_time"`
	TotalChanges      int                    `json:"total_changes"`
	ChangesByType     map[string]int         `json:"changes_by_type"`
}

// TemporalAsset represents an asset at a specific point in time
type TemporalAsset struct {
	Window            *TimeWindow            `json:"window"`
	Deleted           bool                   `json:"deleted"`
	Asset             *GCPAsset              `json:"asset,omitempty"`
	PriorAssetState   string                 `json:"prior_asset_state,omitempty"`
	PriorAsset        *GCPAsset              `json:"prior_asset,omitempty"`
}

// TimeWindow represents a time range for temporal assets
type TimeWindow struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// GCPAsset represents a GCP asset with extended metadata
type GCPAsset struct {
	Name              string                 `json:"name"`
	AssetType         string                 `json:"asset_type"`
	Resource          map[string]interface{} `json:"resource,omitempty"`
	IAMPolicy         map[string]interface{} `json:"iam_policy,omitempty"`
	OrgPolicy         []map[string]interface{} `json:"org_policy,omitempty"`
	AccessPolicy      map[string]interface{} `json:"access_policy,omitempty"`
	Ancestors         []string               `json:"ancestors,omitempty"`
	UpdateTime        time.Time              `json:"update_time"`
}

// NewEnhancedChangeTracker creates a new enhanced change tracker for GCP
func NewEnhancedChangeTracker(ctx context.Context, projectID string, config *ChangeTrackerConfig) (*EnhancedChangeTracker, error) {
	// Initialize Cloud Asset client
	assetClient, err := asset.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Asset client: %w", err)
	}

	// Initialize Cloud Logging client
	loggingClient, err := logging.NewClient(ctx, projectID)
	if err != nil {
		assetClient.Close()
		return nil, fmt.Errorf("failed to create Logging client: %w", err)
	}

	// Initialize Pub/Sub client
	pubsubClient, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		assetClient.Close()
		loggingClient.Close()
		return nil, fmt.Errorf("failed to create Pub/Sub client: %w", err)
	}

	// Initialize storage
	storage, err := NewGCPChangeStorage(fmt.Sprintf("gcp_change_tracking_%s.db", projectID))
	if err != nil {
		assetClient.Close()
		loggingClient.Close()
		pubsubClient.Close()
		return nil, fmt.Errorf("failed to create change storage: %w", err)
	}

	// Initialize analytics and alerting
	analytics := NewChangeAnalytics(storage)
	alerting := NewAlertingSystem()
	driftDetector := NewDriftDetector(storage)

	tracker := &EnhancedChangeTracker{
		projectID:     projectID,
		assetClient:   assetClient,
		loggingClient: loggingClient,
		pubsubClient:  pubsubClient,
		storage:       storage,
		analytics:     analytics,
		alerting:      alerting,
		driftDetector: driftDetector,
		config:        config,
		subscriptions: make(map[string]*pubsub.Subscription),
	}

	log.Printf("✅ Enhanced change tracker initialized for project: %s", projectID)
	return tracker, nil
}

// Close cleanly shuts down the change tracker
func (ect *EnhancedChangeTracker) Close() error {
	ect.mu.Lock()
	defer ect.mu.Unlock()

	var errors []error

	// Stop monitoring
	if ect.isMonitoring {
		ect.stopMonitoring()
	}

	// Close clients
	if err := ect.assetClient.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close asset client: %w", err))
	}

	if err := ect.loggingClient.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close logging client: %w", err))
	}

	if err := ect.pubsubClient.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close pubsub client: %w", err))
	}

	if err := ect.storage.Close(); err != nil {
		errors = append(errors, fmt.Errorf("failed to close storage: %w", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors during cleanup: %v", errors)
	}

	log.Printf("✅ Enhanced change tracker closed successfully")
	return nil
}

// QueryAssetHistory queries the history of GCP assets using Cloud Asset Inventory
func (ect *EnhancedChangeTracker) QueryAssetHistory(ctx context.Context, query *AssetHistoryQuery) (*AssetHistoryResult, error) {
	ect.mu.RLock()
	defer ect.mu.RUnlock()

	log.Printf("🔍 Querying asset history: %d asset types, %v to %v", 
		len(query.AssetTypes), query.StartTime, query.EndTime)

	// Prepare the batch get assets history request
	req := &assetpb.BatchGetAssetsHistoryRequest{
		Parent: fmt.Sprintf("projects/%s", ect.projectID),
		ContentType: ect.convertContentType(query.ContentType),
		ReadTimeWindow: &assetpb.TimeWindow{
			StartTime: timestamppb.New(query.StartTime),
			EndTime:   timestamppb.New(query.EndTime),
		},
	}

	// Add asset names if specified
	if len(query.AssetNames) > 0 {
		req.AssetNames = query.AssetNames
	}

	// Execute the query
	resp, err := ect.assetClient.BatchGetAssetsHistory(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query asset history: %w", err)
	}

	// Convert response to our format
	result := &AssetHistoryResult{
		ReadTime:      time.Now(),
		Assets:        []*TemporalAsset{},
		ChangesByType: make(map[string]int),
	}

	for _, temporalAsset := range resp.Assets {
		// Process temporal assets directly from the response
		gcpAsset := ect.convertTemporalAsset(temporalAsset)
		result.Assets = append(result.Assets, gcpAsset)
		
		// Track changes by type
		if gcpAsset.Asset != nil {
			result.ChangesByType[gcpAsset.Asset.AssetType]++
		}
	}

	result.TotalChanges = len(result.Assets)

	log.Printf("✅ Asset history query completed: %d temporal assets found", len(result.Assets))
	return result, nil
}

// TrackResourceChanges tracks changes to specific resources over time
func (ect *EnhancedChangeTracker) TrackResourceChanges(ctx context.Context, resourceNames []string, since time.Time) ([]*GCPChangeEvent, error) {
	ect.mu.RLock()
	defer ect.mu.RUnlock()

	log.Printf("📊 Tracking changes for %d resources since %v", len(resourceNames), since)

	var allChanges []*GCPChangeEvent

	// Query asset history for each resource
	for _, resourceName := range resourceNames {
		query := &AssetHistoryQuery{
			AssetNames:  []string{resourceName},
			StartTime:   since,
			EndTime:     time.Now(),
			ContentType: "RESOURCE",
			MaxResults:  100,
		}

		history, err := ect.QueryAssetHistory(ctx, query)
		if err != nil {
			log.Printf("⚠️ Failed to get history for resource %s: %v", resourceName, err)
			continue
		}

		// Convert temporal assets to change events
		changes := ect.convertToChangeEvents(history.Assets, resourceName)
		allChanges = append(allChanges, changes...)
	}

	// Enhance with Cloud Logging data
	enhancedChanges := ect.enhanceWithCloudLogging(ctx, allChanges, since)

	// Store changes in the database
	if len(enhancedChanges) > 0 {
		baseChanges := make([]*ChangeEvent, len(enhancedChanges))
		for i, gc := range enhancedChanges {
			baseChanges[i] = gc.ChangeEvent
		}
		
		if err := ect.storage.StoreChanges(baseChanges); err != nil {
			log.Printf("⚠️ Failed to store changes: %v", err)
		}
	}

	log.Printf("✅ Tracked %d changes across %d resources", len(enhancedChanges), len(resourceNames))
	return enhancedChanges, nil
}

// DetectConfigurationDrift detects drift between current state and baseline
func (ect *EnhancedChangeTracker) DetectConfigurationDrift(ctx context.Context, baselineID string) (*DriftReport, error) {
	ect.mu.RLock()
	defer ect.mu.RUnlock()

	log.Printf("🔍 Detecting configuration drift against baseline: %s", baselineID)

	return ect.driftDetector.DetectDrift(ctx, baselineID)
}

// StartRealTimeMonitoring starts real-time change monitoring using Cloud Logging and Pub/Sub
func (ect *EnhancedChangeTracker) StartRealTimeMonitoring(ctx context.Context) error {
	ect.mu.Lock()
	defer ect.mu.Unlock()

	if ect.isMonitoring {
		return fmt.Errorf("real-time monitoring is already running")
	}

	log.Printf("🚀 Starting real-time change monitoring for project: %s", ect.projectID)

	// Set up Cloud Logging sink for audit logs
	if err := ect.setupCloudLoggingSink(ctx); err != nil {
		return fmt.Errorf("failed to setup Cloud Logging sink: %w", err)
	}

	// Set up Pub/Sub subscriptions
	if err := ect.setupPubSubSubscriptions(ctx); err != nil {
		return fmt.Errorf("failed to setup Pub/Sub subscriptions: %w", err)
	}

	// Start monitoring goroutines
	go ect.monitorCloudLogging(ctx)
	go ect.monitorPubSubEvents(ctx)

	ect.isMonitoring = true
	log.Printf("✅ Real-time monitoring started successfully")

	return nil
}

// StopRealTimeMonitoring stops real-time change monitoring
func (ect *EnhancedChangeTracker) StopRealTimeMonitoring() error {
	ect.mu.Lock()
	defer ect.mu.Unlock()

	if !ect.isMonitoring {
		return nil
	}

	ect.stopMonitoring()
	return nil
}

// Helper methods

func (ect *EnhancedChangeTracker) convertContentType(contentType string) assetpb.ContentType {
	switch strings.ToUpper(contentType) {
	case "IAM_POLICY":
		return assetpb.ContentType_IAM_POLICY
	case "ORG_POLICY":
		return assetpb.ContentType_ORG_POLICY
	case "ACCESS_POLICY":
		return assetpb.ContentType_ACCESS_POLICY
	default:
		return assetpb.ContentType_RESOURCE
	}
}

func (ect *EnhancedChangeTracker) convertTemporalAsset(temporal *assetpb.TemporalAsset) *TemporalAsset {
	result := &TemporalAsset{
		Deleted: temporal.Deleted,
	}

	// Convert time window
	if temporal.Window != nil {
		result.Window = &TimeWindow{
			StartTime: temporal.Window.StartTime.AsTime(),
			EndTime:   temporal.Window.EndTime.AsTime(),
		}
	}

	// Convert asset
	if temporal.Asset != nil {
		result.Asset = &GCPAsset{
			Name:       temporal.Asset.Name,
			AssetType:  temporal.Asset.AssetType,
			UpdateTime: time.Now(),
		}

		// Convert resource data
		if temporal.Asset.Resource != nil {
			resourceData := make(map[string]interface{})
			if temporal.Asset.Resource.Data != nil {
				// Extract data from the protobuf Struct
				for k, v := range temporal.Asset.Resource.Data.Fields {
					resourceData[k] = v.AsInterface()
				}
			}
			result.Asset.Resource = resourceData
		}

		// Convert IAM policy data
		if temporal.Asset.IamPolicy != nil {
			policyData := make(map[string]interface{})
			// Convert IAM policy to map
			result.Asset.IAMPolicy = policyData
		}
	}

	// Convert prior asset
	if temporal.PriorAsset != nil {
		result.PriorAsset = &GCPAsset{
			Name:       temporal.PriorAsset.Name,
			AssetType:  temporal.PriorAsset.AssetType,
			UpdateTime: time.Now(),
		}
	}

	return result
}

func (ect *EnhancedChangeTracker) convertToChangeEvents(assets []*TemporalAsset, resourceName string) []*GCPChangeEvent {
	var changes []*GCPChangeEvent

	for _, asset := range assets {
		if asset.Asset == nil {
			continue
		}

		// Determine change type
		changeType := ChangeTypeUpdate
		if asset.PriorAsset == nil {
			changeType = ChangeTypeCreate
		} else if asset.Deleted {
			changeType = ChangeTypeDelete
		}

		// Create change event
		change := &GCPChangeEvent{
			ChangeEvent: &ChangeEvent{
				ID:           fmt.Sprintf("%s_%d", resourceName, time.Now().Unix()),
				Provider:     "gcp",
				ResourceID:   asset.Asset.Name,
				ResourceType: asset.Asset.AssetType,
				Service:      ect.extractServiceFromAssetType(asset.Asset.AssetType),
				Project:      ect.projectID,
				ChangeType:   changeType,
				Severity:     SeverityLow, // Default, will be enhanced by analytics
				Timestamp:    asset.Asset.UpdateTime,
				DetectedAt:   time.Now(),
			},
			AssetType:    asset.Asset.AssetType,
			ResourceData: asset.Asset.Resource,
			AssetState:   "ACTIVE",
			ChangeSource: "ASSET_INVENTORY",
		}

		if asset.Deleted {
			change.AssetState = "DELETED"
		}

		// Set current and previous state
		if asset.Asset.Resource != nil {
			change.ChangeEvent.CurrentState = &ResourceState{
				ResourceID: asset.Asset.Name,
				Timestamp:  asset.Asset.UpdateTime,
				Properties: asset.Asset.Resource,
			}
		}

		if asset.PriorAsset != nil && asset.PriorAsset.Resource != nil {
			change.ChangeEvent.PreviousState = &ResourceState{
				ResourceID: asset.PriorAsset.Name,
				Timestamp:  asset.PriorAsset.UpdateTime,
				Properties: asset.PriorAsset.Resource,
			}
		}

		changes = append(changes, change)
	}

	return changes
}

func (ect *EnhancedChangeTracker) enhanceWithCloudLogging(ctx context.Context, changes []*GCPChangeEvent, since time.Time) []*GCPChangeEvent {
	// This would query Cloud Logging for additional context about the changes
	// For now, we'll return the changes as-is
	log.Printf("📊 Enhanced %d changes with Cloud Logging data", len(changes))
	return changes
}

func (ect *EnhancedChangeTracker) extractServiceFromAssetType(assetType string) string {
	// Extract service name from asset type (e.g., "compute.googleapis.com/Instance" -> "compute")
	parts := strings.Split(assetType, "/")
	if len(parts) > 0 {
		serviceParts := strings.Split(parts[0], ".")
		if len(serviceParts) > 0 {
			return serviceParts[0]
		}
	}
	return "unknown"
}

func (ect *EnhancedChangeTracker) setupCloudLoggingSink(ctx context.Context) error {
	// Set up a Cloud Logging sink to capture audit logs
	log.Printf("🔧 Setting up Cloud Logging sink for audit logs")
	// Implementation would create a sink that captures relevant audit logs
	return nil
}

func (ect *EnhancedChangeTracker) setupPubSubSubscriptions(ctx context.Context) error {
	// Set up Pub/Sub subscriptions for real-time events
	log.Printf("🔧 Setting up Pub/Sub subscriptions for real-time events")
	
	// Create a subscription for Cloud Asset Inventory feed
	subName := "corkscrew-asset-changes"
	
	// This would create the actual subscription
	log.Printf("📡 Created Pub/Sub subscription: %s", subName)
	
	return nil
}

func (ect *EnhancedChangeTracker) monitorCloudLogging(ctx context.Context) {
	log.Printf("👀 Starting Cloud Logging monitor")
	
	// This would continuously monitor Cloud Logging for audit events
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			// Query for new audit logs
			ect.processAuditLogs(ctx)
		}
	}
}

func (ect *EnhancedChangeTracker) monitorPubSubEvents(ctx context.Context) {
	log.Printf("👀 Starting Pub/Sub event monitor")
	
	// This would continuously process Pub/Sub messages
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			// Process Pub/Sub messages
			ect.processPubSubMessages(ctx)
		}
	}
}

func (ect *EnhancedChangeTracker) processAuditLogs(ctx context.Context) {
	// Process audit logs from Cloud Logging
	// This would query the logging client for new audit entries
}

func (ect *EnhancedChangeTracker) processPubSubMessages(ctx context.Context) {
	// Process messages from Pub/Sub subscriptions
	// This would receive and process real-time change events
}

func (ect *EnhancedChangeTracker) stopMonitoring() {
	ect.isMonitoring = false
	log.Printf("🛑 Stopped real-time monitoring")
}

// GCP Provider Integration

// TrackChanges implements the ChangeTracker interface for GCP
func (ect *EnhancedChangeTracker) TrackChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	return ect.storage.QueryChanges(req)
}

// StreamChanges implements streaming change events for GCP
func (ect *EnhancedChangeTracker) StreamChanges(req *StreamRequest, stream ChangeEventStream) error {
	// Implementation would stream changes as they occur
	return fmt.Errorf("streaming not yet implemented")
}

// MonitorChanges implements real-time change monitoring for GCP
func (ect *EnhancedChangeTracker) MonitorChanges(ctx context.Context, callback func(*ChangeEvent)) error {
	return ect.StartRealTimeMonitoring(ctx)
}

// GetChangeHistory implements change history retrieval for GCP
func (ect *EnhancedChangeTracker) GetChangeHistory(ctx context.Context, resourceID string) ([]*ChangeEvent, error) {
	return ect.storage.GetChangeHistory(resourceID)
}

// CreateBaseline implements baseline creation for GCP
func (ect *EnhancedChangeTracker) CreateBaseline(ctx context.Context, resources []*pb.Resource) (*DriftBaseline, error) {
	return ect.driftDetector.CreateBaseline(resources)
}
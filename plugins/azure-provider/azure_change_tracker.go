package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ChangeType represents the type of change that occurred
type ChangeType string

const (
	ChangeTypeCreate       ChangeType = "CREATE"
	ChangeTypeUpdate       ChangeType = "UPDATE"
	ChangeTypeDelete       ChangeType = "DELETE"
	ChangeTypePolicyChange ChangeType = "POLICY_CHANGE"
	ChangeTypeTagChange    ChangeType = "TAG_CHANGE"
	ChangeTypeStateChange  ChangeType = "STATE_CHANGE"
)

// ChangeSeverity represents the impact level of a change
type ChangeSeverity string

const (
	SeverityLow      ChangeSeverity = "LOW"
	SeverityMedium   ChangeSeverity = "MEDIUM"
	SeverityHigh     ChangeSeverity = "HIGH"
	SeverityCritical ChangeSeverity = "CRITICAL"
)

// AzureChangeTracker implements change tracking for Azure resources
type AzureChangeTracker struct {
	*BaseChangeTracker
	resourceGraphClient *armresourcegraph.Client
	credential         azcore.TokenCredential
	subscriptionIDs    []string
	tenantID          string
	config            *AzureChangeTrackerConfig
}

// AzureChangeTrackerConfig provides Azure-specific configuration
type AzureChangeTrackerConfig struct {
	SubscriptionIDs      []string      `json:"subscription_ids"`
	TenantID            string        `json:"tenant_id"`
	QueryInterval       time.Duration `json:"query_interval"`
	ChangeRetention     time.Duration `json:"change_retention"`
	EnableRealTime      bool          `json:"enable_real_time"`
	EventGridEndpoint   string        `json:"event_grid_endpoint,omitempty"`
	ActivityLogEnabled  bool          `json:"activity_log_enabled"`
	StorageConfig       StorageConfig `json:"storage_config"`
}

// StorageConfig defines storage configuration
type StorageConfig struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// AzureChangeEvent represents a change event from Azure Resource Graph
type AzureChangeEvent struct {
	ResourceID      string                 `json:"resourceId"`
	ResourceType    string                 `json:"resourceType"`
	ChangeType      string                 `json:"changeType"`
	Timestamp       time.Time              `json:"timestamp"`
	Properties      map[string]interface{} `json:"properties"`
	PreviousValue   interface{}            `json:"previousValue,omitempty"`
	NewValue        interface{}            `json:"newValue,omitempty"`
	SubscriptionID  string                 `json:"subscriptionId"`
	ResourceGroup   string                 `json:"resourceGroup"`
	Location        string                 `json:"location"`
	Tags            map[string]string      `json:"tags,omitempty"`
}

// NewAzureChangeTracker creates a new Azure change tracker
func NewAzureChangeTracker(ctx context.Context, config *AzureChangeTrackerConfig) (*AzureChangeTracker, error) {
	if config == nil {
		config = &AzureChangeTrackerConfig{
			QueryInterval:      5 * time.Minute,
			ChangeRetention:    365 * 24 * time.Hour,
			EnableRealTime:     true,
			ActivityLogEnabled: true,
			StorageConfig: StorageConfig{
				Type: "duckdb",
				Path: "./azure_changes.db",
			},
		}
	}

	// Initialize Azure credential
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	// Create Resource Graph client
	resourceGraphClient, err := armresourcegraph.NewClient(credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Resource Graph client: %w", err)
	}

	// Initialize storage
	storage, err := NewDuckDBChangeStorage(config.StorageConfig.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create base change tracker configuration
	baseConfig := &ChangeTrackerConfig{
		Provider:               "azure",
		EnableRealTimeMonitoring: config.EnableRealTime,
		ChangeRetention:        config.ChangeRetention,
		DriftCheckInterval:     1 * time.Hour,
		AlertingEnabled:        true,
		AnalyticsEnabled:       true,
		CacheEnabled:           true,
		CacheTTL:               5 * time.Minute,
		MaxConcurrentStreams:   100,
		BatchSize:              1000,
		MaxQueryTimeRange:      30 * 24 * time.Hour,
	}

	baseTracker := NewBaseChangeTracker("azure", storage, baseConfig)

	return &AzureChangeTracker{
		BaseChangeTracker:   baseTracker,
		resourceGraphClient: resourceGraphClient,
		credential:         credential,
		subscriptionIDs:    config.SubscriptionIDs,
		tenantID:          config.TenantID,
		config:            config,
	}, nil
}

// QueryChanges retrieves change events based on query parameters
func (act *AzureChangeTracker) QueryChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	if err := act.ValidateChangeQuery(req); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	log.Printf("Querying Azure changes for time range: %v to %v", req.StartTime, req.EndTime)

	// Query Azure Resource Graph for changes
	azureChanges, err := act.queryResourceGraphChanges(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query Resource Graph changes: %w", err)
	}

	// Convert Azure changes to universal format
	var changes []*ChangeEvent
	for _, azureChange := range azureChanges {
		change := act.convertAzureToChangeEvent(azureChange)
		if change != nil {
			// Analyze impact
			change.ImpactAssessment = act.AnalyzeChangeImpact(change)
			changes = append(changes, change)
		}
	}

	// Store changes for future queries
	if len(changes) > 0 {
		if err := act.storage.StoreChanges(changes); err != nil {
			log.Printf("Failed to store changes: %v", err)
		}
	}

	log.Printf("Retrieved %d Azure changes", len(changes))
	return changes, nil
}

// StreamChanges implements real-time change streaming
func (act *AzureChangeTracker) StreamChanges(req *StreamRequest, stream ChangeEventStream) error {
	if req == nil || req.Query == nil {
		return fmt.Errorf("stream request and query cannot be nil")
	}

	log.Printf("Starting Azure change stream for provider: %s", req.Query.Provider)

	// Create a ticker for periodic polling
	ticker := time.NewTicker(act.config.QueryInterval)
	defer ticker.Stop()

	lastQueryTime := time.Now().Add(-1 * time.Hour) // Start with last hour

	for {
		select {
		case <-stream.Context().Done():
			log.Printf("Azure change stream cancelled")
			return nil

		case <-ticker.C:
			// Query for new changes since last check
			query := &ChangeQuery{
				Provider:  "azure",
				StartTime: lastQueryTime,
				EndTime:   time.Now(),
				Limit:     1000,
			}

			changes, err := act.QueryChanges(stream.Context(), query)
			if err != nil {
				log.Printf("Failed to query changes for stream: %v", err)
				continue
			}

			// Send changes to stream
			for _, change := range changes {
				if err := stream.Send(change); err != nil {
					log.Printf("Failed to send change to stream: %v", err)
					return err
				}
			}

			lastQueryTime = time.Now()
		}
	}
}

// DetectDrift detects configuration drift against a baseline
func (act *AzureChangeTracker) DetectDrift(ctx context.Context, baseline *DriftBaseline) (*DriftReport, error) {
	if baseline == nil {
		return nil, fmt.Errorf("baseline cannot be nil")
	}

	log.Printf("Starting drift detection for baseline: %s", baseline.ID)

	report := &DriftReport{
		ID:              fmt.Sprintf("drift_%s_%d", baseline.ID, time.Now().Unix()),
		BaselineID:      baseline.ID,
		GeneratedAt:     time.Now(),
		TotalResources:  len(baseline.Resources),
		DriftedResources: 0,
		DriftItems:      []*DriftItem{},
		Summary: &DriftSummary{
			DriftByType:    make(map[string]int),
			DriftByService: make(map[string]int),
		},
	}

	// Query current state for all resources in baseline
	for resourceID, baselineState := range baseline.Resources {
		currentState, err := act.getCurrentResourceState(ctx, resourceID)
		if err != nil {
			log.Printf("Failed to get current state for resource %s: %v", resourceID, err)
			continue
		}

		if currentState == nil {
			// Resource was deleted
			report.DriftedResources++
			report.DriftItems = append(report.DriftItems, &DriftItem{
				ResourceID:      resourceID,
				ResourceType:    baselineState.Properties["resourceType"].(string),
				DriftType:       "DELETED",
				Severity:        SeverityHigh,
				Description:     "Resource was deleted since baseline",
				DetectedAt:      time.Now(),
				BaselineValue:   "EXISTS",
				CurrentValue:    "DELETED",
				Field:           "existence",
			})
			continue
		}

		// Compare states and detect drift
		driftItems := act.compareResourceStates(baselineState, currentState)
		if len(driftItems) > 0 {
			report.DriftedResources++
			for _, item := range driftItems {
				item.ResourceID = resourceID
				report.DriftItems = append(report.DriftItems, item)
				report.Summary.DriftByType[item.DriftType]++
				if service, ok := currentState.Properties["service"].(string); ok {
					report.Summary.DriftByService[service]++
				}
			}
		}
	}

	// Calculate compliance score
	if report.TotalResources > 0 {
		compliancePercentage := float64(report.TotalResources-report.DriftedResources) / float64(report.TotalResources) * 100
		report.Summary.ComplianceScore = compliancePercentage
	}

	log.Printf("Drift detection completed: %d/%d resources drifted", report.DriftedResources, report.TotalResources)
	return report, nil
}

// MonitorChanges starts continuous monitoring for changes
func (act *AzureChangeTracker) MonitorChanges(ctx context.Context, callback func(*ChangeEvent)) error {
	log.Printf("Starting Azure change monitoring")

	ticker := time.NewTicker(act.config.QueryInterval)
	defer ticker.Stop()

	lastCheck := time.Now().Add(-1 * time.Hour)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Azure change monitoring stopped")
			return nil

		case <-ticker.C:
			query := &ChangeQuery{
				Provider:  "azure",
				StartTime: lastCheck,
				EndTime:   time.Now(),
				Limit:     1000,
			}

			changes, err := act.QueryChanges(ctx, query)
			if err != nil {
				log.Printf("Failed to query changes during monitoring: %v", err)
				continue
			}

			for _, change := range changes {
				callback(change)
			}

			lastCheck = time.Now()
		}
	}
}

// GetChangeHistory retrieves change history for a specific resource
func (act *AzureChangeTracker) GetChangeHistory(ctx context.Context, resourceID string) ([]*ChangeEvent, error) {
	return act.storage.GetChangeHistory(resourceID)
}

// CreateBaseline creates a new drift baseline from current resources
func (act *AzureChangeTracker) CreateBaseline(ctx context.Context, resources []*pb.Resource) (*DriftBaseline, error) {
	if len(resources) == 0 {
		return nil, fmt.Errorf("no resources provided for baseline")
	}

	baseline := &DriftBaseline{
		ID:          fmt.Sprintf("azure_baseline_%d", time.Now().Unix()),
		Name:        fmt.Sprintf("Azure Baseline %s", time.Now().Format("2006-01-02 15:04:05")),
		Description: "Azure configuration baseline created by change tracker",
		Provider:    "azure",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Resources:   make(map[string]*ResourceState),
		Active:      true,
		Version:     "1.0",
	}

	// Convert resources to resource states
	for _, resource := range resources {
		state := &ResourceState{
			ResourceID:    resource.Id,
			Timestamp:     time.Now(),
			Properties:    make(map[string]interface{}),
			Tags:          make(map[string]string),
			Status:        "active",
			Configuration: make(map[string]interface{}),
		}

		// Extract properties from resource
		state.Properties["resourceType"] = resource.Type
		state.Properties["location"] = resource.Region
		state.Properties["service"] = resource.Service

		// Copy tags
		for k, v := range resource.Tags {
			state.Tags[k] = v
		}

		// Calculate checksum
		state.Checksum = act.CalculateResourceChecksum(state)

		baseline.Resources[resource.Id] = state
	}

	// Store baseline
	if err := act.storage.StoreBaseline(baseline); err != nil {
		return nil, fmt.Errorf("failed to store baseline: %w", err)
	}

	log.Printf("Created Azure baseline with %d resources", len(baseline.Resources))
	return baseline, nil
}

// Azure-specific helper methods

func (act *AzureChangeTracker) queryResourceGraphChanges(ctx context.Context, req *ChangeQuery) ([]*AzureChangeEvent, error) {
	// Build Resource Graph query for change data
	query := act.buildResourceGraphQuery(req)

	// Prepare query request
	queryRequest := armresourcegraph.QueryRequest{
		Query: &query,
	}

	// Add subscriptions if specified
	if len(act.subscriptionIDs) > 0 {
		subscriptions := make([]*string, len(act.subscriptionIDs))
		for i, sub := range act.subscriptionIDs {
			subscriptions[i] = &sub
		}
		queryRequest.Subscriptions = subscriptions
	}

	// Execute query
	result, err := act.resourceGraphClient.Resources(ctx, queryRequest, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Resource Graph query: %w", err)
	}

	// Parse results
	var changes []*AzureChangeEvent
	if result.Data != nil {
		if err := act.parseResourceGraphResults(result.Data, &changes); err != nil {
			return nil, fmt.Errorf("failed to parse query results: %w", err)
		}
	}

	return changes, nil
}

func (act *AzureChangeTracker) buildResourceGraphQuery(req *ChangeQuery) string {
	// Build KQL query for Azure Resource Graph
	// Note: This uses the resourcechanges table for change history
	query := "resourcechanges"

	var conditions []string

	// Time range filter
	if !req.StartTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp >= datetime('%s')", req.StartTime.Format(time.RFC3339)))
	}
	if !req.EndTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp <= datetime('%s')", req.EndTime.Format(time.RFC3339)))
	}

	// Resource filter
	if req.ResourceFilter != nil {
		rf := req.ResourceFilter

		if len(rf.ResourceIDs) > 0 {
			resourceList := "'" + strings.Join(rf.ResourceIDs, "','") + "'"
			conditions = append(conditions, fmt.Sprintf("resourceId in (%s)", resourceList))
		}

		if len(rf.ResourceTypes) > 0 {
			typeList := "'" + strings.Join(rf.ResourceTypes, "','") + "'"
			conditions = append(conditions, fmt.Sprintf("resourceType in (%s)", typeList))
		}
	}

	// Change type filter
	if len(req.ChangeTypes) > 0 {
		var azureChangeTypes []string
		for _, ct := range req.ChangeTypes {
			azureChangeTypes = append(azureChangeTypes, act.mapToAzureChangeType(ct))
		}
		typeList := "'" + strings.Join(azureChangeTypes, "','") + "'"
		conditions = append(conditions, fmt.Sprintf("changeType in (%s)", typeList))
	}

	// Add conditions to query
	if len(conditions) > 0 {
		query += " | where " + strings.Join(conditions, " and ")
	}

	// Add ordering and limit
	query += " | order by timestamp desc"
	if req.Limit > 0 {
		query += fmt.Sprintf(" | limit %d", req.Limit)
	}

	// Project required fields
	query += " | project resourceId, resourceType, changeType, timestamp, properties, previousValue, newValue, subscriptionId, resourceGroup, location, tags"

	return query
}

func (act *AzureChangeTracker) parseResourceGraphResults(data interface{}, changes *[]*AzureChangeEvent) error {
	// Convert interface{} to JSON and then to our struct
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal query results: %w", err)
	}

	var rawResults struct {
		Rows [][]interface{} `json:"rows"`
	}

	if err := json.Unmarshal(jsonData, &rawResults); err != nil {
		return fmt.Errorf("failed to unmarshal query results: %w", err)
	}

	// Parse each row into AzureChangeEvent
	for _, row := range rawResults.Rows {
		if len(row) >= 7 { // Ensure we have required fields
			change := &AzureChangeEvent{}

			// Parse fields based on projection order
			if resourceID, ok := row[0].(string); ok {
				change.ResourceID = resourceID
			}
			if resourceType, ok := row[1].(string); ok {
				change.ResourceType = resourceType
			}
			if changeType, ok := row[2].(string); ok {
				change.ChangeType = changeType
			}
			if timestampStr, ok := row[3].(string); ok {
				if timestamp, err := time.Parse(time.RFC3339, timestampStr); err == nil {
					change.Timestamp = timestamp
				}
			}

			// Parse properties, tags, etc. if available
			if len(row) > 4 && row[4] != nil {
				if props, ok := row[4].(map[string]interface{}); ok {
					change.Properties = props
				}
			}

			if len(row) > 9 && row[9] != nil {
				if tags, ok := row[9].(map[string]interface{}); ok {
					change.Tags = make(map[string]string)
					for k, v := range tags {
						if strVal, ok := v.(string); ok {
							change.Tags[k] = strVal
						}
					}
				}
			}

			*changes = append(*changes, change)
		}
	}

	return nil
}

func (act *AzureChangeTracker) convertAzureToChangeEvent(azureChange *AzureChangeEvent) *ChangeEvent {
	if azureChange == nil {
		return nil
	}

	// Generate unique change ID
	changeID := act.GenerateChangeID(azureChange.ResourceID, azureChange.Timestamp, act.mapFromAzureChangeType(azureChange.ChangeType))

	// Extract service from resource type
	service := act.extractServiceFromResourceType(azureChange.ResourceType)

	// Map severity based on change type and resource type
	severity := act.mapAzureSeverity(azureChange)

	change := &ChangeEvent{
		ID:           changeID,
		Provider:     "azure",
		ResourceID:   azureChange.ResourceID,
		ResourceName: act.extractResourceName(azureChange.ResourceID),
		ResourceType: azureChange.ResourceType,
		Service:      service,
		Project:      azureChange.SubscriptionID,
		Region:       azureChange.Location,
		ChangeType:   act.mapFromAzureChangeType(azureChange.ChangeType),
		Severity:     severity,
		Timestamp:    azureChange.Timestamp,
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"subscriptionId": azureChange.SubscriptionID,
			"resourceGroup":  azureChange.ResourceGroup,
			"location":       azureChange.Location,
			"provider":       "azure",
		},
	}

	// Set previous and current state if available
	if azureChange.PreviousValue != nil || azureChange.NewValue != nil {
		change.PreviousState = &ResourceState{
			ResourceID:    azureChange.ResourceID,
			Timestamp:     azureChange.Timestamp,
			Properties:    make(map[string]interface{}),
			Tags:          azureChange.Tags,
			Configuration: make(map[string]interface{}),
		}

		change.CurrentState = &ResourceState{
			ResourceID:    azureChange.ResourceID,
			Timestamp:     azureChange.Timestamp,
			Properties:    make(map[string]interface{}),
			Tags:          azureChange.Tags,
			Configuration: make(map[string]interface{}),
		}

		if azureChange.PreviousValue != nil {
			change.PreviousState.Properties["value"] = azureChange.PreviousValue
		}

		if azureChange.NewValue != nil {
			change.CurrentState.Properties["value"] = azureChange.NewValue
		}
	}

	return change
}

func (act *AzureChangeTracker) getCurrentResourceState(ctx context.Context, resourceID string) (*ResourceState, error) {
	// Query current resource state using Resource Graph
	query := fmt.Sprintf("resources | where id == '%s'", resourceID)

	queryRequest := armresourcegraph.QueryRequest{
		Query: &query,
	}

	result, err := act.resourceGraphClient.Resources(ctx, queryRequest, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query current resource state: %w", err)
	}

	if result.Data == nil {
		return nil, nil // Resource not found
	}

	// Parse result into ResourceState
	// This is a simplified implementation
	state := &ResourceState{
		ResourceID:    resourceID,
		Timestamp:     time.Now(),
		Properties:    make(map[string]interface{}),
		Tags:          make(map[string]string),
		Configuration: make(map[string]interface{}),
		Status:        "active",
	}

	return state, nil
}

func (act *AzureChangeTracker) compareResourceStates(baseline, current *ResourceState) []*DriftItem {
	var driftItems []*DriftItem

	// Compare properties
	for key, baselineValue := range baseline.Properties {
		if currentValue, exists := current.Properties[key]; !exists {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["resourceType"].(string),
				DriftType:      "MISSING_PROPERTY",
				Severity:       SeverityMedium,
				Description:    fmt.Sprintf("Property '%s' missing from current state", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   nil,
				Field:          key,
			})
		} else if !act.compareValues(baselineValue, currentValue) {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["resourceType"].(string),
				DriftType:      "PROPERTY_CHANGE",
				Severity:       SeverityMedium,
				Description:    fmt.Sprintf("Property '%s' value changed", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   currentValue,
				Field:          key,
			})
		}
	}

	// Compare tags
	for key, baselineValue := range baseline.Tags {
		if currentValue, exists := current.Tags[key]; !exists {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["resourceType"].(string),
				DriftType:      "MISSING_TAG",
				Severity:       SeverityLow,
				Description:    fmt.Sprintf("Tag '%s' missing from current state", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   nil,
				Field:          fmt.Sprintf("tags.%s", key),
			})
		} else if baselineValue != currentValue {
			driftItems = append(driftItems, &DriftItem{
				ResourceType:   baseline.Properties["resourceType"].(string),
				DriftType:      "TAG_CHANGE",
				Severity:       SeverityLow,
				Description:    fmt.Sprintf("Tag '%s' value changed", key),
				DetectedAt:     time.Now(),
				BaselineValue:  baselineValue,
				CurrentValue:   currentValue,
				Field:          fmt.Sprintf("tags.%s", key),
			})
		}
	}

	return driftItems
}

// Helper methods for Azure-specific logic

func (act *AzureChangeTracker) mapToAzureChangeType(changeType ChangeType) string {
	switch changeType {
	case ChangeTypeCreate:
		return "Create"
	case ChangeTypeUpdate:
		return "Update"
	case ChangeTypeDelete:
		return "Delete"
	default:
		return "Update"
	}
}

func (act *AzureChangeTracker) mapFromAzureChangeType(azureChangeType string) ChangeType {
	switch strings.ToLower(azureChangeType) {
	case "create":
		return ChangeTypeCreate
	case "update":
		return ChangeTypeUpdate
	case "delete":
		return ChangeTypeDelete
	default:
		return ChangeTypeUpdate
	}
}

func (act *AzureChangeTracker) extractServiceFromResourceType(resourceType string) string {
	// Extract Azure service from resource type (e.g., Microsoft.Compute/virtualMachines -> Compute)
	parts := strings.Split(resourceType, "/")
	if len(parts) >= 1 {
		serviceParts := strings.Split(parts[0], ".")
		if len(serviceParts) >= 2 {
			return serviceParts[1] // Return the service part (e.g., "Compute")
		}
	}
	return "Unknown"
}

func (act *AzureChangeTracker) extractResourceName(resourceID string) string {
	// Extract resource name from Azure resource ID
	parts := strings.Split(resourceID, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

func (act *AzureChangeTracker) mapAzureSeverity(azureChange *AzureChangeEvent) ChangeSeverity {
	// Map severity based on resource type and change type
	switch azureChange.ChangeType {
	case "Delete":
		return SeverityHigh
	case "Create":
		return SeverityMedium
	case "Update":
		// Check if it's a critical resource type
		if act.isCriticalResourceType(azureChange.ResourceType) {
			return SeverityHigh
		}
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func (act *AzureChangeTracker) isCriticalResourceType(resourceType string) bool {
	criticalTypes := []string{
		"Microsoft.Compute/virtualMachines",
		"Microsoft.Sql/servers",
		"Microsoft.Storage/storageAccounts",
		"Microsoft.KeyVault/vaults",
		"Microsoft.Network/loadBalancers",
		"Microsoft.Network/applicationGateways",
	}

	for _, criticalType := range criticalTypes {
		if strings.Contains(resourceType, criticalType) {
			return true
		}
	}

	return false
}

func (act *AzureChangeTracker) compareValues(a, b interface{}) bool {
	// Simple value comparison - in production this would be more sophisticated
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
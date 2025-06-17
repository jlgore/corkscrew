package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailTypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	configTypes "github.com/aws/aws-sdk-go-v2/service/configservice/types"
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

// AWSChangeTracker implements change tracking for AWS resources
type AWSChangeTracker struct {
	*BaseChangeTracker
	configClient    *configservice.Client
	cloudtrailClient *cloudtrail.Client
	awsConfig       aws.Config
	accountIDs      []string
	regions         []string
	config          *AWSChangeTrackerConfig
}

// AWSChangeTrackerConfig provides AWS-specific configuration
type AWSChangeTrackerConfig struct {
	AccountIDs        []string      `json:"account_ids"`
	Regions           []string      `json:"regions"`
	QueryInterval     time.Duration `json:"query_interval"`
	ChangeRetention   time.Duration `json:"change_retention"`
	EnableRealTime    bool          `json:"enable_real_time"`
	CloudTrailEnabled bool          `json:"cloudtrail_enabled"`
	ConfigEnabled     bool          `json:"config_enabled"`
	StorageConfig     StorageConfig `json:"storage_config"`
}

// StorageConfig defines storage configuration
type StorageConfig struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// AWSConfigChangeEvent represents a change event from AWS Config
type AWSConfigChangeEvent struct {
	ResourceID           string                 `json:"resourceId"`
	ResourceType         string                 `json:"resourceType"`
	ResourceName         string                 `json:"resourceName"`
	AwsRegion           string                 `json:"awsRegion"`
	AvailabilityZone    string                 `json:"availabilityZone"`
	ConfigurationItemCaptureTime time.Time     `json:"configurationItemCaptureTime"`
	ConfigurationItemStatus      string        `json:"configurationItemStatus"`
	ConfigurationStateId         string        `json:"configurationStateId"`
	AccountId           string                 `json:"accountId"`
	Configuration       map[string]interface{} `json:"configuration"`
	Tags                map[string]string      `json:"tags"`
	RelatedEvents       []string               `json:"relatedEvents"`
}

// AWSCloudTrailEvent represents a change event from CloudTrail
type AWSCloudTrailEvent struct {
	EventID       string                 `json:"eventId"`
	EventName     string                 `json:"eventName"`
	EventSource   string                 `json:"eventSource"`
	EventTime     time.Time              `json:"eventTime"`
	AwsRegion     string                 `json:"awsRegion"`
	UserIdentity  map[string]interface{} `json:"userIdentity"`
	RequestParameters map[string]interface{} `json:"requestParameters"`
	ResponseElements  map[string]interface{} `json:"responseElements"`
	Resources     []CloudTrailResource   `json:"resources"`
	ErrorCode     string                 `json:"errorCode,omitempty"`
	ErrorMessage  string                 `json:"errorMessage,omitempty"`
}

// CloudTrailResource represents a resource referenced in CloudTrail
type CloudTrailResource struct {
	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`
	AccountID    string `json:"accountId"`
}

// NewAWSChangeTracker creates a new AWS change tracker
func NewAWSChangeTracker(ctx context.Context, changeConfig *AWSChangeTrackerConfig) (*AWSChangeTracker, error) {
	if changeConfig == nil {
		changeConfig = &AWSChangeTrackerConfig{
			QueryInterval:     5 * time.Minute,
			ChangeRetention:   365 * 24 * time.Hour,
			EnableRealTime:    true,
			CloudTrailEnabled: true,
			ConfigEnabled:     true,
			Regions:          []string{"us-east-1"},
			StorageConfig: StorageConfig{
				Type: "duckdb",
				Path: "./aws_changes.db",
			},
		}
	}

	// Load AWS configuration
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create AWS service clients
	configClient := configservice.NewFromConfig(awsConfig)
	cloudtrailClient := cloudtrail.NewFromConfig(awsConfig)

	// Initialize storage
	storage, err := NewDuckDBChangeStorage(changeConfig.StorageConfig.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create base change tracker configuration
	baseConfig := &ChangeTrackerConfig{
		Provider:               "aws",
		EnableRealTimeMonitoring: changeConfig.EnableRealTime,
		ChangeRetention:        changeConfig.ChangeRetention,
		DriftCheckInterval:     1 * time.Hour,
		AlertingEnabled:        true,
		AnalyticsEnabled:       true,
		CacheEnabled:           true,
		CacheTTL:               5 * time.Minute,
		MaxConcurrentStreams:   100,
		BatchSize:              1000,
		MaxQueryTimeRange:      30 * 24 * time.Hour,
	}

	baseTracker := NewBaseChangeTracker("aws", storage, baseConfig)

	return &AWSChangeTracker{
		BaseChangeTracker: baseTracker,
		configClient:     configClient,
		cloudtrailClient: cloudtrailClient,
		awsConfig:        awsConfig,
		accountIDs:       changeConfig.AccountIDs,
		regions:          changeConfig.Regions,
		config:           changeConfig,
	}, nil
}

// QueryChanges retrieves change events based on query parameters
func (act *AWSChangeTracker) QueryChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	if err := act.ValidateChangeQuery(req); err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	log.Printf("Querying AWS changes for time range: %v to %v", req.StartTime, req.EndTime)

	var allChanges []*ChangeEvent

	// Query AWS Config for configuration changes if enabled
	if act.config.ConfigEnabled {
		configChanges, err := act.queryConfigChanges(ctx, req)
		if err != nil {
			log.Printf("Failed to query Config changes: %v", err)
		} else {
			allChanges = append(allChanges, configChanges...)
		}
	}

	// Query CloudTrail for API-level changes if enabled
	if act.config.CloudTrailEnabled {
		cloudtrailChanges, err := act.queryCloudTrailChanges(ctx, req)
		if err != nil {
			log.Printf("Failed to query CloudTrail changes: %v", err)
		} else {
			allChanges = append(allChanges, cloudtrailChanges...)
		}
	}

	// Store changes for future queries
	if len(allChanges) > 0 {
		if err := act.storage.StoreChanges(allChanges); err != nil {
			log.Printf("Failed to store changes: %v", err)
		}
	}

	log.Printf("Retrieved %d AWS changes", len(allChanges))
	return allChanges, nil
}

// StreamChanges implements real-time change streaming
func (act *AWSChangeTracker) StreamChanges(req *StreamRequest, stream ChangeEventStream) error {
	if req == nil || req.Query == nil {
		return fmt.Errorf("stream request and query cannot be nil")
	}

	log.Printf("Starting AWS change stream for provider: %s", req.Query.Provider)

	// Create a ticker for periodic polling
	ticker := time.NewTicker(act.config.QueryInterval)
	defer ticker.Stop()

	lastQueryTime := time.Now().Add(-1 * time.Hour) // Start with last hour

	for {
		select {
		case <-stream.Context().Done():
			log.Printf("AWS change stream cancelled")
			return nil

		case <-ticker.C:
			// Query for new changes since last check
			query := &ChangeQuery{
				Provider:  "aws",
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
func (act *AWSChangeTracker) DetectDrift(ctx context.Context, baseline *DriftBaseline) (*DriftReport, error) {
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
		resourceType := ""
		if rt, ok := baselineState.Properties["resourceType"].(string); ok {
			resourceType = rt
		}
		currentState, err := act.getCurrentResourceState(ctx, resourceID, resourceType)
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
func (act *AWSChangeTracker) MonitorChanges(ctx context.Context, callback func(*ChangeEvent)) error {
	log.Printf("Starting AWS change monitoring")

	ticker := time.NewTicker(act.config.QueryInterval)
	defer ticker.Stop()

	lastCheck := time.Now().Add(-1 * time.Hour)

	for {
		select {
		case <-ctx.Done():
			log.Printf("AWS change monitoring stopped")
			return nil

		case <-ticker.C:
			query := &ChangeQuery{
				Provider:  "aws",
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
func (act *AWSChangeTracker) GetChangeHistory(ctx context.Context, resourceID string) ([]*ChangeEvent, error) {
	return act.storage.GetChangeHistory(resourceID)
}

// CreateBaseline creates a new drift baseline from current resources
func (act *AWSChangeTracker) CreateBaseline(ctx context.Context, resources []*pb.Resource) (*DriftBaseline, error) {
	if len(resources) == 0 {
		return nil, fmt.Errorf("no resources provided for baseline")
	}

	baseline := &DriftBaseline{
		ID:          fmt.Sprintf("aws_baseline_%d", time.Now().Unix()),
		Name:        fmt.Sprintf("AWS Baseline %s", time.Now().Format("2006-01-02 15:04:05")),
		Description: "AWS configuration baseline created by change tracker",
		Provider:    "aws",
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
		state.Checksum = act.CalculateResourceChecksum(state)

		baseline.Resources[resource.Id] = state
	}

	// Store baseline
	if err := act.storage.StoreBaseline(baseline); err != nil {
		return nil, fmt.Errorf("failed to store baseline: %w", err)
	}

	log.Printf("Created AWS baseline with %d resources", len(baseline.Resources))
	return baseline, nil
}

// AWS-specific helper methods

func (act *AWSChangeTracker) queryConfigChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	var allChanges []*ChangeEvent

	// Iterate through specified regions
	regions := act.regions
	if len(regions) == 0 {
		regions = []string{"us-east-1"} // Default region
	}

	for _, region := range regions {
		// Create region-specific config client
		regionConfig := act.awsConfig.Copy()
		regionConfig.Region = region
		configClient := configservice.NewFromConfig(regionConfig)

		// Query configuration history
		input := &configservice.GetResourceConfigHistoryInput{
			LaterTime:   &req.EndTime,
			EarlierTime: &req.StartTime,
			Limit:       100,
		}

		// Add resource filters if specified
		if req.ResourceFilter != nil && len(req.ResourceFilter.ResourceIDs) > 0 {
			for _, resourceID := range req.ResourceFilter.ResourceIDs {
				input.ResourceId = aws.String(resourceID)
				
				result, err := configClient.GetResourceConfigHistory(ctx, input)
				if err != nil {
					log.Printf("Failed to get config history for resource %s: %v", resourceID, err)
					continue
				}

				// Convert config items to change events
				for _, item := range result.ConfigurationItems {
					change := act.convertConfigItemToChangeEvent(item, region)
					if change != nil {
						change.ImpactAssessment = act.AnalyzeChangeImpact(change)
						allChanges = append(allChanges, change)
					}
				}
			}
		} else {
			// Query all resources if no specific filter
			// This is a simplified approach - in production you'd paginate through all resources
			paginator := configservice.NewGetResourceConfigHistoryPaginator(configClient, input)
			for paginator.HasMorePages() {
				result, err := paginator.NextPage(ctx)
				if err != nil {
					log.Printf("Failed to get config history page: %v", err)
					break
				}

				for _, item := range result.ConfigurationItems {
					change := act.convertConfigItemToChangeEvent(item, region)
					if change != nil {
						change.ImpactAssessment = act.AnalyzeChangeImpact(change)
						allChanges = append(allChanges, change)
					}
				}
			}
		}
	}

	return allChanges, nil
}

func (act *AWSChangeTracker) queryCloudTrailChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error) {
	var allChanges []*ChangeEvent

	// CloudTrail lookup events
	input := &cloudtrail.LookupEventsInput{
		StartTime: &req.StartTime,
		EndTime:   &req.EndTime,
	}

	// Add resource name filters if specified
	if req.ResourceFilter != nil && len(req.ResourceFilter.ResourceIDs) > 0 {
		lookupAttributes := make([]cloudtrailTypes.LookupAttribute, 0)
		for _, resourceID := range req.ResourceFilter.ResourceIDs {
			lookupAttributes = append(lookupAttributes, cloudtrailTypes.LookupAttribute{
				AttributeKey:   cloudtrailTypes.LookupAttributeKeyResourceName,
				AttributeValue: aws.String(resourceID),
			})
		}
		input.LookupAttributes = lookupAttributes
	}

	// Query CloudTrail events
	paginator := cloudtrail.NewLookupEventsPaginator(act.cloudtrailClient, input)
	for paginator.HasMorePages() {
		result, err := paginator.NextPage(ctx)
		if err != nil {
			log.Printf("Failed to lookup CloudTrail events: %v", err)
			break
		}

		for _, event := range result.Events {
			change := act.convertCloudTrailEventToChangeEvent(event)
			if change != nil {
				change.ImpactAssessment = act.AnalyzeChangeImpact(change)
				allChanges = append(allChanges, change)
			}
		}
	}

	return allChanges, nil
}

func (act *AWSChangeTracker) convertConfigItemToChangeEvent(item configTypes.ConfigurationItem, region string) *ChangeEvent {
	if item.ResourceId == nil || item.ResourceType == "" {
		return nil
	}

	// Determine change type based on configuration item status
	changeType := ChangeTypeUpdate
	if item.ConfigurationItemStatus == configTypes.ConfigurationItemStatusResourceDiscovered {
		changeType = ChangeTypeCreate
	} else if item.ConfigurationItemStatus == configTypes.ConfigurationItemStatusResourceDeleted {
		changeType = ChangeTypeDelete
	}

	// Generate unique change ID
	timestamp := time.Now()
	if item.ConfigurationItemCaptureTime != nil {
		timestamp = *item.ConfigurationItemCaptureTime
	}
	changeID := act.GenerateChangeID(*item.ResourceId, timestamp, changeType)

	// Extract service from resource type
	service := act.extractServiceFromResourceType(string(item.ResourceType))

	// Map severity
	severity := act.mapAWSConfigSeverity(item)

	change := &ChangeEvent{
		ID:           changeID,
		Provider:     "aws",
		ResourceID:   *item.ResourceId,
		ResourceName: act.extractResourceName(*item.ResourceId),
		ResourceType: string(item.ResourceType),
		Service:      service,
		Project:      aws.ToString(item.AccountId),
		Region:       region,
		ChangeType:   changeType,
		Severity:     severity,
		Timestamp:    timestamp,
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"accountId":               aws.ToString(item.AccountId),
			"availabilityZone":        aws.ToString(item.AvailabilityZone),
			"configurationStateId":    aws.ToString(item.ConfigurationStateId),
			"configurationItemStatus": string(item.ConfigurationItemStatus),
			"provider":                "aws",
			"source":                  "config",
		},
	}

	// Set resource state
	if item.Configuration != nil {
		change.CurrentState = &ResourceState{
			ResourceID:    *item.ResourceId,
			Timestamp:     timestamp,
			Properties:    make(map[string]interface{}),
			Tags:          make(map[string]string),
			Configuration: make(map[string]interface{}),
			Status:        string(item.ConfigurationItemStatus),
		}

		// Parse configuration
		if item.Configuration != nil {
			if configStr := aws.ToString(item.Configuration); configStr != "" {
				var config map[string]interface{}
				if err := json.Unmarshal([]byte(configStr), &config); err == nil {
					change.CurrentState.Configuration = config
				}
			}
		}

		// Add tags if available
		if item.Tags != nil {
			for k, v := range item.Tags {
				change.CurrentState.Tags[k] = v
			}
		}
	}

	return change
}

func (act *AWSChangeTracker) convertCloudTrailEventToChangeEvent(event cloudtrailTypes.Event) *ChangeEvent {
	if event.EventId == nil || event.EventName == nil {
		return nil
	}

	// Map CloudTrail event names to change types
	changeType := act.mapCloudTrailEventNameToChangeType(aws.ToString(event.EventName))

	// Generate unique change ID
	timestamp := time.Now()
	if event.EventTime != nil {
		timestamp = *event.EventTime
	}

	// Extract resource information
	resourceID := ""
	resourceType := ""
	service := ""

	if len(event.Resources) > 0 {
		resource := event.Resources[0]
		if resource.ResourceName != nil {
			resourceID = *resource.ResourceName
		}
		if resource.ResourceType != nil {
			resourceType = *resource.ResourceType
		}
	}

	if event.EventSource != nil {
		service = act.extractServiceFromEventSource(*event.EventSource)
	}

	if resourceID == "" {
		resourceID = *event.EventId // Fallback to event ID
	}

	changeID := act.GenerateChangeID(resourceID, timestamp, changeType)

	// Map severity
	severity := act.mapCloudTrailSeverity(event)

	// Extract region from CloudTrail event JSON
	region := act.extractRegionFromCloudTrailEvent(event)

	change := &ChangeEvent{
		ID:           changeID,
		Provider:     "aws",
		ResourceID:   resourceID,
		ResourceName: act.extractResourceName(resourceID),
		ResourceType: resourceType,
		Service:      service,
		Region:       region,
		ChangeType:   changeType,
		Severity:     severity,
		Timestamp:    timestamp,
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"eventName":   aws.ToString(event.EventName),
			"eventSource": aws.ToString(event.EventSource),
			"provider":    "aws",
			"source":      "cloudtrail",
		},
	}

	// Extract additional fields from CloudTrail event JSON
	act.enrichChangeMetadataFromCloudTrailEvent(change, event)

	return change
}

func (act *AWSChangeTracker) getCurrentResourceState(ctx context.Context, resourceID, resourceType string) (*ResourceState, error) {
	// Query current resource configuration using AWS Config
	input := &configservice.GetResourceConfigHistoryInput{
		ResourceId:   aws.String(resourceID),
		ResourceType: configTypes.ResourceType(resourceType),
		Limit:        1,
	}

	result, err := act.configClient.GetResourceConfigHistory(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to get current resource state: %w", err)
	}

	if len(result.ConfigurationItems) == 0 {
		return nil, nil // Resource not found
	}

	item := result.ConfigurationItems[0]
	
	state := &ResourceState{
		ResourceID:    resourceID,
		Timestamp:     time.Now(),
		Properties:    make(map[string]interface{}),
		Tags:          make(map[string]string),
		Configuration: make(map[string]interface{}),
		Status:        string(item.ConfigurationItemStatus),
	}

	// Parse configuration
	if item.Configuration != nil {
		var config map[string]interface{}
		if err := json.Unmarshal([]byte(*item.Configuration), &config); err == nil {
			state.Configuration = config
		}
	}

	// Add tags
	if item.Tags != nil {
		for k, v := range item.Tags {
			state.Tags[k] = v
		}
	}

	return state, nil
}

func (act *AWSChangeTracker) compareResourceStates(baseline, current *ResourceState) []*DriftItem {
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

// Helper methods for AWS-specific logic

func (act *AWSChangeTracker) extractServiceFromResourceType(resourceType string) string {
	// Extract AWS service from resource type (e.g., AWS::EC2::Instance -> EC2)
	parts := strings.Split(resourceType, "::")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "Unknown"
}

func (act *AWSChangeTracker) extractServiceFromEventSource(eventSource string) string {
	// Extract service from event source (e.g., ec2.amazonaws.com -> EC2)
	parts := strings.Split(eventSource, ".")
	if len(parts) > 0 {
		return strings.ToUpper(parts[0])
	}
	return "Unknown"
}

func (act *AWSChangeTracker) extractResourceName(resourceID string) string {
	// Extract resource name from AWS resource ID/ARN
	if strings.Contains(resourceID, ":") {
		// It's an ARN
		parts := strings.Split(resourceID, ":")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	} else if strings.Contains(resourceID, "/") {
		// It's a path-like ID
		parts := strings.Split(resourceID, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	return resourceID
}

func (act *AWSChangeTracker) mapAWSConfigSeverity(item configTypes.ConfigurationItem) ChangeSeverity {
	// Map severity based on resource type and status
	switch item.ConfigurationItemStatus {
	case configTypes.ConfigurationItemStatusResourceDeleted:
		return SeverityHigh
	case configTypes.ConfigurationItemStatusResourceDiscovered:
		return SeverityMedium
	default:
		// Check if it's a critical resource type
		if act.isCriticalAWSResourceType(string(item.ResourceType)) {
			return SeverityHigh
		}
		return SeverityMedium
	}
}

func (act *AWSChangeTracker) mapCloudTrailSeverity(event cloudtrailTypes.Event) ChangeSeverity {
	// Map severity based on event type and potential impact
	// Check for errors in the CloudTrail event JSON
	if act.hasCloudTrailError(event) {
		return SeverityHigh // Errors are high severity
	}

	eventName := aws.ToString(event.EventName)
	
	// High-impact operations
	highImpactEvents := []string{
		"DeleteInstance", "TerminateInstances", "DeleteVolume", "DeleteSnapshot",
		"DeleteDBInstance", "DeleteCluster", "DeleteBucket", "DeleteKey",
		"DeleteRole", "DeletePolicy", "DeleteUser", "DetachUserPolicy",
	}

	for _, highEvent := range highImpactEvents {
		if strings.Contains(eventName, highEvent) {
			return SeverityHigh
		}
	}

	// Medium-impact operations
	mediumImpactEvents := []string{
		"Create", "Run", "Launch", "Start", "Stop", "Reboot", "Modify", "Update",
		"Attach", "Detach", "Associate", "Disassociate",
	}

	for _, medEvent := range mediumImpactEvents {
		if strings.Contains(eventName, medEvent) {
			return SeverityMedium
		}
	}

	return SeverityLow
}

func (act *AWSChangeTracker) mapCloudTrailEventNameToChangeType(eventName string) ChangeType {
	eventNameLower := strings.ToLower(eventName)
	
	if strings.Contains(eventNameLower, "create") || strings.Contains(eventNameLower, "run") || 
	   strings.Contains(eventNameLower, "launch") || strings.Contains(eventNameLower, "start") {
		return ChangeTypeCreate
	} else if strings.Contains(eventNameLower, "delete") || strings.Contains(eventNameLower, "terminate") ||
			  strings.Contains(eventNameLower, "stop") {
		return ChangeTypeDelete
	} else if strings.Contains(eventNameLower, "policy") || strings.Contains(eventNameLower, "permission") {
		return ChangeTypePolicyChange
	} else if strings.Contains(eventNameLower, "tag") {
		return ChangeTypeTagChange
	} else {
		return ChangeTypeUpdate
	}
}

func (act *AWSChangeTracker) isCriticalAWSResourceType(resourceType string) bool {
	criticalTypes := []string{
		"AWS::EC2::Instance",
		"AWS::RDS::DBInstance",
		"AWS::S3::Bucket",
		"AWS::IAM::Role",
		"AWS::IAM::Policy",
		"AWS::KMS::Key",
		"AWS::ELB::LoadBalancer",
		"AWS::ElasticLoadBalancingV2::LoadBalancer",
	}

	for _, criticalType := range criticalTypes {
		if resourceType == criticalType {
			return true
		}
	}

	return false
}

func (act *AWSChangeTracker) compareValues(a, b interface{}) bool {
	// Simple value comparison - in production this would be more sophisticated
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// extractRegionFromCloudTrailEvent extracts the AWS region from the CloudTrail event JSON
func (act *AWSChangeTracker) extractRegionFromCloudTrailEvent(event cloudtrailTypes.Event) string {
	// First, try to parse the CloudTrailEvent JSON to extract awsRegion
	if event.CloudTrailEvent != nil {
		var eventData map[string]interface{}
		if err := json.Unmarshal([]byte(*event.CloudTrailEvent), &eventData); err == nil {
			if region, ok := eventData["awsRegion"].(string); ok && region != "" {
				return region
			}
		}
	}

	// Fallback to the configured AWS region from the client
	return act.awsConfig.Region
}

// enrichChangeMetadataFromCloudTrailEvent extracts additional fields from the CloudTrail event JSON
func (act *AWSChangeTracker) enrichChangeMetadataFromCloudTrailEvent(change *ChangeEvent, event cloudtrailTypes.Event) {
	if event.CloudTrailEvent == nil {
		return
	}

	var eventData map[string]interface{}
	if err := json.Unmarshal([]byte(*event.CloudTrailEvent), &eventData); err != nil {
		return
	}

	// Extract common CloudTrail fields
	if userIdentity, ok := eventData["userIdentity"]; ok {
		change.ChangeMetadata["userIdentity"] = userIdentity
	}
	if sourceIPAddress, ok := eventData["sourceIPAddress"].(string); ok {
		change.ChangeMetadata["sourceIPAddress"] = sourceIPAddress
	}
	if userAgent, ok := eventData["userAgent"].(string); ok {
		change.ChangeMetadata["userAgent"] = userAgent
	}
	if errorCode, ok := eventData["errorCode"].(string); ok {
		change.ChangeMetadata["errorCode"] = errorCode
	}
	if errorMessage, ok := eventData["errorMessage"].(string); ok {
		change.ChangeMetadata["errorMessage"] = errorMessage
	}
	if requestID, ok := eventData["requestID"].(string); ok {
		change.ChangeMetadata["requestID"] = requestID
	}
	if requestParameters, ok := eventData["requestParameters"]; ok {
		change.ChangeMetadata["requestParameters"] = requestParameters
	}
	if responseElements, ok := eventData["responseElements"]; ok {
		change.ChangeMetadata["responseElements"] = responseElements
	}
}

// hasCloudTrailError checks if the CloudTrail event contains error information
func (act *AWSChangeTracker) hasCloudTrailError(event cloudtrailTypes.Event) bool {
	if event.CloudTrailEvent == nil {
		return false
	}

	var eventData map[string]interface{}
	if err := json.Unmarshal([]byte(*event.CloudTrailEvent), &eventData); err != nil {
		return false
	}

	// Check for error code in the event data
	if errorCode, ok := eventData["errorCode"].(string); ok && errorCode != "" {
		return true
	}

	return false
}
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

// ChangeTracker provides unified change tracking across cloud providers
type ChangeTracker interface {
	QueryChanges(ctx context.Context, req *ChangeQuery) ([]*ChangeEvent, error)
	StreamChanges(req *StreamRequest, stream ChangeEventStream) error
	DetectDrift(ctx context.Context, baseline *DriftBaseline) (*DriftReport, error)
	MonitorChanges(ctx context.Context, callback func(*ChangeEvent)) error
	GetChangeHistory(ctx context.Context, resourceID string) ([]*ChangeEvent, error)
	CreateBaseline(ctx context.Context, resources []*pb.Resource) (*DriftBaseline, error)
}

// ChangeEvent represents a universal change event structure
type ChangeEvent struct {
	ID               string                 `json:"id"`
	Provider         string                 `json:"provider"`
	ResourceID       string                 `json:"resource_id"`
	ResourceName     string                 `json:"resource_name"`
	ResourceType     string                 `json:"resource_type"`
	Service          string                 `json:"service"`
	Project          string                 `json:"project,omitempty"`
	Region           string                 `json:"region,omitempty"`
	ChangeType       ChangeType             `json:"change_type"`
	Severity         ChangeSeverity         `json:"severity"`
	Timestamp        time.Time              `json:"timestamp"`
	DetectedAt       time.Time              `json:"detected_at"`
	PreviousState    *ResourceState         `json:"previous_state,omitempty"`
	CurrentState     *ResourceState         `json:"current_state,omitempty"`
	ChangedFields    []string               `json:"changed_fields"`
	ChangeMetadata   map[string]interface{} `json:"change_metadata"`
	ImpactAssessment *ImpactAssessment      `json:"impact_assessment,omitempty"`
	ComplianceImpact *ComplianceImpact      `json:"compliance_impact,omitempty"`
	RelatedChanges   []string               `json:"related_changes,omitempty"`
}

// ResourceState represents the state of a resource at a point in time
type ResourceState struct {
	ResourceID   string                 `json:"resource_id"`
	Timestamp    time.Time              `json:"timestamp"`
	Properties   map[string]interface{} `json:"properties"`
	Tags         map[string]string      `json:"tags"`
	IAMPolicies  []IAMPolicy            `json:"iam_policies,omitempty"`
	Status       string                 `json:"status"`
	Configuration map[string]interface{} `json:"configuration"`
	Checksum     string                 `json:"checksum"`
}

// ChangeQuery defines parameters for querying changes
type ChangeQuery struct {
	Provider       string        `json:"provider,omitempty"`
	ResourceFilter *ResourceFilter `json:"resource_filter,omitempty"`
	ChangeTypes    []ChangeType    `json:"change_types,omitempty"`
	Severities     []ChangeSeverity `json:"severities,omitempty"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        time.Time       `json:"end_time"`
	Limit          int             `json:"limit,omitempty"`
	Offset         int             `json:"offset,omitempty"`
	SortBy         string          `json:"sort_by,omitempty"`
	SortOrder      string          `json:"sort_order,omitempty"`
}

// ResourceFilter defines resource filtering criteria
type ResourceFilter struct {
	ResourceIDs   []string          `json:"resource_ids,omitempty"`
	ResourceTypes []string          `json:"resource_types,omitempty"`
	Services      []string          `json:"services,omitempty"`
	Projects      []string          `json:"projects,omitempty"`
	Regions       []string          `json:"regions,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
}

// StreamRequest defines parameters for streaming changes
type StreamRequest struct {
	Query        *ChangeQuery `json:"query"`
	BufferSize   int          `json:"buffer_size,omitempty"`
	BatchTimeout time.Duration `json:"batch_timeout,omitempty"`
}

// ChangeEventStream interface for streaming change events
type ChangeEventStream interface {
	Send(*ChangeEvent) error
	Context() context.Context
}

// ImpactAssessment provides analysis of change impact
type ImpactAssessment struct {
	SecurityImpact    SecurityImpact    `json:"security_impact"`
	CostImpact        CostImpact        `json:"cost_impact"`
	PerformanceImpact PerformanceImpact `json:"performance_impact"`
	AvailabilityImpact AvailabilityImpact `json:"availability_impact"`
	RiskScore         float64           `json:"risk_score"`
	Recommendations   []string          `json:"recommendations"`
}

// SecurityImpact assesses security implications of changes
type SecurityImpact struct {
	Level           ChangeSeverity `json:"level"`
	IAMChanges      bool          `json:"iam_changes"`
	NetworkChanges  bool          `json:"network_changes"`
	EncryptionChanges bool        `json:"encryption_changes"`
	PublicAccess    bool          `json:"public_access"`
	Vulnerabilities []string      `json:"vulnerabilities,omitempty"`
}

// CostImpact assesses financial implications
type CostImpact struct {
	Level            ChangeSeverity `json:"level"`
	EstimatedChange  float64       `json:"estimated_change"`
	Currency         string        `json:"currency"`
	CostDrivers      []string      `json:"cost_drivers"`
	OptimizationTips []string      `json:"optimization_tips"`
}

// PerformanceImpact assesses performance implications
type PerformanceImpact struct {
	Level           ChangeSeverity `json:"level"`
	LatencyImpact   string        `json:"latency_impact"`
	ThroughputImpact string       `json:"throughput_impact"`
	ResourceUsage   string        `json:"resource_usage"`
}

// AvailabilityImpact assesses availability implications
type AvailabilityImpact struct {
	Level         ChangeSeverity `json:"level"`
	DowntimeRisk  string        `json:"downtime_risk"`
	SLAImpact     string        `json:"sla_impact"`
	RecoveryTime  string        `json:"recovery_time"`
}

// ComplianceImpact assesses compliance implications
type ComplianceImpact struct {
	Level              ChangeSeverity    `json:"level"`
	AffectedFrameworks []string         `json:"affected_frameworks"`
	ComplianceRisk     string           `json:"compliance_risk"`
	RequiredActions    []ComplianceAction `json:"required_actions"`
}

// ComplianceAction represents a required compliance action
type ComplianceAction struct {
	Framework   string `json:"framework"`
	Action      string `json:"action"`
	Deadline    string `json:"deadline"`
	Priority    string `json:"priority"`
	Owner       string `json:"owner,omitempty"`
}

// DriftBaseline represents a configuration baseline for drift detection
type DriftBaseline struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Description  string                    `json:"description"`
	Provider     string                    `json:"provider"`
	CreatedAt    time.Time                 `json:"created_at"`
	UpdatedAt    time.Time                 `json:"updated_at"`
	Resources    map[string]*ResourceState `json:"resources"`
	Policies     []BaselinePolicy          `json:"policies"`
	Tags         map[string]string         `json:"tags"`
	Version      string                    `json:"version"`
	Active       bool                      `json:"active"`
}

// BaselinePolicy defines a policy rule for drift detection
type BaselinePolicy struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	RuleType    string            `json:"rule_type"`
	Rules       map[string]interface{} `json:"rules"`
	Severity    ChangeSeverity    `json:"severity"`
	Enabled     bool              `json:"enabled"`
}

// DriftReport contains the results of drift detection
type DriftReport struct {
	ID              string          `json:"id"`
	BaselineID      string          `json:"baseline_id"`
	GeneratedAt     time.Time       `json:"generated_at"`
	TotalResources  int             `json:"total_resources"`
	DriftedResources int            `json:"drifted_resources"`
	DriftItems      []*DriftItem    `json:"drift_items"`
	Summary         *DriftSummary   `json:"summary"`
	ComplianceStatus *ComplianceStatus `json:"compliance_status"`
}

// DriftItem represents a specific drift detection
type DriftItem struct {
	ResourceID      string            `json:"resource_id"`
	ResourceType    string            `json:"resource_type"`
	DriftType       string            `json:"drift_type"`
	Severity        ChangeSeverity    `json:"severity"`
	Description     string            `json:"description"`
	DetectedAt      time.Time         `json:"detected_at"`
	BaselineValue   interface{}       `json:"baseline_value"`
	CurrentValue    interface{}       `json:"current_value"`
	Field           string            `json:"field"`
	PolicyViolated  string            `json:"policy_violated,omitempty"`
	Remediation     *RemediationSuggestion `json:"remediation,omitempty"`
}

// DriftSummary provides aggregated drift information
type DriftSummary struct {
	HighSeverityCount     int                      `json:"high_severity_count"`
	MediumSeverityCount   int                      `json:"medium_severity_count"`
	LowSeverityCount      int                      `json:"low_severity_count"`
	CriticalSeverityCount int                      `json:"critical_severity_count"`
	DriftByType          map[string]int           `json:"drift_by_type"`
	DriftByService       map[string]int           `json:"drift_by_service"`
	ComplianceScore      float64                  `json:"compliance_score"`
	Recommendations      []string                 `json:"recommendations"`
}

// ComplianceStatus provides compliance assessment
type ComplianceStatus struct {
	OverallScore    float64                    `json:"overall_score"`
	FrameworkScores map[string]float64         `json:"framework_scores"`
	Violations      []ComplianceViolation      `json:"violations"`
	Recommendations []ComplianceRecommendation `json:"recommendations"`
}

// ComplianceViolation represents a compliance violation
type ComplianceViolation struct {
	Framework   string         `json:"framework"`
	Rule        string         `json:"rule"`
	Severity    ChangeSeverity `json:"severity"`
	ResourceID  string         `json:"resource_id"`
	Description string         `json:"description"`
	Remediation string         `json:"remediation"`
}

// ComplianceRecommendation provides compliance improvement suggestions
type ComplianceRecommendation struct {
	Framework   string `json:"framework"`
	Priority    string `json:"priority"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Impact      string `json:"impact"`
}

// RemediationSuggestion provides automated remediation options
type RemediationSuggestion struct {
	Type            string                 `json:"type"`
	Description     string                 `json:"description"`
	AutomationLevel string                 `json:"automation_level"`
	Commands        []string               `json:"commands,omitempty"`
	Scripts         []string               `json:"scripts,omitempty"`
	Documentation   string                 `json:"documentation,omitempty"`
	EstimatedTime   string                 `json:"estimated_time"`
	RiskLevel       ChangeSeverity         `json:"risk_level"`
	Prerequisites   []string               `json:"prerequisites,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// BaseChangeTracker provides shared functionality for all provider implementations
type BaseChangeTracker struct {
	provider       string
	storage        ChangeStorage
	analytics      *ChangeAnalytics
	alerting       *AlertingSystem
	cache          *ChangeCache
	mu             sync.RWMutex
	config         *ChangeTrackerConfig
}

// ChangeTrackerConfig provides configuration for change tracking
type ChangeTrackerConfig struct {
	Provider               string        `json:"provider"`
	EnableRealTimeMonitoring bool         `json:"enable_real_time_monitoring"`
	ChangeRetention        time.Duration `json:"change_retention"`
	DriftCheckInterval     time.Duration `json:"drift_check_interval"`
	AlertingEnabled        bool          `json:"alerting_enabled"`
	AnalyticsEnabled       bool          `json:"analytics_enabled"`
	CacheEnabled           bool          `json:"cache_enabled"`
	CacheTTL               time.Duration `json:"cache_ttl"`
	MaxConcurrentStreams   int           `json:"max_concurrent_streams"`
	BatchSize              int           `json:"batch_size"`
	MaxQueryTimeRange      time.Duration `json:"max_query_time_range"`
}

// ChangeStorage interface for persisting change data
type ChangeStorage interface {
	StoreChange(change *ChangeEvent) error
	StoreChanges(changes []*ChangeEvent) error
	QueryChanges(query *ChangeQuery) ([]*ChangeEvent, error)
	GetChangeHistory(resourceID string) ([]*ChangeEvent, error)
	GetChange(changeID string) (*ChangeEvent, error)
	DeleteChanges(olderThan time.Time) error
	
	// Baseline management
	StoreBaseline(baseline *DriftBaseline) error
	GetBaseline(baselineID string) (*DriftBaseline, error)
	ListBaselines(provider string) ([]*DriftBaseline, error)
	UpdateBaseline(baseline *DriftBaseline) error
	DeleteBaseline(baselineID string) error
}

// ChangeCache provides caching for change tracking operations
type ChangeCache struct {
	changes   map[string]*ChangeEvent
	queries   map[string]*CachedQuery
	mu        sync.RWMutex
	ttl       time.Duration
	maxSize   int
}

// CachedQuery represents a cached query result
type CachedQuery struct {
	Query     *ChangeQuery   `json:"query"`
	Results   []*ChangeEvent `json:"results"`
	CachedAt  time.Time      `json:"cached_at"`
	ExpiresAt time.Time      `json:"expires_at"`
}

// NewBaseChangeTracker creates a new base change tracker
func NewBaseChangeTracker(provider string, storage ChangeStorage, config *ChangeTrackerConfig) *BaseChangeTracker {
	if config == nil {
		config = &ChangeTrackerConfig{
			Provider:               provider,
			EnableRealTimeMonitoring: true,
			ChangeRetention:        365 * 24 * time.Hour, // 1 year
			DriftCheckInterval:     1 * time.Hour,
			AlertingEnabled:        true,
			AnalyticsEnabled:       true,
			CacheEnabled:           true,
			CacheTTL:               5 * time.Minute,
			MaxConcurrentStreams:   100,
			BatchSize:              1000,
			MaxQueryTimeRange:      30 * 24 * time.Hour, // 30 days
		}
	}

	bct := &BaseChangeTracker{
		provider: provider,
		storage:  storage,
		config:   config,
	}

	// Initialize components
	if config.AnalyticsEnabled {
		bct.analytics = NewChangeAnalytics(storage)
	}

	if config.AlertingEnabled {
		bct.alerting = NewAlertingSystem()
	}

	if config.CacheEnabled {
		bct.cache = NewChangeCache(config.CacheTTL, 10000) // Max 10k cached items
	}

	return bct
}

// ValidateChangeQuery validates a change query
func (bct *BaseChangeTracker) ValidateChangeQuery(query *ChangeQuery) error {
	if query == nil {
		return fmt.Errorf("query cannot be nil")
	}

	// Validate time range
	if query.StartTime.IsZero() {
		return fmt.Errorf("start time is required")
	}

	if query.EndTime.IsZero() {
		query.EndTime = time.Now()
	}

	if query.EndTime.Before(query.StartTime) {
		return fmt.Errorf("end time cannot be before start time")
	}

	// Validate time range limits
	if bct.config.MaxQueryTimeRange > 0 {
		if query.EndTime.Sub(query.StartTime) > bct.config.MaxQueryTimeRange {
			return fmt.Errorf("query time range exceeds maximum allowed duration of %v", bct.config.MaxQueryTimeRange)
		}
	}

	// Set defaults
	if query.Limit <= 0 {
		query.Limit = 1000
	}

	if query.Limit > 10000 {
		query.Limit = 10000 // Max limit
	}

	if query.SortBy == "" {
		query.SortBy = "timestamp"
	}

	if query.SortOrder == "" {
		query.SortOrder = "desc"
	}

	return nil
}

// GenerateChangeID creates a unique change ID
func (bct *BaseChangeTracker) GenerateChangeID(resourceID string, timestamp time.Time, changeType ChangeType) string {
	return fmt.Sprintf("%s_%s_%s_%d",
		bct.provider,
		resourceID,
		string(changeType),
		timestamp.Unix())
}

// CalculateResourceChecksum computes a checksum for resource state
func (bct *BaseChangeTracker) CalculateResourceChecksum(state *ResourceState) string {
	// This is a simplified checksum calculation
	// In production, you'd use a proper hashing algorithm
	return fmt.Sprintf("%s_%d_%d",
		state.ResourceID,
		len(state.Properties),
		state.Timestamp.Unix())
}

// AnalyzeChangeImpact provides impact analysis for a change
func (bct *BaseChangeTracker) AnalyzeChangeImpact(change *ChangeEvent) *ImpactAssessment {
	assessment := &ImpactAssessment{
		SecurityImpact:     bct.analyzeSecurityImpact(change),
		CostImpact:        bct.analyzeCostImpact(change),
		PerformanceImpact: bct.analyzePerformanceImpact(change),
		AvailabilityImpact: bct.analyzeAvailabilityImpact(change),
	}

	// Calculate overall risk score
	assessment.RiskScore = bct.calculateRiskScore(assessment)

	// Generate recommendations
	assessment.Recommendations = bct.generateRecommendations(change, assessment)

	return assessment
}

// Helper methods for impact analysis
func (bct *BaseChangeTracker) analyzeSecurityImpact(change *ChangeEvent) SecurityImpact {
	impact := SecurityImpact{Level: SeverityLow}

	// Check for IAM changes
	for _, field := range change.ChangedFields {
		if strings.Contains(strings.ToLower(field), "iam") ||
			strings.Contains(strings.ToLower(field), "policy") ||
			strings.Contains(strings.ToLower(field), "permission") {
			impact.IAMChanges = true
			impact.Level = SeverityMedium
		}

		if strings.Contains(strings.ToLower(field), "network") ||
			strings.Contains(strings.ToLower(field), "firewall") ||
			strings.Contains(strings.ToLower(field), "security") {
			impact.NetworkChanges = true
			if impact.Level == SeverityLow {
				impact.Level = SeverityMedium
			}
		}

		if strings.Contains(strings.ToLower(field), "encryption") ||
			strings.Contains(strings.ToLower(field), "kms") ||
			strings.Contains(strings.ToLower(field), "key") {
			impact.EncryptionChanges = true
			impact.Level = SeverityHigh
		}

		if strings.Contains(strings.ToLower(field), "public") ||
			strings.Contains(strings.ToLower(field), "external") {
			impact.PublicAccess = true
			impact.Level = SeverityHigh
		}
	}

	// Escalate for critical resource types
	if change.ResourceType == "Instance" || change.ResourceType == "Database" {
		if impact.Level == SeverityMedium {
			impact.Level = SeverityHigh
		}
	}

	return impact
}

func (bct *BaseChangeTracker) analyzeCostImpact(change *ChangeEvent) CostImpact {
	impact := CostImpact{
		Level:    SeverityLow,
		Currency: "USD",
	}

	// Analyze based on change type and resource type
	if change.ChangeType == ChangeTypeCreate {
		impact.Level = SeverityMedium
		impact.EstimatedChange = 10.0 // Placeholder
		impact.CostDrivers = []string{"New resource provisioning"}
	} else if change.ChangeType == ChangeTypeDelete {
		impact.Level = SeverityLow
		impact.EstimatedChange = -5.0 // Cost savings
		impact.CostDrivers = []string{"Resource deprovisioning"}
	}

	// High-cost resource types
	highCostResources := []string{"Instance", "Database", "LoadBalancer", "Cluster"}
	for _, resourceType := range highCostResources {
		if change.ResourceType == resourceType {
			impact.Level = SeverityHigh
			impact.EstimatedChange *= 5 // Amplify cost impact
			break
		}
	}

	return impact
}

func (bct *BaseChangeTracker) analyzePerformanceImpact(change *ChangeEvent) PerformanceImpact {
	impact := PerformanceImpact{Level: SeverityLow}

	// Analyze performance-related fields
	for _, field := range change.ChangedFields {
		if strings.Contains(strings.ToLower(field), "cpu") ||
			strings.Contains(strings.ToLower(field), "memory") ||
			strings.Contains(strings.ToLower(field), "disk") {
			impact.Level = SeverityMedium
			impact.ResourceUsage = "Changed"
		}

		if strings.Contains(strings.ToLower(field), "network") ||
			strings.Contains(strings.ToLower(field), "bandwidth") {
			impact.LatencyImpact = "Potential increase"
			impact.ThroughputImpact = "May be affected"
		}
	}

	return impact
}

func (bct *BaseChangeTracker) analyzeAvailabilityImpact(change *ChangeEvent) AvailabilityImpact {
	impact := AvailabilityImpact{Level: SeverityLow}

	if change.ChangeType == ChangeTypeDelete {
		impact.Level = SeverityHigh
		impact.DowntimeRisk = "High"
		impact.SLAImpact = "Potential SLA breach"
	}

	// Critical resources
	criticalResources := []string{"LoadBalancer", "Database", "Cluster"}
	for _, resourceType := range criticalResources {
		if change.ResourceType == resourceType {
			impact.Level = SeverityHigh
			impact.DowntimeRisk = "High"
			break
		}
	}

	return impact
}

func (bct *BaseChangeTracker) calculateRiskScore(assessment *ImpactAssessment) float64 {
	score := 0.0

	// Weight different impact types
	switch assessment.SecurityImpact.Level {
	case SeverityCritical:
		score += 40
	case SeverityHigh:
		score += 30
	case SeverityMedium:
		score += 15
	case SeverityLow:
		score += 5
	}

	switch assessment.AvailabilityImpact.Level {
	case SeverityCritical:
		score += 30
	case SeverityHigh:
		score += 20
	case SeverityMedium:
		score += 10
	case SeverityLow:
		score += 2
	}

	// Add cost and performance factors
	if assessment.CostImpact.Level == SeverityHigh {
		score += 15
	}
	if assessment.PerformanceImpact.Level == SeverityHigh {
		score += 15
	}

	return score
}

func (bct *BaseChangeTracker) generateRecommendations(change *ChangeEvent, assessment *ImpactAssessment) []string {
	var recommendations []string

	if assessment.SecurityImpact.Level >= SeverityHigh {
		recommendations = append(recommendations, "Review security implications before applying")
		recommendations = append(recommendations, "Ensure proper access controls are in place")
	}

	if assessment.AvailabilityImpact.Level >= SeverityHigh {
		recommendations = append(recommendations, "Plan for potential downtime")
		recommendations = append(recommendations, "Notify stakeholders of availability impact")
	}

	if assessment.CostImpact.Level >= SeverityMedium {
		recommendations = append(recommendations, "Review cost implications")
		recommendations = append(recommendations, "Consider cost optimization opportunities")
	}

	if assessment.RiskScore > 50 {
		recommendations = append(recommendations, "High-risk change - consider additional approval workflow")
	}

	return recommendations
}

// NewChangeCache creates a new change cache
func NewChangeCache(ttl time.Duration, maxSize int) *ChangeCache {
	return &ChangeCache{
		changes:  make(map[string]*ChangeEvent),
		queries:  make(map[string]*CachedQuery),
		ttl:      ttl,
		maxSize:  maxSize,
	}
}

// Get retrieves a cached change event
func (cc *ChangeCache) Get(changeID string) (*ChangeEvent, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	change, exists := cc.changes[changeID]
	return change, exists
}

// Set stores a change event in cache
func (cc *ChangeCache) Set(changeID string, change *ChangeEvent) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Simple eviction - remove oldest if at capacity
	if len(cc.changes) >= cc.maxSize {
		// Remove one item (simplified LRU)
		for k := range cc.changes {
			delete(cc.changes, k)
			break
		}
	}

	cc.changes[changeID] = change
}

// Clear removes all cached items
func (cc *ChangeCache) Clear() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.changes = make(map[string]*ChangeEvent)
	cc.queries = make(map[string]*CachedQuery)
}
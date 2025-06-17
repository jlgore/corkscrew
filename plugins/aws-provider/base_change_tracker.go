package main

import (
	"fmt"
	"strings"
	"time"
)

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
package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ChangeAnalytics provides analytics capabilities for change tracking
type ChangeAnalytics struct {
	storage ChangeStorage
}

// AnalyticsReport contains aggregated analytics data
type AnalyticsReport struct {
	Provider               string                    `json:"provider"`
	TimeRange             TimeRange                 `json:"time_range"`
	GeneratedAt           time.Time                 `json:"generated_at"`
	TotalChanges          int                       `json:"total_changes"`
	ChangesByType         map[string]int            `json:"changes_by_type"`
	ChangesBySeverity     map[string]int            `json:"changes_by_severity"`
	ChangesByService      map[string]int            `json:"changes_by_service"`
	ChangesByResourceType map[string]int            `json:"changes_by_resource_type"`
	ChangesByProject      map[string]int            `json:"changes_by_project"`
	ChangesByRegion       map[string]int            `json:"changes_by_region"`
	FrequencyTrends       []*FrequencyDataPoint     `json:"frequency_trends"`
	ImpactTrends          []*ImpactDataPoint        `json:"impact_trends"`
	TopChangedResources   []*ResourceChangesSummary `json:"top_changed_resources"`
	AnomalousPatterns     []*ChangeAnomaly          `json:"anomalous_patterns"`
	ComplianceMetrics     *ComplianceMetrics        `json:"compliance_metrics"`
	Recommendations       []*AnalyticsRecommendation `json:"recommendations"`
}

// TimeRange represents a time period for analytics
type TimeRange struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  string    `json:"duration"`
}

// FrequencyDataPoint represents change frequency at a point in time
type FrequencyDataPoint struct {
	Timestamp    time.Time `json:"timestamp"`
	ChangeCount  int       `json:"change_count"`
	Bucket       string    `json:"bucket"` // hour, day, week, month
	ServiceBreakdown map[string]int `json:"service_breakdown"`
}

// ImpactDataPoint represents impact metrics at a point in time
type ImpactDataPoint struct {
	Timestamp         time.Time `json:"timestamp"`
	AverageImpactScore float64   `json:"average_impact_score"`
	TotalImpactScore   float64   `json:"total_impact_score"`
	HighImpactChanges  int       `json:"high_impact_changes"`
	CriticalChanges    int       `json:"critical_changes"`
}

// ResourceChangesSummary summarizes changes for a specific resource
type ResourceChangesSummary struct {
	ResourceID     string            `json:"resource_id"`
	ResourceType   string            `json:"resource_type"`
	Service        string            `json:"service"`
	Project        string            `json:"project"`
	TotalChanges   int               `json:"total_changes"`
	ChangesByType  map[string]int    `json:"changes_by_type"`
	LastChange     time.Time         `json:"last_change"`
	AverageImpact  float64           `json:"average_impact"`
	RiskLevel      ChangeSeverity    `json:"risk_level"`
}

// ChangeAnomaly represents detected anomalous change patterns
type ChangeAnomaly struct {
	ID              string            `json:"id"`
	AnomalyType     string            `json:"anomaly_type"`
	Description     string            `json:"description"`
	Severity        ChangeSeverity    `json:"severity"`
	DetectedAt      time.Time         `json:"detected_at"`
	TimeWindow      TimeRange         `json:"time_window"`
	AffectedResources []string        `json:"affected_resources"`
	Metrics         map[string]float64 `json:"metrics"`
	Confidence      float64           `json:"confidence"`
	Recommendations []string          `json:"recommendations"`
}

// ComplianceMetrics provides compliance-related analytics
type ComplianceMetrics struct {
	OverallScore         float64            `json:"overall_score"`
	FrameworkScores      map[string]float64 `json:"framework_scores"`
	ViolationCount       int                `json:"violation_count"`
	CriticalViolations   int                `json:"critical_violations"`
	TrendDirection       string             `json:"trend_direction"` // "improving", "stable", "declining"
	RecentViolations     []*ComplianceViolation `json:"recent_violations"`
}

// AnalyticsRecommendation provides actionable insights
type AnalyticsRecommendation struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Priority     string         `json:"priority"` // "high", "medium", "low"
	Category     string         `json:"category"` // "security", "cost", "performance", "compliance"
	Evidence     []string       `json:"evidence"`
	Actions      []string       `json:"actions"`
	ImpactLevel  ChangeSeverity `json:"impact_level"`
	EstimatedROI string         `json:"estimated_roi"`
}

// AnalyticsQuery defines parameters for analytics queries
type AnalyticsQuery struct {
	Provider      string            `json:"provider"`
	TimeRange     TimeRange         `json:"time_range"`
	Granularity   string            `json:"granularity"` // "hour", "day", "week", "month"
	Services      []string          `json:"services,omitempty"`
	ResourceTypes []string          `json:"resource_types,omitempty"`
	Projects      []string          `json:"projects,omitempty"`
	Regions       []string          `json:"regions,omitempty"`
	ChangeTypes   []ChangeType      `json:"change_types,omitempty"`
	Severities    []ChangeSeverity  `json:"severities,omitempty"`
	IncludeAnomalies bool           `json:"include_anomalies"`
	IncludeCompliance bool          `json:"include_compliance"`
	TopN          int               `json:"top_n"` // For top resources, services, etc.
}

// NewChangeAnalytics creates a new change analytics engine
func NewChangeAnalytics(storage ChangeStorage) *ChangeAnalytics {
	return &ChangeAnalytics{
		storage: storage,
	}
}

// GenerateReport creates a comprehensive analytics report
func (ca *ChangeAnalytics) GenerateReport(ctx context.Context, query *AnalyticsQuery) (*AnalyticsReport, error) {
	if query == nil {
		return nil, fmt.Errorf("analytics query cannot be nil")
	}

	log.Printf("Generating analytics report for provider: %s, time range: %v to %v",
		query.Provider, query.TimeRange.StartTime, query.TimeRange.EndTime)

	report := &AnalyticsReport{
		Provider:    query.Provider,
		TimeRange:   query.TimeRange,
		GeneratedAt: time.Now(),
	}

	// Get basic statistics
	if err := ca.populateBasicStats(ctx, report, query); err != nil {
		return nil, fmt.Errorf("failed to populate basic stats: %w", err)
	}

	// Get frequency trends
	if err := ca.populateFrequencyTrends(ctx, report, query); err != nil {
		log.Printf("Failed to populate frequency trends: %v", err)
	}

	// Get impact trends
	if err := ca.populateImpactTrends(ctx, report, query); err != nil {
		log.Printf("Failed to populate impact trends: %v", err)
	}

	// Get top changed resources
	if err := ca.populateTopChangedResources(ctx, report, query); err != nil {
		log.Printf("Failed to populate top changed resources: %v", err)
	}

	// Detect anomalies if requested
	if query.IncludeAnomalies {
		if err := ca.populateAnomalies(ctx, report, query); err != nil {
			log.Printf("Failed to populate anomalies: %v", err)
		}
	}

	// Get compliance metrics if requested
	if query.IncludeCompliance {
		if err := ca.populateComplianceMetrics(ctx, report, query); err != nil {
			log.Printf("Failed to populate compliance metrics: %v", err)
		}
	}

	// Generate recommendations
	if err := ca.generateRecommendations(ctx, report, query); err != nil {
		log.Printf("Failed to generate recommendations: %v", err)
	}

	log.Printf("Analytics report generated successfully with %d total changes", report.TotalChanges)
	return report, nil
}

// GetChangeFrequency returns change frequency data
func (ca *ChangeAnalytics) GetChangeFrequency(ctx context.Context, provider string, timeRange TimeRange, granularity string) ([]*FrequencyDataPoint, error) {
	// Use the storage interface to query aggregated data
	changeQuery := &ChangeQuery{
		Provider:  provider,
		StartTime: timeRange.StartTime,
		EndTime:   timeRange.EndTime,
		SortBy:    "timestamp",
		SortOrder: "asc",
		Limit:     10000, // Large limit to get all data
	}

	changes, err := ca.storage.QueryChanges(changeQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query changes: %w", err)
	}

	// Aggregate changes by time buckets
	buckets := ca.createTimeBuckets(timeRange, granularity)
	dataPoints := make([]*FrequencyDataPoint, 0, len(buckets))

	for _, bucket := range buckets {
		dp := &FrequencyDataPoint{
			Timestamp:        bucket,
			ChangeCount:      0,
			Bucket:          granularity,
			ServiceBreakdown: make(map[string]int),
		}

		// Count changes in this bucket
		for _, change := range changes {
			if ca.isInTimeBucket(change.Timestamp, bucket, granularity) {
				dp.ChangeCount++
				dp.ServiceBreakdown[change.Service]++
			}
		}

		dataPoints = append(dataPoints, dp)
	}

	return dataPoints, nil
}

// DetectAnomalies identifies unusual change patterns
func (ca *ChangeAnalytics) DetectAnomalies(ctx context.Context, provider string, timeRange TimeRange) ([]*ChangeAnomaly, error) {
	var anomalies []*ChangeAnomaly

	// Skip frequency data for now - we'll use changes directly

	// Get changes for anomaly detection
	changeQuery := &ChangeQuery{
		Provider:  provider,
		StartTime: timeRange.StartTime,
		EndTime:   timeRange.EndTime,
		Limit:     10000,
	}

	changes, err := ca.storage.QueryChanges(changeQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query changes for anomaly detection: %w", err)
	}

	// Detect frequency spikes
	freqAnomalies := ca.detectFrequencyAnomalies(changes)
	anomalies = append(anomalies, freqAnomalies...)

	// Detect unusual change patterns
	patternAnomalies := ca.detectPatternAnomalies(changes)
	anomalies = append(anomalies, patternAnomalies...)

	// Detect resource-specific anomalies
	resourceAnomalies := ca.detectResourceAnomalies(changes)
	anomalies = append(anomalies, resourceAnomalies...)

	return anomalies, nil
}

// GetResourceChangesTrend returns change trends for specific resources
func (ca *ChangeAnalytics) GetResourceChangesTrend(ctx context.Context, resourceID string, timeRange TimeRange) (*ResourceChangesSummary, error) {
	changes, err := ca.storage.GetChangeHistory(resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get change history: %w", err)
	}

	// Filter by time range
	filteredChanges := make([]*ChangeEvent, 0)
	for _, change := range changes {
		if change.Timestamp.After(timeRange.StartTime) && change.Timestamp.Before(timeRange.EndTime) {
			filteredChanges = append(filteredChanges, change)
		}
	}

	if len(filteredChanges) == 0 {
		return nil, fmt.Errorf("no changes found for resource %s in the specified time range", resourceID)
	}

	// Build summary
	summary := &ResourceChangesSummary{
		ResourceID:    resourceID,
		ResourceType:  filteredChanges[0].ResourceType,
		Service:       filteredChanges[0].Service,
		Project:       filteredChanges[0].Project,
		TotalChanges:  len(filteredChanges),
		ChangesByType: make(map[string]int),
		LastChange:    filteredChanges[0].Timestamp,
	}

	// Aggregate by change type and calculate metrics
	totalImpact := 0.0
	for _, change := range filteredChanges {
		summary.ChangesByType[string(change.ChangeType)]++

		if change.Timestamp.After(summary.LastChange) {
			summary.LastChange = change.Timestamp
		}

		if change.ImpactAssessment != nil {
			totalImpact += change.ImpactAssessment.RiskScore
		}

		// Determine overall risk level
		if change.Severity > summary.RiskLevel {
			summary.RiskLevel = change.Severity
		}
	}

	if len(filteredChanges) > 0 {
		summary.AverageImpact = totalImpact / float64(len(filteredChanges))
	}

	return summary, nil
}

// Helper methods for analytics

func (ca *ChangeAnalytics) populateBasicStats(ctx context.Context, report *AnalyticsReport, query *AnalyticsQuery) error {
	// Build change query from analytics query
	changeQuery := &ChangeQuery{
		Provider:       query.Provider,
		StartTime:      query.TimeRange.StartTime,
		EndTime:        query.TimeRange.EndTime,
		ResourceFilter: ca.buildResourceFilterFromAnalyticsQuery(query),
		ChangeTypes:    query.ChangeTypes,
		Severities:     query.Severities,
		Limit:          100000, // Large limit to get comprehensive stats
	}

	changes, err := ca.storage.QueryChanges(changeQuery)
	if err != nil {
		return fmt.Errorf("failed to query changes for basic stats: %w", err)
	}

	report.TotalChanges = len(changes)
	report.ChangesByType = make(map[string]int)
	report.ChangesBySeverity = make(map[string]int)
	report.ChangesByService = make(map[string]int)
	report.ChangesByResourceType = make(map[string]int)
	report.ChangesByProject = make(map[string]int)
	report.ChangesByRegion = make(map[string]int)

	// Aggregate statistics
	for _, change := range changes {
		report.ChangesByType[string(change.ChangeType)]++
		report.ChangesBySeverity[string(change.Severity)]++
		report.ChangesByService[change.Service]++
		report.ChangesByResourceType[change.ResourceType]++
		
		if change.Project != "" {
			report.ChangesByProject[change.Project]++
		}
		
		if change.Region != "" {
			report.ChangesByRegion[change.Region]++
		}
	}

	return nil
}

func (ca *ChangeAnalytics) populateFrequencyTrends(ctx context.Context, report *AnalyticsReport, query *AnalyticsQuery) error {
	granularity := query.Granularity
	if granularity == "" {
		granularity = ca.determineOptimalGranularity(query.TimeRange)
	}

	frequencyData, err := ca.GetChangeFrequency(ctx, query.Provider, query.TimeRange, granularity)
	if err != nil {
		return err
	}

	report.FrequencyTrends = frequencyData
	return nil
}

func (ca *ChangeAnalytics) populateImpactTrends(ctx context.Context, report *AnalyticsReport, query *AnalyticsQuery) error {
	// This would typically use pre-aggregated analytics data
	// For now, we'll compute it from the change events
	changeQuery := &ChangeQuery{
		Provider:  query.Provider,
		StartTime: query.TimeRange.StartTime,
		EndTime:   query.TimeRange.EndTime,
		SortBy:    "timestamp",
		SortOrder: "asc",
		Limit:     10000,
	}

	changes, err := ca.storage.QueryChanges(changeQuery)
	if err != nil {
		return err
	}

	// Create time buckets and aggregate impact scores
	granularity := query.Granularity
	if granularity == "" {
		granularity = "day"
	}

	buckets := ca.createTimeBuckets(query.TimeRange, granularity)
	impactTrends := make([]*ImpactDataPoint, 0, len(buckets))

	for _, bucket := range buckets {
		dp := &ImpactDataPoint{
			Timestamp: bucket,
		}

		var totalImpact float64
		var changeCount int
		var highImpactCount int
		var criticalCount int

		for _, change := range changes {
			if ca.isInTimeBucket(change.Timestamp, bucket, granularity) {
				changeCount++
				
				if change.ImpactAssessment != nil {
					totalImpact += change.ImpactAssessment.RiskScore
					
					if change.ImpactAssessment.RiskScore > 70 {
						highImpactCount++
					}
				}
				
				if change.Severity == SeverityCritical {
					criticalCount++
				}
			}
		}

		if changeCount > 0 {
			dp.AverageImpactScore = totalImpact / float64(changeCount)
		}
		dp.TotalImpactScore = totalImpact
		dp.HighImpactChanges = highImpactCount
		dp.CriticalChanges = criticalCount

		impactTrends = append(impactTrends, dp)
	}

	report.ImpactTrends = impactTrends
	return nil
}

func (ca *ChangeAnalytics) populateTopChangedResources(ctx context.Context, report *AnalyticsReport, query *AnalyticsQuery) error {
	// Get all changes and group by resource
	changeQuery := &ChangeQuery{
		Provider:  query.Provider,
		StartTime: query.TimeRange.StartTime,
		EndTime:   query.TimeRange.EndTime,
		Limit:     50000, // Large limit to capture all resources
	}

	changes, err := ca.storage.QueryChanges(changeQuery)
	if err != nil {
		return err
	}

	// Group changes by resource
	resourceChanges := make(map[string][]*ChangeEvent)
	for _, change := range changes {
		resourceChanges[change.ResourceID] = append(resourceChanges[change.ResourceID], change)
	}

	// Create summaries
	var summaries []*ResourceChangesSummary
	for resourceID, resourceChangeList := range resourceChanges {
		summary := ca.createResourceSummary(resourceID, resourceChangeList)
		summaries = append(summaries, summary)
	}

	// Sort by total changes (descending)
	ca.sortResourceSummaries(summaries)

	// Limit to top N
	topN := query.TopN
	if topN <= 0 {
		topN = 10
	}

	if len(summaries) > topN {
		summaries = summaries[:topN]
	}

	report.TopChangedResources = summaries
	return nil
}

func (ca *ChangeAnalytics) populateAnomalies(ctx context.Context, report *AnalyticsReport, query *AnalyticsQuery) error {
	anomalies, err := ca.DetectAnomalies(ctx, query.Provider, query.TimeRange)
	if err != nil {
		return err
	}

	report.AnomalousPatterns = anomalies
	return nil
}

func (ca *ChangeAnalytics) populateComplianceMetrics(ctx context.Context, report *AnalyticsReport, query *AnalyticsQuery) error {
	// This would integrate with compliance frameworks
	// For now, we'll provide a basic implementation
	changeQuery := &ChangeQuery{
		Provider:  query.Provider,
		StartTime: query.TimeRange.StartTime,
		EndTime:   query.TimeRange.EndTime,
		Limit:     10000,
	}

	changes, err := ca.storage.QueryChanges(changeQuery)
	if err != nil {
		return err
	}

	// Calculate basic compliance metrics
	metrics := &ComplianceMetrics{
		FrameworkScores: make(map[string]float64),
	}

	// Count compliance-related changes
	var complianceChanges int
	var criticalViolations int

	for _, change := range changes {
		if change.ComplianceImpact != nil {
			complianceChanges++
			
			if change.ComplianceImpact.Level == SeverityCritical {
				criticalViolations++
			}
			
			// Aggregate framework scores (simplified)
			for _, framework := range change.ComplianceImpact.AffectedFrameworks {
				if _, exists := metrics.FrameworkScores[framework]; !exists {
					metrics.FrameworkScores[framework] = 85.0 // Default baseline
				}
				
				// Reduce score based on severity
				switch change.Severity {
				case SeverityCritical:
					metrics.FrameworkScores[framework] -= 10
				case SeverityHigh:
					metrics.FrameworkScores[framework] -= 5
				case SeverityMedium:
					metrics.FrameworkScores[framework] -= 2
				}
			}
		}
	}

	metrics.ViolationCount = complianceChanges
	metrics.CriticalViolations = criticalViolations

	// Calculate overall score
	if len(metrics.FrameworkScores) > 0 {
		var total float64
		for _, score := range metrics.FrameworkScores {
			total += score
		}
		metrics.OverallScore = total / float64(len(metrics.FrameworkScores))
	} else {
		metrics.OverallScore = 100.0 // No compliance issues
	}

	// Determine trend (simplified)
	if metrics.OverallScore > 90 {
		metrics.TrendDirection = "improving"
	} else if metrics.OverallScore > 75 {
		metrics.TrendDirection = "stable"
	} else {
		metrics.TrendDirection = "declining"
	}

	report.ComplianceMetrics = metrics
	return nil
}

func (ca *ChangeAnalytics) generateRecommendations(ctx context.Context, report *AnalyticsReport, query *AnalyticsQuery) error {
	var recommendations []*AnalyticsRecommendation

	// High change frequency recommendation
	if len(report.FrequencyTrends) > 0 {
		avgChanges := ca.calculateAverageFrequency(report.FrequencyTrends)
		if avgChanges > 100 { // Threshold for high frequency
			recommendations = append(recommendations, &AnalyticsRecommendation{
				ID:          "high_change_frequency",
				Title:       "High Change Frequency Detected",
				Description: fmt.Sprintf("Average of %.1f changes per time period. Consider implementing change approval workflows.", avgChanges),
				Priority:    "medium",
				Category:    "governance",
				Evidence:    []string{fmt.Sprintf("Average %.1f changes per period", avgChanges)},
				Actions:     []string{"Implement change approval workflow", "Review change automation policies"},
				ImpactLevel: SeverityMedium,
			})
		}
	}

	// High impact changes recommendation
	if len(report.ImpactTrends) > 0 {
		avgImpact := ca.calculateAverageImpact(report.ImpactTrends)
		if avgImpact > 60 { // Threshold for high impact
			recommendations = append(recommendations, &AnalyticsRecommendation{
				ID:          "high_impact_changes",
				Title:       "High Impact Changes Detected",
				Description: fmt.Sprintf("Average impact score of %.1f indicates potentially risky changes.", avgImpact),
				Priority:    "high",
				Category:    "security",
				Evidence:    []string{fmt.Sprintf("Average impact score: %.1f", avgImpact)},
				Actions:     []string{"Review high-impact changes", "Implement additional approval for high-risk changes"},
				ImpactLevel: SeverityHigh,
			})
		}
	}

	// Service-specific recommendations
	if len(report.ChangesByService) > 0 {
		topService := ca.findTopService(report.ChangesByService)
		if report.ChangesByService[topService] > report.TotalChanges/2 {
			recommendations = append(recommendations, &AnalyticsRecommendation{
				ID:          "service_concentration",
				Title:       "Change Concentration in Single Service",
				Description: fmt.Sprintf("Service '%s' accounts for majority of changes. Consider focused optimization.", topService),
				Priority:    "medium",
				Category:    "optimization",
				Evidence:    []string{fmt.Sprintf("%s: %d changes", topService, report.ChangesByService[topService])},
				Actions:     []string{"Review service architecture", "Implement service-specific monitoring"},
				ImpactLevel: SeverityMedium,
			})
		}
	}

	// Compliance recommendations
	if report.ComplianceMetrics != nil && report.ComplianceMetrics.OverallScore < 80 {
		recommendations = append(recommendations, &AnalyticsRecommendation{
			ID:          "compliance_improvement",
			Title:       "Compliance Score Below Threshold",
			Description: fmt.Sprintf("Overall compliance score of %.1f%% needs improvement.", report.ComplianceMetrics.OverallScore),
			Priority:    "high",
			Category:    "compliance",
			Evidence:    []string{fmt.Sprintf("Compliance score: %.1f%%", report.ComplianceMetrics.OverallScore)},
			Actions:     []string{"Review compliance violations", "Implement automated compliance checks"},
			ImpactLevel: SeverityHigh,
		})
	}

	report.Recommendations = recommendations
	return nil
}

// Helper method implementations

func (ca *ChangeAnalytics) createTimeBuckets(timeRange TimeRange, granularity string) []time.Time {
	var buckets []time.Time
	start := timeRange.StartTime
	end := timeRange.EndTime
	
	var interval time.Duration
	switch granularity {
	case "minute":
		interval = time.Minute
	case "hour":
		interval = time.Hour
	case "day":
		interval = 24 * time.Hour
	case "week":
		interval = 7 * 24 * time.Hour
	case "month":
		interval = 30 * 24 * time.Hour
	default:
		interval = 24 * time.Hour
	}
	
	for current := start; current.Before(end); current = current.Add(interval) {
		buckets = append(buckets, current)
	}
	
	return buckets
}

func (ca *ChangeAnalytics) isInTimeBucket(timestamp, bucket time.Time, granularity string) bool {
	var interval time.Duration
	switch granularity {
	case "minute":
		interval = time.Minute
	case "hour":
		interval = time.Hour
	case "day":
		interval = 24 * time.Hour
	case "week":
		interval = 7 * 24 * time.Hour
	case "month":
		interval = 30 * 24 * time.Hour
	default:
		interval = 24 * time.Hour
	}
	
	return timestamp.After(bucket) && timestamp.Before(bucket.Add(interval))
}

func (ca *ChangeAnalytics) buildResourceFilterFromAnalyticsQuery(query *AnalyticsQuery) *ResourceFilter {
	return &ResourceFilter{
		Services:      query.Services,
		ResourceTypes: query.ResourceTypes,
		Projects:      query.Projects,
		Regions:       query.Regions,
	}
}

func (ca *ChangeAnalytics) detectFrequencyAnomalies(changes []*ChangeEvent) []*ChangeAnomaly {
	var anomalies []*ChangeAnomaly
	// Simplified implementation - in production would use statistical analysis
	if len(changes) > 1000 {
		anomalies = append(anomalies, &ChangeAnomaly{
			AnomalyType: "frequency_spike",
			Description: "Unusually high change frequency detected",
			Severity:    SeverityHigh,
			Confidence:  0.8,
		})
	}
	return anomalies
}

func (ca *ChangeAnalytics) detectPatternAnomalies(changes []*ChangeEvent) []*ChangeAnomaly {
	var anomalies []*ChangeAnomaly
	// Simplified implementation
	serviceCount := make(map[string]int)
	for _, change := range changes {
		serviceCount[change.Service]++
	}
	
	for service, count := range serviceCount {
		if count > len(changes)/2 {
			anomalies = append(anomalies, &ChangeAnomaly{
				AnomalyType: "service_concentration",
				Description: fmt.Sprintf("Unusual concentration of changes in service: %s", service),
				Severity:    SeverityMedium,
				Confidence:  0.7,
			})
		}
	}
	
	return anomalies
}

func (ca *ChangeAnalytics) detectResourceAnomalies(changes []*ChangeEvent) []*ChangeAnomaly {
	var anomalies []*ChangeAnomaly
	// Simplified implementation
	resourceCount := make(map[string]int)
	for _, change := range changes {
		resourceCount[change.ResourceID]++
	}
	
	for resource, count := range resourceCount {
		if count > 50 { // Threshold for single resource
			anomalies = append(anomalies, &ChangeAnomaly{
				AnomalyType: "resource_hotspot",
				Description: fmt.Sprintf("Resource with excessive changes: %s", resource),
				Severity:    SeverityMedium,
				Confidence:  0.9,
			})
		}
	}
	
	return anomalies
}

// Additional helper methods

func (ca *ChangeAnalytics) determineOptimalGranularity(timeRange TimeRange) string {
	duration := timeRange.EndTime.Sub(timeRange.StartTime)
	
	if duration <= 2*time.Hour {
		return "minute"
	} else if duration <= 48*time.Hour {
		return "hour"  
	} else if duration <= 30*24*time.Hour {
		return "day"
	} else if duration <= 365*24*time.Hour {
		return "week"
	} else {
		return "month"
	}
}


// Additional missing helper methods

func (ca *ChangeAnalytics) createResourceSummary(resourceID string, changes []*ChangeEvent) *ResourceChangesSummary {
	if len(changes) == 0 {
		return nil
	}
	
	summary := &ResourceChangesSummary{
		ResourceID:    resourceID,
		ResourceType:  changes[0].ResourceType,
		Service:       changes[0].Service,
		Project:       changes[0].Project,
		TotalChanges:  len(changes),
		ChangesByType: make(map[string]int),
		LastChange:    changes[0].Timestamp,
	}
	
	var totalImpact float64
	for _, change := range changes {
		summary.ChangesByType[string(change.ChangeType)]++
		
		if change.Timestamp.After(summary.LastChange) {
			summary.LastChange = change.Timestamp
		}
		
		if change.ImpactAssessment != nil {
			totalImpact += change.ImpactAssessment.RiskScore
		}
		
		if change.Severity > summary.RiskLevel {
			summary.RiskLevel = change.Severity
		}
	}
	
	if len(changes) > 0 {
		summary.AverageImpact = totalImpact / float64(len(changes))
	}
	
	return summary
}

func (ca *ChangeAnalytics) sortResourceSummaries(summaries []*ResourceChangesSummary) {
	// Simple bubble sort by total changes (descending)
	for i := 0; i < len(summaries)-1; i++ {
		for j := 0; j < len(summaries)-i-1; j++ {
			if summaries[j].TotalChanges < summaries[j+1].TotalChanges {
				summaries[j], summaries[j+1] = summaries[j+1], summaries[j]
			}
		}
	}
}

func (ca *ChangeAnalytics) calculateAverageFrequency(dataPoints []*FrequencyDataPoint) float64 {
	if len(dataPoints) == 0 {
		return 0
	}
	
	var total int
	for _, dp := range dataPoints {
		total += dp.ChangeCount
	}
	
	return float64(total) / float64(len(dataPoints))
}

func (ca *ChangeAnalytics) calculateAverageImpact(dataPoints []*ImpactDataPoint) float64 {
	if len(dataPoints) == 0 {
		return 0
	}
	
	var total float64
	for _, dp := range dataPoints {
		total += dp.AverageImpactScore
	}
	
	return total / float64(len(dataPoints))
}

func (ca *ChangeAnalytics) findTopService(serviceChanges map[string]int) string {
	var topService string
	var maxChanges int
	
	for service, count := range serviceChanges {
		if count > maxChanges {
			maxChanges = count
			topService = service
		}
	}
	
	return topService
}

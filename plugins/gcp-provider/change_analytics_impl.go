package main

import (
	"fmt"
	"log"
	"time"
)

// Note: Types are defined in change_analytics.go and alerting_system.go

// AnalyzeChanges analyzes change patterns and trends
func (ca *ChangeAnalytics) AnalyzeChanges(query *ChangeQuery) (*ChangeAnalysisReport, error) {
	// Get changes from storage
	changes, err := ca.storage.QueryChanges(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query changes: %w", err)
	}

	report := &ChangeAnalysisReport{
		TotalChanges:    len(changes),
		AnalysisTime:    time.Now(),
		TimeRange:       fmt.Sprintf("%v to %v", query.StartTime, query.EndTime),
		ChangesByType:   make(map[string]int),
		ChangesBySeverity: make(map[string]int),
		ChangesByService:  make(map[string]int),
		Trends:          []*ChangeTrend{},
		Recommendations: []string{},
	}

	// Analyze change patterns
	for _, change := range changes {
		report.ChangesByType[string(change.ChangeType)]++
		report.ChangesBySeverity[string(change.Severity)]++
		report.ChangesByService[change.Service]++
	}

	// Generate trends and recommendations
	report.Trends = ca.generateTrends(changes)
	report.Recommendations = ca.generateRecommendationsForChanges(changes)

	log.Printf("🔍 Change analysis completed: %d changes analyzed", len(changes))
	return report, nil
}

// ChangeAnalysisReport represents the results of change analysis
type ChangeAnalysisReport struct {
	TotalChanges      int                    `json:"total_changes"`
	AnalysisTime      time.Time              `json:"analysis_time"`
	TimeRange         string                 `json:"time_range"`
	ChangesByType     map[string]int         `json:"changes_by_type"`
	ChangesBySeverity map[string]int         `json:"changes_by_severity"`
	ChangesByService  map[string]int         `json:"changes_by_service"`
	Trends            []*ChangeTrend         `json:"trends"`
	Recommendations   []string               `json:"recommendations"`
}

// ChangeTrend represents a trend in change patterns
type ChangeTrend struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Value       float64   `json:"value"`
	Direction   string    `json:"direction"` // "increasing", "decreasing", "stable"
	Confidence  float64   `json:"confidence"`
	Period      string    `json:"period"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
}

func (ca *ChangeAnalytics) generateTrends(changes []*ChangeEvent) []*ChangeTrend {
	var trends []*ChangeTrend

	// Analyze change frequency trends
	if len(changes) > 0 {
		trend := &ChangeTrend{
			Type:        "frequency",
			Description: "Change frequency is within normal range",
			Value:       float64(len(changes)),
			Direction:   "stable",
			Confidence:  0.8,
			Period:      "current",
		}
		trends = append(trends, trend)
	}

	return trends
}

func (ca *ChangeAnalytics) generateRecommendationsForChanges(changes []*ChangeEvent) []string {
	var recommendations []string

	// Count high severity changes
	highSeverityCount := 0
	for _, change := range changes {
		if change.Severity == SeverityHigh || change.Severity == SeverityCritical {
			highSeverityCount++
		}
	}

	if highSeverityCount > 5 {
		recommendations = append(recommendations, "🚨 High number of critical/high severity changes detected - review security policies")
	}

	if len(changes) > 100 {
		recommendations = append(recommendations, "📊 Consider implementing change approval workflows for high-volume environments")
	}

	return recommendations
}

// SendAlert sends an alert for a change event
func (as *AlertingSystem) SendAlert(change *ChangeEvent) error {
	// Determine if this change requires an alert
	if as.shouldAlert(change) {
		log.Printf("🚨 ALERT: %s change in %s service - Resource: %s", 
			change.Severity, change.Service, change.ResourceID)
		// In a real implementation, this would send alerts via email, Slack, etc.
	}

	return nil
}

func (as *AlertingSystem) shouldAlert(change *ChangeEvent) bool {
	// Alert on high and critical severity changes
	return change.Severity == SeverityHigh || change.Severity == SeverityCritical
}
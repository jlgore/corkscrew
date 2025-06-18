package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"reflect"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// DriftDetector detects configuration drift in GCP resources
type DriftDetector struct {
	storage          GCPChangeStorage
	complianceRules  map[string]*ComplianceRule
	driftThresholds  map[string]float64
}

// Note: DriftReport, DriftItem, DriftSummary, ComplianceStatus, and related types
// are defined in change_tracker.go to avoid duplication

// ComplianceRule defines a compliance rule for drift detection
type ComplianceRule struct {
	ID              string                 `json:"id"`
	Framework       string                 `json:"framework"` // CIS, SOC2, NIST, etc.
	Category        string                 `json:"category"`
	ResourceTypes   []string               `json:"resource_types"`
	RequiredFields  map[string]interface{} `json:"required_fields"`
	ForbiddenValues map[string]interface{} `json:"forbidden_values"`
	Severity        ChangeSeverity         `json:"severity"`
	Description     string                 `json:"description"`
	Remediation     string                 `json:"remediation"`
}

// Note: RemediationSuggestion is defined in change_tracker.go

// NewDriftDetector creates a new drift detector
func NewDriftDetector(storage GCPChangeStorage) *DriftDetector {
	detector := &DriftDetector{
		storage:         storage,
		complianceRules: make(map[string]*ComplianceRule),
		driftThresholds: make(map[string]float64),
	}
	
	// Initialize default compliance rules
	detector.initializeComplianceRules()
	
	// Initialize drift thresholds
	detector.initializeDriftThresholds()
	
	log.Printf("✅ Drift detector initialized with %d compliance rules", len(detector.complianceRules))
	return detector
}

// DetectDrift detects configuration drift against a baseline
func (dd *DriftDetector) DetectDrift(ctx context.Context, baselineID string) (*DriftReport, error) {
	log.Printf("🔍 Starting drift detection for baseline: %s", baselineID)
	
	// Get baseline from storage
	baseline, err := dd.storage.GetBaseline(baselineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get baseline: %w", err)
	}
	
	// Get current state of resources
	currentResources, err := dd.getCurrentResourceState(ctx, baseline)
	if err != nil {
		return nil, fmt.Errorf("failed to get current resource state: %w", err)
	}
	
	// Initialize drift report
	report := &DriftReport{
		BaselineID:      baselineID,
		GeneratedAt:     time.Now(),
		TotalResources:  len(baseline.Resources),
		DriftItems:      []*DriftItem{},
		Summary:         &DriftSummary{},
		ComplianceStatus: &ComplianceStatus{},
	}
	
	// Detect drift for each resource
	for resourceID, baselineResource := range baseline.Resources {
		if currentResource, exists := currentResources[resourceID]; exists {
			driftItems := dd.detectResourceDrift(resourceID, baselineResource, currentResource)
			report.DriftItems = append(report.DriftItems, driftItems...)
		} else {
			// Resource was deleted
			deletionDrift := &DriftItem{
				ResourceID:    resourceID,
				ResourceType:  dd.extractResourceType(baselineResource),
				DriftType:     "STATE",
				Severity:      SeverityHigh,
				Description:   "Resource has been deleted",
				DetectedAt:    time.Now(),
				Field:         "existence",
				BaselineValue: "exists",
				CurrentValue:  "deleted",
			}
			report.DriftItems = append(report.DriftItems, deletionDrift)
		}
	}
	
	// Check for new resources not in baseline
	for resourceID, currentResource := range currentResources {
		if _, exists := baseline.Resources[resourceID]; !exists {
			newResourceDrift := &DriftItem{
				ResourceID:    resourceID,
				ResourceType:  dd.extractResourceType(currentResource),
				DriftType:     "STATE",
				Severity:      SeverityMedium,
				Description:   "New resource not in baseline",
				DetectedAt:    time.Now(),
				Field:         "existence",
				BaselineValue: "not_exists",
				CurrentValue:  "exists",
			}
			report.DriftItems = append(report.DriftItems, newResourceDrift)
		}
	}
	
	// Generate summary and compliance status
	report.DriftedResources = len(report.DriftItems)
	report.Summary = dd.generateDriftSummary(report.DriftItems)
	report.ComplianceStatus = dd.assessCompliance(report.DriftItems)
	
	log.Printf("✅ Drift detection completed: %d resources analyzed, %d drift items found", 
		report.TotalResources, len(report.DriftItems))
	
	return report, nil
}

// CreateBaseline creates a new configuration baseline
func (dd *DriftDetector) CreateBaseline(resources []*pb.Resource) (*DriftBaseline, error) {
	log.Printf("📸 Creating new baseline from %d resources", len(resources))
	
	baseline := &DriftBaseline{
		ID:          fmt.Sprintf("baseline_%d", time.Now().Unix()),
		Name:        fmt.Sprintf("Baseline created %s", time.Now().Format("2006-01-02 15:04:05")),
		Description: "Automatically generated baseline for drift detection",
		Provider:    "gcp",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Resources:   make(map[string]*ResourceState),
		Active:      true,
		Version:     "1.0",
	}
	
	// Convert resources to resource states
	for _, resource := range resources {
		resourceState := &ResourceState{
			ResourceID: resource.Id,
			Timestamp:  time.Now(),
			Properties: make(map[string]interface{}),
			Tags:       resource.Tags,
		}
		
		// Extract properties from resource metadata if available
		// Note: Using resource properties instead of metadata field
		
		// Add basic resource information
		resourceState.Properties["name"] = resource.Name
		resourceState.Properties["type"] = resource.Type
		resourceState.Properties["service"] = resource.Service
		resourceState.Properties["region"] = resource.Region
		resourceState.Properties["arn"] = resource.Arn
		
		baseline.Resources[resource.Id] = resourceState
	}
	
	// Store baseline
	if err := dd.storage.StoreBaseline(baseline); err != nil {
		return nil, fmt.Errorf("failed to store baseline: %w", err)
	}
	
	log.Printf("✅ Baseline created successfully: %s", baseline.ID)
	return baseline, nil
}

// Helper methods

func (dd *DriftDetector) getCurrentResourceState(ctx context.Context, baseline *DriftBaseline) (map[string]*ResourceState, error) {
	// This would query the current state of all resources in the baseline
	// For now, we'll return a mock implementation
	currentResources := make(map[string]*ResourceState)
	
	// In a real implementation, this would:
	// 1. Query Cloud Asset Inventory for current resource states
	// 2. Convert them to ResourceState format
	// 3. Return the map
	
	log.Printf("📊 Retrieved current state for %d resources", len(currentResources))
	return currentResources, nil
}

func (dd *DriftDetector) detectResourceDrift(resourceID string, baseline, current *ResourceState) []*DriftItem {
	var driftItems []*DriftItem
	
	// Compare properties
	for key, baselineValue := range baseline.Properties {
		if currentValue, exists := current.Properties[key]; exists {
			if !dd.valuesEqual(baselineValue, currentValue) {
				driftItem := &DriftItem{
					ResourceID:    resourceID,
					ResourceType:  dd.extractResourceTypeFromProperties(baseline.Properties),
					DriftType:     "CONFIGURATION",
					Severity:      dd.calculateDriftSeverity(key, baselineValue, currentValue),
					Description:   fmt.Sprintf("Configuration drift detected in field '%s'", key),
					DetectedAt:    time.Now(),
					Field:         key,
					BaselineValue: baselineValue,
					CurrentValue:  currentValue,
					Remediation:   dd.generateRemediation(resourceID, key, baselineValue, currentValue),
				}
				
				driftItems = append(driftItems, driftItem)
			}
		} else {
			// Property was removed
			driftItem := &DriftItem{
				ResourceID:    resourceID,
				ResourceType:  dd.extractResourceTypeFromProperties(baseline.Properties),
				DriftType:     "CONFIGURATION",
				Severity:      SeverityMedium,
				Description:   fmt.Sprintf("Property '%s' was removed", key),
				DetectedAt:    time.Now(),
				Field:         key,
				BaselineValue: baselineValue,
				CurrentValue:  nil,
			}
			
			driftItems = append(driftItems, driftItem)
		}
	}
	
	// Check for new properties
	for key, currentValue := range current.Properties {
		if _, exists := baseline.Properties[key]; !exists {
			driftItem := &DriftItem{
				ResourceID:    resourceID,
				ResourceType:  dd.extractResourceTypeFromProperties(current.Properties),
				DriftType:     "CONFIGURATION",
				Severity:      SeverityLow,
				Description:   fmt.Sprintf("New property '%s' was added", key),
				DetectedAt:    time.Now(),
				Field:         key,
				BaselineValue: nil,
				CurrentValue:  currentValue,
			}
			
			driftItems = append(driftItems, driftItem)
		}
	}
	
	// Compare tags
	tagDrift := dd.detectTagDrift(resourceID, baseline.Tags, current.Tags)
	driftItems = append(driftItems, tagDrift...)
	
	return driftItems
}

func (dd *DriftDetector) detectTagDrift(resourceID string, baselineTags, currentTags map[string]string) []*DriftItem {
	var driftItems []*DriftItem
	
	// Check for changed/removed tags
	for key, baselineValue := range baselineTags {
		if currentValue, exists := currentTags[key]; exists {
			if baselineValue != currentValue {
				driftItem := &DriftItem{
					ResourceID:    resourceID,
					DriftType:     "TAGS",
					Severity:      SeverityLow,
					Description:   fmt.Sprintf("Tag '%s' value changed", key),
					DetectedAt:    time.Now(),
					Field:         fmt.Sprintf("tags.%s", key),
					BaselineValue: baselineValue,
					CurrentValue:  currentValue,
				}
				driftItems = append(driftItems, driftItem)
			}
		} else {
			driftItem := &DriftItem{
				ResourceID:    resourceID,
				DriftType:     "TAGS",
				Severity:      SeverityLow,
				Description:   fmt.Sprintf("Tag '%s' was removed", key),
				DetectedAt:    time.Now(),
				Field:         fmt.Sprintf("tags.%s", key),
				BaselineValue: baselineValue,
				CurrentValue:  nil,
			}
			driftItems = append(driftItems, driftItem)
		}
	}
	
	// Check for new tags
	for key, currentValue := range currentTags {
		if _, exists := baselineTags[key]; !exists {
			driftItem := &DriftItem{
				ResourceID:    resourceID,
				DriftType:     "TAGS",
				Severity:      SeverityLow,
				Description:   fmt.Sprintf("Tag '%s' was added", key),
				DetectedAt:    time.Now(),
				Field:         fmt.Sprintf("tags.%s", key),
				BaselineValue: nil,
				CurrentValue:  currentValue,
			}
			driftItems = append(driftItems, driftItem)
		}
	}
	
	return driftItems
}

func (dd *DriftDetector) valuesEqual(a, b interface{}) bool {
	return reflect.DeepEqual(a, b)
}

func (dd *DriftDetector) calculateDriftSeverity(field string, baseline, current interface{}) ChangeSeverity {
	// Determine severity based on field importance and change magnitude
	criticalFields := []string{"iam_policy", "security_policy", "firewall_rules", "encryption"}
	
	for _, criticalField := range criticalFields {
		if strings.Contains(strings.ToLower(field), criticalField) {
			return SeverityHigh
		}
	}
	
	return SeverityMedium
}

func (dd *DriftDetector) generateRemediation(resourceID, field string, baseline, current interface{}) *RemediationSuggestion {
	return &RemediationSuggestion{
		Type:            "configuration_restore",
		Description:     fmt.Sprintf("Restore field '%s' to baseline value", field),
		AutomationLevel: "SEMI_AUTOMATED",
		EstimatedTime:   "5-10 minutes",
		RiskLevel:       SeverityLow,
		Metadata: map[string]interface{}{
			"field":          field,
			"baseline_value": baseline,
			"current_value":  current,
		},
	}
}

func (dd *DriftDetector) extractResourceType(resource *ResourceState) string {
	if resourceType, exists := resource.Properties["type"]; exists {
		if typeStr, ok := resourceType.(string); ok {
			return typeStr
		}
	}
	return "unknown"
}

func (dd *DriftDetector) extractResourceTypeFromProperties(properties map[string]interface{}) string {
	if resourceType, exists := properties["type"]; exists {
		if typeStr, ok := resourceType.(string); ok {
			return typeStr
		}
	}
	return "unknown"
}

func (dd *DriftDetector) generateDriftSummary(driftItems []*DriftItem) *DriftSummary {
	summary := &DriftSummary{
		DriftByType:    make(map[string]int),
		DriftByService: make(map[string]int),
	}
	
	for _, item := range driftItems {
		// Count by severity
		switch item.Severity {
		case SeverityCritical:
			summary.CriticalSeverityCount++
		case SeverityHigh:
			summary.HighSeverityCount++
		case SeverityMedium:
			summary.MediumSeverityCount++
		case SeverityLow:
			summary.LowSeverityCount++
		}
		
		// Count by drift type
		summary.DriftByType[item.DriftType]++
		
		// Extract service from resource type and count
		service := dd.extractServiceFromResourceType(item.ResourceType)
		summary.DriftByService[service]++
	}
	
	// Calculate compliance score (simple algorithm)
	totalItems := len(driftItems)
	if totalItems == 0 {
		summary.ComplianceScore = 100.0
	} else {
		weightedScore := float64(summary.CriticalSeverityCount*4 + summary.HighSeverityCount*3 + summary.MediumSeverityCount*2 + summary.LowSeverityCount)
		maxScore := float64(totalItems * 4)
		summary.ComplianceScore = math.Max(0, 100.0-(weightedScore/maxScore)*100.0)
	}
	
	// Note: SecurityScore removed as it's not part of DriftSummary type
	
	return summary
}

func (dd *DriftDetector) assessCompliance(driftItems []*DriftItem) *ComplianceStatus {
	status := &ComplianceStatus{
		FrameworkScores: make(map[string]float64),
		Violations:      []ComplianceViolation{},
		Recommendations: []ComplianceRecommendation{},
	}
	
	// Assess against compliance rules
	for _, rule := range dd.complianceRules {
		violations := dd.checkComplianceRule(rule, driftItems)
		status.Violations = append(status.Violations, violations...)
	}
	
	// Calculate overall score
	if len(driftItems) == 0 {
		status.OverallScore = 100.0
	} else {
		violationCount := len(status.Violations)
		status.OverallScore = math.Max(0, 100.0-float64(violationCount)/float64(len(driftItems))*100.0)
	}
	
	return status
}

func (dd *DriftDetector) checkComplianceRule(rule *ComplianceRule, driftItems []*DriftItem) []ComplianceViolation {
	var violations []ComplianceViolation
	
	for _, item := range driftItems {
		// Check if drift item violates the compliance rule
		if dd.violatesRule(rule, item) {
			violation := ComplianceViolation{
				Framework:   rule.Framework,
				Rule:        rule.ID,
				Severity:    rule.Severity,
				ResourceID:  item.ResourceID,
				Description: rule.Description,
				Remediation: rule.Remediation,
			}
			violations = append(violations, violation)
		}
	}
	
	return violations
}

func (dd *DriftDetector) violatesRule(rule *ComplianceRule, item *DriftItem) bool {
	// Check if the resource type matches
	for _, resourceType := range rule.ResourceTypes {
		if resourceType == item.ResourceType || resourceType == "*" {
			// Check if the field and value violate the rule
			if dd.fieldViolatesRule(rule, item.Field, item.CurrentValue) {
				return true
			}
		}
	}
	return false
}

func (dd *DriftDetector) fieldViolatesRule(rule *ComplianceRule, field string, value interface{}) bool {
	// Check required fields
	if expectedValue, required := rule.RequiredFields[field]; required {
		if !dd.valuesEqual(value, expectedValue) {
			return true
		}
	}
	
	// Check forbidden values
	if forbiddenValue, forbidden := rule.ForbiddenValues[field]; forbidden {
		if dd.valuesEqual(value, forbiddenValue) {
			return true
		}
	}
	
	return false
}

func (dd *DriftDetector) generateRecommendations(report *DriftReport) []string {
	var recommendations []string
	
	if report.Summary.CriticalSeverityCount > 0 {
		recommendations = append(recommendations, "🚨 Address critical severity drift items immediately")
	}
	
	if report.Summary.HighSeverityCount > 0 {
		recommendations = append(recommendations, "⚠️ Review and address high severity drift items")
	}
	
	if report.Summary.ComplianceScore < 80 {
		recommendations = append(recommendations, "📋 Improve compliance posture - score below 80%")
	}
	
	// Security-related recommendation based on severity counts
	if report.Summary.HighSeverityCount+report.Summary.CriticalSeverityCount > 0 {
		recommendations = append(recommendations, "🔒 Address security-related drift items")
	}
	
	// Add service-specific recommendations
	for service, count := range report.Summary.DriftByService {
		if count > 5 {
			recommendations = append(recommendations, fmt.Sprintf("🔧 Review %s service configuration - %d drift items detected", service, count))
		}
	}
	
	return recommendations
}

func (dd *DriftDetector) extractServiceFromResourceType(resourceType string) string {
	// Extract service from resource type (e.g., "compute.googleapis.com/Instance" -> "compute")
	if strings.Contains(resourceType, ".googleapis.com/") {
		parts := strings.Split(resourceType, ".googleapis.com/")
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return "unknown"
}

func (dd *DriftDetector) initializeComplianceRules() {
	// Initialize with common GCP compliance rules
	
	// CIS GCP Benchmark rules
	dd.complianceRules["cis-gcp-1.1"] = &ComplianceRule{
		ID:            "cis-gcp-1.1",
		Framework:     "CIS",
		Category:      "Identity and Access Management",
		ResourceTypes: []string{"compute.googleapis.com/Instance"},
		ForbiddenValues: map[string]interface{}{
			"service_accounts": "default",
		},
		Severity:    SeverityHigh,
		Description: "Ensure that instances are not configured to use the default service account",
		Remediation: "Configure instances to use a custom service account with minimal required permissions",
	}
	
	dd.complianceRules["cis-gcp-2.1"] = &ComplianceRule{
		ID:            "cis-gcp-2.1",
		Framework:     "CIS",
		Category:      "Logging and Monitoring",
		ResourceTypes: []string{"*"},
		RequiredFields: map[string]interface{}{
			"audit_logging_enabled": true,
		},
		Severity:    SeverityMedium,
		Description: "Ensure that audit logging is enabled",
		Remediation: "Enable audit logging for all supported services",
	}
	
	// Add more compliance rules as needed
}

func (dd *DriftDetector) initializeDriftThresholds() {
	// Initialize drift detection thresholds
	dd.driftThresholds["configuration_change"] = 0.1
	dd.driftThresholds["iam_policy_change"] = 0.0 // Zero tolerance for IAM changes
	dd.driftThresholds["security_setting_change"] = 0.0
	dd.driftThresholds["tag_change"] = 0.3
}

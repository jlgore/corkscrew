package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AlertingSystem provides intelligent change alerting and notifications
type AlertingSystem struct {
	rules       []*AlertRule
	channels    map[string]AlertChannel
	processor   *AlertProcessor
	throttler   *AlertThrottler
	mu          sync.RWMutex
	config      *AlertingConfig
}

// AlertRule defines conditions and actions for change alerts
type AlertRule struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Enabled           bool                   `json:"enabled"`
	Provider          string                 `json:"provider"`
	Conditions        *AlertConditions       `json:"conditions"`
	Actions           []*AlertAction         `json:"actions"`
	Throttling        *AlertThrottling       `json:"throttling"`
	Priority          string                 `json:"priority"` // "critical", "high", "medium", "low"
	Tags              map[string]string      `json:"tags"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
	LastTriggered     *time.Time             `json:"last_triggered,omitempty"`
	TriggerCount      int                    `json:"trigger_count"`
}

// AlertConditions defines when an alert should trigger
type AlertConditions struct {
	ChangeTypes       []ChangeType           `json:"change_types,omitempty"`
	Severities        []ChangeSeverity       `json:"severities,omitempty"`
	Services          []string               `json:"services,omitempty"`
	ResourceTypes     []string               `json:"resource_types,omitempty"`
	Projects          []string               `json:"projects,omitempty"`
	Regions           []string               `json:"regions,omitempty"`
	ResourcePatterns  []string               `json:"resource_patterns,omitempty"` // Regex patterns for resource IDs
	ImpactThreshold   float64                `json:"impact_threshold,omitempty"`
	FrequencyThreshold *FrequencyThreshold   `json:"frequency_threshold,omitempty"`
	ComplianceFrameworks []string            `json:"compliance_frameworks,omitempty"`
	CustomConditions  map[string]interface{} `json:"custom_conditions,omitempty"`
}

// FrequencyThreshold defines thresholds for change frequency alerts
type FrequencyThreshold struct {
	Count      int           `json:"count"`
	TimeWindow time.Duration `json:"time_window"`
	Scope      string        `json:"scope"` // "resource", "service", "project", "global"
}

// AlertAction defines what happens when an alert triggers
type AlertAction struct {
	Type       string                 `json:"type"` // "webhook", "email", "slack", "teams", "pagerduty", "log"
	Config     map[string]interface{} `json:"config"`
	Template   *AlertTemplate         `json:"template,omitempty"`
	Enabled    bool                   `json:"enabled"`
}

// AlertTemplate defines message formatting for alerts
type AlertTemplate struct {
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	Format      string `json:"format"` // "text", "html", "markdown", "json"
	IncludeDetails bool `json:"include_details"`
}

// AlertThrottling controls alert frequency
type AlertThrottling struct {
	Enabled       bool          `json:"enabled"`
	Interval      time.Duration `json:"interval"`
	MaxAlerts     int           `json:"max_alerts"`
	GroupBy       []string      `json:"group_by"` // Fields to group alerts by
	ResetInterval time.Duration `json:"reset_interval"`
}

// AlertChannel interface for different notification channels
type AlertChannel interface {
	SendAlert(ctx context.Context, alert *Alert) error
	GetType() string
	GetConfig() map[string]interface{}
	IsHealthy(ctx context.Context) bool
}

// Alert represents a triggered alert with context
type Alert struct {
	ID              string            `json:"id"`
	RuleID          string            `json:"rule_id"`
	RuleName        string            `json:"rule_name"`
	Severity        ChangeSeverity    `json:"severity"`
	Priority        string            `json:"priority"`
	Title           string            `json:"title"`
	Message         string            `json:"message"`
	TriggeredAt     time.Time         `json:"triggered_at"`
	TriggeredBy     *ChangeEvent      `json:"triggered_by"`
	Context         *AlertContext     `json:"context"`
	Status          string            `json:"status"` // "open", "acknowledged", "resolved"
	AcknowledgedBy  string            `json:"acknowledged_by,omitempty"`
	AcknowledgedAt  *time.Time        `json:"acknowledged_at,omitempty"`
	ResolvedAt      *time.Time        `json:"resolved_at,omitempty"`
	Tags            map[string]string `json:"tags"`
}

// AlertContext provides additional information about the alert
type AlertContext struct {
	Provider        string                 `json:"provider"`
	ResourceCount   int                    `json:"resource_count"`
	ServiceCount    int                    `json:"service_count"`
	ProjectCount    int                    `json:"project_count"`
	RelatedChanges  []*ChangeEvent         `json:"related_changes,omitempty"`
	ImpactSummary   *ImpactSummary         `json:"impact_summary,omitempty"`
	Recommendations []string               `json:"recommendations,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// ImpactSummary summarizes the impact of changes that triggered an alert
type ImpactSummary struct {
	TotalRiskScore     float64 `json:"total_risk_score"`
	AverageRiskScore   float64 `json:"average_risk_score"`
	SecurityImpact     int     `json:"security_impact"`
	AvailabilityImpact int     `json:"availability_impact"`
	CostImpact         float64 `json:"cost_impact"`
	ComplianceImpact   int     `json:"compliance_impact"`
}

// AlertingConfig provides configuration for the alerting system
type AlertingConfig struct {
	DefaultChannel      string        `json:"default_channel"`
	MaxConcurrentAlerts int           `json:"max_concurrent_alerts"`
	AlertRetention      time.Duration `json:"alert_retention"`
	EnableThrottling    bool          `json:"enable_throttling"`
	DefaultThrottling   *AlertThrottling `json:"default_throttling"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
}

// AlertProcessor handles alert processing logic
type AlertProcessor struct {
	rules    []*AlertRule
	channels map[string]AlertChannel
	mu       sync.RWMutex
}

// AlertThrottler manages alert frequency and deduplication
type AlertThrottler struct {
	alertCounts map[string]*ThrottleState
	mu          sync.RWMutex
	config      *AlertingConfig
}

// ThrottleState tracks throttling information for alert rules
type ThrottleState struct {
	Count         int       `json:"count"`
	LastAlert     time.Time `json:"last_alert"`
	WindowStart   time.Time `json:"window_start"`
	GroupedAlerts map[string]int `json:"grouped_alerts"`
}

// NewAlertingSystem creates a new alerting system
func NewAlertingSystem() *AlertingSystem {
	config := &AlertingConfig{
		DefaultChannel:      "log",
		MaxConcurrentAlerts: 100,
		AlertRetention:      30 * 24 * time.Hour, // 30 days
		EnableThrottling:    true,
		DefaultThrottling: &AlertThrottling{
			Enabled:       true,
			Interval:      5 * time.Minute,
			MaxAlerts:     10,
			ResetInterval: 1 * time.Hour,
		},
		HealthCheckInterval: 5 * time.Minute,
	}

	as := &AlertingSystem{
		rules:     make([]*AlertRule, 0),
		channels:  make(map[string]AlertChannel),
		config:    config,
		throttler: NewAlertThrottler(config),
	}

	// Initialize default channels
	as.initializeDefaultChannels()

	// Initialize alert processor
	as.processor = &AlertProcessor{
		rules:    as.rules,
		channels: as.channels,
	}

	return as
}

// ProcessChange processes a change event for alert triggers
func (as *AlertingSystem) ProcessChange(change *ChangeEvent) {
	as.mu.RLock()
	rules := make([]*AlertRule, len(as.rules))
	copy(rules, as.rules)
	as.mu.RUnlock()

	// Process each rule
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// Check if the change matches the rule conditions
		if as.matchesRule(change, rule) {
			// Check throttling
			if as.config.EnableThrottling && as.throttler.ShouldThrottle(rule, change) {
				log.Printf("Alert throttled for rule %s", rule.ID)
				continue
			}

			// Create and send alert
			alert := as.createAlert(rule, change)
			go as.sendAlert(alert)

			// Update rule statistics
			as.updateRuleStats(rule)
		}
	}
}

// AddRule adds a new alert rule
func (as *AlertingSystem) AddRule(rule *AlertRule) error {
	if rule == nil {
		return fmt.Errorf("rule cannot be nil")
	}

	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule_%d", time.Now().Unix())
	}

	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	as.mu.Lock()
	defer as.mu.Unlock()

	as.rules = append(as.rules, rule)
	as.processor.UpdateRules(as.rules)

	log.Printf("Added alert rule: %s", rule.Name)
	return nil
}

// RemoveRule removes an alert rule
func (as *AlertingSystem) RemoveRule(ruleID string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	for i, rule := range as.rules {
		if rule.ID == ruleID {
			as.rules = append(as.rules[:i], as.rules[i+1:]...)
			as.processor.UpdateRules(as.rules)
			log.Printf("Removed alert rule: %s", ruleID)
			return nil
		}
	}

	return fmt.Errorf("rule not found: %s", ruleID)
}

// AddChannel adds a new alert channel
func (as *AlertingSystem) AddChannel(name string, channel AlertChannel) {
	as.mu.Lock()
	defer as.mu.Unlock()

	as.channels[name] = channel
	as.processor.UpdateChannels(as.channels)

	log.Printf("Added alert channel: %s (%s)", name, channel.GetType())
}

// GetRules returns all alert rules
func (as *AlertingSystem) GetRules() []*AlertRule {
	as.mu.RLock()
	defer as.mu.RUnlock()

	rules := make([]*AlertRule, len(as.rules))
	copy(rules, as.rules)
	return rules
}

// Helper methods

func (as *AlertingSystem) matchesRule(change *ChangeEvent, rule *AlertRule) bool {
	conditions := rule.Conditions
	if conditions == nil {
		return false
	}

	// Provider filter
	if rule.Provider != "" && rule.Provider != change.Provider {
		return false
	}

	// Change type filter
	if len(conditions.ChangeTypes) > 0 {
		found := false
		for _, changeType := range conditions.ChangeTypes {
			if changeType == change.ChangeType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Severity filter
	if len(conditions.Severities) > 0 {
		found := false
		for _, severity := range conditions.Severities {
			if severity == change.Severity {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Service filter
	if len(conditions.Services) > 0 && !contains(conditions.Services, change.Service) {
		return false
	}

	// Resource type filter
	if len(conditions.ResourceTypes) > 0 && !contains(conditions.ResourceTypes, change.ResourceType) {
		return false
	}

	// Project filter
	if len(conditions.Projects) > 0 && !contains(conditions.Projects, change.Project) {
		return false
	}

	// Region filter
	if len(conditions.Regions) > 0 && !contains(conditions.Regions, change.Region) {
		return false
	}

	// Impact threshold
	if conditions.ImpactThreshold > 0 && change.ImpactAssessment != nil {
		if change.ImpactAssessment.RiskScore < conditions.ImpactThreshold {
			return false
		}
	}

	// Compliance framework filter
	if len(conditions.ComplianceFrameworks) > 0 && change.ComplianceImpact != nil {
		found := false
		for _, framework := range conditions.ComplianceFrameworks {
			if contains(change.ComplianceImpact.AffectedFrameworks, framework) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Resource pattern matching (simplified)
	if len(conditions.ResourcePatterns) > 0 {
		found := false
		for _, pattern := range conditions.ResourcePatterns {
			if strings.Contains(change.ResourceID, pattern) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (as *AlertingSystem) createAlert(rule *AlertRule, change *ChangeEvent) *Alert {
	alert := &Alert{
		ID:          fmt.Sprintf("alert_%s_%d", rule.ID, time.Now().UnixNano()),
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Severity:    change.Severity,
		Priority:    rule.Priority,
		Title:       as.generateAlertTitle(rule, change),
		Message:     as.generateAlertMessage(rule, change),
		TriggeredAt: time.Now(),
		TriggeredBy: change,
		Status:      "open",
		Tags:        rule.Tags,
		Context:     as.buildAlertContext(change),
	}

	return alert
}

func (as *AlertingSystem) generateAlertTitle(rule *AlertRule, change *ChangeEvent) string {
	return fmt.Sprintf("[%s] %s - %s %s in %s",
		strings.ToUpper(string(change.Severity)),
		rule.Name,
		string(change.ChangeType),
		change.ResourceType,
		change.Service)
}

func (as *AlertingSystem) generateAlertMessage(rule *AlertRule, change *ChangeEvent) string {
	var message strings.Builder
	
	message.WriteString(fmt.Sprintf("Alert triggered by rule: %s\n", rule.Name))
	message.WriteString(fmt.Sprintf("Change Type: %s\n", change.ChangeType))
	message.WriteString(fmt.Sprintf("Resource: %s (%s)\n", change.ResourceID, change.ResourceType))
	message.WriteString(fmt.Sprintf("Service: %s\n", change.Service))
	message.WriteString(fmt.Sprintf("Severity: %s\n", change.Severity))
	message.WriteString(fmt.Sprintf("Timestamp: %s\n", change.Timestamp.Format(time.RFC3339)))
	
	if change.Project != "" {
		message.WriteString(fmt.Sprintf("Project: %s\n", change.Project))
	}
	
	if change.Region != "" {
		message.WriteString(fmt.Sprintf("Region: %s\n", change.Region))
	}

	if change.ImpactAssessment != nil {
		message.WriteString(fmt.Sprintf("Risk Score: %.1f\n", change.ImpactAssessment.RiskScore))
		
		if len(change.ImpactAssessment.Recommendations) > 0 {
			message.WriteString("Recommendations:\n")
			for _, rec := range change.ImpactAssessment.Recommendations {
				message.WriteString(fmt.Sprintf("- %s\n", rec))
			}
		}
	}

	return message.String()
}

func (as *AlertingSystem) buildAlertContext(change *ChangeEvent) *AlertContext {
	context := &AlertContext{
		Provider:     change.Provider,
		ResourceCount: 1,
		ServiceCount:  1,
		ProjectCount:  1,
		Metadata:     make(map[string]interface{}),
	}

	if change.ImpactAssessment != nil {
		context.ImpactSummary = &ImpactSummary{
			TotalRiskScore:   change.ImpactAssessment.RiskScore,
			AverageRiskScore: change.ImpactAssessment.RiskScore,
		}
		
		// Count impact types
		if change.ImpactAssessment.SecurityImpact.Level >= SeverityMedium {
			context.ImpactSummary.SecurityImpact = 1
		}
		if change.ImpactAssessment.AvailabilityImpact.Level >= SeverityMedium {
			context.ImpactSummary.AvailabilityImpact = 1
		}
		if change.ImpactAssessment.CostImpact.EstimatedChange != 0 {
			context.ImpactSummary.CostImpact = change.ImpactAssessment.CostImpact.EstimatedChange
		}
		
		context.Recommendations = change.ImpactAssessment.Recommendations
	}

	if change.ComplianceImpact != nil {
		context.ImpactSummary.ComplianceImpact = len(change.ComplianceImpact.AffectedFrameworks)
	}

	return context
}

func (as *AlertingSystem) sendAlert(alert *Alert) {
	ctx := context.Background()

	// Get the rule to determine which actions to execute
	var rule *AlertRule
	as.mu.RLock()
	for _, r := range as.rules {
		if r.ID == alert.RuleID {
			rule = r
			break
		}
	}
	as.mu.RUnlock()

	if rule == nil {
		log.Printf("Rule not found for alert: %s", alert.RuleID)
		return
	}

	// Execute each action
	for _, action := range rule.Actions {
		if !action.Enabled {
			continue
		}

		channel, exists := as.channels[action.Type]
		if !exists {
			log.Printf("Alert channel not found: %s", action.Type)
			continue
		}

		// Apply template if configured
		if action.Template != nil {
			as.applyTemplate(alert, action.Template)
		}

		// Send alert
		if err := channel.SendAlert(ctx, alert); err != nil {
			log.Printf("Failed to send alert via %s: %v", action.Type, err)
		} else {
			log.Printf("Alert sent successfully via %s: %s", action.Type, alert.Title)
		}
	}
}

func (as *AlertingSystem) applyTemplate(alert *Alert, template *AlertTemplate) {
	if template.Subject != "" {
		alert.Title = as.processTemplate(template.Subject, alert)
	}
	
	if template.Body != "" {
		alert.Message = as.processTemplate(template.Body, alert)
	}
}

func (as *AlertingSystem) processTemplate(template string, alert *Alert) string {
	// Simple template processing - in production would use a proper template engine
	result := template
	
	result = strings.ReplaceAll(result, "{{.Title}}", alert.Title)
	result = strings.ReplaceAll(result, "{{.Severity}}", string(alert.Severity))
	result = strings.ReplaceAll(result, "{{.Priority}}", alert.Priority)
	result = strings.ReplaceAll(result, "{{.RuleName}}", alert.RuleName)
	result = strings.ReplaceAll(result, "{{.TriggeredAt}}", alert.TriggeredAt.Format(time.RFC3339))
	
	if alert.TriggeredBy != nil {
		result = strings.ReplaceAll(result, "{{.ResourceID}}", alert.TriggeredBy.ResourceID)
		result = strings.ReplaceAll(result, "{{.ResourceType}}", alert.TriggeredBy.ResourceType)
		result = strings.ReplaceAll(result, "{{.Service}}", alert.TriggeredBy.Service)
		result = strings.ReplaceAll(result, "{{.ChangeType}}", string(alert.TriggeredBy.ChangeType))
	}
	
	return result
}

func (as *AlertingSystem) updateRuleStats(rule *AlertRule) {
	now := time.Now()
	rule.LastTriggered = &now
	rule.TriggerCount++
	rule.UpdatedAt = now
}

func (as *AlertingSystem) initializeDefaultChannels() {
	// Add log channel
	as.channels["log"] = &LogAlertChannel{}
	
	// Add webhook channel
	as.channels["webhook"] = &WebhookAlertChannel{}
}

// Alert Channel Implementations

// LogAlertChannel sends alerts to the application log
type LogAlertChannel struct{}

func (lac *LogAlertChannel) SendAlert(ctx context.Context, alert *Alert) error {
	log.Printf("ALERT [%s/%s]: %s - %s", alert.Severity, alert.Priority, alert.Title, alert.Message)
	return nil
}

func (lac *LogAlertChannel) GetType() string {
	return "log"
}

func (lac *LogAlertChannel) GetConfig() map[string]interface{} {
	return map[string]interface{}{"type": "log"}
}

func (lac *LogAlertChannel) IsHealthy(ctx context.Context) bool {
	return true
}

// WebhookAlertChannel sends alerts to HTTP webhooks
type WebhookAlertChannel struct {
	URL     string
	Headers map[string]string
	Timeout time.Duration
}

func (wac *WebhookAlertChannel) SendAlert(ctx context.Context, alert *Alert) error {
	if wac.URL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	payload, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	client := &http.Client{
		Timeout: wac.Timeout,
	}
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}

	req, err := http.NewRequestWithContext(ctx, "POST", wac.URL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range wac.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned error status: %d", resp.StatusCode)
	}

	return nil
}

func (wac *WebhookAlertChannel) GetType() string {
	return "webhook"
}

func (wac *WebhookAlertChannel) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"type": "webhook",
		"url":  wac.URL,
	}
}

func (wac *WebhookAlertChannel) IsHealthy(ctx context.Context) bool {
	if wac.URL == "" {
		return false
	}

	// Simple health check - ping the webhook URL
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "HEAD", wac.URL, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode < 500
}

// Alert Throttling Implementation

func NewAlertThrottler(config *AlertingConfig) *AlertThrottler {
	return &AlertThrottler{
		alertCounts: make(map[string]*ThrottleState),
		config:      config,
	}
}

func (at *AlertThrottler) ShouldThrottle(rule *AlertRule, change *ChangeEvent) bool {
	if !at.config.EnableThrottling {
		return false
	}

	throttling := rule.Throttling
	if throttling == nil {
		throttling = at.config.DefaultThrottling
	}

	if !throttling.Enabled {
		return false
	}

	at.mu.Lock()
	defer at.mu.Unlock()

	// Get or create throttle state
	state, exists := at.alertCounts[rule.ID]
	if !exists {
		state = &ThrottleState{
			GroupedAlerts: make(map[string]int),
			WindowStart:   time.Now(),
		}
		at.alertCounts[rule.ID] = state
	}

	now := time.Now()

	// Check if we need to reset the window
	if now.Sub(state.WindowStart) > throttling.ResetInterval {
		state.Count = 0
		state.WindowStart = now
		state.GroupedAlerts = make(map[string]int)
	}

	// Check if we've exceeded the max alerts for this interval
	if state.Count >= throttling.MaxAlerts {
		return true
	}

	// Check the minimum interval between alerts
	if now.Sub(state.LastAlert) < throttling.Interval {
		return true
	}

	// Update state
	state.Count++
	state.LastAlert = now

	return false
}

// UpdateRules updates the rules in the alert processor
func (ap *AlertProcessor) UpdateRules(rules []*AlertRule) {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	ap.rules = rules
}

// UpdateChannels updates the channels in the alert processor
func (ap *AlertProcessor) UpdateChannels(channels map[string]AlertChannel) {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	ap.channels = channels
}

// Helper function for alerting system
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

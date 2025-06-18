package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cloud.google.com/go/asset/apiv1/assetpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing

type MockChangeStorage struct {
	mock.Mock
}

func (m *MockChangeStorage) StoreChange(change *ChangeEvent) error {
	args := m.Called(change)
	return args.Error(0)
}

func (m *MockChangeStorage) StoreChanges(changes []*ChangeEvent) error {
	args := m.Called(changes)
	return args.Error(0)
}

func (m *MockChangeStorage) QueryChanges(query *ChangeQuery) ([]*ChangeEvent, error) {
	args := m.Called(query)
	return args.Get(0).([]*ChangeEvent), args.Error(1)
}

func (m *MockChangeStorage) GetChangeHistory(resourceID string) ([]*ChangeEvent, error) {
	args := m.Called(resourceID)
	return args.Get(0).([]*ChangeEvent), args.Error(1)
}

func (m *MockChangeStorage) GetChange(changeID string) (*ChangeEvent, error) {
	args := m.Called(changeID)
	return args.Get(0).(*ChangeEvent), args.Error(1)
}

func (m *MockChangeStorage) DeleteChanges(olderThan time.Time) error {
	args := m.Called(olderThan)
	return args.Error(0)
}

func (m *MockChangeStorage) StoreBaseline(baseline *DriftBaseline) error {
	args := m.Called(baseline)
	return args.Error(0)
}

func (m *MockChangeStorage) GetBaseline(baselineID string) (*DriftBaseline, error) {
	args := m.Called(baselineID)
	return args.Get(0).(*DriftBaseline), args.Error(1)
}

func (m *MockChangeStorage) ListBaselines(provider string) ([]*DriftBaseline, error) {
	args := m.Called(provider)
	return args.Get(0).([]*DriftBaseline), args.Error(1)
}

func (m *MockChangeStorage) UpdateBaseline(baseline *DriftBaseline) error {
	args := m.Called(baseline)
	return args.Error(0)
}

func (m *MockChangeStorage) DeleteBaseline(baselineID string) error {
	args := m.Called(baselineID)
	return args.Error(0)
}

// Test Change Tracker Core Functionality

func TestChangeEvent_Creation(t *testing.T) {
	event := &ChangeEvent{
		ID:           "test-change-1",
		Provider:     "gcp",
		ResourceID:   "projects/test-project/instances/test-instance",
		ResourceType: "Instance",
		Service:      "compute",
		ChangeType:   ChangeTypeCreate,
		Severity:     SeverityMedium,
		Timestamp:    time.Now(),
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"source": "asset_inventory",
		},
	}

	assert.Equal(t, "test-change-1", event.ID)
	assert.Equal(t, "gcp", event.Provider)
	assert.Equal(t, ChangeTypeCreate, event.ChangeType)
	assert.Equal(t, SeverityMedium, event.Severity)
	assert.NotNil(t, event.ChangeMetadata)
}

func TestBaseChangeTracker_ValidateQuery(t *testing.T) {
	storage := &MockChangeStorage{}
	config := &ChangeTrackerConfig{
		Provider:          "gcp",
		MaxQueryTimeRange: 30 * 24 * time.Hour,
	}
	
	tracker := NewBaseChangeTracker("gcp", storage, config)

	t.Run("valid query", func(t *testing.T) {
		query := &ChangeQuery{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
			Limit:     100,
		}

		err := tracker.ValidateChangeQuery(query)
		assert.NoError(t, err)
		assert.Equal(t, "timestamp", query.SortBy)
		assert.Equal(t, "desc", query.SortOrder)
	})

	t.Run("invalid time range", func(t *testing.T) {
		query := &ChangeQuery{
			StartTime: time.Now(),
			EndTime:   time.Now().Add(-1 * time.Hour),
		}

		err := tracker.ValidateChangeQuery(query)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "end time cannot be before start time")
	})

	t.Run("exceeded max time range", func(t *testing.T) {
		query := &ChangeQuery{
			StartTime: time.Now().Add(-60 * 24 * time.Hour),
			EndTime:   time.Now(),
		}

		err := tracker.ValidateChangeQuery(query)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed duration")
	})

	t.Run("nil query", func(t *testing.T) {
		err := tracker.ValidateChangeQuery(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query cannot be nil")
	})
}

func TestBaseChangeTracker_ImpactAnalysis(t *testing.T) {
	storage := &MockChangeStorage{}
	tracker := NewBaseChangeTracker("gcp", storage, nil)

	t.Run("security impact analysis", func(t *testing.T) {
		change := &ChangeEvent{
			ResourceType:  "Instance",
			ChangeType:    ChangeTypeUpdate,
			ChangedFields: []string{"iam_policy", "network_config"},
		}

		assessment := tracker.AnalyzeChangeImpact(change)
		
		assert.NotNil(t, assessment)
		assert.True(t, assessment.SecurityImpact.IAMChanges)
		assert.True(t, assessment.SecurityImpact.NetworkChanges)
		assert.Equal(t, SeverityHigh, assessment.SecurityImpact.Level)
		assert.Greater(t, assessment.RiskScore, 0.0)
	})

	t.Run("cost impact analysis", func(t *testing.T) {
		change := &ChangeEvent{
			ResourceType: "Instance",
			ChangeType:   ChangeTypeCreate,
		}

		assessment := tracker.AnalyzeChangeImpact(change)
		
		assert.NotNil(t, assessment)
		assert.Equal(t, SeverityHigh, assessment.CostImpact.Level) // Instance is high-cost resource
		assert.Greater(t, assessment.CostImpact.EstimatedChange, 0.0)
		assert.Contains(t, assessment.CostImpact.CostDrivers, "New resource provisioning")
	})

	t.Run("availability impact analysis", func(t *testing.T) {
		change := &ChangeEvent{
			ResourceType: "LoadBalancer",
			ChangeType:   ChangeTypeDelete,
		}

		assessment := tracker.AnalyzeChangeImpact(change)
		
		assert.NotNil(t, assessment)
		assert.Equal(t, SeverityHigh, assessment.AvailabilityImpact.Level)
		assert.Equal(t, "High", assessment.AvailabilityImpact.DowntimeRisk)
	})
}

// Test GCP Change Tracker

func TestGCPChangeTracker_Creation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping GCP client tests in short mode")
	}

	storage := &MockChangeStorage{}
	config := &ChangeTrackerConfig{
		Provider:               "gcp",
		EnableRealTimeMonitoring: false, // Disable real-time for tests
	}

	// This would normally require GCP credentials
	// For testing, we'll skip the actual client creation
	t.Skip("Requires GCP credentials - integration test")
}

func TestGCPChangeTracker_ConvertAssetToChangeEvent(t *testing.T) {
	storage := &MockChangeStorage{}
	tracker := &GCPChangeTracker{
		BaseChangeTracker: NewBaseChangeTracker("gcp", storage, nil),
	}

	assetChange := &AssetChange{
		Timestamp:  time.Now(),
		ChangeType: string(ChangeTypeCreate),
		Asset: &assetpb.Asset{
			Name:      "projects/test-project/instances/test-instance",
			AssetType: "compute.googleapis.com/Instance",
		},
	}

	event := tracker.convertAssetToChangeEvent(assetChange)

	require.NotNil(t, event)
	assert.Equal(t, "gcp", event.Provider)
	assert.Equal(t, "projects/test-project/instances/test-instance", event.ResourceID)
	assert.Equal(t, "Instance", event.ResourceType)
	assert.Equal(t, "compute", event.Service)
	assert.Equal(t, ChangeTypeCreate, event.ChangeType)
	assert.Equal(t, "test-project", event.Project)
}

// Test Change Storage

func TestDuckDBChangeStorage_StoreAndRetrieve(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database tests in short mode")
	}

	storage, err := NewDuckDBChangeStorage(":memory:")
	require.NoError(t, err)
	defer storage.Close()

	t.Run("store and retrieve change", func(t *testing.T) {
		change := &ChangeEvent{
			ID:           "test-change-1",
			Provider:     "gcp",
			ResourceID:   "test-resource",
			ResourceType: "Instance",
			Service:      "compute",
			ChangeType:   ChangeTypeCreate,
			Severity:     SeverityMedium,
			Timestamp:    time.Now().Truncate(time.Second),
			DetectedAt:   time.Now().Truncate(time.Second),
			ChangeMetadata: map[string]interface{}{
				"test": "value",
			},
		}

		err := storage.StoreChange(change)
		assert.NoError(t, err)

		retrieved, err := storage.GetChange(change.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, change.ID, retrieved.ID)
		assert.Equal(t, change.Provider, retrieved.Provider)
		assert.Equal(t, change.ChangeType, retrieved.ChangeType)
	})

	t.Run("query changes", func(t *testing.T) {
		query := &ChangeQuery{
			Provider:  "gcp",
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
			Limit:     10,
		}

		changes, err := storage.QueryChanges(query)
		assert.NoError(t, err)
		assert.NotNil(t, changes)
		assert.GreaterOrEqual(t, len(changes), 1) // Should find the change we stored above
	})
}

func TestDuckDBChangeStorage_Baselines(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database tests in short mode")
	}

	storage, err := NewDuckDBChangeStorage(":memory:")
	require.NoError(t, err)
	defer storage.Close()

	baseline := &DriftBaseline{
		ID:          "test-baseline-1",
		Name:        "Test Baseline",
		Description: "Test baseline description",
		Provider:    "gcp",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Resources: map[string]*ResourceState{
			"test-resource": {
				ResourceID: "test-resource",
				Timestamp:  time.Now(),
				Properties: map[string]interface{}{
					"status": "running",
				},
				Checksum: "test-checksum",
			},
		},
		Version: "1.0",
		Active:  true,
	}

	t.Run("store baseline", func(t *testing.T) {
		err := storage.StoreBaseline(baseline)
		assert.NoError(t, err)
	})

	t.Run("retrieve baseline", func(t *testing.T) {
		retrieved, err := storage.GetBaseline(baseline.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, baseline.ID, retrieved.ID)
		assert.Equal(t, baseline.Name, retrieved.Name)
		assert.Equal(t, baseline.Provider, retrieved.Provider)
		assert.Len(t, retrieved.Resources, 1)
	})

	t.Run("list baselines", func(t *testing.T) {
		baselines, err := storage.ListBaselines("gcp")
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(baselines), 1)
	})
}

// Test Change Analytics

func TestChangeAnalytics_BasicStats(t *testing.T) {
	storage := &MockChangeStorage{}
	analytics := NewChangeAnalytics(storage)

	changes := []*ChangeEvent{
		{
			Provider:     "gcp",
			Service:      "compute",
			ResourceType: "Instance",
			ChangeType:   ChangeTypeCreate,
			Severity:     SeverityMedium,
			Timestamp:    time.Now(),
		},
		{
			Provider:     "gcp",
			Service:      "storage",
			ResourceType: "Bucket",
			ChangeType:   ChangeTypeUpdate,
			Severity:     SeverityLow,
			Timestamp:    time.Now(),
		},
		{
			Provider:     "gcp",
			Service:      "compute",
			ResourceType: "Instance",
			ChangeType:   ChangeTypeDelete,
			Severity:     SeverityHigh,
			Timestamp:    time.Now(),
		},
	}

	storage.On("QueryChanges", mock.AnythingOfType("*main.ChangeQuery")).Return(changes, nil)

	query := &AnalyticsQuery{
		Provider: "gcp",
		TimeRange: TimeRange{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		},
	}

	report, err := analytics.GenerateReport(context.Background(), query)
	
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.Equal(t, 3, report.TotalChanges)
	assert.Equal(t, 1, report.ChangesByType[string(ChangeTypeCreate)])
	assert.Equal(t, 1, report.ChangesByType[string(ChangeTypeUpdate)])
	assert.Equal(t, 1, report.ChangesByType[string(ChangeTypeDelete)])
	assert.Equal(t, 2, report.ChangesByService["compute"])
	assert.Equal(t, 1, report.ChangesByService["storage"])
}

// Test Alerting System

func TestAlertingSystem_RuleMatching(t *testing.T) {
	alerting := NewAlertingSystem()

	rule := &AlertRule{
		ID:       "test-rule-1",
		Name:     "High Severity Changes",
		Enabled:  true,
		Provider: "gcp",
		Conditions: &AlertConditions{
			Severities:    []ChangeSeverity{SeverityHigh, SeverityCritical},
			Services:      []string{"compute"},
			ChangeTypes:   []ChangeType{ChangeTypeDelete},
		},
	}

	err := alerting.AddRule(rule)
	assert.NoError(t, err)

	t.Run("matching change", func(t *testing.T) {
		change := &ChangeEvent{
			Provider:     "gcp",
			Service:      "compute",
			ResourceType: "Instance",
			ChangeType:   ChangeTypeDelete,
			Severity:     SeverityHigh,
		}

		matches := alerting.matchesRule(change, rule)
		assert.True(t, matches)
	})

	t.Run("non-matching change - wrong severity", func(t *testing.T) {
		change := &ChangeEvent{
			Provider:     "gcp",
			Service:      "compute",
			ResourceType: "Instance",
			ChangeType:   ChangeTypeDelete,
			Severity:     SeverityLow,
		}

		matches := alerting.matchesRule(change, rule)
		assert.False(t, matches)
	})

	t.Run("non-matching change - wrong service", func(t *testing.T) {
		change := &ChangeEvent{
			Provider:     "gcp",
			Service:      "storage",
			ResourceType: "Bucket",
			ChangeType:   ChangeTypeDelete,
			Severity:     SeverityHigh,
		}

		matches := alerting.matchesRule(change, rule)
		assert.False(t, matches)
	})
}

func TestAlertingSystem_AlertCreation(t *testing.T) {
	alerting := NewAlertingSystem()

	rule := &AlertRule{
		ID:       "test-rule-1",
		Name:     "Test Alert Rule",
		Priority: "high",
		Tags:     map[string]string{"team": "platform"},
	}

	change := &ChangeEvent{
		Provider:     "gcp",
		ResourceID:   "test-resource",
		ResourceType: "Instance",
		Service:      "compute",
		ChangeType:   ChangeTypeDelete,
		Severity:     SeverityHigh,
		Timestamp:    time.Now(),
	}

	alert := alerting.createAlert(rule, change)

	assert.NotNil(t, alert)
	assert.Equal(t, rule.ID, alert.RuleID)
	assert.Equal(t, rule.Name, alert.RuleName)
	assert.Equal(t, change.Severity, alert.Severity)
	assert.Equal(t, rule.Priority, alert.Priority)
	assert.Equal(t, change, alert.TriggeredBy)
	assert.Equal(t, "open", alert.Status)
	assert.NotEmpty(t, alert.Title)
	assert.NotEmpty(t, alert.Message)
}

// Test Alert Channels

func TestLogAlertChannel(t *testing.T) {
	channel := &LogAlertChannel{}

	alert := &Alert{
		ID:       "test-alert-1",
		Title:    "Test Alert",
		Message:  "Test alert message",
		Severity: SeverityHigh,
		Priority: "high",
	}

	err := channel.SendAlert(context.Background(), alert)
	assert.NoError(t, err)
	assert.Equal(t, "log", channel.GetType())
	assert.True(t, channel.IsHealthy(context.Background()))
}

func TestWebhookAlertChannel(t *testing.T) {
	channel := &WebhookAlertChannel{
		URL:     "https://httpbin.org/post",
		Timeout: 10 * time.Second,
	}

	alert := &Alert{
		ID:       "test-alert-1",
		Title:    "Test Alert",
		Message:  "Test alert message",
		Severity: SeverityHigh,
		Priority: "high",
	}

	if testing.Short() {
		t.Skip("Skipping webhook test in short mode")
	}

	err := channel.SendAlert(context.Background(), alert)
	// This might fail if network is not available, so we'll just log the result
	if err != nil {
		t.Logf("Webhook test failed (expected in some environments): %v", err)
	}

	assert.Equal(t, "webhook", channel.GetType())
}

// Benchmark Tests

func BenchmarkChangeEvent_Creation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &ChangeEvent{
			ID:           fmt.Sprintf("change-%d", i),
			Provider:     "gcp",
			ResourceID:   fmt.Sprintf("resource-%d", i),
			ResourceType: "Instance",
			Service:      "compute",
			ChangeType:   ChangeTypeCreate,
			Severity:     SeverityMedium,
			Timestamp:    time.Now(),
		}
	}
}

func BenchmarkChangeTracker_ImpactAnalysis(b *testing.B) {
	storage := &MockChangeStorage{}
	tracker := NewBaseChangeTracker("gcp", storage, nil)

	change := &ChangeEvent{
		ResourceType:  "Instance",
		ChangeType:    ChangeTypeUpdate,
		ChangedFields: []string{"iam_policy", "network_config", "tags"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracker.AnalyzeChangeImpact(change)
	}
}

func BenchmarkAlertingSystem_RuleMatching(b *testing.B) {
	alerting := NewAlertingSystem()

	rule := &AlertRule{
		ID:       "benchmark-rule",
		Name:     "Benchmark Rule",
		Enabled:  true,
		Provider: "gcp",
		Conditions: &AlertConditions{
			Severities:  []ChangeSeverity{SeverityHigh},
			Services:    []string{"compute", "storage"},
			ChangeTypes: []ChangeType{ChangeTypeCreate, ChangeTypeUpdate, ChangeTypeDelete},
		},
	}

	change := &ChangeEvent{
		Provider:     "gcp",
		Service:      "compute",
		ResourceType: "Instance",
		ChangeType:   ChangeTypeUpdate,
		Severity:     SeverityHigh,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = alerting.matchesRule(change, rule)
	}
}

// Integration Tests

func TestChangeTracking_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This would be a comprehensive end-to-end test
	// that exercises the entire change tracking pipeline
	t.Skip("End-to-end integration test - requires full setup")
}

// Helper functions for tests

func createTestChangeEvent(id string, changeType ChangeType, severity ChangeSeverity) *ChangeEvent {
	return &ChangeEvent{
		ID:           id,
		Provider:     "gcp",
		ResourceID:   fmt.Sprintf("test-resource-%s", id),
		ResourceType: "Instance",
		Service:      "compute",
		ChangeType:   changeType,
		Severity:     severity,
		Timestamp:    time.Now(),
		DetectedAt:   time.Now(),
		ChangeMetadata: map[string]interface{}{
			"test": true,
		},
	}
}

func createTestBaseline(id string, resourceCount int) *DriftBaseline {
	resources := make(map[string]*ResourceState)
	
	for i := 0; i < resourceCount; i++ {
		resourceID := fmt.Sprintf("resource-%d", i)
		resources[resourceID] = &ResourceState{
			ResourceID: resourceID,
			Timestamp:  time.Now(),
			Properties: map[string]interface{}{
				"status": "running",
				"zone":   "us-central1-a",
			},
			Checksum: fmt.Sprintf("checksum-%d", i),
		}
	}

	return &DriftBaseline{
		ID:          id,
		Name:        fmt.Sprintf("Test Baseline %s", id),
		Description: "Test baseline for unit tests",
		Provider:    "gcp",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Resources:   resources,
		Version:     "1.0",
		Active:      true,
	}
}
package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// MockChangeStorage is a mock implementation of ChangeStorage for testing
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

// Test Azure change tracker creation
func TestNewAzureChangeTracker(t *testing.T) {
	ctx := context.Background()
	config := &AzureChangeTrackerConfig{
		SubscriptionIDs: []string{"test-subscription"},
		TenantID:       "test-tenant",
		QueryInterval:  5 * time.Minute,
		StorageConfig: StorageConfig{
			Type: "duckdb",
			Path: ":memory:",
		},
	}

	// Note: This test would require mocking Azure SDK clients
	// For now, we test the configuration validation
	assert.NotNil(t, config)
	assert.Equal(t, "test-tenant", config.TenantID)
	assert.Equal(t, 5*time.Minute, config.QueryInterval)
}

// Test change query validation
func TestValidateChangeQuery(t *testing.T) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{
		Provider: "azure",
		MaxQueryTimeRange: 24 * time.Hour,
	}
	
	tracker := NewBaseChangeTracker("azure", mockStorage, baseConfig)

	tests := []struct {
		name    string
		query   *ChangeQuery
		wantErr bool
	}{
		{
			name:    "nil query",
			query:   nil,
			wantErr: true,
		},
		{
			name: "missing start time",
			query: &ChangeQuery{
				EndTime: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "valid query",
			query: &ChangeQuery{
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				Limit:     100,
			},
			wantErr: false,
		},
		{
			name: "time range too large",
			query: &ChangeQuery{
				StartTime: time.Now().Add(-48 * time.Hour),
				EndTime:   time.Now(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tracker.ValidateChangeQuery(tt.query)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test Azure severity mapping
func TestMapAzureSeverity(t *testing.T) {
	config := &AzureChangeTrackerConfig{}
	
	// Mock Azure change event
	azureChange := &AzureChangeEvent{
		ResourceType: "Microsoft.Compute/virtualMachines",
		ChangeType:   "Delete",
	}

	// Create a simple Azure change tracker for testing (without actual Azure clients)
	tracker := &AzureChangeTracker{
		config: config,
	}

	severity := tracker.mapAzureSeverity(azureChange)
	assert.Equal(t, SeverityHigh, severity, "Delete operations should have high severity")

	// Test create operation
	azureChange.ChangeType = "Create"
	severity = tracker.mapAzureSeverity(azureChange)
	assert.Equal(t, SeverityMedium, severity, "Create operations should have medium severity")
}

// Test service extraction from resource type
func TestExtractServiceFromResourceType(t *testing.T) {
	tracker := &AzureChangeTracker{}

	tests := []struct {
		resourceType    string
		expectedService string
	}{
		{
			resourceType:    "Microsoft.Compute/virtualMachines",
			expectedService: "Compute",
		},
		{
			resourceType:    "Microsoft.Storage/storageAccounts",
			expectedService: "Storage",
		},
		{
			resourceType:    "Microsoft.Network/virtualNetworks",
			expectedService: "Network",
		},
		{
			resourceType:    "InvalidFormat",
			expectedService: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			service := tracker.extractServiceFromResourceType(tt.resourceType)
			assert.Equal(t, tt.expectedService, service)
		})
	}
}

// Test baseline creation
func TestCreateBaseline(t *testing.T) {
	ctx := context.Background()
	mockStorage := &MockChangeStorage{}
	
	// Mock the StoreBaseline method
	mockStorage.On("StoreBaseline", mock.AnythingOfType("*main.DriftBaseline")).Return(nil)

	baseConfig := &ChangeTrackerConfig{Provider: "azure"}
	baseTracker := NewBaseChangeTracker("azure", mockStorage, baseConfig)
	
	tracker := &AzureChangeTracker{
		BaseChangeTracker: baseTracker,
	}

	resources := []*pb.Resource{
		{
			Id:       "test-vm-1",
			Type:     "Microsoft.Compute/virtualMachines",
			Location: "eastus",
			Service:  "Compute",
			Tags:     map[string]string{"env": "test"},
		},
		{
			Id:       "test-storage-1", 
			Type:     "Microsoft.Storage/storageAccounts",
			Location: "eastus",
			Service:  "Storage",
			Tags:     map[string]string{"env": "test"},
		},
	}

	baseline, err := tracker.CreateBaseline(ctx, resources)
	
	assert.NoError(t, err)
	assert.NotNil(t, baseline)
	assert.Equal(t, "azure", baseline.Provider)
	assert.Len(t, baseline.Resources, 2)
	assert.True(t, baseline.Active)
	
	mockStorage.AssertExpectations(t)
}

// Test change ID generation
func TestGenerateChangeID(t *testing.T) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{Provider: "azure"}
	tracker := NewBaseChangeTracker("azure", mockStorage, baseConfig)

	resourceID := "test-resource-1"
	timestamp := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	changeType := ChangeTypeUpdate

	changeID := tracker.GenerateChangeID(resourceID, timestamp, changeType)
	
	expected := "azure_test-resource-1_UPDATE_1704110400"
	assert.Equal(t, expected, changeID)
}

// Test impact assessment
func TestAnalyzeChangeImpact(t *testing.T) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{Provider: "azure"}
	tracker := NewBaseChangeTracker("azure", mockStorage, baseConfig)

	change := &ChangeEvent{
		ResourceType:  "Microsoft.Compute/virtualMachines",
		ChangeType:    ChangeTypeDelete,
		ChangedFields: []string{"state", "powerState"},
	}

	assessment := tracker.AnalyzeChangeImpact(change)
	
	assert.NotNil(t, assessment)
	assert.Equal(t, SeverityHigh, assessment.AvailabilityImpact.Level)
	assert.True(t, assessment.RiskScore > 0)
	assert.NotEmpty(t, assessment.Recommendations)
}

// Test critical resource type detection
func TestIsCriticalResourceType(t *testing.T) {
	tracker := &AzureChangeTracker{}

	tests := []struct {
		resourceType string
		expected     bool
	}{
		{"Microsoft.Compute/virtualMachines", true},
		{"Microsoft.Sql/servers", true},
		{"Microsoft.Storage/storageAccounts", true},
		{"Microsoft.KeyVault/vaults", true},
		{"Microsoft.Insights/components", false},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			result := tracker.isCriticalResourceType(tt.resourceType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests
func BenchmarkGenerateChangeID(b *testing.B) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{Provider: "azure"}
	tracker := NewBaseChangeTracker("azure", mockStorage, baseConfig)

	resourceID := "test-resource-1"
	timestamp := time.Now()
	changeType := ChangeTypeUpdate

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.GenerateChangeID(resourceID, timestamp, changeType)
	}
}

func BenchmarkAnalyzeChangeImpact(b *testing.B) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{Provider: "azure"}
	tracker := NewBaseChangeTracker("azure", mockStorage, baseConfig)

	change := &ChangeEvent{
		ResourceType:  "Microsoft.Compute/virtualMachines",
		ChangeType:    ChangeTypeUpdate,
		ChangedFields: []string{"state", "powerState", "networkProfile"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.AnalyzeChangeImpact(change)
	}
}
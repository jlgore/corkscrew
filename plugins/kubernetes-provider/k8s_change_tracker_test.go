package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/apimachinery/pkg/runtime"
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

// Test Kubernetes change tracker creation
func TestNewK8sChangeTracker(t *testing.T) {
	ctx := context.Background()
	
	// Create fake Kubernetes clients
	fakeClientset := fake.NewSimpleClientset()
	scheme := runtime.NewScheme()
	fakeDynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	
	config := &K8sChangeTrackerConfig{
		WatchNamespaces: []string{"default", "test"},
		ResourceTypes:   []string{"pods", "services"},
		QueryInterval:   30 * time.Second,
		StorageConfig: StorageConfig{
			Type: "duckdb",
			Path: ":memory:",
		},
	}

	tracker, err := NewK8sChangeTracker(ctx, fakeClientset, fakeDynamicClient, config)
	
	assert.NoError(t, err)
	assert.NotNil(t, tracker)
	assert.Equal(t, "kubernetes", tracker.provider)
	assert.Equal(t, []string{"default", "test"}, tracker.watchNamespaces)
}

// Test watch event to change type mapping
func TestMapWatchEventToChangeType(t *testing.T) {
	tracker := &K8sChangeTracker{}

	tests := []struct {
		eventType       watch.EventType
		expectedChangeType ChangeType
	}{
		{
			eventType:       watch.Added,
			expectedChangeType: ChangeTypeCreate,
		},
		{
			eventType:       watch.Modified,
			expectedChangeType: ChangeTypeUpdate,
		},
		{
			eventType:       watch.Deleted,
			expectedChangeType: ChangeTypeDelete,
		},
		{
			eventType:       watch.Error,
			expectedChangeType: ChangeTypeUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			changeType := tracker.mapWatchEventToChangeType(tt.eventType)
			assert.Equal(t, tt.expectedChangeType, changeType)
		})
	}
}

// Test Kubernetes severity mapping
func TestMapK8sSeverity(t *testing.T) {
	tracker := &K8sChangeTracker{}

	tests := []struct {
		eventType    watch.EventType
		resourceType string
		expectedSeverity ChangeSeverity
	}{
		{
			eventType:    watch.Deleted,
			resourceType: "Pod",
			expectedSeverity: SeverityHigh,
		},
		{
			eventType:    watch.Added,
			resourceType: "Pod",
			expectedSeverity: SeverityMedium,
		},
		{
			eventType:    watch.Modified,
			resourceType: "Secret",
			expectedSeverity: SeverityHigh,
		},
		{
			eventType:    watch.Modified,
			resourceType: "ConfigMap",
			expectedSeverity: SeverityMedium,
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType)+"_"+tt.resourceType, func(t *testing.T) {
			severity := tracker.mapK8sSeverity(tt.eventType, tt.resourceType)
			assert.Equal(t, tt.expectedSeverity, severity)
		})
	}
}

// Test critical Kubernetes resource type detection
func TestIsCriticalK8sResourceType(t *testing.T) {
	tracker := &K8sChangeTracker{}

	tests := []struct {
		resourceType string
		expected     bool
	}{
		{"Deployment", true},
		{"Service", true},
		{"Secret", true},
		{"ServiceAccount", true},
		{"ClusterRole", true},
		{"Pod", false},
		{"ConfigMap", false},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			result := tracker.isCriticalK8sResourceType(tt.resourceType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test baseline creation
func TestCreateK8sBaseline(t *testing.T) {
	ctx := context.Background()
	mockStorage := &MockChangeStorage{}
	
	// Mock the StoreBaseline method
	mockStorage.On("StoreBaseline", mock.AnythingOfType("*main.DriftBaseline")).Return(nil)

	baseConfig := &ChangeTrackerConfig{Provider: "kubernetes"}
	baseTracker := NewBaseChangeTracker("kubernetes", mockStorage, baseConfig)
	
	tracker := &K8sChangeTracker{
		BaseChangeTracker: baseTracker,
	}

	resources := []*pb.Resource{
		{
			Id:       "default/pod/test-pod",
			Type:     "Pod",
			Location: "default",
			Service:  "core",
			Tags:     map[string]string{"app": "test"},
		},
		{
			Id:       "default/service/test-service",
			Type:     "Service",
			Location: "default",
			Service:  "core",
			Tags:     map[string]string{"app": "test"},
		},
	}

	baseline, err := tracker.CreateBaseline(ctx, resources)
	
	assert.NoError(t, err)
	assert.NotNil(t, baseline)
	assert.Equal(t, "kubernetes", baseline.Provider)
	assert.Len(t, baseline.Resources, 2)
	assert.True(t, baseline.Active)
	
	mockStorage.AssertExpectations(t)
}

// Test change query validation with Kubernetes-specific scenarios
func TestValidateK8sChangeQuery(t *testing.T) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{
		Provider: "kubernetes",
		MaxQueryTimeRange: 24 * time.Hour,
	}
	
	tracker := NewBaseChangeTracker("kubernetes", mockStorage, baseConfig)

	tests := []struct {
		name    string
		query   *ChangeQuery
		wantErr bool
	}{
		{
			name: "valid K8s query with namespaces",
			query: &ChangeQuery{
				Provider:  "kubernetes",
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				ResourceFilter: &ResourceFilter{
					Projects: []string{"default", "kube-system"}, // Projects = namespaces in K8s
				},
			},
			wantErr: false,
		},
		{
			name: "valid K8s query with resource types",
			query: &ChangeQuery{
				Provider:  "kubernetes",
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				ResourceFilter: &ResourceFilter{
					ResourceTypes: []string{"Pod", "Service", "Deployment"},
				},
			},
			wantErr: false,
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

// Test time filter matching
func TestMatchesTimeFilter(t *testing.T) {
	tracker := &K8sChangeTracker{}
	
	baseTime := time.Now()
	startTime := baseTime.Add(-2 * time.Hour)
	endTime := baseTime.Add(-1 * time.Hour)
	
	query := &ChangeQuery{
		StartTime: startTime,
		EndTime:   endTime,
	}

	tests := []struct {
		name      string
		changeTime time.Time
		expected   bool
	}{
		{
			name:      "change within range",
			changeTime: baseTime.Add(-90 * time.Minute),
			expected:   true,
		},
		{
			name:      "change before range",
			changeTime: baseTime.Add(-3 * time.Hour),
			expected:   false,
		},
		{
			name:      "change after range",
			changeTime: baseTime.Add(-30 * time.Minute),
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := &ChangeEvent{
				Timestamp: tt.changeTime,
			}
			result := tracker.matchesTimeFilter(change, query)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test drift detection
func TestDetectK8sDrift(t *testing.T) {
	ctx := context.Background()
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{Provider: "kubernetes"}
	baseTracker := NewBaseChangeTracker("kubernetes", mockStorage, baseConfig)
	
	tracker := &K8sChangeTracker{
		BaseChangeTracker: baseTracker,
	}

	// Create a test baseline
	baseline := &DriftBaseline{
		ID:       "test-k8s-baseline",
		Provider: "kubernetes",
		Resources: map[string]*ResourceState{
			"default/pod/test-pod": {
				ResourceID: "default/pod/test-pod",
				Properties: map[string]interface{}{
					"kind":      "Pod",
					"namespace": "default",
					"status":    "Running",
				},
				Tags: map[string]string{
					"app": "test",
				},
			},
		},
	}

	report, err := tracker.DetectDrift(ctx, baseline)
	
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.Equal(t, "test-k8s-baseline", report.BaselineID)
	assert.Equal(t, 1, report.TotalResources)
}

// Test change event conversion
func TestConvertPodToChangeEvent(t *testing.T) {
	tracker := &K8sChangeTracker{}
	
	change := tracker.convertPodToChangeEvent(nil, watch.Added)
	
	assert.NotNil(t, change)
	assert.Equal(t, "kubernetes", change.Provider)
	assert.Equal(t, "Pod", change.ResourceType)
	assert.Equal(t, ChangeTypeCreate, change.ChangeType)
	assert.Equal(t, "core", change.Service)
}

// Test impact analysis for Kubernetes changes
func TestAnalyzeK8sChangeImpact(t *testing.T) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{Provider: "kubernetes"}
	tracker := NewBaseChangeTracker("kubernetes", mockStorage, baseConfig)

	change := &ChangeEvent{
		ResourceType:  "Secret",
		ChangeType:    ChangeTypeUpdate,
		ChangedFields: []string{"data", "type"},
	}

	assessment := tracker.AnalyzeChangeImpact(change)
	
	assert.NotNil(t, assessment)
	assert.Equal(t, SeverityHigh, assessment.SecurityImpact.Level)
	assert.True(t, assessment.RiskScore > 0)
	assert.NotEmpty(t, assessment.Recommendations)
}

// Benchmark tests
func BenchmarkMapWatchEventToChangeType(b *testing.B) {
	tracker := &K8sChangeTracker{}
	eventType := watch.Modified

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.mapWatchEventToChangeType(eventType)
	}
}

func BenchmarkIsCriticalK8sResourceType(b *testing.B) {
	tracker := &K8sChangeTracker{}
	resourceType := "Deployment"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.isCriticalK8sResourceType(resourceType)
	}
}

func BenchmarkConvertPodToChangeEvent(b *testing.B) {
	tracker := &K8sChangeTracker{}
	eventType := watch.Modified

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.convertPodToChangeEvent(nil, eventType)
	}
}
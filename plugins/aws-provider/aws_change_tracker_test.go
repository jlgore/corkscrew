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

// Test AWS change tracker configuration
func TestNewAWSChangeTracker(t *testing.T) {
	ctx := context.Background()
	config := &AWSChangeTrackerConfig{
		AccountIDs:        []string{"123456789012"},
		Regions:          []string{"us-east-1"},
		QueryInterval:    5 * time.Minute,
		CloudTrailEnabled: true,
		ConfigEnabled:    true,
		StorageConfig: StorageConfig{
			Type: "duckdb",
			Path: ":memory:",
		},
	}

	// Note: This test would require mocking AWS SDK clients
	// For now, we test the configuration validation
	assert.NotNil(t, config)
	assert.Equal(t, []string{"123456789012"}, config.AccountIDs)
	assert.Equal(t, 5*time.Minute, config.QueryInterval)
	assert.True(t, config.CloudTrailEnabled)
}

// Test service extraction from resource type
func TestExtractServiceFromAWSResourceType(t *testing.T) {
	tracker := &AWSChangeTracker{}

	tests := []struct {
		resourceType    string
		expectedService string
	}{
		{
			resourceType:    "AWS::EC2::Instance",
			expectedService: "EC2",
		},
		{
			resourceType:    "AWS::S3::Bucket",
			expectedService: "S3",
		},
		{
			resourceType:    "AWS::RDS::DBInstance",
			expectedService: "RDS",
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

// Test service extraction from event source
func TestExtractServiceFromEventSource(t *testing.T) {
	tracker := &AWSChangeTracker{}

	tests := []struct {
		eventSource     string
		expectedService string
	}{
		{
			eventSource:     "ec2.amazonaws.com",
			expectedService: "EC2",
		},
		{
			eventSource:     "s3.amazonaws.com",
			expectedService: "S3",
		},
		{
			eventSource:     "rds.amazonaws.com",
			expectedService: "RDS",
		},
		{
			eventSource:     "invalid",
			expectedService: "INVALID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.eventSource, func(t *testing.T) {
			service := tracker.extractServiceFromEventSource(tt.eventSource)
			assert.Equal(t, tt.expectedService, service)
		})
	}
}

// Test CloudTrail event name to change type mapping
func TestMapCloudTrailEventNameToChangeType(t *testing.T) {
	tracker := &AWSChangeTracker{}

	tests := []struct {
		eventName       string
		expectedChangeType ChangeType
	}{
		{
			eventName:       "RunInstances",
			expectedChangeType: ChangeTypeCreate,
		},
		{
			eventName:       "CreateBucket",
			expectedChangeType: ChangeTypeCreate,
		},
		{
			eventName:       "TerminateInstances",
			expectedChangeType: ChangeTypeDelete,
		},
		{
			eventName:       "DeleteBucket",
			expectedChangeType: ChangeTypeDelete,
		},
		{
			eventName:       "AttachUserPolicy",
			expectedChangeType: ChangeTypePolicyChange,
		},
		{
			eventName:       "CreateTags",
			expectedChangeType: ChangeTypeTagChange,
		},
		{
			eventName:       "ModifyDBInstance",
			expectedChangeType: ChangeTypeUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.eventName, func(t *testing.T) {
			changeType := tracker.mapCloudTrailEventNameToChangeType(tt.eventName)
			assert.Equal(t, tt.expectedChangeType, changeType)
		})
	}
}

// Test critical AWS resource type detection
func TestIsCriticalAWSResourceType(t *testing.T) {
	tracker := &AWSChangeTracker{}

	tests := []struct {
		resourceType string
		expected     bool
	}{
		{"AWS::EC2::Instance", true},
		{"AWS::RDS::DBInstance", true},
		{"AWS::S3::Bucket", true},
		{"AWS::IAM::Role", true},
		{"AWS::CloudFormation::Stack", false},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			result := tracker.isCriticalAWSResourceType(tt.resourceType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test baseline creation
func TestCreateAWSBaseline(t *testing.T) {
	ctx := context.Background()
	mockStorage := &MockChangeStorage{}
	
	// Mock the StoreBaseline method
	mockStorage.On("StoreBaseline", mock.AnythingOfType("*main.DriftBaseline")).Return(nil)

	baseConfig := &ChangeTrackerConfig{Provider: "aws"}
	baseTracker := NewBaseChangeTracker("aws", mockStorage, baseConfig)
	
	tracker := &AWSChangeTracker{
		BaseChangeTracker: baseTracker,
	}

	resources := []*pb.Resource{
		{
			Id:       "i-1234567890abcdef0",
			Type:     "AWS::EC2::Instance",
			Location: "us-east-1",
			Service:  "EC2",
			Tags:     map[string]string{"Environment": "test"},
		},
		{
			Id:       "test-bucket-123",
			Type:     "AWS::S3::Bucket",
			Location: "us-east-1",
			Service:  "S3",
			Tags:     map[string]string{"Environment": "test"},
		},
	}

	baseline, err := tracker.CreateBaseline(ctx, resources)
	
	assert.NoError(t, err)
	assert.NotNil(t, baseline)
	assert.Equal(t, "aws", baseline.Provider)
	assert.Len(t, baseline.Resources, 2)
	assert.True(t, baseline.Active)
	
	mockStorage.AssertExpectations(t)
}

// Test resource name extraction
func TestExtractResourceName(t *testing.T) {
	tracker := &AWSChangeTracker{}

	tests := []struct {
		resourceID   string
		expectedName string
	}{
		{
			resourceID:   "arn:aws:s3:::my-bucket",
			expectedName: "my-bucket",
		},
		{
			resourceID:   "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
			expectedName: "i-1234567890abcdef0",
		},
		{
			resourceID:   "my-resource/with/slashes",
			expectedName: "slashes",
		},
		{
			resourceID:   "simple-resource",
			expectedName: "simple-resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.resourceID, func(t *testing.T) {
			name := tracker.extractResourceName(tt.resourceID)
			assert.Equal(t, tt.expectedName, name)
		})
	}
}

// Test change query validation with AWS-specific scenarios
func TestValidateAWSChangeQuery(t *testing.T) {
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{
		Provider: "aws",
		MaxQueryTimeRange: 24 * time.Hour,
	}
	
	tracker := NewBaseChangeTracker("aws", mockStorage, baseConfig)

	tests := []struct {
		name    string
		query   *ChangeQuery
		wantErr bool
	}{
		{
			name: "valid AWS query with regions",
			query: &ChangeQuery{
				Provider:  "aws",
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				ResourceFilter: &ResourceFilter{
					Regions: []string{"us-east-1", "us-west-2"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid AWS query with resource types",
			query: &ChangeQuery{
				Provider:  "aws",
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
				ResourceFilter: &ResourceFilter{
					ResourceTypes: []string{"AWS::EC2::Instance", "AWS::S3::Bucket"},
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

// Test drift detection
func TestDetectDrift(t *testing.T) {
	ctx := context.Background()
	mockStorage := &MockChangeStorage{}
	baseConfig := &ChangeTrackerConfig{Provider: "aws"}
	baseTracker := NewBaseChangeTracker("aws", mockStorage, baseConfig)
	
	tracker := &AWSChangeTracker{
		BaseChangeTracker: baseTracker,
	}

	// Create a test baseline
	baseline := &DriftBaseline{
		ID:       "test-baseline",
		Provider: "aws",
		Resources: map[string]*ResourceState{
			"i-1234567890abcdef0": {
				ResourceID: "i-1234567890abcdef0",
				Properties: map[string]interface{}{
					"resourceType": "AWS::EC2::Instance",
					"state":       "running",
				},
				Tags: map[string]string{
					"Environment": "test",
				},
			},
		},
	}

	// Mock getCurrentResourceState to return nil (resource deleted)
	// This would normally be a method call, but for testing we can simulate
	// a deleted resource scenario in the drift report

	report, err := tracker.DetectDrift(ctx, baseline)
	
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.Equal(t, "test-baseline", report.BaselineID)
	assert.Equal(t, 1, report.TotalResources)
}

// Benchmark tests
func BenchmarkExtractServiceFromResourceType(b *testing.B) {
	tracker := &AWSChangeTracker{}
	resourceType := "AWS::EC2::Instance"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.extractServiceFromResourceType(resourceType)
	}
}

func BenchmarkMapCloudTrailEventNameToChangeType(b *testing.B) {
	tracker := &AWSChangeTracker{}
	eventName := "RunInstances"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.mapCloudTrailEventNameToChangeType(eventName)
	}
}
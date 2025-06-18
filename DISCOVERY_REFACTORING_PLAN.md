# Corkscrew Service Discovery Refactoring Plan

## Executive Summary

This plan addresses the fundamental architectural flaw in corkscrew's service detection system, which currently relies on hardcoded service mappings instead of dynamic discovery. The refactoring will transform the system into a truly dynamic, plugin-based architecture that can adapt to any cloud provider.

## Current State Analysis

### Core Problems
1. **Static Service Mapping**: Hardcoded service lists in `pkg/discovery/service_detector.go`
2. **Mixed Provider Concerns**: All providers' services in one monolithic map
3. **Arbitrary Scoring**: Undocumented, unmaintainable priority scores
4. **False Advertising**: Functions named "detect" and "analyze" that do neither
5. **Zero Configurability**: No external configuration options
6. **Poor Extensibility**: Adding providers requires core code changes

## Proposed Architecture

### Phase 1: Provider Plugin Interface (Week 1)

#### 1.1 Define Service Discovery Interface

```go
// internal/shared/interfaces.go
package shared

import (
    "context"
    "time"
)

// ServiceDiscoverer defines the contract for dynamic service discovery
type ServiceDiscoverer interface {
    // DiscoverAvailableServices queries the provider to get all available services
    DiscoverAvailableServices(ctx context.Context, region string) ([]ServiceInfo, error)
    
    // AnalyzeServiceActivity checks actual resource usage for prioritization
    AnalyzeServiceActivity(ctx context.Context, service ServiceInfo, region string) (*ServiceActivity, error)
    
    // GetServiceMetadata returns detailed information about a service
    GetServiceMetadata(ctx context.Context, serviceName string) (*ServiceMetadata, error)
}

// ServiceInfo represents discovered service information
type ServiceInfo struct {
    Name            string
    DisplayName     string
    Category        string   // compute, storage, database, etc.
    GlobalService   bool     // true for services like IAM
    RegionalService bool     // true for region-specific services
    ApiEndpoints    []string // actual API endpoints for verification
    ResourceTypes   []string // types of resources this service manages
}

// ServiceActivity represents actual usage data
type ServiceActivity struct {
    ServiceName     string
    ResourceCount   int
    LastActivity    time.Time
    ApiCallCount    int64
    EstimatedCost   float64
    ActivityScore   float64  // calculated, not hardcoded
    UsagePatterns   map[string]interface{}
}

// ServiceMetadata provides detailed service information
type ServiceMetadata struct {
    ServiceInfo
    Documentation   string
    Quotas          map[string]int
    Regions         []string
    Dependencies    []string
    CostModel       CostModel
}
```

#### 1.2 Provider-Specific Implementation Pattern

```go
// plugins/corkscrew-aws/service_discoverer.go
package main

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/servicequotas"
    "github.com/aws/aws-sdk-go-v2/service/cloudformation"
    "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
)

type AWSServiceDiscoverer struct {
    config           aws.Config
    quotasClient     *servicequotas.Client
    cfClient         *cloudformation.Client
    taggingClient    *resourcegroupstaggingapi.Client
    cache            *ServiceCache
}

func (d *AWSServiceDiscoverer) DiscoverAvailableServices(ctx context.Context, region string) ([]ServiceInfo, error) {
    services := []ServiceInfo{}
    
    // Method 1: Query Service Quotas API for available services
    quotasResp, err := d.quotasClient.ListServices(ctx, &servicequotas.ListServicesInput{})
    if err == nil {
        for _, svc := range quotasResp.Services {
            services = append(services, d.parseServiceQuotaInfo(svc))
        }
    }
    
    // Method 2: Query CloudFormation for supported resource types
    cfTypes, err := d.cfClient.ListTypes(ctx, &cloudformation.ListTypesInput{
        Type: types.RegistryTypeResource,
    })
    if err == nil {
        services = d.mergeCloudFormationTypes(services, cfTypes)
    }
    
    // Method 3: Use resource tagging API to find active services
    activeServices := d.discoverFromResourceTags(ctx, region)
    services = d.mergeActiveServices(services, activeServices)
    
    return services, nil
}

func (d *AWSServiceDiscoverer) AnalyzeServiceActivity(ctx context.Context, service ServiceInfo, region string) (*ServiceActivity, error) {
    activity := &ServiceActivity{
        ServiceName: service.Name,
    }
    
    // Get actual resource counts
    resources, err := d.taggingClient.GetResources(ctx, &resourcegroupstaggingapi.GetResourcesInput{
        ResourceTypeFilters: aws.StringSlice(service.ResourceTypes),
    })
    if err == nil {
        activity.ResourceCount = len(resources.ResourceTagMappingList)
    }
    
    // Calculate activity score based on actual data
    activity.ActivityScore = d.calculateActivityScore(
        activity.ResourceCount,
        activity.ApiCallCount,
        activity.EstimatedCost,
    )
    
    return activity, nil
}
```

### Phase 2: Configuration System (Week 1-2)

#### 2.1 Service Priority Configuration

```yaml
# config/service-priorities.yaml
version: 1.0
providers:
  aws:
    # Global scoring factors
    scoring:
      resource_count_weight: 0.3
      api_activity_weight: 0.2
      cost_weight: 0.2
      compliance_weight: 0.3
    
    # Service-specific overrides
    services:
      iam:
        priority: critical
        reason: "Security service always scanned"
      s3:
        priority: high
        categories: [storage, compliance]
      ec2:
        priority: high
        categories: [compute]
    
    # Category-based rules
    categories:
      security:
        base_score: 0.9
      compliance:
        base_score: 0.8
      compute:
        base_score: 0.7
      storage:
        base_score: 0.7
      database:
        base_score: 0.6
      
  azure:
    # Similar structure for Azure
    
  gcp:
    # Similar structure for GCP
```

#### 2.2 Configuration Loader

```go
// internal/config/service_config.go
package config

type ServiceConfig struct {
    Version   string
    Providers map[string]ProviderConfig
}

type ProviderConfig struct {
    Scoring    ScoringConfig
    Services   map[string]ServiceOverride
    Categories map[string]CategoryConfig
}

type ScoringConfig struct {
    ResourceCountWeight float64 `yaml:"resource_count_weight"`
    ApiActivityWeight   float64 `yaml:"api_activity_weight"`
    CostWeight         float64 `yaml:"cost_weight"`
    ComplianceWeight   float64 `yaml:"compliance_weight"`
}

func LoadServiceConfig(path string) (*ServiceConfig, error) {
    // Load from file with defaults fallback
    config := &ServiceConfig{}
    
    if path == "" {
        path = findConfigFile([]string{
            "./config/service-priorities.yaml",
            "~/.corkscrew/service-priorities.yaml",
            "/etc/corkscrew/service-priorities.yaml",
        })
    }
    
    if err := loadYAMLFile(path, config); err != nil {
        return getDefaultConfig(), nil
    }
    
    return config, nil
}
```

### Phase 3: Dynamic Service Discovery Engine (Week 2)

#### 3.1 Refactored Service Detector

```go
// internal/discovery/dynamic_service_detector.go
package discovery

import (
    "context"
    "sync"
    "github.com/jlgore/corkscrew/internal/config"
    "github.com/jlgore/corkscrew/internal/shared"
)

type DynamicServiceDetector struct {
    config       *config.ServiceConfig
    discoverers  map[string]shared.ServiceDiscoverer
    activityData sync.Map // concurrent-safe activity cache
    mu           sync.RWMutex
}

func NewDynamicServiceDetector(cfg *config.ServiceConfig) *DynamicServiceDetector {
    return &DynamicServiceDetector{
        config:      cfg,
        discoverers: make(map[string]shared.ServiceDiscoverer),
    }
}

func (d *DynamicServiceDetector) RegisterDiscoverer(provider string, discoverer shared.ServiceDiscoverer) {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.discoverers[provider] = discoverer
}

func (d *DynamicServiceDetector) DetectActiveServices(ctx context.Context, provider string, region string) ([]models.Service, error) {
    discoverer, exists := d.discoverers[provider]
    if !exists {
        return nil, fmt.Errorf("no discoverer registered for provider: %s", provider)
    }
    
    // Step 1: Discover available services dynamically
    availableServices, err := discoverer.DiscoverAvailableServices(ctx, region)
    if err != nil {
        return nil, fmt.Errorf("failed to discover services: %w", err)
    }
    
    // Step 2: Analyze actual activity for each service
    var wg sync.WaitGroup
    serviceChan := make(chan models.Service, len(availableServices))
    errorChan := make(chan error, len(availableServices))
    
    for _, svc := range availableServices {
        wg.Add(1)
        go func(service shared.ServiceInfo) {
            defer wg.Done()
            
            activity, err := discoverer.AnalyzeServiceActivity(ctx, service, region)
            if err != nil {
                errorChan <- err
                return
            }
            
            // Step 3: Apply configuration-based scoring
            score := d.calculateScore(provider, service, activity)
            
            serviceChan <- models.Service{
                Name:         service.Name,
                DisplayName:  service.DisplayName,
                Category:     service.Category,
                Priority:     score,
                Activity:     activity,
                Metadata:     d.getServiceMetadata(provider, service.Name),
            }
        }(svc)
    }
    
    wg.Wait()
    close(serviceChan)
    close(errorChan)
    
    // Collect results
    services := []models.Service{}
    for svc := range serviceChan {
        services = append(services, svc)
    }
    
    // Sort by calculated priority
    sort.Slice(services, func(i, j int) bool {
        return services[i].Priority > services[j].Priority
    })
    
    return services, nil
}

func (d *DynamicServiceDetector) calculateScore(provider string, service shared.ServiceInfo, activity *shared.ServiceActivity) float64 {
    providerConfig := d.config.Providers[provider]
    
    // Start with category base score
    categoryScore := 0.5 // default
    if category, exists := providerConfig.Categories[service.Category]; exists {
        categoryScore = category.BaseScore
    }
    
    // Apply service-specific overrides
    if override, exists := providerConfig.Services[service.Name]; exists {
        if override.Priority == "critical" {
            return 1.0
        }
        if override.Priority == "high" {
            categoryScore = max(categoryScore, 0.8)
        }
    }
    
    // Calculate weighted score based on actual activity
    activityScore := (
        activity.ResourceCount * providerConfig.Scoring.ResourceCountWeight +
        normalizeApiActivity(activity.ApiCallCount) * providerConfig.Scoring.ApiActivityWeight +
        normalizeCost(activity.EstimatedCost) * providerConfig.Scoring.CostWeight,
    ) / 3.0
    
    // Combine category and activity scores
    return (categoryScore + activityScore) / 2.0
}
```

### Phase 4: Plugin Integration (Week 2-3)

#### 4.1 Plugin Loader Enhancement

```go
// internal/client/plugin_loader.go
package client

type PluginLoader struct {
    pluginDir string
    loaded    map[string]*Plugin
}

func (l *PluginLoader) LoadProviderPlugin(provider string) (*Plugin, error) {
    // Load the plugin binary
    plugin, err := l.loadPlugin(provider)
    if err != nil {
        return nil, err
    }
    
    // Verify it implements ServiceDiscoverer interface
    discoverer, err := plugin.Lookup("ServiceDiscoverer")
    if err != nil {
        return nil, fmt.Errorf("plugin missing ServiceDiscoverer: %w", err)
    }
    
    // Type assert to ensure interface compliance
    if _, ok := discoverer.(shared.ServiceDiscoverer); !ok {
        return nil, fmt.Errorf("ServiceDiscoverer does not implement required interface")
    }
    
    return plugin, nil
}
```

### Phase 5: Activity Analysis Engine (Week 3)

#### 5.1 Real Activity Tracker

```go
// internal/discovery/activity_tracker.go
package discovery

import (
    "context"
    "time"
    "github.com/jlgore/corkscrew/internal/db"
)

type ActivityTracker struct {
    db        *db.ResourceDB
    cache     *ActivityCache
    analyzer  *UsageAnalyzer
}

func (t *ActivityTracker) TrackServiceActivity(ctx context.Context, provider, service, region string) error {
    // Record API calls
    t.recordApiCall(provider, service, region)
    
    // Update resource counts from database
    count, err := t.db.GetResourceCount(provider, service, region)
    if err == nil {
        t.updateResourceCount(provider, service, region, count)
    }
    
    // Analyze usage patterns
    patterns := t.analyzer.AnalyzeUsagePatterns(provider, service, region)
    t.updateUsagePatterns(provider, service, region, patterns)
    
    return nil
}

func (t *ActivityTracker) GetHistoricalActivity(provider, service string, duration time.Duration) (*HistoricalActivity, error) {
    // Query database for historical data
    endTime := time.Now()
    startTime := endTime.Add(-duration)
    
    activities, err := t.db.QueryActivities(provider, service, startTime, endTime)
    if err != nil {
        return nil, err
    }
    
    return t.analyzer.AggregateActivities(activities), nil
}
```

### Phase 6: Testing & Migration (Week 3-4)

#### 6.1 Testing Strategy

```go
// internal/discovery/service_detector_test.go
package discovery_test

func TestDynamicServiceDiscovery(t *testing.T) {
    tests := []struct {
        name           string
        provider       string
        mockServices   []shared.ServiceInfo
        mockActivities map[string]*shared.ServiceActivity
        config         *config.ServiceConfig
        expectedOrder  []string
    }{
        {
            name:     "AWS with high IAM activity",
            provider: "aws",
            mockServices: []shared.ServiceInfo{
                {Name: "iam", Category: "security"},
                {Name: "s3", Category: "storage"},
                {Name: "ec2", Category: "compute"},
            },
            mockActivities: map[string]*shared.ServiceActivity{
                "iam": {ResourceCount: 100, ApiCallCount: 5000},
                "s3":  {ResourceCount: 50, ApiCallCount: 1000},
                "ec2": {ResourceCount: 10, ApiCallCount: 500},
            },
            expectedOrder: []string{"iam", "s3", "ec2"},
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

#### 6.2 Migration Path

```go
// cmd/corkscrew/migrate.go
package main

func migrateCommand() *cobra.Command {
    return &cobra.Command{
        Use:   "migrate-config",
        Short: "Migrate from hardcoded services to dynamic configuration",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Extract current hardcoded values
            oldConfig := extractHardcodedConfig()
            
            // Convert to new configuration format
            newConfig := convertToServiceConfig(oldConfig)
            
            // Write configuration file
            configPath := filepath.Join(os.UserHomeDir(), ".corkscrew", "service-priorities.yaml")
            return writeConfig(configPath, newConfig)
        },
    }
}
```

## Implementation Phases

### Phase 1: Foundation (Week 1)
- [ ] Define service discovery interfaces
- [ ] Create configuration schema
- [ ] Implement configuration loader
- [ ] Write comprehensive tests

### Phase 2: AWS Provider (Week 2)
- [ ] Implement AWS service discoverer
- [ ] Add AWS-specific activity analysis
- [ ] Create AWS service metadata loader
- [ ] Test with real AWS environments

### Phase 3: Core Refactoring (Week 2-3)
- [ ] Replace static service detector
- [ ] Implement dynamic scoring engine
- [ ] Add activity tracking
- [ ] Create migration tooling

### Phase 4: Additional Providers (Week 3-4)
- [ ] Implement Azure service discoverer
- [ ] Implement GCP service discoverer
- [ ] Add provider-specific configurations
- [ ] Ensure consistent behavior across providers

### Phase 5: Advanced Features (Week 4+)
- [ ] Add machine learning for activity prediction
- [ ] Implement cost-based prioritization
- [ ] Add compliance-aware discovery
- [ ] Create service dependency mapping

## Prompts for Implementation

### Prompt 1: Interface Implementation
```
Implement the ServiceDiscoverer interface for AWS that:
1. Uses AWS SDK v2 to query actual available services
2. Leverages Service Quotas API, CloudFormation types, and Resource Groups Tagging API
3. Implements proper error handling and retries
4. Includes comprehensive logging for debugging
5. Uses context for cancellation and timeouts
```

### Prompt 2: Configuration System
```
Create a configuration system that:
1. Loads service priorities from YAML files
2. Supports provider-specific settings
3. Allows runtime configuration updates
4. Validates configuration schema
5. Provides sensible defaults when config is missing
```

### Prompt 3: Activity Analysis
```
Build an activity analysis engine that:
1. Tracks real-time API usage per service
2. Monitors resource creation/deletion patterns
3. Calculates cost estimates based on resource usage
4. Identifies service dependencies
5. Provides historical trend analysis
```

### Prompt 4: Testing Framework
```
Develop a comprehensive testing framework that:
1. Mocks cloud provider APIs effectively
2. Tests various activity scenarios
3. Validates scoring algorithms
4. Ensures backward compatibility
5. Includes integration tests with real providers
```

## Success Metrics

1. **Dynamic Discovery**: 100% of available services discovered without hardcoding
2. **Configuration Flexibility**: All service priorities configurable without code changes
3. **Performance**: Service discovery completes in <5 seconds for any provider
4. **Accuracy**: Activity scores correlate with actual resource usage (>0.8 correlation)
5. **Extensibility**: New provider added in <1 day of development
6. **Maintainability**: Zero hardcoded service names in core code

## Risk Mitigation

1. **API Rate Limits**: Implement caching and batch requests
2. **Provider API Changes**: Abstract provider-specific logic into plugins
3. **Performance Impact**: Use concurrent discovery with proper throttling
4. **Configuration Complexity**: Provide sensible defaults and validation
5. **Migration Issues**: Offer compatibility mode during transition

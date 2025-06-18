package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
)

type ServiceDetector struct {
	config        SmartDiscoveryConfig
	serviceCache  map[string][]models.Service
	serviceScore  map[string]ServiceScore
	activityData  map[string]ServiceActivity
	mutex         sync.RWMutex
}

type ServiceScore struct {
	Priority     int
	ActivityScore float64
	ResourceCount int
	LastSeen     time.Time
}

type ServiceActivity struct {
	HasResources     bool
	ResourceCount    int
	RecentActivity   bool
	ConfigurationAge time.Duration
	ErrorRate        float64
}

type ServicePriority struct {
	Service models.Service
	Score   float64
	Reason  string
}

func NewServiceDetector(config SmartDiscoveryConfig) *ServiceDetector {
	return &ServiceDetector{
		config:       config,
		serviceCache: make(map[string][]models.Service),
		serviceScore: make(map[string]ServiceScore),
		activityData: make(map[string]ServiceActivity),
	}
}

func (sd *ServiceDetector) DetectActiveServices(ctx context.Context, provider CloudProvider, region string) ([]models.Service, error) {
	cacheKey := sd.getCacheKey(provider.GetName(), region)
	
	sd.mutex.RLock()
	cached, exists := sd.serviceCache[cacheKey]
	sd.mutex.RUnlock()

	if exists && sd.isCacheValid(cacheKey) {
		return sd.prioritizeServices(cached), nil
	}

	services, err := provider.GetServices(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("failed to get services: %w", err)
	}

	services = sd.analyzeServiceActivity(ctx, provider, region, services)

	sd.mutex.Lock()
	sd.serviceCache[cacheKey] = services
	sd.mutex.Unlock()

	return sd.prioritizeServices(services), nil
}

func (sd *ServiceDetector) analyzeServiceActivity(ctx context.Context, provider CloudProvider, region string, services []models.Service) []models.Service {
	var wg sync.WaitGroup
	activityChan := make(chan ServiceActivityData, len(services))

	for _, service := range services {
		wg.Add(1)
		go func(s models.Service) {
			defer wg.Done()
			activity := sd.measureServiceActivity(ctx, provider, s)
			activityChan <- ServiceActivityData{Service: s, Activity: activity}
		}(service)
	}

	go func() {
		wg.Wait()
		close(activityChan)
	}()

	sd.mutex.Lock()
	for activityData := range activityChan {
		key := sd.getServiceKey(provider.GetName(), region, activityData.Service.Name)
		sd.activityData[key] = activityData.Activity
		sd.serviceScore[key] = sd.calculateServiceScore(activityData.Service, activityData.Activity)
	}
	sd.mutex.Unlock()

	return services
}

type ServiceActivityData struct {
	Service  models.Service
	Activity ServiceActivity
}

func (sd *ServiceDetector) measureServiceActivity(ctx context.Context, provider CloudProvider, service models.Service) ServiceActivity {
	// Try to discover resources to determine activity
	resources, err := provider.DiscoverResources(ctx, service)
	
	hasResources := len(resources) > 0
	errorRate := 0.0
	if err != nil {
		errorRate = 1.0
	}

	return ServiceActivity{
		HasResources:     hasResources,
		ResourceCount:    len(resources),
		RecentActivity:   hasResources, // Simplified - could analyze timestamps
		ConfigurationAge: time.Hour,    // Simplified - would analyze actual config age
		ErrorRate:        errorRate,
	}
}

func (sd *ServiceDetector) calculateServiceScore(service models.Service, activity ServiceActivity) ServiceScore {
	score := 0.5 // Base score

	// Boost for having resources
	if activity.HasResources {
		score += 0.3
	}

	// Boost based on resource count
	if activity.ResourceCount > 10 {
		score += 0.2
	} else if activity.ResourceCount > 0 {
		score += 0.1
	}

	// Boost for recent activity
	if activity.RecentActivity {
		score += 0.2
	}

	// Penalize for errors
	score -= activity.ErrorRate * 0.3

	// Service-specific priorities
	score += sd.getServicePriorityBoost(service.Name)

	priority := sd.calculatePriority(score)

	return ServiceScore{
		Priority:      priority,
		ActivityScore: max(0, min(1, score)),
		ResourceCount: activity.ResourceCount,
		LastSeen:      time.Now(),
	}
}

func (sd *ServiceDetector) getServicePriorityBoost(serviceName string) float64 {
	// Core infrastructure services get priority
	coreServices := map[string]float64{
		// AWS Core Services
		"ec2":             0.3,
		"s3":              0.3,
		"rds":             0.25,
		"lambda":          0.25,
		"iam":             0.3,
		"vpc":             0.25,
		"cloudformation":  0.2,
		"cloudwatch":      0.2,
		"elb":             0.2,
		"elbv2":           0.2,
		"route53":         0.2,
		
		// Azure Core Services
		"virtualmachines": 0.3,
		"azurestorage":    0.3,
		"sqldatabase":     0.25,
		"functionapp":     0.25,
		"keyvault":        0.25,
		"virtualnetwork":  0.25,
		"resourcegroup":   0.2,
		"monitor":         0.2,
		"loadbalancer":    0.2,
		
		// GCP Core Services
		"compute":         0.3,
		"gcpstorage":      0.3,
		"sql":             0.25,
		"functions":       0.25,
		"gcpiam":          0.3,
		"networking":      0.25,
		"deployment":      0.2,
		"monitoring":      0.2,
		"loadbalancing":   0.2,
		
		// Kubernetes Core
		"pods":            0.3,
		"services":        0.3,
		"deployments":     0.25,
		"configmaps":      0.2,
		"secrets":         0.25,
		"ingress":         0.2,
		"persistentvolumes": 0.2,
	}

	serviceLower := strings.ToLower(serviceName)
	for key, boost := range coreServices {
		if strings.Contains(serviceLower, key) {
			return boost
		}
	}

	// Security and governance services
	securityServices := []string{"security", "compliance", "audit", "guard", "shield", "waf", "firewall"}
	for _, sec := range securityServices {
		if strings.Contains(serviceLower, sec) {
			return 0.15
		}
	}

	return 0
}

func (sd *ServiceDetector) calculatePriority(score float64) int {
	if score > 0.8 {
		return 1 // High priority
	} else if score > 0.6 {
		return 2 // Medium priority
	} else if score > 0.4 {
		return 3 // Low priority
	} else {
		return 4 // Very low priority
	}
}

func (sd *ServiceDetector) prioritizeServices(services []models.Service) []models.Service {
	priorities := make([]ServicePriority, len(services))
	
	for i, service := range services {
		score := sd.getServiceScoreFromCache(service.Name)
		priorities[i] = ServicePriority{
			Service: service,
			Score:   score,
			Reason:  sd.getScoreReason(score),
		}
	}

	sort.Slice(priorities, func(i, j int) bool {
		return priorities[i].Score > priorities[j].Score
	})

	optimizedServices := make([]models.Service, len(priorities))
	for i, priority := range priorities {
		service := priority.Service
		service.Metadata = map[string]interface{}{
			"priority_score":  priority.Score,
			"priority_reason": priority.Reason,
			"detection_time":  time.Now(),
		}
		optimizedServices[i] = service
	}

	return optimizedServices
}

func (sd *ServiceDetector) getServiceScoreFromCache(serviceName string) float64 {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()

	// Try to find any cached score for this service across regions
	for key, score := range sd.serviceScore {
		if strings.Contains(key, serviceName) {
			return score.ActivityScore
		}
	}

	return 0.5 // Default score
}

func (sd *ServiceDetector) getScoreReason(score float64) string {
	if score > 0.8 {
		return "High priority: Core service with active resources"
	} else if score > 0.6 {
		return "Medium priority: Active service with resources"
	} else if score > 0.4 {
		return "Low priority: Limited activity or resources"
	} else {
		return "Very low priority: No resources or high error rate"
	}
}

func (sd *ServiceDetector) getCacheKey(provider, region string) string {
	return fmt.Sprintf("%s:%s", provider, region)
}

func (sd *ServiceDetector) getServiceKey(provider, region, service string) string {
	return fmt.Sprintf("%s:%s:%s", provider, region, service)
}

func (sd *ServiceDetector) isCacheValid(cacheKey string) bool {
	// Simplified cache validation - in real implementation, 
	// would track cache timestamps
	return false
}
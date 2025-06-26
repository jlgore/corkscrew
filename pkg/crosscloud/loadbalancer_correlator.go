package crosscloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
	"github.com/google/uuid"
)

// LoadBalancerCrossCloudCorrelator detects load balancer relationships across cloud providers
type LoadBalancerCrossCloudCorrelator struct {
	confidenceThreshold float64
}

// TrafficManagerCorrelator detects Azure Traffic Manager correlations
type TrafficManagerCorrelator struct {
	confidenceThreshold float64
}

// HealthCheckCorrelator correlates health checks across cloud providers
type HealthCheckCorrelator struct {
	confidenceThreshold float64
}

// BackendPoolCorrelator analyzes backend pool relationships
type BackendPoolCorrelator struct {
	confidenceThreshold float64
}

// LoadBalancerConfig represents load balancer configuration
type LoadBalancerConfig struct {
	Type             string                 `json:"type"`
	Scheme           string                 `json:"scheme"`
	Listeners        []ListenerConfig       `json:"listeners"`
	Backends         []BackendConfig        `json:"backends"`
	HealthChecks     []HealthCheckConfig    `json:"health_checks"`
	RoutingRules     []RoutingRuleConfig    `json:"routing_rules"`
	DNSName          string                 `json:"dns_name"`
	IPAddresses      []string               `json:"ip_addresses"`
	Attributes       map[string]interface{} `json:"attributes"`
}

type ListenerConfig struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	SSLCert  string `json:"ssl_cert"`
}

type BackendConfig struct {
	Target   string `json:"target"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Weight   int    `json:"weight"`
	Health   string `json:"health"`
}

type HealthCheckConfig struct {
	Type        string `json:"type"`
	Path        string `json:"path"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Interval    int    `json:"interval"`
	Timeout     int    `json:"timeout"`
	Threshold   int    `json:"threshold"`
}

type RoutingRuleConfig struct {
	Priority   int                    `json:"priority"`
	Conditions []string               `json:"conditions"`
	Actions    []string               `json:"actions"`
	Targets    []string               `json:"targets"`
	Attributes map[string]interface{} `json:"attributes"`
}

// NewLoadBalancerCrossCloudCorrelator creates a new load balancer correlator
func NewLoadBalancerCrossCloudCorrelator(confidenceThreshold float64) *LoadBalancerCrossCloudCorrelator {
	return &LoadBalancerCrossCloudCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *LoadBalancerCrossCloudCorrelator) GetName() string {
	return "loadbalancer_crosscloud_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *LoadBalancerCrossCloudCorrelator) GetSupportedTypes() []string {
	return []string{
		"cross_cloud_load_balancing", "backend_pool_correlation", 
		"health_check_correlation", "traffic_distribution",
		"failover_configuration", "geo_load_balancing",
	}
}

// FindCorrelations finds load balancer correlations across cloud providers
func (c *LoadBalancerCrossCloudCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Extract load balancer resources
	loadBalancers := c.extractLoadBalancerResources(resources)
	
	// Find backend target correlations
	backendCorrelations := c.findBackendTargetCorrelations(loadBalancers, resources)
	correlations = append(correlations, backendCorrelations...)
	
	// Find DNS-based load balancer correlations
	dnsCorrelations := c.findDNSLoadBalancerCorrelations(loadBalancers)
	correlations = append(correlations, dnsCorrelations...)
	
	// Find health check correlations
	healthCorrelations := c.findHealthCheckCorrelations(loadBalancers)
	correlations = append(correlations, healthCorrelations...)
	
	// Find traffic distribution patterns
	trafficCorrelations := c.findTrafficDistributionPatterns(loadBalancers)
	correlations = append(correlations, trafficCorrelations...)
	
	// Find failover configurations
	failoverCorrelations := c.findFailoverConfigurations(loadBalancers)
	correlations = append(correlations, failoverCorrelations...)
	
	return correlations, nil
}

// extractLoadBalancerResources extracts load balancer related resources
func (c *LoadBalancerCrossCloudCorrelator) extractLoadBalancerResources(resources []*models.Resource) map[string]*models.Resource {
	loadBalancers := make(map[string]*models.Resource)
	
	for _, resource := range resources {
		if c.isLoadBalancerResource(resource) {
			loadBalancers[resource.ID] = resource
		}
	}
	
	return loadBalancers
}

// isLoadBalancerResource checks if a resource is load balancer related
func (c *LoadBalancerCrossCloudCorrelator) isLoadBalancerResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	resourceName := strings.ToLower(resource.Name)
	
	// Load balancer resource types
	lbTypes := []string{
		"loadbalancer", "applicationloadbalancer", "networkloadbalancer",
		"trafficmanager", "frontdoor", "cloudloadbalancing",
		"aws::elasticloadbalancingv2::loadbalancer",
		"aws::elasticloadbalancing::loadbalancer",
		"microsoft.network/loadbalancers",
		"microsoft.network/applicationgateways",
		"microsoft.network/trafficmanagerprofiles",
		"microsoft.network/frontdoors",
		"google.compute.urlmap",
		"google.compute.backendservice",
		"google.compute.targetpool",
	}
	
	for _, lbType := range lbTypes {
		if strings.Contains(resourceType, lbType) {
			return true
		}
	}
	
	// Check for load balancer in name
	lbNames := []string{"lb", "alb", "nlb", "elb", "loadbalancer", "trafficmanager"}
	for _, lbName := range lbNames {
		if strings.Contains(resourceName, lbName) {
			return true
		}
	}
	
	return false
}

// findBackendTargetCorrelations finds correlations based on backend targets
func (c *LoadBalancerCrossCloudCorrelator) findBackendTargetCorrelations(loadBalancers map[string]*models.Resource, allResources []*models.Resource) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Create resource lookup map for quick access
	resourceMap := make(map[string]*models.Resource)
	for _, resource := range allResources {
		resourceMap[resource.ID] = resource
	}
	
	// Group load balancers by their backend targets
	targetGroups := make(map[string][]*models.Resource)
	
	for _, lb := range loadBalancers {
		config := c.extractLoadBalancerConfig(lb)
		
		// Group by backend targets
		for _, backend := range config.Backends {
			if _, exists := resourceMap[backend.Target]; exists {
				key := fmt.Sprintf("%s:%d", backend.Target, backend.Port)
				targetGroups[key] = append(targetGroups[key], lb)
			}
		}
	}
	
	// Find load balancers from different providers targeting same resources
	for targetKey, lbs := range targetGroups {
		if len(lbs) < 2 {
			continue
		}
		
		// Group by provider
		providerGroups := make(map[string][]*models.Resource)
		for _, lb := range lbs {
			providerGroups[lb.Provider] = append(providerGroups[lb.Provider], lb)
		}
		
		// Create correlations between different providers
		if len(providerGroups) > 1 {
			providers := make([]string, 0, len(providerGroups))
			for p := range providerGroups {
				providers = append(providers, p)
			}
			
			for i := 0; i < len(providers); i++ {
				for j := i + 1; j < len(providers); j++ {
					provider1, provider2 := providers[i], providers[j]
					
					for _, lb1 := range providerGroups[provider1] {
						for _, lb2 := range providerGroups[provider2] {
							confidence := c.calculateBackendCorrelationConfidence(lb1, lb2, targetKey)
							if confidence >= c.confidenceThreshold {
								correlation := &CrossCloudCorrelation{
									ID:                 uuid.New().String(),
									SourceResourceID:   lb1.ID,
									TargetResourceID:   lb2.ID,
									SourceProvider:     lb1.Provider,
									TargetProvider:     lb2.Provider,
									CorrelationType:    "backend_pool_correlation",
									CorrelationMethod:  "backend_target_analysis",
									ConfidenceScore:    confidence,
									Evidence: map[string]interface{}{
										"shared_target":   targetKey,
										"lb1_config":      c.extractLoadBalancerConfig(lb1),
										"lb2_config":      c.extractLoadBalancerConfig(lb2),
									},
									Description: fmt.Sprintf("Load balancers share backend target %s", targetKey),
									Status:      "active",
									DiscoveredAt: time.Now(),
								}
								correlations = append(correlations, correlation)
							}
						}
					}
				}
			}
		}
	}
	
	return correlations
}

// findDNSLoadBalancerCorrelations finds DNS-based load balancer correlations
func (c *LoadBalancerCrossCloudCorrelator) findDNSLoadBalancerCorrelations(loadBalancers map[string]*models.Resource) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group load balancers by DNS name patterns
	dnsGroups := make(map[string][]*models.Resource)
	
	for _, lb := range loadBalancers {
		config := c.extractLoadBalancerConfig(lb)
		if config.DNSName != "" {
			// Extract base domain for grouping
			baseDomain := c.extractBaseDomain(config.DNSName)
			dnsGroups[baseDomain] = append(dnsGroups[baseDomain], lb)
		}
	}
	
	// Find load balancers from different providers with similar DNS patterns
	for domain, lbs := range dnsGroups {
		if len(lbs) < 2 {
			continue
		}
		
		// Group by provider
		providerGroups := make(map[string][]*models.Resource)
		for _, lb := range lbs {
			providerGroups[lb.Provider] = append(providerGroups[lb.Provider], lb)
		}
		
		if len(providerGroups) > 1 {
			providers := make([]string, 0, len(providerGroups))
			for p := range providerGroups {
				providers = append(providers, p)
			}
			
			for i := 0; i < len(providers); i++ {
				for j := i + 1; j < len(providers); j++ {
					provider1, provider2 := providers[i], providers[j]
					
					for _, lb1 := range providerGroups[provider1] {
						for _, lb2 := range providerGroups[provider2] {
							confidence := c.calculateDNSCorrelationConfidence(lb1, lb2, domain)
							if confidence >= c.confidenceThreshold {
								correlation := &CrossCloudCorrelation{
									ID:                 uuid.New().String(),
									SourceResourceID:   lb1.ID,
									TargetResourceID:   lb2.ID,
									SourceProvider:     lb1.Provider,
									TargetProvider:     lb2.Provider,
									CorrelationType:    "dns_load_balancing",
									CorrelationMethod:  "dns_pattern_analysis",
									ConfidenceScore:    confidence,
									Evidence: map[string]interface{}{
										"shared_domain":   domain,
										"lb1_dns":         c.extractLoadBalancerConfig(lb1).DNSName,
										"lb2_dns":         c.extractLoadBalancerConfig(lb2).DNSName,
									},
									Description: fmt.Sprintf("DNS-based load balancing for domain %s", domain),
									Status:      "active",
									DiscoveredAt: time.Now(),
								}
								correlations = append(correlations, correlation)
							}
						}
					}
				}
			}
		}
	}
	
	return correlations
}

// findHealthCheckCorrelations finds health check correlations
func (c *LoadBalancerCrossCloudCorrelator) findHealthCheckCorrelations(loadBalancers map[string]*models.Resource) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group load balancers by health check configuration
	healthGroups := make(map[string][]*models.Resource)
	
	for _, lb := range loadBalancers {
		config := c.extractLoadBalancerConfig(lb)
		
		for _, healthCheck := range config.HealthChecks {
			key := fmt.Sprintf("%s:%s:%d", healthCheck.Type, healthCheck.Path, healthCheck.Port)
			healthGroups[key] = append(healthGroups[key], lb)
		}
	}
	
	// Find correlations based on similar health check configurations
	for healthKey, lbs := range healthGroups {
		if len(lbs) < 2 {
			continue
		}
		
		providerGroups := make(map[string][]*models.Resource)
		for _, lb := range lbs {
			providerGroups[lb.Provider] = append(providerGroups[lb.Provider], lb)
		}
		
		if len(providerGroups) > 1 {
			// Create correlations between providers
			for provider1, lbs1 := range providerGroups {
				for provider2, lbs2 := range providerGroups {
					if provider1 >= provider2 {
						continue
					}
					
					for _, lb1 := range lbs1 {
						for _, lb2 := range lbs2 {
							correlation := &CrossCloudCorrelation{
								ID:                 uuid.New().String(),
								SourceResourceID:   lb1.ID,
								TargetResourceID:   lb2.ID,
								SourceProvider:     lb1.Provider,
								TargetProvider:     lb2.Provider,
								CorrelationType:    "health_check_correlation",
								CorrelationMethod:  "health_check_analysis",
								ConfidenceScore:    0.7,
								Evidence: map[string]interface{}{
									"health_check_config": healthKey,
								},
								Description: fmt.Sprintf("Similar health check configuration: %s", healthKey),
								Status:      "active",
								DiscoveredAt: time.Now(),
							}
							correlations = append(correlations, correlation)
						}
					}
				}
			}
		}
	}
	
	return correlations
}

// findTrafficDistributionPatterns finds traffic distribution patterns
func (c *LoadBalancerCrossCloudCorrelator) findTrafficDistributionPatterns(loadBalancers map[string]*models.Resource) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Analyze traffic distribution across providers
	distributionPatterns := c.analyzeTrafficDistribution(loadBalancers)
	
	for pattern, lbs := range distributionPatterns {
		if len(lbs) < 2 {
			continue
		}
		
		// Group by provider
		providerGroups := make(map[string][]*models.Resource)
		for _, lb := range lbs {
			providerGroups[lb.Provider] = append(providerGroups[lb.Provider], lb)
		}
		
		if len(providerGroups) > 1 {
			providers := make([]string, 0, len(providerGroups))
			for p := range providerGroups {
				providers = append(providers, p)
			}
			
			for i := 0; i < len(providers); i++ {
				for j := i + 1; j < len(providers); j++ {
					provider1, provider2 := providers[i], providers[j]
					
					lb1 := providerGroups[provider1][0]
					lb2 := providerGroups[provider2][0]
					
					correlation := &CrossCloudCorrelation{
						ID:                 uuid.New().String(),
						SourceResourceID:   lb1.ID,
						TargetResourceID:   lb2.ID,
						SourceProvider:     provider1,
						TargetProvider:     provider2,
						CorrelationType:    "traffic_distribution",
						CorrelationMethod:  "traffic_pattern_analysis",
						ConfidenceScore:    0.6,
						Evidence: map[string]interface{}{
							"distribution_pattern": pattern,
							"involved_lbs":         len(lbs),
						},
						Description: fmt.Sprintf("Traffic distribution pattern: %s", pattern),
						Status:      "active",
						DiscoveredAt: time.Now(),
					}
					correlations = append(correlations, correlation)
				}
			}
		}
	}
	
	return correlations
}

// findFailoverConfigurations finds failover configurations
func (c *LoadBalancerCrossCloudCorrelator) findFailoverConfigurations(loadBalancers map[string]*models.Resource) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Analyze failover configurations
	failoverGroups := c.analyzeFailoverConfigurations(loadBalancers)
	
	for _, group := range failoverGroups {
		if len(group) < 2 {
			continue
		}
		
		// Check if failover spans multiple providers
		providers := make(map[string]bool)
		for _, lb := range group {
			providers[lb.Provider] = true
		}
		
		if len(providers) > 1 {
			// Create correlations between load balancers in failover group
			for i := 0; i < len(group); i++ {
				for j := i + 1; j < len(group); j++ {
					lb1, lb2 := group[i], group[j]
					
					if lb1.Provider != lb2.Provider {
						correlation := &CrossCloudCorrelation{
							ID:                 uuid.New().String(),
							SourceResourceID:   lb1.ID,
							TargetResourceID:   lb2.ID,
							SourceProvider:     lb1.Provider,
							TargetProvider:     lb2.Provider,
							CorrelationType:    "failover_configuration",
							CorrelationMethod:  "failover_analysis",
							ConfidenceScore:    0.8,
							Evidence: map[string]interface{}{
								"failover_group_size": len(group),
								"failover_type":       c.determineFailoverType(lb1, lb2),
							},
							Description: "Cross-cloud failover configuration",
							Status:      "active",
							DiscoveredAt: time.Now(),
						}
						correlations = append(correlations, correlation)
					}
				}
			}
		}
	}
	
	return correlations
}

// Helper functions

// extractLoadBalancerConfig extracts load balancer configuration
func (c *LoadBalancerCrossCloudCorrelator) extractLoadBalancerConfig(resource *models.Resource) *LoadBalancerConfig {
	config := &LoadBalancerConfig{
		Type:        c.determineLoadBalancerType(resource),
		Listeners:   make([]ListenerConfig, 0),
		Backends:    make([]BackendConfig, 0),
		HealthChecks: make([]HealthCheckConfig, 0),
		RoutingRules: make([]RoutingRuleConfig, 0),
		Attributes:  make(map[string]interface{}),
	}
	
	// Extract DNS name
	if dnsName, ok := resource.Attributes["dns_name"].(string); ok {
		config.DNSName = dnsName
	}
	
	// Extract IP addresses
	for _, ip := range resource.IPAddresses {
		config.IPAddresses = append(config.IPAddresses, ip.Address)
	}
	
	// Extract listeners
	if listeners, ok := resource.Attributes["listeners"]; ok {
		config.Listeners = c.parseListeners(listeners)
	}
	
	// Extract backends/targets
	if backends, ok := resource.Attributes["backends"]; ok {
		config.Backends = c.parseBackends(backends)
	} else if targets, ok := resource.Attributes["targets"]; ok {
		config.Backends = c.parseBackends(targets)
	}
	
	// Extract health checks
	if healthChecks, ok := resource.Attributes["health_checks"]; ok {
		config.HealthChecks = c.parseHealthChecks(healthChecks)
	}
	
	// Extract routing rules
	if rules, ok := resource.Attributes["routing_rules"]; ok {
		config.RoutingRules = c.parseRoutingRules(rules)
	}
	
	// Extract scheme
	if scheme, ok := resource.Attributes["scheme"].(string); ok {
		config.Scheme = scheme
	}
	
	return config
}

// determineLoadBalancerType determines the type of load balancer
func (c *LoadBalancerCrossCloudCorrelator) determineLoadBalancerType(resource *models.Resource) string {
	resourceType := strings.ToLower(resource.Type)
	
	if strings.Contains(resourceType, "application") {
		return "application"
	}
	if strings.Contains(resourceType, "network") {
		return "network"
	}
	if strings.Contains(resourceType, "gateway") {
		return "gateway"
	}
	if strings.Contains(resourceType, "trafficmanager") {
		return "traffic_manager"
	}
	
	return "classic"
}

// parseListeners parses listener configuration
func (c *LoadBalancerCrossCloudCorrelator) parseListeners(data interface{}) []ListenerConfig {
	listeners := make([]ListenerConfig, 0)
	
	// Implementation would parse the actual listener data structure
	// This is a simplified version
	
	return listeners
}

// parseBackends parses backend configuration
func (c *LoadBalancerCrossCloudCorrelator) parseBackends(data interface{}) []BackendConfig {
	backends := make([]BackendConfig, 0)
	
	// Implementation would parse the actual backend data structure
	// This is a simplified version
	
	return backends
}

// parseHealthChecks parses health check configuration
func (c *LoadBalancerCrossCloudCorrelator) parseHealthChecks(data interface{}) []HealthCheckConfig {
	healthChecks := make([]HealthCheckConfig, 0)
	
	// Implementation would parse the actual health check data structure
	// This is a simplified version
	
	return healthChecks
}

// parseRoutingRules parses routing rules
func (c *LoadBalancerCrossCloudCorrelator) parseRoutingRules(data interface{}) []RoutingRuleConfig {
	rules := make([]RoutingRuleConfig, 0)
	
	// Implementation would parse the actual routing rules data structure
	// This is a simplified version
	
	return rules
}

// extractBaseDomain extracts base domain from DNS name
func (c *LoadBalancerCrossCloudCorrelator) extractBaseDomain(dnsName string) string {
	parts := strings.Split(strings.ToLower(dnsName), ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return dnsName
}

// calculateBackendCorrelationConfidence calculates confidence for backend correlation
func (c *LoadBalancerCrossCloudCorrelator) calculateBackendCorrelationConfidence(lb1, lb2 *models.Resource, targetKey string) float64 {
	confidence := 0.5 // Base confidence for shared backend
	
	config1 := c.extractLoadBalancerConfig(lb1)
	config2 := c.extractLoadBalancerConfig(lb2)
	
	// Check for similar listener configurations
	if c.haveSimilarListeners(config1.Listeners, config2.Listeners) {
		confidence += 0.2
	}
	
	// Check for similar health check configurations
	if c.haveSimilarHealthChecks(config1.HealthChecks, config2.HealthChecks) {
		confidence += 0.2
	}
	
	// Check for similar routing rules
	if c.haveSimilarRoutingRules(config1.RoutingRules, config2.RoutingRules) {
		confidence += 0.1
	}
	
	return confidence
}

// calculateDNSCorrelationConfidence calculates confidence for DNS correlation
func (c *LoadBalancerCrossCloudCorrelator) calculateDNSCorrelationConfidence(lb1, lb2 *models.Resource, domain string) float64 {
	confidence := 0.4 // Base confidence for shared domain
	
	config1 := c.extractLoadBalancerConfig(lb1)
	config2 := c.extractLoadBalancerConfig(lb2)
	
	// Check DNS name similarity
	similarity := c.calculateDNSSimilarity(config1.DNSName, config2.DNSName)
	confidence += similarity * 0.3
	
	// Check for similar configurations
	if c.haveSimilarConfigurations(config1, config2) {
		confidence += 0.3
	}
	
	return confidence
}

// analyzeTrafficDistribution analyzes traffic distribution patterns
func (c *LoadBalancerCrossCloudCorrelator) analyzeTrafficDistribution(loadBalancers map[string]*models.Resource) map[string][]*models.Resource {
	patterns := make(map[string][]*models.Resource)
	
	// Group by similar distribution patterns
	for _, lb := range loadBalancers {
		pattern := c.determineDistributionPattern(lb)
		patterns[pattern] = append(patterns[pattern], lb)
	}
	
	return patterns
}

// analyzeFailoverConfigurations analyzes failover configurations
func (c *LoadBalancerCrossCloudCorrelator) analyzeFailoverConfigurations(loadBalancers map[string]*models.Resource) [][]*models.Resource {
	groups := make([][]*models.Resource, 0)
	
	// Implementation would analyze actual failover configurations
	// This is a simplified version
	
	return groups
}

// determineFailoverType determines the type of failover
func (c *LoadBalancerCrossCloudCorrelator) determineFailoverType(lb1, lb2 *models.Resource) string {
	// Analyze the load balancer configurations to determine failover type
	return "active_passive" // Simplified
}

// Helper functions for similarity analysis

func (c *LoadBalancerCrossCloudCorrelator) haveSimilarListeners(listeners1, listeners2 []ListenerConfig) bool {
	// Compare listener configurations
	return len(listeners1) > 0 && len(listeners2) > 0 // Simplified
}

func (c *LoadBalancerCrossCloudCorrelator) haveSimilarHealthChecks(checks1, checks2 []HealthCheckConfig) bool {
	// Compare health check configurations
	return len(checks1) > 0 && len(checks2) > 0 // Simplified
}

func (c *LoadBalancerCrossCloudCorrelator) haveSimilarRoutingRules(rules1, rules2 []RoutingRuleConfig) bool {
	// Compare routing rule configurations
	return len(rules1) > 0 && len(rules2) > 0 // Simplified
}

func (c *LoadBalancerCrossCloudCorrelator) haveSimilarConfigurations(config1, config2 *LoadBalancerConfig) bool {
	// Compare overall configurations
	return config1.Type == config2.Type && config1.Scheme == config2.Scheme
}

func (c *LoadBalancerCrossCloudCorrelator) calculateDNSSimilarity(dns1, dns2 string) float64 {
	// Calculate similarity between DNS names
	if dns1 == dns2 {
		return 1.0
	}
	
	// Extract subdomains and compare
	parts1 := strings.Split(dns1, ".")
	parts2 := strings.Split(dns2, ".")
	
	commonParts := 0
	maxParts := len(parts1)
	if len(parts2) > maxParts {
		maxParts = len(parts2)
	}
	
	for i := 1; i <= len(parts1) && i <= len(parts2); i++ {
		if parts1[len(parts1)-i] == parts2[len(parts2)-i] {
			commonParts++
		} else {
			break
		}
	}
	
	if maxParts > 0 {
		return float64(commonParts) / float64(maxParts)
	}
	
	return 0.0
}

func (c *LoadBalancerCrossCloudCorrelator) determineDistributionPattern(lb *models.Resource) string {
	// Determine traffic distribution pattern
	config := c.extractLoadBalancerConfig(lb)
	
	if len(config.Backends) > 1 {
		return "multi_target"
	}
	
	return "single_target"
}
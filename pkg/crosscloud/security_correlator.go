package crosscloud

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
	"github.com/google/uuid"
)

// SecurityGroupCorrelator analyzes security group rule correlations across cloud providers
type SecurityGroupCorrelator struct {
	confidenceThreshold float64
}

// NetworkACLCorrelator analyzes network ACL correlations
type NetworkACLCorrelator struct {
	confidenceThreshold float64
}

// FirewallRuleCorrelator analyzes firewall rule correlations
type FirewallRuleCorrelator struct {
	confidenceThreshold float64
}

// SecurityRule represents a unified security rule across providers
type SecurityRule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Direction   string                 `json:"direction"`   // inbound, outbound
	Action      string                 `json:"action"`      // allow, deny
	Priority    int                    `json:"priority"`
	Protocol    string                 `json:"protocol"`    // tcp, udp, icmp, any
	SourceCIDR  []string               `json:"source_cidr"`
	DestCIDR    []string               `json:"dest_cidr"`
	SourcePorts []PortRange            `json:"source_ports"`
	DestPorts   []PortRange            `json:"dest_ports"`
	Tags        map[string]string      `json:"tags"`
	Attributes  map[string]interface{} `json:"attributes"`
	Resource    *models.Resource       `json:"resource"`
}

// PortRange represents a port range
type PortRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// SecurityRuleOverlap represents overlap analysis between security rules
type SecurityRuleOverlap struct {
	OverlapType        string  `json:"overlap_type"`
	OverlapPercentage  float64 `json:"overlap_percentage"`
	ConflictingRules   []SecurityRuleConflict `json:"conflicting_rules"`
	ComplementaryRules []SecurityRuleComplement `json:"complementary_rules"`
}

type SecurityRuleConflict struct {
	Rule1       *SecurityRule `json:"rule1"`
	Rule2       *SecurityRule `json:"rule2"`
	ConflictType string       `json:"conflict_type"`
	Description  string       `json:"description"`
}

type SecurityRuleComplement struct {
	Rule1       *SecurityRule `json:"rule1"`
	Rule2       *SecurityRule `json:"rule2"`
	Complement  string        `json:"complement_type"`
	Description string        `json:"description"`
}

// NewSecurityGroupCorrelator creates a new security group correlator
func NewSecurityGroupCorrelator(confidenceThreshold float64) *SecurityGroupCorrelator {
	return &SecurityGroupCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *SecurityGroupCorrelator) GetName() string {
	return "security_group_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *SecurityGroupCorrelator) GetSupportedTypes() []string {
	return []string{
		"security_rule_overlap", "firewall_rule_correlation", "network_acl_correlation",
		"port_protocol_correlation", "security_policy_similarity", "access_control_pattern",
	}
}

// FindCorrelations finds security group correlations across cloud providers
func (c *SecurityGroupCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Extract security-related resources
	securityResources := c.extractSecurityResources(resources)
	
	// Convert to unified security rules
	securityRules := c.convertToUnifiedRules(securityResources)
	
	// Find rule overlap correlations
	overlapCorrelations := c.findSecurityRuleOverlaps(securityRules)
	correlations = append(correlations, overlapCorrelations...)
	
	// Find port and protocol correlations
	portCorrelations := c.findPortProtocolCorrelations(securityRules)
	correlations = append(correlations, portCorrelations...)
	
	// Find security policy patterns
	policyCorrelations := c.findSecurityPolicyPatterns(securityRules)
	correlations = append(correlations, policyCorrelations...)
	
	// Find access control patterns
	accessCorrelations := c.findAccessControlPatterns(securityRules)
	correlations = append(correlations, accessCorrelations...)
	
	return correlations, nil
}

// extractSecurityResources extracts security-related resources
func (c *SecurityGroupCorrelator) extractSecurityResources(resources []*models.Resource) []*models.Resource {
	securityResources := make([]*models.Resource, 0)
	
	for _, resource := range resources {
		if c.isSecurityResource(resource) {
			securityResources = append(securityResources, resource)
		}
	}
	
	return securityResources
}

// isSecurityResource checks if a resource is security-related
func (c *SecurityGroupCorrelator) isSecurityResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	
	securityTypes := []string{
		"securitygroup", "networkacl", "firewall", "firewallrule",
		"aws::ec2::securitygroup", "aws::ec2::networkacl",
		"microsoft.network/networksecuritygroups",
		"microsoft.network/networksecuritygroups/securityrules",
		"google.compute.firewall",
		"google.compute.securitypolicy",
	}
	
	for _, secType := range securityTypes {
		if strings.Contains(resourceType, secType) {
			return true
		}
	}
	
	return false
}

// convertToUnifiedRules converts provider-specific resources to unified security rules
func (c *SecurityGroupCorrelator) convertToUnifiedRules(resources []*models.Resource) []*SecurityRule {
	rules := make([]*SecurityRule, 0)
	
	for _, resource := range resources {
		providerRules := c.extractRulesFromResource(resource)
		rules = append(rules, providerRules...)
	}
	
	return rules
}

// extractRulesFromResource extracts security rules from a resource based on provider
func (c *SecurityGroupCorrelator) extractRulesFromResource(resource *models.Resource) []*SecurityRule {
	rules := make([]*SecurityRule, 0)
	
	switch strings.ToLower(resource.Provider) {
	case "aws":
		rules = append(rules, c.extractAWSSecurityRules(resource)...)
	case "azure":
		rules = append(rules, c.extractAzureSecurityRules(resource)...)
	case "gcp":
		rules = append(rules, c.extractGCPSecurityRules(resource)...)
	}
	
	return rules
}

// extractAWSSecurityRules extracts security rules from AWS resources
func (c *SecurityGroupCorrelator) extractAWSSecurityRules(resource *models.Resource) []*SecurityRule {
	rules := make([]*SecurityRule, 0)
	
	// Extract inbound rules
	if inboundRules, ok := resource.Attributes["inbound_rules"]; ok {
		if rulesArray, ok := inboundRules.([]interface{}); ok {
			for i, rule := range rulesArray {
				if ruleMap, ok := rule.(map[string]interface{}); ok {
					securityRule := &SecurityRule{
						ID:        fmt.Sprintf("%s-inbound-%d", resource.ID, i),
						Name:      fmt.Sprintf("%s-inbound-%d", resource.Name, i),
						Direction: "inbound",
						Action:    "allow", // AWS security groups are allow-only
						Resource:  resource,
					}
					
					c.populateAWSRule(securityRule, ruleMap)
					rules = append(rules, securityRule)
				}
			}
		}
	}
	
	// Extract outbound rules
	if outboundRules, ok := resource.Attributes["outbound_rules"]; ok {
		if rulesArray, ok := outboundRules.([]interface{}); ok {
			for i, rule := range rulesArray {
				if ruleMap, ok := rule.(map[string]interface{}); ok {
					securityRule := &SecurityRule{
						ID:        fmt.Sprintf("%s-outbound-%d", resource.ID, i),
						Name:      fmt.Sprintf("%s-outbound-%d", resource.Name, i),
						Direction: "outbound",
						Action:    "allow",
						Resource:  resource,
					}
					
					c.populateAWSRule(securityRule, ruleMap)
					rules = append(rules, securityRule)
				}
			}
		}
	}
	
	return rules
}

// extractAzureSecurityRules extracts security rules from Azure resources
func (c *SecurityGroupCorrelator) extractAzureSecurityRules(resource *models.Resource) []*SecurityRule {
	rules := make([]*SecurityRule, 0)
	
	if securityRules, ok := resource.Attributes["security_rules"]; ok {
		if rulesArray, ok := securityRules.([]interface{}); ok {
			for i, rule := range rulesArray {
				if ruleMap, ok := rule.(map[string]interface{}); ok {
					securityRule := &SecurityRule{
						ID:       fmt.Sprintf("%s-rule-%d", resource.ID, i),
						Resource: resource,
					}
					
					c.populateAzureRule(securityRule, ruleMap)
					rules = append(rules, securityRule)
				}
			}
		}
	}
	
	return rules
}

// extractGCPSecurityRules extracts security rules from GCP resources
func (c *SecurityGroupCorrelator) extractGCPSecurityRules(resource *models.Resource) []*SecurityRule {
	rules := make([]*SecurityRule, 0)
	
	// GCP firewall rules are typically individual resources
	securityRule := &SecurityRule{
		ID:       resource.ID,
		Name:     resource.Name,
		Resource: resource,
	}
	
	c.populateGCPRule(securityRule, resource.Attributes)
	rules = append(rules, securityRule)
	
	return rules
}

// populateAWSRule populates AWS-specific rule details
func (c *SecurityGroupCorrelator) populateAWSRule(rule *SecurityRule, ruleData map[string]interface{}) {
	if protocol, ok := ruleData["protocol"].(string); ok {
		rule.Protocol = protocol
	}
	
	if fromPort, ok := ruleData["from_port"]; ok {
		if toPort, ok := ruleData["to_port"]; ok {
			rule.DestPorts = []PortRange{{
				From: c.interfaceToInt(fromPort),
				To:   c.interfaceToInt(toPort),
			}}
		}
	}
	
	if cidrBlocks, ok := ruleData["cidr_blocks"].([]interface{}); ok {
		for _, cidr := range cidrBlocks {
			if cidrStr, ok := cidr.(string); ok {
				rule.SourceCIDR = append(rule.SourceCIDR, cidrStr)
			}
		}
	}
}

// populateAzureRule populates Azure-specific rule details
func (c *SecurityGroupCorrelator) populateAzureRule(rule *SecurityRule, ruleData map[string]interface{}) {
	if name, ok := ruleData["name"].(string); ok {
		rule.Name = name
	}
	
	if direction, ok := ruleData["direction"].(string); ok {
		rule.Direction = strings.ToLower(direction)
	}
	
	if access, ok := ruleData["access"].(string); ok {
		rule.Action = strings.ToLower(access)
	}
	
	if priority, ok := ruleData["priority"]; ok {
		rule.Priority = c.interfaceToInt(priority)
	}
	
	if protocol, ok := ruleData["protocol"].(string); ok {
		rule.Protocol = strings.ToLower(protocol)
	}
	
	if sourcePortRange, ok := ruleData["source_port_range"].(string); ok {
		rule.SourcePorts = c.parsePortRange(sourcePortRange)
	}
	
	if destPortRange, ok := ruleData["destination_port_range"].(string); ok {
		rule.DestPorts = c.parsePortRange(destPortRange)
	}
	
	if sourcePrefix, ok := ruleData["source_address_prefix"].(string); ok {
		rule.SourceCIDR = []string{sourcePrefix}
	}
	
	if destPrefix, ok := ruleData["destination_address_prefix"].(string); ok {
		rule.DestCIDR = []string{destPrefix}
	}
}

// populateGCPRule populates GCP-specific rule details
func (c *SecurityGroupCorrelator) populateGCPRule(rule *SecurityRule, ruleData map[string]interface{}) {
	if direction, ok := ruleData["direction"].(string); ok {
		rule.Direction = strings.ToLower(direction)
	}
	
	if action, ok := ruleData["action"].(string); ok {
		rule.Action = strings.ToLower(action)
	}
	
	if priority, ok := ruleData["priority"]; ok {
		rule.Priority = c.interfaceToInt(priority)
	}
	
	if sourceRanges, ok := ruleData["source_ranges"].([]interface{}); ok {
		for _, sourceRange := range sourceRanges {
			if rangeStr, ok := sourceRange.(string); ok {
				rule.SourceCIDR = append(rule.SourceCIDR, rangeStr)
			}
		}
	}
	
	if allowed, ok := ruleData["allowed"].([]interface{}); ok {
		for _, allowedRule := range allowed {
			if allowedMap, ok := allowedRule.(map[string]interface{}); ok {
				if protocol, ok := allowedMap["protocol"].(string); ok {
					rule.Protocol = protocol
				}
				if ports, ok := allowedMap["ports"].([]interface{}); ok {
					for _, port := range ports {
						if portStr, ok := port.(string); ok {
							rule.DestPorts = append(rule.DestPorts, c.parsePortRange(portStr)...)
						}
					}
				}
			}
		}
	}
}

// findSecurityRuleOverlaps finds overlapping security rules across providers
func (c *SecurityGroupCorrelator) findSecurityRuleOverlaps(rules []*SecurityRule) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group rules by provider
	providerRules := make(map[string][]*SecurityRule)
	for _, rule := range rules {
		providerRules[rule.Resource.Provider] = append(providerRules[rule.Resource.Provider], rule)
	}
	
	// Compare rules between different providers
	providers := make([]string, 0, len(providerRules))
	for provider := range providerRules {
		providers = append(providers, provider)
	}
	
	for i := 0; i < len(providers); i++ {
		for j := i + 1; j < len(providers); j++ {
			provider1, provider2 := providers[i], providers[j]
			
			for _, rule1 := range providerRules[provider1] {
				for _, rule2 := range providerRules[provider2] {
					overlap := c.analyzeRuleOverlap(rule1, rule2)
					if overlap.OverlapPercentage >= 0.7 { // 70% overlap threshold
						confidence := c.calculateOverlapConfidence(overlap)
						if confidence >= c.confidenceThreshold {
							correlation := &CrossCloudCorrelation{
								ID:                 uuid.New().String(),
								SourceResourceID:   rule1.Resource.ID,
								TargetResourceID:   rule2.Resource.ID,
								SourceProvider:     provider1,
								TargetProvider:     provider2,
								CorrelationType:    "security_rule_overlap",
								CorrelationMethod:  "rule_overlap_analysis",
								ConfidenceScore:    confidence,
								Evidence: map[string]interface{}{
									"rule1":         rule1,
									"rule2":         rule2,
									"overlap_analysis": overlap,
								},
								Description: fmt.Sprintf("Security rule overlap: %.1f%%", overlap.OverlapPercentage*100),
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

// findPortProtocolCorrelations finds port and protocol correlations
func (c *SecurityGroupCorrelator) findPortProtocolCorrelations(rules []*SecurityRule) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group rules by port/protocol combinations
	portProtocolGroups := make(map[string][]*SecurityRule)
	
	for _, rule := range rules {
		for _, portRange := range rule.DestPorts {
			key := fmt.Sprintf("%s:%d-%d", rule.Protocol, portRange.From, portRange.To)
			portProtocolGroups[key] = append(portProtocolGroups[key], rule)
		}
	}
	
	// Find correlations for each port/protocol group
	for portProtocol, groupRules := range portProtocolGroups {
		if len(groupRules) < 2 {
			continue
		}
		
		// Group by provider
		providerGroups := make(map[string][]*SecurityRule)
		for _, rule := range groupRules {
			providerGroups[rule.Resource.Provider] = append(providerGroups[rule.Resource.Provider], rule)
		}
		
		if len(providerGroups) > 1 {
			// Create correlations between providers
			providers := make([]string, 0, len(providerGroups))
			for provider := range providerGroups {
				providers = append(providers, provider)
			}
			
			for i := 0; i < len(providers); i++ {
				for j := i + 1; j < len(providers); j++ {
					provider1, provider2 := providers[i], providers[j]
					
					rule1 := providerGroups[provider1][0]
					rule2 := providerGroups[provider2][0]
					
					correlation := &CrossCloudCorrelation{
						ID:                 uuid.New().String(),
						SourceResourceID:   rule1.Resource.ID,
						TargetResourceID:   rule2.Resource.ID,
						SourceProvider:     provider1,
						TargetProvider:     provider2,
						CorrelationType:    "port_protocol_correlation",
						CorrelationMethod:  "port_protocol_analysis",
						ConfidenceScore:    0.8,
						Evidence: map[string]interface{}{
							"port_protocol":   portProtocol,
							"rule_count":      len(groupRules),
							"provider_count":  len(providerGroups),
						},
						Description: fmt.Sprintf("Port/protocol correlation: %s", portProtocol),
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

// findSecurityPolicyPatterns finds security policy patterns
func (c *SecurityGroupCorrelator) findSecurityPolicyPatterns(rules []*SecurityRule) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Analyze security patterns
	patterns := c.analyzeSecurityPatterns(rules)
	
	for pattern, patternRules := range patterns {
		if len(patternRules) < 2 {
			continue
		}
		
		// Group by provider
		providerGroups := make(map[string][]*SecurityRule)
		for _, rule := range patternRules {
			providerGroups[rule.Resource.Provider] = append(providerGroups[rule.Resource.Provider], rule)
		}
		
		if len(providerGroups) > 1 {
			providers := make([]string, 0, len(providerGroups))
			for provider := range providerGroups {
				providers = append(providers, provider)
			}
			
			for i := 0; i < len(providers); i++ {
				for j := i + 1; j < len(providers); j++ {
					provider1, provider2 := providers[i], providers[j]
					
					rule1 := providerGroups[provider1][0]
					rule2 := providerGroups[provider2][0]
					
					correlation := &CrossCloudCorrelation{
						ID:                 uuid.New().String(),
						SourceResourceID:   rule1.Resource.ID,
						TargetResourceID:   rule2.Resource.ID,
						SourceProvider:     provider1,
						TargetProvider:     provider2,
						CorrelationType:    "security_policy_similarity",
						CorrelationMethod:  "security_pattern_analysis",
						ConfidenceScore:    0.7,
						Evidence: map[string]interface{}{
							"security_pattern": pattern,
							"rule_count":       len(patternRules),
						},
						Description: fmt.Sprintf("Security policy pattern: %s", pattern),
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

// findAccessControlPatterns finds access control patterns
func (c *SecurityGroupCorrelator) findAccessControlPatterns(rules []*SecurityRule) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Analyze access control patterns
	accessPatterns := c.analyzeAccessControlPatterns(rules)
	
	for pattern, patternRules := range accessPatterns {
		if len(patternRules) < 2 {
			continue
		}
		
		// Check if pattern spans multiple providers
		providers := make(map[string]bool)
		for _, rule := range patternRules {
			providers[rule.Resource.Provider] = true
		}
		
		if len(providers) > 1 {
			// Create correlations for cross-provider access patterns
			providerList := make([]string, 0, len(providers))
			rulesByProvider := make(map[string]*SecurityRule)
			
			for _, rule := range patternRules {
				if _, exists := rulesByProvider[rule.Resource.Provider]; !exists {
					providerList = append(providerList, rule.Resource.Provider)
					rulesByProvider[rule.Resource.Provider] = rule
				}
			}
			
			for i := 0; i < len(providerList); i++ {
				for j := i + 1; j < len(providerList); j++ {
					provider1, provider2 := providerList[i], providerList[j]
					
					rule1 := rulesByProvider[provider1]
					rule2 := rulesByProvider[provider2]
					
					correlation := &CrossCloudCorrelation{
						ID:                 uuid.New().String(),
						SourceResourceID:   rule1.Resource.ID,
						TargetResourceID:   rule2.Resource.ID,
						SourceProvider:     provider1,
						TargetProvider:     provider2,
						CorrelationType:    "access_control_pattern",
						CorrelationMethod:  "access_pattern_analysis",
						ConfidenceScore:    0.6,
						Evidence: map[string]interface{}{
							"access_pattern": pattern,
							"rule_count":     len(patternRules),
						},
						Description: fmt.Sprintf("Access control pattern: %s", pattern),
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

// Helper functions

// analyzeRuleOverlap analyzes overlap between two security rules
func (c *SecurityGroupCorrelator) analyzeRuleOverlap(rule1, rule2 *SecurityRule) *SecurityRuleOverlap {
	overlap := &SecurityRuleOverlap{
		ConflictingRules:   make([]SecurityRuleConflict, 0),
		ComplementaryRules: make([]SecurityRuleComplement, 0),
	}
	
	// Calculate overlap percentage
	overlap.OverlapPercentage = c.calculateRuleOverlapPercentage(rule1, rule2)
	
	// Determine overlap type
	if overlap.OverlapPercentage >= 0.9 {
		overlap.OverlapType = "near_identical"
	} else if overlap.OverlapPercentage >= 0.7 {
		overlap.OverlapType = "significant_overlap"
	} else if overlap.OverlapPercentage >= 0.3 {
		overlap.OverlapType = "partial_overlap"
	} else {
		overlap.OverlapType = "minimal_overlap"
	}
	
	// Check for conflicts
	if c.rulesConflict(rule1, rule2) {
		conflict := SecurityRuleConflict{
			Rule1:        rule1,
			Rule2:        rule2,
			ConflictType: c.determineConflictType(rule1, rule2),
			Description:  fmt.Sprintf("Rules have conflicting %s", c.determineConflictType(rule1, rule2)),
		}
		overlap.ConflictingRules = append(overlap.ConflictingRules, conflict)
	}
	
	// Check for complementary rules
	if c.rulesComplement(rule1, rule2) {
		complement := SecurityRuleComplement{
			Rule1:       rule1,
			Rule2:       rule2,
			Complement:  c.determineComplementType(rule1, rule2),
			Description: fmt.Sprintf("Rules are complementary: %s", c.determineComplementType(rule1, rule2)),
		}
		overlap.ComplementaryRules = append(overlap.ComplementaryRules, complement)
	}
	
	return overlap
}

// calculateRuleOverlapPercentage calculates the overlap percentage between two rules
func (c *SecurityGroupCorrelator) calculateRuleOverlapPercentage(rule1, rule2 *SecurityRule) float64 {
	score := 0.0
	totalCriteria := 5.0 // protocol, direction, action, ports, CIDR
	
	// Protocol match
	if rule1.Protocol == rule2.Protocol {
		score += 1.0
	}
	
	// Direction match
	if rule1.Direction == rule2.Direction {
		score += 1.0
	}
	
	// Action match
	if rule1.Action == rule2.Action {
		score += 1.0
	}
	
	// Port overlap
	portOverlap := c.calculatePortOverlap(rule1.DestPorts, rule2.DestPorts)
	score += portOverlap
	
	// CIDR overlap
	cidrOverlap := c.calculateCIDROverlap(rule1.SourceCIDR, rule2.SourceCIDR)
	score += cidrOverlap
	
	return score / totalCriteria
}

// calculatePortOverlap calculates overlap between port ranges
func (c *SecurityGroupCorrelator) calculatePortOverlap(ports1, ports2 []PortRange) float64 {
	if len(ports1) == 0 || len(ports2) == 0 {
		return 0.0
	}
	
	overlapCount := 0
	totalPairs := len(ports1) * len(ports2)
	
	for _, p1 := range ports1 {
		for _, p2 := range ports2 {
			if c.portRangesOverlap(p1, p2) {
				overlapCount++
			}
		}
	}
	
	if totalPairs > 0 {
		return float64(overlapCount) / float64(totalPairs)
	}
	
	return 0.0
}

// calculateCIDROverlap calculates overlap between CIDR blocks
func (c *SecurityGroupCorrelator) calculateCIDROverlap(cidrs1, cidrs2 []string) float64 {
	if len(cidrs1) == 0 || len(cidrs2) == 0 {
		return 0.0
	}
	
	overlapCount := 0
	totalPairs := len(cidrs1) * len(cidrs2)
	
	for _, cidr1 := range cidrs1 {
		for _, cidr2 := range cidrs2 {
			if c.cidrsOverlap(cidr1, cidr2) {
				overlapCount++
			}
		}
	}
	
	if totalPairs > 0 {
		return float64(overlapCount) / float64(totalPairs)
	}
	
	return 0.0
}

// portRangesOverlap checks if two port ranges overlap
func (c *SecurityGroupCorrelator) portRangesOverlap(p1, p2 PortRange) bool {
	return p1.From <= p2.To && p2.From <= p1.To
}

// cidrsOverlap checks if two CIDR blocks overlap
func (c *SecurityGroupCorrelator) cidrsOverlap(cidr1, cidr2 string) bool {
	_, net1, err1 := net.ParseCIDR(cidr1)
	_, net2, err2 := net.ParseCIDR(cidr2)
	
	if err1 != nil || err2 != nil {
		// If not valid CIDR, do string comparison
		return cidr1 == cidr2
	}
	
	// Check if networks overlap
	return net1.Contains(net2.IP) || net2.Contains(net1.IP)
}

// calculateOverlapConfidence calculates confidence based on overlap analysis
func (c *SecurityGroupCorrelator) calculateOverlapConfidence(overlap *SecurityRuleOverlap) float64 {
	confidence := overlap.OverlapPercentage
	
	// Increase confidence for complementary rules
	if len(overlap.ComplementaryRules) > 0 {
		confidence += 0.1
	}
	
	// Decrease confidence for conflicting rules
	if len(overlap.ConflictingRules) > 0 {
		confidence -= 0.2
	}
	
	return confidence
}

// rulesConflict checks if two rules conflict
func (c *SecurityGroupCorrelator) rulesConflict(rule1, rule2 *SecurityRule) bool {
	// Rules conflict if they have same scope but different actions
	if rule1.Direction == rule2.Direction &&
		rule1.Protocol == rule2.Protocol &&
		rule1.Action != rule2.Action &&
		c.calculatePortOverlap(rule1.DestPorts, rule2.DestPorts) > 0.5 &&
		c.calculateCIDROverlap(rule1.SourceCIDR, rule2.SourceCIDR) > 0.5 {
		return true
	}
	
	return false
}

// rulesComplement checks if two rules complement each other
func (c *SecurityGroupCorrelator) rulesComplement(rule1, rule2 *SecurityRule) bool {
	// Rules complement if they cover different directions for same resource
	if rule1.Direction != rule2.Direction &&
		rule1.Protocol == rule2.Protocol &&
		rule1.Action == rule2.Action &&
		c.calculatePortOverlap(rule1.DestPorts, rule2.DestPorts) > 0.7 {
		return true
	}
	
	return false
}

// determineConflictType determines the type of conflict between rules
func (c *SecurityGroupCorrelator) determineConflictType(rule1, rule2 *SecurityRule) string {
	if rule1.Action != rule2.Action {
		return "action_conflict"
	}
	if rule1.Priority != rule2.Priority {
		return "priority_conflict"
	}
	return "general_conflict"
}

// determineComplementType determines the type of complement between rules
func (c *SecurityGroupCorrelator) determineComplementType(rule1, rule2 *SecurityRule) string {
	if rule1.Direction != rule2.Direction {
		return "directional_complement"
	}
	return "general_complement"
}

// analyzeSecurityPatterns analyzes security patterns in rules
func (c *SecurityGroupCorrelator) analyzeSecurityPatterns(rules []*SecurityRule) map[string][]*SecurityRule {
	patterns := make(map[string][]*SecurityRule)
	
	for _, rule := range rules {
		// Classify rule into patterns
		rulePatterns := c.classifySecurityRule(rule)
		for _, pattern := range rulePatterns {
			patterns[pattern] = append(patterns[pattern], rule)
		}
	}
	
	return patterns
}

// classifySecurityRule classifies a security rule into patterns
func (c *SecurityGroupCorrelator) classifySecurityRule(rule *SecurityRule) []string {
	patterns := make([]string, 0)
	
	// Web service pattern
	if c.isWebServiceRule(rule) {
		patterns = append(patterns, "web_service")
	}
	
	// Database pattern
	if c.isDatabaseRule(rule) {
		patterns = append(patterns, "database")
	}
	
	// SSH/RDP pattern
	if c.isRemoteAccessRule(rule) {
		patterns = append(patterns, "remote_access")
	}
	
	// Internal communication pattern
	if c.isInternalCommunicationRule(rule) {
		patterns = append(patterns, "internal_communication")
	}
	
	return patterns
}

// isWebServiceRule checks if rule is for web services
func (c *SecurityGroupCorrelator) isWebServiceRule(rule *SecurityRule) bool {
	for _, portRange := range rule.DestPorts {
		if (portRange.From <= 80 && 80 <= portRange.To) ||
			(portRange.From <= 443 && 443 <= portRange.To) ||
			(portRange.From <= 8080 && 8080 <= portRange.To) {
			return true
		}
	}
	return false
}

// isDatabaseRule checks if rule is for database services
func (c *SecurityGroupCorrelator) isDatabaseRule(rule *SecurityRule) bool {
	dbPorts := []int{3306, 5432, 1433, 3389, 27017, 6379}
	
	for _, portRange := range rule.DestPorts {
		for _, dbPort := range dbPorts {
			if portRange.From <= dbPort && dbPort <= portRange.To {
				return true
			}
		}
	}
	return false
}

// isRemoteAccessRule checks if rule is for remote access
func (c *SecurityGroupCorrelator) isRemoteAccessRule(rule *SecurityRule) bool {
	for _, portRange := range rule.DestPorts {
		if (portRange.From <= 22 && 22 <= portRange.To) ||   // SSH
			(portRange.From <= 3389 && 3389 <= portRange.To) { // RDP
			return true
		}
	}
	return false
}

// isInternalCommunicationRule checks if rule is for internal communication
func (c *SecurityGroupCorrelator) isInternalCommunicationRule(rule *SecurityRule) bool {
	for _, cidr := range rule.SourceCIDR {
		if strings.HasPrefix(cidr, "10.") ||
			strings.HasPrefix(cidr, "192.168.") ||
			strings.HasPrefix(cidr, "172.") {
			return true
		}
	}
	return false
}

// analyzeAccessControlPatterns analyzes access control patterns
func (c *SecurityGroupCorrelator) analyzeAccessControlPatterns(rules []*SecurityRule) map[string][]*SecurityRule {
	patterns := make(map[string][]*SecurityRule)
	
	for _, rule := range rules {
		// Classify access control patterns
		if rule.Action == "allow" && len(rule.SourceCIDR) == 1 && rule.SourceCIDR[0] == "0.0.0.0/0" {
			patterns["public_access"] = append(patterns["public_access"], rule)
		}
		
		if rule.Action == "deny" {
			patterns["explicit_deny"] = append(patterns["explicit_deny"], rule)
		}
		
		if c.isRestrictiveRule(rule) {
			patterns["restrictive_access"] = append(patterns["restrictive_access"], rule)
		}
	}
	
	return patterns
}

// isRestrictiveRule checks if rule is restrictive
func (c *SecurityGroupCorrelator) isRestrictiveRule(rule *SecurityRule) bool {
	// Rule is restrictive if it has specific source CIDRs (not 0.0.0.0/0)
	for _, cidr := range rule.SourceCIDR {
		if cidr != "0.0.0.0/0" && cidr != "::/0" {
			return true
		}
	}
	return false
}

// Utility functions

// interfaceToInt converts interface{} to int
func (c *SecurityGroupCorrelator) interfaceToInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

// parsePortRange parses a port range string
func (c *SecurityGroupCorrelator) parsePortRange(portRange string) []PortRange {
	ranges := make([]PortRange, 0)
	
	if portRange == "*" || portRange == "any" {
		ranges = append(ranges, PortRange{From: 0, To: 65535})
		return ranges
	}
	
	parts := strings.Split(portRange, "-")
	if len(parts) == 1 {
		// Single port
		if port, err := strconv.Atoi(parts[0]); err == nil {
			ranges = append(ranges, PortRange{From: port, To: port})
		}
	} else if len(parts) == 2 {
		// Port range
		if fromPort, err1 := strconv.Atoi(parts[0]); err1 == nil {
			if toPort, err2 := strconv.Atoi(parts[1]); err2 == nil {
				ranges = append(ranges, PortRange{From: fromPort, To: toPort})
			}
		}
	}
	
	return ranges
}
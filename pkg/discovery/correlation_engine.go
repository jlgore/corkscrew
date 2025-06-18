package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jlgore/corkscrew/pkg/models"
)

type CorrelationEngine struct {
	config             SmartDiscoveryConfig
	correlationRules   []CorrelationRule
	crossProviderRules []CrossProviderRule
	mutex              sync.RWMutex
}

type CorrelationRule struct {
	Name        string
	Description string
	SourceType  string
	TargetType  string
	Matcher     func(source, target models.Resource) bool
	Confidence  float64
}

type CrossProviderRule struct {
	Name         string
	Description  string
	ProviderA    string
	ProviderB    string
	ResourceTypeA string
	ResourceTypeB string
	Matcher      func(resourceA, resourceB models.Resource) bool
	Confidence   float64
}

func NewCorrelationEngine(config SmartDiscoveryConfig) *CorrelationEngine {
	ce := &CorrelationEngine{
		config:             config,
		correlationRules:   make([]CorrelationRule, 0),
		crossProviderRules: make([]CrossProviderRule, 0),
	}
	
	ce.initializeBuiltInRules()
	return ce
}

func (ce *CorrelationEngine) initializeBuiltInRules() {
	// Same-provider correlation rules
	ce.correlationRules = append(ce.correlationRules, []CorrelationRule{
		{
			Name:        "EC2-EBS-Attachment",
			Description: "EC2 instances attached to EBS volumes",
			SourceType:  "aws:ec2:instance",
			TargetType:  "aws:ebs:volume",
			Matcher:     ce.matchEC2ToEBS,
			Confidence:  0.95,
		},
		{
			Name:        "LoadBalancer-Target",
			Description: "Load balancers and their target instances",
			SourceType:  "aws:elbv2:loadbalancer",
			TargetType:  "aws:ec2:instance",
			Matcher:     ce.matchLoadBalancerToTargets,
			Confidence:  0.9,
		},
		{
			Name:        "VPC-Subnet-Relationship",
			Description: "VPC to subnet relationships",
			SourceType:  "aws:vpc:vpc",
			TargetType:  "aws:vpc:subnet",
			Matcher:     ce.matchVPCToSubnet,
			Confidence:  0.98,
		},
		{
			Name:        "K8s-Pod-Service",
			Description: "Kubernetes pods and services relationship",
			SourceType:  "k8s:core:service",
			TargetType:  "k8s:core:pod",
			Matcher:     ce.matchK8sServiceToPod,
			Confidence:  0.92,
		},
		{
			Name:        "Azure-VM-Disk",
			Description: "Azure VMs and their attached disks",
			SourceType:  "azure:compute:virtualmachine",
			TargetType:  "azure:storage:disk",
			Matcher:     ce.matchAzureVMToDisk,
			Confidence:  0.95,
		},
		{
			Name:        "GCP-Instance-Disk",
			Description: "GCP instances and persistent disks",
			SourceType:  "gcp:compute:instance",
			TargetType:  "gcp:storage:disk",
			Matcher:     ce.matchGCPInstanceToDisk,
			Confidence:  0.95,
		},
	}...)

	// Cross-provider correlation rules
	ce.crossProviderRules = append(ce.crossProviderRules, []CrossProviderRule{
		{
			Name:          "Multi-Cloud-Database",
			Description:   "Databases across cloud providers with similar configurations",
			ProviderA:     "aws",
			ProviderB:     "azure",
			ResourceTypeA: "aws:rds:instance",
			ResourceTypeB: "azure:sql:database",
			Matcher:       ce.matchCrossCloudDatabases,
			Confidence:    0.75,
		},
		{
			Name:          "Cross-Cloud-Storage",
			Description:   "Storage buckets/containers across providers",
			ProviderA:     "aws",
			ProviderB:     "gcp",
			ResourceTypeA: "aws:s3:bucket",
			ResourceTypeB: "gcp:storage:bucket",
			Matcher:       ce.matchCrossCloudStorage,
			Confidence:    0.7,
		},
		{
			Name:          "Hybrid-Load-Balancing",
			Description:   "Load balancers that might be part of hybrid architecture",
			ProviderA:     "aws",
			ProviderB:     "azure",
			ResourceTypeA: "aws:elbv2:loadbalancer",
			ResourceTypeB: "azure:network:loadbalancer",
			Matcher:       ce.matchHybridLoadBalancers,
			Confidence:    0.6,
		},
	}...)
}

func (ce *CorrelationEngine) FindCorrelations(ctx context.Context, resources []models.Resource) ([]models.ResourceCorrelation, error) {
	var correlations []models.ResourceCorrelation
	
	// Same-provider correlations
	sameProviderCorr := ce.findSameProviderCorrelations(resources)
	correlations = append(correlations, sameProviderCorr...)
	
	// Cross-provider correlations (if enabled)
	if ce.config.EnableCrossProviderCorre {
		crossProviderCorr := ce.findCrossProviderCorrelations(resources)
		correlations = append(correlations, crossProviderCorr...)
	}
	
	return correlations, nil
}

func (ce *CorrelationEngine) findSameProviderCorrelations(resources []models.Resource) []models.ResourceCorrelation {
	var correlations []models.ResourceCorrelation
	
	for _, rule := range ce.correlationRules {
		sources := ce.filterResourcesByType(resources, rule.SourceType)
		targets := ce.filterResourcesByType(resources, rule.TargetType)
		
		for _, source := range sources {
			for _, target := range targets {
				if rule.Matcher(source, target) {
					correlation := models.ResourceCorrelation{
						ID:          fmt.Sprintf("%s-%s", source.ID, target.ID),
						SourceID:    source.ID,
						TargetID:    target.ID,
						Type:        rule.Name,
						Description: rule.Description,
						Confidence:  rule.Confidence,
						Metadata: map[string]interface{}{
							"rule":        rule.Name,
							"provider":    source.Provider,
							"source_type": rule.SourceType,
							"target_type": rule.TargetType,
						},
					}
					correlations = append(correlations, correlation)
				}
			}
		}
	}
	
	return correlations
}

func (ce *CorrelationEngine) findCrossProviderCorrelations(resources []models.Resource) []models.ResourceCorrelation {
	var correlations []models.ResourceCorrelation
	
	for _, rule := range ce.crossProviderRules {
		resourcesA := ce.filterResourcesByProviderAndType(resources, rule.ProviderA, rule.ResourceTypeA)
		resourcesB := ce.filterResourcesByProviderAndType(resources, rule.ProviderB, rule.ResourceTypeB)
		
		for _, resourceA := range resourcesA {
			for _, resourceB := range resourcesB {
				if rule.Matcher(resourceA, resourceB) {
					correlation := models.ResourceCorrelation{
						ID:          fmt.Sprintf("cross-%s-%s", resourceA.ID, resourceB.ID),
						SourceID:    resourceA.ID,
						TargetID:    resourceB.ID,
						Type:        rule.Name,
						Description: rule.Description,
						Confidence:  rule.Confidence,
						Metadata: map[string]interface{}{
							"rule":          rule.Name,
							"cross_provider": true,
							"provider_a":    rule.ProviderA,
							"provider_b":    rule.ProviderB,
							"type_a":        rule.ResourceTypeA,
							"type_b":        rule.ResourceTypeB,
						},
					}
					correlations = append(correlations, correlation)
				}
			}
		}
	}
	
	return correlations
}

func (ce *CorrelationEngine) filterResourcesByType(resources []models.Resource, resourceType string) []models.Resource {
	var filtered []models.Resource
	for _, resource := range resources {
		if resource.Type == resourceType {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func (ce *CorrelationEngine) filterResourcesByProviderAndType(resources []models.Resource, provider, resourceType string) []models.Resource {
	var filtered []models.Resource
	for _, resource := range resources {
		if resource.Provider == provider && resource.Type == resourceType {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

// Matcher functions for specific correlation rules

func (ce *CorrelationEngine) matchEC2ToEBS(source, target models.Resource) bool {
	// Check if EC2 instance has EBS volumes attached
	if instanceID, ok := source.Metadata["instance_id"].(string); ok {
		if attachments, ok := target.Metadata["attachments"].([]interface{}); ok {
			for _, attachment := range attachments {
				if attachMap, ok := attachment.(map[string]interface{}); ok {
					if attachedInstance, ok := attachMap["instance_id"].(string); ok {
						if attachedInstance == instanceID {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func (ce *CorrelationEngine) matchLoadBalancerToTargets(source, target models.Resource) bool {
	// Check if load balancer targets include the instance
	if targets, ok := source.Metadata["targets"].([]interface{}); ok {
		targetID := target.ID
		for _, target := range targets {
			if targetMap, ok := target.(map[string]interface{}); ok {
				if targetInstance, ok := targetMap["id"].(string); ok {
					if targetInstance == targetID {
						return true
					}
				}
			}
		}
	}
	return false
}

func (ce *CorrelationEngine) matchVPCToSubnet(source, target models.Resource) bool {
	// Check if subnet belongs to VPC
	if vpcID, ok := source.Metadata["vpc_id"].(string); ok {
		if subnetVPC, ok := target.Metadata["vpc_id"].(string); ok {
			return vpcID == subnetVPC
		}
	}
	return false
}

func (ce *CorrelationEngine) matchK8sServiceToPod(source, target models.Resource) bool {
	// Match Kubernetes service to pods via selectors
	if selectors, ok := source.Metadata["selectors"].(map[string]interface{}); ok {
		if podLabels, ok := target.Metadata["labels"].(map[string]interface{}); ok {
			for key, value := range selectors {
				if podValue, exists := podLabels[key]; exists {
					if podValue == value {
						return true
					}
				}
			}
		}
	}
	return false
}

func (ce *CorrelationEngine) matchAzureVMToDisk(source, target models.Resource) bool {
	// Check if Azure VM has the disk attached
	if vmID, ok := source.Metadata["vm_id"].(string); ok {
		if attachedVM, ok := target.Metadata["attached_vm"].(string); ok {
			return vmID == attachedVM
		}
	}
	return false
}

func (ce *CorrelationEngine) matchGCPInstanceToDisk(source, target models.Resource) bool {
	// Check if GCP instance has the persistent disk attached
	if instanceName, ok := source.Metadata["name"].(string); ok {
		if attachments, ok := target.Metadata["users"].([]interface{}); ok {
			for _, attachment := range attachments {
				if attachedInstance, ok := attachment.(string); ok {
					if strings.Contains(attachedInstance, instanceName) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (ce *CorrelationEngine) matchCrossCloudDatabases(resourceA, resourceB models.Resource) bool {
	// Match databases across cloud providers by name similarity and configuration
	nameA := ce.getResourceName(resourceA)
	nameB := ce.getResourceName(resourceB)
	
	// Check for similar naming patterns
	if ce.calculateNameSimilarity(nameA, nameB) > 0.7 {
		// Additional checks for database configuration similarity
		if ce.compareDatabaseConfigs(resourceA, resourceB) > 0.6 {
			return true
		}
	}
	return false
}

func (ce *CorrelationEngine) matchCrossCloudStorage(resourceA, resourceB models.Resource) bool {
	// Match storage across providers by naming and access patterns
	nameA := ce.getResourceName(resourceA)
	nameB := ce.getResourceName(resourceB)
	
	if ce.calculateNameSimilarity(nameA, nameB) > 0.8 {
		return true
	}
	return false
}

func (ce *CorrelationEngine) matchHybridLoadBalancers(resourceA, resourceB models.Resource) bool {
	// Match load balancers that might be part of hybrid architecture
	// Look for similar DNS names or IP ranges
	if dnsA, ok := resourceA.Metadata["dns_name"].(string); ok {
		if dnsB, ok := resourceB.Metadata["dns_name"].(string); ok {
			if ce.calculateNameSimilarity(dnsA, dnsB) > 0.6 {
				return true
			}
		}
	}
	return false
}

func (ce *CorrelationEngine) getResourceName(resource models.Resource) string {
	if name, ok := resource.Metadata["name"].(string); ok {
		return name
	}
	return resource.ID
}

func (ce *CorrelationEngine) calculateNameSimilarity(name1, name2 string) float64 {
	// Simple Levenshtein distance-based similarity
	// In production, you might want to use more sophisticated algorithms
	name1 = strings.ToLower(name1)
	name2 = strings.ToLower(name2)
	
	if name1 == name2 {
		return 1.0
	}
	
	// Simple substring matching for now
	if strings.Contains(name1, name2) || strings.Contains(name2, name1) {
		return 0.8
	}
	
	// Check for common prefixes/suffixes
	if len(name1) > 3 && len(name2) > 3 {
		if name1[:3] == name2[:3] {
			return 0.6
		}
	}
	
	return 0.0
}

func (ce *CorrelationEngine) compareDatabaseConfigs(resourceA, resourceB models.Resource) float64 {
	score := 0.0
	checks := 0
	
	// Compare engine types
	if engineA, okA := resourceA.Metadata["engine"].(string); okA {
		if engineB, okB := resourceB.Metadata["engine"].(string); okB {
			checks++
			if strings.EqualFold(engineA, engineB) {
				score += 1.0
			}
		}
	}
	
	// Compare instance sizes/tiers
	if sizeA, okA := resourceA.Metadata["instance_class"].(string); okA {
		if sizeB, okB := resourceB.Metadata["sku"].(string); okB {
			checks++
			if ce.compareInstanceSizes(sizeA, sizeB) {
				score += 0.8
			}
		}
	}
	
	if checks == 0 {
		return 0.0
	}
	
	return score / float64(checks)
}

func (ce *CorrelationEngine) compareInstanceSizes(sizeA, sizeB string) bool {
	// Simplified instance size comparison
	// In practice, you'd want a more sophisticated mapping
	sizeA = strings.ToLower(sizeA)
	sizeB = strings.ToLower(sizeB)
	
	// Look for similar size indicators
	if strings.Contains(sizeA, "small") && strings.Contains(sizeB, "small") {
		return true
	}
	if strings.Contains(sizeA, "medium") && strings.Contains(sizeB, "medium") {
		return true
	}
	if strings.Contains(sizeA, "large") && strings.Contains(sizeB, "large") {
		return true
	}
	
	return false
}
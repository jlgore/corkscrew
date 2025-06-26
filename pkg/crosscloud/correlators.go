package crosscloud

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
	"github.com/google/uuid"
)

// IPAddressCorrelator finds correlations based on IP address matches
type IPAddressCorrelator struct {
	confidenceThreshold float64
}

// DNSCorrelator finds correlations based on DNS name matches
type DNSCorrelator struct {
	confidenceThreshold float64
}

// NetworkTopologyCorrelator finds correlations based on network topology
type NetworkTopologyCorrelator struct {
	confidenceThreshold float64
}

// LoadBalancerCorrelator finds correlations based on load balancer relationships
type LoadBalancerCorrelator struct {
	confidenceThreshold float64
}

// NewIPAddressCorrelator creates a new IP address correlator
func NewIPAddressCorrelator(confidenceThreshold float64) *IPAddressCorrelator {
	return &IPAddressCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *IPAddressCorrelator) GetName() string {
	return "ip_address_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *IPAddressCorrelator) GetSupportedTypes() []string {
	return []string{"ip_match", "network_overlap", "elastic_ip_association"}
}

// FindCorrelations finds IP-based correlations between resources
func (c *IPAddressCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group resources by IP addresses
	ipResourceMap := make(map[string][]*models.Resource)
	
	for _, resource := range resources {
		for _, ip := range resource.IPAddresses {
			if ip.Address != "" {
				ipResourceMap[ip.Address] = append(ipResourceMap[ip.Address], resource)
			}
		}
	}
	
	// Find correlations for resources sharing IP addresses
	for ipAddress, resourcesWithIP := range ipResourceMap {
		if len(resourcesWithIP) < 2 {
			continue
		}
		
		// Create correlations between all pairs
		for i := 0; i < len(resourcesWithIP); i++ {
			for j := i + 1; j < len(resourcesWithIP); j++ {
				source := resourcesWithIP[i]
				target := resourcesWithIP[j]
				
				// Skip if same provider (not cross-cloud)
				if source.Provider == target.Provider {
					continue
				}
				
				confidence := c.calculateIPConfidence(source, target, ipAddress)
				if confidence < c.confidenceThreshold {
					continue
				}
				
				correlation := &CrossCloudCorrelation{
					ID:                 uuid.New().String(),
					SourceResourceID:   source.ID,
					TargetResourceID:   target.ID,
					SourceProvider:     source.Provider,
					TargetProvider:     target.Provider,
					CorrelationType:    "ip_match",
					CorrelationMethod:  "ip_address_analysis",
					ConfidenceScore:    confidence,
					Evidence: map[string]interface{}{
						"shared_ip_address": ipAddress,
						"ip_type":          c.getIPType(ipAddress),
					},
					MatchingAttributes: map[string]interface{}{
						"ip_addresses": []string{ipAddress},
					},
					Description: fmt.Sprintf("Resources share IP address %s", ipAddress),
					Status:      "active",
					DiscoveredAt: time.Now(),
				}
				
				correlations = append(correlations, correlation)
			}
		}
	}
	
	// Find network overlap correlations
	networkCorrelations := c.findNetworkOverlapCorrelations(resources)
	correlations = append(correlations, networkCorrelations...)
	
	return correlations, nil
}

// calculateIPConfidence calculates confidence score for IP-based correlation
func (c *IPAddressCorrelator) calculateIPConfidence(source, target *models.Resource, ipAddress string) float64 {
	confidence := 0.5 // Base confidence
	
	// Higher confidence for public IPs
	if c.isPublicIP(ipAddress) {
		confidence += 0.3
	}
	
	// Higher confidence if resources have same name patterns
	if c.hasSimilarNames(source.Name, target.Name) {
		confidence += 0.2
	}
	
	// Higher confidence for elastic/reserved IPs
	for _, ip := range source.IPAddresses {
		if ip.Address == ipAddress && (ip.Type == "elastic" || ip.Type == "reserved") {
			confidence += 0.2
			break
		}
	}
	
	return min(confidence, 1.0)
}

// findNetworkOverlapCorrelations finds correlations based on network CIDR overlap
func (c *IPAddressCorrelator) findNetworkOverlapCorrelations(resources []*models.Resource) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group resources by network interfaces
	for i := 0; i < len(resources); i++ {
		for j := i + 1; j < len(resources); j++ {
			source := resources[i]
			target := resources[j]
			
			// Skip if same provider
			if source.Provider == target.Provider {
				continue
			}
			
			overlap := c.findNetworkOverlap(source, target)
			if overlap != nil {
				confidence := c.calculateNetworkOverlapConfidence(overlap)
				if confidence >= c.confidenceThreshold {
					correlation := &CrossCloudCorrelation{
						ID:                 uuid.New().String(),
						SourceResourceID:   source.ID,
						TargetResourceID:   target.ID,
						SourceProvider:     source.Provider,
						TargetProvider:     target.Provider,
						CorrelationType:    "network_overlap",
						CorrelationMethod:  "network_cidr_analysis",
						ConfidenceScore:    confidence,
						Evidence:           overlap,
						Description:        "Resources have overlapping network ranges",
						Status:            "active",
						DiscoveredAt:      time.Now(),
					}
					
					correlations = append(correlations, correlation)
				}
			}
		}
	}
	
	return correlations
}

// NewDNSCorrelator creates a new DNS correlator
func NewDNSCorrelator(confidenceThreshold float64) *DNSCorrelator {
	return &DNSCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *DNSCorrelator) GetName() string {
	return "dns_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *DNSCorrelator) GetSupportedTypes() []string {
	return []string{"dns_match", "cname_chain", "dns_load_balancing"}
}

// FindCorrelations finds DNS-based correlations between resources
func (c *DNSCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group resources by DNS names
	dnsResourceMap := make(map[string][]*models.Resource)
	
	for _, resource := range resources {
		for _, dns := range resource.DNSNames {
			normalizedName := strings.ToLower(dns.Name)
			dnsResourceMap[normalizedName] = append(dnsResourceMap[normalizedName], resource)
		}
	}
	
	// Find correlations for resources sharing DNS names
	for dnsName, resourcesWithDNS := range dnsResourceMap {
		if len(resourcesWithDNS) < 2 {
			continue
		}
		
		// Create correlations between all pairs
		for i := 0; i < len(resourcesWithDNS); i++ {
			for j := i + 1; j < len(resourcesWithDNS); j++ {
				source := resourcesWithDNS[i]
				target := resourcesWithDNS[j]
				
				// Skip if same provider
				if source.Provider == target.Provider {
					continue
				}
				
				confidence := c.calculateDNSConfidence(source, target, dnsName)
				if confidence < c.confidenceThreshold {
					continue
				}
				
				correlation := &CrossCloudCorrelation{
					ID:                 uuid.New().String(),
					SourceResourceID:   source.ID,
					TargetResourceID:   target.ID,
					SourceProvider:     source.Provider,
					TargetProvider:     target.Provider,
					CorrelationType:    "dns_match",
					CorrelationMethod:  "dns_name_analysis",
					ConfidenceScore:    confidence,
					Evidence: map[string]interface{}{
						"shared_dns_name": dnsName,
						"dns_resolution":  c.resolveDNS(dnsName),
					},
					MatchingAttributes: map[string]interface{}{
						"dns_names": []string{dnsName},
					},
					Description: fmt.Sprintf("Resources share DNS name %s", dnsName),
					Status:      "active",
					DiscoveredAt: time.Now(),
				}
				
				correlations = append(correlations, correlation)
			}
		}
	}
	
	// Find CNAME chain correlations
	cnameCorrelations := c.findCNAMEChainCorrelations(resources)
	correlations = append(correlations, cnameCorrelations...)
	
	return correlations, nil
}

// calculateDNSConfidence calculates confidence score for DNS-based correlation
func (c *DNSCorrelator) calculateDNSConfidence(source, target *models.Resource, dnsName string) float64 {
	confidence := 0.6 // Base confidence for DNS matches
	
	// Higher confidence for load balancer DNS names
	if strings.Contains(dnsName, "elb") || strings.Contains(dnsName, "lb") || 
	   strings.Contains(dnsName, "trafficmanager") || strings.Contains(dnsName, "azurefd") {
		confidence += 0.2
	}
	
	// Higher confidence for custom domain names
	if !strings.Contains(dnsName, "amazonaws.com") && 
	   !strings.Contains(dnsName, "azure.com") && 
	   !strings.Contains(dnsName, "googleapis.com") {
		confidence += 0.2
	}
	
	return min(confidence, 1.0)
}

// findCNAMEChainCorrelations finds correlations based on CNAME chains
func (c *DNSCorrelator) findCNAMEChainCorrelations(resources []*models.Resource) []*CrossCloudCorrelation {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Build CNAME map
	cnameMap := make(map[string]string)
	for _, resource := range resources {
		for _, dns := range resource.DNSNames {
			if dns.Type == "CNAME" && len(dns.Values) > 0 {
				cnameMap[strings.ToLower(dns.Name)] = strings.ToLower(dns.Values[0])
			}
		}
	}
	
	// Find CNAME chains that cross cloud providers
	for _, resource := range resources {
		for _, dns := range resource.DNSNames {
			if target, exists := cnameMap[strings.ToLower(dns.Name)]; exists {
				// Find resources that match the CNAME target
				for _, targetResource := range resources {
					if targetResource.Provider == resource.Provider {
						continue
					}
					
					for _, targetDNS := range targetResource.DNSNames {
						if strings.ToLower(targetDNS.Name) == target {
							correlation := &CrossCloudCorrelation{
								ID:                 uuid.New().String(),
								SourceResourceID:   resource.ID,
								TargetResourceID:   targetResource.ID,
								SourceProvider:     resource.Provider,
								TargetProvider:     targetResource.Provider,
								CorrelationType:    "cname_chain",
								CorrelationMethod:  "cname_resolution",
								ConfidenceScore:    0.8,
								Evidence: map[string]interface{}{
									"cname_source": dns.Name,
									"cname_target": target,
								},
								Description: fmt.Sprintf("CNAME chain from %s to %s", dns.Name, target),
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

// Helper functions

func (c *IPAddressCorrelator) getIPType(ipAddress string) string {
	ip := net.ParseIP(ipAddress)
	if ip == nil {
		return "invalid"
	}
	
	if ip.IsPrivate() {
		return "private"
	} else if ip.IsLoopback() {
		return "loopback"
	} else if ip.IsMulticast() {
		return "multicast"
	}
	
	return "public"
}

func (c *IPAddressCorrelator) isPublicIP(ipAddress string) bool {
	ip := net.ParseIP(ipAddress)
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsMulticast()
}

func (c *IPAddressCorrelator) hasSimilarNames(name1, name2 string) bool {
	// Simple similarity check - could be enhanced with more sophisticated algorithms
	name1 = strings.ToLower(name1)
	name2 = strings.ToLower(name2)
	
	// Check for common prefixes or suffixes
	if len(name1) > 3 && len(name2) > 3 {
		if strings.HasPrefix(name1, name2[:3]) || strings.HasPrefix(name2, name1[:3]) {
			return true
		}
		if strings.HasSuffix(name1, name2[len(name2)-3:]) || strings.HasSuffix(name2, name1[len(name1)-3:]) {
			return true
		}
	}
	
	return false
}

func (c *IPAddressCorrelator) findNetworkOverlap(source, target *models.Resource) map[string]interface{} {
	// Check for overlapping network interfaces
	for _, sourceNI := range source.NetworkInterfaces {
		for _, targetNI := range target.NetworkInterfaces {
			// Check if IP addresses are in overlapping subnets
			for _, sourceIP := range sourceNI.IPAddresses {
				for _, targetIP := range targetNI.IPAddresses {
					if c.areIPsInSameSubnet(sourceIP.Address, targetIP.Address) {
						return map[string]interface{}{
							"source_ip":      sourceIP.Address,
							"target_ip":      targetIP.Address,
							"source_subnet":  sourceNI.SubnetID,
							"target_subnet":  targetNI.SubnetID,
							"overlap_type":   "subnet_overlap",
						}
					}
				}
			}
		}
	}
	
	return nil
}

func (c *IPAddressCorrelator) areIPsInSameSubnet(ip1, ip2 string) bool {
	// Simple check - could be enhanced with actual CIDR overlap calculation
	parsedIP1 := net.ParseIP(ip1)
	parsedIP2 := net.ParseIP(ip2)
	
	if parsedIP1 == nil || parsedIP2 == nil {
		return false
	}
	
	// Check if they're in common private subnets
	if parsedIP1.IsPrivate() && parsedIP2.IsPrivate() {
		// Simple /24 subnet check
		ip1Parts := strings.Split(ip1, ".")
		ip2Parts := strings.Split(ip2, ".")
		
		if len(ip1Parts) == 4 && len(ip2Parts) == 4 {
			return ip1Parts[0] == ip2Parts[0] && 
				   ip1Parts[1] == ip2Parts[1] && 
				   ip1Parts[2] == ip2Parts[2]
		}
	}
	
	return false
}

func (c *IPAddressCorrelator) calculateNetworkOverlapConfidence(overlap map[string]interface{}) float64 {
	// Base confidence for network overlap
	return 0.7
}

func (c *DNSCorrelator) resolveDNS(dnsName string) []string {
	// Simple DNS resolution - in production, this should be more robust
	ips, err := net.LookupHost(dnsName)
	if err != nil {
		return []string{}
	}
	return ips
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
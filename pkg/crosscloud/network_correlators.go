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

// VPNConnectionCorrelator detects VPN connections between cloud providers
type VPNConnectionCorrelator struct {
	confidenceThreshold float64
}

// NetworkPeeringCorrelator detects network peering relationships
type NetworkPeeringCorrelator struct {
	confidenceThreshold float64
}

// DirectConnectionCorrelator detects direct connections (AWS Direct Connect, Azure ExpressRoute, GCP Interconnect)
type DirectConnectionCorrelator struct {
	confidenceThreshold float64
}

// CrossCloudRoutingCorrelator analyzes routing relationships
type CrossCloudRoutingCorrelator struct {
	confidenceThreshold float64
}

// NewVPNConnectionCorrelator creates a new VPN connection correlator
func NewVPNConnectionCorrelator(confidenceThreshold float64) *VPNConnectionCorrelator {
	return &VPNConnectionCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *VPNConnectionCorrelator) GetName() string {
	return "vpn_connection_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *VPNConnectionCorrelator) GetSupportedTypes() []string {
	return []string{"vpn_connection", "site_to_site_vpn", "vpn_gateway_connection"}
}

// FindCorrelations finds VPN connection correlations between cloud providers
func (c *VPNConnectionCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group VPN-related resources by provider
	vpnResources := c.extractVPNResources(resources)
	
	// Find potential VPN connections between providers
	for provider1, vpnList1 := range vpnResources {
		for provider2, vpnList2 := range vpnResources {
			if provider1 >= provider2 { // Avoid duplicate comparisons
				continue
			}
			
			// Compare VPN resources between providers
			for _, vpn1 := range vpnList1 {
				for _, vpn2 := range vpnList2 {
					if correlation := c.analyzeVPNConnection(vpn1, vpn2); correlation != nil {
						correlations = append(correlations, correlation)
					}
				}
			}
		}
	}
	
	return correlations, nil
}

// extractVPNResources extracts VPN-related resources grouped by provider
func (c *VPNConnectionCorrelator) extractVPNResources(resources []*models.Resource) map[string][]*models.Resource {
	vpnResources := make(map[string][]*models.Resource)
	
	for _, resource := range resources {
		if c.isVPNResource(resource) {
			vpnResources[resource.Provider] = append(vpnResources[resource.Provider], resource)
		}
	}
	
	return vpnResources
}

// isVPNResource checks if a resource is VPN-related
func (c *VPNConnectionCorrelator) isVPNResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	resourceName := strings.ToLower(resource.Name)
	
	// Check for VPN-related resource types
	vpnTypes := []string{
		"vpn", "gateway", "connection", "tunnel",
		"aws::ec2::vpngateway", "aws::ec2::vpnconnection",
		"microsoft.network/vpngateways", "microsoft.network/connections",
		"google.compute.vpngateway", "google.compute.vpntunnel",
	}
	
	for _, vpnType := range vpnTypes {
		if strings.Contains(resourceType, vpnType) {
			return true
		}
	}
	
	// Check for VPN-related names
	vpnNames := []string{"vpn", "tunnel", "gateway", "site-to-site"}
	for _, vpnName := range vpnNames {
		if strings.Contains(resourceName, vpnName) {
			return true
		}
	}
	
	return false
}

// analyzeVPNConnection analyzes potential VPN connection between two resources
func (c *VPNConnectionCorrelator) analyzeVPNConnection(vpn1, vpn2 *models.Resource) *CrossCloudCorrelation {
	// Extract connection details
	details1 := c.extractVPNDetails(vpn1)
	details2 := c.extractVPNDetails(vpn2)
	
	// Check for matching connection parameters
	confidence := c.calculateVPNConfidence(details1, details2)
	if confidence < c.confidenceThreshold {
		return nil
	}
	
	return &CrossCloudCorrelation{
		ID:                 uuid.New().String(),
		SourceResourceID:   vpn1.ID,
		TargetResourceID:   vpn2.ID,
		SourceProvider:     vpn1.Provider,
		TargetProvider:     vpn2.Provider,
		CorrelationType:    "vpn_connection",
		CorrelationMethod:  "vpn_parameter_analysis",
		ConfidenceScore:    confidence,
		Evidence: map[string]interface{}{
			"vpn1_details": details1,
			"vpn2_details": details2,
			"connection_type": c.determineVPNConnectionType(details1, details2),
		},
		Description: fmt.Sprintf("VPN connection between %s and %s", vpn1.Name, vpn2.Name),
		Status:      "active",
		DiscoveredAt: time.Now(),
	}
}

// extractVPNDetails extracts VPN connection details from a resource
func (c *VPNConnectionCorrelator) extractVPNDetails(resource *models.Resource) map[string]interface{} {
	details := make(map[string]interface{})
	
	// Extract common VPN attributes
	if attributes, ok := resource.Attributes["vpn_config"]; ok {
		details["config"] = attributes
	}
	
	if attributes, ok := resource.Attributes["peer_ip"]; ok {
		details["peer_ip"] = attributes
	}
	
	if _, ok := resource.Attributes["shared_key"]; ok {
		details["has_shared_key"] = true // Don't store actual key
	}
	
	if attributes, ok := resource.Attributes["local_networks"]; ok {
		details["local_networks"] = attributes
	}
	
	if attributes, ok := resource.Attributes["remote_networks"]; ok {
		details["remote_networks"] = attributes
	}
	
	if attributes, ok := resource.Attributes["ike_version"]; ok {
		details["ike_version"] = attributes
	}
	
	// Extract IP addresses
	if len(resource.IPAddresses) > 0 {
		ips := make([]string, len(resource.IPAddresses))
		for i, ip := range resource.IPAddresses {
			ips[i] = ip.Address
		}
		details["ip_addresses"] = ips
	}
	
	return details
}

// calculateVPNConfidence calculates confidence score for VPN connection
func (c *VPNConnectionCorrelator) calculateVPNConfidence(details1, details2 map[string]interface{}) float64 {
	confidence := 0.0
	
	// Check for matching peer IPs
	if ips1, ok1 := details1["ip_addresses"].([]string); ok1 {
		if ips2, ok2 := details2["ip_addresses"].([]string); ok2 {
			if c.hasMatchingIPs(ips1, ips2) {
				confidence += 0.4
			}
		}
	}
	
	// Check for complementary network configurations
	if c.hasComplementaryNetworks(details1, details2) {
		confidence += 0.3
	}
	
	// Check for matching IKE versions
	if ike1, ok1 := details1["ike_version"]; ok1 {
		if ike2, ok2 := details2["ike_version"]; ok2 {
			if ike1 == ike2 {
				confidence += 0.2
			}
		}
	}
	
	// Check for shared key presence (both sides need to have it)
	if _, ok1 := details1["has_shared_key"]; ok1 {
		if _, ok2 := details2["has_shared_key"]; ok2 {
			confidence += 0.1
		}
	}
	
	return confidence
}

// hasMatchingIPs checks if two IP lists have matching entries
func (c *VPNConnectionCorrelator) hasMatchingIPs(ips1, ips2 []string) bool {
	for _, ip1 := range ips1 {
		for _, ip2 := range ips2 {
			if ip1 == ip2 {
				return true
			}
		}
	}
	return false
}

// hasComplementaryNetworks checks if networks are complementary (one's local = other's remote)
func (c *VPNConnectionCorrelator) hasComplementaryNetworks(details1, details2 map[string]interface{}) bool {
	local1, ok1l := details1["local_networks"]
	remote1, ok1r := details1["remote_networks"]
	local2, ok2l := details2["local_networks"]
	remote2, ok2r := details2["remote_networks"]
	
	if !ok1l || !ok1r || !ok2l || !ok2r {
		return false
	}
	
	// Check if local1 matches remote2 and local2 matches remote1
	return c.networksMatch(local1, remote2) && c.networksMatch(local2, remote1)
}

// networksMatch checks if two network configurations match
func (c *VPNConnectionCorrelator) networksMatch(net1, net2 interface{}) bool {
	// Simple string comparison - could be enhanced with CIDR matching
	return fmt.Sprintf("%v", net1) == fmt.Sprintf("%v", net2)
}

// determineVPNConnectionType determines the type of VPN connection
func (c *VPNConnectionCorrelator) determineVPNConnectionType(details1, details2 map[string]interface{}) string {
	// Analyze the details to determine connection type
	if c.hasComplementaryNetworks(details1, details2) {
		return "site_to_site_vpn"
	}
	return "vpn_gateway_connection"
}

// NewNetworkPeeringCorrelator creates a new network peering correlator
func NewNetworkPeeringCorrelator(confidenceThreshold float64) *NetworkPeeringCorrelator {
	return &NetworkPeeringCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *NetworkPeeringCorrelator) GetName() string {
	return "network_peering_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *NetworkPeeringCorrelator) GetSupportedTypes() []string {
	return []string{"network_peering", "vpc_peering", "vnet_peering", "cross_cloud_peering"}
}

// FindCorrelations finds network peering correlations
func (c *NetworkPeeringCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group peering-related resources by provider
	peeringResources := c.extractPeeringResources(resources)
	
	// Find potential peering connections between providers
	for provider1, peeringList1 := range peeringResources {
		for provider2, peeringList2 := range peeringResources {
			if provider1 >= provider2 {
				continue
			}
			
			for _, peer1 := range peeringList1 {
				for _, peer2 := range peeringList2 {
					if correlation := c.analyzePeeringConnection(peer1, peer2); correlation != nil {
						correlations = append(correlations, correlation)
					}
				}
			}
		}
	}
	
	return correlations, nil
}

// extractPeeringResources extracts peering-related resources
func (c *NetworkPeeringCorrelator) extractPeeringResources(resources []*models.Resource) map[string][]*models.Resource {
	peeringResources := make(map[string][]*models.Resource)
	
	for _, resource := range resources {
		if c.isPeeringResource(resource) {
			peeringResources[resource.Provider] = append(peeringResources[resource.Provider], resource)
		}
	}
	
	return peeringResources
}

// isPeeringResource checks if a resource is peering-related
func (c *NetworkPeeringCorrelator) isPeeringResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	
	peeringTypes := []string{
		"peering", "peer",
		"aws::ec2::vpcpeeringconnection",
		"microsoft.network/virtualnetworkpeerings",
		"google.compute.networkpeering",
	}
	
	for _, peeringType := range peeringTypes {
		if strings.Contains(resourceType, peeringType) {
			return true
		}
	}
	
	return false
}

// analyzePeeringConnection analyzes potential peering connection
func (c *NetworkPeeringCorrelator) analyzePeeringConnection(peer1, peer2 *models.Resource) *CrossCloudCorrelation {
	details1 := c.extractPeeringDetails(peer1)
	details2 := c.extractPeeringDetails(peer2)
	
	confidence := c.calculatePeeringConfidence(details1, details2)
	if confidence < c.confidenceThreshold {
		return nil
	}
	
	return &CrossCloudCorrelation{
		ID:                 uuid.New().String(),
		SourceResourceID:   peer1.ID,
		TargetResourceID:   peer2.ID,
		SourceProvider:     peer1.Provider,
		TargetProvider:     peer2.Provider,
		CorrelationType:    "network_peering",
		CorrelationMethod:  "peering_analysis",
		ConfidenceScore:    confidence,
		Evidence: map[string]interface{}{
			"peer1_details": details1,
			"peer2_details": details2,
		},
		Description: fmt.Sprintf("Network peering between %s and %s", peer1.Name, peer2.Name),
		Status:      "active",
		DiscoveredAt: time.Now(),
	}
}

// extractPeeringDetails extracts peering connection details
func (c *NetworkPeeringCorrelator) extractPeeringDetails(resource *models.Resource) map[string]interface{} {
	details := make(map[string]interface{})
	
	if vpc, ok := resource.Attributes["vpc_id"]; ok {
		details["vpc_id"] = vpc
	}
	
	if peer, ok := resource.Attributes["peer_vpc_id"]; ok {
		details["peer_vpc_id"] = peer
	}
	
	if cidr, ok := resource.Attributes["cidr_block"]; ok {
		details["cidr_block"] = cidr
	}
	
	if state, ok := resource.Attributes["state"]; ok {
		details["state"] = state
	}
	
	return details
}

// calculatePeeringConfidence calculates confidence for peering connection
func (c *NetworkPeeringCorrelator) calculatePeeringConfidence(details1, details2 map[string]interface{}) float64 {
	confidence := 0.0
	
	// Check for complementary peering relationship
	if vpc1, ok1 := details1["vpc_id"]; ok1 {
		if peer2, ok2 := details2["peer_vpc_id"]; ok2 {
			if vpc1 == peer2 {
				confidence += 0.4
			}
		}
	}
	
	if vpc2, ok1 := details2["vpc_id"]; ok1 {
		if peer1, ok2 := details1["peer_vpc_id"]; ok2 {
			if vpc2 == peer1 {
				confidence += 0.4
			}
		}
	}
	
	// Check for active state
	if state1, ok1 := details1["state"]; ok1 {
		if state2, ok2 := details2["state"]; ok2 {
			if state1 == "active" && state2 == "active" {
				confidence += 0.2
			}
		}
	}
	
	return confidence
}

// NewDirectConnectionCorrelator creates a new direct connection correlator
func NewDirectConnectionCorrelator(confidenceThreshold float64) *DirectConnectionCorrelator {
	return &DirectConnectionCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *DirectConnectionCorrelator) GetName() string {
	return "direct_connection_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *DirectConnectionCorrelator) GetSupportedTypes() []string {
	return []string{"direct_connection", "dedicated_connection", "expressroute", "interconnect"}
}

// FindCorrelations finds direct connection correlations
func (c *DirectConnectionCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group direct connection resources by provider
	directResources := c.extractDirectConnectionResources(resources)
	
	// Find potential direct connections between providers
	for provider1, directList1 := range directResources {
		for provider2, directList2 := range directResources {
			if provider1 >= provider2 {
				continue
			}
			
			for _, direct1 := range directList1 {
				for _, direct2 := range directList2 {
					if correlation := c.analyzeDirectConnection(direct1, direct2); correlation != nil {
						correlations = append(correlations, correlation)
					}
				}
			}
		}
	}
	
	return correlations, nil
}

// extractDirectConnectionResources extracts direct connection resources
func (c *DirectConnectionCorrelator) extractDirectConnectionResources(resources []*models.Resource) map[string][]*models.Resource {
	directResources := make(map[string][]*models.Resource)
	
	for _, resource := range resources {
		if c.isDirectConnectionResource(resource) {
			directResources[resource.Provider] = append(directResources[resource.Provider], resource)
		}
	}
	
	return directResources
}

// isDirectConnectionResource checks if a resource is a direct connection
func (c *DirectConnectionCorrelator) isDirectConnectionResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	
	directTypes := []string{
		"directconnect", "expressroute", "interconnect",
		"aws::directconnect::connection",
		"microsoft.network/expressroutecircuits",
		"google.compute.interconnectattachment",
	}
	
	for _, directType := range directTypes {
		if strings.Contains(resourceType, directType) {
			return true
		}
	}
	
	return false
}

// analyzeDirectConnection analyzes potential direct connection
func (c *DirectConnectionCorrelator) analyzeDirectConnection(direct1, direct2 *models.Resource) *CrossCloudCorrelation {
	details1 := c.extractDirectConnectionDetails(direct1)
	details2 := c.extractDirectConnectionDetails(direct2)
	
	confidence := c.calculateDirectConnectionConfidence(details1, details2)
	if confidence < c.confidenceThreshold {
		return nil
	}
	
	return &CrossCloudCorrelation{
		ID:                 uuid.New().String(),
		SourceResourceID:   direct1.ID,
		TargetResourceID:   direct2.ID,
		SourceProvider:     direct1.Provider,
		TargetProvider:     direct2.Provider,
		CorrelationType:    "direct_connection",
		CorrelationMethod:  "direct_connection_analysis",
		ConfidenceScore:    confidence,
		Evidence: map[string]interface{}{
			"direct1_details": details1,
			"direct2_details": details2,
		},
		Description: fmt.Sprintf("Direct connection between %s and %s", direct1.Name, direct2.Name),
		Status:      "active",
		DiscoveredAt: time.Now(),
	}
}

// extractDirectConnectionDetails extracts direct connection details
func (c *DirectConnectionCorrelator) extractDirectConnectionDetails(resource *models.Resource) map[string]interface{} {
	details := make(map[string]interface{})
	
	if location, ok := resource.Attributes["location"]; ok {
		details["location"] = location
	}
	
	if bandwidth, ok := resource.Attributes["bandwidth"]; ok {
		details["bandwidth"] = bandwidth
	}
	
	if provider, ok := resource.Attributes["connection_provider"]; ok {
		details["connection_provider"] = provider
	}
	
	if vlan, ok := resource.Attributes["vlan"]; ok {
		details["vlan"] = vlan
	}
	
	return details
}

// calculateDirectConnectionConfidence calculates confidence for direct connection
func (c *DirectConnectionCorrelator) calculateDirectConnectionConfidence(details1, details2 map[string]interface{}) float64 {
	confidence := 0.0
	
	// Check for same connection provider
	if provider1, ok1 := details1["connection_provider"]; ok1 {
		if provider2, ok2 := details2["connection_provider"]; ok2 {
			if provider1 == provider2 {
				confidence += 0.3
			}
		}
	}
	
	// Check for same location
	if location1, ok1 := details1["location"]; ok1 {
		if location2, ok2 := details2["location"]; ok2 {
			if location1 == location2 {
				confidence += 0.3
			}
		}
	}
	
	// Check for matching bandwidth
	if bandwidth1, ok1 := details1["bandwidth"]; ok1 {
		if bandwidth2, ok2 := details2["bandwidth"]; ok2 {
			if bandwidth1 == bandwidth2 {
				confidence += 0.2
			}
		}
	}
	
	// Check for complementary VLANs
	if vlan1, ok1 := details1["vlan"]; ok1 {
		if vlan2, ok2 := details2["vlan"]; ok2 {
			if vlan1 != vlan2 { // Different VLANs might indicate separate circuits
				confidence += 0.2
			}
		}
	}
	
	return confidence
}

// NewCrossCloudRoutingCorrelator creates a new routing correlator
func NewCrossCloudRoutingCorrelator(confidenceThreshold float64) *CrossCloudRoutingCorrelator {
	return &CrossCloudRoutingCorrelator{
		confidenceThreshold: confidenceThreshold,
	}
}

// GetName returns the correlator name
func (c *CrossCloudRoutingCorrelator) GetName() string {
	return "cross_cloud_routing_correlator"
}

// GetSupportedTypes returns supported correlation types
func (c *CrossCloudRoutingCorrelator) GetSupportedTypes() []string {
	return []string{"routing_relationship", "route_propagation", "cross_cloud_routing"}
}

// FindCorrelations finds routing correlations across clouds
func (c *CrossCloudRoutingCorrelator) FindCorrelations(ctx context.Context, resources []*models.Resource) ([]*CrossCloudCorrelation, error) {
	correlations := make([]*CrossCloudCorrelation, 0)
	
	// Group routing resources by provider
	routingResources := c.extractRoutingResources(resources)
	
	// Analyze routing relationships between providers
	for provider1, routeList1 := range routingResources {
		for provider2, routeList2 := range routingResources {
			if provider1 >= provider2 {
				continue
			}
			
			for _, route1 := range routeList1 {
				for _, route2 := range routeList2 {
					if correlation := c.analyzeRoutingRelationship(route1, route2); correlation != nil {
						correlations = append(correlations, correlation)
					}
				}
			}
		}
	}
	
	return correlations, nil
}

// extractRoutingResources extracts routing-related resources
func (c *CrossCloudRoutingCorrelator) extractRoutingResources(resources []*models.Resource) map[string][]*models.Resource {
	routingResources := make(map[string][]*models.Resource)
	
	for _, resource := range resources {
		if c.isRoutingResource(resource) {
			routingResources[resource.Provider] = append(routingResources[resource.Provider], resource)
		}
	}
	
	return routingResources
}

// isRoutingResource checks if a resource is routing-related
func (c *CrossCloudRoutingCorrelator) isRoutingResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	
	routingTypes := []string{
		"route", "routing", "routetable",
		"aws::ec2::routetable", "aws::ec2::route",
		"microsoft.network/routetables",
		"google.compute.route",
	}
	
	for _, routingType := range routingTypes {
		if strings.Contains(resourceType, routingType) {
			return true
		}
	}
	
	return false
}

// analyzeRoutingRelationship analyzes routing relationships
func (c *CrossCloudRoutingCorrelator) analyzeRoutingRelationship(route1, route2 *models.Resource) *CrossCloudCorrelation {
	details1 := c.extractRoutingDetails(route1)
	details2 := c.extractRoutingDetails(route2)
	
	confidence := c.calculateRoutingConfidence(details1, details2)
	if confidence < c.confidenceThreshold {
		return nil
	}
	
	return &CrossCloudCorrelation{
		ID:                 uuid.New().String(),
		SourceResourceID:   route1.ID,
		TargetResourceID:   route2.ID,
		SourceProvider:     route1.Provider,
		TargetProvider:     route2.Provider,
		CorrelationType:    "routing_relationship",
		CorrelationMethod:  "routing_analysis",
		ConfidenceScore:    confidence,
		Evidence: map[string]interface{}{
			"route1_details": details1,
			"route2_details": details2,
		},
		Description: fmt.Sprintf("Routing relationship between %s and %s", route1.Name, route2.Name),
		Status:      "active",
		DiscoveredAt: time.Now(),
	}
}

// extractRoutingDetails extracts routing details from resource
func (c *CrossCloudRoutingCorrelator) extractRoutingDetails(resource *models.Resource) map[string]interface{} {
	details := make(map[string]interface{})
	
	if destination, ok := resource.Attributes["destination_cidr"]; ok {
		details["destination_cidr"] = destination
	}
	
	if target, ok := resource.Attributes["target"]; ok {
		details["target"] = target
	}
	
	if gateway, ok := resource.Attributes["gateway_id"]; ok {
		details["gateway_id"] = gateway
	}
	
	if state, ok := resource.Attributes["state"]; ok {
		details["state"] = state
	}
	
	return details
}

// calculateRoutingConfidence calculates confidence for routing relationships
func (c *CrossCloudRoutingCorrelator) calculateRoutingConfidence(details1, details2 map[string]interface{}) float64 {
	confidence := 0.0
	
	// Check for overlapping destination CIDRs
	if cidr1, ok1 := details1["destination_cidr"]; ok1 {
		if cidr2, ok2 := details2["destination_cidr"]; ok2 {
			if c.cidrsOverlap(fmt.Sprintf("%v", cidr1), fmt.Sprintf("%v", cidr2)) {
				confidence += 0.4
			}
		}
	}
	
	// Check for routing to same target types
	if target1, ok1 := details1["target"]; ok1 {
		if target2, ok2 := details2["target"]; ok2 {
			if c.targetsRelated(fmt.Sprintf("%v", target1), fmt.Sprintf("%v", target2)) {
				confidence += 0.3
			}
		}
	}
	
	// Check for active routing states
	if state1, ok1 := details1["state"]; ok1 {
		if state2, ok2 := details2["state"]; ok2 {
			if state1 == "active" && state2 == "active" {
				confidence += 0.3
			}
		}
	}
	
	return confidence
}

// cidrsOverlap checks if two CIDR blocks overlap
func (c *CrossCloudRoutingCorrelator) cidrsOverlap(cidr1, cidr2 string) bool {
	_, net1, err1 := net.ParseCIDR(cidr1)
	_, net2, err2 := net.ParseCIDR(cidr2)
	
	if err1 != nil || err2 != nil {
		return false
	}
	
	// Check if networks overlap
	return net1.Contains(net2.IP) || net2.Contains(net1.IP)
}

// targetsRelated checks if routing targets are related
func (c *CrossCloudRoutingCorrelator) targetsRelated(target1, target2 string) bool {
	// Simple check for related target types
	target1 = strings.ToLower(target1)
	target2 = strings.ToLower(target2)
	
	relatedTypes := [][]string{
		{"vpn", "gateway"},
		{"peering", "peer"},
		{"directconnect", "expressroute", "interconnect"},
	}
	
	for _, group := range relatedTypes {
		inGroup1, inGroup2 := false, false
		for _, t := range group {
			if strings.Contains(target1, t) {
				inGroup1 = true
			}
			if strings.Contains(target2, t) {
				inGroup2 = true
			}
		}
		if inGroup1 && inGroup2 {
			return true
		}
	}
	
	return false
}
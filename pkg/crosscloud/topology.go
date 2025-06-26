package crosscloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/pkg/models"
	"github.com/google/uuid"
)

// TopologyVisualizer creates network topology visualizations
type TopologyVisualizer struct {
	renderer TopologyRenderer
}

// TopologyRenderer interface for different visualization outputs
type TopologyRenderer interface {
	RenderTopology(topology *NetworkTopology) (string, error)
	RenderCorrelations(correlations []*CrossCloudCorrelation) (string, error)
	GetFormat() string
}

// NetworkTopology represents a cross-cloud network topology
type NetworkTopology struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Providers   []string                 `json:"providers"`
	Nodes       []*TopologyNode          `json:"nodes"`
	Edges       []*TopologyEdge          `json:"edges"`
	Clusters    []*TopologyCluster       `json:"clusters"`
	Metadata    map[string]interface{}   `json:"metadata"`
	CreatedAt   time.Time                `json:"created_at"`
}

// TopologyNode represents a node in the topology
type TopologyNode struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Provider    string                 `json:"provider"`
	Region      string                 `json:"region"`
	NodeClass   string                 `json:"node_class"`   // network, compute, storage, security
	Position    *Position              `json:"position"`
	Properties  map[string]interface{} `json:"properties"`
	Resource    *models.Resource       `json:"resource"`
}

// TopologyEdge represents a connection between nodes
type TopologyEdge struct {
	ID         string                 `json:"id"`
	SourceID   string                 `json:"source_id"`
	TargetID   string                 `json:"target_id"`
	EdgeType   string                 `json:"edge_type"`
	Direction  string                 `json:"direction"`  // bidirectional, unidirectional
	Protocol   string                 `json:"protocol"`
	Bandwidth  string                 `json:"bandwidth"`
	Status     string                 `json:"status"`
	Properties map[string]interface{} `json:"properties"`
}

// TopologyCluster represents a logical grouping of nodes
type TopologyCluster struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Provider    string                 `json:"provider"`
	Region      string                 `json:"region"`
	ClusterType string                 `json:"cluster_type"` // vpc, vnet, project
	NodeIDs     []string               `json:"node_ids"`
	Properties  map[string]interface{} `json:"properties"`
}

// Position represents a 2D position for layout
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ASCIITopologyRenderer renders topology using ASCII art
type ASCIITopologyRenderer struct {
	width  int
	height int
}

// DOTTopologyRenderer renders topology in Graphviz DOT format
type DOTTopologyRenderer struct {
	includeAttributes bool
}

// JSONTopologyRenderer renders topology in JSON format
type JSONTopologyRenderer struct {
	prettyPrint bool
}

// MermaidTopologyRenderer renders topology in Mermaid format
type MermaidTopologyRenderer struct {
	direction string // TB, LR, etc.
}

// NewTopologyVisualizer creates a new topology visualizer
func NewTopologyVisualizer(renderer TopologyRenderer) *TopologyVisualizer {
	return &TopologyVisualizer{
		renderer: renderer,
	}
}

// BuildNetworkTopology builds network topology from resources and correlations
func (v *TopologyVisualizer) BuildNetworkTopology(ctx context.Context, resources []*models.Resource, correlations []*CrossCloudCorrelation) (*NetworkTopology, error) {
	topology := &NetworkTopology{
		ID:          uuid.New().String(),
		Name:        "Cross-Cloud Network Topology",
		Description: "Network topology visualization across cloud providers",
		Providers:   make([]string, 0),
		Nodes:       make([]*TopologyNode, 0),
		Edges:       make([]*TopologyEdge, 0),
		Clusters:    make([]*TopologyCluster, 0),
		Metadata:    make(map[string]interface{}),
		CreatedAt:   time.Now(),
	}

	// Build nodes from network-related resources
	networkResources := v.filterNetworkResources(resources)
	providerSet := make(map[string]bool)
	
	for _, resource := range networkResources {
		node := v.createTopologyNode(resource)
		topology.Nodes = append(topology.Nodes, node)
		providerSet[resource.Provider] = true
	}
	
	// Extract provider list
	for provider := range providerSet {
		topology.Providers = append(topology.Providers, provider)
	}
	
	// Build edges from correlations
	for _, correlation := range correlations {
		if v.isNetworkCorrelation(correlation) {
			edge := v.createTopologyEdge(correlation)
			topology.Edges = append(topology.Edges, edge)
		}
	}
	
	// Build clusters (group by VPC/VNet/Project)
	clusters := v.buildClusters(networkResources)
	topology.Clusters = clusters
	
	// Calculate layout positions
	v.calculateLayout(topology)
	
	// Add metadata
	topology.Metadata["node_count"] = len(topology.Nodes)
	topology.Metadata["edge_count"] = len(topology.Edges)
	topology.Metadata["cluster_count"] = len(topology.Clusters)
	topology.Metadata["provider_count"] = len(topology.Providers)
	
	return topology, nil
}

// RenderTopology renders the topology using the configured renderer
func (v *TopologyVisualizer) RenderTopology(topology *NetworkTopology) (string, error) {
	return v.renderer.RenderTopology(topology)
}

// RenderCorrelations renders correlations using the configured renderer
func (v *TopologyVisualizer) RenderCorrelations(correlations []*CrossCloudCorrelation) (string, error) {
	return v.renderer.RenderCorrelations(correlations)
}

// filterNetworkResources filters resources to network-related ones
func (v *TopologyVisualizer) filterNetworkResources(resources []*models.Resource) []*models.Resource {
	networkResources := make([]*models.Resource, 0)
	
	for _, resource := range resources {
		if v.isNetworkResource(resource) {
			networkResources = append(networkResources, resource)
		}
	}
	
	return networkResources
}

// isNetworkResource checks if a resource is network-related
func (v *TopologyVisualizer) isNetworkResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	
	networkTypes := []string{
		"vpc", "vnet", "network", "subnet", "securitygroup", "firewall",
		"loadbalancer", "gateway", "vpn", "peering", "route", "dns",
		"trafficmanager", "frontdoor", "interconnect", "directconnect",
		"expressroute", "natgateway", "internetgateway",
	}
	
	for _, netType := range networkTypes {
		if strings.Contains(resourceType, netType) {
			return true
		}
	}
	
	return false
}

// isNetworkCorrelation checks if a correlation is network-related
func (v *TopologyVisualizer) isNetworkCorrelation(correlation *CrossCloudCorrelation) bool {
	networkCorrelationTypes := []string{
		"vpn_connection", "network_peering", "direct_connection",
		"dns_load_balancing", "backend_pool_correlation", "routing_relationship",
		"security_rule_overlap", "ip_match", "network_overlap",
	}
	
	for _, netType := range networkCorrelationTypes {
		if correlation.CorrelationType == netType {
			return true
		}
	}
	
	return false
}

// createTopologyNode creates a topology node from a resource
func (v *TopologyVisualizer) createTopologyNode(resource *models.Resource) *TopologyNode {
	node := &TopologyNode{
		ID:         resource.ID,
		Name:       resource.Name,
		Type:       resource.Type,
		Provider:   resource.Provider,
		Region:     resource.Region,
		NodeClass:  v.determineNodeClass(resource),
		Properties: make(map[string]interface{}),
		Resource:   resource,
	}
	
	// Extract relevant properties
	if len(resource.IPAddresses) > 0 {
		ips := make([]string, len(resource.IPAddresses))
		for i, ip := range resource.IPAddresses {
			ips[i] = ip.Address
		}
		node.Properties["ip_addresses"] = ips
	}
	
	if len(resource.DNSNames) > 0 {
		dnsNames := make([]string, len(resource.DNSNames))
		for i, dns := range resource.DNSNames {
			dnsNames[i] = dns.Name
		}
		node.Properties["dns_names"] = dnsNames
	}
	
	// Add provider-specific properties
	if cidr, ok := resource.Attributes["cidr_block"]; ok {
		node.Properties["cidr_block"] = cidr
	}
	
	if state, ok := resource.Attributes["state"]; ok {
		node.Properties["state"] = state
	}
	
	return node
}

// createTopologyEdge creates a topology edge from a correlation
func (v *TopologyVisualizer) createTopologyEdge(correlation *CrossCloudCorrelation) *TopologyEdge {
	edge := &TopologyEdge{
		ID:         correlation.ID,
		SourceID:   correlation.SourceResourceID,
		TargetID:   correlation.TargetResourceID,
		EdgeType:   correlation.CorrelationType,
		Direction:  v.determineEdgeDirection(correlation),
		Protocol:   v.extractProtocol(correlation),
		Status:     correlation.Status,
		Properties: make(map[string]interface{}),
	}
	
	// Add correlation evidence as properties
	edge.Properties["confidence_score"] = correlation.ConfidenceScore
	edge.Properties["correlation_method"] = correlation.CorrelationMethod
	edge.Properties["evidence"] = correlation.Evidence
	
	return edge
}

// buildClusters builds topology clusters from resources
func (v *TopologyVisualizer) buildClusters(resources []*models.Resource) []*TopologyCluster {
	clusters := make([]*TopologyCluster, 0)
	
	// Group by VPC/VNet/Project
	clusterGroups := make(map[string][]*models.Resource)
	
	for _, resource := range resources {
		clusterKey := v.getClusterKey(resource)
		if clusterKey != "" {
			clusterGroups[clusterKey] = append(clusterGroups[clusterKey], resource)
		}
	}
	
	// Create clusters
	for clusterKey, clusterResources := range clusterGroups {
		if len(clusterResources) > 0 {
			cluster := &TopologyCluster{
				ID:          uuid.New().String(),
				Name:        clusterKey,
				Provider:    clusterResources[0].Provider,
				Region:      clusterResources[0].Region,
				ClusterType: v.determineClusterType(clusterResources[0]),
				NodeIDs:     make([]string, len(clusterResources)),
				Properties:  make(map[string]interface{}),
			}
			
			for i, resource := range clusterResources {
				cluster.NodeIDs[i] = resource.ID
			}
			
			clusters = append(clusters, cluster)
		}
	}
	
	return clusters
}

// calculateLayout calculates positions for nodes in the topology
func (v *TopologyVisualizer) calculateLayout(topology *NetworkTopology) {
	// Simple layout algorithm - could be enhanced with more sophisticated algorithms
	
	// Group nodes by provider
	providerNodes := make(map[string][]*TopologyNode)
	for _, node := range topology.Nodes {
		providerNodes[node.Provider] = append(providerNodes[node.Provider], node)
	}
	
	// Position providers horizontally
	providerSpacing := 200.0
	currentX := 0.0
	
	for _, provider := range topology.Providers {
		nodes := providerNodes[provider]
		
		// Position nodes in this provider vertically
		nodeSpacing := 100.0
		currentY := 0.0
		
		for _, node := range nodes {
			node.Position = &Position{
				X: currentX,
				Y: currentY,
			}
			currentY += nodeSpacing
		}
		
		currentX += providerSpacing
	}
}

// Helper methods

func (v *TopologyVisualizer) determineNodeClass(resource *models.Resource) string {
	resourceType := strings.ToLower(resource.Type)
	
	if strings.Contains(resourceType, "vpc") || strings.Contains(resourceType, "vnet") ||
		strings.Contains(resourceType, "network") || strings.Contains(resourceType, "subnet") {
		return "network"
	}
	
	if strings.Contains(resourceType, "securitygroup") || strings.Contains(resourceType, "firewall") ||
		strings.Contains(resourceType, "acl") {
		return "security"
	}
	
	if strings.Contains(resourceType, "loadbalancer") || strings.Contains(resourceType, "gateway") {
		return "load_balancer"
	}
	
	if strings.Contains(resourceType, "compute") || strings.Contains(resourceType, "instance") ||
		strings.Contains(resourceType, "vm") {
		return "compute"
	}
	
	if strings.Contains(resourceType, "storage") || strings.Contains(resourceType, "disk") ||
		strings.Contains(resourceType, "volume") {
		return "storage"
	}
	
	return "other"
}

func (v *TopologyVisualizer) determineEdgeDirection(correlation *CrossCloudCorrelation) string {
	switch correlation.CorrelationType {
	case "vpn_connection", "network_peering", "direct_connection":
		return "bidirectional"
	case "dns_load_balancing", "backend_pool_correlation":
		return "unidirectional"
	default:
		return "bidirectional"
	}
}

func (v *TopologyVisualizer) extractProtocol(correlation *CrossCloudCorrelation) string {
	if evidence, ok := correlation.Evidence["protocol"]; ok {
		return fmt.Sprintf("%v", evidence)
	}
	
	switch correlation.CorrelationType {
	case "vpn_connection":
		return "IPSec"
	case "network_peering":
		return "Private"
	case "direct_connection":
		return "Dedicated"
	case "dns_load_balancing":
		return "DNS"
	default:
		return "Unknown"
	}
}

func (v *TopologyVisualizer) getClusterKey(resource *models.Resource) string {
	// Try to extract VPC/VNet/Project identifier
	if vpcId, ok := resource.Attributes["vpc_id"]; ok {
		return fmt.Sprintf("%s:%s", resource.Provider, vpcId)
	}
	
	if vnetId, ok := resource.Attributes["vnet_id"]; ok {
		return fmt.Sprintf("%s:%s", resource.Provider, vnetId)
	}
	
	if networkId, ok := resource.Attributes["network_id"]; ok {
		return fmt.Sprintf("%s:%s", resource.Provider, networkId)
	}
	
	if projectId, ok := resource.Attributes["project_id"]; ok {
		return fmt.Sprintf("%s:%s", resource.Provider, projectId)
	}
	
	// Fallback to provider:region
	return fmt.Sprintf("%s:%s", resource.Provider, resource.Region)
}

func (v *TopologyVisualizer) determineClusterType(resource *models.Resource) string {
	switch resource.Provider {
	case "aws":
		return "vpc"
	case "azure":
		return "vnet"
	case "gcp":
		return "network"
	default:
		return "group"
	}
}

// Renderer implementations

// NewASCIITopologyRenderer creates a new ASCII topology renderer
func NewASCIITopologyRenderer(width, height int) *ASCIITopologyRenderer {
	return &ASCIITopologyRenderer{
		width:  width,
		height: height,
	}
}

func (r *ASCIITopologyRenderer) GetFormat() string {
	return "ascii"
}

func (r *ASCIITopologyRenderer) RenderTopology(topology *NetworkTopology) (string, error) {
	var output strings.Builder
	
	// Header
	output.WriteString("Cross-Cloud Network Topology\n")
	output.WriteString(strings.Repeat("=", 50) + "\n\n")
	
	// Summary
	output.WriteString(fmt.Sprintf("Providers: %s\n", strings.Join(topology.Providers, ", ")))
	output.WriteString(fmt.Sprintf("Nodes: %d, Edges: %d, Clusters: %d\n\n", 
		len(topology.Nodes), len(topology.Edges), len(topology.Clusters)))
	
	// Clusters
	if len(topology.Clusters) > 0 {
		output.WriteString("Clusters:\n")
		for _, cluster := range topology.Clusters {
			output.WriteString(fmt.Sprintf("  ┌─ %s (%s)\n", cluster.Name, cluster.Provider))
			for _, nodeID := range cluster.NodeIDs {
				if node := r.findNode(topology.Nodes, nodeID); node != nil {
					output.WriteString(fmt.Sprintf("  │  • %s [%s]\n", node.Name, node.NodeClass))
				}
			}
			output.WriteString("  └─\n")
		}
		output.WriteString("\n")
	}
	
	// Connections
	if len(topology.Edges) > 0 {
		output.WriteString("Cross-Cloud Connections:\n")
		for _, edge := range topology.Edges {
			sourceNode := r.findNode(topology.Nodes, edge.SourceID)
			targetNode := r.findNode(topology.Nodes, edge.TargetID)
			
			if sourceNode != nil && targetNode != nil {
				arrow := "↔"
				if edge.Direction == "unidirectional" {
					arrow = "→"
				}
				
				output.WriteString(fmt.Sprintf("  %s (%s) %s %s (%s) [%s]\n",
					sourceNode.Name, sourceNode.Provider,
					arrow,
					targetNode.Name, targetNode.Provider,
					edge.EdgeType))
			}
		}
	}
	
	return output.String(), nil
}

func (r *ASCIITopologyRenderer) RenderCorrelations(correlations []*CrossCloudCorrelation) (string, error) {
	var output strings.Builder
	
	output.WriteString("Cross-Cloud Correlations\n")
	output.WriteString(strings.Repeat("=", 30) + "\n\n")
	
	// Group by correlation type
	typeGroups := make(map[string][]*CrossCloudCorrelation)
	for _, correlation := range correlations {
		typeGroups[correlation.CorrelationType] = append(typeGroups[correlation.CorrelationType], correlation)
	}
	
	for corrType, corrs := range typeGroups {
		output.WriteString(fmt.Sprintf("%s (%d):\n", strings.ToTitle(strings.ReplaceAll(corrType, "_", " ")), len(corrs)))
		
		for _, corr := range corrs {
			confidence := fmt.Sprintf("%.1f%%", corr.ConfidenceScore*100)
			output.WriteString(fmt.Sprintf("  • %s ↔ %s [%s]\n", 
				corr.SourceProvider, corr.TargetProvider, confidence))
		}
		output.WriteString("\n")
	}
	
	return output.String(), nil
}

func (r *ASCIITopologyRenderer) findNode(nodes []*TopologyNode, nodeID string) *TopologyNode {
	for _, node := range nodes {
		if node.ID == nodeID {
			return node
		}
	}
	return nil
}

// NewDOTTopologyRenderer creates a new DOT topology renderer
func NewDOTTopologyRenderer(includeAttributes bool) *DOTTopologyRenderer {
	return &DOTTopologyRenderer{
		includeAttributes: includeAttributes,
	}
}

func (r *DOTTopologyRenderer) GetFormat() string {
	return "dot"
}

func (r *DOTTopologyRenderer) RenderTopology(topology *NetworkTopology) (string, error) {
	var output strings.Builder
	
	output.WriteString("digraph CrossCloudTopology {\n")
	output.WriteString("  rankdir=LR;\n")
	output.WriteString("  node [shape=box];\n\n")
	
	// Clusters
	for _, cluster := range topology.Clusters {
		output.WriteString(fmt.Sprintf("  subgraph cluster_%s {\n", r.sanitizeID(cluster.ID)))
		output.WriteString(fmt.Sprintf("    label=\"%s (%s)\";\n", cluster.Name, cluster.Provider))
		output.WriteString("    style=filled;\n")
		output.WriteString("    fillcolor=lightgray;\n")
		
		for _, nodeID := range cluster.NodeIDs {
			output.WriteString(fmt.Sprintf("    \"%s\";\n", nodeID))
		}
		
		output.WriteString("  }\n\n")
	}
	
	// Nodes
	for _, node := range topology.Nodes {
		label := node.Name
		if r.includeAttributes {
			label += fmt.Sprintf("\\n[%s]", node.NodeClass)
		}
		
		color := r.getNodeColor(node.Provider)
		output.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\", fillcolor=\"%s\", style=filled];\n", 
			node.ID, label, color))
	}
	
	output.WriteString("\n")
	
	// Edges
	for _, edge := range topology.Edges {
		style := "solid"
		if edge.Direction == "bidirectional" {
			style = "solid"
		}
		
		output.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [label=\"%s\", style=\"%s\"];\n",
			edge.SourceID, edge.TargetID, edge.EdgeType, style))
	}
	
	output.WriteString("}\n")
	
	return output.String(), nil
}

func (r *DOTTopologyRenderer) RenderCorrelations(correlations []*CrossCloudCorrelation) (string, error) {
	var output strings.Builder
	
	output.WriteString("digraph CrossCloudCorrelations {\n")
	output.WriteString("  rankdir=LR;\n")
	output.WriteString("  node [shape=ellipse];\n\n")
	
	// Create nodes for providers
	providers := make(map[string]bool)
	for _, corr := range correlations {
		providers[corr.SourceProvider] = true
		providers[corr.TargetProvider] = true
	}
	
	for provider := range providers {
		color := r.getNodeColor(provider)
		output.WriteString(fmt.Sprintf("  \"%s\" [fillcolor=\"%s\", style=filled];\n", provider, color))
	}
	
	output.WriteString("\n")
	
	// Add correlation edges
	for _, corr := range correlations {
		confidence := fmt.Sprintf("%.1f%%", corr.ConfidenceScore*100)
		output.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [label=\"%s\\n%s\"];\n",
			corr.SourceProvider, corr.TargetProvider, corr.CorrelationType, confidence))
	}
	
	output.WriteString("}\n")
	
	return output.String(), nil
}

func (r *DOTTopologyRenderer) sanitizeID(id string) string {
	return strings.ReplaceAll(strings.ReplaceAll(id, "-", "_"), ":", "_")
}

func (r *DOTTopologyRenderer) getNodeColor(provider string) string {
	switch strings.ToLower(provider) {
	case "aws":
		return "lightblue"
	case "azure":
		return "lightgreen"
	case "gcp":
		return "lightyellow"
	default:
		return "lightgray"
	}
}

// NewJSONTopologyRenderer creates a new JSON topology renderer
func NewJSONTopologyRenderer(prettyPrint bool) *JSONTopologyRenderer {
	return &JSONTopologyRenderer{
		prettyPrint: prettyPrint,
	}
}

func (r *JSONTopologyRenderer) GetFormat() string {
	return "json"
}

func (r *JSONTopologyRenderer) RenderTopology(topology *NetworkTopology) (string, error) {
	// In a real implementation, this would use json.Marshal
	// For simplicity, we'll create a basic JSON representation
	
	var output strings.Builder
	output.WriteString("{\n")
	output.WriteString(fmt.Sprintf("  \"id\": \"%s\",\n", topology.ID))
	output.WriteString(fmt.Sprintf("  \"name\": \"%s\",\n", topology.Name))
	output.WriteString(fmt.Sprintf("  \"providers\": %q,\n", topology.Providers))
	output.WriteString(fmt.Sprintf("  \"node_count\": %d,\n", len(topology.Nodes)))
	output.WriteString(fmt.Sprintf("  \"edge_count\": %d,\n", len(topology.Edges)))
	output.WriteString(fmt.Sprintf("  \"cluster_count\": %d\n", len(topology.Clusters)))
	output.WriteString("}\n")
	
	return output.String(), nil
}

func (r *JSONTopologyRenderer) RenderCorrelations(correlations []*CrossCloudCorrelation) (string, error) {
	var output strings.Builder
	output.WriteString("{\n")
	output.WriteString(fmt.Sprintf("  \"correlation_count\": %d,\n", len(correlations)))
	output.WriteString("  \"correlations\": [\n")
	
	for i, corr := range correlations {
		output.WriteString("    {\n")
		output.WriteString(fmt.Sprintf("      \"type\": \"%s\",\n", corr.CorrelationType))
		output.WriteString(fmt.Sprintf("      \"source_provider\": \"%s\",\n", corr.SourceProvider))
		output.WriteString(fmt.Sprintf("      \"target_provider\": \"%s\",\n", corr.TargetProvider))
		output.WriteString(fmt.Sprintf("      \"confidence\": %.2f\n", corr.ConfidenceScore))
		output.WriteString("    }")
		
		if i < len(correlations)-1 {
			output.WriteString(",")
		}
		output.WriteString("\n")
	}
	
	output.WriteString("  ]\n")
	output.WriteString("}\n")
	
	return output.String(), nil
}

// NewMermaidTopologyRenderer creates a new Mermaid topology renderer
func NewMermaidTopologyRenderer(direction string) *MermaidTopologyRenderer {
	if direction == "" {
		direction = "TB" // Top to Bottom
	}
	return &MermaidTopologyRenderer{
		direction: direction,
	}
}

func (r *MermaidTopologyRenderer) GetFormat() string {
	return "mermaid"
}

func (r *MermaidTopologyRenderer) RenderTopology(topology *NetworkTopology) (string, error) {
	var output strings.Builder
	
	output.WriteString(fmt.Sprintf("graph %s\n", r.direction))
	
	// Clusters
	for _, cluster := range topology.Clusters {
		output.WriteString(fmt.Sprintf("  subgraph %s[\"%s (%s)\"]\n", 
			r.sanitizeID(cluster.ID), cluster.Name, cluster.Provider))
		
		for _, nodeID := range cluster.NodeIDs {
			if node := r.findNode(topology.Nodes, nodeID); node != nil {
				output.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", 
					r.sanitizeID(nodeID), node.Name))
			}
		}
		
		output.WriteString("  end\n")
	}
	
	// Edges
	for _, edge := range topology.Edges {
		connector := "-->"
		if edge.Direction == "bidirectional" {
			connector = "---"
		}
		
		output.WriteString(fmt.Sprintf("  %s %s %s\n",
			r.sanitizeID(edge.SourceID), connector, r.sanitizeID(edge.TargetID)))
	}
	
	return output.String(), nil
}

func (r *MermaidTopologyRenderer) RenderCorrelations(correlations []*CrossCloudCorrelation) (string, error) {
	var output strings.Builder
	
	output.WriteString(fmt.Sprintf("graph %s\n", r.direction))
	
	// Add provider nodes
	providers := make(map[string]bool)
	for _, corr := range correlations {
		providers[corr.SourceProvider] = true
		providers[corr.TargetProvider] = true
	}
	
	for provider := range providers {
		output.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", provider, provider))
	}
	
	// Add correlation edges
	for _, corr := range correlations {
		confidence := fmt.Sprintf("%.0f%%", corr.ConfidenceScore*100)
		output.WriteString(fmt.Sprintf("  %s --> %s\n", corr.SourceProvider, corr.TargetProvider))
		output.WriteString(fmt.Sprintf("  %s -.->|%s| %s\n", 
			corr.SourceProvider, confidence, corr.TargetProvider))
	}
	
	return output.String(), nil
}

func (r *MermaidTopologyRenderer) findNode(nodes []*TopologyNode, nodeID string) *TopologyNode {
	for _, node := range nodes {
		if node.ID == nodeID {
			return node
		}
	}
	return nil
}

func (r *MermaidTopologyRenderer) sanitizeID(id string) string {
	return strings.ReplaceAll(strings.ReplaceAll(id, "-", "_"), ":", "_")
}
package crosscloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/jlgore/corkscrew/pkg/diagrams/pkg/renderer"
	"github.com/jlgore/corkscrew/pkg/models"
)

// CrossCloudTopologyVisualizer integrates with the existing diagrams package
// to provide cross-cloud network topology visualization
type CrossCloudTopologyVisualizer struct {
	renderer renderer.DiagramRenderer
}

// NewCrossCloudTopologyVisualizer creates a new visualizer using the existing diagrams framework
func NewCrossCloudTopologyVisualizer(diagramRenderer renderer.DiagramRenderer) *CrossCloudTopologyVisualizer {
	return &CrossCloudTopologyVisualizer{
		renderer: diagramRenderer,
	}
}

// VisualizeNetworkTopology creates a network topology visualization using the existing diagrams framework
func (v *CrossCloudTopologyVisualizer) VisualizeNetworkTopology(ctx context.Context, resources []*models.Resource, correlations []*CrossCloudCorrelation) (string, error) {
	// Convert cross-cloud data to the diagrams framework format
	graphData := v.convertToGraphData(resources, correlations)
	
	// Use the existing ASCII renderer to display the topology
	return v.renderer.RenderASCII(graphData)
}

// VisualizeCorrelations creates a correlation-focused visualization
func (v *CrossCloudTopologyVisualizer) VisualizeCorrelations(correlations []*CrossCloudCorrelation) (string, error) {
	graphData := v.convertCorrelationsToGraphData(correlations)
	return v.renderer.RenderASCII(graphData)
}

// VisualizeMermaidTopology creates a Mermaid diagram for the topology
func (v *CrossCloudTopologyVisualizer) VisualizeMermaidTopology(ctx context.Context, resources []*models.Resource, correlations []*CrossCloudCorrelation) (string, error) {
	graphData := v.convertToGraphData(resources, correlations)
	mermaidContent := v.convertToMermaid(graphData)
	return v.renderer.RenderMermaid(mermaidContent)
}

// convertToGraphData converts cross-cloud resources and correlations to the diagrams framework format
func (v *CrossCloudTopologyVisualizer) convertToGraphData(resources []*models.Resource, correlations []*CrossCloudCorrelation) *renderer.GraphData {
	// Filter to network-related resources only
	networkResources := v.filterNetworkResources(resources)
	
	// Convert resources to nodes
	nodes := make([]renderer.Node, 0, len(networkResources))
	nodeMap := make(map[string]*models.Resource)
	
	for _, resource := range networkResources {
		node := renderer.Node{
			ID:    resource.ID,
			Label: v.formatResourceLabel(resource),
			Type:  v.categorizeNetworkResource(resource),
			Properties: map[string]string{
				"provider": resource.Provider,
				"region":   resource.Region,
				"type":     resource.Type,
			},
		}
		
		// Add IP addresses if available
		if len(resource.IPAddresses) > 0 {
			ips := make([]string, len(resource.IPAddresses))
			for i, ip := range resource.IPAddresses {
				ips[i] = ip.Address
			}
			node.Properties["ips"] = strings.Join(ips, ",")
		}
		
		// Add DNS names if available
		if len(resource.DNSNames) > 0 {
			dnsNames := make([]string, len(resource.DNSNames))
			for i, dns := range resource.DNSNames {
				dnsNames[i] = dns.Name
			}
			node.Properties["dns"] = strings.Join(dnsNames, ",")
		}
		
		nodes = append(nodes, node)
		nodeMap[resource.ID] = resource
	}
	
	// Convert correlations to edges
	edges := make([]renderer.Edge, 0, len(correlations))
	for _, correlation := range correlations {
		// Only include correlations where both resources are in our node set
		if _, sourceExists := nodeMap[correlation.SourceResourceID]; sourceExists {
			if _, targetExists := nodeMap[correlation.TargetResourceID]; targetExists {
				edge := renderer.Edge{
					From:  correlation.SourceResourceID,
					To:    correlation.TargetResourceID,
					Label: v.formatCorrelationLabel(correlation),
					Type:  correlation.CorrelationType,
					Properties: map[string]string{
						"confidence": fmt.Sprintf("%.1f%%", correlation.ConfidenceScore*100),
						"method":     correlation.CorrelationMethod,
						"providers":  fmt.Sprintf("%s↔%s", correlation.SourceProvider, correlation.TargetProvider),
					},
				}
				edges = append(edges, edge)
			}
		}
	}
	
	return &renderer.GraphData{
		Nodes: nodes,
		Edges: edges,
		Title: "Cross-Cloud Network Topology",
	}
}

// convertCorrelationsToGraphData creates a provider-focused correlation view
func (v *CrossCloudTopologyVisualizer) convertCorrelationsToGraphData(correlations []*CrossCloudCorrelation) *renderer.GraphData {
	// Create provider nodes
	providers := make(map[string]bool)
	for _, corr := range correlations {
		providers[corr.SourceProvider] = true
		providers[corr.TargetProvider] = true
	}
	
	nodes := make([]renderer.Node, 0, len(providers))
	for provider := range providers {
		// Count correlations for this provider
		inCount, outCount := v.countProviderCorrelations(provider, correlations)
		
		node := renderer.Node{
			ID:    provider,
			Label: fmt.Sprintf("%s\n(In:%d Out:%d)", strings.ToUpper(provider), inCount, outCount),
			Type:  "cloud_provider",
			Properties: map[string]string{
				"provider":         provider,
				"incoming_corr":    fmt.Sprintf("%d", inCount),
				"outgoing_corr":    fmt.Sprintf("%d", outCount),
				"total_corr":       fmt.Sprintf("%d", inCount+outCount),
			},
		}
		nodes = append(nodes, node)
	}
	
	// Group correlations by provider pair and type
	corrGroups := make(map[string]*CorrelationGroup)
	
	for _, corr := range correlations {
		key := fmt.Sprintf("%s→%s:%s", corr.SourceProvider, corr.TargetProvider, corr.CorrelationType)
		if group, exists := corrGroups[key]; exists {
			group.Count++
			group.TotalConfidence += corr.ConfidenceScore
		} else {
			corrGroups[key] = &CorrelationGroup{
				SourceProvider:  corr.SourceProvider,
				TargetProvider:  corr.TargetProvider,
				CorrelationType: corr.CorrelationType,
				Count:           1,
				TotalConfidence: corr.ConfidenceScore,
			}
		}
	}
	
	// Convert correlation groups to edges
	edges := make([]renderer.Edge, 0, len(corrGroups))
	for _, group := range corrGroups {
		avgConfidence := group.TotalConfidence / float64(group.Count)
		
		edge := renderer.Edge{
			From:  group.SourceProvider,
			To:    group.TargetProvider,
			Label: v.formatCorrelationGroupLabel(group, avgConfidence),
			Type:  group.CorrelationType,
			Properties: map[string]string{
				"count":            fmt.Sprintf("%d", group.Count),
				"avg_confidence":   fmt.Sprintf("%.1f%%", avgConfidence*100),
				"correlation_type": group.CorrelationType,
			},
		}
		edges = append(edges, edge)
	}
	
	return &renderer.GraphData{
		Nodes: nodes,
		Edges: edges,
		Title: "Cross-Cloud Correlations Summary",
	}
}

// convertToMermaid converts GraphData to Mermaid diagram format
func (v *CrossCloudTopologyVisualizer) convertToMermaid(graphData *renderer.GraphData) string {
	var mermaid strings.Builder
	
	mermaid.WriteString("graph TD\n")
	
	// Add title as comment
	if graphData.Title != "" {
		mermaid.WriteString(fmt.Sprintf("    %% %s\n", graphData.Title))
	}
	
	// Group nodes by provider for better layout
	providerNodes := make(map[string][]renderer.Node)
	for _, node := range graphData.Nodes {
		provider := node.Properties["provider"]
		if provider == "" {
			provider = "unknown"
		}
		providerNodes[provider] = append(providerNodes[provider], node)
	}
	
	// Create subgraphs for each provider
	for provider, nodes := range providerNodes {
		if len(nodes) > 1 {
			mermaid.WriteString(fmt.Sprintf("    subgraph %s[%s]\n", provider, strings.ToUpper(provider)))
			for _, node := range nodes {
				mermaid.WriteString(fmt.Sprintf("        %s[\"%s\"]\n", 
					v.sanitizeID(node.ID), v.escapeLabel(node.Label)))
			}
			mermaid.WriteString("    end\n")
		} else if len(nodes) == 1 {
			// Single node, add directly
			node := nodes[0]
			mermaid.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", 
				v.sanitizeID(node.ID), v.escapeLabel(node.Label)))
		}
	}
	
	// Add edges
	for _, edge := range graphData.Edges {
		arrow := "-->"
		if v.isBidirectionalCorrelation(edge.Type) {
			arrow = "---"
		}
		
		edgeLabel := ""
		if edge.Label != "" {
			edgeLabel = fmt.Sprintf("|%s|", v.escapeLabel(edge.Label))
		}
		
		mermaid.WriteString(fmt.Sprintf("    %s %s%s %s\n", 
			v.sanitizeID(edge.From), arrow, edgeLabel, v.sanitizeID(edge.To)))
	}
	
	// Add styling for different providers
	mermaid.WriteString("\n    %% Provider-specific styling\n")
	for provider := range providerNodes {
		color := v.getProviderColor(provider)
		mermaid.WriteString(fmt.Sprintf("    classDef %sStyle fill:%s\n", provider, color))
		
		for _, node := range providerNodes[provider] {
			mermaid.WriteString(fmt.Sprintf("    class %s %sStyle\n", 
				v.sanitizeID(node.ID), provider))
		}
	}
	
	return mermaid.String()
}

// Helper functions

type CorrelationGroup struct {
	SourceProvider  string
	TargetProvider  string
	CorrelationType string
	Count           int
	TotalConfidence float64
}

func (v *CrossCloudTopologyVisualizer) filterNetworkResources(resources []*models.Resource) []*models.Resource {
	networkResources := make([]*models.Resource, 0)
	
	for _, resource := range resources {
		if v.isNetworkResource(resource) {
			networkResources = append(networkResources, resource)
		}
	}
	
	return networkResources
}

func (v *CrossCloudTopologyVisualizer) isNetworkResource(resource *models.Resource) bool {
	resourceType := strings.ToLower(resource.Type)
	
	networkTypes := []string{
		"vpc", "vnet", "network", "subnet", "securitygroup", "firewall",
		"loadbalancer", "gateway", "vpn", "peering", "route", "dns",
		"trafficmanager", "frontdoor", "interconnect", "directconnect",
		"expressroute", "natgateway", "internetgateway", "networkinterface",
	}
	
	for _, netType := range networkTypes {
		if strings.Contains(resourceType, netType) {
			return true
		}
	}
	
	// Also include resources with network interfaces or IP addresses
	if len(resource.IPAddresses) > 0 || len(resource.NetworkInterfaces) > 0 {
		return true
	}
	
	return false
}

func (v *CrossCloudTopologyVisualizer) formatResourceLabel(resource *models.Resource) string {
	name := resource.Name
	if len(name) > 20 {
		name = name[:17] + "..."
	}
	
	// Add provider prefix for clarity
	providerPrefix := ""
	switch strings.ToLower(resource.Provider) {
	case "aws":
		providerPrefix = "🅰️"
	case "azure":
		providerPrefix = "🅰️z"
	case "gcp":
		providerPrefix = "🅶"
	default:
		providerPrefix = "☁️"
	}
	
	return fmt.Sprintf("%s %s", providerPrefix, name)
}

func (v *CrossCloudTopologyVisualizer) categorizeNetworkResource(resource *models.Resource) string {
	resourceType := strings.ToLower(resource.Type)
	
	if strings.Contains(resourceType, "vpc") || strings.Contains(resourceType, "vnet") ||
		strings.Contains(resourceType, "network") {
		return "network"
	}
	
	if strings.Contains(resourceType, "subnet") {
		return "subnet"
	}
	
	if strings.Contains(resourceType, "security") || strings.Contains(resourceType, "firewall") {
		return "security"
	}
	
	if strings.Contains(resourceType, "load") || strings.Contains(resourceType, "balancer") {
		return "load_balancer"
	}
	
	if strings.Contains(resourceType, "gateway") {
		return "gateway"
	}
	
	if strings.Contains(resourceType, "vpn") {
		return "vpn"
	}
	
	if strings.Contains(resourceType, "dns") {
		return "dns"
	}
	
	return "other"
}

func (v *CrossCloudTopologyVisualizer) formatCorrelationLabel(correlation *CrossCloudCorrelation) string {
	// Create a concise label for the correlation
	confidence := fmt.Sprintf("%.0f%%", correlation.ConfidenceScore*100)
	
	switch correlation.CorrelationType {
	case "vpn_connection":
		return fmt.Sprintf("VPN (%s)", confidence)
	case "network_peering":
		return fmt.Sprintf("Peering (%s)", confidence)
	case "direct_connection":
		return fmt.Sprintf("Direct (%s)", confidence)
	case "dns_load_balancing":
		return fmt.Sprintf("DNS LB (%s)", confidence)
	case "security_rule_overlap":
		return fmt.Sprintf("Security (%s)", confidence)
	case "backend_pool_correlation":
		return fmt.Sprintf("Backend (%s)", confidence)
	default:
		return fmt.Sprintf("%s (%s)", correlation.CorrelationType, confidence)
	}
}

func (v *CrossCloudTopologyVisualizer) formatCorrelationGroupLabel(group *CorrelationGroup, avgConfidence float64) string {
	if group.Count == 1 {
		return fmt.Sprintf("%s\n%.0f%%", group.CorrelationType, avgConfidence*100)
	}
	return fmt.Sprintf("%s ×%d\n%.0f%%", group.CorrelationType, group.Count, avgConfidence*100)
}

func (v *CrossCloudTopologyVisualizer) countProviderCorrelations(provider string, correlations []*CrossCloudCorrelation) (int, int) {
	inCount := 0
	outCount := 0
	
	for _, corr := range correlations {
		if corr.TargetProvider == provider {
			inCount++
		}
		if corr.SourceProvider == provider {
			outCount++
		}
	}
	
	return inCount, outCount
}

func (v *CrossCloudTopologyVisualizer) sanitizeID(id string) string {
	// Replace characters that aren't valid in Mermaid IDs
	id = strings.ReplaceAll(id, "-", "_")
	id = strings.ReplaceAll(id, ":", "_")
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, ".", "_")
	
	// Ensure it starts with a letter
	if len(id) > 0 && (id[0] >= '0' && id[0] <= '9') {
		id = "n" + id
	}
	
	return id
}

func (v *CrossCloudTopologyVisualizer) escapeLabel(label string) string {
	// Escape characters that have special meaning in Mermaid
	label = strings.ReplaceAll(label, "\"", "'")
	label = strings.ReplaceAll(label, "\n", "\\n")
	return label
}

func (v *CrossCloudTopologyVisualizer) isBidirectionalCorrelation(correlationType string) bool {
	bidirectionalTypes := []string{
		"vpn_connection", "network_peering", "direct_connection",
		"security_rule_overlap", "ip_match",
	}
	
	for _, bidType := range bidirectionalTypes {
		if correlationType == bidType {
			return true
		}
	}
	
	return false
}

func (v *CrossCloudTopologyVisualizer) getProviderColor(provider string) string {
	switch strings.ToLower(provider) {
	case "aws":
		return "#FF9900"  // AWS Orange
	case "azure":
		return "#0078D4"  // Azure Blue
	case "gcp":
		return "#4285F4"  // Google Blue
	default:
		return "#9E9E9E"  // Gray
	}
}

// CreateNetworkSummaryReport creates a text-based summary of the network topology
func (v *CrossCloudTopologyVisualizer) CreateNetworkSummaryReport(resources []*models.Resource, correlations []*CrossCloudCorrelation) string {
	var report strings.Builder
	
	report.WriteString("Cross-Cloud Network Analysis Report\n")
	report.WriteString(strings.Repeat("=", 50) + "\n\n")
	
	// Provider summary
	providerResources := make(map[string]int)
	networkResources := v.filterNetworkResources(resources)
	
	for _, resource := range networkResources {
		providerResources[resource.Provider]++
	}
	
	report.WriteString("Provider Summary:\n")
	for provider, count := range providerResources {
		report.WriteString(fmt.Sprintf("  %s: %d network resources\n", strings.ToUpper(provider), count))
	}
	report.WriteString("\n")
	
	// Correlation summary
	correlationTypes := make(map[string]int)
	totalConfidence := 0.0
	
	for _, corr := range correlations {
		correlationTypes[corr.CorrelationType]++
		totalConfidence += corr.ConfidenceScore
	}
	
	report.WriteString("Cross-Cloud Correlations:\n")
	for corrType, count := range correlationTypes {
		report.WriteString(fmt.Sprintf("  %s: %d correlations\n", 
			strings.Title(strings.ReplaceAll(corrType, "_", " ")), count))
	}
	
	if len(correlations) > 0 {
		avgConfidence := totalConfidence / float64(len(correlations))
		report.WriteString(fmt.Sprintf("\nAverage Confidence: %.1f%%\n", avgConfidence*100))
	}
	
	// High-confidence correlations
	report.WriteString("\nHigh-Confidence Correlations (>80%):\n")
	highConfCount := 0
	for _, corr := range correlations {
		if corr.ConfidenceScore > 0.8 {
			report.WriteString(fmt.Sprintf("  %s ↔ %s: %s (%.1f%%)\n",
				corr.SourceProvider, corr.TargetProvider, 
				corr.CorrelationType, corr.ConfidenceScore*100))
			highConfCount++
		}
	}
	
	if highConfCount == 0 {
		report.WriteString("  None found\n")
	}
	
	return report.String()
}
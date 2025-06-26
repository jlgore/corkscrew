package renderer

import (
	"fmt"
	"strings"
)

// MermaidRenderer implements DiagramRenderer for Mermaid diagrams
type MermaidRenderer struct {
	width  int
	height int
	theme  Theme
}

// NewMermaidRenderer creates a new Mermaid renderer
func NewMermaidRenderer() *MermaidRenderer {
	return &MermaidRenderer{
		width:  80,
		height: 40,
		theme:  DefaultTheme(),
	}
}

// RenderMermaid renders Mermaid content directly
func (r *MermaidRenderer) RenderMermaid(mermaidContent string) (string, error) {
	return mermaidContent, nil
}

// RenderASCII converts GraphData to Mermaid format then renders as ASCII
func (r *MermaidRenderer) RenderASCII(graphData *GraphData) (string, error) {
	mermaidContent, err := r.GraphDataToMermaid(graphData)
	if err != nil {
		return "", err
	}
	
	// For now, return mermaid content with ASCII borders
	// In a full implementation, this would use mermaid-ascii or similar
	ascii := NewASCIIRenderer()
	ascii.SetDimensions(r.width, r.height)
	return ascii.RenderMermaid(mermaidContent)
}

// GraphDataToMermaid converts GraphData to Mermaid diagram format
func (r *MermaidRenderer) GraphDataToMermaid(graphData *GraphData) (string, error) {
	var result strings.Builder
	
	// Start with graph declaration
	result.WriteString("graph TD\n")
	
	if graphData.Title != "" {
		// Add title as a comment
		result.WriteString(fmt.Sprintf("    %%{init: {'theme':'base', 'themeVariables': {'primaryColor':'#ff0000'}}}%%\n"))
		result.WriteString(fmt.Sprintf("    %% %s\n", graphData.Title))
	}
	
	// Add nodes with styling based on type
	nodeStyles := make(map[string]string)
	for _, node := range graphData.Nodes {
		// Clean node ID for Mermaid (remove special characters)
		cleanID := r.cleanNodeID(node.ID)
		
		// Format label to handle special characters
		cleanLabel := r.escapeLabel(node.Label)
		
		// Define node with shape based on type
		switch node.Type {
		case "vpc":
			result.WriteString(fmt.Sprintf("    %s[%s]\n", cleanID, cleanLabel))
			nodeStyles[cleanID] = "fill:#e1f5fe,stroke:#01579b"
		case "subnet":
			result.WriteString(fmt.Sprintf("    %s(%s)\n", cleanID, cleanLabel))
			nodeStyles[cleanID] = "fill:#f3e5f5,stroke:#4a148c"
		case "instance", "ec2":
			result.WriteString(fmt.Sprintf("    %s[%s]\n", cleanID, cleanLabel))
			nodeStyles[cleanID] = "fill:#fff3e0,stroke:#e65100"
		case "s3", "bucket":
			result.WriteString(fmt.Sprintf("    %s{%s}\n", cleanID, cleanLabel))
			nodeStyles[cleanID] = "fill:#e8f5e8,stroke:#2e7d32"
		case "database", "rds":
			result.WriteString(fmt.Sprintf("    %s[(%s)]\n", cleanID, cleanLabel))
			nodeStyles[cleanID] = "fill:#fce4ec,stroke:#c2185b"
		case "lambda", "function":
			result.WriteString(fmt.Sprintf("    %s>%s]\n", cleanID, cleanLabel))
			nodeStyles[cleanID] = "fill:#fff8e1,stroke:#f57c00"
		default:
			result.WriteString(fmt.Sprintf("    %s[%s]\n", cleanID, cleanLabel))
			nodeStyles[cleanID] = "fill:#f5f5f5,stroke:#616161"
		}
	}
	
	// Add edges
	for _, edge := range graphData.Edges {
		cleanFromID := r.cleanNodeID(edge.From)
		cleanToID := r.cleanNodeID(edge.To)
		
		// Choose arrow style based on relationship type
		var arrow string
		switch edge.Type {
		case "contains", "has":
			arrow = "-->"
		case "connects_to", "routes_to":
			arrow = "-..->"
		case "depends_on":
			arrow = "==>"
		case "manages", "controls":
			arrow = "-.->"
		default:
			arrow = "-->"
		}
		
		if edge.Label != "" {
			cleanLabel := r.escapeLabel(edge.Label)
			result.WriteString(fmt.Sprintf("    %s %s|%s| %s\n", cleanFromID, arrow, cleanLabel, cleanToID))
		} else {
			result.WriteString(fmt.Sprintf("    %s %s %s\n", cleanFromID, arrow, cleanToID))
		}
	}
	
	// Add styling
	if len(nodeStyles) > 0 {
		result.WriteString("\n    %% Styling\n")
		for nodeID, style := range nodeStyles {
			result.WriteString(fmt.Sprintf("    classDef %sStyle %s\n", nodeID, style))
			result.WriteString(fmt.Sprintf("    class %s %sStyle\n", nodeID, nodeID))
		}
	}
	
	return result.String(), nil
}

// cleanNodeID removes special characters from node IDs for Mermaid compatibility
func (r *MermaidRenderer) cleanNodeID(id string) string {
	// Replace common problematic characters
	replacer := strings.NewReplacer(
		"-", "_",
		":", "_",
		"/", "_",
		".", "_",
		" ", "_",
		"(", "_",
		")", "_",
		"[", "_",
		"]", "_",
		"{", "_",
		"}", "_",
	)
	
	cleaned := replacer.Replace(id)
	
	// Ensure it starts with a letter
	if len(cleaned) > 0 && !isLetter(cleaned[0]) {
		cleaned = "n" + cleaned
	}
	
	return cleaned
}

// escapeLabel escapes special characters in labels for Mermaid
func (r *MermaidRenderer) escapeLabel(label string) string {
	// Remove or escape characters that break Mermaid syntax
	replacer := strings.NewReplacer(
		"\"", "&quot;",
		"'", "&apos;",
		"<", "&lt;",
		">", "&gt;",
		"&", "&amp;",
		"|", "&#124;",
		"[", "&#91;",
		"]", "&#93;",
		"{", "&#123;",
		"}", "&#125;",
	)
	
	return replacer.Replace(label)
}

// isLetter checks if a character is a letter
func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// SetDimensions sets the rendering dimensions
func (r *MermaidRenderer) SetDimensions(width, height int) {
	r.width = width
	r.height = height
}

// SetTheme sets the rendering theme
func (r *MermaidRenderer) SetTheme(theme Theme) error {
	r.theme = theme
	return nil
}

// GenerateNetworkDiagram creates a Mermaid network topology diagram
func (r *MermaidRenderer) GenerateNetworkDiagram(graphData *GraphData) (string, error) {
	var result strings.Builder
	
	result.WriteString("graph TD\n")
	
	if graphData.Title != "" {
		result.WriteString(fmt.Sprintf("    %% %s\n", graphData.Title))
	}
	
	// Group nodes by network hierarchy
	vpcs := []Node{}
	subnets := []Node{}
	instances := []Node{}
	other := []Node{}
	
	for _, node := range graphData.Nodes {
		switch node.Type {
		case "vpc":
			vpcs = append(vpcs, node)
		case "subnet":
			subnets = append(subnets, node)
		case "instance", "ec2":
			instances = append(instances, node)
		default:
			other = append(other, node)
		}
	}
	
	// Create subgraphs for VPCs
	for _, vpc := range vpcs {
		cleanVPCID := r.cleanNodeID(vpc.ID)
		result.WriteString(fmt.Sprintf("    subgraph %s [\"%s\"]\n", cleanVPCID, vpc.Label))
		
		// Add subnets in this VPC
		for _, subnet := range subnets {
			if r.isConnected(vpc.ID, subnet.ID, graphData.Edges) {
				cleanSubnetID := r.cleanNodeID(subnet.ID)
				result.WriteString(fmt.Sprintf("        %s(%s)\n", cleanSubnetID, subnet.Label))
				
				// Add instances in this subnet
				for _, instance := range instances {
					if r.isConnected(subnet.ID, instance.ID, graphData.Edges) {
						cleanInstanceID := r.cleanNodeID(instance.ID)
						result.WriteString(fmt.Sprintf("        %s[%s]\n", cleanInstanceID, instance.Label))
					}
				}
			}
		}
		
		result.WriteString("    end\n")
	}
	
	// Add other resources
	for _, node := range other {
		cleanID := r.cleanNodeID(node.ID)
		result.WriteString(fmt.Sprintf("    %s[%s]\n", cleanID, node.Label))
	}
	
	// Add edges
	for _, edge := range graphData.Edges {
		cleanFromID := r.cleanNodeID(edge.From)
		cleanToID := r.cleanNodeID(edge.To)
		result.WriteString(fmt.Sprintf("    %s --> %s\n", cleanFromID, cleanToID))
	}
	
	return result.String(), nil
}

// isConnected checks if two nodes are connected by an edge
func (r *MermaidRenderer) isConnected(fromID, toID string, edges []Edge) bool {
	for _, edge := range edges {
		if (edge.From == fromID && edge.To == toID) || (edge.From == toID && edge.To == fromID) {
			return true
		}
	}
	return false
}
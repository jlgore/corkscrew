package renderer

import (
	"fmt"
	"sort"
	"strings"
)

// ASCIIRenderer implements DiagramRenderer for ASCII art diagrams
type ASCIIRenderer struct {
	width  int
	height int
	theme  Theme
}

// NewASCIIRenderer creates a new ASCII renderer
func NewASCIIRenderer() *ASCIIRenderer {
	return &ASCIIRenderer{
		width:  80,
		height: 40,
		theme:  DefaultTheme(),
	}
}

// RenderMermaid renders a Mermaid diagram as ASCII (simplified)
func (r *ASCIIRenderer) RenderMermaid(mermaidContent string) (string, error) {
	// For now, return the mermaid content as-is with ASCII borders
	// In a full implementation, this would parse and render the mermaid syntax
	lines := strings.Split(mermaidContent, "\n")
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	
	if maxLen > r.width-4 {
		maxLen = r.width - 4
	}
	
	var result strings.Builder
	
	// Top border
	result.WriteString("┌" + strings.Repeat("─", maxLen+2) + "┐\n")
	
	// Content lines
	for _, line := range lines {
		if len(line) > maxLen {
			line = line[:maxLen-3] + "..."
		}
		result.WriteString(fmt.Sprintf("│ %-*s │\n", maxLen, line))
	}
	
	// Bottom border
	result.WriteString("└" + strings.Repeat("─", maxLen+2) + "┘\n")
	
	return result.String(), nil
}

// RenderASCII renders a GraphData structure as ASCII art
func (r *ASCIIRenderer) RenderASCII(graphData *GraphData) (string, error) {
	if len(graphData.Nodes) == 0 {
		return "No nodes to display", nil
	}
	
	var result strings.Builder
	
	// Add title if present
	if graphData.Title != "" {
		title := fmt.Sprintf(" %s ", graphData.Title)
		titleLen := len(title)
		if titleLen > r.width {
			title = title[:r.width-3] + "..."
			titleLen = r.width
		}
		
		padding := (r.width - titleLen) / 2
		result.WriteString(strings.Repeat(" ", padding) + title + "\n")
		result.WriteString(strings.Repeat("═", r.width) + "\n\n")
	}
	
	// Simple node layout - arrange nodes in a grid
	nodesPerRow := 3
	if len(graphData.Nodes) < 3 {
		nodesPerRow = len(graphData.Nodes)
	}
	
	rows := (len(graphData.Nodes) + nodesPerRow - 1) / nodesPerRow
	nodeWidth := (r.width - (nodesPerRow + 1)) / nodesPerRow
	
	// Create a map for quick node lookup
	nodeMap := make(map[string]Node)
	for _, node := range graphData.Nodes {
		nodeMap[node.ID] = node
	}
	
	// Render nodes row by row
	for row := 0; row < rows; row++ {
		// Draw node boxes
		for line := 0; line < 3; line++ { // Each node is 3 lines tall
			for col := 0; col < nodesPerRow; col++ {
				nodeIndex := row*nodesPerRow + col
				if nodeIndex >= len(graphData.Nodes) {
					break
				}
				
				node := graphData.Nodes[nodeIndex]
				startCol := col * (nodeWidth + 1)
				
				if line == 0 {
					// Top border
					result.WriteString(fmt.Sprintf("%*s┌%s┐", startCol, "", strings.Repeat("─", nodeWidth-2)))
				} else if line == 1 {
					// Content line
					label := node.Label
					if len(label) > nodeWidth-4 {
						label = label[:nodeWidth-7] + "..."
					}
					content := fmt.Sprintf(" %-*s ", nodeWidth-4, label)
					result.WriteString(fmt.Sprintf("%*s│%s│", startCol, "", content))
				} else {
					// Bottom border
					result.WriteString(fmt.Sprintf("%*s└%s┘", startCol, "", strings.Repeat("─", nodeWidth-2)))
				}
			}
			result.WriteString("\n")
		}
		
		// Draw connections between nodes in this row and the next
		if row < rows-1 {
			r.drawConnections(&result, graphData.Edges, row, nodesPerRow, nodeWidth)
		}
	}
	
	// Add legend for relationships
	if len(graphData.Edges) > 0 {
		result.WriteString("\nRelationships:\n")
		relationshipTypes := make(map[string]int)
		for _, edge := range graphData.Edges {
			relationshipTypes[edge.Type]++
		}
		
		for relType, count := range relationshipTypes {
			result.WriteString(fmt.Sprintf("  %s: %d\n", relType, count))
		}
	}
	
	return result.String(), nil
}

// drawConnections draws ASCII connections between nodes
func (r *ASCIIRenderer) drawConnections(result *strings.Builder, edges []Edge, row, nodesPerRow, nodeWidth int) {
	// This is a simplified connection drawing - in a full implementation,
	// you'd calculate actual paths between nodes
	for _, edge := range edges {
		// Find positions of source and target nodes
		// This is simplified - a full implementation would track actual positions
		result.WriteString(fmt.Sprintf("  %s → %s (%s)\n", edge.From, edge.To, edge.Type))
	}
}

// SetDimensions sets the rendering dimensions
func (r *ASCIIRenderer) SetDimensions(width, height int) {
	r.width = width
	r.height = height
}

// SetTheme sets the rendering theme
func (r *ASCIIRenderer) SetTheme(theme Theme) error {
	r.theme = theme
	return nil
}

// DefaultTheme returns a default ASCII theme
func DefaultTheme() Theme {
	return Theme{
		Name: "default",
		Colors: map[string]string{
			"node":       "white",
			"edge":       "gray",
			"background": "black",
		},
		NodeStyle: NodeStyle{
			Shape:       "box",
			BorderStyle: "solid",
			FillStyle:   "none",
		},
		EdgeStyle: EdgeStyle{
			LineStyle: "solid",
			ArrowType: "simple",
		},
		BackgroundColor: "transparent",
	}
}

// RenderSimpleList renders nodes as a simple list (fallback)
func (r *ASCIIRenderer) RenderSimpleList(graphData *GraphData) string {
	var result strings.Builder
	
	if graphData.Title != "" {
		result.WriteString(fmt.Sprintf("%s\n%s\n\n", graphData.Title, strings.Repeat("=", len(graphData.Title))))
	}
	
	// Sort nodes by type then by label
	sortedNodes := make([]Node, len(graphData.Nodes))
	copy(sortedNodes, graphData.Nodes)
	sort.Slice(sortedNodes, func(i, j int) bool {
		if sortedNodes[i].Type != sortedNodes[j].Type {
			return sortedNodes[i].Type < sortedNodes[j].Type
		}
		return sortedNodes[i].Label < sortedNodes[j].Label
	})
	
	currentType := ""
	for _, node := range sortedNodes {
		if node.Type != currentType {
			if currentType != "" {
				result.WriteString("\n")
			}
			result.WriteString(fmt.Sprintf("%s:\n", strings.ToUpper(node.Type)))
			currentType = node.Type
		}
		result.WriteString(fmt.Sprintf("  • %s (%s)\n", node.Label, node.ID))
	}
	
	return result.String()
}
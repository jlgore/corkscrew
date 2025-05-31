package renderer

import (
	"context"
)

// DiagramRenderer defines the interface for rendering diagrams
type DiagramRenderer interface {
	RenderMermaid(mermaidContent string) (string, error)
	RenderASCII(graphData *GraphData) (string, error)
	SetDimensions(width, height int)
	SetTheme(theme Theme) error
}

// GraphData represents the graph structure for rendering
type GraphData struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	Title string `json:"title,omitempty"`
}

// Node represents a single node in the graph
type Node struct {
	ID         string            `json:"id"`
	Label      string            `json:"label"`
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties,omitempty"`
	Position   *Position         `json:"position,omitempty"`
}

// Edge represents a connection between two nodes
type Edge struct {
	From       string            `json:"from"`
	To         string            `json:"to"`
	Label      string            `json:"label,omitempty"`
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Position represents x,y coordinates for node positioning
type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Theme represents visual styling options
type Theme struct {
	Name            string            `json:"name"`
	Colors          map[string]string `json:"colors"`
	NodeStyle       NodeStyle         `json:"node_style"`
	EdgeStyle       EdgeStyle         `json:"edge_style"`
	BackgroundColor string            `json:"background_color"`
}

// NodeStyle defines styling for nodes
type NodeStyle struct {
	Shape       string `json:"shape"`
	BorderStyle string `json:"border_style"`
	FillStyle   string `json:"fill_style"`
}

// EdgeStyle defines styling for edges
type EdgeStyle struct {
	LineStyle string `json:"line_style"`
	ArrowType string `json:"arrow_type"`
}

// DiagramType represents different types of diagrams
type DiagramType int

const (
	DiagramTypeRelationship DiagramType = iota
	DiagramTypeDependency
	DiagramTypeNetwork
	DiagramTypeService
)

// DiagramOptions contains configuration for diagram generation
type DiagramOptions struct {
	Type        DiagramType       `json:"type"`
	Title       string            `json:"title,omitempty"`
	ResourceID  string            `json:"resource_id,omitempty"`
	Depth       int               `json:"depth,omitempty"`
	Filters     map[string]string `json:"filters,omitempty"`
	ShowDetails bool              `json:"show_details"`
	Layout      string            `json:"layout"`
}

// Converter defines interface for converting data to graph format
type Converter interface {
	ConvertToGraphData(ctx context.Context, options DiagramOptions) (*GraphData, error)
	ToMermaidGraph(data *GraphData) (string, error)
}
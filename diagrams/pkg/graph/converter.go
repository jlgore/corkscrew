package graph

import (
	"context"
	"fmt"
	"strings"

	"github.com/jlgore/corkscrew/diagrams/pkg/renderer"
	"github.com/jlgore/corkscrew/internal/db"
)

// GraphConverter converts DuckDB data to diagram format
type GraphConverter struct {
	graphLoader *db.GraphLoader
}

// NewGraphConverter creates a new graph converter
func NewGraphConverter(graphLoader *db.GraphLoader) *GraphConverter {
	return &GraphConverter{
		graphLoader: graphLoader,
	}
}

// ConvertToGraphData converts database data to GraphData format
func (gc *GraphConverter) ConvertToGraphData(ctx context.Context, options renderer.DiagramOptions) (*renderer.GraphData, error) {
	switch options.Type {
	case renderer.DiagramTypeRelationship:
		return gc.convertResourceRelationships(ctx, options)
	case renderer.DiagramTypeDependency:
		return gc.convertResourceDependencies(ctx, options)
	case renderer.DiagramTypeNetwork:
		return gc.convertNetworkTopology(ctx, options)
	case renderer.DiagramTypeService:
		return gc.convertServiceMap(ctx, options)
	default:
		return nil, fmt.Errorf("unsupported diagram type: %v", options.Type)
	}
}

// convertResourceRelationships creates a resource relationship diagram
func (gc *GraphConverter) convertResourceRelationships(ctx context.Context, options renderer.DiagramOptions) (*renderer.GraphData, error) {
	var nodes []renderer.Node
	var edges []renderer.Edge
	
	if options.ResourceID != "" {
		// Get neighborhood around specific resource
		depth := options.Depth
		if depth <= 0 {
			depth = 2
		}
		
		neighborData, err := gc.graphLoader.GetResourceNeighborhood(ctx, options.ResourceID, depth)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource neighborhood: %w", err)
		}
		
		// Convert to nodes
		nodeMap := make(map[string]bool)
		for _, data := range neighborData {
			id := getString(data["id"])
			if id == "" || nodeMap[id] {
				continue
			}
			nodeMap[id] = true
			
			nodes = append(nodes, renderer.Node{
				ID:    id,
				Label: gc.formatNodeLabel(getString(data["name"]), getString(data["type"])),
				Type:  getString(data["type"]),
				Properties: map[string]string{
					"distance": fmt.Sprintf("%v", data["distance"]),
				},
			})
		}
		
		// Get relationships for these nodes
		edges, err = gc.getRelationshipsForNodes(ctx, nodeMap)
		if err != nil {
			return nil, fmt.Errorf("failed to get relationships: %w", err)
		}
		
	} else {
		// Get all resources with filters
		query := "SELECT id, type, name, region, arn FROM aws_resources WHERE 1=1"
		args := []interface{}{}
		
		// Apply filters
		if service, ok := options.Filters["service"]; ok {
			query += " AND service = ?"
			args = append(args, service)
		}
		if region, ok := options.Filters["region"]; ok {
			query += " AND region = ?"
			args = append(args, region)
		}
		if resourceType, ok := options.Filters["type"]; ok {
			query += " AND type = ?"
			args = append(args, resourceType)
		}
		
		query += " ORDER BY type, name LIMIT 50"
		
		resourceData, err := gc.graphLoader.Query(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to query resources: %w", err)
		}
		
		// Convert to nodes
		nodeMap := make(map[string]bool)
		for _, data := range resourceData {
			id := getString(data["id"])
			if id == "" {
				continue
			}
			nodeMap[id] = true
			
			nodes = append(nodes, renderer.Node{
				ID:    id,
				Label: gc.formatNodeLabel(getString(data["name"]), getString(data["type"])),
				Type:  getString(data["type"]),
				Properties: map[string]string{
					"region": getString(data["region"]),
					"arn":    getString(data["arn"]),
				},
			})
		}
		
		// Get relationships
		edges, err = gc.getRelationshipsForNodes(ctx, nodeMap)
		if err != nil {
			return nil, fmt.Errorf("failed to get relationships: %w", err)
		}
	}
	
	title := "Resource Relationships"
	if options.Title != "" {
		title = options.Title
	} else if options.ResourceID != "" {
		title = fmt.Sprintf("Relationships for %s", options.ResourceID)
	}
	
	return &renderer.GraphData{
		Nodes: nodes,
		Edges: edges,
		Title: title,
	}, nil
}

// convertResourceDependencies creates a dependency tree diagram
func (gc *GraphConverter) convertResourceDependencies(ctx context.Context, options renderer.DiagramOptions) (*renderer.GraphData, error) {
	if options.ResourceID == "" {
		return nil, fmt.Errorf("resource ID required for dependency diagram")
	}
	
	// Get dependencies (what this resource depends on)
	deps, err := gc.graphLoader.GetResourceDependencies(ctx, options.ResourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies: %w", err)
	}
	
	// Get dependents (what depends on this resource)
	dependents, err := gc.graphLoader.GetResourceDependents(ctx, options.ResourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependents: %w", err)
	}
	
	// Get the root resource info
	rootData, err := gc.graphLoader.Query(ctx, "SELECT id, type, name FROM aws_resources WHERE id = ?", options.ResourceID)
	if err != nil || len(rootData) == 0 {
		return nil, fmt.Errorf("resource not found: %s", options.ResourceID)
	}
	
	var nodes []renderer.Node
	var edges []renderer.Edge
	
	// Add root node
	root := rootData[0]
	nodes = append(nodes, renderer.Node{
		ID:    getString(root["id"]),
		Label: gc.formatNodeLabel(getString(root["name"]), getString(root["type"])),
		Type:  getString(root["type"]),
		Properties: map[string]string{
			"role": "root",
		},
	})
	
	// Add dependency nodes and edges
	for _, dep := range deps {
		targetID := getString(dep["target_id"])
		nodes = append(nodes, renderer.Node{
			ID:    targetID,
			Label: gc.formatNodeLabel(getString(dep["target_name"]), getString(dep["target_type"])),
			Type:  getString(dep["target_type"]),
			Properties: map[string]string{
				"role": "dependency",
			},
		})
		
		edges = append(edges, renderer.Edge{
			From:  options.ResourceID,
			To:    targetID,
			Label: getString(dep["relationship"]),
			Type:  "depends_on",
		})
	}
	
	// Add dependent nodes and edges
	for _, dependent := range dependents {
		sourceID := getString(dependent["source_id"])
		nodes = append(nodes, renderer.Node{
			ID:    sourceID,
			Label: gc.formatNodeLabel(getString(dependent["source_name"]), getString(dependent["source_type"])),
			Type:  getString(dependent["source_type"]),
			Properties: map[string]string{
				"role": "dependent",
			},
		})
		
		edges = append(edges, renderer.Edge{
			From:  sourceID,
			To:    options.ResourceID,
			Label: getString(dependent["relationship"]),
			Type:  "depends_on",
		})
	}
	
	title := fmt.Sprintf("Dependencies for %s", options.ResourceID)
	if options.Title != "" {
		title = options.Title
	}
	
	return &renderer.GraphData{
		Nodes: nodes,
		Edges: edges,
		Title: title,
	}, nil
}

// convertNetworkTopology creates a network topology diagram
func (gc *GraphConverter) convertNetworkTopology(ctx context.Context, options renderer.DiagramOptions) (*renderer.GraphData, error) {
	// Focus on network-related resources
	query := `
		SELECT id, type, name, region, parent_id
		FROM aws_resources 
		WHERE type IN ('vpc', 'subnet', 'security-group', 'route-table', 'internet-gateway', 'nat-gateway', 'instance', 'load-balancer')
	`
	args := []interface{}{}
	
	if region, ok := options.Filters["region"]; ok {
		query += " AND region = ?"
		args = append(args, region)
	}
	
	if vpc, ok := options.Filters["vpc"]; ok {
		query += " AND (id = ? OR parent_id = ?)"
		args = append(args, vpc, vpc)
	}
	
	query += " ORDER BY type, name"
	
	resourceData, err := gc.graphLoader.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query network resources: %w", err)
	}
	
	var nodes []renderer.Node
	nodeMap := make(map[string]bool)
	
	for _, data := range resourceData {
		id := getString(data["id"])
		if id == "" || nodeMap[id] {
			continue
		}
		nodeMap[id] = true
		
		nodes = append(nodes, renderer.Node{
			ID:    id,
			Label: gc.formatNodeLabel(getString(data["name"]), getString(data["type"])),
			Type:  getString(data["type"]),
			Properties: map[string]string{
				"region":    getString(data["region"]),
				"parent_id": getString(data["parent_id"]),
			},
		})
	}
	
	// Get relationships for network topology
	edges, err := gc.getRelationshipsForNodes(ctx, nodeMap)
	if err != nil {
		return nil, fmt.Errorf("failed to get network relationships: %w", err)
	}
	
	title := "Network Topology"
	if options.Title != "" {
		title = options.Title
	}
	
	return &renderer.GraphData{
		Nodes: nodes,
		Edges: edges,
		Title: title,
	}, nil
}

// convertServiceMap creates a service-level resource map
func (gc *GraphConverter) convertServiceMap(ctx context.Context, options renderer.DiagramOptions) (*renderer.GraphData, error) {
	// Get resource counts by service and type
	query := `
		SELECT service, type, COUNT(*) as count, 
		       MIN(region) as sample_region
		FROM aws_resources 
		WHERE service IS NOT NULL
	`
	args := []interface{}{}
	
	if service, ok := options.Filters["service"]; ok {
		query += " AND service = ?"
		args = append(args, service)
	}
	
	query += " GROUP BY service, type ORDER BY service, count DESC"
	
	serviceData, err := gc.graphLoader.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query service data: %w", err)
	}
	
	var nodes []renderer.Node
	var edges []renderer.Edge
	
	serviceNodes := make(map[string]bool)
	
	for _, data := range serviceData {
		service := getString(data["service"])
		resourceType := getString(data["type"])
		count := getInt(data["count"])
		region := getString(data["sample_region"])
		
		// Create service node if not exists
		serviceNodeID := fmt.Sprintf("service_%s", service)
		if !serviceNodes[serviceNodeID] {
			nodes = append(nodes, renderer.Node{
				ID:    serviceNodeID,
				Label: strings.ToUpper(service),
				Type:  "service",
				Properties: map[string]string{
					"service": service,
				},
			})
			serviceNodes[serviceNodeID] = true
		}
		
		// Create resource type node
		typeNodeID := fmt.Sprintf("%s_%s", service, resourceType)
		nodes = append(nodes, renderer.Node{
			ID:    typeNodeID,
			Label: fmt.Sprintf("%s (%d)", resourceType, count),
			Type:  resourceType,
			Properties: map[string]string{
				"service":      service,
				"count":        fmt.Sprintf("%d", count),
				"region":       region,
				"resource_type": resourceType,
			},
		})
		
		// Create edge from service to resource type
		edges = append(edges, renderer.Edge{
			From:  serviceNodeID,
			To:    typeNodeID,
			Label: fmt.Sprintf("%d", count),
			Type:  "contains",
		})
	}
	
	title := "Service Map"
	if options.Title != "" {
		title = options.Title
	}
	
	return &renderer.GraphData{
		Nodes: nodes,
		Edges: edges,
		Title: title,
	}, nil
}

// getRelationshipsForNodes gets all relationships between a set of nodes
func (gc *GraphConverter) getRelationshipsForNodes(ctx context.Context, nodeMap map[string]bool) ([]renderer.Edge, error) {
	if len(nodeMap) == 0 {
		return []renderer.Edge{}, nil
	}
	
	// Build IN clause for node IDs
	nodeIDs := make([]string, 0, len(nodeMap))
	for nodeID := range nodeMap {
		nodeIDs = append(nodeIDs, nodeID)
	}
	
	placeholders := strings.Repeat("?,", len(nodeIDs))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
	
	query := fmt.Sprintf(`
		SELECT from_id, to_id, relationship_type, properties
		FROM aws_relationships
		WHERE from_id IN (%s) AND to_id IN (%s)
	`, placeholders, placeholders)
	
	args := make([]interface{}, len(nodeIDs)*2)
	for i, nodeID := range nodeIDs {
		args[i] = nodeID
		args[i+len(nodeIDs)] = nodeID
	}
	
	relationshipData, err := gc.graphLoader.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	
	var edges []renderer.Edge
	for _, data := range relationshipData {
		fromID := getString(data["from_id"])
		toID := getString(data["to_id"])
		
		// Only include if both nodes are in our set
		if nodeMap[fromID] && nodeMap[toID] {
			edges = append(edges, renderer.Edge{
				From:  fromID,
				To:    toID,
				Label: "",
				Type:  getString(data["relationship_type"]),
				Properties: map[string]string{
					"properties": getString(data["properties"]),
				},
			})
		}
	}
	
	return edges, nil
}

// ToMermaidGraph converts GraphData to Mermaid format
func (gc *GraphConverter) ToMermaidGraph(data *renderer.GraphData) (string, error) {
	mermaidRenderer := renderer.NewMermaidRenderer()
	return mermaidRenderer.GraphDataToMermaid(data)
}

// formatNodeLabel creates a readable label for a node
func (gc *GraphConverter) formatNodeLabel(name, resourceType string) string {
	if name == "" {
		return resourceType
	}
	
	// Truncate long names
	if len(name) > 30 {
		name = name[:27] + "..."
	}
	
	return fmt.Sprintf("%s\n(%s)", name, resourceType)
}

// Helper functions
func getString(value interface{}) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return fmt.Sprintf("%v", value)
}

func getInt(value interface{}) int {
	if value == nil {
		return 0
	}
	if i, ok := value.(int); ok {
		return i
	}
	if i64, ok := value.(int64); ok {
		return int(i64)
	}
	return 0
}
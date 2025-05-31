package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jlgore/corkscrew/diagrams/pkg/graph"
	"github.com/jlgore/corkscrew/diagrams/pkg/renderer"
	"github.com/jlgore/corkscrew/diagrams/pkg/ui"
	"github.com/jlgore/corkscrew/internal/db"
)

func main() {
	var (
		dbPath     = flag.String("db", "corkscrew.db", "Path to DuckDB database")
		resourceID = flag.String("resource", "", "Specific resource ID to visualize")
		diagramType = flag.String("type", "relationships", "Diagram type: relationships, dependencies, network, services")
		depth      = flag.Int("depth", 2, "Depth for relationship traversal")
		export     = flag.String("export", "", "Export to file (mermaid.md, ascii.txt)")
		service    = flag.String("service", "", "Filter by service")
		region     = flag.String("region", "", "Filter by region")
		title      = flag.String("title", "", "Custom diagram title")
		help       = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		showHelp()
		return
	}

	// Initialize database connection
	graphLoader, err := db.NewGraphLoader(*dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer graphLoader.Close()

	// Parse diagram type
	var diagType renderer.DiagramType
	switch *diagramType {
	case "relationships", "rel":
		diagType = renderer.DiagramTypeRelationship
	case "dependencies", "deps":
		diagType = renderer.DiagramTypeDependency
	case "network", "net":
		diagType = renderer.DiagramTypeNetwork
	case "services", "svc":
		diagType = renderer.DiagramTypeService
	default:
		fmt.Printf("Unknown diagram type: %s\n", *diagramType)
		showHelp()
		os.Exit(1)
	}

	// Build options
	options := renderer.DiagramOptions{
		Type:       diagType,
		ResourceID: *resourceID,
		Depth:      *depth,
		Title:      *title,
		Filters:    make(map[string]string),
	}

	if *service != "" {
		options.Filters["service"] = *service
	}
	if *region != "" {
		options.Filters["region"] = *region
	}

	// If export is specified, generate and export diagram
	if *export != "" {
		err := exportDiagram(graphLoader, options, *export)
		if err != nil {
			log.Fatalf("Failed to export diagram: %v", err)
		}
		fmt.Printf("Diagram exported to: %s\n", *export)
		return
	}

	// Launch interactive viewer
	model := ui.NewDiagramModelWithOptions(graphLoader, options)
	
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := program.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}

func exportDiagram(graphLoader *db.GraphLoader, options renderer.DiagramOptions, filename string) error {
	ctx := context.Background()
	
	// Create converter and generate data
	converter := graph.NewGraphConverter(graphLoader)
	data, err := converter.ConvertToGraphData(ctx, options)
	if err != nil {
		return fmt.Errorf("failed to generate diagram data: %w", err)
	}

	var content string
	
	// Determine export format based on filename extension
	if strings.HasSuffix(filename, ".md") || strings.HasSuffix(filename, ".mmd") {
		// Export as Mermaid
		content, err = converter.ToMermaidGraph(data)
		if err != nil {
			return fmt.Errorf("failed to generate Mermaid diagram: %w", err)
		}
		
		// Wrap in markdown code block
		content = "```mermaid\n" + content + "\n```\n"
		
	} else {
		// Export as ASCII
		asciiRenderer := renderer.NewASCIIRenderer()
		asciiRenderer.SetDimensions(120, 80) // Set reasonable dimensions for export
		
		content, err = asciiRenderer.RenderASCII(data)
		if err != nil {
			return fmt.Errorf("failed to generate ASCII diagram: %w", err)
		}
	}

	// Write to file
	return os.WriteFile(filename, []byte(content), 0644)
}

func showHelp() {
	fmt.Printf(`Corkscrew Diagram Viewer

USAGE:
    diagram [FLAGS]

FLAGS:
    -db <path>         Path to DuckDB database (default: corkscrew.db)
    -resource <id>     Specific resource ID to visualize
    -type <type>       Diagram type: relationships, dependencies, network, services
    -depth <int>       Depth for relationship traversal (default: 2)
    -export <file>     Export to file (.md for Mermaid, .txt for ASCII)
    -service <name>    Filter by service
    -region <name>     Filter by region
    -title <string>    Custom diagram title
    -help              Show this help

DIAGRAM TYPES:
    relationships      Show resource relationships (default)
    dependencies       Show resource dependencies (requires -resource)
    network           Show network topology
    services          Show service map with resource counts

EXAMPLES:
    # Launch interactive viewer
    diagram

    # View relationships for a specific resource
    diagram -resource vpc-123456789 -type relationships

    # View dependencies for an EC2 instance
    diagram -resource i-0123456789abcdef0 -type dependencies

    # Show network topology for a region
    diagram -type network -region us-east-1

    # Export service map to Mermaid file
    diagram -type services -export services.md

    # Show relationships with increased depth
    diagram -resource vpc-123456789 -depth 3

INTERACTIVE CONTROLS:
    q/Ctrl+C     Quit
    h/?          Show help
    r            Refresh data
    1            ASCII view
    2            Mermaid source view
    3            List view
    n            Relationships diagram
    d            Dependencies diagram (requires resource)
    t            Network topology diagram
    s            Services diagram
    +/-          Increase/decrease depth
    ↑/↓, j/k     Scroll
    PgUp/PgDn    Page up/down
`)
}
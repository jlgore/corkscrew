package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jlgore/corkscrew/diagrams/pkg/graph"
	"github.com/jlgore/corkscrew/diagrams/pkg/renderer"
	"github.com/jlgore/corkscrew/internal/db"
)

// DiagramModel represents the main BubbleTea model for diagram viewing
type DiagramModel struct {
	// Core components
	viewport     viewport.Model
	graphLoader  *db.GraphLoader
	converter    *graph.GraphConverter
	renderer     renderer.DiagramRenderer
	
	// State
	currentData  *renderer.GraphData
	currentView  ViewType
	options      renderer.DiagramOptions
	
	// UI state
	width        int
	height       int
	ready        bool
	error        string
	loading      bool
	
	// Interaction state
	selectedNode string
	showHelp     bool
	
	// Styles
	titleStyle   lipgloss.Style
	errorStyle   lipgloss.Style
	helpStyle    lipgloss.Style
	statusStyle  lipgloss.Style
}

// ViewType represents different diagram view modes
type ViewType int

const (
	ViewTypeASCII ViewType = iota
	ViewTypeMermaid
	ViewTypeList
)

// NewDiagramModel creates a new diagram model
func NewDiagramModel(graphLoader *db.GraphLoader) *DiagramModel {
	converter := graph.NewGraphConverter(graphLoader)
	asciiRenderer := renderer.NewASCIIRenderer()
	
	return &DiagramModel{
		graphLoader: graphLoader,
		converter:   converter,
		renderer:    asciiRenderer,
		currentView: ViewTypeASCII,
		options: renderer.DiagramOptions{
			Type:  renderer.DiagramTypeRelationship,
			Depth: 2,
		},
		titleStyle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
		errorStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		helpStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		statusStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	}
}

// NewDiagramModelWithOptions creates a diagram model with specific options
func NewDiagramModelWithOptions(graphLoader *db.GraphLoader, options renderer.DiagramOptions) *DiagramModel {
	model := NewDiagramModel(graphLoader)
	model.options = options
	return model
}

// Init initializes the model
func (m DiagramModel) Init() tea.Cmd {
	return tea.Batch(
		tea.WindowSize(),
		m.loadDiagramData(),
	)
}

// Update handles messages and updates the model
func (m DiagramModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := 3
		footerHeight := 3
		verticalMarginHeight := headerHeight + footerHeight
		
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
		}
		
		m.width = msg.Width
		m.height = msg.Height
		
		// Update renderer dimensions
		m.renderer.SetDimensions(m.viewport.Width, m.viewport.Height)
		
		// Re-render with new dimensions
		if m.currentData != nil {
			cmds = append(cmds, m.renderCurrentData())
		}
		
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
			
		case "h", "?":
			m.showHelp = !m.showHelp
			cmds = append(cmds, m.renderCurrentData())
			
		case "r":
			// Refresh/reload data
			m.loading = true
			cmds = append(cmds, m.loadDiagramData())
			
		case "1":
			// Switch to ASCII view
			m.currentView = ViewTypeASCII
			m.renderer = renderer.NewASCIIRenderer()
			m.renderer.SetDimensions(m.viewport.Width, m.viewport.Height)
			cmds = append(cmds, m.renderCurrentData())
			
		case "2":
			// Switch to Mermaid view
			m.currentView = ViewTypeMermaid
			m.renderer = renderer.NewMermaidRenderer()
			m.renderer.SetDimensions(m.viewport.Width, m.viewport.Height)
			cmds = append(cmds, m.renderCurrentData())
			
		case "3":
			// Switch to List view
			m.currentView = ViewTypeList
			cmds = append(cmds, m.renderCurrentData())
			
		case "n":
			// Switch diagram type to relationship
			m.options.Type = renderer.DiagramTypeRelationship
			m.loading = true
			cmds = append(cmds, m.loadDiagramData())
			
		case "d":
			// Switch diagram type to dependency
			if m.options.ResourceID != "" {
				m.options.Type = renderer.DiagramTypeDependency
				m.loading = true
				cmds = append(cmds, m.loadDiagramData())
			}
			
		case "t":
			// Switch diagram type to network topology
			m.options.Type = renderer.DiagramTypeNetwork
			m.loading = true
			cmds = append(cmds, m.loadDiagramData())
			
		case "s":
			// Switch diagram type to service map
			m.options.Type = renderer.DiagramTypeService
			m.loading = true
			cmds = append(cmds, m.loadDiagramData())
			
		case "+", "=":
			// Increase depth
			if m.options.Depth < 5 {
				m.options.Depth++
				m.loading = true
				cmds = append(cmds, m.loadDiagramData())
			}
			
		case "-":
			// Decrease depth
			if m.options.Depth > 1 {
				m.options.Depth--
				m.loading = true
				cmds = append(cmds, m.loadDiagramData())
			}
		}
		
	case DiagramDataMsg:
		m.loading = false
		m.currentData = msg.Data
		m.error = ""
		cmds = append(cmds, m.renderCurrentData())
		
	case DiagramErrorMsg:
		m.loading = false
		m.error = msg.Error
		
	case DiagramContentMsg:
		m.viewport.SetContent(msg.Content)
	}
	
	// Update viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	
	return m, tea.Batch(cmds...)
}

// View renders the model
func (m DiagramModel) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	
	// Header
	header := m.renderHeader()
	
	// Footer
	footer := m.renderFooter()
	
	// Main content
	content := m.viewport.View()
	
	// Help overlay
	if m.showHelp {
		content = m.renderHelpOverlay(content)
	}
	
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderHeader renders the header section
func (m DiagramModel) renderHeader() string {
	title := "Corkscrew - Resource Diagrams"
	if m.currentData != nil && m.currentData.Title != "" {
		title = m.currentData.Title
	}
	
	var status string
	if m.loading {
		status = m.statusStyle.Render("Loading...")
	} else if m.error != "" {
		status = m.errorStyle.Render(fmt.Sprintf("Error: %s", m.error))
	} else if m.currentData != nil {
		status = m.statusStyle.Render(fmt.Sprintf("Nodes: %d, Edges: %d", len(m.currentData.Nodes), len(m.currentData.Edges)))
	}
	
	titleLine := m.titleStyle.Render(title)
	statusLine := status
	
	headerContent := lipgloss.JoinVertical(lipgloss.Left, titleLine, statusLine)
	
	return lipgloss.NewStyle().
		Width(m.width).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		Render(headerContent)
}

// renderFooter renders the footer with controls
func (m DiagramModel) renderFooter() string {
	controls := []string{
		"q: quit",
		"r: refresh",
		"h: help",
		"1: ASCII",
		"2: Mermaid",
		"3: List",
	}
	
	if m.options.ResourceID != "" {
		controls = append(controls, "d: dependencies")
	}
	
	controls = append(controls, "n: relationships", "t: topology", "s: services")
	
	if m.currentView == ViewTypeASCII || m.currentView == ViewTypeMermaid {
		controls = append(controls, "+/-: depth")
	}
	
	controlsText := lipgloss.JoinHorizontal(lipgloss.Left, controls...)
	
	return lipgloss.NewStyle().
		Width(m.width).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		Render(m.helpStyle.Render(controlsText))
}

// renderHelpOverlay renders the help overlay
func (m DiagramModel) renderHelpOverlay(content string) string {
	helpText := `
Corkscrew Diagram Viewer Help

Navigation:
  ↑/↓, j/k     Scroll up/down
  PgUp/PgDn    Page up/down
  Home/End     Go to top/bottom

Views:
  1            ASCII diagram view
  2            Mermaid source view
  3            Simple list view

Diagram Types:
  n            Resource relationships
  d            Dependencies (requires resource ID)
  t            Network topology
  s            Service map

Options:
  +/-          Increase/decrease depth
  r            Refresh data
  h/?          Toggle this help
  q/Ctrl+C     Quit

Current Settings:
  Type:        %s
  Depth:       %d
  Resource:    %s
`
	
	diagramType := "Relationships"
	switch m.options.Type {
	case renderer.DiagramTypeDependency:
		diagramType = "Dependencies"
	case renderer.DiagramTypeNetwork:
		diagramType = "Network"
	case renderer.DiagramTypeService:
		diagramType = "Service Map"
	}
	
	resourceID := m.options.ResourceID
	if resourceID == "" {
		resourceID = "All"
	}
	
	help := fmt.Sprintf(helpText, diagramType, m.options.Depth, resourceID)
	
	helpBox := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Foreground(lipgloss.Color("252")).
		Padding(1, 2).
		Margin(2, 4).
		Border(lipgloss.RoundedBorder()).
		Render(help)
	
	// Center the help box
	return lipgloss.Place(m.width, m.height-6, lipgloss.Center, lipgloss.Center, helpBox)
}

// Commands

// loadDiagramData loads diagram data asynchronously
func (m DiagramModel) loadDiagramData() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		data, err := m.converter.ConvertToGraphData(ctx, m.options)
		if err != nil {
			return DiagramErrorMsg{Error: err.Error()}
		}
		return DiagramDataMsg{Data: data}
	}
}

// renderCurrentData renders the current data
func (m DiagramModel) renderCurrentData() tea.Cmd {
	return func() tea.Msg {
		if m.currentData == nil {
			return DiagramContentMsg{Content: "No data to display"}
		}
		
		var content string
		var err error
		
		switch m.currentView {
		case ViewTypeASCII:
			content, err = m.renderer.RenderASCII(m.currentData)
		case ViewTypeMermaid:
			content, err = m.converter.ToMermaidGraph(m.currentData)
		case ViewTypeList:
			asciiRenderer := renderer.NewASCIIRenderer()
			content = asciiRenderer.RenderSimpleList(m.currentData)
		default:
			content, err = m.renderer.RenderASCII(m.currentData)
		}
		
		if err != nil {
			return DiagramErrorMsg{Error: err.Error()}
		}
		
		return DiagramContentMsg{Content: content}
	}
}

// Messages

// DiagramDataMsg carries loaded diagram data
type DiagramDataMsg struct {
	Data *renderer.GraphData
}

// DiagramErrorMsg carries error information
type DiagramErrorMsg struct {
	Error string
}

// DiagramContentMsg carries rendered content
type DiagramContentMsg struct {
	Content string
}
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jlgore/corkscrew/internal/tui/styles"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

// BreadcrumbModel represents navigation breadcrumbs
type BreadcrumbModel struct {
	path      []BreadcrumbItem
	separator string
	maxWidth  int
	showIcons bool
}

// BreadcrumbItem represents a single breadcrumb
type BreadcrumbItem struct {
	Label    string
	Icon     string
	ViewType types.ViewType
	Active   bool
	Data     interface{} // Additional data for the breadcrumb
}

// NewBreadcrumbModel creates a new breadcrumb model
func NewBreadcrumbModel() BreadcrumbModel {
	return BreadcrumbModel{
		path:      []BreadcrumbItem{},
		separator: " > ",
		maxWidth:  80,
		showIcons: true,
	}
}

// AddCrumb adds a new breadcrumb to the path
func (m *BreadcrumbModel) AddCrumb(item BreadcrumbItem) {
	// Set all existing items as inactive
	for i := range m.path {
		m.path[i].Active = false
	}
	
	// Add new item as active
	item.Active = true
	m.path = append(m.path, item)
}

// PopCrumb removes the last breadcrumb from the path
func (m *BreadcrumbModel) PopCrumb() *BreadcrumbItem {
	if len(m.path) == 0 {
		return nil
	}
	
	// Get the last item
	last := m.path[len(m.path)-1]
	
	// Remove it from the path
	m.path = m.path[:len(m.path)-1]
	
	// Set the new last item as active if it exists
	if len(m.path) > 0 {
		m.path[len(m.path)-1].Active = true
	}
	
	return &last
}

// SetActiveCrumb sets a specific breadcrumb as active
func (m *BreadcrumbModel) SetActiveCrumb(index int) {
	if index < 0 || index >= len(m.path) {
		return
	}
	
	// Set all items as inactive
	for i := range m.path {
		m.path[i].Active = false
	}
	
	// Set the specified item as active
	m.path[index].Active = true
}

// NavigateToView updates breadcrumbs when switching views
func (m *BreadcrumbModel) NavigateToView(viewType types.ViewType, label string, data interface{}) {
	// Check if we're going back to an existing breadcrumb
	for i, crumb := range m.path {
		if crumb.ViewType == viewType {
			// Truncate path at this point and set as active
			m.path = m.path[:i+1]
			m.SetActiveCrumb(i)
			return
		}
	}
	
	// Add new breadcrumb
	icon := getViewIcon(viewType)
	m.AddCrumb(BreadcrumbItem{
		Label:    label,
		Icon:     icon,
		ViewType: viewType,
		Active:   true,
		Data:     data,
	})
}

// Clear removes all breadcrumbs
func (m *BreadcrumbModel) Clear() {
	m.path = []BreadcrumbItem{}
}

// GetCurrentCrumb returns the currently active breadcrumb
func (m BreadcrumbModel) GetCurrentCrumb() *BreadcrumbItem {
	for i := len(m.path) - 1; i >= 0; i-- {
		if m.path[i].Active {
			return &m.path[i]
		}
	}
	return nil
}

// GetPath returns the full breadcrumb path
func (m BreadcrumbModel) GetPath() []BreadcrumbItem {
	return m.path
}

// View renders the breadcrumb navigation
func (m BreadcrumbModel) View() string {
	if len(m.path) == 0 {
		return ""
	}
	
	// Render each breadcrumb
	var segments []string
	totalWidth := 0
	
	for i, crumb := range m.path {
		segment := m.renderCrumb(crumb)
		segmentWidth := lipgloss.Width(segment)
		
		// Check if adding this segment would exceed max width
		if totalWidth+segmentWidth > m.maxWidth && i > 0 {
			// Add ellipsis and break
			segments = append(segments, styles.BreadcrumbStyle.Render("..."))
			break
		}
		
		segments = append(segments, segment)
		totalWidth += segmentWidth
		
		// Add separator if not the last item
		if i < len(m.path)-1 {
			separatorWidth := lipgloss.Width(m.separator)
			if totalWidth+separatorWidth <= m.maxWidth {
				segments = append(segments, styles.BreadcrumbSeparatorStyle.Render(m.separator))
				totalWidth += separatorWidth
			}
		}
	}
	
	return strings.Join(segments, "")
}

// renderCrumb renders a single breadcrumb item
func (m BreadcrumbModel) renderCrumb(crumb BreadcrumbItem) string {
	var text string
	
	// Add icon if enabled
	if m.showIcons && crumb.Icon != "" {
		text = crumb.Icon + " "
	}
	
	// Add label
	text += crumb.Label
	
	// Style based on active state
	if crumb.Active {
		return styles.BreadcrumbActiveStyle.Render(text)
	}
	
	return styles.BreadcrumbStyle.Render(text)
}

// SetMaxWidth sets the maximum width for the breadcrumb
func (m *BreadcrumbModel) SetMaxWidth(width int) {
	m.maxWidth = width
}

// SetSeparator sets the separator between breadcrumbs
func (m *BreadcrumbModel) SetSeparator(separator string) {
	m.separator = separator
}

// SetShowIcons enables or disables icon display
func (m *BreadcrumbModel) SetShowIcons(show bool) {
	m.showIcons = show
}

// Helper function to get the appropriate icon for a view
func getViewIcon(viewType types.ViewType) string {
	switch viewType {
	case types.ViewMain:
		return "🏠" // Home
	case types.ViewScan:
		return styles.IconScan
	case types.ViewResults:
		return styles.IconResults
	case types.ViewConfig:
		return styles.IconConfig
	case types.ViewDiagrams:
		return styles.IconDiagrams
	case types.ViewCompliance:
		return styles.IconCompliance
	case types.ViewQuery:
		return styles.IconQuery
	default:
		return ""
	}
}

// GetViewLabel returns a human-readable label for a view type
func GetViewLabel(viewType types.ViewType) string {
	switch viewType {
	case types.ViewMain:
		return "Home"
	case types.ViewScan:
		return "Scan"
	case types.ViewResults:
		return "Results"
	case types.ViewConfig:
		return "Configuration"
	case types.ViewDiagrams:
		return "Diagrams"
	case types.ViewCompliance:
		return "Compliance"
	case types.ViewQuery:
		return "Query"
	default:
		return "Unknown"
	}
}

// CreateViewBreadcrumb creates a breadcrumb item for a view
func CreateViewBreadcrumb(viewType types.ViewType, data interface{}) BreadcrumbItem {
	return BreadcrumbItem{
		Label:    GetViewLabel(viewType),
		Icon:     getViewIcon(viewType),
		ViewType: viewType,
		Active:   false,
		Data:     data,
	}
}
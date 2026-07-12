package views

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jlgore/corkscrew/internal/tui/styles"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

// SummaryViewModel is a lightweight destination view used while feature-specific
// TUI workflows are built out.
type SummaryViewModel struct {
	state    ViewState
	subtitle string
	sections []SummarySection
}

// SummarySection represents one section in a summary view.
type SummarySection struct {
	Title string
	Items []string
}

// NewSummaryViewModel creates a lightweight view for a routed TUI destination.
func NewSummaryViewModel(viewType types.ViewType, title, subtitle string, sections []SummarySection) *SummaryViewModel {
	return &SummaryViewModel{
		state:    NewViewState(viewType, title),
		subtitle: subtitle,
		sections: sections,
	}
}

// Init initializes the view.
func (m *SummaryViewModel) Init() tea.Cmd {
	return nil
}

// Update handles basic view messages.
func (m *SummaryViewModel) Update(msg tea.Msg) (BaseView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Resize(msg.Width, msg.Height)
	}
	return m, nil
}

// View renders the summary view.
func (m *SummaryViewModel) View() string {
	width := m.state.Width - 4
	height := m.state.Height - 2
	if width < 40 {
		width = 40
	}
	if height < 10 {
		height = 10
	}

	content := []string{
		styles.TitleStyle.Render(m.state.Title),
		styles.SubtitleStyle.Render(m.subtitle),
		"",
	}

	for sectionIndex, section := range m.sections {
		content = append(content, styles.InfoStyle.Render(section.Title))
		for _, item := range section.Items {
			content = append(content, fmt.Sprintf("  %s", item))
		}
		if sectionIndex < len(m.sections)-1 {
			content = append(content, "")
		}
	}

	border := styles.BorderStyle
	if m.state.Focused {
		border = styles.FocusedBorderStyle
	}

	return border.
		Width(width).
		Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

// Title returns the view title.
func (m *SummaryViewModel) Title() string {
	return m.state.Title
}

// ShortHelp returns the compact key help.
func (m *SummaryViewModel) ShortHelp() []string {
	return []string{"esc back", "? help", "q quit"}
}

// FullHelp returns detailed key help.
func (m *SummaryViewModel) FullHelp() [][]string {
	return [][]string{
		{"esc", "back"},
		{"?", "help"},
		{"q", "quit"},
	}
}

// Focus marks the view as active.
func (m *SummaryViewModel) Focus() {
	m.state.Focused = true
}

// Blur marks the view as inactive.
func (m *SummaryViewModel) Blur() {
	m.state.Focused = false
}

// Resize updates the rendered dimensions.
func (m *SummaryViewModel) Resize(width, height int) {
	m.state.Width = width
	m.state.Height = height
}

// ViewType returns the routed view type.
func (m *SummaryViewModel) ViewType() types.ViewType {
	return m.state.ViewType
}

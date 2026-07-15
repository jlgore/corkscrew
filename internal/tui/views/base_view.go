package views

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

// BaseView defines the interface that all TUI views must implement
type BaseView interface {
	// Init initializes the view (called when view is first created)
	Init() tea.Cmd

	// Update handles messages and updates the view state
	Update(msg tea.Msg) (BaseView, tea.Cmd)

	// View renders the view to a string
	View() string

	// Title returns the display title for this view
	Title() string

	// ShortHelp returns key bindings for the mini help
	ShortHelp() []string

	// FullHelp returns detailed help information
	FullHelp() [][]string

	// Focus sets the view as focused/unfocused
	Focus()
	Blur()

	// Resize updates the view dimensions
	Resize(width, height int)

	// ViewType returns the type of this view
	ViewType() types.ViewType
}

// ViewState represents common state that all views share
type ViewState struct {
	Width    int
	Height   int
	Focused  bool
	Title    string
	ViewType types.ViewType
}

// NewViewState creates a new view state
func NewViewState(viewType types.ViewType, title string) ViewState {
	return ViewState{
		ViewType: viewType,
		Title:    title,
		Focused:  false,
		Width:    80,
		Height:   24,
	}
}

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

// Common view helper functions

// HandleCommonKeys processes standard key bindings common to all views
func HandleCommonKeys(msg tea.KeyMsg) tea.Cmd {
	switch {
	case msg.String() == "ctrl+c":
		return tea.Quit
	case msg.String() == "q":
		return tea.Quit
	case msg.String() == "?":
		return func() tea.Msg { return types.ToggleHelpMsg{} }
	case msg.String() == "esc":
		return func() tea.Msg { return types.BackMsg{} }
	}
	return nil
}

// HandleViewSwitching processes view switching key bindings
func HandleViewSwitching(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "1", "ctrl+1":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewMain}
		}
	case "2", "ctrl+2":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewScan}
		}
	case "3", "ctrl+3":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewResults}
		}
	case "4", "ctrl+4":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewConfig}
		}
	case "5", "ctrl+5":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewDiagrams}
		}
	case "6", "ctrl+6":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewQuery}
		}
	case "7", "ctrl+7":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewCompliance}
		}
	}
	return nil
}

// ViewContainer wraps a view with common functionality
type ViewContainer struct {
	view     BaseView
	state    ViewState
	showHelp bool
}

// NewViewContainer creates a new view container
func NewViewContainer(view BaseView) *ViewContainer {
	return &ViewContainer{
		view:     view,
		state:    NewViewState(view.ViewType(), view.Title()),
		showHelp: false,
	}
}

// Init initializes the container and wrapped view
func (vc *ViewContainer) Init() tea.Cmd {
	return vc.view.Init()
}

// Update handles messages for the container and delegates to the wrapped view
func (vc *ViewContainer) Update(msg tea.Msg) (*ViewContainer, tea.Cmd) {
	var cmd tea.Cmd

	// Handle container-level messages first
	switch msg := msg.(type) {
	case types.WindowSizeMsg:
		vc.state.Width = msg.Width
		vc.state.Height = msg.Height
		vc.view.Resize(msg.Width, msg.Height)
		return vc, nil

	case types.ToggleHelpMsg:
		vc.showHelp = !vc.showHelp
		return vc, nil

	case tea.KeyMsg:
		// Handle common keys first
		if cmd = HandleCommonKeys(msg); cmd != nil {
			return vc, cmd
		}

		// Handle view switching
		if cmd = HandleViewSwitching(msg); cmd != nil {
			return vc, cmd
		}
	}

	// Delegate to the wrapped view
	vc.view, cmd = vc.view.Update(msg)
	return vc, cmd
}

// View renders the container and wrapped view
func (vc *ViewContainer) View() string {
	if vc.showHelp {
		return vc.renderHelpView()
	}
	return vc.view.View()
}

// renderHelpView renders the help overlay
func (vc *ViewContainer) renderHelpView() string {
	// This would render a help overlay over the current view
	// For now, just return the regular view
	return vc.view.View()
}

// Focus sets the container as focused
func (vc *ViewContainer) Focus() {
	vc.state.Focused = true
	vc.view.Focus()
}

// Blur sets the container as unfocused
func (vc *ViewContainer) Blur() {
	vc.state.Focused = false
	vc.view.Blur()
}

// GetView returns the wrapped view
func (vc *ViewContainer) GetView() BaseView {
	return vc.view
}

// GetState returns the container state
func (vc *ViewContainer) GetState() ViewState {
	return vc.state
}

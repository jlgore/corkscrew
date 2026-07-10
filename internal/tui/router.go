package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jlgore/corkscrew/internal/tui/views"
)

// ViewRouter manages navigation between different TUI views
type ViewRouter struct {
	// Current state
	currentView ViewType
	views       map[ViewType]views.BaseView
	history     []ViewType

	// Dependencies
	database interface{} // *db.GraphLoader
	config   interface{} // *config.Config
	scanner  interface{}

	// Layout state
	width  int
	height int
}

// NewViewRouter creates a new view router
func NewViewRouter() *ViewRouter {
	router := &ViewRouter{
		currentView: ViewMain,
		views:       make(map[ViewType]views.BaseView),
		history:     []ViewType{},
		width:       80,
		height:      24,
	}

	// Initialize main menu view immediately
	router.views[ViewMain] = views.NewMainMenuModel()

	return router
}

// SetDependencies sets the required dependencies
func (r *ViewRouter) SetDependencies(database, config, scanner interface{}) {
	r.database = database
	r.config = config
	r.scanner = scanner

	// Update existing views with dependencies
	for _, view := range r.views {
		r.injectDependencies(view)
	}
}

// SwitchView switches to a specific view
func (r *ViewRouter) SwitchView(viewType ViewType, data interface{}) tea.Cmd {
	// Add current view to history (unless switching to same view)
	if viewType != r.currentView {
		r.history = append(r.history, r.currentView)
	}

	// Blur current view
	if currentView, exists := r.views[r.currentView]; exists {
		currentView.Blur()
	}

	// Create view if it doesn't exist
	if _, exists := r.views[viewType]; !exists {
		view := r.createView(viewType)
		if view != nil {
			r.views[viewType] = view
			r.injectDependencies(view)
		}
	}

	// Switch to new view
	r.currentView = viewType

	// Focus new view and initialize if needed
	var cmd tea.Cmd
	if newView, exists := r.views[viewType]; exists {
		newView.Focus()
		newView.Resize(r.width, r.height)

		// Handle view-specific data
		if data != nil {
			cmd = r.handleViewData(viewType, data)
		}

		// Initialize view if this is the first time
		initCmd := newView.Init()
		if initCmd != nil && cmd != nil {
			return tea.Batch(cmd, initCmd)
		} else if initCmd != nil {
			return initCmd
		}
	}

	return cmd
}

// GoBack navigates to the previous view in history
func (r *ViewRouter) GoBack() tea.Cmd {
	if len(r.history) == 0 {
		return nil
	}

	// Get previous view from history
	previousView := r.history[len(r.history)-1]
	r.history = r.history[:len(r.history)-1]

	// Switch to previous view without adding to history
	oldCurrent := r.currentView
	r.currentView = previousView

	// Blur old view, focus new view
	if oldView, exists := r.views[oldCurrent]; exists {
		oldView.Blur()
	}
	if newView, exists := r.views[r.currentView]; exists {
		newView.Focus()
		newView.Resize(r.width, r.height)
	}

	return nil
}

// GetCurrentView returns the currently active view
func (r *ViewRouter) GetCurrentView() views.BaseView {
	if view, exists := r.views[r.currentView]; exists {
		return view
	}
	return nil
}

// GetCurrentViewType returns the current view type
func (r *ViewRouter) GetCurrentViewType() ViewType {
	return r.currentView
}

// Update handles messages for the current view
func (r *ViewRouter) Update(msg tea.Msg) tea.Cmd {
	// Handle routing messages first
	switch msg := msg.(type) {
	case SwitchViewMsg:
		return r.SwitchView(msg.View, msg.Data)
	case BackMsg:
		return r.GoBack()
	case WindowSizeMsg:
		r.Resize(msg.Width, msg.Height)
	}

	// Delegate to current view
	if currentView, exists := r.views[r.currentView]; exists {
		updatedView, cmd := currentView.Update(msg)
		r.views[r.currentView] = updatedView
		return cmd
	}

	return nil
}

// View renders the current view
func (r *ViewRouter) View() string {
	if currentView, exists := r.views[r.currentView]; exists {
		return currentView.View()
	}
	return "No view available"
}

// Resize updates the router and all views with new dimensions
func (r *ViewRouter) Resize(width, height int) {
	r.width = width
	r.height = height

	// Resize all existing views
	for _, view := range r.views {
		view.Resize(width, height)
	}
}

// GetHistory returns the navigation history
func (r *ViewRouter) GetHistory() []ViewType {
	return r.history
}

// ClearHistory clears the navigation history
func (r *ViewRouter) ClearHistory() {
	r.history = []ViewType{}
}

// createView creates a new view instance based on the view type
func (r *ViewRouter) createView(viewType ViewType) views.BaseView {
	switch viewType {
	case ViewMain:
		return views.NewMainMenuModel()
	case ViewScan:
		return r.createScanView()
	case ViewResults:
		return r.createResultsView()
	case ViewConfig:
		return r.createConfigView()
	case ViewDiagrams:
		return r.createDiagramsView()
	case ViewQuery:
		return r.createQueryView()
	case ViewCompliance:
		return r.createComplianceView()
	default:
		// Return a placeholder or error view
		return nil
	}
}

// Placeholder view creation methods (to be implemented)
func (r *ViewRouter) createScanView() views.BaseView {
	// TODO: Implement scan view
	return nil
}

func (r *ViewRouter) createResultsView() views.BaseView {
	// TODO: Implement results view
	return nil
}

func (r *ViewRouter) createConfigView() views.BaseView {
	// TODO: Implement config view
	return nil
}

func (r *ViewRouter) createDiagramsView() views.BaseView {
	// TODO: Implement diagrams view wrapper
	return nil
}

func (r *ViewRouter) createQueryView() views.BaseView {
	// TODO: Implement query view
	return nil
}

func (r *ViewRouter) createComplianceView() views.BaseView {
	// TODO: Implement compliance view
	return nil
}

// injectDependencies injects dependencies into a view
func (r *ViewRouter) injectDependencies(view views.BaseView) {
	// Use type assertion to inject dependencies into specific view types
	// This is a simplified approach - in a real implementation, you might
	// want to use interfaces or dependency injection patterns

	switch v := view.(type) {
	case *views.MainMenuModel:
		// Inject database and config into main menu
		if r.database != nil {
			// v.SetDatabase(r.database)
			_ = v // Avoid unused variable error
		}
		if r.config != nil {
			// v.SetConfig(r.config)
		}
	default:
		// Handle other view types as they're implemented
	}
}

// handleViewData processes view-specific data when switching views
func (r *ViewRouter) handleViewData(viewType ViewType, data interface{}) tea.Cmd {
	switch viewType {
	case ViewScan:
		// Handle scan view data (e.g., "quick" for quick scan)
		if scanType, ok := data.(string); ok && scanType == "quick" {
			// Return command to start quick scan
			return func() tea.Msg {
				return ScanStartedMsg{ScanID: "quick-scan"}
			}
		}
	case ViewResults:
		// Handle results view data (e.g., "correlate" for correlation mode)
		if mode, ok := data.(string); ok && mode == "correlate" {
			// Set results view to correlation mode
		}
	case ViewConfig:
		// Handle config view data (e.g., "settings" for settings mode)
		if mode, ok := data.(string); ok && mode == "settings" {
			// Set config view to settings mode
		}
	}
	return nil
}

// IsViewAvailable checks if a view type is available/implemented
func (r *ViewRouter) IsViewAvailable(viewType ViewType) bool {
	switch viewType {
	case ViewMain:
		return true
	case ViewScan, ViewResults, ViewConfig, ViewDiagrams, ViewQuery, ViewCompliance:
		// These will be implemented in future phases
		return false
	default:
		return false
	}
}

// GetAvailableViews returns a list of available view types
func (r *ViewRouter) GetAvailableViews() []ViewType {
	var available []ViewType
	for viewType := ViewMain; viewType <= ViewCompliance; viewType++ {
		if r.IsViewAvailable(viewType) {
			available = append(available, viewType)
		}
	}
	return available
}

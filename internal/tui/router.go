package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	appconfig "github.com/jlgore/corkscrew/internal/config"
	"github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/internal/tui/views"
)

// ViewRouter manages navigation between different TUI views
type ViewRouter struct {
	// Current state
	currentView ViewType
	views       map[ViewType]views.BaseView
	history     []ViewType

	// Dependencies
	database *data.Session
	config   *appconfig.CorkscrewConfig

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
func (r *ViewRouter) SetDependencies(database *data.Session, config *appconfig.CorkscrewConfig) {
	r.database = database
	r.config = config

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
		return views.NewResultsViewModel()
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

func (r *ViewRouter) createScanView() views.BaseView {
	return views.NewSummaryViewModel(ViewScan, "Scan", "Scan execution workspace", []views.SummarySection{
		{
			Title: "Current scan",
			Items: []string{
				"No active scan",
				"Configured provider and service selection will appear here",
			},
		},
		{
			Title: "Status",
			Items: []string{
				"Progress updates are shown in the status bar",
				"Completed scans can be opened from Results",
			},
		},
	})
}

func (r *ViewRouter) createConfigView() views.BaseView {
	return views.NewSummaryViewModel(ViewConfig, "Configuration", "Provider, region, and service settings", []views.SummarySection{
		{
			Title: "Providers",
			Items: []string{
				"Configuration file status is shown in the main menu",
				"Provider auth and service defaults will be edited here",
			},
		},
		{
			Title: "Validation",
			Items: []string{
				"Config validation messages will appear in this workspace",
			},
		},
	})
}

func (r *ViewRouter) createDiagramsView() views.BaseView {
	return views.NewSummaryViewModel(ViewDiagrams, "Diagrams", "Architecture diagram workspace", []views.SummarySection{
		{
			Title: "Diagram source",
			Items: []string{
				"No graph loaded",
				"ASCII and Mermaid diagram modes will appear here",
			},
		},
	})
}

func (r *ViewRouter) createQueryView() views.BaseView {
	return views.NewSummaryViewModel(ViewQuery, "Query", "SQL query workspace", []views.SummarySection{
		{
			Title: "Editor",
			Items: []string{
				"No query loaded",
				"Saved queries and parameter inputs will appear here",
			},
		},
		{
			Title: "Results",
			Items: []string{
				"Query output tables will render below the editor",
			},
		},
	})
}

func (r *ViewRouter) createComplianceView() views.BaseView {
	return views.NewSummaryViewModel(ViewCompliance, "Compliance", "Compliance pack execution workspace", []views.SummarySection{
		{
			Title: "Packs",
			Items: []string{
				"No compliance pack selected",
				"Pack parameters and run results will appear here",
			},
		},
	})
}

// injectDependencies injects dependencies into a view
func (r *ViewRouter) injectDependencies(view views.BaseView) {
	// Use type assertion to inject dependencies into specific view types
	// This is a simplified approach - in a real implementation, you might
	// want to use interfaces or dependency injection patterns

	switch v := view.(type) {
	case *views.MainMenuModel:
		if r.database != nil {
			v.SetDatabase(r.database)
		}
		if r.config != nil {
			v.SetConfig(r.config)
		}
	case *views.ResultsViewModel:
		if r.database != nil {
			v.SetDatabase(r.database)
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
			return func() tea.Msg { return quickScanRequestedMsg{} }
		}
	case ViewResults:
		// Handle results view data (e.g., "correlate" for correlation mode)
		if mode, ok := data.(string); ok && mode == "correlate" {
			if resultsView, ok := r.views[ViewResults].(*views.ResultsViewModel); ok {
				resultsView.SetMode(mode)
			}
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
	case ViewMain, ViewScan, ViewResults, ViewConfig, ViewDiagrams, ViewQuery, ViewCompliance:
		return true
	default:
		return false
	}
}

// GetAvailableViews returns a list of available view types
func (r *ViewRouter) GetAvailableViews() []ViewType {
	var available []ViewType
	for _, viewType := range []ViewType{
		ViewMain,
		ViewScan,
		ViewResults,
		ViewConfig,
		ViewDiagrams,
		ViewCompliance,
		ViewQuery,
	} {
		if r.IsViewAvailable(viewType) {
			available = append(available, viewType)
		}
	}
	return available
}

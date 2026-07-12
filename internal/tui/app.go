package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jlgore/corkscrew/internal/tui/components"
	"github.com/jlgore/corkscrew/internal/tui/styles"
)

// CorkscrewApp represents the main TUI application
type CorkscrewApp struct {
	// Core components
	router      *ViewRouter
	statusBar   components.StatusBarModel
	breadcrumb  components.BreadcrumbModel
	help        help.Model
	keyBindings KeyBindings

	// Dependencies
	database interface{} // *db.GraphLoader
	scanner  interface{}
	config   interface{} // *config.Config

	// Application state
	width     int
	height    int
	ready     bool
	showHelp  bool
	lastError error
	startView ViewType

	// Scan state
	scanStatus *ScanStatus
	scanning   bool

	// Ticker for periodic updates
	ticker *time.Ticker
}

// NewCorkscrewApp creates a new Corkscrew TUI application
func NewCorkscrewApp() *CorkscrewApp {
	app := &CorkscrewApp{
		router:      NewViewRouter(),
		statusBar:   components.NewStatusBarModel(),
		breadcrumb:  components.NewBreadcrumbModel(),
		help:        help.New(),
		keyBindings: DefaultKeyBindings,
		width:       styles.MinWidth,
		height:      styles.MinHeight,
		ready:       false,
		showHelp:    false,
		startView:   ViewMain,
		scanning:    false,
		ticker:      time.NewTicker(time.Second),
	}

	// Configure help
	app.help.Width = 80

	return app
}

// SetDependencies sets the application dependencies
func (app *CorkscrewApp) SetDependencies(database, config, scanner interface{}) {
	app.database = database
	app.config = config
	app.scanner = scanner

	// Inject dependencies into router
	app.router.SetDependencies(database, config, scanner)

	// Update status bar with initial provider info
	if config != nil {
		// This would extract provider info from config
		providers := []string{"aws", "azure", "gcp"}
		app.statusBar.SetProviders(providers)
	}
}

// StartWithView sets the initial view to display
func (app *CorkscrewApp) StartWithView(viewType ViewType) {
	app.startView = viewType
}

// StartWithMainMenu starts with the main menu
func (app *CorkscrewApp) StartWithMainMenu() {
	app.startView = ViewMain
}

// StartWithScanView starts with the scan view
func (app *CorkscrewApp) StartWithScanView() {
	app.startView = ViewScan
}

// StartWithResultsView starts with the results view
func (app *CorkscrewApp) StartWithResultsView() {
	app.startView = ViewResults
}

// StartWithConfigView starts with the config view
func (app *CorkscrewApp) StartWithConfigView() {
	app.startView = ViewConfig
}

// Init initializes the application (BubbleTea interface)
func (app *CorkscrewApp) Init() tea.Cmd {
	return tea.Batch(
		// Switch to the initial view
		func() tea.Msg {
			return SwitchViewMsg{View: app.startView}
		},
		// Start periodic ticker for status updates
		tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return TickMsg{Time: t}
		}),
		// Load initial data
		app.loadInitialData(),
	)
}

// Update handles all application messages (BubbleTea interface)
func (app *CorkscrewApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		app.handleWindowSize(msg)

		// Forward to components
		cmds = append(cmds, func() tea.Msg { return msg })

	case tea.KeyMsg:
		cmd := app.handleGlobalKeys(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SwitchViewMsg:
		cmd := app.handleViewSwitch(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case BackMsg:
		cmd := app.router.GoBack()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		app.updateBreadcrumb()

	case QuitMsg:
		return app, tea.Quit

	case ToggleHelpMsg:
		app.showHelp = !app.showHelp

	case ErrorMsg:
		app.lastError = msg.Error
		app.statusBar.SetStatusMessage(msg.Error.Error(), StatusError)

	case StatusUpdateMsg:
		app.statusBar.SetStatusMessage(msg.Message, msg.Level)

	case TickMsg:
		// Periodic updates
		app.statusBar = app.statusBar.Update(msg)
		cmds = append(cmds, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return TickMsg{Time: t}
		}))

	case ScanStartedMsg:
		app.handleScanStarted(msg)

	case ScanProgressMsg:
		app.handleScanProgress(msg)

	case ScanCompleteMsg:
		app.handleScanComplete(msg)

	case DatabaseConnectedMsg:
		app.database = msg.Database
		app.statusBar.SetStatusMessage("Database connected", StatusSuccess)

	case ConfigLoadedMsg:
		if msg.Error == nil {
			app.config = msg.Config
			app.statusBar.SetStatusMessage("Configuration loaded", StatusSuccess)
		} else {
			app.statusBar.SetStatusMessage("Failed to load config: "+msg.Error.Error(), StatusError)
		}
	}

	// Update router (current view)
	routerCmd := app.router.Update(msg)
	if routerCmd != nil {
		cmds = append(cmds, routerCmd)
	}

	// Update status bar
	app.statusBar = app.statusBar.Update(msg)

	return app, tea.Batch(cmds...)
}

// View renders the entire application (BubbleTea interface)
func (app *CorkscrewApp) View() string {
	if !app.ready {
		return "Initializing Corkscrew TUI..."
	}

	if app.showHelp {
		return app.renderHelpOverlay()
	}

	// Calculate layout
	headerHeight := styles.HeaderHeight
	statusBarHeight := styles.StatusBarHeight
	mainHeight := app.height - headerHeight - statusBarHeight

	// Render components
	header := app.renderHeader()
	main := app.renderMain(mainHeight)
	statusBar := app.renderStatusBar()

	// Combine layout
	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		main,
		statusBar,
	)
}

// renderHeader renders the application header with breadcrumbs
func (app *CorkscrewApp) renderHeader() string {
	title := styles.TitleStyle.Render("Corkscrew Cloud Scanner")

	// Set breadcrumb max width
	breadcrumbWidth := app.width - lipgloss.Width(title) - 4
	app.breadcrumb.SetMaxWidth(breadcrumbWidth)

	breadcrumbView := app.breadcrumb.View()

	// Create header layout
	header := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		breadcrumbView,
	)

	return styles.WithPadding(styles.BaseStyle, 1).
		Width(app.width).
		Render(header)
}

// renderMain renders the main content area
func (app *CorkscrewApp) renderMain(height int) string {
	mainContent := app.router.View()

	return styles.BaseStyle.
		Width(app.width).
		Height(height).
		Render(mainContent)
}

// renderStatusBar renders the status bar
func (app *CorkscrewApp) renderStatusBar() string {
	app.statusBar.Resize(app.width)
	return app.statusBar.View()
}

// renderHelpOverlay renders the help overlay
func (app *CorkscrewApp) renderHelpOverlay() string {
	currentView := app.router.GetCurrentView()
	if currentView == nil {
		return "Help not available"
	}

	// Get help from current view
	helpContent := app.help.View(app.keyBindings)

	// Create overlay
	overlay := styles.BorderStyle.
		Width(app.width - 4).
		Height(app.height - 4).
		Render(helpContent)

	return lipgloss.Place(
		app.width,
		app.height,
		lipgloss.Center,
		lipgloss.Center,
		overlay,
	)
}

// Event handlers

func (app *CorkscrewApp) handleWindowSize(msg tea.WindowSizeMsg) {
	app.width = msg.Width
	app.height = msg.Height
	app.ready = true

	// Ensure minimum dimensions
	if app.width < styles.MinWidth {
		app.width = styles.MinWidth
	}
	if app.height < styles.MinHeight {
		app.height = styles.MinHeight
	}

	// Update help width
	app.help.Width = app.width

	// Forward to router
	app.router.Resize(app.width, app.height)
}

func (app *CorkscrewApp) handleGlobalKeys(msg tea.KeyMsg) tea.Cmd {
	switch {
	case msg.String() == "ctrl+c":
		return tea.Quit
	case msg.String() == "q" && !app.showHelp:
		return tea.Quit
	case msg.String() == "?":
		return func() tea.Msg { return ToggleHelpMsg{} }
	case msg.String() == "esc":
		if app.showHelp {
			return func() tea.Msg { return ToggleHelpMsg{} }
		}
		return func() tea.Msg { return BackMsg{} }
	}
	return nil
}

func (app *CorkscrewApp) handleViewSwitch(msg SwitchViewMsg) tea.Cmd {
	// Update breadcrumb
	label := components.GetViewLabel(msg.View)
	app.breadcrumb.NavigateToView(msg.View, label, msg.Data)

	// Switch view
	return app.router.SwitchView(msg.View, msg.Data)
}

func (app *CorkscrewApp) handleScanStarted(msg ScanStartedMsg) {
	if msg.Error == nil {
		app.scanning = true
		app.scanStatus = &ScanStatus{
			Active:    true,
			Progress:  0.0,
			Operation: "Starting scan...",
			StartTime: time.Now(),
			Resources: 0,
			Errors:    0,
			Providers: []string{},
		}
		app.statusBar.SetScanStatus(app.scanStatus)
	}
}

func (app *CorkscrewApp) handleScanProgress(msg ScanProgressMsg) {
	if app.scanStatus != nil {
		app.scanStatus.Progress = msg.Progress
		app.scanStatus.Operation = msg.Operation
		app.scanStatus.Resources = msg.Resources
		app.scanStatus.Errors = msg.Errors
	}
}

func (app *CorkscrewApp) handleScanComplete(msg ScanCompleteMsg) {
	app.scanning = false
	app.scanStatus = nil
	app.statusBar.SetScanStatus(nil)
}

func (app *CorkscrewApp) updateBreadcrumb() {
	currentViewType := app.router.GetCurrentViewType()
	label := components.GetViewLabel(currentViewType)
	app.breadcrumb.NavigateToView(currentViewType, label, nil)
}

func (app *CorkscrewApp) loadInitialData() tea.Cmd {
	return tea.Batch(
		// Load configuration
		func() tea.Msg {
			// config, err := config.LoadConfig("./corkscrew.yaml")
			return ConfigLoadedMsg{Config: app.config, Error: nil}
		},
		// Connect to database
		func() tea.Msg {
			if app.database != nil {
				return DatabaseConnectedMsg{Database: app.database}
			}
			return nil
		},
	)
}

// Public methods for controlling the application

// StartQuickScan starts a quick scan with default settings
func (app *CorkscrewApp) StartQuickScan() tea.Cmd {
	if app.scanner == nil {
		return func() tea.Msg {
			return ErrorMsg{Error: fmt.Errorf("scanner not available")}
		}
	}

	return func() tea.Msg {
		// This would start a scan using the orchestrator
		return ScanStartedMsg{ScanID: "quick-scan"}
	}
}

// GetCurrentView returns the current view type
func (app *CorkscrewApp) GetCurrentView() ViewType {
	return app.router.GetCurrentViewType()
}

// GetScanStatus returns the current scan status
func (app *CorkscrewApp) GetScanStatus() *ScanStatus {
	return app.scanStatus
}

// IsScanning returns whether a scan is currently active
func (app *CorkscrewApp) IsScanning() bool {
	return app.scanning
}

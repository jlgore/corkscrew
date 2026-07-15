package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
	appconfig "github.com/jlgore/corkscrew/internal/config"
	"github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/internal/tui/components"
	"github.com/jlgore/corkscrew/internal/tui/styles"
)

// ScanRunFunc is the TUI-owned seam to the transport-neutral scan application.
type ScanRunFunc func(context.Context, scanapp.Request) (scanapp.Outcome, error)

type quickScanRequestedMsg struct{}

type providerScanCompleteMsg struct {
	provider string
	outcome  scanapp.Outcome
	err      error
}

// CorkscrewApp represents the main TUI application
type CorkscrewApp struct {
	// Core components
	router      *ViewRouter
	statusBar   components.StatusBarModel
	breadcrumb  components.BreadcrumbModel
	help        help.Model
	keyBindings KeyBindings

	// Dependencies
	database   *data.Session
	config     *appconfig.CorkscrewConfig
	scanRunner ScanRunFunc

	// Application state
	width     int
	height    int
	ready     bool
	showHelp  bool
	lastError error
	startView ViewType

	// Scan state
	scanStatus         *ScanStatus
	scanning           bool
	quickScanProviders []string
	quickScanCompleted int
	quickScanErrors    []error
	quickScanStarted   time.Time

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
func (app *CorkscrewApp) SetDependencies(database *data.Session, config *appconfig.CorkscrewConfig, scanRunner ScanRunFunc) {
	app.database = database
	app.config = config
	app.scanRunner = scanRunner

	// Inject dependencies into router
	app.router.SetDependencies(database, config)

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

	case quickScanRequestedMsg:
		if cmd := app.beginQuickScan(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case providerScanCompleteMsg:
		if cmd := app.handleProviderScanComplete(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

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
	if msg.Error != nil {
		app.lastError = msg.Error
		app.statusBar.SetStatusMessage(msg.Error.Error(), StatusError)
		return
	}
	app.statusBar.SetStatusMessage(fmt.Sprintf("Scan completed: %d resources", msg.Resources), StatusSuccess)
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
	return func() tea.Msg { return quickScanRequestedMsg{} }
}

func (app *CorkscrewApp) beginQuickScan() tea.Cmd {
	if app.scanRunner == nil {
		return func() tea.Msg { return ErrorMsg{Error: fmt.Errorf("scan application is not available")} }
	}
	if app.config == nil {
		return func() tea.Msg { return ErrorMsg{Error: fmt.Errorf("scan configuration is not available")} }
	}
	providers := make([]string, 0, len(app.config.Providers))
	for name, provider := range app.config.Providers {
		if provider.Enabled {
			providers = append(providers, name)
		}
	}
	sort.Strings(providers)
	if len(providers) == 0 {
		return func() tea.Msg { return ErrorMsg{Error: fmt.Errorf("no providers are enabled")} }
	}

	app.quickScanProviders = providers
	app.quickScanCompleted = 0
	app.quickScanErrors = nil
	app.quickScanStarted = time.Now()
	app.handleScanStarted(ScanStartedMsg{ScanID: "quick-scan"})
	app.scanStatus.Providers = append([]string(nil), providers...)
	app.scanStatus.Operation = fmt.Sprintf("Scanning %s", providers[0])
	return app.runQuickScanProvider(providers[0])
}

func (app *CorkscrewApp) runQuickScanProvider(provider string) tea.Cmd {
	return func() tea.Msg {
		outcome, err := app.scanRunner(context.Background(), scanapp.Request{Provider: provider})
		return providerScanCompleteMsg{provider: provider, outcome: outcome, err: err}
	}
}

func (app *CorkscrewApp) handleProviderScanComplete(msg providerScanCompleteMsg) tea.Cmd {
	app.quickScanCompleted++
	if app.scanStatus != nil {
		app.scanStatus.Resources += len(msg.outcome.Resources)
		app.scanStatus.Progress = float64(app.quickScanCompleted) / float64(len(app.quickScanProviders))
	}
	if msg.err != nil {
		app.quickScanErrors = append(app.quickScanErrors, fmt.Errorf("%s: %w", msg.provider, msg.err))
		if app.scanStatus != nil {
			app.scanStatus.Errors++
		}
	}
	if app.quickScanCompleted < len(app.quickScanProviders) {
		next := app.quickScanProviders[app.quickScanCompleted]
		if app.scanStatus != nil {
			app.scanStatus.Operation = fmt.Sprintf("Scanning %s", next)
		}
		return app.runQuickScanProvider(next)
	}

	resources := 0
	if app.scanStatus != nil {
		resources = app.scanStatus.Resources
	}
	duration := time.Since(app.quickScanStarted)
	combined := errors.Join(app.quickScanErrors...)
	return func() tea.Msg {
		return ScanCompleteMsg{ScanID: "quick-scan", Resources: resources, Duration: duration, Error: combined}
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

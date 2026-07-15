package views

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appconfig "github.com/jlgore/corkscrew/internal/config"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
	"github.com/jlgore/corkscrew/internal/tui/styles"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

// MainMenuModel represents the main menu view
type MainMenuModel struct {
	// Base view state
	state ViewState

	// Components
	list    list.Model
	session *dataaccess.Session
	config  *appconfig.CorkscrewConfig

	// State
	lastScanTime  string
	resourceCount int
	recentScans   []ScanInfo
	systemStatus  SystemStatus
}

// MenuItem represents a menu item
type MenuItem struct {
	title       string
	description string
	action      string
	icon        string
	enabled     bool
	shortcut    string
}

// ScanInfo represents information about a recent scan
type ScanInfo struct {
	ID        string
	Timestamp string
	Resources int
	Providers []string
	Duration  string
}

// SystemStatus represents current system status
type SystemStatus struct {
	DatabaseConnected   bool
	ProvidersConfigured int
	LastScanTime        string
	TotalResources      int
	HasErrors           bool
}

// Implement list.Item interface for MenuItem
func (i MenuItem) FilterValue() string { return i.title }

// menuItemDelegate implements list.ItemDelegate for styling menu items
type menuItemDelegate struct{}

func (d menuItemDelegate) Height() int                               { return 3 }
func (d menuItemDelegate) Spacing() int                              { return 1 }
func (d menuItemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d menuItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(MenuItem)
	if !ok {
		return
	}

	// Determine styling based on selection and enabled state
	var titleStyle, descStyle lipgloss.Style

	if index == m.Index() {
		// Selected item
		titleStyle = styles.SelectedMenuItemStyle
		descStyle = styles.MenuDescriptionStyle.Foreground(styles.TextColor)
	} else {
		// Regular item
		titleStyle = styles.MenuItemStyle
		descStyle = styles.MenuDescriptionStyle
	}

	if !i.enabled {
		titleStyle = titleStyle.Foreground(styles.SubtleColor)
		descStyle = descStyle.Foreground(styles.SubtleColor)
	}

	// Render title with icon and shortcut
	title := fmt.Sprintf("%s %s", i.icon, i.title)
	if i.shortcut != "" {
		title += lipgloss.NewStyle().
			Foreground(styles.AccentColor).
			Render(" (" + i.shortcut + ")")
	}

	fmt.Fprint(w, titleStyle.Render(title))
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, descStyle.Render(i.description))
}

// NewMainMenuModel creates a new main menu model
func NewMainMenuModel() *MainMenuModel {
	items := []list.Item{
		MenuItem{
			title:       "Quick Scan",
			description: "Start scan with current configuration",
			action:      "quick-scan",
			icon:        styles.IconScan,
			enabled:     true,
			shortcut:    "s",
		},
		MenuItem{
			title:       "Configure Scan",
			description: "Set up providers, regions, and services",
			action:      "configure",
			icon:        styles.IconConfig,
			enabled:     true,
			shortcut:    "c",
		},
		MenuItem{
			title:       "View Results",
			description: "Browse and analyze scan results",
			action:      "results",
			icon:        styles.IconResults,
			enabled:     true,
			shortcut:    "r",
		},
		MenuItem{
			title:       "Cross-Cloud Analysis",
			description: "Find correlations between providers",
			action:      "correlate",
			icon:        styles.IconCrossCloud,
			enabled:     true,
			shortcut:    "x",
		},
		MenuItem{
			title:       "Compliance Check",
			description: "Run compliance packs and audits",
			action:      "compliance",
			icon:        styles.IconCompliance,
			enabled:     true,
			shortcut:    "p",
		},
		MenuItem{
			title:       "Query Builder",
			description: "Interactive SQL query builder",
			action:      "query",
			icon:        styles.IconQuery,
			enabled:     true,
			shortcut:    "b",
		},
		MenuItem{
			title:       "Diagrams",
			description: "Generate architecture diagrams",
			action:      "diagrams",
			icon:        styles.IconDiagrams,
			enabled:     true,
			shortcut:    "d",
		},
		MenuItem{
			title:       "Settings",
			description: "Configure Corkscrew settings",
			action:      "settings",
			icon:        styles.IconSettings,
			enabled:     true,
			shortcut:    "g",
		},
	}

	l := list.New(items, menuItemDelegate{}, 0, 0)
	l.Title = "Corkscrew - Cloud Resource Scanner"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.Styles.Title = styles.TitleStyle

	return &MainMenuModel{
		state: NewViewState(types.ViewMain, "Main Menu"),
		list:  l,
		systemStatus: SystemStatus{
			DatabaseConnected:   false,
			ProvidersConfigured: 0,
			LastScanTime:        "Never",
			TotalResources:      0,
			HasErrors:           false,
		},
	}
}

// Init initializes the main menu
func (m *MainMenuModel) Init() tea.Cmd {
	return tea.Batch(
		m.loadSystemStatus(),
		m.loadRecentScans(),
	)
}

// Update handles messages for the main menu
func (m *MainMenuModel) Update(msg tea.Msg) (BaseView, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter", " ":
			// Handle menu selection
			if selected, ok := m.list.SelectedItem().(MenuItem); ok {
				return m, m.handleMenuAction(selected.action)
			}

		case "s":
			return m, m.handleMenuAction("quick-scan")
		case "c":
			return m, m.handleMenuAction("configure")
		case "r":
			return m, m.handleMenuAction("results")
		case "x":
			return m, m.handleMenuAction("correlate")
		case "p":
			return m, m.handleMenuAction("compliance")
		case "q":
			return m, nil
		case "b":
			return m, m.handleMenuAction("query")
		case "d":
			return m, m.handleMenuAction("diagrams")
		case "g":
			return m, m.handleMenuAction("settings")
		}

	case types.DatabaseConnectedMsg:
		m.session = msg.Database
		m.systemStatus.DatabaseConnected = true
		return m, m.loadSystemStatus()

	case types.ConfigLoadedMsg:
		if msg.Error == nil {
			m.config = msg.Config
			m.systemStatus.ProvidersConfigured = m.countConfiguredProviders()
		}
		return m, nil

	case SystemStatusLoadedMsg:
		m.systemStatus = msg.Status
		return m, nil

	case RecentScansLoadedMsg:
		m.recentScans = msg.Scans
		return m, nil
	}

	// Update the list
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the main menu
func (m *MainMenuModel) View() string {
	// Calculate layout dimensions
	menuWidth := m.state.Width * 3 / 5
	sidebarWidth := m.state.Width - menuWidth - 2

	if menuWidth < 40 {
		menuWidth = 40
	}
	if sidebarWidth < 30 {
		sidebarWidth = 30
	}

	// Update list size
	m.list.SetSize(menuWidth, m.state.Height-4)

	// Render components
	menuView := m.renderMenu(menuWidth)
	sidebarView := m.renderSidebar(sidebarWidth)

	// Join horizontally
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		menuView,
		styles.BorderStyle.Width(1).Height(m.state.Height-2).Render(""),
		sidebarView,
	)
}

// renderMenu renders the main menu list
func (m *MainMenuModel) renderMenu(width int) string {
	return styles.BorderStyle.
		Width(width).
		Height(m.state.Height - 2).
		Render(m.list.View())
}

// renderSidebar renders the sidebar with system information
func (m *MainMenuModel) renderSidebar(width int) string {
	statusSection := m.renderSystemStatus()
	recentSection := m.renderRecentScans()

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		statusSection,
		"",
		recentSection,
	)

	return styles.BorderStyle.
		Width(width).
		Height(m.state.Height - 2).
		Render(content)
}

// renderSystemStatus renders the system status section
func (m *MainMenuModel) renderSystemStatus() string {
	var statusItems []string

	// Title
	title := styles.TitleStyle.Render("System Status")
	statusItems = append(statusItems, title)

	// Database status
	dbStatus := "❌ Disconnected"
	if m.systemStatus.DatabaseConnected {
		dbStatus = "✅ Connected"
	}
	statusItems = append(statusItems, fmt.Sprintf("Database: %s", dbStatus))

	// Providers
	providerText := fmt.Sprintf("Providers: %d configured", m.systemStatus.ProvidersConfigured)
	statusItems = append(statusItems, providerText)

	// Last scan
	lastScanText := fmt.Sprintf("Last scan: %s", m.systemStatus.LastScanTime)
	statusItems = append(statusItems, lastScanText)

	// Resources
	resourceText := fmt.Sprintf("Total resources: %d", m.systemStatus.TotalResources)
	statusItems = append(statusItems, resourceText)

	return lipgloss.JoinVertical(lipgloss.Left, statusItems...)
}

// renderRecentScans renders the recent scans section
func (m *MainMenuModel) renderRecentScans() string {
	var scanItems []string

	// Title
	title := styles.TitleStyle.Render("Recent Scans")
	scanItems = append(scanItems, title)

	if len(m.recentScans) == 0 {
		scanItems = append(scanItems, styles.SubtitleStyle.Render("No recent scans"))
	} else {
		for i, scan := range m.recentScans {
			if i >= 5 { // Show only last 5 scans
				break
			}
			scanText := fmt.Sprintf("• %s - %d resources", scan.Timestamp, scan.Resources)
			scanItems = append(scanItems, scanText)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, scanItems...)
}

// handleMenuAction processes menu actions
func (m *MainMenuModel) handleMenuAction(action string) tea.Cmd {
	switch action {
	case "quick-scan":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewScan, Data: "quick"}
		}
	case "configure":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewConfig}
		}
	case "results":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewResults}
		}
	case "correlate":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewResults, Data: "correlate"}
		}
	case "compliance":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewCompliance}
		}
	case "query":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewQuery}
		}
	case "diagrams":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewDiagrams}
		}
	case "settings":
		return func() tea.Msg {
			return types.SwitchViewMsg{View: types.ViewConfig, Data: "settings"}
		}
	}
	return nil
}

// loadSystemStatus loads current system status
func (m *MainMenuModel) loadSystemStatus() tea.Cmd {
	return func() tea.Msg {
		totalResources, dbOK := m.countStoredResources()
		if totalResources == 0 {
			totalResources = m.systemStatus.TotalResources
		}

		lastScanTime := m.latestScanTime()
		if lastScanTime == "" {
			lastScanTime = m.systemStatus.LastScanTime
		}

		status := SystemStatus{
			DatabaseConnected:   dbOK,
			ProvidersConfigured: m.countConfiguredProviders(),
			LastScanTime:        lastScanTime,
			TotalResources:      totalResources,
			HasErrors:           false,
		}
		return SystemStatusLoadedMsg{Status: status}
	}
}

// loadRecentScans loads recent scan information
func (m *MainMenuModel) loadRecentScans() tea.Cmd {
	return func() tea.Msg {
		return RecentScansLoadedMsg{Scans: m.queryRecentScans()}
	}
}

// countConfiguredProviders counts enabled providers in the typed config.
func (m *MainMenuModel) countConfiguredProviders() int {
	if m.config == nil {
		return 0
	}
	count := 0
	for _, provider := range m.config.Providers {
		if provider.Enabled {
			count++
		}
	}
	return count
}

func (m *MainMenuModel) countStoredResources() (int, bool) {
	if m.session == nil {
		return 0, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	total, err := m.session.Inventory().Count(ctx, dataaccess.InventoryFilter{})
	return total, err == nil
}

func (m *MainMenuModel) latestScanTime() string {
	scans := m.queryRecentScans()
	if len(scans) == 0 {
		return "Never"
	}
	return scans[0].Timestamp
}

func (m *MainMenuModel) queryRecentScans() []ScanInfo {
	if m.session == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	summaries, err := m.session.RecentScans(ctx, 5)
	if err != nil {
		return nil
	}

	scans := make([]ScanInfo, 0, len(summaries))
	for _, summary := range summaries {
		scans = append(scans, ScanInfo{
			ID:        summary.ID,
			Timestamp: formatScanTime(summary.StartedAt),
			Resources: summary.Resources,
			Providers: splitProviderList(summary.Label),
			Duration:  formatDuration(summary.Duration),
		})
	}
	return scans
}

func formatScanTime(scanTime time.Time) string {
	if scanTime.IsZero() {
		return "Unknown"
	}
	return scanTime.Format("2006-01-02 15:04")
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	return duration.Round(time.Millisecond).String()
}

func splitProviderList(provider string) []string {
	fields := strings.FieldsFunc(provider, func(r rune) bool {
		return r == ',' || r == ' ' || r == '[' || r == ']'
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, `"`)
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}

// SetDatabase updates the data session dependency.
func (m *MainMenuModel) SetDatabase(session *dataaccess.Session) {
	m.session = session
}

// SetConfig updates the config dependency.
func (m *MainMenuModel) SetConfig(config *appconfig.CorkscrewConfig) {
	m.config = config
}

// Interface implementations
func (m *MainMenuModel) Title() string            { return m.state.Title }
func (m *MainMenuModel) ViewType() types.ViewType { return m.state.ViewType }

func (m *MainMenuModel) ShortHelp() []string {
	return []string{
		"↑/k up", "↓/j down", "enter select", "? help", "q quit",
	}
}

func (m *MainMenuModel) FullHelp() [][]string {
	return [][]string{
		{"↑/k", "up", "↓/j", "down"},
		{"enter/space", "select", "esc", "back"},
		{"s", "quick scan", "c", "configure"},
		{"r", "results", "b", "query"},
		{"?", "help", "ctrl+c", "quit"},
	}
}

func (m *MainMenuModel) Focus() { m.state.Focused = true }
func (m *MainMenuModel) Blur()  { m.state.Focused = false }

func (m *MainMenuModel) Resize(width, height int) {
	m.state.Width = width
	m.state.Height = height
	m.list.SetSize(width*3/5, height-4)
}

// Custom messages for the main menu
type (
	SystemStatusLoadedMsg struct {
		Status SystemStatus
	}

	RecentScansLoadedMsg struct {
		Scans []ScanInfo
	}
)

package tui

import "github.com/charmbracelet/bubbles/key"

// KeyBindings defines all keyboard shortcuts for the TUI
type KeyBindings struct {
	// Global navigation
	Quit    key.Binding
	Help    key.Binding
	Back    key.Binding
	Refresh key.Binding
	Search  key.Binding
	Filter  key.Binding

	// Standard navigation
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	// Selection and actions
	Select key.Binding
	Edit   key.Binding
	Delete key.Binding
	Export key.Binding
	Copy   key.Binding

	// View switching (quick access)
	MainMenu       key.Binding
	ScanView       key.Binding
	ResultsView    key.Binding
	ConfigView     key.Binding
	DiagramView    key.Binding
	QueryView      key.Binding
	ComplianceView key.Binding

	// Tab navigation
	NextTab key.Binding
	PrevTab key.Binding

	// Scan specific
	StartScan key.Binding
	StopScan  key.Binding
	QuickScan key.Binding

	// Results specific
	ShowDetails key.Binding
	ToggleMode  key.Binding

	// Input handling
	Submit key.Binding
	Cancel key.Binding
	Clear  key.Binding
}

// DefaultKeyBindings returns the default key bindings
var DefaultKeyBindings = KeyBindings{
	// Global navigation
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Refresh: key.NewBinding(key.WithKeys("r", "f5"), key.WithHelp("r", "refresh")),
	Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Filter:  key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),

	// Standard navigation (vim-style + arrow keys)
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
	Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
	Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home/g", "top")),
	End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end/G", "bottom")),

	// Selection and actions
	Select: key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "select")),
	Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Export: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "export")),
	Copy:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),

	// View switching (number keys + ctrl combinations)
	MainMenu:       key.NewBinding(key.WithKeys("1", "ctrl+1"), key.WithHelp("1", "main menu")),
	ScanView:       key.NewBinding(key.WithKeys("2", "ctrl+2"), key.WithHelp("2", "scan")),
	ResultsView:    key.NewBinding(key.WithKeys("3", "ctrl+3"), key.WithHelp("3", "results")),
	ConfigView:     key.NewBinding(key.WithKeys("4", "ctrl+4"), key.WithHelp("4", "config")),
	DiagramView:    key.NewBinding(key.WithKeys("5", "ctrl+5"), key.WithHelp("5", "diagrams")),
	QueryView:      key.NewBinding(key.WithKeys("6", "ctrl+6"), key.WithHelp("6", "query")),
	ComplianceView: key.NewBinding(key.WithKeys("7", "ctrl+7"), key.WithHelp("7", "compliance")),

	// Tab navigation
	NextTab: key.NewBinding(key.WithKeys("tab", "ctrl+n"), key.WithHelp("tab", "next tab")),
	PrevTab: key.NewBinding(key.WithKeys("shift+tab", "ctrl+p"), key.WithHelp("shift+tab", "prev tab")),

	// Scan specific
	StartScan: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start scan")),
	StopScan:  key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "stop scan")),
	QuickScan: key.NewBinding(key.WithKeys("shift+s"), key.WithHelp("shift+s", "quick scan")),

	// Results specific
	ShowDetails: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "details")),
	ToggleMode:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "toggle mode")),

	// Input handling
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel")),
	Clear:  key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear")),
}

// ShortHelp returns key bindings to show in the mini help view
func (k KeyBindings) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k KeyBindings) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},                       // Navigation
		{k.Select, k.Back, k.Help, k.Quit},                    // Basic actions
		{k.MainMenu, k.ScanView, k.ResultsView, k.ConfigView}, // Views
		{k.Search, k.Filter, k.Refresh, k.Export},             // Actions
	}
}

// GetViewSpecificKeys returns key bindings specific to a view
func GetViewSpecificKeys(view ViewType) []key.Binding {
	switch view {
	case ViewMain:
		return []key.Binding{
			DefaultKeyBindings.Select,
			DefaultKeyBindings.QuickScan,
			DefaultKeyBindings.Help,
			DefaultKeyBindings.Quit,
		}
	case ViewScan:
		return []key.Binding{
			DefaultKeyBindings.StartScan,
			DefaultKeyBindings.StopScan,
			DefaultKeyBindings.Back,
			DefaultKeyBindings.Help,
		}
	case ViewResults:
		return []key.Binding{
			DefaultKeyBindings.Search,
			DefaultKeyBindings.Filter,
			DefaultKeyBindings.ShowDetails,
			DefaultKeyBindings.Export,
			DefaultKeyBindings.Back,
		}
	case ViewConfig:
		return []key.Binding{
			DefaultKeyBindings.Edit,
			DefaultKeyBindings.Submit,
			DefaultKeyBindings.Cancel,
			DefaultKeyBindings.Back,
		}
	case ViewDiagrams:
		return []key.Binding{
			DefaultKeyBindings.ToggleMode,
			DefaultKeyBindings.Export,
			DefaultKeyBindings.Search,
			DefaultKeyBindings.Back,
		}
	case ViewQuery:
		return []key.Binding{
			DefaultKeyBindings.Submit,
			DefaultKeyBindings.Clear,
			DefaultKeyBindings.Export,
			DefaultKeyBindings.Back,
		}
	default:
		return DefaultKeyBindings.ShortHelp()
	}
}

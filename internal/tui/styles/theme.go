package styles

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	PrimaryColor   = lipgloss.Color("#7C3AED") // Purple
	SecondaryColor = lipgloss.Color("#06B6D4") // Cyan
	AccentColor    = lipgloss.Color("#F59E0B") // Amber
	SuccessColor   = lipgloss.Color("#10B981") // Green
	ErrorColor     = lipgloss.Color("#EF4444") // Red
	WarningColor   = lipgloss.Color("#F59E0B") // Amber
	InfoColor      = lipgloss.Color("#3B82F6") // Blue
	
	// Neutral colors
	TextColor      = lipgloss.Color("#FAFAFA") // Light gray
	SubtleColor    = lipgloss.Color("#888888") // Medium gray
	BackgroundColor = lipgloss.Color("#1A1A1A") // Dark gray
	BorderColor    = lipgloss.Color("#374151") // Border gray
)

// Base styles
var (
	BaseStyle = lipgloss.NewStyle().
		Foreground(TextColor).
		Background(BackgroundColor)

	TitleStyle = lipgloss.NewStyle().
		Foreground(PrimaryColor).
		Bold(true).
		MarginBottom(1)

	SubtitleStyle = lipgloss.NewStyle().
		Foreground(SubtleColor).
		Italic(true)

	ErrorStyle = lipgloss.NewStyle().
		Foreground(ErrorColor).
		Bold(true)

	SuccessStyle = lipgloss.NewStyle().
		Foreground(SuccessColor).
		Bold(true)

	WarningStyle = lipgloss.NewStyle().
		Foreground(WarningColor).
		Bold(true)

	InfoStyle = lipgloss.NewStyle().
		Foreground(InfoColor)
)

// Component styles
var (
	// Status bar styles
	StatusBarStyle = lipgloss.NewStyle().
		Background(BorderColor).
		Foreground(TextColor).
		Padding(0, 1)

	StatusBarActiveStyle = lipgloss.NewStyle().
		Background(PrimaryColor).
		Foreground(TextColor).
		Padding(0, 1).
		Bold(true)

	// Table styles
	TableHeaderStyle = lipgloss.NewStyle().
		Foreground(TextColor).
		Background(PrimaryColor).
		Bold(true).
		Padding(0, 1)

	SelectedRowStyle = lipgloss.NewStyle().
		Foreground(TextColor).
		Background(SecondaryColor).
		Bold(true)

	AlternateRowStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("#2A2A2A"))

	// Menu styles
	MenuItemStyle = lipgloss.NewStyle().
		Foreground(TextColor).
		Padding(0, 2)

	SelectedMenuItemStyle = lipgloss.NewStyle().
		Foreground(TextColor).
		Background(PrimaryColor).
		Padding(0, 2).
		Bold(true)

	MenuDescriptionStyle = lipgloss.NewStyle().
		Foreground(SubtleColor).
		Padding(0, 2, 0, 4)

	// Border styles
	BorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderColor)

	FocusedBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PrimaryColor)

	// Help styles
	HelpKeyStyle = lipgloss.NewStyle().
		Foreground(AccentColor).
		Bold(true)

	HelpDescStyle = lipgloss.NewStyle().
		Foreground(SubtleColor)

	HelpSeparatorStyle = lipgloss.NewStyle().
		Foreground(BorderColor)

	// Progress styles
	ProgressBarStyle = lipgloss.NewStyle().
		Background(BorderColor).
		Foreground(SuccessColor)

	ProgressCompleteStyle = lipgloss.NewStyle().
		Background(SuccessColor).
		Foreground(TextColor)

	// Breadcrumb styles
	BreadcrumbStyle = lipgloss.NewStyle().
		Foreground(SubtleColor)

	BreadcrumbActiveStyle = lipgloss.NewStyle().
		Foreground(PrimaryColor).
		Bold(true)

	BreadcrumbSeparatorStyle = lipgloss.NewStyle().
		Foreground(BorderColor)
)

// Layout constants
const (
	StatusBarHeight = 1
	HeaderHeight    = 3
	FooterHeight    = 2
	MinWidth        = 80
	MinHeight       = 24
)

// Icons and symbols
const (
	IconScan       = "🔍"
	IconConfig     = "⚙️"
	IconResults    = "📊" 
	IconDiagrams   = "📈"
	IconCompliance = "📋"
	IconQuery      = "🗃️"
	IconCrossCloud = "🔗"
	IconSettings   = "⚙️"
	IconSuccess    = "✅"
	IconError      = "❌"
	IconWarning    = "⚠️"
	IconInfo       = "ℹ️"
	IconLoading    = "⏳"
	
	// Navigation symbols
	SymbolUp       = "↑"
	SymbolDown     = "↓"
	SymbolLeft     = "←"
	SymbolRight    = "→"
	SymbolEnter    = "⏎"
	SymbolEscape   = "⎋"
	SymbolTab      = "⇥"
	SymbolSpace    = "␣"
)

// Utility functions
func WithPadding(style lipgloss.Style, padding int) lipgloss.Style {
	return style.Padding(padding)
}

func WithMargin(style lipgloss.Style, margin int) lipgloss.Style {
	return style.Margin(margin)
}

func WithWidth(style lipgloss.Style, width int) lipgloss.Style {
	return style.Width(width)
}

func WithHeight(style lipgloss.Style, height int) lipgloss.Style {
	return style.Height(height)
}

// Context-aware styling
func StatusStyle(level int) lipgloss.Style {
	switch level {
	case 0: // Info
		return InfoStyle
	case 1: // Success
		return SuccessStyle
	case 2: // Warning
		return WarningStyle
	case 3: // Error
		return ErrorStyle
	default:
		return BaseStyle
	}
}

func ProviderStyle(provider string) lipgloss.Style {
	switch provider {
	case "aws":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9900"))
	case "azure":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#0078D4"))
	case "gcp":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#4285F4"))
	case "kubernetes":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#326CE5"))
	default:
		return BaseStyle
	}
}
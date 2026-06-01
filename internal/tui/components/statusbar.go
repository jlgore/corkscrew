package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jlgore/corkscrew/internal/tui/styles"
	"github.com/jlgore/corkscrew/internal/tui/types"
)

// StatusBarModel represents the status bar at the bottom of the screen
type StatusBarModel struct {
	// Status information
	scanStatus      *types.ScanStatus
	resourceCount   int
	errorCount      int
	currentTime     time.Time
	activeProviders []string
	statusMessage   string
	statusLevel     types.StatusLevel

	// Display state
	width         int
	showTime      bool
	showProviders bool
	showProgress  bool
}

// NewStatusBarModel creates a new status bar model
func NewStatusBarModel() StatusBarModel {
	return StatusBarModel{
		scanStatus:      nil,
		resourceCount:   0,
		errorCount:      0,
		currentTime:     time.Now(),
		activeProviders: []string{},
		statusMessage:   "Ready",
		statusLevel:     types.StatusInfo,
		width:           80,
		showTime:        true,
		showProviders:   true,
		showProgress:    true,
	}
}

// Update updates the status bar with new information
func (m StatusBarModel) Update(msg interface{}) StatusBarModel {
	switch msg := msg.(type) {
	case types.ScanStartedMsg:
		if msg.Error == nil {
			m.scanStatus = &types.ScanStatus{
				Active:    true,
				Progress:  0.0,
				Operation: "Starting scan...",
				StartTime: time.Now(),
				Resources: 0,
				Errors:    0,
				Providers: m.activeProviders,
			}
			m.statusMessage = "Scan started"
			m.statusLevel = types.StatusInfo
		} else {
			m.statusMessage = fmt.Sprintf("Scan failed: %v", msg.Error)
			m.statusLevel = types.StatusError
		}

	case types.ScanProgressMsg:
		if m.scanStatus != nil {
			m.scanStatus.Progress = msg.Progress
			m.scanStatus.Operation = msg.Operation
			m.scanStatus.Resources = msg.Resources
			m.scanStatus.Errors = msg.Errors
		}
		m.resourceCount = msg.Resources
		m.errorCount = msg.Errors

	case types.ScanCompleteMsg:
		if msg.Error == nil {
			m.statusMessage = fmt.Sprintf("Scan completed: %d resources", msg.Resources)
			m.statusLevel = types.StatusSuccess
		} else {
			m.statusMessage = fmt.Sprintf("Scan failed: %v", msg.Error)
			m.statusLevel = types.StatusError
		}
		m.scanStatus = nil
		m.resourceCount = msg.Resources

	case types.StatusUpdateMsg:
		m.statusMessage = msg.Message
		m.statusLevel = msg.Level

	case types.TickMsg:
		m.currentTime = msg.Time

	case types.WindowSizeMsg:
		m.width = msg.Width
	}

	return m
}

// View renders the status bar
func (m StatusBarModel) View() string {
	leftSection := m.renderLeftSection()
	centerSection := m.renderCenterSection()
	rightSection := m.renderRightSection()

	// Calculate widths
	leftWidth := lipgloss.Width(leftSection)
	rightWidth := lipgloss.Width(rightSection)
	centerWidth := m.width - leftWidth - rightWidth

	if centerWidth < 0 {
		centerWidth = 0
	}

	// Style sections
	leftStyled := styles.StatusBarStyle.Render(leftSection)
	centerStyled := styles.StatusBarStyle.Width(centerWidth).Align(lipgloss.Center).Render(centerSection)
	rightStyled := styles.StatusBarStyle.Align(lipgloss.Right).Render(rightSection)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftStyled,
		centerStyled,
		rightStyled,
	)
}

// renderLeftSection renders the left side of the status bar (resource count, errors)
func (m StatusBarModel) renderLeftSection() string {
	var parts []string

	// Resource count
	resourceText := fmt.Sprintf("Resources: %d", m.resourceCount)
	parts = append(parts, resourceText)

	// Error count
	if m.errorCount > 0 {
		errorText := styles.ErrorStyle.Render(fmt.Sprintf("Errors: %d", m.errorCount))
		parts = append(parts, errorText)
	}

	// Active providers
	if m.showProviders && len(m.activeProviders) > 0 {
		providerText := fmt.Sprintf("Providers: %s", joinProviders(m.activeProviders))
		parts = append(parts, providerText)
	}

	return joinParts(parts, " │ ")
}

// renderCenterSection renders the center of the status bar (scan status, progress)
func (m StatusBarModel) renderCenterSection() string {
	if m.scanStatus != nil && m.scanStatus.Active {
		return m.renderScanProgress()
	}

	// Show status message with appropriate styling
	return styles.StatusStyle(int(m.statusLevel)).Render(m.statusMessage)
}

// renderRightSection renders the right side of the status bar (time)
func (m StatusBarModel) renderRightSection() string {
	var parts []string

	// Current time
	if m.showTime {
		timeText := m.currentTime.Format("15:04:05")
		parts = append(parts, timeText)
	}

	// Scan duration
	if m.scanStatus != nil && m.scanStatus.Active {
		duration := time.Since(m.scanStatus.StartTime)
		durationText := fmt.Sprintf("⏱ %v", duration.Round(time.Second))
		parts = append(parts, durationText)
	}

	return joinParts(parts, " │ ")
}

// renderScanProgress renders the scan progress indicator
func (m StatusBarModel) renderScanProgress() string {
	if m.scanStatus == nil {
		return ""
	}

	// Progress bar
	progressBar := m.renderProgressBar(m.scanStatus.Progress, 20)

	// Progress text
	progressText := fmt.Sprintf("%.0f%%", m.scanStatus.Progress*100)

	// Current operation
	operation := m.scanStatus.Operation
	if len(operation) > 25 {
		operation = operation[:22] + "..."
	}

	return fmt.Sprintf("%s %s %s",
		progressBar,
		styles.InfoStyle.Render(progressText),
		styles.SubtitleStyle.Render(operation),
	)
}

// renderProgressBar renders a text-based progress bar
func (m StatusBarModel) renderProgressBar(progress float64, width int) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	completed := int(progress * float64(width))
	remaining := width - completed

	var bar string
	for i := 0; i < completed; i++ {
		bar += "█"
	}
	for i := 0; i < remaining; i++ {
		bar += "░"
	}

	return styles.ProgressBarStyle.Render(bar)
}

// SetScanStatus updates the scan status
func (m *StatusBarModel) SetScanStatus(status *types.ScanStatus) {
	m.scanStatus = status
}

// SetProviders updates the active providers list
func (m *StatusBarModel) SetProviders(providers []string) {
	m.activeProviders = providers
}

// SetStatusMessage updates the status message
func (m *StatusBarModel) SetStatusMessage(message string, level types.StatusLevel) {
	m.statusMessage = message
	m.statusLevel = level
}

// Resize updates the status bar width
func (m *StatusBarModel) Resize(width int) {
	m.width = width
}

// Helper functions

func joinProviders(providers []string) string {
	if len(providers) == 0 {
		return "none"
	}

	var styledProviders []string
	for _, provider := range providers {
		styled := styles.ProviderStyle(provider).Render(provider)
		styledProviders = append(styledProviders, styled)
	}

	if len(styledProviders) <= 3 {
		return joinParts(styledProviders, ", ")
	}

	return fmt.Sprintf("%s +%d more",
		joinParts(styledProviders[:2], ", "),
		len(styledProviders)-2)
}

func joinParts(parts []string, separator string) string {
	var result string
	for i, part := range parts {
		if i > 0 {
			result += separator
		}
		result += part
	}
	return result
}

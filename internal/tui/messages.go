package tui

// Re-export types from types package to maintain compatibility
import "github.com/jlgore/corkscrew/internal/tui/types"

type ViewType = types.ViewType
type StatusLevel = types.StatusLevel
type Resource = types.Resource
type ScanStatus = types.ScanStatus

type SwitchViewMsg = types.SwitchViewMsg
type BackMsg = types.BackMsg
type QuitMsg = types.QuitMsg
type WindowSizeMsg = types.WindowSizeMsg
type StatusUpdateMsg = types.StatusUpdateMsg
type ErrorMsg = types.ErrorMsg
type DatabaseConnectedMsg = types.DatabaseConnectedMsg
type DatabaseErrorMsg = types.DatabaseErrorMsg
type ConfigLoadedMsg = types.ConfigLoadedMsg
type ConfigSavedMsg = types.ConfigSavedMsg
type ScanStartedMsg = types.ScanStartedMsg
type ScanProgressMsg = types.ScanProgressMsg
type ScanCompleteMsg = types.ScanCompleteMsg
type ResourcesLoadedMsg = types.ResourcesLoadedMsg
type ResourceSelectedMsg = types.ResourceSelectedMsg
type ToggleHelpMsg = types.ToggleHelpMsg
type TickMsg = types.TickMsg

// Re-export constants
const (
	ViewMain       = types.ViewMain
	ViewScan       = types.ViewScan
	ViewResults    = types.ViewResults
	ViewConfig     = types.ViewConfig
	ViewDiagrams   = types.ViewDiagrams
	ViewCompliance = types.ViewCompliance
	ViewQuery      = types.ViewQuery
)

const (
	StatusInfo    = types.StatusInfo
	StatusSuccess = types.StatusSuccess
	StatusWarning = types.StatusWarning
	StatusError   = types.StatusError
)
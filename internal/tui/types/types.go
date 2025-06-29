package types

import (
	"time"
)

// ViewType represents different TUI views
type ViewType int

const (
	ViewMain ViewType = iota
	ViewScan
	ViewResults
	ViewConfig
	ViewDiagrams
	ViewCompliance
	ViewQuery
)

// StatusLevel represents message severity
type StatusLevel int

const (
	StatusInfo StatusLevel = iota
	StatusSuccess
	StatusWarning
	StatusError
)

// Resource represents a cloud resource
type Resource struct {
	ID         string
	Type       string
	Provider   string
	Region     string
	Service    string
	Name       string
	Attributes map[string]interface{}
	Tags       map[string]string
	CreatedAt  time.Time
}

// ScanStatus represents the current scan state
type ScanStatus struct {
	Active     bool
	Progress   float64
	Operation  string
	StartTime  time.Time
	Resources  int
	Errors     int
	Providers  []string
}

// Core TUI Messages
type (
	// Navigation messages
	SwitchViewMsg struct {
		View ViewType
		Data interface{}
	}

	BackMsg struct{}

	QuitMsg struct{}

	// Window size message
	WindowSizeMsg struct {
		Width  int
		Height int
	}

	// Status messages
	StatusUpdateMsg struct {
		Message string
		Level   StatusLevel
	}

	ErrorMsg struct {
		Error error
	}

	// Database messages
	DatabaseConnectedMsg struct {
		Database interface{} // *db.GraphLoader
	}

	DatabaseErrorMsg struct {
		Error error
	}

	// Config messages
	ConfigLoadedMsg struct {
		Config interface{} // *config.Config
		Error  error
	}

	ConfigSavedMsg struct {
		Error error
	}

	// Scan messages
	ScanStartedMsg struct {
		ScanID string
		Error  error
	}

	ScanProgressMsg struct {
		ScanID     string
		Progress   float64
		Operation  string
		Resources  int
		Errors     int
	}

	ScanCompleteMsg struct {
		ScanID    string
		Resources int
		Duration  time.Duration
		Error     error
	}

	// Results messages
	ResourcesLoadedMsg struct {
		Resources []Resource
		Total     int
		Page      int
		Error     error
	}

	ResourceSelectedMsg struct {
		Resource *Resource
	}

	// Help messages
	ToggleHelpMsg struct{}

	// Tick messages for periodic updates
	TickMsg struct {
		Time time.Time
	}
)
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jlgore/corkscrew/internal/config"
	"github.com/jlgore/corkscrew/internal/db"
	"github.com/jlgore/corkscrew/internal/tui"
	"github.com/jlgore/corkscrew/pkg/crosscloud"
)

// runTUIMode starts the interactive TUI interface
func runTUIMode(args []string) error {
	// Parse TUI-specific flags
	var (
		dbPath     = "./corkscrew.db"
		configPath = "./corkscrew.yaml"
		startView  = "main"
	)

	// Parse args for specific starting views
	if len(args) > 0 {
		switch args[0] {
		case "scan":
			startView = "scan"
		case "results":
			startView = "results"
		case "config":
			startView = "config"
		case "diagrams":
			startView = "diagrams"
		case "query":
			startView = "query"
		case "compliance":
			startView = "compliance"
		default:
			startView = "main"
		}
	}

	// Initialize dependencies
	database, config, scanner, err := initializeTUIDependencies(dbPath, configPath)
	if err != nil {
		return fmt.Errorf("failed to initialize TUI dependencies: %w", err)
	}

	// Create TUI application
	app := tui.NewCorkscrewApp()
	app.SetDependencies(database, config, scanner)

	// Set initial view based on arguments
	switch startView {
	case "scan":
		app.StartWithScanView()
	case "results":
		app.StartWithResultsView()
	case "config":
		app.StartWithConfigView()
	case "main":
		fallthrough
	default:
		app.StartWithMainMenu()
	}

	// Configure and start the TUI
	program := tea.NewProgram(
		app,
		tea.WithAltScreen(),        // Use alt screen buffer
		tea.WithMouseCellMotion(),  // Enable mouse support
		tea.WithoutSignalHandler(), // Handle signals ourselves
	)

	// Set up graceful shutdown
	setupTUISignalHandling(program)

	// Start the program
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI failed to start: %w", err)
	}

	return nil
}

// initializeTUIDependencies initializes all required dependencies for the TUI
// Returns (database, config, scanner, error) as interfaces to decouple TUI from specific implementations
func initializeTUIDependencies(dbPath, configPath string) (interface{}, interface{}, interface{}, error) {
	// Initialize database
	database, err := db.NewGraphLoader(dbPath)
	if err != nil {
		fmt.Printf("Warning: Failed to initialize database (%v), some features may be limited\n", err)
		database = nil
	}

	// Load configuration
	cfg, err := config.LoadServiceConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to load configuration (%v), using defaults\n", err)
		cfg = nil
	}

	// Initialize CrossCloud orchestrator
	// Now that GraphLoader implements DatabaseInterface, we can use CrossCloudOrchestrator
	var scanner interface{}
	if database != nil {
		// Create CrossCloud orchestrator with the database
		crossCloudOrchestrator := crosscloud.NewCrossCloudOrchestrator(database)
		scanner = crossCloudOrchestrator
	}

	return database, cfg, scanner, nil
}

// setupTUISignalHandling sets up graceful shutdown for the TUI
func setupTUISignalHandling(program *tea.Program) {
	// This would set up signal handlers for graceful shutdown
	// For now, rely on BubbleTea's built-in signal handling
}

// addTUIFlags adds TUI-related flags to existing commands
func addTUIFlags() {
	// This function would add TUI flags to existing commands
	// Since this project uses a custom command structure, we'll handle
	// TUI flags in the main function instead
}

// checkTUIMode checks if TUI mode should be launched based on flags
func checkTUIMode(cmdName string, args []string) bool {
	// Check for global --tui flag
	for _, arg := range os.Args {
		if arg == "--tui" || arg == "-t" {
			return true
		}
	}

	// Check for command-specific TUI mode
	switch cmdName {
	case "scan":
		return containsFlag(args, "--tui") || containsFlag(args, "-t")
	case "query":
		return containsFlag(args, "--tui") || containsFlag(args, "-t")
	case "config":
		return containsFlag(args, "--tui") || containsFlag(args, "-t")
	case "diagrams":
		return containsFlag(args, "--tui") || containsFlag(args, "-t")
	}

	return false
}

// containsFlag checks if a flag is present in the arguments
func containsFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// fallbackToCLI gracefully falls back to CLI mode if TUI fails
func fallbackToCLI(err error, originalArgs []string) {
	fmt.Printf("TUI mode unavailable (%v), falling back to CLI mode\n", err)
	
	// Remove --tui flags from args
	var cleanArgs []string
	for _, arg := range originalArgs {
		if arg != "--tui" && arg != "-t" {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	// Execute the original command in CLI mode
	// This would call the appropriate CLI function based on the command
	// For now, just exit with error
	fmt.Println("CLI fallback not implemented in this version")
	os.Exit(1)
}

// TUI command implementations for integration with existing CLI

// runScanTUI starts the scan view in TUI mode
func runScanTUI() error {
	return runTUIMode([]string{"scan"})
}

// runQueryTUI starts the query builder in TUI mode
func runQueryTUI() error {
	return runTUIMode([]string{"query"})
}

// runConfigTUI starts the configuration wizard in TUI mode
func runConfigTUI() error {
	return runTUIMode([]string{"config"})
}

// runDiagramsTUI starts the diagram viewer in TUI mode
func runDiagramsTUI() error {
	return runTUIMode([]string{"diagrams"})
}

// runResultsTUI starts the results browser in TUI mode
func runResultsTUI() error {
	return runTUIMode([]string{"results"})
}

// Helper function to check if terminal supports TUI
func isTUISupported() bool {
	// Check if we're in a terminal that supports TUI
	if os.Getenv("TERM") == "" {
		return false
	}

	// Check if stdout is a terminal
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		return false
	}

	return true
}

// TUI launcher with validation
func launchTUI(args []string) error {
	// Validate TUI support
	if !isTUISupported() {
		return fmt.Errorf("TUI mode requires a compatible terminal")
	}

	// Check terminal size
	// This would check if terminal is large enough for TUI
	// For now, just proceed

	// Launch TUI
	return runTUIMode(args)
}

// Main TUI entry point that can be called from main()
func handleTUIRequest(cmdName string, args []string) bool {
	if !checkTUIMode(cmdName, args) {
		return false
	}

	var tuiArgs []string
	switch cmdName {
	case "scan":
		tuiArgs = []string{"scan"}
	case "query":
		tuiArgs = []string{"query"}
	case "config":
		tuiArgs = []string{"config"}
	case "diagrams":
		tuiArgs = []string{"diagrams"}
	default:
		tuiArgs = []string{"main"}
	}

	if err := launchTUI(tuiArgs); err != nil {
		fallbackToCLI(err, args)
		return true
	}

	return true
}
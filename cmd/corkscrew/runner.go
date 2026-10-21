package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func runCLI(args []string) int {
	if len(args) > 0 && (args[0] == "--tui" || args[0] == "-t") {
		if err := runTUIMode(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start TUI: %v\n", err)
			return 1
		}
		return 0
	}

	if len(args) == 0 {
		printUsage()
		return 1
	}

	command := args[0]
	commandArgs := args[1:]

	if handleTUIRequest(command, commandArgs) {
		return 0
	}

	// One root context for the whole command: SIGINT/SIGTERM cancel registry downloads, plugin
	// RPCs and remote queries instead of leaving them to hang. Created after the TUI branch so
	// the TUI keeps its own key handling.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal, restore default handling so a second Ctrl-C still kills a
	// command that does not observe ctx.
	context.AfterFunc(ctx, stop)

	return runCommand(ctx, command, commandArgs)
}

// runCommand dispatches one CLI command under the root context.
func runCommand(ctx context.Context, command string, commandArgs []string) int {
	switch command {
	case "init":
		runInit(ctx, commandArgs)
	case "scan":
		if err := runScanE(ctx, commandArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Scan failed: %v\n", err)
			return 1
		}
	case "scans":
		return runScans(ctx, commandArgs)
	case "drift":
		return runDrift(ctx, commandArgs)
	case "findings":
		return runFindings(ctx, commandArgs)
	case "discover":
		runDiscover(ctx, commandArgs)
	case "list":
		runList(ctx, commandArgs)
	case "describe":
		runDescribe(ctx, commandArgs)
	case "info":
		runInfo(ctx, commandArgs)
	case "schemas":
		runSchemas(ctx, commandArgs)
	case "query":
		runQuery(ctx, commandArgs)
	case "diagram":
		printUnavailableCommandMessage(os.Stderr, "diagram", "diagram generation is not wired into this binary")
		return 1
	case "config":
		if err := runConfigE(commandArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Config failed: %v\n", err)
			return 1
		}
	case "plugin":
		if err := runPluginE(ctx, commandArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Plugin failed: %v\n", err)
			return 1
		}
	case "pack":
		runPack(ctx, commandArgs)
	case "aws-org":
		runAWSOrg(ctx, commandArgs)
	case "serve":
		runServe(ctx, commandArgs)
	case "quack":
		runQuack(ctx, commandArgs)
	case "graph":
		return runGraph(ctx, commandArgs)
	case "github":
		runGitHub(ctx, commandArgs)
	case "cloudflare":
		runCloudflare(ctx, commandArgs)
	case "crosscloud":
		runCrossCloud(commandArgs)
	case "correlate":
		runCorrelate(commandArgs)
	case "version", "--version", "-v":
		fmt.Printf("Corkscrew %s (commit: %s, built: %s)\n", version, commit, date)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		return 1
	}

	return 0
}

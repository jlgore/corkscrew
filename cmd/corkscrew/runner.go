package main

import (
	"fmt"
	"os"
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

	switch command {
	case "init":
		runInit(commandArgs)
	case "scan":
		if err := runScanE(commandArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Scan failed: %v\n", err)
			return 1
		}
	case "discover":
		runDiscover(commandArgs)
	case "list":
		runList(commandArgs)
	case "describe":
		runDescribe(commandArgs)
	case "info":
		runInfo(commandArgs)
	case "schemas":
		runSchemas(commandArgs)
	case "query":
		runQuery(commandArgs)
	case "diagram":
		printUnavailableCommandMessage(os.Stderr, "diagram", "diagram generation is not wired into this binary")
		return 1
	case "config":
		if err := runConfigE(commandArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Config failed: %v\n", err)
			return 1
		}
	case "plugin":
		if err := runPluginE(commandArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Plugin failed: %v\n", err)
			return 1
		}
	case "pack":
		runPack(commandArgs)
	case "aws-org":
		runAWSOrg(commandArgs)
	case "serve":
		runServe(commandArgs)
	case "quack":
		runQuack(commandArgs)
	case "graph":
		runGraph(commandArgs)
	case "github":
		runGitHub(commandArgs)
	case "cloudflare":
		runCloudflare(commandArgs)
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

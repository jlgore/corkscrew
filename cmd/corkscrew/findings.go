package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	appfindings "github.com/jlgore/corkscrew/internal/app/findings"
)

func runFindings(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: corkscrew findings evaluate|list|show|suppress|unsuppress")
		return 1
	}
	switch args[0] {
	case "evaluate":
		return runFindingsEvaluate(args[1:])
	case "list":
		return runFindingsList(args[1:])
	case "show":
		return runFindingsShow(args[1:])
	case "suppress":
		return runFindingsSuppress(args[1:])
	case "unsuppress":
		return runFindingsUnsuppress(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown findings subcommand %q\n", args[0])
		return 1
	}
}

func runFindingsEvaluate(args []string) int {
	fs := flag.NewFlagSet("findings evaluate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	scanID := fs.String("scan", "", "Completed scan ID")
	pack := fs.String("pack", "", "Pack reference")
	control := fs.String("control", "", "Control reference")
	tagsValue := fs.String("tags", "", "Comma-separated tags")
	output := fs.String("output", "table", "table|json")
	failSeverity := fs.String("fail-on-severity", "", "low|medium|high|critical")
	params := parameterFlags{}
	fs.Var(params, "param", "key=value parameter")
	if fs.Parse(args) != nil {
		return 1
	}
	if *scanID == "" || (*pack == "" && *control == "" && *tagsValue == "") {
		fmt.Fprintln(os.Stderr, "--scan and one of --pack, --control, or --tags are required")
		return 1
	}
	var tags []string
	for _, tag := range strings.Split(*tagsValue, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	result, err := appfindings.Evaluate(context.Background(), appfindings.EvaluateRequest{Target: *dbPath, ScanID: *scanID,
		PackName: *pack, ControlID: *control, Tags: tags, Parameters: params})
	if err != nil {
		fmt.Fprintf(os.Stderr, "findings evaluation failed: %v\n", err)
		return 1
	}
	if *output == "json" {
		encoded, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("Evaluation %s: %s (opened=%d recurred=%d resolved=%d)\n", result.ID, result.Status, result.Opened, result.Recurred, result.Resolved)
	}
	if *failSeverity != "" {
		open, err := appfindings.List(context.Background(), *dbPath, appfindings.ListFilter{Status: "open", MinSeverity: *failSeverity})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(open) > 0 {
			return 2
		}
	}
	return 0
}

func runFindingsList(args []string) int {
	fs := flag.NewFlagSet("findings list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	status := fs.String("status", "", "open|resolved|suppressed")
	provider := fs.String("provider", "", "Provider filter")
	control := fs.String("control", "", "Control filter")
	scanID := fs.String("scan", "", "Last-seen scan filter")
	pack := fs.String("pack", "", "Pack filter")
	severity := fs.String("min-severity", "", "Minimum severity")
	output := fs.String("output", "table", "table|json")
	if fs.Parse(args) != nil {
		return 1
	}
	rows, err := appfindings.List(context.Background(), *dbPath, appfindings.ListFilter{Status: *status, Provider: *provider, ControlID: *control, MinSeverity: *severity, ScanID: *scanID, PackRef: *pack})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	renderFindings(rows, *output)
	return 0
}

func runFindingsShow(args []string) int {
	fs := flag.NewFlagSet("findings show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	output := fs.String("output", "json", "table|json")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: corkscrew findings show <fingerprint>")
		return 1
	}
	rows, err := appfindings.List(context.Background(), *dbPath, appfindings.ListFilter{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, row := range rows {
		if row.Fingerprint == fs.Arg(0) {
			renderFindings([]appfindings.Finding{row}, *output)
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "finding not found")
	return 1
}

func runFindingsSuppress(args []string) int {
	fs := flag.NewFlagSet("findings suppress", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	reason := fs.String("reason", "", "Required suppression reason")
	untilValue := fs.String("until", "", "Optional RFC3339 expiry")
	actor := fs.String("by", os.Getenv("USER"), "Actor")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: corkscrew findings suppress <fingerprint> --reason <text>")
		return 1
	}
	var until *time.Time
	if *untilValue != "" {
		parsed, err := time.Parse(time.RFC3339, *untilValue)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		until = &parsed
	}
	if err := appfindings.Suppress(context.Background(), *dbPath, fs.Arg(0), *reason, *actor, until); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runFindingsUnsuppress(args []string) int {
	fs := flag.NewFlagSet("findings unsuppress", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: corkscrew findings unsuppress <fingerprint>")
		return 1
	}
	if err := appfindings.Unsuppress(context.Background(), *dbPath, fs.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func renderFindings(rows []appfindings.Finding, output string) {
	if output == "json" {
		encoded, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(encoded))
		return
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "STATUS\tSEVERITY\tCONTROL\tPROVIDER\tRESOURCE\tFINGERPRINT")
	for _, row := range rows {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Status, row.Severity, row.ControlID, row.Provider, row.ResourceID, row.Fingerprint)
	}
	_ = writer.Flush()
}

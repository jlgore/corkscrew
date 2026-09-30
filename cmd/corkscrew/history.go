package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	appdrift "github.com/jlgore/corkscrew/internal/app/drift"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
)

func runScans(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: corkscrew scans list|show [options]")
		return 1
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("scans list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		dbPath := fs.String("db", defaultDatabasePath(), "Database path")
		provider := fs.String("provider", "", "Provider filter")
		limit := fs.Int("limit", 50, "Maximum scans")
		output := fs.String("output", "table", "table|json|csv")
		if fs.Parse(args[1:]) != nil {
			return 1
		}
		session, err := dataaccess.OpenSession(context.Background(), *dbPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer session.Close()
		scans, err := session.ListScans(context.Background(), *provider, *limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return renderScans(scans, *output)
	case "show":
		fs := flag.NewFlagSet("scans show", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		dbPath := fs.String("db", defaultDatabasePath(), "Database path")
		output := fs.String("output", "table", "table|json")
		if fs.Parse(args[1:]) != nil {
			return 1
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: corkscrew scans show <scan-id>")
			return 1
		}
		session, err := dataaccess.OpenSession(context.Background(), *dbPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer session.Close()
		scan, err := session.GetScan(context.Background(), fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return renderScans([]dataaccess.ScanRecord{scan}, *output)
	default:
		fmt.Fprintf(os.Stderr, "unknown scans subcommand %q\n", args[0])
		return 1
	}
}

func runDrift(args []string) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	provider := fs.String("provider", "", "Provider for automatic scan selection")
	from := fs.String("from", "", "Baseline scan ID")
	to := fs.String("to", "", "Target scan ID")
	output := fs.String("output", "table", "table|json|csv")
	failOnChange := fs.Bool("fail-on-change", false, "Return exit 2 when changes exist")
	if fs.Parse(args) != nil {
		return 1
	}
	result, err := appdrift.Run(context.Background(), appdrift.Request{Target: *dbPath, Provider: *provider, FromScan: *from, ToScan: *to})
	if err != nil {
		fmt.Fprintf(os.Stderr, "drift failed: %v\n", err)
		return 1
	}
	if renderDrift(result, *output) != 0 {
		return 1
	}
	if *failOnChange && len(result.Changes) > 0 {
		return 2
	}
	return 0
}

func renderScans(scans []dataaccess.ScanRecord, output string) int {
	switch output {
	case "json":
		encoded, _ := json.MarshalIndent(scans, "", "  ")
		fmt.Println(string(encoded))
	case "csv":
		writer := csv.NewWriter(os.Stdout)
		_ = writer.Write([]string{"id", "provider", "status", "started_at", "resources", "new", "updated", "deleted", "scope_key"})
		for _, scan := range scans {
			_ = writer.Write([]string{scan.ID, scan.Provider, scan.Status, scan.StartedAt.Format(time.RFC3339),
				strconv.Itoa(scan.TotalResources), strconv.Itoa(scan.NewResources), strconv.Itoa(scan.UpdatedResources),
				strconv.Itoa(scan.DeletedResources), scan.ScopeKey})
		}
		writer.Flush()
	default:
		writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tPROVIDER\tSTATUS\tSTARTED\tRESOURCES\t+\t~\t-")
		for _, scan := range scans {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\n", scan.ID, scan.Provider, scan.Status,
				scan.StartedAt.Format(time.RFC3339), scan.TotalResources, scan.NewResources, scan.UpdatedResources, scan.DeletedResources)
		}
		_ = writer.Flush()
	}
	return 0
}

func renderDrift(result appdrift.Result, output string) int {
	switch output {
	case "json":
		encoded, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(encoded))
	case "csv":
		writer := csv.NewWriter(os.Stdout)
		_ = writer.Write([]string{"change_type", "provider", "resource_id", "changed_fields"})
		for _, change := range result.Changes {
			fields, _ := json.Marshal(change.ChangedFields)
			_ = writer.Write([]string{string(change.Type), change.Provider, change.ResourceID, string(fields)})
		}
		writer.Flush()
	default:
		fmt.Printf("Drift %s -> %s (comparable=%t, incomplete=%t)\n", result.From.ID, result.To.ID, result.Comparable, result.Incomplete)
		writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "CHANGE\tPROVIDER\tRESOURCE\tFIELDS")
		for _, change := range result.Changes {
			fields, _ := json.Marshal(change.ChangedFields)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", change.Type, change.Provider, change.ResourceID, string(fields))
		}
		_ = writer.Flush()
	}
	return 0
}

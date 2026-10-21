package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	appdrift "github.com/jlgore/corkscrew/internal/app/drift"
	dataaccess "github.com/jlgore/corkscrew/internal/data"
)

func runScans(ctx context.Context, args []string) int {
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
		session, err := dataaccess.OpenSession(ctx, *dbPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer session.Close()
		scans, err := session.ListScans(ctx, *provider, *limit)
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
		session, err := dataaccess.OpenSession(ctx, *dbPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer session.Close()
		scan, err := session.GetScan(ctx, fs.Arg(0))
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

func runDrift(ctx context.Context, args []string) int {
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
	result, err := appdrift.Run(ctx, appdrift.Request{Target: *dbPath, Provider: *provider, FromScan: *from, ToScan: *to})
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

// renderScans writes scans to stdout. Output errors (a closed pipe, a full
// disk) are reported and fail the command rather than truncating silently.
func renderScans(scans []dataaccess.ScanRecord, output string) int {
	if err := writeScans(os.Stdout, scans, output); err != nil {
		fmt.Fprintf(os.Stderr, "write scans: %v\n", err)
		return 1
	}
	return 0
}

func writeScans(out io.Writer, scans []dataaccess.ScanRecord, output string) error {
	switch output {
	case "json":
		return writeJSON(out, scans)
	case "csv":
		writer := csv.NewWriter(out)
		header := []string{"id", "provider", "status", "started_at", "resources",
			"new", "updated", "deleted", "scope_key"}
		if err := writer.Write(header); err != nil {
			return err
		}
		for _, scan := range scans {
			record := []string{scan.ID, scan.Provider, scan.Status,
				scan.StartedAt.Format(time.RFC3339), strconv.Itoa(scan.TotalResources),
				strconv.Itoa(scan.NewResources), strconv.Itoa(scan.UpdatedResources),
				strconv.Itoa(scan.DeletedResources), scan.ScopeKey}
			if err := writer.Write(record); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	default:
		writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tPROVIDER\tSTATUS\tSTARTED\tRESOURCES\t+\t~\t-")
		for _, scan := range scans {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\n", scan.ID, scan.Provider,
				scan.Status, scan.StartedAt.Format(time.RFC3339), scan.TotalResources,
				scan.NewResources, scan.UpdatedResources, scan.DeletedResources)
		}
		// tabwriter buffers every row until Flush, so Flush reports any
		// write error from the rows above.
		return writer.Flush()
	}
}

func renderDrift(result appdrift.Result, output string) int {
	if err := writeDrift(os.Stdout, result, output); err != nil {
		fmt.Fprintf(os.Stderr, "write drift: %v\n", err)
		return 1
	}
	return 0
}

func writeDrift(out io.Writer, result appdrift.Result, output string) error {
	switch output {
	case "json":
		return writeJSON(out, result)
	case "csv":
		writer := csv.NewWriter(out)
		header := []string{"change_type", "provider", "resource_id", "changed_fields"}
		if err := writer.Write(header); err != nil {
			return err
		}
		for _, change := range result.Changes {
			fields, err := json.Marshal(change.ChangedFields)
			if err != nil {
				return fmt.Errorf("encode changed fields of %s: %w", change.ResourceID, err)
			}
			record := []string{string(change.Type), change.Provider, change.ResourceID,
				string(fields)}
			if err := writer.Write(record); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	default:
		fmt.Fprintf(out, "Drift %s -> %s (comparable=%t, incomplete=%t)\n",
			result.From.ID, result.To.ID, result.Comparable, result.Incomplete)
		writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "CHANGE\tPROVIDER\tRESOURCE\tFIELDS")
		for _, change := range result.Changes {
			fields, err := json.Marshal(change.ChangedFields)
			if err != nil {
				return fmt.Errorf("encode changed fields of %s: %w", change.ResourceID, err)
			}
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", change.Type, change.Provider,
				change.ResourceID, string(fields))
		}
		return writer.Flush()
	}
}

// writeJSON writes value as indented JSON followed by a newline.
func writeJSON(out io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(encoded))
	return err
}

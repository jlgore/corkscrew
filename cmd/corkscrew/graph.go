package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	appcorrelation "github.com/jlgore/corkscrew/internal/app/correlation"
	contract "github.com/jlgore/corkscrew/pkg/correlation"
	"github.com/jlgore/corkscrew/pkg/graphquery"
)

func runGraph(args []string) int {
	if len(args) > 0 && args[0] == "refresh-correlations" {
		return runRefreshCorrelations(args[1:])
	}
	if len(args) > 0 && args[0] == "correlation-status" {
		return runCorrelationStatus(args[1:])
	}
	return graphquery.NewRunner(os.Stdout, os.Stderr).Run(args)
}

func runRefreshCorrelations(args []string) int {
	fs := flag.NewFlagSet("graph refresh-correlations", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	kindsValue := fs.String("kinds", "all", "Comma-separated correlation kinds")
	output := fs.String("output", "table", "table|json")
	if fs.Parse(args) != nil {
		return 1
	}
	kinds, err := parseEvidenceKinds(*kindsValue)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	result, err := appcorrelation.Refresh(context.Background(), appcorrelation.Request{Target: *dbPath, Kinds: kinds})
	if err != nil {
		fmt.Fprintf(os.Stderr, "correlation refresh failed: %v\n", err)
		return 1
	}
	renderCorrelationRun(result, *output)
	return 0
}

func runCorrelationStatus(args []string) int {
	fs := flag.NewFlagSet("graph correlation-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", defaultDatabasePath(), "Database path")
	output := fs.String("output", "table", "table|json")
	if fs.Parse(args) != nil {
		return 1
	}
	result, err := appcorrelation.Latest(context.Background(), *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "correlation status failed: %v\n", err)
		return 1
	}
	renderCorrelationRun(result, *output)
	return 0
}

func parseEvidenceKinds(value string) ([]contract.Kind, error) {
	if strings.TrimSpace(value) == "" || strings.EqualFold(strings.TrimSpace(value), "all") {
		return nil, nil
	}
	allowed := map[contract.Kind]bool{}
	for _, kind := range contract.AllKinds() {
		allowed[kind] = true
	}
	var result []contract.Kind
	for _, part := range strings.Split(value, ",") {
		kind := contract.Kind(strings.TrimSpace(part))
		if !allowed[kind] {
			return nil, fmt.Errorf("unsupported correlation kind %q", part)
		}
		result = append(result, kind)
	}
	return result, nil
}

func renderCorrelationRun(result appcorrelation.Result, output string) {
	if output == "json" {
		encoded, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(encoded))
		return
	}
	fmt.Printf("Correlation materialization %s: %s\n", result.RunID, result.Status)
	fmt.Printf("Evidence: %d  Rows: %d  Rejected: %d\n", result.EvidenceCount, result.RowCount, result.RejectedCount)
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "KIND:PROVIDER\tEVIDENCE")
	keys := make([]string, 0, len(result.Coverage))
	for key := range result.Coverage {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		count := result.Coverage[key]
		fmt.Fprintf(writer, "%s\t%d\n", key, count)
	}
	_ = writer.Flush()
}

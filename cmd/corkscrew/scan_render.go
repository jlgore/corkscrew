package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
)

func renderScanOutcome(output, warnings io.Writer, request scanapp.Request, outcome scanapp.Outcome) error {
	notices := output
	if request.OutputFormat == "json" || request.OutputFormat == "csv" {
		notices = warnings
	}
	for _, expansion := range outcome.Expansions {
		fmt.Fprintf(notices, "📦 Expanding group '%s' to: %s\n", expansion.Name, strings.Join(expansion.Services, ", "))
	}
	for _, warning := range outcome.Warnings {
		fmt.Fprintf(warnings, "⚠️ Warning: %s\n", strings.TrimSpace(warning.Message))
	}
	if outcome.Persisted {
		fmt.Fprintf(notices, "💾 Results stored to database\n")
	}

	switch request.OutputFormat {
	case "json":
		if err := json.NewEncoder(output).Encode(outcome); err != nil {
			return fmt.Errorf("render scan JSON: %w", err)
		}
	case "csv":
		writer := csv.NewWriter(output)
		if err := writer.Write([]string{"Provider", "Scope", "Service", "Type", "Name", "ID"}); err != nil {
			return err
		}
		for _, resource := range outcome.Resources {
			if resource == nil {
				continue
			}
			if err := writer.Write([]string{resource.Provider, resource.Region, resource.Service, resource.Type, resource.Name, resource.Id}); err != nil {
				return err
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return fmt.Errorf("render scan CSV: %w", err)
		}
	default:
		fmt.Fprintf(output, "\nScan %s: %d resources in %s\n", outcome.Status, len(outcome.Resources), outcome.Duration.Round(time.Millisecond))
		writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "SCOPE\tSERVICE\tTYPE\tNAME")
		for _, resource := range outcome.Resources {
			if resource == nil {
				continue
			}
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", resource.Region, resource.Service, resource.Type, resource.Name)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("render scan table: %w", err)
		}
	}

	if request.SaveToFile {
		filename := fmt.Sprintf("enhanced-scan-%s-%s.json", outcome.Provider, time.Now().Format("20060102-150405"))
		if err := saveScanOutcome(filename, outcome); err != nil {
			return fmt.Errorf("save scan output: %w", err)
		}
		fmt.Fprintf(notices, "💾 Results saved to file: %s\n", filename)
	}
	return nil
}

func saveScanOutcome(filename string, outcome scanapp.Outcome) error {
	data, err := json.MarshalIndent(outcome, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal scan output: %w", err)
	}
	return os.WriteFile(filename, data, 0o644)
}

package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/scanexec"
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
		_, err := fmt.Fprintf(output, "\nScan %s: %d resources in %s\n", outcome.Status,
			countResources(outcome.Resources), outcome.Duration.Round(time.Millisecond))
		if err != nil {
			return fmt.Errorf("render scan summary: %w", err)
		}
		if err := renderScanTable(output, request, outcome); err != nil {
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
	return renderScanNextSteps(notices, outcome)
}

// renderScanTable lists every resource only on request: a real account holds thousands, and
// counts by service and type are far shorter.
func renderScanTable(output io.Writer, request scanapp.Request, outcome scanapp.Outcome) error {
	if request.ListResources {
		return renderResourceList(output, outcome.Resources)
	}
	if countResources(outcome.Resources) == 0 {
		return nil
	}
	if err := renderResourceCounts(output, outcome.Resources); err != nil {
		return err
	}
	_, err := io.WriteString(output,
		"List each resource with --list-resources, or use --output json.\n")
	return err
}

func renderResourceList(output io.Writer, resources []*pb.Resource) error {
	writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "SCOPE\tSERVICE\tTYPE\tNAME"); err != nil {
		return err
	}
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		_, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
			resource.Region, resource.Service, resource.Type, resource.Name)
		if err != nil {
			return err
		}
	}
	return writer.Flush()
}

func renderResourceCounts(output io.Writer, resources []*pb.Resource) error {
	type serviceType struct{ service, kind string }
	counts := map[serviceType]int{}
	for _, resource := range resources {
		if resource != nil {
			counts[serviceType{resource.Service, resource.Type}]++
		}
	}
	keys := make([]serviceType, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].service != keys[j].service {
			return keys[i].service < keys[j].service
		}
		return keys[i].kind < keys[j].kind
	})
	writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "SERVICE\tTYPE\tCOUNT"); err != nil {
		return err
	}
	for _, key := range keys {
		_, err := fmt.Fprintf(writer, "%s\t%s\t%d\n", key.service, key.kind, counts[key])
		if err != nil {
			return err
		}
	}
	return writer.Flush()
}

func countResources(resources []*pb.Resource) int {
	count := 0
	for _, resource := range resources {
		if resource != nil {
			count++
		}
	}
	return count
}

// renderScanNextSteps prints the scan ID last, so it stays visible below a long resource table,
// and says how to evaluate the scan. findings.Evaluate accepts only completed snapshots.
func renderScanNextSteps(notices io.Writer, outcome scanapp.Outcome) error {
	var text string
	switch {
	case !outcome.Persisted:
		text = fmt.Sprintf("\nScan ID: %s (not stored, so it can't be queried or evaluated)\n",
			outcome.ID)
	case outcome.Status == scanexec.StatusComplete:
		text = fmt.Sprintf("\nScan ID: %s\n", outcome.ID) +
			fmt.Sprintf("Evaluate it: corkscrew findings evaluate --scan %s --pack <pack>%s\n",
				outcome.ID, databaseFlag(outcome.Database)) +
			"List packs:  corkscrew query --list-packs\n"
	case outcome.Status == scanexec.StatusPartial:
		text = fmt.Sprintf("\nScan ID: %s\n", outcome.ID) +
			"This scan is partial, and only completed scans can be evaluated: " +
			"a partial snapshot can't show that a finding was resolved.\n" +
			"Failed scopes: " + failedScopes(outcome.Scopes) + "\n"
	default:
		text = fmt.Sprintf("\nScan ID: %s\n", outcome.ID)
	}
	if _, err := io.WriteString(notices, text); err != nil {
		return fmt.Errorf("render scan next steps: %w", err)
	}
	return nil
}

// databaseFlag names the scan's database for `findings evaluate`, whose --db defaults to
// defaultDatabasePath() and reads neither --database nor the config file.
func databaseFlag(database string) string {
	if database == "" || database == defaultDatabasePath() {
		return ""
	}
	if strings.ContainsAny(database, " \t'\"$`\\") {
		database = "'" + strings.ReplaceAll(database, "'", `'\''`) + "'"
	}
	return " --db " + database
}

func failedScopes(scopes []scanapp.ScopeResult) string {
	const shown = 5
	var names []string
	for _, scope := range scopes {
		if len(scope.Errors) > 0 {
			names = append(names, scope.Scope)
		}
	}
	if len(names) > shown {
		return fmt.Sprintf("%s, and %d more", strings.Join(names[:shown], ", "), len(names)-shown)
	}
	return strings.Join(names, ", ")
}

func saveScanOutcome(filename string, outcome scanapp.Outcome) error {
	data, err := json.MarshalIndent(outcome, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal scan output: %w", err)
	}
	return os.WriteFile(filename, data, 0o644)
}

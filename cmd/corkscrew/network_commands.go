package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jlgore/corkscrew/pkg/graphquery"
)

// NetworkAnalysisOptions contains options for graph-backed cross-cloud analysis
// commands. Provider and region filters are kept for CLI compatibility; the
// Rust graph extension currently reads normalized correlation tables directly.
type NetworkAnalysisOptions struct {
	Providers           []string `json:"providers"`
	Regions             []string `json:"regions"`
	CorrelationTypes    []string `json:"correlation_types"`
	MinConfidence       float64  `json:"min_confidence"`
	OutputFormat        string   `json:"output_format"`
	VisualizationFormat string   `json:"visualization_format"`
	ShowDetails         bool     `json:"show_details"`
	IncludeMetrics      bool     `json:"include_metrics"`
	MaxResults          int      `json:"max_results"`
	SortBy              string   `json:"sort_by"`
	GroupBy             string   `json:"group_by"`
}

// runNetworkAnalysis performs graph-extension-backed cross-cloud analysis.
func runNetworkAnalysis(dbPath string, options NetworkAnalysisOptions) error {
	kinds := options.CorrelationTypes
	if len(kinds) == 0 {
		kinds = []string{"network", "connectivity", "dns", "load-balancer", "security"}
	}
	return runGraphCorrelations(dbPath, kinds, options)
}

// runNetworkTopology now uses the graph extension's explicit network topology
// correlation table function.
func runNetworkTopology(dbPath string, options NetworkAnalysisOptions) error {
	return runGraphCorrelation(dbPath, "network", options)
}

// runCorrelationAnalysis performs specific graph-backed correlation analysis.
func runCorrelationAnalysis(dbPath string, correlationType string, options NetworkAnalysisOptions) error {
	kind, err := graphCorrelationKind(correlationType)
	if err != nil {
		return err
	}
	return runGraphCorrelation(dbPath, kind, options)
}

// runNetworkScan used to invoke the removed Go crosscloud orchestrator. Scans
// are now the responsibility of `corkscrew scan`; graph correlation consumes the
// normalized DuckDB tables produced by scans.
func runNetworkScan(dbPath string, options NetworkAnalysisOptions) error {
	_ = dbPath
	_ = options
	return fmt.Errorf("crosscloud scan was removed; run `corkscrew scan --database <db>` first, then `corkscrew graph correlate <type> --db <db>`")
}

func runGraphCorrelations(dbPath string, kinds []string, options NetworkAnalysisOptions) error {
	for _, kind := range kinds {
		normalized, err := graphCorrelationKind(kind)
		if err != nil {
			return err
		}
		if normalized == "all" {
			return runGraphCorrelations(dbPath, allGraphCorrelationKinds(), options)
		}
		if len(kinds) > 1 && normalizeOutputFormat(options.OutputFormat) == "table" {
			fmt.Printf("\n=== %s correlations ===\n", normalized)
		}
		if err := runGraphCorrelation(dbPath, normalized, options); err != nil {
			return err
		}
	}
	return nil
}

func runGraphCorrelation(dbPath string, kind string, options NetworkAnalysisOptions) error {
	args := []string{"correlate", kind}
	if dbPath = strings.TrimSpace(dbPath); dbPath != "" {
		args = append(args, "--db", dbPath)
	}
	if options.MinConfidence > 0 {
		args = append(args, "--confidence", fmt.Sprintf("%.6f", options.MinConfidence))
	}
	if output := normalizeOutputFormat(options.OutputFormat); output != "" {
		args = append(args, "--output", output)
	}

	exitCode := graphquery.NewRunner(os.Stdout, os.Stderr).Run(args)
	if exitCode != 0 {
		return fmt.Errorf("graph correlate %s failed with exit code %d", kind, exitCode)
	}
	return nil
}

func graphCorrelationKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "all":
		return "all", nil
	case "ip", "ips":
		return "ip", nil
	case "dns":
		return "dns", nil
	case "network", "networks", "topology":
		return "network", nil
	case "loadbalancer", "load-balancer", "lb":
		return "load-balancer", nil
	case "connectivity", "vpn", "peering", "direct", "directconnect":
		return "connectivity", nil
	case "security", "securitygroup", "security-group":
		return "security", nil
	case "domain", "domains", "certificate", "certificates":
		return "domain", nil
	case "identity", "federation":
		return "identity", nil
	case "policy", "policies":
		return "policy", nil
	case "secret", "secrets":
		return "secret", nil
	default:
		return "", fmt.Errorf("unsupported graph correlation type %q", kind)
	}
}

func parseCorrelationTypes(types string) []string {
	if strings.TrimSpace(types) == "" || strings.EqualFold(strings.TrimSpace(types), "all") {
		return allGraphCorrelationKinds()
	}

	parts := strings.Split(types, ",")
	kinds := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		kind, err := graphCorrelationKind(part)
		if err != nil || kind == "all" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	return kinds
}

func allGraphCorrelationKinds() []string {
	return []string{
		"ip",
		"dns",
		"network",
		"load-balancer",
		"connectivity",
		"security",
		"domain",
		"identity",
		"policy",
		"secret",
	}
}

func normalizeOutputFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table", "ascii", "graph", "mermaid", "dot":
		return "table"
	case "json":
		return "json"
	case "csv":
		return "csv"
	default:
		return format
	}
}

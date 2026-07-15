package main

import (
	"fmt"
	"os"

	"github.com/jlgore/corkscrew/internal/app/crosscloud"
)

// NetworkAnalysisOptions collects cross-cloud analysis flags parsed by the CLI.
// Provider and region filters are accepted for compatibility; graph correlation
// reads the normalized correlation tables directly.
type NetworkAnalysisOptions struct {
	Providers           []string
	Regions             []string
	CorrelationTypes    []string
	MinConfidence       float64
	OutputFormat        string
	VisualizationFormat string
	ShowDetails         bool
	IncludeMetrics      bool
	MaxResults          int
	SortBy              string
	GroupBy             string
}

func (o NetworkAnalysisOptions) request(dbPath string, kinds []string) crosscloud.Request {
	return crosscloud.Request{
		DBPath:        dbPath,
		Kinds:         kinds,
		MinConfidence: o.MinConfidence,
		OutputFormat:  o.OutputFormat,
	}
}

// runNetworkAnalysis performs comprehensive graph-backed cross-cloud analysis.
func runNetworkAnalysis(dbPath string, options NetworkAnalysisOptions) error {
	kinds := options.CorrelationTypes
	if len(kinds) == 0 {
		kinds = crosscloud.DefaultNetworkKinds()
	}
	return crosscloud.Correlate(options.request(dbPath, kinds), os.Stdout, os.Stderr)
}

// runNetworkTopology renders the network topology correlation.
func runNetworkTopology(dbPath string, options NetworkAnalysisOptions) error {
	return crosscloud.Correlate(options.request(dbPath, []string{"network"}), os.Stdout, os.Stderr)
}

// runCorrelationAnalysis renders one correlation kind.
func runCorrelationAnalysis(dbPath, correlationType string, options NetworkAnalysisOptions) error {
	return crosscloud.Correlate(options.request(dbPath, []string{correlationType}), os.Stdout, os.Stderr)
}

// runGraphCorrelations renders the given correlation kinds.
func runGraphCorrelations(dbPath string, kinds []string, options NetworkAnalysisOptions) error {
	return crosscloud.Correlate(options.request(dbPath, kinds), os.Stdout, os.Stderr)
}

// runNetworkScan reports that cross-cloud scanning was removed; scans now flow
// through `corkscrew scan`, and graph correlation consumes the normalized tables.
func runNetworkScan(dbPath string, options NetworkAnalysisOptions) error {
	_ = dbPath
	_ = options
	return fmt.Errorf("crosscloud scan was removed; run `corkscrew scan --database <db>` first, then `corkscrew graph correlate <type> --db <db>`")
}

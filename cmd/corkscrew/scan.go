package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
)

// runScanE is the CLI adapter for the scan application workflow. It owns flag
// syntax only; normalization, environment precedence, and execution live in
// internal/app/scan.
func runScanE(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	providerName := fs.String("provider", "aws", "Cloud provider (official or custom plugin name)")
	servicesStr := fs.String("services", "", "Comma-separated list of services (default: from config)")
	regionsStr := fs.String("region", "", "Comma-separated regions or 'all' (default: from config)")
	outputFormat := fs.String("output", "table", "Output format (table, json, csv)")
	showEmpty := fs.Bool("show-empty", false, "Show empty regions and services")
	configPath := fs.String("config", "", "Path to configuration file")
	concurrency := fs.Int("concurrency", 3, "Number of regions to scan concurrently")
	saveToFile := fs.Bool("save", false, "Save results to timestamped JSON file")
	databasePath := fs.String("database", "", "Path to DuckDB database file, or a quack: URI for a remote server (default: config database.path or ~/.corkscrew/db/corkscrew.duckdb)")
	quackToken := fs.String("token", "", "Auth token for a remote quack: database (or CORKSCREW_QUACK_TOKEN)")
	dbProviderTable := fs.String("db-provider-table", "", "Override target table for persistence (routes all rows)")
	namespace := fs.String("namespace", "", "Kubernetes namespace filter (single)")
	labelSelector := fs.String("label-selector", "", "Kubernetes label selector (e.g., app=myapp)")
	fieldSelector := fs.String("field-selector", "", "Kubernetes field selector")
	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig file for Kubernetes provider")
	kubeContext := fs.String("context", "", "Kubernetes context name")
	includeRels := fs.Bool("include-relationships", true, "Include and persist basic relationships")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return fmt.Errorf("parse scan flags: %w", err)
	}

	return scanapp.Run(context.Background(), scanapp.Request{
		Provider:                *providerName,
		Regions:                 *regionsStr,
		Services:                *servicesStr,
		OutputFormat:            *outputFormat,
		SaveToFile:              *saveToFile,
		ShowEmpty:               *showEmpty,
		ConfigPath:              *configPath,
		MaxConcurrency:          *concurrency,
		DatabasePath:            *databasePath,
		QuackToken:              *quackToken,
		DBProviderTableOverride: *dbProviderTable,
		Namespace:               *namespace,
		LabelSelector:           *labelSelector,
		FieldSelector:           *fieldSelector,
		KubeconfigPath:          *kubeconfig,
		KubeContext:             *kubeContext,
		IncludeRelationships:    *includeRels,
	}, scanapp.Dependencies{})
}

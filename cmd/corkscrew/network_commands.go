package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jlgore/corkscrew/pkg/diagrams/pkg/renderer"
	"github.com/jlgore/corkscrew/internal/db"
	"github.com/jlgore/corkscrew/pkg/crosscloud"
	"github.com/jlgore/corkscrew/pkg/models"
)

// NetworkAnalysisOptions contains options for network analysis commands
type NetworkAnalysisOptions struct {
	Providers             []string `json:"providers"`
	Regions               []string `json:"regions"`
	CorrelationTypes      []string `json:"correlation_types"`
	MinConfidence         float64  `json:"min_confidence"`
	OutputFormat          string   `json:"output_format"`          // json, table, ascii, mermaid
	VisualizationFormat   string   `json:"visualization_format"`   // ascii, mermaid, dot
	ShowDetails           bool     `json:"show_details"`
	IncludeMetrics        bool     `json:"include_metrics"`
	MaxResults            int      `json:"max_results"`
	SortBy                string   `json:"sort_by"`                // confidence, provider, type
	GroupBy               string   `json:"group_by"`               // provider, type, region
}

// runNetworkAnalysis performs cross-cloud network analysis
func runNetworkAnalysis(dbPath string, options NetworkAnalysisOptions) error {
	// Initialize database
	dbConfig, err := db.InitializeUnifiedDatabase(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer dbConfig.Close()

	// Initialize cross-cloud orchestrator
	orchestrator := crosscloud.NewCrossCloudOrchestrator(dbConfig)
	
	// Register correlators
	registerNetworkCorrelators(orchestrator)
	
	// Get resources for analysis
	resources, err := getResourcesForNetworkAnalysis(dbConfig, options)
	if err != nil {
		return fmt.Errorf("failed to get resources: %w", err)
	}
	
	if len(resources) == 0 {
		fmt.Println("No resources found for network analysis")
		return nil
	}
	
	// Run correlation analysis
	ctx := context.Background()
	correlationResult, err := orchestrator.CorrelateResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to correlate resources: %w", err)
	}
	
	// Filter correlations based on options
	filteredCorrelations := filterNetworkCorrelations(correlationResult.Correlations, options)
	
	// Display results
	return displayNetworkAnalysisResults(resources, filteredCorrelations, options)
}

// runNetworkTopology generates and displays network topology
func runNetworkTopology(dbPath string, options NetworkAnalysisOptions) error {
	// Initialize database
	dbConfig, err := db.InitializeUnifiedDatabase(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer dbConfig.Close()

	// Get resources and correlations
	resources, err := getResourcesForNetworkAnalysis(dbConfig, options)
	if err != nil {
		return fmt.Errorf("failed to get resources: %w", err)
	}
	
	correlations, err := getExistingCorrelations(dbConfig, options)
	if err != nil {
		return fmt.Errorf("failed to get correlations: %w", err)
	}
	
	// Create topology visualizer
	var diagramRenderer renderer.DiagramRenderer
	switch options.VisualizationFormat {
	case "ascii":
		diagramRenderer = renderer.NewASCIIRenderer()
	case "mermaid":
		// Would use mermaid renderer here
		diagramRenderer = renderer.NewASCIIRenderer() // Fallback to ASCII
	default:
		diagramRenderer = renderer.NewASCIIRenderer()
	}
	
	visualizer := crosscloud.NewCrossCloudTopologyVisualizer(diagramRenderer)
	
	// Generate visualization
	ctx := context.Background()
	output, err := visualizer.VisualizeNetworkTopology(ctx, resources, correlations)
	if err != nil {
		return fmt.Errorf("failed to generate topology visualization: %w", err)
	}
	
	fmt.Println(output)
	
	// Also create a summary report
	if options.ShowDetails {
		fmt.Println("\n" + strings.Repeat("=", 60))
		summary := visualizer.CreateNetworkSummaryReport(resources, correlations)
		fmt.Println(summary)
	}
	
	return nil
}

// runCorrelationAnalysis performs specific correlation analysis
func runCorrelationAnalysis(dbPath string, correlationType string, options NetworkAnalysisOptions) error {
	// Initialize database
	dbConfig, err := db.InitializeUnifiedDatabase(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer dbConfig.Close()

	// Initialize specific correlator based on type
	var correlator crosscloud.Correlator
	
	switch correlationType {
	case "vpn":
		correlator = crosscloud.NewVPNConnectionCorrelator(options.MinConfidence)
	case "peering":
		correlator = crosscloud.NewNetworkPeeringCorrelator(options.MinConfidence)
	case "direct":
		correlator = crosscloud.NewDirectConnectionCorrelator(options.MinConfidence)
	case "dns":
		correlator = crosscloud.NewEnhancedDNSCorrelator(options.MinConfidence)
	case "loadbalancer", "lb":
		correlator = crosscloud.NewLoadBalancerCrossCloudCorrelator(options.MinConfidence)
	case "security":
		correlator = crosscloud.NewSecurityGroupCorrelator(options.MinConfidence)
	default:
		return fmt.Errorf("unsupported correlation type: %s", correlationType)
	}
	
	// Get resources
	resources, err := getResourcesForNetworkAnalysis(dbConfig, options)
	if err != nil {
		return fmt.Errorf("failed to get resources: %w", err)
	}
	
	// Run specific correlation analysis
	ctx := context.Background()
	correlations, err := correlator.FindCorrelations(ctx, resources)
	if err != nil {
		return fmt.Errorf("failed to find correlations: %w", err)
	}
	
	// Display results
	return displayCorrelationResults(correlationType, correlations, options)
}

// runNetworkScan performs a fresh network scan and analysis
func runNetworkScan(dbPath string, options NetworkAnalysisOptions) error {
	fmt.Println("Starting cross-cloud network scan...")
	
	// Initialize database
	dbConfig, err := db.InitializeUnifiedDatabase(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer dbConfig.Close()

	// Initialize cross-cloud orchestrator
	orchestrator := crosscloud.NewCrossCloudOrchestrator(dbConfig)
	
	// Register correlators
	registerNetworkCorrelators(orchestrator)
	
	// Configure scan
	scanConfig := crosscloud.ScanConfig{
		Providers:        options.Providers,
		Regions:          options.Regions,
		Services:         []string{"vpc", "subnet", "securitygroup", "loadbalancer", "vpn", "dns"},
		IncludeRelations: true,
		ParallelScans:    3,
		Timeout:          10 * time.Minute,
		Filters: map[string]string{
			"network_only": "true",
		},
	}
	
	// Perform scan
	ctx := context.Background()
	scanResult, err := orchestrator.ScanAllProviders(ctx, scanConfig)
	if err != nil {
		return fmt.Errorf("failed to scan providers: %w", err)
	}
	
	fmt.Printf("Scan completed. Found %d resources across %d providers\n", 
		scanResult.TotalResources, len(options.Providers))
	
	// Extract network information
	fmt.Println("Extracting network information...")
	if err := orchestrator.ExtractNetworkInformation(ctx); err != nil {
		return fmt.Errorf("failed to extract network information: %w", err)
	}
	
	// Run correlation analysis
	fmt.Println("Running correlation analysis...")
	correlationResult, err := orchestrator.CorrelateResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to correlate resources: %w", err)
	}
	
	fmt.Printf("Found %d correlations with average confidence %.1f%%\n",
		correlationResult.TotalCorrelations,
		calculateAverageConfidence(correlationResult.Correlations)*100)
	
	// Display summary
	if options.OutputFormat != "silent" {
		return displayScanSummary(scanResult, correlationResult, options)
	}
	
	return nil
}

// Helper functions

func registerNetworkCorrelators(orchestrator *crosscloud.CrossCloudOrchestrator) {
	// Register all network correlators
	orchestrator.RegisterCorrelator(crosscloud.NewVPNConnectionCorrelator(0.7))
	orchestrator.RegisterCorrelator(crosscloud.NewNetworkPeeringCorrelator(0.7))
	orchestrator.RegisterCorrelator(crosscloud.NewDirectConnectionCorrelator(0.7))
	orchestrator.RegisterCorrelator(crosscloud.NewEnhancedDNSCorrelator(0.6))
	orchestrator.RegisterCorrelator(crosscloud.NewLoadBalancerCrossCloudCorrelator(0.6))
	orchestrator.RegisterCorrelator(crosscloud.NewSecurityGroupCorrelator(0.5))
	
	// Also register the basic correlators
	orchestrator.RegisterCorrelator(crosscloud.NewIPAddressCorrelator(0.8))
	orchestrator.RegisterCorrelator(crosscloud.NewDNSCorrelator(0.7))
}

func getResourcesForNetworkAnalysis(dbConfig *db.UnifiedDatabaseConfig, options NetworkAnalysisOptions) ([]*models.Resource, error) {
	// Build query to get network-related resources
	query := `
		SELECT id, name, type, provider, region, attributes, raw_data, scanned_at
		FROM all_cloud_resources 
		WHERE 1=1`
	
	args := []interface{}{}
	
	// Filter by providers
	if len(options.Providers) > 0 {
		placeholders := make([]string, len(options.Providers))
		for i, provider := range options.Providers {
			placeholders[i] = "?"
			args = append(args, provider)
		}
		query += fmt.Sprintf(" AND provider IN (%s)", strings.Join(placeholders, ","))
	}
	
	// Filter by regions
	if len(options.Regions) > 0 {
		placeholders := make([]string, len(options.Regions))
		for i, region := range options.Regions {
			placeholders[i] = "?"
			args = append(args, region)
		}
		query += fmt.Sprintf(" AND location IN (%s)", strings.Join(placeholders, ","))
	}
	
	// Filter to network-related resources
	networkTypes := []string{
		"vpc", "vnet", "network", "subnet", "securitygroup", "firewall",
		"loadbalancer", "gateway", "vpn", "peering", "route", "dns",
		"trafficmanager", "frontdoor", "interconnect", "directconnect",
	}
	
	typeConditions := make([]string, len(networkTypes))
	for i, netType := range networkTypes {
		typeConditions[i] = "type LIKE ?"
		args = append(args, "%"+netType+"%")
	}
	query += fmt.Sprintf(" AND (%s)", strings.Join(typeConditions, " OR "))
	
	// Limit results
	if options.MaxResults > 0 {
		query += fmt.Sprintf(" LIMIT %d", options.MaxResults)
	} else {
		query += " LIMIT 1000" // Default limit
	}
	
	rows, err := dbConfig.GetDB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var resources []*models.Resource
	for rows.Next() {
		var resource models.Resource
		var attributesJSON, rawDataJSON string
		
		err := rows.Scan(
			&resource.ID,
			&resource.Name,
			&resource.Type,
			&resource.Provider,
			&resource.Region,
			&attributesJSON,
			&rawDataJSON,
			&resource.ScannedAt,
		)
		if err != nil {
			continue
		}
		
		// Parse JSON attributes
		if attributesJSON != "" {
			json.Unmarshal([]byte(attributesJSON), &resource.Attributes)
		}
		if rawDataJSON != "" {
			json.Unmarshal([]byte(rawDataJSON), &resource.RawData)
		}
		
		resources = append(resources, &resource)
	}
	
	return resources, nil
}

func getExistingCorrelations(dbConfig *db.UnifiedDatabaseConfig, options NetworkAnalysisOptions) ([]*crosscloud.CrossCloudCorrelation, error) {
	query := `
		SELECT id, source_resource_id, target_resource_id, source_provider, target_provider,
		       correlation_type, correlation_method, confidence_score, evidence, description,
		       status, discovered_at
		FROM cross_cloud_correlations 
		WHERE 1=1`
	
	args := []interface{}{}
	
	// Filter by correlation types
	if len(options.CorrelationTypes) > 0 {
		placeholders := make([]string, len(options.CorrelationTypes))
		for i, corrType := range options.CorrelationTypes {
			placeholders[i] = "?"
			args = append(args, corrType)
		}
		query += fmt.Sprintf(" AND correlation_type IN (%s)", strings.Join(placeholders, ","))
	}
	
	// Filter by confidence
	if options.MinConfidence > 0 {
		query += " AND confidence_score >= ?"
		args = append(args, options.MinConfidence)
	}
	
	// Limit results
	if options.MaxResults > 0 {
		query += fmt.Sprintf(" LIMIT %d", options.MaxResults)
	}
	
	rows, err := dbConfig.GetDB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var correlations []*crosscloud.CrossCloudCorrelation
	for rows.Next() {
		var correlation crosscloud.CrossCloudCorrelation
		var evidenceJSON string
		
		err := rows.Scan(
			&correlation.ID,
			&correlation.SourceResourceID,
			&correlation.TargetResourceID,
			&correlation.SourceProvider,
			&correlation.TargetProvider,
			&correlation.CorrelationType,
			&correlation.CorrelationMethod,
			&correlation.ConfidenceScore,
			&evidenceJSON,
			&correlation.Description,
			&correlation.Status,
			&correlation.DiscoveredAt,
		)
		if err != nil {
			continue
		}
		
		// Parse evidence JSON
		if evidenceJSON != "" {
			json.Unmarshal([]byte(evidenceJSON), &correlation.Evidence)
		}
		
		correlations = append(correlations, &correlation)
	}
	
	return correlations, nil
}

func filterNetworkCorrelations(correlations []*crosscloud.CrossCloudCorrelation, options NetworkAnalysisOptions) []*crosscloud.CrossCloudCorrelation {
	var filtered []*crosscloud.CrossCloudCorrelation
	
	for _, corr := range correlations {
		// Filter by confidence
		if corr.ConfidenceScore < options.MinConfidence {
			continue
		}
		
		// Filter by correlation types
		if len(options.CorrelationTypes) > 0 {
			found := false
			for _, corrType := range options.CorrelationTypes {
				if corr.CorrelationType == corrType {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		
		// Filter by providers
		if len(options.Providers) > 0 {
			sourceMatch := false
			targetMatch := false
			for _, provider := range options.Providers {
				if corr.SourceProvider == provider {
					sourceMatch = true
				}
				if corr.TargetProvider == provider {
					targetMatch = true
				}
			}
			if !sourceMatch || !targetMatch {
				continue
			}
		}
		
		filtered = append(filtered, corr)
	}
	
	return filtered
}

func displayNetworkAnalysisResults(resources []*models.Resource, correlations []*crosscloud.CrossCloudCorrelation, options NetworkAnalysisOptions) error {
	switch options.OutputFormat {
	case "json":
		return displayNetworkResultsJSON(resources, correlations)
	case "table":
		return displayNetworkResultsTable(correlations, options)
	default:
		return displayNetworkResultsTable(correlations, options)
	}
}

func displayNetworkResultsJSON(resources []*models.Resource, correlations []*crosscloud.CrossCloudCorrelation) error {
	result := map[string]interface{}{
		"resources":    resources,
		"correlations": correlations,
		"summary": map[string]interface{}{
			"resource_count":    len(resources),
			"correlation_count": len(correlations),
			"avg_confidence":    calculateAverageConfidence(correlations),
		},
	}
	
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func displayNetworkResultsTable(correlations []*crosscloud.CrossCloudCorrelation, options NetworkAnalysisOptions) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	
	fmt.Fprintln(w, "TYPE\tSOURCE\tTARGET\tCONFIDENCE\tMETHOD\tSTATUS")
	fmt.Fprintln(w, "----\t------\t------\t----------\t------\t------")
	
	for _, corr := range correlations {
		confidence := fmt.Sprintf("%.1f%%", corr.ConfidenceScore*100)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			corr.CorrelationType,
			corr.SourceProvider,
			corr.TargetProvider,
			confidence,
			corr.CorrelationMethod,
			corr.Status,
		)
	}
	
	w.Flush()
	
	// Display summary
	fmt.Printf("\nSummary: %d correlations found\n", len(correlations))
	if len(correlations) > 0 {
		avgConfidence := calculateAverageConfidence(correlations)
		fmt.Printf("Average confidence: %.1f%%\n", avgConfidence*100)
		
		// Group by type
		typeCount := make(map[string]int)
		for _, corr := range correlations {
			typeCount[corr.CorrelationType]++
		}
		
		fmt.Println("\nCorrelation types:")
		for corrType, count := range typeCount {
			fmt.Printf("  %s: %d\n", corrType, count)
		}
	}
	
	return nil
}

func displayCorrelationResults(correlationType string, correlations []*crosscloud.CrossCloudCorrelation, options NetworkAnalysisOptions) error {
	fmt.Printf("=== %s Correlation Analysis ===\n\n", strings.ToUpper(correlationType))
	
	if len(correlations) == 0 {
		fmt.Println("No correlations found")
		return nil
	}
	
	switch options.OutputFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(correlations)
	default:
		return displayCorrelationTable(correlations, options)
	}
}

func displayCorrelationTable(correlations []*crosscloud.CrossCloudCorrelation, options NetworkAnalysisOptions) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	
	if options.ShowDetails {
		fmt.Fprintln(w, "SOURCE PROVIDER\tTARGET PROVIDER\tCONFIDENCE\tMETHOD\tEVIDENCE\tDESCRIPTION")
		fmt.Fprintln(w, "--------------\t---------------\t----------\t------\t--------\t-----------")
		
		for _, corr := range correlations {
			evidence := "N/A"
			if corr.Evidence != nil && len(corr.Evidence) > 0 {
				// Show key evidence
				for key, value := range corr.Evidence {
					evidence = fmt.Sprintf("%s:%v", key, value)
					break // Just show first piece of evidence
				}
			}
			
			confidence := fmt.Sprintf("%.1f%%", corr.ConfidenceScore*100)
			description := corr.Description
			if len(description) > 50 {
				description = description[:47] + "..."
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				corr.SourceProvider,
				corr.TargetProvider,
				confidence,
				corr.CorrelationMethod,
				evidence,
				description,
			)
		}
	} else {
		fmt.Fprintln(w, "SOURCE\tTARGET\tCONFIDENCE\tDESCRIPTION")
		fmt.Fprintln(w, "------\t------\t----------\t-----------")
		
		for _, corr := range correlations {
			confidence := fmt.Sprintf("%.1f%%", corr.ConfidenceScore*100)
			description := corr.Description
			if len(description) > 60 {
				description = description[:57] + "..."
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				corr.SourceProvider,
				corr.TargetProvider,
				confidence,
				description,
			)
		}
	}
	
	w.Flush()
	return nil
}

func displayScanSummary(scanResult *crosscloud.CrossCloudScanResult, correlationResult *crosscloud.CorrelationResult, options NetworkAnalysisOptions) error {
	fmt.Println("\n=== Cross-Cloud Network Scan Summary ===")
	fmt.Printf("Scan Duration: %v\n", scanResult.Duration)
	fmt.Printf("Total Resources: %d\n", scanResult.TotalResources)
	fmt.Printf("Total Correlations: %d\n", correlationResult.TotalCorrelations)
	
	if len(correlationResult.Correlations) > 0 {
		avgConfidence := calculateAverageConfidence(correlationResult.Correlations)
		fmt.Printf("Average Confidence: %.1f%%\n", avgConfidence*100)
	}
	
	// Provider breakdown
	fmt.Println("\nProvider Results:")
	for provider, result := range scanResult.Results {
		fmt.Printf("  %s: %d resources (scan took %v)\n", 
			strings.ToUpper(provider), result.ResourceCount, result.ScanDuration)
	}
	
	// Error summary
	if len(scanResult.Errors) > 0 {
		fmt.Printf("\nErrors encountered: %d\n", len(scanResult.Errors))
		for _, err := range scanResult.Errors {
			fmt.Printf("  - %v\n", err)
		}
	}
	
	return nil
}

func calculateAverageConfidence(correlations []*crosscloud.CrossCloudCorrelation) float64 {
	if len(correlations) == 0 {
		return 0
	}
	
	total := 0.0
	for _, corr := range correlations {
		total += corr.ConfidenceScore
	}
	
	return total / float64(len(correlations))
}
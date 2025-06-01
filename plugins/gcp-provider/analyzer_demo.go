package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// DemoGCPClientLibraryAnalyzer demonstrates the GCP client library analyzer
func DemoGCPClientLibraryAnalyzer() {
	log.Printf("GCP Client Library Analyzer Demo")
	log.Printf("================================")

	// Determine GOPATH
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = filepath.Join(os.Getenv("HOME"), "go")
	}

	log.Printf("Using GOPATH: %s", goPath)

	// Create analyzer
	analyzer := NewClientLibraryAnalyzer(goPath, "cloud.google.com/go")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Analyze GCP libraries
	log.Printf("Starting GCP client library analysis...")
	services, err := analyzer.AnalyzeGCPLibraries(ctx)
	if err != nil {
		log.Printf("Analysis failed: %v", err)
		return
	}

	log.Printf("Analysis completed. Found %d services", len(services))

	// Generate full analysis report
	report := analyzer.GenerateAnalysisReport()
	report["services_info"] = services

	// Create hierarchy analyzer
	hierarchyAnalyzer := NewResourceHierarchyAnalyzer(analyzer.ServiceMappings)
	if err := hierarchyAnalyzer.AnalyzeResourceHierarchies(ctx); err != nil {
		log.Printf("Hierarchy analysis failed: %v", err)
	} else {
		relationships := hierarchyAnalyzer.GenerateHierarchyRelationships()
		report["relationships"] = relationships
		report["total_relationships"] = len(relationships)
		log.Printf("Generated %d relationships", len(relationships))
	}

	// Print summary
	printAnalysisSummary(services, analyzer)

	// Optionally save detailed report to file
	for _, arg := range os.Args {
		if arg == "--save-report" {
			saveDetailedReport(report)
			break
		}
	}
}

// printAnalysisSummary prints a human-readable summary
func printAnalysisSummary(services []*pb.ServiceInfo, analyzer *ClientLibraryAnalyzer) {
	fmt.Printf("\n=== GCP Client Library Analysis Summary ===\n")
	fmt.Printf("Analysis completed at: %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Total services discovered: %d\n", len(services))

	// Count patterns
	totalPatterns := 0
	totalIterators := 0
	for _, mapping := range analyzer.ServiceMappings {
		totalPatterns += len(mapping.ResourcePatterns)
		totalIterators += len(mapping.IteratorPatterns)
	}

	fmt.Printf("Total resource patterns: %d\n", totalPatterns)
	fmt.Printf("Total iterator patterns: %d\n", totalIterators)
	fmt.Printf("Total asset inventory mappings: %d\n", len(analyzer.AssetInventoryMappings))

	fmt.Printf("\n=== Services Discovered ===\n")
	for i, service := range services {
		if i >= 10 {
			fmt.Printf("... and %d more services\n", len(services)-10)
			break
		}
		
		resourceCount := len(service.ResourceTypes)
		fmt.Printf("- %s (%s): %d resource types\n", 
			service.Name, service.DisplayName, resourceCount)

		// Show first few resource types
		if resourceCount > 0 {
			shown := 0
			for _, rt := range service.ResourceTypes {
				if shown < 3 {
					fmt.Printf("  * %s -> %s\n", rt.Name, rt.TypeName)
					shown++
				} else if shown == 3 && resourceCount > 3 {
					fmt.Printf("  ... and %d more\n", resourceCount-3)
					break
				}
			}
		}
	}

	fmt.Printf("\n=== Sample Asset Inventory Mappings ===\n")
	count := 0
	for key, value := range analyzer.AssetInventoryMappings {
		if count >= 10 {
			fmt.Printf("... and %d more mappings\n", len(analyzer.AssetInventoryMappings)-10)
			break
		}
		fmt.Printf("- %s -> %s\n", key, value)
		count++
	}

	fmt.Printf("\n=== Sample Resource Patterns ===\n")
	count = 0
	for serviceName, mapping := range analyzer.ServiceMappings {
		if count >= 5 {
			break
		}
		
		if len(mapping.ResourcePatterns) > 0 {
			fmt.Printf("\nService: %s\n", serviceName)
			patternCount := 0
			for _, pattern := range mapping.ResourcePatterns {
				if patternCount >= 3 {
					fmt.Printf("  ... and %d more patterns\n", len(mapping.ResourcePatterns)-3)
					break
				}
				
				methodType := "Unknown"
				if pattern.ListMethod {
					methodType = "List"
				} else if pattern.GetMethod {
					methodType = "Get"
				}
				
				fmt.Printf("  - %s (%s): %s -> %s\n", 
					pattern.MethodName, methodType, pattern.ResourceType, pattern.AssetType)
				patternCount++
			}
			count++
		}
	}

	fmt.Printf("\nUse --save-report to save detailed JSON report to file\n")
}

// saveDetailedReport saves the full analysis report to a JSON file
func saveDetailedReport(report map[string]interface{}) {
	filename := fmt.Sprintf("gcp-analysis-report-%s.json", 
		time.Now().Format("2006-01-02-150405"))
	
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal report: %v", err)
		return
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Printf("Failed to write report file: %v", err)
		return
	}

	log.Printf("Detailed analysis report saved to: %s", filename)
}
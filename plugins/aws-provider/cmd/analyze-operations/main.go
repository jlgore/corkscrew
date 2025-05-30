package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
)

func main() {
	var (
		serviceName = flag.String("service", "ec2", "AWS service name to analyze")
		githubToken = flag.String("token", os.Getenv("GITHUB_TOKEN"), "GitHub token for API access")
		outputJSON  = flag.Bool("json", false, "Output results as JSON")
		filter      = flag.String("filter", "", "Filter operations by type (List, Describe, Get, etc.)")
	)
	flag.Parse()

	if *serviceName == "" {
		log.Fatal("Service name is required")
	}

	// Create service discovery instance
	sd := discovery.NewAWSServiceDiscovery(*githubToken)
	ctx := context.Background()

	fmt.Printf("Analyzing operations for AWS %s service...\n\n", *serviceName)

	// Perform deep analysis
	analysis, err := sd.AnalyzeServiceOperations(ctx, *serviceName)
	if err != nil {
		log.Fatalf("Failed to analyze service operations: %v", err)
	}

	if *outputJSON {
		// Output as JSON
		data, err := json.MarshalIndent(analysis, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal analysis to JSON: %v", err)
		}
		fmt.Println(string(data))
		return
	}

	// Display human-readable output
	displayAnalysis(analysis, *filter)
}

func displayAnalysis(analysis *discovery.ServiceAnalysis, filter string) {
	fmt.Printf("Service: %s\n", analysis.ServiceName)
	fmt.Printf("Last Analyzed: %s\n\n", analysis.LastAnalyzed.Format("2006-01-02 15:04:05"))

	// Operation Summary
	operationsByType := make(map[string][]discovery.OperationAnalysis)
	paginatedCount := 0
	
	for _, op := range analysis.Operations {
		if filter != "" && !strings.Contains(strings.ToLower(op.Type), strings.ToLower(filter)) {
			continue
		}
		operationsByType[op.Type] = append(operationsByType[op.Type], op)
		if op.IsPaginated {
			paginatedCount++
		}
	}

	fmt.Printf("=== OPERATION SUMMARY ===\n")
	fmt.Printf("Total Operations: %d\n", len(analysis.Operations))
	fmt.Printf("Paginated Operations: %d\n", paginatedCount)
	fmt.Printf("\nOperations by Type:\n")
	
	// Sort operation types
	var types []string
	for opType := range operationsByType {
		types = append(types, opType)
	}
	sort.Strings(types)

	for _, opType := range types {
		ops := operationsByType[opType]
		fmt.Printf("  %s: %d operations\n", opType, len(ops))
	}

	// Detailed Operations
	fmt.Printf("\n=== OPERATIONS ===\n")
	for _, opType := range types {
		ops := operationsByType[opType]
		fmt.Printf("\n%s Operations (%d):\n", opType, len(ops))
		
		// Sort operations by name
		sort.Slice(ops, func(i, j int) bool {
			return ops[i].Name < ops[j].Name
		})

		for _, op := range ops {
			fmt.Printf("  • %s", op.Name)
			if op.ResourceType != "" {
				fmt.Printf(" [Resource: %s]", op.ResourceType)
			}
			if op.IsPaginated {
				fmt.Printf(" (Paginated: %s)", op.PaginationField)
			}
			fmt.Println()
			
			if op.InputType != "" || op.OutputType != "" {
				fmt.Printf("    Input: %s → Output: %s\n", op.InputType, op.OutputType)
			}
			
			if len(op.RequiredParams) > 0 {
				fmt.Printf("    Required: %s\n", strings.Join(op.RequiredParams, ", "))
			}
		}
	}

	// Resource Types
	if len(analysis.ResourceTypes) > 0 {
		fmt.Printf("\n=== RESOURCE TYPES (%d) ===\n", len(analysis.ResourceTypes))
		for _, res := range analysis.ResourceTypes {
			fmt.Printf("\n%s:\n", res.Name)
			if res.IdentifierField != "" {
				fmt.Printf("  Identifier: %s\n", res.IdentifierField)
			}
			if res.NameField != "" {
				fmt.Printf("  Name Field: %s\n", res.NameField)
			}
			if res.ARNField != "" {
				fmt.Printf("  ARN Field: %s\n", res.ARNField)
			}
			if res.TagsField != "" {
				fmt.Printf("  Tags Field: %s\n", res.TagsField)
			}
			if len(res.Operations) > 0 {
				fmt.Printf("  Operations: %s\n", strings.Join(res.Operations, ", "))
			}
		}
	}

	// Pagination Patterns
	if len(analysis.PaginationInfo) > 0 {
		fmt.Printf("\n=== PAGINATION PATTERNS ===\n")
		for opName, pattern := range analysis.PaginationInfo {
			fmt.Printf("\n%s:\n", opName)
			fmt.Printf("  Token Field: %s\n", pattern.TokenField)
			if pattern.ResultsField != "" {
				fmt.Printf("  Results Field: %s\n", pattern.ResultsField)
			}
			if pattern.LimitField != "" {
				fmt.Printf("  Limit Field: %s\n", pattern.LimitField)
			}
		}
	}

	// Relationships
	if len(analysis.Relationships) > 0 {
		fmt.Printf("\n=== RESOURCE RELATIONSHIPS ===\n")
		for _, rel := range analysis.Relationships {
			fmt.Printf("  %s %s %s", rel.SourceResource, rel.RelationshipType, rel.TargetResource)
			if rel.FieldName != "" {
				fmt.Printf(" (via %s)", rel.FieldName)
			}
			fmt.Println()
		}
	}
}
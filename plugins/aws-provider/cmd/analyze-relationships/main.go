package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
)

func main() {
	var (
		servicesDir = flag.String("services-dir", "", "Directory containing service analysis JSON files")
		outputFile  = flag.String("output", "", "Output file for the relationship graph")
		format      = flag.String("format", "json", "Output format: json, mermaid, deps, or summary")
		service     = flag.String("service", "", "Analyze specific service (e.g., ec2, lambda)")
	)
	flag.Parse()

	if *servicesDir == "" {
		log.Fatal("Please provide -services-dir flag")
	}

	// Load all service analysis files
	services := loadServicesFromDirectory(*servicesDir)
	
	if len(services) == 0 {
		log.Fatal("No service analysis files found")
	}

	log.Printf("Loaded %d services for analysis\n", len(services))

	// Build the relationship graph
	graph := discovery.BuildResourceRelationshipGraph(services)

	// Generate output based on format
	var output string
	
	if *service != "" {
		// Analyze specific service
		output = analyzeService(graph, *service, *format)
	} else {
		// Analyze all services
		switch *format {
		case "json":
			jsonData := graph.ToJSON()
			bytes, _ := json.MarshalIndent(jsonData, "", "  ")
			output = string(bytes)
			
		case "mermaid":
			output = graph.GenerateMermaidDiagram()
			
		case "deps":
			order := graph.GetDependencyOrder()
			output = "=== Resource Scanning Order ===\n\n"
			for i, res := range order {
				output += fmt.Sprintf("%d. %s\n", i+1, res)
			}
			
		case "summary":
			output = generateSummary(graph)
			
		default:
			log.Fatalf("Unknown format: %s", *format)
		}
	}

	// Write output
	if *outputFile != "" {
		if err := ioutil.WriteFile(*outputFile, []byte(output), 0644); err != nil {
			log.Fatalf("Failed to write output: %v", err)
		}
		log.Printf("Output written to %s\n", *outputFile)
	} else {
		fmt.Println(output)
	}
}

func loadServicesFromDirectory(dir string) map[string]*discovery.ServiceAnalysis {
	services := make(map[string]*discovery.ServiceAnalysis)
	
	// Walk the directory
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Look for JSON files
		if strings.HasSuffix(path, "_analysis.json") || strings.HasSuffix(path, "_service.json") {
			data, err := ioutil.ReadFile(path)
			if err != nil {
				log.Printf("Warning: Failed to read %s: %v", path, err)
				return nil
			}
			
			var serviceAnalysis discovery.ServiceAnalysis
			if err := json.Unmarshal(data, &serviceAnalysis); err != nil {
				log.Printf("Warning: Failed to parse %s: %v", path, err)
				return nil
			}
			
			if serviceAnalysis.ServiceName != "" {
				services[serviceAnalysis.ServiceName] = &serviceAnalysis
				log.Printf("Loaded service: %s from %s", serviceAnalysis.ServiceName, path)
			}
		}
		
		return nil
	})
	
	if err != nil {
		log.Printf("Warning: Error walking directory: %v", err)
	}
	
	return services
}

func analyzeService(graph *discovery.ResourceGraph, serviceName, format string) string {
	var output strings.Builder
	
	// Count relationships
	inbound := 0
	outbound := 0
	internal := 0
	
	for _, rel := range graph.Relationships {
		if rel.SourceService == serviceName {
			if rel.TargetService == serviceName {
				internal++
			} else {
				outbound++
			}
		} else if rel.TargetService == serviceName {
			inbound++
		}
	}
	
	switch format {
	case "json":
		result := map[string]interface{}{
			"service": serviceName,
			"resources": graph.ServiceMap[serviceName],
			"relationships": map[string]int{
				"inbound":  inbound,
				"outbound": outbound,
				"internal": internal,
				"total":    inbound + outbound + internal,
			},
			"details": getServiceRelationships(graph, serviceName),
		}
		bytes, _ := json.MarshalIndent(result, "", "  ")
		return string(bytes)
		
	default:
		output.WriteString(fmt.Sprintf("=== Service Analysis: %s ===\n\n", serviceName))
		output.WriteString(fmt.Sprintf("Resources: %v\n\n", graph.ServiceMap[serviceName]))
		output.WriteString(fmt.Sprintf("Relationships:\n"))
		output.WriteString(fmt.Sprintf("  - Inbound: %d (other services referencing this)\n", inbound))
		output.WriteString(fmt.Sprintf("  - Outbound: %d (this service referencing others)\n", outbound))
		output.WriteString(fmt.Sprintf("  - Internal: %d (within this service)\n", internal))
		output.WriteString(fmt.Sprintf("  - Total: %d\n\n", inbound+outbound+internal))
		
		// Show details
		output.WriteString("Relationship Details:\n")
		for _, rel := range graph.Relationships {
			if rel.SourceService == serviceName || rel.TargetService == serviceName {
				output.WriteString(fmt.Sprintf("  %s::%s -[%s]-> %s::%s (via %s, %s)\n",
					rel.SourceService, rel.SourceResource,
					rel.RelationType,
					rel.TargetService, rel.TargetResource,
					rel.SourceField, rel.Cardinality))
			}
		}
	}
	
	return output.String()
}

func getServiceRelationships(graph *discovery.ResourceGraph, serviceName string) []discovery.GraphRelationship {
	var rels []discovery.GraphRelationship
	for _, rel := range graph.Relationships {
		if rel.SourceService == serviceName || rel.TargetService == serviceName {
			rels = append(rels, rel)
		}
	}
	return rels
}

func generateSummary(graph *discovery.ResourceGraph) string {
	var output strings.Builder
	
	output.WriteString("=== AWS Resource Relationship Graph Summary ===\n\n")
	
	// Basic stats
	jsonData := graph.ToJSON()
	stats := jsonData["stats"].(map[string]interface{})
	
	output.WriteString(fmt.Sprintf("Total Services: %v\n", stats["totalServices"]))
	output.WriteString(fmt.Sprintf("Total Resources: %v\n", stats["totalNodes"]))
	output.WriteString(fmt.Sprintf("Total Relationships: %v\n", stats["totalRelationships"]))
	output.WriteString("\n")
	
	// Services overview
	output.WriteString("Services and Resources:\n")
	for service, resources := range graph.ServiceMap {
		output.WriteString(fmt.Sprintf("  %s: %d resources (%v)\n", service, len(resources), resources))
	}
	output.WriteString("\n")
	
	// Relationship types
	relTypes := make(map[string]int)
	cardTypes := make(map[string]int)
	crossService := 0
	
	for _, rel := range graph.Relationships {
		relTypes[rel.RelationType]++
		cardTypes[rel.Cardinality]++
		if rel.SourceService != rel.TargetService {
			crossService++
		}
	}
	
	output.WriteString("Relationship Types:\n")
	for relType, count := range relTypes {
		output.WriteString(fmt.Sprintf("  %s: %d\n", relType, count))
	}
	output.WriteString("\n")
	
	output.WriteString("Cardinality Distribution:\n")
	for cardType, count := range cardTypes {
		output.WriteString(fmt.Sprintf("  %s: %d\n", cardType, count))
	}
	output.WriteString("\n")
	
	output.WriteString(fmt.Sprintf("Cross-Service Relationships: %d (%.1f%%)\n", 
		crossService, float64(crossService)/float64(len(graph.Relationships))*100))
	
	// Most connected resources
	output.WriteString("\nMost Connected Resources:\n")
	type connectivity struct {
		resource string
		count    int
	}
	
	var connections []connectivity
	for key, node := range graph.Resources {
		total := len(node.InboundRefs) + len(node.OutboundRefs)
		if total > 0 {
			connections = append(connections, connectivity{key, total})
		}
	}
	
	// Sort by connectivity (simple bubble sort for demo)
	for i := 0; i < len(connections); i++ {
		for j := i + 1; j < len(connections); j++ {
			if connections[j].count > connections[i].count {
				connections[i], connections[j] = connections[j], connections[i]
			}
		}
	}
	
	// Show top 10
	for i := 0; i < 10 && i < len(connections); i++ {
		conn := connections[i]
		node := graph.Resources[conn.resource]
		output.WriteString(fmt.Sprintf("  %s: %d connections (in:%d, out:%d)\n",
			conn.resource, conn.count, len(node.InboundRefs), len(node.OutboundRefs)))
	}
	
	return output.String()
}
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
)

func main() {
	var (
		servicesFile   = flag.String("services", "", "Path to services analysis JSON file")
		outputFormat   = flag.String("format", "json", "Output format: json, mermaid, or deps")
		outputFile     = flag.String("output", "", "Output file (stdout if not specified)")
		analyzeService = flag.String("analyze", "", "Analyze specific service relationships")
	)
	flag.Parse()

	// Load service analysis data
	services := make(map[string]*discovery.ServiceAnalysis)
	
	if *servicesFile != "" {
		// Load from file
		data, err := ioutil.ReadFile(*servicesFile)
		if err != nil {
			log.Fatalf("Failed to read services file: %v", err)
		}
		
		if err := json.Unmarshal(data, &services); err != nil {
			log.Fatalf("Failed to parse services JSON: %v", err)
		}
	} else {
		// Load from default location or use example data
		services = loadExampleServices()
	}

	// Build the resource graph
	graph := discovery.BuildResourceRelationshipGraph(services)

	// Generate output based on format
	var output string
	switch *outputFormat {
	case "mermaid":
		output = graph.GenerateMermaidDiagram()
	case "deps":
		order := graph.GetDependencyOrder()
		output = "Resource Scanning Order:\n"
		for i, resource := range order {
			output += fmt.Sprintf("%d. %s\n", i+1, resource)
		}
	case "json":
		jsonData := graph.ToJSON()
		bytes, _ := json.MarshalIndent(jsonData, "", "  ")
		output = string(bytes)
	default:
		log.Fatalf("Unknown format: %s", *outputFormat)
	}

	// If analyzing specific service, filter results
	if *analyzeService != "" {
		output = analyzeServiceRelationships(graph, *analyzeService, *outputFormat)
	}

	// Write output
	if *outputFile != "" {
		if err := ioutil.WriteFile(*outputFile, []byte(output), 0644); err != nil {
			log.Fatalf("Failed to write output file: %v", err)
		}
		fmt.Printf("Output written to %s\n", *outputFile)
	} else {
		fmt.Println(output)
	}
}

// analyzeServiceRelationships analyzes relationships for a specific service
func analyzeServiceRelationships(graph *discovery.ResourceGraph, serviceName, format string) string {
	var output string
	
	if format == "json" {
		// Filter relationships for the service
		serviceRels := []discovery.GraphRelationship{}
		for _, rel := range graph.Relationships {
			if rel.SourceService == serviceName || rel.TargetService == serviceName {
				serviceRels = append(serviceRels, rel)
			}
		}
		
		result := map[string]interface{}{
			"service":       serviceName,
			"relationships": serviceRels,
			"stats": map[string]int{
				"totalRelationships": len(serviceRels),
				"outbound":          countOutbound(serviceRels, serviceName),
				"inbound":           countInbound(serviceRels, serviceName),
			},
		}
		
		bytes, _ := json.MarshalIndent(result, "", "  ")
		output = string(bytes)
	} else {
		output = fmt.Sprintf("=== Relationships for %s ===\n\n", serviceName)
		
		// Outbound relationships
		output += "Outbound (this service references):\n"
		for _, rel := range graph.Relationships {
			if rel.SourceService == serviceName {
				output += fmt.Sprintf("  %s::%s -[%s]-> %s::%s (via %s)\n",
					rel.SourceService, rel.SourceResource,
					rel.RelationType,
					rel.TargetService, rel.TargetResource,
					rel.SourceField)
			}
		}
		
		// Inbound relationships
		output += "\nInbound (referenced by other services):\n"
		for _, rel := range graph.Relationships {
			if rel.TargetService == serviceName {
				output += fmt.Sprintf("  %s::%s -[%s]-> %s::%s (via %s)\n",
					rel.SourceService, rel.SourceResource,
					rel.RelationType,
					rel.TargetService, rel.TargetResource,
					rel.SourceField)
			}
		}
	}
	
	return output
}

func countOutbound(rels []discovery.GraphRelationship, service string) int {
	count := 0
	for _, rel := range rels {
		if rel.SourceService == service {
			count++
		}
	}
	return count
}

func countInbound(rels []discovery.GraphRelationship, service string) int {
	count := 0
	for _, rel := range rels {
		if rel.TargetService == service {
			count++
		}
	}
	return count
}

// loadExampleServices loads example service data for testing
func loadExampleServices() map[string]*discovery.ServiceAnalysis {
	return map[string]*discovery.ServiceAnalysis{
		"ec2": {
			ServiceName: "ec2",
			ResourceTypes: []discovery.ResourceTypeAnalysis{
				{
					Name:            "Instance",
					IdentifierField: "InstanceId",
					Fields: []discovery.FieldInfo{
						{Name: "InstanceId", GoType: "string"},
						{Name: "VpcId", GoType: "string"},
						{Name: "SubnetId", GoType: "string"},
						{Name: "SecurityGroupIds", GoType: "[]string", IsList: true},
						{Name: "IamInstanceProfile", GoType: "string"},
					},
				},
				{
					Name:            "Vpc",
					IdentifierField: "VpcId",
					Fields: []discovery.FieldInfo{
						{Name: "VpcId", GoType: "string"},
					},
				},
				{
					Name:            "Subnet",
					IdentifierField: "SubnetId",
					Fields: []discovery.FieldInfo{
						{Name: "SubnetId", GoType: "string"},
						{Name: "VpcId", GoType: "string"},
					},
				},
				{
					Name:            "SecurityGroup",
					IdentifierField: "GroupId",
					Fields: []discovery.FieldInfo{
						{Name: "GroupId", GoType: "string"},
						{Name: "VpcId", GoType: "string"},
					},
				},
			},
		},
		"iam": {
			ServiceName: "iam",
			ResourceTypes: []discovery.ResourceTypeAnalysis{
				{
					Name:            "Role",
					IdentifierField: "RoleName",
					Fields: []discovery.FieldInfo{
						{Name: "RoleName", GoType: "string"},
						{Name: "Arn", GoType: "string"},
					},
				},
			},
		},
		"lambda": {
			ServiceName: "lambda",
			ResourceTypes: []discovery.ResourceTypeAnalysis{
				{
					Name:            "Function",
					IdentifierField: "FunctionName",
					Fields: []discovery.FieldInfo{
						{Name: "FunctionName", GoType: "string"},
						{Name: "Role", GoType: "string"},
						{Name: "VpcConfig.SubnetIds", GoType: "[]string", IsList: true},
						{Name: "VpcConfig.SecurityGroupIds", GoType: "[]string", IsList: true},
					},
				},
			},
		},
	}
}

func init() {
	// Create examples directory if it doesn't exist
	examplesDir := "examples/relationships"
	if err := os.MkdirAll(examplesDir, 0755); err != nil {
		log.Printf("Warning: Could not create examples directory: %v", err)
	}
	
	// Generate example outputs
	services := loadExampleServices()
	graph := discovery.BuildResourceRelationshipGraph(services)
	
	// Save Mermaid diagram
	mermaidFile := filepath.Join(examplesDir, "aws-resources.mmd")
	if err := ioutil.WriteFile(mermaidFile, []byte(graph.GenerateMermaidDiagram()), 0644); err != nil {
		log.Printf("Warning: Could not write Mermaid example: %v", err)
	}
	
	// Save JSON graph
	jsonData := graph.ToJSON()
	jsonBytes, _ := json.MarshalIndent(jsonData, "", "  ")
	jsonFile := filepath.Join(examplesDir, "aws-resources-graph.json")
	if err := ioutil.WriteFile(jsonFile, jsonBytes, 0644); err != nil {
		log.Printf("Warning: Could not write JSON example: %v", err)
	}
}
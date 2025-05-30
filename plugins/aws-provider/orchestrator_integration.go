package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ConfigureDiscovery sets up AWS-specific discovery sources
func (p *AWSProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	log.Printf("🔧 Configuring AWS discovery with %d sources", len(req.Sources))
	
	configuredSources := []string{}
	
	for _, source := range req.Sources {
		switch source.SourceType {
		case "github":
			// AWS-specific GitHub configuration
			log.Printf("✅ Configured GitHub discovery for AWS SDK")
			configuredSources = append(configuredSources, "github:aws-sdk-go-v2")
			
		case "api":
			// AWS can use its own APIs for additional discovery
			log.Printf("✅ Configured AWS API discovery")
			configuredSources = append(configuredSources, "api:aws-service-catalog")
			
		default:
			log.Printf("⚠️  Unknown source type: %s", source.SourceType)
		}
	}
	
	return &pb.ConfigureDiscoveryResponse{
		Success:           true,
		ConfiguredSources: configuredSources,
		Metadata: map[string]string{
			"provider":          "aws",
			"discovery_version": "2.0",
			"sdk_version":       "v1.36.3",
			"configured_at":     time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

// AnalyzeDiscoveredData uses AWS-specific analyzer to process raw discovery data
func (p *AWSProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	log.Printf("🔍 Analyzing discovered data from source: %s", req.SourceType)
	
	// Use the existing AWS analyzer from generator package
	analyzer := generator.NewAWSAnalyzer()
	
	switch req.SourceType {
	case "github":
		return p.analyzeGitHubData(ctx, analyzer, req.RawData, req.Options)
		
	case "api":
		return p.analyzeAPIData(ctx, analyzer, req.RawData, req.Options)
		
	default:
		return &pb.AnalysisResponse{
			Success: false,
			Error:   fmt.Sprintf("unsupported source type: %s", req.SourceType),
		}, nil
	}
}

// analyzeGitHubData processes GitHub discovery data using AWS analyzer
func (p *AWSProvider) analyzeGitHubData(ctx context.Context, analyzer *generator.AWSAnalyzer, rawData []byte, options map[string]string) (*pb.AnalysisResponse, error) {
	// Parse raw GitHub data - this would come from the orchestrator's GitHubSource
	var githubFiles struct {
		Files []struct {
			Path    string `json:"path"`
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"files"`
	}
	
	if err := json.Unmarshal(rawData, &githubFiles); err != nil {
		return &pb.AnalysisResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to parse GitHub data: %v", err),
		}, nil
	}
	
	log.Printf("📊 Analyzing %d GitHub files", len(githubFiles.Files))
	
	// Group files by service
	serviceFiles := make(map[string][]string)
	for _, file := range githubFiles.Files {
		// Extract service name from path like "service/ec2/api_op_*.go"
		parts := strings.Split(file.Path, "/")
		if len(parts) >= 2 && parts[0] == "service" {
			serviceName := parts[1]
			serviceFiles[serviceName] = append(serviceFiles[serviceName], file.Path)
		}
	}
	
	// Analyze each service using existing AWS analyzer
	services := []*pb.ServiceAnalysis{}
	resources := []*pb.ResourceAnalysis{}
	operations := []*pb.OperationAnalysis{}
	
	for serviceName, files := range serviceFiles {
		log.Printf("🔬 Analyzing AWS service: %s (%d files)", serviceName, len(files))
		
		// This would use the existing service analysis logic
		serviceAnalysis := &pb.ServiceAnalysis{
			Name:        serviceName,
			DisplayName: strings.Title(serviceName),
			Description: fmt.Sprintf("AWS %s service", strings.ToUpper(serviceName)),
			PackageName: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
			Metadata: map[string]string{
				"provider":    "aws",
				"service":     serviceName,
				"files_count": fmt.Sprintf("%d", len(files)),
			},
		}
		
		services = append(services, serviceAnalysis)
		
		// Extract resource types and operations from the service
		// This would leverage the existing AWS analysis logic
		for _, operation := range getAWSOperationsForService(serviceName) {
			operations = append(operations, &pb.OperationAnalysis{
				Name:          operation.Name,
				Service:       serviceName,
				ResourceType:  operation.ResourceType,
				OperationType: operation.Type,
				Description:   operation.Description,
				Paginated:     operation.Paginated,
				Metadata: map[string]string{
					"aws_operation": "true",
					"input_type":    operation.InputType,
					"output_type":   operation.OutputType,
				},
			})
		}
		
		// Extract resource types
		for _, resource := range getAWSResourcesForService(serviceName) {
			resources = append(resources, &pb.ResourceAnalysis{
				Name:        resource.Name,
				Service:     serviceName,
				DisplayName: resource.DisplayName,
				Description: resource.Description,
				Identifiers: resource.Identifiers,
				Metadata: map[string]string{
					"aws_resource": "true",
					"arn_field":    resource.ARNField,
					"id_field":     resource.IDField,
				},
			})
		}
	}
	
	log.Printf("✅ Analysis complete: %d services, %d resources, %d operations", 
		len(services), len(resources), len(operations))
	
	return &pb.AnalysisResponse{
		Success:   true,
		Services:  services,
		Resources: resources,
		Operations: operations,
		Metadata: map[string]string{
			"source_type":      "github",
			"analyzed_at":      time.Now().UTC().Format(time.RFC3339),
			"services_count":   fmt.Sprintf("%d", len(services)),
			"resources_count":  fmt.Sprintf("%d", len(resources)),
			"operations_count": fmt.Sprintf("%d", len(operations)),
		},
	}, nil
}

// analyzeAPIData processes AWS API discovery data
func (p *AWSProvider) analyzeAPIData(ctx context.Context, analyzer *generator.AWSAnalyzer, rawData []byte, options map[string]string) (*pb.AnalysisResponse, error) {
	// This would process data from AWS APIs like Service Catalog, etc.
	log.Printf("📡 Analyzing AWS API data")
	
	// For now, return a basic response
	return &pb.AnalysisResponse{
		Success: true,
		Metadata: map[string]string{
			"source_type": "api",
			"analyzed_at": time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

// GenerateFromAnalysis uses AWS-specific generator to create scanner code
func (p *AWSProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	log.Printf("🏗️  Generating AWS scanners from analysis (%d services)", len(req.Analysis.Services))
	
	// Use the existing AWS generator
	gen := generator.NewAWSProviderGenerator("./generated")
	gen.SetDebug(true)
	
	// Convert proto analysis back to generator types
	services := convertFromProtoAnalysis(req.Analysis)
	
	// Filter to target services if specified
	if len(req.TargetServices) > 0 {
		filteredServices := []*generator.AWSServiceInfo{}
		targetSet := make(map[string]bool)
		for _, target := range req.TargetServices {
			targetSet[target] = true
		}
		
		for _, service := range services {
			if targetSet[service.Name] {
				filteredServices = append(filteredServices, service)
			}
		}
		services = filteredServices
		log.Printf("🎯 Filtering to %d target services", len(services))
	}
	
	// Generate scanner code using existing generator
	result, err := gen.GenerateServiceScanners(services)
	if err != nil {
		return &pb.GenerateResponse{
			Success: false,
			Error:   fmt.Sprintf("generation failed: %v", err),
		}, nil
	}
	
	// Convert generation result to proto response
	files := []*pb.GeneratedFile{}
	for serviceName, plugin := range result.GeneratedPlugins {
		// Read the generated file content
		content := ""
		if plugin.FilePath != "" {
			// For now, we'll indicate the file path since content is written to disk
			content = fmt.Sprintf("// Generated AWS scanner plugin for %s\n// File: %s\n// Generated at: %s", 
				serviceName, plugin.FilePath, plugin.GeneratedAt)
		}
		
		files = append(files, &pb.GeneratedFile{
			Path:     plugin.FilePath,
			Content:  content,
			Template: "aws-scanner-plugin",
			Service:  serviceName,
			Metadata: map[string]string{
				"plugin_type":    "scanner",
				"aws_service":    serviceName,
				"binary_name":    plugin.BinaryName,
				"resource_types": strings.Join(plugin.ResourceTypes, ","),
				"operations":     strings.Join(plugin.Operations, ","),
				"capabilities":   strings.Join(plugin.Capabilities, ","),
				"generated_at":   plugin.GeneratedAt,
			},
		})
	}
	
	stats := &pb.GenerationStats{
		TotalFiles:       int32(len(files)),
		TotalServices:    int32(result.SuccessfulServices),
		GenerationTimeMs: 0, // Would be calculated from actual generation time
		FileCountsByType: map[string]int32{
			"scanner": int32(len(files)),
		},
	}
	
	log.Printf("✅ Generation complete: %d files for %d services", len(files), result.SuccessfulServices)
	
	return &pb.GenerateResponse{
		Success: true,
		Files:   files,
		Stats:   stats,
		Warnings: result.Errors, // Include any warnings from generation
	}, nil
}

// Helper functions for AWS-specific analysis
func getAWSOperationsForService(serviceName string) []AWSOperationInfo {
	// This would use the existing AWS operation discovery logic
	// For now, return some common operations based on service patterns
	operations := []AWSOperationInfo{}
	
	switch serviceName {
	case "ec2":
		operations = append(operations, 
			AWSOperationInfo{
				Name: "DescribeInstances", Type: "List", ResourceType: "Instance",
				Description: "Lists EC2 instances", InputType: "DescribeInstancesInput", 
				OutputType: "DescribeInstancesOutput", Paginated: true,
			},
			AWSOperationInfo{
				Name: "DescribeVpcs", Type: "List", ResourceType: "Vpc",
				Description: "Lists VPCs", InputType: "DescribeVpcsInput",
				OutputType: "DescribeVpcsOutput", Paginated: true,
			},
		)
	case "s3":
		operations = append(operations,
			AWSOperationInfo{
				Name: "ListBuckets", Type: "List", ResourceType: "Bucket",
				Description: "Lists S3 buckets", InputType: "ListBucketsInput",
				OutputType: "ListBucketsOutput", Paginated: false,
			},
		)
	}
	
	return operations
}

func getAWSResourcesForService(serviceName string) []AWSResourceInfo {
	// This would use existing AWS resource discovery logic
	resources := []AWSResourceInfo{}
	
	switch serviceName {
	case "ec2":
		resources = append(resources,
			AWSResourceInfo{
				Name: "Instance", DisplayName: "EC2 Instance", 
				Description: "Amazon EC2 virtual machine instance",
				Identifiers: []string{"InstanceId"}, ARNField: "InstanceArn", IDField: "InstanceId",
			},
			AWSResourceInfo{
				Name: "Vpc", DisplayName: "VPC", 
				Description: "Amazon Virtual Private Cloud",
				Identifiers: []string{"VpcId"}, IDField: "VpcId",
			},
		)
	case "s3":
		resources = append(resources,
			AWSResourceInfo{
				Name: "Bucket", DisplayName: "S3 Bucket",
				Description: "Amazon S3 storage bucket", 
				Identifiers: []string{"Name"}, ARNField: "Arn", IDField: "Name",
			},
		)
	}
	
	return resources
}

// convertFromProtoAnalysis converts protobuf analysis back to generator types
func convertFromProtoAnalysis(analysis *pb.AnalysisResponse) []*generator.AWSServiceInfo {
	services := []*generator.AWSServiceInfo{}
	
	for _, protoService := range analysis.Services {
		service := &generator.AWSServiceInfo{
			Name:        protoService.Name,
			PackageName: protoService.PackageName,
			ClientType:  fmt.Sprintf("%sClient", strings.Title(protoService.Name)),
		}
		
		// Convert operations
		for _, protoOp := range analysis.Operations {
			if protoOp.Service == protoService.Name {
				service.Operations = append(service.Operations, generator.AWSOperation{
					Name:         protoOp.Name,
					ResourceType: protoOp.ResourceType,
					IsList:       protoOp.OperationType == "List",
					IsDescribe:   protoOp.OperationType == "Describe",
					IsGet:        protoOp.OperationType == "Get",
					Paginated:    protoOp.Paginated,
				})
			}
		}
		
		// Convert resources
		for _, protoRes := range analysis.Resources {
			if protoRes.Service == protoService.Name {
				service.ResourceTypes = append(service.ResourceTypes, generator.AWSResourceType{
					Name:     protoRes.Name,
					TypeName: protoRes.DisplayName,
					IDField:  protoRes.Identifiers[0], // Use first identifier as ID field
				})
			}
		}
		
		services = append(services, service)
	}
	
	return services
}

// Helper types for AWS analysis
type AWSOperationInfo struct {
	Name         string
	Type         string // List, Describe, Get, etc.
	ResourceType string
	Description  string
	InputType    string
	OutputType   string
	Paginated    bool
}

type AWSResourceInfo struct {
	Name        string
	DisplayName string
	Description string
	Identifiers []string
	ARNField    string
	IDField     string
}
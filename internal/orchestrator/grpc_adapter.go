package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// GRPCProviderAnalyzer adapts gRPC provider to local ProviderAnalyzer interface
type GRPCProviderAnalyzer struct {
	client       pb.CloudProviderClient
	providerName string
}

// GRPCProviderGenerator adapts gRPC provider to local ProviderGenerator interface
type GRPCProviderGenerator struct {
	client       pb.CloudProviderClient
	providerName string
}

// NewGRPCProviderAnalyzer creates a new gRPC provider analyzer adapter
func NewGRPCProviderAnalyzer(client pb.CloudProviderClient, providerName string) *GRPCProviderAnalyzer {
	return &GRPCProviderAnalyzer{
		client:       client,
		providerName: providerName,
	}
}

// NewGRPCProviderGenerator creates a new gRPC provider generator adapter
func NewGRPCProviderGenerator(client pb.CloudProviderClient, providerName string) *GRPCProviderGenerator {
	return &GRPCProviderGenerator{
		client:       client,
		providerName: providerName,
	}
}

// Analyze converts DiscoveryResult to pb.AnalyzeRequest, calls gRPC, and converts response
func (g *GRPCProviderAnalyzer) Analyze(ctx context.Context, discovered *DiscoveryResult) (*AnalysisResult, error) {
	// Convert DiscoveryResult to gRPC request
	analyzeReq, err := g.convertDiscoveryResultToAnalyzeRequest(discovered)
	if err != nil {
		return nil, fmt.Errorf("failed to convert discovery result: %w", err)
	}

	// Call gRPC method
	response, err := g.client.AnalyzeDiscoveredData(ctx, analyzeReq)
	if err != nil {
		return nil, fmt.Errorf("gRPC analysis failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("analysis failed: %s", response.Error)
	}

	// Convert gRPC response to AnalysisResult
	result, err := g.convertAnalysisResponseToResult(response)
	if err != nil {
		return nil, fmt.Errorf("failed to convert analysis response: %w", err)
	}

	return result, nil
}

// Validate checks if the analysis results are valid and complete
func (g *GRPCProviderAnalyzer) Validate(analysis *AnalysisResult) []string {
	warnings := []string{}

	// Basic validation checks
	if len(analysis.Services) == 0 {
		warnings = append(warnings, "No services found in analysis")
	}

	if len(analysis.Resources) == 0 {
		warnings = append(warnings, "No resource types found in analysis")
	}

	if len(analysis.Operations) == 0 {
		warnings = append(warnings, "No operations found in analysis")
	}

	// Check for services without resources
	serviceHasResources := make(map[string]bool)
	for _, resource := range analysis.Resources {
		serviceHasResources[resource.Service] = true
	}

	for _, service := range analysis.Services {
		if !serviceHasResources[service.Name] {
			warnings = append(warnings, fmt.Sprintf("Service '%s' has no resource types", service.Name))
		}
	}

	// Check for resources without operations
	resourceHasOperations := make(map[string]bool)
	for _, op := range analysis.Operations {
		key := fmt.Sprintf("%s:%s", op.Service, op.ResourceType)
		resourceHasOperations[key] = true
	}

	for _, resource := range analysis.Resources {
		key := fmt.Sprintf("%s:%s", resource.Service, resource.Name)
		if !resourceHasOperations[key] {
			warnings = append(warnings, fmt.Sprintf("Resource '%s.%s' has no operations", resource.Service, resource.Name))
		}
	}

	// Add any warnings from the original analysis
	warnings = append(warnings, analysis.Warnings...)

	return warnings
}

// Generate creates code from analysis results
func (g *GRPCProviderGenerator) Generate(ctx context.Context, analysis *AnalysisResult, options GenerationOptions) (*GenerationResult, error) {
	// Convert AnalysisResult to gRPC request
	generateReq, err := g.convertAnalysisResultToGenerateRequest(analysis, options)
	if err != nil {
		return nil, fmt.Errorf("failed to convert analysis result: %w", err)
	}

	// Call gRPC method
	response, err := g.client.GenerateFromAnalysis(ctx, generateReq)
	if err != nil {
		return nil, fmt.Errorf("gRPC generation failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("generation failed: %s", response.Error)
	}

	// Convert gRPC response to GenerationResult
	result, err := g.convertGenerateResponseToResult(response)
	if err != nil {
		return nil, fmt.Errorf("failed to convert generation response: %w", err)
	}

	return result, nil
}

// GetTemplates returns available generation templates
func (g *GRPCProviderGenerator) GetTemplates() []GenerationTemplate {
	// Return AWS-specific templates
	templates := []GenerationTemplate{
		{
			Name:        "scanner",
			Description: "AWS resource scanner plugin",
			FilePattern: "{{.Service}}_scanner.go",
			Type:        "scanner",
		},
		{
			Name:        "types",
			Description: "AWS resource type definitions",
			FilePattern: "{{.Service}}_types.go",
			Type:        "types",
		},
		{
			Name:        "client",
			Description: "AWS service client wrapper",
			FilePattern: "{{.Service}}_client.go",
			Type:        "client",
		},
		{
			Name:        "plugin",
			Description: "Complete AWS plugin with all components",
			FilePattern: "{{.Service}}_plugin.go",
			Type:        "plugin",
		},
	}

	return templates
}

// Helper methods for type conversion

func (g *GRPCProviderAnalyzer) convertDiscoveryResultToAnalyzeRequest(discovered *DiscoveryResult) (*pb.AnalyzeRequest, error) {
	// Determine source type based on which source has data
	sourceType := "github" // default
	var rawData []byte
	var err error

	// Check which source has data and marshal it
	for sourceName, sourceData := range discovered.Sources {
		if sourceData != nil {
			sourceType = sourceName
			rawData, err = json.Marshal(sourceData)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal source data for %s: %w", sourceName, err)
			}
			break
		}
	}

	// Convert filters to options
	options := make(map[string]string)
	if discovered.Metadata != nil {
		for k, v := range discovered.Metadata {
			if str, ok := v.(string); ok {
				options[k] = str
			}
		}
	}

	return &pb.AnalyzeRequest{
		SourceType: sourceType,
		RawData:    rawData,
		Options:    options,
	}, nil
}

func (g *GRPCProviderAnalyzer) convertAnalysisResponseToResult(response *pb.AnalysisResponse) (*AnalysisResult, error) {
	result := &AnalysisResult{
		Provider:      g.providerName,
		AnalyzedAt:    time.Now(),
		Services:      []ServiceInfo{},
		Resources:     []ResourceInfo{},
		Operations:    []OperationInfo{},
		Relationships: []RelationshipInfo{},
		Metadata:      make(map[string]interface{}),
		Warnings:      response.Warnings,
	}

	// Convert services
	for _, protoService := range response.Services {
		service := ServiceInfo{
			Name:        protoService.Name,
			DisplayName: protoService.DisplayName,
			Description: protoService.Description,
			Version:     protoService.Version,
			Metadata:    make(map[string]interface{}),
		}

		// Convert metadata
		for k, v := range protoService.Metadata {
			service.Metadata[k] = v
		}

		result.Services = append(result.Services, service)
	}

	// Convert resources
	for _, protoResource := range response.Resources {
		resource := ResourceInfo{
			Name:         protoResource.Name,
			Service:      protoResource.Service,
			DisplayName:  protoResource.DisplayName,
			Description:  protoResource.Description,
			Identifiers:  protoResource.Identifiers,
			Attributes:   []AttributeInfo{},
			Metadata:     make(map[string]interface{}),
		}

		// Convert attributes
		for _, protoAttr := range protoResource.Attributes {
			attr := AttributeInfo{
				Name:        protoAttr.Name,
				Type:        protoAttr.Type,
				Required:    protoAttr.Required,
				Description: protoAttr.Description,
			}
			resource.Attributes = append(resource.Attributes, attr)
		}

		// Convert metadata
		for k, v := range protoResource.Metadata {
			resource.Metadata[k] = v
		}

		result.Resources = append(result.Resources, resource)
	}

	// Convert operations
	for _, protoOp := range response.Operations {
		operation := OperationInfo{
			Name:         protoOp.Name,
			Service:      protoOp.Service,
			ResourceType: protoOp.ResourceType,
			Type:         protoOp.OperationType,
			Description:  protoOp.Description,
			Metadata:     make(map[string]interface{}),
		}

		// Convert metadata
		for k, v := range protoOp.Metadata {
			operation.Metadata[k] = v
		}

		result.Operations = append(result.Operations, operation)
	}

	// Convert response metadata
	for k, v := range response.Metadata {
		result.Metadata[k] = v
	}

	return result, nil
}

func (g *GRPCProviderGenerator) convertAnalysisResultToGenerateRequest(analysis *AnalysisResult, options GenerationOptions) (*pb.GenerateFromAnalysisRequest, error) {
	// Convert AnalysisResult back to AnalysisResponse format for the gRPC call
	analysisResponse := &pb.AnalysisResponse{
		Success:   true,
		Services:  []*pb.ServiceAnalysis{},
		Resources: []*pb.ResourceAnalysis{},
		Operations: []*pb.OperationAnalysis{},
		Metadata:  make(map[string]string),
		Warnings:  analysis.Warnings,
	}

	// Convert services
	for _, service := range analysis.Services {
		protoService := &pb.ServiceAnalysis{
			Name:        service.Name,
			DisplayName: service.DisplayName,
			Description: service.Description,
			Version:     service.Version,
			PackageName: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", service.Name), // AWS-specific
			Metadata:    make(map[string]string),
		}

		// Convert metadata
		for k, v := range service.Metadata {
			if str, ok := v.(string); ok {
				protoService.Metadata[k] = str
			}
		}

		analysisResponse.Services = append(analysisResponse.Services, protoService)
	}

	// Convert resources
	for _, resource := range analysis.Resources {
		protoResource := &pb.ResourceAnalysis{
			Name:        resource.Name,
			Service:     resource.Service,
			DisplayName: resource.DisplayName,
			Description: resource.Description,
			Identifiers: resource.Identifiers,
			Attributes:  []*pb.AttributeAnalysis{},
			Metadata:    make(map[string]string),
		}

		// Convert attributes
		for _, attr := range resource.Attributes {
			protoAttr := &pb.AttributeAnalysis{
				Name:        attr.Name,
				Type:        attr.Type,
				Required:    attr.Required,
				Description: attr.Description,
			}
			protoResource.Attributes = append(protoResource.Attributes, protoAttr)
		}

		// Convert metadata
		for k, v := range resource.Metadata {
			if str, ok := v.(string); ok {
				protoResource.Metadata[k] = str
			}
		}

		analysisResponse.Resources = append(analysisResponse.Resources, protoResource)
	}

	// Convert operations
	for _, operation := range analysis.Operations {
		protoOp := &pb.OperationAnalysis{
			Name:          operation.Name,
			Service:       operation.Service,
			ResourceType:  operation.ResourceType,
			OperationType: operation.Type,
			Description:   operation.Description,
			Paginated:     false, // Would need to be determined from metadata
			Metadata:      make(map[string]string),
		}

		// Convert metadata
		for k, v := range operation.Metadata {
			if str, ok := v.(string); ok {
				protoOp.Metadata[k] = str
			}
		}

		analysisResponse.Operations = append(analysisResponse.Operations, protoOp)
	}

	// Convert analysis metadata
	for k, v := range analysis.Metadata {
		if str, ok := v.(string); ok {
			analysisResponse.Metadata[k] = str
		}
	}

	// Convert generation options
	gRPCOptions := make(map[string]string)
	gRPCOptions["output_dir"] = options.OutputDir
	if options.Overwrite {
		gRPCOptions["overwrite"] = "true"
	}
	if options.DryRun {
		gRPCOptions["dry_run"] = "true"
	}

	// Convert templates to target services if specific services are requested
	targetServices := []string{}
	if len(options.Templates) > 0 {
		// If specific templates are requested, we could filter services here
		// For now, generate for all services
		for _, service := range analysis.Services {
			targetServices = append(targetServices, service.Name)
		}
	}

	return &pb.GenerateFromAnalysisRequest{
		Analysis:       analysisResponse,
		TargetServices: targetServices,
		Options:        gRPCOptions,
	}, nil
}

func (g *GRPCProviderGenerator) convertGenerateResponseToResult(response *pb.GenerateResponse) (*GenerationResult, error) {
	result := &GenerationResult{
		Provider:    g.providerName,
		GeneratedAt: time.Now(),
		Files:       []GeneratedFile{},
		Stats:       GenerationStats{},
		Errors:      response.Warnings, // Include warnings as errors for compatibility
	}

	// Convert files
	for _, protoFile := range response.Files {
		file := GeneratedFile{
			Path:     protoFile.Path,
			Content:  protoFile.Content,
			Template: protoFile.Template,
			Service:  protoFile.Service,
			Resource: protoFile.Resource,
		}
		result.Files = append(result.Files, file)
	}

	// Convert stats
	if response.Stats != nil {
		result.Stats = GenerationStats{
			TotalFiles:       int(response.Stats.TotalFiles),
			TotalServices:    int(response.Stats.TotalServices),
			TotalResources:   int(response.Stats.TotalResources),
			TotalOperations:  int(response.Stats.TotalOperations),
			GenerationTimeMs: int(response.Stats.GenerationTimeMs),
		}
	}

	return result, nil
}
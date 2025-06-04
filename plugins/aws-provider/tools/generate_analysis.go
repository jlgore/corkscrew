package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// AnalysisGenerator generates configuration analysis files for all AWS services
type AnalysisGenerator struct {
	config       aws.Config
	outputDir    string
	clientFactory ClientFactory
	discoveredServices []string
}

// ServiceAnalysis represents the analysis data for a service  
type ServiceAnalysis struct {
	ServiceName string      `json:"serviceName"`
	Services    []Service   `json:"services"`
	Metadata    interface{} `json:"metadata,omitempty"`
}

// Service represents a single AWS service analysis
type Service struct {
	Name       string      `json:"name"`
	Operations []Operation `json:"operations"`
}

// Operation represents an AWS API operation
type Operation struct {
	Name        string                 `json:"name"`
	IsList      bool                   `json:"is_list"`
	InputType   string                 `json:"input_type,omitempty"`
	OutputType  string                 `json:"output_type,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ClientFactory interface for creating AWS service clients
type ClientFactory interface {
	GetClient(serviceName string) interface{}
	GetAvailableServices() []string
}

// NewAnalysisGenerator creates a new analysis generator
func NewAnalysisGenerator(outputDir string, clientFactory ClientFactory) (*AnalysisGenerator, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get discovered services from client factory
	discoveredServices := clientFactory.GetAvailableServices()

	return &AnalysisGenerator{
		config:             cfg,
		outputDir:          outputDir,
		clientFactory:      clientFactory,
		discoveredServices: discoveredServices,
	}, nil
}

// GenerateAll generates analysis files for all discovered services
func (g *AnalysisGenerator) GenerateAll() error {
	log.Printf("Generating analysis files for %d services", len(g.discoveredServices))

	successCount := 0
	for _, serviceName := range g.discoveredServices {
		if err := g.GenerateForService(serviceName); err != nil {
			log.Printf("Failed to generate analysis for %s: %v", serviceName, err)
			continue
		}
		successCount++
		log.Printf("Generated analysis for service: %s", serviceName)
	}

	log.Printf("Successfully generated analysis for %d/%d services", successCount, len(g.discoveredServices))
	return nil
}

// GenerateForService generates analysis for a specific service
func (g *AnalysisGenerator) GenerateForService(serviceName string) error {
	client := g.clientFactory.GetClient(serviceName)
	if client == nil {
		return fmt.Errorf("no client available for service: %s", serviceName)
	}

	analysis, err := g.analyzeService(serviceName, client)
	if err != nil {
		return fmt.Errorf("failed to analyze service %s: %w", serviceName, err)
	}

	// Save analysis to file
	filename := fmt.Sprintf("%s_final.json", serviceName)
	filepath := filepath.Join(g.outputDir, filename)

	return g.saveAnalysis(filepath, analysis)
}

// analyzeService analyzes a service client using reflection
func (g *AnalysisGenerator) analyzeService(serviceName string, client interface{}) (*ServiceAnalysis, error) {
	clientValue := reflect.ValueOf(client)
	clientType := clientValue.Type()

	var operations []Operation
	configOpCount := 0
	listOpCount := 0

	log.Printf("Analyzing %s client type: %v", serviceName, clientType)

	// Discover all operations using reflection
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		methodName := method.Name

		// Skip non-public methods and special methods
		if !method.IsExported() || g.isSpecialMethod(methodName) {
			continue
		}

		// Analyze the operation
		operation := Operation{
			Name:     methodName,
			IsList:   g.isListOperation(methodName),
			Metadata: make(map[string]interface{}),
		}

		// Add method type information
		if method.Type.NumIn() > 1 { // Skip receiver
			operation.InputType = method.Type.In(1).String()
		}
		if method.Type.NumOut() > 0 {
			operation.OutputType = method.Type.Out(0).String()
		}

		// Categorize operation type
		opType := g.categorizeOperation(methodName)
		operation.Metadata["operationType"] = opType
		operation.Metadata["isConfiguration"] = g.isConfigurationOperation(methodName)
		operation.Metadata["requiresResourceId"] = g.requiresResourceID(methodName)
		operation.Metadata["isIdempotent"] = g.isIdempotent(methodName)
		operation.Metadata["isMutating"] = g.isMutating(methodName)

		// Track operation counts
		if operation.IsList {
			listOpCount++
		}
		if g.isConfigurationOperation(methodName) {
			configOpCount++
		}

		operations = append(operations, operation)
	}

	return &ServiceAnalysis{
		ServiceName: serviceName,
		Services: []Service{
			{
				Name:       serviceName,
				Operations: operations,
			},
		},
		Metadata: map[string]interface{}{
			"generatedAt":              time.Now().Format(time.RFC3339),
			"totalOperations":          len(operations),
			"configurationOperations":  configOpCount,
			"listOperations":          listOpCount,
			"clientType":              clientType.String(),
			"analysisVersion":         "1.0",
		},
	}, nil
}

// Helper methods for operation analysis

func (g *AnalysisGenerator) isSpecialMethod(methodName string) bool {
	specialMethods := []string{
		"String", "GoString", "Error", "Validate", "SetContext",
		"Context", "Options", "WithRegion", "WithCredentials",
		"New", "Clone", "Copy",
	}
	
	for _, special := range specialMethods {
		if methodName == special {
			return true
		}
	}
	
	return false
}

func (g *AnalysisGenerator) isListOperation(methodName string) bool {
	listPrefixes := []string{"List", "Describe"}
	
	for _, prefix := range listPrefixes {
		if strings.HasPrefix(methodName, prefix) {
			// Exclude configuration operations that happen to start with Describe
			if !g.isConfigurationOperation(methodName) {
				return true
			}
		}
	}
	
	return false
}

func (g *AnalysisGenerator) isConfigurationOperation(methodName string) bool {
	configPatterns := []string{
		"Get", "Head", "Check", "Retrieve", "Fetch",
	}
	
	// Include specific Describe operations that get configuration for single resources
	specificDescribe := []string{
		"DescribeBucket", "DescribeInstance", "DescribeFunction",
		"DescribeTable", "DescribeKey", "DescribePolicy",
		"DescribeRole", "DescribeUser", "DescribeGroup",
	}
	
	for _, pattern := range configPatterns {
		if strings.HasPrefix(methodName, pattern) {
			return true
		}
	}
	
	for _, specific := range specificDescribe {
		if strings.Contains(methodName, specific) {
			return true
		}
	}
	
	return false
}

func (g *AnalysisGenerator) categorizeOperation(methodName string) string {
	if strings.HasPrefix(methodName, "List") || strings.HasPrefix(methodName, "Describe") {
		if g.isConfigurationOperation(methodName) {
			return "configuration"
		}
		return "discovery"
	}
	if strings.HasPrefix(methodName, "Get") || strings.HasPrefix(methodName, "Head") {
		return "configuration"
	}
	if strings.HasPrefix(methodName, "Create") || strings.HasPrefix(methodName, "Put") {
		return "create"
	}
	if strings.HasPrefix(methodName, "Update") || strings.HasPrefix(methodName, "Modify") {
		return "update"
	}
	if strings.HasPrefix(methodName, "Delete") || strings.HasPrefix(methodName, "Remove") {
		return "delete"
	}
	if strings.HasPrefix(methodName, "Tag") || strings.HasPrefix(methodName, "Untag") {
		return "tagging"
	}
	
	return "unknown"
}

func (g *AnalysisGenerator) requiresResourceID(methodName string) bool {
	// Operations that typically require a specific resource ID
	requiresID := []string{
		"Get", "Delete", "Update", "Modify", "Put", "Head",
		"Tag", "Untag", "Enable", "Disable", "Start", "Stop",
	}
	
	for _, prefix := range requiresID {
		if strings.HasPrefix(methodName, prefix) {
			return true
		}
	}
	
	// Some Describe operations require resource IDs
	if strings.HasPrefix(methodName, "Describe") && g.isConfigurationOperation(methodName) {
		return true
	}
	
	return false
}

func (g *AnalysisGenerator) isIdempotent(methodName string) bool {
	// Idempotent operations can be retried safely
	idempotentPrefixes := []string{
		"Get", "List", "Describe", "Head", "Check", "Put",
	}
	
	for _, prefix := range idempotentPrefixes {
		if strings.HasPrefix(methodName, prefix) {
			return true
		}
	}
	
	return false
}

func (g *AnalysisGenerator) isMutating(methodName string) bool {
	// Operations that modify resources
	mutatingPrefixes := []string{
		"Create", "Delete", "Update", "Modify", "Put",
		"Tag", "Untag", "Enable", "Disable", "Start", "Stop",
		"Attach", "Detach", "Associate", "Disassociate",
	}
	
	for _, prefix := range mutatingPrefixes {
		if strings.HasPrefix(methodName, prefix) {
			return true
		}
	}
	
	return false
}

// saveAnalysis saves the analysis to a JSON file
func (g *AnalysisGenerator) saveAnalysis(filepath string, analysis *ServiceAnalysis) error {
	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal analysis: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write analysis file: %w", err)
	}

	log.Printf("Analysis saved to: %s", filepath)
	return nil
}

// GenerateForDiscoveredServices generates analysis files for services discovered via discovery system
func (g *AnalysisGenerator) GenerateForDiscoveredServices(discoveredServices []string) error {
	log.Printf("Generating analysis files for %d discovered services", len(discoveredServices))

	successCount := 0
	for _, serviceName := range discoveredServices {
		if err := g.GenerateForService(serviceName); err != nil {
			log.Printf("Failed to generate analysis for discovered service %s: %v", serviceName, err)
			continue
		}
		successCount++
		log.Printf("Generated analysis for discovered service: %s", serviceName)
	}

	log.Printf("Successfully generated analysis for %d/%d discovered services", successCount, len(discoveredServices))
	return nil
}

// ValidateAnalysisFile validates an existing analysis file
func (g *AnalysisGenerator) ValidateAnalysisFile(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("failed to read analysis file: %w", err)
	}

	var analysis ServiceAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return fmt.Errorf("failed to parse analysis file: %w", err)
	}

	// Basic validation
	if analysis.ServiceName == "" {
		return fmt.Errorf("analysis file missing service name")
	}

	if len(analysis.Services) == 0 {
		return fmt.Errorf("analysis file has no services")
	}

	if len(analysis.Services[0].Operations) == 0 {
		return fmt.Errorf("analysis file has no operations")
	}

	log.Printf("Analysis file %s is valid with %d operations", filepath, len(analysis.Services[0].Operations))
	return nil
}
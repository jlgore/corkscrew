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

// GenerateForFilteredServices generates analysis files for specific requested services
func (g *AnalysisGenerator) GenerateForFilteredServices(discoveredServices []string, requestedServices []string) error {
	// Create a service filter map for faster lookup
	serviceFilter := make(map[string]bool)
	if len(requestedServices) > 0 {
		for _, svc := range requestedServices {
			serviceFilter[strings.ToLower(svc)] = true
		}
	}

	// Filter discovered services if a filter is provided
	var servicesToProcess []string
	if len(serviceFilter) > 0 {
		for _, serviceName := range discoveredServices {
			if serviceFilter[strings.ToLower(serviceName)] {
				servicesToProcess = append(servicesToProcess, serviceName)
			}
		}
		log.Printf("Filtered %d services down to %d requested services", len(discoveredServices), len(servicesToProcess))
	} else {
		servicesToProcess = discoveredServices
		log.Printf("No filter provided, processing all %d discovered services", len(discoveredServices))
	}

	return g.GenerateForDiscoveredServices(servicesToProcess)
}

// GenerateForDiscoveredServices generates analysis files for services discovered via discovery system
func (g *AnalysisGenerator) GenerateForDiscoveredServices(discoveredServices []string) error {
	log.Printf("Starting analysis generation for %d discovered services", len(discoveredServices))

	var errors []error
	generated := 0
	skipped := 0
	
	for _, serviceName := range discoveredServices {
		outputPath := filepath.Join(g.outputDir, fmt.Sprintf("%s_final.json", serviceName))
		
		// Skip if already exists and is recent
		if g.isAnalysisRecent(outputPath) {
			log.Printf("Skipping %s - analysis file is recent", serviceName)
			skipped++
			continue
		}
		
		if err := g.generateServiceAnalysis(serviceName, outputPath); err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", serviceName, err))
			log.Printf("Failed to generate analysis for service %s: %v", serviceName, err)
		} else {
			generated++
			log.Printf("Generated analysis for service: %s", serviceName)
		}
	}
	
	log.Printf("Analysis generation complete: %d generated, %d skipped, %d errors", 
		generated, skipped, len(errors))
	
	if len(errors) > 0 {
		return fmt.Errorf("generated %d/%d analysis files with %d errors: %v", 
			generated, len(discoveredServices), len(errors), errors)
	}
	
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

// isAnalysisRecent checks if an analysis file exists and was generated recently
func (g *AnalysisGenerator) isAnalysisRecent(filePath string) bool {
	info, err := os.Stat(filePath)
	if err != nil {
		// File doesn't exist
		return false
	}
	
	// Consider file recent if it's less than 24 hours old
	maxAge := 24 * time.Hour
	fileAge := time.Since(info.ModTime())
	
	if fileAge > maxAge {
		log.Printf("Analysis file %s is stale (age: %v)", filePath, fileAge)
		return false
	}
	
	// Validate the file content
	if err := g.ValidateAnalysisFile(filePath); err != nil {
		log.Printf("Analysis file %s is invalid: %v", filePath, err)
		return false
	}
	
	return true
}

// generateServiceAnalysis generates analysis for a specific service and saves to specified path
func (g *AnalysisGenerator) generateServiceAnalysis(serviceName, outputPath string) error {
	client := g.clientFactory.GetClient(serviceName)
	if client == nil {
		return fmt.Errorf("no client available for service: %s", serviceName)
	}
	
	analysis, err := g.analyzeService(serviceName, client)
	if err != nil {
		return fmt.Errorf("failed to analyze service %s: %w", serviceName, err)
	}
	
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	
	return g.saveAnalysis(outputPath, analysis)
}

// GetGeneratedAnalysisFiles returns a list of all generated analysis files
func (g *AnalysisGenerator) GetGeneratedAnalysisFiles() ([]string, error) {
	var files []string
	
	entries, err := os.ReadDir(g.outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return files, nil // Empty list if directory doesn't exist
		}
		return nil, fmt.Errorf("failed to read output directory: %w", err)
	}
	
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_final.json") {
			files = append(files, filepath.Join(g.outputDir, entry.Name()))
		}
	}
	
	return files, nil
}

// ValidateAllGeneratedFiles validates all generated analysis files
func (g *AnalysisGenerator) ValidateAllGeneratedFiles() error {
	files, err := g.GetGeneratedAnalysisFiles()
	if err != nil {
		return fmt.Errorf("failed to get generated files: %w", err)
	}
	
	if len(files) == 0 {
		return fmt.Errorf("no analysis files found in output directory: %s", g.outputDir)
	}
	
	var validationErrors []error
	validCount := 0
	
	for _, file := range files {
		if err := g.ValidateAnalysisFile(file); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("%s: %w", filepath.Base(file), err))
		} else {
			validCount++
		}
	}
	
	log.Printf("Validated %d/%d analysis files", validCount, len(files))
	
	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed for %d files: %v", len(validationErrors), validationErrors)
	}
	
	return nil
}

// EnsureOutputDirectory creates the output directory if it doesn't exist
func (g *AnalysisGenerator) EnsureOutputDirectory() error {
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", g.outputDir, err)
	}
	
	log.Printf("Output directory ensured: %s", g.outputDir)
	return nil
}

// GetAnalysisStats returns statistics about generated analysis files
func (g *AnalysisGenerator) GetAnalysisStats() (*AnalysisStats, error) {
	files, err := g.GetGeneratedAnalysisFiles()
	if err != nil {
		return nil, err
	}
	
	stats := &AnalysisStats{
		TotalFiles:      len(files),
		ValidFiles:      0,
		InvalidFiles:    0,
		TotalOperations: 0,
		OutputDirectory: g.outputDir,
	}
	
	for _, file := range files {
		if err := g.ValidateAnalysisFile(file); err != nil {
			stats.InvalidFiles++
		} else {
			stats.ValidFiles++
			
			// Count operations in this file
			if analysis, err := g.loadAnalysisFile(file); err == nil {
				if len(analysis.Services) > 0 {
					stats.TotalOperations += len(analysis.Services[0].Operations)
				}
			}
		}
	}
	
	return stats, nil
}

// AnalysisStats represents statistics about generated analysis files
type AnalysisStats struct {
	TotalFiles      int    `json:"total_files"`
	ValidFiles      int    `json:"valid_files"`
	InvalidFiles    int    `json:"invalid_files"`
	TotalOperations int    `json:"total_operations"`
	OutputDirectory string `json:"output_directory"`
}

// loadAnalysisFile loads and parses an analysis file
func (g *AnalysisGenerator) loadAnalysisFile(filePath string) (*ServiceAnalysis, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	var analysis ServiceAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return nil, err
	}
	
	return &analysis, nil
}
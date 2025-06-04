package main

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
	clientCache  map[string]interface{}
	serviceList  []string
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

// NewAnalysisGenerator creates a new analysis generator
func NewAnalysisGenerator(outputDir string) (*AnalysisGenerator, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	return &AnalysisGenerator{
		config:      cfg,
		outputDir:   outputDir,
		clientCache: make(map[string]interface{}),
		serviceList: getDiscoveredServices(),
	}, nil
}

// GenerateAll generates analysis files for all discovered services
func (g *AnalysisGenerator) GenerateAll() error {
	log.Printf("Generating analysis files for %d services", len(g.serviceList))

	for _, serviceName := range g.serviceList {
		if err := g.GenerateForService(serviceName); err != nil {
			log.Printf("Failed to generate analysis for %s: %v", serviceName, err)
			continue
		}
		log.Printf("Generated analysis for service: %s", serviceName)
	}

	return nil
}

// GenerateForService generates analysis for a specific service
func (g *AnalysisGenerator) GenerateForService(serviceName string) error {
	client, err := g.getOrCreateClient(serviceName)
	if err != nil {
		return fmt.Errorf("failed to create client for %s: %w", serviceName, err)
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
	clientType := reflect.TypeOf(client)
	if clientType.Kind() == reflect.Ptr {
		clientType = clientType.Elem()
	}

	var operations []Operation

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
		if method.Type.NumIn() > 0 {
			operation.InputType = method.Type.In(1).String() // Skip receiver
		}
		if method.Type.NumOut() > 0 {
			operation.OutputType = method.Type.Out(0).String()
		}

		// Categorize operation type
		operation.Metadata["operationType"] = g.categorizeOperation(methodName)
		operation.Metadata["isConfiguration"] = g.isConfigurationOperation(methodName)
		operation.Metadata["requiresResourceId"] = g.requiresResourceID(methodName)

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
			"generatedAt":     time.Now().Format(time.RFC3339),
			"totalOperations": len(operations),
			"clientType":      clientType.String(),
		},
	}, nil
}

// getOrCreateClient gets or creates a client for the specified service
func (g *AnalysisGenerator) getOrCreateClient(serviceName string) (interface{}, error) {
	if client, exists := g.clientCache[serviceName]; exists {
		return client, nil
	}

	// Use reflection to create service clients dynamically
	client, err := g.createServiceClient(serviceName)
	if err != nil {
		return nil, err
	}

	g.clientCache[serviceName] = client
	return client, nil
}

// createServiceClient creates a client for the specified service using reflection
func (g *AnalysisGenerator) createServiceClient(serviceName string) (interface{}, error) {
	// This is a simplified version - in practice, you'd use the ClientFactory
	// or discovery mechanisms to create the appropriate client
	
	switch strings.ToLower(serviceName) {
	case "s3":
		return g.createS3Client()
	case "ec2":
		return g.createEC2Client()
	case "lambda":
		return g.createLambdaClient()
	case "rds":
		return g.createRDSClient()
	case "iam":
		return g.createIAMClient()
	case "dynamodb":
		return g.createDynamoDBClient()
	default:
		return nil, fmt.Errorf("unsupported service: %s", serviceName)
	}
}

// Placeholder methods for client creation - these would be implemented
// to use the actual AWS SDK v2 clients
func (g *AnalysisGenerator) createS3Client() (interface{}, error) {
	// Implementation would create actual S3 client
	return struct{ Name string }{"s3-client"}, nil
}

func (g *AnalysisGenerator) createEC2Client() (interface{}, error) {
	return struct{ Name string }{"ec2-client"}, nil
}

func (g *AnalysisGenerator) createLambdaClient() (interface{}, error) {
	return struct{ Name string }{"lambda-client"}, nil
}

func (g *AnalysisGenerator) createRDSClient() (interface{}, error) {
	return struct{ Name string }{"rds-client"}, nil
}

func (g *AnalysisGenerator) createIAMClient() (interface{}, error) {
	return struct{ Name string }{"iam-client"}, nil
}

func (g *AnalysisGenerator) createDynamoDBClient() (interface{}, error) {
	return struct{ Name string }{"dynamodb-client"}, nil
}

// Helper methods for operation analysis

func (g *AnalysisGenerator) isSpecialMethod(methodName string) bool {
	specialMethods := []string{
		"String", "GoString", "Error", "Validate", "SetContext",
		"Context", "Options", "WithRegion", "WithCredentials",
	}
	
	for _, special := range specialMethods {
		if methodName == special {
			return true
		}
	}
	
	return false
}

func (g *AnalysisGenerator) isListOperation(methodName string) bool {
	return strings.HasPrefix(methodName, "List") || 
		   strings.HasPrefix(methodName, "Describe") && 
		   !g.isConfigurationOperation(methodName)
}

func (g *AnalysisGenerator) isConfigurationOperation(methodName string) bool {
	configPatterns := []string{
		"Get", "Describe", "Head", "Check", "Retrieve",
	}
	
	// Exclude list operations
	if g.isListOperation(methodName) {
		return false
	}
	
	for _, pattern := range configPatterns {
		if strings.HasPrefix(methodName, pattern) {
			return true
		}
	}
	
	return false
}

func (g *AnalysisGenerator) categorizeOperation(methodName string) string {
	if strings.HasPrefix(methodName, "List") || strings.HasPrefix(methodName, "Describe") {
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
	
	return "unknown"
}

func (g *AnalysisGenerator) requiresResourceID(methodName string) bool {
	// Operations that typically require a specific resource ID
	return strings.HasPrefix(methodName, "Get") ||
		   strings.HasPrefix(methodName, "Delete") ||
		   strings.HasPrefix(methodName, "Update") ||
		   strings.HasPrefix(methodName, "Modify") ||
		   strings.HasPrefix(methodName, "Put") ||
		   strings.HasPrefix(methodName, "Head")
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

// getDiscoveredServices returns the list of discovered services
// This would typically come from the discovery system
func getDiscoveredServices() []string {
	// Check environment variable first
	if servicesEnv := os.Getenv("AWS_SERVICES"); servicesEnv != "" {
		services := strings.Split(servicesEnv, ",")
		for i := range services {
			services[i] = strings.TrimSpace(services[i])
		}
		return services
	}
	
	// Default to all services
	return []string{
		"s3", "ec2", "lambda", "rds", "iam", "dynamodb",
		"ecs", "eks", "elb", "elbv2", "cloudformation",
		"cloudwatch", "logs", "sns", "sqs", "kms",
		"secretsmanager", "ssm", "apigateway", "route53",
		"acm", "cloudfront", "cognito", "elasticache",
		"redshift", "emr", "glue", "athena", "kinesis",
	}
}

func main() {
	// Default output directory
	outputDir := "generated"
	if len(os.Args) > 1 {
		outputDir = os.Args[1]
	}

	generator, err := NewAnalysisGenerator(outputDir)
	if err != nil {
		log.Fatalf("Failed to create analysis generator: %v", err)
	}

	if err := generator.GenerateAll(); err != nil {
		log.Fatalf("Failed to generate analysis files: %v", err)
	}

	log.Println("Analysis generation completed successfully")
}
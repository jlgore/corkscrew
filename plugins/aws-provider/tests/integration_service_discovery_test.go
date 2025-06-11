//go:build integration
// +build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ServiceAnalysis represents the structure of generated analysis files
type ServiceAnalysis struct {
	Version       string    `json:"version"`
	AnalyzedAt    time.Time `json:"analyzed_at"`
	SDKVersion    string    `json:"sdk_version"`
	Services      []ServiceInfo `json:"services"`
	TotalOperations int     `json:"total_operations"`
	TotalResources  int     `json:"total_resources"`
}

type ServiceInfo struct {
	Name          string         `json:"name"`
	PackagePath   string         `json:"package_path"`
	ClientType    string         `json:"client_type"`
	Operations    []OperationInfo `json:"operations"`
	ResourceTypes []ResourceType  `json:"resource_types"`
	LastUpdated   time.Time      `json:"last_updated"`
}

type OperationInfo struct {
	Name         string `json:"name"`
	InputType    string `json:"input_type"`
	OutputType   string `json:"output_type"`
	ResourceType string `json:"resource_type"`
	IsList       bool   `json:"is_list"`
	IsPaginated  bool   `json:"is_paginated"`
}

type ResourceType struct {
	Name       string            `json:"name"`
	GoType     string            `json:"go_type"`
	PrimaryKey string            `json:"primary_key"`
	Fields     map[string]string `json:"fields"`
}

// TestGeneratedServiceDiscovery tests discovery using generated analysis files
func TestGeneratedServiceDiscovery(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=true to run.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err, "Failed to load AWS config")

	suite := &IntegrationTestSuite{
		cfg: cfg,
		ctx: ctx,
	}

	t.Run("LoadGeneratedServices", suite.testLoadGeneratedServices)
	t.Run("ValidateServiceStructure", suite.testValidateServiceStructure)
	t.Run("ScanGeneratedServices", suite.testScanGeneratedServices)
	t.Run("CompareWithRuntimeDiscovery", suite.testCompareWithRuntimeDiscovery)
	t.Run("ServiceAvailabilityCheck", suite.testServiceAvailabilityCheck)
}

type IntegrationTestSuite struct {
	cfg             aws.Config
	ctx             context.Context
	generatedAnalysis *ServiceAnalysis
	pipeline        *runtime.RuntimePipeline
}

// testLoadGeneratedServices loads and validates the generated services.json file
func (s *IntegrationTestSuite) testLoadGeneratedServices(t *testing.T) {
	// Load the generated services analysis
	generatedPath := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"
	
	data, err := os.ReadFile(generatedPath)
	require.NoError(t, err, "Failed to read generated services file")

	var analysis ServiceAnalysis
	err = json.Unmarshal(data, &analysis)
	require.NoError(t, err, "Failed to parse generated services JSON")

	s.generatedAnalysis = &analysis

	// Validate basic structure
	assert.NotEmpty(t, analysis.Version, "Analysis should have version")
	assert.NotZero(t, analysis.AnalyzedAt, "Analysis should have analyzed timestamp")
	assert.NotEmpty(t, analysis.Services, "Analysis should contain services")
	assert.Greater(t, analysis.TotalOperations, 0, "Should have discovered operations")
	assert.Greater(t, analysis.TotalResources, 0, "Should have discovered resources")

	t.Logf("Loaded generated analysis: %d services, %d operations, %d resources", 
		len(analysis.Services), analysis.TotalOperations, analysis.TotalResources)

	// Verify expected services are present
	expectedServices := []string{"ec2", "s3", "lambda", "rds", "dynamodb", "iam"}
	serviceMap := make(map[string]ServiceInfo)
	for _, service := range analysis.Services {
		serviceMap[service.Name] = service
	}

	for _, expected := range expectedServices {
		assert.Contains(t, serviceMap, expected, "Expected service %s should be in generated analysis", expected)
		if service, exists := serviceMap[expected]; exists {
			assert.NotEmpty(t, service.Operations, "Service %s should have operations", expected)
			assert.NotEmpty(t, service.PackagePath, "Service %s should have package path", expected)
		}
	}
}

// testValidateServiceStructure validates the structure of each discovered service
func (s *IntegrationTestSuite) testValidateServiceStructure(t *testing.T) {
	require.NotNil(t, s.generatedAnalysis, "Generated analysis must be loaded first")

	for _, service := range s.generatedAnalysis.Services {
		t.Run(fmt.Sprintf("Service_%s", service.Name), func(t *testing.T) {
			// Validate service metadata
			assert.NotEmpty(t, service.Name, "Service should have name")
			assert.NotEmpty(t, service.PackagePath, "Service should have package path")
			assert.NotEmpty(t, service.ClientType, "Service should have client type")
			assert.NotZero(t, service.LastUpdated, "Service should have last updated timestamp")

			// Validate operations
			assert.NotEmpty(t, service.Operations, "Service %s should have operations", service.Name)
			for _, op := range service.Operations {
				assert.NotEmpty(t, op.Name, "Operation should have name")
				assert.NotEmpty(t, op.InputType, "Operation should have input type")
				assert.NotEmpty(t, op.OutputType, "Operation should have output type")
				
				// List operations should have resource type
				if op.IsList {
					assert.NotEmpty(t, op.ResourceType, "List operation should have resource type")
				}
			}

			// Validate resource types
			if len(service.ResourceTypes) > 0 {
				for _, rt := range service.ResourceTypes {
					assert.NotEmpty(t, rt.Name, "Resource type should have name")
					assert.NotEmpty(t, rt.GoType, "Resource type should have Go type")
					assert.NotEmpty(t, rt.PrimaryKey, "Resource type should have primary key")
					assert.NotEmpty(t, rt.Fields, "Resource type should have fields")
				}
			}

			t.Logf("Service %s: %d operations, %d resource types", 
				service.Name, len(service.Operations), len(service.ResourceTypes))
		})
	}
}

// testScanGeneratedServices attempts to scan each service discovered in the generated analysis
func (s *IntegrationTestSuite) testScanGeneratedServices(t *testing.T) {
	require.NotNil(t, s.generatedAnalysis, "Generated analysis must be loaded first")

	// Create pipeline for scanning
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      2,
		ScanTimeout:         30 * time.Second,
		UseResourceExplorer: false,
		BatchSize:           25,
		FlushInterval:       2 * time.Second,
		EnableAutoDiscovery: true,
		StreamingEnabled:    false,
	}

	pipeline, err := runtime.NewRuntimePipeline(s.cfg, pipelineConfig)
	require.NoError(t, err, "Failed to create pipeline")
	s.pipeline = pipeline

	err = pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")
	defer pipeline.Stop()

	successCount := 0
	var failedServices []string

	for _, service := range s.generatedAnalysis.Services {
		t.Run(fmt.Sprintf("Scan_%s", service.Name), func(t *testing.T) {
			scanCtx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
			defer cancel()

			result, err := pipeline.ScanService(scanCtx, service.Name, s.cfg, "us-east-1")
			
			if err != nil {
				t.Logf("Service %s scan failed: %v", service.Name, err)
				failedServices = append(failedServices, service.Name)
				return
			}

			// Validate scan result
			assert.Equal(t, service.Name, result.Service, "Result should match requested service")
			assert.Equal(t, "us-east-1", result.Region, "Result should match requested region")
			assert.NotZero(t, result.Duration, "Scan should have measurable duration")
			
			successCount++
			t.Logf("Service %s: PASS (Duration: %v)", service.Name, result.Duration)
		})
	}

	// Report overall results
	totalServices := len(s.generatedAnalysis.Services)
	successRate := float64(successCount) / float64(totalServices) * 100

	t.Logf("Generated Service Scanning Results:")
	t.Logf("  Total services: %d", totalServices)
	t.Logf("  Successful scans: %d", successCount)
	t.Logf("  Failed scans: %d (%v)", len(failedServices), failedServices)
	t.Logf("  Success rate: %.1f%%", successRate)

	// At least 80% should succeed
	assert.GreaterOrEqual(t, successRate, 80.0, "At least 80%% of generated services should scan successfully")
}

// testCompareWithRuntimeDiscovery compares generated services with runtime discovery
func (s *IntegrationTestSuite) testCompareWithRuntimeDiscovery(t *testing.T) {
	require.NotNil(t, s.generatedAnalysis, "Generated analysis must be loaded first")

	// Perform runtime discovery
	runtimeDiscovery := discovery.NewRuntimeServiceDiscovery(s.cfg)
	runtimeServices, err := runtimeDiscovery.DiscoverServices(s.ctx)
	require.NoError(t, err, "Runtime discovery should succeed")

	// Create maps for comparison
	generatedServiceMap := make(map[string]ServiceInfo)
	for _, service := range s.generatedAnalysis.Services {
		generatedServiceMap[service.Name] = service
	}

	runtimeServiceMap := make(map[string]discovery.ServiceInfo)
	for _, service := range runtimeServices {
		runtimeServiceMap[service.Name] = service
	}

	// Compare service lists
	var onlyInGenerated, onlyInRuntime, inBoth []string

	for name := range generatedServiceMap {
		if _, exists := runtimeServiceMap[name]; exists {
			inBoth = append(inBoth, name)
		} else {
			onlyInGenerated = append(onlyInGenerated, name)
		}
	}

	for name := range runtimeServiceMap {
		if _, exists := generatedServiceMap[name]; !exists {
			onlyInRuntime = append(onlyInRuntime, name)
		}
	}

	t.Logf("Service Discovery Comparison:")
	t.Logf("  Generated services: %d", len(generatedServiceMap))
	t.Logf("  Runtime services: %d", len(runtimeServiceMap))
	t.Logf("  In both: %d (%v)", len(inBoth), inBoth)
	t.Logf("  Only in generated: %d (%v)", len(onlyInGenerated), onlyInGenerated)
	t.Logf("  Only in runtime: %d (%v)", len(onlyInRuntime), onlyInRuntime)

	// Validate overlap - should have significant overlap
	overlapRatio := float64(len(inBoth)) / float64(len(generatedServiceMap)) * 100
	assert.GreaterOrEqual(t, overlapRatio, 70.0, "Should have at least 70%% overlap between generated and runtime discovery")

	// Compare operations for overlapping services
	for _, serviceName := range inBoth {
		genService := generatedServiceMap[serviceName]
		rtService := runtimeServiceMap[serviceName]

		t.Run(fmt.Sprintf("CompareOperations_%s", serviceName), func(t *testing.T) {
			genOps := make(map[string]bool)
			for _, op := range genService.Operations {
				genOps[op.Name] = true
			}

			rtOps := make(map[string]bool)
			for _, op := range rtService.Operations {
				rtOps[op] = true
			}

			// Check for operation overlap
			var commonOps []string
			for op := range genOps {
				if rtOps[op] {
					commonOps = append(commonOps, op)
				}
			}

			if len(genService.Operations) > 0 && len(rtService.Operations) > 0 {
				opOverlap := float64(len(commonOps)) / float64(len(genService.Operations)) * 100
				t.Logf("Service %s operation overlap: %.1f%% (%d/%d)", 
					serviceName, opOverlap, len(commonOps), len(genService.Operations))
			}
		})
	}
}

// testServiceAvailabilityCheck tests availability of services in current AWS region
func (s *IntegrationTestSuite) testServiceAvailabilityCheck(t *testing.T) {
	require.NotNil(t, s.generatedAnalysis, "Generated analysis must be loaded first")
	require.NotNil(t, s.pipeline, "Pipeline must be initialized")

	// Test service availability by trying quick operations
	availableServices := make(map[string]bool)
	var unavailableServices []string

	for _, service := range s.generatedAnalysis.Services {
		t.Run(fmt.Sprintf("Availability_%s", service.Name), func(t *testing.T) {
			// Try to check service availability using a lightweight operation
			available := s.checkServiceAvailability(t, service.Name)
			availableServices[service.Name] = available
			
			if !available {
				unavailableServices = append(unavailableServices, service.Name)
				t.Logf("Service %s: NOT AVAILABLE in current region/account", service.Name)
			} else {
				t.Logf("Service %s: AVAILABLE", service.Name)
			}
		})
	}

	availableCount := 0
	for _, available := range availableServices {
		if available {
			availableCount++
		}
	}

	t.Logf("Service Availability Results:")
	t.Logf("  Total services: %d", len(s.generatedAnalysis.Services))
	t.Logf("  Available: %d", availableCount)
	t.Logf("  Unavailable: %d (%v)", len(unavailableServices), unavailableServices)

	// Most services should be available in us-east-1
	availabilityRate := float64(availableCount) / float64(len(s.generatedAnalysis.Services)) * 100
	assert.GreaterOrEqual(t, availabilityRate, 60.0, "At least 60%% of services should be available")
}

// checkServiceAvailability performs a lightweight check to see if a service is available
func (s *IntegrationTestSuite) checkServiceAvailability(t *testing.T, serviceName string) bool {
	// Create a short timeout context for availability check
	checkCtx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	// Try a basic scan with very short timeout
	result, err := s.pipeline.ScanService(checkCtx, serviceName, s.cfg, "us-east-1")
	
	if err != nil {
		// Log the error for debugging but don't fail the test
		t.Logf("Service %s availability check failed: %v", serviceName, err)
		return false
	}

	return result != nil && result.Service == serviceName
}

// TestGeneratedServiceCoverage tests coverage of AWS services via GitHub API comparison
func TestGeneratedServiceCoverage(t *testing.T) {
	if os.Getenv("RUN_COVERAGE_TESTS") != "true" {
		t.Skip("Skipping coverage test. Set RUN_COVERAGE_TESTS=true to run.")
	}

	// Load generated services
	generatedPath := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"
	data, err := os.ReadFile(generatedPath)
	require.NoError(t, err, "Failed to read generated services file")

	var analysis ServiceAnalysis
	err = json.Unmarshal(data, &analysis)
	require.NoError(t, err, "Failed to parse generated services JSON")

	// Create a test that validates we're discovering a reasonable percentage of AWS services
	// This is a proxy test since we can't easily call GitHub API here
	
	// Known major AWS services that should be discoverable
	majorServices := []string{
		"ec2", "s3", "lambda", "rds", "dynamodb", "iam", 
		"ecs", "eks", "cloudformation", "cloudwatch",
		"sns", "sqs", "kinesis", "glue", "secretsmanager", "ssm",
	}

	discoveredServices := make(map[string]bool)
	for _, service := range analysis.Services {
		discoveredServices[service.Name] = true
	}

	var foundMajorServices []string
	var missingMajorServices []string

	for _, major := range majorServices {
		if discoveredServices[major] {
			foundMajorServices = append(foundMajorServices, major)
		} else {
			missingMajorServices = append(missingMajorServices, major)
		}
	}

	coverageRate := float64(len(foundMajorServices)) / float64(len(majorServices)) * 100

	t.Logf("Major Service Coverage Analysis:")
	t.Logf("  Total major services: %d", len(majorServices))
	t.Logf("  Found: %d (%v)", len(foundMajorServices), foundMajorServices)
	t.Logf("  Missing: %d (%v)", len(missingMajorServices), missingMajorServices)
	t.Logf("  Coverage rate: %.1f%%", coverageRate)

	// Should find at least 75% of major services
	assert.GreaterOrEqual(t, coverageRate, 75.0, "Should discover at least 75%% of major AWS services")

	// Total discovered services should be reasonable
	assert.GreaterOrEqual(t, len(analysis.Services), 5, "Should discover at least 5 services")
	assert.LessOrEqual(t, len(analysis.Services), 50, "Discovered services should be reasonable (not discovering too many)")
}

// TestPercentageBasedServiceValidation tests a percentage of discovered services
func TestPercentageBasedServiceValidation(t *testing.T) {
	if os.Getenv("RUN_PERCENTAGE_TESTS") != "true" {
		t.Skip("Skipping percentage test. Set RUN_PERCENTAGE_TESTS=true to run.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err, "Failed to load AWS config")

	// Load generated services
	generatedPath := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"
	data, err := os.ReadFile(generatedPath)
	require.NoError(t, err, "Failed to read generated services file")

	var analysis ServiceAnalysis
	err = json.Unmarshal(data, &analysis)
	require.NoError(t, err, "Failed to parse generated services JSON")

	// Test 30% of discovered services (minimum 3, maximum 10)
	totalServices := len(analysis.Services)
	testCount := max(3, min(10, totalServices*30/100))

	t.Logf("Testing %d out of %d discovered services (%.1f%%)", 
		testCount, totalServices, float64(testCount)/float64(totalServices)*100)

	// Create pipeline
	pipelineConfig := &runtime.PipelineConfig{
		MaxConcurrency:      3,
		ScanTimeout:         30 * time.Second,
		UseResourceExplorer: false,
		BatchSize:           25,
		FlushInterval:       2 * time.Second,
		EnableAutoDiscovery: true,
		StreamingEnabled:    false,
	}

	pipeline, err := runtime.NewRuntimePipeline(cfg, pipelineConfig)
	require.NoError(t, err, "Failed to create pipeline")

	err = pipeline.Start()
	require.NoError(t, err, "Failed to start pipeline")
	defer pipeline.Stop()

	// Test the first testCount services
	successCount := 0
	for i := 0; i < testCount && i < len(analysis.Services); i++ {
		service := analysis.Services[i]
		
		t.Run(fmt.Sprintf("PercentageTest_%s", service.Name), func(t *testing.T) {
			scanCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()

			result, err := pipeline.ScanService(scanCtx, service.Name, cfg, "us-east-1")
			
			if err != nil {
				t.Logf("Service %s failed: %v", service.Name, err)
				return
			}

			assert.Equal(t, service.Name, result.Service)
			assert.NotZero(t, result.Duration)
			successCount++
			
			t.Logf("Service %s: SUCCESS (Duration: %v)", service.Name, result.Duration)
		})
	}

	successRate := float64(successCount) / float64(testCount) * 100
	t.Logf("Percentage Test Results: %d/%d services passed (%.1f%%)", 
		successCount, testCount, successRate)

	// At least 70% of tested services should work
	assert.GreaterOrEqual(t, successRate, 70.0, "At least 70%% of tested services should work")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
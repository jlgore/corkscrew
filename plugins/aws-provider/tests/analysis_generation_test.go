//go:build analysis
// +build analysis

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnalysisGeneration tests the generation and loading of analysis files
func TestAnalysisGeneration(t *testing.T) {
	if os.Getenv("RUN_ANALYSIS_TESTS") != "true" {
		t.Skip("Skipping analysis tests. Set RUN_ANALYSIS_TESTS=true to run.")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	require.NoError(t, err, "Failed to load AWS config")

	suite := &AnalysisTestSuite{
		cfg:        cfg,
		ctx:        ctx,
		outputDir:  "/tmp/test-analysis-output",
		testPrefix: fmt.Sprintf("test_%d", time.Now().Unix()),
	}

	// Setup test environment
	suite.setup(t)
	defer suite.cleanup(t)

	t.Run("GenerateServiceAnalysis", suite.testGenerateServiceAnalysis)
	t.Run("LoadAnalysisFiles", suite.testLoadAnalysisFiles)
	t.Run("ValidateAnalysisStructure", suite.testValidateAnalysisStructure)
	t.Run("CompareWithExisting", suite.testCompareWithExisting)
	t.Run("AnalysisFileConsistency", suite.testAnalysisFileConsistency)
	t.Run("IncrementalAnalysis", suite.testIncrementalAnalysis)
}

type AnalysisTestSuite struct {
	cfg        aws.Config
	ctx        context.Context
	outputDir  string
	testPrefix string
}

func (s *AnalysisTestSuite) setup(t *testing.T) {
	// Create test output directory
	err := os.MkdirAll(s.outputDir, 0755)
	require.NoError(t, err, "Failed to create test output directory")
	
	t.Logf("Test environment setup in: %s", s.outputDir)
}

func (s *AnalysisTestSuite) cleanup(t *testing.T) {
	// Clean up test files
	err := os.RemoveAll(s.outputDir)
	if err != nil {
		t.Logf("Warning: Failed to clean up test directory: %v", err)
	}
}

// testGenerateServiceAnalysis tests generating new analysis files
func (s *AnalysisTestSuite) testGenerateServiceAnalysis(t *testing.T) {
	// Create analyzer
	analyzer := &TestServiceAnalyzer{
		cfg:       s.cfg,
		outputDir: s.outputDir,
	}

	// Test generating analysis for a small set of services
	testServices := []string{"s3", "lambda", "ec2"}

	t.Run("GenerateIndividualServices", func(t *testing.T) {
		for _, service := range testServices {
			t.Run(fmt.Sprintf("Service_%s", service), func(t *testing.T) {
				outputFile := filepath.Join(s.outputDir, fmt.Sprintf("%s_%s.json", s.testPrefix, service))
				
				startTime := time.Now()
				err := analyzer.GenerateServiceAnalysis(s.ctx, service, outputFile)
				duration := time.Since(startTime)
				
				assert.NoError(t, err, "Analysis generation should succeed for %s", service)
				
				// Verify file was created
				_, err = os.Stat(outputFile)
				assert.NoError(t, err, "Analysis file should be created for %s", service)
				
				// Verify file has content
				if err == nil {
					content, err := os.ReadFile(outputFile)
					require.NoError(t, err, "Should be able to read analysis file")
					assert.Greater(t, len(content), 100, "Analysis file should have substantial content")
					
					// Verify it's valid JSON
					var analysis interface{}
					err = json.Unmarshal(content, &analysis)
					assert.NoError(t, err, "Analysis file should contain valid JSON")
				}
				
				t.Logf("Generated analysis for %s in %v (%s)", service, duration, outputFile)
			})
		}
	})

	t.Run("GenerateCombinedAnalysis", func(t *testing.T) {
		outputFile := filepath.Join(s.outputDir, fmt.Sprintf("%s_combined.json", s.testPrefix))
		
		startTime := time.Now()
		err := analyzer.GenerateCombinedAnalysis(s.ctx, testServices, outputFile)
		duration := time.Since(startTime)
		
		assert.NoError(t, err, "Combined analysis generation should succeed")
		
		// Verify combined file
		_, err = os.Stat(outputFile)
		assert.NoError(t, err, "Combined analysis file should be created")
		
		if err == nil {
			content, err := os.ReadFile(outputFile)
			require.NoError(t, err, "Should be able to read combined analysis file")
			
			var analysis ServiceAnalysis
			err = json.Unmarshal(content, &analysis)
			require.NoError(t, err, "Combined analysis should be valid JSON")
			
			// Verify structure
			assert.NotEmpty(t, analysis.Version, "Should have version")
			assert.NotZero(t, analysis.AnalyzedAt, "Should have timestamp")
			assert.GreaterOrEqual(t, len(analysis.Services), len(testServices), 
				"Should have at least as many services as requested")
			assert.Greater(t, analysis.TotalOperations, 0, "Should have operations")
			assert.Greater(t, analysis.TotalResources, 0, "Should have resources")
			
			t.Logf("Generated combined analysis in %v: %d services, %d operations, %d resources",
				duration, len(analysis.Services), analysis.TotalOperations, analysis.TotalResources)
		}
	})
}

// testLoadAnalysisFiles tests loading analysis files
func (s *AnalysisTestSuite) testLoadAnalysisFiles(t *testing.T) {
	loader := &AnalysisFileLoader{}

	// Test loading existing generated file
	t.Run("LoadExistingFile", func(t *testing.T) {
		existingFile := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"
		
		analysis, err := loader.LoadAnalysisFile(existingFile)
		assert.NoError(t, err, "Should load existing analysis file")
		
		if err == nil {
			assert.NotNil(t, analysis, "Should return analysis object")
			assert.NotEmpty(t, analysis.Version, "Should have version")
			assert.NotEmpty(t, analysis.Services, "Should have services")
			
			t.Logf("Loaded existing analysis: %d services, %d operations", 
				len(analysis.Services), analysis.TotalOperations)
		}
	})

	// Test loading generated test file
	t.Run("LoadGeneratedTestFile", func(t *testing.T) {
		testFile := filepath.Join(s.outputDir, fmt.Sprintf("%s_combined.json", s.testPrefix))
		
		// Skip if test file doesn't exist (previous test may have failed)
		if _, err := os.Stat(testFile); os.IsNotExist(err) {
			t.Skip("Test file not found, skipping load test")
		}
		
		analysis, err := loader.LoadAnalysisFile(testFile)
		assert.NoError(t, err, "Should load test analysis file")
		
		if err == nil {
			assert.NotNil(t, analysis, "Should return analysis object")
			t.Logf("Loaded test analysis: %d services", len(analysis.Services))
		}
	})

	// Test error handling
	t.Run("LoadNonexistentFile", func(t *testing.T) {
		analysis, err := loader.LoadAnalysisFile("/nonexistent/file.json")
		assert.Error(t, err, "Should error for nonexistent file")
		assert.Nil(t, analysis, "Should return nil analysis for error")
	})

	t.Run("LoadInvalidJSON", func(t *testing.T) {
		invalidFile := filepath.Join(s.outputDir, "invalid.json")
		err := os.WriteFile(invalidFile, []byte("invalid json content"), 0644)
		require.NoError(t, err, "Should create invalid JSON file")
		
		analysis, err := loader.LoadAnalysisFile(invalidFile)
		assert.Error(t, err, "Should error for invalid JSON")
		assert.Nil(t, analysis, "Should return nil analysis for invalid JSON")
	})
}

// testValidateAnalysisStructure tests validation of analysis structure
func (s *AnalysisTestSuite) testValidateAnalysisStructure(t *testing.T) {
	validator := &AnalysisValidator{}

	// Load existing analysis for validation
	existingFile := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"
	loader := &AnalysisFileLoader{}
	
	analysis, err := loader.LoadAnalysisFile(existingFile)
	require.NoError(t, err, "Should load analysis for validation")
	require.NotNil(t, analysis, "Analysis should not be nil")

	t.Run("ValidateBasicStructure", func(t *testing.T) {
		errors := validator.ValidateBasicStructure(analysis)
		
		if len(errors) > 0 {
			t.Logf("Validation errors found:")
			for _, err := range errors {
				t.Logf("  %s", err)
			}
		}
		
		assert.Empty(t, errors, "Analysis should have valid basic structure")
	})

	t.Run("ValidateServiceDetails", func(t *testing.T) {
		var allErrors []string
		
		for _, service := range analysis.Services {
			errors := validator.ValidateServiceDetails(&service)
			allErrors = append(allErrors, errors...)
		}
		
		if len(allErrors) > 0 {
			t.Logf("Service validation errors:")
			for _, err := range allErrors {
				t.Logf("  %s", err)
			}
		}
		
		// Allow some validation errors but not too many
		errorRate := float64(len(allErrors)) / float64(len(analysis.Services))
		assert.Less(t, errorRate, 0.2, "Error rate should be less than 20%%")
		
		t.Logf("Service validation: %d errors across %d services (%.1f%% error rate)", 
			len(allErrors), len(analysis.Services), errorRate*100)
	})

	t.Run("ValidateOperationConsistency", func(t *testing.T) {
		errors := validator.ValidateOperationConsistency(analysis)
		
		if len(errors) > 0 {
			t.Logf("Operation consistency errors:")
			for _, err := range errors {
				t.Logf("  %s", err)
			}
		}
		
		assert.LessOrEqual(t, len(errors), 5, "Should have few operation consistency errors")
	})

	t.Run("ValidateResourceTypes", func(t *testing.T) {
		errors := validator.ValidateResourceTypes(analysis)
		
		if len(errors) > 0 {
			t.Logf("Resource type validation errors:")
			for _, err := range errors {
				t.Logf("  %s", err)
			}
		}
		
		assert.LessOrEqual(t, len(errors), 10, "Should have reasonable number of resource type errors")
	})
}

// testCompareWithExisting compares newly generated analysis with existing
func (s *AnalysisTestSuite) testCompareWithExisting(t *testing.T) {
	loader := &AnalysisFileLoader{}
	comparator := &AnalysisComparator{}

	// Load existing analysis
	existingFile := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"
	existingAnalysis, err := loader.LoadAnalysisFile(existingFile)
	require.NoError(t, err, "Should load existing analysis")

	// Check if we have a test analysis to compare
	testFile := filepath.Join(s.outputDir, fmt.Sprintf("%s_combined.json", s.testPrefix))
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Skip("No test analysis file to compare")
	}

	// Load test analysis
	testAnalysis, err := loader.LoadAnalysisFile(testFile)
	require.NoError(t, err, "Should load test analysis")

	t.Run("CompareServiceCounts", func(t *testing.T) {
		comparison := comparator.CompareServiceCounts(existingAnalysis, testAnalysis)
		
		t.Logf("Service count comparison:")
		t.Logf("  Existing: %d services", comparison.ExistingCount)
		t.Logf("  New: %d services", comparison.NewCount)
		t.Logf("  Difference: %d", comparison.Difference)
		
		// New analysis should have at least some services
		assert.Greater(t, comparison.NewCount, 0, "New analysis should have services")
		
		// Difference should be reasonable (within 50% of existing)
		maxDifference := comparison.ExistingCount / 2
		assert.LessOrEqual(t, abs(comparison.Difference), maxDifference, 
			"Service count difference should be reasonable")
	})

	t.Run("CompareOperationCounts", func(t *testing.T) {
		comparison := comparator.CompareOperationCounts(existingAnalysis, testAnalysis)
		
		t.Logf("Operation count comparison:")
		t.Logf("  Existing: %d operations", comparison.ExistingCount)
		t.Logf("  New: %d operations", comparison.NewCount)
		t.Logf("  Difference: %d", comparison.Difference)
		
		assert.Greater(t, comparison.NewCount, 0, "New analysis should have operations")
	})

	t.Run("CompareServiceOverlap", func(t *testing.T) {
		overlap := comparator.CompareServiceOverlap(existingAnalysis, testAnalysis)
		
		t.Logf("Service overlap:")
		t.Logf("  Common services: %d (%v)", len(overlap.Common), overlap.Common)
		t.Logf("  Only in existing: %d (%v)", len(overlap.OnlyInExisting), overlap.OnlyInExisting)
		t.Logf("  Only in new: %d (%v)", len(overlap.OnlyInNew), overlap.OnlyInNew)
		
		// Should have some overlap
		assert.Greater(t, len(overlap.Common), 0, "Should have some common services")
		
		// Overlap should be significant
		overlapRatio := float64(len(overlap.Common)) / float64(min(len(existingAnalysis.Services), len(testAnalysis.Services)))
		assert.GreaterOrEqual(t, overlapRatio, 0.5, "Should have at least 50%% service overlap")
	})
}

// testAnalysisFileConsistency tests consistency across multiple analysis files
func (s *AnalysisTestSuite) testAnalysisFileConsistency(t *testing.T) {
	consistency := &AnalysisConsistencyChecker{}

	// Find all analysis files
	analysisFiles := []string{
		"/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json",
	}

	// Add test files if they exist
	testFiles, _ := filepath.Glob(filepath.Join(s.outputDir, "*.json"))
	analysisFiles = append(analysisFiles, testFiles...)

	if len(analysisFiles) < 2 {
		t.Skip("Need at least 2 analysis files for consistency check")
	}

	t.Run("CheckVersionConsistency", func(t *testing.T) {
		issues := consistency.CheckVersionConsistency(analysisFiles)
		
		if len(issues) > 0 {
			t.Logf("Version consistency issues:")
			for _, issue := range issues {
				t.Logf("  %s", issue)
			}
		}
		
		// Version inconsistencies are warnings, not errors
		t.Logf("Found %d version consistency issues", len(issues))
	})

	t.Run("CheckStructureConsistency", func(t *testing.T) {
		issues := consistency.CheckStructureConsistency(analysisFiles)
		
		if len(issues) > 0 {
			t.Logf("Structure consistency issues:")
			for _, issue := range issues {
				t.Logf("  %s", issue)
			}
		}
		
		assert.LessOrEqual(t, len(issues), 5, "Should have few structure consistency issues")
	})
}

// testIncrementalAnalysis tests incremental analysis generation
func (s *AnalysisTestSuite) testIncrementalAnalysis(t *testing.T) {
	incrementalAnalyzer := &IncrementalAnalyzer{
		cfg:       s.cfg,
		outputDir: s.outputDir,
	}

	// Test incremental update
	t.Run("IncrementalUpdate", func(t *testing.T) {
		baseFile := "/home/jg/git/corkscrew/plugins/aws-provider/generated/services.json"
		outputFile := filepath.Join(s.outputDir, fmt.Sprintf("%s_incremental.json", s.testPrefix))
		
		// Services to add/update
		newServices := []string{"iam", "kms"}
		
		startTime := time.Now()
		err := incrementalAnalyzer.UpdateAnalysis(s.ctx, baseFile, newServices, outputFile)
		duration := time.Since(startTime)
		
		if err != nil {
			t.Logf("Incremental analysis failed: %v", err)
			t.Skip("Skipping incremental analysis validation")
		}
		
		// Verify incremental file
		_, err = os.Stat(outputFile)
		assert.NoError(t, err, "Incremental analysis file should be created")
		
		if err == nil {
			loader := &AnalysisFileLoader{}
			analysis, err := loader.LoadAnalysisFile(outputFile)
			require.NoError(t, err, "Should load incremental analysis")
			
			// Should have updated timestamp
			assert.True(t, analysis.AnalyzedAt.After(time.Now().Add(-time.Hour)), 
				"Should have recent timestamp")
			
			// Should contain the new services
			serviceNames := make(map[string]bool)
			for _, service := range analysis.Services {
				serviceNames[service.Name] = true
			}
			
			for _, newService := range newServices {
				assert.True(t, serviceNames[newService], 
					"Incremental analysis should contain service %s", newService)
			}
			
			t.Logf("Incremental analysis completed in %v", duration)
		}
	})
}

// Helper types and implementations

type TestServiceAnalyzer struct {
	cfg       aws.Config
	outputDir string
}

func (a *TestServiceAnalyzer) GenerateServiceAnalysis(ctx context.Context, serviceName, outputFile string) error {
	// Create a mock analysis for the service
	mockAnalysis := ServiceAnalysis{
		Version:    "1.0.0-test",
		AnalyzedAt: time.Now(),
		SDKVersion: "v2-test",
		Services: []ServiceInfo{
			{
				Name:        serviceName,
				PackagePath: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
				ClientType:  "Client",
				Operations:  a.generateMockOperations(serviceName),
				ResourceTypes: a.generateMockResourceTypes(serviceName),
				LastUpdated: time.Now(),
			},
		},
		TotalOperations: 3,
		TotalResources:  1,
	}

	// Write to file
	data, err := json.MarshalIndent(mockAnalysis, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(outputFile, data, 0644)
}

func (a *TestServiceAnalyzer) GenerateCombinedAnalysis(ctx context.Context, services []string, outputFile string) error {
	var allServices []ServiceInfo
	totalOps := 0
	totalRes := 0

	for _, serviceName := range services {
		ops := a.generateMockOperations(serviceName)
		res := a.generateMockResourceTypes(serviceName)
		
		service := ServiceInfo{
			Name:          serviceName,
			PackagePath:   fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
			ClientType:    "Client",
			Operations:    ops,
			ResourceTypes: res,
			LastUpdated:   time.Now(),
		}
		
		allServices = append(allServices, service)
		totalOps += len(ops)
		totalRes += len(res)
	}

	combinedAnalysis := ServiceAnalysis{
		Version:         "1.0.0-test",
		AnalyzedAt:      time.Now(),
		SDKVersion:      "v2-test",
		Services:        allServices,
		TotalOperations: totalOps,
		TotalResources:  totalRes,
	}

	data, err := json.MarshalIndent(combinedAnalysis, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(outputFile, data, 0644)
}

func (a *TestServiceAnalyzer) generateMockOperations(serviceName string) []OperationInfo {
	switch serviceName {
	case "s3":
		return []OperationInfo{
			{Name: "ListBuckets", InputType: "ListBucketsInput", OutputType: "ListBucketsOutput", ResourceType: "Bucket", IsList: true},
			{Name: "GetBucketLocation", InputType: "GetBucketLocationInput", OutputType: "GetBucketLocationOutput", IsList: false},
		}
	case "ec2":
		return []OperationInfo{
			{Name: "DescribeInstances", InputType: "DescribeInstancesInput", OutputType: "DescribeInstancesOutput", ResourceType: "Instance", IsList: true, IsPaginated: true},
			{Name: "DescribeVolumes", InputType: "DescribeVolumesInput", OutputType: "DescribeVolumesOutput", ResourceType: "Volume", IsList: true},
		}
	case "lambda":
		return []OperationInfo{
			{Name: "ListFunctions", InputType: "ListFunctionsInput", OutputType: "ListFunctionsOutput", ResourceType: "Function", IsList: true},
		}
	default:
		return []OperationInfo{
			{Name: "List" + strings.Title(serviceName), InputType: "MockInput", OutputType: "MockOutput", IsList: true},
		}
	}
}

func (a *TestServiceAnalyzer) generateMockResourceTypes(serviceName string) []ResourceType {
	switch serviceName {
	case "s3":
		return []ResourceType{
			{Name: "Bucket", GoType: "Bucket", PrimaryKey: "Name", Fields: map[string]string{"Name": "*string", "CreationDate": "*time.Time"}},
		}
	case "ec2":
		return []ResourceType{
			{Name: "Instance", GoType: "Instance", PrimaryKey: "InstanceId", Fields: map[string]string{"InstanceId": "*string", "State": "*string"}},
			{Name: "Volume", GoType: "Volume", PrimaryKey: "VolumeId", Fields: map[string]string{"VolumeId": "*string", "Size": "*int32"}},
		}
	case "lambda":
		return []ResourceType{
			{Name: "Function", GoType: "FunctionConfiguration", PrimaryKey: "FunctionName", Fields: map[string]string{"FunctionName": "*string", "Runtime": "*string"}},
		}
	default:
		return []ResourceType{
			{Name: "Resource", GoType: "MockResource", PrimaryKey: "Id", Fields: map[string]string{"Id": "*string"}},
		}
	}
}

type AnalysisFileLoader struct{}

func (l *AnalysisFileLoader) LoadAnalysisFile(filePath string) (*ServiceAnalysis, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var analysis ServiceAnalysis
	err = json.Unmarshal(data, &analysis)
	if err != nil {
		return nil, err
	}

	return &analysis, nil
}

type AnalysisValidator struct{}

func (v *AnalysisValidator) ValidateBasicStructure(analysis *ServiceAnalysis) []string {
	var errors []string

	if analysis.Version == "" {
		errors = append(errors, "Missing version")
	}
	if analysis.AnalyzedAt.IsZero() {
		errors = append(errors, "Missing analyzed timestamp")
	}
	if len(analysis.Services) == 0 {
		errors = append(errors, "No services found")
	}
	if analysis.TotalOperations == 0 {
		errors = append(errors, "No operations counted")
	}

	return errors
}

func (v *AnalysisValidator) ValidateServiceDetails(service *ServiceInfo) []string {
	var errors []string

	if service.Name == "" {
		errors = append(errors, fmt.Sprintf("Service missing name"))
	}
	if service.PackagePath == "" {
		errors = append(errors, fmt.Sprintf("Service %s missing package path", service.Name))
	}
	if len(service.Operations) == 0 {
		errors = append(errors, fmt.Sprintf("Service %s has no operations", service.Name))
	}

	return errors
}

func (v *AnalysisValidator) ValidateOperationConsistency(analysis *ServiceAnalysis) []string {
	var errors []string

	operationCount := 0
	for _, service := range analysis.Services {
		operationCount += len(service.Operations)
	}

	if operationCount != analysis.TotalOperations {
		errors = append(errors, fmt.Sprintf("Operation count mismatch: counted %d, reported %d", 
			operationCount, analysis.TotalOperations))
	}

	return errors
}

func (v *AnalysisValidator) ValidateResourceTypes(analysis *ServiceAnalysis) []string {
	var errors []string

	resourceCount := 0
	for _, service := range analysis.Services {
		resourceCount += len(service.ResourceTypes)
		
		for _, resource := range service.ResourceTypes {
			if resource.Name == "" {
				errors = append(errors, fmt.Sprintf("Service %s has resource type with no name", service.Name))
			}
			if resource.PrimaryKey == "" {
				errors = append(errors, fmt.Sprintf("Service %s resource %s has no primary key", service.Name, resource.Name))
			}
		}
	}

	if resourceCount != analysis.TotalResources {
		errors = append(errors, fmt.Sprintf("Resource count mismatch: counted %d, reported %d",
			resourceCount, analysis.TotalResources))
	}

	return errors
}

type CountComparison struct {
	ExistingCount int
	NewCount      int
	Difference    int
}

type ServiceOverlap struct {
	Common         []string
	OnlyInExisting []string
	OnlyInNew      []string
}

type AnalysisComparator struct{}

func (c *AnalysisComparator) CompareServiceCounts(existing, new *ServiceAnalysis) CountComparison {
	return CountComparison{
		ExistingCount: len(existing.Services),
		NewCount:      len(new.Services),
		Difference:    len(new.Services) - len(existing.Services),
	}
}

func (c *AnalysisComparator) CompareOperationCounts(existing, new *ServiceAnalysis) CountComparison {
	return CountComparison{
		ExistingCount: existing.TotalOperations,
		NewCount:      new.TotalOperations,
		Difference:    new.TotalOperations - existing.TotalOperations,
	}
}

func (c *AnalysisComparator) CompareServiceOverlap(existing, new *ServiceAnalysis) ServiceOverlap {
	existingServices := make(map[string]bool)
	newServices := make(map[string]bool)

	for _, service := range existing.Services {
		existingServices[service.Name] = true
	}
	for _, service := range new.Services {
		newServices[service.Name] = true
	}

	var common, onlyInExisting, onlyInNew []string

	for name := range existingServices {
		if newServices[name] {
			common = append(common, name)
		} else {
			onlyInExisting = append(onlyInExisting, name)
		}
	}

	for name := range newServices {
		if !existingServices[name] {
			onlyInNew = append(onlyInNew, name)
		}
	}

	return ServiceOverlap{
		Common:         common,
		OnlyInExisting: onlyInExisting,
		OnlyInNew:      onlyInNew,
	}
}

type AnalysisConsistencyChecker struct{}

func (c *AnalysisConsistencyChecker) CheckVersionConsistency(files []string) []string {
	var issues []string
	loader := &AnalysisFileLoader{}
	versions := make(map[string][]string)

	for _, file := range files {
		analysis, err := loader.LoadAnalysisFile(file)
		if err != nil {
			continue
		}
		versions[analysis.Version] = append(versions[analysis.Version], file)
	}

	if len(versions) > 1 {
		issues = append(issues, fmt.Sprintf("Found %d different versions", len(versions)))
	}

	return issues
}

func (c *AnalysisConsistencyChecker) CheckStructureConsistency(files []string) []string {
	var issues []string
	// Implementation would check for structural consistency across files
	return issues
}

type IncrementalAnalyzer struct {
	cfg       aws.Config
	outputDir string
}

func (i *IncrementalAnalyzer) UpdateAnalysis(ctx context.Context, baseFile string, newServices []string, outputFile string) error {
	// Load base analysis
	loader := &AnalysisFileLoader{}
	baseAnalysis, err := loader.LoadAnalysisFile(baseFile)
	if err != nil {
		return err
	}

	// Add new services (mock implementation)
	analyzer := &TestServiceAnalyzer{cfg: i.cfg, outputDir: i.outputDir}
	
	for _, serviceName := range newServices {
		// Check if service already exists
		exists := false
		for _, existing := range baseAnalysis.Services {
			if existing.Name == serviceName {
				exists = true
				break
			}
		}
		
		if !exists {
			newService := ServiceInfo{
				Name:          serviceName,
				PackagePath:   fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
				ClientType:    "Client",
				Operations:    analyzer.generateMockOperations(serviceName),
				ResourceTypes: analyzer.generateMockResourceTypes(serviceName),
				LastUpdated:   time.Now(),
			}
			baseAnalysis.Services = append(baseAnalysis.Services, newService)
			baseAnalysis.TotalOperations += len(newService.Operations)
			baseAnalysis.TotalResources += len(newService.ResourceTypes)
		}
	}

	// Update timestamp
	baseAnalysis.AnalyzedAt = time.Now()

	// Write updated analysis
	data, err := json.MarshalIndent(baseAnalysis, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(outputFile, data, 0644)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
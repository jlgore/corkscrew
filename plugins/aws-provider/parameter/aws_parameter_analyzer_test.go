package parameter

import (
	"reflect"
	"testing"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// Mock AWS SDK input struct for testing
type MockS3ListObjectsInput struct {
	Bucket    *string `location:"uri" locationName:"Bucket" validate:"required"`
	MaxKeys   *int32  `location:"querystring" locationName:"max-keys"`
	Prefix    *string `location:"querystring" locationName:"prefix"`
	Delimiter *string `location:"querystring" locationName:"delimiter"`
}

// Mock AWS SDK output struct for testing
type MockS3ListObjectsOutput struct {
	Contents []MockS3Object `locationName:"Contents"`
}

type MockS3Object struct {
	Key  *string `locationName:"Key"`
	Size *int64  `locationName:"Size"`
}

// Mock client type for testing
type MockS3Client struct{}

func (c *MockS3Client) ListObjects(input *MockS3ListObjectsInput) (*MockS3ListObjectsOutput, error) {
	return nil, nil
}

func (c *MockS3Client) GetObject(input *MockS3GetObjectInput) (*MockS3GetObjectOutput, error) {
	return nil, nil
}

type MockS3GetObjectInput struct {
	Bucket *string `location:"uri" locationName:"Bucket" validate:"required"`
	Key    *string `location:"uri" locationName:"Key" validate:"required"`
}

type MockS3GetObjectOutput struct {
	Body []byte `locationName:"Body"`
}

func TestNewAWSParameterAnalyzer(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	if analyzer == nil {
		t.Fatal("Expected analyzer to be created, got nil")
	}

	if analyzer.servicePatterns == nil {
		t.Error("Expected servicePatterns to be initialized")
	}

	if analyzer.typeInferenceCache == nil {
		t.Error("Expected typeInferenceCache to be initialized")
	}

	// Check that built-in service patterns are loaded
	if _, exists := analyzer.servicePatterns["s3"]; !exists {
		t.Error("Expected S3 service pattern to be initialized")
	}

	if _, exists := analyzer.servicePatterns["ec2"]; !exists {
		t.Error("Expected EC2 service pattern to be initialized")
	}

	if _, exists := analyzer.servicePatterns["iam"]; !exists {
		t.Error("Expected IAM service pattern to be initialized")
	}
}

func TestAnalyzeAWSOperation(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	// Create a mock operation
	operation := generator.AWSOperation{
		Name:   "ListObjects",
		Method: "ListObjects",
	}

	// Get the client type for reflection
	clientType := reflect.TypeOf(&MockS3Client{})

	// Analyze the operation
	analysis := analyzer.AnalyzeAWSOperation("s3", operation, clientType)

	// Verify basic analysis structure
	if analysis.OperationName != "ListObjects" {
		t.Errorf("Expected operation name 'ListObjects', got '%s'", analysis.OperationName)
	}

	if analysis.Confidence <= 0.0 {
		t.Errorf("Expected confidence > 0.0, got %f", analysis.Confidence)
	}

	// Check that parameters were detected
	totalParams := len(analysis.RequiredParams) + len(analysis.OptionalParams)
	if totalParams == 0 {
		t.Error("Expected some parameters to be detected")
	}

	// Verify caching works
	analysis2 := analyzer.AnalyzeAWSOperation("s3", operation, clientType)
	if analysis2.OperationName != analysis.OperationName {
		t.Error("Expected cached result to match original analysis")
	}
}

func TestAnalyzeAWSStructField(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	// Test with a required field
	bucketField := reflect.StructField{
		Name: "Bucket",
		Type: reflect.TypeOf((*string)(nil)),
		Tag:  `location:"uri" locationName:"Bucket" validate:"required"`,
	}

	paramInfo := analyzer.analyzeAWSStructField(bucketField)

	if paramInfo.Name != "Bucket" {
		t.Errorf("Expected name 'Bucket', got '%s'", paramInfo.Name)
	}

	if !paramInfo.IsPointer {
		t.Error("Expected IsPointer to be true for *string")
	}

	if paramInfo.IsRequired {
		t.Error("Expected IsRequired to be false for pointer field (AWS SDK pattern)")
	}

	if paramInfo.Description == "" {
		t.Error("Expected description to be generated")
	}

	if len(paramInfo.Examples) == 0 {
		t.Error("Expected examples to be generated")
	}
}

func TestIsAWSFieldRequired(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	tests := []struct {
		name     string
		field    reflect.StructField
		expected bool
	}{
		{
			name: "pointer field should be optional",
			field: reflect.StructField{
				Name: "OptionalField",
				Type: reflect.TypeOf((*string)(nil)),
			},
			expected: false,
		},
		{
			name: "non-pointer field should be required",
			field: reflect.StructField{
				Name: "RequiredField",
				Type: reflect.TypeOf(""),
			},
			expected: true,
		},
		{
			name: "bucket field should be required",
			field: reflect.StructField{
				Name: "BucketName",
				Type: reflect.TypeOf(""),
			},
			expected: true,
		},
		{
			name: "key field should be required",
			field: reflect.StructField{
				Name: "ObjectKey",
				Type: reflect.TypeOf(""),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.isAWSFieldRequired(tt.field)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGenerateAWSFieldExamples(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	tests := []struct {
		name          string
		field         reflect.StructField
		expectedCount int
	}{
		{
			name: "bucket field",
			field: reflect.StructField{
				Name: "Bucket",
				Type: reflect.TypeOf(""),
			},
			expectedCount: 2, // Should generate bucket examples
		},
		{
			name: "key field",
			field: reflect.StructField{
				Name: "Key",
				Type: reflect.TypeOf(""),
			},
			expectedCount: 2, // Should generate key examples
		},
		{
			name: "region field",
			field: reflect.StructField{
				Name: "Region",
				Type: reflect.TypeOf(""),
			},
			expectedCount: 3, // Should generate region examples
		},
		{
			name: "max results field",
			field: reflect.StructField{
				Name: "MaxResults",
				Type: reflect.TypeOf(int32(0)),
			},
			expectedCount: 3, // Should generate number examples
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			examples := analyzer.generateAWSFieldExamples(tt.field)
			if len(examples) != tt.expectedCount {
				t.Errorf("Expected %d examples, got %d", tt.expectedCount, len(examples))
			}
		})
	}
}

func TestDetectAWSParameterDependencies(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	// Create analysis with parameters that should have dependencies
	analysis := &AWSParameterAnalysis{
		OperationName: "GetUser",
		RequiredParams: []AWSParameterInfo{
			{
				Name:     "UserName",
				TypeName: "string",
			},
		},
	}

	analyzer.detectAWSParameterDependencies("iam", "GetUser", analysis)

	// Should detect dependency on ListUsers
	if len(analysis.Dependencies) == 0 {
		t.Error("Expected dependencies to be detected")
	}

	found := false
	for _, dep := range analysis.Dependencies {
		if dep.SourceOperation == "ListUsers" && dep.TargetParam == "UserName" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected dependency on ListUsers for UserName parameter")
	}
}

func TestGenerateAWSValidationRules(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	analysis := &AWSParameterAnalysis{
		RequiredParams: []AWSParameterInfo{
			{
				Name:     "BucketName",
				TypeName: "string",
				Constraints: AWSParameterConstraint{
					MinLength: intPtr(3),
					MaxLength: intPtr(63),
				},
			},
		},
		OptionalParams: []AWSParameterInfo{
			{
				Name:     "MaxKeys",
				TypeName: "int32",
				Constraints: AWSParameterConstraint{
					MinValue: int64Ptr(1),
					MaxValue: int64Ptr(1000),
				},
			},
		},
	}

	analyzer.generateAWSValidationRules(analysis)

	if len(analysis.ValidationRules) == 0 {
		t.Error("Expected validation rules to be generated")
	}

	// Check for required field validation
	foundRequired := false
	foundMinLength := false
	foundMaxLength := false

	for _, rule := range analysis.ValidationRules {
		switch rule.RuleType {
		case "required":
			if rule.Field == "BucketName" {
				foundRequired = true
			}
		case "min_length":
			if rule.Field == "BucketName" {
				foundMinLength = true
			}
		case "max_length":
			if rule.Field == "BucketName" {
				foundMaxLength = true
			}
		}
	}

	if !foundRequired {
		t.Error("Expected required validation rule for BucketName")
	}
	if !foundMinLength {
		t.Error("Expected min_length validation rule for BucketName")
	}
	if !foundMaxLength {
		t.Error("Expected max_length validation rule for BucketName")
	}
}

func TestDetermineAWSExecutionStrategy(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	tests := []struct {
		name               string
		analysis           *AWSParameterAnalysis
		expectedCanExecute bool
		expectedStrategy   string
	}{
		{
			name: "no required params - can auto execute",
			analysis: &AWSParameterAnalysis{
				RequiredParams: []AWSParameterInfo{},
			},
			expectedCanExecute: true,
			expectedStrategy:   "static",
		},
		{
			name: "required params with dependencies - can auto execute",
			analysis: &AWSParameterAnalysis{
				RequiredParams: []AWSParameterInfo{
					{Name: "UserName"},
				},
				Dependencies: []AWSParameterDependency{
					{TargetParam: "UserName", SourceOperation: "ListUsers"},
				},
			},
			expectedCanExecute: true,
			expectedStrategy:   "dependency",
		},
		{
			name: "required params without dependencies - needs user input",
			analysis: &AWSParameterAnalysis{
				RequiredParams: []AWSParameterInfo{
					{Name: "CustomParam"},
				},
				Dependencies: []AWSParameterDependency{},
			},
			expectedCanExecute: false,
			expectedStrategy:   "user_input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzer.determineAWSExecutionStrategy(tt.analysis)

			if tt.analysis.CanAutoExecute != tt.expectedCanExecute {
				t.Errorf("Expected CanAutoExecute %v, got %v", tt.expectedCanExecute, tt.analysis.CanAutoExecute)
			}

			if tt.analysis.DefaultStrategy.StrategyType != tt.expectedStrategy {
				t.Errorf("Expected strategy %s, got %s", tt.expectedStrategy, tt.analysis.DefaultStrategy.StrategyType)
			}
		})
	}
}

func TestCalculateAWSConfidenceScore(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	tests := []struct {
		name     string
		analysis *AWSParameterAnalysis
		minScore float64
		maxScore float64
	}{
		{
			name: "minimal analysis",
			analysis: &AWSParameterAnalysis{
				RequiredParams:  []AWSParameterInfo{},
				Dependencies:    []AWSParameterDependency{},
				ValidationRules: []AWSValidationRule{},
				CanAutoExecute:  false,
			},
			minScore: 0.0,
			maxScore: 0.2,
		},
		{
			name: "comprehensive analysis",
			analysis: &AWSParameterAnalysis{
				RequiredParams: []AWSParameterInfo{
					{Name: "Param1"},
				},
				Dependencies: []AWSParameterDependency{
					{SourceOperation: "ListOp"},
				},
				ValidationRules: []AWSValidationRule{
					{RuleType: "required"},
				},
				CanAutoExecute: true,
			},
			minScore: 0.8,
			maxScore: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := analyzer.calculateAWSConfidenceScore(tt.analysis)

			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("Expected score between %f and %f, got %f", tt.minScore, tt.maxScore, score)
			}
		})
	}
}

func TestValidateAWSParameters(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	analysis := AWSParameterAnalysis{
		RequiredParams: []AWSParameterInfo{
			{Name: "BucketName"},
		},
		ValidationRules: []AWSValidationRule{
			{
				RuleType:  "required",
				Field:     "BucketName",
				Condition: "not_empty",
				ErrorMsg:  "BucketName is required",
			},
			{
				RuleType:  "min_length",
				Field:     "BucketName",
				Condition: ">=",
				Value:     3,
				ErrorMsg:  "BucketName must be at least 3 characters",
			},
		},
	}

	tests := []struct {
		name           string
		params         map[string]interface{}
		expectedErrors int
	}{
		{
			name:           "missing required param",
			params:         map[string]interface{}{},
			expectedErrors: 1,
		},
		{
			name: "valid params",
			params: map[string]interface{}{
				"BucketName": "my-bucket",
			},
			expectedErrors: 0,
		},
		{
			name: "invalid length",
			params: map[string]interface{}{
				"BucketName": "ab", // Too short
			},
			expectedErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := analyzer.ValidateAWSParameters(analysis, tt.params)

			if len(errors) != tt.expectedErrors {
				t.Errorf("Expected %d errors, got %d: %v", tt.expectedErrors, len(errors), errors)
			}
		})
	}
}

func TestExtractAWSFieldFromResults(t *testing.T) {
	analyzer := NewAWSParameterAnalyzer()

	// Mock results structure
	type MockUser struct {
		UserName *string
		UserId   *string
	}

	results := []MockUser{
		{UserName: stringPtr("user1"), UserId: stringPtr("id1")},
		{UserName: stringPtr("user2"), UserId: stringPtr("id2")},
	}

	values := analyzer.extractAWSFieldFromResults(results, "UserName")

	if len(values) != 2 {
		t.Errorf("Expected 2 values, got %d", len(values))
	}

	if values[0] != "user1" {
		t.Errorf("Expected 'user1', got %v", values[0])
	}

	if values[1] != "user2" {
		t.Errorf("Expected 'user2', got %v", values[1])
	}
}

// Helper functions for tests
func intPtr(i int) *int {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}

func stringPtr(s string) *string {
	return &s
}

// Benchmark tests
func BenchmarkAnalyzeAWSOperation(b *testing.B) {
	analyzer := NewAWSParameterAnalyzer()
	operation := generator.AWSOperation{
		Name:   "ListObjects",
		Method: "ListObjects",
	}
	clientType := reflect.TypeOf(&MockS3Client{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer.AnalyzeAWSOperation("s3", operation, clientType)
	}
}

func BenchmarkValidateAWSParameters(b *testing.B) {
	analyzer := NewAWSParameterAnalyzer()
	analysis := AWSParameterAnalysis{
		RequiredParams: []AWSParameterInfo{
			{Name: "BucketName"},
		},
		ValidationRules: []AWSValidationRule{
			{
				RuleType:  "required",
				Field:     "BucketName",
				Condition: "not_empty",
				ErrorMsg:  "BucketName is required",
			},
		},
	}
	params := map[string]interface{}{
		"BucketName": "my-test-bucket",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer.ValidateAWSParameters(analysis, params)
	}
}

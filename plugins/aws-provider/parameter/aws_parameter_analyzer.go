package parameter

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// AWSParameterAnalysis represents the result of analyzing AWS operation parameters
type AWSParameterAnalysis struct {
	OperationName   string                   `json:"operation_name"`
	RequiredParams  []AWSParameterInfo       `json:"required_params"`
	OptionalParams  []AWSParameterInfo       `json:"optional_params"`
	Dependencies    []AWSParameterDependency `json:"dependencies"`
	ValidationRules []AWSValidationRule      `json:"validation_rules"`
	DefaultStrategy AWSParameterStrategy     `json:"default_strategy"`
	Confidence      float64                  `json:"confidence"`
	CanAutoExecute  bool                     `json:"can_auto_execute"`
}

// AWSParameterInfo represents detailed information about a parameter
type AWSParameterInfo struct {
	Name         string                 `json:"name"`
	Type         reflect.Type           `json:"-"`
	TypeName     string                 `json:"type_name"`
	IsPointer    bool                   `json:"is_pointer"`
	IsSlice      bool                   `json:"is_slice"`
	IsRequired   bool                   `json:"is_required"`
	DefaultValue interface{}            `json:"default_value,omitempty"`
	Constraints  AWSParameterConstraint `json:"constraints"`
	Description  string                 `json:"description"`
	Examples     []interface{}          `json:"examples,omitempty"`
}

// AWSParameterDependency represents a dependency between operations
type AWSParameterDependency struct {
	SourceOperation string `json:"source_operation"`
	SourceField     string `json:"source_field"`
	TargetParam     string `json:"target_param"`
	Relationship    string `json:"relationship"` // "one-to-one", "one-to-many", "many-to-one"
	Required        bool   `json:"required"`
}

// AWSParameterConstraint represents validation constraints for a parameter
type AWSParameterConstraint struct {
	MinLength     *int     `json:"min_length,omitempty"`
	MaxLength     *int     `json:"max_length,omitempty"`
	Pattern       string   `json:"pattern,omitempty"`
	AllowedValues []string `json:"allowed_values,omitempty"`
	MinValue      *int64   `json:"min_value,omitempty"`
	MaxValue      *int64   `json:"max_value,omitempty"`
}

// AWSValidationRule represents a validation rule for parameters
type AWSValidationRule struct {
	RuleType  string      `json:"rule_type"`
	Field     string      `json:"field"`
	Condition string      `json:"condition"`
	Value     interface{} `json:"value"`
	ErrorMsg  string      `json:"error_msg"`
}

// AWSParameterStrategy represents a strategy for parameter resolution
type AWSParameterStrategy struct {
	StrategyType    string                 `json:"strategy_type"` // "static", "dynamic", "dependency", "user_input"
	StaticValues    map[string]interface{} `json:"static_values,omitempty"`
	DependencyChain []string               `json:"dependency_chain,omitempty"`
	FallbackValues  map[string]interface{} `json:"fallback_values,omitempty"`
}

// AWSParameterAnalyzer provides advanced parameter analysis for AWS operations
type AWSParameterAnalyzer struct {
	servicePatterns    map[string]AWSServiceParameterPattern
	dependencyGraph    map[string][]AWSParameterDependency
	validationRules    map[string][]AWSValidationRule
	typeInferenceCache map[string]AWSParameterAnalysis
}

// AWSServiceParameterPattern defines parameter patterns for specific AWS services
type AWSServiceParameterPattern struct {
	ServiceName       string                         `json:"service_name"`
	CommonParams      map[string]AWSParameterInfo    `json:"common_params"`
	OperationPatterns map[string]AWSOperationPattern `json:"operation_patterns"`
	DependencyRules   []AWSParameterDependency       `json:"dependency_rules"`
}

// AWSOperationPattern defines parameter patterns for specific operations
type AWSOperationPattern struct {
	OperationPrefix string                            `json:"operation_prefix"`
	RequiredParams  []string                          `json:"required_params"`
	OptionalParams  []string                          `json:"optional_params"`
	DefaultStrategy AWSParameterStrategy              `json:"default_strategy"`
	Constraints     map[string]AWSParameterConstraint `json:"constraints"`
}

// NewAWSParameterAnalyzer creates a new AWS parameter analyzer
func NewAWSParameterAnalyzer() *AWSParameterAnalyzer {
	analyzer := &AWSParameterAnalyzer{
		servicePatterns:    make(map[string]AWSServiceParameterPattern),
		dependencyGraph:    make(map[string][]AWSParameterDependency),
		validationRules:    make(map[string][]AWSValidationRule),
		typeInferenceCache: make(map[string]AWSParameterAnalysis),
	}

	// Initialize with built-in AWS service patterns
	analyzer.initializeAWSServicePatterns()

	return analyzer
}

// AnalyzeAWSOperation performs comprehensive parameter analysis for an AWS operation
func (a *AWSParameterAnalyzer) AnalyzeAWSOperation(serviceName string, operation generator.AWSOperation, clientType reflect.Type) AWSParameterAnalysis {
	cacheKey := fmt.Sprintf("%s:%s", serviceName, operation.Name)

	// Check cache first
	if cached, exists := a.typeInferenceCache[cacheKey]; exists {
		return cached
	}

	analysis := AWSParameterAnalysis{
		OperationName:   operation.Name,
		RequiredParams:  []AWSParameterInfo{},
		OptionalParams:  []AWSParameterInfo{},
		Dependencies:    []AWSParameterDependency{},
		ValidationRules: []AWSValidationRule{},
		Confidence:      0.0,
		CanAutoExecute:  false,
	}

	// Get method from client type for signature analysis
	method, found := clientType.MethodByName(operation.Name)
	if !found {
		analysis.Confidence = 0.0
		return analysis
	}

	// Analyze method signature using reflection
	a.analyzeAWSMethodSignature(method, &analysis)

	// Apply service-specific patterns
	a.applyAWSServicePatterns(serviceName, operation.Name, &analysis)

	// Detect dependencies
	a.detectAWSParameterDependencies(serviceName, operation.Name, &analysis)

	// Generate validation rules
	a.generateAWSValidationRules(&analysis)

	// Determine execution strategy
	a.determineAWSExecutionStrategy(&analysis)

	// Calculate confidence score
	analysis.Confidence = a.calculateAWSConfidenceScore(&analysis)

	// Cache the result
	a.typeInferenceCache[cacheKey] = analysis

	return analysis
}

// analyzeAWSMethodSignature analyzes the method signature using reflection
func (a *AWSParameterAnalyzer) analyzeAWSMethodSignature(method reflect.Method, analysis *AWSParameterAnalysis) {
	methodType := method.Type

	// AWS SDK methods typically have signature: (ctx context.Context, input *InputStruct) (*OutputStruct, error)
	if methodType.NumIn() < 3 { // receiver, context, input
		return
	}

	inputType := methodType.In(2) // Third parameter is the input struct
	if inputType.Kind() == reflect.Ptr {
		inputType = inputType.Elem()
	}

	if inputType.Kind() != reflect.Struct {
		return
	}

	// Analyze each field in the input struct
	for i := 0; i < inputType.NumField(); i++ {
		field := inputType.Field(i)
		paramInfo := a.analyzeAWSStructField(field)

		if paramInfo.IsRequired {
			analysis.RequiredParams = append(analysis.RequiredParams, paramInfo)
		} else {
			analysis.OptionalParams = append(analysis.OptionalParams, paramInfo)
		}
	}
}

// analyzeAWSStructField analyzes a struct field to extract parameter information
func (a *AWSParameterAnalyzer) analyzeAWSStructField(field reflect.StructField) AWSParameterInfo {
	paramInfo := AWSParameterInfo{
		Name:      field.Name,
		Type:      field.Type,
		TypeName:  field.Type.String(),
		IsPointer: field.Type.Kind() == reflect.Ptr,
		IsSlice:   field.Type.Kind() == reflect.Slice,
	}

	// Determine if field is required based on AWS SDK patterns
	paramInfo.IsRequired = a.isAWSFieldRequired(field)

	// Extract constraints from struct tags
	paramInfo.Constraints = a.extractAWSConstraintsFromTags(field)

	// Generate examples based on field type and name
	paramInfo.Examples = a.generateAWSFieldExamples(field)

	// Set description based on field name patterns
	paramInfo.Description = a.generateAWSFieldDescription(field.Name)

	return paramInfo
}

// isAWSFieldRequired determines if a field is required based on AWS patterns
func (a *AWSParameterAnalyzer) isAWSFieldRequired(field reflect.StructField) bool {
	// In AWS SDK, pointer fields are typically optional, non-pointer are required
	if field.Type.Kind() == reflect.Ptr {
		return false
	}

	// Check for common required field patterns
	requiredPatterns := []string{
		"Name", "Id", "Arn", "Key", "Bucket", "Function", "Table", "Topic", "Queue",
	}

	for _, pattern := range requiredPatterns {
		if strings.Contains(field.Name, pattern) {
			return true
		}
	}

	return false
}

// extractAWSConstraintsFromTags extracts validation constraints from struct tags
func (a *AWSParameterAnalyzer) extractAWSConstraintsFromTags(field reflect.StructField) AWSParameterConstraint {
	constraint := AWSParameterConstraint{}

	// AWS SDK uses specific validation tags
	if validate := field.Tag.Get("validate"); validate != "" {
		// Parse validation tags (e.g., "required,min=1,max=255")
		rules := strings.Split(validate, ",")
		for _, rule := range rules {
			if strings.HasPrefix(rule, "min=") {
				// Handle min constraints
			} else if strings.HasPrefix(rule, "max=") {
				// Handle max constraints
			}
		}
	}

	// Check for location tags that indicate parameter type
	if location := field.Tag.Get("location"); location != "" {
		switch location {
		case "uri":
			// URI parameters are typically required
		case "querystring":
			// Query parameters are typically optional
		case "header":
			// Header parameters have specific constraints
		}
	}

	return constraint
}

// generateAWSFieldExamples generates example values for AWS fields
func (a *AWSParameterAnalyzer) generateAWSFieldExamples(field reflect.StructField) []interface{} {
	var examples []interface{}

	fieldName := strings.ToLower(field.Name)

	// Generate examples based on common AWS field patterns
	switch {
	case strings.Contains(fieldName, "bucket"):
		examples = append(examples, "my-bucket", "example-bucket-name")
	case strings.Contains(fieldName, "key"):
		examples = append(examples, "my-object-key", "path/to/file.txt")
	case strings.Contains(fieldName, "function"):
		examples = append(examples, "my-function", "lambda-function-name")
	case strings.Contains(fieldName, "table"):
		examples = append(examples, "my-table", "dynamodb-table-name")
	case strings.Contains(fieldName, "topic"):
		examples = append(examples, "my-topic", "sns-topic-name")
	case strings.Contains(fieldName, "queue"):
		examples = append(examples, "my-queue", "sqs-queue-name")
	case strings.Contains(fieldName, "region"):
		examples = append(examples, "us-east-1", "us-west-2", "eu-west-1")
	case strings.Contains(fieldName, "maxresults") || strings.Contains(fieldName, "limit"):
		examples = append(examples, 10, 50, 100)
	default:
		// Generate type-based examples
		switch field.Type.Kind() {
		case reflect.String:
			examples = append(examples, "example-value")
		case reflect.Int, reflect.Int32, reflect.Int64:
			examples = append(examples, 1, 10, 100)
		case reflect.Bool:
			examples = append(examples, true, false)
		}
	}

	return examples
}

// generateAWSFieldDescription generates descriptions for AWS fields
func (a *AWSParameterAnalyzer) generateAWSFieldDescription(fieldName string) string {
	descriptions := map[string]string{
		"Bucket":       "The name of the S3 bucket",
		"Key":          "The object key or identifier",
		"FunctionName": "The name of the Lambda function",
		"TableName":    "The name of the DynamoDB table",
		"TopicArn":     "The ARN of the SNS topic",
		"QueueUrl":     "The URL of the SQS queue",
		"MaxResults":   "The maximum number of results to return",
		"NextToken":    "Token for pagination",
		"Marker":       "Marker for pagination",
	}

	if desc, exists := descriptions[fieldName]; exists {
		return desc
	}

	// Generate description based on field name patterns
	if strings.HasSuffix(fieldName, "Name") {
		return fmt.Sprintf("The name of the %s", strings.TrimSuffix(fieldName, "Name"))
	}
	if strings.HasSuffix(fieldName, "Id") {
		return fmt.Sprintf("The ID of the %s", strings.TrimSuffix(fieldName, "Id"))
	}
	if strings.HasSuffix(fieldName, "Arn") {
		return fmt.Sprintf("The ARN of the %s", strings.TrimSuffix(fieldName, "Arn"))
	}

	return fmt.Sprintf("The %s parameter", fieldName)
}

// initializeAWSServicePatterns initializes built-in AWS service patterns
func (a *AWSParameterAnalyzer) initializeAWSServicePatterns() {
	// S3 Service Pattern
	a.servicePatterns["s3"] = AWSServiceParameterPattern{
		ServiceName: "s3",
		CommonParams: map[string]AWSParameterInfo{
			"Bucket": {
				Name:        "Bucket",
				TypeName:    "string",
				IsRequired:  true,
				Description: "The name of the S3 bucket",
				Examples:    []interface{}{"my-bucket", "example-bucket"},
			},
		},
		OperationPatterns: map[string]AWSOperationPattern{
			"List": {
				OperationPrefix: "List",
				RequiredParams:  []string{},
				OptionalParams:  []string{"MaxKeys", "Prefix", "Delimiter"},
				DefaultStrategy: AWSParameterStrategy{
					StrategyType: "static",
					StaticValues: map[string]interface{}{
						"MaxKeys": 1000,
					},
				},
			},
		},
	}

	// EC2 Service Pattern
	a.servicePatterns["ec2"] = AWSServiceParameterPattern{
		ServiceName: "ec2",
		CommonParams: map[string]AWSParameterInfo{
			"MaxResults": {
				Name:        "MaxResults",
				TypeName:    "int32",
				IsRequired:  false,
				Description: "The maximum number of results to return",
				Examples:    []interface{}{10, 50, 100},
			},
		},
		OperationPatterns: map[string]AWSOperationPattern{
			"Describe": {
				OperationPrefix: "Describe",
				RequiredParams:  []string{},
				OptionalParams:  []string{"MaxResults", "NextToken", "Filters"},
				DefaultStrategy: AWSParameterStrategy{
					StrategyType: "static",
					StaticValues: map[string]interface{}{
						"MaxResults": 100,
					},
				},
			},
		},
	}

	// IAM Service Pattern
	a.servicePatterns["iam"] = AWSServiceParameterPattern{
		ServiceName: "iam",
		DependencyRules: []AWSParameterDependency{
			{
				SourceOperation: "ListUsers",
				SourceField:     "UserName",
				TargetParam:     "UserName",
				Relationship:    "one-to-many",
				Required:        true,
			},
			{
				SourceOperation: "ListRoles",
				SourceField:     "RoleName",
				TargetParam:     "RoleName",
				Relationship:    "one-to-many",
				Required:        true,
			},
		},
	}
}

// applyAWSServicePatterns applies service-specific patterns to the analysis
func (a *AWSParameterAnalyzer) applyAWSServicePatterns(serviceName, operationName string, analysis *AWSParameterAnalysis) {
	pattern, exists := a.servicePatterns[serviceName]
	if !exists {
		return
	}

	// Apply operation-specific patterns
	for prefix, opPattern := range pattern.OperationPatterns {
		if strings.HasPrefix(operationName, prefix) {
			analysis.DefaultStrategy = opPattern.DefaultStrategy

			// Apply constraints
			for _, param := range analysis.RequiredParams {
				if constraint, exists := opPattern.Constraints[param.Name]; exists {
					param.Constraints = constraint
				}
			}
			for _, param := range analysis.OptionalParams {
				if constraint, exists := opPattern.Constraints[param.Name]; exists {
					param.Constraints = constraint
				}
			}
			break
		}
	}
}

// detectAWSParameterDependencies detects parameter dependencies between operations
func (a *AWSParameterAnalyzer) detectAWSParameterDependencies(serviceName, operationName string, analysis *AWSParameterAnalysis) {
	pattern, exists := a.servicePatterns[serviceName]
	if !exists {
		return
	}

	// Check for dependencies based on parameter names
	for _, param := range analysis.RequiredParams {
		for _, depRule := range pattern.DependencyRules {
			if param.Name == depRule.TargetParam {
				analysis.Dependencies = append(analysis.Dependencies, depRule)
			}
		}
	}

	// Detect implicit dependencies based on naming patterns
	a.detectImplicitAWSDependencies(serviceName, operationName, analysis)
}

// detectImplicitAWSDependencies detects implicit dependencies based on naming patterns
func (a *AWSParameterAnalyzer) detectImplicitAWSDependencies(serviceName, operationName string, analysis *AWSParameterAnalysis) {
	// Common AWS dependency patterns
	dependencyPatterns := map[string]string{
		"UserName":     "ListUsers",
		"RoleName":     "ListRoles",
		"GroupName":    "ListGroups",
		"PolicyName":   "ListPolicies",
		"BucketName":   "ListBuckets",
		"FunctionName": "ListFunctions",
		"TableName":    "ListTables",
		"TopicArn":     "ListTopics",
		"QueueUrl":     "ListQueues",
		"InstanceId":   "DescribeInstances",
		"VolumeId":     "DescribeVolumes",
		"VpcId":        "DescribeVpcs",
	}

	for _, param := range analysis.RequiredParams {
		if sourceOp, exists := dependencyPatterns[param.Name]; exists {
			dependency := AWSParameterDependency{
				SourceOperation: sourceOp,
				SourceField:     param.Name,
				TargetParam:     param.Name,
				Relationship:    "one-to-many",
				Required:        true,
			}
			analysis.Dependencies = append(analysis.Dependencies, dependency)
		}
	}
}

// generateAWSValidationRules generates validation rules for parameters
func (a *AWSParameterAnalyzer) generateAWSValidationRules(analysis *AWSParameterAnalysis) {
	for _, param := range analysis.RequiredParams {
		// Required parameter validation
		rule := AWSValidationRule{
			RuleType:  "required",
			Field:     param.Name,
			Condition: "not_empty",
			ErrorMsg:  fmt.Sprintf("%s is required", param.Name),
		}
		analysis.ValidationRules = append(analysis.ValidationRules, rule)

		// Type-specific validation
		a.addAWSTypeSpecificValidation(param, analysis)
	}

	for _, param := range analysis.OptionalParams {
		// Optional parameter validation
		a.addAWSTypeSpecificValidation(param, analysis)
	}
}

// addAWSTypeSpecificValidation adds type-specific validation rules
func (a *AWSParameterAnalyzer) addAWSTypeSpecificValidation(param AWSParameterInfo, analysis *AWSParameterAnalysis) {
	switch param.TypeName {
	case "string", "*string":
		if param.Constraints.MinLength != nil {
			rule := AWSValidationRule{
				RuleType:  "min_length",
				Field:     param.Name,
				Condition: ">=",
				Value:     *param.Constraints.MinLength,
				ErrorMsg:  fmt.Sprintf("%s must be at least %d characters", param.Name, *param.Constraints.MinLength),
			}
			analysis.ValidationRules = append(analysis.ValidationRules, rule)
		}

		if param.Constraints.MaxLength != nil {
			rule := AWSValidationRule{
				RuleType:  "max_length",
				Field:     param.Name,
				Condition: "<=",
				Value:     *param.Constraints.MaxLength,
				ErrorMsg:  fmt.Sprintf("%s must be at most %d characters", param.Name, *param.Constraints.MaxLength),
			}
			analysis.ValidationRules = append(analysis.ValidationRules, rule)
		}

	case "int32", "*int32", "int64", "*int64":
		if param.Constraints.MinValue != nil {
			rule := AWSValidationRule{
				RuleType:  "min_value",
				Field:     param.Name,
				Condition: ">=",
				Value:     *param.Constraints.MinValue,
				ErrorMsg:  fmt.Sprintf("%s must be at least %d", param.Name, *param.Constraints.MinValue),
			}
			analysis.ValidationRules = append(analysis.ValidationRules, rule)
		}

		if param.Constraints.MaxValue != nil {
			rule := AWSValidationRule{
				RuleType:  "max_value",
				Field:     param.Name,
				Condition: "<=",
				Value:     *param.Constraints.MaxValue,
				ErrorMsg:  fmt.Sprintf("%s must be at most %d", param.Name, *param.Constraints.MaxValue),
			}
			analysis.ValidationRules = append(analysis.ValidationRules, rule)
		}
	}
}

// determineAWSExecutionStrategy determines if the operation can be auto-executed
func (a *AWSParameterAnalyzer) determineAWSExecutionStrategy(analysis *AWSParameterAnalysis) {
	// Can auto-execute if:
	// 1. No required parameters, OR
	// 2. All required parameters have dependencies that can be resolved, OR
	// 3. All required parameters have default values

	if len(analysis.RequiredParams) == 0 {
		analysis.CanAutoExecute = true
		analysis.DefaultStrategy.StrategyType = "static"
		return
	}

	resolvableParams := 0
	for _, param := range analysis.RequiredParams {
		// Check if parameter has a dependency
		hasDependency := false
		for _, dep := range analysis.Dependencies {
			if dep.TargetParam == param.Name {
				hasDependency = true
				break
			}
		}

		// Check if parameter has a default value
		hasDefault := param.DefaultValue != nil

		if hasDependency || hasDefault {
			resolvableParams++
		}
	}

	if resolvableParams == len(analysis.RequiredParams) {
		analysis.CanAutoExecute = true
		if len(analysis.Dependencies) > 0 {
			analysis.DefaultStrategy.StrategyType = "dependency"
			// Build dependency chain
			for _, dep := range analysis.Dependencies {
				analysis.DefaultStrategy.DependencyChain = append(analysis.DefaultStrategy.DependencyChain, dep.SourceOperation)
			}
		} else {
			analysis.DefaultStrategy.StrategyType = "static"
		}
	} else {
		analysis.CanAutoExecute = false
		analysis.DefaultStrategy.StrategyType = "user_input"
	}
}

// calculateAWSConfidenceScore calculates a confidence score for the analysis
func (a *AWSParameterAnalyzer) calculateAWSConfidenceScore(analysis *AWSParameterAnalysis) float64 {
	score := 0.0
	maxScore := 100.0

	// Base score for having parameter information
	if len(analysis.RequiredParams) > 0 || len(analysis.OptionalParams) > 0 {
		score += 20.0
	}

	// Score for dependency detection
	if len(analysis.Dependencies) > 0 {
		score += 30.0
	}

	// Score for validation rules
	if len(analysis.ValidationRules) > 0 {
		score += 20.0
	}

	// Score for execution strategy
	if analysis.CanAutoExecute {
		score += 30.0
	} else if analysis.DefaultStrategy.StrategyType != "" {
		score += 15.0
	}

	return score / maxScore
}

// ValidateAWSParameters validates parameters against the analysis rules
func (a *AWSParameterAnalyzer) ValidateAWSParameters(analysis AWSParameterAnalysis, params map[string]interface{}) []error {
	var errors []error

	// Check required parameters
	for _, param := range analysis.RequiredParams {
		if _, exists := params[param.Name]; !exists {
			errors = append(errors, fmt.Errorf("required parameter %s is missing", param.Name))
		}
	}

	// Apply validation rules
	for _, rule := range analysis.ValidationRules {
		if err := a.applyAWSValidationRule(rule, params); err != nil {
			errors = append(errors, err)
		}
	}

	return errors
}

// applyAWSValidationRule applies a single validation rule
func (a *AWSParameterAnalyzer) applyAWSValidationRule(rule AWSValidationRule, params map[string]interface{}) error {
	value, exists := params[rule.Field]
	if !exists && rule.RuleType == "required" {
		return fmt.Errorf(rule.ErrorMsg)
	}

	if !exists {
		return nil // Skip validation for missing optional parameters
	}

	switch rule.RuleType {
	case "min_length":
		if str, ok := value.(string); ok {
			if minLen, ok := rule.Value.(int); ok && len(str) < minLen {
				return fmt.Errorf(rule.ErrorMsg)
			}
		}
	case "max_length":
		if str, ok := value.(string); ok {
			if maxLen, ok := rule.Value.(int); ok && len(str) > maxLen {
				return fmt.Errorf(rule.ErrorMsg)
			}
		}
	case "min_value":
		if num, ok := value.(int64); ok {
			if minVal, ok := rule.Value.(int64); ok && num < minVal {
				return fmt.Errorf(rule.ErrorMsg)
			}
		}
	case "max_value":
		if num, ok := value.(int64); ok {
			if maxVal, ok := rule.Value.(int64); ok && num > maxVal {
				return fmt.Errorf(rule.ErrorMsg)
			}
		}
	}

	return nil
}

// ResolveAWSParameterDependencies resolves parameter dependencies by executing prerequisite operations
func (a *AWSParameterAnalyzer) ResolveAWSParameterDependencies(ctx context.Context, analysis AWSParameterAnalysis, executor AWSOperationExecutor) (map[string]interface{}, error) {
	resolvedParams := make(map[string]interface{})

	// Execute dependency operations in order
	for _, dep := range analysis.Dependencies {
		results, err := executor.ExecuteAWSOperation(ctx, dep.SourceOperation, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependency %s: %w", dep.SourceOperation, err)
		}

		// Extract the required field from results
		values := a.extractAWSFieldFromResults(results, dep.SourceField)
		if len(values) > 0 {
			if dep.Relationship == "one-to-one" {
				resolvedParams[dep.TargetParam] = values[0]
			} else {
				resolvedParams[dep.TargetParam] = values
			}
		}
	}

	return resolvedParams, nil
}

// extractAWSFieldFromResults extracts field values from operation results
func (a *AWSParameterAnalyzer) extractAWSFieldFromResults(results interface{}, fieldName string) []interface{} {
	var values []interface{}

	resultsValue := reflect.ValueOf(results)
	if resultsValue.Kind() == reflect.Ptr {
		resultsValue = resultsValue.Elem()
	}

	if resultsValue.Kind() == reflect.Slice {
		for i := 0; i < resultsValue.Len(); i++ {
			item := resultsValue.Index(i)
			if item.Kind() == reflect.Ptr {
				item = item.Elem()
			}

			if item.Kind() == reflect.Struct {
				field := item.FieldByName(fieldName)
				if field.IsValid() {
					if field.Kind() == reflect.Ptr && !field.IsNil() {
						values = append(values, field.Elem().Interface())
					} else if field.Kind() != reflect.Ptr {
						values = append(values, field.Interface())
					}
				}
			}
		}
	}

	return values
}

// AWSOperationExecutor interface for executing AWS operations
type AWSOperationExecutor interface {
	ExecuteAWSOperation(ctx context.Context, operationName string, params map[string]interface{}) (interface{}, error)
}

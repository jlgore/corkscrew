package parameter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// AWSParameterCLI provides CLI commands for parameter intelligence
type AWSParameterCLI struct {
	enhancer *AWSPluginParameterEnhancer
}

// NewAWSParameterCLI creates a new parameter CLI
func NewAWSParameterCLI() *AWSParameterCLI {
	return &AWSParameterCLI{
		enhancer: NewAWSPluginParameterEnhancer(),
	}
}

// AWSParameterAnalyzeCommand represents the analyze command arguments
type AWSParameterAnalyzeCommand struct {
	Service    string  `arg:"--service" help:"AWS service to analyze (e.g., s3, ec2, iam)"`
	Operation  string  `arg:"--operation" help:"Specific operation to analyze"`
	Output     string  `arg:"--output" help:"Output format: json, table, summary" default:"table"`
	Verbose    bool    `arg:"-v,--verbose" help:"Show detailed analysis"`
	Report     bool    `arg:"--report" help:"Generate comprehensive report"`
	Confidence float64 `arg:"--min-confidence" help:"Minimum confidence threshold" default:"0.0"`
}

// AWSParameterValidateCommand represents the validate command arguments
type AWSParameterValidateCommand struct {
	Service    string            `arg:"--service" help:"AWS service name"`
	Operation  string            `arg:"--operation" help:"Operation name"`
	Parameters map[string]string `arg:"--params" help:"Parameters to validate (key=value format)"`
	File       string            `arg:"--file" help:"JSON file containing parameters"`
}

// AWSParameterPlanCommand represents the plan command arguments
type AWSParameterPlanCommand struct {
	Service string `arg:"--service" help:"AWS service to plan for"`
	Output  string `arg:"--output" help:"Output format: json, table" default:"table"`
	AutoRun bool   `arg:"--auto-run" help:"Show only auto-executable operations"`
}

// ExecuteAWSParameterAnalyze executes the parameter analyze command
func (cli *AWSParameterCLI) ExecuteAWSParameterAnalyze(cmd AWSParameterAnalyzeCommand) error {
	if cmd.Service == "" {
		return fmt.Errorf("service name is required")
	}

	// Get operations for the service (this would integrate with existing discovery)
	operations, err := cli.getAWSOperationsForService(cmd.Service)
	if err != nil {
		return fmt.Errorf("failed to get operations for service %s: %w", cmd.Service, err)
	}

	// Filter to specific operation if specified
	if cmd.Operation != "" {
		filtered := []generator.AWSOperation{}
		for _, op := range operations {
			if strings.EqualFold(op.Name, cmd.Operation) {
				filtered = append(filtered, op)
				break
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("operation %s not found in service %s", cmd.Operation, cmd.Service)
		}
		operations = filtered
	}

	// Get client type for reflection analysis
	clientType := cli.enhancer.getAWSClientType(cmd.Service)
	if clientType == nil {
		return fmt.Errorf("unable to get client type for service %s", cmd.Service)
	}

	// Enhance operations with parameter intelligence
	enhanced := cli.enhancer.EnhanceAWSOperations(cmd.Service, operations, clientType)

	// Filter by confidence if specified
	if cmd.Confidence > 0.0 {
		filtered := []*AWSEnhancedOperation{}
		for _, op := range enhanced {
			if op.Analysis.Confidence >= cmd.Confidence {
				filtered = append(filtered, op)
			}
		}
		enhanced = filtered
	}

	// Output results
	switch cmd.Output {
	case "json":
		return cli.outputAWSAnalysisJSON(enhanced)
	case "summary":
		return cli.outputAWSAnalysisSummary(enhanced, cmd.Service)
	case "table":
		return cli.outputAWSAnalysisTable(enhanced, cmd.Verbose)
	default:
		return fmt.Errorf("unsupported output format: %s", cmd.Output)
	}
}

// ExecuteAWSParameterValidate executes the parameter validate command
func (cli *AWSParameterCLI) ExecuteAWSParameterValidate(cmd AWSParameterValidateCommand) error {
	if cmd.Service == "" || cmd.Operation == "" {
		return fmt.Errorf("both service and operation are required")
	}

	// Get the operation
	operations, err := cli.getAWSOperationsForService(cmd.Service)
	if err != nil {
		return fmt.Errorf("failed to get operations: %w", err)
	}

	var targetOp generator.AWSOperation
	found := false
	for _, op := range operations {
		if strings.EqualFold(op.Name, cmd.Operation) {
			targetOp = op
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("operation %s not found in service %s", cmd.Operation, cmd.Service)
	}

	// Get client type and analyze
	clientType := cli.enhancer.getAWSClientType(cmd.Service)
	if clientType == nil {
		return fmt.Errorf("unable to get client type for service %s", cmd.Service)
	}

	enhanced := cli.enhancer.EnhanceAWSOperation(cmd.Service, targetOp, clientType)

	// Prepare parameters for validation
	params := make(map[string]interface{})

	// Load from file if specified
	if cmd.File != "" {
		fileParams, err := cli.loadAWSParametersFromFile(cmd.File)
		if err != nil {
			return fmt.Errorf("failed to load parameters from file: %w", err)
		}
		for k, v := range fileParams {
			params[k] = v
		}
	}

	// Add command line parameters
	for k, v := range cmd.Parameters {
		params[k] = v
	}

	// Validate parameters
	errors := cli.enhancer.analyzer.ValidateAWSParameters(enhanced.Analysis, params)

	// Output validation results
	if len(errors) == 0 {
		fmt.Println("✅ All parameters are valid")
		return nil
	}

	fmt.Printf("❌ Found %d validation errors:\n", len(errors))
	for i, err := range errors {
		fmt.Printf("  %d. %s\n", i+1, err.Error())
	}

	return fmt.Errorf("parameter validation failed")
}

// ExecuteAWSParameterPlan executes the parameter plan command
func (cli *AWSParameterCLI) ExecuteAWSParameterPlan(cmd AWSParameterPlanCommand) error {
	if cmd.Service == "" {
		return fmt.Errorf("service name is required")
	}

	// Get operations for the service
	operations, err := cli.getAWSOperationsForService(cmd.Service)
	if err != nil {
		return fmt.Errorf("failed to get operations: %w", err)
	}

	// Get client type and enhance operations
	clientType := cli.enhancer.getAWSClientType(cmd.Service)
	if clientType == nil {
		return fmt.Errorf("unable to get client type for service %s", cmd.Service)
	}

	enhanced := cli.enhancer.EnhanceAWSOperations(cmd.Service, operations, clientType)

	// Filter for auto-run if specified
	if cmd.AutoRun {
		filtered := []*AWSEnhancedOperation{}
		for _, op := range enhanced {
			if op.CanAutoRun {
				filtered = append(filtered, op)
			}
		}
		enhanced = filtered
	}

	// Generate execution plan
	plan := cli.enhancer.GenerateAWSExecutionPlan(enhanced)

	// Output plan
	switch cmd.Output {
	case "json":
		return cli.outputAWSPlanJSON(plan)
	case "table":
		return cli.outputAWSPlanTable(plan)
	default:
		return fmt.Errorf("unsupported output format: %s", cmd.Output)
	}
}

// getAWSOperationsForService gets operations for a service (placeholder)
func (cli *AWSParameterCLI) getAWSOperationsForService(serviceName string) ([]generator.AWSOperation, error) {
	// This would integrate with the existing discovery system
	// For now, return a placeholder
	return []generator.AWSOperation{}, fmt.Errorf("service discovery integration needed")
}

// loadAWSParametersFromFile loads parameters from a JSON file
func (cli *AWSParameterCLI) loadAWSParametersFromFile(filename string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var params map[string]interface{}
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}

	return params, nil
}

// outputAWSAnalysisJSON outputs analysis results as JSON
func (cli *AWSParameterCLI) outputAWSAnalysisJSON(enhanced []*AWSEnhancedOperation) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(enhanced)
}

// outputAWSAnalysisSummary outputs a summary of the analysis
func (cli *AWSParameterCLI) outputAWSAnalysisSummary(enhanced []*AWSEnhancedOperation, serviceName string) error {
	autoExecutable := 0
	dependent := 0
	manual := 0

	for _, op := range enhanced {
		switch op.Strategy.StrategyType {
		case "static":
			autoExecutable++
		case "dependency":
			dependent++
		case "user_input":
			manual++
		}
	}

	fmt.Printf("Parameter Analysis Summary for %s\n", strings.ToUpper(serviceName))
	fmt.Printf("=====================================\n")
	fmt.Printf("Total Operations: %d\n", len(enhanced))
	fmt.Printf("Auto-Executable: %d (%.1f%%)\n", autoExecutable, float64(autoExecutable)/float64(len(enhanced))*100)
	fmt.Printf("Dependency-Based: %d (%.1f%%)\n", dependent, float64(dependent)/float64(len(enhanced))*100)
	fmt.Printf("Manual Input Required: %d (%.1f%%)\n", manual, float64(manual)/float64(len(enhanced))*100)

	return nil
}

// outputAWSAnalysisTable outputs analysis results as a table
func (cli *AWSParameterCLI) outputAWSAnalysisTable(enhanced []*AWSEnhancedOperation, verbose bool) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	if verbose {
		fmt.Fprintln(w, "OPERATION\tREQUIRED PARAMS\tOPTIONAL PARAMS\tDEPENDENCIES\tSTRATEGY\tCONFIDENCE\tAUTO-RUN")
		fmt.Fprintln(w, "=========\t===============\t===============\t============\t========\t==========\t========")
	} else {
		fmt.Fprintln(w, "OPERATION\tREQUIRED\tOPTIONAL\tSTRATEGY\tAUTO-RUN")
		fmt.Fprintln(w, "=========\t========\t========\t========\t========")
	}

	for _, op := range enhanced {
		autoRun := "❌"
		if op.CanAutoRun {
			autoRun = "✅"
		}

		if verbose {
			deps := strings.Join(op.Dependencies, ", ")
			if deps == "" {
				deps = "none"
			}
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%.2f\t%s\n",
				op.BaseOperation.Name,
				len(op.Analysis.RequiredParams),
				len(op.Analysis.OptionalParams),
				deps,
				op.Strategy.StrategyType,
				op.Analysis.Confidence,
				autoRun)
		} else {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n",
				op.BaseOperation.Name,
				len(op.Analysis.RequiredParams),
				len(op.Analysis.OptionalParams),
				op.Strategy.StrategyType,
				autoRun)
		}
	}

	return nil
}

// outputAWSPlanJSON outputs execution plan as JSON
func (cli *AWSParameterCLI) outputAWSPlanJSON(plan *AWSExecutionPlan) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

// outputAWSPlanTable outputs execution plan as a table
func (cli *AWSParameterCLI) outputAWSPlanTable(plan *AWSExecutionPlan) error {
	for i, phase := range plan.Phases {
		fmt.Printf("Phase %d: %s\n", i+1, phase.Name)
		fmt.Printf("Description: %s\n", phase.Description)
		fmt.Printf("Auto-Run: %v\n", phase.CanAutoRun)
		fmt.Printf("Operations (%d):\n", len(phase.Operations))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  OPERATION\tREQUIRED PARAMS\tDEPENDENCIES")
		fmt.Fprintln(w, "  =========\t===============\t============")

		for _, op := range phase.Operations {
			deps := strings.Join(op.Dependencies, ", ")
			if deps == "" {
				deps = "none"
			}
			fmt.Fprintf(w, "  %s\t%d\t%s\n",
				op.BaseOperation.Name,
				len(op.Analysis.RequiredParams),
				deps)
		}
		w.Flush()
		fmt.Println()
	}

	return nil
}

// AWSParameterExecutor provides execution capabilities with parameter intelligence
type AWSParameterExecutor struct {
	enhancer *AWSPluginParameterEnhancer
}

// NewAWSParameterExecutor creates a new parameter executor
func NewAWSParameterExecutor() *AWSParameterExecutor {
	return &AWSParameterExecutor{
		enhancer: NewAWSPluginParameterEnhancer(),
	}
}

// ExecuteAWSOperationWithIntelligence executes an operation with parameter intelligence
func (e *AWSParameterExecutor) ExecuteAWSOperationWithIntelligence(ctx context.Context, serviceName, operationName string, userParams map[string]interface{}, executor AWSOperationExecutor) (interface{}, error) {
	// Get the operation
	operations, err := e.getOperationsForService(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get operations: %w", err)
	}

	var targetOp generator.AWSOperation
	found := false
	for _, op := range operations {
		if strings.EqualFold(op.Name, operationName) {
			targetOp = op
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("operation %s not found in service %s", operationName, serviceName)
	}

	// Get client type and enhance operation
	clientType := e.enhancer.getAWSClientType(serviceName)
	if clientType == nil {
		return nil, fmt.Errorf("unable to get client type for service %s", serviceName)
	}

	enhanced := e.enhancer.EnhanceAWSOperation(serviceName, targetOp, clientType)

	// Resolve dependencies if needed
	resolvedParams := make(map[string]interface{})
	if len(enhanced.Analysis.Dependencies) > 0 {
		depParams, err := e.enhancer.analyzer.ResolveAWSParameterDependencies(ctx, enhanced.Analysis, executor)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
		}
		for k, v := range depParams {
			resolvedParams[k] = v
		}
	}

	// Merge user parameters with resolved parameters (user params take precedence)
	finalParams := make(map[string]interface{})
	for k, v := range resolvedParams {
		finalParams[k] = v
	}
	for k, v := range userParams {
		finalParams[k] = v
	}

	// Apply static defaults from strategy
	if enhanced.Strategy.StaticValues != nil {
		for k, v := range enhanced.Strategy.StaticValues {
			if _, exists := finalParams[k]; !exists {
				finalParams[k] = v
			}
		}
	}

	// Validate parameters
	errors := e.enhancer.analyzer.ValidateAWSParameters(enhanced.Analysis, finalParams)
	if len(errors) > 0 {
		return nil, fmt.Errorf("parameter validation failed: %v", errors)
	}

	// Execute the operation
	return executor.ExecuteAWSOperation(ctx, operationName, finalParams)
}

// getOperationsForService gets operations for a service (placeholder)
func (e *AWSParameterExecutor) getOperationsForService(serviceName string) ([]generator.AWSOperation, error) {
	// This would integrate with the existing discovery system
	return []generator.AWSOperation{}, fmt.Errorf("service discovery integration needed")
}

// AWSParameterMiddleware provides middleware for parameter enhancement
type AWSParameterMiddleware struct {
	enhancer *AWSPluginParameterEnhancer
}

// NewAWSParameterMiddleware creates new parameter middleware
func NewAWSParameterMiddleware() *AWSParameterMiddleware {
	return &AWSParameterMiddleware{
		enhancer: NewAWSPluginParameterEnhancer(),
	}
}

// EnhanceAWSOperationExecution enhances operation execution with parameter intelligence
func (m *AWSParameterMiddleware) EnhanceAWSOperationExecution(next AWSOperationHandler) AWSOperationHandler {
	return func(ctx context.Context, serviceName, operationName string, params map[string]interface{}) (interface{}, error) {
		// Get operations and enhance with parameter intelligence
		operations, err := m.getOperationsForService(serviceName)
		if err != nil {
			// If we can't enhance, just pass through to next handler
			return next(ctx, serviceName, operationName, params)
		}

		var targetOp generator.AWSOperation
		found := false
		for _, op := range operations {
			if strings.EqualFold(op.Name, operationName) {
				targetOp = op
				found = true
				break
			}
		}

		if !found {
			// If operation not found in our analysis, pass through
			return next(ctx, serviceName, operationName, params)
		}

		// Get client type and enhance
		clientType := m.enhancer.getAWSClientType(serviceName)
		if clientType == nil {
			// If we can't get client type, pass through
			return next(ctx, serviceName, operationName, params)
		}

		enhanced := m.enhancer.EnhanceAWSOperation(serviceName, targetOp, clientType)

		// Apply parameter intelligence
		enhancedParams := make(map[string]interface{})
		for k, v := range params {
			enhancedParams[k] = v
		}

		// Apply static defaults
		if enhanced.Strategy.StaticValues != nil {
			for k, v := range enhanced.Strategy.StaticValues {
				if _, exists := enhancedParams[k]; !exists {
					enhancedParams[k] = v
				}
			}
		}

		// Validate parameters (log warnings but don't fail)
		errors := m.enhancer.analyzer.ValidateAWSParameters(enhanced.Analysis, enhancedParams)
		if len(errors) > 0 {
			fmt.Printf("Warning: Parameter validation issues detected: %v\n", errors)
		}

		// Call next handler with enhanced parameters
		return next(ctx, serviceName, operationName, enhancedParams)
	}
}

// getOperationsForService gets operations for a service (placeholder)
func (m *AWSParameterMiddleware) getOperationsForService(serviceName string) ([]generator.AWSOperation, error) {
	// This would integrate with the existing discovery system
	return []generator.AWSOperation{}, fmt.Errorf("service discovery integration needed")
}

// AWSOperationHandler represents a handler for AWS operations
type AWSOperationHandler func(ctx context.Context, serviceName, operationName string, params map[string]interface{}) (interface{}, error)

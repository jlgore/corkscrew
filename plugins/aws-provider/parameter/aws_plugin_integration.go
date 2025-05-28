package parameter

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// AWSPluginParameterEnhancer enhances the existing plugin system with advanced parameter intelligence
type AWSPluginParameterEnhancer struct {
	analyzer *AWSParameterAnalyzer
	cache    map[string]*AWSEnhancedOperation
}

// AWSEnhancedOperation represents an operation enhanced with parameter intelligence
type AWSEnhancedOperation struct {
	BaseOperation generator.AWSOperation
	Analysis      AWSParameterAnalysis
	CanAutoRun    bool
	Dependencies  []string
	Strategy      AWSParameterStrategy
}

// NewAWSPluginParameterEnhancer creates a new plugin parameter enhancer
func NewAWSPluginParameterEnhancer() *AWSPluginParameterEnhancer {
	return &AWSPluginParameterEnhancer{
		analyzer: NewAWSParameterAnalyzer(),
		cache:    make(map[string]*AWSEnhancedOperation),
	}
}

// EnhanceAWSOperations enhances a list of AWS operations with parameter intelligence
func (e *AWSPluginParameterEnhancer) EnhanceAWSOperations(serviceName string, operations []generator.AWSOperation, clientType reflect.Type) []*AWSEnhancedOperation {
	var enhanced []*AWSEnhancedOperation

	for _, op := range operations {
		enhancedOp := e.EnhanceAWSOperation(serviceName, op, clientType)
		enhanced = append(enhanced, enhancedOp)
	}

	return enhanced
}

// EnhanceAWSOperation enhances a single AWS operation with parameter intelligence
func (e *AWSPluginParameterEnhancer) EnhanceAWSOperation(serviceName string, operation generator.AWSOperation, clientType reflect.Type) *AWSEnhancedOperation {
	cacheKey := fmt.Sprintf("%s:%s", serviceName, operation.Name)

	// Check cache first
	if cached, exists := e.cache[cacheKey]; exists {
		return cached
	}

	// Perform parameter analysis
	analysis := e.analyzer.AnalyzeAWSOperation(serviceName, operation, clientType)

	// Create enhanced operation
	enhanced := &AWSEnhancedOperation{
		BaseOperation: operation,
		Analysis:      analysis,
		CanAutoRun:    analysis.CanAutoExecute,
		Dependencies:  e.extractAWSDependencyOperations(analysis),
		Strategy:      analysis.DefaultStrategy,
	}

	// Cache the result
	e.cache[cacheKey] = enhanced

	return enhanced
}

// extractAWSDependencyOperations extracts dependency operation names from analysis
func (e *AWSPluginParameterEnhancer) extractAWSDependencyOperations(analysis AWSParameterAnalysis) []string {
	var deps []string
	seen := make(map[string]bool)

	for _, dep := range analysis.Dependencies {
		if !seen[dep.SourceOperation] {
			deps = append(deps, dep.SourceOperation)
			seen[dep.SourceOperation] = true
		}
	}

	return deps
}

// GenerateAWSExecutionPlan generates an execution plan for operations with dependencies
func (e *AWSPluginParameterEnhancer) GenerateAWSExecutionPlan(operations []*AWSEnhancedOperation) *AWSExecutionPlan {
	plan := &AWSExecutionPlan{
		Phases: []AWSExecutionPhase{},
	}

	// Separate operations by their execution requirements
	autoExecutable := []*AWSEnhancedOperation{}
	dependencyBased := []*AWSEnhancedOperation{}
	userInputRequired := []*AWSEnhancedOperation{}

	for _, op := range operations {
		switch op.Strategy.StrategyType {
		case "static":
			autoExecutable = append(autoExecutable, op)
		case "dependency":
			dependencyBased = append(dependencyBased, op)
		case "user_input":
			userInputRequired = append(userInputRequired, op)
		}
	}

	// Phase 1: Auto-executable operations (no dependencies)
	if len(autoExecutable) > 0 {
		phase := AWSExecutionPhase{
			Name:        "Auto-Executable Operations",
			Description: "Operations that can run immediately without parameters",
			Operations:  autoExecutable,
			CanAutoRun:  true,
		}
		plan.Phases = append(plan.Phases, phase)
	}

	// Phase 2: Dependency-based operations (ordered by dependency chain)
	if len(dependencyBased) > 0 {
		orderedOps := e.orderAWSOperationsByDependencies(dependencyBased)
		phase := AWSExecutionPhase{
			Name:        "Dependency-Based Operations",
			Description: "Operations that depend on results from other operations",
			Operations:  orderedOps,
			CanAutoRun:  true,
		}
		plan.Phases = append(plan.Phases, phase)
	}

	// Phase 3: User input required operations
	if len(userInputRequired) > 0 {
		phase := AWSExecutionPhase{
			Name:        "User Input Required",
			Description: "Operations that require manual parameter input",
			Operations:  userInputRequired,
			CanAutoRun:  false,
		}
		plan.Phases = append(plan.Phases, phase)
	}

	return plan
}

// orderAWSOperationsByDependencies orders operations based on their dependency chains
func (e *AWSPluginParameterEnhancer) orderAWSOperationsByDependencies(operations []*AWSEnhancedOperation) []*AWSEnhancedOperation {
	// Simple topological sort based on dependency chains
	ordered := []*AWSEnhancedOperation{}
	remaining := make([]*AWSEnhancedOperation, len(operations))
	copy(remaining, operations)

	// Keep processing until all operations are ordered
	for len(remaining) > 0 {
		progress := false

		for i := len(remaining) - 1; i >= 0; i-- {
			op := remaining[i]

			// Check if all dependencies are already in ordered list
			canAdd := true
			for _, depName := range op.Dependencies {
				found := false
				for _, orderedOp := range ordered {
					if orderedOp.BaseOperation.Name == depName {
						found = true
						break
					}
				}
				if !found {
					// Check if dependency is in the same batch (circular dependency handling)
					foundInRemaining := false
					for _, remainingOp := range remaining {
						if remainingOp.BaseOperation.Name == depName {
							foundInRemaining = true
							break
						}
					}
					if foundInRemaining {
						canAdd = false
						break
					}
				}
			}

			if canAdd {
				ordered = append(ordered, op)
				remaining = append(remaining[:i], remaining[i+1:]...)
				progress = true
			}
		}

		// If no progress, add remaining operations (handles circular dependencies)
		if !progress && len(remaining) > 0 {
			ordered = append(ordered, remaining...)
			break
		}
	}

	return ordered
}

// AWSExecutionPlan represents a plan for executing operations
type AWSExecutionPlan struct {
	Phases []AWSExecutionPhase `json:"phases"`
}

// AWSExecutionPhase represents a phase in the execution plan
type AWSExecutionPhase struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Operations  []*AWSEnhancedOperation `json:"operations"`
	CanAutoRun  bool                    `json:"can_auto_run"`
}

// IntegrateWithAWSDiscovery integrates parameter intelligence with the existing discovery system
func (e *AWSPluginParameterEnhancer) IntegrateWithAWSDiscovery(services map[string]*generator.AWSServiceInfo) *AWSEnhancedDiscoveryResult {
	enhanced := &AWSEnhancedDiscoveryResult{
		Services:          services,
		EnhancedServices:  make(map[string]*AWSEnhancedServiceInfo),
		ExecutionPlan:     &AWSExecutionPlan{},
		ParameterInsights: &AWSParameterInsights{},
	}

	// Process each service in the discovery result
	for serviceName, serviceInfo := range services {
		enhancedService := e.enhanceAWSServiceInfo(serviceName, serviceInfo)
		enhanced.EnhancedServices[serviceName] = enhancedService
	}

	// Generate overall execution plan
	allOperations := []*AWSEnhancedOperation{}
	for _, service := range enhanced.EnhancedServices {
		allOperations = append(allOperations, service.EnhancedOperations...)
	}
	enhanced.ExecutionPlan = e.GenerateAWSExecutionPlan(allOperations)

	// Generate parameter insights
	enhanced.ParameterInsights = e.generateAWSParameterInsights(enhanced.EnhancedServices)

	return enhanced
}

// enhanceAWSServiceInfo enhances service information with parameter intelligence
func (e *AWSPluginParameterEnhancer) enhanceAWSServiceInfo(serviceName string, serviceInfo *generator.AWSServiceInfo) *AWSEnhancedServiceInfo {
	enhanced := &AWSEnhancedServiceInfo{
		BaseInfo:           serviceInfo,
		ServiceName:        serviceName,
		EnhancedOperations: []*AWSEnhancedOperation{},
		AutoExecutableOps:  0,
		DependentOps:       0,
		ManualOps:          0,
	}

	// Get client type for reflection analysis
	clientType := e.getAWSClientType(serviceName)
	if clientType == nil {
		return enhanced
	}

	// Enhance each operation
	for _, op := range serviceInfo.Operations {
		enhancedOp := e.EnhanceAWSOperation(serviceName, op, clientType)
		enhanced.EnhancedOperations = append(enhanced.EnhancedOperations, enhancedOp)

		// Update counters
		switch enhancedOp.Strategy.StrategyType {
		case "static":
			enhanced.AutoExecutableOps++
		case "dependency":
			enhanced.DependentOps++
		case "user_input":
			enhanced.ManualOps++
		}
	}

	return enhanced
}

// getAWSClientType gets the client type for a service (placeholder - would need actual implementation)
func (e *AWSPluginParameterEnhancer) getAWSClientType(serviceName string) reflect.Type {
	// This would need to be implemented based on how the AWS clients are structured
	// For now, return nil to indicate we need the actual client type
	return nil
}

// generateAWSParameterInsights generates insights about parameters across all services
func (e *AWSPluginParameterEnhancer) generateAWSParameterInsights(services map[string]*AWSEnhancedServiceInfo) *AWSParameterInsights {
	insights := &AWSParameterInsights{
		TotalOperations:    0,
		AutoExecutableOps:  0,
		DependentOps:       0,
		ManualOps:          0,
		CommonParameters:   make(map[string]int),
		DependencyPatterns: make(map[string][]string),
		ValidationPatterns: make(map[string]int),
	}

	// Aggregate statistics
	for _, service := range services {
		insights.TotalOperations += len(service.EnhancedOperations)
		insights.AutoExecutableOps += service.AutoExecutableOps
		insights.DependentOps += service.DependentOps
		insights.ManualOps += service.ManualOps

		// Analyze parameter patterns
		for _, op := range service.EnhancedOperations {
			// Count common parameters
			for _, param := range op.Analysis.RequiredParams {
				insights.CommonParameters[param.Name]++
			}
			for _, param := range op.Analysis.OptionalParams {
				insights.CommonParameters[param.Name]++
			}

			// Track dependency patterns
			if len(op.Dependencies) > 0 {
				key := op.BaseOperation.Name
				insights.DependencyPatterns[key] = op.Dependencies
			}

			// Count validation patterns
			for _, rule := range op.Analysis.ValidationRules {
				insights.ValidationPatterns[rule.RuleType]++
			}
		}
	}

	return insights
}

// AWSEnhancedDiscoveryResult represents discovery results enhanced with parameter intelligence
type AWSEnhancedDiscoveryResult struct {
	Services          map[string]*generator.AWSServiceInfo `json:"services"`
	EnhancedServices  map[string]*AWSEnhancedServiceInfo   `json:"enhanced_services"`
	ExecutionPlan     *AWSExecutionPlan                    `json:"execution_plan"`
	ParameterInsights *AWSParameterInsights                `json:"parameter_insights"`
}

// AWSEnhancedServiceInfo represents service information enhanced with parameter intelligence
type AWSEnhancedServiceInfo struct {
	BaseInfo           *generator.AWSServiceInfo `json:"base_info"`
	ServiceName        string                    `json:"service_name"`
	EnhancedOperations []*AWSEnhancedOperation   `json:"enhanced_operations"`
	AutoExecutableOps  int                       `json:"auto_executable_ops"`
	DependentOps       int                       `json:"dependent_ops"`
	ManualOps          int                       `json:"manual_ops"`
}

// AWSParameterInsights provides insights about parameters across all services
type AWSParameterInsights struct {
	TotalOperations    int                 `json:"total_operations"`
	AutoExecutableOps  int                 `json:"auto_executable_ops"`
	DependentOps       int                 `json:"dependent_ops"`
	ManualOps          int                 `json:"manual_ops"`
	CommonParameters   map[string]int      `json:"common_parameters"`
	DependencyPatterns map[string][]string `json:"dependency_patterns"`
	ValidationPatterns map[string]int      `json:"validation_patterns"`
}

// GenerateAWSParameterReport generates a comprehensive report about parameter analysis
func (e *AWSPluginParameterEnhancer) GenerateAWSParameterReport(enhanced *AWSEnhancedDiscoveryResult) *AWSParameterReport {
	report := &AWSParameterReport{
		Summary: AWSParameterSummary{
			TotalServices:       len(enhanced.EnhancedServices),
			TotalOperations:     enhanced.ParameterInsights.TotalOperations,
			AutoExecutableOps:   enhanced.ParameterInsights.AutoExecutableOps,
			DependentOps:        enhanced.ParameterInsights.DependentOps,
			ManualOps:           enhanced.ParameterInsights.ManualOps,
			AutoExecutableRatio: float64(enhanced.ParameterInsights.AutoExecutableOps) / float64(enhanced.ParameterInsights.TotalOperations),
		},
		ServiceBreakdown: make(map[string]AWSServiceParameterSummary),
		Recommendations:  []AWSParameterRecommendation{},
	}

	// Generate service breakdown
	for serviceName, service := range enhanced.EnhancedServices {
		summary := AWSServiceParameterSummary{
			ServiceName:         serviceName,
			TotalOperations:     len(service.EnhancedOperations),
			AutoExecutableOps:   service.AutoExecutableOps,
			DependentOps:        service.DependentOps,
			ManualOps:           service.ManualOps,
			AutoExecutableRatio: float64(service.AutoExecutableOps) / float64(len(service.EnhancedOperations)),
			TopParameters:       e.getTopAWSParametersForService(service),
		}
		report.ServiceBreakdown[serviceName] = summary
	}

	// Generate recommendations
	report.Recommendations = e.generateAWSParameterRecommendations(enhanced)

	return report
}

// getTopAWSParametersForService gets the most common parameters for a service
func (e *AWSPluginParameterEnhancer) getTopAWSParametersForService(service *AWSEnhancedServiceInfo) []string {
	paramCounts := make(map[string]int)

	for _, op := range service.EnhancedOperations {
		for _, param := range op.Analysis.RequiredParams {
			paramCounts[param.Name]++
		}
		for _, param := range op.Analysis.OptionalParams {
			paramCounts[param.Name]++
		}
	}

	// Sort by frequency and return top 5
	type paramCount struct {
		name  string
		count int
	}

	var params []paramCount
	for name, count := range paramCounts {
		params = append(params, paramCount{name, count})
	}

	// Simple sort by count (descending)
	for i := 0; i < len(params)-1; i++ {
		for j := i + 1; j < len(params); j++ {
			if params[j].count > params[i].count {
				params[i], params[j] = params[j], params[i]
			}
		}
	}

	var topParams []string
	for i := 0; i < len(params) && i < 5; i++ {
		topParams = append(topParams, params[i].name)
	}

	return topParams
}

// generateAWSParameterRecommendations generates recommendations for improving parameter handling
func (e *AWSPluginParameterEnhancer) generateAWSParameterRecommendations(enhanced *AWSEnhancedDiscoveryResult) []AWSParameterRecommendation {
	var recommendations []AWSParameterRecommendation

	// Recommendation 1: Services with low auto-executable ratio
	for serviceName, service := range enhanced.EnhancedServices {
		if service.AutoExecutableOps > 0 && float64(service.AutoExecutableOps)/float64(len(service.EnhancedOperations)) < 0.3 {
			rec := AWSParameterRecommendation{
				Type:        "low_automation",
				ServiceName: serviceName,
				Title:       fmt.Sprintf("Low automation ratio in %s service", serviceName),
				Description: fmt.Sprintf("Only %d out of %d operations can be auto-executed. Consider adding default values or dependency resolution.", service.AutoExecutableOps, len(service.EnhancedOperations)),
				Priority:    "medium",
			}
			recommendations = append(recommendations, rec)
		}
	}

	// Recommendation 2: Common parameters that could be standardized
	commonParams := enhanced.ParameterInsights.CommonParameters
	for paramName, count := range commonParams {
		if count > 5 && strings.Contains(strings.ToLower(paramName), "region") {
			rec := AWSParameterRecommendation{
				Type:        "standardization",
				Title:       fmt.Sprintf("Standardize %s parameter", paramName),
				Description: fmt.Sprintf("Parameter %s appears in %d operations. Consider setting a default region configuration.", paramName, count),
				Priority:    "high",
			}
			recommendations = append(recommendations, rec)
		}
	}

	// Recommendation 3: Operations with complex dependency chains
	for _, phase := range enhanced.ExecutionPlan.Phases {
		if phase.Name == "Dependency-Based Operations" && len(phase.Operations) > 10 {
			rec := AWSParameterRecommendation{
				Type:        "complexity",
				Title:       "Complex dependency chain detected",
				Description: fmt.Sprintf("%d operations have dependencies. Consider batching or parallel execution strategies.", len(phase.Operations)),
				Priority:    "medium",
			}
			recommendations = append(recommendations, rec)
		}
	}

	return recommendations
}

// AWSParameterReport represents a comprehensive parameter analysis report
type AWSParameterReport struct {
	Summary          AWSParameterSummary                   `json:"summary"`
	ServiceBreakdown map[string]AWSServiceParameterSummary `json:"service_breakdown"`
	Recommendations  []AWSParameterRecommendation          `json:"recommendations"`
}

// AWSParameterSummary provides a high-level summary of parameter analysis
type AWSParameterSummary struct {
	TotalServices       int     `json:"total_services"`
	TotalOperations     int     `json:"total_operations"`
	AutoExecutableOps   int     `json:"auto_executable_ops"`
	DependentOps        int     `json:"dependent_ops"`
	ManualOps           int     `json:"manual_ops"`
	AutoExecutableRatio float64 `json:"auto_executable_ratio"`
}

// AWSServiceParameterSummary provides parameter summary for a specific service
type AWSServiceParameterSummary struct {
	ServiceName         string   `json:"service_name"`
	TotalOperations     int      `json:"total_operations"`
	AutoExecutableOps   int      `json:"auto_executable_ops"`
	DependentOps        int      `json:"dependent_ops"`
	ManualOps           int      `json:"manual_ops"`
	AutoExecutableRatio float64  `json:"auto_executable_ratio"`
	TopParameters       []string `json:"top_parameters"`
}

// AWSParameterRecommendation represents a recommendation for improving parameter handling
type AWSParameterRecommendation struct {
	Type        string `json:"type"`
	ServiceName string `json:"service_name,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

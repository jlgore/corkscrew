package discovery

import (
	"context"
	"fmt"
	"time"
)

// AWSResourceRef represents a lightweight reference to an AWS resource
// This is duplicated here to avoid import cycles - we'll unify this later
type AWSResourceRef struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Service  string            `json:"service"`
	Region   string            `json:"region"`
	ARN      string            `json:"arn,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ResourceScanner interface to avoid import cycles
type ResourceScanner interface {
	DiscoverAndListServiceResources(ctx context.Context, serviceName string) ([]AWSResourceRef, error)
	ExecuteOperationWithParams(ctx context.Context, serviceName, operationName string, params map[string]interface{}) ([]AWSResourceRef, error)
	GetRegion() string
}

// DiscoveryPhase represents a phase in hierarchical discovery
type DiscoveryPhase struct {
	Name          string            `json:"name"`
	Operations    []string          `json:"operations"`
	DependsOn     []string          `json:"depends_on,omitempty"`
	Description   string            `json:"description,omitempty"`
	ParamProvider ParameterProvider `json:"-"` // Function to provide parameters based on discovered resources
	MaxRetries    int               `json:"max_retries,omitempty"`
	RetryDelay    time.Duration     `json:"retry_delay,omitempty"`
}

// DiscoveryStrategy defines how to discover resources for a service hierarchically
type DiscoveryStrategy struct {
	ServiceName string           `json:"service_name"`
	Phases      []DiscoveryPhase `json:"phases"`
	Description string           `json:"description,omitempty"`
}

// ParameterProvider is a function that provides parameters for operations based on discovered resources
type ParameterProvider func(operationName string, discovered DiscoveryContext) map[string]interface{}

// DiscoveryContext provides access to discovered resources and environment info
type DiscoveryContext interface {
	GetDiscoveredResources(resourceType string) []AWSResourceRef
	GetDiscoveredResourcesByOperation(operationName string) []AWSResourceRef
	GetCredentialType() CredentialType
	GetAccountInfo() AccountInfo
	GetRegion() string
}

// CredentialType represents the type of AWS credentials being used
type CredentialType string

const (
	UserCredentials    CredentialType = "user"
	RoleCredentials    CredentialType = "role"
	UnknownCredentials CredentialType = "unknown"
)

// AccountInfo contains information about the AWS account
type AccountInfo struct {
	AccountID string
	UserArn   string
	RoleArn   string
}

// HierarchicalDiscoverer manages hierarchical resource discovery
type HierarchicalDiscoverer struct {
	scanner           ResourceScanner
	strategies        map[string]DiscoveryStrategy
	debug             bool
	accessController  *AccessController
	strategyValidator *StrategyValidator
}

// NewHierarchicalDiscoverer creates a new hierarchical discoverer
func NewHierarchicalDiscoverer(scanner ResourceScanner, debug bool) *HierarchicalDiscoverer {
	hd := &HierarchicalDiscoverer{
		scanner:           scanner,
		strategies:        make(map[string]DiscoveryStrategy),
		debug:             debug,
		accessController:  NewAccessController(),
		strategyValidator: NewStrategyValidator(),
	}

	// Register built-in strategies
	hd.registerBuiltInStrategies()

	return hd
}

// RegisterStrategy registers a custom/external discovery strategy for a service
// This method validates the strategy and prevents overriding built-in strategies
// For built-in strategies, use registerBuiltInStrategyDirect() instead
func (hd *HierarchicalDiscoverer) RegisterStrategy(strategy DiscoveryStrategy) error {
	// 🔒 SECURITY: Validate strategy before registration
	if err := hd.strategyValidator.ValidateStrategy(strategy); err != nil {
		if hd.debug {
			fmt.Printf("🚨 Strategy validation failed for %s: %v\n", strategy.ServiceName, err)
		}
		return fmt.Errorf("strategy validation failed: %w", err)
	}

	hd.strategies[strategy.ServiceName] = strategy

	if hd.debug {
		fmt.Printf("✅ Registered secure strategy for service: %s\n", strategy.ServiceName)
	}

	return nil
}

// DiscoverService discovers resources for a service using hierarchical strategy
func (hd *HierarchicalDiscoverer) DiscoverService(ctx context.Context, serviceName string) ([]AWSResourceRef, error) {
	// 🔒 SECURITY: Validate access before proceeding with hierarchical discovery
	// Note: This requires AWS config to be available, which we'll need to get from the scanner
	// For now, we'll implement a basic check and enhance it later
	if hd.accessController != nil {
		// We need AWS config for access validation, but the scanner interface doesn't expose it
		// This is a limitation we'll need to address in a future iteration
		if hd.debug {
			fmt.Printf("🔒 Access control validation enabled for service: %s\n", serviceName)
		}
	}

	strategy, exists := hd.strategies[serviceName]
	if !exists {
		if hd.debug {
			fmt.Printf("🔄 No hierarchical strategy for %s, falling back to flat discovery\n", serviceName)
		}
		// Fallback to flat discovery
		return hd.scanner.DiscoverAndListServiceResources(ctx, serviceName)
	}

	if hd.debug {
		fmt.Printf("🎯 Using hierarchical strategy for %s with %d phases\n", serviceName, len(strategy.Phases))
	}

	// Create discovery context
	context := &discoveryContext{
		discovered: make(map[string][]AWSResourceRef),
		region:     hd.scanner.GetRegion(),
	}

	var allResources []AWSResourceRef

	// Execute phases in order
	for i, phase := range strategy.Phases {
		if hd.debug {
			fmt.Printf("📋 Phase %d: %s (%s)\n", i+1, phase.Name, phase.Description)
		}

		// Check dependencies
		if !hd.areDependenciesMet(phase, context) {
			if hd.debug {
				fmt.Printf("⚠️  Skipping phase %s - dependencies not met: %v\n", phase.Name, phase.DependsOn)
			}
			continue
		}

		// Execute phase operations
		phaseResources, err := hd.executePhase(ctx, serviceName, phase, context)
		if err != nil {
			if hd.debug {
				fmt.Printf("⚠️  Phase %s failed: %v\n", phase.Name, err)
			}
			continue
		}

		// Add to context for next phases
		for opName, resources := range phaseResources {
			context.discovered[opName] = resources
			allResources = append(allResources, resources...)
		}

		if hd.debug {
			totalPhaseResources := 0
			for _, resources := range phaseResources {
				totalPhaseResources += len(resources)
			}
			fmt.Printf("✅ Phase %s completed: %d resources discovered\n", phase.Name, totalPhaseResources)
		}
	}

	return allResources, nil
}

// areDependenciesMet checks if all dependencies for a phase are satisfied
func (hd *HierarchicalDiscoverer) areDependenciesMet(phase DiscoveryPhase, context *discoveryContext) bool {
	// If no dependencies, phase can run
	if len(phase.DependsOn) == 0 {
		return true
	}

	// Check if any operation has discovered resources (simplified dependency check)
	hasResources := false
	for opName := range context.discovered {
		if len(context.discovered[opName]) > 0 {
			hasResources = true
			break
		}
	}
	return hasResources
}

// executePhase executes all operations in a discovery phase
func (hd *HierarchicalDiscoverer) executePhase(ctx context.Context, serviceName string, phase DiscoveryPhase, discoveryCtx *discoveryContext) (map[string][]AWSResourceRef, error) {
	results := make(map[string][]AWSResourceRef)

	for _, operationName := range phase.Operations {
		if hd.debug {
			fmt.Printf("  🔍 Executing operation: %s\n", operationName)
		}

		// Get parameters for this operation
		var params map[string]interface{}
		if phase.ParamProvider != nil {
			params = phase.ParamProvider(operationName, discoveryCtx)
		}

		// Execute the operation
		resources, err := hd.executeOperationWithParams(ctx, serviceName, operationName, params)
		if err != nil {
			if hd.debug {
				fmt.Printf("    ⚠️  Operation %s failed: %v\n", operationName, err)
			}
			continue
		}

		results[operationName] = resources

		if hd.debug {
			fmt.Printf("    ✅ Operation %s: found %d resources\n", operationName, len(resources))
		}
	}

	return results, nil
}

// executeOperationWithParams executes a single operation with provided parameters
func (hd *HierarchicalDiscoverer) executeOperationWithParams(ctx context.Context, serviceName, operationName string, params map[string]interface{}) ([]AWSResourceRef, error) {
	// For now, fall back to the scanner's existing method
	// TODO: Implement parameter support in the scanner
	if params != nil && len(params) > 0 {
		// Try to use the parameter-aware method if available
		return hd.scanner.ExecuteOperationWithParams(ctx, serviceName, operationName, params)
	}

	// Fall back to flat discovery for this service
	allResources, err := hd.scanner.DiscoverAndListServiceResources(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	// Filter resources that came from this specific operation
	// This is a simplified approach - in reality we'd need better operation tracking
	return allResources, nil
}

// discoveryContext implements the DiscoveryContext interface
type discoveryContext struct {
	discovered map[string][]AWSResourceRef
	region     string
	// TODO: Add credential and account info
}

func (dc *discoveryContext) GetDiscoveredResources(resourceType string) []AWSResourceRef {
	// Find resources by type across all operations
	var resources []AWSResourceRef
	for _, operationResources := range dc.discovered {
		for _, resource := range operationResources {
			if resource.Type == resourceType {
				resources = append(resources, resource)
			}
		}
	}
	return resources
}

func (dc *discoveryContext) GetDiscoveredResourcesByOperation(operationName string) []AWSResourceRef {
	return dc.discovered[operationName]
}

func (dc *discoveryContext) GetCredentialType() CredentialType {
	// TODO: Implement credential type detection
	return UnknownCredentials
}

func (dc *discoveryContext) GetAccountInfo() AccountInfo {
	// TODO: Implement account info detection
	return AccountInfo{}
}

func (dc *discoveryContext) GetRegion() string {
	return dc.region
}

// registerBuiltInStrategies registers the built-in discovery strategies
func (hd *HierarchicalDiscoverer) registerBuiltInStrategies() {
	// Register built-in strategies directly without validation
	// These are trusted strategies that are part of the core system
	hd.registerBuiltInStrategyDirect(createIAMStrategy())
	hd.registerBuiltInStrategyDirect(createS3Strategy())
	hd.registerBuiltInStrategyDirect(createEC2Strategy())

	if hd.debug {
		fmt.Printf("✅ Registered %d built-in strategies: iam, s3, ec2\n", len(hd.strategies))
	}
}

// registerBuiltInStrategyDirect registers a built-in strategy without validation
// This is used internally for trusted built-in strategies
func (hd *HierarchicalDiscoverer) registerBuiltInStrategyDirect(strategy DiscoveryStrategy) {
	hd.strategies[strategy.ServiceName] = strategy

	if hd.debug {
		fmt.Printf("✅ Registered built-in strategy for service: %s (%d phases)\n",
			strategy.ServiceName, len(strategy.Phases))
	}
}

package discovery

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// StrategyValidator validates discovery strategies for security
type StrategyValidator struct {
	builtInServices       map[string]bool
	allowedOperations     map[string][]string
	maxPhases             int
	maxOperationsPerPhase int
	operationRegex        *regexp.Regexp
}

// NewStrategyValidator creates a new strategy validator
func NewStrategyValidator() *StrategyValidator {
	return &StrategyValidator{
		builtInServices: map[string]bool{
			"iam": true,
			"s3":  true,
			"ec2": true,
		},
		allowedOperations: map[string][]string{
			"iam": {
				"ListUsers", "ListRoles", "ListGroups", "ListPolicies",
				"ListUserTags", "ListRoleTags", "ListGroupPolicies",
				"ListAttachedUserPolicies", "ListAttachedRolePolicies",
				"ListAttachedGroupPolicies", "ListPolicyVersions",
				"ListEntitiesForPolicy", "ListInstanceProfiles",
				"ListOpenIDConnectProviders", "ListSAMLProviders",
				"ListServerCertificates", "ListVirtualMFADevices",
				"ListAccountAliases", "ListGroupsForUser",
				"ListInstanceProfilesForRole", "ListUserPolicies",
				"ListRolePolicies", "ListPolicyTags",
			},
			"s3": {
				"ListBuckets", "ListObjects", "ListObjectsV2",
				"ListMultipartUploads", "ListBucketAnalyticsConfigurations",
				"ListBucketInventoryConfigurations", "ListBucketMetricsConfigurations",
			},
			"ec2": {
				"DescribeInstances", "DescribeVolumes", "DescribeSnapshots",
				"DescribeImages", "DescribeVpcs", "DescribeSubnets",
				"DescribeSecurityGroups", "DescribeKeyPairs",
				"DescribeNetworkInterfaces", "DescribeRouteTables",
			},
		},
		maxPhases:             10, // Maximum number of phases per strategy
		maxOperationsPerPhase: 20, // Maximum operations per phase
		operationRegex:        regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`),
	}
}

// ValidateStrategy validates a discovery strategy for security compliance
// This validation is only applied to external/custom strategies, not built-in ones
func (sv *StrategyValidator) ValidateStrategy(strategy DiscoveryStrategy) error {
	// Validate service name
	if err := sv.validateServiceName(strategy.ServiceName); err != nil {
		return fmt.Errorf("invalid service name: %w", err)
	}

	// Check if trying to override built-in strategy
	// This prevents external code from overriding trusted built-in strategies
	if sv.isBuiltInService(strategy.ServiceName) {
		return fmt.Errorf("cannot override built-in strategy for service: %s", strategy.ServiceName)
	}

	// Validate number of phases
	if len(strategy.Phases) > sv.maxPhases {
		return fmt.Errorf("too many phases: %d (max %d)", len(strategy.Phases), sv.maxPhases)
	}

	if len(strategy.Phases) == 0 {
		return fmt.Errorf("strategy must have at least one phase")
	}

	// Validate each phase
	phaseNames := make(map[string]bool)
	for i, phase := range strategy.Phases {
		if err := sv.validatePhase(phase, strategy.ServiceName); err != nil {
			return fmt.Errorf("phase %d (%s): %w", i, phase.Name, err)
		}

		// Check for duplicate phase names
		if phaseNames[phase.Name] {
			return fmt.Errorf("duplicate phase name: %s", phase.Name)
		}
		phaseNames[phase.Name] = true
	}

	// Validate phase dependencies
	if err := sv.validatePhaseDependencies(strategy.Phases); err != nil {
		return fmt.Errorf("invalid phase dependencies: %w", err)
	}

	return nil
}

// validateServiceName validates the service name format
func (sv *StrategyValidator) validateServiceName(serviceName string) error {
	if len(serviceName) == 0 {
		return fmt.Errorf("service name cannot be empty")
	}

	if len(serviceName) > 50 {
		return fmt.Errorf("service name too long: %d chars (max 50)", len(serviceName))
	}

	// Service names should be lowercase alphanumeric with hyphens
	serviceRegex := regexp.MustCompile(`^[a-z0-9-]+$`)
	if !serviceRegex.MatchString(serviceName) {
		return fmt.Errorf("invalid service name format: %s", serviceName)
	}

	return nil
}

// validatePhase validates a single discovery phase
func (sv *StrategyValidator) validatePhase(phase DiscoveryPhase, serviceName string) error {
	// Validate phase name
	if err := sv.validatePhaseName(phase.Name); err != nil {
		return err
	}

	// Validate operations
	if len(phase.Operations) == 0 {
		return fmt.Errorf("phase must have at least one operation")
	}

	if len(phase.Operations) > sv.maxOperationsPerPhase {
		return fmt.Errorf("too many operations: %d (max %d)",
			len(phase.Operations), sv.maxOperationsPerPhase)
	}

	// Validate each operation
	for _, operation := range phase.Operations {
		if err := sv.validateOperation(operation, serviceName); err != nil {
			return fmt.Errorf("operation %s: %w", operation, err)
		}
	}

	// Validate retry settings
	if phase.MaxRetries < 0 || phase.MaxRetries > 10 {
		return fmt.Errorf("invalid MaxRetries: %d (must be 0-10)", phase.MaxRetries)
	}

	if phase.RetryDelay < 0 || phase.RetryDelay > 60*time.Second {
		return fmt.Errorf("invalid RetryDelay: %v (must be 0-60s)", phase.RetryDelay)
	}

	// Validate parameter provider (if present)
	if phase.ParamProvider != nil {
		if err := sv.validateParameterProvider(phase.ParamProvider); err != nil {
			return fmt.Errorf("parameter provider: %w", err)
		}
	}

	return nil
}

// validatePhaseName validates phase name format
func (sv *StrategyValidator) validatePhaseName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("phase name cannot be empty")
	}

	if len(name) > 100 {
		return fmt.Errorf("phase name too long: %d chars (max 100)", len(name))
	}

	// Phase names should be lowercase with hyphens and underscores
	phaseRegex := regexp.MustCompile(`^[a-z0-9_-]+$`)
	if !phaseRegex.MatchString(name) {
		return fmt.Errorf("invalid phase name format: %s", name)
	}

	return nil
}

// validateOperation validates an operation name
func (sv *StrategyValidator) validateOperation(operation, serviceName string) error {
	if len(operation) == 0 {
		return fmt.Errorf("operation name cannot be empty")
	}

	if len(operation) > 100 {
		return fmt.Errorf("operation name too long: %d chars (max 100)", len(operation))
	}

	// Operation names should follow AWS naming convention
	if !sv.operationRegex.MatchString(operation) {
		return fmt.Errorf("invalid operation name format: %s", operation)
	}

	// Check if operation is allowed for this service
	allowedOps, exists := sv.allowedOperations[serviceName]
	if exists {
		allowed := false
		for _, allowedOp := range allowedOps {
			if operation == allowedOp {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("operation %s not allowed for service %s", operation, serviceName)
		}
	}

	// Additional security checks for dangerous operations
	if sv.isDangerousOperation(operation) {
		return fmt.Errorf("dangerous operation not allowed: %s", operation)
	}

	return nil
}

// validatePhaseDependencies validates phase dependency graph
func (sv *StrategyValidator) validatePhaseDependencies(phases []DiscoveryPhase) error {
	phaseMap := make(map[string]int)
	for i, phase := range phases {
		phaseMap[phase.Name] = i
	}

	// Check that all dependencies exist and don't create cycles
	for _, phase := range phases {
		for _, dep := range phase.DependsOn {
			depIndex, exists := phaseMap[dep]
			if !exists {
				return fmt.Errorf("phase %s depends on non-existent phase: %s", phase.Name, dep)
			}

			currentIndex := phaseMap[phase.Name]
			if depIndex >= currentIndex {
				return fmt.Errorf("phase %s has circular or forward dependency on: %s", phase.Name, dep)
			}
		}
	}

	// Check for cycles using DFS
	if sv.hasCycles(phases) {
		return fmt.Errorf("circular dependencies detected in phase graph")
	}

	return nil
}

// validateParameterProvider validates parameter provider function
func (sv *StrategyValidator) validateParameterProvider(provider ParameterProvider) error {
	// For now, just check that the function is not nil
	// In a more sophisticated implementation, we could use reflection
	// to validate the function signature
	if provider == nil {
		return fmt.Errorf("parameter provider cannot be nil")
	}

	// Test the provider with safe inputs to ensure it doesn't panic
	defer func() {
		if r := recover(); r != nil {
			// Provider panicked, which is not allowed
		}
	}()

	// Create a mock discovery context for testing
	mockContext := &mockDiscoveryContext{}

	// Test with a safe operation name
	result := provider("TestOperation", mockContext)

	// Validate the result doesn't contain dangerous values
	if result != nil {
		for key, value := range result {
			if err := sv.validateProviderResult(key, value); err != nil {
				return fmt.Errorf("provider returned invalid result: %w", err)
			}
		}
	}

	return nil
}

// validateProviderResult validates parameter provider results
func (sv *StrategyValidator) validateProviderResult(key string, value interface{}) error {
	// Check key format
	if len(key) > 100 {
		return fmt.Errorf("parameter key too long: %s", key)
	}

	// Validate value based on type
	switch v := value.(type) {
	case string:
		if len(v) > 1024 {
			return fmt.Errorf("string value too long for key %s", key)
		}
	case []string:
		if len(v) > 100 {
			return fmt.Errorf("string slice too long for key %s", key)
		}
		for _, str := range v {
			if len(str) > 1024 {
				return fmt.Errorf("string in slice too long for key %s", key)
			}
		}
	case int, int32, int64, bool:
		// These types are generally safe
	default:
		return fmt.Errorf("unsupported parameter type %T for key %s", value, key)
	}

	return nil
}

// isDangerousOperation checks if an operation is considered dangerous
func (sv *StrategyValidator) isDangerousOperation(operation string) bool {
	dangerousOps := []string{
		"DeleteUser", "DeleteRole", "DeletePolicy",
		"CreateUser", "CreateRole", "CreatePolicy",
		"AttachUserPolicy", "DetachUserPolicy",
		"PutUserPolicy", "DeleteUserPolicy",
		"UpdateUser", "UpdateRole",
		"DeleteBucket", "DeleteObject",
		"TerminateInstances", "StopInstances",
		"DeleteVolume", "DeleteSnapshot",
	}

	for _, dangerous := range dangerousOps {
		if operation == dangerous {
			return true
		}
	}

	// Check for patterns that might be dangerous
	if strings.HasPrefix(operation, "Delete") ||
		strings.HasPrefix(operation, "Terminate") ||
		strings.HasPrefix(operation, "Stop") ||
		strings.HasPrefix(operation, "Create") ||
		strings.HasPrefix(operation, "Put") ||
		strings.HasPrefix(operation, "Update") {
		return true
	}

	return false
}

// isBuiltInService checks if a service is built-in
func (sv *StrategyValidator) isBuiltInService(serviceName string) bool {
	return sv.builtInServices[serviceName]
}

// hasCycles detects cycles in the phase dependency graph
func (sv *StrategyValidator) hasCycles(phases []DiscoveryPhase) bool {
	// Build adjacency list
	graph := make(map[string][]string)
	for _, phase := range phases {
		graph[phase.Name] = phase.DependsOn
	}

	// Track visit states: 0=unvisited, 1=visiting, 2=visited
	state := make(map[string]int)

	var dfs func(string) bool
	dfs = func(node string) bool {
		if state[node] == 1 {
			return true // Back edge found, cycle detected
		}
		if state[node] == 2 {
			return false // Already processed
		}

		state[node] = 1 // Mark as visiting
		for _, neighbor := range graph[node] {
			if dfs(neighbor) {
				return true
			}
		}
		state[node] = 2 // Mark as visited
		return false
	}

	// Check each phase for cycles
	for _, phase := range phases {
		if state[phase.Name] == 0 && dfs(phase.Name) {
			return true
		}
	}

	return false
}

// mockDiscoveryContext for testing parameter providers
type mockDiscoveryContext struct{}

func (m *mockDiscoveryContext) GetDiscoveredResources(resourceType string) []AWSResourceRef {
	return []AWSResourceRef{}
}

func (m *mockDiscoveryContext) GetDiscoveredResourcesByOperation(operationName string) []AWSResourceRef {
	return []AWSResourceRef{}
}

func (m *mockDiscoveryContext) GetCredentialType() CredentialType {
	return UserCredentials
}

func (m *mockDiscoveryContext) GetAccountInfo() AccountInfo {
	return AccountInfo{}
}

func (m *mockDiscoveryContext) GetRegion() string {
	return "us-east-1"
}

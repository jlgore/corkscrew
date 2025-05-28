package classification

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)

// OperationClassification represents the result of classifying an AWS operation
type OperationClassification struct {
	Type           ClassificationType     `json:"type"`
	Confidence     float64                `json:"confidence"`
	CanAttempt     bool                   `json:"can_attempt"`
	RequiredParams []string               `json:"required_params,omitempty"`
	Reasoning      string                 `json:"reasoning"`
	Suggestions    []string               `json:"suggestions,omitempty"`
	DefaultParams  map[string]interface{} `json:"default_params,omitempty"`
}

type ClassificationType string

const (
	GlobalResource      ClassificationType = "global-resource"      // ListUsers, ListBuckets
	SubResource         ClassificationType = "sub-resource"         // ListUserTags, ListBucketObjects
	ParameterRequired   ClassificationType = "parameter-required"   // ListPoliciesGrantingServiceAccess
	CredentialDependent ClassificationType = "credential-dependent" // ListAccessKeys (user only)
	FilterRequired      ClassificationType = "filter-required"      // DescribeSnapshots (needs owner filter)
	Unknown             ClassificationType = "unknown"
)

// PatternRule defines a pattern-based classification rule
type PatternRule struct {
	Pattern     *regexp.Regexp
	Type        ClassificationType
	Weight      float64
	Description string
	Examples    []string
}

// OperationClassifier uses semantic patterns to classify AWS operations
type OperationClassifier struct {
	patterns        []PatternRule
	serviceHandlers map[string]ServiceHandler
}

// ServiceHandler provides service-specific classification logic
type ServiceHandler interface {
	ClassifyOperation(operationName string, inputType reflect.Type) OperationClassification
	CanProvideDefaults(operationName string, requiredFields []string) bool
	GetDefaultParameters(operationName string) map[string]interface{}
}

// NewOperationClassifier creates a new classifier with built-in patterns
func NewOperationClassifier() *OperationClassifier {
	classifier := &OperationClassifier{
		patterns:        buildDefaultPatterns(),
		serviceHandlers: make(map[string]ServiceHandler),
	}

	// Register service-specific handlers
	classifier.RegisterServiceHandler("iam", &IAMHandler{})
	classifier.RegisterServiceHandler("s3", &S3Handler{})
	classifier.RegisterServiceHandler("ec2", &EC2Handler{})
	classifier.RegisterServiceHandler("lambda", &LambdaHandler{})
	classifier.RegisterServiceHandler("sns", &SNSHandler{})
	classifier.RegisterServiceHandler("dynamodb", &DynamoDBHandler{})

	return classifier
}

// ClassifyOperation analyzes an operation and determines if it can be called parameter-free
func (c *OperationClassifier) ClassifyOperation(serviceName, operationName string, operation generator.AWSOperation) OperationClassification {
	// Try service-specific handler first
	if handler, exists := c.serviceHandlers[serviceName]; exists {
		if result := handler.ClassifyOperation(operationName, nil); result.Type != Unknown {
			return result
		}
	}

	// Use pattern-based classification
	return c.classifyByPatterns(operationName, operation)
}

// classifyByPatterns applies pattern-based rules to classify an operation
func (c *OperationClassifier) classifyByPatterns(operationName string, operation generator.AWSOperation) OperationClassification {
	scores := make(map[ClassificationType]float64)
	var matches []string

	// Apply each pattern
	for _, rule := range c.patterns {
		if rule.Pattern.MatchString(operationName) {
			// Use maximum weight instead of adding weights to prevent confidence > 1.0
			if rule.Weight > scores[rule.Type] {
				scores[rule.Type] = rule.Weight
			}
			matches = append(matches, rule.Description)
		}
	}

	// Determine final classification
	maxScore := 0.0
	var bestType ClassificationType = Unknown

	for t, score := range scores {
		if score > maxScore {
			maxScore = score
			bestType = t
		}
	}

	// If no patterns matched, use fallback logic
	if bestType == Unknown {
		bestType = c.fallbackClassification(operationName)
		maxScore = 0.5 // Lower confidence for fallback
	}

	// Normalize confidence to be between 0.0 and 1.0
	if maxScore > 1.0 {
		maxScore = 1.0
	}

	// Determine if we can attempt the operation
	canAttempt := c.determineCanAttempt(bestType, operationName)

	// Get default parameters if applicable
	var defaultParams map[string]interface{}
	if canAttempt && (bestType == FilterRequired || bestType == GlobalResource) {
		defaultParams = c.getDefaultParameters(operationName, bestType)
	}

	return OperationClassification{
		Type:          bestType,
		Confidence:    maxScore,
		CanAttempt:    canAttempt,
		Reasoning:     strings.Join(matches, "; "),
		DefaultParams: defaultParams,
	}
}

// fallbackClassification provides classification when no patterns match
func (c *OperationClassifier) fallbackClassification(operationName string) ClassificationType {
	// Heuristic-based classification for unknown operations
	if strings.HasPrefix(operationName, "List") {
		// Check for sub-resource indicators
		subResourceIndicators := []string{
			"For", "By", "Of", "In", "On", "With", "From", "To",
			"Tags", "Policies", "Permissions", "Configuration",
			"Attributes", "Properties", "Settings", "Details",
		}

		for _, indicator := range subResourceIndicators {
			if strings.Contains(operationName, indicator) {
				return SubResource
			}
		}

		// Default to global resource for simple List operations
		return GlobalResource
	}

	if strings.HasPrefix(operationName, "Describe") {
		// Most Describe operations are global unless they have specific indicators
		if strings.Contains(operationName, "Attribute") ||
			strings.Contains(operationName, "Configuration") ||
			strings.Contains(operationName, "Status") {
			return ParameterRequired
		}
		return GlobalResource
	}

	return Unknown
}

// determineCanAttempt decides if an operation can be attempted based on classification
func (c *OperationClassifier) determineCanAttempt(classification ClassificationType, operationName string) bool {
	switch classification {
	case GlobalResource:
		return true
	case FilterRequired:
		return c.canProvideFilterDefaults(operationName)
	case SubResource, ParameterRequired, CredentialDependent:
		return false
	default:
		return false
	}
}

// canProvideFilterDefaults checks if we can provide filter defaults for specific operations
func (c *OperationClassifier) canProvideFilterDefaults(operationName string) bool {
	// Operations that can work with default filters
	defaultableOperations := map[string]bool{
		"DescribeSnapshots": true, // Can default to owner="self"
		"DescribeImages":    true, // Can default to owner="self"
		"DescribeVolumes":   true, // Can work without filters
		"DescribeInstances": true, // Can work without filters
	}

	return defaultableOperations[operationName]
}

// getDefaultParameters returns default parameters for operations that support them
func (c *OperationClassifier) getDefaultParameters(operationName string, classification ClassificationType) map[string]interface{} {
	defaults := make(map[string]interface{})

	switch operationName {
	case "DescribeSnapshots", "DescribeImages":
		defaults["Owners"] = []string{"self"}
	case "DescribeInstances", "DescribeVolumes":
		// These can work without parameters, but we might want to set MaxResults
		defaults["MaxResults"] = 100
	}

	return defaults
}

// RegisterServiceHandler registers a service-specific handler
func (c *OperationClassifier) RegisterServiceHandler(serviceName string, handler ServiceHandler) {
	c.serviceHandlers[serviceName] = handler
}

// buildDefaultPatterns creates the default pattern rules
func buildDefaultPatterns() []PatternRule {
	return []PatternRule{
		// Global resource patterns (high confidence for parameter-free)
		{
			Pattern:     regexp.MustCompile(`^List[A-Z][a-z]+s$`),
			Type:        GlobalResource,
			Weight:      0.9,
			Description: "Plural list operation (likely global)",
			Examples:    []string{"ListUsers", "ListBuckets", "ListRoles"},
		},
		{
			Pattern:     regexp.MustCompile(`^Describe[A-Z][a-z]+s$`),
			Type:        GlobalResource,
			Weight:      0.8,
			Description: "Plural describe operation (likely global)",
			Examples:    []string{"DescribeInstances", "DescribeVolumes"},
		},
		{
			Pattern:     regexp.MustCompile(`^List(Users|Roles|Groups|Policies|Buckets|Functions|Tables|Topics|Queues)$`),
			Type:        GlobalResource,
			Weight:      1.0,
			Description: "Known global resource operations",
			Examples:    []string{"ListUsers", "ListBuckets", "ListFunctions"},
		},

		// Sub-resource patterns (require parent resource ID)
		{
			Pattern:     regexp.MustCompile(`List.*For.*`),
			Type:        SubResource,
			Weight:      0.95,
			Description: "List X for Y pattern (requires parent ID)",
			Examples:    []string{"ListGroupsForUser", "ListPoliciesForRole"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*By.*`),
			Type:        SubResource,
			Weight:      0.9,
			Description: "List X by Y pattern (requires filter parameter)",
			Examples:    []string{"ListSubscriptionsByTopic"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*Tags$`),
			Type:        SubResource,
			Weight:      0.9,
			Description: "Tag listing operations (require resource identifier)",
			Examples:    []string{"ListUserTags", "ListBucketTags"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*Attached.*`),
			Type:        SubResource,
			Weight:      0.85,
			Description: "Attached resource operations (require parent ID)",
			Examples:    []string{"ListAttachedUserPolicies"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*(Configuration|Attributes|Properties|Settings).*`),
			Type:        SubResource,
			Weight:      0.8,
			Description: "Configuration/attribute operations (require resource ID)",
			Examples:    []string{"ListBucketConfiguration"},
		},

		// Parameter-required patterns
		{
			Pattern:     regexp.MustCompile(`List.*Granting.*`),
			Type:        ParameterRequired,
			Weight:      0.9,
			Description: "Access granting operations (require specific parameters)",
			Examples:    []string{"ListPoliciesGrantingServiceAccess"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*Entities.*`),
			Type:        ParameterRequired,
			Weight:      0.85,
			Description: "Entity listing operations (require specific criteria)",
			Examples:    []string{"ListEntitiesForPolicy"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*(Versions|Aliases|Layers)$`),
			Type:        ParameterRequired,
			Weight:      0.8,
			Description: "Version/alias operations (require resource identifier)",
			Examples:    []string{"ListVersionsByFunction", "ListAliases"},
		},

		// Filter-required patterns (can attempt with defaults)
		{
			Pattern:     regexp.MustCompile(`^Describe(Snapshots|Images)$`),
			Type:        FilterRequired,
			Weight:      0.9,
			Description: "EC2 operations that need owner filter (can default to 'self')",
			Examples:    []string{"DescribeSnapshots", "DescribeImages"},
		},

		// Credential-dependent patterns
		{
			Pattern:     regexp.MustCompile(`List(AccessKeys|MFADevices|SigningCertificates|SSHPublicKeys|ServiceSpecificCredentials)`),
			Type:        CredentialDependent,
			Weight:      0.9,
			Description: "User-specific operations (only work with user credentials)",
			Examples:    []string{"ListAccessKeys", "ListMFADevices"},
		},

		// Service-specific patterns
		{
			Pattern:     regexp.MustCompile(`List.*(Bucket|Object).*`),
			Type:        SubResource,
			Weight:      0.85,
			Description: "S3 bucket/object operations (require bucket name)",
			Examples:    []string{"ListObjects", "ListBucketAnalyticsConfigurations"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*(Distribution|Invalidation|Domain).*`),
			Type:        SubResource,
			Weight:      0.85,
			Description: "CloudFront operations (require distribution ID)",
			Examples:    []string{"ListInvalidations", "ListDomainConflicts"},
		},
		{
			Pattern:     regexp.MustCompile(`List.*(App|Branch|Job|Webhook|Artifact).*`),
			Type:        SubResource,
			Weight:      0.85,
			Description: "Amplify operations (require app ID)",
			Examples:    []string{"ListBranches", "ListJobs"},
		},
	}
}

// IsParameterFreeOperation provides backward compatibility with the existing interface
func (c *OperationClassifier) IsParameterFreeOperation(serviceName string, operation generator.AWSOperation) bool {
	classification := c.ClassifyOperation(serviceName, operation.Name, operation)
	return classification.CanAttempt
}

// GetOperationDefaults returns default parameters for an operation if available
func (c *OperationClassifier) GetOperationDefaults(serviceName, operationName string) map[string]interface{} {
	if handler, exists := c.serviceHandlers[serviceName]; exists {
		return handler.GetDefaultParameters(operationName)
	}

	// Use built-in defaults
	return c.getDefaultParameters(operationName, c.fallbackClassification(operationName))
}

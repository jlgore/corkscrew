package classification

import (
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
}

// Note: ServiceHandler interface removed in favor of dynamic pattern-based classification
// The OperationClassifier now handles all classification logic dynamically

// ConfigurationOperation represents a configuration API call needed to enrich a resource
type ConfigurationOperation struct {
	Name           string            `json:"name"`            // e.g., "GetBucketEncryption"
	ParameterName  string            `json:"parameter_name"`  // e.g., "Bucket" 
	ParameterField string            `json:"parameter_field"` // e.g., "Name" from bucket.Name
	ConfigKey      string            `json:"config_key"`      // e.g., "Encryption" for storing in raw_data
	Required       bool              `json:"required"`        // Whether this config is critical
	DefaultParams  map[string]string `json:"default_params"`  // Any additional default parameters
}

// NewOperationClassifier creates a new classifier with built-in patterns
func NewOperationClassifier() *OperationClassifier {
	classifier := &OperationClassifier{
		patterns: buildDefaultPatterns(),
	}

	return classifier
}

// ClassifyOperation analyzes an operation and determines if it can be called parameter-free
func (c *OperationClassifier) ClassifyOperation(serviceName, operationName string, operation generator.AWSOperation) OperationClassification {
	// Use dynamic pattern-based classification for all services
	// This replaces the previous hardcoded service handlers with a more flexible approach
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

// Note: RegisterServiceHandler method removed - using dynamic pattern-based classification

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
	// Use built-in defaults based on dynamic pattern classification
	classification := c.fallbackClassification(operationName)
	return c.getDefaultParameters(operationName, classification)
}

// GetConfigurationOperations dynamically discovers configuration operations for a resource type
func (c *OperationClassifier) GetConfigurationOperations(serviceName, resourceType string) []ConfigurationOperation {
	var operations []ConfigurationOperation
	
	// Define common configuration operation patterns by service
	configPatterns := map[string][]string{
		"s3": {
			"GetBucketEncryption", "GetBucketVersioning", "GetPublicAccessBlock", 
			"GetBucketPolicy", "GetBucketLifecycleConfiguration", "GetBucketLogging",
			"GetBucketNotificationConfiguration", "GetBucketWebsite", "GetBucketCors",
		},
		"ec2": {
			"DescribeInstanceAttribute", "DescribeVolumeAttribute", "DescribeSnapshotAttribute",
			"DescribeImageAttribute", "GetGroupsForCapacityReservation", "DescribeInstanceTypes",
		},
		"lambda": {
			"GetFunctionConfiguration", "GetFunctionCodeSigningConfig", "GetFunction",
			"GetLayerVersionPolicy", "GetAlias", "GetEventSourceMapping",
		},
		"iam": {
			"GetUser", "GetRole", "GetPolicy", "GetGroup", "GetInstanceProfile",
			"GetUserPolicy", "GetRolePolicy", "GetGroupPolicy",
		},
		"dynamodb": {
			"DescribeTable", "DescribeTimeToLive", "DescribeBackup", "DescribeContinuousBackups",
			"DescribeContributorInsights", "ListTagsOfResource",
		},
		"sns": {
			"GetTopicAttributes", "GetSubscriptionAttributes", "GetPlatformApplicationAttributes",
		},
	}
	
	// Get service-specific patterns, or use generic ones if service not found
	patterns := configPatterns[serviceName]
	if patterns == nil {
		// Generic patterns for unknown services
		patterns = []string{
			"Get" + resourceType, "Describe" + resourceType, "Get" + resourceType + "Attributes",
			"Get" + resourceType + "Configuration", "Describe" + resourceType + "Attributes",
		}
	}
	
	// Create configuration operations based on patterns
	for _, pattern := range patterns {
		// Determine parameter name based on common AWS conventions
		parameterName := c.getParameterNameForResource(serviceName, resourceType)
		
		operations = append(operations, ConfigurationOperation{
			Name:           pattern,
			ParameterName:  parameterName,
			ParameterField: c.getParameterFieldForResource(serviceName, resourceType),
			ConfigKey:      c.getConfigKeyFromOperation(pattern),
			Required:       c.isConfigurationRequired(pattern),
		})
	}
	
	return operations
}

// Helper methods for dynamic configuration operation discovery

func (c *OperationClassifier) getParameterNameForResource(serviceName, resourceType string) string {
	// Common parameter name patterns by service
	parameterNames := map[string]map[string]string{
		"s3": {"Bucket": "Bucket", "Object": "Bucket"}, // S3 uses "Bucket" parameter
		"ec2": {"Instance": "InstanceId", "Volume": "VolumeId", "SecurityGroup": "GroupId"},
		"lambda": {"Function": "FunctionName", "Layer": "LayerName"},
		"iam": {"User": "UserName", "Role": "RoleName", "Policy": "PolicyArn", "Group": "GroupName"},
		"dynamodb": {"Table": "TableName"},
		"sns": {"Topic": "TopicArn", "Subscription": "SubscriptionArn"},
	}
	
	if serviceParams, exists := parameterNames[serviceName]; exists {
		if param, exists := serviceParams[resourceType]; exists {
			return param
		}
	}
	
	// Default: use resource type + "Name" or "Id"
	if resourceType == "Bucket" || resourceType == "Function" || resourceType == "Table" {
		return resourceType + "Name"
	}
	return resourceType + "Id"
}

func (c *OperationClassifier) getParameterFieldForResource(serviceName, resourceType string) string {
	// Most AWS resources use "Name" or "Id" fields
	commonIdFields := map[string]string{
		"Bucket": "Name",
		"Function": "FunctionName", 
		"Table": "TableName",
		"Topic": "TopicArn",
		"User": "UserName",
		"Role": "RoleName",
		"Group": "GroupName",
		"Policy": "Arn",
	}
	
	if field, exists := commonIdFields[resourceType]; exists {
		return field
	}
	
	// Default to common patterns
	return "Name"
}

func (c *OperationClassifier) getConfigKeyFromOperation(operationName string) string {
	// Extract config key from operation name
	configKey := strings.TrimPrefix(operationName, "Get")
	configKey = strings.TrimPrefix(configKey, "Describe")
	
	// Handle special cases
	specialCases := map[string]string{
		"BucketEncryption": "ServerSideEncryptionConfiguration",
		"PublicAccessBlock": "PublicAccessBlockConfiguration", 
		"BucketLifecycleConfiguration": "LifecycleConfiguration",
		"BucketNotificationConfiguration": "NotificationConfiguration",
	}
	
	if special, exists := specialCases[configKey]; exists {
		return special
	}
	
	return configKey
}

func (c *OperationClassifier) isConfigurationRequired(operationName string) bool {
	// Core security and compliance configurations are typically required
	requiredConfigs := []string{
		"GetBucketEncryption", "GetBucketVersioning", "GetPublicAccessBlock",
		"DescribeInstanceAttribute", "GetFunctionConfiguration",
		"DescribeTable", "GetTopicAttributes",
	}
	
	for _, required := range requiredConfigs {
		if operationName == required {
			return true
		}
	}
	
	// Default to false (optional configuration)
	return false
}

package compliance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// QueryPack represents a collection of compliance queries with hierarchical organization
type QueryPack struct {
	Metadata    PackMetadata      `yaml:"metadata" json:"metadata"`
	Queries     []ComplianceQuery `yaml:"queries" json:"queries"`
	Parameters  []PackParameter   `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Includes    []string          `yaml:"includes,omitempty" json:"includes,omitempty"`     // Other packs to include
	DependsOn   []string          `yaml:"depends_on,omitempty" json:"depends_on,omitempty"` // Pack dependencies

	// Runtime fields (not serialized)
	Namespace   string `yaml:"-" json:"-"` // publisher/framework format
	LoadedFrom  string `yaml:"-" json:"-"` // file path where pack was loaded
	LoadedAt    time.Time `yaml:"-" json:"-"`
}

// PackMetadata contains metadata about a query pack with support for hierarchical organization
type PackMetadata struct {
	APIVersion     string            `yaml:"apiVersion" json:"apiVersion"`
	Kind           string            `yaml:"kind" json:"kind"`
	Name           string            `yaml:"name" json:"name"`
	Namespace      string            `yaml:"namespace,omitempty" json:"namespace,omitempty"` // publisher/framework
	Version        string            `yaml:"version" json:"version"`
	Description    string            `yaml:"description" json:"description"`
	Author         string            `yaml:"author,omitempty" json:"author,omitempty"`
	Maintainers    []string          `yaml:"maintainers,omitempty" json:"maintainers,omitempty"`
	Tags           []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Provider       string            `yaml:"provider" json:"provider"` // aws, azure, gcp, kubernetes, etc.
	Resources      []string          `yaml:"resources,omitempty" json:"resources,omitempty"` // s3, ec2, storage-account, etc.
	Frameworks     []string          `yaml:"frameworks,omitempty" json:"frameworks,omitempty"` // ccc, iso27001, nist, etc.
	MinEngineVersion string          `yaml:"min_engine_version,omitempty" json:"min_engine_version,omitempty"`
	CreatedAt      time.Time         `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt      time.Time         `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations    map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// ComplianceQuery represents a single compliance check with framework mappings
type ComplianceQuery struct {
	ID              string               `yaml:"id" json:"id"`
	Title           string               `yaml:"title" json:"title"`
	Description     string               `yaml:"description" json:"description"`
	Objective       string               `yaml:"objective,omitempty" json:"objective,omitempty"`
	Severity        string               `yaml:"severity" json:"severity"` // CRITICAL, HIGH, MEDIUM, LOW, INFO
	Category        string               `yaml:"category" json:"category"` // security, cost, operations, governance, etc.
	ControlFamily   string               `yaml:"control_family,omitempty" json:"control_family,omitempty"`
	NISTCSF         string               `yaml:"nist_csf,omitempty" json:"nist_csf,omitempty"`
	Tags            []string             `yaml:"tags,omitempty" json:"tags,omitempty"`
	QueryFile       string               `yaml:"query_file" json:"query_file"`
	SQL             string               `yaml:"-" json:"-"` // Loaded from file
	Parameters      []string             `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	DependsOn       []string             `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Threats         []string             `yaml:"threats,omitempty" json:"threats,omitempty"`
	ControlMappings ControlMappings      `yaml:"control_mappings,omitempty" json:"control_mappings,omitempty"`
	TestRequirements []TestRequirement   `yaml:"test_requirements,omitempty" json:"test_requirements,omitempty"`
	Remediation     RemediationInfo      `yaml:"remediation,omitempty" json:"remediation,omitempty"`
	References      []Reference          `yaml:"references,omitempty" json:"references,omitempty"`
	Enabled         bool                 `yaml:"enabled" json:"enabled"`
	LastRun         *time.Time           `yaml:"-" json:"-"` // Runtime field
	LastResult      *QueryResult         `yaml:"-" json:"-"` // Runtime field
}

// ControlMappings maps controls to various compliance frameworks
type ControlMappings struct {
	CCM        []string `yaml:"CCM,omitempty" json:"CCM,omitempty"`
	ISO27001   []string `yaml:"ISO_27001,omitempty" json:"ISO_27001,omitempty"`
	NIST80053  []string `yaml:"NIST_800_53,omitempty" json:"NIST_800_53,omitempty"`
	SOC2       []string `yaml:"SOC2,omitempty" json:"SOC2,omitempty"`
	CISControls []string `yaml:"CIS_Controls,omitempty" json:"CIS_Controls,omitempty"`
	PCIDSS     []string `yaml:"PCI_DSS,omitempty" json:"PCI_DSS,omitempty"`
	HIPAA      []string `yaml:"HIPAA,omitempty" json:"HIPAA,omitempty"`
	GDPR       []string `yaml:"GDPR,omitempty" json:"GDPR,omitempty"`
	Custom     map[string][]string `yaml:"custom,omitempty" json:"custom,omitempty"`
}

// TestRequirement represents a specific test requirement for a control
type TestRequirement struct {
	ID        string   `yaml:"id" json:"id"`
	Text      string   `yaml:"text" json:"text"`
	TLPLevels []string `yaml:"tlp_levels,omitempty" json:"tlp_levels,omitempty"`
}

// PackParameter defines a parameter that can be used across queries with validation
type PackParameter struct {
	Name         string                 `yaml:"name" json:"name"`
	Description  string                 `yaml:"description" json:"description"`
	Type         string                 `yaml:"type" json:"type"` // string, int, bool, list, object
	Default      interface{}            `yaml:"default,omitempty" json:"default,omitempty"`
	Required     bool                   `yaml:"required" json:"required"`
	Validation   *ParameterValidation   `yaml:"validation,omitempty" json:"validation,omitempty"`
	Examples     []interface{}          `yaml:"examples,omitempty" json:"examples,omitempty"`
	EnumValues   []interface{}          `yaml:"enum_values,omitempty" json:"enum_values,omitempty"`
	Sensitive    bool                   `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`
}

// ParameterValidation defines validation rules for parameters
type ParameterValidation struct {
	MinLength    *int     `yaml:"min_length,omitempty" json:"min_length,omitempty"`
	MaxLength    *int     `yaml:"max_length,omitempty" json:"max_length,omitempty"`
	Pattern      string   `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	MinValue     *float64 `yaml:"min_value,omitempty" json:"min_value,omitempty"`
	MaxValue     *float64 `yaml:"max_value,omitempty" json:"max_value,omitempty"`
	Regex        *regexp.Regexp `yaml:"-" json:"-"` // Compiled pattern
}

// RemediationInfo provides guidance on fixing compliance issues with Terraform support
type RemediationInfo struct {
	Description    string            `yaml:"description" json:"description"`
	Steps          []string          `yaml:"steps,omitempty" json:"steps,omitempty"`
	Links          []string          `yaml:"links,omitempty" json:"links,omitempty"`
	TerraformModule string           `yaml:"terraform_module,omitempty" json:"terraform_module,omitempty"`
	TerraformCode  string            `yaml:"terraform_code,omitempty" json:"terraform_code,omitempty"`
	Automated      bool              `yaml:"automated,omitempty" json:"automated,omitempty"`
	Complexity     string            `yaml:"complexity,omitempty" json:"complexity,omitempty"` // LOW, MEDIUM, HIGH
	EstimatedTime  string            `yaml:"estimated_time,omitempty" json:"estimated_time,omitempty"`
	Risks          []string          `yaml:"risks,omitempty" json:"risks,omitempty"`
	Prerequisites  []string          `yaml:"prerequisites,omitempty" json:"prerequisites,omitempty"`
	AdditionalInfo map[string]string `yaml:"additional_info,omitempty" json:"additional_info,omitempty"`
}

// Reference provides external documentation links with enhanced metadata
type Reference struct {
	Name        string `yaml:"name" json:"name"`
	URL         string `yaml:"url" json:"url"`
	Type        string `yaml:"type" json:"type"` // documentation, blog, standard, regulation, etc.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Version     string `yaml:"version,omitempty" json:"version,omitempty"`
}

// QueryResult represents the result of executing a compliance query
type QueryResult struct {
	QueryID      string                   `json:"query_id"`
	Status       string                   `json:"status"` // PASS, FAIL, ERROR, SKIP
	Message      string                   `json:"message,omitempty"`
	ResourceCount int                     `json:"resource_count"`
	FailedCount   int                     `json:"failed_count"`
	PassedCount   int                     `json:"passed_count"`
	Resources     []map[string]interface{} `json:"resources,omitempty"`
	ExecutionTime time.Duration            `json:"execution_time"`
	Timestamp     time.Time                `json:"timestamp"`
	Error         string                   `json:"error,omitempty"`
}

// PackRegistry manages discovery and loading of query packs
type PackRegistry struct {
	SearchPaths []string
	packs       map[string]*QueryPack
	namespaces  map[string][]string // namespace -> pack names
}

// PackVersion represents version information for compatibility checking
type PackVersion struct {
	Major int
	Minor int
	Patch int
	Prerelease string
	Build      string
}

// ValidationError represents a validation error with context
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
	Rule    string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field '%s': %s (value: %v)", e.Field, e.Message, e.Value)
}

// LoadError represents an error during pack loading
type LoadError struct {
	Path    string
	Message string
	Cause   error
}

func (e LoadError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("failed to load pack from '%s': %s (caused by: %v)", e.Path, e.Message, e.Cause)
	}
	return fmt.Sprintf("failed to load pack from '%s': %s", e.Path, e.Message)
}

func (e LoadError) Unwrap() error {
	return e.Cause
}

// NewPackRegistry creates a new pack registry with default search paths
func NewPackRegistry() *PackRegistry {
	return &PackRegistry{
		SearchPaths: []string{
			"./packs",
			"~/.corkscrew/packs",
			"/etc/corkscrew/packs",
		},
		packs:      make(map[string]*QueryPack),
		namespaces: make(map[string][]string),
	}
}

// LoadPack loads a query pack from a file path
func (r *PackRegistry) LoadPack(path string) (*QueryPack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, LoadError{Path: path, Message: "failed to read file", Cause: err}
	}

	var pack QueryPack
	if err := yaml.Unmarshal(data, &pack); err != nil {
		return nil, LoadError{Path: path, Message: "failed to parse YAML", Cause: err}
	}

	// Set runtime fields
	pack.LoadedFrom = path
	pack.LoadedAt = time.Now()
	if pack.Metadata.Namespace != "" {
		pack.Namespace = pack.Metadata.Namespace
	}

	// Validate the pack
	if err := r.ValidatePack(&pack); err != nil {
		return nil, LoadError{Path: path, Message: "validation failed", Cause: err}
	}

	// Load SQL queries from files
	if err := r.loadQueryFiles(&pack, filepath.Dir(path)); err != nil {
		return nil, LoadError{Path: path, Message: "failed to load query files", Cause: err}
	}

	// Register the pack
	packKey := r.generatePackKey(&pack)
	r.packs[packKey] = &pack

	// Update namespace registry
	if pack.Namespace != "" {
		r.namespaces[pack.Namespace] = append(r.namespaces[pack.Namespace], pack.Metadata.Name)
	}

	return &pack, nil
}

// ValidatePack validates a query pack structure and content
func (r *PackRegistry) ValidatePack(pack *QueryPack) error {
	var errors []ValidationError

	// Validate metadata
	if pack.Metadata.Name == "" {
		errors = append(errors, ValidationError{Field: "metadata.name", Message: "name is required"})
	}
	if pack.Metadata.Version == "" {
		errors = append(errors, ValidationError{Field: "metadata.version", Message: "version is required"})
	}
	if pack.Metadata.Provider == "" {
		errors = append(errors, ValidationError{Field: "metadata.provider", Message: "provider is required"})
	}

	// Validate version format
	if pack.Metadata.Version != "" {
		if _, err := ParseVersion(pack.Metadata.Version); err != nil {
			errors = append(errors, ValidationError{
				Field: "metadata.version",
				Value: pack.Metadata.Version,
				Message: "invalid version format",
			})
		}
	}

	// Validate namespace format
	if pack.Metadata.Namespace != "" {
		if !isValidNamespace(pack.Metadata.Namespace) {
			errors = append(errors, ValidationError{
				Field: "metadata.namespace",
				Value: pack.Metadata.Namespace,
				Message: "invalid namespace format (should be publisher/framework)",
			})
		}
	}

	// Validate queries
	queryIDs := make(map[string]bool)
	for i, query := range pack.Queries {
		if query.ID == "" {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("queries[%d].id", i),
				Message: "query ID is required",
			})
		}
		if queryIDs[query.ID] {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("queries[%d].id", i),
				Value: query.ID,
				Message: "duplicate query ID",
			})
		}
		queryIDs[query.ID] = true

		if query.Title == "" {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("queries[%d].title", i),
				Message: "query title is required",
			})
		}
		if query.QueryFile == "" {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("queries[%d].query_file", i),
				Message: "query file is required",
			})
		}
		if !isValidSeverity(query.Severity) {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("queries[%d].severity", i),
				Value: query.Severity,
				Message: "invalid severity (must be CRITICAL, HIGH, MEDIUM, LOW, or INFO)",
			})
		}
	}

	// Validate parameters
	paramNames := make(map[string]bool)
	for i, param := range pack.Parameters {
		if param.Name == "" {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("parameters[%d].name", i),
				Message: "parameter name is required",
			})
		}
		if paramNames[param.Name] {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("parameters[%d].name", i),
				Value: param.Name,
				Message: "duplicate parameter name",
			})
		}
		paramNames[param.Name] = true

		if !isValidParameterType(param.Type) {
			errors = append(errors, ValidationError{
				Field: fmt.Sprintf("parameters[%d].type", i),
				Value: param.Type,
				Message: "invalid parameter type",
			})
		}

		// Validate parameter validation rules
		if param.Validation != nil {
			if err := r.validateParameterValidation(param.Validation, fmt.Sprintf("parameters[%d].validation", i)); err != nil {
				errors = append(errors, err...)
			}
		}
	}

	// Return combined errors
	if len(errors) > 0 {
		messages := make([]string, len(errors))
		for i, err := range errors {
			messages[i] = err.Error()
		}
		return fmt.Errorf("pack validation failed: %s", strings.Join(messages, "; "))
	}

	return nil
}

// validateParameterValidation validates parameter validation rules
func (r *PackRegistry) validateParameterValidation(validation *ParameterValidation, fieldPrefix string) []ValidationError {
	var errors []ValidationError

	if validation.Pattern != "" {
		if _, err := regexp.Compile(validation.Pattern); err != nil {
			errors = append(errors, ValidationError{
				Field: fieldPrefix + ".pattern",
				Value: validation.Pattern,
				Message: "invalid regex pattern",
			})
		} else {
			// Compile and store the regex for later use
			validation.Regex = regexp.MustCompile(validation.Pattern)
		}
	}

	if validation.MinLength != nil && *validation.MinLength < 0 {
		errors = append(errors, ValidationError{
			Field: fieldPrefix + ".min_length",
			Value: *validation.MinLength,
			Message: "min_length cannot be negative",
		})
	}

	if validation.MaxLength != nil && *validation.MaxLength < 0 {
		errors = append(errors, ValidationError{
			Field: fieldPrefix + ".max_length",
			Value: *validation.MaxLength,
			Message: "max_length cannot be negative",
		})
	}

	if validation.MinLength != nil && validation.MaxLength != nil && *validation.MinLength > *validation.MaxLength {
		errors = append(errors, ValidationError{
			Field: fieldPrefix,
			Message: "min_length cannot be greater than max_length",
		})
	}

	if validation.MinValue != nil && validation.MaxValue != nil && *validation.MinValue > *validation.MaxValue {
		errors = append(errors, ValidationError{
			Field: fieldPrefix,
			Message: "min_value cannot be greater than max_value",
		})
	}

	return errors
}

// loadQueryFiles loads SQL content from query files
func (r *PackRegistry) loadQueryFiles(pack *QueryPack, packDir string) error {
	for i := range pack.Queries {
		query := &pack.Queries[i]
		if query.QueryFile == "" {
			continue
		}

		filePath := filepath.Join(packDir, query.QueryFile)
		sqlContent, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read query file '%s' for query '%s': %w", query.QueryFile, query.ID, err)
		}

		query.SQL = string(sqlContent)
	}

	return nil
}

// generatePackKey generates a unique key for a pack
func (r *PackRegistry) generatePackKey(pack *QueryPack) string {
	if pack.Namespace != "" {
		return fmt.Sprintf("%s/%s:%s", pack.Namespace, pack.Metadata.Name, pack.Metadata.Version)
	}
	return fmt.Sprintf("%s:%s", pack.Metadata.Name, pack.Metadata.Version)
}

// GetPack retrieves a pack by name and optional version
func (r *PackRegistry) GetPack(name, version string) (*QueryPack, error) {
	if version == "" {
		version = "latest"
	}

	key := fmt.Sprintf("%s:%s", name, version)
	if pack, exists := r.packs[key]; exists {
		return pack, nil
	}

	// If looking for latest, find the highest version
	if version == "latest" {
		return r.getLatestVersion(name)
	}

	return nil, fmt.Errorf("pack '%s:%s' not found", name, version)
}

// getLatestVersion finds the latest version of a pack
func (r *PackRegistry) getLatestVersion(name string) (*QueryPack, error) {
	var latestPack *QueryPack
	var latestVersion *PackVersion

	for key, pack := range r.packs {
		if !strings.HasPrefix(key, name+":") {
			continue
		}

		version, err := ParseVersion(pack.Metadata.Version)
		if err != nil {
			continue
		}

		if latestVersion == nil || version.Compare(latestVersion) > 0 {
			latestVersion = version
			latestPack = pack
		}
	}

	if latestPack == nil {
		return nil, fmt.Errorf("no versions found for pack '%s'", name)
	}

	return latestPack, nil
}

// ListPacks returns all loaded packs
func (r *PackRegistry) ListPacks() map[string]*QueryPack {
	return r.packs
}

// ListNamespaces returns all registered namespaces
func (r *PackRegistry) ListNamespaces() map[string][]string {
	return r.namespaces
}

// ParseVersion parses a semantic version string
func ParseVersion(version string) (*PackVersion, error) {
	// Simple semantic version parsing (major.minor.patch)
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid version format: %s", version)
	}

	var major, minor, patch int
	if _, err := fmt.Sscanf(parts[0], "%d", &major); err != nil {
		return nil, fmt.Errorf("invalid major version: %s", parts[0])
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minor); err != nil {
		return nil, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &patch); err != nil {
		return nil, fmt.Errorf("invalid patch version: %s", parts[2])
	}

	return &PackVersion{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

// Compare compares two versions (-1 if v < other, 0 if equal, 1 if v > other)
func (v *PackVersion) Compare(other *PackVersion) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// String returns the string representation of a version
func (v *PackVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ValidateParameter validates a parameter value against its definition
func (p *PackParameter) ValidateParameter(value interface{}) error {
	// Check required
	if p.Required && value == nil {
		return ValidationError{
			Field: p.Name,
			Message: "parameter is required",
		}
	}

	if value == nil {
		return nil // Optional parameter not provided
	}

	// Type checking
	switch p.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: "expected string value",
			}
		}
	case "int":
		switch v := value.(type) {
		case int, int32, int64:
			// Valid
		case float64:
			if v != float64(int(v)) {
				return ValidationError{
					Field: p.Name,
					Value: value,
					Message: "expected integer value",
				}
			}
		default:
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: "expected integer value",
			}
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: "expected boolean value",
			}
		}
	case "list":
		if _, ok := value.([]interface{}); !ok {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: "expected list value",
			}
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: "expected object value",
			}
		}
	}

	// Apply validation rules
	if p.Validation != nil {
		return p.applyValidationRules(value)
	}

	// Check enum values
	if len(p.EnumValues) > 0 {
		for _, enumValue := range p.EnumValues {
			if value == enumValue {
				return nil
			}
		}
		return ValidationError{
			Field: p.Name,
			Value: value,
			Message: fmt.Sprintf("value must be one of: %v", p.EnumValues),
		}
	}

	return nil
}

// applyValidationRules applies validation rules to a parameter value
func (p *PackParameter) applyValidationRules(value interface{}) error {
	validation := p.Validation

	// String validations
	if strValue, ok := value.(string); ok {
		if validation.MinLength != nil && len(strValue) < *validation.MinLength {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: fmt.Sprintf("string length must be at least %d", *validation.MinLength),
			}
		}
		if validation.MaxLength != nil && len(strValue) > *validation.MaxLength {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: fmt.Sprintf("string length must be at most %d", *validation.MaxLength),
			}
		}
		if validation.Regex != nil && !validation.Regex.MatchString(strValue) {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: fmt.Sprintf("string must match pattern: %s", validation.Pattern),
			}
		}
	}

	// Numeric validations
	if numValue, ok := value.(float64); ok {
		if validation.MinValue != nil && numValue < *validation.MinValue {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: fmt.Sprintf("value must be at least %f", *validation.MinValue),
			}
		}
		if validation.MaxValue != nil && numValue > *validation.MaxValue {
			return ValidationError{
				Field: p.Name,
				Value: value,
				Message: fmt.Sprintf("value must be at most %f", *validation.MaxValue),
			}
		}
	}

	return nil
}

// Helper functions

func isValidNamespace(namespace string) bool {
	parts := strings.Split(namespace, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func isValidSeverity(severity string) bool {
	validSeverities := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO"}
	for _, valid := range validSeverities {
		if severity == valid {
			return true
		}
	}
	return false
}

func isValidParameterType(paramType string) bool {
	validTypes := []string{"string", "int", "bool", "list", "object"}
	for _, valid := range validTypes {
		if paramType == valid {
			return true
		}
	}
	return false
}
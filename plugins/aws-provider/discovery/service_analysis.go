package discovery

import (
	"time"
)

// ServiceAnalysis represents comprehensive analysis of an AWS service
type ServiceAnalysis struct {
	ServiceName     string                       `json:"service_name"`
	Operations      []OperationAnalysis          `json:"operations"`
	ResourceTypes   []ResourceTypeAnalysis       `json:"resource_types"`
	Relationships   []ResourceRelationship       `json:"relationships"`
	PaginationInfo  map[string]PaginationPattern `json:"pagination_info"`
	LastAnalyzed    time.Time                    `json:"last_analyzed"`
}

// OperationAnalysis represents detailed analysis of a service operation
type OperationAnalysis struct {
	Name            string   `json:"name"`
	Type            string   `json:"type"` // "List", "Describe", "Get", "Create", "Update", "Delete", etc.
	InputType       string   `json:"input_type"`
	OutputType      string   `json:"output_type"`
	ResourceType    string   `json:"resource_type"` // Extracted from operation name
	IsPaginated     bool     `json:"is_paginated"`
	PaginationField string   `json:"pagination_field"` // NextToken, Marker, etc.
	RequiredParams  []string `json:"required_params"`
	OptionalParams  []string `json:"optional_params"`
}

// ResourceTypeAnalysis represents analysis of a resource type
type ResourceTypeAnalysis struct {
	Name            string      `json:"name"`
	GoTypeName      string      `json:"go_type_name"`
	IdentifierField string      `json:"identifier_field"` // e.g., "InstanceId", "BucketName"
	NameField       string      `json:"name_field"`
	ARNField        string      `json:"arn_field"`
	TagsField       string      `json:"tags_field"`
	Operations      []string    `json:"operations"` // Operations that work with this resource
	Fields          []FieldInfo `json:"fields"`     // Detailed field analysis
	Relationships   []ResourceRelationship `json:"relationships"` // Detected relationships
}

// FieldInfo represents detailed information about a resource field
type FieldInfo struct {
	Name         string `json:"name"`
	GoType       string `json:"go_type"`
	JSONTag      string `json:"json_tag"`
	IsRequired   bool   `json:"is_required"`
	IsIdentifier bool   `json:"is_identifier"`
	IsList       bool   `json:"is_list"`
	IsNested     bool   `json:"is_nested"`
	NestedType   string `json:"nested_type"` // For complex types
	IsPointer    bool   `json:"is_pointer"`
	IsTimeField  bool   `json:"is_time_field"`
}

// ResourceRelationship represents a relationship between resources
type ResourceRelationship struct {
	SourceResource   string `json:"source_resource"`
	TargetResource   string `json:"target_resource"`
	RelationshipType string `json:"relationship_type"` // "has_many", "belongs_to", "references"
	FieldName        string `json:"field_name"`
	IsArray          bool   `json:"is_array"`
}

// PaginationPattern represents pagination information for an operation
type PaginationPattern struct {
	TokenField      string `json:"token_field"`       // Field name for pagination token
	LimitField      string `json:"limit_field"`       // Field name for page size limit
	ResultsField    string `json:"results_field"`     // Field containing the results array
	HasMoreField    string `json:"has_more_field"`    // Field indicating if more results exist
	IsCursorBased   bool   `json:"is_cursor_based"`   // True for cursor/token, false for page number
	MaxPageSize     int    `json:"max_page_size"`     // Maximum allowed page size
}

// AnalysisOperationType represents the type of AWS operation for analysis
type AnalysisOperationType int

const (
	OpTypeUnknown AnalysisOperationType = iota
	OpTypeList
	OpTypeDescribe
	OpTypeGet
	OpTypeCreate
	OpTypeUpdate
	OpTypeDelete
	OpTypePut
	OpTypeTag
	OpTypeUntag
	OpTypeEnable
	OpTypeDisable
	OpTypeStart
	OpTypeStop
)

// String returns the string representation of an operation type
func (op AnalysisOperationType) String() string {
	switch op {
	case OpTypeList:
		return "List"
	case OpTypeDescribe:
		return "Describe"
	case OpTypeGet:
		return "Get"
	case OpTypeCreate:
		return "Create"
	case OpTypeUpdate:
		return "Update"
	case OpTypeDelete:
		return "Delete"
	case OpTypePut:
		return "Put"
	case OpTypeTag:
		return "Tag"
	case OpTypeUntag:
		return "Untag"
	case OpTypeEnable:
		return "Enable"
	case OpTypeDisable:
		return "Disable"
	case OpTypeStart:
		return "Start"
	case OpTypeStop:
		return "Stop"
	default:
		return "Unknown"
	}
}

// GetOperationType determines the operation type from the operation name
func GetOperationType(operationName string) AnalysisOperationType {
	prefixes := map[string]AnalysisOperationType{
		"List":     OpTypeList,
		"Describe": OpTypeDescribe,
		"Get":      OpTypeGet,
		"Create":   OpTypeCreate,
		"Update":   OpTypeUpdate,
		"Delete":   OpTypeDelete,
		"Put":      OpTypePut,
		"Tag":      OpTypeTag,
		"Untag":    OpTypeUntag,
		"Enable":   OpTypeEnable,
		"Disable":  OpTypeDisable,
		"Start":    OpTypeStart,
		"Stop":     OpTypeStop,
	}

	for prefix, opType := range prefixes {
		if len(operationName) > len(prefix) && operationName[:len(prefix)] == prefix {
			return opType
		}
	}

	return OpTypeUnknown
}

// ExtractResourceType extracts the resource type from an operation name
func ExtractResourceType(operationName string, opType AnalysisOperationType) string {
	prefix := opType.String()
	if len(operationName) > len(prefix) {
		resourceType := operationName[len(prefix):]
		// Handle special cases like "DescribeInstances" -> "Instance"
		if len(resourceType) > 1 && resourceType[len(resourceType)-1] == 's' {
			// Check if it's a plural form
			if resourceType != "Status" && resourceType != "Access" {
				return resourceType[:len(resourceType)-1]
			}
		}
		return resourceType
	}
	return ""
}

// CommonPaginationFields are commonly used pagination field names in AWS APIs
var CommonPaginationFields = []string{
	"NextToken",
	"NextMarker",
	"Marker",
	"PageToken",
	"ContinuationToken",
	"StartingToken",
}

// CommonLimitFields are commonly used limit field names in AWS APIs
var CommonLimitFields = []string{
	"MaxResults",
	"MaxItems",
	"Limit",
	"PageSize",
	"MaxRecords",
}

// CommonIdentifierPatterns are patterns for identifying resource identifiers
var CommonIdentifierPatterns = []string{
	"Id",
	"ID",
	"Arn",
	"ARN",
	"Name",
	"Key",
	"Identifier",
}
package scanner

import (
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"fmt"
	"reflect"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ResourceConverter converts AWS SDK responses to standardized resource references
type ResourceConverter struct {
	service string
	region  string
}

// NewResourceConverter creates a new resource converter
func NewResourceConverter(service, region string) *ResourceConverter {
	return &ResourceConverter{
		service: service,
		region:  region,
	}
}

// ConvertToResourceRef converts an AWS SDK response to an discovery.AWSResourceRef
func (rc *ResourceConverter) ConvertToResourceRef(data interface{}, resourceType string) (*discovery.AWSResourceRef, error) {
	if data == nil {
		return nil, fmt.Errorf("data is nil")
	}

	// Use reflection to extract common fields
	value := reflect.ValueOf(data)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil, fmt.Errorf("data pointer is nil")
		}
		value = value.Elem()
	}

	ref := &discovery.AWSResourceRef{
		Type:     resourceType,
		Service:  rc.service,
		Region:   rc.region,
		Metadata: make(map[string]string),
	}

	// Extract fields using reflection
	valueType := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		fieldType := valueType.Field(i)
		fieldName := fieldType.Name

		if !field.CanInterface() {
			continue
		}

		// Extract ID fields
		if strings.Contains(strings.ToLower(fieldName), "id") && ref.ID == "" {
			if str := extractStringValue(field); str != "" {
				ref.ID = str
			}
		}

		// Extract Name fields
		if (strings.Contains(strings.ToLower(fieldName), "name") || fieldName == "Name") && ref.Name == "" {
			if str := extractStringValue(field); str != "" {
				ref.Name = str
			}
		}

		// Extract ARN fields
		if strings.Contains(strings.ToLower(fieldName), "arn") && ref.ARN == "" {
			if str := extractStringValue(field); str != "" {
				ref.ARN = str
			}
		}

		// Add other fields to metadata
		if str := extractStringValue(field); str != "" && len(str) < 200 {
			ref.Metadata[strings.ToLower(fieldName)] = str
		}
	}

	// Set defaults if not found
	if ref.ID == "" {
		if ref.Name != "" {
			ref.ID = ref.Name
		} else {
			ref.ID = fmt.Sprintf("unknown-%s", resourceType)
		}
	}
	if ref.Name == "" {
		ref.Name = ref.ID
	}

	return ref, nil
}

// extractStringValue extracts a string value from a reflect.Value
func extractStringValue(field reflect.Value) string {
	switch field.Kind() {
	case reflect.String:
		return field.String()
	case reflect.Ptr:
		if !field.IsNil() && field.Elem().Kind() == reflect.String {
			return field.Elem().String()
		}
	case reflect.Interface:
		if field.CanInterface() {
			if str, ok := field.Interface().(string); ok {
				return str
			}
		}
	}
	return ""
}

// ConvertPbListToScanner converts a list of pb.ResourceRef to scanner.discovery.AWSResourceRef
func (rc *ResourceConverter) ConvertPbListToScanner(pbRefs []*pb.ResourceRef) []discovery.AWSResourceRef {
	var scannerRefs []discovery.AWSResourceRef

	for _, pbRef := range pbRefs {
		if pbRef == nil {
			continue
		}

		scannerRef := discovery.AWSResourceRef{
			ID:       pbRef.Id,
			Name:     pbRef.Name,
			Type:     pbRef.Type,
			Service:  pbRef.Service,
			Region:   pbRef.Region,
			Metadata: make(map[string]string),
		}

		// Copy basic attributes to metadata
		if pbRef.BasicAttributes != nil {
			for k, v := range pbRef.BasicAttributes {
				scannerRef.Metadata[k] = v
			}
		}

		scannerRefs = append(scannerRefs, scannerRef)
	}

	return scannerRefs
}

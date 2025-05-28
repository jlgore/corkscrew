package generator

import (
	"reflect"
	"time"
	
	"github.com/jlgore/corkscrew/internal/generator"
)

// AWSProvider implements Provider for AWS SDK types
type AWSProvider struct{}

func (p *AWSProvider) Name() string { return "aws" }

func (p *AWSProvider) MapToProtoType(field generator.FieldInfo) string {
	// Handle slices (repeated fields)
	if field.IsSlice {
		elType := field.Type.Elem()
		elKind := elType.Kind()
		fakeField := field
		fakeField.Type = elType
		fakeField.Kind = elKind
		return "repeated " + p.MapToProtoType(fakeField)
	}

	// Handle maps (not common in AWS SDK, but proto3 supports map<key, value>)
	if field.IsMap {
		// TODO: implement map support if needed
		return "map<string, string>" // stub
	}

	// Handle pointers (unwrap only if pointer)
	if field.IsPointer && field.Type.Kind() == reflect.Ptr {
		fakeField := field
		fakeField.Type = field.Type.Elem()
		fakeField.Kind = field.Type.Elem().Kind()
		return p.MapToProtoType(fakeField)
	}

	// Handle time.Time
	if field.Type == reflect.TypeOf(time.Time{}) {
		return "google.protobuf.Timestamp"
	}

	switch field.Kind {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int32:
		return "int32"
	case reflect.Int64:
		return "int64"
	case reflect.Bool:
		return "bool"
	case reflect.Float32:
		return "float"
	case reflect.Float64:
		return "double"
	}

	// TODO: handle enums, custom types, etc.
	return field.Type.Name()
}

func (p *AWSProvider) GenerateFieldMapping(field generator.FieldInfo, accessor string) string {
	// Handle pointers
	if field.IsPointer {
		// Use aws.ToX helper if available, otherwise dereference
		switch field.Kind {
		case reflect.String:
			return "aws.ToString(" + accessor + ")"
		case reflect.Int, reflect.Int32:
			return "aws.ToInt32(" + accessor + ")"
		case reflect.Int64:
			return "aws.ToInt64(" + accessor + ")"
		case reflect.Bool:
			return "aws.ToBool(" + accessor + ")"
		case reflect.Float32:
			return "float32(*" + accessor + ")"
		case reflect.Float64:
			return "float64(*" + accessor + ")"
		}
		// For time.Time pointer
		if field.Type.Elem() == reflect.TypeOf(time.Time{}) {
			return "timestamppb.New(*" + accessor + ")"
		}
		return "*" + accessor
	}

	// Handle time.Time
	if field.Type == reflect.TypeOf(time.Time{}) {
		return "timestamppb.New(" + accessor + ")"
	}

	// Handle slices (map each element)
	if field.IsSlice {
		// TODO: implement slice mapping logic
		return accessor // stub
	}

	// Handle maps (not common in AWS SDK)
	if field.IsMap {
		// TODO: implement map mapping logic
		return accessor // stub
	}

	// Basic types: direct assignment
	return accessor
}

func (p *AWSProvider) IsResourceStruct(t reflect.Type) bool {
	// TODO: implement logic to identify AWS resource structs
	return true // stub
}

func init() {
	// Registration happens when the plugin is loaded
}
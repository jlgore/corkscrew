package generator

import (
	"reflect"
)

// FieldInfo contains information about a struct field
type FieldInfo struct {
	Name      string
	Type      reflect.Type
	Kind      reflect.Kind
	Tag       reflect.StructTag
	IsPointer bool
	IsSlice   bool
	IsMap     bool
}

// Provider defines the interface for cloud/K8s type mapping and field handling
type Provider interface {
	Name() string
	MapToProtoType(field FieldInfo) string
	GenerateFieldMapping(field FieldInfo, accessor string) string
	IsResourceStruct(t reflect.Type) bool
}

// Provider registry for lookup by name
var providerRegistry = map[string]Provider{}

func RegisterProvider(name string, p Provider) {
	providerRegistry[name] = p
}

func GetProvider(name string) Provider {
	return providerRegistry[name]
}
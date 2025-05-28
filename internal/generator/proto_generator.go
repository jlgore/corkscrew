package generator

import (
	"bytes"
	"fmt"
)

// StructInfo describes a struct for proto generation
type StructInfo struct {
	PackagePath string
	Name        string
	Fields      []FieldInfo
	Imports     map[string]bool
}

// ProtoGenerator generates proto definitions from struct info
// Now uses a Provider for type/field mapping
type ProtoGenerator struct {
	Provider Provider
	Options  ProtoGenOptions
}

type ProtoGenOptions struct {
	PackageName    string
	GoPackagePath  string
	IncludeImports bool
	PreserveCase   bool
}

// generateProtoFields generates proto fields for a struct
func (g *ProtoGenerator) generateProtoFields(structInfo *StructInfo) string {
	var buf bytes.Buffer
	for idx, field := range structInfo.Fields {
		protoType := g.Provider.MapToProtoType(field)
		fmt.Fprintf(&buf, "  %s %s = %d;\n", protoType, field.Name, idx+1)
	}
	return buf.String()
}

// GenerateProto generates a proto message definition for a struct
func (g *ProtoGenerator) GenerateProto(structInfo *StructInfo) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "message %s {\n", structInfo.Name)
	buf.WriteString(g.generateProtoFields(structInfo))
	fmt.Fprintf(&buf, "}\n")
	return buf.String()
}

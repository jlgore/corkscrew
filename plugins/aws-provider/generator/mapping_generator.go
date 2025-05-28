package generator

import (
	"bytes"
	
	"github.com/jlgore/corkscrew/internal/generator"
)

// MappingGenerator generates Go mapping code from AWS SDK types to proto messages
// Now uses a Provider for field mapping
type MappingGenerator struct {
	Provider generator.Provider
	Options  MappingGenOptions
}

type MappingGenOptions struct {
	// Add options as needed
}

// GenerateConverter generates a Go function to convert from an AWS SDK struct to a proto message
func (m *MappingGenerator) GenerateConverter(from, to *generator.StructInfo) string {
	var buf bytes.Buffer
	funcName := "Convert" + from.Name + "To" + to.Name
	buf.WriteString("func " + funcName + "(in *types." + from.Name + ") *pb." + to.Name + " {\n")
	buf.WriteString("\tif in == nil { return nil }\n")
	buf.WriteString("\tout := &pb." + to.Name + "{}\n")
	buf.WriteString(m.generateFieldMappings(from))
	buf.WriteString("\treturn out\n}")
	return buf.String()
}

// generateFieldMappings generates field assignments for the mapping function using the Provider
func (m *MappingGenerator) generateFieldMappings(from *generator.StructInfo) string {
	var buf bytes.Buffer
	for _, field := range from.Fields {
		mapping := m.Provider.GenerateFieldMapping(field, "in."+field.Name)
		buf.WriteString("\t// map field: " + field.Name + "\n")
		buf.WriteString("\t// " + mapping + "\n")
	}
	return buf.String()
}

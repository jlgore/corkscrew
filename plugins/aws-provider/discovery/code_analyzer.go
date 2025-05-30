package discovery

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// CodeAnalyzer analyzes AWS SDK Go code to extract operation and type information
type CodeAnalyzer struct {
	fileSet *token.FileSet
}

// NewCodeAnalyzer creates a new code analyzer
func NewCodeAnalyzer() *CodeAnalyzer {
	return &CodeAnalyzer{
		fileSet: token.NewFileSet(),
	}
}

// AnalyzeServiceCode analyzes downloaded service code to extract operations and types
func (ca *CodeAnalyzer) AnalyzeServiceCode(code *ServiceCode) (*ServiceAnalysis, error) {
	analysis := &ServiceAnalysis{
		ServiceName:    code.ServiceName,
		Operations:     []OperationAnalysis{},
		ResourceTypes:  []ResourceTypeAnalysis{},
		Relationships:  []ResourceRelationship{},
		PaginationInfo: make(map[string]PaginationPattern),
		LastAnalyzed:   code.DownloadedAt,
	}

	// Parse client code to understand the service structure
	if code.ClientCode != "" {
		if err := ca.analyzeClientCode(code.ClientCode, analysis); err != nil {
			return nil, fmt.Errorf("failed to analyze client code: %w", err)
		}
	}

	// Analyze each operation file
	for fileName, content := range code.OperationFiles {
		if err := ca.analyzeOperationFile(fileName, content, analysis); err != nil {
			fmt.Printf("Warning: failed to analyze %s: %v\n", fileName, err)
			continue
		}
	}

	// Analyze types to extract resource information
	if code.TypesCode != "" {
		if err := ca.analyzeTypesCode(code.TypesCode, analysis); err != nil {
			fmt.Printf("Warning: failed to analyze types: %v\n", err)
		}
	}

	// Post-process to establish relationships and mappings
	ca.postProcessAnalysis(analysis)

	return analysis, nil
}

// analyzeClientCode analyzes the client file to understand service structure
func (ca *CodeAnalyzer) analyzeClientCode(content string, analysis *ServiceAnalysis) error {
	file, err := parser.ParseFile(ca.fileSet, "api_client.go", content, parser.ParseComments)
	if err != nil {
		return err
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.TypeSpec:
			// Look for the Client type definition
			if node.Name.Name == "Client" {
				// Extract any relevant client metadata
			}
		}
		return true
	})

	return nil
}

// analyzeOperationFile analyzes a single operation file
func (ca *CodeAnalyzer) analyzeOperationFile(fileName, content string, analysis *ServiceAnalysis) error {
	// Extract operation name from filename
	opName := extractOperationName(fileName)
	if opName == "" {
		return nil
	}

	file, err := parser.ParseFile(ca.fileSet, fileName, content, parser.ParseComments)
	if err != nil {
		return err
	}

	op := OperationAnalysis{
		Name:           opName,
		Type:           GetOperationType(opName).String(),
		ResourceType:   ExtractResourceType(opName, GetOperationType(opName)),
		RequiredParams: []string{},
		OptionalParams: []string{},
	}

	// Find input and output types
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.TypeSpec:
			typeName := node.Name.Name
			
			// Check for input type
			if strings.HasSuffix(typeName, "Input") && strings.HasPrefix(typeName, opName) {
				op.InputType = typeName
				ca.analyzeInputType(node, &op)
			}
			
			// Check for output type
			if strings.HasSuffix(typeName, "Output") && strings.HasPrefix(typeName, opName) {
				op.OutputType = typeName
				ca.analyzeOutputType(node, &op, analysis)
			}
		}
		return true
	})

	analysis.Operations = append(analysis.Operations, op)
	return nil
}

// analyzeInputType analyzes the input type to extract parameters
func (ca *CodeAnalyzer) analyzeInputType(typeSpec *ast.TypeSpec, op *OperationAnalysis) {
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return
	}

	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		fieldName := field.Names[0].Name
		isRequired := false
		
		// Check if field is required based on comments or tags
		if field.Tag != nil {
			tag := field.Tag.Value
			if strings.Contains(tag, "required:\"true\"") {
				isRequired = true
			}
		}

		// Check for pagination fields
		for _, paginationField := range CommonPaginationFields {
			if fieldName == paginationField {
				op.IsPaginated = true
				op.PaginationField = fieldName
				break
			}
		}

		if isRequired {
			op.RequiredParams = append(op.RequiredParams, fieldName)
		} else {
			op.OptionalParams = append(op.OptionalParams, fieldName)
		}
	}
}

// analyzeOutputType analyzes the output type to detect pagination and results
func (ca *CodeAnalyzer) analyzeOutputType(typeSpec *ast.TypeSpec, op *OperationAnalysis, analysis *ServiceAnalysis) {
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return
	}

	pagination := PaginationPattern{}
	
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		fieldName := field.Names[0].Name
		
		// Check for pagination token fields
		for _, paginationField := range CommonPaginationFields {
			if fieldName == paginationField {
				op.IsPaginated = true
				pagination.TokenField = fieldName
				pagination.IsCursorBased = true
			}
		}

		// Check for results array (usually contains the resource type)
		if isArrayType(field.Type) {
			// This might be the results field
			if pagination.ResultsField == "" {
				pagination.ResultsField = fieldName
			}
		}
	}

	if op.IsPaginated {
		analysis.PaginationInfo[op.Name] = pagination
	}
}

// analyzeTypesCode analyzes the types file to extract resource information
func (ca *CodeAnalyzer) analyzeTypesCode(content string, analysis *ServiceAnalysis) error {
	file, err := parser.ParseFile(ca.fileSet, "types.go", content, parser.ParseComments)
	if err != nil {
		return err
	}

	resourceMap := make(map[string]*ResourceTypeAnalysis)

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.TypeSpec:
			typeName := node.Name.Name
			
			// Skip input/output types
			if strings.HasSuffix(typeName, "Input") || strings.HasSuffix(typeName, "Output") {
				return true
			}
			
			// Check if this is a resource type
			if structType, ok := node.Type.(*ast.StructType); ok && isLikelyResource(typeName) {
				resource := &ResourceTypeAnalysis{
					Name:       typeName,
					GoTypeName: typeName,
					Operations: []string{},
				}
				
				// Analyze fields to find identifiers, names, ARNs, and tags
				ca.analyzeResourceFields(structType, resource)
				
				resourceMap[typeName] = resource
			}
		}
		return true
	})

	// Convert map to slice
	for _, resource := range resourceMap {
		analysis.ResourceTypes = append(analysis.ResourceTypes, *resource)
	}

	return nil
}

// analyzeResourceFields analyzes struct fields to identify key resource fields
func (ca *CodeAnalyzer) analyzeResourceFields(structType *ast.StructType, resource *ResourceTypeAnalysis) {
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		fieldName := field.Names[0].Name
		fieldNameLower := strings.ToLower(fieldName)

		// Check for identifier field
		if strings.Contains(fieldNameLower, "id") && resource.IdentifierField == "" {
			resource.IdentifierField = fieldName
		}

		// Check for name field
		if fieldNameLower == "name" && resource.NameField == "" {
			resource.NameField = fieldName
		}

		// Check for ARN field
		if strings.Contains(fieldNameLower, "arn") && resource.ARNField == "" {
			resource.ARNField = fieldName
		}

		// Check for tags field
		if strings.Contains(fieldNameLower, "tag") && resource.TagsField == "" {
			resource.TagsField = fieldName
		}
	}
}

// postProcessAnalysis performs post-processing to establish relationships
func (ca *CodeAnalyzer) postProcessAnalysis(analysis *ServiceAnalysis) {
	// Map operations to resources
	resourceOpsMap := make(map[string][]string)
	
	for _, op := range analysis.Operations {
		if op.ResourceType != "" {
			// Try to find matching resource type
			for i := range analysis.ResourceTypes {
				resource := &analysis.ResourceTypes[i]
				if strings.Contains(resource.Name, op.ResourceType) ||
				   strings.Contains(op.ResourceType, resource.Name) {
					resource.Operations = append(resource.Operations, op.Name)
					resourceOpsMap[resource.Name] = resource.Operations
					break
				}
			}
		}
	}

	// Detect relationships between resources (simplified version)
	for i, res1 := range analysis.ResourceTypes {
		for j, res2 := range analysis.ResourceTypes {
			if i != j {
				// Check if res1 references res2
				// This is a simplified check - in reality, we'd analyze the fields more deeply
				if strings.Contains(res1.Name, res2.Name) || strings.Contains(res2.Name, res1.Name) {
					rel := ResourceRelationship{
						SourceResource:   res1.Name,
						TargetResource:   res2.Name,
						RelationshipType: "references",
					}
					analysis.Relationships = append(analysis.Relationships, rel)
				}
			}
		}
	}
}

// Helper functions

func extractOperationName(fileName string) string {
	if strings.HasPrefix(fileName, "api_op_") && strings.HasSuffix(fileName, ".go") {
		return strings.TrimSuffix(strings.TrimPrefix(fileName, "api_op_"), ".go")
	}
	return ""
}

func isArrayType(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.ArrayType:
		return true
	case *ast.StarExpr:
		if starExpr, ok := expr.(*ast.StarExpr); ok {
			return isArrayType(starExpr.X)
		}
	}
	return false
}
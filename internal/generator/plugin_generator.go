package generator

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"text/template"
)

// PluginGenerator generates plugin code from service analysis
type PluginGenerator struct {
	templates map[string]*template.Template
}

// NewPluginGenerator creates a new plugin generator
func NewPluginGenerator() *PluginGenerator {
	g := &PluginGenerator{
		templates: make(map[string]*template.Template),
	}
	g.loadTemplates()
	return g
}

// GeneratePlugin generates a complete plugin from service information
func (g *PluginGenerator) GeneratePlugin(service *AWSServiceInfo) (string, error) {
	tmpl := g.templates["plugin"]
	if tmpl == nil {
		return "", fmt.Errorf("plugin template not found")
	}

	var buf bytes.Buffer
	err := tmpl.Execute(&buf, service)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	// Format the generated code
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Return unformatted code if formatting fails
		return buf.String(), nil
	}

	return string(formatted), nil
}

// loadTemplates loads all code generation templates
func (g *PluginGenerator) loadTemplates() {
	g.templates["plugin"] = template.Must(template.New("plugin").Funcs(g.templateFuncs()).Parse(pluginTemplate))
}

// templateFuncs returns template helper functions
func (g *PluginGenerator) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"title":     strings.Title,
		"lower":     strings.ToLower,
		"upper":     strings.ToUpper,
		"camelCase": g.toCamelCase,
		"snakeCase": g.toSnakeCase,
		"hasPrefix": strings.HasPrefix,
		"join":      strings.Join,
		"quote":     func(s string) string { return fmt.Sprintf(`"%s"`, s) },
		"add":       func(a, b int) int { return a + b },
		"pluralize": g.pluralize,
		"singularize": g.singularize,
	}
}

// toCamelCase converts a string to camelCase
func (g *PluginGenerator) toCamelCase(s string) string {
	if s == "" {
		return s
	}
	
	words := strings.Fields(strings.ReplaceAll(s, "_", " "))
	if len(words) == 0 {
		return s
	}
	
	result := strings.ToLower(words[0])
	for i := 1; i < len(words); i++ {
		result += strings.Title(strings.ToLower(words[i]))
	}
	
	return result
}

// toSnakeCase converts a string to snake_case
func (g *PluginGenerator) toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// pluralize adds 's' to make a word plural (simple implementation)
func (g *PluginGenerator) pluralize(s string) string {
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") || strings.HasSuffix(s, "z") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") {
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

// singularize removes 's' to make a word singular (simple implementation)
func (g *PluginGenerator) singularize(s string) string {
	if strings.HasSuffix(s, "ies") {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(s, "es") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "s") && len(s) > 1 {
		return s[:len(s)-1]
	}
	return s
}

// pluginTemplate is the main template for generating AWS plugins
const pluginTemplate = `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/{{.Name}}"
	{{- range .ResourceTypes}}
	{{- if .Operations}}
	"github.com/aws/aws-sdk-go-v2/service/{{$.Name}}/types"
	{{- break}}
	{{- end}}
	{{- end}}
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew-generator/internal/shared"
	pb "github.com/jlgore/corkscrew-generator/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	ServiceName = "{{.Name}}"
	Version     = "1.0.0"
)

type {{title .Name}}Scanner struct{}

func (s *{{title .Name}}Scanner) Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	start := time.Now()
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
	}

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(req.Region),
	)
	if err != nil {
		return &pb.ScanResponse{
			Error: fmt.Sprintf("failed to load AWS config: %v", err),
		}, nil
	}

	client := {{.Name}}.NewFromConfig(cfg)

	var resources []*pb.Resource

	{{- range .ResourceTypes}}
	{{- if .Operations}}
	// Scan {{.Name}} resources
	{{camelCase .Name}}Resources, err := s.scan{{.Name}}(ctx, client, req)
	if err != nil {
		log.Printf("Error scanning {{lower .Name}}: %v", err)
		stats.FailedResources += int32(len({{camelCase .Name}}Resources))
	} else {
		resources = append(resources, {{camelCase .Name}}Resources...)
		stats.ResourceCounts["{{.Name}}"] = int32(len({{camelCase .Name}}Resources))
	}
	{{- end}}
	{{- end}}

	stats.TotalResources = int32(len(resources))
	stats.DurationMs = time.Since(start).Milliseconds()

	return &pb.ScanResponse{
		Resources: resources,
		Stats:     stats,
		Metadata: map[string]string{
			"service":   ServiceName,
			"version":   Version,
			"region":    req.Region,
			"scan_time": time.Now().Format(time.RFC3339),
		},
	}, nil
}

func (s *{{title .Name}}Scanner) GetSchemas(ctx context.Context, req *pb.Empty) (*pb.SchemaResponse, error) {
	schemas := []*pb.Schema{
		{{- range .ResourceTypes}}
		{{- if .Operations}}
		{
			Name: "{{snakeCase $.Name}}_{{snakeCase (pluralize .Name)}}",
			Sql: ` + "`" + `CREATE TABLE {{snakeCase $.Name}}_{{snakeCase (pluralize .Name)}} (
				id VARCHAR PRIMARY KEY,
				name VARCHAR,
				region VARCHAR,
				{{- if .ARNField}}
				arn VARCHAR,
				{{- end}}
				{{- if .TagsField}}
				tags JSON,
				{{- end}}
				raw_data JSON,
				created_at TIMESTAMP,
				modified_at TIMESTAMP
			)` + "`" + `,
			Description: "{{title $.Name}} {{.Name}} resources",
		},
		{{- end}}
		{{- end}}
	}

	return &pb.SchemaResponse{Schemas: schemas}, nil
}

func (s *{{title .Name}}Scanner) GetServiceInfo(ctx context.Context, req *pb.Empty) (*pb.ServiceInfoResponse, error) {
	return &pb.ServiceInfoResponse{
		ServiceName: ServiceName,
		Version:     Version,
		SupportedResources: []string{
			{{- range .ResourceTypes}}
			{{- if .Operations}}
			"{{.Name}}",
			{{- end}}
			{{- end}}
		},
		RequiredPermissions: []string{
			{{- range .Operations}}
			{{- if or .IsList .IsDescribe .IsGet}}
			"{{lower $.Name}}:{{.Name}}",
			{{- end}}
			{{- end}}
		},
		Capabilities: map[string]string{
			"streaming":    "true",
			"pagination":   "true",
			"parallelism":  "10",
			"relationships": "true",
		},
	}, nil
}

func (s *{{title .Name}}Scanner) StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	ctx := stream.Context()

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(req.Region),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := {{.Name}}.NewFromConfig(cfg)

	{{- range .ResourceTypes}}
	{{- if .Operations}}
	// Stream {{.Name}} resources
	if err := s.stream{{.Name}}(ctx, client, req, stream); err != nil {
		return fmt.Errorf("error streaming {{lower .Name}}: %w", err)
	}
	{{- end}}
	{{- end}}

	return nil
}

{{- range .ResourceTypes}}
{{- if .Operations}}

func (s *{{title $.Name}}Scanner) scan{{.Name}}(ctx context.Context, client *{{$.Name}}.Client, req *pb.ScanRequest) ([]*pb.Resource, error) {
	var resources []*pb.Resource

	{{- $listOp := ""}}
	{{- $describeOp := ""}}
	{{- range .Operations}}
	{{- if hasPrefix . "List"}}
	{{- $listOp = .}}
	{{- else if hasPrefix . "Describe"}}
	{{- $describeOp = .}}
	{{- end}}
	{{- end}}

	{{- if $listOp}}
	// List {{pluralize .Name}}
	result, err := client.{{$listOp}}(ctx, &{{$.Name}}.{{$listOp}}Input{})
	if err != nil {
		return nil, fmt.Errorf("failed to list {{lower (pluralize .Name)}}: %w", err)
	}

	{{- if eq $.Name "s3"}}
	{{- if eq .Name "Bucket"}}
	for _, item := range result.Buckets {
		resource, err := s.convert{{.Name}}ToResource(ctx, client, item, req.Region)
		if err != nil {
			log.Printf("Error converting {{lower .Name}} %v: %v", item, err)
			continue
		}
		resources = append(resources, resource)
	}
	{{- end}}
	{{- else}}
	// Generic conversion - this would need service-specific implementation
	for _, item := range result.{{pluralize .Name}} {
		resource, err := s.convert{{.Name}}ToResource(ctx, client, item, req.Region)
		if err != nil {
			log.Printf("Error converting {{lower .Name}} %v: %v", item, err)
			continue
		}
		resources = append(resources, resource)
	}
	{{- end}}
	{{- end}}

	return resources, nil
}

func (s *{{title $.Name}}Scanner) convert{{.Name}}ToResource(ctx context.Context, client *{{$.Name}}.Client, item interface{}, region string) (*pb.Resource, error) {
	// This is a generic template - specific implementations would need to be customized
	// based on the actual AWS service response structure
	
	rawData, _ := json.Marshal(item)
	
	resource := &pb.Resource{
		Type:    "{{.Name}}",
		Region:  region,
		RawData: string(rawData),
		Tags:    make(map[string]string),
	}

	{{- if eq $.Name "s3" }}
	{{- if eq .Name "Bucket"}}
	// S3 Bucket specific implementation
	if bucket, ok := item.(types.Bucket); ok {
		resource.Id = *bucket.Name
		resource.Name = *bucket.Name
		resource.CreatedAt = timestamppb.New(*bucket.CreationDate)
		
		// Get bucket ARN
		resource.Arn = fmt.Sprintf("arn:aws:s3:::%s", *bucket.Name)
	}
	{{- end}}
	{{- else}}
	// Generic implementation - would need service-specific customization
	// Extract ID, Name, ARN, etc. based on the service's response structure
	{{- end}}

	{{- if .Relationships}}
	// Add relationships
	resource.Relationships = []*pb.Relationship{
		{{- range .Relationships}}
		{
			TargetType:       "{{.TargetType}}",
			RelationshipType: "{{.RelationshipType}}",
			// TargetId would be extracted from the resource data
		},
		{{- end}}
	}
	{{- end}}

	return resource, nil
}

func (s *{{title $.Name}}Scanner) stream{{.Name}}(ctx context.Context, client *{{$.Name}}.Client, req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	resources, err := s.scan{{.Name}}(ctx, client, req)
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if err := stream.Send(resource); err != nil {
			return fmt.Errorf("failed to send {{lower .Name}} resource: %w", err)
		}
	}

	return nil
}
{{- end}}
{{- end}}

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"scanner": &shared.ScannerGRPCPlugin{
				Impl: &{{title .Name}}Scanner{},
			},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
`

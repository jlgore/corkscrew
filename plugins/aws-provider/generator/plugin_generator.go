package generator

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"text/template"
)

// PluginGenerator generates sophisticated scanner plugins with hierarchical discovery
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

// GeneratePlugin generates a complete scanner plugin from service information
func (g *PluginGenerator) GeneratePlugin(service *AWSServiceInfo) (string, error) {
	tmpl := g.templates["scanner_plugin"]
	if tmpl == nil {
		return "", fmt.Errorf("scanner plugin template not found")
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
	g.templates["scanner_plugin"] = template.Must(template.New("scanner_plugin").Funcs(g.templateFuncs()).Parse(scannerPluginTemplate))
}

// templateFuncs returns template helper functions
func (g *PluginGenerator) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"title":       strings.Title,
		"lower":       strings.ToLower,
		"upper":       strings.ToUpper,
		"camelCase":   g.toCamelCase,
		"snakeCase":   g.toSnakeCase,
		"hasPrefix":   strings.HasPrefix,
		"join":        strings.Join,
		"quote":       func(s string) string { return fmt.Sprintf(`"%s"`, s) },
		"add":         func(a, b int) int { return a + b },
		"pluralize":   g.pluralize,
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

// scannerPluginTemplate generates sophisticated scanner plugins with hierarchical discovery
const scannerPluginTemplate = `package main

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/{{.Name}}"
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/plugins/aws-provider/classification"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/scanner"
	"github.com/jlgore/corkscrew/internal/shared"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	ServiceName = "{{.Name}}"
	Version     = "1.0.0"
)

// {{title .Name}}Scanner implements the Scanner interface with hierarchical discovery
type {{title .Name}}Scanner struct {
	client                *{{.Name}}.Client
	region                string
	hierarchicalDiscoverer *discovery.HierarchicalDiscoverer
	classifier            *classification.OperationClassifier
	initialized           bool
}

// NewScanner creates a new {{.Name}} scanner
func NewScanner() *{{title .Name}}Scanner {
	return &{{title .Name}}Scanner{
		classifier: classification.NewOperationClassifier(),
	}
}

// Initialize sets up the scanner with AWS configuration
func (s *{{title .Name}}Scanner) Initialize(ctx context.Context, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	s.client = {{.Name}}.NewFromConfig(cfg)
	s.region = region
	s.initialized = true

	// Create hierarchical discoverer with this scanner as the resource scanner
	scannerAdapter := &resourceScannerAdapter{scanner: s}
	s.hierarchicalDiscoverer = discovery.NewHierarchicalDiscoverer(scannerAdapter, true)

	return nil
}

// ensureInitialized initializes the scanner if not already initialized
func (s *{{title .Name}}Scanner) ensureInitialized(ctx context.Context, region string) error {
	if s.initialized {
		return nil
	}
	return s.Initialize(ctx, region)
}

// Scan implements the main scanning operation with two-phase discovery
func (s *{{title .Name}}Scanner) Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	// Auto-initialize if needed
	if err := s.ensureInitialized(ctx, req.Region); err != nil {
		return &pb.ScanResponse{
			Error: fmt.Sprintf("failed to initialize scanner: %v", err),
		}, nil
	}

	start := time.Now()
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
		ServiceCounts:  make(map[string]int32),
	}
	
	// Add operation count to service stats
	stats.ServiceCounts[ServiceName] = int32({{len .Operations}})

	// Phase 1: Use hierarchical discovery to find all resources
	discoveredResources, err := s.hierarchicalDiscoverer.DiscoverService(ctx, ServiceName)
	if err != nil {
		log.Printf("Hierarchical discovery failed, falling back to flat discovery: %v", err)
		// Fallback to flat discovery
		discoveredResources, err = s.flatDiscovery(ctx)
		if err != nil {
			return &pb.ScanResponse{
				Error: fmt.Sprintf("both hierarchical and flat discovery failed: %v", err),
			}, nil
		}
	}

	// Phase 2: Convert discovered resources to full Resource objects with relationships
	var resources []*pb.Resource
	for _, ref := range discoveredResources {
		resource, err := s.enrichResource(ctx, ref)
		if err != nil {
			log.Printf("Failed to enrich resource %s: %v", ref.ID, err)
			stats.FailedResources++
			continue
		}
		resources = append(resources, resource)
		stats.ResourceCounts[resource.Type]++
	}

	stats.TotalResources = int32(len(resources))
	stats.DurationMs = time.Since(start).Milliseconds()

	return &pb.ScanResponse{
		Resources: resources,
		Stats:     stats,
		Metadata: map[string]string{
			"service":            ServiceName,
			"version":            Version,
			"region":             req.Region,
			"scan_time":          time.Now().Format(time.RFC3339),
			"discovery_mode":     "hierarchical",
			"operation_count":    "{{len .Operations}}",
			"resource_types":     "{{len .ResourceTypes}}",
			"supported_ops":      "{{range $i, $op := .Operations}}{{if $i}},{{end}}{{$op.Name}}{{end}}",
		},
	}, nil
}

// GetSchemas returns SQL schemas for this service's resources
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
				attributes JSON,
				relationships JSON,
				created_at TIMESTAMP,
				modified_at TIMESTAMP,
				discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)` + "`" + `,
			Description: "{{title $.Name}} {{.Name}} resources with relationships",
		},
		{{- end}}
		{{- end}}
	}

	return &pb.SchemaResponse{Schemas: schemas}, nil
}

// GetServiceInfo returns metadata about this scanner
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
			"streaming":           "true",
			"pagination":          "true",
			"parallelism":         "10",
			"relationships":       "true",
			"hierarchical_discovery": "true",
			"two_phase_scan":      "true",
		},
	}, nil
}

// StreamScan implements streaming scan for large result sets
func (s *{{title .Name}}Scanner) StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	ctx := stream.Context()

	// Auto-initialize if needed
	if err := s.ensureInitialized(ctx, req.Region); err != nil {
		return fmt.Errorf("failed to initialize scanner: %w", err)
	}

	// Use hierarchical discovery for streaming as well
	discoveredResources, err := s.hierarchicalDiscoverer.DiscoverService(ctx, ServiceName)
	if err != nil {
		// Fallback to flat discovery
		discoveredResources, err = s.flatDiscovery(ctx)
		if err != nil {
			return fmt.Errorf("discovery failed: %w", err)
		}
	}

	// Stream enriched resources
	for _, ref := range discoveredResources {
		resource, err := s.enrichResource(ctx, ref)
		if err != nil {
			log.Printf("Failed to enrich resource %s: %v", ref.ID, err)
			continue
		}

		if err := stream.Send(resource); err != nil {
			return fmt.Errorf("failed to send resource: %w", err)
		}
	}

	return nil
}

// flatDiscovery implements fallback flat discovery when hierarchical fails
func (s *{{title .Name}}Scanner) flatDiscovery(ctx context.Context) ([]discovery.AWSResourceRef, error) {
	var resources []discovery.AWSResourceRef

	{{- range .ResourceTypes}}
	{{- if .Operations}}
	{{- $listOp := ""}}
	{{- range .Operations}}
	{{- if hasPrefix . "List"}}
	{{- $listOp = .}}
	{{- break}}
	{{- end}}
	{{- end}}
	{{- if $listOp}}
	// Flat discovery for {{.Name}}
	{{camelCase .Name}}Resources, err := s.list{{.Name}}Resources(ctx)
	if err != nil {
		log.Printf("Failed to list {{lower .Name}} resources: %v", err)
	} else {
		resources = append(resources, {{camelCase .Name}}Resources...)
	}
	{{- end}}
	{{- end}}
	{{- end}}

	return resources, nil
}

{{- range .ResourceTypes}}
{{- if .Operations}}
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

// list{{.Name}}Resources lists {{.Name}} resources using {{$listOp}}
func (s *{{title $.Name}}Scanner) list{{.Name}}Resources(ctx context.Context) ([]discovery.AWSResourceRef, error) {
	var resources []discovery.AWSResourceRef

	// Execute {{$listOp}}
	result, err := s.client.{{$listOp}}(ctx, &{{$.Name}}.{{$listOp}}Input{})
	if err != nil {
		return nil, fmt.Errorf("failed to execute {{$listOp}}: %w", err)
	}

	// Parse results using reflection
	resultValue := reflect.ValueOf(result).Elem()
	for i := 0; i < resultValue.NumField(); i++ {
		field := resultValue.Field(i)
		if field.Kind() == reflect.Slice && field.Len() > 0 {
			for j := 0; j < field.Len(); j++ {
				item := field.Index(j)
				resource := s.parseResourceItem(item, "{{.Name}}")
				if resource != nil {
					resources = append(resources, *resource)
				}
			}
		}
	}

	return resources, nil
}
{{- end}}
{{- end}}
{{- end}}

// enrichResource converts a resource reference to a full Resource with relationships
func (s *{{title .Name}}Scanner) enrichResource(ctx context.Context, ref discovery.AWSResourceRef) (*pb.Resource, error) {
	resource := &pb.Resource{
		Provider:     "aws",
		Service:      ServiceName,
		Type:         ref.Type,
		Id:           ref.ID,
		Name:         ref.Name,
		Region:       ref.Region,
		DiscoveredAt: timestamppb.Now(),
		Tags:         make(map[string]string),
	}

	// Copy metadata to tags
	for k, v := range ref.Metadata {
		resource.Tags[k] = v
	}

	// Extract relationships based on resource type and service
	relationships := s.extractRelationships(ctx, ref)
	resource.Relationships = relationships

	// Get detailed resource data if describe operation is available
	detailedData, err := s.getDetailedResourceData(ctx, ref)
	if err != nil {
		log.Printf("Failed to get detailed data for %s: %v", ref.ID, err)
	} else {
		resource.RawData = detailedData
	}

	return resource, nil
}

// extractRelationships discovers relationships for a resource
func (s *{{title .Name}}Scanner) extractRelationships(ctx context.Context, ref discovery.AWSResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship

	// Service-specific relationship extraction
	switch ServiceName {
	{{- if eq .Name "iam"}}
	case "iam":
		relationships = s.extractIAMRelationships(ctx, ref)
	{{- end}}
	{{- if eq .Name "s3"}}
	case "s3":
		relationships = s.extractS3Relationships(ctx, ref)
	{{- end}}
	{{- if eq .Name "ec2"}}
	case "ec2":
		relationships = s.extractEC2Relationships(ctx, ref)
	{{- end}}
	default:
		// Generic relationship extraction
		relationships = s.extractGenericRelationships(ctx, ref)
	}

	return relationships
}

{{- if eq .Name "iam"}}
// extractIAMRelationships extracts IAM-specific relationships
func (s *{{title .Name}}Scanner) extractIAMRelationships(ctx context.Context, ref discovery.AWSResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship

	switch ref.Type {
	case "User":
		// User -> Groups relationship
		if groups, err := s.client.ListGroupsForUser(ctx, &iam.ListGroupsForUserInput{
			UserName: aws.String(ref.Name),
		}); err == nil {
			for _, group := range groups.Groups {
				relationships = append(relationships, &pb.Relationship{
					TargetId:         *group.GroupName,
					TargetType:       "Group",
					TargetService:    "iam",
					RelationshipType: "member_of",
				})
			}
		}

		// User -> Policies relationship
		if policies, err := s.client.ListAttachedUserPolicies(ctx, &iam.ListAttachedUserPoliciesInput{
			UserName: aws.String(ref.Name),
		}); err == nil {
			for _, policy := range policies.AttachedPolicies {
				relationships = append(relationships, &pb.Relationship{
					TargetId:         *policy.PolicyArn,
					TargetType:       "Policy",
					TargetService:    "iam",
					RelationshipType: "has_policy",
				})
			}
		}

	case "Role":
		// Role -> Policies relationship
		if policies, err := s.client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(ref.Name),
		}); err == nil {
			for _, policy := range policies.AttachedPolicies {
				relationships = append(relationships, &pb.Relationship{
					TargetId:         *policy.PolicyArn,
					TargetType:       "Policy",
					TargetService:    "iam",
					RelationshipType: "has_policy",
				})
			}
		}
	}

	return relationships
}
{{- end}}

{{- if eq .Name "s3"}}
// extractS3Relationships extracts S3-specific relationships
func (s *{{title .Name}}Scanner) extractS3Relationships(ctx context.Context, ref discovery.AWSResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship

	switch ref.Type {
	case "Bucket":
		// Bucket -> Objects relationship (sample a few objects)
		if objects, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:  aws.String(ref.Name),
			MaxKeys: aws.Int32(10), // Sample only
		}); err == nil {
			for _, object := range objects.Contents {
				relationships = append(relationships, &pb.Relationship{
					TargetId:         *object.Key,
					TargetType:       "Object",
					TargetService:    "s3",
					RelationshipType: "contains",
					Properties: map[string]string{
						"size":          fmt.Sprintf("%d", object.Size),
						"last_modified": object.LastModified.Format(time.RFC3339),
					},
				})
			}
		}
	}

	return relationships
}
{{- end}}

{{- if eq .Name "ec2"}}
// extractEC2Relationships extracts EC2-specific relationships
func (s *{{title .Name}}Scanner) extractEC2Relationships(ctx context.Context, ref discovery.AWSResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship

	switch ref.Type {
	case "Instance":
		// Instance -> VPC relationship
		if instances, err := s.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{ref.ID},
		}); err == nil {
			for _, reservation := range instances.Reservations {
				for _, instance := range reservation.Instances {
					if instance.VpcId != nil {
						relationships = append(relationships, &pb.Relationship{
							TargetId:         *instance.VpcId,
							TargetType:       "VPC",
							TargetService:    "ec2",
							RelationshipType: "runs_in",
						})
					}
					if instance.SubnetId != nil {
						relationships = append(relationships, &pb.Relationship{
							TargetId:         *instance.SubnetId,
							TargetType:       "Subnet",
							TargetService:    "ec2",
							RelationshipType: "runs_in",
						})
					}
				}
			}
		}
	}

	return relationships
}
{{- end}}

// extractGenericRelationships extracts generic relationships
func (s *{{title .Name}}Scanner) extractGenericRelationships(ctx context.Context, ref discovery.AWSResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship
	
	// Generic relationship extraction based on common patterns
	// This can be enhanced with more sophisticated logic
	
	return relationships
}

// getDetailedResourceData gets detailed resource data using describe operations
func (s *{{title .Name}}Scanner) getDetailedResourceData(ctx context.Context, ref discovery.AWSResourceRef) (string, error) {
	// Try to find and execute a describe operation for this resource type
	{{- range .ResourceTypes}}
	{{- if .Operations}}
	{{- $describeOp := ""}}
	{{- range .Operations}}
	{{- if hasPrefix . "Describe"}}
	{{- $describeOp = .}}
	{{- break}}
	{{- end}}
	{{- end}}
	{{- if $describeOp}}
	if ref.Type == "{{.Name}}" {
		// Use {{$describeOp}} for detailed data
		// Implementation would depend on the specific operation signature
		return s.executeDescribeOperation(ctx, "{{$describeOp}}", ref)
	}
	{{- end}}
	{{- end}}
	{{- end}}

	return "", nil
}

// executeDescribeOperation executes a describe operation for detailed resource data
func (s *{{title .Name}}Scanner) executeDescribeOperation(ctx context.Context, operationName string, ref discovery.AWSResourceRef) (string, error) {
	// Use reflection to call the describe operation
	clientValue := reflect.ValueOf(s.client)
	method := clientValue.MethodByName(operationName)
	if !method.IsValid() {
		return "", fmt.Errorf("operation %s not found", operationName)
	}

	// This is a simplified implementation - in practice, you'd need to
	// construct the appropriate input based on the operation signature
	// and the resource reference
	
	return "", nil
}

// parseResourceItem parses a resource item from AWS API response
func (s *{{title .Name}}Scanner) parseResourceItem(item reflect.Value, resourceType string) *discovery.AWSResourceRef {
	if item.Kind() == reflect.Ptr {
		if item.IsNil() {
			return nil
		}
		item = item.Elem()
	}

	resource := &discovery.AWSResourceRef{
		Type:     resourceType,
		Service:  ServiceName,
		Region:   s.region,
		Metadata: make(map[string]string),
	}

	// Extract common fields using reflection
	for i := 0; i < item.NumField(); i++ {
		field := item.Field(i)
		fieldType := item.Type().Field(i)
		fieldName := fieldType.Name

		if !field.IsValid() || !field.CanInterface() {
			continue
		}

		switch fieldName {
		case "Name", "BucketName", "FunctionName", "TableName", "ClusterName", "UserName", "RoleName", "GroupName":
			if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
				if !field.IsNil() {
					resource.Name = field.Elem().String()
					if resource.ID == "" {
						resource.ID = resource.Name
					}
				}
			}
		case "Id", "InstanceId", "VolumeId", "SecurityGroupId", "UserId", "RoleId", "GroupId":
			if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
				if !field.IsNil() {
					resource.ID = field.Elem().String()
				}
			}
		case "Arn", "ARN":
			if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
				if !field.IsNil() {
					resource.ARN = field.Elem().String()
				}
			}
		}
	}

	// Ensure we have an ID
	if resource.ID == "" && resource.Name != "" {
		resource.ID = resource.Name
	}
	if resource.ID == "" {
		resource.ID = fmt.Sprintf("%s-%d", strings.ToLower(resourceType), time.Now().UnixNano())
	}

	return resource
}

// resourceScannerAdapter adapts this scanner to implement discovery.ResourceScanner
type resourceScannerAdapter struct {
	scanner *{{title .Name}}Scanner
}

func (rsa *resourceScannerAdapter) DiscoverAndListServiceResources(ctx context.Context, serviceName string) ([]discovery.AWSResourceRef, error) {
	if serviceName != ServiceName {
		return nil, fmt.Errorf("service %s not supported by this scanner", serviceName)
	}
	return rsa.scanner.flatDiscovery(ctx)
}

func (rsa *resourceScannerAdapter) ExecuteOperationWithParams(ctx context.Context, serviceName, operationName string, params map[string]interface{}) ([]discovery.AWSResourceRef, error) {
	if serviceName != ServiceName {
		return nil, fmt.Errorf("service %s not supported by this scanner", serviceName)
	}
	
	// Use reflection to execute the operation with parameters
	// This is a simplified implementation
	return rsa.scanner.executeOperationWithReflection(ctx, operationName, params)
}

func (rsa *resourceScannerAdapter) GetRegion() string {
	return rsa.scanner.region
}

// executeOperationWithReflection executes an operation using reflection with parameters
func (s *{{title .Name}}Scanner) executeOperationWithReflection(ctx context.Context, operationName string, params map[string]interface{}) ([]discovery.AWSResourceRef, error) {
	clientValue := reflect.ValueOf(s.client)
	method := clientValue.MethodByName(operationName)
	if !method.IsValid() {
		return nil, fmt.Errorf("operation %s not found", operationName)
	}

	// Create input struct and set parameters
	methodType := method.Type()
	if methodType.NumIn() < 2 {
		return nil, fmt.Errorf("unexpected method signature for %s", operationName)
	}

	inputType := methodType.In(1)
	var input reflect.Value

	if inputType.Kind() == reflect.Ptr {
		input = reflect.New(inputType.Elem())
		s.setParameters(input, params)
	} else {
		input = reflect.Zero(inputType)
	}

	// Call the method
	args := []reflect.Value{reflect.ValueOf(ctx), input}
	results := method.Call(args)

	if len(results) < 2 {
		return nil, fmt.Errorf("unexpected return values from %s", operationName)
	}

	// Check for error
	if !results[1].IsNil() {
		err := results[1].Interface().(error)
		return nil, err
	}

	// Parse the output
	output := results[0]
	return s.parseOperationOutput(output), nil
}

// setParameters sets parameters on an input struct using reflection
func (s *{{title .Name}}Scanner) setParameters(input reflect.Value, params map[string]interface{}) {
	if input.Kind() == reflect.Ptr {
		input = input.Elem()
	}

	for fieldName, value := range params {
		field := input.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		switch v := value.(type) {
		case string:
			if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
				field.Set(reflect.ValueOf(&v))
			} else if field.Kind() == reflect.String {
				field.SetString(v)
			}
		case []string:
			if field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.String {
				stringSlice := reflect.MakeSlice(field.Type(), len(v), len(v))
				for i, str := range v {
					stringSlice.Index(i).SetString(str)
				}
				field.Set(stringSlice)
			}
		case int32:
			if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Int32 {
				field.Set(reflect.ValueOf(&v))
			} else if field.Kind() == reflect.Int32 {
				field.SetInt(int64(v))
			}
		}
	}
}

// parseOperationOutput parses operation output to extract resources
func (s *{{title .Name}}Scanner) parseOperationOutput(output reflect.Value) []discovery.AWSResourceRef {
	var resources []discovery.AWSResourceRef

	if output.Kind() == reflect.Ptr {
		output = output.Elem()
	}

	// Look for slice fields that contain resources
	for i := 0; i < output.NumField(); i++ {
		field := output.Field(i)
		if field.Kind() == reflect.Slice && field.Len() > 0 {
			// Infer resource type from field name
			fieldType := output.Type().Field(i)
			resourceType := s.inferResourceTypeFromField(fieldType.Name)
			
			for j := 0; j < field.Len(); j++ {
				item := field.Index(j)
				resource := s.parseResourceItem(item, resourceType)
				if resource != nil {
					resources = append(resources, *resource)
				}
			}
		}
	}

	return resources
}

// inferResourceTypeFromField infers resource type from field name
func (s *{{title .Name}}Scanner) inferResourceTypeFromField(fieldName string) string {
	// Remove common suffixes and make singular
	if strings.HasSuffix(fieldName, "s") {
		return fieldName[:len(fieldName)-1]
	}
	return fieldName
}

func main() {
	scanner := NewScanner()
	
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"scanner": &shared.ScannerGRPCPlugin{
				Impl: scanner,
			},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
`

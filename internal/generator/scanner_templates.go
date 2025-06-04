package generator


// createAdvancedScannerTemplate creates a more sophisticated scanner template with proper type handling
func createAdvancedScannerTemplate() string {
	return `// Code generated at {{.GeneratedAt}}. DO NOT EDIT.

package {{.PackageName}}

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"
	{{if .WithRetry}}"math"{{end}}
	{{if .WithMetrics}}"github.com/prometheus/client_golang/prometheus"{{end}}

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/{{.ServiceName}}"
	"github.com/aws/aws-sdk-go-v2/service/{{.ServiceName}}/types"
	pb "github.com/jlgore/corkscrew/proto"
)

// {{.ServiceNameCaps}}Scanner implements ServiceScanner for AWS {{.ServiceNameCaps}}
type {{.ServiceNameCaps}}Scanner struct {
	clientFactory *ClientFactory
	{{if .WithMetrics}}
	metrics struct {
		scanDuration     prometheus.Histogram
		resourcesScanned prometheus.Counter
		errorsTotal      prometheus.Counter
	}
	{{end}}
}

// New{{.ServiceNameCaps}}Scanner creates a new {{.ServiceName}} scanner
func New{{.ServiceNameCaps}}Scanner(clientFactory *ClientFactory) *{{.ServiceNameCaps}}Scanner {
	scanner := &{{.ServiceNameCaps}}Scanner{
		clientFactory: clientFactory,
	}
	{{if .WithMetrics}}
	scanner.initMetrics()
	{{end}}
	return scanner
}

{{if .WithMetrics}}
// initMetrics initializes Prometheus metrics
func (s *{{.ServiceNameCaps}}Scanner) initMetrics() {
	s.metrics.scanDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "corkscrew_{{.ServiceName}}_scan_duration_seconds",
		Help: "Time spent scanning {{.ServiceName}} resources",
	})
	s.metrics.resourcesScanned = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "corkscrew_{{.ServiceName}}_resources_scanned_total",
		Help: "Total number of {{.ServiceName}} resources scanned",
	})
	s.metrics.errorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "corkscrew_{{.ServiceName}}_errors_total",
		Help: "Total number of errors while scanning {{.ServiceName}}",
	})
	
	prometheus.MustRegister(s.metrics.scanDuration, s.metrics.resourcesScanned, s.metrics.errorsTotal)
}
{{end}}

// GetServiceName returns the service name
func (s *{{.ServiceNameCaps}}Scanner) GetServiceName() string {
	return "{{.ServiceName}}"
}

// Scan discovers all {{.ServiceName}} resources
func (s *{{.ServiceNameCaps}}Scanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
	{{if .WithMetrics}}
	start := time.Now()
	defer func() {
		s.metrics.scanDuration.Observe(time.Since(start).Seconds())
	}()
	{{end}}

	client, err := s.clientFactory.Get{{.ServiceNameCaps}}Client(ctx)
	if err != nil {
		{{if .WithMetrics}}s.metrics.errorsTotal.Inc(){{end}}
		return nil, fmt.Errorf("failed to get {{.ServiceName}} client: %w", err)
	}

	var allResources []*pb.ResourceRef

	{{range .Resources}}
	{{if .ListOperation}}
	// Scan {{.Name}} resources
	{{.Name | lower}}Resources, err := s.scan{{.Name}}Resources(ctx, client)
	if err != nil {
		// Log error but continue with other resources
		fmt.Printf("Warning: failed to scan {{.Name}} resources: %v\n", err)
	} else {
		allResources = append(allResources, {{.Name | lower}}Resources...)
		{{if $.WithMetrics}}s.metrics.resourcesScanned.Add(float64(len({{.Name | lower}}Resources))){{end}}
	}
	{{end}}

	{{end}}
	return allResources, nil
}

{{range .Resources}}
{{if .ListOperation}}
// scan{{.Name}}Resources scans all {{.Name}} resources
func (s *{{$.ServiceNameCaps}}Scanner) scan{{.Name}}Resources(ctx context.Context, client *{{$.ServiceName}}.Client) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef
	
	{{if eq $.ServiceName "s3"}}
	// S3 special case - ListBuckets doesn't need input
	{{if eq .ListOperation.Name "ListBuckets"}}
	{{if .Paginated}}
	// Paginated scan for S3 buckets
	input := &{{$.ServiceName}}.{{.ListOperation.InputType}}{}
	for {
		{{if $.WithRetry}}
		output, err := s.retryOperation(ctx, func() (*{{$.ServiceName}}.{{.ListOperation.OutputType}}, error) {
			return client.{{.ListOperation.Name}}(ctx, input)
		})
		{{else}}
		output, err := client.{{.ListOperation.Name}}(ctx, input)
		{{end}}
		if err != nil {
			return nil, fmt.Errorf("failed to list {{.Name}}: %w", err)
		}

		// Convert S3 buckets to ResourceRef
		if output.Buckets != nil {
			for _, bucket := range output.Buckets {
				ref := s.convertS3BucketToResourceRef(bucket)
				if ref != nil {
					resources = append(resources, ref)
				}
			}
		}

		// S3 ListBuckets doesn't support pagination in practice
		break
	}
	{{else}}
	// Single page scan for S3 buckets
	{{if $.WithRetry}}
	output, err := s.retryListBuckets(ctx, client, &{{$.ServiceName}}.{{.ListOperation.InputType}}{})
	{{else}}
	output, err := client.{{.ListOperation.Name}}(ctx, &{{$.ServiceName}}.{{.ListOperation.InputType}}{})
	{{end}}
	if err != nil {
		return nil, fmt.Errorf("failed to list {{.Name}}: %w", err)
	}

	// Convert S3 buckets to ResourceRef
	if output.Buckets != nil {
		for _, bucket := range output.Buckets {
			ref := s.convertS3BucketToResourceRef(bucket)
			if ref != nil {
				resources = append(resources, ref)
			}
		}
	}
	{{end}}
	{{else}}
	// Other S3 operations that need bucket name - skip for now
	return resources, nil
	{{end}}
	{{else}}
	// Non-S3 services - generic handling
	input := &{{$.ServiceName}}.{{.ListOperation.InputType}}{}
	
	{{if .Paginated}}
	// Paginated scan
	for {
		{{if $.WithRetry}}
		result, err := s.retryDescribeOperation(ctx, func() (interface{}, error) {
			return client.{{.ListOperation.Name}}(ctx, input)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list {{.Name}}: %w", err)
		}
		output := result.(*{{$.ServiceName}}.{{.ListOperation.OutputType}})
		{{else}}
		output, err := client.{{.ListOperation.Name}}(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list {{.Name}}: %w", err)
		}
		{{end}}

		// Convert items to ResourceRef using reflection
		convertedRefs := s.convertResponseToResourceRefs(output, "{{.Name}}", "{{.TypeName}}")
		resources = append(resources, convertedRefs...)

		// Check for pagination token
		nextToken := s.extractPaginationToken(output)
		if nextToken == "" {
			break
		}
		s.setPaginationToken(input, nextToken)
	}
	{{else}}
	// Single page scan
	{{if $.WithRetry}}
	result, err := s.retryDescribeOperation(ctx, func() (interface{}, error) {
		return client.{{.ListOperation.Name}}(ctx, input)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list {{.Name}}: %w", err)
	}
	output := result.(*{{$.ServiceName}}.{{.ListOperation.OutputType}})
	{{else}}
	output, err := client.{{.ListOperation.Name}}(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list {{.Name}}: %w", err)
	}
	{{end}}

	// Convert items to ResourceRef using reflection
	convertedRefs := s.convertResponseToResourceRefs(output, "{{.Name}}", "{{.TypeName}}")
	resources = append(resources, convertedRefs...)
	{{end}}
	{{end}}

	return resources, nil
}
{{end}}
{{end}}

{{if eq .ServiceName "s3"}}
// convertS3BucketToResourceRef converts S3 Bucket to ResourceRef
func (s *{{.ServiceNameCaps}}Scanner) convertS3BucketToResourceRef(bucket types.Bucket) *pb.ResourceRef {
	if bucket.Name == nil {
		return nil
	}

	ref := &pb.ResourceRef{
		Id:      *bucket.Name,
		Name:    *bucket.Name,
		Type:    "Bucket",
		Service: "s3",
	}

	if bucket.CreationDate != nil {
		ref.Created = bucket.CreationDate.Format("2006-01-02T15:04:05Z")
	}

	return ref
}
{{end}}

// convertResponseToResourceRefs converts AWS API response to ResourceRef using reflection
func (s *{{.ServiceNameCaps}}Scanner) convertResponseToResourceRefs(response interface{}, resourceName, resourceType string) []*pb.ResourceRef {
	var refs []*pb.ResourceRef

	// Use reflection to find the slice of items in the response
	responseValue := reflect.ValueOf(response)
	if responseValue.Kind() == reflect.Ptr {
		responseValue = responseValue.Elem()
	}

	// Common field names that contain the list of resources
	possibleFields := []string{
		resourceName + "s",      // Instances, Volumes, etc.
		resourceName,            // Instance, Volume (if singular)
		resourceType + "s",      // Same as above but with TypeName
		"Items",                 // Generic Items field
		"Results",               // Generic Results field
		"List",                  // Generic List field
	}

	for _, fieldName := range possibleFields {
		field := responseValue.FieldByName(fieldName)
		if !field.IsValid() || field.Kind() != reflect.Slice {
			continue
		}

		// Found a slice field, iterate through items
		for i := 0; i < field.Len(); i++ {
			item := field.Index(i)
			ref := s.convertItemToResourceRef(item.Interface(), resourceType)
			if ref != nil {
				refs = append(refs, ref)
			}
		}
		break // Found and processed the items field
	}

	return refs
}

// convertItemToResourceRef converts a single AWS resource item to ResourceRef
func (s *{{.ServiceNameCaps}}Scanner) convertItemToResourceRef(item interface{}, resourceType string) *pb.ResourceRef {
	ref := &pb.ResourceRef{
		Type:    resourceType,
		Service: "{{.ServiceName}}",
	}

	// Use reflection to extract common fields
	itemValue := reflect.ValueOf(item)
	if itemValue.Kind() == reflect.Ptr {
		itemValue = itemValue.Elem()
	}

	// Extract ID field
	if id := s.extractStringField(itemValue, []string{"Id", "ID", "Arn", "ARN", "Name"}); id != "" {
		ref.Id = id
	}

	// Extract Name field
	if name := s.extractStringField(itemValue, []string{"Name", "DisplayName", "Title"}); name != "" {
		ref.Name = name
	}

	// Extract ARN field
	if arn := s.extractStringField(itemValue, []string{"Arn", "ARN"}); arn != "" {
		ref.Arn = arn
	}

	// Extract creation time
	if createdTime := s.extractTimeField(itemValue, []string{"CreationDate", "CreatedAt", "LaunchTime"}); createdTime != "" {
		ref.Created = createdTime
	}

	// Only return ref if we found at least an ID
	if ref.Id != "" {
		return ref
	}

	return nil
}

// extractStringField extracts a string field from a struct using reflection
func (s *{{.ServiceNameCaps}}Scanner) extractStringField(structValue reflect.Value, fieldNames []string) string {
	for _, fieldName := range fieldNames {
		field := structValue.FieldByName(fieldName)
		if !field.IsValid() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			return field.String()
		case reflect.Ptr:
			if field.Elem().Kind() == reflect.String {
				return field.Elem().String()
			}
		}
	}
	return ""
}

// extractTimeField extracts a time field and formats it as string
func (s *{{.ServiceNameCaps}}Scanner) extractTimeField(structValue reflect.Value, fieldNames []string) string {
	for _, fieldName := range fieldNames {
		field := structValue.FieldByName(fieldName)
		if !field.IsValid() {
			continue
		}

		// Handle *time.Time
		if field.Kind() == reflect.Ptr && field.Type().String() == "*time.Time" {
			if !field.IsNil() {
				timeVal := field.Interface().(*time.Time)
				return timeVal.Format("2006-01-02T15:04:05Z")
			}
		}
	}
	return ""
}

// extractPaginationToken extracts pagination token from response
func (s *{{.ServiceNameCaps}}Scanner) extractPaginationToken(response interface{}) string {
	responseValue := reflect.ValueOf(response)
	if responseValue.Kind() == reflect.Ptr {
		responseValue = responseValue.Elem()
	}

	tokenFields := []string{"NextToken", "Marker", "ContinuationToken", "PageToken"}
	for _, fieldName := range tokenFields {
		field := responseValue.FieldByName(fieldName)
		if !field.IsValid() {
			continue
		}

		if field.Kind() == reflect.Ptr && field.Elem().Kind() == reflect.String {
			return field.Elem().String()
		} else if field.Kind() == reflect.String {
			return field.String()
		}
	}

	return ""
}

// setPaginationToken sets pagination token in request
func (s *{{.ServiceNameCaps}}Scanner) setPaginationToken(request interface{}, token string) {
	requestValue := reflect.ValueOf(request)
	if requestValue.Kind() == reflect.Ptr {
		requestValue = requestValue.Elem()
	}

	tokenFields := []string{"NextToken", "Marker", "ContinuationToken", "PageToken"}
	for _, fieldName := range tokenFields {
		field := requestValue.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
			tokenPtr := &token
			field.Set(reflect.ValueOf(tokenPtr))
			return
		}
	}
}

// DescribeResource gets detailed information about a specific resource
func (s *{{.ServiceNameCaps}}Scanner) DescribeResource(ctx context.Context, id string) (*pb.ResourceRef, error) {
	client, err := s.clientFactory.Get{{.ServiceNameCaps}}Client(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get {{.ServiceName}} client: %w", err)
	}

	// Try to determine resource type from the first resource type available
	{{if .Resources}}
	resourceType := "{{(index .Resources 0).Name}}"
	{{else}}
	resourceType := "Resource"
	{{end}}
	
	// Use generic configuration collection
	return s.describeResourceWithConfig(ctx, client, id, resourceType)
}

// describeResourceWithConfig collects comprehensive configuration for any resource
func (s *{{.ServiceNameCaps}}Scanner) describeResourceWithConfig(ctx context.Context, client *{{.ServiceName}}.Client, resourceID string, resourceType string) (*pb.ResourceRef, error) {
	// Create base resource reference
	ref := &pb.ResourceRef{
		Id:      resourceID,
		Name:    resourceID,
		Type:    resourceType,
		Service: "{{.ServiceName}}",
	}

	// Collect all configuration data
	configData := make(map[string]interface{})

	{{range .ConfigOperations}}
	// {{.Name}} - {{.Description}}
	if {{.OutputFieldName}}, err := s.call{{.Name}}(ctx, client, resourceID); err == nil {
		configData["{{.ConfigKey}}"] = {{.OutputFieldName}}
	} else {
		// Some configurations may not exist - that's normal for some resources
		fmt.Printf("Debug: Could not get {{.ConfigKey}} for %s %s: %v\n", resourceType, resourceID, err)
	}

	{{end}}
	// Store configuration as JSON in RawData
	if configJSON, err := json.Marshal(configData); err == nil {
		ref.RawData = string(configJSON)
	}

	return ref, nil
}

{{range .ConfigOperations}}
// call{{.Name}} makes a {{.Name}} API call with appropriate input parameters
func (s *{{$.ServiceNameCaps}}Scanner) call{{.Name}}(ctx context.Context, client *{{$.ServiceName}}.Client, resourceID string) (*{{$.ServiceName}}.{{.OutputType}}, error) {
	input := &{{$.ServiceName}}.{{.InputType}}{}
	
	// Use reflection to set the resource identifier field
	s.setResourceIDField(input, resourceID)
	
	return client.{{.Name}}(ctx, input)
}

{{end}}

// setResourceIDField sets the appropriate resource ID field using reflection
func (s *{{.ServiceNameCaps}}Scanner) setResourceIDField(input interface{}, resourceID string) {
	inputValue := reflect.ValueOf(input)
	if inputValue.Kind() == reflect.Ptr {
		inputValue = inputValue.Elem()
	}

	// Common field names for resource identifiers
	idFields := []string{
		"Bucket",           // S3
		"InstanceId",       // EC2
		"InstanceIds",      // EC2 (array)
		"Name",             // Generic
		"Id",               // Generic
		"Arn",              // Generic
		"ResourceArn",      // Generic
		"FunctionName",     // Lambda
		"ClusterName",      // EKS/ECS
		"GroupName",        // Security Groups
	}

	for _, fieldName := range idFields {
		field := inputValue.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.Ptr:
			if field.Type().Elem().Kind() == reflect.String {
				// *string field
				field.Set(reflect.ValueOf(&resourceID))
				return
			}
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.String {
				// []string field (like InstanceIds)
				field.Set(reflect.ValueOf([]string{resourceID}))
				return
			}
		case reflect.String:
			// string field
			field.SetString(resourceID)
			return
		}
	}
}

{{if .WithRetry}}
// retryListBuckets performs ListBuckets operation with exponential backoff
func (s *{{.ServiceNameCaps}}Scanner) retryListBuckets(ctx context.Context, client *{{.ServiceName}}.Client, input *{{.ServiceName}}.ListBucketsInput) (*{{.ServiceName}}.ListBucketsOutput, error) {
	var result *{{.ServiceName}}.ListBucketsOutput
	var err error
	
	maxRetries := 3
	baseDelay := time.Millisecond * 100
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err = client.ListBuckets(ctx, input)
		if err == nil {
			return result, nil
		}
		
		if attempt == maxRetries {
			break
		}
		
		// Exponential backoff
		delay := time.Duration(1<<uint(attempt)) * baseDelay
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(delay):
			continue
		}
	}
	
	return result, err
}

// retryDescribeOperation performs describe operations with exponential backoff
func (s *{{.ServiceNameCaps}}Scanner) retryDescribeOperation(ctx context.Context, operation func() (interface{}, error)) (interface{}, error) {
	var result interface{}
	var err error
	
	maxRetries := 3
	baseDelay := time.Millisecond * 100
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err = operation()
		if err == nil {
			return result, nil
		}
		
		if attempt == maxRetries {
			break
		}
		
		// Exponential backoff
		delay := time.Duration(1<<uint(attempt)) * baseDelay
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(delay):
			continue
		}
	}
	
	return result, err
}
{{end}}
`
}


package main

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UnifiedScanner provides generic scanning capabilities for all AWS services
type UnifiedScanner struct {
	clientFactory        *ClientFactory
	explorer             *ResourceExplorer
	relationshipExtractor *RelationshipExtractor
	mu                   sync.RWMutex
}

// NewUnifiedScanner creates a new unified scanner
func NewUnifiedScanner(clientFactory *ClientFactory) *UnifiedScanner {
	return &UnifiedScanner{
		clientFactory:        clientFactory,
		relationshipExtractor: NewRelationshipExtractor(),
	}
}

// SetResourceExplorer sets the Resource Explorer for high-performance scanning
func (s *UnifiedScanner) SetResourceExplorer(explorer *ResourceExplorer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.explorer = explorer
}

// ScanService scans all resources for a specific service
func (s *UnifiedScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	// Try Resource Explorer first if available
	s.mu.RLock()
	explorer := s.explorer
	s.mu.RUnlock()

	if explorer != nil {
		resources, err := explorer.QueryByService(ctx, serviceName)
		if err == nil && len(resources) > 0 {
			log.Printf("Resource Explorer found %d resources for service %s", len(resources), serviceName)
			return resources, nil
		}
		log.Printf("Resource Explorer query failed or returned no results for %s, falling back to SDK", serviceName)
	}

	// Fall back to SDK scanning
	return s.scanViaSDK(ctx, serviceName)
}

// ScanAllServices scans all available services
func (s *UnifiedScanner) ScanAllServices(ctx context.Context) ([]*pb.ResourceRef, error) {
	// Try Resource Explorer first if available
	s.mu.RLock()
	explorer := s.explorer
	s.mu.RUnlock()

	if explorer != nil {
		resources, err := explorer.QueryAllResources(ctx)
		if err == nil {
			log.Printf("Resource Explorer found %d total resources", len(resources))
			return resources, nil
		}
		log.Printf("Resource Explorer query failed, falling back to SDK: %v", err)
	}

	// Fall back to SDK scanning all services
	var allResources []*pb.ResourceRef
	availableServices := s.clientFactory.GetAvailableServices()

	for _, service := range availableServices {
		resources, err := s.scanViaSDK(ctx, service)
		if err != nil {
			log.Printf("Failed to scan service %s: %v", service, err)
			continue
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// scanViaSDK scans a service using the AWS SDK with reflection
func (s *UnifiedScanner) scanViaSDK(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	client := s.clientFactory.GetClient(serviceName)
	if client == nil {
		return nil, fmt.Errorf("no client available for service: %s", serviceName)
	}

	var allResources []*pb.ResourceRef

	// Find all List operations for this service
	listOps := s.findListOperations(client)
	log.Printf("Found %d list operations for service %s: %v", len(listOps), serviceName, listOps)

	for _, opName := range listOps {
		resources, err := s.invokeListOperation(ctx, client, opName, serviceName)
		if err != nil {
			log.Printf("Failed to invoke %s for service %s: %v", opName, serviceName, err)
			continue
		}
		log.Printf("Operation %s returned %d resources", opName, len(resources))
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// findListOperations discovers list operations using reflection
func (s *UnifiedScanner) findListOperations(client interface{}) []string {
	var listOps []string
	
	clientValue := reflect.ValueOf(client)
	clientType := clientValue.Type()
	
	log.Printf("Debug: Analyzing client type: %v", clientType)
	
	// For AWS SDK clients, methods are on the pointer type, not the value type
	// So we should analyze the pointer type directly
	log.Printf("Debug: Client has %d methods", clientType.NumMethod())
	
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		methodName := method.Name
		
		// Debug: Show all methods with List or Describe
		if strings.Contains(methodName, "List") || strings.Contains(methodName, "Describe") {
			log.Printf("Debug: Found potential list method: %s", methodName)
		}

		if s.isListOperation(methodName) {
			listOps = append(listOps, methodName)
			log.Printf("Debug: Found list operation: %s", methodName)
		}
	}

	return listOps
}

// isListOperation determines if a method is a list operation
func (s *UnifiedScanner) isListOperation(methodName string) bool {
	// Always include these core operations
	coreOperations := []string{
		"ListBuckets", "ListTables", "ListFunctions", "DescribeInstances",
		"DescribeVolumes", "DescribeSecurityGroups", "DescribeVpcs",
		"DescribeDBInstances", "DescribeDBClusters", "ListUsers", "ListRoles",
	}
	
	for _, op := range coreOperations {
		if methodName == op {
			log.Printf("Debug: Matched core operation: %s", methodName)
			return true
		}
	}

	// Exclude operations that are not resource listing
	excludePatterns := []string{
		"ListTagsFor", "ListBucketAnalytics", "ListBucketInventory",
		"ListBucketMetrics", "ListMultipart", "ListObjectVersions",
		"ListParts", "ListVersions", "ListAccountSettings",
		"ListContributorInsights", "ListBackups", "ListExports", "ListImports",
	}

	for _, pattern := range excludePatterns {
		if strings.Contains(methodName, pattern) {
			return false
		}
	}

	// Include common list operation patterns
	return strings.HasPrefix(methodName, "List") || 
		   strings.HasPrefix(methodName, "Describe")
}

// invokeListOperation dynamically invokes a list operation
func (s *UnifiedScanner) invokeListOperation(ctx context.Context, client interface{}, opName, serviceName string) ([]*pb.ResourceRef, error) {
	clientValue := reflect.ValueOf(client)
	method := clientValue.MethodByName(opName)

	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found on client", opName)
	}

	// Create input struct
	methodType := method.Type()
	if methodType.NumIn() < 2 {
		return nil, fmt.Errorf("method %s has unexpected signature", opName)
	}

	inputType := methodType.In(1)
	if inputType.Kind() == reflect.Ptr {
		inputType = inputType.Elem()
	}

	input := reflect.New(inputType)

	// Initialize common pagination fields if they exist
	s.initializePaginationFields(input.Elem())

	// Call the method
	results := method.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		input,
	})

	if len(results) < 2 {
		return nil, fmt.Errorf("method %s returned unexpected number of values", opName)
	}

	// Check for errors
	if errValue := results[1]; !errValue.IsNil() {
		return nil, errValue.Interface().(error)
	}

	// Extract resources from output
	output := results[0].Interface()
	return s.extractResources(output, serviceName, opName)
}

// initializePaginationFields initializes common pagination fields
func (s *UnifiedScanner) initializePaginationFields(inputValue reflect.Value) {
	if inputValue.Kind() != reflect.Struct {
		return
	}

	inputType := inputValue.Type()
	for i := 0; i < inputType.NumField(); i++ {
		field := inputType.Field(i)
		fieldValue := inputValue.Field(i)

		if !fieldValue.CanSet() {
			continue
		}

		fieldName := strings.ToLower(field.Name)

		// Set common pagination defaults
		switch fieldName {
		case "maxresults", "maxitems", "limit":
			if fieldValue.Kind() == reflect.Ptr && fieldValue.Type().Elem().Kind() == reflect.Int32 {
				val := int32(100) // Reasonable batch size
				fieldValue.Set(reflect.ValueOf(&val))
			}
		}
	}
}

// extractResources extracts resources from operation output using reflection
func (s *UnifiedScanner) extractResources(output interface{}, serviceName, opName string) ([]*pb.ResourceRef, error) {
	var resources []*pb.ResourceRef

	outputValue := reflect.ValueOf(output)
	if outputValue.Kind() == reflect.Ptr {
		outputValue = outputValue.Elem()
	}

	if outputValue.Kind() != reflect.Struct {
		return resources, nil
	}

	// Find fields that contain slices (likely resource lists)
	outputType := outputValue.Type()
	for i := 0; i < outputType.NumField(); i++ {
		field := outputValue.Field(i)
		fieldType := outputType.Field(i)

		if field.Kind() == reflect.Slice && field.Len() > 0 {
			// This looks like a resource list
			sliceResources := s.extractResourcesFromSlice(field, serviceName, fieldType.Name)
			resources = append(resources, sliceResources...)
		}
	}

	return resources, nil
}

// extractResourcesFromSlice extracts resources from a slice field
func (s *UnifiedScanner) extractResourcesFromSlice(sliceValue reflect.Value, serviceName, fieldName string) []*pb.ResourceRef {
	var resources []*pb.ResourceRef

	for i := 0; i < sliceValue.Len(); i++ {
		item := sliceValue.Index(i)
		if resource := s.extractResourceFromItem(item, serviceName, fieldName); resource != nil {
			resources = append(resources, resource)
		}
	}

	return resources
}

// extractResourceFromItem extracts a single resource from a struct
func (s *UnifiedScanner) extractResourceFromItem(item reflect.Value, serviceName, fieldName string) *pb.ResourceRef {
	if item.Kind() == reflect.Ptr {
		if item.IsNil() {
			return nil
		}
		item = item.Elem()
	}

	if item.Kind() != reflect.Struct {
		return nil
	}

	resource := &pb.ResourceRef{
		Service:         serviceName,
		BasicAttributes: make(map[string]string),
	}

	// Extract fields from the struct
	itemType := item.Type()
	for i := 0; i < itemType.NumField(); i++ {
		field := item.Field(i)
		fieldType := itemType.Field(i)
		fieldName := fieldType.Name

		if !field.IsValid() || !field.CanInterface() {
			continue
		}

		// Handle common field patterns
		s.extractFieldValue(field, fieldName, resource)
	}

	// Infer resource type from field name or existing data
	if resource.Type == "" {
		resource.Type = s.inferResourceType(serviceName, fieldName)
	}

	// Set default name if not found
	if resource.Name == "" && resource.Id != "" {
		resource.Name = s.extractNameFromId(resource.Id)
	}

	return resource
}

// extractFieldValue extracts a field value and maps it to the appropriate resource field
func (s *UnifiedScanner) extractFieldValue(field reflect.Value, fieldName string, resource *pb.ResourceRef) {
	fieldNameLower := strings.ToLower(fieldName)

	// Handle pointer fields
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return
		}
		field = field.Elem()
	}

	stringValue := s.getStringValue(field)
	if stringValue == "" {
		return
	}

	// Map common field names to resource properties
	switch fieldNameLower {
	case "id", "resourceid", "instanceid", "functionname", "tablename", "bucketname", "dbinstanceidentifier":
		resource.Id = stringValue
	case "name", "resourcename", "displayname":
		resource.Name = stringValue
	case "arn", "resourcearn":
		resource.Id = stringValue
		resource.BasicAttributes["arn"] = stringValue
	case "type", "resourcetype", "instancetype":
		resource.Type = stringValue
	case "region", "availabilityzone":
		if strings.Contains(fieldNameLower, "region") {
			resource.Region = stringValue
		} else {
			resource.BasicAttributes[fieldNameLower] = stringValue
		}
	case "state", "status":
		resource.BasicAttributes[fieldNameLower] = stringValue
	case "tags":
		s.extractTags(field, resource)
	default:
		// Store as basic attribute
		resource.BasicAttributes[fieldNameLower] = stringValue
	}
}

// getStringValue safely extracts a string value from a reflect.Value
func (s *UnifiedScanner) getStringValue(value reflect.Value) string {
	if !value.IsValid() || !value.CanInterface() {
		return ""
	}

	switch value.Kind() {
	case reflect.String:
		return value.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", value.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%f", value.Float())
	case reflect.Bool:
		return fmt.Sprintf("%t", value.Bool())
	default:
		return fmt.Sprintf("%v", value.Interface())
	}
}

// extractTags extracts tags from various tag field formats
func (s *UnifiedScanner) extractTags(field reflect.Value, resource *pb.ResourceRef) {
	if !field.IsValid() || !field.CanInterface() {
		return
	}

	// Handle slice of tag structures
	if field.Kind() == reflect.Slice {
		for i := 0; i < field.Len(); i++ {
			tagItem := field.Index(i)
			if tagItem.Kind() == reflect.Ptr {
				tagItem = tagItem.Elem()
			}

			if tagItem.Kind() == reflect.Struct {
				var key, value string
				
				// Extract key and value from tag struct
				for j := 0; j < tagItem.NumField(); j++ {
					tagField := tagItem.Field(j)
					tagFieldName := strings.ToLower(tagItem.Type().Field(j).Name)

					if tagFieldName == "key" || tagFieldName == "name" {
						key = s.getStringValue(tagField)
					} else if tagFieldName == "value" {
						value = s.getStringValue(tagField)
					}
				}

				if key != "" {
					resource.BasicAttributes["tag_"+key] = value
				}
			}
		}
	}
}

// inferResourceType infers the resource type from service and field name
func (s *UnifiedScanner) inferResourceType(serviceName, fieldName string) string {
	// Service-specific resource type mappings
	typeMap := map[string]map[string]string{
		"ec2": {
			"instances": "Instance",
			"volumes":   "Volume",
			"vpcs":      "Vpc",
			"subnets":   "Subnet",
		},
		"s3": {
			"buckets": "Bucket",
		},
		"lambda": {
			"functions": "Function",
		},
		"rds": {
			"dbinstances": "DBInstance",
			"dbclusters":  "DBCluster",
		},
		"dynamodb": {
			"tables": "Table",
		},
		"iam": {
			"users":    "User",
			"roles":    "Role",
			"policies": "Policy",
		},
	}

	if serviceMap, exists := typeMap[serviceName]; exists {
		fieldLower := strings.ToLower(fieldName)
		if resourceType, exists := serviceMap[fieldLower]; exists {
			return resourceType
		}
	}

	// Default: extract from field name
	if strings.HasSuffix(fieldName, "s") {
		return fieldName[:len(fieldName)-1]
	}

	return fieldName
}

// extractNameFromId extracts a name from an ID or ARN
func (s *UnifiedScanner) extractNameFromId(id string) string {
	// For ARNs, extract the resource name
	if strings.HasPrefix(id, "arn:") {
		parts := strings.Split(id, ":")
		if len(parts) >= 6 {
			resourcePart := strings.Join(parts[5:], ":")
			if strings.Contains(resourcePart, "/") {
				nameParts := strings.Split(resourcePart, "/")
				return nameParts[len(nameParts)-1]
			}
			return resourcePart
		}
	}

	// For other IDs, return as-is
	return id
}

// DescribeResource provides detailed information about a specific resource
func (s *UnifiedScanner) DescribeResource(ctx context.Context, resourceRef *pb.ResourceRef) (*pb.Resource, error) {
	// This would implement detailed resource description
	// For now, return basic resource information
	resource := &pb.Resource{
		Provider:     "aws",
		Service:      resourceRef.Service,
		Type:         resourceRef.Type,
		Id:           resourceRef.Id,
		Name:         resourceRef.Name,
		Region:       resourceRef.Region,
		Tags:         make(map[string]string),
		DiscoveredAt: timestamppb.Now(),
	}

	// Extract tags from basic attributes
	if resourceRef.BasicAttributes != nil {
		for k, v := range resourceRef.BasicAttributes {
			if strings.HasPrefix(k, "tag_") {
				tagName := strings.TrimPrefix(k, "tag_")
				resource.Tags[tagName] = v
			}
		}
	}

	// Extract relationships from the resource
	if s.relationshipExtractor != nil {
		relationships := s.relationshipExtractor.ExtractFromResourceRef(resourceRef)
		for _, rel := range relationships {
			resource.Relationships = append(resource.Relationships, rel)
		}
	}

	return resource, nil
}

// StreamScanResources streams resources as they are discovered
func (s *UnifiedScanner) StreamScanResources(ctx context.Context, services []string, resourceChan chan<- *pb.Resource) error {
	defer close(resourceChan)

	for _, service := range services {
		resourceRefs, err := s.ScanService(ctx, service)
		if err != nil {
			log.Printf("Failed to scan service %s: %v", service, err)
			continue
		}

		for _, ref := range resourceRefs {
			resource, err := s.DescribeResource(ctx, ref)
			if err != nil {
				log.Printf("Failed to describe resource %s: %v", ref.Id, err)
				continue
			}

			select {
			case resourceChan <- resource:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// ExtractRelationships extracts relationships from multiple resources
func (s *UnifiedScanner) ExtractRelationships(resources []*pb.ResourceRef) []*pb.Relationship {
	if s.relationshipExtractor == nil {
		return nil
	}
	return s.relationshipExtractor.ExtractFromMultipleResources(resources)
}
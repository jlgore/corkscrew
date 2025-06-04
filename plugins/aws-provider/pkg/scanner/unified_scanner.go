package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
	"sync"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UnifiedScanner provides generic scanning capabilities for all AWS services
type UnifiedScanner struct {
	clientFactory        ClientFactory
	explorer             ResourceExplorer
	relationshipExtractor RelationshipExtractor
	serviceRegistry      ServiceRegistry
	mu                   sync.RWMutex
}

// NewUnifiedScanner creates a new unified scanner
func NewUnifiedScanner(clientFactory ClientFactory) *UnifiedScanner {
	return &UnifiedScanner{
		clientFactory: clientFactory,
	}
}

// SetServiceRegistry sets the service registry for dynamic resource type discovery
func (s *UnifiedScanner) SetServiceRegistry(registry ServiceRegistry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serviceRegistry = registry
}

// SetResourceExplorer sets the Resource Explorer for high-performance scanning
func (s *UnifiedScanner) SetResourceExplorer(explorer ResourceExplorer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.explorer = explorer
}

// SetRelationshipExtractor sets the relationship extractor
func (s *UnifiedScanner) SetRelationshipExtractor(extractor RelationshipExtractor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relationshipExtractor = extractor
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

	// Generate ARN if not provided and use as unique ID
	s.ensureARNAsID(resource, serviceName)

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

	// Use dynamic field discovery with pattern matching
	if s.isIdentifierField(fieldNameLower) {
		resource.Id = stringValue
	} else if s.isNameField(fieldNameLower) {
		resource.Name = stringValue
	} else if s.isARNField(fieldNameLower) {
		resource.Id = stringValue
		resource.BasicAttributes["arn"] = stringValue
	} else if s.isTypeField(fieldNameLower) {
		resource.Type = stringValue
	} else if s.isRegionField(fieldNameLower) {
		resource.Region = stringValue
	} else if s.isStateField(fieldNameLower) {
		resource.BasicAttributes[fieldNameLower] = stringValue
	} else if fieldNameLower == "tags" {
		s.extractTags(field, resource)
	} else {
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

// inferResourceType infers the resource type using registry or reflection patterns
func (s *UnifiedScanner) inferResourceType(serviceName, fieldName string) string {
	// Try registry first if available
	s.mu.RLock()
	registry := s.serviceRegistry
	s.mu.RUnlock()
	
	if registry != nil {
		if serviceDef, exists := registry.GetService(serviceName); exists {
			fieldLower := strings.ToLower(fieldName)
			
			// Match field name to resource types in registry
			for _, resourceType := range serviceDef.ResourceTypes {
				resourceNameLower := strings.ToLower(resourceType.Name)
				// Check if field name matches resource type (with or without 's' suffix)
				if fieldLower == resourceNameLower || 
				   fieldLower == resourceNameLower+"s" ||
				   strings.Contains(fieldLower, resourceNameLower) {
					return resourceType.Name
				}
			}
		}
	}
	
	// Fallback to reflection-based patterns if registry lookup fails
	return s.inferResourceTypeFromReflection(fieldName)
}

// inferResourceTypeFromReflection uses pattern matching to infer resource types
func (s *UnifiedScanner) inferResourceTypeFromReflection(fieldName string) string {
	// Common patterns for AWS resource naming
	patterns := map[string]string{
		// EC2 patterns
		"instances":   "Instance",
		"volumes":     "Volume",
		"snapshots":   "Snapshot",
		"vpcs":        "Vpc",
		"subnets":     "Subnet",
		"securitygroups": "SecurityGroup",
		"routetables": "RouteTable",
		"internetgateways": "InternetGateway",
		
		// S3 patterns
		"buckets":     "Bucket",
		"objects":     "Object",
		
		// Lambda patterns
		"functions":   "Function",
		"layers":      "Layer",
		
		// RDS patterns
		"dbinstances": "DBInstance",
		"dbclusters":  "DBCluster",
		"dbsnapshots": "DBSnapshot",
		
		// DynamoDB patterns
		"tables":      "Table",
		
		// IAM patterns
		"users":       "User",
		"roles":       "Role",
		"policies":    "Policy",
		"groups":      "Group",
		
		// EKS patterns
		"clusters":    "Cluster",
		"nodegroups": "NodeGroup",
		
		// ELB patterns
		"loadbalancers": "LoadBalancer",
		"targetgroups":  "TargetGroup",
	}
	
	fieldLower := strings.ToLower(fieldName)
	
	// Direct pattern match
	if resourceType, exists := patterns[fieldLower]; exists {
		return resourceType
	}
	
	// Try to match partial patterns
	for pattern, resourceType := range patterns {
		if strings.Contains(fieldLower, pattern) {
			return resourceType
		}
	}
	
	// Default: extract from field name using common conventions
	if strings.HasSuffix(fieldName, "s") {
		return fieldName[:len(fieldName)-1]
	}
	
	return fieldName
}

// Dynamic field discovery methods using reflection patterns

// isIdentifierField checks if a field name represents a resource identifier
func (s *UnifiedScanner) isIdentifierField(fieldName string) bool {
	patterns := []string{
		"id", "resourceid", "instanceid", "functionname", "tablename", 
		"bucketname", "dbinstanceidentifier", "clustername", "groupname",
		"policyname", "rolename", "username", "keyid", "queueurl",
		"topicarn", "streamname", "domainname", "repositoryname",
	}
	
	for _, pattern := range patterns {
		if strings.Contains(fieldName, pattern) {
			return true
		}
	}
	
	return false
}

// isNameField checks if a field represents a resource name
func (s *UnifiedScanner) isNameField(fieldName string) bool {
	patterns := []string{
		"name", "resourcename", "displayname", "title", "label",
	}
	
	for _, pattern := range patterns {
		if fieldName == pattern || strings.HasSuffix(fieldName, pattern) {
			return true
		}
	}
	
	return false
}

// isARNField checks if a field represents an ARN
func (s *UnifiedScanner) isARNField(fieldName string) bool {
	patterns := []string{
		"arn", "resourcearn", "targetarn", "sourcearn",
	}
	
	for _, pattern := range patterns {
		if strings.Contains(fieldName, pattern) {
			return true
		}
	}
	
	return false
}

// isTypeField checks if a field represents a resource type
func (s *UnifiedScanner) isTypeField(fieldName string) bool {
	patterns := []string{
		"type", "resourcetype", "instancetype", "kind",
	}
	
	for _, pattern := range patterns {
		if fieldName == pattern {
			return true
		}
	}
	
	return false
}

// isRegionField checks if a field represents a region
func (s *UnifiedScanner) isRegionField(fieldName string) bool {
	patterns := []string{
		"region", "availability", "placement",
	}
	
	for _, pattern := range patterns {
		if strings.Contains(fieldName, pattern) && strings.Contains(fieldName, "region") {
			return true
		}
	}
	
	return fieldName == "region"
}

// isStateField checks if a field represents a state or status
func (s *UnifiedScanner) isStateField(fieldName string) bool {
	patterns := []string{
		"state", "status", "health", "condition",
	}
	
	for _, pattern := range patterns {
		if strings.Contains(fieldName, pattern) {
			return true
		}
	}
	
	return false
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

// ensureARNAsID ensures the resource has an ARN as its ID for uniqueness
func (s *UnifiedScanner) ensureARNAsID(resource *pb.ResourceRef, serviceName string) {
	// If ID is already an ARN, we're good
	if strings.HasPrefix(resource.Id, "arn:") {
		return
	}

	// Generate ARN based on service and resource type
	if resource.Id != "" {
		resource.Id = s.generateARN(serviceName, resource.Type, resource.Id, resource.Region)
	}
}

// generateARN generates an ARN for a resource
func (s *UnifiedScanner) generateARN(service, resourceType, resourceId, region string) string {
	if resourceId == "" {
		return ""
	}

	// Handle services that don't use regions
	if service == "iam" || service == "s3" {
		region = ""
	}

	// Default region if not specified
	if region == "" && service != "iam" && service != "s3" {
		region = "us-east-1"
	}

	return fmt.Sprintf("arn:aws:%s:%s::%s/%s", service, region, resourceType, resourceId)
}

// DescribeResource provides detailed information about a specific resource
func (s *UnifiedScanner) DescribeResource(ctx context.Context, resourceRef *pb.ResourceRef) (*pb.Resource, error) {
	log.Printf("DEBUG: UnifiedScanner.DescribeResource called for %s:%s in region %s", resourceRef.Service, resourceRef.Id, resourceRef.Region)
	
	// Create base resource with basic information
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

	// Collect detailed configuration based on service and resource type
	log.Printf("DEBUG: About to call collectDetailedConfiguration for %s:%s", resourceRef.Service, resourceRef.Id)
	rawData, attributes, err := s.collectDetailedConfiguration(ctx, resourceRef)
	if err != nil {
		log.Printf("DEBUG: Failed to collect detailed configuration for %s %s: %v", resourceRef.Service, resourceRef.Id, err)
		// Continue with basic resource information even if detailed config fails
	} else {
		log.Printf("DEBUG: Successfully collected detailed configuration for %s:%s, rawData length: %d", resourceRef.Service, resourceRef.Id, len(rawData))
		resource.RawData = rawData
		if attributesJSON, err := json.Marshal(attributes); err == nil {
			resource.Attributes = string(attributesJSON)
		}
	}

	// Extract relationships from the resource
	s.mu.RLock()
	extractor := s.relationshipExtractor
	s.mu.RUnlock()
	
	if extractor != nil {
		relationships := extractor.ExtractFromResourceRef(resourceRef)
		for _, rel := range relationships {
			resource.Relationships = append(resource.Relationships, rel)
		}
	}

	return resource, nil
}

// collectDetailedConfiguration collects detailed configuration data for any AWS resource
func (s *UnifiedScanner) collectDetailedConfiguration(ctx context.Context, resourceRef *pb.ResourceRef) (string, map[string]string, error) {
	log.Printf("Debug: collectDetailedConfiguration called for %s:%s", resourceRef.Service, resourceRef.Id)
	
	// Load the analysis data for this service
	analysisData, err := s.loadServiceAnalysis(resourceRef.Service)
	if err != nil {
		log.Printf("Debug: loadServiceAnalysis failed: %v", err)
		return "", make(map[string]string), nil // Not an error - just no enhanced data available
	}
	
	log.Printf("Debug: Successfully loaded analysis data for %s", resourceRef.Service)
	
	// Use reflection-based configuration collection
	return s.collectConfigurationFromAnalysis(ctx, resourceRef, analysisData)
}

// loadServiceAnalysis loads the JSON analysis data for a service
func (s *UnifiedScanner) loadServiceAnalysis(serviceName string) (map[string]interface{}, error) {
	// Try multiple paths where the analysis file might be located
	searchPaths := []string{
		fmt.Sprintf("generated/%s_final.json", serviceName),
		fmt.Sprintf("plugins/aws-provider/generated/%s_final.json", serviceName),
		fmt.Sprintf("/home/jg/git/corkscrew/plugins/aws-provider/generated/%s_final.json", serviceName),
		fmt.Sprintf("%s/.corkscrew/plugins/aws-provider/generated/%s_final.json", os.Getenv("HOME"), serviceName),
		fmt.Sprintf("%s/.corkscrew/bin/plugin/generated/%s_final.json", os.Getenv("HOME"), serviceName),
	}
	
	var data []byte
	var err error
	var foundPath string
	
	for _, path := range searchPaths {
		data, err = os.ReadFile(path)
		if err == nil {
			foundPath = path
			break
		}
	}
	
	if err != nil {
		// Return empty analysis - scanner will work with basic discovery only
		log.Printf("INFO: No analysis file for service %s, using basic discovery", serviceName)
		return map[string]interface{}{
			"services": []interface{}{
				map[string]interface{}{
					"name": serviceName,
					"operations": []interface{}{},
				},
			},
		}, nil
	}
	
	var analysis map[string]interface{}
	if err := json.Unmarshal(data, &analysis); err != nil {
		// Return empty analysis on parse error - scanner will work with basic discovery only
		log.Printf("WARN: Failed to parse analysis file for service %s: %v, using basic discovery", serviceName, err)
		return map[string]interface{}{
			"services": []interface{}{
				map[string]interface{}{
					"name": serviceName,
					"operations": []interface{}{},
				},
			},
		}, nil
	}
	
	log.Printf("DEBUG: Successfully loaded analysis data for %s from %s", serviceName, foundPath)
	return analysis, nil
}

// collectConfigurationFromAnalysis generically collects configuration using analysis data
func (s *UnifiedScanner) collectConfigurationFromAnalysis(ctx context.Context, resourceRef *pb.ResourceRef, analysis map[string]interface{}) (string, map[string]string, error) {
	log.Printf("DEBUG: collectConfigurationFromAnalysis starting for %s:%s", resourceRef.Service, resourceRef.Id)
	
	client := s.clientFactory.GetClient(resourceRef.Service)
	if client == nil {
		log.Printf("DEBUG: No client available for service: %s", resourceRef.Service)
		return "", nil, fmt.Errorf("no client for service: %s", resourceRef.Service)
	}
	
	// Extract operations from analysis
	services, ok := analysis["services"].([]interface{})
	if !ok || len(services) == 0 {
		log.Printf("DEBUG: No services found in analysis data")
		return "", make(map[string]string), nil
	}
	log.Printf("DEBUG: Found %d services in analysis", len(services))
	
	serviceData, ok := services[0].(map[string]interface{})
	if !ok {
		log.Printf("DEBUG: Could not extract service data from analysis")
		return "", make(map[string]string), nil
	}
	
	operations, ok := serviceData["operations"].([]interface{})
	if !ok {
		log.Printf("DEBUG: No operations found in service data")
		return "", make(map[string]string), nil
	}
	log.Printf("DEBUG: Found %d operations to process", len(operations))
	
	// Collect configuration data using reflection
	configData := make(map[string]interface{})
	
	for i, opInterface := range operations {
		op, ok := opInterface.(map[string]interface{})
		if !ok {
			log.Printf("DEBUG: Operation %d: could not convert to map", i)
			continue
		}
		
		// Skip list operations
		if isList, ok := op["is_list"].(bool); ok && isList {
			log.Printf("DEBUG: Operation %d: skipping list operation", i)
			continue
		}
		
		// This is a configuration operation
		operationName, ok := op["name"].(string)
		if !ok {
			log.Printf("DEBUG: Operation %d: no name found", i)
			continue
		}
		
		log.Printf("DEBUG: Processing operation %d: %s for resource %s", i, operationName, resourceRef.Id)
		
		// Call the operation using reflection
		result, err := s.callOperationReflectively(ctx, client, operationName, resourceRef.Id)
		if err != nil {
			log.Printf("DEBUG: Operation %s failed: %v", operationName, err)
			// Some configurations may not exist - that's normal
			continue
		}
		
		// Store the result
		configKey := strings.TrimPrefix(operationName, "Get")
		configKey = strings.TrimPrefix(configKey, "Describe")
		configData[configKey] = result
		log.Printf("DEBUG: Successfully stored config for key: %s", configKey)
	}
	
	// Convert to JSON
	log.Printf("DEBUG: Final configData has %d entries", len(configData))
	rawData := ""
	if len(configData) > 0 {
		if jsonData, err := json.Marshal(configData); err == nil {
			rawData = string(jsonData)
			log.Printf("DEBUG: Generated JSON rawData, length: %d", len(rawData))
		} else {
			log.Printf("DEBUG: Failed to marshal configData to JSON: %v", err)
		}
	} else {
		log.Printf("DEBUG: No configData collected, rawData will be empty")
	}
	
	// Extract key attributes for easier querying
	attributes := s.extractAttributesFromConfig(configData)
	
	return rawData, attributes, nil
}

// callOperationReflectively calls any AWS operation using reflection
func (s *UnifiedScanner) callOperationReflectively(ctx context.Context, client interface{}, operationName string, resourceID string) (interface{}, error) {
	clientValue := reflect.ValueOf(client)
	method := clientValue.MethodByName(operationName)
	
	if !method.IsValid() {
		return nil, fmt.Errorf("operation %s not found", operationName)
	}
	
	// Create input struct using reflection
	inputType := method.Type().In(1) // Second parameter (first is context)
	input := reflect.New(inputType.Elem()).Interface()
	
	// Set the resource ID field using our generic approach
	s.setResourceIDFieldGeneric(input, resourceID)
	
	// Call the method
	results := method.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(input),
	})
	
	// Check for error (second return value)
	if len(results) > 1 && !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}
	
	// Return the result (first return value)
	return results[0].Interface(), nil
}

// setResourceIDFieldGeneric sets resource ID using reflection (reusable across services)
func (s *UnifiedScanner) setResourceIDFieldGeneric(input interface{}, resourceID string) {
	inputValue := reflect.ValueOf(input)
	if inputValue.Kind() == reflect.Ptr {
		inputValue = inputValue.Elem()
	}

	// Extract bucket name from S3 ARN if needed
	actualID := resourceID
	if strings.HasPrefix(resourceID, "arn:aws:s3:::") {
		// Extract bucket name from ARN: arn:aws:s3:::bucket-name
		actualID = strings.TrimPrefix(resourceID, "arn:aws:s3:::")
		log.Printf("DEBUG: Extracted bucket name '%s' from ARN '%s'", actualID, resourceID)
	}

	// Common field names for resource identifiers across AWS services
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
		"DBInstanceIdentifier", // RDS
		"LoadBalancerName", // ELB
		"QueueUrl",         // SQS
		"TopicArn",         // SNS
	}

	for _, fieldName := range idFields {
		field := inputValue.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.Ptr:
			if field.Type().Elem().Kind() == reflect.String {
				field.Set(reflect.ValueOf(&actualID))
				log.Printf("DEBUG: Set field %s to %s", fieldName, actualID)
				return
			}
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.String {
				field.Set(reflect.ValueOf([]string{actualID}))
				return
			}
		case reflect.String:
			field.SetString(actualID)
			return
		}
	}
}

// extractAttributesFromConfig extracts commonly queried attributes from configuration data
func (s *UnifiedScanner) extractAttributesFromConfig(configData map[string]interface{}) map[string]string {
	attributes := make(map[string]string)
	
	// Extract common configuration patterns across AWS services
	for key, value := range configData {
		strValue := fmt.Sprintf("%v", value)
		
		// Add the raw config as an attribute
		attributes[fmt.Sprintf("config_%s", strings.ToLower(key))] = strValue
		
		// Extract boolean indicators for common compliance checks
		switch strings.ToLower(key) {
		case "encryption", "bucketencryption":
			attributes["encryption_enabled"] = "true"
		case "versioning", "bucketversioning":
			attributes["versioning_enabled"] = "true"
		case "policy", "bucketpolicy":
			attributes["policy_exists"] = "true"
		case "publicaccessblock":
			attributes["public_access_restricted"] = "true"
		case "logging", "bucketlogging":
			attributes["logging_enabled"] = "true"
		}
	}
	
	return attributes
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
	s.mu.RLock()
	extractor := s.relationshipExtractor
	s.mu.RUnlock()
	
	if extractor == nil {
		return nil
	}
	return extractor.ExtractFromMultipleResources(resources)
}
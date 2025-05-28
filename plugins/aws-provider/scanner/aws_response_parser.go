package scanner

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AWSResponseParser handles parsing of AWS API responses using reflection
type AWSResponseParser struct {
	debug bool
}

// NewAWSResponseParser creates a new AWS response parser
func NewAWSResponseParser(debug bool) *AWSResponseParser {
	return &AWSResponseParser{
		debug: debug,
	}
}

// ParseAWSResponse parses any AWS response and extracts resource information
func (p *AWSResponseParser) ParseAWSResponse(response interface{}, operationType, serviceName, region string) ([]*pb.ResourceRef, error) {
	if response == nil {
		return nil, fmt.Errorf("response is nil")
	}

	responseValue := reflect.ValueOf(response)
	if responseValue.Kind() == reflect.Ptr {
		if responseValue.IsNil() {
			return nil, fmt.Errorf("response pointer is nil")
		}
		responseValue = responseValue.Elem()
	}

	// Determine the resource type from the operation
	resourceType := p.inferResourceType(operationType, serviceName)

	// Look for resource lists in the response
	resources := p.extractResourcesFromResponse(responseValue, resourceType, serviceName, region)

	if p.debug {
		fmt.Printf("    📋 Parsed %d resources from %s response\n", len(resources), operationType)
	}

	return resources, nil
}

// extractResourcesFromResponse extracts resources from the response structure
func (p *AWSResponseParser) extractResourcesFromResponse(response reflect.Value, resourceType, serviceName, region string) []*pb.ResourceRef {
	var resources []*pb.ResourceRef

	// Handle different response patterns
	responseType := response.Type()
	
	for i := 0; i < response.NumField(); i++ {
		field := response.Field(i)
		fieldType := responseType.Field(i)
		fieldName := fieldType.Name

		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		// Check if this is a slice field that might contain resources
		if field.Kind() == reflect.Slice {
			// Check field name patterns
			if p.isResourceListField(fieldName, resourceType) {
				resources = append(resources, p.parseResourceList(field, resourceType, serviceName, region)...)
			}
		} else if field.Kind() == reflect.Ptr && !field.IsNil() {
			// Check for single resource responses (Get* operations)
			if p.isResourceField(fieldName, resourceType) {
				if ref := p.parseResourceItem(field.Elem(), resourceType, serviceName, region); ref != nil {
					resources = append(resources, ref)
				}
			}
		}
	}

	// If no resources found, try common patterns
	if len(resources) == 0 {
		resources = p.tryCommonPatterns(response, resourceType, serviceName, region)
	}

	return resources
}

// parseResourceList parses a slice of resources
func (p *AWSResponseParser) parseResourceList(listField reflect.Value, resourceType, serviceName, region string) []*pb.ResourceRef {
	var resources []*pb.ResourceRef

	for i := 0; i < listField.Len(); i++ {
		item := listField.Index(i)
		
		// Handle string slices (like DynamoDB TableNames)
		if item.Kind() == reflect.String {
			ref := &pb.ResourceRef{
				Id:     item.String(),
				Name:   item.String(),
				Type:   resourceType,
				Service: serviceName,
				Region: region,
			}
			resources = append(resources, ref)
		} else {
			// Handle struct/pointer items
			if ref := p.parseResourceItem(item, resourceType, serviceName, region); ref != nil {
				resources = append(resources, ref)
			}
		}
	}

	return resources
}

// parseResourceItem parses a single resource item
func (p *AWSResponseParser) parseResourceItem(item reflect.Value, resourceType, serviceName, region string) *pb.ResourceRef {
	if item.Kind() == reflect.Ptr {
		if item.IsNil() {
			return nil
		}
		item = item.Elem()
	}

	if item.Kind() != reflect.Struct {
		return nil
	}

	ref := &pb.ResourceRef{
		Type:             resourceType,
		Service:          serviceName,
		Region:           region,
		BasicAttributes:  make(map[string]string),
	}

	// Extract fields using reflection
	itemType := item.Type()
	for i := 0; i < item.NumField(); i++ {
		field := item.Field(i)
		fieldType := itemType.Field(i)
		fieldName := fieldType.Name

		if !fieldType.IsExported() || !field.IsValid() {
			continue
		}

		// Extract ID
		if p.isIDField(fieldName, resourceType) && ref.Id == "" {
			ref.Id = p.extractStringValue(field)
		}

		// Extract Name
		if p.isNameField(fieldName, resourceType) && ref.Name == "" {
			ref.Name = p.extractStringValue(field)
		}

		// Extract ARN
		if p.isARNField(fieldName) {
			if arn := p.extractStringValue(field); arn != "" {
				ref.BasicAttributes["arn"] = arn
			}
		}

		// Extract Status
		if p.isStatusField(fieldName) {
			if status := p.extractStringValue(field); status != "" {
				ref.BasicAttributes["status"] = status
			}
		}

		// Extract Tags
		if p.isTagField(fieldName) {
			tags := p.extractTags(field)
			for k, v := range tags {
				ref.BasicAttributes["tag:"+k] = v
			}
		}

		// Extract timestamps
		if p.isCreatedAtField(fieldName) {
			if ts := p.extractTimestamp(field); ts != nil {
				ref.BasicAttributes["created_at"] = ts.AsTime().Format(time.RFC3339)
			}
		}

		if p.isModifiedAtField(fieldName) {
			if ts := p.extractTimestamp(field); ts != nil {
				ref.BasicAttributes["modified_at"] = ts.AsTime().Format(time.RFC3339)
			}
		}
	}

	// Use ID as name if name is empty
	if ref.Name == "" && ref.Id != "" {
		ref.Name = ref.Id
	}

	// Generate ID if still empty
	if ref.Id == "" {
		// Try to generate from ARN
		if arn, ok := ref.BasicAttributes["arn"]; ok && arn != "" {
			ref.Id = p.extractIDFromARN(arn)
		} else {
			// Skip resources without any identifier
			return nil
		}
	}

	return ref
}

// extractStringValue extracts a string value from various field types
func (p *AWSResponseParser) extractStringValue(field reflect.Value) string {
	if !field.IsValid() {
		return ""
	}

	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return ""
		}
		field = field.Elem()
	}

	if field.Kind() == reflect.String {
		return field.String()
	}

	return ""
}

// extractTags extracts tags from various tag field formats
func (p *AWSResponseParser) extractTags(field reflect.Value) map[string]string {
	tags := make(map[string]string)

	if field.Kind() == reflect.Ptr && !field.IsNil() {
		field = field.Elem()
	}

	// Handle slice of tag objects
	if field.Kind() == reflect.Slice {
		for i := 0; i < field.Len(); i++ {
			tagItem := field.Index(i)
			if tagItem.Kind() == reflect.Ptr && !tagItem.IsNil() {
				tagItem = tagItem.Elem()
			}

			if tagItem.Kind() == reflect.Struct {
				var key, value string
				tagType := tagItem.Type()
				
				for j := 0; j < tagItem.NumField(); j++ {
					tagField := tagItem.Field(j)
					tagFieldName := tagType.Field(j).Name

					if strings.EqualFold(tagFieldName, "key") {
						key = p.extractStringValue(tagField)
					} else if strings.EqualFold(tagFieldName, "value") {
						value = p.extractStringValue(tagField)
					}
				}

				if key != "" {
					tags[key] = value
				}
			}
		}
	} else if field.Kind() == reflect.Map {
		// Handle map[string]string tags
		iter := field.MapRange()
		for iter.Next() {
			k := iter.Key()
			v := iter.Value()
			if k.Kind() == reflect.String && v.Kind() == reflect.String {
				tags[k.String()] = v.String()
			}
		}
	}

	return tags
}

// extractTimestamp extracts timestamp from time.Time fields
func (p *AWSResponseParser) extractTimestamp(field reflect.Value) *timestamppb.Timestamp {
	if !field.IsValid() {
		return nil
	}

	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return nil
		}
		field = field.Elem()
	}

	// Check if it's a time.Time
	if field.Type() == reflect.TypeOf(time.Time{}) {
		t := field.Interface().(time.Time)
		if !t.IsZero() {
			return timestamppb.New(t)
		}
	}

	return nil
}

// extractIDFromARN extracts ID from ARN
func (p *AWSResponseParser) extractIDFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		// Handle ARNs with paths
		if strings.Contains(lastPart, "/") {
			pathParts := strings.Split(lastPart, "/")
			return pathParts[len(pathParts)-1]
		}
		return lastPart
	}
	return ""
}

// tryCommonPatterns tries common response patterns
func (p *AWSResponseParser) tryCommonPatterns(response reflect.Value, resourceType, serviceName, region string) []*pb.ResourceRef {
	var resources []*pb.ResourceRef

	// Pattern 1: Check for fields ending with "s" (plural)
	responseType := response.Type()
	for i := 0; i < response.NumField(); i++ {
		field := response.Field(i)
		fieldType := responseType.Field(i)
		fieldName := fieldType.Name

		if !fieldType.IsExported() {
			continue
		}

		// Check for plural field names
		if strings.HasSuffix(fieldName, "s") && field.Kind() == reflect.Slice {
			singularName := strings.TrimSuffix(fieldName, "s")
			if strings.Contains(strings.ToLower(singularName), strings.ToLower(resourceType)) ||
			   strings.Contains(strings.ToLower(resourceType), strings.ToLower(singularName)) {
				resources = append(resources, p.parseResourceList(field, resourceType, serviceName, region)...)
			}
		}
	}

	return resources
}

// Field detection helpers

func (p *AWSResponseParser) isResourceListField(fieldName, resourceType string) bool {
	lowerFieldName := strings.ToLower(fieldName)
	lowerResourceType := strings.ToLower(resourceType)

	// Direct matches
	patterns := []string{
		lowerResourceType + "s",
		lowerResourceType + "list",
		lowerResourceType + "set",
		"items",
		"resources",
		"results",
	}

	for _, pattern := range patterns {
		if lowerFieldName == pattern {
			return true
		}
	}

	// Service-specific patterns
	servicePatterns := map[string][]string{
		"s3":       {"buckets", "contents", "uploads", "parts"},
		"ec2":      {"instances", "volumes", "snapshots", "images", "vpcs", "subnets", "securitygroups"},
		"iam":      {"users", "roles", "groups", "policies", "accesskeys"},
		"lambda":   {"functions", "layers", "aliases"},
		"dynamodb": {"tablenames", "tables"},
		"rds":      {"dbinstances", "dbclusters", "dbsnapshots"},
		"sns":      {"topics", "subscriptions"},
		"sqs":      {"queueurls"},
	}

	for _, patterns := range servicePatterns {
		for _, pattern := range patterns {
			if lowerFieldName == pattern {
				return true
			}
		}
	}

	return false
}

func (p *AWSResponseParser) isResourceField(fieldName, resourceType string) bool {
	lowerFieldName := strings.ToLower(fieldName)
	lowerResourceType := strings.ToLower(resourceType)

	return lowerFieldName == lowerResourceType ||
		   lowerFieldName == lowerResourceType+"detail" ||
		   lowerFieldName == lowerResourceType+"description"
}

func (p *AWSResponseParser) isIDField(fieldName, resourceType string) bool {
	patterns := []string{
		// Generic patterns
		"id", "identifier", "name", "key",
		// Service-specific patterns
		"instanceid", "volumeid", "snapshotid", "imageid", "vpcid", "subnetid",
		"groupid", "keyname", "functionname", "tablename", "topicarn", "queueurl",
		"dbinstanceidentifier", "dbclusteridentifier", "bucketname",
		"username", "rolename", "groupname", "policyarn",
	}

	lowerFieldName := strings.ToLower(fieldName)
	for _, pattern := range patterns {
		if lowerFieldName == pattern || strings.HasSuffix(lowerFieldName, pattern) {
			return true
		}
	}

	// Check for resource-specific ID patterns
	lowerResourceType := strings.ToLower(resourceType)
	return lowerFieldName == lowerResourceType+"id" ||
		   lowerFieldName == lowerResourceType+"name" ||
		   lowerFieldName == lowerResourceType+"identifier"
}

func (p *AWSResponseParser) isNameField(fieldName, resourceType string) bool {
	patterns := []string{
		"name", "displayname", "friendlyname", "title",
		"functionname", "tablename", "bucketname", "topicname",
		"username", "rolename", "groupname", "policyname",
		"dbinstanceidentifier", "dbname",
	}

	lowerFieldName := strings.ToLower(fieldName)
	for _, pattern := range patterns {
		if lowerFieldName == pattern {
			return true
		}
	}

	return false
}

func (p *AWSResponseParser) isARNField(fieldName string) bool {
	lowerFieldName := strings.ToLower(fieldName)
	return strings.Contains(lowerFieldName, "arn") ||
		   lowerFieldName == "topicarn" ||
		   lowerFieldName == "queueurl" ||
		   lowerFieldName == "resourcearn"
}

func (p *AWSResponseParser) isStatusField(fieldName string) bool {
	lowerFieldName := strings.ToLower(fieldName)
	return lowerFieldName == "status" ||
		   lowerFieldName == "state" ||
		   strings.HasSuffix(lowerFieldName, "status") ||
		   strings.HasSuffix(lowerFieldName, "state")
}

func (p *AWSResponseParser) isTagField(fieldName string) bool {
	lowerFieldName := strings.ToLower(fieldName)
	return lowerFieldName == "tags" ||
		   lowerFieldName == "tagset" ||
		   strings.HasSuffix(lowerFieldName, "tags")
}

func (p *AWSResponseParser) isCreatedAtField(fieldName string) bool {
	lowerFieldName := strings.ToLower(fieldName)
	patterns := []string{
		"createdat", "creationdate", "createtime", "createdtime",
		"launchtime", "starttime", "created",
	}

	for _, pattern := range patterns {
		if lowerFieldName == pattern || strings.Contains(lowerFieldName, pattern) {
			return true
		}
	}
	return false
}

func (p *AWSResponseParser) isModifiedAtField(fieldName string) bool {
	lowerFieldName := strings.ToLower(fieldName)
	patterns := []string{
		"modifiedat", "lastmodified", "updatedat", "updatedtime",
		"modifiedtime", "modified", "updated",
	}

	for _, pattern := range patterns {
		if lowerFieldName == pattern || strings.Contains(lowerFieldName, pattern) {
			return true
		}
	}
	return false
}

// inferResourceType infers the resource type from operation name
func (p *AWSResponseParser) inferResourceType(operationType, serviceName string) string {
	// Remove List/Describe/Get prefix
	resourceType := operationType
	prefixes := []string{"List", "Describe", "Get"}
	
	for _, prefix := range prefixes {
		if strings.HasPrefix(resourceType, prefix) {
			resourceType = strings.TrimPrefix(resourceType, prefix)
			break
		}
	}

	// Handle special cases
	specialCases := map[string]map[string]string{
		"s3": {
			"Buckets": "Bucket",
			"Objects": "Object",
			"ObjectsV2": "Object",
			"Parts": "Part",
			"Uploads": "Upload",
			"MultipartUploads": "MultipartUpload",
		},
		"ec2": {
			"Instances": "Instance",
			"Volumes": "Volume",
			"Snapshots": "Snapshot",
			"Images": "Image",
			"Vpcs": "Vpc",
			"Subnets": "Subnet",
			"SecurityGroups": "SecurityGroup",
			"KeyPairs": "KeyPair",
		},
		"iam": {
			"Users": "User",
			"Roles": "Role",
			"Groups": "Group",
			"Policies": "Policy",
			"AccessKeys": "AccessKey",
			"InstanceProfiles": "InstanceProfile",
		},
		"lambda": {
			"Functions": "Function",
			"Layers": "Layer",
			"Aliases": "Alias",
			"EventSourceMappings": "EventSourceMapping",
		},
	}

	if serviceMap, ok := specialCases[serviceName]; ok {
		if singular, ok := serviceMap[resourceType]; ok {
			return singular
		}
	}

	// Generic singularization
	if strings.HasSuffix(resourceType, "ies") {
		// Policies -> Policy
		return strings.TrimSuffix(resourceType, "ies") + "y"
	} else if strings.HasSuffix(resourceType, "es") && len(resourceType) > 3 {
		// Instances -> Instance
		return strings.TrimSuffix(resourceType, "es") + "e"
	} else if strings.HasSuffix(resourceType, "s") && len(resourceType) > 1 {
		// Tables -> Table
		return strings.TrimSuffix(resourceType, "s")
	}

	return resourceType
}

// HandlePaginatedResponse handles paginated AWS responses
func (p *AWSResponseParser) HandlePaginatedResponse(response interface{}) (nextToken string, hasMore bool) {
	responseValue := reflect.ValueOf(response)
	if responseValue.Kind() == reflect.Ptr {
		responseValue = responseValue.Elem()
	}

	// Common pagination field names
	paginationFields := []string{
		"NextToken", "NextMarker", "Marker", "ContinuationToken",
		"NextContinuationToken", "NextPageToken", "PaginationToken",
	}

	for _, fieldName := range paginationFields {
		if field := responseValue.FieldByName(fieldName); field.IsValid() {
			token := p.extractStringValue(field)
			if token != "" {
				return token, true
			}
		}
	}

	// Check for IsTruncated field (S3 pattern)
	if field := responseValue.FieldByName("IsTruncated"); field.IsValid() {
		if field.Kind() == reflect.Bool {
			hasMore = field.Bool()
		} else if field.Kind() == reflect.Ptr && !field.IsNil() {
			hasMore = field.Elem().Bool()
		}

		if hasMore {
			// Look for the actual token
			for _, fieldName := range []string{"NextMarker", "NextKeyMarker", "NextVersionIdMarker"} {
				if field := responseValue.FieldByName(fieldName); field.IsValid() {
					token := p.extractStringValue(field)
					if token != "" {
						return token, true
					}
				}
			}
		}
	}

	return "", false
}
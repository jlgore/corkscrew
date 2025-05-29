package main

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// RelationshipExtractor extracts relationships between AWS resources
type RelationshipExtractor struct {
	// Patterns for identifying relationship fields
	patterns map[string]string
	
	// ARN patterns for parsing resource references
	arnPatterns map[string]*regexp.Regexp
}

// NewRelationshipExtractor creates a new relationship extractor
func NewRelationshipExtractor() *RelationshipExtractor {
	return &RelationshipExtractor{
		patterns: initRelationshipPatterns(),
		arnPatterns: initARNPatterns(),
	}
}

// ExtractFromResource extracts relationships from a resource using reflection
func (r *RelationshipExtractor) ExtractFromResource(resource interface{}, resourceId, resourceType string) []*pb.Relationship {
	var relationships []*pb.Relationship

	val := reflect.ValueOf(resource)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return relationships
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return relationships
	}

	// Extract relationships from struct fields
	relationships = append(relationships, r.extractFromStruct(val, resourceId, resourceType)...)

	return relationships
}

// extractFromStruct extracts relationships from a struct using reflection
func (r *RelationshipExtractor) extractFromStruct(val reflect.Value, sourceId, sourceType string) []*pb.Relationship {
	var relationships []*pb.Relationship

	valType := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := valType.Field(i)
		fieldName := fieldType.Name

		if !field.IsValid() || !field.CanInterface() {
			continue
		}

		// Check if this field represents a relationship
		relType := r.getRelationshipType(fieldName)
		if relType == "" {
			continue
		}

		// Extract target IDs from the field
		targetIds := r.extractTargetIds(field, fieldName)
		
		for _, targetId := range targetIds {
			if targetId != "" && targetId != sourceId {
				properties := r.extractRelationshipProperties(field, fieldName)
				properties["source_id"] = sourceId
				properties["source_type"] = sourceType
				
				relationship := &pb.Relationship{
					TargetId:         targetId,
					TargetType:       r.inferTargetType(fieldName, targetId),
					RelationshipType: relType,
					Properties:       properties,
				}
				relationships = append(relationships, relationship)
			}
		}
	}

	return relationships
}

// getRelationshipType determines the relationship type based on field name
func (r *RelationshipExtractor) getRelationshipType(fieldName string) string {
	fieldLower := strings.ToLower(fieldName)
	
	// Check against known patterns
	for pattern, relType := range r.patterns {
		if strings.Contains(fieldLower, pattern) {
			return relType
		}
	}

	// Additional pattern-based detection
	switch {
	case strings.Contains(fieldLower, "vpc") && strings.Contains(fieldLower, "id"):
		return "contained_in"
	case strings.Contains(fieldLower, "subnet") && strings.Contains(fieldLower, "id"):
		return "deployed_in"
	case strings.Contains(fieldLower, "security") && strings.Contains(fieldLower, "group"):
		return "protected_by"
	case strings.Contains(fieldLower, "role") && strings.Contains(fieldLower, "arn"):
		return "assumes"
	case strings.Contains(fieldLower, "policy") && strings.Contains(fieldLower, "arn"):
		return "governed_by"
	case strings.Contains(fieldLower, "key") && (strings.Contains(fieldLower, "id") || strings.Contains(fieldLower, "arn")):
		return "encrypted_with"
	case strings.Contains(fieldLower, "target") && strings.Contains(fieldLower, "group"):
		return "targets"
	case strings.Contains(fieldLower, "load") && strings.Contains(fieldLower, "balancer"):
		return "load_balanced_by"
	case strings.Contains(fieldLower, "cluster") && strings.Contains(fieldLower, "arn"):
		return "belongs_to"
	case strings.Contains(fieldLower, "database") && strings.Contains(fieldLower, "name"):
		return "stores_data_in"
	case strings.Contains(fieldLower, "topic") && strings.Contains(fieldLower, "arn"):
		return "publishes_to"
	case strings.Contains(fieldLower, "queue") && strings.Contains(fieldLower, "url"):
		return "sends_messages_to"
	case strings.Contains(fieldLower, "stream") && strings.Contains(fieldLower, "arn"):
		return "streams_to"
	default:
		return ""
	}
}

// extractTargetIds extracts target resource IDs from a field
func (r *RelationshipExtractor) extractTargetIds(field reflect.Value, fieldName string) []string {
	var targetIds []string

	// Handle pointer fields
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return targetIds
		}
		field = field.Elem()
	}

	switch field.Kind() {
	case reflect.String:
		// Single string value
		value := field.String()
		if value != "" {
			targetIds = append(targetIds, value)
		}

	case reflect.Slice:
		// Array of strings or objects
		for i := 0; i < field.Len(); i++ {
			item := field.Index(i)
			if item.Kind() == reflect.Ptr {
				if item.IsNil() {
					continue
				}
				item = item.Elem()
			}

			if item.Kind() == reflect.String {
				value := item.String()
				if value != "" {
					targetIds = append(targetIds, value)
				}
			} else if item.Kind() == reflect.Struct {
				// Extract ID from struct (e.g., SecurityGroup struct with GroupId field)
				id := r.extractIdFromStruct(item)
				if id != "" {
					targetIds = append(targetIds, id)
				}
			}
		}

	case reflect.Struct:
		// Single struct value
		id := r.extractIdFromStruct(field)
		if id != "" {
			targetIds = append(targetIds, id)
		}
	}

	return targetIds
}

// extractIdFromStruct extracts an ID field from a struct
func (r *RelationshipExtractor) extractIdFromStruct(val reflect.Value) string {
	if val.Kind() != reflect.Struct {
		return ""
	}

	valType := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldName := strings.ToLower(valType.Field(i).Name)

		// Look for common ID field names
		if r.isIdField(fieldName) {
			if field.Kind() == reflect.Ptr {
				if field.IsNil() {
					continue
				}
				field = field.Elem()
			}

			if field.Kind() == reflect.String {
				return field.String()
			}
		}
	}

	return ""
}

// isIdField checks if a field name represents an ID field
func (r *RelationshipExtractor) isIdField(fieldName string) bool {
	idPatterns := []string{
		"id", "groupid", "instanceid", "volumeid", "subnetid", "vpcid",
		"arn", "resourcearn", "rolearn", "policyarn", "topicarn",
		"name", "resourcename", "queueurl", "streamarn",
	}

	for _, pattern := range idPatterns {
		if fieldName == pattern || strings.HasSuffix(fieldName, pattern) {
			return true
		}
	}

	return false
}

// inferTargetType infers the target resource type from field name and ID
func (r *RelationshipExtractor) inferTargetType(fieldName, targetId string) string {
	fieldLower := strings.ToLower(fieldName)

	// Parse ARN to get resource type
	if strings.HasPrefix(targetId, "arn:") {
		if resourceType := r.parseResourceTypeFromArn(targetId); resourceType != "" {
			return resourceType
		}
	}

	// Infer from field name patterns
	typeMapping := map[string]string{
		"vpc":             "AWS::EC2::Vpc",
		"subnet":          "AWS::EC2::Subnet",
		"securitygroup":   "AWS::EC2::SecurityGroup",
		"instance":        "AWS::EC2::Instance",
		"volume":          "AWS::EC2::Volume",
		"loadbalancer":    "AWS::ElasticLoadBalancing::LoadBalancer",
		"targetgroup":     "AWS::ElasticLoadBalancingV2::TargetGroup",
		"role":            "AWS::IAM::Role",
		"policy":          "AWS::IAM::Policy",
		"user":            "AWS::IAM::User",
		"bucket":          "AWS::S3::Bucket",
		"function":        "AWS::Lambda::Function",
		"table":           "AWS::DynamoDB::Table",
		"cluster":         "AWS::RDS::DBCluster",
		"database":        "AWS::RDS::DBInstance",
		"topic":           "AWS::SNS::Topic",
		"queue":           "AWS::SQS::Queue",
		"stream":          "AWS::Kinesis::Stream",
		"key":             "AWS::KMS::Key",
	}

	for pattern, resourceType := range typeMapping {
		if strings.Contains(fieldLower, pattern) {
			return resourceType
		}
	}

	return "Unknown"
}

// parseResourceTypeFromArn extracts resource type from an ARN
func (r *RelationshipExtractor) parseResourceTypeFromArn(arn string) string {
	// ARN format: arn:partition:service:region:account-id:resource-type/resource-id
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return ""
	}

	service := parts[2]
	resourcePart := strings.Join(parts[5:], ":")
	
	var resourceType string
	if strings.Contains(resourcePart, "/") {
		resourceType = strings.Split(resourcePart, "/")[0]
	} else {
		resourceType = resourcePart
	}

	// Convert to CloudFormation resource type format
	return fmt.Sprintf("AWS::%s::%s", strings.Title(service), strings.Title(resourceType))
}

// extractRelationshipProperties extracts additional properties for the relationship
func (r *RelationshipExtractor) extractRelationshipProperties(field reflect.Value, fieldName string) map[string]string {
	properties := make(map[string]string)
	
	// Add field name as a property
	properties["field_name"] = fieldName
	
	// Add field type information
	properties["field_type"] = field.Type().String()
	
	// Add relationship context based on field name
	fieldLower := strings.ToLower(fieldName)
	switch {
	case strings.Contains(fieldLower, "primary"):
		properties["relationship_context"] = "primary"
	case strings.Contains(fieldLower, "secondary"):
		properties["relationship_context"] = "secondary"
	case strings.Contains(fieldLower, "backup"):
		properties["relationship_context"] = "backup"
	case strings.Contains(fieldLower, "read"):
		properties["relationship_context"] = "read_replica"
	default:
		properties["relationship_context"] = "standard"
	}

	return properties
}

// ExtractFromResourceRef extracts relationships from a ResourceRef
func (r *RelationshipExtractor) ExtractFromResourceRef(resourceRef *pb.ResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship

	if resourceRef.BasicAttributes == nil {
		return relationships
	}

	sourceId := resourceRef.Id
	sourceType := resourceRef.Type

	// Extract relationships from basic attributes
	for key, value := range resourceRef.BasicAttributes {
		relType := r.getRelationshipType(key)
		if relType != "" && value != "" && value != sourceId {
			relationship := &pb.Relationship{
				TargetId:         value,
				TargetType:       r.inferTargetType(key, value),
				RelationshipType: relType,
				Properties: map[string]string{
					"field_name":           key,
					"relationship_context": "basic_attribute",
					"source_id":            sourceId,
					"source_type":          sourceType,
				},
			}
			relationships = append(relationships, relationship)
		}
	}

	return relationships
}

// ExtractFromMultipleResources extracts relationships between multiple resources
func (r *RelationshipExtractor) ExtractFromMultipleResources(resources []*pb.ResourceRef) []*pb.Relationship {
	var allRelationships []*pb.Relationship

	// Build a map for quick lookups
	resourceMap := make(map[string]*pb.ResourceRef)
	for _, resource := range resources {
		resourceMap[resource.Id] = resource
	}

	// Extract relationships from each resource
	for _, resource := range resources {
		relationships := r.ExtractFromResourceRef(resource)
		
		// Validate that target resources exist
		for _, rel := range relationships {
			if _, exists := resourceMap[rel.TargetId]; exists {
				allRelationships = append(allRelationships, rel)
			}
		}
	}

	// Extract implicit relationships (e.g., resources in same VPC)
	implicitRels := r.extractImplicitRelationships(resources)
	allRelationships = append(allRelationships, implicitRels...)

	return allRelationships
}

// extractImplicitRelationships finds implicit relationships between resources
func (r *RelationshipExtractor) extractImplicitRelationships(resources []*pb.ResourceRef) []*pb.Relationship {
	var relationships []*pb.Relationship

	// Group resources by common attributes (VPC, Subnet, etc.)
	vpcGroups := make(map[string][]*pb.ResourceRef)
	subnetGroups := make(map[string][]*pb.ResourceRef)

	for _, resource := range resources {
		if resource.BasicAttributes != nil {
			// Group by VPC
			if vpcId, ok := resource.BasicAttributes["vpcid"]; ok && vpcId != "" {
				vpcGroups[vpcId] = append(vpcGroups[vpcId], resource)
			}
			
			// Group by Subnet
			if subnetId, ok := resource.BasicAttributes["subnetid"]; ok && subnetId != "" {
				subnetGroups[subnetId] = append(subnetGroups[subnetId], resource)
			}
		}
	}

	// Create "peer" relationships for resources in the same VPC
	for vpcId, vpcResources := range vpcGroups {
		if len(vpcResources) > 1 {
			for i, resource1 := range vpcResources {
				for j, resource2 := range vpcResources {
					if i != j {
						relationships = append(relationships, &pb.Relationship{
							TargetId:         resource2.Id,
							TargetType:       resource2.Type,
							RelationshipType: "peer_in_vpc",
							Properties: map[string]string{
								"vpc_id":               vpcId,
								"relationship_context": "implicit",
								"source_id":            resource1.Id,
								"source_type":          resource1.Type,
							},
						})
					}
				}
			}
		}
	}

	// Create "co_located" relationships for resources in the same subnet
	for subnetId, subnetResources := range subnetGroups {
		if len(subnetResources) > 1 {
			for i, resource1 := range subnetResources {
				for j, resource2 := range subnetResources {
					if i != j {
						relationships = append(relationships, &pb.Relationship{
							TargetId:         resource2.Id,
							TargetType:       resource2.Type,
							RelationshipType: "co_located",
							Properties: map[string]string{
								"subnet_id":            subnetId,
								"relationship_context": "implicit",
								"source_id":            resource1.Id,
								"source_type":          resource1.Type,
							},
						})
					}
				}
			}
		}
	}

	return relationships
}

// initRelationshipPatterns initializes the field name to relationship type mapping
func initRelationshipPatterns() map[string]string {
	return map[string]string{
		"vpcid":            "contained_in",
		"subnetid":         "deployed_in",
		"subnetids":        "deployed_in",
		"securitygroupids": "protected_by",
		"securitygroups":   "protected_by",
		"rolearn":          "assumes",
		"executionrole":    "assumes",
		"servicerole":      "assumes",
		"targetgrouparn":   "targets",
		"targetgrouparns":  "targets",
		"loadbalancerarn":  "load_balanced_by",
		"clusterarn":       "belongs_to",
		"dbname":           "stores_data_in",
		"topicarn":         "publishes_to",
		"queueurl":         "sends_messages_to",
		"streamarn":        "streams_to",
		"kmskeyid":         "encrypted_with",
		"kmsarn":           "encrypted_with",
		"policyarn":        "governed_by",
		"policyarns":       "governed_by",
		"instanceid":       "runs_on",
		"volumeid":         "uses",
		"networkinterface": "uses",
		"elasticip":        "uses",
	}
}

// initARNPatterns initializes regex patterns for parsing ARNs
func initARNPatterns() map[string]*regexp.Regexp {
	patterns := make(map[string]*regexp.Regexp)
	
	// Common ARN pattern
	patterns["general"] = regexp.MustCompile(`^arn:aws:([^:]+):([^:]*):([^:]*):(.+)$`)
	
	// Specific service patterns
	patterns["s3"] = regexp.MustCompile(`^arn:aws:s3:::([^/]+)(/.*)?$`)
	patterns["iam"] = regexp.MustCompile(`^arn:aws:iam::([^:]*):([^/]+)/(.+)$`)
	patterns["lambda"] = regexp.MustCompile(`^arn:aws:lambda:([^:]+):([^:]+):function:(.+)$`)
	
	return patterns
}
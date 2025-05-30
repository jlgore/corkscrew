package discovery

import (
	"fmt"
	"strings"
)

// GraphRelationship represents a relationship between AWS resources in the graph
type GraphRelationship struct {
	SourceService  string `json:"source_service"`
	SourceResource string `json:"source_resource"`
	SourceField    string `json:"source_field"`
	TargetService  string `json:"target_service"`
	TargetResource string `json:"target_resource"`
	RelationType   string `json:"relation_type"` // "references", "contains", "manages", "depends_on"
	Cardinality    string `json:"cardinality"`   // "one-to-one", "one-to-many", "many-to-many"
}

// ResourceGraph represents the complete relationship graph between AWS resources
type ResourceGraph struct {
	Resources     map[string]*ResourceNode      `json:"resources"`
	Relationships []GraphRelationship        `json:"relationships"`
	ServiceMap    map[string][]string          `json:"service_map"` // service -> resource types
}

// ResourceNode represents a resource type in the graph
type ResourceNode struct {
	Service      string                 `json:"service"`
	ResourceType string                 `json:"resource_type"`
	InboundRefs  []GraphRelationship `json:"inbound_refs"`  // Other resources pointing to this
	OutboundRefs []GraphRelationship `json:"outbound_refs"` // This resource pointing to others
}

// BuildResourceRelationshipGraph analyzes service data to build a comprehensive relationship graph
func BuildResourceRelationshipGraph(services map[string]*ServiceAnalysis) *ResourceGraph {
	graph := &ResourceGraph{
		Resources:     make(map[string]*ResourceNode),
		Relationships: []GraphRelationship{},
		ServiceMap:    make(map[string][]string),
	}

	// First pass: Create nodes for all resources
	for serviceName, serviceAnalysis := range services {
		resourceTypes := []string{}
		
		for _, resourceType := range serviceAnalysis.ResourceTypes {
			nodeKey := fmt.Sprintf("%s::%s", serviceName, resourceType.Name)
			node := &ResourceNode{
				Service:      serviceName,
				ResourceType: resourceType.Name,
				InboundRefs:  []GraphRelationship{},
				OutboundRefs: []GraphRelationship{},
			}
			graph.Resources[nodeKey] = node
			resourceTypes = append(resourceTypes, resourceType.Name)
		}
		
		graph.ServiceMap[serviceName] = resourceTypes
	}

	// Second pass: Analyze fields and build relationships
	for serviceName, serviceAnalysis := range services {
		for _, resourceType := range serviceAnalysis.ResourceTypes {
			sourceKey := fmt.Sprintf("%s::%s", serviceName, resourceType.Name)
			
			// Analyze each field in the resource
			for _, field := range resourceType.Fields {
				relationships := analyzeFieldForRelationships(
					serviceName,
					resourceType.Name,
					field,
					services,
				)
				
				// Add relationships to graph
				for _, rel := range relationships {
					graph.Relationships = append(graph.Relationships, rel)
					
					// Update source node
					if sourceNode, exists := graph.Resources[sourceKey]; exists {
						sourceNode.OutboundRefs = append(sourceNode.OutboundRefs, rel)
					}
					
					// Update target node
					targetKey := fmt.Sprintf("%s::%s", rel.TargetService, rel.TargetResource)
					if targetNode, exists := graph.Resources[targetKey]; exists {
						targetNode.InboundRefs = append(targetNode.InboundRefs, rel)
					}
				}
			}
			
			// Also process relationships from the ResourceTypeAnalysis
			for _, rel := range resourceType.Relationships {
				relationship := convertToGraphRelationship(serviceName, resourceType.Name, rel)
				graph.Relationships = append(graph.Relationships, relationship)
				
				// Update nodes
				if sourceNode, exists := graph.Resources[sourceKey]; exists {
					sourceNode.OutboundRefs = append(sourceNode.OutboundRefs, relationship)
				}
				
				targetKey := fmt.Sprintf("%s::%s", relationship.TargetService, relationship.TargetResource)
				if targetNode, exists := graph.Resources[targetKey]; exists {
					targetNode.InboundRefs = append(targetNode.InboundRefs, relationship)
				}
			}
		}
	}

	// Third pass: Detect cross-service relationships
	crossServiceRels := detectCrossServiceRelationships(services)
	for _, rel := range crossServiceRels {
		graph.Relationships = append(graph.Relationships, rel)
		
		// Update nodes
		sourceKey := fmt.Sprintf("%s::%s", rel.SourceService, rel.SourceResource)
		if sourceNode, exists := graph.Resources[sourceKey]; exists {
			sourceNode.OutboundRefs = append(sourceNode.OutboundRefs, rel)
		}
		
		targetKey := fmt.Sprintf("%s::%s", rel.TargetService, rel.TargetResource)
		if targetNode, exists := graph.Resources[targetKey]; exists {
			targetNode.InboundRefs = append(targetNode.InboundRefs, rel)
		}
	}

	return graph
}

// analyzeFieldForRelationships analyzes a field to detect relationships
func analyzeFieldForRelationships(
	serviceName string,
	resourceType string,
	field FieldInfo,
	services map[string]*ServiceAnalysis,
) []GraphRelationship {
	relationships := []GraphRelationship{}
	
	fieldName := field.Name
	fieldNameLower := strings.ToLower(fieldName)
	
	// Pattern-based relationship detection
	if isIDReference(fieldNameLower) {
		targetInfo := inferTargetFromFieldName(fieldNameLower, services)
		// Avoid self-references
		if targetInfo.Service != "" && targetInfo.Resource != "" &&
		   !(targetInfo.Service == serviceName && targetInfo.Resource == resourceType) {
			rel := GraphRelationship{
				SourceService:  serviceName,
				SourceResource: resourceType,
				SourceField:    fieldName,
				TargetService:  targetInfo.Service,
				TargetResource: targetInfo.Resource,
				RelationType:   determineRelationType(fieldNameLower, field),
				Cardinality:    determineCardinality(field),
			}
			relationships = append(relationships, rel)
		}
	}
	
	// ARN field detection
	if isARNField(fieldNameLower) {
		rel := GraphRelationship{
			SourceService:  serviceName,
			SourceResource: resourceType,
			SourceField:    fieldName,
			TargetService:  "Multiple", // ARNs can reference various services
			TargetResource: "ARN",
			RelationType:   "references",
			Cardinality:    determineCardinality(field),
		}
		relationships = append(relationships, rel)
	}
	
	return relationships
}

// isIDReference checks if a field name indicates an ID reference
func isIDReference(fieldName string) bool {
	idPatterns := []string{
		"id", "ids", "_id", "_ids",
		"ref", "reference", 
		"name", "names", // Often used as references
	}
	
	// Exclude self-referencing fields (like InstanceId in Instance)
	if strings.HasSuffix(fieldName, "identifier") || 
	   strings.HasSuffix(fieldName, "name") && !strings.Contains(fieldName, "group") && !strings.Contains(fieldName, "role") && !strings.Contains(fieldName, "key") {
		// These are often self-referencing identifiers
		return false
	}
	
	// Check for specific AWS ID patterns
	awsIDPatterns := []string{
		"vpcid", "subnetid", "securitygroupid", 
		"volumeid", "snapshotid", "imageid", "keypair",
		"rolename", "rolearn", "policyarn", "userpoolid",
		"bucketname", "topicarn", "queueurl",
		"tablename", "streamarn", "clusterid",
		"loadbalancername", "targetgrouparn", "certificatearn",
	}
	
	for _, pattern := range idPatterns {
		if strings.HasSuffix(fieldName, pattern) {
			return true
		}
	}
	
	for _, pattern := range awsIDPatterns {
		if strings.Contains(fieldName, pattern) {
			return true
		}
	}
	
	return false
}

// isARNField checks if a field contains ARN
func isARNField(fieldName string) bool {
	return strings.Contains(fieldName, "arn") || strings.HasSuffix(fieldName, "arn")
}

// TargetInfo holds information about inferred target resource
type TargetInfo struct {
	Service  string
	Resource string
}

// inferTargetFromFieldName infers the target service and resource from field name
func inferTargetFromFieldName(fieldName string, services map[string]*ServiceAnalysis) TargetInfo {
	// Common AWS resource mappings
	resourceMappings := map[string]TargetInfo{
		"vpcid":                  {Service: "ec2", Resource: "Vpc"},
		"subnetid":              {Service: "ec2", Resource: "Subnet"},
		"subnetids":             {Service: "ec2", Resource: "Subnet"},
		"securitygroupid":       {Service: "ec2", Resource: "SecurityGroup"},
		"securitygroupids":      {Service: "ec2", Resource: "SecurityGroup"},
		"volumeid":              {Service: "ec2", Resource: "Volume"},
		"snapshotid":            {Service: "ec2", Resource: "Snapshot"},
		"imageid":               {Service: "ec2", Resource: "Image"},
		"amiid":                 {Service: "ec2", Resource: "Image"},
		"keypairname":           {Service: "ec2", Resource: "KeyPair"},
		"internetgatewayid":     {Service: "ec2", Resource: "InternetGateway"},
		"natgatewayid":          {Service: "ec2", Resource: "NatGateway"},
		"routetableid":          {Service: "ec2", Resource: "RouteTable"},
		"networkinterfaceid":    {Service: "ec2", Resource: "NetworkInterface"},
		"elasticipid":           {Service: "ec2", Resource: "ElasticIp"},
		"allocationid":          {Service: "ec2", Resource: "ElasticIp"},
		
		// IAM
		"rolearn":               {Service: "iam", Resource: "Role"},
		"rolename":              {Service: "iam", Resource: "Role"},
		"iaminstanceprofile":    {Service: "iam", Resource: "InstanceProfile"},
		"policyarn":             {Service: "iam", Resource: "Policy"},
		"userpoolid":            {Service: "cognito", Resource: "UserPool"},
		
		// Storage
		"bucketname":            {Service: "s3", Resource: "Bucket"},
		"bucket":                {Service: "s3", Resource: "Bucket"},
		
		// Database
		"dbclusteridentifier":   {Service: "rds", Resource: "DBCluster"},
		"dbsubnetgroupname":     {Service: "rds", Resource: "DBSubnetGroup"},
		"dbparametergroupname":  {Service: "rds", Resource: "DBParameterGroup"},
		
		// Compute
		"functionname":          {Service: "lambda", Resource: "Function"},
		"functionarn":           {Service: "lambda", Resource: "Function"},
		"clusterarn":            {Service: "ecs", Resource: "Cluster"},
		"clustername":           {Service: "ecs", Resource: "Cluster"},
		"taskdefinitionarn":     {Service: "ecs", Resource: "TaskDefinition"},
		"servicename":           {Service: "ecs", Resource: "Service"},
		"servicearn":            {Service: "ecs", Resource: "Service"},
		
		// Messaging
		"topicarn":              {Service: "sns", Resource: "Topic"},
		"queueurl":              {Service: "sqs", Resource: "Queue"},
		"queuename":             {Service: "sqs", Resource: "Queue"},
		"streamarn":             {Service: "kinesis", Resource: "Stream"},
		"streamname":            {Service: "kinesis", Resource: "Stream"},
		
		// Load Balancing
		"loadbalancername":      {Service: "elb", Resource: "LoadBalancer"},
		"loadbalancerarn":       {Service: "elbv2", Resource: "LoadBalancer"},
		"targetgrouparn":        {Service: "elbv2", Resource: "TargetGroup"},
		"targetgrouparns":       {Service: "elbv2", Resource: "TargetGroup"},
		
		// Security
		"keyid":                 {Service: "kms", Resource: "Key"},
		"kmskeyid":              {Service: "kms", Resource: "Key"},
		"certificatearn":        {Service: "acm", Resource: "Certificate"},
		
		// DynamoDB
		"tablename":             {Service: "dynamodb", Resource: "Table"},
		"tablearn":              {Service: "dynamodb", Resource: "Table"},
	}
	
	// Check direct mappings
	for pattern, target := range resourceMappings {
		if strings.Contains(fieldName, pattern) {
			// Verify the service exists in our analysis
			if _, exists := services[target.Service]; exists {
				return target
			}
		}
	}
	
	// Try to infer from field name patterns
	// Handle fields like "SourceSecurityGroupId" or "DestinationVpcId"
	for pattern, target := range resourceMappings {
		if strings.HasSuffix(fieldName, pattern) {
			if _, exists := services[target.Service]; exists {
				return target
			}
		}
	}
	
	return TargetInfo{}
}

// determineRelationType determines the type of relationship based on field name and type
func determineRelationType(fieldName string, field FieldInfo) string {
	// Parent-child relationships
	if strings.Contains(fieldName, "parent") {
		return "contains"
	}
	
	// Management relationships
	if strings.Contains(fieldName, "managed") || strings.Contains(fieldName, "owner") {
		return "manages"
	}
	
	// Dependency relationships
	if strings.Contains(fieldName, "depends") || strings.Contains(fieldName, "requires") {
		return "depends_on"
	}
	
	// Default to references
	return "references"
}

// determineCardinality determines the cardinality of a relationship
func determineCardinality(field FieldInfo) string {
	if field.IsList {
		if strings.Contains(strings.ToLower(field.Name), "primary") {
			return "one-to-many"
		}
		return "many-to-many"
	}
	return "one-to-one"
}

// convertToGraphRelationship converts a ResourceRelationship from ServiceAnalysis to graph format
func convertToGraphRelationship(serviceName, resourceType string, rel ResourceRelationship) GraphRelationship {
	// Parse target resource to extract service
	targetParts := strings.Split(rel.TargetResource, "::")
	targetService := serviceName // Default to same service
	targetResource := rel.TargetResource
	
	if len(targetParts) >= 3 {
		// AWS::Service::Resource format
		targetService = strings.ToLower(targetParts[1])
		targetResource = targetParts[2]
	}
	
	cardinality := "one-to-one"
	if rel.IsArray {
		cardinality = "one-to-many"
	}
	
	relationType := "references"
	switch rel.RelationshipType {
	case "has_many":
		relationType = "contains"
		cardinality = "one-to-many"
	case "belongs_to":
		relationType = "references"
	}
	
	return GraphRelationship{
		SourceService:  serviceName,
		SourceResource: resourceType,
		SourceField:    rel.FieldName,
		TargetService:  targetService,
		TargetResource: targetResource,
		RelationType:   relationType,
		Cardinality:    cardinality,
	}
}

// detectCrossServiceRelationships detects relationships between different AWS services
func detectCrossServiceRelationships(services map[string]*ServiceAnalysis) []GraphRelationship {
	relationships := []GraphRelationship{}
	
	// Known cross-service relationships
	crossServicePatterns := []struct {
		SourceService  string
		SourceResource string
		SourceField    string
		TargetService  string
		TargetResource string
		RelationType   string
		Cardinality    string
	}{
		// EC2 -> IAM
		{"ec2", "Instance", "IamInstanceProfile", "iam", "InstanceProfile", "references", "one-to-one"},
		{"ec2", "Instance", "KeyName", "ec2", "KeyPair", "references", "one-to-one"},
		
		// Lambda -> IAM
		{"lambda", "Function", "Role", "iam", "Role", "references", "one-to-one"},
		{"lambda", "Function", "VpcConfig.SubnetIds", "ec2", "Subnet", "references", "one-to-many"},
		{"lambda", "Function", "VpcConfig.SecurityGroupIds", "ec2", "SecurityGroup", "references", "one-to-many"},
		
		// RDS -> VPC
		{"rds", "DBInstance", "DBSubnetGroupName", "rds", "DBSubnetGroup", "references", "one-to-one"},
		{"rds", "DBSubnetGroup", "SubnetIds", "ec2", "Subnet", "contains", "one-to-many"},
		{"rds", "DBInstance", "VpcSecurityGroupIds", "ec2", "SecurityGroup", "references", "one-to-many"},
		
		// ECS -> IAM
		{"ecs", "TaskDefinition", "TaskRoleArn", "iam", "Role", "references", "one-to-one"},
		{"ecs", "TaskDefinition", "ExecutionRoleArn", "iam", "Role", "references", "one-to-one"},
		{"ecs", "Service", "TaskDefinition", "ecs", "TaskDefinition", "references", "one-to-one"},
		{"ecs", "Service", "Cluster", "ecs", "Cluster", "references", "one-to-one"},
		
		// ELB -> EC2
		{"elbv2", "TargetGroup", "VpcId", "ec2", "Vpc", "references", "one-to-one"},
		{"elbv2", "LoadBalancer", "Subnets", "ec2", "Subnet", "references", "one-to-many"},
		{"elbv2", "LoadBalancer", "SecurityGroups", "ec2", "SecurityGroup", "references", "one-to-many"},
		
		// S3 -> KMS
		{"s3", "Bucket", "EncryptionConfiguration.KMSMasterKeyID", "kms", "Key", "references", "one-to-one"},
		
		// API Gateway -> Lambda
		{"apigateway", "RestApi", "Integration.Uri", "lambda", "Function", "references", "one-to-many"},
		
		// CloudFormation -> All
		{"cloudformation", "Stack", "Resources", "Multiple", "Multiple", "manages", "one-to-many"},
		
		// Auto Scaling
		{"autoscaling", "AutoScalingGroup", "LaunchTemplate", "ec2", "LaunchTemplate", "references", "one-to-one"},
		{"autoscaling", "AutoScalingGroup", "VPCZoneIdentifier", "ec2", "Subnet", "references", "one-to-many"},
		
		// CloudWatch -> Lambda
		{"events", "Rule", "Targets", "lambda", "Function", "references", "one-to-many"},
	}
	
	// Add known patterns if both services exist
	for _, pattern := range crossServicePatterns {
		if _, sourceExists := services[pattern.SourceService]; sourceExists {
			if _, targetExists := services[pattern.TargetService]; targetExists {
				relationships = append(relationships, GraphRelationship{
					SourceService:  pattern.SourceService,
					SourceResource: pattern.SourceResource,
					SourceField:    pattern.SourceField,
					TargetService:  pattern.TargetService,
					TargetResource: pattern.TargetResource,
					RelationType:   pattern.RelationType,
					Cardinality:    pattern.Cardinality,
				})
			}
		}
	}
	
	return relationships
}

// GenerateMermaidDiagram generates a Mermaid diagram from the resource graph
func (g *ResourceGraph) GenerateMermaidDiagram() string {
	var sb strings.Builder
	
	sb.WriteString("graph TD\n")
	sb.WriteString("    %% AWS Resource Relationship Graph\n\n")
	
	// Create subgraphs for each service
	for service, resources := range g.ServiceMap {
		sb.WriteString(fmt.Sprintf("    subgraph %s[%s Service]\n", service, strings.Title(service)))
		for _, resource := range resources {
			nodeId := fmt.Sprintf("%s_%s", service, strings.ReplaceAll(resource, "::", "_"))
			sb.WriteString(fmt.Sprintf("        %s[%s]\n", nodeId, resource))
		}
		sb.WriteString("    end\n\n")
	}
	
	// Add relationships
	relationshipStyles := map[string]string{
		"references": "-->",
		"contains":   "==>",
		"manages":    "-.->",
		"depends_on": "..>",
	}
	
	for _, rel := range g.Relationships {
		sourceId := fmt.Sprintf("%s_%s", rel.SourceService, strings.ReplaceAll(rel.SourceResource, "::", "_"))
		targetId := fmt.Sprintf("%s_%s", rel.TargetService, strings.ReplaceAll(rel.TargetResource, "::", "_"))
		
		arrow := relationshipStyles[rel.RelationType]
		if arrow == "" {
			arrow = "-->"
		}
		
		label := fmt.Sprintf("%s (%s)", rel.SourceField, rel.Cardinality)
		sb.WriteString(fmt.Sprintf("    %s %s|%s| %s\n", sourceId, arrow, label, targetId))
	}
	
	// Add styling
	sb.WriteString("\n    %% Styling\n")
	sb.WriteString("    classDef ec2 fill:#FF9900,stroke:#232F3E,stroke-width:2px,color:#232F3E\n")
	sb.WriteString("    classDef iam fill:#DD344C,stroke:#232F3E,stroke-width:2px,color:#fff\n")
	sb.WriteString("    classDef rds fill:#205081,stroke:#232F3E,stroke-width:2px,color:#fff\n")
	sb.WriteString("    classDef lambda fill:#FF9900,stroke:#232F3E,stroke-width:2px,color:#232F3E\n")
	sb.WriteString("    classDef s3 fill:#569A31,stroke:#232F3E,stroke-width:2px,color:#fff\n")
	
	// Apply styles to services
	serviceStyles := map[string]string{
		"ec2":    "ec2",
		"iam":    "iam",
		"rds":    "rds",
		"lambda": "lambda",
		"s3":     "s3",
	}
	
	for service, style := range serviceStyles {
		if resources, exists := g.ServiceMap[service]; exists {
			for _, resource := range resources {
				nodeId := fmt.Sprintf("%s_%s", service, strings.ReplaceAll(resource, "::", "_"))
				sb.WriteString(fmt.Sprintf("    class %s %s\n", nodeId, style))
			}
		}
	}
	
	return sb.String()
}

// ToJSON returns the graph in a JSON-compatible format
func (g *ResourceGraph) ToJSON() map[string]interface{} {
	nodes := []map[string]interface{}{}
	edges := []map[string]interface{}{}
	
	// Convert nodes
	for key, node := range g.Resources {
		nodes = append(nodes, map[string]interface{}{
			"id":           key,
			"service":      node.Service,
			"resourceType": node.ResourceType,
			"label":        fmt.Sprintf("%s::%s", node.Service, node.ResourceType),
			"inDegree":     len(node.InboundRefs),
			"outDegree":    len(node.OutboundRefs),
		})
	}
	
	// Convert edges
	for i, rel := range g.Relationships {
		sourceKey := fmt.Sprintf("%s::%s", rel.SourceService, rel.SourceResource)
		targetKey := fmt.Sprintf("%s::%s", rel.TargetService, rel.TargetResource)
		
		edges = append(edges, map[string]interface{}{
			"id":           fmt.Sprintf("edge_%d", i),
			"source":       sourceKey,
			"target":       targetKey,
			"label":        rel.SourceField,
			"relationType": rel.RelationType,
			"cardinality":  rel.Cardinality,
		})
	}
	
	return map[string]interface{}{
		"nodes":      nodes,
		"edges":      edges,
		"serviceMap": g.ServiceMap,
		"stats": map[string]interface{}{
			"totalNodes":         len(nodes),
			"totalEdges":         len(edges),
			"totalServices":      len(g.ServiceMap),
			"totalRelationships": len(g.Relationships),
		},
	}
}

// GetDependencyOrder returns resources in dependency order for scanning
func (g *ResourceGraph) GetDependencyOrder() []string {
	// Use topological sort to determine scanning order
	visited := make(map[string]bool)
	tempMark := make(map[string]bool)
	stack := []string{}
	
	var visit func(nodeKey string) error
	visit = func(nodeKey string) error {
		if tempMark[nodeKey] {
			return fmt.Errorf("circular dependency detected at %s", nodeKey)
		}
		if visited[nodeKey] {
			return nil
		}
		
		tempMark[nodeKey] = true
		
		if node, exists := g.Resources[nodeKey]; exists {
			// Visit all nodes that this node depends on
			for _, rel := range node.OutboundRefs {
				targetKey := fmt.Sprintf("%s::%s", rel.TargetService, rel.TargetResource)
				if rel.RelationType == "depends_on" || rel.RelationType == "references" {
					if err := visit(targetKey); err != nil {
						return err
					}
				}
			}
		}
		
		delete(tempMark, nodeKey)
		visited[nodeKey] = true
		stack = append(stack, nodeKey)
		
		return nil
	}
	
	// Visit all nodes
	for nodeKey := range g.Resources {
		if !visited[nodeKey] {
			if err := visit(nodeKey); err != nil {
				// If circular dependency, just add the node
				stack = append(stack, nodeKey)
			}
		}
	}
	
	return stack
}
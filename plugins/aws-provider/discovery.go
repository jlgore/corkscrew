package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// OperationType represents the type of AWS API operation
type OperationType int

const (
	ListOperation OperationType = iota
	DescribeOperation
	GetOperation
	CreateOperation
	UpdateOperation
	DeleteOperation
)

// ServiceMetadata holds metadata about a discovered AWS service
type ServiceMetadata struct {
	Name          string
	Operations    map[string]OperationType
	ResourceTypes []string
	Paginated     map[string]bool
	ClientType    reflect.Type
}

// ServiceDiscovery discovers AWS services and their capabilities dynamically
type ServiceDiscovery struct {
	config     aws.Config
	httpClient *http.Client
	cache      map[string]*ServiceMetadata
	
	// Known service mappings for fallback
	knownServices map[string]*ServiceMetadata
}

// NewServiceDiscovery creates a new service discovery instance
func NewServiceDiscovery(cfg aws.Config) *ServiceDiscovery {
	return &ServiceDiscovery{
		config:     cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]*ServiceMetadata),
		knownServices: initKnownServices(),
	}
}

// DiscoverServices discovers all available AWS services
func (d *ServiceDiscovery) DiscoverServices(ctx context.Context) ([]*pb.ServiceInfo, error) {
	// Try GitHub API first for comprehensive service list
	services, err := d.fetchServicesFromGitHub(ctx)
	if err != nil {
		// Fall back to known services
		services = d.getKnownServiceNames()
	}

	var serviceInfos []*pb.ServiceInfo
	for _, serviceName := range services {
		// Skip internal or testing services
		if d.shouldSkipService(serviceName) {
			continue
		}

		info := d.analyzeService(serviceName)
		if info != nil {
			serviceInfos = append(serviceInfos, info)
		}
	}

	return serviceInfos, nil
}

// fetchServicesFromGitHub fetches AWS services from the official SDK repository
func (d *ServiceDiscovery) fetchServicesFromGitHub(ctx context.Context) ([]string, error) {
	url := "https://api.github.com/repos/aws/aws-sdk-go-v2/contents/service"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var contents []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, err
	}

	var services []string
	for _, item := range contents {
		if item.Type == "dir" && !strings.HasPrefix(item.Name, ".") {
			services = append(services, item.Name)
		}
	}

	return services, nil
}

// analyzeService analyzes a service and returns its metadata
func (d *ServiceDiscovery) analyzeService(serviceName string) *pb.ServiceInfo {
	// Check cache first
	if cached, exists := d.cache[serviceName]; exists {
		return d.convertToServiceInfo(cached)
	}

	// Try to get client for the service
	client := d.createClient(serviceName)
	if client == nil {
		// Fall back to known service info
		if known, exists := d.knownServices[serviceName]; exists {
			return d.convertToServiceInfo(known)
		}
		return nil
	}

	metadata := &ServiceMetadata{
		Name:       serviceName,
		Operations: make(map[string]OperationType),
		Paginated:  make(map[string]bool),
	}

	// Use reflection to analyze the client
	clientType := reflect.TypeOf(client)
	if clientType.Kind() == reflect.Ptr {
		clientType = clientType.Elem()
	}

	metadata.ClientType = clientType

	// Find all methods
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		methodName := method.Name

		// Classify the operation
		opType := d.classifyOperation(methodName)
		metadata.Operations[methodName] = opType

		// Check if it's a List operation for resource discovery
		if opType == ListOperation {
			resourceType := d.extractResourceType(methodName)
			if resourceType != "" {
				metadata.ResourceTypes = append(metadata.ResourceTypes, resourceType)
			}
			
			// Check if the operation supports pagination
			metadata.Paginated[methodName] = d.isPaginated(method)
		}
	}

	// Cache the metadata
	d.cache[serviceName] = metadata

	return d.convertToServiceInfo(metadata)
}

// createClient dynamically creates a client for the specified service
func (d *ServiceDiscovery) createClient(serviceName string) interface{} {
	// Use the client factory to create actual clients
	// This will work for services we have statically imported
	
	// Create a temporary client factory for service discovery
	clientFactory := NewClientFactory(d.config)
	return clientFactory.GetClient(serviceName)
}

// classifyOperation determines the type of AWS API operation
func (d *ServiceDiscovery) classifyOperation(methodName string) OperationType {
	switch {
	case strings.HasPrefix(methodName, "List"):
		return ListOperation
	case strings.HasPrefix(methodName, "Describe"):
		return DescribeOperation
	case strings.HasPrefix(methodName, "Get"):
		return GetOperation
	case strings.HasPrefix(methodName, "Create"):
		return CreateOperation
	case strings.HasPrefix(methodName, "Update"), strings.HasPrefix(methodName, "Modify"), strings.HasPrefix(methodName, "Put"):
		return UpdateOperation
	case strings.HasPrefix(methodName, "Delete"), strings.HasPrefix(methodName, "Terminate"):
		return DeleteOperation
	default:
		return ListOperation // Default assumption
	}
}

// extractResourceType extracts the resource type from an operation name
func (d *ServiceDiscovery) extractResourceType(operationName string) string {
	// Remove common prefixes
	name := operationName
	prefixes := []string{"List", "Describe", "Get", "Create", "Update", "Delete"}
	
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
			break
		}
	}

	// Handle common suffixes
	suffixes := []string{"s", "es"} // Remove plural endings
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}

	return name
}

// isPaginated checks if an operation supports pagination
func (d *ServiceDiscovery) isPaginated(method reflect.Method) bool {
	// Check method signature for pagination indicators
	methodType := method.Type
	
	// Look for output type that might have NextToken or similar
	if methodType.NumOut() >= 1 {
		outputType := methodType.Out(0)
		if outputType.Kind() == reflect.Ptr {
			outputType = outputType.Elem()
		}
		
		// Check for common pagination fields
		if outputType.Kind() == reflect.Struct {
			for i := 0; i < outputType.NumField(); i++ {
				field := outputType.Field(i)
				fieldName := strings.ToLower(field.Name)
				
				if strings.Contains(fieldName, "nexttoken") ||
				   strings.Contains(fieldName, "marker") ||
				   strings.Contains(fieldName, "continuationtoken") {
					return true
				}
			}
		}
	}
	
	return false
}

// convertToServiceInfo converts ServiceMetadata to protobuf ServiceInfo
func (d *ServiceDiscovery) convertToServiceInfo(metadata *ServiceMetadata) *pb.ServiceInfo {
	info := &pb.ServiceInfo{
		Name:        metadata.Name,
		DisplayName: d.formatDisplayName(metadata.Name),
		PackageName: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", metadata.Name),
		ClientType:  fmt.Sprintf("%sClient", strings.Title(metadata.Name)),
	}

	// Add resource types
	for _, resourceType := range metadata.ResourceTypes {
		pbResourceType := &pb.ResourceType{
			Name:              resourceType,
			TypeName:          fmt.Sprintf("AWS::%s::%s", strings.Title(metadata.Name), resourceType),
			ListOperation:     d.findListOperation(metadata, resourceType),
			DescribeOperation: d.findDescribeOperation(metadata, resourceType),
			GetOperation:      d.findGetOperation(metadata, resourceType),
			IdField:           d.getIdField(metadata.Name, resourceType),
			NameField:         d.getNameField(metadata.Name, resourceType),
			SupportsTags:      d.supportsTags(metadata.Name, resourceType),
			Paginated:         d.isResourceTypePaginated(metadata, resourceType),
		}
		info.ResourceTypes = append(info.ResourceTypes, pbResourceType)
	}

	return info
}

// findListOperation finds the List operation for a resource type
func (d *ServiceDiscovery) findListOperation(metadata *ServiceMetadata, resourceType string) string {
	// Look for List operations that match the resource type
	candidates := []string{
		"List" + resourceType + "s",
		"List" + resourceType,
		"Describe" + resourceType + "s",
		"Get" + resourceType + "s",
	}

	for _, candidate := range candidates {
		if _, exists := metadata.Operations[candidate]; exists {
			return candidate
		}
	}

	// Fallback: return first List operation
	for op, opType := range metadata.Operations {
		if opType == ListOperation {
			return op
		}
	}

	return ""
}

// findDescribeOperation finds the Describe operation for a resource type
func (d *ServiceDiscovery) findDescribeOperation(metadata *ServiceMetadata, resourceType string) string {
	candidates := []string{
		"Describe" + resourceType,
		"Get" + resourceType,
	}

	for _, candidate := range candidates {
		if _, exists := metadata.Operations[candidate]; exists {
			return candidate
		}
	}

	return ""
}

// findGetOperation finds the Get operation for a resource type
func (d *ServiceDiscovery) findGetOperation(metadata *ServiceMetadata, resourceType string) string {
	candidates := []string{
		"Get" + resourceType,
		"Describe" + resourceType,
	}

	for _, candidate := range candidates {
		if _, exists := metadata.Operations[candidate]; exists {
			return candidate
		}
	}

	return ""
}

// Helper methods

func (d *ServiceDiscovery) shouldSkipService(serviceName string) bool {
	skipList := []string{
		"internal", "testing", "test", "mock", "example",
	}

	lowerName := strings.ToLower(serviceName)
	for _, skip := range skipList {
		if strings.Contains(lowerName, skip) {
			return true
		}
	}

	return false
}

func (d *ServiceDiscovery) formatDisplayName(serviceName string) string {
	// Convert camelCase or dash-case to Title Case
	words := strings.FieldsFunc(serviceName, func(r rune) bool {
		return r == '-' || r == '_'
	})

	for i, word := range words {
		words[i] = strings.Title(strings.ToLower(word))
	}

	return strings.Join(words, " ")
}

func (d *ServiceDiscovery) getIdField(serviceName, resourceType string) string {
	// Common ID field patterns
	idFields := map[string]string{
		"s3":       "Name",
		"ec2":      "InstanceId",
		"lambda":   "FunctionName",
		"rds":      "DBInstanceIdentifier",
		"dynamodb": "TableName",
		"iam":      "UserName",
	}

	if field, exists := idFields[serviceName]; exists {
		return field
	}

	// Default patterns
	return resourceType + "Id"
}

func (d *ServiceDiscovery) getNameField(serviceName, resourceType string) string {
	// Most AWS resources use "Name" or resource-specific naming
	nameFields := map[string]string{
		"s3":       "Name",
		"lambda":   "FunctionName",
		"rds":      "DBInstanceIdentifier",
		"dynamodb": "TableName",
	}

	if field, exists := nameFields[serviceName]; exists {
		return field
	}

	return "Name"
}

func (d *ServiceDiscovery) supportsTags(serviceName, resourceType string) bool {
	// Most AWS services support tags, but some don't
	noTagsServices := []string{"route53domains", "workspaces"}
	
	for _, service := range noTagsServices {
		if serviceName == service {
			return false
		}
	}

	return true
}

func (d *ServiceDiscovery) isResourceTypePaginated(metadata *ServiceMetadata, resourceType string) bool {
	listOp := d.findListOperation(metadata, resourceType)
	if listOp == "" {
		return false
	}

	return metadata.Paginated[listOp]
}

func (d *ServiceDiscovery) getKnownServiceNames() []string {
	var names []string
	for name := range d.knownServices {
		names = append(names, name)
	}
	return names
}

// initKnownServices initializes the map of known AWS services for fallback
func initKnownServices() map[string]*ServiceMetadata {
	return map[string]*ServiceMetadata{
		"s3": {
			Name: "s3",
			Operations: map[string]OperationType{
				"ListBuckets": ListOperation,
				"ListObjects": ListOperation,
				"GetBucket":   GetOperation,
				"GetObject":   GetOperation,
			},
			ResourceTypes: []string{"Bucket", "Object"},
			Paginated: map[string]bool{
				"ListBuckets": false,
				"ListObjects": true,
			},
		},
		"ec2": {
			Name: "ec2",
			Operations: map[string]OperationType{
				"DescribeInstances":        DescribeOperation,
				"DescribeVolumes":          DescribeOperation,
				"DescribeSecurityGroups":   DescribeOperation,
				"DescribeVpcs":             DescribeOperation,
			},
			ResourceTypes: []string{"Instance", "Volume", "SecurityGroup", "Vpc"},
			Paginated: map[string]bool{
				"DescribeInstances":      true,
				"DescribeVolumes":        true,
				"DescribeSecurityGroups": true,
				"DescribeVpcs":           true,
			},
		},
		"lambda": {
			Name: "lambda",
			Operations: map[string]OperationType{
				"ListFunctions": ListOperation,
				"GetFunction":   GetOperation,
			},
			ResourceTypes: []string{"Function"},
			Paginated: map[string]bool{
				"ListFunctions": true,
			},
		},
		"rds": {
			Name: "rds",
			Operations: map[string]OperationType{
				"DescribeDBInstances": DescribeOperation,
				"DescribeDBClusters":  DescribeOperation,
			},
			ResourceTypes: []string{"DBInstance", "DBCluster"},
			Paginated: map[string]bool{
				"DescribeDBInstances": true,
				"DescribeDBClusters":  true,
			},
		},
		"dynamodb": {
			Name: "dynamodb",
			Operations: map[string]OperationType{
				"ListTables":    ListOperation,
				"DescribeTable": DescribeOperation,
			},
			ResourceTypes: []string{"Table"},
			Paginated: map[string]bool{
				"ListTables": true,
			},
		},
		"iam": {
			Name: "iam",
			Operations: map[string]OperationType{
				"ListUsers":    ListOperation,
				"ListRoles":    ListOperation,
				"ListPolicies": ListOperation,
				"GetUser":      GetOperation,
				"GetRole":      GetOperation,
			},
			ResourceTypes: []string{"User", "Role", "Policy"},
			Paginated: map[string]bool{
				"ListUsers":    true,
				"ListRoles":    true,
				"ListPolicies": true,
			},
		},
	}
}
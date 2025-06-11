package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
)


// RuntimeServiceDiscovery discovers AWS services using reflection as the primary mechanism
type RuntimeServiceDiscovery struct {
	config        aws.Config
	clientFactory ClientFactoryInterface
	
	// Reflection cache for performance
	reflectionCache map[string]*ServiceMetadata
	cacheMu         sync.RWMutex
	
	// Analysis generator (required for fail-fast)
	analysisGenerator AnalysisGeneratorInterface
	enableAnalysisGeneration bool
	
	// HTTP client for GitHub API fallback
	httpClient *http.Client
	
	// Discovery statistics
	stats *DiscoveryStats
}

// DiscoveryStats tracks discovery performance and success rates
type DiscoveryStats struct {
	TotalServices          int
	ReflectionSuccesses    int
	ReflectionFailures     int
	GitHubFallbackUsed     bool
	DiscoveryDuration      time.Duration
	LastDiscoveryTime      time.Time
	FailureReasons         []string
}

// NewRuntimeServiceDiscovery creates a new reflection-first service discovery instance
func NewRuntimeServiceDiscovery(cfg aws.Config) *RuntimeServiceDiscovery {
	return &RuntimeServiceDiscovery{
		config:          cfg,
		reflectionCache: make(map[string]*ServiceMetadata),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		stats:           &DiscoveryStats{},
	}
}

// SetClientFactory sets the client factory for creating AWS service clients
func (d *RuntimeServiceDiscovery) SetClientFactory(factory ClientFactoryInterface) {
	d.clientFactory = factory
}

// SetAnalysisGenerator sets the analysis generator for creating configuration analysis files
func (d *RuntimeServiceDiscovery) SetAnalysisGenerator(generator AnalysisGeneratorInterface) {
	d.analysisGenerator = generator
}

// EnableAnalysisGeneration enables automatic analysis file generation during discovery
func (d *RuntimeServiceDiscovery) EnableAnalysisGeneration(enable bool) {
	d.enableAnalysisGeneration = enable
}

// GetAnalysisGenerator returns the analysis generator
func (d *RuntimeServiceDiscovery) GetAnalysisGenerator() AnalysisGeneratorInterface {
	return d.analysisGenerator
}

// DiscoverServices discovers all available AWS services using reflection-first approach
func (d *RuntimeServiceDiscovery) DiscoverServices(ctx context.Context) ([]*pb.ServiceInfo, error) {
	startTime := time.Now()
	d.stats.LastDiscoveryTime = startTime
	d.stats.FailureReasons = nil
	
	// 1. Try reflection-based discovery first (primary mechanism)
	services, err := d.discoverViaReflection(ctx)
	if err != nil {
		d.stats.FailureReasons = append(d.stats.FailureReasons, fmt.Sprintf("reflection failed: %v", err))
		
		// 2. Try GitHub API as fallback
		services, err = d.fetchServicesFromGitHub(ctx)
		if err != nil {
			d.stats.FailureReasons = append(d.stats.FailureReasons, fmt.Sprintf("github fallback failed: %v", err))
			
			// 3. Fail fast - no fallback to hardcoded services
			d.stats.DiscoveryDuration = time.Since(startTime)
			return nil, fmt.Errorf("service discovery failed - reflection: %v, github: %v", 
				d.stats.FailureReasons[0], err)
		}
		d.stats.GitHubFallbackUsed = true
	}
	
	if len(services) == 0 {
		d.stats.DiscoveryDuration = time.Since(startTime)
		return nil, fmt.Errorf("no services discovered - client factory returned empty service list")
	}
	
	d.stats.TotalServices = len(services)
	d.stats.DiscoveryDuration = time.Since(startTime)
	
	// Skip automatic analysis generation - this will be done on-demand with filtering
	log.Printf("Service discovery completed for %d services - analysis generation will be done on-demand", len(services))
	
	return services, nil
}

// discoverViaReflection discovers services using reflection on the client factory
func (d *RuntimeServiceDiscovery) discoverViaReflection(ctx context.Context) ([]*pb.ServiceInfo, error) {
	if d.clientFactory == nil {
		return nil, fmt.Errorf("client factory not configured")
	}
	
	// Get available services from the client factory
	availableServices := d.clientFactory.GetAvailableServices()
	if len(availableServices) == 0 {
		return nil, fmt.Errorf("no services available from client factory")
	}
	
	var services []*pb.ServiceInfo
	var discoveryErrors []error
	
	for _, serviceName := range availableServices {
		// Skip services that should be ignored
		if d.shouldSkipService(serviceName) {
			continue
		}
		
		// Check cache first
		if cached := d.getCachedService(serviceName); cached != nil {
			services = append(services, d.convertToServiceInfo(cached))
			d.stats.ReflectionSuccesses++
			continue
		}
		
		// Create client for reflection analysis
		client := d.clientFactory.GetClient(serviceName)
		if client == nil {
			discoveryErrors = append(discoveryErrors, 
				fmt.Errorf("failed to create client for service %s", serviceName))
			d.stats.ReflectionFailures++
			continue
		}
		
		// Analyze service via reflection
		metadata, err := d.analyzeServiceViaReflection(client, serviceName)
		if err != nil {
			discoveryErrors = append(discoveryErrors, 
				fmt.Errorf("reflection analysis failed for %s: %w", serviceName, err))
			d.stats.ReflectionFailures++
			continue
		}
		
		// Cache the metadata
		d.cacheService(serviceName, metadata)
		
		// Convert to ServiceInfo
		serviceInfo := d.convertToServiceInfo(metadata)
		if serviceInfo != nil {
			services = append(services, serviceInfo)
			d.stats.ReflectionSuccesses++
		}
	}
	
	if len(services) == 0 {
		return nil, fmt.Errorf("no services discovered via reflection: %v", discoveryErrors)
	}
	
	// Log discovery results
	fmt.Printf("Reflection discovery: %d services discovered, %d failures\n", 
		len(services), len(discoveryErrors))
	
	return services, nil
}

// analyzeServiceViaReflection performs deep reflection analysis of a service client
func (d *RuntimeServiceDiscovery) analyzeServiceViaReflection(client interface{}, serviceName string) (*ServiceMetadata, error) {
	if client == nil {
		return nil, fmt.Errorf("client is nil")
	}
	
	clientType := reflect.TypeOf(client)
	if clientType.Kind() == reflect.Ptr {
		clientType = clientType.Elem()
	}
	
	metadata := &ServiceMetadata{
		Name:          serviceName,
		DisplayName:   d.formatDisplayName(serviceName),
		PackageName:   fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
		ClientType:    fmt.Sprintf("%sClient", strings.Title(serviceName)),
		Operations:    make(map[string]OperationType),
		Paginated:     make(map[string]bool),
		DiscoveredAt:  time.Now(),
		ReflectionData: &ReflectionMetadata{
			ClientTypeName:   clientType.Name(),
			MethodCount:      clientType.NumMethod(),
			MethodSignatures: make(map[string]string),
			PackagePath:      clientType.PkgPath(),
		},
	}
	
	// Analyze all methods using reflection
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		methodName := method.Name
		
		// Skip unexported methods and internal methods
		if !method.IsExported() || d.isInternalMethod(methodName) {
			continue
		}
		
		// Store method signature for debugging
		metadata.ReflectionData.MethodSignatures[methodName] = method.Type.String()
		
		// Classify the operation
		opType := d.classifyOperation(methodName)
		metadata.Operations[methodName] = opType
		
		// Check if it's a List operation for resource discovery
		if opType == ListOperation {
			resourceType := d.extractResourceType(methodName)
			if resourceType != "" && !d.containsString(metadata.ResourceTypes, resourceType) {
				metadata.ResourceTypes = append(metadata.ResourceTypes, resourceType)
			}
			
			// Check if the operation supports pagination
			metadata.Paginated[methodName] = d.isPaginatedViaReflection(method)
		}
	}
	
	if len(metadata.Operations) == 0 {
		return nil, fmt.Errorf("no valid operations found for service %s", serviceName)
	}
	
	return metadata, nil
}

// isPaginatedViaReflection checks if an operation supports pagination using reflection
func (d *RuntimeServiceDiscovery) isPaginatedViaReflection(method reflect.Method) bool {
	methodType := method.Type
	
	// Check if method returns (result, error)
	if methodType.NumOut() < 2 {
		return false
	}
	
	// Check the first return value (should be the result struct)
	resultType := methodType.Out(0)
	if resultType.Kind() == reflect.Ptr {
		resultType = resultType.Elem()
	}
	
	if resultType.Kind() != reflect.Struct {
		return false
	}
	
	// Look for pagination fields in the result struct
	for i := 0; i < resultType.NumField(); i++ {
		field := resultType.Field(i)
		fieldName := strings.ToLower(field.Name)
		
		// Check for common pagination field names
		paginationFields := []string{
			"nexttoken", "continuationtoken", "marker", "nextmarker",
			"nextpagetoken", "pagetoken", "nextcursor", "cursor",
			"maxitems", "istruncated", "hasmorepages",
		}
		
		for _, paginationField := range paginationFields {
			if strings.Contains(fieldName, paginationField) {
				return true
			}
		}
	}
	
	return false
}

// isInternalMethod checks if a method should be skipped during analysis
func (d *RuntimeServiceDiscovery) isInternalMethod(methodName string) bool {
	internalMethods := []string{
		"String", "GoString", "SetLogger", "Copy", "Config",
		"EndpointResolver", "HTTPClient", "Retryer", "APIOptions",
	}
	
	for _, internal := range internalMethods {
		if methodName == internal {
			return true
		}
	}
	
	// Skip methods that start with internal prefixes
	internalPrefixes := []string{"set", "get", "with", "clone"}
	lowerMethod := strings.ToLower(methodName)
	for _, prefix := range internalPrefixes {
		if strings.HasPrefix(lowerMethod, prefix) {
			return true
		}
	}
	
	return false
}

// generateAnalysisFiles generates configuration analysis files for discovered services
func (d *RuntimeServiceDiscovery) generateAnalysisFiles(services []*pb.ServiceInfo) error {
	if d.analysisGenerator == nil {
		return fmt.Errorf("analysis generator not configured")
	}
	
	serviceNames := make([]string, len(services))
	for i, service := range services {
		serviceNames[i] = service.Name
	}
	
	if err := d.analysisGenerator.GenerateForDiscoveredServices(serviceNames); err != nil {
		return fmt.Errorf("failed to generate analysis files: %w", err)
	}
	
	fmt.Printf("Generated analysis files for %d discovered services\n", len(serviceNames))
	return nil
}

// fetchServicesFromGitHub fetches AWS services from the official SDK repository (fallback)
func (d *RuntimeServiceDiscovery) fetchServicesFromGitHub(ctx context.Context) ([]*pb.ServiceInfo, error) {
	url := "https://api.github.com/repos/aws/aws-sdk-go-v2/contents/service"
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub API request: %w", err)
	}
	
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
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
		return nil, fmt.Errorf("failed to decode GitHub API response: %w", err)
	}
	
	var services []*pb.ServiceInfo
	for _, item := range contents {
		if item.Type == "dir" && !strings.HasPrefix(item.Name, ".") && !d.shouldSkipService(item.Name) {
			// Create minimal service info for GitHub-discovered services
			serviceInfo := &pb.ServiceInfo{
				Name:        item.Name,
				DisplayName: d.formatDisplayName(item.Name),
				PackageName: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", item.Name),
				ClientType:  fmt.Sprintf("%sClient", strings.Title(item.Name)),
			}
			services = append(services, serviceInfo)
		}
	}
	
	if len(services) == 0 {
		return nil, fmt.Errorf("no valid services found in GitHub API response")
	}
	
	fmt.Printf("GitHub fallback discovery: %d services found\n", len(services))
	return services, nil
}

// Cache management methods

func (d *RuntimeServiceDiscovery) getCachedService(serviceName string) *ServiceMetadata {
	d.cacheMu.RLock()
	defer d.cacheMu.RUnlock()
	return d.reflectionCache[serviceName]
}

func (d *RuntimeServiceDiscovery) cacheService(serviceName string, metadata *ServiceMetadata) {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	d.reflectionCache[serviceName] = metadata
}

// ClearCache clears the reflection cache
func (d *RuntimeServiceDiscovery) ClearCache() {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	d.reflectionCache = make(map[string]*ServiceMetadata)
}

// GetDiscoveryStats returns current discovery statistics
func (d *RuntimeServiceDiscovery) GetDiscoveryStats() *DiscoveryStats {
	return d.stats
}

// Utility methods

func (d *RuntimeServiceDiscovery) shouldSkipService(serviceName string) bool {
	skipList := []string{
		"internal", "testing", "test", "mock", "example", "types",
		"endpoints", "auth", "middleware", "transport", "protocol",
	}
	
	lowerName := strings.ToLower(serviceName)
	for _, skip := range skipList {
		if strings.Contains(lowerName, skip) {
			return true
		}
	}
	
	return false
}

func (d *RuntimeServiceDiscovery) formatDisplayName(serviceName string) string {
	// Convert service names to proper display names
	displayNames := map[string]string{
		"s3":          "Amazon S3",
		"ec2":         "Amazon EC2",
		"lambda":      "AWS Lambda",
		"rds":         "Amazon RDS",
		"dynamodb":    "Amazon DynamoDB",
		"iam":         "AWS IAM",
		"ecs":         "Amazon ECS",
		"eks":         "Amazon EKS",
		"apigateway":  "Amazon API Gateway",
		"apigatewayv2": "Amazon API Gateway V2",
		"cloudformation": "AWS CloudFormation",
		"cloudwatch":  "Amazon CloudWatch",
	}
	
	if displayName, exists := displayNames[serviceName]; exists {
		return displayName
	}
	
	// Convert camelCase or dash-case to Title Case
	words := strings.FieldsFunc(serviceName, func(r rune) bool {
		return r == '-' || r == '_'
	})
	
	for i, word := range words {
		words[i] = strings.Title(strings.ToLower(word))
	}
	
	return strings.Join(words, " ")
}

func (d *RuntimeServiceDiscovery) classifyOperation(methodName string) OperationType {
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
		return ListOperation // Default assumption for unknown operations
	}
}

func (d *RuntimeServiceDiscovery) extractResourceType(operationName string) string {
	name := operationName
	prefixes := []string{"List", "Describe", "Get", "Create", "Update", "Delete"}
	
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
			break
		}
	}
	
	// Handle common suffixes (remove plural endings)
	suffixes := []string{"s", "es"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	
	return name
}

func (d *RuntimeServiceDiscovery) convertToServiceInfo(metadata *ServiceMetadata) *pb.ServiceInfo {
	info := &pb.ServiceInfo{
		Name:        metadata.Name,
		DisplayName: metadata.DisplayName,
		PackageName: metadata.PackageName,
		ClientType:  metadata.ClientType,
	}
	
	// Add resource types with complete information
	for _, resourceType := range metadata.ResourceTypes {
		pbResourceType := &pb.ResourceType{
			Name:              resourceType,
			TypeName:          fmt.Sprintf("AWS::%s::%s", strings.Title(metadata.Name), resourceType),
			ListOperation:     d.findOperationForResource(metadata, resourceType, ListOperation),
			DescribeOperation: d.findOperationForResource(metadata, resourceType, DescribeOperation),
			GetOperation:      d.findOperationForResource(metadata, resourceType, GetOperation),
			IdField:           d.getIdField(metadata.Name, resourceType),
			NameField:         d.getNameField(metadata.Name, resourceType),
			SupportsTags:      d.supportsTags(metadata.Name),
			Paginated:         d.isResourceTypePaginated(metadata, resourceType),
		}
		info.ResourceTypes = append(info.ResourceTypes, pbResourceType)
	}
	
	return info
}

func (d *RuntimeServiceDiscovery) findOperationForResource(metadata *ServiceMetadata, resourceType string, opType OperationType) string {
	// Look for operations that match the resource type and operation type
	for opName, ot := range metadata.Operations {
		if ot == opType && strings.Contains(opName, resourceType) {
			return opName
		}
	}
	return ""
}

func (d *RuntimeServiceDiscovery) getIdField(serviceName, resourceType string) string {
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
	
	return resourceType + "Id"
}

func (d *RuntimeServiceDiscovery) getNameField(serviceName, resourceType string) string {
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

func (d *RuntimeServiceDiscovery) supportsTags(serviceName string) bool {
	noTagsServices := []string{"route53domains", "workspaces"}
	for _, service := range noTagsServices {
		if serviceName == service {
			return false
		}
	}
	return true
}

func (d *RuntimeServiceDiscovery) isResourceTypePaginated(metadata *ServiceMetadata, resourceType string) bool {
	listOp := d.findOperationForResource(metadata, resourceType, ListOperation)
	if listOp == "" {
		return false
	}
	return metadata.Paginated[listOp]
}

func (d *RuntimeServiceDiscovery) containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
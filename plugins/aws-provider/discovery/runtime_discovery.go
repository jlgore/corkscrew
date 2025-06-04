package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
	"golang.org/x/time/rate"
)

// Note: OperationType is defined in registry_discovery.go

// RuntimeServiceMetadata holds metadata about a discovered AWS service
type RuntimeServiceMetadata struct {
	Name          string
	Operations    map[string]OperationType
	ResourceTypes []string
	Paginated     map[string]bool
	ClientType    reflect.Type
}

// RuntimeServiceDiscovery discovers AWS services and their capabilities dynamically
type RuntimeServiceDiscovery struct {
	config     aws.Config
	httpClient *http.Client
	cache      map[string]*RuntimeServiceMetadata
	
	// Known service mappings for fallback
	knownServices map[string]*RuntimeServiceMetadata
	
	// Registry integration
	registry      registry.DynamicServiceRegistry
	registryMutex sync.RWMutex
	
	// Client factory for creating AWS clients
	clientFactory ClientFactoryInterface
	
	// Analysis generation
	analysisGenerator AnalysisGeneratorInterface
	enableAnalysisGeneration bool
}

// ClientFactoryInterface defines the interface for client factory
type ClientFactoryInterface interface {
	GetClient(serviceName string) interface{}
	GetAvailableServices() []string
}

// AnalysisGeneratorInterface defines the interface for analysis generation
type AnalysisGeneratorInterface interface {
	GenerateForService(serviceName string) error
	GenerateForDiscoveredServices(services []string) error
}

// NewRuntimeServiceDiscovery creates a new service discovery instance
func NewRuntimeServiceDiscovery(cfg aws.Config) *RuntimeServiceDiscovery {
	return &RuntimeServiceDiscovery{
		config:     cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]*RuntimeServiceMetadata),
		knownServices: make(map[string]*RuntimeServiceMetadata), // Will be populated
	}
}

// NewRuntimeServiceDiscoveryWithRegistry creates a new service discovery instance with registry
func NewRuntimeServiceDiscoveryWithRegistry(cfg aws.Config, reg registry.DynamicServiceRegistry) *RuntimeServiceDiscovery {
	d := NewRuntimeServiceDiscovery(cfg)
	d.registry = reg
	return d
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

// DiscoverServices discovers all available AWS services
func (d *RuntimeServiceDiscovery) DiscoverServices(ctx context.Context) ([]*pb.ServiceInfo, error) {
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

// DiscoverAndRegister discovers all services and registers them in the registry
func (d *RuntimeServiceDiscovery) DiscoverAndRegister(ctx context.Context) error {
	if d.registry == nil {
		return fmt.Errorf("registry not configured")
	}

	// Discover services
	services, err := d.DiscoverServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover services: %w", err)
	}

	// Convert and register each service
	var registrationErrors []error
	successCount := 0

	for _, service := range services {
		metadata := d.extractCompleteMetadata(service)
		if err := d.registry.RegisterService(metadata); err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("failed to register %s: %w", service.Name, err))
		} else {
			successCount++
		}
	}

	// Persist the registry for next run
	registryPath := d.getRegistryPath()
	if registryPath != "" {
		if err := d.registry.PersistToFile(registryPath); err != nil {
			fmt.Printf("Warning: Failed to persist registry to %s: %v\n", registryPath, err)
		}
	}

	// Trigger client factory regeneration if needed
	if successCount > 0 {
		if err := d.regenerateClientFactory(); err != nil {
			fmt.Printf("Warning: Failed to regenerate client factory: %v\n", err)
		}
	}

	// Generate analysis files for discovered services if enabled
	if d.enableAnalysisGeneration && d.analysisGenerator != nil && successCount > 0 {
		serviceNames := make([]string, 0, len(services))
		for _, service := range services {
			serviceNames = append(serviceNames, service.Name)
		}
		
		if err := d.analysisGenerator.GenerateForDiscoveredServices(serviceNames); err != nil {
			fmt.Printf("Warning: Failed to generate analysis files: %v\n", err)
		} else {
			fmt.Printf("Generated analysis files for %d discovered services\n", len(serviceNames))
		}
	}

	if len(registrationErrors) > 0 {
		return fmt.Errorf("registered %d services with %d errors: %v", successCount, len(registrationErrors), registrationErrors)
	}

	return nil
}

// extractCompleteMetadata converts ServiceInfo to complete ServiceDefinition
func (d *RuntimeServiceDiscovery) extractCompleteMetadata(info *pb.ServiceInfo) registry.ServiceDefinition {
	def := registry.ServiceDefinition{
		Name:             strings.ToLower(info.Name),
		DisplayName:      info.DisplayName,
		// Description:      info.Description, // Field not available in proto
		PackagePath:      info.PackageName,
		ClientType:       info.ClientType,
		DiscoverySource:  "auto-discovery",
		DiscoveredAt:     time.Now(),
		ValidationStatus: "discovered",
		RateLimit:        10, // Default, will be updated based on service
		BurstLimit:       20, // Default, will be updated based on service
	}

	// Determine service characteristics
	def.GlobalService = d.isGlobalService(info.Name)
	def.RequiresRegion = !def.GlobalService
	def.SupportsPagination = d.detectsPagination(info)
	def.SupportsResourceExplorer = d.supportsResourceExplorer(info.Name)

	// Convert resource types
	for _, rt := range info.ResourceTypes {
		resourceDef := registry.ResourceTypeDefinition{
			Name:              rt.Name,
			// DisplayName:       rt.DisplayName, // Field not available in proto
			ResourceType:      rt.TypeName,
			ListOperation:     rt.ListOperation,
			DescribeOperation: rt.DescribeOperation,
			GetOperation:      rt.GetOperation,
			IDField:           rt.IdField,
			SupportsTags:      rt.SupportsTags,
			Paginated:         rt.Paginated,
		}

		// Add ARN pattern if we can determine it
		resourceDef.ARNPattern = d.getARNPattern(info.Name, rt.Name)

		def.ResourceTypes = append(def.ResourceTypes, resourceDef)
	}

	// Extract operations from resource types
	opMap := make(map[string]bool)
	for _, rt := range info.ResourceTypes {
		if rt.ListOperation != "" {
			opMap[rt.ListOperation] = true
		}
		if rt.DescribeOperation != "" {
			opMap[rt.DescribeOperation] = true
		}
		if rt.GetOperation != "" {
			opMap[rt.GetOperation] = true
		}
	}

	for opName := range opMap {
		def.Operations = append(def.Operations, registry.OperationDefinition{
			Name:          opName,
			OperationType: d.classifyOperationString(opName),
			Paginated:     d.isOperationPaginated(info, opName),
		})
	}

	// Set rate limits based on service type
	d.setServiceRateLimits(&def)

	// Set required permissions
	def.Permissions = d.getRequiredPermissions(info)

	return def
}

// Helper methods for discovery integration

func (d *RuntimeServiceDiscovery) isGlobalService(serviceName string) bool {
	globalServices := []string{"iam", "route53", "cloudfront", "waf", "wafv2"}
	for _, gs := range globalServices {
		if serviceName == gs {
			return true
		}
	}
	return false
}

func (d *RuntimeServiceDiscovery) detectsPagination(info *pb.ServiceInfo) bool {
	for _, rt := range info.ResourceTypes {
		if rt.Paginated {
			return true
		}
	}
	return false
}

func (d *RuntimeServiceDiscovery) supportsResourceExplorer(serviceName string) bool {
	// Services known to work with Resource Explorer
	supportedServices := []string{
		"ec2", "s3", "lambda", "rds", "dynamodb", "ecs", "eks",
		"elasticache", "elasticbeanstalk", "sns", "sqs", "kinesis",
	}
	for _, s := range supportedServices {
		if serviceName == s {
			return true
		}
	}
	return false
}

func (d *RuntimeServiceDiscovery) getARNPattern(serviceName, resourceType string) string {
	// Common ARN patterns
	patterns := map[string]map[string]string{
		"s3": {
			"Bucket": "arn:aws:s3:::%s",
			"Object": "arn:aws:s3:::%s/%s",
		},
		"ec2": {
			"Instance":       "arn:aws:ec2:%s:%s:instance/%s",
			"Volume":         "arn:aws:ec2:%s:%s:volume/%s",
			"SecurityGroup":  "arn:aws:ec2:%s:%s:security-group/%s",
			"Vpc":            "arn:aws:ec2:%s:%s:vpc/%s",
		},
		"lambda": {
			"Function": "arn:aws:lambda:%s:%s:function:%s",
		},
		"rds": {
			"DBInstance": "arn:aws:rds:%s:%s:db:%s",
			"DBCluster":  "arn:aws:rds:%s:%s:cluster:%s",
		},
	}

	if servicePatterns, ok := patterns[serviceName]; ok {
		if pattern, ok := servicePatterns[resourceType]; ok {
			return pattern
		}
	}

	// Default pattern
	return fmt.Sprintf("arn:aws:%s:%%s:%%s:%s/%%s", serviceName, strings.ToLower(resourceType))
}

func (d *RuntimeServiceDiscovery) classifyOperationString(opName string) string {
	switch d.classifyOperation(opName) {
	case ListOperation:
		return "List"
	case DescribeOperation:
		return "Describe"
	case GetOperation:
		return "Get"
	case CreateOperation:
		return "Create"
	case UpdateOperation:
		return "Update"
	case DeleteOperation:
		return "Delete"
	default:
		return "Unknown"
	}
}

func (d *RuntimeServiceDiscovery) isOperationPaginated(info *pb.ServiceInfo, opName string) bool {
	for _, rt := range info.ResourceTypes {
		if rt.ListOperation == opName && rt.Paginated {
			return true
		}
	}
	return false
}

func (d *RuntimeServiceDiscovery) setServiceRateLimits(def *registry.ServiceDefinition) {
	// Set rate limits based on service characteristics
	rateLimits := map[string]struct{ rate, burst int }{
		"s3":       {100, 200},
		"ec2":      {20, 40},
		"lambda":   {50, 100},
		"rds":      {20, 40},
		"dynamodb": {100, 200},
		"iam":      {10, 20},
		"route53":  {5, 10},
	}

	if limits, ok := rateLimits[def.Name]; ok {
		def.RateLimit = rate.Limit(limits.rate)
		def.BurstLimit = limits.burst
	} else {
		// Default limits
		def.RateLimit = rate.Limit(10)
		def.BurstLimit = 20
	}
}

func (d *RuntimeServiceDiscovery) getRequiredPermissions(info *pb.ServiceInfo) []string {
	var permissions []string
	permMap := make(map[string]bool)

	for _, rt := range info.ResourceTypes {
		if rt.ListOperation != "" {
			perm := fmt.Sprintf("%s:%s", info.Name, rt.ListOperation)
			permMap[perm] = true
		}
		if rt.DescribeOperation != "" {
			perm := fmt.Sprintf("%s:%s", info.Name, rt.DescribeOperation)
			permMap[perm] = true
		}
	}

	for perm := range permMap {
		permissions = append(permissions, perm)
	}

	return permissions
}

func (d *RuntimeServiceDiscovery) getRegistryPath() string {
	// Try environment variable first
	if path := os.Getenv("CORKSCREW_REGISTRY_PATH"); path != "" {
		return path
	}

	// Default to ./registry/discovered_services.json
	return "./registry/discovered_services.json"
}

func (d *RuntimeServiceDiscovery) regenerateClientFactory() error {
	if d.registry == nil {
		return fmt.Errorf("registry not configured")
	}

	// Create output directory if it doesn't exist
	outputDir := "./generated"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate client factory
	gen := generator.NewClientFactoryGenerator(
		d.registry,
		filepath.Join(outputDir, "client_factory.go"),
	)

	if err := gen.GenerateClientFactory(); err != nil {
		return fmt.Errorf("failed to generate client factory: %w", err)
	}

	// Also generate dynamic wrapper
	if err := gen.GenerateDynamicWrapper(); err != nil {
		return fmt.Errorf("failed to generate dynamic wrapper: %w", err)
	}

	return nil
}

// fetchServicesFromGitHub fetches AWS services from the official SDK repository
func (d *RuntimeServiceDiscovery) fetchServicesFromGitHub(ctx context.Context) ([]string, error) {
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
func (d *RuntimeServiceDiscovery) analyzeService(serviceName string) *pb.ServiceInfo {
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

	metadata := &RuntimeServiceMetadata{
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
func (d *RuntimeServiceDiscovery) createClient(serviceName string) interface{} {
	if d.clientFactory == nil {
		return nil
	}
	return d.clientFactory.GetClient(serviceName)
}

// classifyOperation determines the type of AWS API operation
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
		return ListOperation // Default assumption
	}
}

// extractResourceType extracts the resource type from an operation name
func (d *RuntimeServiceDiscovery) extractResourceType(operationName string) string {
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
func (d *RuntimeServiceDiscovery) isPaginated(method reflect.Method) bool {
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

// convertToServiceInfo converts RuntimeServiceMetadata to protobuf ServiceInfo
func (d *RuntimeServiceDiscovery) convertToServiceInfo(metadata *RuntimeServiceMetadata) *pb.ServiceInfo {
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
func (d *RuntimeServiceDiscovery) findListOperation(metadata *RuntimeServiceMetadata, resourceType string) string {
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
func (d *RuntimeServiceDiscovery) findDescribeOperation(metadata *RuntimeServiceMetadata, resourceType string) string {
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
func (d *RuntimeServiceDiscovery) findGetOperation(metadata *RuntimeServiceMetadata, resourceType string) string {
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

func (d *RuntimeServiceDiscovery) shouldSkipService(serviceName string) bool {
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

func (d *RuntimeServiceDiscovery) formatDisplayName(serviceName string) string {
	// Convert camelCase or dash-case to Title Case
	words := strings.FieldsFunc(serviceName, func(r rune) bool {
		return r == '-' || r == '_'
	})

	for i, word := range words {
		words[i] = strings.Title(strings.ToLower(word))
	}

	return strings.Join(words, " ")
}

func (d *RuntimeServiceDiscovery) getIdField(serviceName, resourceType string) string {
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

func (d *RuntimeServiceDiscovery) getNameField(serviceName, resourceType string) string {
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

func (d *RuntimeServiceDiscovery) supportsTags(serviceName, resourceType string) bool {
	// Most AWS services support tags, but some don't
	noTagsServices := []string{"route53domains", "workspaces"}
	
	for _, service := range noTagsServices {
		if serviceName == service {
			return false
		}
	}

	return true
}

func (d *RuntimeServiceDiscovery) isResourceTypePaginated(metadata *RuntimeServiceMetadata, resourceType string) bool {
	listOp := d.findListOperation(metadata, resourceType)
	if listOp == "" {
		return false
	}

	return metadata.Paginated[listOp]
}

func (d *RuntimeServiceDiscovery) getKnownServiceNames() []string {
	var names []string
	for name := range d.knownServices {
		names = append(names, name)
	}
	return names
}



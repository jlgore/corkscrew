package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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


// RegistryServiceMetadata holds metadata about a discovered AWS service for registry
type RegistryServiceMetadata struct {
	Name          string
	Operations    map[string]OperationType
	ResourceTypes []string
	Paginated     map[string]bool
	ClientType    reflect.Type
}

// RegistryDiscovery discovers AWS services and integrates with the service registry
type RegistryDiscovery struct {
	config     aws.Config
	httpClient *http.Client
	cache      map[string]*RegistryServiceMetadata
	
	// Known service mappings for fallback
	knownServices map[string]*RegistryServiceMetadata
	
	// Registry integration
	registry      *registry.UnifiedServiceRegistry
	registryMutex sync.RWMutex
	
	// Discovery caching
	discoveryCache *DiscoveryCache
	cacheConfig    CacheConfig
}

// NewRegistryDiscovery creates a new registry-aware service discovery instance
func NewRegistryDiscovery(cfg aws.Config) *RegistryDiscovery {
	cacheConfig := CacheConfig{
		CacheDir:        "./registry/cache",
		TTL:             24 * time.Hour,
		MaxCacheSize:    50 * 1024 * 1024, // 50MB
		EnableChecksum:  true,
		AutoRefresh:     false,
		RefreshInterval: 12 * time.Hour,
	}

	return &RegistryDiscovery{
		config:         cfg,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		cache:          make(map[string]*RegistryServiceMetadata),
		knownServices:  initKnownServices(),
		cacheConfig:    cacheConfig,
		discoveryCache: NewDiscoveryCache(cacheConfig),
	}
}

// NewRegistryDiscoveryWithCacheConfig creates a discovery instance with custom cache config
func NewRegistryDiscoveryWithCacheConfig(cfg aws.Config, cacheConfig CacheConfig) *RegistryDiscovery {
	d := NewRegistryDiscovery(cfg)
	d.cacheConfig = cacheConfig
	d.discoveryCache = NewDiscoveryCache(cacheConfig)
	return d
}

// WithRegistry adds a registry to the discovery instance
func (d *RegistryDiscovery) WithRegistry(reg *registry.UnifiedServiceRegistry) *RegistryDiscovery {
	d.registryMutex.Lock()
	defer d.registryMutex.Unlock()
	d.registry = reg
	return d
}

// NewRegistryDiscoveryWithGlobalRegistry creates a registry discovery with the global registry
// func NewRegistryDiscoveryWithGlobalRegistry(cfg aws.Config) *RegistryDiscovery {
// 	return NewRegistryDiscovery(cfg).WithRegistry(awsprovider.GetServiceRegistry())
// }

// DiscoverServices discovers all available AWS services with caching
func (d *RegistryDiscovery) DiscoverServices(ctx context.Context) ([]*pb.ServiceInfo, error) {
	// Load manual overrides first
	if err := d.discoveryCache.LoadManualOverrides(); err != nil {
		fmt.Printf("Warning: Failed to load manual overrides: %v\n", err)
	}

	// Try to load from cache first
	if cachedServices, err := d.discoveryCache.GetCachedServices(); err == nil {
		fmt.Printf("Using cached discovery results (%d services)\n", len(cachedServices))
		
		// Merge with manual overrides
		mergedServices := d.discoveryCache.MergeWithOverrides(cachedServices)
		return mergedServices, nil
	} else {
		fmt.Printf("Cache miss or expired: %v\n", err)
	}

	fmt.Println("Running fresh discovery...")

	// Try GitHub API first for comprehensive service list
	services, err := d.fetchServicesFromGitHub(ctx)
	source := "github-api"
	if err != nil {
		fmt.Printf("GitHub API failed: %v, falling back to known services\n", err)
		// Fall back to known services
		services = d.getKnownServiceNames()
		source = "fallback"
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

	// Cache the results
	if err := d.discoveryCache.SaveCache(serviceInfos, source); err != nil {
		fmt.Printf("Warning: Failed to cache discovery results: %v\n", err)
	} else {
		fmt.Printf("Cached %d services for future use\n", len(serviceInfos))
	}

	// Merge with manual overrides
	mergedServices := d.discoveryCache.MergeWithOverrides(serviceInfos)
	
	return mergedServices, nil
}

// DiscoverAndRegister discovers all services and registers them in the registry
func (d *RegistryDiscovery) DiscoverAndRegister(ctx context.Context) error {
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
		// Check if service is allowed by registry filter
		if d.registry != nil {
			if !d.registry.IsServiceAllowed(service.Name) {
				continue // Skip services not in filter
			}
		}

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

	if len(registrationErrors) > 0 {
		return fmt.Errorf("registered %d services with %d errors: %v", successCount, len(registrationErrors), registrationErrors)
	}

	return nil
}

// extractCompleteMetadata converts ServiceInfo to complete ServiceDefinition
func (d *RegistryDiscovery) extractCompleteMetadata(info *pb.ServiceInfo) registry.ServiceDefinition {
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

// fetchServicesFromGitHub fetches AWS services from the official SDK repository
func (d *RegistryDiscovery) fetchServicesFromGitHub(ctx context.Context) ([]string, error) {
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
func (d *RegistryDiscovery) analyzeService(serviceName string) *pb.ServiceInfo {
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

	metadata := &RegistryServiceMetadata{
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
func (d *RegistryDiscovery) createClient(serviceName string) interface{} {
	// This would need to be implemented to actually create clients
	// For now, return nil to use fallback
	return nil
}

// Helper methods

func (d *RegistryDiscovery) shouldSkipService(serviceName string) bool {
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

func (d *RegistryDiscovery) classifyOperation(methodName string) OperationType {
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

func (d *RegistryDiscovery) extractResourceType(operationName string) string {
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

func (d *RegistryDiscovery) isPaginated(method reflect.Method) bool {
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

func (d *RegistryDiscovery) convertToServiceInfo(metadata *RegistryServiceMetadata) *pb.ServiceInfo {
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

func (d *RegistryDiscovery) findListOperation(metadata *RegistryServiceMetadata, resourceType string) string {
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

func (d *RegistryDiscovery) findDescribeOperation(metadata *RegistryServiceMetadata, resourceType string) string {
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

func (d *RegistryDiscovery) findGetOperation(metadata *RegistryServiceMetadata, resourceType string) string {
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

// Additional helper methods

func (d *RegistryDiscovery) isGlobalService(serviceName string) bool {
	globalServices := []string{"iam", "route53", "cloudfront", "waf", "wafv2"}
	for _, gs := range globalServices {
		if serviceName == gs {
			return true
		}
	}
	return false
}

func (d *RegistryDiscovery) detectsPagination(info *pb.ServiceInfo) bool {
	for _, rt := range info.ResourceTypes {
		if rt.Paginated {
			return true
		}
	}
	return false
}

func (d *RegistryDiscovery) supportsResourceExplorer(serviceName string) bool {
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

func (d *RegistryDiscovery) getARNPattern(serviceName, resourceType string) string {
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

func (d *RegistryDiscovery) classifyOperationString(opName string) string {
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

func (d *RegistryDiscovery) isOperationPaginated(info *pb.ServiceInfo, opName string) bool {
	for _, rt := range info.ResourceTypes {
		if rt.ListOperation == opName && rt.Paginated {
			return true
		}
	}
	return false
}

func (d *RegistryDiscovery) setServiceRateLimits(def *registry.ServiceDefinition) {
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
		def.RateLimit = 10
		def.BurstLimit = 20
	}
}

func (d *RegistryDiscovery) getRequiredPermissions(info *pb.ServiceInfo) []string {
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

func (d *RegistryDiscovery) formatDisplayName(serviceName string) string {
	// Convert camelCase or dash-case to Title Case
	words := strings.FieldsFunc(serviceName, func(r rune) bool {
		return r == '-' || r == '_'
	})

	for i, word := range words {
		words[i] = strings.Title(strings.ToLower(word))
	}

	return strings.Join(words, " ")
}

func (d *RegistryDiscovery) getIdField(serviceName, resourceType string) string {
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

func (d *RegistryDiscovery) getNameField(serviceName, resourceType string) string {
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

func (d *RegistryDiscovery) supportsTags(serviceName, resourceType string) bool {
	// Most AWS services support tags, but some don't
	noTagsServices := []string{"route53domains", "workspaces"}
	
	for _, service := range noTagsServices {
		if serviceName == service {
			return false
		}
	}

	return true
}

func (d *RegistryDiscovery) isResourceTypePaginated(metadata *RegistryServiceMetadata, resourceType string) bool {
	listOp := d.findListOperation(metadata, resourceType)
	if listOp == "" {
		return false
	}

	return metadata.Paginated[listOp]
}

func (d *RegistryDiscovery) getKnownServiceNames() []string {
	var names []string
	for name := range d.knownServices {
		names = append(names, name)
	}
	return names
}

func (d *RegistryDiscovery) getRegistryPath() string {
	// Try environment variable first
	if path := os.Getenv("CORKSCREW_REGISTRY_PATH"); path != "" {
		return path
	}

	// Default to ./registry/discovered_services.json
	return "./registry/discovered_services.json"
}

func (d *RegistryDiscovery) regenerateClientFactory() error {
	// Create and populate registry from services.json if not configured
	if d.registry == nil {
		log.Printf("Creating unified registry from services.json")
		
		// Create a basic AWS config
		awsConfig := aws.Config{}
		registryConfig := registry.RegistryConfig{
			EnableCache: true,
			PersistencePath: "./generated/unified-registry.json",
		}
		
		unifiedRegistry := registry.NewUnifiedServiceRegistry(awsConfig, registryConfig)
		
		// Load services from generated/services.json using JSON adapter
		servicesPath := filepath.Join("./generated", "services.json")
		jsonAdapter := registry.NewJSONServiceRegistry()
		if err := jsonAdapter.LoadFromServicesJSON(servicesPath); err != nil {
			log.Printf("Warning: Failed to load services.json: %v", err)
		} else {
			// Migrate services from JSON adapter to unified registry
			for _, service := range jsonAdapter.ListServiceDefinitions() {
				unifiedRegistry.RegisterService(service)
			}
		}
		
		d.registry = unifiedRegistry
		log.Printf("Created unified registry with %d services", len(unifiedRegistry.ListServices()))
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

	return nil
}

// Cache management methods

// GetCacheStats returns statistics about the discovery cache
func (d *RegistryDiscovery) GetCacheStats() map[string]interface{} {
	if d.discoveryCache == nil {
		return map[string]interface{}{"error": "cache not initialized"}
	}
	return d.discoveryCache.GetCacheStats()
}

// InvalidateCache forces a fresh discovery on next run
func (d *RegistryDiscovery) InvalidateCache() error {
	if d.discoveryCache == nil {
		return fmt.Errorf("cache not initialized")
	}
	return d.discoveryCache.InvalidateCache()
}

// AddManualOverride adds a manual override for a specific service
func (d *RegistryDiscovery) AddManualOverride(serviceName string, serviceInfo *pb.ServiceInfo) error {
	if d.discoveryCache == nil {
		return fmt.Errorf("cache not initialized")
	}
	
	d.discoveryCache.AddManualOverride(serviceName, serviceInfo)
	
	// Save overrides to disk
	return d.discoveryCache.SaveManualOverrides()
}

// LoadCacheOnStartup loads cache on startup and returns whether cache was used
func (d *RegistryDiscovery) LoadCacheOnStartup() (bool, error) {
	if d.discoveryCache == nil {
		return false, fmt.Errorf("cache not initialized")
	}

	// Try to load existing cache
	cacheData, err := d.discoveryCache.LoadCache()
	if err != nil {
		return false, err
	}

	fmt.Printf("Loaded cache from %v with %d services (expires: %v)\n", 
		cacheData.CachedAt, cacheData.ServiceCount, cacheData.ExpiresAt)
	
	return true, nil
}

// CleanupOldCaches removes old cache backup files
func (d *RegistryDiscovery) CleanupOldCaches() error {
	if d.discoveryCache == nil {
		return fmt.Errorf("cache not initialized")
	}
	
	// Clean up caches older than 7 days
	return d.discoveryCache.CleanupOldCaches(7 * 24 * time.Hour)
}

// ForceRefresh forces a fresh discovery and updates the cache
func (d *RegistryDiscovery) ForceRefresh(ctx context.Context) ([]*pb.ServiceInfo, error) {
	// Invalidate cache first
	if err := d.InvalidateCache(); err != nil {
		return nil, fmt.Errorf("failed to invalidate cache: %w", err)
	}
	
	// Run fresh discovery
	return d.DiscoverServices(ctx)
}

// initKnownServices initializes the map of known AWS services for fallback
func initKnownServices() map[string]*RegistryServiceMetadata {
	return map[string]*RegistryServiceMetadata{
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
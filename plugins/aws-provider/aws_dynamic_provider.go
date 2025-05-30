package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/internal/cache"
	"github.com/jlgore/corkscrew/internal/db"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
	"github.com/jlgore/corkscrew/plugins/aws-provider/scanner"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DynamicAWSProvider implements the CloudProvider interface with dynamic service loading
type DynamicAWSProvider struct {
	mu               sync.RWMutex
	initialized      bool
	config           aws.Config
	region           string
	serviceDiscovery *discovery.ServiceDiscovery
	serviceLoader    *discovery.ServiceLoader
	resourceLister   *scanner.AWSResourceLister
	cacheDir         string
	pluginDir        string
	tempDir          string

	// Configuration
	githubToken     string
	maxConcurrency  int
	enableStreaming bool

	// DuckDB Integration
	graphLoader  *db.GraphLoader
	enableDuckDB bool

	// Schema Generation
	dynamicSchemaGenerator *DynamicSchemaGenerator
	standardSchemaGenerator *SchemaGenerator

	// Cache Manager
	cacheManager *cache.CacheManager
}

// NewDynamicAWSProvider creates a new dynamic AWS provider
func NewDynamicAWSProvider() *DynamicAWSProvider {
	// Setup directories
	homeDir, _ := os.UserHomeDir()
	cacheDir := filepath.Join(homeDir, ".corkscrew", "cache")
	pluginDir := filepath.Join(homeDir, ".corkscrew", "plugins")
	tempDir := filepath.Join(homeDir, ".corkscrew", "temp")

	// Create directories
	os.MkdirAll(cacheDir, 0755)
	os.MkdirAll(pluginDir, 0755)
	os.MkdirAll(tempDir, 0755)

	// Initialize DuckDB if enabled
	var graphLoader *db.GraphLoader
	enableDuckDB := os.Getenv("CORKSCREW_ENABLE_DUCKDB") != "false" // Default to enabled
	if enableDuckDB {
		dbPath := filepath.Join(cacheDir, "corkscrew.duckdb")
		var err error
		graphLoader, err = db.NewGraphLoader(dbPath)
		if err != nil {
			log.Printf("Warning: Failed to initialize DuckDB at %s: %v", dbPath, err)
			enableDuckDB = false
		} else {
			log.Printf("DuckDB initialized at: %s", dbPath)
		}
	}

	return &DynamicAWSProvider{
		serviceDiscovery: discovery.NewAWSServiceDiscovery(os.Getenv("GITHUB_TOKEN")),
		serviceLoader:    discovery.NewAWSServiceLoader(pluginDir, tempDir),
		resourceLister:   scanner.NewAWSResourceLister("us-east-1", false, false, os.Getenv("GITHUB_TOKEN")),
		cacheDir:         cacheDir,
		pluginDir:        pluginDir,
		tempDir:          tempDir,
		githubToken:      os.Getenv("GITHUB_TOKEN"),
		maxConcurrency:   10,
		enableStreaming:  true,
		graphLoader:      graphLoader,
		enableDuckDB:     enableDuckDB,
		dynamicSchemaGenerator: NewDynamicSchemaGenerator(),
		standardSchemaGenerator: NewSchemaGenerator(),
		cacheManager:     cache.NewCacheManager(),
	}
}

// Initialize initializes the AWS provider with configuration
func (p *DynamicAWSProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	log.Printf("Initializing AWS provider with region: %s", req.Config["region"])

	// Parse configuration options
	configOpts := discovery.AWSConfigOptions{
		Region:        req.Config["region"],
		Profile:       req.Config["profile"],
		AssumeRoleARN: req.Config["assume_role_arn"],
		ExternalID:    req.Config["external_id"],
		SessionName:   req.Config["session_name"],
		EndpointURL:   req.Config["endpoint_url"],
	}

	// Support max_retries from config
	if maxRetriesStr := req.Config["max_retries"]; maxRetriesStr != "" {
		if maxRetries, err := strconv.Atoi(maxRetriesStr); err == nil {
			configOpts.MaxRetries = maxRetries
		}
	}

	// Load AWS configuration with advanced options
	cfg, err := p.serviceLoader.CreateAWSConfigWithOptions(ctx, configOpts)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to load AWS config: %v", err),
		}, nil
	}

	// Validate credentials before proceeding
	if err := p.serviceLoader.ValidateAWSCredentials(ctx, cfg); err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("AWS credential validation failed: %v", err),
		}, nil
	}

	p.config = cfg
	p.region = configOpts.Region
	if p.region == "" {
		p.region = "us-east-1"
	}
	p.cacheDir = req.CacheDir
	p.initialized = true

	// Update resource lister with the configured region
	p.resourceLister = scanner.NewAWSResourceLister(p.region, false, true, p.githubToken)

	// Validate plugin environment
	if err := p.serviceLoader.ValidatePluginEnvironment(); err != nil {
		log.Printf("Warning: Plugin environment validation failed: %v", err)
		// Continue anyway - we can still discover services
	}

	// Discover available services
	services, err := p.serviceDiscovery.DiscoverAWSServices(ctx, false)
	if err != nil {
		log.Printf("Warning: Failed to discover services: %v", err)
		// Continue with empty service list
		services = []string{}
	}

	log.Printf("Discovered %d AWS services", len(services))

	return &pb.InitializeResponse{
		Success: true,
		Version: "1.0.0",
	}, nil
}

// DiscoverServices discovers available AWS services
func (p *DynamicAWSProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if !p.initialized {
		return &pb.DiscoverServicesResponse{
			Services: []*pb.ServiceInfo{},
		}, fmt.Errorf("provider not initialized")
	}

	// Discover services from GitHub
	services, err := p.serviceDiscovery.DiscoverAWSServices(ctx, req.ForceRefresh)
	if err != nil {
		return &pb.DiscoverServicesResponse{
			Services: []*pb.ServiceInfo{},
		}, fmt.Errorf("failed to discover services: %w", err)
	}

	// Convert to protobuf services
	pbServices := make([]*pb.ServiceInfo, 0, len(services))
	for _, serviceName := range services {
		service := &pb.ServiceInfo{
			Name:        serviceName,
			DisplayName: strings.Title(serviceName),
			PackageName: fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName),
			ClientType:  fmt.Sprintf("%sClient", strings.Title(serviceName)),
		}

		pbServices = append(pbServices, service)
	}

	return &pb.DiscoverServicesResponse{
		Services:     pbServices,
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   "2.0.0",
	}, nil
}

// ListResources lists resources for a specific service
func (p *DynamicAWSProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if !p.initialized {
		return &pb.ListResourcesResponse{
			Resources: []*pb.ResourceRef{},
		}, fmt.Errorf("provider not initialized")
	}

	// Load the requested service with region
	loadedService, err := p.serviceLoader.LoadServiceWithRegion(ctx, req.Service, req.Region)
	if err != nil {
		return &pb.ListResourcesResponse{
			Resources: []*pb.ResourceRef{},
		}, fmt.Errorf("failed to load service %s: %w", req.Service, err)
	}

	// Scan the service for resources
	resources, err := p.scanServiceForResourceRefs(ctx, req.Service, loadedService, req)
	if err != nil {
		return &pb.ListResourcesResponse{
			Resources: []*pb.ResourceRef{},
		}, fmt.Errorf("failed to scan service: %w", err)
	}

	return &pb.ListResourcesResponse{
		Resources:  resources,
		TotalCount: int32(len(resources)),
	}, nil
}

// BatchScan performs batch scanning of multiple services
func (p *DynamicAWSProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	if !p.initialized {
		return &pb.BatchScanResponse{
			Resources: []*pb.Resource{},
		}, fmt.Errorf("provider not initialized")
	}

	start := time.Now()
	stats := &pb.ScanStats{
		ResourceCounts: make(map[string]int32),
		ServiceCounts:  make(map[string]int32),
	}

	// Load services concurrently with region support
	loadedServices := make(map[string]*discovery.LoadedService)
	var errors []error

	for _, serviceName := range req.Services {
		loadedService, err := p.serviceLoader.LoadServiceWithRegion(ctx, serviceName, req.Region)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to load service %s: %w", serviceName, err))
			continue
		}
		loadedServices[serviceName] = loadedService
	}

	if len(errors) > 0 {
		log.Printf("Failed to load some services: %v", errors)
	}

	var allResources []*pb.Resource

	// Scan each loaded service
	for serviceName, loadedService := range loadedServices {
		resources, err := p.scanServiceForResources(ctx, serviceName, loadedService, req)
		if err != nil {
			log.Printf("Failed to scan service %s: %v", serviceName, err)
			stats.FailedResources++
			continue
		}

		allResources = append(allResources, resources...)
		stats.ResourceCounts[serviceName] = int32(len(resources))

		// Add operation count from service metadata
		if loadedService.Metadata != nil {
			stats.ServiceCounts[serviceName] = int32(len(loadedService.Metadata.Operations))
		}

		// Save resources to DuckDB in real-time
		if p.enableDuckDB && len(resources) > 0 {
			go p.saveResourcesToDuckDB(ctx, resources, serviceName)
		}
	}

	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(start).Milliseconds()

	// Save scan metadata to DuckDB
	if p.enableDuckDB {
		go p.saveScanMetadataToDuckDB(ctx, req.Services, stats, start)
	}

	return &pb.BatchScanResponse{
		Resources: allResources,
		Stats:     stats,
	}, nil
}

// GetSchemas generates SQL schemas for discovered resources
func (p *DynamicAWSProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized {
		return &pb.SchemaResponse{
			Schemas: []*pb.Schema{},
		}, fmt.Errorf("provider not initialized")
	}

	var schemas []*pb.Schema

	// Load services to analyze their resource types
	loadedServices, errors := p.serviceLoader.LoadMultipleServices(ctx, req.Services)
	if len(errors) > 0 {
		log.Printf("Failed to load some services: %v", errors)
	}

	// Generate schemas for each service
	for serviceName, loadedService := range loadedServices {
		serviceSchemas := p.generateServiceSchemas(serviceName, loadedService.Metadata)
		schemas = append(schemas, serviceSchemas...)
	}

	return &pb.SchemaResponse{
		Schemas: schemas,
	}, nil
}

// Helper methods

func (p *DynamicAWSProvider) scanServiceForResourceRefs(ctx context.Context, serviceName string, loadedService *discovery.LoadedService, req *pb.ListResourcesRequest) ([]*pb.ResourceRef, error) {
	// Get full resources first
	batchReq := &pb.BatchScanRequest{
		Services: []string{serviceName},
		Region:   req.Region,
	}
	fullResources, err := p.scanServiceForResources(ctx, serviceName, loadedService, batchReq)
	if err != nil {
		return nil, err
	}

	var resourceRefs []*pb.ResourceRef

	// Convert pb.Resource to pb.ResourceRef
	for _, resource := range fullResources {
		resourceRef := &pb.ResourceRef{
			Id:              resource.Id,
			Name:            resource.Name,
			Type:            resource.Type,
			Service:         resource.Service,
			Region:          resource.Region,
			BasicAttributes: make(map[string]string),
		}

		// Copy tags to basic attributes
		for k, v := range resource.Tags {
			resourceRef.BasicAttributes[k] = v
		}

		if resource.Arn != "" {
			resourceRef.BasicAttributes["arn"] = resource.Arn
		}

		resourceRefs = append(resourceRefs, resourceRef)
	}

	return resourceRefs, nil
}

func (p *DynamicAWSProvider) scanServiceForResources(ctx context.Context, serviceName string, loadedService *discovery.LoadedService, req *pb.BatchScanRequest) ([]*pb.Resource, error) {
	// Implement simple real AWS scanning using reflection
	client := loadedService.Client
	if client == nil {
		return nil, fmt.Errorf("no client available for service %s", serviceName)
	}

	var resources []*pb.Resource

	// Try common list operations for this service
	listOps := p.getCommonListOperations(serviceName)
	for _, opName := range listOps {
		serviceResources, err := p.executeListOperation(ctx, client, serviceName, opName, req.Region)
		if err != nil {
			// Log error but continue with other operations
			log.Printf("Failed to execute %s.%s: %v", serviceName, opName, err)
			continue
		}
		resources = append(resources, serviceResources...)
	}

	return resources, nil
}

// getCommonListOperations returns common list operations for AWS services
func (p *DynamicAWSProvider) getCommonListOperations(serviceName string) []string {
	switch serviceName {
	case "s3":
		return []string{"ListBuckets"}
	case "ec2":
		return []string{"DescribeInstances", "DescribeVpcs", "DescribeSubnets", "DescribeSecurityGroups"}
	case "iam":
		return []string{"ListUsers", "ListRoles", "ListPolicies", "ListGroups"}
	case "lambda":
		return []string{"ListFunctions"}
	case "rds":
		return []string{"DescribeDBInstances", "DescribeDBClusters"}
	case "dynamodb":
		return []string{"ListTables"}
	case "sns":
		return []string{"ListTopics"}
	case "sqs":
		return []string{"ListQueues"}
	case "cloudformation":
		return []string{"ListStacks"}
	default:
		return []string{}
	}
}

// executeListOperation executes a list operation using reflection
func (p *DynamicAWSProvider) executeListOperation(ctx context.Context, client interface{}, serviceName, opName, region string) ([]*pb.Resource, error) {
	clientValue := reflect.ValueOf(client)
	method := clientValue.MethodByName(opName)
	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found on %s client", opName, serviceName)
	}

	// Prepare arguments - most list operations just need context and empty input
	methodType := method.Type()
	args := []reflect.Value{reflect.ValueOf(ctx)}

	// Add input parameter if method expects one
	if methodType.NumIn() > 1 {
		inputType := methodType.In(1)
		var input reflect.Value
		if inputType.Kind() == reflect.Ptr {
			input = reflect.New(inputType.Elem())
		} else {
			input = reflect.Zero(inputType)
		}
		args = append(args, input)
	}

	// Call the method
	results := method.Call(args)
	if len(results) < 2 {
		return nil, fmt.Errorf("unexpected return values from %s", opName)
	}

	// Check for error
	if !results[1].IsNil() {
		err := results[1].Interface().(error)
		return nil, err
	}

	// Parse the output
	output := results[0]
	return p.parseListOperationOutput(output, serviceName, opName, region), nil
}

// parseListOperationOutput parses the output of a list operation
func (p *DynamicAWSProvider) parseListOperationOutput(output reflect.Value, serviceName, opName, region string) []*pb.Resource {
	var resources []*pb.Resource

	if output.Kind() == reflect.Ptr {
		output = output.Elem()
	}

	// Look for slice fields in the output
	for i := 0; i < output.NumField(); i++ {
		field := output.Field(i)
		if field.Kind() == reflect.Slice && field.Len() > 0 {
			resourceType := p.inferResourceTypeFromOperation(opName)

			for j := 0; j < field.Len(); j++ {
				item := field.Index(j)
				resource := p.parseResourceItem(item, resourceType, serviceName, region)
				if resource != nil {
					resources = append(resources, resource)
				}
			}
		}
	}

	return resources
}

// parseResourceItem parses an individual resource item
func (p *DynamicAWSProvider) parseResourceItem(item reflect.Value, resourceType, serviceName, region string) *pb.Resource {
	if item.Kind() == reflect.Ptr {
		if item.IsNil() {
			return nil
		}
		item = item.Elem()
	}

	resource := &pb.Resource{
		Provider:  "aws",
		Service:   serviceName,
		Type:      resourceType,
		Region:    region,
		Tags:      make(map[string]string),
		CreatedAt: timestamppb.Now(),
	}

	// Extract fields using reflection
	itemType := item.Type()
	for i := 0; i < item.NumField(); i++ {
		field := item.Field(i)
		fieldType := itemType.Field(i)
		fieldName := fieldType.Name

		if !field.CanInterface() {
			continue
		}

		// Extract ID
		if (strings.Contains(strings.ToLower(fieldName), "id") || strings.Contains(strings.ToLower(fieldName), "name")) && resource.Id == "" {
			if str := p.extractStringValue(field); str != "" {
				resource.Id = str
			}
		}

		// Extract Name
		if (strings.Contains(strings.ToLower(fieldName), "name") || fieldName == "Name") && resource.Name == "" {
			if str := p.extractStringValue(field); str != "" {
				resource.Name = str
			}
		}

		// Extract ARN
		if strings.Contains(strings.ToLower(fieldName), "arn") && resource.Arn == "" {
			if str := p.extractStringValue(field); str != "" {
				resource.Arn = str
			}
		}
	}

	// Set defaults
	if resource.Id == "" {
		resource.Id = fmt.Sprintf("unknown-%s", resourceType)
	}
	if resource.Name == "" {
		resource.Name = resource.Id
	}

	resource.RawData = fmt.Sprintf(`{"id":"%s","name":"%s","type":"%s","service":"%s"}`,
		resource.Id, resource.Name, resource.Type, resource.Service)

	return resource
}

// extractStringValue extracts a string value from a reflect.Value
func (p *DynamicAWSProvider) extractStringValue(field reflect.Value) string {
	switch field.Kind() {
	case reflect.String:
		return field.String()
	case reflect.Ptr:
		if !field.IsNil() && field.Elem().Kind() == reflect.String {
			return field.Elem().String()
		}
	}
	return ""
}

// inferResourceTypeFromOperation infers resource type from operation name
func (p *DynamicAWSProvider) inferResourceTypeFromOperation(opName string) string {
	switch opName {
	case "ListBuckets":
		return "Bucket"
	case "DescribeInstances":
		return "Instance"
	case "ListFunctions":
		return "Function"
	case "DescribeDBInstances":
		return "DBInstance"
	case "ListTables":
		return "Table"
	case "ListUsers":
		return "User"
	case "ListRoles":
		return "Role"
	case "ListPolicies":
		return "Policy"
	case "ListGroups":
		return "Group"
	case "DescribeVpcs":
		return "Vpc"
	case "DescribeSubnets":
		return "Subnet"
	case "DescribeSecurityGroups":
		return "SecurityGroup"
	case "ListTopics":
		return "Topic"
	case "ListQueues":
		return "Queue"
	case "ListStacks":
		return "Stack"
	default:
		return "Resource"
	}
}

func (p *DynamicAWSProvider) generateServiceSchemas(serviceName string, serviceInfo *generator.AWSServiceInfo) []*pb.Schema {
	var schemas []*pb.Schema

	for _, resourceType := range serviceInfo.ResourceTypes {
		tableName := fmt.Sprintf("aws_%s_%s", serviceName, strings.ToLower(resourceType.Name))
		
		// Try to generate dynamic schema from Go struct if available
		var schema *pb.Schema
		var err error
		
		// Check if we have a Go struct type for this resource
		if goType := p.getGoTypeForResource(serviceName, resourceType.Name); goType != nil {
			// Use dynamic schema generator for detailed analysis
			schema, err = p.dynamicSchemaGenerator.GenerateSchemaFromStruct(
				goType,
				tableName,
				serviceName,
				fmt.Sprintf("AWS::%s::%s", strings.Title(serviceName), resourceType.Name),
			)
			if err != nil {
				log.Printf("Failed to generate dynamic schema for %s.%s: %v", serviceName, resourceType.Name, err)
				schema = nil
			}
		}
		
		// Fallback to standard schema generation if dynamic generation failed or no Go type available
		if schema == nil {
			schema = &pb.Schema{
				Name:         tableName,
				Service:      serviceName,
				ResourceType: resourceType.Name,
				Sql: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
					id VARCHAR PRIMARY KEY,
					name VARCHAR,
					type VARCHAR NOT NULL,
					region VARCHAR,
					account_id VARCHAR,
					arn VARCHAR,
					tags JSON,
					attributes JSON,
					raw_data JSON,
					discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
					last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);
				
				-- Standard indexes
				CREATE INDEX IF NOT EXISTS idx_%s_type ON %s(type);
				CREATE INDEX IF NOT EXISTS idx_%s_region ON %s(region);
				CREATE INDEX IF NOT EXISTS idx_%s_name ON %s(name);
				CREATE INDEX IF NOT EXISTS idx_%s_discovered_at ON %s(discovered_at);`, 
					tableName,
					strings.ReplaceAll(serviceName, "-", "_"),
					tableName,
					strings.ReplaceAll(serviceName, "-", "_"),
					tableName,
					strings.ReplaceAll(serviceName, "-", "_"),
					tableName,
					strings.ReplaceAll(serviceName, "-", "_"),
					tableName,
				),
				Description: fmt.Sprintf("AWS %s %s resources", strings.ToUpper(serviceName), resourceType.Name),
				Metadata: map[string]string{
					"service":       serviceName,
					"resource_type": resourceType.Name,
					"provider":      "aws",
					"schema_type":   "standard",
				},
			}
		} else {
			// Mark as dynamically generated
			if schema.Metadata == nil {
				schema.Metadata = make(map[string]string)
			}
			schema.Metadata["schema_type"] = "dynamic"
		}

		schemas = append(schemas, schema)
	}

	return schemas
}

// getGoTypeForResource returns the Go reflect.Type for a given AWS resource
// This maps AWS service and resource names to their corresponding Go struct types
func (p *DynamicAWSProvider) getGoTypeForResource(serviceName, resourceTypeName string) reflect.Type {
	// Map known AWS resource types to their Go struct types
	// This would typically be populated from the AWS SDK or generated code
	
	resourceKey := fmt.Sprintf("%s.%s", serviceName, resourceTypeName)
	
	// Known resource type mappings - this could be loaded from configuration
	knownTypes := map[string]reflect.Type{
		"ec2.Instance":        reflect.TypeOf(EC2Instance{}),
		"s3.Bucket":          reflect.TypeOf(S3Bucket{}),
		"lambda.Function":    reflect.TypeOf(LambdaFunction{}),
		// Add more mappings as needed
	}
	
	if goType, exists := knownTypes[resourceKey]; exists {
		return goType
	}
	
	// Try common patterns
	switch serviceName {
	case "ec2":
		switch strings.ToLower(resourceTypeName) {
		case "instance", "instances":
			return reflect.TypeOf(EC2Instance{})
		}
	case "s3":
		switch strings.ToLower(resourceTypeName) {
		case "bucket", "buckets":
			return reflect.TypeOf(S3Bucket{})
		}
	case "lambda":
		switch strings.ToLower(resourceTypeName) {
		case "function", "functions":
			return reflect.TypeOf(LambdaFunction{})
		}
	}
	
	// No known Go type found
	return nil
}

// saveResourcesToDuckDB saves resources to DuckDB asynchronously
func (p *DynamicAWSProvider) saveResourcesToDuckDB(ctx context.Context, resources []*pb.Resource, serviceName string) {
	if p.graphLoader == nil {
		return
	}

	// Use a background context to avoid timeout issues
	bgCtx := context.Background()

	if err := p.graphLoader.LoadResources(bgCtx, resources); err != nil {
		log.Printf("Failed to save %d resources from %s to DuckDB: %v", len(resources), serviceName, err)
	} else {
		log.Printf("Successfully saved %d resources from %s to DuckDB", len(resources), serviceName)
	}
}

// saveScanMetadataToDuckDB saves scan metadata to DuckDB
func (p *DynamicAWSProvider) saveScanMetadataToDuckDB(ctx context.Context, services []string, stats *pb.ScanStats, startTime time.Time) {
	if p.graphLoader == nil {
		return
	}

	// Create scan metadata record
	scanID := fmt.Sprintf("scan_%d", startTime.Unix())
	metadata := map[string]interface{}{
		"services_requested": services,
		"service_counts":     stats.ServiceCounts,
		"resource_counts":    stats.ResourceCounts,
		"total_resources":    stats.TotalResources,
		"failed_resources":   stats.FailedResources,
		"scan_duration_ms":   stats.DurationMs,
	}

	// Save to scan_metadata table (need to add this method to GraphLoader)
	bgCtx := context.Background()
	if err := p.saveScanRecord(bgCtx, scanID, services, stats, metadata); err != nil {
		log.Printf("Failed to save scan metadata to DuckDB: %v", err)
	}
}

// saveScanRecord saves a scan record to the scan_metadata table
func (p *DynamicAWSProvider) saveScanRecord(ctx context.Context, scanID string, services []string, stats *pb.ScanStats, metadata map[string]interface{}) error {
	if p.graphLoader == nil {
		return fmt.Errorf("DuckDB not initialized")
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Execute the insert directly using the GraphLoader's DB connection
	query := `
		INSERT INTO scan_metadata 
		(id, service, region, scan_time, total_resources, failed_resources, duration_ms, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	// For multiple services, we'll create one record per service
	for _, service := range services {
		_, err = p.graphLoader.ExecRaw(ctx, query,
			fmt.Sprintf("%s_%s", scanID, service),
			service,
			p.region,
			time.Now(),
			stats.TotalResources,
			stats.FailedResources,
			stats.DurationMs,
			string(metadataJSON),
		)
		if err != nil {
			return fmt.Errorf("failed to insert scan metadata for service %s: %w", service, err)
		}
	}

	return nil
}

// GetDuckDBStats returns statistics from DuckDB
func (p *DynamicAWSProvider) GetDuckDBStats(ctx context.Context, service string, hours int) (map[string]interface{}, error) {
	if !p.enableDuckDB || p.graphLoader == nil {
		return nil, fmt.Errorf("DuckDB not enabled")
	}

	return p.graphLoader.GetAPIActionStats(ctx, service, hours)
}

// QueryDuckDB allows direct SQL queries against the DuckDB instance
func (p *DynamicAWSProvider) QueryDuckDB(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	if !p.enableDuckDB || p.graphLoader == nil {
		return nil, fmt.Errorf("DuckDB not enabled")
	}

	return p.graphLoader.Query(ctx, query, args...)
}

// DescribeResource describes a specific resource by ID
func (p *DynamicAWSProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	if !p.initialized {
		return &pb.DescribeResourceResponse{}, fmt.Errorf("provider not initialized")
	}

	if req.ResourceRef == nil {
		return &pb.DescribeResourceResponse{}, fmt.Errorf("resource reference is required")
	}

	// Load the service with region
	loadedService, err := p.serviceLoader.LoadServiceWithRegion(ctx, req.ResourceRef.Service, req.ResourceRef.Region)
	if err != nil {
		return &pb.DescribeResourceResponse{}, fmt.Errorf("failed to load service %s: %w", req.ResourceRef.Service, err)
	}

	// TODO: Implement actual resource description using the loaded service
	// For now, return a mock response
	resource := &pb.Resource{
		Provider:  "aws",
		Service:   req.ResourceRef.Service,
		Type:      req.ResourceRef.Type,
		Id:        req.ResourceRef.Id,
		Name:      req.ResourceRef.Name,
		Region:    req.ResourceRef.Region,
		Arn:       fmt.Sprintf("arn:aws:%s:%s:123456789012:%s/%s", req.ResourceRef.Service, req.ResourceRef.Region, strings.ToLower(req.ResourceRef.Type), req.ResourceRef.Id),
		Tags:      make(map[string]string),
		CreatedAt: timestamppb.Now(),
		RawData:   fmt.Sprintf(`{"id":"%s","type":"%s","service":"%s"}`, req.ResourceRef.Id, req.ResourceRef.Type, req.ResourceRef.Service),
	}

	// Add tags if requested
	if req.IncludeTags {
		resource.Tags["example"] = "true"
	}

	// Add relationships if requested
	if req.IncludeRelationships {
		// TODO: Implement relationship discovery
	}

	_ = loadedService // Use the loaded service when implementing actual logic

	return &pb.DescribeResourceResponse{
		Resource: resource,
	}, nil
}

// GetProviderInfo returns information about the provider
func (p *DynamicAWSProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "aws",
		Version:     "1.0.0",
		Description: "Dynamic AWS provider with advanced features including multi-region support, caching, and parallel execution",
		Capabilities: map[string]string{
			"multi-region":              "true",
			"cross-account":             "true",
			"caching":                   "true",
			"parallel-execution":        "true",
			"dynamic-service-discovery": "true",
			"hierarchical-scanning":     "true",
		},
		SupportedServices: []string{}, // Will be populated dynamically
	}, nil
}

// GenerateServiceScanners generates scanner plugins for discovered services
func (p *DynamicAWSProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	if !p.initialized {
		return &pb.GenerateScannersResponse{}, fmt.Errorf("provider not initialized")
	}

	// TODO: Implement scanner generation using the plugin generator
	// For now, return an empty response
	return &pb.GenerateScannersResponse{
		Scanners: []*pb.GeneratedScanner{},
	}, nil
}

// StreamScan performs streaming scan of resources
func (p *DynamicAWSProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}

	// TODO: Implement streaming scan
	// For now, just return without sending any resources
	return nil
}

// Cleanup cleans up resources
func (p *DynamicAWSProvider) Cleanup() error {
	p.serviceLoader.UnloadAllServices()

	// Close DuckDB connection
	if p.graphLoader != nil {
		if err := p.graphLoader.Close(); err != nil {
			log.Printf("Error closing DuckDB connection: %v", err)
		}
	}

	return p.serviceLoader.CleanupTempFiles()
}

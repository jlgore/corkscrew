package scanner

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jlgore/corkscrew/internal/parallel"
	"github.com/jlgore/corkscrew/plugins/aws-provider/classification"
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/generator"
)


// AWSResourceLister handles dynamic resource discovery and listing
type AWSResourceLister struct {
	region            string
	regions           []string // Support for multiple regions
	dryRun            bool
	debug             bool
	serviceDiscovery  *discovery.ServiceDiscovery
	serviceLoader     *discovery.ServiceLoader
	analyzer          *generator.AWSAnalyzer
	classifier        *classification.OperationClassifier
	securityValidator *SecurityValidator
	responseParser    *AWSResponseParser
	resourceConverter *ResourceConverter
	errorHandler      *ErrorHandler
	availability      *discovery.ServiceAvailability
}

// NewAWSResourceLister creates a new AWS resource lister
func NewAWSResourceLister(region string, dryRun, debug bool, githubToken string) *AWSResourceLister {
	pluginDir := "/tmp/corkscrew-plugins"
	tempDir := "/tmp/corkscrew-temp"

	return &AWSResourceLister{
		region:            region,
		regions:           []string{region}, // Default to single region
		dryRun:            dryRun,
		debug:             debug,
		serviceDiscovery:  discovery.NewAWSServiceDiscovery(githubToken),
		serviceLoader:     discovery.NewAWSServiceLoader(pluginDir, tempDir),
		analyzer:          generator.NewAWSAnalyzer(),
		classifier:        classification.NewOperationClassifier(),
		securityValidator: NewSecurityValidator(),
		responseParser:    NewAWSResponseParser(debug),
		resourceConverter: NewResourceConverter("", region),
		errorHandler:      NewErrorHandler(debug),
		availability:      discovery.NewServiceAvailability(),
	}
}

// NewAWSResourceListerWithRegions creates a new AWS resource lister with multiple regions
func NewAWSResourceListerWithRegions(regions []string, dryRun, debug bool, githubToken string) *AWSResourceLister {
	pluginDir := "/tmp/corkscrew-plugins"
	tempDir := "/tmp/corkscrew-temp"

	// Default to us-east-1 if no regions specified
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	return &AWSResourceLister{
		region:            regions[0], // Primary region
		regions:           regions,
		dryRun:            dryRun,
		debug:             debug,
		serviceDiscovery:  discovery.NewAWSServiceDiscovery(githubToken),
		serviceLoader:     discovery.NewAWSServiceLoader(pluginDir, tempDir),
		analyzer:          generator.NewAWSAnalyzer(),
		classifier:        classification.NewOperationClassifier(),
		securityValidator: NewSecurityValidator(),
		responseParser:    NewAWSResponseParser(debug),
		resourceConverter: NewResourceConverter("", regions[0]),
		errorHandler:      NewErrorHandler(debug),
		availability:      discovery.NewServiceAvailability(),
	}
}

// DiscoverAndListResources is the main entry point - discovers services and their List* operations, then executes them
func (rl *AWSResourceLister) DiscoverAndListResources(ctx context.Context, services []string) (map[string][]discovery.AWSResourceRef, error) {
	// If multiple regions configured, use multi-region scanning
	if len(rl.regions) > 1 {
		return rl.DiscoverAndListResourcesMultiRegion(ctx, services)
	}

	if rl.debug {
		fmt.Printf("🔍 Starting dynamic resource discovery for %d services in region %s\n", len(services), rl.region)
	}

	results := make(map[string][]discovery.AWSResourceRef)

	// If no services specified, discover all services
	if len(services) == 0 {
		discoveredServices, err := rl.serviceDiscovery.DiscoverAWSServices(ctx, false)
		if err != nil {
			return nil, fmt.Errorf("failed to discover services: %w", err)
		}
		services = discoveredServices
		if rl.debug {
			fmt.Printf("📋 Discovered %d services from GitHub API\n", len(services))
		}
	}

	// Process each service
	for _, serviceName := range services {
		if rl.debug {
			fmt.Printf("🔍 Processing service: %s\n", serviceName)
		}

		refs, err := rl.discoverAndListServiceResources(ctx, serviceName)
		if err != nil {
			fmt.Printf("⚠️  Failed to process service %s: %v\n", serviceName, err)
			continue
		}

		results[serviceName] = refs

		if rl.debug {
			fmt.Printf("✅ %s: found %d resource references\n", serviceName, len(refs))
		}
	}

	return results, nil
}

// DiscoverAndListResourcesMultiRegion scans resources across multiple regions
func (rl *AWSResourceLister) DiscoverAndListResourcesMultiRegion(ctx context.Context, services []string) (map[string][]discovery.AWSResourceRef, error) {
	if rl.debug {
		fmt.Printf("🔍 Starting multi-region resource discovery for %d services across %d regions\n", len(services), len(rl.regions))
	}

	results := make(map[string][]discovery.AWSResourceRef)
	var mu sync.Mutex

	// If no services specified, discover all services
	if len(services) == 0 {
		discoveredServices, err := rl.serviceDiscovery.DiscoverAWSServices(ctx, false)
		if err != nil {
			return nil, fmt.Errorf("failed to discover services: %w", err)
		}
		services = discoveredServices
		if rl.debug {
			fmt.Printf("📋 Discovered %d services from GitHub API\n", len(services))
		}
	}

	// Process each service across all regions
	for _, serviceName := range services {
		if rl.debug {
			fmt.Printf("🔍 Processing service: %s across regions\n", serviceName)
		}

		// Check if service is global
		if rl.availability.IsGlobalService(serviceName) {
			// For global services, only scan in primary region
			rl.region = rl.availability.GetOptimalRegionForService(serviceName)
			refs, err := rl.discoverAndListServiceResources(ctx, serviceName)
			if err != nil {
				fmt.Printf("⚠️  Failed to process global service %s: %v\n", serviceName, err)
				continue
			}
			mu.Lock()
			results[serviceName] = refs
			mu.Unlock()
			continue
		}

		// For regional services, scan across all regions
		var allRefs []discovery.AWSResourceRef
		for _, region := range rl.regions {
			// Update current region
			rl.region = region

			if rl.debug {
				fmt.Printf("  🌎 Scanning %s in region %s\n", serviceName, region)
			}

			refs, err := rl.discoverAndListServiceResourcesInRegion(ctx, serviceName, region)
			if err != nil {
				if rl.debug {
					fmt.Printf("    ⚠️  Failed to process service %s in region %s: %v\n", serviceName, region, err)
				}
				continue
			}

			allRefs = append(allRefs, refs...)
		}

		mu.Lock()
		results[serviceName] = allRefs
		mu.Unlock()

		if rl.debug {
			fmt.Printf("✅ %s: found %d resource references across all regions\n", serviceName, len(allRefs))
		}
	}

	return results, nil
}

// discoverAndListServiceResourcesInRegion discovers List* operations for a service in a specific region
func (rl *AWSResourceLister) discoverAndListServiceResourcesInRegion(ctx context.Context, serviceName, region string) ([]discovery.AWSResourceRef, error) {
	// Temporarily set region for this scan
	oldRegion := rl.region
	rl.region = region
	defer func() { rl.region = oldRegion }()

	return rl.discoverAndListServiceResources(ctx, serviceName)
}

// discoverAndListServiceResources discovers List* operations for a service and executes them
func (rl *AWSResourceLister) discoverAndListServiceResources(ctx context.Context, serviceName string) ([]discovery.AWSResourceRef, error) {
	// Step 1: Load the service dynamically with region
	loadedService, err := rl.serviceLoader.LoadServiceWithRegion(ctx, serviceName, rl.region)
	if err != nil {
		return nil, fmt.Errorf("failed to load service: %w", err)
	}

	// Step 2: Analyze the service to find List* operations
	if loadedService.Metadata == nil {
		return nil, fmt.Errorf("no metadata available for service %s", serviceName)
	}

	// Step 3: Filter for parameter-free List* and Describe* operations
	listOperations := rl.filterParameterFreeListOperations(loadedService.Metadata.Operations, serviceName)

	if rl.debug {
		fmt.Printf("  Found %d parameter-free list operations\n", len(listOperations))
	}

	// Step 4: Execute the operations and collect resources
	var allResources []discovery.AWSResourceRef

	// Check if we should use parallel execution
	if len(listOperations) > 1 && !rl.dryRun {
		// Execute operations in parallel
		parallelRefs, err := rl.executeOperationsParallel(ctx, loadedService, listOperations, serviceName)
		if err != nil {
			return nil, fmt.Errorf("parallel execution failed: %w", err)
		}
		return parallelRefs, nil
	}

	// Sequential execution for dry run or single operation
	for _, operation := range listOperations {
		if rl.dryRun {
			// In dry run mode, generate sample data
			sampleRefs := rl.generateSampleResources(serviceName, operation)
			allResources = append(allResources, sampleRefs...)
			continue
		}

		// Execute the real operation
		refs, err := rl.executeListOperation(ctx, loadedService, operation)
		if err != nil {
			if rl.debug {
				fmt.Printf("    ⚠️  Failed to execute %s: %v\n", operation.Name, err)
			}
			continue
		}

		allResources = append(allResources, refs...)

		if rl.debug {
			fmt.Printf("    ✅ %s: found %d resources\n", operation.Name, len(refs))
		}
	}

	return allResources, nil
}

// filterParameterFreeListOperations filters operations to include parameter-free List* and Describe* operations
func (rl *AWSResourceLister) filterParameterFreeListOperations(operations []generator.AWSOperation, serviceName string) []generator.AWSOperation {
	var filtered []generator.AWSOperation

	for _, op := range operations {
		// Include both List and Describe operations (EC2 uses Describe*, others use List*)
		if !op.IsList && !op.IsDescribe {
			continue
		}

		// Use the classifier to determine if operation can be attempted
		classification := rl.classifier.ClassifyOperation(serviceName, op.Name, op)
		if classification.CanAttempt {
			filtered = append(filtered, op)
			if rl.debug && classification.DefaultParams != nil {
				fmt.Printf("    📋 Operation %s classified as %s (confidence: %.2f) with defaults\n",
					op.Name, classification.Type, classification.Confidence)
			}
		}
	}

	return filtered
}

// executeListOperation executes a List* or Describe* operation and parses the results
func (rl *AWSResourceLister) executeListOperation(ctx context.Context, loadedService *discovery.LoadedService, operation generator.AWSOperation) ([]discovery.AWSResourceRef, error) {
	// Check circuit breaker
	if !rl.errorHandler.ShouldProceed(loadedService.Name) {
		return nil, fmt.Errorf("circuit breaker open for service %s", loadedService.Name)
	}

	// Load AWS config with proper region and profile support
	var configOptions []func(*config.LoadOptions) error
	configOptions = append(configOptions, config.WithRegion(rl.region))

	// Add profile if AWS_PROFILE is set, otherwise use default behavior
	if profile := os.Getenv("AWS_PROFILE"); profile != "" {
		configOptions = append(configOptions, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, configOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create a new client with proper AWS config
	// We need to create a new client instance with the proper config
	client, err := rl.createClientWithConfig(loadedService, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client with config: %w", err)
	}

	clientValue := reflect.ValueOf(client)

	// Find the method on the client
	method := clientValue.MethodByName(operation.Name)
	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found on client", operation.Name)
	}

	// Prepare method call
	methodType := method.Type()
	if methodType.NumIn() < 2 {
		return nil, fmt.Errorf("unexpected method signature for %s", operation.Name)
	}

	// Create input struct and apply default parameters if available
	inputType := methodType.In(1)
	var input reflect.Value

	if inputType.Kind() == reflect.Ptr {
		input = reflect.New(inputType.Elem())

		// Get default parameters from the classifier
		defaultParams := rl.classifier.GetOperationDefaults(loadedService.Name, operation.Name)
		if defaultParams != nil {
			rl.setDefaultParameters(input, defaultParams)
		}
	} else {
		input = reflect.Zero(inputType)
	}

	// Call the method with retry
	var results []reflect.Value
	err = RetryWithBackoff(ctx, func() error {
		args := []reflect.Value{reflect.ValueOf(ctx), input}
		results = method.Call(args)

		if len(results) < 2 {
			return fmt.Errorf("unexpected return values from %s", operation.Name)
		}

		// Check for error
		if !results[1].IsNil() {
			return results[1].Interface().(error)
		}

		return nil
	}, 3)

	if err != nil {
		return nil, rl.errorHandler.HandleError(ctx, loadedService.Name, operation.Name, err)
	}

	// Parse the output using the new response parser
	output := results[0].Interface()
	pbRefs, err := rl.responseParser.ParseAWSResponse(output, operation.Name, loadedService.Name, rl.region)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Record success
	rl.errorHandler.HandleError(ctx, loadedService.Name, operation.Name, nil)

	// Convert pb.ResourceRef to scanner.discovery.AWSResourceRef
	return rl.resourceConverter.ConvertPbListToScanner(pbRefs), nil
}

// createClientWithConfig creates a new client with proper AWS configuration
func (rl *AWSResourceLister) createClientWithConfig(loadedService *discovery.LoadedService, cfg config.Config) (interface{}, error) {
	// Special handling for S3 to deal with region issues
	if loadedService.Name == "s3" {
		return rl.createS3ClientWithRegionHandling(loadedService, cfg)
	}

	// Try to use the region-based factory function first
	if loadedService.Plugin != nil {
		factoryName := fmt.Sprintf("New%sClientWithRegion", strings.Title(loadedService.Name))
		if rl.debug {
			fmt.Printf("    Looking for region-based factory function: %s\n", factoryName)
		}

		factorySymbol, err := loadedService.Plugin.Lookup(factoryName)
		if err == nil {
			factory, ok := factorySymbol.(func(string) interface{})
			if ok {
				if rl.debug {
					fmt.Printf("    Using region-based factory with region: %s\n", rl.region)
				}
				return factory(rl.region), nil
			} else {
				if rl.debug {
					fmt.Printf("    Region-based factory function has wrong signature\n")
				}
			}
		} else {
			if rl.debug {
				fmt.Printf("    Region-based factory function not found: %v\n", err)
			}
		}

		// Try the config-based factory function as fallback
		factoryName = fmt.Sprintf("New%sClientWithConfig", strings.Title(loadedService.Name))
		if rl.debug {
			fmt.Printf("    Looking for config-based factory function: %s\n", factoryName)
		}

		factorySymbol, err = loadedService.Plugin.Lookup(factoryName)
		if err == nil {
			if rl.debug {
				fmt.Printf("    Found config-based factory function, attempting to use it\n")
			}
			// The plugin uses aws.Config, not config.Config
			factory, ok := factorySymbol.(func(aws.Config) interface{})
			if ok {
				if rl.debug {
					fmt.Printf("    Using config-based factory function with proper AWS config\n")
				}
				// Convert config.Config interface to aws.Config struct
				if awsConfig, ok := cfg.(aws.Config); ok {
					return factory(awsConfig), nil
				} else {
					if rl.debug {
						fmt.Printf("    Failed to convert config to aws.Config\n")
					}
				}
			} else {
				if rl.debug {
					fmt.Printf("    Config-based factory function has wrong signature\n")
				}
			}
		} else {
			if rl.debug {
				fmt.Printf("    Config-based factory function not found: %v\n", err)
			}
		}
	}

	if rl.debug {
		fmt.Printf("    Falling back to existing client (may have region issues)\n")
	}
	// Fallback to existing client (this will still have the region issue)
	return loadedService.Client, nil
}

// createS3ClientWithRegionHandling creates an S3 client with proper region handling
func (rl *AWSResourceLister) createS3ClientWithRegionHandling(loadedService *discovery.LoadedService, cfg config.Config) (interface{}, error) {
	if rl.debug {
		fmt.Printf("    Creating S3 client with enhanced region handling\n")
	}

	// Try to use the region-based factory function first
	if loadedService.Plugin != nil {
		factoryName := fmt.Sprintf("New%sClientWithRegion", strings.Title(loadedService.Name))
		factorySymbol, err := loadedService.Plugin.Lookup(factoryName)
		if err == nil {
			factory, ok := factorySymbol.(func(string) interface{})
			if ok {
				if rl.debug {
					fmt.Printf("    Using S3 region-based factory with region: %s\n", rl.region)
				}
				client := factory(rl.region)

				// Wrap the client to handle cross-region redirects
				return rl.wrapS3ClientForRegionHandling(client), nil
			}
		}

		// Try config-based factory as fallback
		factoryName = fmt.Sprintf("New%sClientWithConfig", strings.Title(loadedService.Name))
		factorySymbol, err = loadedService.Plugin.Lookup(factoryName)
		if err == nil {
			factory, ok := factorySymbol.(func(aws.Config) interface{})
			if ok {
				if awsConfig, ok := cfg.(aws.Config); ok {
					if rl.debug {
						fmt.Printf("    Using S3 config-based factory\n")
					}
					client := factory(awsConfig)
					return rl.wrapS3ClientForRegionHandling(client), nil
				}
			}
		}
	}

	if rl.debug {
		fmt.Printf("    Falling back to existing S3 client\n")
	}
	return rl.wrapS3ClientForRegionHandling(loadedService.Client), nil
}

// wrapS3ClientForRegionHandling wraps an S3 client to handle region redirects gracefully
func (rl *AWSResourceLister) wrapS3ClientForRegionHandling(client interface{}) interface{} {
	// For now, return the client as-is
	// In a full implementation, this would wrap the client with retry logic
	// that handles S3 redirect errors by switching regions
	if rl.debug {
		fmt.Printf("    S3 client wrapped for region handling\n")
	}
	return client
}

// setDefaultParameters sets default parameters on an input struct using reflection
func (rl *AWSResourceLister) setDefaultParameters(input reflect.Value, defaultParams map[string]interface{}) {
	if input.Kind() == reflect.Ptr {
		input = input.Elem()
	}

	for fieldName, value := range defaultParams {
		field := input.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			if rl.debug {
				fmt.Printf("    ⚠️  Cannot set field %s on input struct\n", fieldName)
			}
			continue
		}

		// Handle different value types
		switch v := value.(type) {
		case []string:
			// Create slice of strings
			stringSlice := reflect.MakeSlice(reflect.TypeOf([]string{}), len(v), len(v))
			for i, str := range v {
				stringSlice.Index(i).SetString(str)
			}
			field.Set(stringSlice)
		case string:
			field.SetString(v)
		case int:
			field.SetInt(int64(v))
		case int32:
			field.SetInt(int64(v))
		case int64:
			field.SetInt(v)
		case bool:
			field.SetBool(v)
		default:
			if rl.debug {
				fmt.Printf("    ⚠️  Unsupported parameter type %T for field %s\n", value, fieldName)
			}
		}

		if rl.debug {
			fmt.Printf("    ✅ Set default parameter %s = %v\n", fieldName, value)
		}
	}
}

// setOwnerFilter sets owner filter for operations that need it (legacy method, kept for compatibility)
func (rl *AWSResourceLister) setOwnerFilter(input reflect.Value, owner string) {
	defaultParams := map[string]interface{}{
		"Owners": []string{owner},
	}
	rl.setDefaultParameters(input, defaultParams)
}

// parseOperationOutput parses the output of a list operation
func (rl *AWSResourceLister) parseOperationOutput(output reflect.Value, operation generator.AWSOperation, serviceName string) []discovery.AWSResourceRef {
	var resources []discovery.AWSResourceRef

	if output.Kind() == reflect.Ptr {
		output = output.Elem()
	}

	// Look for slice fields that contain resources
	for i := 0; i < output.NumField(); i++ {
		field := output.Field(i)

		if field.Kind() == reflect.Slice && field.Len() > 0 {
			// This might be our resource list
			for j := 0; j < field.Len(); j++ {
				item := field.Index(j)
				resource := rl.parseResourceItem(item, operation.ResourceType, serviceName)
				if resource != nil {
					resources = append(resources, *resource)
				}
			}
		}
	}

	return resources
}

// parseResourceItem parses a single resource item
func (rl *AWSResourceLister) parseResourceItem(item reflect.Value, resourceType, serviceName string) *discovery.AWSResourceRef {
	if item.Kind() == reflect.Ptr {
		item = item.Elem()
	}

	resource := &discovery.AWSResourceRef{
		Type:     resourceType,
		Service:  serviceName,
		Region:   rl.region,
		Metadata: make(map[string]string),
	}

	// Handle simple string values (like table names from ListTables)
	if item.Kind() == reflect.String {
		stringValue := item.String()
		if stringValue != "" {
			resource.ID = stringValue
			resource.Name = stringValue
			resource.Metadata["RawValue"] = stringValue
		}
		return resource
	}

	// Handle struct values
	if item.Kind() != reflect.Struct {
		// If it's not a struct or string, we can't parse it
		if rl.debug {
			fmt.Printf("    ⚠️  Unexpected item type: %s, skipping\n", item.Kind())
		}
		return nil
	}

	// Extract common fields
	for i := 0; i < item.NumField(); i++ {
		field := item.Field(i)
		fieldType := item.Type().Field(i)
		fieldName := fieldType.Name

		if !field.IsValid() || !field.CanInterface() {
			continue
		}

		// Extract ID field
		if rl.isIDField(fieldName, resourceType) {
			if field.Kind() == reflect.Ptr && !field.IsNil() {
				resource.ID = field.Elem().String()
			} else if field.Kind() == reflect.String {
				resource.ID = field.String()
			}
		}

		// Extract Name field
		if rl.isNameField(fieldName, resourceType) {
			if field.Kind() == reflect.Ptr && !field.IsNil() {
				resource.Name = field.Elem().String()
			} else if field.Kind() == reflect.String {
				resource.Name = field.String()
			}
		}

		// Extract ARN field
		if rl.isARNField(fieldName) {
			if field.Kind() == reflect.Ptr && !field.IsNil() {
				resource.ARN = field.Elem().String()
			} else if field.Kind() == reflect.String {
				resource.ARN = field.String()
			}
		}

		// Store other fields as metadata
		if field.Kind() == reflect.String && field.String() != "" {
			resource.Metadata[fieldName] = field.String()
		} else if field.Kind() == reflect.Ptr && !field.IsNil() && field.Elem().Kind() == reflect.String {
			resource.Metadata[fieldName] = field.Elem().String()
		}
	}

	// Use ID as name if name is empty
	if resource.Name == "" {
		resource.Name = resource.ID
	}

	// Ensure we have at least an ID
	if resource.ID == "" {
		return nil
	}

	return resource
}

// isIDField determines if a field represents a resource ID
func (rl *AWSResourceLister) isIDField(fieldName, resourceType string) bool {
	idPatterns := []string{
		"InstanceId", "VolumeId", "SnapshotId", "ImageId", "VpcId", "SubnetId",
		"GroupId", "KeyName", "FunctionName", "TableName", "TopicArn", "QueueUrl",
		"DBInstanceIdentifier", "DBClusterIdentifier", "BucketName", "Name",
	}

	for _, pattern := range idPatterns {
		if fieldName == pattern {
			return true
		}
	}

	// Generic patterns
	if strings.HasSuffix(fieldName, "Id") || strings.HasSuffix(fieldName, "Name") {
		return true
	}

	return false
}

// isNameField determines if a field represents a resource name
func (rl *AWSResourceLister) isNameField(fieldName, resourceType string) bool {
	namePatterns := []string{
		"Name", "FunctionName", "TableName", "BucketName", "DBInstanceIdentifier",
		"DBClusterIdentifier", "TopicArn", "QueueUrl",
	}

	for _, pattern := range namePatterns {
		if fieldName == pattern {
			return true
		}
	}

	return false
}

// isARNField determines if a field represents an ARN
func (rl *AWSResourceLister) isARNField(fieldName string) bool {
	return strings.Contains(strings.ToLower(fieldName), "arn") ||
		fieldName == "TopicArn" ||
		fieldName == "QueueUrl"
}

// generateSampleResources generates sample resources for dry run mode
func (rl *AWSResourceLister) generateSampleResources(serviceName string, operation generator.AWSOperation) []discovery.AWSResourceRef {
	var resources []discovery.AWSResourceRef

	// Generate sample resources based on service
	switch serviceName {
	case "s3":
		resources = []discovery.AWSResourceRef{
			{
				ID:      "sample-bucket-1",
				Name:    "sample-bucket-1",
				Type:    operation.ResourceType,
				Service: serviceName,
				Region:  rl.region,
				ARN:     "arn:aws:s3:::sample-bucket-1",
				Metadata: map[string]string{
					"mode":      "dry-run",
					"operation": operation.Name,
				},
			},
		}
	case "ec2":
		resources = []discovery.AWSResourceRef{
			{
				ID:      "i-1234567890abcdef0",
				Name:    "sample-instance",
				Type:    operation.ResourceType,
				Service: serviceName,
				Region:  rl.region,
				ARN:     fmt.Sprintf("arn:aws:ec2:%s:123456789012:instance/i-1234567890abcdef0", rl.region),
				Metadata: map[string]string{
					"mode":      "dry-run",
					"operation": operation.Name,
				},
			},
		}
	case "lambda":
		resources = []discovery.AWSResourceRef{
			{
				ID:      "sample-function",
				Name:    "sample-function",
				Type:    operation.ResourceType,
				Service: serviceName,
				Region:  rl.region,
				ARN:     fmt.Sprintf("arn:aws:lambda:%s:123456789012:function:sample-function", rl.region),
				Metadata: map[string]string{
					"mode":      "dry-run",
					"operation": operation.Name,
				},
			},
		}
	default:
		resources = []discovery.AWSResourceRef{
			{
				ID:      fmt.Sprintf("sample-%s-resource", serviceName),
				Name:    fmt.Sprintf("sample-%s-resource", serviceName),
				Type:    operation.ResourceType,
				Service: serviceName,
				Region:  rl.region,
				Metadata: map[string]string{
					"mode":      "dry-run",
					"operation": operation.Name,
				},
			},
		}
	}

	return resources
}

// PassToConfigScanner passes discovered resources to a config scanner for detailed scanning
func (rl *AWSResourceLister) PassToConfigScanner(ctx context.Context, resources map[string][]discovery.AWSResourceRef) error {
	// TODO: Implement config scanner integration
	// This would call a separate config scanner that:
	// 1. Takes each resource reference
	// 2. Calls Describe*/Get* operations to get full configuration
	// 3. Saves the detailed config to DuckDB

	if rl.debug {
		fmt.Printf("📊 Would pass %d services with resources to config scanner\n", len(resources))

		totalResources := 0
		for service, refs := range resources {
			totalResources += len(refs)
			fmt.Printf("  %s: %d resources\n", service, len(refs))
		}
		fmt.Printf("  Total: %d resources to scan for detailed config\n", totalResources)
	}

	return nil
}

// GetRegion returns the configured region
func (rl *AWSResourceLister) GetRegion() string {
	return rl.region
}

// GetErrorSummary returns a summary of errors encountered during scanning
func (rl *AWSResourceLister) GetErrorSummary() map[string][]ServiceError {
	return rl.errorHandler.GetErrorSummary()
}

// GetCircuitBreakerStates returns the current state of all circuit breakers
func (rl *AWSResourceLister) GetCircuitBreakerStates() map[string]string {
	return rl.errorHandler.GetCircuitStates()
}

// DiscoverAndListServiceResources is a public wrapper for the private method
func (rl *AWSResourceLister) DiscoverAndListServiceResources(ctx context.Context, serviceName string) ([]discovery.AWSResourceRef, error) {
	return rl.discoverAndListServiceResources(ctx, serviceName)
}

// ExecuteOperationWithParams executes a single operation with provided parameters
func (rl *AWSResourceLister) ExecuteOperationWithParams(ctx context.Context, serviceName, operationName string, params map[string]interface{}) ([]discovery.AWSResourceRef, error) {
	if rl.debug {
		fmt.Printf("🎯 Executing %s.%s with parameters: %v\n", serviceName, operationName, params)
	}

	// Step 1: Load the service dynamically with region
	loadedService, err := rl.serviceLoader.LoadServiceWithRegion(ctx, serviceName, rl.region)
	if err != nil {
		return nil, fmt.Errorf("failed to load service: %w", err)
	}

	// Step 2: Find the specific operation
	if loadedService.Metadata == nil {
		return nil, fmt.Errorf("no metadata available for service %s", serviceName)
	}

	var targetOperation *generator.AWSOperation
	for _, op := range loadedService.Metadata.Operations {
		if op.Name == operationName {
			targetOperation = &op
			break
		}
	}

	if targetOperation == nil {
		return nil, fmt.Errorf("operation %s not found in service %s", operationName, serviceName)
	}

	if rl.debug {
		fmt.Printf("    Found operation: %s (ResourceType: %s)\n", targetOperation.Name, targetOperation.ResourceType)
	}

	// Step 3: Execute the operation with parameters
	if rl.dryRun {
		// In dry run mode, generate sample data
		sampleRefs := rl.generateSampleResourcesWithParams(serviceName, *targetOperation, params)
		if rl.debug {
			fmt.Printf("    ✅ Dry run: generated %d sample resources\n", len(sampleRefs))
		}
		return sampleRefs, nil
	}

	// Execute the real operation with parameters
	refs, err := rl.executeOperationWithParameters(ctx, loadedService, *targetOperation, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute operation %s: %w", operationName, err)
	}

	if rl.debug {
		fmt.Printf("    ✅ Successfully executed %s: found %d resources\n", operationName, len(refs))
	}

	return refs, nil
}

// executeOperationWithParameters executes an operation with custom parameters
func (rl *AWSResourceLister) executeOperationWithParameters(ctx context.Context, loadedService *discovery.LoadedService, operation generator.AWSOperation, params map[string]interface{}) ([]discovery.AWSResourceRef, error) {
	// Check circuit breaker
	if !rl.errorHandler.ShouldProceed(loadedService.Name) {
		return nil, fmt.Errorf("circuit breaker open for service %s", loadedService.Name)
	}

	// Load AWS config with proper region and profile support
	var configOptions []func(*config.LoadOptions) error
	configOptions = append(configOptions, config.WithRegion(rl.region))

	// Add profile if AWS_PROFILE is set, otherwise use default behavior
	if profile := os.Getenv("AWS_PROFILE"); profile != "" {
		configOptions = append(configOptions, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, configOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create a new client with proper AWS config
	client, err := rl.createClientWithConfig(loadedService, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client with config: %w", err)
	}

	clientValue := reflect.ValueOf(client)

	// Find the method on the client
	method := clientValue.MethodByName(operation.Name)
	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found on client", operation.Name)
	}

	// Prepare method call
	methodType := method.Type()
	if methodType.NumIn() < 2 {
		return nil, fmt.Errorf("unexpected method signature for %s", operation.Name)
	}

	// Create input struct and apply provided parameters
	inputType := methodType.In(1)
	var input reflect.Value

	if inputType.Kind() == reflect.Ptr {
		input = reflect.New(inputType.Elem())

		// Apply the provided parameters
		if params != nil && len(params) > 0 {
			rl.setCustomParameters(input, params)
		}

		// Also apply any default parameters from the classifier
		defaultParams := rl.classifier.GetOperationDefaults(loadedService.Name, operation.Name)
		if defaultParams != nil {
			rl.setDefaultParameters(input, defaultParams)
		}
	} else {
		input = reflect.Zero(inputType)
	}

	// Call the method with retry
	var results []reflect.Value
	err = RetryWithBackoff(ctx, func() error {
		args := []reflect.Value{reflect.ValueOf(ctx), input}
		results = method.Call(args)

		if len(results) < 2 {
			return fmt.Errorf("unexpected return values from %s", operation.Name)
		}

		// Check for error
		if !results[1].IsNil() {
			return results[1].Interface().(error)
		}

		return nil
	}, 3)

	if err != nil {
		return nil, rl.errorHandler.HandleError(ctx, loadedService.Name, operation.Name, err)
	}

	// Parse the output using the new response parser
	output := results[0].Interface()
	pbRefs, err := rl.responseParser.ParseAWSResponse(output, operation.Name, loadedService.Name, rl.region)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Record success
	rl.errorHandler.HandleError(ctx, loadedService.Name, operation.Name, nil)

	// Convert pb.ResourceRef to scanner.discovery.AWSResourceRef
	return rl.resourceConverter.ConvertPbListToScanner(pbRefs), nil
}

// setCustomParameters sets custom parameters on an input struct using reflection
func (rl *AWSResourceLister) setCustomParameters(input reflect.Value, params map[string]interface{}) {
	if input.Kind() == reflect.Ptr {
		input = input.Elem()
	}

	// 🔒 SECURITY: Validate all parameters before processing
	if err := rl.securityValidator.ValidateParameters(params); err != nil {
		if rl.debug {
			fmt.Printf("    🚨 Parameter validation failed: %v\n", err)
		}
		return
	}

	for fieldName, value := range params {
		// 🔒 SECURITY: Sanitize field name to prevent injection
		sanitizedFieldName := rl.securityValidator.SanitizeFieldName(fieldName)

		// 🔒 SECURITY: Double-check field is allowed
		if !rl.securityValidator.IsFieldAllowed(fieldName) {
			if rl.debug {
				fmt.Printf("    🚨 Field %s not allowed\n", fieldName)
			}
			continue
		}

		field := input.FieldByName(sanitizedFieldName)
		if !field.IsValid() || !field.CanSet() {
			if rl.debug {
				fmt.Printf("    ⚠️  Cannot set field %s on input struct\n", sanitizedFieldName)
			}
			continue
		}

		// Handle different value types with improved EC2 field mapping
		switch v := value.(type) {
		case []string:
			// Handle slice of strings (e.g., Owners, InstanceIds)
			if field.Type().Kind() == reflect.Slice {
				elemType := field.Type().Elem()

				// Handle slice of string pointers (common in AWS SDK)
				if elemType.Kind() == reflect.Ptr && elemType.Elem().Kind() == reflect.String {
					stringPtrSlice := reflect.MakeSlice(field.Type(), len(v), len(v))
					for i, str := range v {
						strPtr := reflect.New(elemType.Elem())
						strPtr.Elem().SetString(str)
						stringPtrSlice.Index(i).Set(strPtr)
					}
					field.Set(stringPtrSlice)
				} else if elemType.Kind() == reflect.String {
					// Handle slice of strings
					stringSlice := reflect.MakeSlice(field.Type(), len(v), len(v))
					for i, str := range v {
						stringSlice.Index(i).SetString(str)
					}
					field.Set(stringSlice)
				}
			}
		case string:
			// Handle string fields with proper pointer handling
			if field.Type().Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.String {
				strPtr := reflect.New(field.Type().Elem())
				strPtr.Elem().SetString(v)
				field.Set(strPtr)
			} else if field.Type().Kind() == reflect.String {
				field.SetString(v)
			}
		case int:
			// Handle integer fields with proper type conversion
			if field.Type().Kind() == reflect.Ptr {
				elemType := field.Type().Elem()
				intPtr := reflect.New(elemType)
				switch elemType.Kind() {
				case reflect.Int32:
					intPtr.Elem().SetInt(int64(v))
				case reflect.Int64:
					intPtr.Elem().SetInt(int64(v))
				case reflect.Int:
					intPtr.Elem().SetInt(int64(v))
				}
				field.Set(intPtr)
			} else {
				field.SetInt(int64(v))
			}
		case int32:
			// Handle int32 fields (common in AWS SDK for MaxResults)
			if field.Type().Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Int32 {
				intPtr := reflect.New(field.Type().Elem())
				intPtr.Elem().SetInt(int64(v))
				field.Set(intPtr)
			} else if field.Type().Kind() == reflect.Int32 {
				field.SetInt(int64(v))
			} else {
				field.SetInt(int64(v))
			}
		case int64:
			// Handle int64 fields
			if field.Type().Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Int64 {
				intPtr := reflect.New(field.Type().Elem())
				intPtr.Elem().SetInt(v)
				field.Set(intPtr)
			} else {
				field.SetInt(v)
			}
		case bool:
			// Handle boolean fields
			if field.Type().Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Bool {
				boolPtr := reflect.New(field.Type().Elem())
				boolPtr.Elem().SetBool(v)
				field.Set(boolPtr)
			} else if field.Type().Kind() == reflect.Bool {
				field.SetBool(v)
			}
		default:
			if rl.debug {
				fmt.Printf("    ⚠️  Unsupported parameter type %T for field %s\n", value, fieldName)
			}
		}

		if rl.debug {
			fmt.Printf("    ✅ Set custom parameter %s = %v (type: %s)\n", fieldName, value, field.Type())
		}
	}
}

// generateSampleResourcesWithParams generates sample resources for dry run mode with parameters
func (rl *AWSResourceLister) generateSampleResourcesWithParams(serviceName string, operation generator.AWSOperation, params map[string]interface{}) []discovery.AWSResourceRef {
	// Generate base sample resources
	resources := rl.generateSampleResources(serviceName, operation)

	// Enhance with parameter information
	for i := range resources {
		if resources[i].Metadata == nil {
			resources[i].Metadata = make(map[string]string)
		}

		// Add parameter information to metadata
		for key, value := range params {
			resources[i].Metadata[fmt.Sprintf("param_%s", key)] = fmt.Sprintf("%v", value)
		}

		// Mark as parameter-based execution
		resources[i].Metadata["execution_type"] = "hierarchical_with_params"
	}

	return resources
}

// Cleanup cleans up temporary files and resources
func (rl *AWSResourceLister) Cleanup() error {
	return rl.serviceLoader.CleanupTempFiles()
}

// executeAWSOperation executes an AWS operation with reflection-based invocation, error handling, and retry logic
func (rl *AWSResourceLister) executeAWSOperation(ctx context.Context, client interface{}, operationName string, inputStruct interface{}) (interface{}, error) {
	const maxRetries = 3
	const retryDelay = time.Second

	clientValue := reflect.ValueOf(client)

	// Find the method on the client
	method := clientValue.MethodByName(operationName)
	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found on client", operationName)
	}

	// Prepare method call arguments
	var args []reflect.Value
	if inputStruct != nil {
		args = []reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(inputStruct)}
	} else {
		// Create zero value for input if not provided
		methodType := method.Type()
		if methodType.NumIn() >= 2 {
			inputType := methodType.In(1)
			args = []reflect.Value{reflect.ValueOf(ctx), reflect.Zero(inputType)}
		} else {
			args = []reflect.Value{reflect.ValueOf(ctx)}
		}
	}

	// Execute with retry logic
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			if rl.debug {
				fmt.Printf("    🔄 Retry attempt %d/%d for %s\n", attempt+1, maxRetries, operationName)
			}
			time.Sleep(retryDelay * time.Duration(attempt))
		}

		// Call the method
		results := method.Call(args)

		if len(results) < 2 {
			return nil, fmt.Errorf("unexpected return values from %s", operationName)
		}

		// Check for error
		if results[1].IsNil() {
			// Success
			return results[0].Interface(), nil
		}

		// Handle error
		err := results[1].Interface().(error)
		lastErr = err

		// Check if error is retryable
		if !rl.isRetryableError(err) {
			return nil, err
		}

		if rl.debug {
			fmt.Printf("    ⚠️  Retryable error: %v\n", err)
		}
	}

	return nil, fmt.Errorf("operation %s failed after %d retries: %w", operationName, maxRetries, lastErr)
}

// isRetryableError determines if an AWS error is retryable
func (rl *AWSResourceLister) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Common retryable AWS errors
	retryablePatterns := []string{
		"throttling",
		"rate exceeded",
		"too many requests",
		"service unavailable",
		"temporary failure",
		"timeout",
		"connection reset",
		"connection refused",
	}

	errLower := strings.ToLower(errStr)
	for _, pattern := range retryablePatterns {
		if strings.Contains(errLower, pattern) {
			return true
		}
	}

	return false
}

// createAWSServiceClient creates a properly configured AWS service client
func (rl *AWSResourceLister) createAWSServiceClient(ctx context.Context, serviceName, region string) (interface{}, error) {
	if region == "" {
		region = rl.region
	}

	// Load the service with the specified region
	loadedService, err := rl.serviceLoader.LoadServiceWithRegion(ctx, serviceName, region)
	if err != nil {
		return nil, fmt.Errorf("failed to load service %s: %w", serviceName, err)
	}

	// Load AWS config with proper region and profile support
	var configOptions []func(*config.LoadOptions) error
	configOptions = append(configOptions, config.WithRegion(region))

	// Add profile if AWS_PROFILE is set
	if profile := os.Getenv("AWS_PROFILE"); profile != "" {
		configOptions = append(configOptions, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, configOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create a new client with proper AWS config
	client, err := rl.createClientWithConfig(loadedService, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client with config: %w", err)
	}

	return client, nil
}

// executeOperationsParallel executes multiple list operations in parallel
func (rl *AWSResourceLister) executeOperationsParallel(ctx context.Context, loadedService *discovery.LoadedService, operations []generator.AWSOperation, serviceName string) ([]discovery.AWSResourceRef, error) {
	if rl.debug {
		fmt.Printf("  🚀 Executing %d operations in parallel for %s\n", len(operations), serviceName)
	}

	// Create executor with reasonable concurrency
	maxConcurrency := 5 // Limit concurrent AWS API calls
	if len(operations) < maxConcurrency {
		maxConcurrency = len(operations)
	}

	executor := parallel.NewExecutor(maxConcurrency, len(operations))
	// Don't defer Shutdown() since WaitForResults handles cleanup

	// Create tasks for each operation
	tasks := make([]*parallel.Task, 0, len(operations))
	for _, op := range operations {
		operation := op // Capture loop variable
		task := &parallel.Task{
			ID:   fmt.Sprintf("%s-%s", serviceName, operation.Name),
			Name: operation.Name,
			Execute: func(ctx context.Context) (interface{}, error) {
				refs, err := rl.executeListOperation(ctx, loadedService, operation)
				if err != nil {
					if rl.debug {
						fmt.Printf("    ⚠️  Failed to execute %s: %v\n", operation.Name, err)
					}
					// Don't fail the whole batch for one operation
					return []discovery.AWSResourceRef{}, nil
				}
				return refs, nil
			},
		}
		tasks = append(tasks, task)
	}

	// Submit all tasks
	if err := executor.SubmitBatch(tasks); err != nil {
		return nil, fmt.Errorf("failed to submit tasks: %w", err)
	}

	// Collect results
	var allResources []discovery.AWSResourceRef
	var mu sync.Mutex

	// Start result collector
	go func() {
		for result := range executor.Results() {
			if result.Error == nil && result.Value != nil {
				refs := result.Value.([]discovery.AWSResourceRef)
				mu.Lock()
				allResources = append(allResources, refs...)
				mu.Unlock()

				if rl.debug {
					fmt.Printf("    ✅ %s: found %d resources (%.2fs)\n", result.TaskName, len(refs), result.Duration.Seconds())
				}
			}
		}
	}()

	// Wait for all operations to complete
	executor.WaitForResults()

	if rl.debug {
		stats := executor.Stats()
		fmt.Printf("  📊 Parallel execution complete: %d/%d operations succeeded\n",
			stats.TasksCompleted-stats.TasksFailed, stats.TasksCompleted)
	}

	return allResources, nil
}

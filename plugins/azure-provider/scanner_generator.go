package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"text/template"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AzureScannerGenerator generates hybrid scanners that use Resource Graph with SDK fallback
type AzureScannerGenerator struct {
	subscriptionID string
	templates      map[string]*template.Template
}

// NewAzureScannerGenerator creates a new Azure scanner generator
func NewAzureScannerGenerator(subID string) *AzureScannerGenerator {
	return &AzureScannerGenerator{
		subscriptionID: subID,
		templates:      make(map[string]*template.Template),
	}
}

// ScannerTemplateData holds data for generating scanner code
type ScannerTemplateData struct {
	ServiceName         string
	ResourceType        string
	ResourceTypeLower   string
	ClientType          string
	SDKClient           string
	PackageName         string
	ResourceGraphQuery  string
	RequiresResourceGroup bool
	ListOperation       string
	PaginationType      string
	RetryAttempts       int
	Relationships       []RelationshipMapping
	BatchSize           int
	EnableTelemetry     bool
}

// RelationshipMapping defines how to extract relationships from resources
type RelationshipMapping struct {
	Type           string
	TargetProperty string
	RelationType   string
}

// ResourceGraphClientInterface interface for testing (renamed to avoid conflict)
type ResourceGraphClientInterface interface {
	ExecuteQuery(ctx context.Context, query string) ([]map[string]interface{}, error)
}

// AzureClientFactoryInterface interface for testing (renamed to avoid conflict)  
type AzureClientFactoryInterface interface {
	GetClient(resourceType string) (interface{}, error)
	NewResourceGroupsClient() interface{}
}

// GenerateHybridScanner generates a hybrid scanner for the specified resource type
func (g *AzureScannerGenerator) GenerateHybridScanner(resourceType *pb.ResourceType, serviceName string) (string, error) {
	data := g.buildTemplateData(resourceType, serviceName)
	
	scannerTemplate := g.getHybridScannerTemplate()
	if scannerTemplate == nil {
		return "", fmt.Errorf("failed to get scanner template")
	}
	
	var output strings.Builder
	err := scannerTemplate.Execute(&output, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute scanner template: %w", err)
	}
	
	return output.String(), nil
}

// buildTemplateData builds template data from resource type information
func (g *AzureScannerGenerator) buildTemplateData(resourceType *pb.ResourceType, serviceName string) *ScannerTemplateData {
	data := &ScannerTemplateData{
		ServiceName:         g.formatServiceName(serviceName),
		ResourceType:        resourceType.Name,
		ResourceTypeLower:   strings.ToLower(resourceType.Name),
		ClientType:          g.getClientType(resourceType.TypeName),
		SDKClient:           g.getSDKClientName(resourceType.TypeName),
		PackageName:         g.getPackageName(resourceType.TypeName),
		ResourceGraphQuery:  g.buildResourceGraphQuery(resourceType.TypeName),
		RequiresResourceGroup: g.requiresResourceGroup(resourceType.TypeName),
		ListOperation:       resourceType.ListOperation,
		PaginationType:      "standard",
		RetryAttempts:       3,
		Relationships:       g.buildRelationshipMappings(resourceType.TypeName),
		BatchSize:           100,
		EnableTelemetry:     true,
	}
	
	return data
}

// getHybridScannerTemplate returns the main hybrid scanner template
func (g *AzureScannerGenerator) getHybridScannerTemplate() *template.Template {
	templateStr := `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	{{.PackageName}}
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// {{.ServiceName}}Scanner implements hybrid Resource Graph + SDK scanning
type {{.ServiceName}}Scanner struct {
	resourceGraph *ResourceGraphClient
	clientFactory *AzureClientFactory
	{{.SDKClient}} *{{.ClientType}}
	subscriptionID string
	
	// Performance settings
	batchSize     int
	retryAttempts int
	enableTelemetry bool
}

// New{{.ServiceName}}Scanner creates a new {{.ServiceName}} scanner
func New{{.ServiceName}}Scanner(resourceGraph *ResourceGraphClient, clientFactory *AzureClientFactory, subID string) *{{.ServiceName}}Scanner {
	return &{{.ServiceName}}Scanner{
		resourceGraph:   resourceGraph,
		clientFactory:   clientFactory,
		subscriptionID:  subID,
		batchSize:       {{.BatchSize}},
		retryAttempts:   {{.RetryAttempts}},
		enableTelemetry: {{.EnableTelemetry}},
	}
}

// Scan{{.ResourceType}} scans {{.ResourceType}} resources using hybrid approach
func (s *{{.ServiceName}}Scanner) Scan{{.ResourceType}}(ctx context.Context, filters map[string]string) ([]*pb.Resource, error) {
	startTime := time.Now()
	var scanMethod string
	
	defer func() {
		if s.enableTelemetry {
			s.recordScanTelemetry("{{.ResourceType}}", scanMethod, time.Since(startTime))
		}
	}()
	
	// Try Resource Graph first
	if s.resourceGraph != nil {
		scanMethod = "resource_graph"
		resources, err := s.scanViaResourceGraph(ctx, filters)
		if err == nil {
			log.Printf("Resource Graph scan successful for {{.ResourceType}}: %d resources", len(resources))
			return resources, nil
		}
		log.Printf("Resource Graph failed for {{.ResourceType}}: %v, falling back to SDK", err)
	}
	
	// Fall back to SDK
	scanMethod = "sdk"
	return s.scanViaSDK(ctx, filters)
}

// scanViaResourceGraph implements Resource Graph scanning
func (s *{{.ServiceName}}Scanner) scanViaResourceGraph(ctx context.Context, filters map[string]string) ([]*pb.Resource, error) {
	query := ` + "`" + `{{.ResourceGraphQuery}}` + "`" + `
	
	// Apply filters to query
	if len(filters) > 0 {
		whereClause := s.buildWhereClause(filters)
		if whereClause != "" {
			query += " | where " + whereClause
		}
	}
	
	// Add limit for performance
	query += fmt.Sprintf(" | limit %d", s.batchSize*10)
	
	results, err := s.resourceGraph.ExecuteQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("resource graph query failed: %w", err)
	}
	
	return s.convertGraphResults(results), nil
}

// scanViaSDK implements SDK fallback scanning with pagination and batching
func (s *{{.ServiceName}}Scanner) scanViaSDK(ctx context.Context, filters map[string]string) ([]*pb.Resource, error) {
	var resources []*pb.Resource
	
	// Initialize SDK client if needed
	if s.{{.SDKClient}} == nil {
		client, err := s.clientFactory.GetClient("{{.ResourceType}}")
		if err != nil {
			return nil, fmt.Errorf("failed to create SDK client: %w", err)
		}
		s.{{.SDKClient}} = client.(*{{.ClientType}})
	}
	
	{{if .RequiresResourceGroup}}
	// List resource groups first for resource group scoped resources
	rgClient := s.clientFactory.NewResourceGroupsClient()
	rgPager := rgClient.NewListPager(nil)
	
	for rgPager.More() {
		page, err := rgPager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list resource groups: %w", err)
		}
		
		for _, rg := range page.Value {
			if rg.Name == nil {
				continue
			}
			
			// Apply resource group filter if specified
			if rgFilter, ok := filters["resource_group"]; ok && rgFilter != *rg.Name {
				continue
			}
			
			rgResources, err := s.scan{{.ResourceType}}InRG(ctx, *rg.Name, filters)
			if err != nil {
				log.Printf("Failed to scan {{.ResourceType}} in resource group %s: %v", *rg.Name, err)
				continue
			}
			resources = append(resources, rgResources...)
		}
	}
	{{else}}
	// Direct list operation for subscription-scoped resources
	resources, err := s.scanSubscriptionLevel(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to scan subscription level: %w", err)
	}
	{{end}}
	
	return resources, nil
}

{{if .RequiresResourceGroup}}
// scan{{.ResourceType}}InRG scans resources within a specific resource group
func (s *{{.ServiceName}}Scanner) scan{{.ResourceType}}InRG(ctx context.Context, resourceGroup string, filters map[string]string) ([]*pb.Resource, error) {
	var resources []*pb.Resource
	
	pager := s.{{.SDKClient}}.NewListByResourceGroupPager(resourceGroup, nil)
	
	for pager.More() {
		page, err := s.retryOperation(func() (interface{}, error) {
			return pager.NextPage(ctx)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get page: %w", err)
		}
		
		pageResult := page.(interface{}) // Type assertion would be specific to the actual response type
		pageResources := s.convertSDKResults(pageResult, resourceGroup)
		
		// Apply filters
		filteredResources := s.applyFilters(pageResources, filters)
		resources = append(resources, filteredResources...)
	}
	
	return resources, nil
}
{{else}}
// scanSubscriptionLevel scans resources at subscription level
func (s *{{.ServiceName}}Scanner) scanSubscriptionLevel(ctx context.Context, filters map[string]string) ([]*pb.Resource, error) {
	var resources []*pb.Resource
	
	pager := s.{{.SDKClient}}.NewListPager(nil)
	
	for pager.More() {
		page, err := s.retryOperation(func() (interface{}, error) {
			return pager.NextPage(ctx)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get page: %w", err)
		}
		
		pageResult := page.(interface{}) // Type assertion would be specific to the actual response type
		pageResources := s.convertSDKResults(pageResult, "")
		
		// Apply filters
		filteredResources := s.applyFilters(pageResources, filters)
		resources = append(resources, filteredResources...)
	}
	
	return resources, nil
}
{{end}}

// convertGraphResults converts Resource Graph results to pb.Resource
func (s *{{.ServiceName}}Scanner) convertGraphResults(results []map[string]interface{}) []*pb.Resource {
	resources := make([]*pb.Resource, 0, len(results))
	
	for _, result := range results {
		resource := &pb.Resource{
			Provider:     "azure",
			Service:      "{{.ServiceName | lower}}",
			Type:         "{{.ResourceType}}",
			DiscoveredAt: timestamppb.Now(),
			Tags:         make(map[string]string),
		}
		
		// Extract standard fields
		if id, ok := result["id"].(string); ok {
			resource.Id = id
		}
		if name, ok := result["name"].(string); ok {
			resource.Name = name
		}
		if location, ok := result["location"].(string); ok {
			resource.Region = location
		}
		if resourceGroup, ok := result["resourceGroup"].(string); ok {
			resource.ParentId = resourceGroup
		}
		
		// Extract subscription ID
		resource.AccountId = s.subscriptionID
		
		// Extract tags
		if tags, ok := result["tags"].(map[string]interface{}); ok {
			for k, v := range tags {
				if strVal, ok := v.(string); ok {
					resource.Tags[k] = strVal
				}
			}
		}
		
		// Store properties as raw data
		if properties, ok := result["properties"]; ok {
			if propsJSON, err := json.Marshal(properties); err == nil {
				resource.RawData = string(propsJSON)
			}
		}
		
		// Extract relationships
		relationships := s.extractRelationships(resource, result)
		resource.Relationships = append(resource.Relationships, relationships...)
		
		resources = append(resources, resource)
	}
	
	return resources
}

// convertSDKResults converts SDK API results to pb.Resource
func (s *{{.ServiceName}}Scanner) convertSDKResults(pageResult interface{}, resourceGroup string) []*pb.Resource {
	// This would be implemented with specific type assertions based on the actual SDK response type
	// For now, return empty slice as a placeholder
	return []*pb.Resource{}
}

// buildWhereClause builds KQL where clause from filters
func (s *{{.ServiceName}}Scanner) buildWhereClause(filters map[string]string) string {
	var conditions []string
	
	for key, value := range filters {
		switch key {
		case "resource_group":
			conditions = append(conditions, fmt.Sprintf("resourceGroup =~ '%s'", value))
		case "location", "region":
			conditions = append(conditions, fmt.Sprintf("location =~ '%s'", value))
		case "tag":
			// Parse tag filter format: tag:key=value
			if strings.Contains(value, "=") {
				parts := strings.SplitN(value, "=", 2)
				conditions = append(conditions, fmt.Sprintf("tags['%s'] =~ '%s'", parts[0], parts[1]))
			}
		default:
			// Custom property filters
			conditions = append(conditions, fmt.Sprintf("properties.%s =~ '%s'", key, value))
		}
	}
	
	return strings.Join(conditions, " and ")
}

// applyFilters applies filters to resources (for SDK results)
func (s *{{.ServiceName}}Scanner) applyFilters(resources []*pb.Resource, filters map[string]string) []*pb.Resource {
	if len(filters) == 0 {
		return resources
	}
	
	filtered := make([]*pb.Resource, 0, len(resources))
	
	for _, resource := range resources {
		if s.matchesFilters(resource, filters) {
			filtered = append(filtered, resource)
		}
	}
	
	return filtered
}

// matchesFilters checks if a resource matches the given filters
func (s *{{.ServiceName}}Scanner) matchesFilters(resource *pb.Resource, filters map[string]string) bool {
	for key, value := range filters {
		switch key {
		case "resource_group":
			if resource.ParentId != value {
				return false
			}
		case "location", "region":
			if resource.Region != value {
				return false
			}
		case "tag":
			// Parse tag filter format: tag:key=value
			if strings.Contains(value, "=") {
				parts := strings.SplitN(value, "=", 2)
				if tagValue, exists := resource.Tags[parts[0]]; !exists || tagValue != parts[1] {
					return false
				}
			}
		}
	}
	return true
}

// extractRelationships extracts relationships from resource data
func (s *{{.ServiceName}}Scanner) extractRelationships(resource *pb.Resource, data map[string]interface{}) []*pb.Relationship {
	var relationships []*pb.Relationship
	
	{{range .Relationships}}
	// Extract {{.Type}} relationships
	if {{.TargetProperty}}, ok := data["{{.TargetProperty}}"].(string); ok && {{.TargetProperty}} != "" {
		relationships = append(relationships, &pb.Relationship{
			TargetId:         {{.TargetProperty}},
			TargetType:       "{{.Type}}",
			RelationshipType: "{{.RelationType}}",
		})
	}
	{{end}}
	
	// Extract parent-child relationship
	if resource.ParentId != "" {
		relationships = append(relationships, &pb.Relationship{
			TargetId:         resource.ParentId,
			TargetType:       "resourceGroup",
			RelationshipType: "child_of",
		})
	}
	
	return relationships
}

// retryOperation implements retry logic with exponential backoff
func (s *{{.ServiceName}}Scanner) retryOperation(operation func() (interface{}, error)) (interface{}, error) {
	var lastErr error
	
	for attempt := 0; attempt < s.retryAttempts; attempt++ {
		result, err := operation()
		if err == nil {
			return result, nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !s.isRetryableError(err) {
			return nil, err
		}
		
		// Exponential backoff
		if attempt < s.retryAttempts-1 {
			delay := time.Duration(1<<uint(attempt)) * time.Second
			log.Printf("Operation failed (attempt %d/%d): %v, retrying in %v", attempt+1, s.retryAttempts, err, delay)
			time.Sleep(delay)
		}
	}
	
	return nil, fmt.Errorf("operation failed after %d attempts: %w", s.retryAttempts, lastErr)
}

// isRetryableError determines if an error is retryable
func (s *{{.ServiceName}}Scanner) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	
	errStr := strings.ToLower(err.Error())
	
	// Retryable conditions
	retryableErrors := []string{
		"rate limit",
		"throttled",
		"timeout",
		"temporary failure",
		"service unavailable",
		"internal server error",
		"bad gateway",
		"gateway timeout",
	}
	
	for _, retryable := range retryableErrors {
		if strings.Contains(errStr, retryable) {
			return true
		}
	}
	
	return false
}

// recordScanTelemetry records telemetry data for scan operations
func (s *{{.ServiceName}}Scanner) recordScanTelemetry(resourceType, method string, duration time.Duration) {
	if !s.enableTelemetry {
		return
	}
	
	log.Printf("TELEMETRY: resource_type=%s method=%s duration=%v subscription=%s", 
		resourceType, method, duration, s.subscriptionID)
}

// BatchScan{{.ResourceType}} performs batch scanning with optimized Resource Graph queries
func (s *{{.ServiceName}}Scanner) BatchScan{{.ResourceType}}(ctx context.Context, batchFilters []map[string]string) ([]*pb.Resource, error) {
	if s.resourceGraph != nil {
		return s.batchScanViaResourceGraph(ctx, batchFilters)
	}
	
	// Fall back to sequential SDK scanning
	var allResources []*pb.Resource
	for _, filters := range batchFilters {
		resources, err := s.scanViaSDK(ctx, filters)
		if err != nil {
			log.Printf("Batch SDK scan failed for filters %v: %v", filters, err)
			continue
		}
		allResources = append(allResources, resources...)
	}
	
	return allResources, nil
}

// batchScanViaResourceGraph optimizes multiple scans using Resource Graph
func (s *{{.ServiceName}}Scanner) batchScanViaResourceGraph(ctx context.Context, batchFilters []map[string]string) ([]*pb.Resource, error) {
	// Build unified query for all batch filters
	baseQuery := ` + "`" + `{{.ResourceGraphQuery}}` + "`" + `
	
	var unionQueries []string
	for _, filters := range batchFilters {
		whereClause := s.buildWhereClause(filters)
		if whereClause != "" {
			subQuery := baseQuery + " | where " + whereClause
		} else {
			subQuery := baseQuery
		}
		unionQueries = append(unionQueries, fmt.Sprintf("(%s)", subQuery))
	}
	
	// Combine with union
	finalQuery := strings.Join(unionQueries, " | union ") + fmt.Sprintf(" | limit %d", s.batchSize*10)
	
	results, err := s.resourceGraph.ExecuteQuery(ctx, finalQuery)
	if err != nil {
		return nil, fmt.Errorf("batch resource graph query failed: %w", err)
	}
	
	return s.convertGraphResults(results), nil
}

// GetScannerMetrics returns performance metrics for the scanner
func (s *{{.ServiceName}}Scanner) GetScannerMetrics() map[string]interface{} {
	return map[string]interface{}{
		"service":          "{{.ServiceName | lower}}",
		"resource_type":    "{{.ResourceType}}",
		"batch_size":       s.batchSize,
		"retry_attempts":   s.retryAttempts,
		"telemetry_enabled": s.enableTelemetry,
		"has_resource_graph": s.resourceGraph != nil,
		"has_sdk_client":   s.{{.SDKClient}} != nil,
	}
}
`

	tmpl, err := template.New("hybridScanner").Funcs(template.FuncMap{
		"lower": strings.ToLower,
		"title": strings.Title,
		"first": func(slice interface{}) interface{} {
			// Simple helper to get first element
			return slice
		},
	}).Parse(templateStr)
	if err != nil {
		log.Printf("Template parsing error: %v", err)
		return nil
	}
	return tmpl
}

// Helper methods

func (g *AzureScannerGenerator) formatServiceName(serviceName string) string {
	// Convert "storage" to "Storage"
	return strings.Title(strings.ToLower(serviceName))
}

func (g *AzureScannerGenerator) getClientType(resourceType string) string {
	// Extract client type from resource type
	// Microsoft.Storage/storageAccounts -> AccountsClient
	parts := strings.Split(resourceType, "/")
	if len(parts) >= 2 {
		service := strings.TrimPrefix(parts[0], "Microsoft.")
		resource := parts[1]
		
		// Convert plural to singular and add Client suffix
		if strings.HasSuffix(resource, "s") {
			resource = strings.TrimSuffix(resource, "s")
		}
		
		return fmt.Sprintf("arm%s.%sClient", strings.ToLower(service), strings.Title(resource))
	}
	
	return "armresources.Client"
}

func (g *AzureScannerGenerator) getSDKClientName(resourceType string) string {
	// Generate field name for the SDK client
	parts := strings.Split(resourceType, "/")
	if len(parts) >= 2 {
		resource := parts[1]
		if strings.HasSuffix(resource, "s") {
			resource = strings.TrimSuffix(resource, "s")
		}
		return fmt.Sprintf("%sClient", strings.ToLower(resource))
	}
	return "resourcesClient"
}

func (g *AzureScannerGenerator) getPackageName(resourceType string) string {
	// Generate import package name
	parts := strings.Split(resourceType, "/")
	if len(parts) >= 1 {
		service := strings.ToLower(strings.TrimPrefix(parts[0], "Microsoft."))
		return fmt.Sprintf(`"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/%s/arm%s"`, service, service)
	}
	return `"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"`
}

func (g *AzureScannerGenerator) buildResourceGraphQuery(resourceType string) string {
	// Build KQL query for Resource Graph
	return fmt.Sprintf("Resources | where type =~ '%s'", strings.ToLower(resourceType))
}

func (g *AzureScannerGenerator) requiresResourceGroup(resourceType string) bool {
	// Determine if resource type requires resource group enumeration
	resourceGroupScoped := []string{
		"Microsoft.Compute/virtualMachines",
		"Microsoft.Storage/storageAccounts", 
		"Microsoft.Network/virtualNetworks",
		"Microsoft.Network/networkInterfaces",
		"Microsoft.KeyVault/vaults",
		"Microsoft.Web/sites",
		"Microsoft.Sql/servers",
	}
	
	for _, rgType := range resourceGroupScoped {
		if strings.EqualFold(resourceType, rgType) {
			return true
		}
	}
	
	return false
}

func (g *AzureScannerGenerator) buildRelationshipMappings(resourceType string) []RelationshipMapping {
	// Define relationship mappings based on resource type
	switch strings.ToLower(resourceType) {
	case "microsoft.compute/virtualmachines":
		return []RelationshipMapping{
			{Type: "Microsoft.Network/networkInterfaces", TargetProperty: "networkProfile.networkInterfaces[0].id", RelationType: "uses"},
			{Type: "Microsoft.Compute/availabilitySets", TargetProperty: "availabilitySet.id", RelationType: "member_of"},
			{Type: "Microsoft.Compute/disks", TargetProperty: "storageProfile.osDisk.managedDisk.id", RelationType: "uses"},
		}
	case "microsoft.storage/storageaccounts":
		return []RelationshipMapping{
			{Type: "Microsoft.Network/privateEndpoints", TargetProperty: "privateEndpointConnections[0].privateEndpoint.id", RelationType: "connected_to"},
		}
	case "microsoft.network/virtualnetworks":
		return []RelationshipMapping{
			{Type: "Microsoft.Network/virtualNetworks/subnets", TargetProperty: "subnets[0].id", RelationType: "contains"},
		}
	default:
		return []RelationshipMapping{}
	}
}

// GenerateServiceScanners generates scanners for all resource types in a service
func (g *AzureScannerGenerator) GenerateServiceScanners(ctx context.Context, serviceInfo *pb.ServiceInfo) ([]*pb.GeneratedScanner, error) {
	var scanners []*pb.GeneratedScanner
	
	for _, resourceType := range serviceInfo.ResourceTypes {
		_, err := g.GenerateHybridScanner(resourceType, serviceInfo.Name)
		if err != nil {
			log.Printf("Failed to generate scanner for %s: %v", resourceType.Name, err)
			continue
		}
		
		scanner := &pb.GeneratedScanner{
			Service:       serviceInfo.Name,
			FilePath:      fmt.Sprintf("%s_scanner.go", strings.ToLower(serviceInfo.Name)),
			ResourceTypes: []string{resourceType.TypeName},
			GeneratedAt:   timestamppb.Now(),
		}
		
		scanners = append(scanners, scanner)
	}
	
	return scanners, nil
}

// GenerateBatchScanner generates a batch scanner that can handle multiple resource types
func (g *AzureScannerGenerator) GenerateBatchScanner(services []*pb.ServiceInfo) (string, error) {
	templateStr := `package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// AzureBatchScanner handles batch scanning across multiple services
type AzureBatchScanner struct {
	resourceGraph *ResourceGraphClient
	clientFactory *AzureClientFactory
	subscriptionID string
	
	// Service-specific scanners
	{{range .Services}}
	{{.Name}}Scanner *{{.Name | title}}Scanner
	{{end}}
	
	// Configuration
	maxConcurrency int
	batchSize      int
	enableTelemetry bool
}

// NewAzureBatchScanner creates a new batch scanner
func NewAzureBatchScanner(resourceGraph *ResourceGraphClient, clientFactory *AzureClientFactory, subID string) *AzureBatchScanner {
	scanner := &AzureBatchScanner{
		resourceGraph:   resourceGraph,
		clientFactory:   clientFactory,
		subscriptionID:  subID,
		maxConcurrency:  10,
		batchSize:       100,
		enableTelemetry: true,
	}
	
	// Initialize service scanners
	{{range .Services}}
	scanner.{{.Name}}Scanner = New{{.Name | title}}Scanner(resourceGraph, clientFactory, subID)
	{{end}}
	
	return scanner
}

// BatchScanAllServices scans all configured services concurrently
func (s *AzureBatchScanner) BatchScanAllServices(ctx context.Context, filters map[string]string) ([]*pb.Resource, error) {
	startTime := time.Now()
	
	// Channel for collecting results
	resultChan := make(chan []*pb.Resource, len(s.getServices()))
	errorChan := make(chan error, len(s.getServices()))
	
	// Semaphore for controlling concurrency
	semaphore := make(chan struct{}, s.maxConcurrency)
	var wg sync.WaitGroup
	
	services := s.getServices()
	for _, service := range services {
		wg.Add(1)
		go func(svc string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release
			
			resources, err := s.scanService(ctx, svc, filters)
			if err != nil {
				errorChan <- fmt.Errorf("failed to scan service %s: %w", svc, err)
				return
			}
			
			resultChan <- resources
		}(service)
	}
	
	// Close channels when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()
	
	// Collect results
	var allResources []*pb.Resource
	var errors []error
	
	for {
		select {
		case resources, ok := <-resultChan:
			if !ok {
				resultChan = nil
			} else {
				allResources = append(allResources, resources...)
			}
		case err, ok := <-errorChan:
			if !ok {
				errorChan = nil
			} else {
				errors = append(errors, err)
			}
		}
		
		if resultChan == nil && errorChan == nil {
			break
		}
	}
	
	if s.enableTelemetry {
		log.Printf("BATCH_SCAN_TELEMETRY: services=%d resources=%d errors=%d duration=%v",
			len(services), len(allResources), len(errors), time.Since(startTime))
	}
	
	if len(errors) > 0 {
		log.Printf("Batch scan completed with %d errors: %v", len(errors), errors)
	}
	
	return allResources, nil
}

// scanService scans a specific service using the appropriate scanner
func (s *AzureBatchScanner) scanService(ctx context.Context, service string, filters map[string]string) ([]*pb.Resource, error) {
	switch service {
	{{range .Services}}
	case "{{.Name}}":
		return s.{{.Name}}Scanner.Scan{{(index .ResourceTypes 0).Name}}(ctx, filters)
	{{end}}
	default:
		return nil, fmt.Errorf("unsupported service: %s", service)
	}
}

// getServices returns list of supported services
func (s *AzureBatchScanner) getServices() []string {
	return []string{
		{{range .Services}}
		"{{.Name}}",
		{{end}}
	}
}

// GetBatchMetrics returns metrics for the batch scanner
func (s *AzureBatchScanner) GetBatchMetrics() map[string]interface{} {
	return map[string]interface{}{
		"supported_services": s.getServices(),
		"max_concurrency":    s.maxConcurrency,
		"batch_size":         s.batchSize,
		"telemetry_enabled":  s.enableTelemetry,
		"subscription_id":    s.subscriptionID,
	}
}
`

	data := struct {
		Services []*pb.ServiceInfo
	}{
		Services: services,
	}
	
	tmpl, err := template.New("batchScanner").Funcs(template.FuncMap{
		"lower": strings.ToLower,
		"title": strings.Title,
		"first": func(slice interface{}) interface{} {
			return slice
		},
	}).Parse(templateStr)
	if err != nil {
		return "", err
	}
	
	var output strings.Builder
	err = tmpl.Execute(&output, data)
	if err != nil {
		return "", err
	}
	
	return output.String(), nil
}
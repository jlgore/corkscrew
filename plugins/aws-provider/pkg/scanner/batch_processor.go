package scanner

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// BatchProcessor optimizes batch operations to minimize reflection calls
type BatchProcessor struct {
	reflectionCache *ReflectionCache
	maxBatchSize    int
	timeout         time.Duration
}

// BatchResult contains results from batch processing
type BatchResult struct {
	Resources []*pb.ResourceRef
	Errors    []error
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(cache *ReflectionCache) *BatchProcessor {
	return &BatchProcessor{
		reflectionCache: cache,
		maxBatchSize:    100,
		timeout:         30 * time.Second,
	}
}

// ProcessBatch processes multiple items with minimal reflection overhead
func (b *BatchProcessor) ProcessBatch(ctx context.Context, items reflect.Value, serviceName, fieldName string) *BatchResult {
	if items.Kind() != reflect.Slice || items.Len() == 0 {
		return &BatchResult{Resources: []*pb.ResourceRef{}}
	}
	
	result := &BatchResult{
		Resources: make([]*pb.ResourceRef, 0, items.Len()),
		Errors:    make([]error, 0),
	}
	
	// Get type info once for the entire batch
	firstItem := items.Index(0)
	if firstItem.Kind() == reflect.Ptr && !firstItem.IsNil() {
		firstItem = firstItem.Elem()
	}
	
	itemType := firstItem.Type()
	typeInfo := b.reflectionCache.GetTypeInfo(itemType)
	
	// Create field extractors once
	extractors := b.createFieldExtractors(typeInfo)
	
	// Process items in chunks for better memory usage
	chunkSize := min(b.maxBatchSize, items.Len())
	
	for i := 0; i < items.Len(); i += chunkSize {
		end := min(i+chunkSize, items.Len())
		chunk := b.processChunk(items, i, end, serviceName, fieldName, typeInfo, extractors)
		result.Resources = append(result.Resources, chunk...)
	}
	
	return result
}

// FieldExtractor efficiently extracts field values
type FieldExtractor struct {
	FieldIndex int
	FieldName  string
	ExtractFn  func(reflect.Value) string
}

// createFieldExtractors pre-creates extractors for common fields
func (b *BatchProcessor) createFieldExtractors(typeInfo *TypeInfo) []FieldExtractor {
	var extractors []FieldExtractor
	
	// Create extractors for resource fields
	for fieldName, fieldIndex := range typeInfo.ResourceFields {
		extractor := FieldExtractor{
			FieldIndex: fieldIndex,
			FieldName:  fieldName,
		}
		
		// Optimize extraction based on field type
		field := typeInfo.Type.Field(fieldIndex)
		switch field.Type.Kind() {
		case reflect.String:
			extractor.ExtractFn = func(v reflect.Value) string {
				return v.Field(fieldIndex).String()
			}
		case reflect.Ptr:
			if field.Type.Elem().Kind() == reflect.String {
				extractor.ExtractFn = func(v reflect.Value) string {
					f := v.Field(fieldIndex)
					if f.IsNil() {
						return ""
					}
					return f.Elem().String()
				}
			}
		default:
			extractor.ExtractFn = func(v reflect.Value) string {
				f := v.Field(fieldIndex)
				if !f.IsValid() || !f.CanInterface() {
					return ""
				}
				return getStringValueOptimized(f)
			}
		}
		
		extractors = append(extractors, extractor)
	}
	
	return extractors
}

// processChunk processes a chunk of items
func (b *BatchProcessor) processChunk(items reflect.Value, start, end int, serviceName, fieldName string, typeInfo *TypeInfo, extractors []FieldExtractor) []*pb.ResourceRef {
	resources := make([]*pb.ResourceRef, 0, end-start)
	
	for i := start; i < end; i++ {
		item := items.Index(i)
		resource := b.extractResourceOptimized(item, serviceName, fieldName, extractors)
		if resource != nil {
			resources = append(resources, resource)
		}
	}
	
	return resources
}

// extractResourceOptimized extracts a resource using pre-created extractors
func (b *BatchProcessor) extractResourceOptimized(item reflect.Value, serviceName, fieldName string, extractors []FieldExtractor) *pb.ResourceRef {
	if item.Kind() == reflect.Ptr {
		if item.IsNil() {
			return nil
		}
		item = item.Elem()
	}
	
	resource := &pb.ResourceRef{
		Service:         serviceName,
		BasicAttributes: make(map[string]string),
	}
	
	// Apply all extractors
	for _, extractor := range extractors {
		value := extractor.ExtractFn(item)
		if value == "" {
			continue
		}
		
		// Map to resource fields
		b.mapFieldToResource(extractor.FieldName, value, resource)
	}
	
	// Set defaults
	if resource.Type == "" {
		resource.Type = inferResourceTypeOptimized(serviceName, fieldName)
	}
	
	if resource.Name == "" && resource.Id != "" {
		resource.Name = extractNameFromIdOptimized(resource.Id)
	}
	
	ensureARNAsIDOptimized(resource, serviceName)
	
	return resource
}

// mapFieldToResource maps extracted field values to resource properties
func (b *BatchProcessor) mapFieldToResource(fieldName, value string, resource *pb.ResourceRef) {
	fieldNameLower := strings.ToLower(fieldName)
	
	switch {
	case strings.Contains(fieldNameLower, "arn"):
		resource.Id = value
		resource.BasicAttributes["arn"] = value
	case strings.Contains(fieldNameLower, "id") && resource.Id == "":
		resource.Id = value
	case strings.Contains(fieldNameLower, "name") && resource.Name == "":
		resource.Name = value
	case strings.Contains(fieldNameLower, "type"):
		resource.Type = value
	case strings.Contains(fieldNameLower, "region"):
		resource.Region = value
	case strings.Contains(fieldNameLower, "state") || strings.Contains(fieldNameLower, "status"):
		resource.BasicAttributes[fieldNameLower] = value
	case strings.HasPrefix(fieldNameLower, "tag"):
		// Handle tags
		resource.BasicAttributes[fieldNameLower] = value
	default:
		resource.BasicAttributes[fieldNameLower] = value
	}
}

// Optimized helper functions
func getStringValueOptimized(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Bool:
		return fmt.Sprintf("%t", v.Bool())
	default:
		if v.CanInterface() {
			return fmt.Sprintf("%v", v.Interface())
		}
		return ""
	}
}

func inferResourceTypeOptimized(serviceName, fieldName string) string {
	// Use pre-computed mappings
	typeMap := getResourceTypeMap()
	
	key := fmt.Sprintf("%s:%s", serviceName, strings.ToLower(fieldName))
	if resourceType, exists := typeMap[key]; exists {
		return resourceType
	}
	
	// Fallback to simple inference
	if strings.HasSuffix(fieldName, "s") {
		return fieldName[:len(fieldName)-1]
	}
	return fieldName
}

func extractNameFromIdOptimized(id string) string {
	if strings.HasPrefix(id, "arn:") {
		parts := strings.Split(id, ":")
		if len(parts) >= 6 {
			resourcePart := strings.Join(parts[5:], ":")
			if idx := strings.LastIndex(resourcePart, "/"); idx != -1 {
				return resourcePart[idx+1:]
			}
			return resourcePart
		}
	}
	
	// For other IDs, extract last component
	if idx := strings.LastIndex(id, "/"); idx != -1 {
		return id[idx+1:]
	}
	return id
}

func ensureARNAsIDOptimized(resource *pb.ResourceRef, serviceName string) {
	if strings.HasPrefix(resource.Id, "arn:") {
		return
	}
	
	if resource.Id != "" {
		region := resource.Region
		if region == "" && serviceName != "iam" && serviceName != "s3" {
			region = "us-east-1"
		}
		
		resource.Id = fmt.Sprintf("arn:aws:%s:%s::%s/%s", 
			serviceName, region, resource.Type, resource.Id)
	}
}

// getResourceTypeMap returns pre-computed resource type mappings
func getResourceTypeMap() map[string]string {
	return map[string]string{
		"ec2:instances":         "Instance",
		"ec2:volumes":           "Volume",
		"ec2:snapshots":         "Snapshot",
		"ec2:securitygroups":    "SecurityGroup",
		"ec2:vpcs":              "Vpc",
		"s3:buckets":            "Bucket",
		"lambda:functions":      "Function",
		"rds:dbinstances":       "DBInstance",
		"rds:dbclusters":        "DBCluster",
		"dynamodb:tables":       "Table",
		"iam:users":             "User",
		"iam:roles":             "Role",
		"iam:policies":          "Policy",
		"eks:clusters":          "Cluster",
		"ecs:clusters":          "Cluster",
		"ecs:services":          "Service",
		"ecs:tasks":             "Task",
		// Add more as needed
	}
}

// ConcurrentBatchProcessor processes batches concurrently
type ConcurrentBatchProcessor struct {
	*BatchProcessor
	workers int
}

// NewConcurrentBatchProcessor creates a concurrent batch processor
func NewConcurrentBatchProcessor(cache *ReflectionCache, workers int) *ConcurrentBatchProcessor {
	return &ConcurrentBatchProcessor{
		BatchProcessor: NewBatchProcessor(cache),
		workers:        workers,
	}
}

// ProcessConcurrent processes items concurrently
func (c *ConcurrentBatchProcessor) ProcessConcurrent(ctx context.Context, items reflect.Value, serviceName, fieldName string) *BatchResult {
	if items.Len() <= c.maxBatchSize {
		// Small batch, process normally
		return c.ProcessBatch(ctx, items, serviceName, fieldName)
	}
	
	// Large batch, process concurrently
	result := &BatchResult{
		Resources: make([]*pb.ResourceRef, 0, items.Len()),
		Errors:    make([]error, 0),
	}
	
	// Create work channel
	type workItem struct {
		start, end int
	}
	
	workChan := make(chan workItem, c.workers)
	resultChan := make(chan []*pb.ResourceRef, c.workers)
	
	// Get type info once
	firstItem := items.Index(0)
	if firstItem.Kind() == reflect.Ptr && !firstItem.IsNil() {
		firstItem = firstItem.Elem()
	}
	
	itemType := firstItem.Type()
	typeInfo := c.reflectionCache.GetTypeInfo(itemType)
	extractors := c.createFieldExtractors(typeInfo)
	
	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range workChan {
				chunk := c.processChunk(items, work.start, work.end, serviceName, fieldName, typeInfo, extractors)
				resultChan <- chunk
			}
		}()
	}
	
	// Queue work
	go func() {
		chunkSize := items.Len() / c.workers
		if chunkSize < 10 {
			chunkSize = 10
		}
		
		for i := 0; i < items.Len(); i += chunkSize {
			end := min(i+chunkSize, items.Len())
			workChan <- workItem{start: i, end: end}
		}
		close(workChan)
	}()
	
	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()
	
	for chunk := range resultChan {
		result.Resources = append(result.Resources, chunk...)
	}
	
	return result
}
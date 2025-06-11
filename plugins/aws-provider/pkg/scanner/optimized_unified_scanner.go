package scanner

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

// PerformanceMetrics tracks scanner performance
type PerformanceMetrics struct {
	mu                    sync.RWMutex
	ReflectionCacheHits   int64
	ReflectionCacheMisses int64
	AnalysisCacheHits     int64
	AnalysisCacheMisses   int64
	TotalScanTime         time.Duration
	TotalEnrichTime       time.Duration
	ResourcesScanned      int64
}

// OptimizedUnifiedScanner provides high-performance scanning with caching
type OptimizedUnifiedScanner struct {
	*UnifiedScanner
	
	// Caching
	reflectionCache *ReflectionCache
	analysisCache   map[string]map[string]interface{} // service -> analysis data
	analysisMu      sync.RWMutex
	
	// Performance
	metrics         *PerformanceMetrics
	
	// Concurrency control
	scanLimiter     *rate.Limiter
	enrichLimiter   *rate.Limiter
	singleflight    singleflight.Group
	
	// Configuration
	maxConcurrency  int
	cacheSize       int
}

// NewOptimizedUnifiedScanner creates a new optimized scanner
func NewOptimizedUnifiedScanner(clientFactory ClientFactory) *OptimizedUnifiedScanner {
	return &OptimizedUnifiedScanner{
		UnifiedScanner:  NewUnifiedScanner(clientFactory),
		reflectionCache: NewReflectionCache(30 * time.Minute),
		analysisCache:   make(map[string]map[string]interface{}),
		metrics:         &PerformanceMetrics{},
		scanLimiter:     rate.NewLimiter(rate.Limit(100), 200), // 100 ops/sec, burst 200
		enrichLimiter:   rate.NewLimiter(rate.Limit(50), 100),  // 50 ops/sec, burst 100
		maxConcurrency:  10,
		cacheSize:       100,
	}
}

// ScanService scans all resources for a specific service with optimizations
func (o *OptimizedUnifiedScanner) ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	startTime := time.Now()
	defer func() {
		o.metrics.mu.Lock()
		o.metrics.TotalScanTime += time.Since(startTime)
		o.metrics.mu.Unlock()
	}()
	
	// Try Resource Explorer first if available
	o.mu.RLock()
	explorer := o.explorer
	o.mu.RUnlock()

	if explorer != nil {
		resources, err := explorer.QueryByService(ctx, serviceName)
		if err == nil && len(resources) > 0 {
			log.Printf("Resource Explorer found %d resources for service %s", len(resources), serviceName)
			o.updateResourceCount(int64(len(resources)))
			return resources, nil
		}
		log.Printf("Resource Explorer query failed or returned no results for %s, falling back to SDK", serviceName)
	}

	// Fall back to optimized SDK scanning
	return o.scanViaSDKOptimized(ctx, serviceName)
}

// scanViaSDKOptimized performs optimized SDK scanning with caching
func (o *OptimizedUnifiedScanner) scanViaSDKOptimized(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error) {
	// Rate limiting
	if err := o.scanLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}
	
	client := o.clientFactory.GetClient(serviceName)
	if client == nil {
		return nil, fmt.Errorf("no client available for service: %s", serviceName)
	}

	// Get cached list operations
	listOps := o.findListOperationsCached(client, serviceName)
	log.Printf("Found %d cached list operations for service %s", len(listOps), serviceName)

	// Concurrent operation invocation with worker pool
	var allResources []*pb.ResourceRef
	var mu sync.Mutex
	
	// Create worker pool
	opChan := make(chan string, len(listOps))
	resultChan := make(chan []*pb.ResourceRef, len(listOps))
	errorChan := make(chan error, len(listOps))
	
	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < min(o.maxConcurrency, len(listOps)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for opName := range opChan {
				resources, err := o.invokeListOperationOptimized(ctx, client, opName, serviceName)
				if err != nil {
					errorChan <- err
					continue
				}
				resultChan <- resources
			}
		}()
	}
	
	// Queue operations
	for _, op := range listOps {
		opChan <- op
	}
	close(opChan)
	
	// Wait for workers to complete
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()
	
	// Collect results
	for resources := range resultChan {
		mu.Lock()
		allResources = append(allResources, resources...)
		mu.Unlock()
	}
	
	// Log any errors
	for err := range errorChan {
		log.Printf("Operation error: %v", err)
	}
	
	o.updateResourceCount(int64(len(allResources)))
	return allResources, nil
}

// findListOperationsCached uses cached reflection data
func (o *OptimizedUnifiedScanner) findListOperationsCached(client interface{}, serviceName string) []string {
	// Try to get from cache using singleflight to prevent duplicate work
	key := fmt.Sprintf("listops:%s", serviceName)
	result, err, _ := o.singleflight.Do(key, func() (interface{}, error) {
		return o.findListOperations(client), nil
	})
	
	if err != nil {
		return []string{}
	}
	
	return result.([]string)
}

// invokeListOperationOptimized invokes operations with cached reflection
func (o *OptimizedUnifiedScanner) invokeListOperationOptimized(ctx context.Context, client interface{}, opName, serviceName string) ([]*pb.ResourceRef, error) {
	// Get cached method info
	methodInfo := o.reflectionCache.GetMethodInfo(serviceName, opName, client)
	if !methodInfo.IsValid {
		o.incrementCacheMiss()
		return nil, fmt.Errorf("method %s not found", opName)
	}
	
	o.incrementCacheHit()
	
	// Use cached reflection data to invoke method
	clientValue := reflect.ValueOf(client)
	method := clientValue.MethodByName(opName)
	
	// Create input with cached type info
	input := reflect.New(methodInfo.InputType)
	
	// Set pagination fields using cached indices
	o.setPaginationFieldsOptimized(input.Elem(), methodInfo)
	
	// Call method
	results := method.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		input,
	})
	
	if len(results) < 2 {
		return nil, fmt.Errorf("unexpected result count")
	}
	
	// Check for errors
	if errValue := results[1]; !errValue.IsNil() {
		return nil, errValue.Interface().(error)
	}
	
	// Extract resources using cached type info
	output := results[0].Interface()
	return o.extractResourcesOptimized(output, serviceName, opName)
}

// setPaginationFieldsOptimized uses cached field indices
func (o *OptimizedUnifiedScanner) setPaginationFieldsOptimized(inputValue reflect.Value, methodInfo *MethodInfo) {
	// Use cached pagination field indices
	for fieldName, fieldIndex := range methodInfo.PaginationFields {
		field := inputValue.Field(fieldIndex)
		if !field.CanSet() {
			continue
		}
		
		// Set pagination values
		if strings.Contains(strings.ToLower(fieldName), "max") || strings.Contains(strings.ToLower(fieldName), "limit") {
			if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Int32 {
				val := int32(100)
				field.Set(reflect.ValueOf(&val))
			}
		}
	}
}

// extractResourcesOptimized uses cached type information
func (o *OptimizedUnifiedScanner) extractResourcesOptimized(output interface{}, serviceName, opName string) ([]*pb.ResourceRef, error) {
	outputValue := reflect.ValueOf(output)
	if outputValue.Kind() == reflect.Ptr {
		outputValue = outputValue.Elem()
	}
	
	outputType := outputValue.Type()
	typeInfo := o.reflectionCache.GetTypeInfo(outputType)
	
	var resources []*pb.ResourceRef
	
	// Use cached slice field indices
	for _, fieldIndex := range typeInfo.SliceFields {
		field := outputValue.Field(fieldIndex)
		if field.Len() > 0 {
			fieldName := outputType.Field(fieldIndex).Name
			sliceResources := o.extractResourcesFromSliceOptimized(field, serviceName, fieldName)
			resources = append(resources, sliceResources...)
		}
	}
	
	return resources, nil
}

// extractResourcesFromSliceOptimized processes slices with minimal reflection
func (o *OptimizedUnifiedScanner) extractResourcesFromSliceOptimized(sliceValue reflect.Value, serviceName, fieldName string) []*pb.ResourceRef {
	if sliceValue.Len() == 0 {
		return []*pb.ResourceRef{}
	}
	
	// Use batch processor for large slices
	if sliceValue.Len() > 10 {
		batchProcessor := NewConcurrentBatchProcessor(o.reflectionCache, o.maxConcurrency)
		result := batchProcessor.ProcessConcurrent(context.Background(), sliceValue, serviceName, fieldName)
		return result.Resources
	}
	
	// Small slice, process normally with cached type info
	resources := make([]*pb.ResourceRef, 0, sliceValue.Len())
	
	firstItem := sliceValue.Index(0)
	if firstItem.Kind() == reflect.Ptr && !firstItem.IsNil() {
		firstItem = firstItem.Elem()
	}
	
	itemType := firstItem.Type()
	typeInfo := o.reflectionCache.GetTypeInfo(itemType)
	
	for i := 0; i < sliceValue.Len(); i++ {
		item := sliceValue.Index(i)
		if resource := o.extractResourceFromItemOptimized(item, serviceName, fieldName, typeInfo); resource != nil {
			resources = append(resources, resource)
		}
	}
	
	return resources
}

// extractResourceFromItemOptimized extracts resources using cached field info
func (o *OptimizedUnifiedScanner) extractResourceFromItemOptimized(item reflect.Value, serviceName, fieldName string, typeInfo *TypeInfo) *pb.ResourceRef {
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
	
	// Use cached field indices for common resource fields
	for fieldName, fieldIndex := range typeInfo.ResourceFields {
		field := item.Field(fieldIndex)
		if !field.IsValid() || !field.CanInterface() {
			continue
		}
		
		value := o.getStringValue(field)
		if value == "" {
			continue
		}
		
		// Map to resource fields based on patterns
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
		default:
			resource.BasicAttributes[fieldNameLower] = value
		}
	}
	
	// Set defaults
	if resource.Type == "" {
		resource.Type = o.inferResourceType(serviceName, fieldName)
	}
	
	if resource.Name == "" && resource.Id != "" {
		resource.Name = o.extractNameFromId(resource.Id)
	}
	
	o.ensureARNAsID(resource, serviceName)
	
	return resource
}

// DescribeResource with optimized configuration collection
func (o *OptimizedUnifiedScanner) DescribeResource(ctx context.Context, resourceRef *pb.ResourceRef) (*pb.Resource, error) {
	startTime := time.Now()
	defer func() {
		o.metrics.mu.Lock()
		o.metrics.TotalEnrichTime += time.Since(startTime)
		o.metrics.mu.Unlock()
	}()
	
	// Rate limiting for enrichment
	if err := o.enrichLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("enrichment rate limit exceeded: %w", err)
	}
	
	// Call parent implementation with optimized config collection
	return o.UnifiedScanner.DescribeResource(ctx, resourceRef)
}

// loadServiceAnalysis with caching
func (o *OptimizedUnifiedScanner) loadServiceAnalysis(serviceName string) (map[string]interface{}, error) {
	// Check cache first
	o.analysisMu.RLock()
	if analysis, exists := o.analysisCache[serviceName]; exists {
		o.analysisMu.RUnlock()
		o.incrementAnalysisCacheHit()
		return analysis, nil
	}
	o.analysisMu.RUnlock()
	
	o.incrementAnalysisCacheMiss()
	
	// Use singleflight to prevent duplicate loads
	key := fmt.Sprintf("analysis:%s", serviceName)
	result, err, _ := o.singleflight.Do(key, func() (interface{}, error) {
		return o.UnifiedScanner.loadServiceAnalysis(serviceName)
	})
	
	if err != nil {
		return nil, err
	}
	
	analysis := result.(map[string]interface{})
	
	// Cache the result
	o.analysisMu.Lock()
	if len(o.analysisCache) >= o.cacheSize {
		// Simple eviction - remove first item
		for k := range o.analysisCache {
			delete(o.analysisCache, k)
			break
		}
	}
	o.analysisCache[serviceName] = analysis
	o.analysisMu.Unlock()
	
	return analysis, nil
}

// Performance metric helpers
func (o *OptimizedUnifiedScanner) incrementCacheHit() {
	o.metrics.mu.Lock()
	o.metrics.ReflectionCacheHits++
	o.metrics.mu.Unlock()
}

func (o *OptimizedUnifiedScanner) incrementCacheMiss() {
	o.metrics.mu.Lock()
	o.metrics.ReflectionCacheMisses++
	o.metrics.mu.Unlock()
}

func (o *OptimizedUnifiedScanner) incrementAnalysisCacheHit() {
	o.metrics.mu.Lock()
	o.metrics.AnalysisCacheHits++
	o.metrics.mu.Unlock()
}

func (o *OptimizedUnifiedScanner) incrementAnalysisCacheMiss() {
	o.metrics.mu.Lock()
	o.metrics.AnalysisCacheMisses++
	o.metrics.mu.Unlock()
}

func (o *OptimizedUnifiedScanner) updateResourceCount(count int64) {
	o.metrics.mu.Lock()
	o.metrics.ResourcesScanned += count
	o.metrics.mu.Unlock()
}

// GetMetrics returns performance metrics
func (o *OptimizedUnifiedScanner) GetMetrics() PerformanceMetrics {
	o.metrics.mu.RLock()
	defer o.metrics.mu.RUnlock()
	return *o.metrics
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
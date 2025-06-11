package scanner

import (
	"reflect"
	"strings"
	"sync"
	"time"
)

// MethodInfo caches reflection information about a method
type MethodInfo struct {
	Name       string
	InputType  reflect.Type
	OutputType reflect.Type
	NumIn      int
	NumOut     int
	IsValid    bool
	
	// Cached field indices for common patterns
	PaginationFields map[string]int // field name -> field index
	IDFields         map[string]int // field name -> field index
}

// ReflectionCache caches reflection analysis results with TTL
type ReflectionCache struct {
	mu            sync.RWMutex
	methodCache   map[string]map[string]*MethodInfo // service -> method -> info
	typeCache     map[reflect.Type]*TypeInfo       // type -> analyzed info
	ttl           time.Duration
	lastCleanup   time.Time
	cleanupPeriod time.Duration
}

// TypeInfo caches analyzed information about a struct type
type TypeInfo struct {
	Type          reflect.Type
	Fields        map[string]int        // field name -> index
	SliceFields   []int                 // indices of slice fields
	ResourceFields map[string]int        // common resource field patterns
	CachedAt      time.Time
}

// NewReflectionCache creates a new reflection cache
func NewReflectionCache(ttl time.Duration) *ReflectionCache {
	return &ReflectionCache{
		methodCache:   make(map[string]map[string]*MethodInfo),
		typeCache:     make(map[reflect.Type]*TypeInfo),
		ttl:           ttl,
		lastCleanup:   time.Now(),
		cleanupPeriod: 5 * time.Minute,
	}
}

// GetMethodInfo retrieves cached method information
func (rc *ReflectionCache) GetMethodInfo(service, method string, client interface{}) *MethodInfo {
	rc.mu.RLock()
	if serviceCache, exists := rc.methodCache[service]; exists {
		if info, exists := serviceCache[method]; exists {
			rc.mu.RUnlock()
			return info
		}
	}
	rc.mu.RUnlock()
	
	// Cache miss - analyze method
	info := rc.analyzeMethod(client, method)
	
	rc.mu.Lock()
	defer rc.mu.Unlock()
	
	if rc.methodCache[service] == nil {
		rc.methodCache[service] = make(map[string]*MethodInfo)
	}
	rc.methodCache[service][method] = info
	
	// Periodic cleanup
	if time.Since(rc.lastCleanup) > rc.cleanupPeriod {
		go rc.cleanup()
	}
	
	return info
}

// analyzeMethod performs reflection analysis on a method
func (rc *ReflectionCache) analyzeMethod(client interface{}, methodName string) *MethodInfo {
	clientValue := reflect.ValueOf(client)
	method := clientValue.MethodByName(methodName)
	
	info := &MethodInfo{
		Name:             methodName,
		IsValid:          method.IsValid(),
		PaginationFields: make(map[string]int),
		IDFields:         make(map[string]int),
	}
	
	if !info.IsValid {
		return info
	}
	
	methodType := method.Type()
	info.NumIn = methodType.NumIn()
	info.NumOut = methodType.NumOut()
	
	if info.NumIn >= 2 {
		info.InputType = methodType.In(1)
		if info.InputType.Kind() == reflect.Ptr {
			info.InputType = info.InputType.Elem()
		}
		
		// Cache common field indices
		rc.cacheFieldIndices(info.InputType, info)
	}
	
	if info.NumOut >= 1 {
		info.OutputType = methodType.In(0)
	}
	
	return info
}

// cacheFieldIndices caches field indices for common patterns
func (rc *ReflectionCache) cacheFieldIndices(t reflect.Type, info *MethodInfo) {
	if t.Kind() != reflect.Struct {
		return
	}
	
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldNameLower := toLowerCamelCase(field.Name)
		
		// Cache pagination fields
		if isPaginationField(fieldNameLower) {
			info.PaginationFields[field.Name] = i
		}
		
		// Cache ID fields
		if isIDField(fieldNameLower) {
			info.IDFields[field.Name] = i
		}
	}
}

// GetTypeInfo retrieves cached type information
func (rc *ReflectionCache) GetTypeInfo(t reflect.Type) *TypeInfo {
	rc.mu.RLock()
	if info, exists := rc.typeCache[t]; exists {
		if time.Since(info.CachedAt) < rc.ttl {
			rc.mu.RUnlock()
			return info
		}
	}
	rc.mu.RUnlock()
	
	// Cache miss or expired - analyze type
	info := rc.analyzeType(t)
	
	rc.mu.Lock()
	rc.typeCache[t] = info
	rc.mu.Unlock()
	
	return info
}

// analyzeType performs reflection analysis on a type
func (rc *ReflectionCache) analyzeType(t reflect.Type) *TypeInfo {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	
	info := &TypeInfo{
		Type:           t,
		Fields:         make(map[string]int),
		SliceFields:    []int{},
		ResourceFields: make(map[string]int),
		CachedAt:       time.Now(),
	}
	
	if t.Kind() != reflect.Struct {
		return info
	}
	
	// Analyze all fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		info.Fields[field.Name] = i
		
		// Track slice fields (potential resource lists)
		if field.Type.Kind() == reflect.Slice {
			info.SliceFields = append(info.SliceFields, i)
		}
		
		// Track resource-related fields
		fieldNameLower := toLowerCamelCase(field.Name)
		if isResourceField(fieldNameLower) {
			info.ResourceFields[field.Name] = i
		}
	}
	
	return info
}

// cleanup removes expired entries
func (rc *ReflectionCache) cleanup() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	
	now := time.Now()
	
	// Clean type cache
	for t, info := range rc.typeCache {
		if now.Sub(info.CachedAt) > rc.ttl {
			delete(rc.typeCache, t)
		}
	}
	
	rc.lastCleanup = now
}

// Helper functions
func toLowerCamelCase(s string) string {
	if len(s) == 0 {
		return s
	}
	// Simple lowercase for comparison
	return strings.ToLower(s)
}

func isPaginationField(fieldName string) bool {
	paginationPatterns := []string{"maxresults", "maxitems", "limit", "nexttoken", "marker"}
	for _, pattern := range paginationPatterns {
		if strings.Contains(fieldName, pattern) {
			return true
		}
	}
	return false
}

func isIDField(fieldName string) bool {
	idPatterns := []string{"id", "name", "arn", "identifier", "key"}
	for _, pattern := range idPatterns {
		if strings.Contains(fieldName, pattern) {
			return true
		}
	}
	return false
}

func isResourceField(fieldName string) bool {
	return isIDField(fieldName) || 
		strings.Contains(fieldName, "state") || 
		strings.Contains(fieldName, "status") ||
		strings.Contains(fieldName, "type") ||
		strings.Contains(fieldName, "region")
}
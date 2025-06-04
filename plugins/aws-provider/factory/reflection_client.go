package factory

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
)

// ReflectionClient provides reflection-based client creation capabilities
type ReflectionClient struct {
	registry         registry.DynamicServiceRegistry
	reflectionCache  sync.Map // Cache of reflection metadata
	methodCache      sync.Map // Cache of NewFromConfig methods
	typeCache        sync.Map // Cache of client types
	packageCache     sync.Map // Cache of loaded packages
	mu               sync.RWMutex
}

// ReflectionMetadata contains cached reflection information for a service
type ReflectionMetadata struct {
	ServiceName     string
	PackagePath     string
	ClientType      reflect.Type
	NewFromConfig   reflect.Value
	ConstructorType reflect.Type
	CachedAt        string
}

// ClientCreationResult represents the result of client creation
type ClientCreationResult struct {
	Client      interface{}
	Method      string // How the client was created
	ServiceInfo *registry.ServiceDefinition
	Error       error
}

// NewReflectionClient creates a new reflection-based client factory
func NewReflectionClient(reg registry.DynamicServiceRegistry) *ReflectionClient {
	return &ReflectionClient{
		registry: reg,
	}
}

// CreateClient attempts to create a client using multiple fallback strategies
func (rc *ReflectionClient) CreateClient(ctx context.Context, serviceName string, config aws.Config) (*ClientCreationResult, error) {
	serviceName = strings.ToLower(serviceName)

	// Strategy 1: Try cached reflection metadata
	if result := rc.tryCreateFromCache(serviceName, config); result != nil {
		if result.Error == nil {
			return result, nil
		}
		// Continue to next strategy if cache failed
	}

	// Strategy 2: Try using registry metadata with reflection
	if result := rc.tryCreateFromRegistry(serviceName, config); result != nil {
		if result.Error == nil {
			return result, nil
		}
		// Continue to next strategy if registry-based creation failed
	}

	// Strategy 3: Try runtime discovery with reflection
	if result := rc.tryCreateWithRuntimeDiscovery(ctx, serviceName, config); result != nil {
		if result.Error == nil {
			return result, nil
		}
	}

	// Strategy 4: Try known type registration
	if result := rc.tryCreateFromKnownTypes(serviceName, config); result != nil {
		if result.Error == nil {
			return result, nil
		}
	}

	// All strategies failed
	return &ClientCreationResult{
		Method: "none",
		Error:  fmt.Errorf("failed to create client for service %s: all strategies exhausted", serviceName),
	}, nil
}

// tryCreateFromCache attempts to create a client using cached reflection data
func (rc *ReflectionClient) tryCreateFromCache(serviceName string, config aws.Config) *ClientCreationResult {
	// Check if we have cached metadata
	if cached, ok := rc.reflectionCache.Load(serviceName); ok {
		metadata := cached.(*ReflectionMetadata)
		
		// Check if we have a cached NewFromConfig method
		if metadata.NewFromConfig.IsValid() {
			// Call the method
			results := metadata.NewFromConfig.Call([]reflect.Value{reflect.ValueOf(config)})
			if len(results) > 0 {
				return &ClientCreationResult{
					Client: results[0].Interface(),
					Method: "cached_reflection",
				}
			}
		}
	}

	return nil
}

// tryCreateFromRegistry attempts to create a client using registry information
func (rc *ReflectionClient) tryCreateFromRegistry(serviceName string, config aws.Config) *ClientCreationResult {
	service, exists := rc.registry.GetService(serviceName)
	if !exists {
		return &ClientCreationResult{
			Method: "registry",
			Error:  fmt.Errorf("service %s not found in registry", serviceName),
		}
	}

	// If we have a ClientRef (runtime type), use it directly
	if service.ClientRef != nil {
		client, err := rc.createFromType(service.ClientRef, config)
		if err == nil {
			return &ClientCreationResult{
				Client:      client,
				Method:      "registry_type",
				ServiceInfo: service,
			}
		}
	}

	// Try to load the package and find the client type
	if service.PackagePath != "" {
		client, err := rc.createFromPackagePath(service.PackagePath, service.ClientType, config)
		if err == nil {
			// Cache the successful creation
			rc.cacheReflectionData(serviceName, service)
			return &ClientCreationResult{
				Client:      client,
				Method:      "registry_package",
				ServiceInfo: service,
			}
		}
	}

	return &ClientCreationResult{
		Method: "registry",
		Error:  fmt.Errorf("failed to create client from registry data for %s", serviceName),
	}
}

// tryCreateWithRuntimeDiscovery attempts runtime discovery to create a client
func (rc *ReflectionClient) tryCreateWithRuntimeDiscovery(ctx context.Context, serviceName string, config aws.Config) *ClientCreationResult {
	// This would involve more complex runtime discovery
	// For now, we'll implement a basic version

	// Common AWS SDK v2 package pattern
	packagePath := fmt.Sprintf("github.com/aws/aws-sdk-go-v2/service/%s", serviceName)
	clientType := fmt.Sprintf("*%s.Client", serviceName)

	client, err := rc.createFromPackagePath(packagePath, clientType, config)
	if err == nil {
		// Register discovered service in registry
		discoveredService := registry.ServiceDefinition{
			Name:             serviceName,
			DisplayName:      formatServiceName(serviceName),
			PackagePath:      packagePath,
			ClientType:       clientType,
			DiscoverySource:  "runtime_reflection",
		}
		
		rc.registry.RegisterService(discoveredService)
		
		return &ClientCreationResult{
			Client:      client,
			Method:      "runtime_discovery",
			ServiceInfo: &discoveredService,
		}
	}

	return &ClientCreationResult{
		Method: "runtime_discovery",
		Error:  fmt.Errorf("runtime discovery failed for %s: %v", serviceName, err),
	}
}

// tryCreateFromKnownTypes tries to create from pre-registered types
func (rc *ReflectionClient) tryCreateFromKnownTypes(serviceName string, config aws.Config) *ClientCreationResult {
	// Check if we have a known type registered
	if typeVal, ok := rc.typeCache.Load(serviceName); ok {
		clientType := typeVal.(reflect.Type)
		client, err := rc.createFromType(clientType, config)
		if err == nil {
			return &ClientCreationResult{
				Client: client,
				Method: "known_type",
			}
		}
	}

	return nil
}

// createFromPackagePath creates a client given a package path and client type name
func (rc *ReflectionClient) createFromPackagePath(packagePath, clientTypeName string, config aws.Config) (interface{}, error) {
	// In a real implementation, this would use plugin loading or other dynamic loading
	// For now, we'll simulate with reflection on already imported types
	
	// Extract package name from path
	parts := strings.Split(packagePath, "/")
	packageName := parts[len(parts)-1]

	// Try to find the type in the runtime
	targetType := fmt.Sprintf("%s.Client", packageName)
	
	// Look for the type using reflection
	// This is a simplified version - real implementation would be more complex
	clientType := rc.findTypeByName(targetType)
	if clientType == nil {
		return nil, fmt.Errorf("could not find type %s", targetType)
	}

	return rc.createFromType(clientType, config)
}

// createFromType creates a client from a reflect.Type
func (rc *ReflectionClient) createFromType(clientType reflect.Type, config aws.Config) (interface{}, error) {
	// Look for NewFromConfig method
	method, found := clientType.MethodByName("NewFromConfig")
	if !found {
		// Try as a pointer type
		if clientType.Kind() != reflect.Ptr {
			ptrType := reflect.PtrTo(clientType)
			method, found = ptrType.MethodByName("NewFromConfig")
			if !found {
				return nil, fmt.Errorf("type %s has no NewFromConfig method", clientType.Name())
			}
		} else {
			return nil, fmt.Errorf("type %s has no NewFromConfig method", clientType.Name())
		}
	}

	// Check method signature
	methodType := method.Type
	if methodType.NumIn() != 1 || methodType.NumOut() != 1 {
		return nil, fmt.Errorf("NewFromConfig has unexpected signature")
	}

	// Create config value and call method
	configValue := reflect.ValueOf(config)
	results := method.Func.Call([]reflect.Value{configValue})
	
	if len(results) != 1 {
		return nil, fmt.Errorf("NewFromConfig returned unexpected number of results")
	}

	return results[0].Interface(), nil
}

// findTypeByName attempts to find a type by name in the runtime
func (rc *ReflectionClient) findTypeByName(typeName string) reflect.Type {
	// This is a placeholder - in a real implementation, we would:
	// 1. Use plugin.Open to load service packages dynamically
	// 2. Use debug/dwarf to inspect binary symbols
	// 3. Maintain a registry of types at compile time
	
	// For now, check our type cache
	if cached, ok := rc.typeCache.Load(typeName); ok {
		return cached.(reflect.Type)
	}

	return nil
}

// RegisterType registers a known type for a service
func (rc *ReflectionClient) RegisterType(serviceName string, clientType reflect.Type) {
	rc.typeCache.Store(serviceName, clientType)
	
	// Also try to cache the NewFromConfig method
	if method, found := clientType.MethodByName("NewFromConfig"); found {
		rc.methodCache.Store(serviceName, method)
	}
}

// cacheReflectionData caches reflection data for a service
func (rc *ReflectionClient) cacheReflectionData(serviceName string, service *registry.ServiceDefinition) {
	metadata := &ReflectionMetadata{
		ServiceName: serviceName,
		PackagePath: service.PackagePath,
		ClientType:  service.ClientRef,
		CachedAt:    runtime.Version(),
	}
	
	rc.reflectionCache.Store(serviceName, metadata)
}

// PreloadTypes attempts to preload types for common services
func (rc *ReflectionClient) PreloadTypes() {
	// This would be called at startup to register known types
	// In a real implementation, this could use code generation
	// to register all available service client types
}

// GetCachedServices returns a list of services with cached reflection data
func (rc *ReflectionClient) GetCachedServices() []string {
	var services []string
	rc.reflectionCache.Range(func(key, value interface{}) bool {
		services = append(services, key.(string))
		return true
	})
	return services
}

// ClearCache clears all cached reflection data
func (rc *ReflectionClient) ClearCache() {
	rc.reflectionCache = sync.Map{}
	rc.methodCache = sync.Map{}
	rc.packageCache = sync.Map{}
}

// Helper function to format service names
func formatServiceName(name string) string {
	return "AWS " + strings.ToUpper(name)
}
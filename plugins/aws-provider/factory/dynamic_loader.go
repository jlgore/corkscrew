package factory

import (
	"fmt"
	"plugin"
	"reflect"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// DynamicLoader provides advanced dynamic loading capabilities
type DynamicLoader struct {
	pluginDir      string
	loadedPlugins  sync.Map // serviceName -> *plugin.Plugin
	symbolCache    sync.Map // symbol -> reflect.Value
	typeRegistry   sync.Map // typeName -> reflect.Type
	mu             sync.RWMutex
}

// ServicePlugin represents a loaded service plugin
type ServicePlugin struct {
	Plugin      *plugin.Plugin
	ServiceName string
	Version     string
	LoadedAt    string
}

// NewDynamicLoader creates a new dynamic loader
func NewDynamicLoader(pluginDir string) *DynamicLoader {
	return &DynamicLoader{
		pluginDir: pluginDir,
	}
}

// LoadServiceClient attempts to load a service client dynamically
func (dl *DynamicLoader) LoadServiceClient(serviceName string, config aws.Config) (interface{}, error) {
	// Try to load from plugin first
	if client, err := dl.loadFromPlugin(serviceName, config); err == nil {
		return client, nil
	}

	// Try to load using reflection on existing types
	if client, err := dl.loadFromExistingTypes(serviceName, config); err == nil {
		return client, nil
	}

	// Try to create a generic client
	return dl.createGenericClient(serviceName, config)
}

// loadFromPlugin loads a service client from a plugin file
func (dl *DynamicLoader) loadFromPlugin(serviceName string, config aws.Config) (interface{}, error) {
	// Check if plugin is already loaded
	if p, ok := dl.loadedPlugins.Load(serviceName); ok {
		return dl.createClientFromPlugin(p.(*plugin.Plugin), config)
	}

	// Try to load the plugin
	pluginPath := fmt.Sprintf("%s/aws-%s.so", dl.pluginDir, serviceName)
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin %s: %w", pluginPath, err)
	}

	// Cache the loaded plugin
	dl.loadedPlugins.Store(serviceName, p)

	return dl.createClientFromPlugin(p, config)
}

// createClientFromPlugin creates a client from a loaded plugin
func (dl *DynamicLoader) createClientFromPlugin(p *plugin.Plugin, config aws.Config) (interface{}, error) {
	// Look for standard symbols
	symbols := []string{"NewClient", "NewFromConfig", "CreateClient"}
	
	for _, symbolName := range symbols {
		sym, err := p.Lookup(symbolName)
		if err != nil {
			continue
		}

		// Try different function signatures
		// Signature 1: func(aws.Config) interface{}
		if fn, ok := sym.(func(aws.Config) interface{}); ok {
			return fn(config), nil
		}

		// Signature 2: func(aws.Config) (interface{}, error)
		if fn, ok := sym.(func(aws.Config) (interface{}, error)); ok {
			return fn(config)
		}

		// Signature 3: Using reflection for more complex signatures
		symValue := reflect.ValueOf(sym)
		if symValue.Kind() == reflect.Func {
			result := dl.callFunction(symValue, config)
			if result != nil {
				return result, nil
			}
		}
	}

	return nil, fmt.Errorf("no suitable constructor found in plugin")
}

// loadFromExistingTypes tries to find and use already loaded types
func (dl *DynamicLoader) loadFromExistingTypes(serviceName string, config aws.Config) (interface{}, error) {
	// Check type registry
	typeName := fmt.Sprintf("%s.Client", serviceName)
	if t, ok := dl.typeRegistry.Load(typeName); ok {
		return dl.createFromType(t.(reflect.Type), config)
	}

	// Try common patterns
	patterns := []string{
		fmt.Sprintf("*%s.Client", serviceName),
		fmt.Sprintf("%s.Client", serviceName),
		fmt.Sprintf("*%sClient", serviceName),
		fmt.Sprintf("%sClient", serviceName),
	}

	for _, pattern := range patterns {
		if t := dl.findTypeByPattern(pattern); t != nil {
			// Cache for future use
			dl.typeRegistry.Store(typeName, t)
			return dl.createFromType(t, config)
		}
	}

	return nil, fmt.Errorf("type not found for service %s", serviceName)
}

// createGenericClient creates a generic client that can invoke any operation
func (dl *DynamicLoader) createGenericClient(serviceName string, config aws.Config) (interface{}, error) {
	return &GenericServiceClient{
		ServiceName: serviceName,
		Config:      config,
		Loader:      dl,
	}, nil
}

// callFunction calls a function with various argument patterns
func (dl *DynamicLoader) callFunction(fn reflect.Value, config aws.Config) interface{} {
	fnType := fn.Type()
	
	// Check if function accepts aws.Config
	for i := 0; i < fnType.NumIn(); i++ {
		inType := fnType.In(i)
		if inType == reflect.TypeOf(config) {
			// Create arguments
			args := make([]reflect.Value, fnType.NumIn())
			args[i] = reflect.ValueOf(config)
			
			// Fill other arguments with zero values
			for j := 0; j < fnType.NumIn(); j++ {
				if j != i {
					args[j] = reflect.Zero(fnType.In(j))
				}
			}

			// Call the function
			results := fn.Call(args)
			if len(results) > 0 && !results[0].IsNil() {
				return results[0].Interface()
			}
		}
	}

	return nil
}

// createFromType creates an instance from a type
func (dl *DynamicLoader) createFromType(t reflect.Type, config aws.Config) (interface{}, error) {
	// Look for constructor methods
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Try to find NewFromConfig as a package function
	// This would require more advanced reflection or code generation
	
	// For now, return error
	return nil, fmt.Errorf("cannot instantiate type %s", t.Name())
}

// findTypeByPattern searches for a type matching a pattern
func (dl *DynamicLoader) findTypeByPattern(pattern string) reflect.Type {
	// In a real implementation, this would search through:
	// 1. Runtime type information
	// 2. Debug symbols
	// 3. Pre-registered types
	
	// Check our type registry
	var foundType reflect.Type
	dl.typeRegistry.Range(func(key, value interface{}) bool {
		if matched, _ := matchPattern(key.(string), pattern); matched {
			foundType = value.(reflect.Type)
			return false // Stop iteration
		}
		return true
	})

	return foundType
}

// RegisterType registers a type for dynamic loading
func (dl *DynamicLoader) RegisterType(name string, t reflect.Type) {
	dl.typeRegistry.Store(name, t)
}

// RegisterSymbol registers a symbol (function/variable) for dynamic loading
func (dl *DynamicLoader) RegisterSymbol(name string, symbol interface{}) {
	dl.symbolCache.Store(name, reflect.ValueOf(symbol))
}

// GetLoadedPlugins returns information about loaded plugins
func (dl *DynamicLoader) GetLoadedPlugins() []ServicePlugin {
	var plugins []ServicePlugin
	dl.loadedPlugins.Range(func(key, value interface{}) bool {
		plugins = append(plugins, ServicePlugin{
			Plugin:      value.(*plugin.Plugin),
			ServiceName: key.(string),
		})
		return true
	})
	return plugins
}

// UnloadPlugin unloads a specific plugin
func (dl *DynamicLoader) UnloadPlugin(serviceName string) error {
	dl.loadedPlugins.Delete(serviceName)
	// Note: Go plugins cannot actually be unloaded from memory
	// This just removes our reference to it
	return nil
}

// GenericServiceClient provides a generic client implementation
type GenericServiceClient struct {
	ServiceName string
	Config      aws.Config
	Loader      *DynamicLoader
	client      interface{} // Cached actual client if loaded
	mu          sync.Mutex
}

// Invoke invokes an operation on the service
func (c *GenericServiceClient) Invoke(operation string, input interface{}) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to get or create the actual client
	if c.client == nil {
		client, err := c.Loader.LoadServiceClient(c.ServiceName, c.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to load client: %w", err)
		}
		c.client = client
	}

	// Use reflection to invoke the operation
	clientValue := reflect.ValueOf(c.client)
	method := clientValue.MethodByName(operation)
	
	if !method.IsValid() {
		return nil, fmt.Errorf("operation %s not found on %s client", operation, c.ServiceName)
	}

	// Prepare arguments
	var args []reflect.Value
	if input != nil {
		args = append(args, reflect.ValueOf(input))
	}

	// Call the method
	results := method.Call(args)
	
	// Handle results
	if len(results) == 0 {
		return nil, nil
	}

	// Check for error in last result
	if len(results) > 1 {
		lastResult := results[len(results)-1]
		if err, ok := lastResult.Interface().(error); ok && err != nil {
			return nil, err
		}
	}

	// Return first result
	return results[0].Interface(), nil
}

// GetServiceName returns the service name
func (c *GenericServiceClient) GetServiceName() string {
	return c.ServiceName
}

// Helper function to match patterns
func matchPattern(str, pattern string) (bool, error) {
	// Simple pattern matching - in production, use more sophisticated matching
	pattern = strings.ReplaceAll(pattern, "*", "")
	return strings.Contains(str, pattern), nil
}
package client

import (
	"context"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// ReflectionClientFactory uses the generated client factory for service discovery and client creation
type ReflectionClientFactory struct {
	config  aws.Config
	mu      sync.RWMutex
	clients map[string]interface{}
}

// NewReflectionClientFactory creates a factory that delegates to the generated client factory
func NewReflectionClientFactory(cfg aws.Config) *ReflectionClientFactory {
	return &ReflectionClientFactory{
		config:  cfg,
		clients: make(map[string]interface{}),
	}
}

// GetClient returns a client for the specified service
func (cf *ReflectionClientFactory) GetClient(serviceName string) interface{} {
	log.Printf("DEBUG: ReflectionFactory.GetClient called for service: %s", serviceName)
	
	cf.mu.RLock()
	if client, exists := cf.clients[serviceName]; exists {
		cf.mu.RUnlock()
		log.Printf("DEBUG: Client for %s found in cache", serviceName)
		return client
	}
	cf.mu.RUnlock()
	log.Printf("DEBUG: Client for %s not in cache, checking constructors", serviceName)

	// Check registered constructors first
	constructor := GetConstructor(serviceName)
	if constructor != nil {
		log.Printf("DEBUG: Constructor found for %s", serviceName)
		cf.mu.Lock()
		defer cf.mu.Unlock()
		
		// Double-check in case another goroutine created it
		if client, exists := cf.clients[serviceName]; exists {
			return client
		}
		
		// Create client using registered constructor
		client := constructor(cf.config)
		if client != nil {
			cf.clients[serviceName] = client
			log.Printf("Created %s client using registered constructor", serviceName)
			return client
		}
	} else {
		log.Printf("DEBUG: No constructor found for %s", serviceName)
	}

	// Try using the generated factory if available
	generatedFactory := GetGeneratedFactory()
	if generatedFactory != nil {
		log.Printf("DEBUG: Generated factory available, trying to create %s client", serviceName)
		cf.mu.Lock()
		defer cf.mu.Unlock()
		
		// Double-check in case another goroutine created it
		if client, exists := cf.clients[serviceName]; exists {
			return client
		}
		
		// Create client using generated factory
		if client, err := generatedFactory.CreateClient(context.Background(), serviceName); err == nil && client != nil {
			cf.clients[serviceName] = client
			log.Printf("Created %s client using generated factory", serviceName)
			return client
		} else if err != nil {
			log.Printf("Generated factory failed to create %s client: %v", serviceName, err)
		}
	} else {
		log.Printf("DEBUG: No generated factory available")
	}

	log.Printf("No factory available to create %s client", serviceName)
	return nil
}

// GetAvailableServices returns the list of available services
func (cf *ReflectionClientFactory) GetAvailableServices() []string {
	// Combine registered services with generated factory services
	registeredServices := ListRegisteredServices()
	
	// Create a set to avoid duplicates
	serviceSet := make(map[string]bool)
	
	// Add registered services
	for _, service := range registeredServices {
		serviceSet[service] = true
	}
	
	// Add services from generated factory
	if generatedFactory := GetGeneratedFactory(); generatedFactory != nil {
		for _, service := range generatedFactory.ListAvailableServices() {
			serviceSet[service] = true
		}
	}
	
	// Convert back to slice
	allServices := make([]string, 0, len(serviceSet))
	for service := range serviceSet {
		allServices = append(allServices, service)
	}
	
	return allServices
}

// HasClient checks if a client exists for the given service
func (cf *ReflectionClientFactory) HasClient(serviceName string) bool {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	_, exists := cf.clients[serviceName]
	return exists
}

// ClearCache removes all cached clients
func (cf *ReflectionClientFactory) ClearCache() {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.clients = make(map[string]interface{})
}

// GetConfig returns the AWS configuration
func (cf *ReflectionClientFactory) GetConfig() aws.Config {
	return cf.config
}

// SetConfig updates the AWS configuration
func (cf *ReflectionClientFactory) SetConfig(cfg aws.Config) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.config = cfg
	// Clear cache since config changed
	cf.clients = make(map[string]interface{})
}

// GetClientMethodNames returns method names for a service client
func (cf *ReflectionClientFactory) GetClientMethodNames(serviceName string) []string {
	// Return common AWS SDK method patterns
	return []string{
		"List*", "Describe*", "Get*", "Put*", "Create*", "Delete*", "Update*",
	}
}

// GetListOperations returns list/scan operations for a service
func (cf *ReflectionClientFactory) GetListOperations(serviceName string) []string {
	// Service-specific list operations based on AWS SDK patterns
	switch serviceName {
	case "s3":
		return []string{"ListBuckets", "ListObjects", "ListObjectsV2"}
	case "ec2":
		return []string{"DescribeInstances", "DescribeImages", "DescribeVolumes", "DescribeSecurityGroups", "DescribeVpcs", "DescribeSubnets"}
	case "iam":
		return []string{"ListUsers", "ListRoles", "ListPolicies", "ListGroups"}
	case "lambda":
		return []string{"ListFunctions", "ListLayers", "ListEventSourceMappings"}
	case "rds":
		return []string{"DescribeDBInstances", "DescribeDBClusters", "DescribeDBSnapshots"}
	case "dynamodb":
		return []string{"ListTables", "DescribeTable"}
	case "kms":
		return []string{"ListKeys", "DescribeKey"}
	default:
		return []string{"List*", "Describe*"}
	}
}

// GetDescribeOperations returns describe/detail operations for a service
func (cf *ReflectionClientFactory) GetDescribeOperations(serviceName string) []string {
	// Service-specific describe operations based on AWS SDK patterns
	switch serviceName {
	case "s3":
		return []string{"GetObject", "GetBucketPolicy", "GetBucketAcl", "GetBucketVersioning", "GetBucketEncryption"}
	case "ec2":
		return []string{"DescribeInstanceAttribute", "DescribeImageAttribute", "DescribeVolumeAttribute"}
	case "iam":
		return []string{"GetUser", "GetRole", "GetPolicy", "GetGroup"}
	case "lambda":
		return []string{"GetFunction", "GetLayerVersion", "GetEventSourceMapping"}
	case "rds":
		return []string{"DescribeDBInstances", "DescribeDBClusters"}
	case "dynamodb":
		return []string{"DescribeTable", "DescribeBackup"}
	case "kms":
		return []string{"DescribeKey", "GetKeyPolicy"}
	default:
		return []string{"Get*", "Describe*"}
	}
}
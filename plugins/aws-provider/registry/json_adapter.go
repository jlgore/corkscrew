package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"golang.org/x/time/rate"
)

// JSONServiceRegistry implements DynamicServiceRegistry by loading from services.json
type JSONServiceRegistry struct {
	services   map[string]*ServiceDefinition
	config     RegistryConfig
	lastLoaded time.Time
}

// NewJSONServiceRegistry creates a registry that loads from services.json
func NewJSONServiceRegistry() *JSONServiceRegistry {
	return &JSONServiceRegistry{
		services: make(map[string]*ServiceDefinition),
		config: RegistryConfig{
			EnableCache:  true,
			AutoPersist:  false,
			StrictValidation: true,
		},
	}
}

// JSONServiceData represents the structure of services.json
type JSONServiceData struct {
	Version    string       `json:"version"`
	AnalyzedAt string       `json:"analyzed_at"`
	SDKVersion string       `json:"sdk_version"`
	Services   []JSONService `json:"services"`
}

// JSONService represents a service as stored in services.json
type JSONService struct {
	Name        string          `json:"name"`
	PackagePath string          `json:"package_path"`
	ClientType  string          `json:"client_type"`
	Operations  []JSONOperation `json:"operations"`
}

// JSONOperation represents an operation as stored in services.json
type JSONOperation struct {
	Name         string `json:"name"`
	InputType    string `json:"input_type"`
	OutputType   string `json:"output_type"`
	ResourceType string `json:"resource_type"`
	IsList       bool   `json:"is_list"`
	IsPaginated  bool   `json:"is_paginated"`
}

// LoadFromServicesJSON loads service definitions from services.json file
func (r *JSONServiceRegistry) LoadFromServicesJSON(filePath string) error {
	// Read the JSON file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read services.json: %w", err)
	}

	// Parse JSON
	var serviceData JSONServiceData
	if err := json.Unmarshal(data, &serviceData); err != nil {
		return fmt.Errorf("failed to parse services.json: %w", err)
	}

	// Convert JSON services to ServiceDefinitions
	for _, jsonSvc := range serviceData.Services {
		serviceDef := r.convertJSONServiceToDefinition(jsonSvc)
		r.services[serviceDef.Name] = &serviceDef
	}

	r.lastLoaded = time.Now()
	return nil
}

// convertJSONServiceToDefinition converts a JSONService to ServiceDefinition
func (r *JSONServiceRegistry) convertJSONServiceToDefinition(jsonSvc JSONService) ServiceDefinition {
	// Convert operations
	operations := make([]OperationDefinition, 0, len(jsonSvc.Operations))
	resourceTypes := make(map[string]*ResourceTypeDefinition)

	for _, op := range jsonSvc.Operations {
		// Create operation definition
		opDef := OperationDefinition{
			Name:          op.Name,
			DisplayName:   op.Name,
			Description:   fmt.Sprintf("%s operation for %s service", op.Name, jsonSvc.Name),
			InputType:     op.InputType,
			OutputType:    op.OutputType,
			Paginated:     op.IsPaginated,
			OperationType: determineOperationType(op.Name),
		}

		// Set permissions based on operation name
		opDef.RequiredPermissions = []string{
			fmt.Sprintf("%s:%s", jsonSvc.Name, op.Name),
		}

		operations = append(operations, opDef)

		// Create resource type if not exists
		if op.ResourceType != "" {
			if _, exists := resourceTypes[op.ResourceType]; !exists {
				resourceTypes[op.ResourceType] = &ResourceTypeDefinition{
					Name:               op.ResourceType,
					DisplayName:        op.ResourceType,
					Description:        fmt.Sprintf("%s resource in %s service", op.ResourceType, jsonSvc.Name),
					ResourceType:       fmt.Sprintf("AWS::%s::%s", strings.ToUpper(jsonSvc.Name), op.ResourceType),
					Paginated:          op.IsPaginated,
					SupportedOperations: []string{},
				}
			}
			// Add operation to resource type
			resourceTypes[op.ResourceType].SupportedOperations = append(
				resourceTypes[op.ResourceType].SupportedOperations, 
				op.Name,
			)

			// Set primary operations
			if op.IsList {
				if resourceTypes[op.ResourceType].ListOperation == "" {
					resourceTypes[op.ResourceType].ListOperation = op.Name
				}
			} else {
				if resourceTypes[op.ResourceType].DescribeOperation == "" {
					resourceTypes[op.ResourceType].DescribeOperation = op.Name
				}
			}
		}
	}

	// Convert resource types map to slice
	resourceTypeSlice := make([]ResourceTypeDefinition, 0, len(resourceTypes))
	for _, rt := range resourceTypes {
		resourceTypeSlice = append(resourceTypeSlice, *rt)
	}

	// Set default rate limits based on service
	rateLimit := rate.Limit(20)  // Default
	burstLimit := 40
	
	// Set service-specific rate limits
	switch jsonSvc.Name {
	case "s3":
		rateLimit = rate.Limit(100)
		burstLimit = 200
	case "lambda":
		rateLimit = rate.Limit(50)
		burstLimit = 100
	case "iam":
		rateLimit = rate.Limit(10)
		burstLimit = 20
	case "dynamodb":
		rateLimit = rate.Limit(25)
		burstLimit = 50
	}

	// Determine if service is global
	globalService := jsonSvc.Name == "iam" || jsonSvc.Name == "s3" || jsonSvc.Name == "cloudfront"

	return ServiceDefinition{
		Name:        jsonSvc.Name,
		DisplayName: generateDisplayName(jsonSvc.Name),
		Description: fmt.Sprintf("AWS %s service", strings.ToUpper(jsonSvc.Name)),
		PackagePath: jsonSvc.PackagePath,
		ClientType:  jsonSvc.ClientType,
		
		ResourceTypes: resourceTypeSlice,
		Operations:    operations,
		
		RateLimit:  rateLimit,
		BurstLimit: burstLimit,
		
		SupportsPagination:       hasPaginatedOperations(jsonSvc.Operations),
		SupportsResourceExplorer: true, // Most services support resource explorer
		SupportsParallelScan:     true,
		GlobalService:            globalService,
		RequiresRegion:           !globalService,
		
		DiscoveredAt:     time.Now(),
		DiscoverySource:  "services.json",
		DiscoveryVersion: "v2-latest",
		LastValidated:    time.Now(),
		ValidationStatus: "valid",
	}
}

// Helper functions
func determineOperationType(opName string) string {
	opName = strings.ToLower(opName)
	switch {
	case strings.HasPrefix(opName, "list"):
		return "List"
	case strings.HasPrefix(opName, "describe"):
		return "Describe"
	case strings.HasPrefix(opName, "get"):
		return "Get"
	case strings.HasPrefix(opName, "create"):
		return "Create"
	case strings.HasPrefix(opName, "update"):
		return "Update"
	case strings.HasPrefix(opName, "delete"):
		return "Delete"
	default:
		return "Other"
	}
}

func generateDisplayName(serviceName string) string {
	switch serviceName {
	case "s3":
		return "Amazon S3"
	case "ec2":
		return "Amazon EC2"
	case "iam":
		return "AWS IAM"
	case "lambda":
		return "AWS Lambda"
	case "rds":
		return "Amazon RDS"
	case "dynamodb":
		return "Amazon DynamoDB"
	case "kms":
		return "AWS KMS"
	default:
		return fmt.Sprintf("AWS %s", strings.ToUpper(serviceName))
	}
}

func hasPaginatedOperations(operations []JSONOperation) bool {
	for _, op := range operations {
		if op.IsPaginated {
			return true
		}
	}
	return false
}

// Implement DynamicServiceRegistry interface

func (r *JSONServiceRegistry) RegisterService(def ServiceDefinition) error {
	r.services[def.Name] = &def
	return nil
}

func (r *JSONServiceRegistry) GetService(name string) (*ServiceDefinition, bool) {
	service, exists := r.services[name]
	return service, exists
}

func (r *JSONServiceRegistry) ListServices() []string {
	services := make([]string, 0, len(r.services))
	for name := range r.services {
		services = append(services, name)
	}
	return services
}

func (r *JSONServiceRegistry) ListServiceDefinitions() []ServiceDefinition {
	definitions := make([]ServiceDefinition, 0, len(r.services))
	for _, def := range r.services {
		definitions = append(definitions, *def)
	}
	return definitions
}

func (r *JSONServiceRegistry) UpdateService(name string, updates func(*ServiceDefinition) error) error {
	service, exists := r.services[name]
	if !exists {
		return fmt.Errorf("service %s not found", name)
	}
	return updates(service)
}

func (r *JSONServiceRegistry) RemoveService(name string) error {
	delete(r.services, name)
	return nil
}

func (r *JSONServiceRegistry) RegisterServices(defs []ServiceDefinition) error {
	for _, def := range defs {
		r.services[def.Name] = &def
	}
	return nil
}

func (r *JSONServiceRegistry) GetServices(names []string) ([]*ServiceDefinition, error) {
	services := make([]*ServiceDefinition, 0, len(names))
	for _, name := range names {
		if service, exists := r.services[name]; exists {
			services = append(services, service)
		}
	}
	return services, nil
}

func (r *JSONServiceRegistry) FilterServices(filter ServiceFilter) []ServiceDefinition {
	// Simple implementation - can be enhanced
	definitions := r.ListServiceDefinitions()
	
	if len(filter.Names) > 0 {
		filtered := make([]ServiceDefinition, 0)
		nameSet := make(map[string]bool)
		for _, name := range filter.Names {
			nameSet[name] = true
		}
		
		for _, def := range definitions {
			if nameSet[def.Name] {
				filtered = append(filtered, def)
			}
		}
		definitions = filtered
	}
	
	return definitions
}

func (r *JSONServiceRegistry) PopulateFromDiscovery(discovered []*pb.ServiceInfo) error {
	// Not implemented for JSON adapter
	return fmt.Errorf("PopulateFromDiscovery not supported for JSONServiceRegistry")
}

func (r *JSONServiceRegistry) PopulateFromReflection(serviceClients map[string]interface{}) error {
	// Not implemented for JSON adapter
	return fmt.Errorf("PopulateFromReflection not supported for JSONServiceRegistry")
}

func (r *JSONServiceRegistry) MergeWithExisting(def ServiceDefinition) error {
	r.services[def.Name] = &def
	return nil
}

func (r *JSONServiceRegistry) PersistToFile(path string) error {
	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Convert to JSON format
	serviceData := JSONServiceData{
		Version:    "1.0.0",
		AnalyzedAt: time.Now().Format(time.RFC3339),
		SDKVersion: "v2-latest",
		Services:   make([]JSONService, 0, len(r.services)),
	}

	for _, def := range r.services {
		jsonService := JSONService{
			Name:        def.Name,
			PackagePath: def.PackagePath,
			ClientType:  def.ClientType,
			Operations:  make([]JSONOperation, 0, len(def.Operations)),
		}

		for _, op := range def.Operations {
			jsonService.Operations = append(jsonService.Operations, JSONOperation{
				Name:         op.Name,
				InputType:    op.InputType,
				OutputType:   op.OutputType,
				IsList:       op.OperationType == "List",
				IsPaginated:  op.Paginated,
			})
		}

		serviceData.Services = append(serviceData.Services, jsonService)
	}

	// Write to file
	data, err := json.MarshalIndent(serviceData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (r *JSONServiceRegistry) LoadFromFile(path string) error {
	return r.LoadFromServicesJSON(path)
}

func (r *JSONServiceRegistry) LoadFromDirectory(dir string) error {
	servicesPath := filepath.Join(dir, "services.json")
	return r.LoadFromServicesJSON(servicesPath)
}

func (r *JSONServiceRegistry) ExportServices(services []string, path string) error {
	// Filter services and export
	filtered := r.FilterServices(ServiceFilter{Names: services})
	
	tempRegistry := NewJSONServiceRegistry()
	if err := tempRegistry.RegisterServices(filtered); err != nil {
		return err
	}
	
	return tempRegistry.PersistToFile(path)
}

func (r *JSONServiceRegistry) GetStats() RegistryStats {
	totalResourceTypes := 0
	totalOperations := 0
	
	for _, def := range r.services {
		totalResourceTypes += len(def.ResourceTypes)
		totalOperations += len(def.Operations)
	}
	
	return RegistryStats{
		TotalServices:      len(r.services),
		TotalResourceTypes: totalResourceTypes,
		TotalOperations:    totalOperations,
		LastUpdated:        r.lastLoaded,
		ServicesBySource:   map[string]int{"services.json": len(r.services)},
		ServicesByStatus:   map[string]int{"valid": len(r.services)},
	}
}

func (r *JSONServiceRegistry) ValidateRegistry() []error {
	var errors []error
	
	for name, def := range r.services {
		if def.PackagePath == "" {
			errors = append(errors, fmt.Errorf("service %s missing package path", name))
		}
		if def.ClientType == "" {
			errors = append(errors, fmt.Errorf("service %s missing client type", name))
		}
		if len(def.Operations) == 0 {
			errors = append(errors, fmt.Errorf("service %s has no operations", name))
		}
	}
	
	return errors
}

func (r *JSONServiceRegistry) GetServiceHealth(name string) error {
	if _, exists := r.services[name]; !exists {
		return fmt.Errorf("service %s not found", name)
	}
	return nil
}

func (r *JSONServiceRegistry) GetConfig() RegistryConfig {
	return r.config
}

func (r *JSONServiceRegistry) UpdateConfig(config RegistryConfig) error {
	r.config = config
	return nil
}
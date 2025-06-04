package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
)

// BuildTagManager manages build tags for conditional compilation
type BuildTagManager struct {
	registry     registry.DynamicServiceRegistry
	outputDir    string
	baseTag      string
	serviceTags  map[string]string
}

// NewBuildTagManager creates a new build tag manager
func NewBuildTagManager(reg registry.DynamicServiceRegistry, outputDir string) *BuildTagManager {
	return &BuildTagManager{
		registry:    reg,
		outputDir:   outputDir,
		baseTag:     "aws_services",
		serviceTags: make(map[string]string),
	}
}

// GenerateBuildConfiguration generates build configuration files
func (m *BuildTagManager) GenerateBuildConfiguration() error {
	services := m.registry.ListServiceDefinitions()
	
	// Generate main build file
	if err := m.generateMainBuildFile(services); err != nil {
		return fmt.Errorf("failed to generate main build file: %w", err)
	}

	// Generate per-service build files
	if err := m.generateServiceBuildFiles(services); err != nil {
		return fmt.Errorf("failed to generate service build files: %w", err)
	}

	// Generate Makefile targets
	if err := m.generateMakefileTargets(services); err != nil {
		return fmt.Errorf("failed to generate Makefile targets: %w", err)
	}

	return nil
}

// generateMainBuildFile creates the main build configuration
func (m *BuildTagManager) generateMainBuildFile(services []registry.ServiceDefinition) error {
	tmpl := template.Must(template.New("buildConfig").Parse(mainBuildTemplate))

	data := struct {
		BaseTag  string
		Services []registry.ServiceDefinition
		Tags     map[string]string
	}{
		BaseTag:  m.baseTag,
		Services: services,
		Tags:     m.generateServiceTags(services),
	}

	outputPath := filepath.Join(m.outputDir, "build_config.go")
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

// generateServiceBuildFiles creates individual service build files
func (m *BuildTagManager) generateServiceBuildFiles(services []registry.ServiceDefinition) error {
	serviceDir := filepath.Join(m.outputDir, "services")
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return err
	}

	for _, service := range services {
		if err := m.generateServiceFile(service, serviceDir); err != nil {
			return fmt.Errorf("failed to generate file for %s: %w", service.Name, err)
		}
	}

	return nil
}

// generateServiceFile creates a build file for a specific service
func (m *BuildTagManager) generateServiceFile(service registry.ServiceDefinition, outputDir string) error {
	tag := m.getServiceTag(service.Name)
	
	tmpl := template.Must(template.New("service").Parse(serviceBuildTemplate))

	data := struct {
		Service  registry.ServiceDefinition
		BuildTag string
		BaseTag  string
	}{
		Service:  service,
		BuildTag: tag,
		BaseTag:  m.baseTag,
	}

	filename := fmt.Sprintf("%s_client.go", service.Name)
	outputPath := filepath.Join(outputDir, filename)
	
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

// generateMakefileTargets creates Makefile targets for different build configurations
func (m *BuildTagManager) generateMakefileTargets(services []registry.ServiceDefinition) error {
	tmpl := template.Must(template.New("makefile").Parse(makefileTemplate))

	// Group services by common characteristics
	groups := m.groupServices(services)

	data := struct {
		BaseTag       string
		AllServices   []string
		ServiceGroups map[string][]string
	}{
		BaseTag:       m.baseTag,
		AllServices:   m.getServiceNames(services),
		ServiceGroups: groups,
	}

	outputPath := filepath.Join(m.outputDir, "Makefile.services")
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

// Helper methods

func (m *BuildTagManager) generateServiceTags(services []registry.ServiceDefinition) map[string]string {
	tags := make(map[string]string)
	for _, service := range services {
		tags[service.Name] = m.getServiceTag(service.Name)
	}
	return tags
}

func (m *BuildTagManager) getServiceTag(serviceName string) string {
	if tag, exists := m.serviceTags[serviceName]; exists {
		return tag
	}
	tag := fmt.Sprintf("aws_%s", serviceName)
	m.serviceTags[serviceName] = tag
	return tag
}

func (m *BuildTagManager) getServiceNames(services []registry.ServiceDefinition) []string {
	names := make([]string, len(services))
	for i, service := range services {
		names[i] = service.Name
	}
	return names
}

func (m *BuildTagManager) groupServices(services []registry.ServiceDefinition) map[string][]string {
	groups := map[string][]string{
		"core":      {},
		"compute":   {},
		"storage":   {},
		"database":  {},
		"network":   {},
		"analytics": {},
		"ml":        {},
		"security":  {},
		"other":     {},
	}

	for _, service := range services {
		group := m.categorizeService(service)
		groups[group] = append(groups[group], service.Name)
	}

	return groups
}

func (m *BuildTagManager) categorizeService(service registry.ServiceDefinition) string {
	name := strings.ToLower(service.Name)
	
	// Categorize based on service name patterns
	switch {
	case contains([]string{"s3", "efs", "fsx", "backup"}, name):
		return "storage"
	case contains([]string{"ec2", "ecs", "eks", "lambda", "batch"}, name):
		return "compute"
	case contains([]string{"rds", "dynamodb", "elasticache", "redshift"}, name):
		return "database"
	case contains([]string{"vpc", "elb", "route53", "cloudfront"}, name):
		return "network"
	case contains([]string{"athena", "emr", "kinesis", "glue"}, name):
		return "analytics"
	case contains([]string{"sagemaker", "comprehend", "rekognition"}, name):
		return "ml"
	case contains([]string{"iam", "kms", "secretsmanager", "guardduty"}, name):
		return "security"
	case contains([]string{"iam", "sts", "organizations"}, name):
		return "core"
	default:
		return "other"
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Templates

const mainBuildTemplate = `// Code generated by build_tags.go. DO NOT EDIT.
// +build {{ .BaseTag }}

package main

// BuildConfiguration defines which services are included in this build
var BuildConfiguration = struct {
	BaseTag      string
	EnabledTags  []string
	ServiceTags  map[string]string
}{
	BaseTag: "{{ .BaseTag }}",
	EnabledTags: []string{
		{{ range .Services }}"{{ $.Tags.Name }}",
		{{ end }}
	},
	ServiceTags: map[string]string{
		{{ range $name, $tag := .Tags }}"{{ $name }}": "{{ $tag }}",
		{{ end }}
	},
}

// IsServiceEnabled checks if a service is enabled in this build
func IsServiceEnabled(serviceName string) bool {
	_, exists := BuildConfiguration.ServiceTags[serviceName]
	return exists
}

// GetEnabledServices returns all enabled services
func GetEnabledServices() []string {
	services := make([]string, 0, len(BuildConfiguration.ServiceTags))
	for service := range BuildConfiguration.ServiceTags {
		services = append(services, service)
	}
	return services
}
`

const serviceBuildTemplate = `// Code generated by build_tags.go. DO NOT EDIT.
// +build {{ .BaseTag }},{{ .BuildTag }}

package services

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	{{ if .Service.PackagePath }}{{ extractPackageName .Service.PackagePath }} "{{ .Service.PackagePath }}"{{ end }}
)

// init registers the {{ .Service.Name }} service
func init() {
	RegisterService("{{ .Service.Name }}", ServiceRegistration{
		Name:        "{{ .Service.Name }}",
		DisplayName: "{{ .Service.DisplayName }}",
		CreateFunc: func(cfg aws.Config) interface{} {
			{{ if .Service.PackagePath }}return {{ extractPackageName .Service.PackagePath }}.NewFromConfig(cfg){{ else }}return nil{{ end }}
		},
		RateLimit:  {{ .Service.RateLimit }},
		BurstLimit: {{ .Service.BurstLimit }},
	})
}
`

const makefileTemplate = `# Generated Makefile for AWS service builds
# Base tag: {{ .BaseTag }}

# Build with all services
build-all:
	go build -tags "{{ .BaseTag }}" -o corkscrew-aws ./cmd/corkscrew

# Build with minimal services (core only)
build-minimal:
	go build -tags "{{ .BaseTag }},{{ range $i, $svc := .ServiceGroups.core }}{{ if $i }},{{ end }}aws_{{ $svc }}{{ end }}" -o corkscrew-aws-minimal ./cmd/corkscrew

# Build groups
{{ range $group, $services := .ServiceGroups }}
{{ if $services }}
build-{{ $group }}:
	go build -tags "{{ $.BaseTag }},{{ range $i, $svc := $services }}{{ if $i }},{{ end }}aws_{{ $svc }}{{ end }}" -o corkscrew-aws-{{ $group }} ./cmd/corkscrew
{{ end }}
{{ end }}

# Build with specific services (example: make build-custom SERVICES="s3,ec2,lambda")
build-custom:
	@if [ -z "$(SERVICES)" ]; then \
		echo "Error: SERVICES not specified. Example: make build-custom SERVICES=\"s3,ec2,lambda\""; \
		exit 1; \
	fi
	@tags="{{ .BaseTag }}"
	@for svc in $$(echo $(SERVICES) | tr ',' ' '); do \
		tags="$$tags,aws_$$svc"; \
	done
	go build -tags "$$tags" -o corkscrew-aws-custom ./cmd/corkscrew

# List available services
list-services:
	@echo "Available services:"
	{{ range .AllServices }}@echo "  - {{ . }}"
	{{ end }}

# List service groups
list-groups:
	@echo "Service groups:"
	{{ range $group, $services := .ServiceGroups }}{{ if $services }}@echo "  {{ $group }}: {{ join $services ", " }}"
	{{ end }}{{ end }}

# Generate build configurations
generate-configs:
	go run ./cmd/generator/build_config_generator.go

# Clean generated files
clean-generated:
	rm -f build_config.go
	rm -rf services/
	rm -f Makefile.services

.PHONY: build-all build-minimal build-custom list-services list-groups generate-configs clean-generated {{ range $group, $_ := .ServiceGroups }}build-{{ $group }} {{ end }}
`
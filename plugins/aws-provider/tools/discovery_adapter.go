package tools

import (
	"log"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// AnalysisGeneratorAdapter adapts the tools.AnalysisGenerator to work with the discovery interface
type AnalysisGeneratorAdapter struct {
	generator *AnalysisGenerator
}

// NewAnalysisGeneratorAdapter creates a new adapter for the discovery system
func NewAnalysisGeneratorAdapter(outputDir string, clientFactory ClientFactory) (*AnalysisGeneratorAdapter, error) {
	generator, err := NewAnalysisGenerator(outputDir, clientFactory)
	if err != nil {
		return nil, err
	}
	
	// Ensure output directory exists
	if err := generator.EnsureOutputDirectory(); err != nil {
		return nil, err
	}
	
	return &AnalysisGeneratorAdapter{
		generator: generator,
	}, nil
}

// GenerateForService implements the discovery.AnalysisGeneratorInterface
func (a *AnalysisGeneratorAdapter) GenerateForService(serviceName string) error {
	return a.generator.GenerateForService(serviceName)
}

// GenerateForDiscoveredServices implements the discovery.AnalysisGeneratorInterface for discovered services
func (a *AnalysisGeneratorAdapter) GenerateForDiscoveredServices(services []string) error {
	log.Printf("AnalysisGeneratorAdapter: Generating analysis for %d discovered services", len(services))
	return a.generator.GenerateForDiscoveredServices(services)
}

// GenerateForDiscoveredServiceInfos generates analysis files from ServiceInfo objects
func (a *AnalysisGeneratorAdapter) GenerateForDiscoveredServiceInfos(services []*pb.ServiceInfo) error {
	serviceNames := make([]string, len(services))
	for i, service := range services {
		serviceNames[i] = service.Name
	}
	
	log.Printf("AnalysisGeneratorAdapter: Generating analysis for %d ServiceInfo objects", len(services))
	return a.generator.GenerateForDiscoveredServices(serviceNames)
}

// ValidateGeneratedFiles validates all generated analysis files
func (a *AnalysisGeneratorAdapter) ValidateGeneratedFiles() error {
	return a.generator.ValidateAllGeneratedFiles()
}

// GetAnalysisStats returns statistics about generated files
func (a *AnalysisGeneratorAdapter) GetAnalysisStats() (*AnalysisStats, error) {
	return a.generator.GetAnalysisStats()
}

// GetOutputDirectory returns the output directory path
func (a *AnalysisGeneratorAdapter) GetOutputDirectory() string {
	return a.generator.outputDir
}

// GenerateForFilteredServices implements the discovery.AnalysisGeneratorInterface for filtered services
func (a *AnalysisGeneratorAdapter) GenerateForFilteredServices(discoveredServices []*pb.ServiceInfo, requestedServices []string) error {
	// Convert ServiceInfo objects to service names for the discovered services
	discoveredServiceNames := make([]string, len(discoveredServices))
	for i, service := range discoveredServices {
		discoveredServiceNames[i] = service.Name
	}
	
	log.Printf("AnalysisGeneratorAdapter: Generating analysis for %d requested services out of %d discovered", len(requestedServices), len(discoveredServices))
	return a.generator.GenerateForFilteredServices(discoveredServiceNames, requestedServices)
}
package tools

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
	
	return &AnalysisGeneratorAdapter{
		generator: generator,
	}, nil
}

// GenerateForService implements the discovery.AnalysisGeneratorInterface
func (a *AnalysisGeneratorAdapter) GenerateForService(serviceName string) error {
	return a.generator.GenerateForService(serviceName)
}

// GenerateForDiscoveredServices implements the discovery.AnalysisGeneratorInterface
func (a *AnalysisGeneratorAdapter) GenerateForDiscoveredServices(services []string) error {
	return a.generator.GenerateForDiscoveredServices(services)
}
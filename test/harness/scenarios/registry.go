package scenarios

import (
	"fmt"

	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/jlgore/corkscrew/test/harness/scenarios/azure"
	"github.com/jlgore/corkscrew/test/harness/scenarios/gcp"
)

// ScenarioRegistry manages all available test scenarios
type ScenarioRegistry struct {
	scenarios map[string]func() automation.Scenario
}

// NewScenarioRegistry creates a new scenario registry
func NewScenarioRegistry() *ScenarioRegistry {
	registry := &ScenarioRegistry{
		scenarios: make(map[string]func() automation.Scenario),
	}

	// Register all available scenarios
	// AWS scenarios
	registry.Register("simple-s3", func() automation.Scenario { return NewSimpleS3Scenario() })
	registry.Register("network-stack", func() automation.Scenario { return NewNetworkStackScenario() })
	registry.Register("compute-stack", func() automation.Scenario { return NewComputeStackScenario() })
	registry.Register("security-stack", func() automation.Scenario { return NewSecurityStackScenario() })
	registry.Register("storage-stack", func() automation.Scenario { return NewStorageStackScenario() })

	// Azure scenarios
	registry.Register("azure-resource-group", func() automation.Scenario { return azure.NewResourceGroupScenario() })
	registry.Register("azure-vnet", func() automation.Scenario { return azure.NewVNetScenario() })
	registry.Register("azure-identity", func() automation.Scenario { return azure.NewIdentityScenario() })

	// GCP scenarios
	registry.Register("gcp-project", func() automation.Scenario { return gcp.NewProjectScenario() })
	registry.Register("gcp-vpc", func() automation.Scenario { return gcp.NewVPCScenario() })
	registry.Register("gcp-iam", func() automation.Scenario { return gcp.NewIAMScenario() })

	return registry
}

// Register adds a new scenario to the registry
func (r *ScenarioRegistry) Register(name string, factory func() automation.Scenario) {
	r.scenarios[name] = factory
}

// Get retrieves a scenario by name
func (r *ScenarioRegistry) Get(name string) (automation.Scenario, error) {
	factory, exists := r.scenarios[name]
	if !exists {
		return nil, fmt.Errorf("scenario '%s' not found", name)
	}
	return factory(), nil
}

// List returns all available scenario names
func (r *ScenarioRegistry) List() []string {
	names := make([]string, 0, len(r.scenarios))
	for name := range r.scenarios {
		names = append(names, name)
	}
	return names
}

// GetScenarioInfo returns detailed information about all scenarios
func (r *ScenarioRegistry) GetScenarioInfo() map[string]ScenarioInfo {
	info := make(map[string]ScenarioInfo)

	for name, factory := range r.scenarios {
		scenario := factory()
		info[name] = ScenarioInfo{
			Name:        scenario.GetName(),
			Services:    scenario.GetServices(),
			Description: getScenarioDescription(name),
		}
	}

	return info
}

// ScenarioInfo contains metadata about a scenario
type ScenarioInfo struct {
	Name        string   `json:"name"`
	Services    []string `json:"services"`
	Description string   `json:"description"`
}

// getScenarioDescription returns a human-readable description of the scenario
func getScenarioDescription(name string) string {
	descriptions := map[string]string{
		// AWS scenarios
		"simple-s3":      "Creates a single S3 bucket with versioning and encryption for basic testing",
		"network-stack":  "Creates a complete VPC setup with public/private subnets, security groups, NAT gateway, and routing",
		"compute-stack":  "Creates EC2 instances, auto-scaling groups, launch templates, and classic load balancer",
		"security-stack": "Creates IAM roles, policies, users, groups, KMS keys, and secrets manager resources",
		"storage-stack":  "Creates multiple storage types: S3 buckets with different configurations, EBS volumes, EFS file systems",

		// Azure scenarios
		"azure-resource-group": "Creates Azure resource group with storage account for basic Azure testing",
		"azure-vnet":           "Creates complete Azure virtual network with public/private subnets and network security groups",
		"azure-identity":       "Creates Azure managed identities with role assignments and storage account permissions",

		// GCP scenarios
		"gcp-project": "Creates basic GCP resources including GCS bucket and compute instance with firewall rules",
		"gcp-vpc":     "Creates complete GCP VPC with custom subnets, NAT gateway, and firewall rules",
		"gcp-iam":     "Creates GCP service accounts with IAM bindings and bucket permissions for identity testing",
	}

	if desc, exists := descriptions[name]; exists {
		return desc
	}
	return "No description available"
}

// DefaultRegistry returns the default scenario registry with all built-in scenarios
var DefaultRegistry = NewScenarioRegistry()

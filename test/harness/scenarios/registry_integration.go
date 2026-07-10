//go:build integration

package scenarios

import (
	"github.com/jlgore/corkscrew/test/harness/automation"
	"github.com/jlgore/corkscrew/test/harness/scenarios/azure"
	"github.com/jlgore/corkscrew/test/harness/scenarios/gcp"
)

func init() {
	DefaultRegistry.Register("azure-resource-group", func() automation.Scenario { return azure.NewResourceGroupScenario() })
	DefaultRegistry.Register("azure-vnet", func() automation.Scenario { return azure.NewVNetScenario() })
	DefaultRegistry.Register("azure-identity", func() automation.Scenario { return azure.NewIdentityScenario() })

	DefaultRegistry.Register("gcp-project", func() automation.Scenario { return gcp.NewProjectScenario() })
	DefaultRegistry.Register("gcp-vpc", func() automation.Scenario { return gcp.NewVPCScenario() })
	DefaultRegistry.Register("gcp-iam", func() automation.Scenario { return gcp.NewIAMScenario() })
}

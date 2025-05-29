package main

import (
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
)

func main() {
	// Create the Azure provider which implements the CloudProvider interface
	azureProvider := NewAzureProvider()

	// Serve the plugin using the shared plugin configuration
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: azureProvider},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

package main

import (
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
)

func main() {
	// Create the DynamicAWSProvider which has all the Phase 5 features
	awsProvider := NewDynamicAWSProvider()

	// Serve the plugin
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: awsProvider},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
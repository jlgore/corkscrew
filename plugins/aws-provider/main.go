package main

import (
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
)

func main() {
	awsProvider := NewAWSProvider()
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: awsProvider},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

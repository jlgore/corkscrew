package main

import (
	"os"

	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
)

func main() {
	// If run with --test flag, run the test instead of serving plugin
	if len(os.Args) > 1 && os.Args[1] == "--test" {
		testPlugin()
		return
	}
	
	// If run with --test-aws flag, run real AWS testing
	if len(os.Args) > 1 && os.Args[1] == "--test-aws" {
		testRealAWS()
		return
	}
	
	// If run with --check-explorer flag, check Resource Explorer setup
	if len(os.Args) > 1 && os.Args[1] == "--check-explorer" {
		testResourceExplorerSetup()
		return
	}
	
	// If run with --demo-explorer flag, show Resource Explorer benefits
	if len(os.Args) > 1 && os.Args[1] == "--demo-explorer" {
		testWithResourceExplorerEnabled()
		return
	}

	// Create the new AWS provider v2
	awsProvider := NewAWSProvider()

	// Serve the plugin
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: awsProvider},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
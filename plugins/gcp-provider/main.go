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
	
	// If run with --test-gcp flag, run real GCP testing
	if len(os.Args) > 1 && os.Args[1] == "--test-gcp" {
		testRealGCP()
		return
	}
	
	// If run with --check-asset-inventory flag, check Asset Inventory setup
	if len(os.Args) > 1 && os.Args[1] == "--check-asset-inventory" {
		testAssetInventorySetup()
		return
	}
	
	// If run with --demo flag, run the library analyzer demo
	if len(os.Args) > 1 && os.Args[1] == "--demo" {
		DemoGCPClientLibraryAnalyzer()
		return
	}

	// Create the new GCP provider
	gcpProvider := NewGCPProvider()

	// Serve the plugin
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: gcpProvider},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
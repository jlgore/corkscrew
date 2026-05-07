package main

import (
	"flag"
	"log"

	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
)

func main() {
	testMode := flag.Bool("test", false, "Run provider self-test")
	flag.Parse()

	if *testMode {
		provider := NewGitHubProvider()
		if info, err := provider.GetProviderInfo(nil, nil); err != nil || info.GetName() != "github" {
			log.Fatal("provider self-test failed")
		}
		log.Print("GitHub provider self-test passed")
		return
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: NewGitHubProvider()},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

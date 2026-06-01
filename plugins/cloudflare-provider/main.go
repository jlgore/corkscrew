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
		provider := NewCloudflareProvider()
		info, err := provider.GetProviderInfo(nil, nil)
		if err != nil || info.GetName() != "cloudflare" {
			log.Fatal("cloudflare provider self-test failed")
		}
		log.Print("Cloudflare provider self-test passed")
		return
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: NewCloudflareProvider()},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

package provider

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
)

type HashicorpLauncher struct{}

func (HashicorpLauncher) Launch(executablePath string) (shared.CloudProvider, io.Closer, error) {
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  shared.HandshakeConfig,
		Plugins:          shared.PluginMap,
		Cmd:              exec.Command(executablePath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		SyncStdout:       os.Stdout,
		SyncStderr:       os.Stderr,
	})
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("create plugin RPC client: %w", err)
	}
	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense provider plugin: %w", err)
	}
	providerClient, ok := raw.(shared.CloudProvider)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("provider plugin returned unexpected type")
	}
	return providerClient, pluginProcess{client: client}, nil
}

type pluginProcess struct{ client *plugin.Client }

func (p pluginProcess) Close() error {
	p.client.Kill()
	return nil
}

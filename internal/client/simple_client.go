package client

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
	plugincatalog "github.com/jlgore/corkscrew/pkg/plugins"
)

// PluginClient represents a simple plugin client
type PluginClient struct {
	client   *plugin.Client
	provider shared.CloudProvider
}

// NewPluginClient creates a new plugin client for the specified provider
func NewPluginClient(providerName string) (*PluginClient, error) {
	pluginPath, err := plugincatalog.NewPluginManager().FindPlugin(providerName)
	if err != nil {
		return nil, fmt.Errorf("plugin %s not found. Please run 'corkscrew init' or 'make plugin-%s'", providerName, providerName)
	}

	client, provider, err := startProviderPlugin(pluginPath, []plugin.Protocol{plugin.ProtocolGRPC}, 0)
	if err != nil {
		return nil, err
	}

	return &PluginClient{
		client:   client,
		provider: provider,
	}, nil
}

func startProviderPlugin(pluginPath string, protocols []plugin.Protocol, startTimeout time.Duration) (*plugin.Client, shared.CloudProvider, error) {
	if len(protocols) == 0 {
		protocols = []plugin.Protocol{plugin.ProtocolGRPC}
	}

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  shared.HandshakeConfig,
		Plugins:          shared.PluginMap,
		Cmd:              exec.Command(pluginPath),
		AllowedProtocols: protocols,
		SyncStdout:       os.Stdout,
		SyncStderr:       os.Stderr,
		StartTimeout:     startTimeout,
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("failed to create RPC client: %w", err)
	}

	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("failed to dispense plugin: %w", err)
	}

	provider, ok := raw.(shared.CloudProvider)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("unexpected type from plugin")
	}

	return client, provider, nil
}

// GetProvider returns the cloud provider interface
func (pc *PluginClient) GetProvider() (shared.CloudProvider, error) {
	if pc.provider == nil {
		return nil, fmt.Errorf("provider not initialized")
	}
	return pc.provider, nil
}

// Close closes the plugin client
func (pc *PluginClient) Close() error {
	if pc.client != nil {
		pc.client.Kill()
	}
	return nil
}

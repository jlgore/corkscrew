package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
)

// PluginClient represents a simple plugin client
type PluginClient struct {
	client   *plugin.Client
	provider shared.CloudProvider
}

// NewPluginClient creates a new plugin client for the specified provider
func NewPluginClient(providerName string) (*PluginClient, error) {
	// Find plugin binary
	home, _ := os.UserHomeDir()
	pluginPaths := []string{
		filepath.Join(".", "plugins", providerName+"-provider", providerName+"-provider"),
		filepath.Join(home, ".corkscrew", "plugins", providerName+"-provider"),
		filepath.Join(".", "plugins", providerName+"-provider"),
	}

	var pluginPath string
	for _, path := range pluginPaths {
		if _, err := os.Stat(path); err == nil {
			pluginPath = path
			break
		}
	}

	if pluginPath == "" {
		return nil, fmt.Errorf("plugin %s not found. Please run 'corkscrew init' or 'make plugin-%s'", providerName, providerName)
	}

	// Create plugin client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins:         shared.PluginMap,
		Cmd:             exec.Command(pluginPath),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolGRPC,
		},
		SyncStdout: os.Stdout,
		SyncStderr: os.Stderr,
	})

	// Connect via RPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to create RPC client: %w", err)
	}

	// Request the plugin
	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense plugin: %w", err)
	}

	provider, ok := raw.(shared.CloudProvider)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected type from plugin")
	}

	return &PluginClient{
		client:   client,
		provider: provider,
	}, nil
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
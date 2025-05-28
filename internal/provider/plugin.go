package provider

import (
	"context"
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// CloudProviderPlugin is the plugin implementation for HashiCorp go-plugin
type CloudProviderPlugin struct {
	Impl CloudProvider
}

func (p *CloudProviderPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &CloudProviderRPCServer{Impl: p.Impl}, nil
}

func (p *CloudProviderPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &CloudProviderRPC{client: c}, nil
}

// CloudProviderRPC is the RPC client
type CloudProviderRPC struct {
	client *rpc.Client
}

func (g *CloudProviderRPC) GetProviderName() string {
	var resp string
	err := g.client.Call("Plugin.GetProviderName", new(interface{}), &resp)
	if err != nil {
		return ""
	}
	return resp
}

func (g *CloudProviderRPC) GetSupportedServices(ctx context.Context) ([]ServiceInfo, error) {
	var resp []ServiceInfo
	err := g.client.Call("Plugin.GetSupportedServices", new(interface{}), &resp)
	return resp, err
}

func (g *CloudProviderRPC) DiscoverServices(ctx context.Context, options *DiscoveryOptions) (*DiscoveryResult, error) {
	var resp DiscoveryResult
	err := g.client.Call("Plugin.DiscoverServices", options, &resp)
	return &resp, err
}

func (g *CloudProviderRPC) ScanResources(ctx context.Context, request *ScanRequest) (*ScanResult, error) {
	var resp ScanResult
	err := g.client.Call("Plugin.ScanResources", request, &resp)
	return &resp, err
}

func (g *CloudProviderRPC) Validate(ctx context.Context) error {
	var resp error
	err := g.client.Call("Plugin.Validate", new(interface{}), &resp)
	if err != nil {
		return err
	}
	return resp
}

func (g *CloudProviderRPC) Close() error {
	var resp error
	err := g.client.Call("Plugin.Close", new(interface{}), &resp)
	return err
}

// CloudProviderRPCServer is the RPC server
type CloudProviderRPCServer struct {
	Impl CloudProvider
}

func (s *CloudProviderRPCServer) GetProviderName(args interface{}, resp *string) error {
	*resp = s.Impl.GetProviderName()
	return nil
}

func (s *CloudProviderRPCServer) GetSupportedServices(args interface{}, resp *[]ServiceInfo) error {
	services, err := s.Impl.GetSupportedServices(context.Background())
	*resp = services
	return err
}

func (s *CloudProviderRPCServer) DiscoverServices(args *DiscoveryOptions, resp *DiscoveryResult) error {
	result, err := s.Impl.DiscoverServices(context.Background(), args)
	if result != nil {
		*resp = *result
	}
	return err
}

func (s *CloudProviderRPCServer) ScanResources(args *ScanRequest, resp *ScanResult) error {
	result, err := s.Impl.ScanResources(context.Background(), args)
	if result != nil {
		*resp = *result
	}
	return err
}

func (s *CloudProviderRPCServer) Validate(args interface{}, resp *error) error {
	*resp = s.Impl.Validate(context.Background())
	return nil
}

func (s *CloudProviderRPCServer) Close(args interface{}, resp *error) error {
	*resp = s.Impl.Close()
	return nil
}
package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"google.golang.org/grpc"
)

// HandshakeConfig is used to agree on plugin protocol
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  2,
	MagicCookieKey:   "CORKSCREW_PLUGIN",
	MagicCookieValue: "v2-provider-plugin",
}

// PluginMap is the map of plugins we can dispense
var PluginMap = map[string]plugin.Plugin{
	"provider": &CloudProviderGRPCPlugin{},
}

// CloudProvider is the interface that all provider plugins must implement
type CloudProvider interface {
	// Plugin lifecycle
	Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error)
	GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error)

	// Service discovery and generation
	DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error)
	GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error)

	// Resource operations following Discovery -> List -> Describe pattern
	ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error)
	DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error)

	// Schema and metadata
	GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error)

	// Batch operations
	BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error)
	StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error

	// Enhanced scanning methods to support all Scanner use cases
	ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error)
	GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error)
	StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error

	// Orchestrator integration methods
	ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error)
	AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error)
	GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error)
}

// GRPCClientProvider is an interface for providers that can expose their underlying gRPC client
type GRPCClientProvider interface {
	GetUnderlyingClient() pb.CloudProviderClient
}

// CloudProviderGRPCPlugin implements plugin.GRPCPlugin for provider plugins
type CloudProviderGRPCPlugin struct {
	plugin.Plugin
	Impl CloudProvider
}

func (p *CloudProviderGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterCloudProviderServer(s, &grpcProviderServer{Impl: p.Impl})
	return nil
}

func (p *CloudProviderGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &grpcProviderClient{client: pb.NewCloudProviderClient(c), ctx: ctx}, nil
}


// grpcProviderServer is the gRPC server implementation for CloudProvider
type grpcProviderServer struct {
	pb.UnimplementedCloudProviderServer
	Impl CloudProvider
}

func (s *grpcProviderServer) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	return s.Impl.Initialize(ctx, req)
}

func (s *grpcProviderServer) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return s.Impl.GetProviderInfo(ctx, req)
}

func (s *grpcProviderServer) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	return s.Impl.DiscoverServices(ctx, req)
}

func (s *grpcProviderServer) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	return s.Impl.GenerateServiceScanners(ctx, req)
}

func (s *grpcProviderServer) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	return s.Impl.ListResources(ctx, req)
}

func (s *grpcProviderServer) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	return s.Impl.DescribeResource(ctx, req)
}

func (s *grpcProviderServer) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	return s.Impl.GetSchemas(ctx, req)
}

func (s *grpcProviderServer) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	return s.Impl.BatchScan(ctx, req)
}

func (s *grpcProviderServer) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	return s.Impl.StreamScan(req, stream)
}

func (s *grpcProviderServer) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	return s.Impl.ScanService(ctx, req)
}

func (s *grpcProviderServer) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	return s.Impl.GetServiceInfo(ctx, req)
}

func (s *grpcProviderServer) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	return s.Impl.StreamScanService(req, stream)
}

func (s *grpcProviderServer) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	return s.Impl.ConfigureDiscovery(ctx, req)
}

func (s *grpcProviderServer) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	return s.Impl.AnalyzeDiscoveredData(ctx, req)
}

func (s *grpcProviderServer) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	return s.Impl.GenerateFromAnalysis(ctx, req)
}


// grpcProviderClient is the gRPC client implementation for CloudProvider
type grpcProviderClient struct {
	client pb.CloudProviderClient
	ctx    context.Context
}

// GetUnderlyingClient returns the underlying gRPC client for orchestrator integration
func (c *grpcProviderClient) GetUnderlyingClient() pb.CloudProviderClient {
	return c.client
}

func (c *grpcProviderClient) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	return c.client.Initialize(ctx, req)
}

func (c *grpcProviderClient) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return c.client.GetProviderInfo(ctx, req)
}

func (c *grpcProviderClient) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	return c.client.DiscoverServices(ctx, req)
}

func (c *grpcProviderClient) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	return c.client.GenerateServiceScanners(ctx, req)
}

func (c *grpcProviderClient) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	return c.client.ListResources(ctx, req)
}

func (c *grpcProviderClient) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	return c.client.DescribeResource(ctx, req)
}

func (c *grpcProviderClient) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	return c.client.GetSchemas(ctx, req)
}

func (c *grpcProviderClient) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	return c.client.BatchScan(ctx, req)
}

func (c *grpcProviderClient) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	streamClient, err := c.client.StreamScan(c.ctx, req)
	if err != nil {
		return err
	}

	for {
		resource, err := streamClient.Recv()
		if err != nil {
			break
		}
		if err := stream.Send(resource); err != nil {
			return err
		}
	}

	return nil
}

func (c *grpcProviderClient) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	return c.client.ConfigureDiscovery(ctx, req)
}

func (c *grpcProviderClient) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	return c.client.AnalyzeDiscoveredData(ctx, req)
}

func (c *grpcProviderClient) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	return c.client.GenerateFromAnalysis(ctx, req)
}

func (c *grpcProviderClient) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	return c.client.ScanService(ctx, req)
}

func (c *grpcProviderClient) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	return c.client.GetServiceInfo(ctx, req)
}

func (c *grpcProviderClient) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	streamClient, err := c.client.StreamScanService(c.ctx, req)
	if err != nil {
		return err
	}

	for {
		resource, err := streamClient.Recv()
		if err != nil {
			break
		}
		if err := stream.Send(resource); err != nil {
			return err
		}
	}

	return nil
}


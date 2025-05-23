package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	pb "github.com/jlgore/corkscrew-generator/internal/proto"
	"google.golang.org/grpc"
)

// HandshakeConfig is used to agree on plugin protocol
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "CORKSCREW_PLUGIN",
	MagicCookieValue: "v1-scanner-plugin",
}

// PluginMap is the map of plugins we can dispense
var PluginMap = map[string]plugin.Plugin{
	"scanner": &ScannerGRPCPlugin{},
}

// Scanner is the interface that plugins must implement
type Scanner interface {
	Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error)
	GetSchemas(ctx context.Context, req *pb.Empty) (*pb.SchemaResponse, error)
	GetServiceInfo(ctx context.Context, req *pb.Empty) (*pb.ServiceInfoResponse, error)
	StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error
}

// ScannerGRPCPlugin implements plugin.GRPCPlugin
type ScannerGRPCPlugin struct {
	plugin.Plugin
	Impl Scanner
}

func (p *ScannerGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterScannerServer(s, &grpcServer{Impl: p.Impl})
	return nil
}

func (p *ScannerGRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &grpcClient{client: pb.NewScannerClient(c), ctx: ctx}, nil
}

// grpcServer is the gRPC server implementation
type grpcServer struct {
	pb.UnimplementedScannerServer
	Impl Scanner
}

func (s *grpcServer) Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	return s.Impl.Scan(ctx, req)
}

func (s *grpcServer) GetSchemas(ctx context.Context, req *pb.Empty) (*pb.SchemaResponse, error) {
	return s.Impl.GetSchemas(ctx, req)
}

func (s *grpcServer) GetServiceInfo(ctx context.Context, req *pb.Empty) (*pb.ServiceInfoResponse, error) {
	return s.Impl.GetServiceInfo(ctx, req)
}

func (s *grpcServer) StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	return s.Impl.StreamScan(req, stream)
}

// grpcClient is the gRPC client implementation
type grpcClient struct {
	client pb.ScannerClient
	ctx    context.Context
}

func (c *grpcClient) Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error) {
	return c.client.Scan(ctx, req)
}

func (c *grpcClient) GetSchemas(ctx context.Context, req *pb.Empty) (*pb.SchemaResponse, error) {
	return c.client.GetSchemas(ctx, req)
}

func (c *grpcClient) GetServiceInfo(ctx context.Context, req *pb.Empty) (*pb.ServiceInfoResponse, error) {
	return c.client.GetServiceInfo(ctx, req)
}

func (c *grpcClient) StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error {
	// For client-side streaming, we need to handle this differently
	// This is a simplified implementation - in practice you'd want proper streaming
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

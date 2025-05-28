package client

import (
	"context"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

// ProviderClient wraps the gRPC client to provide a more convenient interface
type ProviderClient struct {
	client shared.CloudProvider
}

// NewProviderClient creates a new provider client wrapper
func NewProviderClient(client shared.CloudProvider) *ProviderClient {
	return &ProviderClient{client: client}
}

// Initialize initializes the provider
func (c *ProviderClient) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	return c.client.Initialize(ctx, req)
}

// GetProviderInfo gets provider information
func (c *ProviderClient) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return c.client.GetProviderInfo(ctx, req)
}

// DiscoverServices discovers available services
func (c *ProviderClient) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	return c.client.DiscoverServices(ctx, req)
}

// ListResources lists resources for a service
func (c *ProviderClient) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	return c.client.ListResources(ctx, req)
}

// DescribeResource describes a specific resource
func (c *ProviderClient) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	return c.client.DescribeResource(ctx, req)
}

// BatchScan performs batch scanning
func (c *ProviderClient) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	return c.client.BatchScan(ctx, req)
}

// GetSchemas gets schemas
func (c *ProviderClient) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	return c.client.GetSchemas(ctx, req)
}

// GenerateServiceScanners generates service scanners
func (c *ProviderClient) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	return c.client.GenerateServiceScanners(ctx, req)
}
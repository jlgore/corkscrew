// Package providers implements vertical provider inspection and inventory workflows.
package providers

import (
	"context"
	"fmt"

	pb "github.com/jlgore/corkscrew/internal/proto"
	providerRuntime "github.com/jlgore/corkscrew/internal/provider"
)

type Runtime interface {
	List() []providerRuntime.Descriptor
	Open(context.Context, string, map[string]string) (providerRuntime.Session, error)
}

type Info struct {
	Descriptor providerRuntime.Descriptor
	Runtime    *pb.ProviderInfoResponse
}

func List(runtime Runtime) []providerRuntime.Descriptor {
	return runtime.List()
}

func GetInfo(ctx context.Context, runtime Runtime, name string, config map[string]string) (Info, error) {
	session, err := runtime.Open(ctx, name, config)
	if err != nil {
		return Info{}, err
	}
	info, err := session.Provider().GetProviderInfo(ctx, &pb.Empty{})
	if err != nil {
		return Info{}, fmt.Errorf("get provider %q info: %w", name, err)
	}
	return Info{Descriptor: session.Descriptor(), Runtime: info}, nil
}

func DiscoverServices(ctx context.Context, runtime Runtime, name string, config map[string]string, request *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	session, err := runtime.Open(ctx, name, config)
	if err != nil {
		return nil, err
	}
	if err := session.Require(providerRuntime.CapabilityDiscover); err != nil {
		return nil, err
	}
	response, err := session.Provider().DiscoverServices(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("discover %q services: %w", name, err)
	}
	return response, nil
}

func ListResources(ctx context.Context, runtime Runtime, name string, config map[string]string, request *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	session, err := runtime.Open(ctx, name, config)
	if err != nil {
		return nil, err
	}
	if err := session.Require(providerRuntime.CapabilityList); err != nil {
		return nil, err
	}
	response, err := session.Provider().ListResources(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("list %q resources: %w", name, err)
	}
	return response, nil
}

func DescribeResource(ctx context.Context, runtime Runtime, name string, config map[string]string, request *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	session, err := runtime.Open(ctx, name, config)
	if err != nil {
		return nil, err
	}
	if err := session.Require(providerRuntime.CapabilityDescribe); err != nil {
		return nil, err
	}
	response, err := session.Provider().DescribeResource(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("describe %q resource: %w", name, err)
	}
	return response, nil
}

func GetSchemas(ctx context.Context, runtime Runtime, name string, config map[string]string, request *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	session, err := runtime.Open(ctx, name, config)
	if err != nil {
		return nil, err
	}
	if err := session.Require(providerRuntime.CapabilitySchemas); err != nil {
		return nil, err
	}
	response, err := session.Provider().GetSchemas(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get %q schemas: %w", name, err)
	}
	return response, nil
}

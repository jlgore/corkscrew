package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-plugin"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var version = "dev"

type fixtureProvider struct {
	shared.CloudProvider
	failScope string
}

func (p *fixtureProvider) Initialize(_ context.Context, request *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	stateDirectory := request.Config["state_dir"]
	session := request.Config["session"]
	if stateDirectory == "" || session == "" {
		return &pb.InitializeResponse{Success: false, Error: "state_dir and session are required"}, nil
	}
	event, err := json.Marshal(struct {
		PID     int               `json:"pid"`
		Config  map[string]string `json:"config"`
		Session string            `json:"session"`
	}{PID: os.Getpid(), Config: request.Config, Session: session})
	if err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(stateDirectory, "initializations.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return &pb.InitializeResponse{Success: false, Error: fmt.Sprintf("open state log: %v", err)}, nil
	}
	if _, err := log.Write(append(event, '\n')); err != nil {
		_ = log.Close()
		return &pb.InitializeResponse{Success: false, Error: fmt.Sprintf("write state log: %v", err)}, nil
	}
	if err := log.Close(); err != nil {
		return &pb.InitializeResponse{Success: false, Error: fmt.Sprintf("close state log: %v", err)}, nil
	}
	p.failScope = request.Config["fail_scope"]
	return &pb.InitializeResponse{Success: true, Version: version}, nil
}

func (*fixtureProvider) GetProviderInfo(context.Context, *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:              "fixture-cloud",
		Version:           version,
		SupportedServices: []string{"widgets"},
		Capabilities: map[string]string{
			"discover":   "true",
			"list":       "true",
			"describe":   "true",
			"schemas":    "true",
			"batch_scan": "true",
		},
		Description: "Hermetic protocol-v2 fixture provider",
	}, nil
}

func (*fixtureProvider) DiscoverServices(context.Context, *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	return &pb.DiscoverServicesResponse{
		Services: []*pb.ServiceInfo{{
			Name:        "widgets",
			DisplayName: "Fixture Widgets",
			PackageName: "fixture/widgets",
			ClientType:  "hermetic",
			ResourceTypes: []*pb.ResourceType{{
				Name: "widget", TypeName: "widget", ListOperation: "ListWidgets",
				DescribeOperation: "DescribeWidget", IdField: "id", NameField: "name",
				SupportsTags: true,
			}},
		}},
		DiscoveredAt: timestamppb.New(time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)),
		SdkVersion:   "fixture-sdk-1",
	}, nil
}

func (*fixtureProvider) ListResources(_ context.Context, request *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	scope := request.Region
	if scope == "" {
		scope = "scope-a"
	}
	resources := []*pb.ResourceRef{
		resourceRef("fixture://shared", "Shared Widget", "global"),
		resourceRef("fixture://"+scope+"/widget", "Widget "+scope, scope),
	}
	return &pb.ListResourcesResponse{Resources: resources, TotalCount: int32(len(resources)), Metadata: map[string]string{"source": "fixture"}}, nil
}

func (*fixtureProvider) DescribeResource(_ context.Context, request *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	if request.ResourceRef == nil {
		return &pb.DescribeResourceResponse{Error: "resource_ref is required"}, nil
	}
	resource := describedResource(request.ResourceRef.Id, request.ResourceRef.Name, request.ResourceRef.Region)
	if !request.IncludeRelationships {
		resource.Relationships = nil
	}
	if !request.IncludeTags {
		resource.Tags = nil
	}
	return &pb.DescribeResourceResponse{Resource: resource}, nil
}

func (*fixtureProvider) GetSchemas(context.Context, *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	return &pb.SchemaResponse{Schemas: []*pb.Schema{{
		Name: "fixture_widgets", Service: "widgets", ResourceType: "widget",
		Description: "Fixture provider discovery schema",
		Metadata:    map[string]string{"format": "json", "owner": "fixture-provider"},
	}}}, nil
}

func (p *fixtureProvider) BatchScan(_ context.Context, request *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	if p.failScope != "" && request.Region == p.failScope {
		return nil, fmt.Errorf("intentional fixture failure for scope %s", request.Region)
	}
	shared := describedResource("fixture://shared", "Shared Widget", "global")
	scoped := describedResource("fixture://"+request.Region+"/widget", "Widget "+request.Region, request.Region)
	if !request.IncludeRelationships {
		scoped.Relationships = nil
	}
	return &pb.BatchScanResponse{
		Resources: []*pb.Resource{shared, scoped},
		Stats: &pb.ScanStats{
			TotalResources: 2,
			ResourceCounts: map[string]int32{"widget": 2},
			ServiceCounts:  map[string]int32{"widgets": 2},
		},
	}, nil
}

func resourceRef(id, name, scope string) *pb.ResourceRef {
	return &pb.ResourceRef{
		Id: id, Name: name, Type: "widget", Service: "widgets", Region: scope,
		AccountId: "fixture-account", BasicAttributes: map[string]string{"status": "active"},
	}
}

func describedResource(id, name, scope string) *pb.Resource {
	if name == "" {
		name = id
	}
	resource := &pb.Resource{
		Provider: "fixture-cloud", Service: "widgets", Type: "widget", Id: id, Name: name,
		Region: scope, AccountId: "fixture-account", Arn: id,
		Tags:         map[string]string{"fixture": "true"},
		DiscoveredAt: timestamppb.New(time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)),
		RawData:      `{"source":"fixture"}`,
		Attributes:   `{"deterministic":true}`,
	}
	if id != "fixture://shared" {
		resource.Relationships = []*pb.Relationship{{
			TargetId: "fixture://shared", TargetType: "widget", TargetService: "widgets",
			RelationshipType: "uses", Properties: map[string]string{"source": "fixture"},
		}}
	}
	return resource
}

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"provider": &shared.CloudProviderGRPCPlugin{Impl: &fixtureProvider{}},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

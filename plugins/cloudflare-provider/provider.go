package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/providers/cloudflareauth"
	"github.com/jlgore/corkscrew/internal/shared"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	providerVersion   = "0.1.0"
	unsupportedReason = "operation not implemented yet by provider skeleton"
)

type CloudflareProvider struct {
	mu          sync.RWMutex
	initialized bool
	config      *cloudflareauth.CloudflareConfig
	auth        *cloudflareauth.ResolvedAuth
	client      *cloudflare.Client
	planner     cloudflareauth.PermissionPlanner
	listCache   sync.Map
}

func NewCloudflareProvider() *CloudflareProvider {
	return &CloudflareProvider{planner: &cloudflareauth.StaticPermissionPlanner{}}
}

func (p *CloudflareProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cfg, err := cloudflareauth.LoadConfig(req.GetConfig())
	if err != nil {
		return &pb.InitializeResponse{Success: false, Error: err.Error()}, nil
	}

	resolver := &cloudflareauth.DefaultAuthResolver{
		Planner:       p.planner,
		Store:         &cloudflareauth.FileOAuthStore{},
		AllowFallback: true,
		Validate:      true,
	}
	auth, err := resolver.Resolve(ctx, cloudflareauth.ResolveAuthRequest{Config: cfg, Services: cfg.Scan.Services})
	if err != nil {
		return &pb.InitializeResponse{Success: false, Error: err.Error()}, nil
	}

	p.config = cfg
	p.auth = auth
	p.client = newCloudflareClient(auth)
	p.initialized = true

	metadata := map[string]string{
		"auth_method": string(auth.Method),
		"auth_source": auth.Source,
	}
	if len(cfg.Scan.Services) > 0 {
		metadata["requested_services"] = strings.Join(cfg.Scan.Services, ",")
	}

	return &pb.InitializeResponse{Success: true, Version: providerVersion, Metadata: metadata}, nil
}

func (p *CloudflareProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:              "cloudflare",
		Version:           providerVersion,
		SupportedServices: append([]string(nil), cloudflareauth.CanonicalServices...),
		Description:       "Cloudflare provider for edge, Workers, storage, data, and Zero Trust posture inventory",
		Capabilities: map[string]string{
			"discovery":       "true",
			"scanning":        "partial",
			"oauth_planned":   "true",
			"token_auth":      "true",
			"read_only_focus": "true",
		},
	}, nil
}

func (p *CloudflareProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	services := make([]*pb.ServiceInfo, 0, len(cloudflareauth.CanonicalServices))
	for _, service := range filterServices(cloudflareauth.CanonicalServices, req) {
		plan, err := p.planner.Plan([]string{service})
		if err != nil {
			return nil, err
		}
		services = append(services, &pb.ServiceInfo{
			Name:                service,
			DisplayName:         displayName(service),
			PackageName:         service,
			ClientType:          "cloudflare-go/v6",
			ResourceTypes:       resourceTypesForService(service),
			RequiredPermissions: plan.Scopes,
		})
	}

	return &pb.DiscoverServicesResponse{
		Services:     services,
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   "cloudflare-go/v6",
	}, nil
}

func (p *CloudflareProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if err := p.ensureInitialized(); err != nil {
		return nil, err
	}

	service := req.GetService()
	if service == "" {
		service = "zones"
	}

	var (
		resources []*pb.Resource
		errs      []string
	)

	switch service {
	case "accounts":
		resources, errs = p.scanAccounts(ctx)
	case "data":
		resources, errs = p.scanData(ctx)
	case "dns":
		resources, errs = p.scanDNS(ctx)
	case "storage":
		resources, errs = p.scanStorage(ctx)
	case "workers":
		resources, errs = p.scanWorkers(ctx)
	case "zones":
		resources, errs = p.scanZones(ctx)
	default:
		return nil, fmt.Errorf("service %q not implemented yet", service)
	}

	resources = compactResources(resources)
	return &pb.ScanServiceResponse{
		Service:   service,
		Resources: resources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(resources)),
			ResourceCounts: countByType(resources),
			ServiceCounts:  map[string]int32{service: int32(len(resources))},
		},
		Errors: errs,
	}, nil
}

func (p *CloudflareProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if err := p.ensureInitialized(); err != nil {
		return nil, err
	}
	return p.listResourcesPaged(ctx, req)
}

func (p *CloudflareProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	if err := p.ensureInitialized(); err != nil {
		return nil, err
	}
	if req == nil || req.ResourceRef == nil {
		return &pb.DescribeResourceResponse{Error: "resource_ref is required"}, nil
	}
	resource, err := p.describeByType(ctx, req.ResourceRef)
	if err != nil {
		return &pb.DescribeResourceResponse{Error: err.Error()}, nil
	}
	return &pb.DescribeResourceResponse{Resource: resource}, nil
}

func (p *CloudflareProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	if err := p.ensureInitialized(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf(unsupportedReason)
}

func (p *CloudflareProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	if err := p.ensureInitialized(); err != nil {
		return err
	}
	return fmt.Errorf(unsupportedReason)
}

func (p *CloudflareProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if err := p.ensureInitialized(); err != nil {
		return err
	}
	return fmt.Errorf(unsupportedReason)
}

func (p *CloudflareProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	service := req.GetService()
	if service == "" {
		service = "zones"
	}
	plan, err := p.planner.Plan([]string{service})
	if err != nil {
		return nil, err
	}
	return &pb.ServiceInfoResponse{
		ServiceName:         service,
		Version:             providerVersion,
		SupportedResources:  supportedResourcesForService(service),
		RequiredPermissions: plan.Scopes,
		Capabilities: map[string]string{
			"read_only": "true",
		},
	}, nil
}

func (p *CloudflareProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	return &pb.SchemaResponse{Schemas: []*pb.Schema{
		{Name: "cloudflare_resources", Service: "cloudflare", ResourceType: "resource", Description: "Normalized Cloudflare inventory resources", Sql: cloudflareResourceSchema()},
	}}, nil
}

func (p *CloudflareProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	return shared.UnsupportedGenerateScanners(shared.UnsupportedOperationReason("cloudflare", unsupportedReason)), nil
}

func (p *CloudflareProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	return shared.UnsupportedConfigureDiscovery(shared.UnsupportedOperationReason("cloudflare", unsupportedReason)), nil
}

func (p *CloudflareProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	return shared.UnsupportedAnalysis(shared.UnsupportedOperationReason("cloudflare", unsupportedReason)), nil
}

func (p *CloudflareProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	return shared.UnsupportedGenerate(shared.UnsupportedOperationReason("cloudflare", unsupportedReason)), nil
}

func (p *CloudflareProvider) ensureInitialized() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.initialized || p.client == nil {
		return fmt.Errorf("provider not initialized")
	}
	return nil
}

func filterServices(services []string, req *pb.DiscoverServicesRequest) []string {
	include := make(map[string]struct{}, len(req.GetIncludeServices()))
	for _, service := range req.GetIncludeServices() {
		include[service] = struct{}{}
	}
	exclude := make(map[string]struct{}, len(req.GetExcludeServices()))
	for _, service := range req.GetExcludeServices() {
		exclude[service] = struct{}{}
	}

	filtered := make([]string, 0, len(services))
	for _, service := range services {
		if len(include) > 0 {
			if _, ok := include[service]; !ok {
				continue
			}
		}
		if _, ok := exclude[service]; ok {
			continue
		}
		filtered = append(filtered, service)
	}
	return filtered
}

func displayName(service string) string {
	parts := strings.Split(service, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func resourceTypesForService(service string) []*pb.ResourceType {
	resources := supportedResourcesForService(service)
	resourceTypes := make([]*pb.ResourceType, 0, len(resources))
	for _, resource := range resources {
		resourceTypes = append(resourceTypes, &pb.ResourceType{Name: resource, TypeName: resource, IdField: "id", NameField: "name"})
	}
	return resourceTypes
}

func supportedResourcesForService(service string) []string {
	switch service {
	case "accounts":
		return []string{"account"}
	case "data":
		return []string{"d1_database", "durable_object_namespace", "durable_object", "secret_store", "secret_store_secret"}
	case "dns":
		return []string{"dns_record"}
	case "storage":
		return []string{"r2_bucket", "kv_namespace", "queue"}
	case "workers":
		return []string{"worker_script", "worker_route", "worker_domain"}
	case "zones":
		return []string{"zone"}
	default:
		return nil
	}
}

func countByType(resources []*pb.Resource) map[string]int32 {
	counts := make(map[string]int32)
	for _, resource := range resources {
		counts[resource.Type]++
	}
	return counts
}

func compactResources(resources []*pb.Resource) []*pb.Resource {
	compact := resources[:0]
	for _, resource := range resources {
		if resource != nil {
			compact = append(compact, resource)
		}
	}
	return compact
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func cloudflareResourceSchema() string {
	return `CREATE TABLE IF NOT EXISTS cloudflare_resources (
		provider VARCHAR,
		service VARCHAR,
		type VARCHAR,
		id VARCHAR,
		name VARCHAR,
		region VARCHAR,
		account_id VARCHAR,
		parent_id VARCHAR,
		arn VARCHAR,
		tags JSON,
		created_at TIMESTAMP,
		modified_at TIMESTAMP,
		discovered_at TIMESTAMP,
		relationships JSON,
		raw_data JSON,
		attributes JSON
	);`
}

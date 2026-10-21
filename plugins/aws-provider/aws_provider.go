package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/plugins/aws-provider/orgscan"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/scanner"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// buildScanner picks single-account or org-mode based on env vars.
//
//	CORKSCREW_AWS_ORG_SCAN=true              opt into org fan-out
//	CORKSCREW_AWS_ORG_ROLE                   role to assume (default CorkscrewScanRole)
//	CORKSCREW_AWS_ORG_EXTERNAL_ID            sts:ExternalId, optional
//	CORKSCREW_AWS_ORG_INCLUDE_ACCOUNTS       CSV of account IDs to include
//	CORKSCREW_AWS_ORG_EXCLUDE_ACCOUNTS       CSV of account IDs to skip
//	CORKSCREW_AWS_ORG_MAX_CONCURRENCY        bounded parallelism (default 5)
func buildScanner(cfg aws.Config) activeScanner {
	if !envBool("CORKSCREW_AWS_ORG_SCAN") {
		return scanner.NewCloudControlScanner(cfg)
	}

	opts := orgscan.DefaultConfig()
	if v := os.Getenv("CORKSCREW_AWS_ORG_ROLE"); v != "" {
		opts.RoleName = v
	}
	if v := os.Getenv("CORKSCREW_AWS_ORG_EXTERNAL_ID"); v != "" {
		opts.ExternalID = v
	}
	if v := os.Getenv("CORKSCREW_AWS_ORG_INCLUDE_ACCOUNTS"); v != "" {
		opts.IncludeAccounts = splitCSV(v)
	}
	if v := os.Getenv("CORKSCREW_AWS_ORG_EXCLUDE_ACCOUNTS"); v != "" {
		opts.ExcludeAccounts = splitCSV(v)
	}
	if v := os.Getenv("CORKSCREW_AWS_ORG_MAX_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.MaxConcurrentAccounts = n
		}
	}
	log.Printf("AWS Org scan mode active (role=%s, max-concurrency=%d, include=%d, exclude=%d)",
		opts.RoleName, opts.MaxConcurrentAccounts, len(opts.IncludeAccounts), len(opts.ExcludeAccounts))
	return orgscan.New(cfg, opts)
}

func envBool(k string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
	return v == "1" || v == "true" || v == "yes"
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// activeScanner is the surface AWSProvider needs from whichever scanner
// is active — single-account CloudControl, or the org-scan fan-out.
type activeScanner interface {
	SupportedServices() []string
	ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error)
	DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error)
	UnsupportedTypes() map[string]string
}

const unsupportedCodegenReason = "Cloud Control API handles resource discovery dynamically"

// AWSProvider implements pb.CloudProvider on top of the AWS Cloud Control API.
// Resource Explorer is used for discovery when available; CC GetResource
// handles per-resource enrichment. The legacy reflection scanner, codegen
// pipeline, parameter inference, and analysis JSON have all been removed.
type AWSProvider struct {
	mu          sync.RWMutex
	initialized bool
	config      aws.Config

	scanner   activeScanner
	explorer  *ResourceExplorer
	schemaGen *SchemaGenerator

	// regional holds a scanner per region other than config.Region, built
	// on first use: a BatchScanRequest names its region, and the scanner
	// built at Initialize only reaches the region it was configured for.
	regional map[string]activeScanner
	// wireExplorer attaches a region's Resource Explorer default view to a
	// scanner built for that region. A field so tests can stub the lookup.
	wireExplorer func(ctx context.Context, cfg aws.Config, s activeScanner) *ResourceExplorer

	rateLimiter    *rate.Limiter
	maxConcurrency int

	currentProgressTracker *ScanProgressTracker
}

// NewAWSProvider returns an uninitialized provider. Initialize must be called
// before any other method.
func NewAWSProvider() *AWSProvider {
	return &AWSProvider{
		rateLimiter:    rate.NewLimiter(rate.Limit(50), 100),
		maxConcurrency: 10,
	}
}

func (p *AWSProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cfg, authMethod, err := loadAWSConfig(ctx, req.GetConfig(), nil)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	p.config = cfg

	p.scanner = buildScanner(cfg)
	p.schemaGen = NewSchemaGenerator()
	p.regional = map[string]activeScanner{}
	if p.wireExplorer == nil {
		p.wireExplorer = wireResourceExplorer
	}
	p.explorer = p.wireExplorer(ctx, cfg, p.scanner)

	p.initialized = true
	return &pb.InitializeResponse{
		Success: true,
		Version: "4.0.0",
		Metadata: map[string]string{
			"region":            cfg.Region,
			"auth_method":       authMethod,
			"resource_explorer": fmt.Sprintf("%t", p.explorer != nil),
			"scanner_mode":      "cloudcontrol",
		},
	}, nil
}

// wireResourceExplorer looks up the default Resource Explorer view in
// cfg.Region and, when it is healthy, hands it to s for discovery. Returns
// the explorer, or nil when the region has no usable view.
//
// A view only searches the index of its own region unless the account has
// an aggregator index, so every region needs its own lookup.
func wireResourceExplorer(ctx context.Context, cfg aws.Config, s activeScanner) *ResourceExplorer {
	viewArn := defaultViewArn(ctx, cfg)
	if viewArn == "" {
		log.Printf("Resource Explorer view not found in %s; using per-type ListResources", cfg.Region)
		return nil
	}
	explorer := NewResourceExplorer(cfg, viewArn, "")
	if !explorer.IsHealthy(ctx) {
		log.Printf("Resource Explorer view found in %s but unhealthy; ignoring", cfg.Region)
		return nil
	}
	// RE indexes are per-account. In org-scan mode, member-account
	// scanners run without an RE handoff (they fall back to per-type
	// ListResources). Single-account mode wires it through.
	if cc, ok := s.(*scanner.CloudControlScanner); ok {
		cc.SetResourceExplorer(explorer)
	}
	log.Printf("Resource Explorer wired with view: %s", viewArn)
	return explorer
}

// defaultViewArn returns the default view ARN for cfg.Region, or "".
func defaultViewArn(ctx context.Context, cfg aws.Config) string {
	client := resourceexplorer2.NewFromConfig(cfg)
	out, err := client.GetDefaultView(ctx, &resourceexplorer2.GetDefaultViewInput{})
	if err != nil || out.ViewArn == nil {
		return ""
	}
	return *out.ViewArn
}

// scannerFor returns the scanner for region: the one built at Initialize
// when region is empty or the configured region, otherwise one built for
// that region on first use and kept for the rest of the process.
func (p *AWSProvider) scannerFor(ctx context.Context, region string) activeScanner {
	p.mu.RLock()
	base, home := p.scanner, p.config.Region
	cached, ok := p.regional[region]
	p.mu.RUnlock()
	if region == "" || region == home {
		return base
	}
	if ok {
		return cached
	}

	cfg := p.config.Copy()
	cfg.Region = region
	built := buildScanner(cfg)
	p.wireExplorer(ctx, cfg, built)

	p.mu.Lock()
	defer p.mu.Unlock()
	// Two scopes of one region can race here; the first one kept wins.
	if existing, ok := p.regional[region]; ok {
		return existing
	}
	p.regional[region] = built
	return built
}

func (p *AWSProvider) GetProviderInfo(ctx context.Context, _ *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "aws",
		Version:     "4.0.0",
		Description: "AWS provider backed by Cloud Control API",
		Capabilities: shared.WithOptionalCapabilities(map[string]string{
			"discovery":  "cloudformation.ListTypes + Resource Explorer",
			"enrichment": "cloudcontrol.GetResource",
			"vault_auth": "true",
		}, map[string]bool{
			shared.OptionalGenerateServiceScanners: false,
			shared.OptionalConfigureDiscovery:      false,
			shared.OptionalAnalyzeDiscoveredData:   false,
			shared.OptionalGenerateFromAnalysis:    false,
		}),
	}, nil
}

func (p *AWSProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	services := p.scanner.SupportedServices()
	out := make([]*pb.ServiceInfo, 0, len(services))
	for _, s := range services {
		out = append(out, &pb.ServiceInfo{Name: s})
	}
	return &pb.DiscoverServicesResponse{Services: out}, nil
}

func (p *AWSProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	regional := p.scannerFor(ctx, req.Region)
	var refs []*pb.ResourceRef
	var err error
	if req.Service != "" {
		refs, err = regional.ScanService(ctx, req.Service)
	} else {
		for _, svc := range regional.SupportedServices() {
			r, e := regional.ScanService(ctx, svc)
			if e != nil {
				log.Printf("ListResources(%s) error: %v", svc, e)
				continue
			}
			refs = append(refs, r...)
		}
	}
	if err != nil {
		return nil, err
	}
	return &pb.ListResourcesResponse{
		Resources: refs,
		Metadata: map[string]string{
			"resource_count": fmt.Sprintf("%d", len(refs)),
			"scan_time":      time.Now().Format(time.RFC3339),
			"method":         "cloudcontrol",
		},
	}, nil
}

func (p *AWSProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	scanStartTime := time.Now()
	// UnixNano keeps scan IDs unique across rapid back-to-back BatchScan
	// calls (e.g. one per region in multi-region runs). Unix() collided
	// when two regions started in the same second.
	scanID := fmt.Sprintf("scan_%d", scanStartTime.UnixNano())
	p.currentProgressTracker = NewScanProgressTracker(scanID, req.Services)

	stats := &pb.ScanStats{
		ResourceCounts: map[string]int32{},
		ServiceCounts:  map[string]int32{},
	}
	var allResources []*pb.Resource
	var errs []string

	regional := p.scannerFor(ctx, req.Region)
	services := req.Services
	if len(services) == 0 {
		services = regional.SupportedServices()
	}

	// describeConcurrency caps in-flight GetResource calls. Empirically 20 is
	// well under Cloud Control's per-account burst (~100) but high enough to
	// fan out a 1000-resource service in seconds rather than minutes.
	const describeConcurrency = 20

	var statsMu sync.Mutex

	for _, svc := range services {
		p.currentProgressTracker.StartService(svc)
		refs, err := regional.ScanService(ctx, svc)
		if err != nil {
			errs = append(errs, fmt.Sprintf("Service %s: %v", svc, err))
			p.currentProgressTracker.CompleteService(svc, 0, err)
			continue
		}

		// Concurrent GetResource enrichment with bounded parallelism.
		results := make(chan *pb.Resource, len(refs))
		sem := make(chan struct{}, describeConcurrency)
		var wg sync.WaitGroup
		for _, ref := range refs {
			wg.Add(1)
			sem <- struct{}{}
			go func(r *pb.ResourceRef) {
				defer wg.Done()
				defer func() { <-sem }()
				res, derr := regional.DescribeResource(ctx, r)
				if derr != nil {
					log.Printf("Describe %s/%s failed: %v", r.Type, r.Id, derr)
					// Discovery already found the resource; losing it
					// because enrichment failed would make the inventory
					// silently incomplete. Keep what the ref knows, as
					// ScanService does.
					res = unenrichedResource(r, res)
				}
				results <- res
			}(ref)
		}
		wg.Wait()
		close(results)

		for res := range results {
			allResources = append(allResources, res)
			statsMu.Lock()
			stats.ResourceCounts[res.Type]++
			statsMu.Unlock()
		}
		stats.ServiceCounts[svc] = int32(len(refs))
		p.currentProgressTracker.CompleteService(svc, len(refs), nil)
	}
	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(scanStartTime).Milliseconds()

	for typeName, reason := range regional.UnsupportedTypes() {
		errs = append(errs, fmt.Sprintf("unsupported_type:%s: %s", typeName, reason))
	}

	log.Printf("Batch scan %s: %d resources across %d services (%d unsupported types)",
		scanID, len(allResources), len(services), len(regional.UnsupportedTypes()))
	enrichCorrelationEvidence(allResources)

	return &pb.BatchScanResponse{
		Resources: allResources,
		Stats:     stats,
		Errors:    errs,
	}, nil
}

func (p *AWSProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}
	ctx := stream.Context()
	regional := p.scannerFor(ctx, req.Region)
	services := req.Services
	if len(services) == 0 {
		services = regional.SupportedServices()
	}
	for _, svc := range services {
		refs, err := regional.ScanService(ctx, svc)
		if err != nil {
			log.Printf("Stream scan(%s) error: %v", svc, err)
			continue
		}
		for _, ref := range refs {
			res, err := regional.DescribeResource(ctx, ref)
			if err != nil {
				continue
			}
			if err := stream.Send(res); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *AWSProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	return p.schemaGen.GenerateSchemas(req.Services), nil
}

func (p *AWSProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	if req.ResourceRef == nil {
		return &pb.DescribeResourceResponse{Error: "resource_ref is required"}, nil
	}
	res, err := p.scannerFor(ctx, req.ResourceRef.Region).DescribeResource(ctx, req.ResourceRef)
	if err != nil {
		return &pb.DescribeResourceResponse{Error: err.Error()}, nil
	}
	return &pb.DescribeResourceResponse{Resource: res}, nil
}

func (p *AWSProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	regional := p.scannerFor(ctx, req.Region)
	refs, err := regional.ScanService(ctx, req.Service)
	if err != nil {
		return nil, fmt.Errorf("scan service %s: %w", req.Service, err)
	}
	var resources []*pb.Resource
	if req.IncludeRelationships {
		for _, ref := range refs {
			res, err := regional.DescribeResource(ctx, ref)
			if err != nil {
				res = unenrichedResource(ref, res)
			}
			resources = append(resources, res)
		}
	}
	return &pb.ScanServiceResponse{
		Service:   req.Service,
		Resources: resources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(refs)),
			ServiceCounts:  map[string]int32{req.Service: int32(len(refs))},
		},
	}, nil
}

func (p *AWSProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized {
		return nil, fmt.Errorf("provider not initialized")
	}
	return &pb.ServiceInfoResponse{
		ServiceName: req.Service,
	}, nil
}

func (p *AWSProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized {
		return fmt.Errorf("provider not initialized")
	}
	ctx := stream.Context()
	regional := p.scannerFor(ctx, req.Region)
	refs, err := regional.ScanService(ctx, req.Service)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		res, err := regional.DescribeResource(ctx, ref)
		if err != nil {
			continue
		}
		if err := stream.Send(res); err != nil {
			return err
		}
	}
	return nil
}

// The following gRPC methods backed code-generation and analysis pipelines
// that no longer exist. They are kept as compliant stubs so the gRPC
// surface stays stable.

func (p *AWSProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	return shared.UnsupportedGenerateScanners(shared.UnsupportedOperationReason("aws", unsupportedCodegenReason)), nil
}

func (p *AWSProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	return shared.UnsupportedAnalysis(shared.UnsupportedOperationReason("aws", "analysis pipeline removed; Cloud Control returns config directly via GetResource")), nil
}

func (p *AWSProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	return shared.UnsupportedConfigureDiscovery(shared.UnsupportedOperationReason("aws", unsupportedCodegenReason)), nil
}

func (p *AWSProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	return shared.UnsupportedGenerate(shared.UnsupportedOperationReason("aws", unsupportedCodegenReason)), nil
}

func (p *AWSProvider) Cleanup() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.explorer = nil
	p.scanner = nil
	p.regional = nil
	p.initialized = false
	return nil
}

// unenrichedResource is what is known of a resource whose GetResource call
// failed: the partial resource the scanner returned alongside the error,
// if any, otherwise the discovery ref's own fields. A discovered ARN is
// kept as the resource's ARN, since that is what identifies it elsewhere.
func unenrichedResource(ref *pb.ResourceRef, partial *pb.Resource) *pb.Resource {
	res := partial
	if res == nil {
		res = &pb.Resource{
			Provider:     "aws",
			Service:      ref.Service,
			Type:         ref.Type,
			Id:           ref.Id,
			Name:         ref.Name,
			Region:       ref.Region,
			AccountId:    ref.AccountId,
			DiscoveredAt: timestamppb.Now(),
		}
	}
	if res.Arn == "" && strings.HasPrefix(res.Id, "arn:") {
		res.Arn = res.Id
	}
	return res
}

func (p *AWSProvider) GetScanProgress() *ProgressReport {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.currentProgressTracker == nil {
		return nil
	}
	return p.currentProgressTracker.GetProgressReport()
}

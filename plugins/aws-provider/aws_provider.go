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
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	pb "github.com/jlgore/corkscrew/internal/proto"
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

// AWSProvider implements pb.CloudProvider on top of the AWS Cloud Control API.
// Resource Explorer is used for discovery when available; CC GetResource
// handles per-resource enrichment. The legacy reflection scanner, codegen
// pipeline, parameter inference, and analysis JSON have all been removed.
type AWSProvider struct {
	mu          sync.RWMutex
	initialized bool
	config      aws.Config

	scanner       activeScanner
	explorer      *ResourceExplorer
	schemaGen     *SchemaGenerator
	changeStorage *DuckDBChangeStorage

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

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return &pb.InitializeResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to load AWS config: %v", err),
		}, nil
	}
	p.config = cfg

	p.scanner = buildScanner(cfg)
	p.schemaGen = NewSchemaGenerator()

	if cs, err := NewDuckDBChangeStorage("aws_scans.db"); err == nil {
		p.changeStorage = cs
		log.Printf("Database auto-save initialized: aws_scans.db")
	} else {
		log.Printf("Warning: change storage init failed (continuing without auto-save): %v", err)
	}

	if viewArn := p.checkResourceExplorer(ctx); viewArn != "" {
		p.explorer = NewResourceExplorer(cfg, viewArn, "")
		if p.explorer.IsHealthy(ctx) {
			// RE indexes are per-account. In org-scan mode, member-account
			// scanners run without an RE handoff (they fall back to per-
			// type ListResources). Single-account mode wires it through.
			if cc, ok := p.scanner.(*scanner.CloudControlScanner); ok {
				cc.SetResourceExplorer(p.explorer)
			}
			log.Printf("Resource Explorer wired with view: %s", viewArn)
		} else {
			log.Printf("Resource Explorer view found but unhealthy; ignoring")
			p.explorer = nil
		}
	} else {
		log.Printf("Resource Explorer view not found; using per-type ListResources")
	}

	p.initialized = true
	return &pb.InitializeResponse{
		Success: true,
		Version: "4.0.0",
		Metadata: map[string]string{
			"region":            cfg.Region,
			"resource_explorer": fmt.Sprintf("%t", p.explorer != nil),
			"scanner_mode":      "cloudcontrol",
		},
	}, nil
}

// checkResourceExplorer returns the default view ARN for the region, or "".
func (p *AWSProvider) checkResourceExplorer(ctx context.Context) string {
	client := resourceexplorer2.NewFromConfig(p.config)
	out, err := client.GetDefaultView(ctx, &resourceexplorer2.GetDefaultViewInput{})
	if err != nil || out.ViewArn == nil {
		return ""
	}
	return *out.ViewArn
}

func (p *AWSProvider) GetProviderInfo(ctx context.Context, _ *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:        "aws",
		Version:     "4.0.0",
		Description: "AWS provider backed by Cloud Control API",
		Capabilities: map[string]string{
			"discovery":  "cloudformation.ListTypes + Resource Explorer",
			"enrichment": "cloudcontrol.GetResource",
		},
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

	var refs []*pb.ResourceRef
	var err error
	if req.Service != "" {
		refs, err = p.scanner.ScanService(ctx, req.Service)
	} else {
		for _, svc := range p.scanner.SupportedServices() {
			r, e := p.scanner.ScanService(ctx, svc)
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

	services := req.Services
	if len(services) == 0 {
		services = p.scanner.SupportedServices()
	}

	for _, svc := range services {
		p.currentProgressTracker.StartService(svc)
		refs, err := p.scanner.ScanService(ctx, svc)
		if err != nil {
			errs = append(errs, fmt.Sprintf("Service %s: %v", svc, err))
			p.currentProgressTracker.CompleteService(svc, 0, err)
			continue
		}
		for _, ref := range refs {
			res, derr := p.scanner.DescribeResource(ctx, ref)
			if derr != nil {
				log.Printf("Describe %s/%s failed: %v", ref.Type, ref.Id, derr)
				continue
			}
			allResources = append(allResources, res)
			stats.ResourceCounts[res.Type]++
		}
		stats.ServiceCounts[svc] = int32(len(refs))
		p.currentProgressTracker.CompleteService(svc, len(refs), nil)
	}
	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(scanStartTime).Milliseconds()

	if p.changeStorage != nil && len(allResources) > 0 {
		go p.autoSaveScanResults(scanID, scanStartTime, allResources, services)
	}

	for typeName, reason := range p.scanner.UnsupportedTypes() {
		errs = append(errs, fmt.Sprintf("unsupported_type:%s: %s", typeName, reason))
	}

	log.Printf("Batch scan %s: %d resources across %d services (%d unsupported types)",
		scanID, len(allResources), len(services), len(p.scanner.UnsupportedTypes()))

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
	services := req.Services
	if len(services) == 0 {
		services = p.scanner.SupportedServices()
	}
	for _, svc := range services {
		refs, err := p.scanner.ScanService(ctx, svc)
		if err != nil {
			log.Printf("Stream scan(%s) error: %v", svc, err)
			continue
		}
		for _, ref := range refs {
			res, err := p.scanner.DescribeResource(ctx, ref)
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
	res, err := p.scanner.DescribeResource(ctx, req.ResourceRef)
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

	refs, err := p.scanner.ScanService(ctx, req.Service)
	if err != nil {
		return nil, fmt.Errorf("scan service %s: %w", req.Service, err)
	}
	var resources []*pb.Resource
	if req.IncludeRelationships {
		for _, ref := range refs {
			res, err := p.scanner.DescribeResource(ctx, ref)
			if err != nil {
				res = &pb.Resource{
					Service:      ref.Service,
					Type:         ref.Type,
					Id:           ref.Id,
					Name:         ref.Name,
					Region:       ref.Region,
					DiscoveredAt: timestamppb.Now(),
				}
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
	refs, err := p.scanner.ScanService(ctx, req.Service)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		res, err := p.scanner.DescribeResource(ctx, ref)
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
	return &pb.GenerateScannersResponse{
		Errors: []string{"scanner generation removed; Cloud Control API handles all resource types dynamically"},
	}, nil
}

func (p *AWSProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	return &pb.AnalysisResponse{
		Success: false,
		Error:   "analysis pipeline removed; Cloud Control returns config directly via GetResource",
	}, nil
}

func (p *AWSProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	return &pb.ConfigureDiscoveryResponse{Success: true}, nil
}

func (p *AWSProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	return &pb.GenerateResponse{
		Success: false,
		Error:   "code generation removed",
	}, nil
}

func (p *AWSProvider) Cleanup() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.changeStorage != nil {
		_ = p.changeStorage.Close()
		p.changeStorage = nil
	}
	p.explorer = nil
	p.scanner = nil
	p.initialized = false
	return nil
}

func (p *AWSProvider) GetScanProgress() *ProgressReport {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.currentProgressTracker == nil {
		return nil
	}
	return p.currentProgressTracker.GetProgressReport()
}

// autoSaveScanResults saves scan results to the database asynchronously.
func (p *AWSProvider) autoSaveScanResults(scanID string, startTime time.Time, resources []*pb.Resource, services []string) {
	if p.changeStorage == nil {
		return
	}

	events := make([]*ChangeEvent, 0, len(resources))
	for _, r := range resources {
		events = append(events, &ChangeEvent{
			ID:           fmt.Sprintf("%s_%s", scanID, r.Id),
			Provider:     "aws",
			ResourceID:   r.Id,
			ResourceName: r.Name,
			ResourceType: r.Type,
			Service:      r.Service,
			Region:       r.Region,
			ChangeType:   ChangeTypeCreate,
			Severity:     SeverityLow,
			Timestamp:    startTime,
			DetectedAt:   time.Now(),
			CurrentState: &ResourceState{
				ResourceID: r.Id,
				Timestamp:  time.Now(),
				Properties: map[string]interface{}{
					"arn":           r.Arn,
					"resource_type": r.Type,
					"region":        r.Region,
				},
				Tags: r.Tags,
			},
			ChangeMetadata: map[string]interface{}{
				"scan_id":  scanID,
				"services": services,
			},
		})
	}
	if err := p.changeStorage.StoreChanges(events); err != nil {
		log.Printf("autoSaveScanResults: %v", err)
	}
}

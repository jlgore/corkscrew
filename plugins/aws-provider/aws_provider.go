package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourceexplorer2"
	"github.com/jlgore/corkscrew/internal/assert"
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
	// ScanService returns the refs it observed even when it also returns an
	// error. The error, often an errors.Join of one failure per type or
	// account, means the service was not fully observed.
	ScanService(ctx context.Context, serviceName string) ([]*pb.ResourceRef, error)
	DescribeResource(ctx context.Context, ref *pb.ResourceRef) (*pb.Resource, error)
	UnsupportedTypes() map[string]string
	// DiscoveryErr is non-nil while dynamic type discovery has failed and
	// scans use the curated fallback map, which misses most resource types.
	DiscoveryErr() error
}

// forScan scopes s to one scan of several services. In org mode that lists
// the organization's accounts once for the scan rather than once per service;
// a single-account scanner has nothing to scope and is returned as is.
func forScan(s activeScanner) activeScanner {
	if org, ok := s.(*orgscan.OrgScanner); ok {
		return org.ForScan()
	}
	return s
}

// serviceErrorEntries turns a ScanService error into BatchScanResponse
// Errors entries, one per failed type or account, so each is visible on
// its own. An access denial is a coverage notice (the credential's scope, an
// IAM fix); every other failure is an error, since resources may be missing.
func serviceErrorEntries(service string, err error) []string {
	leaves := scanner.FlattenErrors(err)
	entries := make([]string, 0, len(leaves))
	for _, e := range leaves {
		var denied *scanner.AccessDeniedError
		if errors.As(e, &denied) {
			entries = append(entries, shared.AccessDeniedNotice(denied.Type, denied.Err))
			continue
		}
		entries = append(entries, shared.ServiceScanError(service, e))
	}
	return entries
}

// discoveryErrorEntries reports a failed type discovery as an error: the
// curated fallback leaves most types unscanned, so the scope is incomplete.
func discoveryErrorEntries(s activeScanner) []string {
	err := s.DiscoveryErr()
	if err == nil {
		return nil
	}
	return []string{shared.ServiceScanError("discovery", err)}
}

const unsupportedCodegenReason = "Cloud Control API handles resource discovery dynamically"

// AWSProvider implements pb.CloudProvider on top of the AWS Cloud Control API.
// Resource Explorer is used for discovery when available; CC GetResource
// handles per-resource enrichment. The legacy reflection scanner, codegen
// pipeline, parameter inference, and analysis JSON have all been removed.
type AWSProvider struct {
	mu sync.RWMutex
	// initialized is atomic because every RPC checks it before taking mu,
	// while Initialize and Cleanup flip it under mu.
	initialized atomic.Bool
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

	// currentProgressTracker is guarded by mu: BatchScans for different
	// regions run concurrently and each replaces it.
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

	p.initialized.Store(true)
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

// errNotInitialized is returned when an RPC arrives before Initialize or
// after Cleanup.
var errNotInitialized = errors.New("provider not initialized")

// scannerFor returns the scanner for region: the one built at Initialize
// when region is empty or the configured region, otherwise one built for
// that region on first use and kept for the rest of the process. It fails
// once Cleanup has run, since an RPC may pass the initialized check just
// before Cleanup clears the scanners.
func (p *AWSProvider) scannerFor(ctx context.Context, region string) (activeScanner, error) {
	p.mu.RLock()
	base, cfg := p.scanner, p.config.Copy()
	cached, ok := p.regional[region]
	p.mu.RUnlock()
	if base == nil {
		return nil, errNotInitialized
	}
	if region == "" || region == cfg.Region {
		return base, nil
	}
	if ok {
		return cached, nil
	}

	cfg.Region = region
	built := buildScanner(cfg)
	p.wireExplorer(ctx, cfg, built)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.regional == nil {
		return nil, errNotInitialized
	}
	// Two scopes of one region can race here; the first one kept wins.
	if existing, ok := p.regional[region]; ok {
		return existing, nil
	}
	p.regional[region] = built
	return built, nil
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
	if !p.initialized.Load() {
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
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	regional, err := p.scannerFor(ctx, req.Region)
	if err != nil {
		return nil, err
	}
	regional = forScan(regional)
	services := []string{req.Service}
	if req.Service == "" {
		services = regional.SupportedServices()
	}
	var refs []*pb.ResourceRef
	errs := discoveryErrorEntries(regional)
	for _, svc := range services {
		r, e := regional.ScanService(ctx, svc)
		refs = append(refs, r...)
		errs = append(errs, serviceErrorEntries(svc, e)...)
	}
	metadata := map[string]string{
		"resource_count": fmt.Sprintf("%d", len(refs)),
		"scan_time":      time.Now().Format(time.RFC3339),
		"method":         "cloudcontrol",
	}
	// ListResourcesResponse has no Errors field. Report the failures in
	// metadata so a partial listing is not mistaken for a complete one.
	if len(errs) > 0 {
		log.Printf("ListResources: %d failures: %s", len(errs), strings.Join(errs, "; "))
		metadata["complete"] = "false"
		metadata["errors"] = strings.Join(errs, "\n")
	}
	return &pb.ListResourcesResponse{Resources: refs, Metadata: metadata}, nil
}

func (p *AWSProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}
	regional, err := p.scannerFor(ctx, req.Region)
	if err != nil {
		return nil, err
	}
	regional = forScan(regional)
	scanStartTime := time.Now()
	// UnixNano keeps scan IDs unique across rapid back-to-back BatchScan
	// calls (e.g. one per region in multi-region runs). Unix() collided
	// when two regions started in the same second.
	scanID := fmt.Sprintf("scan_%d", scanStartTime.UnixNano())
	// The local tracker is what this scan reports to; the field only points
	// GetScanProgress at the most recent scan, so it is the only part that
	// needs mu.
	tracker := NewScanProgressTracker(scanID, req.Services)
	p.mu.Lock()
	p.currentProgressTracker = tracker
	p.mu.Unlock()

	stats := &pb.ScanStats{
		ResourceCounts: map[string]int32{},
		ServiceCounts:  map[string]int32{},
	}
	var allResources []*pb.Resource
	var errs []string

	services := req.Services
	if len(services) == 0 {
		services = regional.SupportedServices()
	}

	for _, svc := range services {
		described, entries, cancelled := batchScanService(ctx, regional, tracker, svc)
		for _, res := range described {
			allResources = append(allResources, res)
			stats.ResourceCounts[res.Type]++
		}
		// += because a request may name a service twice.
		stats.ServiceCounts[svc] += int32(len(described))
		errs = append(errs, entries...)
		if cancelled {
			// Start no further services; the ones not started are covered
			// by the cancellation entry batchScanService reported.
			break
		}
	}
	stats.TotalResources = int32(len(allResources))
	stats.DurationMs = time.Since(scanStartTime).Milliseconds()
	assertScanStats(stats)

	errs = append(errs, discoveryErrorEntries(regional)...)
	errs = append(errs, unsupportedNotices(regional)...)

	log.Printf("Batch scan %s: %d resources across %d services (%d unsupported types)",
		scanID, len(allResources), len(services), len(regional.UnsupportedTypes()))
	enrichCorrelationEvidence(allResources)

	return &pb.BatchScanResponse{
		Resources: allResources,
		Stats:     stats,
		Errors:    errs,
	}, nil
}

// batchScanService scans and describes one service. It returns every
// resource it observed, one Errors entry per failure, and whether ctx was
// cancelled, in which case the caller starts no further services.
func batchScanService(
	ctx context.Context, s activeScanner, tracker *ScanProgressTracker, svc string,
) ([]*pb.Resource, []string, bool) {
	tracker.StartService(svc)
	// A failed scan still returns the refs it got; describe those too, so
	// one failed type or account does not drop the rest of the service.
	refs, scanErr := s.ScanService(ctx, svc)
	entries := serviceErrorEntries(svc, scanErr)
	described, describeErr := describeAll(ctx, s, refs)
	if describeErr != nil {
		entries = append(entries, shared.ServiceScanError(svc, describeErr))
	}
	tracker.CompleteService(svc, len(described), errors.Join(scanErr, describeErr))
	return described, entries, describeErr != nil
}

// unsupportedNotices reports the types the scanner could not list as notices,
// sorted so the same scan yields the same Errors.
func unsupportedNotices(s activeScanner) []string {
	unsupported := s.UnsupportedTypes()
	typeNames := make([]string, 0, len(unsupported))
	for typeName := range unsupported {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)
	notices := make([]string, 0, len(typeNames))
	for _, typeName := range typeNames {
		notices = append(notices, shared.UnsupportedTypeNotice(typeName, unsupported[typeName]))
	}
	return notices
}

// assertScanStats asserts that the counts agree: every resource is counted
// once by service and once by type, so a mismatch is a programmer error that
// would report a different inventory size than was returned.
func assertScanStats(stats *pb.ScanStats) {
	var byService, byType int64
	for _, n := range stats.ServiceCounts {
		byService += int64(n)
	}
	for _, n := range stats.ResourceCounts {
		byType += int64(n)
	}
	total := int64(stats.TotalResources)
	assert.Thatf(byService == total, "aws: scan stats agree: total %d, by service %d",
		total, byService)
	assert.Thatf(byType == total, "aws: scan stats agree: total %d, by type %d", total, byType)
}

// describeConcurrency caps in-flight GetResource calls. Empirically 20 is
// well under Cloud Control's per-account burst (~100) but high enough to
// fan out a 1000-resource service in seconds rather than minutes.
const describeConcurrency = 20

// describeAll enriches refs with bounded parallelism. When ctx is cancelled
// it stops starting describes, waits for the ones in flight, and returns
// what they produced together with ctx's error.
func describeAll(ctx context.Context, s activeScanner, refs []*pb.ResourceRef) ([]*pb.Resource, error) {
	results := make(chan *pb.Resource, len(refs))
	sem := make(chan struct{}, describeConcurrency)
	var wg sync.WaitGroup
	var cancelled error
	launched := 0
refLoop:
	for _, ref := range refs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			cancelled = ctx.Err()
			break refLoop
		}
		launched++
		wg.Add(1)
		go func(r *pb.ResourceRef) {
			defer wg.Done()
			defer func() { <-sem }()
			res, derr := s.DescribeResource(ctx, r)
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

	assert.That(launched <= len(refs), "aws: describes launched are at most the refs")
	if cancelled == nil {
		assert.That(launched == len(refs), "aws: without cancellation every ref is described")
	}
	out := drainDescribed(results, launched)
	if cancelled == nil {
		assert.Thatf(len(out) == len(refs),
			"aws: len(results) == len(refs) after draining: %d results, %d refs", len(out),
			len(refs))
	}
	return out, cancelled
}

// drainDescribed collects the closed results channel. Every launched describe sends exactly
// one resource, failed describes included (as unenrichedResource), so anything else means a
// discovered resource was lost.
func drainDescribed(results <-chan *pb.Resource, launched int) []*pb.Resource {
	out := make([]*pb.Resource, 0, launched)
	for res := range results {
		assert.That(res != nil, "aws: every describe yields a resource")
		out = append(out, res)
	}
	assert.Thatf(len(out) == launched,
		"aws: one result per launched describe: %d results, %d launched", len(out), launched)
	return out
}

func (p *AWSProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized.Load() {
		return fmt.Errorf("provider not initialized")
	}
	ctx := stream.Context()
	regional, err := p.scannerFor(ctx, req.Region)
	if err != nil {
		return err
	}
	regional = forScan(regional)
	services := req.Services
	if len(services) == 0 {
		services = regional.SupportedServices()
	}
	errs := discoveryErrorEntries(regional)
	for _, svc := range services {
		refs, err := regional.ScanService(ctx, svc)
		errs = append(errs, serviceErrorEntries(svc, err)...)
		if err := streamResources(ctx, regional, refs, stream); err != nil {
			return err
		}
		if ctx.Err() != nil {
			break
		}
	}
	// A stream has no Errors field: it is complete only if it ends cleanly
	// (ADR 0023), so any failure must end it with an error after sending
	// everything that was observed.
	if len(errs) > 0 {
		return fmt.Errorf("aws stream scan incomplete: %s", strings.Join(errs, "; "))
	}
	return nil
}

// streamResources describes and sends each ref. A ref whose describe fails is
// sent unenriched, as BatchScan does, rather than dropped from the stream.
func streamResources(
	ctx context.Context, s activeScanner, refs []*pb.ResourceRef,
	stream pb.CloudProvider_StreamScanServer,
) error {
	for _, ref := range refs {
		res, err := s.DescribeResource(ctx, ref)
		if err != nil {
			res = unenrichedResource(ref, res)
		}
		if err := stream.Send(res); err != nil {
			return err
		}
	}
	return nil
}

func (p *AWSProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}
	return p.schemaGen.GenerateSchemas(req.Services), nil
}

func (p *AWSProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}
	if req.ResourceRef == nil {
		return &pb.DescribeResourceResponse{Error: "resource_ref is required"}, nil
	}
	regional, err := p.scannerFor(ctx, req.ResourceRef.Region)
	if err != nil {
		return nil, err
	}
	res, err := regional.DescribeResource(ctx, req.ResourceRef)
	if err != nil {
		return &pb.DescribeResourceResponse{Error: err.Error()}, nil
	}
	return &pb.DescribeResourceResponse{Resource: res}, nil
}

func (p *AWSProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	regional, err := p.scannerFor(ctx, req.Region)
	if err != nil {
		return nil, err
	}
	// Keep what was observed and report the failures in Errors, as BatchScan
	// does, rather than discarding the service on its first failed type.
	refs, scanErr := regional.ScanService(ctx, req.Service)
	errs := append(discoveryErrorEntries(regional), serviceErrorEntries(req.Service, scanErr)...)
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
		Errors: errs,
	}, nil
}

func (p *AWSProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	if !p.initialized.Load() {
		return nil, fmt.Errorf("provider not initialized")
	}
	return &pb.ServiceInfoResponse{
		ServiceName: req.Service,
	}, nil
}

func (p *AWSProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	if !p.initialized.Load() {
		return fmt.Errorf("provider not initialized")
	}
	ctx := stream.Context()
	regional, err := p.scannerFor(ctx, req.Region)
	if err != nil {
		return err
	}
	// Send what was observed before reporting the failure: the stream's
	// error already marks it incomplete, and the refs are still real.
	refs, scanErr := regional.ScanService(ctx, req.Service)
	if err := streamResources(ctx, regional, refs, stream); err != nil {
		return err
	}
	errs := append(discoveryErrorEntries(regional), serviceErrorEntries(req.Service, scanErr)...)
	if len(errs) > 0 {
		return fmt.Errorf("aws stream scan incomplete: %s", strings.Join(errs, "; "))
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
	p.initialized.Store(false)
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

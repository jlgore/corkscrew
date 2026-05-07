package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const providerVersion = "0.1.0"

var githubServices = []string{"orgs", "repos", "security", "actions", "access", "findings"}

type GitHubProvider struct {
	initialized     bool
	org             string
	client          *github.Client
	gqlClient       *githubv4.Client
	concurrency     int
	protectionCache sync.Map // key: "owner/name@branch" → branchProtectionState
	listCache       sync.Map // key: listCacheKey(...) → *listEntry
}

const defaultRepoConcurrency = 8

type providerConfig struct {
	Org            string
	Token          string
	TokenEnv       string
	AppID          int64
	InstallationID int64
	PrivateKeyPath string
	BaseURL        string
	Concurrency    int
}

func NewGitHubProvider() *GitHubProvider {
	return &GitHubProvider{}
}

func (p *GitHubProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	cfg, err := readConfig(req)
	if err != nil {
		return &pb.InitializeResponse{Success: false, Error: err.Error()}, nil
	}

	client, httpClient, err := newGitHubClient(ctx, cfg)
	if err != nil {
		return &pb.InitializeResponse{Success: false, Error: err.Error()}, nil
	}

	p.org = cfg.Org
	p.client = client
	p.gqlClient = newGraphQLClient(httpClient, cfg.BaseURL)
	p.concurrency = cfg.Concurrency
	if p.concurrency < 1 {
		p.concurrency = defaultRepoConcurrency
	}
	p.initialized = true

	return &pb.InitializeResponse{
		Success: true,
		Version: providerVersion,
		Metadata: map[string]string{
			"org":                  p.org,
			"auth_mode":            authMode(cfg),
			"github_app_supported": "true",
		},
	}, nil
}

func (p *GitHubProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
	return &pb.ProviderInfoResponse{
		Name:              "github",
		Version:           providerVersion,
		SupportedServices: githubServices,
		Description:       "GitHub provider for organization, repository, security, actions, access, and finding scans",
		Capabilities: map[string]string{
			"discovery":       "true",
			"scanning":        "true",
			"github_app_auth": "true",
			"findings":        "true",
		},
	}, nil
}

func (p *GitHubProvider) DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error) {
	services := []*pb.ServiceInfo{
		serviceInfo("orgs", "GitHub Organizations", "organization", "member", "outside_collaborator", "org_webhook"),
		serviceInfo("repos", "GitHub Repositories", "repository", "branch_protection", "ruleset", "repo_webhook", "deploy_key"),
		serviceInfo("security", "GitHub Security", "dependabot_alert", "secret_scanning_alert", "code_scanning_alert"),
		serviceInfo("actions", "GitHub Actions", "workflow", "org_runner", "repo_runner"),
		serviceInfo("access", "GitHub Access", "team", "collaborator"),
		serviceInfo("findings", "GitHub Findings", "finding"),
	}

	return &pb.DiscoverServicesResponse{
		Services:     filterServices(services, req),
		DiscoveredAt: timestamppb.Now(),
		SdkVersion:   goGithubVersion(),
	}, nil
}

// goGithubVersion reads the linked go-github dependency version from build
// info so the advertised SdkVersion stays correct across `go get -u`. Falls
// back to the import-path major when build info is unavailable (e.g.
// `go run` without a module).
func goGithubVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "go-github/unknown"
	}
	for _, dep := range info.Deps {
		if strings.HasPrefix(dep.Path, "github.com/google/go-github/") {
			major := dep.Path[strings.LastIndex(dep.Path, "/")+1:]
			return fmt.Sprintf("go-github/%s@%s", major, dep.Version)
		}
	}
	return "go-github/unknown"
}

func (p *GitHubProvider) ScanService(ctx context.Context, req *pb.ScanServiceRequest) (*pb.ScanServiceResponse, error) {
	if err := p.ensureInitialized(); err != nil {
		return nil, err
	}

	start := time.Now()
	service := req.Service
	if service == "" {
		service = "repos"
	}

	var resources []*pb.Resource
	var errors []string

	switch service {
	case "orgs":
		resources, errors = p.scanOrgs(ctx)
	case "repos":
		resources, errors = p.scanRepos(ctx)
	case "security":
		resources, errors = p.scanSecurity(ctx)
	case "actions":
		resources, errors = p.scanActions(ctx)
	case "access":
		resources, errors = p.scanAccess(ctx)
	case "findings":
		resources, errors = p.scanFindings(ctx)
	default:
		return nil, fmt.Errorf("unsupported GitHub service %q", service)
	}

	return &pb.ScanServiceResponse{
		Service:   service,
		Resources: resources,
		Stats: &pb.ScanStats{
			TotalResources: int32(len(resources)),
			DurationMs:     time.Since(start).Milliseconds(),
			ResourceCounts: countByType(resources),
			ServiceCounts:  map[string]int32{service: int32(len(resources))},
		},
		Metadata: map[string]string{"org": p.org},
		Errors:   errors,
	}, nil
}

func (p *GitHubProvider) ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error) {
	if err := p.ensureInitialized(); err != nil {
		return nil, err
	}
	return p.listResourcesPaged(ctx, req)
}

func (p *GitHubProvider) DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error) {
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

func (p *GitHubProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
	start := time.Now()
	services := req.Services
	if len(services) == 0 {
		services = []string{"repos", "security", "findings"}
	}

	var resources []*pb.Resource
	var errors []string
	for _, service := range services {
		resp, err := p.ScanService(ctx, &pb.ScanServiceRequest{Service: service, IncludeRelationships: req.IncludeRelationships, Filters: req.Filters})
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", service, err))
			continue
		}
		resources = append(resources, resp.Resources...)
		errors = append(errors, resp.Errors...)
	}

	return &pb.BatchScanResponse{
		Resources: resources,
		Stats:     &pb.ScanStats{TotalResources: int32(len(resources)), DurationMs: time.Since(start).Milliseconds(), ResourceCounts: countByType(resources)},
		Errors:    errors,
	}, nil
}

func (p *GitHubProvider) StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error {
	services := req.Services
	if len(services) == 0 {
		services = []string{"repos", "security", "findings"}
	}
	for _, service := range services {
		resp, err := p.ScanService(stream.Context(), &pb.ScanServiceRequest{Service: service, IncludeRelationships: req.IncludeRelationships})
		if err != nil {
			return err
		}
		for _, resource := range resp.Resources {
			if err := stream.Send(resource); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *GitHubProvider) StreamScanService(req *pb.ScanServiceRequest, stream pb.CloudProvider_StreamScanServer) error {
	resp, err := p.ScanService(stream.Context(), req)
	if err != nil {
		return err
	}
	for _, resource := range resp.Resources {
		if err := stream.Send(resource); err != nil {
			return err
		}
	}
	return nil
}

func (p *GitHubProvider) GetServiceInfo(ctx context.Context, req *pb.GetServiceInfoRequest) (*pb.ServiceInfoResponse, error) {
	service := req.Service
	if service == "" {
		service = "repos"
	}
	return &pb.ServiceInfoResponse{
		ServiceName:         service,
		Version:             providerVersion,
		SupportedResources:  serviceResources(service),
		RequiredPermissions: []string{"metadata:read", "contents:read", "administration:read", "members:read", "security_events:read", "actions:read"},
		Capabilities:        map[string]string{"github_app_auth": "true", "read_only": "true"},
	}, nil
}

func (p *GitHubProvider) GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error) {
	schemas := []*pb.Schema{
		{Name: "github_resources", Service: "github", ResourceType: "resource", Description: "Normalized GitHub inventory resources", Sql: githubResourceSchema()},
		{Name: "github_findings", Service: "findings", ResourceType: "finding", Description: "Derived GitHub posture findings", Sql: githubFindingSchema()},
	}
	return &pb.SchemaResponse{Schemas: schemas}, nil
}

// The following four methods are part of the provider interface but are
// codegen-oriented — they exist to support providers (AWS, Azure) that
// generate their scanners from API analysis. The github provider is hand-
// written, so it has nothing to generate or configure here. Returning
// Success: false with a clear message is the honest answer; the previous
// implementation returned Success: true and silently did nothing, which
// looked like the operation had succeeded.

const unsupportedReason = "operation not supported by github provider (handwritten scanner; nothing to generate or configure)"

func (p *GitHubProvider) GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error) {
	return &pb.GenerateScannersResponse{GeneratedCount: 0, Errors: []string{unsupportedReason}}, nil
}

func (p *GitHubProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
	return &pb.ConfigureDiscoveryResponse{Success: false, Error: unsupportedReason}, nil
}

func (p *GitHubProvider) AnalyzeDiscoveredData(ctx context.Context, req *pb.AnalyzeRequest) (*pb.AnalysisResponse, error) {
	return &pb.AnalysisResponse{Success: false, Error: unsupportedReason}, nil
}

func (p *GitHubProvider) GenerateFromAnalysis(ctx context.Context, req *pb.GenerateFromAnalysisRequest) (*pb.GenerateResponse, error) {
	return &pb.GenerateResponse{Success: false, Error: unsupportedReason}, nil
}

func readConfig(req *pb.InitializeRequest) (*providerConfig, error) {
	if req == nil {
		return nil, fmt.Errorf("initialize request is required")
	}
	defaults, err := loadDefaultConfig()
	if err != nil {
		return nil, err
	}
	c := mergeConfig(defaults, req.Config)
	cfg := &providerConfig{
		Org:            firstNonEmpty(c["org"], c["organization"], os.Getenv("GITHUB_ORG")),
		Token:          firstNonEmpty(c["token"], os.Getenv("GITHUB_TOKEN")),
		TokenEnv:       firstNonEmpty(c["token_env"], "GITHUB_TOKEN"),
		PrivateKeyPath: firstNonEmpty(c["private_key_path"], os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH")),
		BaseURL:        c["base_url"],
	}
	if cfg.Token == "" && cfg.TokenEnv != "" {
		cfg.Token = os.Getenv(cfg.TokenEnv)
	}
	if v := firstNonEmpty(c["app_id"], os.Getenv("GITHUB_APP_ID")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid app_id/GITHUB_APP_ID %q: %w", v, err)
		}
		cfg.AppID = id
	}
	if v := firstNonEmpty(c["installation_id"], os.Getenv("GITHUB_APP_INSTALLATION_ID")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid installation_id/GITHUB_APP_INSTALLATION_ID %q: %w", v, err)
		}
		cfg.InstallationID = id
	}
	if v := firstNonEmpty(c["concurrency"], os.Getenv("GITHUB_CONCURRENCY")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid concurrency %q: must be a positive integer", v)
		}
		cfg.Concurrency = n
	}
	if cfg.Org == "" {
		return nil, fmt.Errorf("github org is required via config org or GITHUB_ORG")
	}
	return cfg, nil
}

// loadDefaultConfig returns config from the user's app.json. A missing file is
// fine (empty config); a malformed file is an error so the user knows their
// config is broken instead of silently falling through to env vars.
func loadDefaultConfig() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return map[string]string{}, nil
	}
	path := firstNonEmpty(os.Getenv("CORKSCREW_GITHUB_CONFIG"), home+"/.corkscrew/providers/github/app.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read github config %s: %w", path, err)
	}
	config := map[string]string{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse github config %s: %w", path, err)
	}
	return config, nil
}

func mergeConfig(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		if v != "" {
			merged[k] = v
		}
	}
	return merged
}

// newGitHubClient returns the REST client and the underlying *http.Client so
// callers can build sibling clients (e.g. GraphQL) sharing the same auth and
// rate-limit transport.
func newGitHubClient(ctx context.Context, cfg *providerConfig) (*github.Client, *http.Client, error) {
	var httpClient *http.Client
	switch {
	case cfg.AppID > 0 && cfg.PrivateKeyPath != "":
		if cfg.InstallationID == 0 {
			installationID, err := resolveInstallationID(ctx, cfg)
			if err != nil {
				return nil, nil, err
			}
			cfg.InstallationID = installationID
		}
		appTransport, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, cfg.AppID, cfg.InstallationID, cfg.PrivateKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("create GitHub App installation transport: %w", err)
		}
		// Wrap with rate-limit/retry handling on the outside so backoff
		// applies to the auth-completed request, not raw JWT exchanges.
		httpClient = &http.Client{Transport: newRateLimitTransport(appTransport)}
	case cfg.Token != "":
		// Plumb through oauth2.Transport manually so the underlying base
		// transport is our rate-limit wrapper instead of http.DefaultTransport.
		src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Token})
		httpClient = &http.Client{Transport: &oauth2.Transport{
			Source: src,
			Base:   newRateLimitTransport(http.DefaultTransport),
		}}
	default:
		return nil, nil, fmt.Errorf("github auth missing: provide a token (token / GITHUB_TOKEN / token_env) or GitHub App credentials (app_id + private_key_path)")
	}

	client := github.NewClient(httpClient)
	if cfg.BaseURL != "" {
		enterpriseClient, err := client.WithEnterpriseURLs(cfg.BaseURL, cfg.BaseURL)
		if err != nil {
			return nil, nil, err
		}
		client = enterpriseClient
	}
	return client, httpClient, nil
}

func resolveInstallationID(ctx context.Context, cfg *providerConfig) (int64, error) {
	transport, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, cfg.AppID, cfg.PrivateKeyPath)
	if err != nil {
		return 0, fmt.Errorf("create GitHub App transport: %w", err)
	}
	httpClient := &http.Client{Transport: newRateLimitTransport(transport)}
	client := github.NewClient(httpClient)
	if cfg.BaseURL != "" {
		client, err = client.WithEnterpriseURLs(cfg.BaseURL, cfg.BaseURL)
		if err != nil {
			return 0, err
		}
	}
	opt := &github.ListOptions{PerPage: 100}
	for {
		installations, resp, err := client.Apps.ListInstallations(ctx, opt)
		if err != nil {
			return 0, fmt.Errorf("list GitHub App installations: %w", err)
		}
		for _, installation := range installations {
			if strings.EqualFold(installation.GetAccount().GetLogin(), cfg.Org) {
				return installation.GetID(), nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return 0, fmt.Errorf("GitHub App is not installed for org %q; install it or set installation_id", cfg.Org)
}

func authMode(cfg *providerConfig) string {
	if cfg.AppID > 0 && cfg.InstallationID > 0 && cfg.PrivateKeyPath != "" {
		return "github_app"
	}
	return "token"
}

func (p *GitHubProvider) ensureInitialized() error {
	if !p.initialized || p.client == nil {
		return fmt.Errorf("provider not initialized")
	}
	return nil
}

func serviceInfo(name, display string, resources ...string) *pb.ServiceInfo {
	resourceTypes := make([]*pb.ResourceType, 0, len(resources))
	for _, resource := range resources {
		resourceTypes = append(resourceTypes, &pb.ResourceType{Name: resource, TypeName: resource, IdField: "id", NameField: "name"})
	}
	return &pb.ServiceInfo{Name: name, DisplayName: display, PackageName: "github.com/google/go-github/v71/github", ClientType: "github", ResourceTypes: resourceTypes}
}

func filterServices(services []*pb.ServiceInfo, req *pb.DiscoverServicesRequest) []*pb.ServiceInfo {
	if req == nil || (len(req.IncludeServices) == 0 && len(req.ExcludeServices) == 0) {
		return services
	}
	include := stringSet(req.IncludeServices)
	exclude := stringSet(req.ExcludeServices)
	filtered := make([]*pb.ServiceInfo, 0, len(services))
	for _, service := range services {
		if len(include) > 0 && !include[service.Name] {
			continue
		}
		if exclude[service.Name] {
			continue
		}
		filtered = append(filtered, service)
	}
	return filtered
}

func serviceResources(service string) []string {
	switch service {
	case "orgs":
		return []string{"organization", "member", "outside_collaborator", "org_webhook"}
	case "repos":
		return []string{"repository", "branch_protection", "ruleset", "repo_webhook", "deploy_key"}
	case "security":
		return []string{"dependabot_alert", "secret_scanning_alert", "code_scanning_alert"}
	case "actions":
		return []string{"workflow", "org_runner", "repo_runner"}
	case "access":
		return []string{"team", "collaborator"}
	case "findings":
		return []string{"finding"}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// mustJSON encodes value to JSON. Encoding failures should be impossible for
// the data we feed it (maps, structs from go-github, primitives), so a failure
// here indicates a real bug — log it to stderr (captured by go-plugin's hclog)
// rather than silently returning "{}" which masks the problem.
func mustJSON(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[github-provider] mustJSON encode failure: %v (value type=%T)\n", err, value)
		return "{}"
	}
	return string(data)
}

func githubResourceSchema() string {
	return `CREATE TABLE github_resources (id VARCHAR, org VARCHAR, service VARCHAR, type VARCHAR, name VARCHAR, raw_data JSON, attributes JSON, discovered_at TIMESTAMP);`
}

func githubFindingSchema() string {
	return `CREATE TABLE github_findings (id VARCHAR, org VARCHAR, repository VARCHAR, severity VARCHAR, problem_type VARCHAR, recommendation VARCHAR, raw_data JSON, discovered_at TIMESTAMP);`
}

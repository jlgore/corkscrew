package main

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
	"github.com/jlgore/corkscrew/internal/shared/paging"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// branchProtectionStatus differentiates "actually unprotected" (404) from
// "we couldn't tell" (403, 5xx, network error). The former is a real finding;
// the latter must not be emitted as one.
type branchProtectionStatus int

const (
	bpUnknown branchProtectionStatus = iota
	bpProtected
	bpUnprotected
	bpNoBranch
)

type branchProtectionState struct {
	resource *pb.Resource
	status   branchProtectionStatus
}

// alertItem pairs a resource with its parsed severity so finding synthesis can
// avoid re-parsing the raw JSON.
type alertItem struct {
	resource *pb.Resource
	severity string
}

func (p *GitHubProvider) scanOrgs(ctx context.Context) ([]*pb.Resource, []string) {
	var errors []string
	resources := make([]*pb.Resource, 0)

	org, _, err := p.client.Organizations.Get(ctx, p.org)
	if err != nil {
		// Without org metadata we still want member/webhook coverage if perms
		// allow it, so record the error and continue rather than aborting.
		errors = append(errors, err.Error())
	} else {
		resources = append(resources, p.resource("orgs", "organization", org.GetNodeID(), org.GetLogin(), org, map[string]string{
			"login":         org.GetLogin(),
			"billing_email": org.GetBillingEmail(),
		}))
	}

	resources = append(resources, p.listMembers(ctx, &errors)...)
	resources = append(resources, p.listOutsideCollaborators(ctx, &errors)...)
	resources = append(resources, p.listOrgWebhooks(ctx, &errors)...)
	return resources, errors
}

func (p *GitHubProvider) scanRepos(ctx context.Context) ([]*pb.Resource, []string) {
	repos, listErrors := p.listRepos(ctx)
	parRes, parErrs := p.parallelRepos(ctx, repos, func(ctx context.Context, repo *github.Repository) ([]*pb.Resource, []string) {
		var res []*pb.Resource
		var errs []string
		res = append(res, p.repoResource(repo))
		state := p.checkBranchProtection(ctx, repo, &errs)
		if state.resource != nil {
			res = append(res, state.resource)
		}
		res = append(res, p.rulesetResources(ctx, repo, &errs)...)
		res = append(res, p.listRepoWebhooks(ctx, repo, &errs)...)
		res = append(res, p.listRepoDeployKeys(ctx, repo, &errs)...)
		return res, errs
	})
	return compactResources(parRes), append(listErrors, parErrs...)
}

func (p *GitHubProvider) scanSecurity(ctx context.Context) ([]*pb.Resource, []string) {
	repos, listErrors := p.listRepos(ctx)
	parRes, parErrs := p.parallelRepos(ctx, repos, func(ctx context.Context, repo *github.Repository) ([]*pb.Resource, []string) {
		owner, name := repoOwnerName(repo)
		var res []*pb.Resource
		var errs []string
		res = append(res, alertResources(p.dependabotAlerts(ctx, owner, name, &errs))...)
		res = append(res, alertResources(p.secretScanningAlerts(ctx, owner, name, &errs))...)
		res = append(res, alertResources(p.codeScanningAlerts(ctx, owner, name, &errs))...)
		return res, errs
	})
	return parRes, append(listErrors, parErrs...)
}

func (p *GitHubProvider) scanActions(ctx context.Context) ([]*pb.Resource, []string) {
	repos, listErrors := p.listRepos(ctx)

	// Org-level runners are a single paginated call independent of repos.
	orgRunners := p.listOrgRunners(ctx, &listErrors)

	parRes, parErrs := p.parallelRepos(ctx, repos, func(ctx context.Context, repo *github.Repository) ([]*pb.Resource, []string) {
		owner, name := repoOwnerName(repo)
		var res []*pb.Resource
		var errs []string
		res = append(res, p.listRepoRunners(ctx, repo, &errs)...)
		res = append(res, p.workflowResources(ctx, repo, owner, name, &errs)...)
		return res, errs
	})
	return append(orgRunners, parRes...), append(listErrors, parErrs...)
}

type workflowItem struct {
	ID        int64  `json:"id"`
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type workflowsPage struct {
	TotalCount int            `json:"total_count"`
	Workflows  []workflowItem `json:"workflows"`
}

// workflowResources pages through every workflow. A single request stopped at GitHub's default
// page of 30 and silently dropped the rest of a repository's workflows.
func (p *GitHubProvider) workflowResources(
	ctx context.Context, repo *github.Repository, owner, name string, errors *[]string,
) []*pb.Resource {
	var res []*pb.Resource
	path := fmt.Sprintf("repos/%s/%s/actions/workflows?per_page=%d", owner, name, restPerPage)
	err := getRESTPages(ctx, p, "workflows "+repo.GetFullName(), path, func(page *workflowsPage) {
		for _, workflow := range page.Workflows {
			id := workflow.NodeID
			if id == "" {
				id = fmt.Sprintf("%s/actions/workflows/%d", repo.GetFullName(), workflow.ID)
			}
			res = append(res, p.resource("actions", "workflow", id, workflow.Name, workflow,
				map[string]string{
					"repository": repo.GetFullName(),
					"path":       workflow.Path,
					"state":      workflow.State,
				}))
		}
	})
	appendAPIError(errors, "actions workflows", repo.GetFullName(), err)
	return res
}

func (p *GitHubProvider) scanAccess(ctx context.Context) ([]*pb.Resource, []string) {
	var resources []*pb.Resource
	var errors []string
	opt := &github.ListOptions{PerPage: 100}
	guard := paging.New("teams "+p.org, paging.PagesMax)
	for {
		teams, resp, err := p.client.Teams.ListTeams(ctx, p.org, opt)
		if err != nil {
			appendAPIError(&errors, "teams", p.org, err)
			break
		}
		for _, team := range teams {
			resources = append(resources, p.resource("access", "team", fmt.Sprintf("%d", team.GetID()), team.GetName(), team, map[string]string{
				"slug":       team.GetSlug(),
				"permission": team.GetPermission(),
			}))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		if err := guard.Next(strconv.Itoa(resp.NextPage)); err != nil {
			appendAPIError(&errors, "teams", p.org, err)
			break
		}
		opt.Page = resp.NextPage
	}

	// Per-repo collaborators. List repos once, fan out concurrently.
	repos, listErrs := p.listRepos(ctx)
	errors = append(errors, listErrs...)
	parRes, parErrs := p.parallelRepos(ctx, repos, func(ctx context.Context, repo *github.Repository) ([]*pb.Resource, []string) {
		var errs []string
		return p.listRepoCollaborators(ctx, repo, &errs), errs
	})
	resources = append(resources, parRes...)
	errors = append(errors, parErrs...)

	return resources, errors
}

func (p *GitHubProvider) scanFindings(ctx context.Context) ([]*pb.Resource, []string) {
	repos, listErrors := p.listRepos(ctx)
	parRes, parErrs := p.parallelRepos(ctx, repos, func(ctx context.Context, repo *github.Repository) ([]*pb.Resource, []string) {
		owner, name := repoOwnerName(repo)
		var res []*pb.Resource
		var errs []string
		if !repo.GetPrivate() && p.org != "" {
			res = append(res, p.finding(repo, "medium", "public_repository", "Repository is public; confirm this is intentional or make it private."))
		}
		if repo.GetArchived() {
			res = append(res, p.finding(repo, "low", "archived_repository", "Archived repository remains visible; review access and retention requirements."))
		}
		state := p.checkBranchProtection(ctx, repo, &errs)
		if state.status == bpUnprotected {
			res = append(res, p.finding(repo, "high", "default_branch_unprotected", "Enable branch protection or repository rulesets on the default branch."))
		}
		if !repo.GetHasIssues() {
			res = append(res, p.finding(repo, "low", "issues_disabled", "Issues are disabled; verify vulnerability intake and operational workflows are covered elsewhere."))
		}
		res = append(res, p.alertFindings(ctx, repo, owner, name, &errs)...)
		return res, errs
	})
	return parRes, append(listErrors, parErrs...)
}

// parallelRepos runs fn concurrently across repos with a semaphore-bounded
// worker pool. Each invocation of fn writes to its own local slices, which the
// helper merges under a mutex — there is no cross-goroutine slice aliasing.
// Concurrency comes from p.concurrency (config "concurrency" /
// $GITHUB_CONCURRENCY, default 8).
func (p *GitHubProvider) parallelRepos(
	ctx context.Context,
	repos []*github.Repository,
	fn func(context.Context, *github.Repository) ([]*pb.Resource, []string),
) ([]*pb.Resource, []string) {
	limit := p.concurrency
	if limit < 1 {
		limit = defaultRepoConcurrency
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var resources []*pb.Resource
	var errors []string

	// The label matters: a bare break inside the select would only leave the
	// select, and the loop would then start a goroutine that never acquired a
	// slot. That goroutine's release (<-sem) can block forever, hanging Wait.
repoLoop:
	for _, repo := range repos {
		if ctx.Err() != nil {
			break
		}
		repo := repo
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break repoLoop
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res, errs := fn(ctx, repo)
			mu.Lock()
			resources = append(resources, res...)
			errors = append(errors, errs...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return resources, errors
}

// listRepos returns the org's repositories, listing them once per scan: every later service of
// the scan reuses the scan cache's list. A listing that reported errors is not cached, so the
// next service retries it rather than scanning a possibly truncated list again, and reports its
// own errors, as each service did before the cache.
func (p *GitHubProvider) listRepos(ctx context.Context) ([]*github.Repository, []string) {
	cache := scanCacheFrom(ctx)
	if cache == nil {
		return p.listReposUncached(ctx)
	}

	cache.reposMu.Lock()
	defer cache.reposMu.Unlock()

	if cache.reposListed {
		return cache.repos, nil
	}
	if cache.repos != nil {
		panic("github: repository cache holds repositories it never listed")
	}
	repos, errors := p.listReposUncached(ctx)
	if len(errors) == 0 {
		cache.repos = repos
		cache.reposListed = true
	}
	return repos, errors
}

// listReposUncached prefers GraphQL, which returns repo metadata + default branch
// protection rule in a single paginated query (eliminating the per-repo REST
// roundtrip for branch protection on subsequent scan paths). Falls back to
// REST on Enterprise (no GraphQL client built) or any GraphQL error so the
// scanner is never worse than the REST-only baseline.
func (p *GitHubProvider) listReposUncached(ctx context.Context) ([]*github.Repository, []string) {
	if p.gqlClient != nil {
		repos, err := p.listReposGraphQL(ctx)
		if err == nil {
			return repos, nil
		}
		warnGraphQLFallback(err)
	}
	return p.listReposREST(ctx)
}

func (p *GitHubProvider) listReposREST(ctx context.Context) ([]*github.Repository, []string) {
	var repos []*github.Repository
	var errors []string
	opt := &github.RepositoryListByOrgOptions{Type: "all", ListOptions: github.ListOptions{PerPage: 100}}
	guard := paging.New("repositories "+p.org, paging.PagesMax)
	for {
		page, resp, err := p.client.Repositories.ListByOrg(ctx, p.org, opt)
		if err != nil {
			return repos, []string{err.Error()}
		}
		repos = append(repos, page...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		if err := guard.Next(strconv.Itoa(resp.NextPage)); err != nil {
			return repos, []string{err.Error()}
		}
		opt.Page = resp.NextPage
	}
	return repos, errors
}

func (p *GitHubProvider) repoResource(repo *github.Repository) *pb.Resource {
	return p.resource("repos", "repository", repo.GetNodeID(), repo.GetFullName(), repo, map[string]string{
		"full_name":      repo.GetFullName(),
		"visibility":     repo.GetVisibility(),
		"private":        fmt.Sprintf("%t", repo.GetPrivate()),
		"archived":       fmt.Sprintf("%t", repo.GetArchived()),
		"default_branch": repo.GetDefaultBranch(),
	})
}

// checkBranchProtection returns a structured status. A 404 means the branch
// genuinely has no protection; any other error (403 Forbidden, 5xx, transport)
// becomes bpUnknown so callers don't synthesize a false-positive finding.
// Known results are cached for the current scan (see withScanCache) so
// BatchScan doesn't re-fetch across `repos` and `findings` services.
func (p *GitHubProvider) checkBranchProtection(ctx context.Context, repo *github.Repository, errors *[]string) branchProtectionState {
	owner, name := repoOwnerName(repo)
	branch := repo.GetDefaultBranch()
	if branch == "" {
		return branchProtectionState{status: bpNoBranch}
	}
	key := fmt.Sprintf("%s/%s@%s", owner, name, branch)
	cache := protectionCacheFrom(ctx)
	if cache != nil {
		if cached, ok := cache.Load(key); ok {
			return cached.(branchProtectionState)
		}
	}

	protection, resp, err := p.client.Repositories.GetBranchProtection(ctx, owner, name, branch)
	var state branchProtectionState
	switch {
	case err == nil:
		state = branchProtectionState{
			resource: p.resource("repos", "branch_protection",
				fmt.Sprintf("%s/branches/%s/protection", repo.GetFullName(), branch),
				branch, protection, map[string]string{
					"repository": repo.GetFullName(),
					"branch":     branch,
				}),
			status: bpProtected,
		}
	case resp != nil && resp.StatusCode == http.StatusNotFound:
		state = branchProtectionState{status: bpUnprotected}
	default:
		appendAPIError(errors, "branch protection", repo.GetFullName(), err)
		state = branchProtectionState{status: bpUnknown}
	}
	// Unknown usually means a transient failure (5xx, rate limit, network). Caching it would make
	// the next service in the scan skip the retry and report the repository as unknown again.
	if cache != nil && state.status != bpUnknown {
		cache.Store(key, state)
	}
	return state
}

type scanCacheKey struct{}

// scanCache holds what one scan has already fetched, so the services of a BatchScan share it:
// the default batch (repos, security, findings) used to list every repository three times and
// fetch every alert listing twice. It lives in the scan's ctx and never on the provider: a cache
// that outlived the scan made a later scan report the state an earlier one saw.
type scanCache struct {
	// protection maps "owner/name@branch" to branchProtectionState.
	protection sync.Map

	// reposMu is held for the whole listing, so a second caller waits for the first listing
	// rather than starting its own. Services run in sequence, so it is rarely contended.
	reposMu     sync.Mutex
	reposListed bool
	repos       []*github.Repository

	// alerts maps alertCacheKey to alertListing.
	alerts      sync.Map
	alertsCount atomic.Int64
}

// alertListing is one alert endpoint's result for one repository: the alerts and the notices
// the listing reported, which a cache hit replays so every service still reports them.
type alertListing struct {
	items   []alertItem
	notices []string
}

// withScanCache returns ctx carrying the cache for one scan, reusing one already in ctx.
func withScanCache(ctx context.Context) context.Context {
	if scanCacheFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, scanCacheKey{}, &scanCache{})
}

// scanCacheFrom returns the scan's cache, or nil outside a scan, in which case callers neither
// read nor store.
func scanCacheFrom(ctx context.Context) *scanCache {
	cache, _ := ctx.Value(scanCacheKey{}).(*scanCache)
	return cache
}

// protectionCacheFrom returns the scan's branch protection cache, or nil outside a scan.
func protectionCacheFrom(ctx context.Context) *sync.Map {
	cache := scanCacheFrom(ctx)
	if cache == nil {
		return nil
	}
	return &cache.protection
}

func (p *GitHubProvider) rulesetResources(ctx context.Context, repo *github.Repository, errors *[]string) []*pb.Resource {
	owner, name := repoOwnerName(repo)
	path := fmt.Sprintf("repos/%s/%s/rulesets?per_page=%d", owner, name, restPerPage)
	rulesets, err := p.getRESTMaps(ctx, "rulesets "+repo.GetFullName(), path)
	appendAPIError(errors, "rulesets", repo.GetFullName(), err)
	resources := make([]*pb.Resource, 0, len(rulesets))
	for _, ruleset := range rulesets {
		id := fmt.Sprintf("%v", ruleset["id"])
		name, _ := ruleset["name"].(string)
		resources = append(resources, p.resource("repos", "ruleset", fmt.Sprintf("%s/rulesets/%s", repo.GetFullName(), id), name, ruleset, map[string]string{"repository": repo.GetFullName()}))
	}
	return resources
}

func (p *GitHubProvider) dependabotAlerts(ctx context.Context, owner, repo string, errors *[]string) []alertItem {
	return p.cachedAlerts(ctx, alertSource{
		operation: "dependabot alerts", alertType: "dependabot_alert", endpoint: "dependabot",
		severity: dependabotSeverity,
	}, owner, repo, errors)
}

func (p *GitHubProvider) secretScanningAlerts(ctx context.Context, owner, repo string, errors *[]string) []alertItem {
	return p.cachedAlerts(ctx, alertSource{
		operation: "secret scanning alerts", alertType: "secret_scanning_alert",
		endpoint: "secret-scanning", severity: secretScanningSeverity,
	}, owner, repo, errors)
}

func (p *GitHubProvider) codeScanningAlerts(ctx context.Context, owner, repo string, errors *[]string) []alertItem {
	return p.cachedAlerts(ctx, alertSource{
		operation: "code scanning alerts", alertType: "code_scanning_alert",
		endpoint: "code-scanning", severity: codeScanningSeverity,
	}, owner, repo, errors)
}

// alertSource describes one of the three per-repository alert endpoints.
type alertSource struct {
	operation string // names the listing in errors and notices
	alertType string // the resource type of each alert
	endpoint  string // the path segment under repos/{owner}/{repo}/
	severity  func(map[string]interface{}) string
}

// alertSourcesCount is the number of alertSource values above; the cache bound counts on it.
const alertSourcesCount = 3

// alertCacheEntriesMax bounds the scan's alert cache at one entry per alert source per
// repository a listing can return (paging.PagesMax pages of restPerPage repositories).
const alertCacheEntriesMax = alertSourcesCount * paging.PagesMax * restPerPage

// alertCacheKey identifies one alert listing of one repository within a scan.
type alertCacheKey struct {
	endpoint string
	repo     string
}

// cachedAlerts lists source's open alerts for owner/repo once per scan: `security` and
// `findings` both read every repository's alerts. A listing that ended in a notice (403/404) is
// cached with its notices, which a hit replays. Any other failure is not cached, so the next
// service retries it instead of repeating what is usually a transient error.
func (p *GitHubProvider) cachedAlerts(
	ctx context.Context, source alertSource, owner, repo string, errors *[]string,
) []alertItem {
	if source.endpoint == "" {
		panic("github: alert source has no endpoint")
	}
	cache := scanCacheFrom(ctx)
	key := alertCacheKey{endpoint: source.endpoint, repo: owner + "/" + repo}
	if cache != nil {
		if cached, ok := cache.alerts.Load(key); ok {
			listing := cached.(alertListing)
			*errors = append(*errors, listing.notices...)
			return listing.items
		}
	}

	var entries []string
	items := p.listAlerts(ctx, source, owner, repo, &entries)
	*errors = append(*errors, entries...)
	if cache == nil {
		return items
	}
	for _, entry := range entries {
		if !shared.IsScanNotice(entry) {
			return items
		}
	}
	if cache.alertsCount.Load() < alertCacheEntriesMax {
		if _, loaded := cache.alerts.LoadOrStore(key, alertListing{items, entries}); !loaded {
			cache.alertsCount.Add(1)
		}
	}
	return items
}

// listAlerts fetches every page of source's open alerts for owner/repo.
func (p *GitHubProvider) listAlerts(
	ctx context.Context, source alertSource, owner, repo string, errors *[]string,
) []alertItem {
	fullName := owner + "/" + repo
	path := fmt.Sprintf("repos/%s/%s/%s/alerts?state=open&per_page=%d",
		owner, repo, source.endpoint, restPerPage)
	alerts, err := p.getRESTMaps(ctx, source.operation+" "+fullName, path)
	appendAPIError(errors, source.operation, fullName, err)
	return p.buildAlertItems(source.alertType, fullName, alerts, source.severity)
}

func (p *GitHubProvider) buildAlertItems(alertType, repo string, alerts []map[string]interface{}, severityFn func(map[string]interface{}) string) []alertItem {
	items := make([]alertItem, 0, len(alerts))
	for _, alert := range alerts {
		sev := severityFn(alert)
		id := fmt.Sprintf("%s/%s/%v", repo, alertType, alert["number"])
		attrs := map[string]string{
			"repository": repo,
			"state":      fmt.Sprintf("%v", alert["state"]),
			"severity":   sev,
		}
		items = append(items, alertItem{
			resource: p.resource("security", alertType, id, id, alert, attrs),
			severity: sev,
		})
	}
	return items
}

func alertResources(items []alertItem) []*pb.Resource {
	out := make([]*pb.Resource, 0, len(items))
	for _, it := range items {
		out = append(out, it.resource)
	}
	return out
}

func (p *GitHubProvider) alertFindings(ctx context.Context, repo *github.Repository, owner, name string, errors *[]string) []*pb.Resource {
	var findings []*pb.Resource
	for _, item := range p.dependabotAlerts(ctx, owner, name, errors) {
		f := p.finding(repo, item.severity, "open_dependabot_alert", "Review and remediate the open Dependabot alert.")
		f.Id = f.Id + "/" + strings.ReplaceAll(item.resource.Id, "/", "_")
		findings = append(findings, f)
	}
	for _, item := range p.secretScanningAlerts(ctx, owner, name, errors) {
		f := p.finding(repo, item.severity, "open_secret_scanning_alert", "Rotate the exposed secret and close the secret scanning alert.")
		f.Id = f.Id + "/" + strings.ReplaceAll(item.resource.Id, "/", "_")
		findings = append(findings, f)
	}
	for _, item := range p.codeScanningAlerts(ctx, owner, name, errors) {
		f := p.finding(repo, item.severity, "open_code_scanning_alert", "Review and remediate the open code scanning alert.")
		f.Id = f.Id + "/" + strings.ReplaceAll(item.resource.Id, "/", "_")
		findings = append(findings, f)
	}
	return findings
}

// dependabotSeverity reads the structured severity field from a Dependabot
// alert payload. GitHub's API returns one of critical/high/medium/low under
// security_advisory.severity (preferred) or security_vulnerability.severity.
func dependabotSeverity(alert map[string]interface{}) string {
	if adv, ok := alert["security_advisory"].(map[string]interface{}); ok {
		if s, ok := adv["severity"].(string); ok {
			if n := normalizeSeverity(s); n != "" {
				return n
			}
		}
	}
	if vuln, ok := alert["security_vulnerability"].(map[string]interface{}); ok {
		if s, ok := vuln["severity"].(string); ok {
			if n := normalizeSeverity(s); n != "" {
				return n
			}
		}
	}
	return "medium"
}

// codeScanningSeverity prefers rule.security_severity_level (critical/high/
// medium/low). Falls back to rule.severity which uses note/warning/error and
// must be mapped.
func codeScanningSeverity(alert map[string]interface{}) string {
	rule, ok := alert["rule"].(map[string]interface{})
	if !ok {
		return "medium"
	}
	if s, ok := rule["security_severity_level"].(string); ok {
		if n := normalizeSeverity(s); n != "" {
			return n
		}
	}
	if s, ok := rule["severity"].(string); ok {
		switch strings.ToLower(s) {
		case "error":
			return "high"
		case "warning":
			return "medium"
		case "note":
			return "low"
		}
	}
	return "medium"
}

func secretScanningSeverity(_ map[string]interface{}) string {
	return "critical"
}

func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium", "moderate":
		return "medium"
	case "low":
		return "low"
	}
	return ""
}

func (p *GitHubProvider) finding(repo *github.Repository, severity, problemType, recommendation string) *pb.Resource {
	id := fmt.Sprintf("%s/findings/%s", repo.GetFullName(), problemType)
	attrs := map[string]string{
		"repository":     repo.GetFullName(),
		"severity":       severity,
		"problem_type":   problemType,
		"recommendation": recommendation,
	}
	// nil raw avoids the previous double-encode where attrs landed in both
	// RawData and Attributes.
	return p.resource("findings", "finding", id, problemType, nil, attrs)
}

func (p *GitHubProvider) resource(service, typ, id, name string, raw interface{}, attrs map[string]string) *pb.Resource {
	if id == "" {
		id = name
	}
	rawJSON := ""
	if raw != nil {
		rawJSON = mustJSON(raw)
	}
	return &pb.Resource{
		Provider:     "github",
		Service:      service,
		Type:         typ,
		Id:           id,
		Name:         name,
		AccountId:    p.org,
		RawData:      rawJSON,
		Attributes:   mustJSON(attrs),
		DiscoveredAt: timestamppb.Now(),
	}
}

func (p *GitHubProvider) getREST(ctx context.Context, path string, out interface{}) error {
	_, err := p.getRESTResponse(ctx, path, out)
	return err
}

func (p *GitHubProvider) getRESTResponse(
	ctx context.Context, path string, out interface{},
) (*github.Response, error) {
	path = strings.TrimPrefix(path, "/")
	req, err := p.client.NewRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return p.client.Do(ctx, req, out)
}

// restPerPage is GitHub's largest REST page size; the default of 30 needs over 3x the requests.
const restPerPage = 100

// getRESTPages GETs path and then every rel="next" page from the Link header, decoding each
// page into a fresh P for onPage. Following the Link URL verbatim covers both page numbers and
// the before/after cursors the alert endpoints use. Pages already handed to onPage stay
// delivered when a later page fails, so callers keep the resources they observed.
func getRESTPages[P any](
	ctx context.Context, p *GitHubProvider, label, path string, onPage func(*P),
) error {
	guard := paging.New(label, paging.PagesMax)
	next := path
	for {
		var page P
		resp, err := p.getRESTResponse(ctx, next, &page)
		if err != nil {
			return err
		}
		onPage(&page)
		next, err = p.nextPageURL(resp)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		if next == "" {
			return nil
		}
		if err := guard.Next(next); err != nil {
			return err
		}
	}
}

// getRESTMaps pages through a REST endpoint whose body is a JSON array of objects.
func (p *GitHubProvider) getRESTMaps(
	ctx context.Context, label, path string,
) ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	err := getRESTPages(ctx, p, label, path, func(page *[]map[string]interface{}) {
		items = append(items, *page...)
	})
	return items, err
}

// nextPageURL returns the rel="next" URL from resp's Link header, or "" on the last page. The
// URL comes from the server and the request carries our credentials, so a link to any scheme or
// host other than the API base is refused rather than followed.
func (p *GitHubProvider) nextPageURL(resp *github.Response) (string, error) {
	if resp == nil || resp.Response == nil {
		return "", nil
	}
	next := linkNext(resp.Header.Get("Link"))
	if next == "" {
		return "", nil
	}
	target, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("parse next page link %q: %w", next, err)
	}
	base := p.client.BaseURL
	if target.Scheme != base.Scheme || target.Host != base.Host {
		return "", fmt.Errorf("next page link %q leaves API host %s", next, base.Host)
	}
	return next, nil
}

// linkNext extracts the rel="next" target from an RFC 8288 Link header value.
func linkNext(header string) string {
	for _, link := range strings.Split(header, ",") {
		segments := strings.Split(strings.TrimSpace(link), ";")
		if len(segments) < 2 {
			continue
		}
		target := strings.TrimSpace(segments[0])
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, param := range segments[1:] {
			if strings.TrimSpace(param) == `rel="next"` {
				return target[1 : len(target)-1]
			}
		}
	}
	return ""
}

func repoOwnerName(repo *github.Repository) (string, string) {
	if repo.GetOwner().GetLogin() != "" {
		return repo.GetOwner().GetLogin(), repo.GetName()
	}
	parts := strings.SplitN(repo.GetFullName(), "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", repo.GetName()
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

// appendAPIError records err for one listing. A 403 means the token lacks the scope and a 404
// means the feature is off (or hidden from this token) for target: both describe the scan's
// coverage, so they are notices and the scan stays comparable (ADR 0023). Rate limits are retried
// by the transport before reaching here. Any other failure is an error: resources may be missing.
func appendAPIError(errors *[]string, operation, target string, err error) {
	if err == nil {
		return
	}
	subject := operation + " " + target
	var ghErr *github.ErrorResponse
	if stderrors.As(err, &ghErr) && ghErr.Response != nil {
		switch ghErr.Response.StatusCode {
		case http.StatusForbidden:
			*errors = append(*errors, shared.AccessDeniedNotice(subject, fmt.Errorf("%s", ghErr.Response.Status)))
			return
		case http.StatusNotFound:
			*errors = append(*errors, shared.NotEnabledNotice(subject, ghErr.Response.Status))
			return
		}
	}
	*errors = append(*errors, fmt.Sprintf("%s failed: %v", subject, err))
}

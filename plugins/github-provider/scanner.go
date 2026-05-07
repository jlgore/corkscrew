package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
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
		var workflows struct {
			TotalCount int `json:"total_count"`
			Workflows  []struct {
				ID        int64  `json:"id"`
				NodeID    string `json:"node_id"`
				Name      string `json:"name"`
				Path      string `json:"path"`
				State     string `json:"state"`
				HTMLURL   string `json:"html_url"`
				CreatedAt string `json:"created_at"`
				UpdatedAt string `json:"updated_at"`
			} `json:"workflows"`
		}
		if err := p.getREST(ctx, fmt.Sprintf("repos/%s/%s/actions/workflows", owner, name), &workflows); err != nil {
			appendAPIError(&errs, "actions workflows", repo.GetFullName(), err)
			return res, errs
		}
		for _, workflow := range workflows.Workflows {
			id := workflow.NodeID
			if id == "" {
				id = fmt.Sprintf("%s/actions/workflows/%d", repo.GetFullName(), workflow.ID)
			}
			res = append(res, p.resource("actions", "workflow", id, workflow.Name, workflow, map[string]string{
				"repository": repo.GetFullName(),
				"path":       workflow.Path,
				"state":      workflow.State,
			}))
		}
		return res, errs
	})
	return append(orgRunners, parRes...), append(listErrors, parErrs...)
}

func (p *GitHubProvider) scanAccess(ctx context.Context) ([]*pb.Resource, []string) {
	var resources []*pb.Resource
	var errors []string
	opt := &github.ListOptions{PerPage: 100}
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

	for _, repo := range repos {
		if ctx.Err() != nil {
			break
		}
		repo := repo
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break
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

// listRepos prefers GraphQL, which returns repo metadata + default branch
// protection rule in a single paginated query (eliminating the per-repo REST
// roundtrip for branch protection on subsequent scan paths). Falls back to
// REST on Enterprise (no GraphQL client built) or any GraphQL error so the
// scanner is never worse than the REST-only baseline.
func (p *GitHubProvider) listRepos(ctx context.Context) ([]*github.Repository, []string) {
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
	for {
		page, resp, err := p.client.Repositories.ListByOrg(ctx, p.org, opt)
		if err != nil {
			return repos, []string{err.Error()}
		}
		repos = append(repos, page...)
		if resp == nil || resp.NextPage == 0 {
			break
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
// Results are cached per provider instance so BatchScan doesn't re-fetch
// across `repos` and `findings` services.
func (p *GitHubProvider) checkBranchProtection(ctx context.Context, repo *github.Repository, errors *[]string) branchProtectionState {
	owner, name := repoOwnerName(repo)
	branch := repo.GetDefaultBranch()
	if branch == "" {
		return branchProtectionState{status: bpNoBranch}
	}
	key := fmt.Sprintf("%s/%s@%s", owner, name, branch)
	if cached, ok := p.protectionCache.Load(key); ok {
		return cached.(branchProtectionState)
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
	p.protectionCache.Store(key, state)
	return state
}

func (p *GitHubProvider) rulesetResources(ctx context.Context, repo *github.Repository, errors *[]string) []*pb.Resource {
	owner, name := repoOwnerName(repo)
	var rulesets []map[string]interface{}
	if err := p.getREST(ctx, fmt.Sprintf("repos/%s/%s/rulesets", owner, name), &rulesets); err != nil {
		appendAPIError(errors, "rulesets", repo.GetFullName(), err)
		return nil
	}
	resources := make([]*pb.Resource, 0, len(rulesets))
	for _, ruleset := range rulesets {
		id := fmt.Sprintf("%v", ruleset["id"])
		name, _ := ruleset["name"].(string)
		resources = append(resources, p.resource("repos", "ruleset", fmt.Sprintf("%s/rulesets/%s", repo.GetFullName(), id), name, ruleset, map[string]string{"repository": repo.GetFullName()}))
	}
	return resources
}

func (p *GitHubProvider) dependabotAlerts(ctx context.Context, owner, repo string, errors *[]string) []alertItem {
	var alerts []map[string]interface{}
	if err := p.getREST(ctx, fmt.Sprintf("repos/%s/%s/dependabot/alerts?state=open&per_page=100", owner, repo), &alerts); err != nil {
		appendAPIError(errors, "dependabot alerts", owner+"/"+repo, err)
		return nil
	}
	return p.buildAlertItems("dependabot_alert", owner+"/"+repo, alerts, dependabotSeverity)
}

func (p *GitHubProvider) secretScanningAlerts(ctx context.Context, owner, repo string, errors *[]string) []alertItem {
	var alerts []map[string]interface{}
	if err := p.getREST(ctx, fmt.Sprintf("repos/%s/%s/secret-scanning/alerts?state=open&per_page=100", owner, repo), &alerts); err != nil {
		appendAPIError(errors, "secret scanning alerts", owner+"/"+repo, err)
		return nil
	}
	return p.buildAlertItems("secret_scanning_alert", owner+"/"+repo, alerts, secretScanningSeverity)
}

func (p *GitHubProvider) codeScanningAlerts(ctx context.Context, owner, repo string, errors *[]string) []alertItem {
	var alerts []map[string]interface{}
	if err := p.getREST(ctx, fmt.Sprintf("repos/%s/%s/code-scanning/alerts?state=open&per_page=100", owner, repo), &alerts); err != nil {
		appendAPIError(errors, "code scanning alerts", owner+"/"+repo, err)
		return nil
	}
	return p.buildAlertItems("code_scanning_alert", owner+"/"+repo, alerts, codeScanningSeverity)
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
	path = strings.TrimPrefix(path, "/")
	req, err := p.client.NewRequest("GET", path, nil)
	if err != nil {
		return err
	}
	_, err = p.client.Do(ctx, req, out)
	return err
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

func appendAPIError(errors *[]string, operation, target string, err error) {
	if err == nil {
		return
	}
	if ghErr, ok := err.(*github.ErrorResponse); ok && (ghErr.Response.StatusCode == http.StatusForbidden || ghErr.Response.StatusCode == http.StatusNotFound) {
		*errors = append(*errors, fmt.Sprintf("%s %s skipped: %s", operation, target, ghErr.Response.Status))
		return
	}
	*errors = append(*errors, fmt.Sprintf("%s %s failed: %v", operation, target, err))
}

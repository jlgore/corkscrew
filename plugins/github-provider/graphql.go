package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/google/go-github/v71/github"
	"github.com/shurcooL/githubv4"
)

// orgReposQuery fetches a page of org repositories with the metadata we need
// for the inventory pass plus the default branch's protection rule, in one
// round trip. The branch protection embed is the main reason we use GraphQL
// here: REST requires a separate GET per repo, which is the dominant cost on
// large orgs.
type orgReposQuery struct {
	Organization struct {
		Repositories struct {
			PageInfo struct {
				EndCursor   githubv4.String
				HasNextPage bool
			}
			Nodes []orgRepoNode
		} `graphql:"repositories(first: 100, after: $cursor)"`
	} `graphql:"organization(login: $org)"`
}

type orgRepoNode struct {
	ID               githubv4.String
	Name             githubv4.String
	NameWithOwner    githubv4.String
	IsPrivate        githubv4.Boolean
	IsArchived       githubv4.Boolean
	Visibility       githubv4.String
	HasIssuesEnabled githubv4.Boolean
	Owner            struct {
		Login githubv4.String
	}
	DefaultBranchRef *struct {
		Name                 githubv4.String
		BranchProtectionRule *branchProtectionRuleNode
	}
}

type branchProtectionRuleNode struct {
	ID                           githubv4.String
	Pattern                      githubv4.String
	RequiresApprovingReviews     githubv4.Boolean
	RequiredApprovingReviewCount githubv4.Int
	RequiresStatusChecks         githubv4.Boolean
	RequiresLinearHistory        githubv4.Boolean
	AllowsForcePushes            githubv4.Boolean
	AllowsDeletions              githubv4.Boolean
	RequiresCodeOwnerReviews     githubv4.Boolean
	DismissesStaleReviews        githubv4.Boolean
	RequireLastPushApproval      githubv4.Boolean
	RestrictsPushes              githubv4.Boolean
	IsAdminEnforced              githubv4.Boolean
}

// newGraphQLClient builds a GraphQL client sharing the same authenticated,
// rate-limited http.Client as the REST client. Returns nil for Enterprise
// (BaseURL set) — Enterprise GraphQL endpoint derivation is left to a future
// pass; falling back to REST is correct.
func newGraphQLClient(httpClient *http.Client, baseURL string) *githubv4.Client {
	if httpClient == nil {
		return nil
	}
	if baseURL != "" {
		// TODO: derive the Enterprise GraphQL endpoint
		// (typically https://HOST/api/graphql) and use NewEnterpriseClient.
		return nil
	}
	return githubv4.NewClient(httpClient)
}

// listReposGraphQL fetches all org repos via GraphQL, populating
// protectionCache as it goes so the subsequent scan paths skip the per-repo
// REST roundtrip for branch protection.
func (p *GitHubProvider) listReposGraphQL(ctx context.Context) ([]*github.Repository, error) {
	if p.gqlClient == nil {
		return nil, fmt.Errorf("graphql client unavailable")
	}
	var repos []*github.Repository
	vars := map[string]interface{}{
		"org":    githubv4.String(p.org),
		"cursor": (*githubv4.String)(nil),
	}
	for {
		var q orgReposQuery
		if err := p.gqlClient.Query(ctx, &q, vars); err != nil {
			return nil, err
		}
		for _, node := range q.Organization.Repositories.Nodes {
			repo := convertGraphQLRepo(node)
			repos = append(repos, repo)
			p.cacheBranchProtectionFromGraphQL(repo, node)
		}
		if !q.Organization.Repositories.PageInfo.HasNextPage {
			break
		}
		vars["cursor"] = githubv4.NewString(q.Organization.Repositories.PageInfo.EndCursor)
	}
	return repos, nil
}

// convertGraphQLRepo synthesizes a *github.Repository from a GraphQL node so
// downstream scanners (which already work with go-github types) don't need to
// branch on data source.
func convertGraphQLRepo(n orgRepoNode) *github.Repository {
	repo := &github.Repository{
		NodeID:     github.Ptr(string(n.ID)),
		Name:       github.Ptr(string(n.Name)),
		FullName:   github.Ptr(string(n.NameWithOwner)),
		Private:    github.Ptr(bool(n.IsPrivate)),
		Archived:   github.Ptr(bool(n.IsArchived)),
		Visibility: github.Ptr(string(n.Visibility)),
		HasIssues:  github.Ptr(bool(n.HasIssuesEnabled)),
		Owner: &github.User{
			Login: github.Ptr(string(n.Owner.Login)),
		},
	}
	if n.DefaultBranchRef != nil {
		repo.DefaultBranch = github.Ptr(string(n.DefaultBranchRef.Name))
	}
	return repo
}

// cacheBranchProtectionFromGraphQL writes the (possibly absent) branch
// protection state into protectionCache so checkBranchProtection becomes a
// pure cache hit. Distinguishing nil DefaultBranchRef (empty repo, no default
// branch — bpNoBranch) from a present-but-unprotected branch (bpUnprotected)
// matches the REST path's semantics.
func (p *GitHubProvider) cacheBranchProtectionFromGraphQL(repo *github.Repository, n orgRepoNode) {
	owner, name := repoOwnerName(repo)
	if n.DefaultBranchRef == nil {
		key := fmt.Sprintf("%s/%s@", owner, name)
		p.protectionCache.Store(key, branchProtectionState{status: bpNoBranch})
		return
	}
	branch := string(n.DefaultBranchRef.Name)
	key := fmt.Sprintf("%s/%s@%s", owner, name, branch)

	if n.DefaultBranchRef.BranchProtectionRule == nil {
		p.protectionCache.Store(key, branchProtectionState{status: bpUnprotected})
		return
	}

	rule := n.DefaultBranchRef.BranchProtectionRule
	attrs := map[string]string{
		"repository":          repo.GetFullName(),
		"branch":              branch,
		"pattern":             string(rule.Pattern),
		"requires_reviews":    fmt.Sprintf("%t", bool(rule.RequiresApprovingReviews)),
		"required_reviews":    fmt.Sprintf("%d", int(rule.RequiredApprovingReviewCount)),
		"requires_status":     fmt.Sprintf("%t", bool(rule.RequiresStatusChecks)),
		"linear_history":      fmt.Sprintf("%t", bool(rule.RequiresLinearHistory)),
		"allows_force_pushes": fmt.Sprintf("%t", bool(rule.AllowsForcePushes)),
		"allows_deletions":    fmt.Sprintf("%t", bool(rule.AllowsDeletions)),
		"code_owner_reviews":  fmt.Sprintf("%t", bool(rule.RequiresCodeOwnerReviews)),
		"dismisses_stale":     fmt.Sprintf("%t", bool(rule.DismissesStaleReviews)),
		"admin_enforced":      fmt.Sprintf("%t", bool(rule.IsAdminEnforced)),
	}
	resource := p.resource("repos", "branch_protection",
		fmt.Sprintf("%s/branches/%s/protection", repo.GetFullName(), branch),
		branch, rule, attrs)
	p.protectionCache.Store(key, branchProtectionState{
		resource: resource,
		status:   bpProtected,
	})
}

func warnGraphQLFallback(err error) {
	fmt.Fprintf(os.Stderr, "[github-provider] GraphQL listRepos failed, falling back to REST: %v\n", err)
}

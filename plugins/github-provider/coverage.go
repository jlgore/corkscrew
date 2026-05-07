package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v71/github"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// This file holds the resource scanners added in Phase 6 — coverage for
// security-relevant surfaces that were either advertised but unimplemented
// (members, outside_collaborators, collaborator) or simply missing
// (webhooks, deploy keys, self-hosted runners).
//
// All paginators follow the same shape: drive go-github's typed list call
// with PerPage:100, terminate on no NextPage, and on error route through
// appendAPIError so 403/404 become "skipped" notes rather than scan failures.

// ----- org-level inventory -----

func (p *GitHubProvider) listMembers(ctx context.Context, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	opt := &github.ListMembersOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		members, resp, err := p.client.Organizations.ListMembers(ctx, p.org, opt)
		if err != nil {
			appendAPIError(errors, "members", p.org, err)
			return resources
		}
		for _, m := range members {
			id := fmt.Sprintf("%s/members/%s", p.org, m.GetLogin())
			resources = append(resources, p.resource("orgs", "member", id, m.GetLogin(), m, map[string]string{
				"login":   m.GetLogin(),
				"user_id": fmt.Sprintf("%d", m.GetID()),
			}))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

func (p *GitHubProvider) listOutsideCollaborators(ctx context.Context, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	opt := &github.ListOutsideCollaboratorsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		users, resp, err := p.client.Organizations.ListOutsideCollaborators(ctx, p.org, opt)
		if err != nil {
			appendAPIError(errors, "outside_collaborators", p.org, err)
			return resources
		}
		for _, u := range users {
			id := fmt.Sprintf("%s/outside_collaborators/%s", p.org, u.GetLogin())
			resources = append(resources, p.resource("orgs", "outside_collaborator", id, u.GetLogin(), u, map[string]string{
				"login":   u.GetLogin(),
				"user_id": fmt.Sprintf("%d", u.GetID()),
			}))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

func (p *GitHubProvider) listOrgWebhooks(ctx context.Context, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	opt := &github.ListOptions{PerPage: 100}
	for {
		hooks, resp, err := p.client.Organizations.ListHooks(ctx, p.org, opt)
		if err != nil {
			appendAPIError(errors, "org_webhooks", p.org, err)
			return resources
		}
		for _, h := range hooks {
			resources = append(resources, p.hookResource("orgs", "org_webhook", p.org, p.org, h))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

func (p *GitHubProvider) listOrgRunners(ctx context.Context, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	opt := &github.ListRunnersOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		runners, resp, err := p.client.Actions.ListOrganizationRunners(ctx, p.org, opt)
		if err != nil {
			appendAPIError(errors, "org_runners", p.org, err)
			return resources
		}
		if runners == nil {
			break
		}
		for _, r := range runners.Runners {
			id := fmt.Sprintf("%s/runners/%d", p.org, r.GetID())
			resources = append(resources, p.resource("actions", "org_runner", id, r.GetName(), r, map[string]string{
				"scope":  "organization",
				"name":   r.GetName(),
				"os":     r.GetOS(),
				"status": r.GetStatus(),
				"busy":   fmt.Sprintf("%t", r.GetBusy()),
				"labels": runnerLabels(r),
			}))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

// ----- per-repo inventory -----

func (p *GitHubProvider) listRepoCollaborators(ctx context.Context, repo *github.Repository, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	owner, name := repoOwnerName(repo)
	opt := &github.ListCollaboratorsOptions{Affiliation: "all", ListOptions: github.ListOptions{PerPage: 100}}
	for {
		users, resp, err := p.client.Repositories.ListCollaborators(ctx, owner, name, opt)
		if err != nil {
			appendAPIError(errors, "collaborators", repo.GetFullName(), err)
			return resources
		}
		for _, u := range users {
			id := fmt.Sprintf("%s/collaborators/%s", repo.GetFullName(), u.GetLogin())
			resources = append(resources, p.resource("access", "collaborator", id, u.GetLogin(), u, map[string]string{
				"login":      u.GetLogin(),
				"repository": repo.GetFullName(),
				"permission": collaboratorPermission(u),
				"role_name":  u.GetRoleName(),
			}))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

func (p *GitHubProvider) listRepoWebhooks(ctx context.Context, repo *github.Repository, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	owner, name := repoOwnerName(repo)
	opt := &github.ListOptions{PerPage: 100}
	for {
		hooks, resp, err := p.client.Repositories.ListHooks(ctx, owner, name, opt)
		if err != nil {
			appendAPIError(errors, "repo_webhooks", repo.GetFullName(), err)
			return resources
		}
		for _, h := range hooks {
			resources = append(resources, p.hookResource("repos", "repo_webhook", repo.GetFullName(), repo.GetFullName(), h))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

func (p *GitHubProvider) listRepoDeployKeys(ctx context.Context, repo *github.Repository, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	owner, name := repoOwnerName(repo)
	opt := &github.ListOptions{PerPage: 100}
	for {
		keys, resp, err := p.client.Repositories.ListKeys(ctx, owner, name, opt)
		if err != nil {
			appendAPIError(errors, "deploy_keys", repo.GetFullName(), err)
			return resources
		}
		for _, k := range keys {
			id := fmt.Sprintf("%s/keys/%d", repo.GetFullName(), k.GetID())
			resources = append(resources, p.resource("repos", "deploy_key", id, k.GetTitle(), k, map[string]string{
				"repository": repo.GetFullName(),
				"title":      k.GetTitle(),
				"read_only":  fmt.Sprintf("%t", k.GetReadOnly()),
				"verified":   fmt.Sprintf("%t", k.GetVerified()),
			}))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

func (p *GitHubProvider) listRepoRunners(ctx context.Context, repo *github.Repository, errors *[]string) []*pb.Resource {
	var resources []*pb.Resource
	owner, name := repoOwnerName(repo)
	opt := &github.ListRunnersOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		runners, resp, err := p.client.Actions.ListRunners(ctx, owner, name, opt)
		if err != nil {
			appendAPIError(errors, "repo_runners", repo.GetFullName(), err)
			return resources
		}
		if runners == nil {
			break
		}
		for _, r := range runners.Runners {
			id := fmt.Sprintf("%s/runners/%d", repo.GetFullName(), r.GetID())
			resources = append(resources, p.resource("actions", "repo_runner", id, r.GetName(), r, map[string]string{
				"scope":      "repository",
				"repository": repo.GetFullName(),
				"name":       r.GetName(),
				"os":         r.GetOS(),
				"status":     r.GetStatus(),
				"busy":       fmt.Sprintf("%t", r.GetBusy()),
				"labels":     runnerLabels(r),
			}))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return resources
}

// ----- helpers -----

// hookResource normalizes both org and repo webhooks into a Resource. The
// scope string differentiates the two in the cached attrs without changing
// the resource Type discriminator.
func (p *GitHubProvider) hookResource(service, typ, scopeID, scopeLabel string, h *github.Hook) *pb.Resource {
	id := fmt.Sprintf("%s/hooks/%d", scopeID, h.GetID())
	return p.resource(service, typ, id, h.GetName(), h, map[string]string{
		"scope":        scopeLabel,
		"active":       fmt.Sprintf("%t", h.GetActive()),
		"url":          hookConfigURL(h),
		"events":       strings.Join(h.Events, ","),
		"content_type": hookContentType(h),
		"insecure_ssl": hookInsecureSSL(h),
	})
}

func hookConfigURL(h *github.Hook) string {
	if h.Config == nil {
		return ""
	}
	return h.Config.GetURL()
}

func hookContentType(h *github.Hook) string {
	if h.Config == nil {
		return ""
	}
	return h.Config.GetContentType()
}

func hookInsecureSSL(h *github.Hook) string {
	if h.Config == nil {
		return ""
	}
	return h.Config.GetInsecureSSL()
}

// collaboratorPermission picks the highest-privilege permission set on the
// user. GitHub returns a boolean map (admin/maintain/push/triage/pull) where
// having "admin" implies all the others, so we walk highest-to-lowest.
func collaboratorPermission(u *github.User) string {
	if len(u.Permissions) == 0 {
		return ""
	}
	for _, lvl := range []string{"admin", "maintain", "push", "triage", "pull"} {
		if u.Permissions[lvl] {
			return lvl
		}
	}
	return ""
}

func runnerLabels(r *github.Runner) string {
	if len(r.Labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.Labels))
	for _, l := range r.Labels {
		if l == nil {
			continue
		}
		parts = append(parts, l.GetName())
	}
	return strings.Join(parts, ",")
}

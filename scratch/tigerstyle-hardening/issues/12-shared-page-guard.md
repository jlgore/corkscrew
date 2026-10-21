# Add a shared pagination guard and use it in every page loop

Status: ready-for-agent
Phase: P3

## Problem

About 30 `for {}` page loops have no page cap and never check that the token advances. A server
that returns the same token again loops until the deadline and keeps appending duplicates. GitHub
GraphQL (`plugins/github-provider/graphql.go:91`) loops forever if `HasNextPage` stays true with
the same `EndCursor`.

Locations:
- AWS: `cloudcontrol_scanner.go:295,452`, `resource_explorer.go:81` (checks for nil but not
  `""`), `orgscan.go:238`.
- GCP: `discovery.go:116`, `asset_inventory.go:119,154,191`, `gcp_provider.go:235,270`.
- Azure: `resource_graph.go:309`, `scanner.go:371`, `management_group_client.go:237`.
- Kubernetes: `scanner.go:293`.
- GitHub: `coverage.go:26,50,74,94,128,155,176,203`, `scanner.go:141,262`, `provider.go:455`,
  `graphql.go:91`.

## Change

A small `internal/shared/paging` package (the plugins already `replace` the core module):

```go
type Guard struct{ pagesMax int; page int; tokenPrevious string }
func (g *Guard) Next(token string) error // ErrPageLimit, ErrTokenRepeated
```

Each loop becomes `for { …; if done { break }; if err := guard.Next(token); err != nil { return
refs, err } }`, and the error flows into Errors under the issue 08 contract. Start with
`pagesMax = 10_000`, a named constant per call site.

## Acceptance

- Unit tests for both guard errors.
- A grep check in `internal/architecture` that fails when a plugin has a `for {` loop around a
  `NextToken|NextPageToken|EndCursor|NextPage` without a `paging.Guard`.

## Comments

2026-10-21 (GitHub and Cloudflare, with issue 11): guarded loops in `plugins/github-provider`:
- `coverage.go`: `listMembers`, `listOutsideCollaborators`, `listOrgWebhooks`, `listOrgRunners`,
  `listRepoCollaborators`, `listRepoWebhooks`, `listRepoDeployKeys`, `listRepoRunners` (8).
- `scanner.go`: `scanAccess` teams, `listReposREST`, plus the new `getRESTPages` (alerts,
  rulesets, workflows), which guards on the full next-page URL.
- `provider.go`: `resolveInstallationID`.
- `graphql.go`: `listReposGraphQL`, which guards on `EndCursor` and treats `HasNextPage` with an
  empty cursor as an error.

Typed go-github loops use `strconv.Itoa(resp.NextPage)` as the token. Every site uses
`paging.PagesMax`, not a per-site constant. Cloudflare has no hand-written page loops; its
`ListAutoPaging` iterators are bounded by an item cap instead (issue 11). The `internal/architecture`
grep check is still open.

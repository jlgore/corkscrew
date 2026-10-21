# GitHub and Cloudflare: paginate everything and report truncation

Status: ready-for-agent
Phase: P2
Depends on: 08, 12

- `plugins/github-provider/scanner.go:343,352,361`: the alert endpoints make one `getREST` call each
  (`per_page=100`). Rulesets (`:328`) and workflows (`:117`) stop at GitHub's default of 30. Page
  through them all with the paging guard.
- `plugins/github-provider/describe.go:229`: `resp.Errors` from ScanService is dropped.
- `plugins/github-provider/scanner.go:321`: `protectionCache` stores `bpUnknown` after a transient
  5xx and keeps it for the life of the process. Don't cache unknown results, and scope the cache to
  one scan.
- Cloudflare, every `ListAutoPaging` call (`traversal.go`, `data_scanner.go`, `storage_scanner.go`,
  `workers_scanner.go`): put an item cap on each, and report an error when the cap is hit.
- Cloudflare and GitHub `list.go`: `MaxResults <= 0` currently means unlimited. Make the default
  limit explicit.

# GitHub and Cloudflare: paginate everything and report truncation

Status: complete
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

## Comments

2026-10-21: Implemented.

GitHub: the alert endpoints, rulesets and workflows go through `getRESTPages`, which follows the
Link header's `rel="next"` URL verbatim (covering both `page=` and the alerts' `after=` cursors),
bounded by `paging.Guard`. A next link to a different scheme or host is refused, since the request
carries credentials. Failed or truncated listings keep the items already fetched and add an error
entry. `ListResources` (GitHub's `list.go` is `describe.go`) no longer drops `resp.Errors`: the
response carries metadata `partial=true` and `errors`, and a first-page request rescans a cached
partial listing while continuation pages reuse it. `MaxResults <= 0` now means
`listPageSizeDefault` (1000). `protectionCache` moved from the provider to a per-scan context value
(`withProtectionCache`): `BatchScan`/`StreamScan` share one across services, and each standalone
`ScanService` gets its own. `bpUnknown` is never cached.

Cloudflare: all 14 `ListAutoPaging` uses drain through `collectAutoPaging`, capped at
`listItemsMax` (100,000; a var so tests can lower it), and return `errListItemCap` when it's hit.
That cap is the only bound here, because the SDK pager stops only on an empty page. Listings return
their partial items with the error, and the scanners keep those items instead of skipping the
account, namespace or store. `ListResources` reports errors and the default page size the same way
GitHub does (I made the same `resp.Errors` fix here because the code is identical).

Tests (httptest, no network): 3-page alerts, a repeated Link that must stop within a timeout, a
foreign-host link, GraphQL with a repeated and an empty cursor, `ListResources` error propagation
and rescan, the default page size, a 502 that must not be cached in branch protection, and
Cloudflare hitting the cap against a KV server that never ends.

Left for later: existing entries still use the provider's unprefixed `"... failed: ..."` /
`"... skipped: 403"` strings, not `shared.ServiceScanError`. Under ADR 0023 they already count as
errors. That includes 403/404 "skipped" for disabled features (for example secret scanning off), so
those repos make the scan partial. Whether some of those should be `UnsupportedTypeNotice` is a
separate decision.

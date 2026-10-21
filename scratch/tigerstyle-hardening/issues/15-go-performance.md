# Go performance: credentials, repeated listing, N+1 queries

Status: complete
Phase: P4

- `plugins/aws-provider/orgscan/orgscan.go:158,216-228`: every DescribeResource builds a new
  AssumeRoleProvider, which means one `sts:AssumeRole` per resource. The "STS credentials cache
  themselves" comment is wrong. Cache one scanner per account in a map guarded by `o.mu`, capped at
  the account count. `ListAccounts` also runs once per service (`:114`), so do it once per scan.
- `plugins/github-provider/scanner.go:62,80,94,160,173,394-410`: the default BatchScan lists every
  repo 3 times and fetches the alerts twice. Use a repo/alert cache that lasts one scan.
- `plugins/azure-provider/scanner.go:127-131`, `azure_provider.go:929`: one ARM GET per resource, in
  sequence, plus several unconditional `log.Printf("DEBUG…")` calls. Use a bounded `errgroup` with
  `SetLimit`, and put the logs behind a debug flag.
- `plugins/gcp-provider/scanner.go:94`: one goroutine per scanner. Reuse `maxConcurrency`.
  `cache.go:103-110`: 3 tickers that never stop and maps with no size cap. Add `Stop()` and a
  max-entries limit.
- `internal/app/findings/findings.go:150-164`: a SELECT, an upsert and an INSERT for every failing
  row. Load the existing fingerprints into a set with one query, then batch the inserts.
- `internal/cache/operation_cache.go`: nothing imports it. Delete it.

## Comments

2026-10-21: Implemented in two parallel parts on `perf/p4`.

- AWS `orgscan`: per-account scanners cached under `o.mu`, never more than the listed accounts;
  `retainAccounts` drops accounts that leave the org. Credentials wrapped in
  `aws.NewCredentialsCache`. `OrgScanner.ForScan()` lists accounts once per scan; a listing
  failure is reported by every service. Judgment call: needed a per-scan hook in
  `aws_provider.go` (`forScan`), outside the cited file. A lone `DescribeResource` for an
  unlisted account (or before any scan) still assumes the role per call, to keep the cap.
- GitHub: `scanCache` in ctx, one scan only, holds repos, alerts and branch protection. Errors
  are not cached (next service retries and reports); 403/404 notices are cached and replayed.
- Azure: `describeEach` uses `errgroup` with `SetLimit(16)`, no shared ctx, errors per index, so
  partial-scan reporting is unchanged. DEBUG logs behind new `CORKSCREW_DEBUG` (no existing
  switch). The Resource Graph field dump moved from stdout to the debug log.
- GCP: `ScanAllServices` bounded by `maxConcurrency`. Cache gets `Stop()` (wired via
  `GCPProvider.Close()` in `main.go`) and caps of 1024/16384/4096 entries (expired first, then
  nearest expiry). Left: `Close` doesn't stop the `ClientFactory`; `Cache.Get` still spawns a
  goroutine to delete an expired key.
- Findings: `recordResult` groups by fingerprint, `IN (...)` existence lookups, multi-row
  upserts/inserts, `findingBatchCountMax = 256`. Test with 600 rows checks per-row semantics are
  identical. `findings.Evaluate` is still over 70 lines (was already).
- `internal/cache` deleted; nothing in root or any plugin module imported it.

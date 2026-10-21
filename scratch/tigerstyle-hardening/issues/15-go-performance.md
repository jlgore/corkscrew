# Go performance: credentials, repeated listing, N+1 queries

Status: ready-for-agent
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

# Pack: cloudflare/workers-baseline

Status: complete
Depends on: 06

Implement CF.WRK.01–05 as specified in the PRD, in
`pkg/query/compliance/packs/cloudflare/workers-baseline/` (`manifest.yaml` + `queries/*.sql`,
`provider: cloudflare`, namespace `cloudflare/workers-baseline`).

- Every query reads `cloudflare_resources` (and `cloud_relationships` where it needs edges),
  and returns `status`, `resource_id`, `resource_name`, `resource_type`, `control_id`,
  `control_name`, `severity`, `issue_description`, and `details`. The executor rejects a query
  missing any of these, `control_id` and `control_name` included.
- Every parameter has a default, so the pack runs with no flags.
- Each control maps to remediation steps and a Cloudflare docs reference.

## Acceptance

- The golden fixture has at least one PASS and one FAIL resource for every control, asserted
  through the issue 06 harness: `packtest.Run` plus `Outcomes.Check` in
  `internal/packtest/cloudflare_test.go`. Add fixtures in
  `plugins/cloudflare-provider/testdata/fakeapi/baseline.json`, then regenerate the golden scan
  with `go test -run TestGoldenScan -update` in the provider.
- Running with no parameters works against an empty `cloudflare_resources` (zero rows, no
  error), checked with `packtest.Results`.

## Comments

2026-10-22: Implemented in `pkg/query/compliance/packs/cloudflare/workers-baseline/`. Tests are in
`internal/packtest/cloudflare_workers_test.go`: every control has a PASS and a FAIL, the
allowlist is covered, and an empty inventory returns zero rows with no error.

- Controls evaluate only resources whose inputs are known. A script whose subdomain, settings,
  or exposure request failed gets no row. CF.WRK.01 and CF.WRK.02 evaluate only Workers with a
  route or custom domain.
- CF.WRK.04 takes `as_of_date` (empty means today), so tests are deterministic against the
  golden's fixed dates. The packtest run uses `2026-01-01`.
- List parameters arrive as a comma-joined string (`normalizeSQLParam`), so allowlists use
  `list_contains(string_split(:param, ','), …)`. `workers_dev_allowlist` matches a script
  name or its Corkscrew ID.

Provider changes the pack needed:

- **Routes carry their account.** `worker_route.account_id` (the column and the attribute) was
  the zone ID. It is now the zone's account, falling back to the zone ID the way DNS records do,
  so CF.WRK.05 can match a route to its account's scripts.
- **Effective log setting.** `observability_logs_enabled` is now `logs.enabled` when settings
  include a `logs` object, and `observability.enabled` otherwise. Logs default to on with
  observability, so reading `logs.enabled` alone flagged Workers that do log. **Assumption to
  confirm in issue 10 (live).**

Fixtures added: `www-site` (routed, off workers.dev, Preview URLs on, observability on with no
logs object) and a route `example.com/old/*` → `legacy-redirect`, a script that doesn't exist.

Found along the way and fixed in its own commit: the pack loader reported a manifest that
didn't parse as "manifest not found".

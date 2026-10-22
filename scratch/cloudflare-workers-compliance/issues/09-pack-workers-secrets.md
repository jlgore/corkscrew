# Pack: cloudflare/workers-secrets

Status: complete
Depends on: 06

Implement CF.SEC.01–07 as specified in the PRD, in
`pkg/query/compliance/packs/cloudflare/workers-secrets/` (`manifest.yaml` + `queries/*.sql`,
`provider: cloudflare`, namespace `cloudflare/workers-secrets`).

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

2026-10-22: Implemented in `pkg/query/compliance/packs/cloudflare/workers-secrets/`. Tests are in
`internal/packtest/cloudflare_secrets_test.go`: every control has a PASS and a FAIL, the
allowed-scopes parameter is covered, CF.SEC.04 is checked to stay silent without a workers scan,
and an empty inventory returns zero rows.

- **CF.SEC.03 reads the secret's own times from attributes** (`created_on`, `modified_on`, now
  recorded by the provider). The `created_at` and `modified_at` columns can't be used: the
  generic store writes both as the scan time, and finding evaluation's snapshot hydration drops
  resource timestamps. That bug affects every provider, so it's filed as issue 13 rather than
  fixed here. `as_of_date` makes the test deterministic.
- **CF.SEC.04 evaluates only accounts whose Workers were scanned**, so a data-only scan can't
  make every secret look unused. A binding counts as a reference whether or not its target
  resolved (`target_id`, or a `target_ref` of `<store>/<secret>`).
- **The tests caught a real bug:** `list()` over no rows is NULL in DuckDB, not `[]`. Without
  the `coalesce`, `len(scripts) = 0` was NULL and the unused secret passed. Other aggregates in
  the packs only group rows that exist, so they're safe.
- CF.SEC.02's default pattern adds `access_?key` to the PRD's list, so `*_ACCESS_KEY` is caught.
- CF.SEC.01 and CF.SEC.02 read only the provider's derived signals and the binding name. No
  value is ever stored to query.

Fixtures added: `OLD_SENDGRID_KEY` (scoped to Workers, unbound, modified 2024-02-01),
`ROTATING_SIGNING_KEY` (pending, bound by `billing-cron`), and the `SENDGRID_API_KEY`
secret_text in three Workers.

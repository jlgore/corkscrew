# Pack: cloudflare/workers-bindings

Status: complete
Depends on: 06

Implement CF.BND.01–06 as specified in the PRD (CF.BND.04 covers Secrets Store secrets too, with its own `max_secret_fan_in` threshold), in
`pkg/query/compliance/packs/cloudflare/workers-bindings/` (`manifest.yaml` + `queries/*.sql`,
`provider: cloudflare`, namespace `cloudflare/workers-bindings`).

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

2026-10-22: Implemented in `pkg/query/compliance/packs/cloudflare/workers-bindings/`. Tests are in
`internal/packtest/cloudflare_bindings_test.go`: every control has a PASS and a FAIL, the
threshold is a parameter, and an empty inventory returns zero rows.

Changes from the PRD:

- **CF.BND.03 uses two patterns, not one.** With only `prod_name_pattern`, every Worker not
  named "prod" counted as non-production, so nearly every binding to a production resource
  would fail. It now fails only when the Worker matches `nonprod_name_pattern` (dev, staging,
  test, qa, preview, sandbox, and similar) **and** the target matches `prod_name_pattern`. A
  Secrets Store secret is named `<store name>/<secret name>` for matching, so a secret in a store
  called "production" is production.
- **CF.BND.01's kinds are a parameter** (`privileged_binding_kinds`, defaulting to the PRD's
  list). The allowlist takes `<script>:<kind>`, by script name or Corkscrew ID.
- **CF.BND.04 is per resource**, with the bound Workers listed in `details.scripts`. Its
  severity is MEDIUM over the limit for secrets and LOW for storage.

Every control reads binding attributes (`target_resolved`, `target_id`, `target_type`), not
`cloud_relationships`, so the pack works whether or not the scan asked for relationships.
CF.BND.06 evaluates only `target_resolved` `true` or `false`, so a binding whose target listing
was denied is never called dangling. The tests check that unresolvable kinds get no row.

Fixtures added: `checkout-staging` (binds the production store's `STRIPE_KEY` and a
`staging-sessions` KV namespace). `www-site` now binds `STRIPE_KEY` (3 Workers on it) and has a
service binding to `billing-cron` (internal, off workers.dev). `api-gateway` gained an
`mtls_certificate` binding.

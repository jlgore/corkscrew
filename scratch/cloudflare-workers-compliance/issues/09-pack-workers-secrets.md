# Pack: cloudflare/workers-secrets

Status: ready-for-agent
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

# Pack: cloudflare/workers-bindings

Status: ready-for-agent
Depends on: 06

Implement CF.BND.01–06 as specified in the PRD (CF.BND.04 covers Secrets Store secrets too, with its own `max_secret_fan_in` threshold), in
`pkg/query/compliance/packs/cloudflare/workers-bindings/` (`manifest.yaml` + `queries/*.sql`,
`provider: cloudflare`, namespace `cloudflare/workers-bindings`).

- Every query reads `cloudflare_resources` (and `cloud_relationships` where it needs edges),
  and returns `status`, `resource_id`, `resource_name`, `resource_type`, `severity`,
  `issue_description`, and `details`.
- Every parameter has a default, so the pack runs with no flags.
- Each control maps to remediation steps and a Cloudflare docs reference.

## Acceptance

- The golden fixture has at least one PASS and one FAIL resource for every control, asserted
  through the issue 06 harness.
- Running with no parameters works against an empty `cloudflare_resources` (zero rows, no
  error).

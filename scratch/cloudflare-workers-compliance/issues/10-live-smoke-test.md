# Live smoke test and wrangler seed

Status: ready-for-human
Depends on: 07, 08, 09

Needs a throwaway Cloudflare account and a token with the `workers_read`, storage, and data
read scopes, plus a separate deploy token for seeding.

- `scripts/cloudflare-fixture/seed.sh` and `teardown.sh` use wrangler to deploy Workers that
  mirror the golden fixture's good and bad cases. All seeded names share a prefix. Teardown deletes
  by that prefix only.
- Credential-shaped fixture values use vendor-documented fake examples (for example
  `AKIAIOSFODNN7EXAMPLE`) so that push protection and secret scanning don't trip.
- `go test -tags live ./...`, gated on `CORKSCREW_CF_LIVE=1`, `CLOUDFLARE_API_TOKEN`, and
  `CLOUDFLARE_ACCOUNT_ID`. It scans the seeded prefix, runs the three packs, and asserts the same
  control outcomes as the hermetic tables.
- Answers PRD open question 2 (whether settings return `secret_text` bindings) and confirms that
  `workers:read` covers settings and subdomain. Record both answers here.
- Confirm that a Worker with `[observability] enabled = true` and no `[observability.logs]`
  section collects logs. The provider assumes it does (`logsEnabled`, issue 07). Also confirm
  what settings return for `observability.logs` in that case: absent, or present with
  `enabled: true`.

# Script settings and exposure enrichment

Status: complete
Depends on: 01

For each listed script, call `Scripts.ScriptAndVersionSettings.Get` and `Scripts.Subdomain.Get`,
and add the `worker_script` attributes from the PRD's data model table.

- Bounded worker pool (default 4). Bound and assert it like the other fan-outs.
- A failure on one script records an error entry (making the scan partial) and still emits the
  script with list-level attributes. A 403 is a coverage warning (ADR 0023), not an error.
- Derive `has_route` and `has_custom_domain` from the routes and domains the workers scan already
  lists. Routes are zone-scoped, so the workers scan already traverses every zone.
- Keep the settings response so issue 04 can derive bindings from it without a second call.

## Acceptance

- Fixture scripts cover workers.dev on and off, previews on and off, logs on and off, a tail
  consumer, and no observability at all.
- A settings 500 for one script yields a partial scan in which the other scripts still have full
  attributes.

## Comments

2026-10-22: Implemented. `workers_settings.go` fetches settings and subdomain for each script.
At most `scriptDetailConcurrency` (4) scripts run at once, and the slots are asserted released
afterwards. The result is a `scriptDetail` per script.

- A field whose request failed is left out. Pack SQL must treat a missing attribute as unknown,
  not false. Every value is the string `"true"` or `"false"`, or a list joined with commas.
- 403 → `shared.AccessDeniedNotice`. Any other failure is an unprefixed error, so the scan is
  partial (ADR 0023). The script is still emitted with its list attributes.
- `has_route` and `has_custom_domain` are set only when every zone's route listing and the
  account's domain listing succeeded. Otherwise they are left out.
- The settings response stays in memory on `scriptDetail.settings`, for issue 04. It never goes
  into `raw_data`, because its bindings carry `plain_text` values.
- Describe fetches the detail for its one script, but leaves the exposure attributes out.

Fixtures: the baseline now has three scripts. `api-gateway` is routed, on a custom domain, fully
observed, and on workers.dev. `internal-auth` has no route, no observability, and has workers.dev
and previews on. `billing-cron` is off workers.dev and has logs off. The fake gained a `Delay`
fault and `MaxInFlight(op)` for the concurrency test.

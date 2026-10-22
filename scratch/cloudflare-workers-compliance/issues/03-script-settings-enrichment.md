# Script settings and exposure enrichment

Status: ready-for-agent
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

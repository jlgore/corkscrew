# Binding relationships and target resolution

Status: ready-for-agent
Depends on: 04

Resolve each binding to a Corkscrew ID according to the PRD's target resolution table, set
`target_id` and `target_resolved`, and attach a `binds` relationship (properties `binding_name`,
`kind`) to the `worker_script`.

- Build the name→ID indexes for Queues, DO namespaces, and Secrets Store secrets once per account
  per scan, through the provider's list cache, so a full scan doesn't list them twice. When
  the workers service is scanned alone, the indexes still trigger those lists. If a list fails
  with 403, leave those bindings unresolved and add a coverage warning, not an error.
- `service` bindings resolve to the issue 02 script ID. A service binding to a script in the
  dispatch namespace is out of scope (`target_resolved=false`, `target_ref` set).

## Acceptance

- Fixture edges for every resolvable kind, checked through `cloud_relationships` after
  persistence (via the golden in issue 06).
- One dangling binding of each kind yields `target_resolved=false` and no edge.

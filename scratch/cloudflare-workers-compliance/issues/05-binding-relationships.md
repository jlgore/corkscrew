# Binding relationships and target resolution

Status: complete
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

2026-10-22: Implemented (`binding_targets.go`, `list_memo.go`).

- `target_resolved` has three states. `"true"` comes with `target_id`. `"false"` means the
  listing succeeded and the target isn't in it: dangling. **No attribute** means the target
  couldn't be checked: its listing failed or was denied, an R2 bucket is outside the default
  jurisdiction (the default listing doesn't show those), or a DO class is in another
  environment or a dispatch namespace. CF.BND.06 must fire only on `"false"`.
- `target_type` and `target_service` are set for every resolvable kind, even when unresolved, so
  pack SQL can tell whether the target's service was scanned.
- Listings are fetched lazily per kind, the first time a binding needs them. Each failed listing
  is reported once per account: a 403 as a coverage notice, anything else as an error.
- `list_memo.go` is a per-scan memo in the context (the same pattern as GitHub's
  `protectionCache`). It covers the R2, KV, Queues, D1, DO-namespace, and Secrets Store
  listings, which the storage and data scanners share. It remembers only successful listings.
  `TestBatchScanSharesListingsAndRelationships` checks one listing per kind across a workers,
  storage, and data batch. Mutation check: with the memo off, all six counts double.
- `binds` edges sit on the `worker_script` and are deduplicated per target (the graph store
  keys edges by source, target, and type anyway). They are attached only when the request sets
  `IncludeRelationships`. **This fixed a bug:** `batchScanServices` was dropping
  `IncludeRelationships` when it built each service's request.
- `service` bindings resolve against the account's script listing. A binding to a script in a
  dispatch namespace stays unknown.

The check that edges reach `cloud_relationships` moves to issue 06, which persists the golden
scan.

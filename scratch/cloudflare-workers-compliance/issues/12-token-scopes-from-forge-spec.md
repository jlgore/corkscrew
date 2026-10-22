# Derive token scopes from the Forge spec

Status: needs-triage
Depends on: 01

Every operation in `openapi.forge.json` has an `x-api-token-group` listing the token permission
groups that grant it. `GET …/workers/scripts/{script_name}/settings`, for example, lists `Workers
Scripts Read`, `Workers Admin`, `Workers Editor`, and eight more. Today
`internal/providers/cloudflareauth/permissions.go` maintains the per-service scopes by hand.

Proposal: generate a table that maps each provider operation to its permission groups, using the
issue 01 operation allowlist and the same Forge pipeline. Pick the least-privileged read group for
each operation, and have a test check that `auth plan --services <svc>` covers every operation
that service's scanner calls.

Open question: the list doesn't say which group is least privileged. That needs a hand-maintained
ranking, or a rule such as "prefer `* Read`, then `* Read-Only`".

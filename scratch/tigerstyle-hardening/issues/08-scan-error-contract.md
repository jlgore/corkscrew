# Define the scan error contract: dropped data means a partial scan

Status: ready-for-agent
Phase: P2

## Problem

Findings are only evaluated against completed scans (`internal/app/findings/findings.go:71`), and
evaluation resolves open findings that are absent from the latest scan. When a plugin logs an error
and drops it, the scan reports `complete` with resources missing, and **those resources' findings
are resolved even though they were never observed**.

Core-side gaps:

- `internal/shared/plugin.go:190,229`: any `Recv()` error is treated as end of stream, and the
  call returns `nil`. It also uses the stored `c.ctx` instead of the stream's context.
- `internal/scanexec/executor.go`: per-scope status is decided only by RPC-level failure.
  `BatchScanResponse.Errors` entries don't affect `Status`, and nothing checks that every scope
  result was filled in.

## Change

- Contract, documented in `docs/` and in a new ADR:
  - A plugin puts every per-service, per-type, per-account or per-page failure into
    `BatchScanResponse.Errors` with a stable prefix (`service:<name>: …`).
  - `unsupported_type:` entries are informational.
  - Any other entry makes that scope `partial`.
- `scanexec`:
  - A scope with non-informational errors is partial.
  - Assert `len(outcome.Scopes) == len(plan.Scopes)`, and that every scope result has a status.
- `plugin.go`: `io.EOF` returns nil, and any other error is returned. Use `stream.Context()`.
- A contract test in `internal/architecture` (or a shared plugin test kit) that each provider
  module runs: inject a failure into one service and expect a `partial` scope.

## Decision

PRD decision 4: yes, any non-informational service error makes the scope (and so the scan) `partial`.

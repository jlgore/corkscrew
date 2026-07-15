# Architecture deepening program

Status: implemented

## Outcome

Make official and custom providers first-class runtime registrations, complete vertical application workflows, and give inventory and graph consumers one normalized data-access module.

## Decisions

- Provider manifests are required and may be JSON or YAML; managed installs normalize to JSON.
- Official names are reserved. Unsigned custom executables are allowed with warnings.
- Provider protocol v2 remains compatible.
- Custom resources default to a core-owned generic table.
- Explicit persistence failures fail scans; optional default persistence failures warn.
- Any failed scan scope produces a partial result and nonzero command status.
- The experimental orchestrator is retired rather than redesigned.

## Acceptance criteria

- Custom providers install, list, open, scan, and persist without a core edit.
- CLI, server, and TUI adapters contain no provider lifecycle or scan execution policy.
- Inventory and graph consumers do not enumerate official provider tables.
- Local DuckDB and Quack implementations satisfy the same data-access contract.
- Architecture guardrails and the full Go suite pass.

## Verification

- Implemented on 2026-07-14.
- `GOCACHE=/tmp/corkscrew-gocache go test ./...` passes.
- The graph integration test clears `PATH` and exercises the packaged extension in-process.

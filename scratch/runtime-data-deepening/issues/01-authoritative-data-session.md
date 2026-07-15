# Make the data session the authoritative storage-opening module

Status: complete

Completed: 2026-07-14

## Completion evidence

- `internal/data.OpenSession` resolves targets, opens local or Quack adapters,
  applies every schema migration, loads requested trusted graph capability, and
  closes failed construction attempts.
- Query, graph-loader, Quack serving, and scan-persistence callers now open
  through `internal/data`.
- Query-owned JSON installation and the superseded unified-database and
  graph-loader opening constructors were removed.
- Architecture tests reject high-level storage opening outside `internal/data`.
- Local `~` paths are expanded by the session, and hermetic remote-adapter tests prove lifecycle and persistence behavior without an external Quack process.

Type: AFK

## Parent

`scratch/runtime-data-deepening/PRD.md`

## What to build

Make one high-level data-session module establish every invariant required to use Corkscrew storage. Local DuckDB and remote Quack remain separate adapters behind the same seam, while target resolution, schema lifecycle, extension capability, connection ownership, and failure cleanup become one implementation concern.

All current query, graph, persistence, Inventory, and Relationships callers must finish this issue using that module rather than independently opening or preparing storage.

## Problem Statement

High-level callers currently repeat target resolution, database opening, extension setup, connection-pool policy, and schema readiness. Some opening paths run the schema lifecycle and others do not. A caller therefore needs implementation knowledge that ADR-0001 and ADR-0004 say should be hidden by the data-session module.

## Solution

Deepen the data session so successful construction means the target is connected, the current storage schema exists, requested trusted extensions are loaded, capabilities are known, and ownership is explicit. Migrate every high-level caller, then delete the superseded initialization paths.

## Acceptance criteria

- [x] Opening a new local target through the data session creates the current storage schema.
- [x] Opening an existing target upgrades it before repositories or read-only queries are returned.
- [x] Remote Quack opening applies the same schema-lifecycle contract after attachment.
- [x] Initialization failure closes every connection acquired during the attempt.
- [x] Inventory, Relationships, query execution, graph loading, and scan persistence use the authoritative opening path.
- [x] JSON and graph extension behavior is explicit session capability rather than caller-owned setup.
- [x] No high-level caller independently installs extensions or decides whether to run the schema lifecycle.
- [x] Architecture tests reject new high-level storage-opening paths outside the owning modules.

## Commits

1. Add a behavioral test that opens an empty local target through the data session and reads the current schema version plus normalized inventory objects.
2. Add a behavioral test that opens an older schema and observes a completed upgrade before the session is usable.
3. Add a failure-ownership test proving a schema-lifecycle error leaves no live connection owned by the failed session.
4. Make local data-session construction establish the schema lifecycle without changing its public read behavior.
5. Add equivalent contract coverage for the remote adapter using the existing hermetic remote test seam.
6. Consolidate target parsing, default-path resolution, connection ownership, and schema readiness inside data-session construction.
7. Move extension loading and capability reporting behind data-session construction while preserving trusted-extension restrictions.
8. Migrate normalized Inventory and Relationships construction to the deepened session without observable changes.
9. Migrate query execution to receive an initialized session; preserve named parameters, validation, result shapes, and statistics.
10. Delete query-owned extension installation, autoinstall policy, and duplicate target-opening behavior.
11. Migrate graph loading and graph reads to the initialized session while preserving normalized resource and canonical relationship queries.
12. Migrate scan persistence opening to the initialized session while preserving local and Quack authentication behavior.
13. Delete superseded high-level database constructors and pass-through graph-loading initialization.
14. Add an architecture guardrail that permits low-level connection creation only inside the storage adapter and authoritative data-session implementation.
15. Run package-level behavior tests, the hermetic provider harness, and the full repository suite.

## Decision Document

- The data session is the high-level storage-opening module.
- Low-level DuckDB connection construction remains an implementation detail used by local and Quack adapters.
- Schema readiness is guaranteed on every successful high-level open, including remote targets.
- Read-only query execution still uses rollback-only transactions after schema readiness is established.
- Trusted graph-extension loading remains opt-in and is reported as a session capability.
- Callers do not receive an uninitialized session.
- Failed construction owns and closes all partially created resources.

## Testing Decisions

- Tests assert behavior through a successfully opened session, Inventory, Relationships, and guarded read-only execution.
- Local tests use temporary DuckDB targets.
- Remote tests use the existing hermetic adapter seam and never require an external Quack server.
- Failure tests observe resource ownership and error behavior, not private call ordering.
- Existing normalized data-session and query-engine tests provide prior art.

## Blocked by

None - can start immediately.

## Out of Scope

- Changing the DuckDB or Quack wire protocols.
- New storage schema entities.
- General-purpose mutation through the read-only query interface.
- Graph rendering changes.

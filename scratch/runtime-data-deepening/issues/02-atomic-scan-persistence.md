# Persist each scan outcome atomically

Status: complete

Completed: 2026-07-14

## Completion evidence

- `data.Session.PersistScanOutcome` commits deduplicated provider resources,
  canonical relationships, and scan metadata in one DuckDB/Quack transaction.
- Behavioral tests inject resource, relationship, and metadata write failures
  and prove complete rollback, including preservation of a prior replacement.
- Stable outcome IDs make retries idempotent, and partial status plus failed
  scopes are stored in scan metadata.
- Provider-owned Azure and GCP database integrations were deleted; provider plugins cannot bypass application-owned persistence.
- Concurrent results are deduplicated in requested scope order, independent of worker completion timing.

Type: AFK

## Parent

`scratch/runtime-data-deepening/PRD.md`

## What to build

Persist one scan outcome—resources, embedded relationships, and scan metadata—as one transaction. A successful call exposes the entire outcome through normalized inventory, canonical relationships, and scan history. Any failure rolls back all three.

The same behavior must apply to official-provider adapters, the generic custom-provider adapter, local DuckDB, and remote Quack targets.

## Problem Statement

Persistence currently commits resources and relationships in smaller independent operations, while scan metadata follows another path. A fatal persistence error can therefore leave a prefix of a scan visible even though the application reports failure.

## Solution

Deepen scan persistence around an atomic scan outcome. Specialized provider row conversion remains internal implementation detail, but every write participates in one caller-owned transaction and one commit decision.

## Acceptance criteria

- [x] Successful persistence commits resources, relationships, and one scan-metadata record together.
- [x] A failure while writing any resource rolls back all outcome writes.
- [x] A failure while writing any relationship rolls back all resources and scan metadata.
- [x] A failure while writing scan metadata rolls back all resources and relationships.
- [x] Existing rows are not partially updated when a replacement scan fails.
- [x] Resource and relationship deduplication remains deterministic.
- [x] Partial scan outcomes can be persisted with metadata recording their partial status and failed scopes.
- [x] Generic custom-provider resources use the same transaction semantics without provider-specific core logic.
- [x] Explicit persistence failure remains fatal; optional persistence failure remains a structured warning.

## Commits

1. Add a behavioral rollback test that fails after the first resource and proves normalized inventory, canonical relationships, and scan history remain unchanged.
2. Add rollback tests for relationship failure and scan-metadata failure.
3. Add a successful-outcome test covering an official provider and a generic custom provider in the same storage schema.
4. Introduce the persisted scan-outcome concept at the existing persistence seam without changing callers.
5. Make provider resource adapters execute against a transaction supplied by the persistence implementation.
6. Move generic custom-provider persistence into the same transaction contract.
7. Move canonical relationship persistence into the same transaction as its resources.
8. Move scan-metadata persistence into that transaction and record complete, partial, and failed outcome status consistently.
9. Commit only after all resources, relationships, and metadata have succeeded.
10. Preserve deterministic deduplication before writes and verify retries remain idempotent.
11. Migrate the current scan caller to persist one outcome rather than separate resource and metadata operations.
12. Preserve the explicit-fatal and optional-warning policies through behavioral tests at the application seam.
13. Delete per-resource and relationship-owned transaction creation that is no longer reachable.
14. Add an architecture test that prevents scan outcome writes from bypassing the atomic persistence implementation.
15. Run storage, data-access, scan, hermetic integration, and full repository tests.

## Decision Document

- Transaction scope includes resources, relationships, and scan metadata.
- Partial provider execution is a valid persistable outcome; persistence failure is not.
- Provider-specific conversion may vary internally, but commit semantics do not.
- Deduplication happens deterministically before persistence.
- Explicit and optional persistence policies remain application decisions outside the transaction implementation.
- Retrying an outcome must not duplicate canonical resources or relationships.

## Testing Decisions

- Tests inject failures at observable storage-write stages and assert the final normalized state.
- Tests never assert private transaction call order.
- Temporary DuckDB files provide the primary transaction proof.
- The custom-provider harness proves generic resources and relationships participate without core provider branching.
- Existing graph-store, schema-lifecycle, and normalized data-access tests provide prior art.

## Blocked by

- `scratch/runtime-data-deepening/issues/01-authoritative-data-session.md`

## Out of Scope

- Historical snapshot retention.
- Cross-scan reconciliation or resource deletion policy.
- Storage table redesign.
- Distributed transactions across multiple database targets.

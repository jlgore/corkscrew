# Deepen scan, provider management, and storage modules

Status: complete

Completed: 2026-07-14

The authoritative data session, atomic scan outcome persistence, deep scan
application workflow, and manifest-driven build/install workflow are now in the
working tree. The legacy `pkg/smartscan` and `pkg/plugins` modules and their
duplicate opening/discovery behavior were deleted.

## Problem Statement

The first architecture-deepening program established a manifest-driven provider runtime, application workflows, a versioned storage schema lifecycle, and normalized data access. Several older implementations still sit beside those seams:

- the scan application module delegates the real workflow to a legacy smart-scan module;
- plugin build and status behavior still use filename discovery and a duplicate provider catalog;
- high-level database callers repeat opening, extension, and schema-readiness policy;
- persistence commits resources, relationships, and scan metadata independently.

These duplicates reduce locality, allow behavior to drift, and make accepted ADR invariants optional at some call sites.

## Outcome

Corkscrew has four deep modules with unambiguous ownership:

1. The data session owns every high-level DuckDB and Quack opening invariant.
2. Scan persistence commits resources, relationships, and scan metadata atomically.
3. The scan application module owns the complete scan use case and returns transport-neutral outcomes.
4. Provider management builds, packages, installs, lists, and reports status through the manifest-driven runtime.

## User stories

- As a caller, I can open Corkscrew storage without knowing path, extension, schema-lifecycle, or connection-ownership rules.
- As an operator, a failed persistence attempt never leaves a partial scan in storage.
- As an adapter author, I can execute a scan without importing provider runtime, protobuf scheduling, DuckDB, or rendering policy.
- As a provider developer, `plugin build` leaves the built provider installed and immediately visible to the runtime.
- As a maintainer, obsolete scan and plugin implementations are deleted instead of retained as compatibility paths.

## Decisions

- Breaking removal of obsolete in-repository Go interfaces is allowed; no compatibility shim is required after callers migrate.
- `plugin build` automatically packages and installs the provider through the managed manifest path.
- One persistence transaction includes resources, relationships, and scan metadata.
- The schema lifecycle runs automatically for every high-level local DuckDB and remote Quack opening, consistent with ADR-0001.
- `scanexec` is the sole multi-scope scheduler and aggregator.
- Manifest default scopes replace provider-name switches and hard-coded region catalogs.
- Rendering remains adapter-owned.
- Official-provider catalog metadata remains product metadata, not an execution allowlist.
- Provider plugins return discovered data and schema metadata but never open Corkscrew storage.
- The application `Outcome` is the sole JSON, CSV, table, and saved-file rendering contract; legacy empty-output controls are removed.
- TUI Quick Scan runs the single-provider application workflow for each enabled provider in deterministic name order.
- Managed official installations override same-named local builds while local-only official builds remain discoverable.

## Delivery order

1. `01-authoritative-data-session.md`
2. `02-atomic-scan-persistence.md`
3. `03-deep-scan-application-workflow.md`
4. `04-manifest-provider-management.md` may proceed independently, but should merge after the shared architecture guardrails are established.

## Program acceptance criteria

- [x] Every high-level data consumer opens storage through the data-session module.
- [x] Every successful opening establishes the current storage schema lifecycle.
- [x] Failed scan persistence rolls back resources, relationships, and scan metadata together.
- [x] Every implemented scan adapter uses the same application workflow and outcomes; a future server scan endpoint must do the same.
- [x] Multi-scope execution has one implementation.
- [x] `plugin build` produces a manifest-backed managed installation visible to the runtime.
- [x] Filename-only plugin discovery and duplicate provider catalogs are removed.
- [x] The hermetic custom-provider harness proves the complete path after the refactor.
- [x] Architecture guardrails prevent the removed ownership patterns from returning.
- [x] The full Go test and vet suites pass.

## Out of scope

- Provider protocol v3.
- New provider capabilities.
- Storage table redesign or new storage schema entities beyond metadata needed for atomic scan persistence.
- UI redesign.
- Changing Quack wire behavior.
- Crash-safe concurrent managed activation; that remains a separate architecture candidate.

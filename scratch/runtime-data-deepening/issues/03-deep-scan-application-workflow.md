# Make the scan application module own the complete workflow

Status: complete

Completed: 2026-07-14

## Completion evidence

- `internal/app/scan.Run` owns configuration precedence, manifest scope
  defaults, runtime/session lifecycle, capability gating, `scanexec` execution,
  atomic persistence, typed errors, events, outcomes, and warnings.
- The CLI renders the application `Outcome` as the sole JSON/CSV/table and saved-file contract. JSON and CSV stdout remain machine-only; notices use stderr.
- TUI Quick Scan invokes the same application workflow sequentially for every enabled provider and continues after per-provider failures.
- The hermetic fixture enters through this application seam for complete,
  partial, explicit-persistence, and optional-persistence behavior.
- `pkg/smartscan` and its second scheduler, provider switches, printing, and
  persistence implementation were deleted. No server scan invocation exists
  today; any future endpoint must use the same application workflow.

Type: AFK

## Parent

`scratch/runtime-data-deepening/PRD.md`

## What to build

Give adapters one transport-neutral scan application workflow that resolves configuration and manifest defaults, opens the provider runtime, executes all scopes, persists the atomic outcome, and returns structured results, events, warnings, and typed errors.

Make the provider-neutral scan executor the only scheduler and aggregator. Move rendering and file output to adapters, migrate every caller, then delete the obsolete smart-scan module and hard-coded provider scope logic.

## Problem Statement

The current scan application module mainly normalizes options before delegating to a legacy module. The legacy module still owns runtime creation, provider initialization, scope policy, scheduling setup, persistence policy, file output, and global stdout rendering. A second multi-scope implementation duplicates the provider-neutral executor and has already drifted in deduplication and default-scope behavior.

## Solution

Deepen the scan application module around the entire use case. Its interface returns application-owned outcomes and events. Runtime, protobuf scheduling, and persistence become implementation knowledge; rendering remains adapter knowledge. Delete the alternate execution and compatibility paths once callers migrate.

## Acceptance criteria

- [x] One application call performs configuration resolution, provider opening, multi-scope execution, atomic persistence, and outcome construction.
- [x] Manifest default scopes are authoritative when explicit or configured scopes are absent or request `all`.
- [x] The provider-neutral executor is the only scheduler, timeout owner, aggregator, and deduplicator.
- [x] Successful, partial, and failed execution return structured outcomes with typed errors and scope events.
- [x] Explicit persistence failure is fatal after preserving the execution outcome; optional failure is returned as a warning.
- [x] Application code does not print, choose table/JSON/CSV presentation, or write result files.
- [x] Every implemented scan adapter consumes the same workflow behavior; a future server endpoint must use it.
- [x] The obsolete smart-scan module, alternate scheduler, provider-name scope switches, and compatibility interfaces are deleted.
- [x] The hermetic custom-provider test exercises the complete application workflow rather than calling the legacy scan module.
- [x] Architecture tests prevent adapters from importing runtime, scan executor, or storage persistence directly.

## Commits

1. Add an application-level tracer test that sends the fixture custom provider through configuration, runtime, execution, and atomic persistence.
2. Add application-level tests for successful, partial, failed, explicit-persistence, and optional-persistence outcomes.
3. Introduce application-owned request, outcome, warning, and event vocabulary while adapting the existing implementation behind it.
4. Move configuration loading, provider-enabled validation, and explicit-option precedence into the application implementation.
5. Move configured-scope and manifest-default-scope resolution into the application implementation.
6. Delete provider-name scope discovery and hard-coded region catalogs after manifest-default tests are green.
7. Move provider runtime creation, session opening, initialization configuration, and capability gating into the application implementation.
8. Invoke the provider-neutral executor directly and map its scope results, statistics, events, and typed errors into the application outcome.
9. Migrate filtering and summary calculation without retaining a second scheduler or deduplicator.
10. Integrate atomic outcome persistence and preserve required-versus-optional failure policy as structured application behavior.
11. Change the CLI adapter to render the returned `Outcome` as the sole contract, with machine-readable stdout and preserved exit status.
12. Move timestamped file output to the CLI rendering adapter.
13. Migrate TUI scan invocation to the same application outcome.
14. Migrate server scan invocation to the same application outcome where supported, without transport-specific policy in the application module.
15. Redirect the hermetic end-to-end harness through the application interface and remove global stdout capture from the workflow test.
16. Delete the obsolete multi-scope scanner implementation and its divergent deduplication, prioritization, and provider switches.
17. Delete the remaining legacy smart-scan module and compatibility types after all callers compile against the application module.
18. Add architecture guardrails for single scheduler ownership, adapter-only rendering, and scan persistence access.
19. Run adapter, application, executor, persistence, integration, architecture, and full repository tests.

## Decision Document

- Breaking deletion of the obsolete smart-scan interfaces is explicitly allowed.
- The scan application module owns the use case; adapters own syntax and rendering.
- The provider-neutral executor is the sole multi-scope implementation.
- Manifest default scopes replace provider-name switches.
- The application outcome preserves successful resources even when some scopes fail.
- Partial execution is returned with a typed nonzero error after persistence.
- Persistence warnings are structured data, not direct stdout writes.
- File output is rendering and remains adapter-owned.
- Legacy output compatibility and empty-output controls are intentionally removed rather than maintained as a second rendering contract.

## Testing Decisions

- Tests enter through the application interface and assert outcomes, persisted normalized data, events, warnings, and typed errors.
- The fixture provider supplies real protocol-v2 runtime behavior without credentials or network access.
- Rendering tests remain adapter tests and assert output separately from workflow behavior.
- Existing scan executor and custom-provider integration tests provide prior art.
- No test substitutes an entire alternate workflow callback; seams represent actual runtime, persistence, or rendering variation.

## Blocked by

- `scratch/runtime-data-deepening/issues/02-atomic-scan-persistence.md`

## Out of Scope

- New scan capabilities or provider protocol changes.
- Interactive UI redesign.
- Atomic cross-provider persistence or a new multi-provider application workflow.
- Historical resource reconciliation.

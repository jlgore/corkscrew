# Add an assertion helper and the most important invariants

Status: complete
Phase: P5

There are 3 `panic`s across 1,903 Go functions. Rather than retrofitting assertions everywhere,
add `internal/assert` (`assert.That(cond, msg)`, never compiled out) and put assertions where the
audit found that a broken invariant does real damage:

- `scanexec.Execute`: every scope has a result, and `Σ scope resources == TotalResources` before
  dedupe.
- `findings.Evaluate`: `Opened + Recurred == len(Findings)`.
- `archive` (issue 05): the destination is absolute and clean, and the bytes written are
  `<= BytesMax`.
- `paging.Guard` (issue 12): `page <= pagesMax`.
- `loadPackInternal`: `len(loadPath) <= packDepthMax`.
- AWS `BatchScan`: `len(results) == len(refs)` after draining.
- The TUI progress bounds (issue 02).

Rust: use `assert!`, not `debug_assert!`, for these invariants. The crate currently has 0 runtime
assertions.

Document the rule in `docs/` and in CONTEXT-adjacent contributor notes: assertions are for
programmer errors, and operating errors go into `Errors`.

## Comments

2026-10-21: Implemented on `hardening/p5`. Rule documented in
`docs/contributing/assertions.md` (linked from README and AGENTS.md).

- `internal/assert`: `That`, `Thatf`; panics with `assert.Failure` (implements `error`, prefix
  `assertion failed: `). `internal/assert/asserttest` (`Fails`, `FailsWith`) rejects any other
  panic value. Each invariant has a test that breaks it through a seam.
- `scanexec.Execute` split into `dispatchScopes`/`scanScope`/`collectScopeResults`/
  `aggregateScopes`; asserts result slots, Σ scope resources == merged count before dedupe,
  dedupe never grows, unreachable <= failed.
- `findings`: `recordResults` split out; `assertEvaluationTotals` (Opened + Recurred ==
  len(Findings), both >= 0).
- `archive`: `checkDestination` became `assertDestination` (callers already use
  `filepath.Abs`); budget tracks written and skipped, `written + skipped + remaining == BytesMax`.
- `paging.Guard.Next`: pages < pagesMax on entry, <= after.
- Packs: new `packDepthMax = 32`. Manifests are user data, so a deeper chain is an error; the
  assertion only guards the already-checked path.
- AWS `describeAll`/`drainDescribed`: one resource per launched describe; launched ==
  len(refs) without cancellation. orgscan: a blank account ID from `ListAccounts` is now a
  listing error, not an assertion.
- TUI: `quickScanProgress` bounds, `beginQuickScan` has providers, history cap.
- Ad-hoc invariant `panic`s in findings, scanexec, paging, tui, db, config, aws, gcp, azure and
  github converted. Left: the template panic in the kubernetes scanner generator (generators
  are a non-goal).
- Not asserted: provider `Stats.TotalResources` (plugin data; the sum is checked against our
  own merge instead).
- Rust: `assert!` in `next_chunk`, `hops_to_i32`, BFS hop bounds, list column offsets, cache
  bounds. Issue 04's `params::guard` (`catch_unwind`) turns a failed assertion into a query
  error; a compile-time check rejects `panic = "abort"`, and a test proves the connection
  survives.

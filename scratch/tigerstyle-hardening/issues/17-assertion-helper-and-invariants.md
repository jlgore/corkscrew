# Add an assertion helper and the most important invariants

Status: ready-for-agent
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

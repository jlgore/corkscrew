# Make the graph extension's FFI boundary panic-free

Status: complete
Phase: P0

## Problem

duckdb-rs's `extern "C"` vtab trampolines don't `catch_unwind`, so any panic in bind, init or func
**aborts the host process**.

- `functions/{paths,reachable,blast,match_pattern,info,correlate_*}.rs` bind calls
  `get_parameter(n).to_string()` on a SQL `NULL`. Per `traverse.rs:154`, this "aborts on NULL".
  Only `traverse.rs` parameter 4 is guarded.
- 17 calls of `init.get_bind_data::<T>().as_ref().unwrap()` with no `// SAFETY:` comment.
- Numbers go through `to_string().parse()`. A NaN `min_confidence` survives `clamp`, which silently
  disables the filter. `hops as i32` (`common.rs:128,238,287`) relies on an unstated range.
- 5 calls of `Mutex::lock().unwrap()` in `cache.rs`, which poisons the lock and panics on the next
  call.

## Change

- One checked parameter reader per type (`param_varchar`, `param_i64`, `param_f64`) that returns
  `Err` for NULL or non-finite values. Use it in every bind.
- Replace bind-data unwraps with `.ok_or("missing bind data")?`. Add `// SAFETY:` to each of the
  19 `unsafe` blocks.
- Wrap each vtab callback body in `std::panic::catch_unwind` and turn a panic into a DuckDB error,
  as defense in depth.
- Use `i32::try_from` for hops, with a named `HOPS_MAX` constant.

## Acceptance

- In `extensions/corkscrew-graph-tests`: for each table function, NULL in every parameter position
  returns a SQL error and the process survives. Same for `'NaN'::DOUBLE`.
- `cargo clippy --locked -- -D warnings -W clippy::undocumented_unsafe_blocks` is clean.
- Note: the local `build/` artifact targets DuckDB 1.5.6 while the installed CLI is 1.5.5. The test
  run needs a matching DuckDB.

## Comments

2026-10-21: Implemented. New `functions/params.rs` holds `param_varchar`, `param_i64`,
`param_f64`, `param_bool` and `param_hops`. They reject NULL and non-finite values, and limit hops to
`0..=HOPS_MAX` (4096). It also holds `bind_data::<V>()`, which replaces the bind-data unwraps,
`hops_to_i32`, and `guard`. `guard` wraps every bind/init/func body in `catch_unwind` and escapes
NUL bytes, because duckdb-rs's `set_error` unwraps `CString::new` outside our frame. `cache.rs`
recovers poisoned locks with `into_inner`. Every `unsafe` block has a `// SAFETY:` comment. Tests in
`corkscrew-graph-tests` cover NULL in every position of every table function, NaN and ±Infinity for
every DOUBLE, out-of-range hops, and `guard` itself. Clippy still reports 4 pre-existing `dead_code`
errors in `graph/loader.rs` and `patterns/mod.rs`, which this issue does not touch. duckdb-rs's
`TableFunctionInfo::get_init_data` still unwraps internally, but `guard` now catches that panic.

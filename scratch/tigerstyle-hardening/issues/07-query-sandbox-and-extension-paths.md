# Sandbox compliance and graph SQL, and load extensions only from trusted paths

Status: complete
Phase: P1

## Problem

- `internal/data/session.go:255-283` + `pkg/query/engine.go:126`: the read-only check is lexical.
  `SELECT * FROM read_text('~/.aws/credentials')` and `read_csv('https://…')` pass it, and DuckDB
  autoloads httpfs. A pack from an external git/OCI source could read local files or send data out
  over the network. (PLAUSIBLE: not exercised yet.)
- `internal/data/session.go:367-378` + `pkg/graphquery/runner.go:346`: with no `--extension`
  flag, the graph extension is resolved from paths relative to the working directory, on a
  connection with `allow_unsigned_extensions` on.
- `pkg/query/compliance/simple_executor.go:106`: compliance queries run under
  `context.Background()`, with no timeout.

## Change

- After the extensions load, compliance and graph sessions run
  `SET enable_external_access=false; SET lock_configuration=true`. Ad hoc `query` gets an opt-in
  `--sandbox` flag (PRD decision 2).
- Resolve extensions only from absolute paths under `~/.corkscrew` or an explicit `--extension`.
  Remove the candidates relative to the working directory.
- Take `ctx` in `ExecuteOptions` and give each query `context.WithTimeout`, using a named
  `queryTimeout` constant.

## Acceptance

- Test: a pack query that calls `read_text` on a temp file fails with a DuckDB permission error.
- Test: a pathological query is cancelled at the timeout.
- Check that the graph extension still loads before the lockdown.

## Comments

2026-10-21: Implemented. `data.Session.Sandbox(ctx)` runs `SET enable_external_access = false`
then `SET lock_configuration = true` (DuckDB 1.5.6). `data.WithSandbox()` calls it at the end of
`OpenSession`, after the graph `LOAD`. Compliance (`compliance.NewExecutor`, via
`query.NewSandboxedEngine`, so `findings` too) and `corkscrew graph` are always sandboxed. Ad hoc
`corkscrew query --sandbox` opts in through `appquery.Request.Sandbox`. Checked by test: the open
database file still answers queries, `read_text`/`read_csv`/URLs fail with
`Permission Error ... disabled by configuration`, and `SET`/`RESET` fail with
`the configuration has been locked`. Graph: the extension's table functions open `db_path`
through their own DuckDB instance (`Connection::open` in `functions/common.rs` and the correlate
functions), which this sandbox does not cover, so graph queries keep working. Tested with the built
extension: `graph_traverse`, `graph_correlate_ips` and `graph_cache_invalidate` all succeed after
the lockdown. Left open: the extension's `db_path` argument can still open any DuckDB file. It
cannot read other file types (it fails with "not a valid DuckDB database file"), and corkscrew
generates `db_path` itself. Remote Quack sessions are sandboxed too. Re-running
`ATTACH IF NOT EXISTS` and `LOAD` for an already-loaded extension on new pooled connections works
after the lockdown, but this was tested against a local stand-in, not a live Quack server.
Extensions now resolve only from `--extension`, an absolute `CORKSCREW_GRAPH_EXTENSION`, or
`~/.corkscrew/extensions/corkscrew_graph.duckdb_extension`. The resolver is no longer recursive.
`make install-graph-extension` copies the build there. Compliance `Execute` now takes `ctx`, and
each query runs under `context.WithTimeout` (`queryTimeout` = 2m, `ExecuteOptions.QueryTimeout`
overrides it). A test cancels a billion-by-billion `range` cross join at 200ms with
`context.DeadlineExceeded`.

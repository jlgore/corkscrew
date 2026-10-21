# Sandbox compliance and graph SQL, and load extensions only from trusted paths

Status: ready-for-agent
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

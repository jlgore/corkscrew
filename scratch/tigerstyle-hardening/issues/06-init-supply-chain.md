# Verify binaries installed by `corkscrew init` and bound its subprocesses

Status: ready-for-agent
Phase: P1
Depends on: 05

## Problem

- `cmd/corkscrew/init.go:382,433-505`: protoc and duckdb are downloaded from GitHub and written
  into `~/.corkscrew/bin` as 0755 executables, and that directory is added to `$PATH` through the
  shell rc file. Nothing verifies them.
- `init.go:365-382`: `client.Head`/`client.Get` ignore `ctx`, and `ContentLength == -1` breaks the
  progress calculation.
- `init.go:864`, `:696-802`: `make build-<p>-plugin` and the generators' `go run` use
  `exec.Command` with `Dir="."` and no deadline. Running `init` inside an untrusted checkout runs
  that checkout's Makefile.

## Change

- A table in source mapping (tool, version, GOOS/GOARCH) to SHA-256. Verify the temp file before
  extracting, and fail closed when the tuple is missing from the table.
- Build requests with `http.NewRequestWithContext`, and treat `ContentLength <= 0` as unknown.
- Run subprocesses with `exec.CommandContext` and a timeout. Resolve the plugin source directory
  explicitly instead of using `.`, and print it before running anything.

## Decision

PRD decision 3: SHA-256 values pinned in source; fail closed for an unpinned tuple.

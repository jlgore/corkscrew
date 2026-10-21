# Verify binaries installed by `corkscrew init` and bound its subprocesses

Status: complete
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

## Comments

**2026-10-21:** Done. `cmd/corkscrew/init_pins.go` holds SHA-256 pins for protoc 25.3 (6
platforms) and DuckDB CLI 1.5.2 (5 platforms; both darwin arches share osx-universal). The pin is
looked up before any request, so an unpinned (tool, version, platform) fails closed with no
download, and the error lists the pinned tuples. The temp archive is hashed before
`archive.ExtractFile`; a mismatch names expected and actual digests and removes the archive.
Versions are hard-coded in `runInit` (no flag or config override), so no override path exists to
bypass the table.

Provenance: DuckDB pins match a local download and the GitHub release API `digest`. protoc v25.3
assets have no API `digest`; their pins match a local download and independent third-party pins
(taiki-e/install-action, cashapp/hermit-packages, winget-pkgs).

Found while pinning: the DuckDB URL builder mapped linux-arm64 to `linux-aarch64` and
windows-amd64 to `win-amd64`. Neither asset exists (404), so init could never have installed
DuckDB there. The URL builders now derive the URL from the archive name, and the names match the
release assets.

HTTP: the HEAD request is gone (the GET carries the same Content-Length). The GET uses
`http.NewRequestWithContext`, and `runInit` uses `signal.NotifyContext` so Ctrl-C cancels it.
`ContentLength <= 0` is recorded as an unknown total of 0, and the reported size comes from bytes
written.

Subprocesses: `runBounded` wraps `exec.CommandContext` with `generatorTimeout` (10m) and
`pluginBuildTimeout` (20m), sets `WaitDelay` so a killed `go run` does not hold the output pipe,
and prints the directory and command. Generation and builds run only when the working directory
has a go.mod declaring `github.com/jlgore/corkscrew` and a `plugins/` directory. Otherwise both
steps are skipped with a message. A hostile fork can still claim the module path, which is why
each command is printed before it runs.

Not changed: the DuckDB CLI is 1.5.2, but the Go bindings are 1.5.6 (duckdb-go v2.10506.0).
`runInit` was already well over 70 lines and grew by the source-root check.

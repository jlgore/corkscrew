# Share one safe, bounded archive extractor

Status: ready-for-agent
Phase: P1

## Problem

- `pkg/query/compliance/registry.go:883`: the check
  `strings.HasPrefix(targetPath, destDir)` has no trailing separator. The entry
  `root/../acme-prod/manifest.yaml`, extracted into `.../packs/acme`, passes the check and
  overwrites the sibling pack `acme-prod`.
- No size limit on the download (`:813`) or on decompression (`:900`). The checksum is skipped
  when it is empty (`:763`).
- `cmd/corkscrew/init.go:407,462,497`: the same unbounded `io.Copy` for downloads, tar and zip.
  `HasSuffix(name, binaryName)` matches `evil-protoc`, and zip entries have no type check.
- `pkg/query/compliance/registry.go:921`: the retry `time.Sleep` ignores ctx, and the
  JSON-configurable backoff has no upper limit.

## Change

New package `internal/archive` with one extractor for tar.gz and one for zip, taking
`Limits{BytesMax, EntryCountMax, EntryBytesMax}`. The extractor:

- confines paths with `filepath.Rel` and rejects any result starting with `..`, absolute paths,
  and non-regular entries;
- wraps every read in `io.LimitReader(r, max+1)` and fails at `max+1`;
- asserts that the destination is absolute and clean.

Use it in `registry.go` and `init.go`. Require a non-empty checksum for registry packs (a
`--allow-unverified` escape hatch is a decision for the PR). The retry sleep selects on
`ctx.Done()`, with a `retryDelayMsMax` constant.

## Acceptance

Table tests for: `..` traversal, a sibling-prefix escape, an absolute path, a symlink, a hardlink,
a gzip bomb over `BytesMax`, too many entries, an entry over `EntryBytesMax`, and an exact binary
name match.

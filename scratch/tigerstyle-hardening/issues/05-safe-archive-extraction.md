# Share one safe, bounded archive extractor

Status: complete
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

## Comments

**2026-10-21:** Done. `internal/archive` provides `ExtractTree(format, path, destDir, strip,
limits)` for pack trees and `ExtractFile(format, path, name, destPath, limits)` for one binary.
Both use stdlib only. Directory entries are skipped, and parents are created as needed. PAX global
headers are skipped because every GitHub tarball has one. Escaping names are an error, not a
silent skip. Tar entries are charged by their declared size even when skipped, because the reader
still decompresses them. `ExtractFile` writes through `<dest>.partial` and renames, so a failure
never leaves a truncated binary. Limits: packs 64 MiB download, 64 MiB total, 10k entries, 16 MiB
per entry. Binaries 512 MiB for download, total and entry. The retry cap is named `retryDelayMax`
(a `time.Duration`, not ms), plus `retryCountMax = 10`. A missing checksum is now a hard error.
`--allow-unverified` was not added because no flag or option path reaches `DownloadPack`. A
registry namespace that escapes `destDir` is also rejected.

2026-10-21: Maintainer decision: a missing pack checksum warns instead of failing. Registry versions
come from GitHub release tarballs and never carry one, so a hard requirement would make every pack
uninstallable. A present checksum is still verified, and a mismatch is fatal. Real verification is
tracked in issue 19.

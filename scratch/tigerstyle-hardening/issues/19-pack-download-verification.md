# Verify registry pack downloads

Status: needs-triage
Phase: P1
Depends on: 05

## Problem

- `RegistryPackVersion.Checksum` (`pkg/query/compliance/registry.go`) is never populated.
  `fetchPackVersions` builds versions from GitHub release tarballs, which carry no checksum, so
  no registry pack download is actually verified.
- External packs are meant to be first-class, so an "official packs only" checksum table is not
  enough.

## Change

Pick one, or combine them:

- **Published asset.** Packs publish a checksum release asset, such as
  `<pack>.tar.gz.sha256` or one checksums file per release. `fetchPackVersions` reads it into
  `Checksum`, and `DownloadPack` downloads that release asset rather than the auto-generated
  source tarball.
- **Lockfile pin.** A local lockfile records the SHA-256 on first install (trust on first use),
  and a later download of the same version that hashes differently fails.

GitHub's auto-generated source tarballs are not guaranteed to be byte-stable across time, so
pinning their hash can produce false mismatches. That favors published assets over pinning
auto-tarballs.

## Acceptance

- A pack whose published checksum or pinned hash does not match is rejected.
- Tests cover a matching download, a mismatch, and a missing checksum asset.

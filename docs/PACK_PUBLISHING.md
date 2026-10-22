# Publishing a compliance pack

`corkscrew pack install` verifies every registry pack it downloads. A pack release has to
publish the pack as a release asset together with a SHA-256 checksum for it. Without the
checksum, users can only install the release with `--allow-unverified`.

## What a release must contain

Attach these assets to a GitHub release (not a draft or prerelease):

1. **Exactly one `.tar.gz` asset: the pack.** It must hold one top-level directory with
   `manifest.yaml` inside it. Corkscrew strips the top-level directory when it installs. If a
   release has two or more `.tar.gz` assets, corkscrew can't tell which one is the pack, so it
   treats the release as unverified.
2. **A checksum for that asset**, in one of two forms:
   - `checksums.txt`: one `sha256sum` line per file, as `<hex>  <name>` (text mode) or
     `<hex> *<name>` (binary mode). Other files can be listed too.
   - `<asset>.sha256`, for example `aws-security-v1.2.0.tar.gz.sha256`. It holds either the
     bare digest or one `sha256sum` line that names the asset.

   If a release has both, `<asset>.sha256` wins.

Corkscrew downloads the asset, not GitHub's auto-generated "Source code" tarball. Those
tarballs are not guaranteed to be byte-identical over time, so no checksum could cover them.

Corkscrew parses checksum files strictly. A file larger than 64 KiB or longer than 1024 lines,
a malformed line, or two entries for the pack asset all fail the install, even with
`--allow-unverified`. A well-formed file that has no entry for the asset counts as a missing
checksum.

## Building a release

From a tagged commit:

```sh
TAG=v1.2.0
NAME=aws-security
git archive --format=tar.gz --prefix="$NAME-$TAG/" -o "$NAME-$TAG.tar.gz" "$TAG"
sha256sum "$NAME-$TAG.tar.gz" > checksums.txt
gh release create "$TAG" "$NAME-$TAG.tar.gz" checksums.txt --title "$TAG"
```

`git archive --prefix` writes the single top-level directory that corkscrew expects.

## Never re-release a version with different bytes

On first install, corkscrew records each version's SHA-256 in `corkscrew-lock.json`, in the
user's pack directory. If a later download of the same version hashes differently, the install
fails. This happens even when your new checksum file matches the new bytes, and even with
`--allow-unverified`. Users can accept the change only by passing `--repin` deliberately. To
change a pack, publish a new version.

## What users see

| Release | `pack install` |
|---|---|
| Asset plus a matching checksum | Installs and pins the hash |
| Asset plus a checksum that does not match | Fails (checksum mismatch) |
| Malformed checksum file | Fails |
| No checksum, or no single `.tar.gz` asset | Fails and suggests `--allow-unverified` |
| The same as above, with `--allow-unverified` | Warns, installs, and pins the hash on first use |
| A pinned version whose hash changed | Fails unless the user passes `--repin` |

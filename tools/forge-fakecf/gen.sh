#!/usr/bin/env bash
# Regenerates plugins/cloudflare-provider/internal/fakecf/fakecf_gen.json from Cloudflare's Forge
# spec, using the Forge commit and spec release pinned beside this script.
#
# Needs git, curl, sha256sum, cargo (rustup installs the toolchain Forge pins), node, and pnpm.
# Forge and the spec are cached under CORKSCREW_FORGE_CACHE (default ~/.cache/corkscrew/forge).
#
# To move to a newer Forge or spec, bump FORGE_COMMIT, SPEC_RELEASE, and openapi.sha256 together,
# run this, and commit the regenerated file with them.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
dest="$repo/plugins/cloudflare-provider/internal/fakecf/fakecf_gen.json"

commit="$(tr -d '[:space:]' < "$here/FORGE_COMMIT")"
release="$(tr -d '[:space:]' < "$here/SPEC_RELEASE")"
cache="${CORKSCREW_FORGE_CACHE:-$HOME/.cache/corkscrew/forge}"
forge_dir="$cache/forge-$commit"
spec_dir="$cache/spec-${release//[@\/]/-}"
spec="$spec_dir/openapi.forge.json"

if [[ ! -f "$forge_dir/.fakecf-ready" ]]; then
  echo "==> building Forge $commit"
  rm -rf "$forge_dir"
  mkdir -p "$forge_dir"
  git -C "$forge_dir" init -q
  git -C "$forge_dir" remote add origin https://github.com/cloudflare/forge.git
  git -C "$forge_dir" fetch -q --depth 1 origin "$commit"
  git -C "$forge_dir" checkout -q FETCH_HEAD
  (cd "$forge_dir" && cargo build --release --features wasm --bin forge)
  (cd "$forge_dir" && pnpm install --frozen-lockfile)
  (cd "$forge_dir/packages/forge" && node scripts/build.mjs)
  touch "$forge_dir/.fakecf-ready"
fi

if [[ ! -f "$spec" ]]; then
  echo "==> downloading spec $release"
  mkdir -p "$spec_dir"
  tag="${release//@/%40}"
  curl -fsSL -o "$spec.tmp" \
    "https://github.com/cloudflare/forge/releases/download/$tag/openapi.forge.json"
  mv "$spec.tmp" "$spec"
fi
(cd "$spec_dir" && sha256sum --check --quiet "$here/openapi.sha256")

# The plugin imports @cloudflare/forge/plugin, which isn't on npm yet: link the built package.
mkdir -p "$here/node_modules/@cloudflare"
ln -sfn "$forge_dir/packages/forge" "$here/node_modules/@cloudflare/forge"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
echo "==> building plugin"
node "$forge_dir/packages/forge/bin/forge-plugin.mjs" build "$here/plugin.ts" -o "$work/fakecf.wasm"
echo "==> generating"
"$forge_dir/target/release/forge" "$spec" --target "wasm://$work/fakecf.wasm" --out "$work/out"

mkdir -p "$(dirname "$dest")"
cp "$work/out/fakecf_gen.json" "$dest"
echo "==> wrote ${dest#"$repo/"}"

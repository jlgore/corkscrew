# forge-fakecf

Generates the schema behind the Cloudflare provider's fake API
(`plugins/cloudflare-provider/internal/fakecf`) from Cloudflare's
[Forge](https://github.com/cloudflare/forge) spec.

- `plugin.ts`: a Forge plugin. For every operation in `operations.ts`, it records the path and
  query parameters and the type of the JSON 200 response, plus every model those types reach. It
  writes them all to `fakecf_gen.json`.
- `operations.ts`: the API operations the provider calls. Add an operation here when the provider
  starts calling a new endpoint. The fake fails any request that matches none of them.
- `FORGE_COMMIT`, `SPEC_RELEASE`, `openapi.sha256`: the pins. Forge isn't on npm yet, so
  `gen.sh` builds it from the pinned commit. The spec is the `openapi.forge.json` asset of the
  pinned `cloudflare/forge` release, checked against the hash.

Regenerate with `make gen-cloudflare-fake`. You need cargo (rustup installs the toolchain Forge
pins), node 26, and pnpm. CI runs the generator and fails if the committed file is stale.

To keep up with Forge, bump the three pins together, regenerate, and fix `plugin.ts` if
`forge.wit` changed (the plugin interface is `forge:plugin@0.3.0`). Commit the pins, the plugin
fix, and the regenerated file together.

# Design official and custom provider loading

Status: ready-for-agent

## Goal

Replace the current mixture of hard-coded lists, source-directory probing, and registry fallback data with an explicit model for Corkscrew-supported providers and user-installed plugins.

## Constraints established

- `pkg/providers` describes official providers; it is not an execution allowlist.
- A custom provider must load without a core-code edit.
- Runtime plugin identity and installation metadata need one authoritative registry.
- Custom-provider persistence needs an explicit registered resource-table mapping or validated override.
- Provider discovery schemas remain metadata and cannot mutate the core storage schema.

## Open design work

- Define manifest fields, discovery paths, precedence, trust/signature policy, and name collision behavior.
- Decide how custom providers declare storage compatibility with the shared graph contract.
- Migrate existing official plugins through the same runtime registry used by custom plugins.

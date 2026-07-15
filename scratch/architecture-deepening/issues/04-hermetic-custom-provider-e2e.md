# Prove the custom-provider vertical path hermetically

Status: complete

- [x] Add a deterministic protocol-v2 fixture provider under an internal test fixture package.
- [x] Install and replace its YAML package through the managed custom-provider path.
- [x] Exercise the real registry, process runtime, capability gate, session cache, and shutdown.
- [x] Exercise provider discovery, info, list, describe, schemas, and multi-scope batch scan workflows.
- [x] Prove aggregation, deduplication, structured scope events, and typed partial failure.
- [x] Persist custom resources and relationships to temporary DuckDB storage.
- [x] Read the persisted fixture through `all_cloud_resources`, Inventory, and Relationships.
- [x] Prove fatal explicit persistence failure and warning-only optional persistence failure.
- [x] Prove CLI visibility through the `runCLI` seam.

## Comments

Implemented on 2026-07-14. The harness builds only repository-local Go source, uses temporary HOME/plugin/database directories, launches no cloud tools, and requires no network or credentials.

# Complete vertical application workflows

Status: complete

- [x] Migrate discovery, listing, info, resource listing, description, and schemas into application workflows.
- [x] Extract scan execution, structured events, and partial outcomes.
- [x] Keep persistence and rendering behind reusable smart-scan functions rather than command handlers.
- [x] Enforce explicit and default persistence failure policy.
- [x] Retire experimental orchestration, legacy plugin clients, and unused execution abstractions.

## Comments

Completed on 2026-07-14. CLI and API adapters now share the manifest-driven provider workflows, and partial scope failure returns a typed nonzero result after persistence/rendering.

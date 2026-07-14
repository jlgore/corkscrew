# Corkscrew Domain Context

Corkscrew discovers resources through provider plugins, persists normalized resource and relationship data, and exposes that data to SQL and graph consumers.

## Ubiquitous language

### Provider discovery schema

A plugin-owned description returned by `GetSchemas` that explains what a provider can discover. It is metadata for discovery and must not create or alter Corkscrew's persistent database objects.

### Official provider

A provider distributed and supported by Corkscrew. Official-provider metadata and canonical storage mappings live in `pkg/providers`. The official catalog is not an allowlist for plugin execution.

### Custom provider

A user-supplied plugin resolved at runtime by name or installation metadata. Custom providers use the same application workflows as official providers and must not require a core-code edit merely to load. Persistent storage for a custom provider requires an explicit registered mapping or validated table override.

### Storage schema

The core-owned DuckDB/Quack tables and views used for persisted resources, relationships, scan metadata, correlations, and graph queries.

### Schema lifecycle

The versioned, transactional process in `internal/db` that creates and upgrades the storage schema. All database entry points call the lifecycle before persistence or graph loading begins.

### Provider resource table

The canonical `<provider>_resources` table for a provider shipped with Corkscrew. Every provider resource table exposes the common graph columns while retaining provider-specific columns.

### Canonical relationship table

`cloud_relationships`, the only writable relationship store. `<provider>_relationships` objects are filtered compatibility views over this table.

### Legacy archive

An old table preserved as `<name>_legacy_v0` during migration. Rows are copied into the canonical schema before the migration commits.

## Ownership boundaries

- `pkg/providers` owns the catalog of providers shipped with Corkscrew.
- Provider plugins own discovery behavior and provider discovery schemas.
- `internal/db` schema lifecycle owns all persistent storage DDL and migrations.
- Graph loaders and stores consume the storage schema; they do not create it opportunistically.
- CLI and API handlers orchestrate reusable packages and should not contain storage or provider business rules.
- Application workflows under `internal/app` accept adapter requests, apply precedence and normalization, and invoke domain packages.
- CLI, TUI, and API adapters own transport syntax and rendering, not workflow policy.

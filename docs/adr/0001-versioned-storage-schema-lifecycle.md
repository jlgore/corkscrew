# ADR 0001: Versioned storage schema lifecycle

Status: Accepted

Date: 2026-07-14

## Context

Persistent DDL was split among unified schema setup, graph loading, and graph persistence. Those paths had incompatible definitions for metadata and relationship tables. The API also maintained its own four-provider list even though GitHub and Cloudflare plugins were shipped.

## Decision

Corkscrew has one versioned storage schema lifecycle in `internal/db`.

- Every local DuckDB and remote Quack entry point runs `EnsureSchema` automatically.
- A migration is executed in one transaction and recorded in `corkscrew_schema_migrations` only after all DDL and data movement succeeds.
- Version 1 creates canonical resource tables for AWS, Azure, GCP, Kubernetes, GitHub, and Cloudflare.
- `cloud_relationships` is the writable relationship table. Provider relationship names are filtered views.
- Incompatible version-0 tables are retained as `_legacy_v0`, with their rows copied into canonical tables.
- Provider resource tables expose a shared graph contract: `id`, `type`, `name`, `region`, `account_id`, `arn`, and `tags`.
- Provider `GetSchemas` results remain discovery metadata and are not authoritative storage DDL.
- Provider names, descriptions, and resource-table mappings live in `pkg/providers` for reuse by API, CLI, persistence, and plugin code.
- Unknown third-party providers fail with an explicit persistence error unless a validated table override is supplied.

## Consequences

Schema-changing features must be expressed as a new migration version. Graph and command code can assume the current storage contract after opening a database. Compatibility views keep existing provider-specific relationship queries working, while legacy archives provide a non-destructive recovery path.

Remote Quack servers receive the same unqualified migration SQL because each pooled connection attaches and selects the remote catalog before use.

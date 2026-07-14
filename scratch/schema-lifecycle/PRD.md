# Versioned storage schema lifecycle

Status: ready-for-human

## Problem

Database DDL and provider knowledge have multiple owners. This makes schema changes risky, leaves GitHub and Cloudflare out of API discovery, and encourages business logic to accumulate in CLI handlers.

## Outcome

Use a single transactional schema lifecycle for local DuckDB and remote Quack. Centralize the shipped-provider catalog, keep provider discovery schemas separate from persistent DDL, preserve legacy data, and make graph code consume rather than create the schema.

## Acceptance criteria

- Automatic, idempotent, versioned migration on every database entry point.
- Fresh schema includes all six shipped providers and the graph contract.
- Existing metadata, network records, and provider relationships migrate in place without dropping legacy tables.
- `cloud_relationships` is canonical and provider relationship names are filtered views.
- API provider listing uses the shared provider catalog.
- Unknown provider persistence fails explicitly.
- Migration failure rolls back all changes and does not advance the version.
- Reviewable historical SQL fixtures prove semantic data preservation, archive integrity, and close/reopen idempotence.
- Architecture terms and the ownership decision are documented.

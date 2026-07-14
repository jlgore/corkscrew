# Migration fixtures

These SQL files describe database shapes produced by released or historically significant Corkscrew persistence paths. Tests build temporary DuckDB files from them, run the production schema entry point, and compare semantic values before and after migration.

## Rules

- Keep fixtures as SQL rather than binary `.duckdb` files so schema changes remain reviewable and independent of DuckDB's file format.
- Treat a fixture as immutable once it represents a released schema. Add another fixture when a distinct historical shape must be supported.
- Include representative rows, nulls, JSON, timestamps, and provider-specific values—not only empty tables.
- Add before/after invariants in `migration_fixtures_test.go` for every renamed or transformed field.
- Verify archive row counts when a migration retains a table as `_legacy_vN`.
- Include a failure fixture for each migration operation that must roll back atomically.
- Open migrated files through `InitializeUnifiedDatabase`, close them, and reopen them to test the same path used by application callers.

Run the harness with:

```bash
go test ./internal/db -run TestVersionZero -count=1 -v
```

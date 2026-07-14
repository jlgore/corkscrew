# Add an automated migration compatibility harness

Status: ready-for-human

- [x] Represent historical schemas as reviewable SQL rather than binary DuckDB files.
- [x] Add minimal, populated, and conflicting version-0 fixtures.
- [x] Compare semantic values across renamed and transformed columns.
- [x] Verify legacy archive row counts.
- [x] Run migration through the production database entry point.
- [x] Verify direct idempotence and a close/reopen cycle.
- [x] Prove failed migrations leave no partial schema objects.
- [x] Document how future schema versions extend the harness.

## Comments

Implemented for review on 2026-07-14.

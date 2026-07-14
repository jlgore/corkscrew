# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- `CONTEXT.md` at the repo root.
- `docs/adr/` for decisions that touch the area being changed.

If these files do not exist, proceed silently. Producer skills create them lazily when terms or decisions are resolved.

## File structure

This is a single-context repository:

```text
/
├── CONTEXT.md
├── docs/adr/
└── ...
```

## Use the glossary's vocabulary

When output names a domain concept, use the term defined in `CONTEXT.md`. Do not drift to synonyms that the glossary explicitly avoids.

If a needed concept is absent, reconsider whether it belongs to the project language or note the gap for a domain-documentation session.

## Flag ADR conflicts

If work contradicts an existing ADR, surface the conflict explicitly instead of silently overriding it.

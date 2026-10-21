# Lint guardrails for new code (no big-bang reformat)

Status: ready-for-human
Phase: P5

Baseline today: 111 Go functions over 70 lines (most are generator templates), 1,590 lines over 100
columns, and 6 Clippy warnings.

- golangci-lint, with `new-from-rev: origin/main` so it only checks new and changed code:
  - `funlen` (lines: 70, statements: -1)
  - `lll` (100 columns, tab width 4)
  - `errcheck`, with Close excluded only on error-return paths
  - `contextcheck`
  - `noctx`
  - `bodyclose`
- Exclude the generator template files by path.
- Clippy in CI: `-D warnings -W clippy::undocumented_unsafe_blocks -W clippy::unwrap_used`, with
  `#[allow]` only where a reason is given.
- Fix the 6 existing Clippy warnings: 4 dead fields and 2 `map_or` → `is_none_or`.
- Split the long runtime functions as they get touched, not up front:
  - `findings.Evaluate` (137)
  - `migrateSchemaV1` (128)
  - `scanexec.Execute` (114)
  - `runQuery` (110)
  - `runInit` (120)
  - each provider's `BatchScan` / `StreamScanService`
  - Cloudflare's `describeByType` (163)

Why human: choosing the linters and the CI gate policy is a maintainer call.

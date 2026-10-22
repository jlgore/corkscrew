# Lint guardrails for new code (no big-bang reformat)

Status: complete
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

## Comments

2026-10-21: Maintainer decisions.

- Go linters: the set above (`funlen` 70 lines, `lll` 100 columns, `errcheck`, `contextcheck`,
  `noctx`, `bodyclose`) plus golangci-lint's defaults, with `new-from-rev: origin/main`.
- Gate: lint failures block PRs. Only new and changed lines are checked, so existing debt
  never blocks.
- Workflow: a new `.github/workflows/lint.yml` on every PR: golangci-lint for the root and each
  `plugins/*` module (matrix), and Clippy for the graph extension.
- Clippy: `-D warnings -W clippy::undocumented_unsafe_blocks -W clippy::unwrap_used`, `#[allow]`
  only with a reason. Fix the existing warnings first.
- Sequencing: start after issue 17 merges into `hardening/p5`, since 17 also edits the graph
  extension and its new code must pass these lints.

2026-10-21: Implemented on `hardening/p5`. How to run: `docs/contributing/lint.md`, `make lint`.

- `.golangci.yml` (v2, one root config for every module): `standard` defaults plus `funlen`,
  `lll`, `contextcheck`, `noctx`, `bodyclose`; `new-from-rev: origin/main`;
  `uniq-by-line: false` (otherwise one issue per line hid findings).
- errcheck: "Close only on error-return paths" can't be expressed (errcheck matches by name,
  exclusions see one line). Closest honest rule: `defer x.Close()` is excluded, any other Close
  must be checked, and `_ = x.Close()` drops an error on purpose. `asserttest.Fails*` excluded.
- Excluded paths: `scanner_generator.go`, `cmd/*-generator/`, `cmd/analyze-*/`, `*.pb.go`,
  plus `generated: lax`.
- `.github/workflows/lint.yml`: golangci-lint v2.14.0 via `golangci-lint-action` v9.3.0 (pinned
  by SHA) over root + 6 plugins with `fetch-depth: 0`; Clippy on both graph crates with Rust
  pinned at 1.96.0 so a new stable lint can't break unrelated PRs. Blocking.
- Clippy: the 4 dead fields removed (`ResourceNode.arn`/`tags`, `RelationshipEdge.provider`,
  `LoadedGraph.node_count`/`edge_count`, `PatternGraph.name`/`description`). Only allows:
  `unwrap_used` at crate level in `tests/graph.rs` and the benchmark, with a reason.
- `make lint` now fails without golangci-lint 2.14.0 (was a soft warning); `make check`
  inherits that. `build-and-publish.yml` still runs `make lint || echo …` (never fails).
- Not covered: the gcp `integration` build tag; push-to-main runs compare main with itself.

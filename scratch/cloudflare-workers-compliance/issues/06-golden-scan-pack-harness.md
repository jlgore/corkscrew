# Golden scan and real-SQL pack test harness

Status: complete
Depends on: 05

The provider is its own Go module, so the end-to-end path splits at a golden file:

1. **Provider side:** a test runs every service against the fake API and writes the emitted
   resources (sorted, timestamps normalized) to
   `pkg/query/compliance/testdata/cloudflare/golden-scan.json`. The `-update` flag rewrites it.
   Without the flag, the test diffs the scan against the file, so the golden doubles as the
   provider's snapshot test.
2. **Core side:** `pkg/query/compliance` loads the golden into a temporary DuckDB through the
   same persistence path real scans use (schema lifecycle + graph store), then runs embedded packs
   through the sandboxed engine.

Helper: `runPackAgainstGolden(t, pack, params) map[controlID]map[resourceID]status`, plus an
assertion table per pack: `{control, resource, want}`.

## Acceptance

- One trivial Cloudflare control runs against the golden and passes, proving the plumbing works
  before issues 07 to 09 build on it.
- The test fails if a pack control returns zero rows. A control that evaluates nothing is a
  broken control.

## Comments

2026-10-22: Implemented, with one change from the plan: the core side is `internal/packtest`, not
`pkg/query/compliance`. The harness needs the data session (`internal/data`), and finding
evaluation lives in `internal/app/findings`, so a `pkg/` package would import `internal/` for a
test helper. The golden file is `internal/packtest/testdata/cloudflare/golden-scan.json`.

- Provider: `TestGoldenScan` (`plugins/cloudflare-provider/golden_test.go`) runs a BatchScan of
  accounts, zones, dns, workers, storage, and data with relationships against the fake, then
  writes the result deterministically: sorted, `discovered_at` cleared, whitespace normalized.
  Without `-update` it fails on any difference. Running it three times in a row produced
  identical bytes.
- Core: `packtest.LoadGolden` → `Persist` (`PersistScanOutcome`, completed scan `golden`) →
  `Run` (`compliance.NewExecutor`, the evaluation path) → `Outcomes.Check([]Expect)`. `Run`
  fails a control that errors or returns zero rows. `Check` fails any control without both an
  expected PASS and an expected FAIL. `Results` gives the raw results for tests that expect
  zero rows (the empty-inventory acceptance in 07 to 09). `HOME` points at a temp dir during
  runs, so a pack installed under `~/.corkscrew/query-packs` can't shadow the one under test.
- Tests: `TestCloudflareGoldenPersists` (19 bindings in `cloudflare_resources`, and the `binds`
  edges in `cloud_relationships` with the right target types). `TestCloudflareSmokePack` (a
  test-only pack in `internal/packtest/query-packs/` runs real SQL in the sandboxed engine).
  `TestCloudflareSmokePackOpensFindings` (end to end through `findings.Evaluate`, which opens 2
  findings).

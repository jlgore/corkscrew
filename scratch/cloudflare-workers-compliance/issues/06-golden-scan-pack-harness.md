# Golden scan and real-SQL pack test harness

Status: ready-for-agent
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

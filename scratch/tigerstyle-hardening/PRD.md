# TigerStyle hardening

Status: complete

## Problem

A read-only TigerStyle audit (2026-10-21) of the Go core, CLI/TUI, all six provider plugins, and the
`corkscrew-graph` Rust extension found defects that fall into four groups:

1. **Crashes and hangs.** Some normal code paths panic or spin:
   - Stream scans close the same channel twice in the GCP, Azure and Kubernetes plugins.
   - A Kubernetes receive loop spins after its channel closes.
   - The TUI re-sends every `WindowSizeMsg` to itself, forever.
   - A GitHub semaphore bug lets a goroutine run without a slot.
   - A SQL `NULL` argument to the graph extension can abort DuckDB.
2. **Untrusted input.** Several code paths trust downloaded or local input:
   - Pack tarballs can write outside their destination directory.
   - Downloads and decompression have no size limit.
   - `corkscrew init` installs binaries without verifying them and runs `make` from the working
     directory.
   - Compliance SQL can still reach the filesystem and network.
   - The graph extension is loaded from paths relative to the working directory.
3. **Silent data loss.** Plugins log errors and drop them: failed pages, failed types, failed
   accounts, failed describes, a cached discovery failure, and unpaginated GitHub endpoints. The
   scan still reports `complete`. Findings are only evaluated against completed scans
   (`internal/app/findings/findings.go:71`), so **a dropped error resolves open findings for
   resources that were never actually scanned.** This is the most consequential correctness
   problem in the audit.
4. **No upper bounds.** About 30 pagination loops have no page cap and no check that the page token
   advances. Most network and DB calls have no deadline. Kubernetes watch mode outlives its client.
   Fan-out ignores cancellation. The gRPC message size limit is `MaxInt32`.

The remaining audit items are performance (N+1 queries, one STS call per resource, O(n²)
correlators) and DX: 111 functions over 70 lines, 1,590 lines over 100 columns, effectively zero
assertions, and `unsafe` blocks with no `// SAFETY:` comments.

## Outcome

- Normal operation never panics, spins, or hangs, and every loop, queue, page walk, download and
  call has an explicit limit.
- A scan reports `complete` only when every requested service and scope was fully observed.
  Anything less surfaces as `partial` with per-service errors, so finding resolution only acts on
  data that was really observed.
- Untrusted archives, binaries, packs and extension paths are confined and verified.
- New code meets TigerStyle limits by default, enforced by lint, without a big-bang reformat.

## Non-goals

- Rewriting the code-generator template functions (`plugins/*/scanner_generator.go`,
  `cmd/scanner-generator`, `cmd/analyze-azure-sdk`) to fit in 70 lines. They are data, not control
  flow.
- Reformatting existing lines to 100 columns outside files already being touched.
- Adopting TigerBeetle naming (`snake_case`, units-last renames of existing exported names). The repo
  keeps Go and Rust conventions.

## Phases

Each phase can ship on its own. Within a phase, the issues are independent unless an issue says
otherwise.

| Phase | Theme | Issues |
|---|---|---|
| P0 | Crashes and hangs | 01, 02, 03, 04 |
| P1 | Untrusted input | 05, 06, 07 |
| P2 | Honest scan results | 08 (contract), then 09, 10, 11 |
| P3 | Limits and deadlines | 12 (paging helper), then 13, 14 |
| P4 | Performance | 15, 16 |
| P5 | Guardrails | 17, 18 |

P0 and issue 05 are small, contained fixes that should land first. P2 is the most valuable phase.
Issue 08 fixes the contract that issues 09–11 apply to each provider.

## Decisions (resolved 2026-10-21)

1. **StreamScan / StreamScanService.** No core code path calls them. `scanexec` uses only
   `BatchScan`, and ADR 0014 moved ingestion to unary RPCs. Option (a) is to fix the plugins
   (issue 01). Option (b) is to deprecate the RPCs in protocol v2 and remove the plugin
   implementations. **Decided:** do (a) now, and open a separate deprecation ADR.
2. **Query sandbox scope (issue 07).** `SET enable_external_access=false` could apply only to
   compliance pack sessions, or to ad hoc `corkscrew query` as well. Users may rely on
   `read_csv('local.csv')` in ad hoc queries. **Decided:** compliance and graph only, plus an opt-in `--sandbox` flag for
   `query`.
3. **Binary pinning (issue 06).** Use checksums pinned in source for each (tool, version, platform)
   (**decided**; fail closed for an unpinned tuple), rather than upstream `.sha256` files fetched
   over the same TLS connection.
4. **Partial-scan semantics (issue 08).** Should a per-service error inside one scope mark the whole
   scan `partial`? **Decided:** yes. A `partial` scan is never comparable and never resolves
   findings, which matches ADR 0009's rule for incomplete envelopes.

## Acceptance criteria

- No `close()` of a channel by anyone except its single producer. Every `select`-inside-`for`
  receive loop exits on channel close.
- `go test -race` passes for core and every plugin module. New tests cover each P0 crash.
- Every pagination loop goes through the shared page guard (issue 12), and nothing matches the
  `for {}` + token pattern without it.
- A plugin error injected into any service of any shipped provider produces a `partial` scan, and
  that scan resolves no findings.
- Tar/zip extraction rejects traversal, oversize content, too many entries, and missing checksums,
  and has table tests for each.
- The graph extension returns a SQL error, not an abort, for `NULL` and non-finite arguments.
- CI runs golangci-lint `funlen` (70 lines) and `lll` (100 columns) on new or changed code only,
  and Clippy with `-D warnings` plus `clippy::undocumented_unsafe_blocks`.

## References

- Audit summary: conversation of 2026-10-21. The reviewer findings are copied into the issues below.
- ADR 0009 (incomplete envelopes never resolve findings), ADR 0014 (unary ingestion), ADR 0020
  (bounded cleanup).
- `CONTEXT.md`: *Resource observation*, *Comparable scan*, *Finding occurrence*.

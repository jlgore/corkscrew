# Graph extension: limit work, results and cache

Status: complete
Phase: P4
Depends on: 04

- `match_pattern.rs:132-150`, `patterns/mod.rs:93`: the VF2 search has no step budget, and
  `.take(256)` only caps results. Assert a pattern has at most 8 nodes and at least one
  `type_filter`, count `node_eq` calls against a step budget, and report truncation.
- Correlators with O(n²) pair loops: `correlate_ips.rs:205`, `correlate_dns.rs:203`,
  `correlate_secrets.rs:115`, `correlate_domains.rs:139`, `correlate_load_balancers.rs:186`. Shared
  RFC1918 or Docker-bridge IPs blow up into millions of rows. Skip groups larger than
  `GROUP_SIZE_MAX`, flag them, and cap the total.
- `loader.rs:51-172`: no node or edge cap. `cache.rs:42-52`: `cache_max_entries` takes any u64, and
  each entry is a whole graph. Add `NODE_COUNT_MAX`/`EDGE_COUNT_MAX` and clamp the cache to 1..=16.
- `common.rs:97-150`: traverse has no cap on result rows, and each row clones its full path. Add
  `ROWS_MAX` with a truncation error, and clamp `max_hops`.
- `cache.rs:107`: the fingerprint is taken after the load, so a concurrent rewrite gets cached as
  fresh. Take it before the load and check it again afterwards, using mtime in nanoseconds.
  `cache.rs:94`: one global load lock serializes loads for all databases. Lock per key.
- `common.rs:186-190`: `find_edge` returns any parallel edge, so a weighted path can report the
  wrong `incoming_rel_type`. Pick the minimum-cost edge.
- Hot loop: `common.rs:332-371` allocates a `HashSet` and a `Vec` per visited node, and lowercases
  both strings for every comparison. Use a reusable `FixedBitSet`, lowercase the filter once, and
  compare with `eq_ignore_ascii_case`.

## Comments

2026-10-21: Implemented on `perf/p4`; 23 new tests in `corkscrew-graph-tests` (76 total), written
first and seen failing.

- Limits: `PATTERN_NODES_MAX` 8, `PATTERN_EDGES_MAX` 56, `MATCH_STEPS_MAX` 100M, `MATCHES_MAX`
  256, `GROUP_SIZE_MAX` 1024, `CORRELATION_ROWS_MAX` and `TRAVERSE_ROWS_MAX` 1M (match the core's
  ReadOnly cap), `NODE_COUNT_MAX` 10M, `EDGE_COUNT_MAX` 50M, cache 1..=16, 16 load-lock stripes.
  Compile-time asserts tie them together and keep counts under `u32::MAX`.
- Surfacing: hard caps fail the query with a message naming the limit. The match cap is a new
  last column `was_truncated` on `graph_match_pattern` (the name the stale
  `test/graph_match.test` already expected). Oversize correlation groups produce a
  `correlation_type = 'group_skipped'` row (confidence 0, evidence has kind/value/size) that
  ignores `min_confidence`. `pkg/graphquery` uses `SELECT *`, so both show in CLI output.
- Behaviour change: `cache_max_entries` 0 used to mean the default (4); it now clamps to 1.
- Cache: fingerprint (`mtime_ns`, size) before and after load; a changed file is returned but
  not cached. Striped per-key locks replace the global load lock.
- Traverse: `for_each_neighbor` with a `FixedBitSet` and parent arrays; `cheapest_edge` for
  parallel edges; `eq_ignore_ascii_case` byte-window match, so no lowercasing at all.
- Caveat: the VF2 step budget counts `node_eq` calls only; candidates rejected structurally
  don't count. vf2 1.0 still sees only one of several parallel edges for edge labels.
- Clippy `-D warnings` on the extension still fails only on the 4 `dead_code` items issue 04
  recorded.

2026-10-21 (after review of `main...perf/p4`); 84 tests now:

- Match: budget spent while probing past `MATCHES_MAX` returns the rows with
  `was_truncated = true` instead of failing. `was_truncated` now means "cap reached, may be more".
- Correlators: `GROUP_SIZE_MAX` replaced by `GROUP_PAIRS_MAX = 500_000` cross-provider pairs.
  Groups are split by provider; single-provider groups are never flagged. Fixes large DNS zones
  and multi-account shared IPs losing all matches. Skipped-row evidence now has
  `group_providers`, `group_pairs`, `group_pairs_max`.
- BFS: `FixedBitSet` and graph-sized parent arrays replaced by `HashMap`/`HashSet`, so memory
  grows with visited nodes (a full-graph traversal costs more; small queries on huge graphs no
  longer allocate ~120 MB). `fixedbitset` dependency removed.
- Patterns: a non-empty node `type_filter` OR edge `rel_filter` satisfies validation.
- Cache fingerprint includes `<db>.wal` (absent = `None`).
- All 10 correlators share `CorrelationRow` and helpers from `correlate_common`.

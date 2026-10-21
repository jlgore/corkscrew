# Graph extension: limit work, results and cache

Status: ready-for-agent
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

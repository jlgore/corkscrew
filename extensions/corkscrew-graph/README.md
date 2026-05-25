# corkscrew-graph extension

This directory contains the in-progress DuckDB Rust extension for graph loading
and graph query functions over corkscrew scan databases.

Quick build:

    cd extensions/corkscrew-graph
    make

The packaged DuckDB extension artifact is written to both:

- `target/release/corkscrew_graph.duckdb_extension`
- `build/corkscrew_graph.duckdb_extension`

To test loading from a DuckDB shell (example):

    -- in sqlite3-like shell that can load extensions
    LOAD './extensions/corkscrew-graph/build/corkscrew_graph.duckdb_extension';

Current SQL surface:

- `graph_info(db_path)` returns provider, node, and edge counts for a corkscrew
  DuckDB file.
- `graph_traverse(db_path, source_id, max_hops, direction, filter_type)`
- `graph_shortest_path(db_path, from_id, to_id, weighted)`
- `graph_blast_radius(db_path, source_id, max_hops)`
- `graph_reachable(db_path, source_id, target_type, max_hops)`
- `graph_match_pattern(db_path, pattern_spec)`
- `graph_list_patterns()`
- `graph_cache_invalidate(db_path)`

Pattern library:

- `cross_account_trust`
- `internet_to_database`
- `k8s_privileged_to_cloud`
- `lateral_movement_risk`
- `overprivileged_lambda`
- `public_lb_to_private_db`
- `public_s3_via_instance`
- `unencrypted_data_path`

Macro aliases registered on `LOAD`:

- `cloud_path(db_path, from_id, to_id)`
- `blast_radius(db_path, resource_id, hops := 8)`
- `attack_patterns()`

The graph loading path is implemented through provider detection, graph
hydration into `petgraph::StableGraph`, and a TTL-backed in-memory cache.

Cache TTL can be overridden per session with:

    SET VARIABLE corkscrew_graph_cache_ttl = 0;

`graph_traverse(..., filter_type)` accepts either SQL `NULL` or the literal
string `'NULL'` to disable type filtering.

`graph_match_pattern` caps results at 256 distinct matches. Detect truncation
with `COUNT(DISTINCT match_id) = 256`.

Provider tables are detected by convention. Any pair of tables named
`<prefix>_resources` and `<prefix>_relationships` (excluding the unified
`cloud_*` views) becomes a provider in the loaded graph — no per-provider
code change required.

The graph cache is bounded by both a TTL and an LRU entry cap. Override
either per session:

    SET VARIABLE corkscrew_graph_cache_ttl = 0;          -- always reload
    SET VARIABLE corkscrew_graph_cache_max_entries = 1;  -- single-entry cache

Cache entries auto-invalidate when the backing DuckDB file's mtime or size
changes, so re-running a scan into the same path doesn't need an explicit
`graph_cache_invalidate` call.

**Edge weights**: `graph_shortest_path(..., weighted := true)` reads a positive
integer `weight` field from each edge's JSON `properties` payload. No provider
currently emits weights — until they do, `weighted := true` behaves identically
to `weighted := false`. To wire it up, have the Go-side relationship emitter
add `{"weight": N, ...}` to the properties for any edge class you want to
penalize (cross-region links, untrusted hops, etc.).

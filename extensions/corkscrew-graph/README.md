# corkscrew-graph extension

This directory contains the in-progress DuckDB Rust extension for graph loading
and graph query functions over corkscrew scan databases.

Quick build:

    cd extensions/corkscrew-graph
    make

The packaged DuckDB extension artifact is written to both:

- `target/release/corkscrew_graph.duckdb_extension`
- `build/corkscrew_graph.duckdb_extension`

`corkscrew graph` does not look for the extension in the working directory.
Install it with `make install-graph-extension` from the repository root (it
copies the build to `~/.corkscrew/extensions/`), or pass `--extension <path>`,
or set `CORKSCREW_GRAPH_EXTENSION` to an absolute path.

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
- `graph_correlate_ips(db_path, min_confidence)` finds cross-provider
  resources that share an IP address using `cross_cloud_ip_addresses`.
- `graph_correlate_dns(db_path, include_cname, min_confidence)` finds
  cross-provider DNS records with the same normalized DNS name using
  `cross_cloud_dns_records`; when `include_cname` is true it also matches
  shared CNAME targets from `record_values`.
- `graph_correlate_networks(db_path, min_confidence)` emits conservative
  cross-provider network correlations from explicit rows in
  `cross_cloud_network_topology`.
- `graph_correlate_load_balancers(db_path, min_confidence)` correlates
  cross-provider load balancers sharing explicit backend or DNS target tokens
  from `cross_cloud_loadbalancer_topology` and `cross_cloud_dns_records`.
- `graph_correlate_connectivity(db_path, min_confidence)` emits explicit
  cross-provider VPN, peering, and direct-connect rows from
  `cross_cloud_vpn_connections`, `cross_cloud_network_peering`, and
  `cross_cloud_direct_connections`.
- `graph_correlate_security(db_path, min_confidence)` emits explicit
  cross-provider security rule correlations from
  `cross_cloud_security_correlations`.
- `graph_correlate_domains(db_path, min_confidence)` correlates DNS domain
  ownership from `cross_cloud_dns_records` and explicit certificate/domain
  rows from `certificate_correlations`.
- `graph_correlate_identity(db_path, min_confidence)` emits explicit identity
  federation and role trust rows from `identity_federation_relationships` and
  `security_role_relationships`.
- `graph_correlate_policies(db_path, min_confidence)` emits policy similarity
  rows from `policy_similarity_analysis`.
- `graph_correlate_secrets(db_path, min_confidence)` correlates cross-provider
  secret material sharing the same secret hash in `shared_secrets_correlation`.

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

`graph_match_pattern` caps results at 256 distinct matches. Every row carries a
`was_truncated` BOOLEAN that is true when the cap was reached and more matches
may exist: the search found a 257th match, or ran out of its step budget while
looking for one. Running out of budget before reaching the cap is an error.

Limits. Every query has fixed upper bounds. A limit on work or result size
fails the query with an error that names the limit. A correlator group that is
too large is skipped and reported in its own row:

- Patterns need 1 to 8 nodes, at most 56 edges, and at least one node with a
  non-empty `type_filter` or one edge with a non-empty `rel_filter`. The VF2
  search fails once it has made 100,000,000 node comparisons, which suggests
  adding `type_filter` to more nodes.
- `graph_traverse` fails if it would return more than 1,000,000 rows. Lower
  `max_hops` or set `filter_type`.
- A graph load fails past 10,000,000 nodes or 50,000,000 edges.
- The correlators that pair records sharing a value (`ips`, `dns`, `domains`,
  `load_balancers`, `secrets`) only compare records from different providers.
  They skip any group whose cross-provider pairs would exceed 500,000, such as a
  shared RFC1918 or Docker-bridge address used in several clouds. Same-provider
  records never count against the limit, so a group with a single provider is
  never skipped, and 1,100 AWS records sharing an IP with one Azure record are
  still paired (1,100 pairs). Each skipped group becomes one row with
  `correlation_type = 'group_skipped'`, `source_id` set to the shared value,
  confidence `0.0`, and evidence `{group_kind, group_value, group_size,
  group_providers, group_pairs, group_pairs_max}`. These rows ignore
  `min_confidence` and sort last under `ORDER BY confidence DESC`. A correlator
  fails if it would return more than 1,000,000 rows.

Provider tables are detected by convention. Any pair of tables named
`<prefix>_resources` and `<prefix>_relationships` (excluding the unified
`cloud_*` views) becomes a provider in the loaded graph — no per-provider
code change required.

The graph cache is bounded by both a TTL and an LRU entry cap. Override
either per session:

    SET VARIABLE corkscrew_graph_cache_ttl = 0;          -- always reload
    SET VARIABLE corkscrew_graph_cache_max_entries = 1;  -- single-entry cache

Each entry holds a whole graph, so `corkscrew_graph_cache_max_entries` is
clamped to 1 through 16.

Cache entries auto-invalidate when the backing DuckDB file's mtime (in
nanoseconds) or size changes, so re-running a scan into the same path doesn't
need an explicit `graph_cache_invalidate` call. The fingerprint is taken before
the load starts. A graph whose file changed during its own load goes back to
that caller but is not cached. Loads of different databases run in parallel.

**Edge weights**: `graph_shortest_path(..., weighted := true)` reads a positive
integer `weight` field from each edge's JSON `properties` payload. No provider
currently emits weights — until they do, `weighted := true` behaves identically
to `weighted := false`. To wire it up, have the Go-side relationship emitter
add `{"weight": N, ...}` to the properties for any edge class you want to
penalize (cross-region links, untrusted hops, etc.).

Cross-cloud correlation functions emit `correlation_id`, `correlation_type`,
`source_id`, `target_id`, `source_provider`, `target_provider`, `confidence`,
`evidence` JSON, and `description`.

The initial confidence model mirrors the legacy IP heuristic: shared IP starts
at `0.5`, public IPs add `0.3`, and elastic/reserved/static allocations add
`0.2`, capped at `1.0`.

CLI usage:

    corkscrew graph correlate ip --db ~/.corkscrew/db/corkscrew.duckdb --confidence 0.8
    corkscrew graph correlate dns --include-cname=true --confidence 0.8
    corkscrew graph correlate network --confidence 0.8
    corkscrew graph correlate load-balancer --confidence 0.8
    corkscrew graph correlate connectivity --confidence 0.8
    corkscrew graph correlate security --confidence 0.8
    corkscrew graph correlate domain --confidence 0.8
    corkscrew graph correlate identity --confidence 0.8
    corkscrew graph correlate policy --confidence 0.8
    corkscrew graph correlate secret --confidence 0.8

The initial network slice is intentionally topology-backed: it reports explicit
cross-provider network connections already present in `cross_cloud_network_topology`
and includes source/target CIDR JSON as evidence, but does not infer arbitrary
CIDR overlaps from provider raw JSON.

The connectivity, security, certificate/domain, identity, policy, and secret
slices are table-backed first passes: they consume explicit normalized
correlation/topology rows and do not yet perform broad provider raw JSON
inference.

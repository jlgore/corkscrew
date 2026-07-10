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

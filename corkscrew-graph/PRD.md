# corkscrew-graph: Implementation Plan

A Rust DuckDB extension that adds graph traversal, pathfinding, and attack pattern
matching to corkscrew — without forking DuckDB's parser.

---

## Background & Design Philosophy

DuckPGQ achieves graph querying by patching DuckDB's parser to understand
`GRAPH_TABLE(... MATCH ...)` syntax. This creates a hard fork dependency on a
specific DuckDB version and a single research-pace maintainer. Every DuckDB minor
release breaks it until someone catches up.

The alternative: **everything as table functions**. Onager already proves the
pattern for analytics (PageRank, Louvain, Dijkstra as `SELECT * FROM
onager_ctr_pagerank(...)`). This plan extends that approach to graph *traversal*
and *pattern matching*, the two capabilities DuckPGQ uniquely provides.

The result is a clean community extension that rebuilds against any DuckDB version
with zero parser surgery, written in Rust against petgraph + vf2, exposed entirely
through standard SQL table functions.

---

## Repository Structure

The extension lives in a new top-level directory inside corkscrew:

```
corkscrew/
├── extensions/
│   └── corkscrew-graph/           # New Rust extension
│       ├── Cargo.toml
│       ├── Makefile
│       ├── build.rs
│       ├── extension_config.cmake
│       ├── src/
│       │   ├── lib.rs             # Extension entry point & registration
│       │   ├── graph/
│       │   │   ├── mod.rs
│       │   │   ├── loader.rs      # DuckDB → petgraph hydration
│       │   │   ├── schema.rs      # Multi-provider schema detection
│       │   │   └── cache.rs       # In-memory graph cache (TTL-based)
│       │   ├── functions/
│       │   │   ├── mod.rs
│       │   │   ├── traverse.rs    # graph_traverse() table function
│       │   │   ├── paths.rs       # graph_shortest_path() table function
│       │   │   ├── blast.rs       # graph_blast_radius() table function
│       │   │   ├── reachable.rs   # graph_reachable() table function
│       │   │   └── match_pattern.rs # graph_match_pattern() table function
│       │   └── patterns/
│       │       ├── mod.rs
│       │       ├── builtin.rs     # Built-in security patterns
│       │       └── loader.rs      # Load patterns from JSON/YAML files
│       └── test/
│           ├── graph_traverse.test
│           ├── graph_paths.test
│           └── graph_match.test
├── pkg/
│   └── graphquery/                # Optional Go wrapper for CLI integration
│       ├── client.go
│       └── commands.go
└── docs/
    └── graph-queries.md
```

---

## Cargo Dependencies

```toml
# extensions/corkscrew-graph/Cargo.toml
[package]
name = "corkscrew-graph"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
# DuckDB extension binding
duckdb = { version = "1.15000.0", features = ["vtab", "vtab-arrow", "loadable-extension"] }

# Graph data structures & core algorithms
petgraph = { version = "0.6", features = ["serde-1"] }

# Subgraph isomorphism / pattern matching
vf2 = "1.0"

# Additional algorithm coverage
graphalgs = "0.4"           # Floyd-Warshall, k-shortest, metrics

# Serialization (for pattern definitions)
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"

# Error handling
anyhow = "1.0"
thiserror = "1.0"

# Caching
dashmap = "5.5"             # Concurrent HashMap for graph cache
```

The `duckdb-rs` version encoding tracks the DuckDB version:
`1.MAJOR_MINOR_PATCH.x`, so `1.15000.0` targets DuckDB 1.5.0. Pin this and
update when corkscrew bumps its DuckDB dependency.

---

## Phase 1: Foundation — Graph Loading

### 1.1 Schema Detection (`graph/schema.rs`)

Corkscrew has provider-prefixed tables (`aws_resources`, `azure_resources`,
`kubernetes_resources`) and a shared relationship table per provider
(`aws_relationships`, etc.). The loader needs to detect which providers are
present and handle the union.

```rust
pub struct ProviderTables {
    pub provider: String,        // "aws", "azure", "gcp", "kubernetes"
    pub resources_table: String, // "aws_resources"
    pub relationships_table: String, // "aws_relationships"
}

pub fn detect_providers(conn: &Connection) -> Result<Vec<ProviderTables>> {
    let known = ["aws", "azure", "gcp", "kubernetes"];
    let mut found = vec![];
    for p in known {
        let res_table = format!("{}_resources", p);
        let rel_table = format!("{}_relationships", p);
        // SHOW TABLES and check membership
        if table_exists(conn, &res_table)? && table_exists(conn, &rel_table)? {
            found.push(ProviderTables {
                provider: p.to_string(),
                resources_table: res_table,
                relationships_table: rel_table,
            });
        }
    }
    Ok(found)
}
```

### 1.2 Graph Loader (`graph/loader.rs`)

The core data model maps corkscrew's schema onto petgraph. Node weights carry
the resource metadata needed to make query results useful; edge weights carry
the relationship type.

```rust
use petgraph::stable_graph::StableGraph;
use std::collections::HashMap;

/// Metadata attached to each graph node
#[derive(Debug, Clone)]
pub struct ResourceNode {
    pub id: String,
    pub resource_type: String,
    pub name: String,
    pub region: String,
    pub account_id: String,
    pub provider: String,
    pub arn: Option<String>,
    pub tags: serde_json::Value,
}

/// Metadata attached to each graph edge
#[derive(Debug, Clone)]
pub struct RelationshipEdge {
    pub relationship_type: String,
    pub properties: serde_json::Value,
    pub provider: String,
}

pub type CloudGraph = StableGraph<ResourceNode, RelationshipEdge>;
pub type NodeMap = HashMap<String, petgraph::stable_graph::NodeIndex>;

pub struct LoadedGraph {
    pub graph: CloudGraph,
    pub node_map: NodeMap,           // resource_id → NodeIndex for O(1) lookup
    pub loaded_at: std::time::Instant,
    pub node_count: usize,
    pub edge_count: usize,
}

pub fn load_graph(conn: &Connection, providers: &[ProviderTables]) -> Result<LoadedGraph> {
    let mut graph = CloudGraph::new();
    let mut node_map = NodeMap::new();

    // Pass 1: insert all resource nodes
    for pt in providers {
        let sql = format!(
            "SELECT id, type, name, region, account_id, arn, tags FROM {}",
            pt.resources_table
        );
        // ... iterate rows, build ResourceNode, graph.add_node(), populate node_map
    }

    // Pass 2: insert all relationship edges
    for pt in providers {
        let sql = format!(
            "SELECT from_id, to_id, relationship_type, properties FROM {}",
            pt.relationships_table
        );
        // ... iterate rows, look up NodeIndex from node_map, graph.add_edge()
    }

    let nc = graph.node_count();
    let ec = graph.edge_count();
    Ok(LoadedGraph { graph, node_map, loaded_at: std::time::Instant::now(), node_count: nc, edge_count: ec })
}
```

**Why `StableGraph` over `Graph`?** Node indices remain stable across
potential future incremental updates. The tradeoff is slightly higher memory
usage, acceptable given cloud graphs are typically 10k–500k nodes.

### 1.3 Graph Cache (`graph/cache.rs`)

Loading the full graph on every query would be expensive. A TTL cache keyed on
the DuckDB file path avoids repeated full loads:

```rust
use dashmap::DashMap;
use std::sync::OnceLock;

const CACHE_TTL_SECS: u64 = 300; // 5 minutes; configurable via SET variable

static GRAPH_CACHE: OnceLock<DashMap<String, LoadedGraph>> = OnceLock::new();

fn cache() -> &'static DashMap<String, LoadedGraph> {
    GRAPH_CACHE.get_or_init(DashMap::new)
}

pub fn get_or_load(conn: &Connection, db_path: &str) -> Result<Arc<LoadedGraph>> {
    if let Some(cached) = cache().get(db_path) {
        if cached.loaded_at.elapsed().as_secs() < CACHE_TTL_SECS {
            return Ok(Arc::clone(&*cached));
        }
    }
    let providers = detect_providers(conn)?;
    let graph = load_graph(conn, &providers)?;
    cache().insert(db_path.to_string(), graph);
    Ok(Arc::clone(cache().get(db_path).unwrap()))
}

/// Expose as a SQL function: SELECT graph_cache_invalidate();
pub fn invalidate(db_path: &str) {
    cache().remove(db_path);
}
```

A DuckDB SET variable `corkscrew_graph_cache_ttl` can override the default TTL,
allowing the CLI to set it to 0 for one-shot analysis pipelines.

---

## Phase 2: Table Functions

All functions follow the same pattern: load (or hit cache), run algorithm,
stream results back as DuckDB chunks. The DuckDB Rust extension template uses
`TableFunctionInfo` and a `BindFunction`/`InitFunction`/`ScanFunction` triad.

### 2.1 `graph_traverse(source_id, max_hops, direction, filter_type)`

BFS traversal from a source node. Returns every reachable node within `max_hops`
with its hop count and the path taken.

**SQL signature:**
```sql
graph_traverse(
    source_id  VARCHAR,          -- resource id to start from
    max_hops   INTEGER  DEFAULT 5,
    direction  VARCHAR  DEFAULT 'outbound',  -- 'outbound' | 'inbound' | 'both'
    filter_type VARCHAR DEFAULT NULL         -- optional: filter to resource type
)
→ TABLE (
    node_id          VARCHAR,
    node_type        VARCHAR,
    node_name        VARCHAR,
    region           VARCHAR,
    account_id       VARCHAR,
    provider         VARCHAR,
    hop_count        INTEGER,
    path_ids         VARCHAR[],    -- ordered list of resource IDs traversed
    relationship_types VARCHAR[]   -- edge labels along the path
)
```

**Implementation using petgraph BFS:**
```rust
use petgraph::visit::{Bfs, EdgeRef};

fn traverse(
    graph: &CloudGraph,
    node_map: &NodeMap,
    source_id: &str,
    max_hops: usize,
    direction: Direction,
) -> Vec<TraverseRow> {
    let start = match node_map.get(source_id) {
        Some(idx) => *idx,
        None => return vec![],
    };

    let mut results = vec![];
    let mut queue = VecDeque::from([(start, 0usize, vec![source_id.to_string()], vec![])]);
    let mut visited = HashSet::new();
    visited.insert(start);

    while let Some((node, hops, path, rel_types)) = queue.pop_front() {
        if hops > 0 {
            let weight = &graph[node];
            results.push(TraverseRow {
                node_id: weight.id.clone(),
                node_type: weight.resource_type.clone(),
                // ...
                hop_count: hops as i32,
                path_ids: path.clone(),
                relationship_types: rel_types.clone(),
            });
        }
        if hops >= max_hops { continue; }

        let neighbors: Box<dyn Iterator<Item=_>> = match direction {
            Direction::Outbound => Box::new(graph.edges(node)),
            Direction::Inbound  => Box::new(graph.edges_directed(node, Incoming)),
            Direction::Both     => Box::new(
                graph.edges(node).chain(graph.edges_directed(node, Incoming))
            ),
        };

        for edge in neighbors {
            let neighbor = edge.target();
            if !visited.insert(neighbor) { continue; }
            let mut new_path = path.clone();
            new_path.push(graph[neighbor].id.clone());
            let mut new_rels = rel_types.clone();
            new_rels.push(edge.weight().relationship_type.clone());
            queue.push_back((neighbor, hops + 1, new_path, new_rels));
        }
    }
    results
}
```

**Example queries:**
```sql
-- What does this VPC connect to within 3 hops?
SELECT * FROM graph_traverse('vpc-0abc123', max_hops := 3)
ORDER BY hop_count, node_type;

-- What resources can reach this S3 bucket? (inbound traversal)
SELECT * FROM graph_traverse(
    'arn:aws:s3:::sensitive-data-bucket',
    max_hops  := 5,
    direction := 'inbound'
) WHERE node_type = 'Instance';
```

---

### 2.2 `graph_shortest_path(from_id, to_id, weight_col)`

Dijkstra's shortest path between two specific resources. This is the "can
an attacker get from resource A to resource B, and what's the route?" query.

**SQL signature:**
```sql
graph_shortest_path(
    from_id    VARCHAR,
    to_id      VARCHAR,
    weighted   BOOLEAN DEFAULT false  -- if true, uses relationship property 'weight'
)
→ TABLE (
    hop_number        INTEGER,
    resource_id       VARCHAR,
    resource_type     VARCHAR,
    resource_name     VARCHAR,
    region            VARCHAR,
    incoming_rel_type VARCHAR,   -- relationship that brought us here
    total_hops        INTEGER    -- total path length (same on all rows)
)
```

**Implementation:**
```rust
use petgraph::algo::dijkstra;

fn shortest_path(
    graph: &CloudGraph,
    node_map: &NodeMap,
    from_id: &str,
    to_id: &str,
) -> Option<Vec<PathRow>> {
    let from = *node_map.get(from_id)?;
    let to   = *node_map.get(to_id)?;

    // dijkstra returns distances; reconstruct path via predecessor map
    let (path_nodes, _cost) = petgraph::algo::astar(
        graph,
        from,
        |n| n == to,
        |_| 1,          // unit cost; swap for weighted edges if needed
        |_| 0,          // no heuristic (makes it Dijkstra)
    )?;

    let total = path_nodes.len() - 1;
    Some(path_nodes.iter().enumerate().map(|(i, &node)| {
        PathRow {
            hop_number: i as i32,
            resource_id: graph[node].id.clone(),
            // ...
            total_hops: total as i32,
        }
    }).collect())
}
```

**Example queries:**
```sql
-- Is there any path from an internet-facing load balancer to the RDS instance?
SELECT * FROM graph_shortest_path(
    'arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/public-lb/abc',
    'arn:aws:rds:us-east-1:123:db:prod-postgres'
);

-- Privilege escalation: can this Lambda role reach admin IAM policies?
SELECT * FROM graph_shortest_path(
    'arn:aws:iam::123:role/LambdaExecutionRole',
    'arn:aws:iam::123:policy/AdministratorAccess'
);
```

---

### 2.3 `graph_blast_radius(source_id, max_hops)`

Wraps traversal with aggregation to give a security-useful blast radius score
— how many resources are reachable, broken down by type and risk category.

**SQL signature:**
```sql
graph_blast_radius(
    source_id VARCHAR,
    max_hops  INTEGER DEFAULT 10
)
→ TABLE (
    resource_type     VARCHAR,
    reachable_count   INTEGER,
    max_hop_distance  INTEGER,
    sample_ids        VARCHAR[]   -- up to 5 example resource IDs of this type
)
```

This is implemented as a thin wrapper over `traverse` with aggregation in Rust
before returning results — avoids making the user write the GROUP BY.

**Example queries:**
```sql
-- If this EC2 instance is compromised, what's the blast radius?
SELECT
    resource_type,
    reachable_count,
    max_hop_distance
FROM graph_blast_radius('i-0abc123def456', max_hops := 8)
ORDER BY reachable_count DESC;

-- Score every EC2 instance by blast radius for a risk dashboard
SELECT
    r.id,
    r.name,
    r.region,
    SUM(b.reachable_count) AS total_reachable,
    COUNT(DISTINCT b.resource_type) AS distinct_types
FROM aws_resources r
CROSS JOIN LATERAL graph_blast_radius(r.id, max_hops := 5) b
WHERE r.type = 'Instance'
GROUP BY r.id, r.name, r.region
ORDER BY total_reachable DESC
LIMIT 20;
```

---

### 2.4 `graph_reachable(source_id, target_type, max_hops)`

Simplified reachability check: "can source_id reach any resource of
target_type?" Returns a boolean + count, optimized to stop early on first hit.

**SQL signature:**
```sql
graph_reachable(
    source_id   VARCHAR,
    target_type VARCHAR,
    max_hops    INTEGER DEFAULT 10
)
→ TABLE (
    is_reachable  BOOLEAN,
    match_count   INTEGER,
    closest_hop   INTEGER,
    example_id    VARCHAR
)
```

Useful in WHERE clauses via a scalar macro wrapper:
```sql
-- Find all EC2 instances that can directly reach an S3 bucket
SELECT r.id, r.name, r.region
FROM aws_resources r
WHERE r.type = 'Instance'
  AND (
      SELECT is_reachable
      FROM graph_reachable(r.id, 'Bucket', max_hops := 3)
  ) = true;
```

---

### 2.5 `graph_match_pattern(pattern_name, pattern_json)` — The DuckPGQ Killer

This is the most powerful function: subgraph isomorphism via vf2. You define
an attack pattern as a small graph (nodes with type constraints, edges with
relationship type constraints), and the function finds all matching subgraphs
in your cloud resource graph.

**SQL signature:**
```sql
-- Named built-in pattern
graph_match_pattern(pattern_name VARCHAR)

-- Inline JSON pattern definition
graph_match_pattern(pattern_json VARCHAR)

→ TABLE (
    match_id        INTEGER,     -- which match instance (multiple can exist)
    pattern_node    VARCHAR,     -- node label in the pattern
    resource_id     VARCHAR,     -- matched resource ID
    resource_type   VARCHAR,
    resource_name   VARCHAR,
    region          VARCHAR,
    account_id      VARCHAR
)
```

**Pattern definition format (JSON):**
```json
{
  "name": "public_internet_to_database",
  "description": "Internet gateway with path to a database instance",
  "nodes": [
    { "label": "igw",      "type_filter": "InternetGateway" },
    { "label": "sg",       "type_filter": "SecurityGroup" },
    { "label": "instance", "type_filter": "Instance" },
    { "label": "db",       "type_filter": "DBInstance" }
  ],
  "edges": [
    { "from": "igw",      "to": "sg",       "rel_filter": null },
    { "from": "sg",       "to": "instance", "rel_filter": "member_of" },
    { "from": "instance", "to": "db",       "rel_filter": "connects_to" }
  ]
}
```

**Implementation sketch:**
```rust
use vf2::IsomorphismAlgorithm;

fn match_pattern(
    data_graph: &CloudGraph,
    pattern: &PatternGraph,  // built from the JSON definition
) -> Vec<MatchRow> {
    let mut results = vec![];
    let mut match_id = 0;

    // vf2 iterates all subgraph isomorphisms
    for isomorphism in vf2::subgraph_isomorphisms_iter(
        &pattern.graph,
        data_graph,
        &mut |pn, dn| {
            // Node compatibility: check type_filter
            let pattern_node = &pattern.graph[pn];
            let data_node = &data_graph[dn];
            pattern_node.type_filter.as_ref()
                .map(|f| data_node.resource_type.contains(f.as_str()))
                .unwrap_or(true)
        },
        &mut |pe, de| {
            // Edge compatibility: check rel_filter
            let pattern_edge = &pattern.graph[pe];
            let data_edge = &data_graph[de];
            pattern_edge.rel_filter.as_ref()
                .map(|f| data_edge.relationship_type == *f)
                .unwrap_or(true)
        },
    ) {
        for (pattern_node_idx, data_node_idx) in isomorphism.iter().enumerate() {
            let pn = &pattern.graph[petgraph::graph::NodeIndex::new(pattern_node_idx)];
            let dn = &data_graph[*data_node_idx];
            results.push(MatchRow {
                match_id,
                pattern_node: pn.label.clone(),
                resource_id: dn.id.clone(),
                resource_type: dn.resource_type.clone(),
                // ...
            });
        }
        match_id += 1;
    }
    results
}
```

---

## Phase 3: Built-in Security Patterns

The extension ships a library of named patterns relevant to cloud security posture.
These are registered at extension load time from the `patterns/builtin.rs` module
and callable by name.

| Pattern Name | Description |
|---|---|
| `internet_to_database` | IGW → SG → Instance → RDS/Database path |
| `public_s3_via_instance` | Public EC2 instance with S3 bucket access |
| `cross_account_trust` | IAM role with cross-account assume-role relationship |
| `overprivileged_lambda` | Lambda function with admin-level IAM role |
| `public_lb_to_private_db` | Load balancer reachable to database without WAF |
| `unencrypted_data_path` | Path from internet to unencrypted storage resource |
| `lateral_movement_risk` | Instance with access to multiple security groups |
| `k8s_privileged_to_cloud` | Privileged K8s pod with cloud provider IAM binding |

```sql
-- Scan entire infrastructure for known attack patterns
SELECT pattern_name, match_count FROM (
    SELECT 'internet_to_database'    AS pattern_name, COUNT(DISTINCT match_id) AS match_count FROM graph_match_pattern('internet_to_database')
    UNION ALL
    SELECT 'cross_account_trust',    COUNT(DISTINCT match_id) FROM graph_match_pattern('cross_account_trust')
    UNION ALL
    SELECT 'overprivileged_lambda',  COUNT(DISTINCT match_id) FROM graph_match_pattern('overprivileged_lambda')
) WHERE match_count > 0
ORDER BY match_count DESC;
```

---

## Phase 4: CLI Integration (`pkg/graphquery/`)

The Go CLI gets a new `graph` command group that wraps the SQL table functions
with a human-friendly interface. This layer is purely cosmetic — it generates
and executes the SQL, then formats the output.

```
corkscrew graph traverse <resource-id> [--hops 5] [--direction outbound] [--output table|json|dot]
corkscrew graph path <from-id> <to-id> [--output table|json]
corkscrew graph blast-radius <resource-id> [--hops 8]
corkscrew graph match <pattern-name> [--pattern-file pattern.json] [--output table|json]
corkscrew graph patterns list
corkscrew graph cache invalidate
```

The `--output dot` flag on `traverse` emits Graphviz DOT format for piping
into visualization tools:
```
corkscrew graph traverse vpc-0abc123 --hops 4 --output dot | dot -Tsvg > blast.svg
```

---

## Phase 5: DuckDB Macro Layer

Register convenience SQL macros at extension load time so users who prefer raw
SQL get nicer names without knowing the table function signatures:

```sql
-- Registered by the extension on LOAD
CREATE OR REPLACE MACRO cloud_path(from_id, to_id) AS TABLE
    SELECT * FROM graph_shortest_path(from_id::VARCHAR, to_id::VARCHAR);

CREATE OR REPLACE MACRO blast_radius(resource_id, hops := 8) AS TABLE
    SELECT * FROM graph_blast_radius(resource_id::VARCHAR, hops::INTEGER);

CREATE OR REPLACE MACRO attack_patterns() AS TABLE
    SELECT DISTINCT pattern_name
    FROM graph_list_patterns();
```

---

## Implementation Phases & Milestones

### Milestone 0 — Scaffold (1–2 days)
- [ ] Create `extensions/corkscrew-graph/` using `duckdb/extension-template-rs`
- [ ] Wire `Cargo.toml` with petgraph, vf2, graphalgs, duckdb
- [ ] Stub `lib.rs` with extension registration and a no-op `graph_info()` function
- [ ] Confirm it builds and loads: `INSTALL './build/corkscrew_graph.duckdb_extension'; LOAD 'corkscrew_graph'; SELECT * FROM graph_info();`
- [ ] Add to corkscrew's top-level `Makefile` as `make build-graph-extension`

### Milestone 1 — Graph Loading (3–5 days)
- [ ] Implement `schema.rs`: provider detection, table existence check
- [ ] Implement `loader.rs`: two-pass load (nodes then edges) into `StableGraph`
- [ ] Implement `cache.rs`: TTL-based `DashMap` cache keyed on db path
- [ ] Write `graph_info()` function that returns node/edge counts per provider
- [ ] Test: load a real corkscrew scan DB, verify counts match `SELECT COUNT(*) FROM aws_resources`

### Milestone 2 — Traversal & Paths (5–7 days)
- [ ] Implement `traverse.rs` with BFS, direction support, hop count, path tracking
- [ ] Implement `paths.rs` with A*/Dijkstra, path reconstruction
- [ ] Implement `reachable.rs` with early-exit BFS
- [ ] Write SQLLogicTest tests for each function
- [ ] Performance test: 50k node graph, traversal to depth 5 should complete <500ms

### Milestone 3 — Blast Radius (2–3 days)
- [ ] Implement `blast.rs` as aggregated traversal
- [ ] Test `CROSS JOIN LATERAL` pattern works correctly in DuckDB
- [ ] Benchmark: scanning all EC2 instances for blast radius should complete <30s on 10k node graph

### Milestone 4 — Pattern Matching (7–10 days)
- [ ] Implement `patterns/` module: `PatternGraph` type, JSON deserializer
- [ ] Implement `match_pattern.rs` using vf2 isomorphism iterator
- [ ] Implement node/edge compatibility predicates (type_filter, rel_filter)
- [ ] Register built-in patterns from `patterns/builtin.rs`
- [ ] Test each built-in pattern against synthetic test data
- [ ] Performance test: vf2 on large graphs can be expensive; add pattern size warnings and a `max_matches` safety limit

### Milestone 5 — CLI & Polish (3–5 days)
- [ ] Add `pkg/graphquery/` Go package with command implementations
- [ ] Add `corkscrew graph` subcommand group to CLI
- [ ] Register SQL macros in extension load
- [ ] Add DOT output format for traverse
- [ ] Write end-to-end test: scan → graph traverse → verify output
- [ ] Add `graph_cache_invalidate()` function
- [ ] Community extension submission prep (extension descriptor YAML, CI workflow)

---

## Key Technical Decisions & Tradeoffs

**`StableGraph` vs `Graph`**
`StableGraph` is chosen because indices remain stable if we ever add incremental
graph updates. Memory overhead is ~2x vs `Graph` but irrelevant at cloud graph
scale (hundreds of thousands of nodes, not billions).

**In-memory vs on-disk graph**
The full graph is loaded into memory on first query, cached for TTL, then
dropped. For a 500k node / 2M edge graph this is roughly 500MB–1GB RAM.
This is acceptable for a CLI tool but would need revisiting for a long-running
server context. A future `corkscrew-graph-server` mode could keep the graph
warm with incremental updates.

**vf2 pattern matching performance**
Subgraph isomorphism is NP-complete in general. In practice, well-constrained
patterns (specific node type filters, directed edges) prune the search space
dramatically and complete in milliseconds on cloud graphs. The built-in patterns
are all designed with this in mind. User-defined patterns that are too generic
(e.g., a 2-node pattern with no type constraints) will be warned about and
optionally limited by `max_matches`.

**Cross-provider graph**
The loader builds a single unified graph from all provider tables. Relationships
in `cross_cloud_correlations` are also loaded as edges, enabling cross-cloud
traversal. A `provider` field on each node allows filtering by provider in
traversal queries.

**DuckDB version compatibility**
Because this extension uses only table functions (no parser modification), it
targets DuckDB's stable C API. The Rust template is explicitly designed to be
more portable across DuckDB versions than C++ extensions. Pin `duckdb-rs` to the
version corkscrew uses and bump together.

---

## Example Full Analysis Workflow

```sql
-- 1. Load extension
LOAD 'corkscrew_graph';

-- 2. Verify graph loaded correctly
SELECT * FROM graph_info();
-- provider | nodes  | edges  | loaded_at
-- aws      | 48,293 | 92,441 | 2026-05-07 14:23:01

-- 3. Find all resources an internet gateway can reach within 4 hops
SELECT node_type, COUNT(*) as count
FROM graph_traverse('igw-0abc123', max_hops := 4)
GROUP BY node_type ORDER BY count DESC;

-- 4. Check if specific path exists (privilege escalation)
SELECT * FROM graph_shortest_path(
    'arn:aws:iam::123456789:role/dev-lambda-role',
    'arn:aws:iam::123456789:policy/AdministratorAccess'
);

-- 5. Scan for all known attack patterns
SELECT pattern_name, COUNT(DISTINCT match_id) AS instances
FROM (
    SELECT 'internet_to_database'   AS pattern_name, match_id FROM graph_match_pattern('internet_to_database')
    UNION ALL
    SELECT 'overprivileged_lambda',  match_id FROM graph_match_pattern('overprivileged_lambda')
    UNION ALL
    SELECT 'cross_account_trust',    match_id FROM graph_match_pattern('cross_account_trust')
)
GROUP BY pattern_name
ORDER BY instances DESC;

-- 6. Top 10 highest blast radius EC2 instances
SELECT
    r.id,
    r.name,
    r.region,
    SUM(b.reachable_count) AS blast_radius_score
FROM aws_resources r
CROSS JOIN LATERAL graph_blast_radius(r.id) b
WHERE r.type = 'Instance'
GROUP BY r.id, r.name, r.region
ORDER BY blast_radius_score DESC
LIMIT 10;
```

---

## Files to Create (Immediate Next Steps)

1. `extensions/corkscrew-graph/Cargo.toml` — dependency manifest above
2. `extensions/corkscrew-graph/src/lib.rs` — extension entry, register functions
3. `extensions/corkscrew-graph/src/graph/mod.rs` — re-exports
4. `extensions/corkscrew-graph/src/graph/schema.rs` — provider detection
5. `extensions/corkscrew-graph/src/graph/loader.rs` — petgraph hydration
6. `extensions/corkscrew-graph/src/graph/cache.rs` — TTL cache
7. `extensions/corkscrew-graph/src/functions/mod.rs` — function registry
8. `extensions/corkscrew-graph/src/functions/traverse.rs` — BFS traversal
9. `extensions/corkscrew-graph/Makefile` — build targets
10. `extensions/corkscrew-graph/test/graph_traverse.test` — SQLLogicTest

Start with Milestone 0 to confirm the build pipeline works before touching
any graph logic. The extension template scaffold gives you a working
`rusty_quack()` function; rename it to `graph_info()` and verify it loads
in DuckDB before writing a single line of petgraph code.

// Graph loader implementation (two-pass): resources then relationships.
use anyhow::Result;
use chrono::{DateTime, Utc};
use duckdb::Connection;
use petgraph::stable_graph::{NodeIndex, StableGraph};
use serde_json::Value as JsonValue;
use std::collections::{BTreeMap, HashMap};
use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::time::Instant;

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
    pub tags: JsonValue,
}

/// Metadata attached to each graph edge
#[derive(Debug, Clone)]
pub struct RelationshipEdge {
    pub relationship_type: String,
    pub properties: JsonValue,
    pub provider: String,
}

/// Ten times a very large multi-account estate (~1M resources); at roughly 400 bytes of
/// metadata per node this bounds node storage near 4 GB.
pub const NODE_COUNT_MAX: usize = 10_000_000;
/// Five relationships per resource on average at `NODE_COUNT_MAX`; real scans emit 1-3.
pub const EDGE_COUNT_MAX: usize = 50_000_000;
// `StableGraph`'s default index type is `u32`, and `u32::MAX` is its reserved end marker.
const _: () = assert!(NODE_COUNT_MAX < u32::MAX as usize);
const _: () = assert!(EDGE_COUNT_MAX < u32::MAX as usize);

pub type CloudGraph = StableGraph<ResourceNode, RelationshipEdge>;
pub type NodeMap = HashMap<String, NodeIndex>;

pub struct LoadedGraph {
    pub graph: CloudGraph,
    pub node_map: NodeMap,
    pub loaded_at: DateTime<Utc>,
    pub loaded_at_instant: Instant,
    pub node_count: usize,
    pub edge_count: usize,
    /// Per-provider (nodes, edges) counts populated during load so that
    /// `graph_info` doesn't have to re-walk the whole graph per call.
    pub provider_counts: BTreeMap<String, (i64, i64)>,
    /// Fingerprint of the backing file taken *before* the load started. Used
    /// to auto-evict the cache entry when the DuckDB file is rewritten under
    /// the same path. `None` if the file could not be stat'd (in-memory DB or
    /// transient I/O error).
    pub file_fingerprint: Option<FileFingerprint>,
}

/// Identity of a DuckDB database's contents as cheaply observable from `stat`:
/// the main file plus its write-ahead log. Committed writes land in `<db>.wal`
/// and reach the main file only at a checkpoint, so a rescan can change the WAL
/// alone; `wal` is `None` while no WAL exists, which differs from every WAL.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct FileFingerprint {
    pub database: StatFingerprint,
    pub wal: Option<StatFingerprint>,
}

/// One file's `stat` identity. The mtime keeps full nanosecond precision: two
/// rewrites within one second that keep the same size must still compare different.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct StatFingerprint {
    pub mtime_ns: u128,
    pub size_bytes: u64,
}

/// Upper bounds for one graph load. Production code uses `GraphLimits::DEFAULT`;
/// tests pass smaller limits to exercise the failure path cheaply.
#[derive(Clone, Copy, Debug)]
pub struct GraphLimits {
    pub node_count_max: usize,
    pub edge_count_max: usize,
}

impl GraphLimits {
    pub const DEFAULT: GraphLimits = GraphLimits {
        node_count_max: NODE_COUNT_MAX,
        edge_count_max: EDGE_COUNT_MAX,
    };
}

pub fn load_graph(
    conn: &Connection,
    providers: &[super::schema::ProviderTables],
) -> Result<LoadedGraph> {
    load_graph_with_limits(conn, providers, GraphLimits::DEFAULT)
}

/// Two-pass load: every provider's resources first, then every provider's
/// relationships, so edges can reference nodes from any provider. Fails as soon
/// as either count would pass its limit, before the graph grows further.
pub fn load_graph_with_limits(
    conn: &Connection,
    providers: &[super::schema::ProviderTables],
    limits: GraphLimits,
) -> Result<LoadedGraph> {
    assert!(limits.node_count_max <= NODE_COUNT_MAX);
    assert!(limits.edge_count_max <= EDGE_COUNT_MAX);

    let mut graph: CloudGraph = StableGraph::new();
    let mut node_map: NodeMap = HashMap::new();
    let mut provider_counts: BTreeMap<String, (i64, i64)> = BTreeMap::new();

    // A provider gets a count entry only once it contributes a node or edge.
    for provider in providers {
        let added = load_nodes(conn, provider, limits, &mut graph, &mut node_map)?;
        if added > 0 {
            provider_counts
                .entry(provider.provider.clone())
                .or_insert((0, 0))
                .0 += added;
        }
    }
    for provider in providers {
        let added = load_edges(conn, provider, limits, &mut graph, &node_map)?;
        if added > 0 {
            provider_counts
                .entry(provider.provider.clone())
                .or_insert((0, 0))
                .1 += added;
        }
    }

    let node_count = graph.node_count();
    let edge_count = graph.edge_count();
    assert!(node_count <= limits.node_count_max);
    assert!(edge_count <= limits.edge_count_max);
    Ok(LoadedGraph {
        graph,
        node_map,
        loaded_at: Utc::now(),
        loaded_at_instant: Instant::now(),
        node_count,
        edge_count,
        provider_counts,
        file_fingerprint: None,
    })
}

/// Inserts one provider's resources and returns how many were added. Rows
/// without an id are skipped.
fn load_nodes(
    conn: &Connection,
    provider: &super::schema::ProviderTables,
    limits: GraphLimits,
    graph: &mut CloudGraph,
    node_map: &mut NodeMap,
) -> Result<i64> {
    let sql = format!(
        "SELECT id, type, name, region, account_id, arn, tags FROM {}",
        provider.resources_table
    );
    let mut stmt = conn.prepare(&sql)?;
    let mut rows = stmt.query([])?;
    let mut added: i64 = 0;
    while let Some(row) = rows.next()? {
        let Some(id) = row.get::<_, Option<String>>(0)? else {
            continue;
        };
        if graph.node_count() >= limits.node_count_max {
            anyhow::bail!(
                "graph has more than {} nodes; query a smaller scan database",
                limits.node_count_max
            );
        }
        let tags = match row.get::<_, Option<String>>(6)?.as_deref() {
            Some(text) => serde_json::from_str(text).unwrap_or(JsonValue::Null),
            None => JsonValue::Null,
        };
        let node = ResourceNode {
            id: id.clone(),
            resource_type: row.get::<_, Option<String>>(1)?.unwrap_or_default(),
            name: row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            region: row.get::<_, Option<String>>(3)?.unwrap_or_default(),
            account_id: row.get::<_, Option<String>>(4)?.unwrap_or_default(),
            provider: provider.provider.clone(),
            arn: row.get(5)?,
            tags,
        };
        let index = graph.add_node(node);
        node_map.insert(id, index);
        added += 1;
    }
    Ok(added)
}

/// Inserts one provider's relationships and returns how many were added. Rows
/// with a missing endpoint id, or an id with no loaded node, are skipped.
fn load_edges(
    conn: &Connection,
    provider: &super::schema::ProviderTables,
    limits: GraphLimits,
    graph: &mut CloudGraph,
    node_map: &NodeMap,
) -> Result<i64> {
    let sql = format!(
        "SELECT from_id, to_id, relationship_type, properties FROM {}",
        provider.relationships_table
    );
    let mut stmt = conn.prepare(&sql)?;
    let mut rows = stmt.query([])?;
    let mut added: i64 = 0;
    while let Some(row) = rows.next()? {
        let from_id: Option<String> = row.get(0)?;
        let to_id: Option<String> = row.get(1)?;
        let Some(from_index) = from_id.and_then(|id| node_map.get(&id).copied()) else {
            continue;
        };
        let Some(to_index) = to_id.and_then(|id| node_map.get(&id).copied()) else {
            continue;
        };
        if graph.edge_count() >= limits.edge_count_max {
            anyhow::bail!(
                "graph has more than {} edges; query a smaller scan database",
                limits.edge_count_max
            );
        }
        let properties = match row.get::<_, Option<String>>(3)?.as_deref() {
            Some(text) => serde_json::from_str(text).unwrap_or(JsonValue::Null),
            None => JsonValue::Null,
        };
        let edge = RelationshipEdge {
            relationship_type: row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            properties,
            provider: provider.provider.clone(),
        };
        graph.add_edge(from_index, to_index, edge);
        added += 1;
    }
    Ok(added)
}

/// Stats the database at `path` and its write-ahead log `<path>.wal`, or returns
/// `None` if the database cannot be stat'd (for example `:memory:` or a missing
/// file). A missing WAL is recorded as `None`, a state distinct from any WAL.
pub fn fingerprint_path(path: &str) -> Option<FileFingerprint> {
    let database = stat_fingerprint(Path::new(path))?;
    let wal = stat_fingerprint(&wal_path(path));
    Some(FileFingerprint { database, wal })
}

/// DuckDB's write-ahead log for the database at `path`: the same name plus `.wal`.
fn wal_path(path: &str) -> PathBuf {
    let mut wal = OsString::from(path);
    wal.push(".wal");
    let wal = PathBuf::from(wal);
    assert!(wal.as_os_str().len() == path.len() + ".wal".len());
    wal
}

fn stat_fingerprint(path: &Path) -> Option<StatFingerprint> {
    let meta = std::fs::metadata(path).ok()?;
    let mtime = meta.modified().ok()?;
    let mtime_ns = mtime.duration_since(std::time::UNIX_EPOCH).ok()?.as_nanos();
    Some(StatFingerprint {
        mtime_ns,
        size_bytes: meta.len(),
    })
}

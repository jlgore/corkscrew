// Graph loader implementation (two-pass): resources then relationships.
use anyhow::Result;
use chrono::{DateTime, Utc};
use duckdb::Connection;
use petgraph::stable_graph::{NodeIndex, StableGraph};
use serde_json::Value as JsonValue;
use std::collections::{BTreeMap, HashMap};
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
    /// File fingerprint captured at load: (mtime as std::time::SystemTime epoch
    /// secs, file size in bytes). Used to auto-evict the cache entry when the
    /// underlying DuckDB file is rewritten under the same path. `None` if the
    /// file could not be stat'd (in-memory DB or transient I/O error).
    pub file_fingerprint: Option<(u64, u64)>,
}

pub fn load_graph(
    conn: &Connection,
    providers: &[super::schema::ProviderTables],
) -> Result<LoadedGraph> {
    let mut graph: CloudGraph = StableGraph::new();
    let mut node_map: NodeMap = HashMap::new();
    let mut provider_counts: BTreeMap<String, (i64, i64)> = BTreeMap::new();

    // Pass 1: insert all resource nodes
    for pt in providers {
        let sql = format!(
            "SELECT id, type, name, region, account_id, arn, tags FROM {}",
            pt.resources_table
        );
        let mut stmt = conn.prepare(&sql)?;
        let mut rows = stmt.query([])?;
        while let Some(row) = rows.next()? {
            // Use Option<String> to allow NULLs
            let id: Option<String> = row.get(0)?;
            let rtype: Option<String> = row.get(1)?;
            let name: Option<String> = row.get(2)?;
            let region: Option<String> = row.get(3)?;
            let account_id: Option<String> = row.get(4)?;
            let arn: Option<String> = row.get(5)?;
            let tags_str: Option<String> = row.get(6)?;

            let id = match id {
                Some(v) => v,
                None => continue, // skip rows without id
            };
            let resource_type = rtype.unwrap_or_default();
            let name = name.unwrap_or_default();
            let region = region.unwrap_or_default();
            let account_id = account_id.unwrap_or_default();
            let tags = match tags_str.as_deref() {
                Some(s) => serde_json::from_str(s).unwrap_or(JsonValue::Null),
                None => JsonValue::Null,
            };

            let node = ResourceNode {
                id: id.clone(),
                resource_type,
                name,
                region,
                account_id,
                provider: pt.provider.clone(),
                arn,
                tags,
            };
            let idx = graph.add_node(node);
            node_map.insert(id, idx);
            provider_counts
                .entry(pt.provider.clone())
                .or_insert((0, 0))
                .0 += 1;
        }
    }

    // Pass 2: insert all relationship edges
    for pt in providers {
        let sql = format!(
            "SELECT from_id, to_id, relationship_type, properties FROM {}",
            pt.relationships_table
        );
        let mut stmt = conn.prepare(&sql)?;
        let mut rows = stmt.query([])?;
        while let Some(row) = rows.next()? {
            let from_id: Option<String> = row.get(0)?;
            let to_id: Option<String> = row.get(1)?;
            let rel_type: Option<String> = row.get(2)?;
            let props_str: Option<String> = row.get(3)?;

            let from_id = match from_id {
                Some(v) => v,
                None => continue,
            };
            let to_id = match to_id {
                Some(v) => v,
                None => continue,
            };

            let from_idx = match node_map.get(&from_id) {
                Some(i) => *i,
                None => continue, // missing node
            };
            let to_idx = match node_map.get(&to_id) {
                Some(i) => *i,
                None => continue,
            };

            let relationship_type = rel_type.unwrap_or_default();
            let properties = match props_str.as_deref() {
                Some(s) => serde_json::from_str(s).unwrap_or(JsonValue::Null),
                None => JsonValue::Null,
            };

            let edge = RelationshipEdge {
                relationship_type,
                properties,
                provider: pt.provider.clone(),
            };
            graph.add_edge(from_idx, to_idx, edge);
            provider_counts
                .entry(pt.provider.clone())
                .or_insert((0, 0))
                .1 += 1;
        }
    }

    let nc = graph.node_count();
    let ec = graph.edge_count();
    Ok(LoadedGraph {
        graph,
        node_map,
        loaded_at: Utc::now(),
        loaded_at_instant: Instant::now(),
        node_count: nc,
        edge_count: ec,
        provider_counts,
        file_fingerprint: None,
    })
}

/// Stat the given path and return (mtime_secs, size_bytes), or None if the
/// path is not a regular file (e.g. `:memory:` or a missing file).
pub fn fingerprint_path(path: &str) -> Option<(u64, u64)> {
    let meta = std::fs::metadata(path).ok()?;
    let mtime = meta.modified().ok()?;
    let mtime_secs = mtime.duration_since(std::time::UNIX_EPOCH).ok()?.as_secs();
    Some((mtime_secs, meta.len()))
}

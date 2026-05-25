use duckdb::Connection;
use duckdb::core::{FlatVector, Inserter, ListVector};
use petgraph::Direction::{Incoming, Outgoing};
use petgraph::algo::astar;
use petgraph::stable_graph::NodeIndex;
use petgraph::visit::EdgeRef;
use std::collections::{BTreeMap, HashMap, HashSet, VecDeque};
use std::sync::atomic::{AtomicUsize, Ordering};

/// Returns the half-open [start, end) row range for the next chunk to emit, or
/// `None` when the cursor has consumed all rows. `capacity` should come from
/// `chunk_capacity()`.
pub fn next_chunk(cursor: &AtomicUsize, total: usize, capacity: usize) -> Option<(usize, usize)> {
    if capacity == 0 {
        return None;
    }
    let start = cursor.fetch_add(capacity, Ordering::Relaxed);
    if start >= total {
        return None;
    }
    Some((start, (start + capacity).min(total)))
}

/// DuckDB's per-call vector capacity (typically 2048). Read at runtime so it
/// stays correct if DuckDB is built with a non-default vector size.
pub fn chunk_capacity() -> usize {
    unsafe { duckdb::ffi::duckdb_vector_size() as usize }
}

use crate::graph::loader::{CloudGraph, LoadedGraph, RelationshipEdge, ResourceNode};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TraversalDirection {
    Outbound,
    Inbound,
    Both,
}

impl TraversalDirection {
    pub fn parse(value: &str) -> Result<Self, Box<dyn std::error::Error>> {
        match value.to_ascii_lowercase().as_str() {
            "outbound" => Ok(Self::Outbound),
            "inbound" => Ok(Self::Inbound),
            "both" => Ok(Self::Both),
            _ => Err(format!("invalid direction '{value}', expected outbound|inbound|both").into()),
        }
    }
}

#[derive(Clone, Debug)]
pub struct TraverseRow {
    pub node_id: String,
    pub node_type: String,
    pub node_name: String,
    pub region: String,
    pub account_id: String,
    pub provider: String,
    pub hop_count: i32,
    pub path_ids: Vec<String>,
    pub relationship_types: Vec<String>,
}

#[derive(Clone, Debug)]
pub struct PathRow {
    pub hop_number: i32,
    pub resource_id: String,
    pub resource_type: String,
    pub resource_name: String,
    pub region: String,
    pub incoming_rel_type: Option<String>,
    pub total_hops: i32,
}

#[derive(Clone, Debug)]
pub struct ReachableRow {
    pub is_reachable: bool,
    pub match_count: i64,
    pub closest_hop: Option<i32>,
    pub example_id: Option<String>,
}

#[derive(Clone, Debug)]
pub struct BlastRow {
    pub resource_type: String,
    pub reachable_count: i32,
    pub max_hop_distance: i32,
    pub sample_ids: Vec<String>,
}

pub fn load_graph_for_path(db_path: &str) -> Result<std::sync::Arc<LoadedGraph>, Box<dyn std::error::Error>> {
    let conn = Connection::open(db_path)?;
    Ok(crate::graph::cache::get_or_load(&conn, db_path)?)
}

pub fn collect_traverse_rows(
    loaded: &LoadedGraph,
    source_id: &str,
    max_hops: usize,
    direction: TraversalDirection,
    filter_type: Option<&str>,
) -> Vec<TraverseRow> {
    let start = match loaded.node_map.get(source_id) {
        Some(idx) => *idx,
        None => return Vec::new(),
    };

    let filter_type = filter_type.filter(|value| !value.is_empty());
    let mut rows = Vec::new();
    let mut parents = HashMap::new();
    let mut visited = HashSet::new();
    let mut queue = VecDeque::from([(start, 0usize)]);
    visited.insert(start);

    while let Some((node, hops)) = queue.pop_front() {
        if hops > 0 {
            let resource = &loaded.graph[node];
            if type_matches(&resource.resource_type, filter_type) {
                let (path_ids, rel_types) = reconstruct_path(loaded, &parents, start, node);
                rows.push(TraverseRow {
                    node_id: resource.id.clone(),
                    node_type: resource.resource_type.clone(),
                    node_name: resource.name.clone(),
                    region: resource.region.clone(),
                    account_id: resource.account_id.clone(),
                    provider: resource.provider.clone(),
                    hop_count: hops as i32,
                    path_ids,
                    relationship_types: rel_types,
                });
            }
        }

        if hops >= max_hops {
            continue;
        }

        for (neighbor, rel_type) in neighbors(&loaded.graph, node, direction) {
            if !visited.insert(neighbor) {
                continue;
            }

            parents.insert(neighbor, (node, rel_type));
            queue.push_back((neighbor, hops + 1));
        }
    }

    rows
}

pub fn collect_shortest_path_rows(
    loaded: &LoadedGraph,
    from_id: &str,
    to_id: &str,
    weighted: bool,
) -> Vec<PathRow> {
    let from = match loaded.node_map.get(from_id) {
        Some(idx) => *idx,
        None => return Vec::new(),
    };
    let to = match loaded.node_map.get(to_id) {
        Some(idx) => *idx,
        None => return Vec::new(),
    };

    let Some((_, path_nodes)) = astar(
        &loaded.graph,
        from,
        |node| node == to,
        |edge| edge_cost(edge.weight(), weighted),
        |_| 0u64,
    ) else {
        return Vec::new();
    };

    let total_hops = path_nodes.len().saturating_sub(1) as i32;
    path_nodes
        .iter()
        .enumerate()
        .map(|(index, node)| {
            let resource = &loaded.graph[*node];
            let incoming_rel_type = if index == 0 {
                None
            } else {
                let previous = path_nodes[index - 1];
                loaded
                    .graph
                    .find_edge(previous, *node)
                    .map(|edge_idx| loaded.graph[edge_idx].relationship_type.clone())
            };

            PathRow {
                hop_number: index as i32,
                resource_id: resource.id.clone(),
                resource_type: resource.resource_type.clone(),
                resource_name: resource.name.clone(),
                region: resource.region.clone(),
                incoming_rel_type,
                total_hops,
            }
        })
        .collect()
}

pub fn collect_reachable_row(
    loaded: &LoadedGraph,
    source_id: &str,
    target_type: &str,
    max_hops: usize,
) -> ReachableRow {
    let Some(start) = loaded.node_map.get(source_id).copied() else {
        return ReachableRow { is_reachable: false, match_count: 0, closest_hop: None, example_id: None };
    };
    let filter = (!target_type.is_empty()).then_some(target_type);

    // Focused BFS — no parent tracking, no path reconstruction. BFS visits in
    // non-decreasing hop order so the first match is the closest; later matches
    // only increment match_count.
    let mut match_count: i64 = 0;
    let mut closest_hop: Option<i32> = None;
    let mut example_id: Option<String> = None;
    let mut visited = HashSet::new();
    let mut queue = VecDeque::from([(start, 0usize)]);
    visited.insert(start);

    while let Some((node, hops)) = queue.pop_front() {
        if hops > 0 {
            let resource = &loaded.graph[node];
            if type_matches(&resource.resource_type, filter) {
                match_count += 1;
                if closest_hop.is_none() {
                    closest_hop = Some(hops as i32);
                    example_id = Some(resource.id.clone());
                }
            }
        }

        if hops >= max_hops {
            continue;
        }

        for (neighbor, _) in neighbors(&loaded.graph, node, TraversalDirection::Outbound) {
            if visited.insert(neighbor) {
                queue.push_back((neighbor, hops + 1));
            }
        }
    }

    ReachableRow {
        is_reachable: match_count > 0,
        match_count,
        closest_hop,
        example_id,
    }
}

pub fn collect_blast_rows(loaded: &LoadedGraph, source_id: &str, max_hops: usize) -> Vec<BlastRow> {
    let Some(start) = loaded.node_map.get(source_id).copied() else {
        return Vec::new();
    };

    // Focused BFS — no parent tracking, no path reconstruction. Group inline
    // by resource_type.
    let mut grouped: BTreeMap<String, BlastRow> = BTreeMap::new();
    let mut visited = HashSet::new();
    let mut queue = VecDeque::from([(start, 0usize)]);
    visited.insert(start);

    while let Some((node, hops)) = queue.pop_front() {
        if hops > 0 {
            let resource = &loaded.graph[node];
            let entry = grouped.entry(resource.resource_type.clone()).or_insert_with(|| BlastRow {
                resource_type: resource.resource_type.clone(),
                reachable_count: 0,
                max_hop_distance: 0,
                sample_ids: Vec::new(),
            });
            entry.reachable_count += 1;
            entry.max_hop_distance = entry.max_hop_distance.max(hops as i32);
            if entry.sample_ids.len() < 5 {
                entry.sample_ids.push(resource.id.clone());
            }
        }

        if hops >= max_hops {
            continue;
        }

        for (neighbor, _) in neighbors(&loaded.graph, node, TraversalDirection::Outbound) {
            if visited.insert(neighbor) {
                queue.push_back((neighbor, hops + 1));
            }
        }
    }

    grouped.into_values().collect()
}

pub fn write_varchar_list_column(vector: &mut ListVector<'_>, values: &[Vec<String>]) -> Result<(), Box<dyn std::error::Error>> {
    let total_values = values.iter().map(Vec::len).sum();
    let child = vector.child(total_values);
    let mut offset = 0usize;
    for (row_idx, row_values) in values.iter().enumerate() {
        vector.set_entry(row_idx, offset, row_values.len());
        for (value_idx, value) in row_values.iter().enumerate() {
            child.insert(offset + value_idx, value);
        }
        offset += row_values.len();
    }
    vector.set_len(total_values);
    Ok(())
}

pub fn write_optional_varchar(vector: &mut FlatVector<'_>, row_idx: usize, value: &Option<String>) {
    match value {
        Some(value) => vector.insert(row_idx, value),
        None => vector.set_null(row_idx),
    }
}

fn neighbors(graph: &CloudGraph, node: NodeIndex, direction: TraversalDirection) -> Vec<(NodeIndex, String)> {
    let mut seen = HashSet::new();
    let mut results = Vec::new();

    if matches!(direction, TraversalDirection::Outbound | TraversalDirection::Both) {
        for edge in graph.edges_directed(node, Outgoing) {
            if seen.insert(edge.target()) {
                results.push((edge.target(), edge.weight().relationship_type.clone()));
            }
        }
    }

    if matches!(direction, TraversalDirection::Inbound | TraversalDirection::Both) {
        for edge in graph.edges_directed(node, Incoming) {
            if seen.insert(edge.source()) {
                results.push((edge.source(), edge.weight().relationship_type.clone()));
            }
        }
    }

    results
}

fn type_matches(resource_type: &str, filter_type: Option<&str>) -> bool {
    filter_type.is_none_or(|filter| {
        resource_type
            .to_ascii_lowercase()
            .contains(&filter.to_ascii_lowercase())
    })
}

fn reconstruct_path(
    loaded: &LoadedGraph,
    parents: &HashMap<NodeIndex, (NodeIndex, String)>,
    start: NodeIndex,
    node: NodeIndex,
) -> (Vec<String>, Vec<String>) {
    let mut path_ids = vec![loaded.graph[node].id.clone()];
    let mut relationship_types = Vec::new();
    let mut current = node;

    while current != start {
        let Some((parent, rel_type)) = parents.get(&current) else {
            break;
        };
        relationship_types.push(rel_type.clone());
        current = *parent;
        path_ids.push(loaded.graph[current].id.clone());
    }

    path_ids.reverse();
    relationship_types.reverse();
    (path_ids, relationship_types)
}

/// Edge cost for `graph_shortest_path(..., weighted := true)`.
///
/// When `weighted` is false, every edge costs 1 (plain hop count). When true,
/// the cost is read from the edge's `properties.weight` (positive integer);
/// any missing, non-numeric, zero, or negative value falls back to 1.
///
/// NOTE: no provider currently emits `weight` properties — this is a forward
/// hook. To wire it up, have the Go-side relationship emitter add a numeric
/// `weight` field to the JSON properties payload (e.g. cross-region links
/// could carry a higher cost than intra-AZ ones).
fn edge_cost(edge: &RelationshipEdge, weighted: bool) -> u64 {
    if !weighted {
        return 1;
    }

    edge.properties
        .get("weight")
        .and_then(|value| value.as_u64().or_else(|| value.as_i64().and_then(|v| u64::try_from(v).ok())))
        .filter(|value| *value > 0)
        .unwrap_or(1)
}

#[allow(dead_code)]
fn _resource(_resource: &ResourceNode) {}

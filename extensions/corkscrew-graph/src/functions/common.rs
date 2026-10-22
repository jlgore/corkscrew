use duckdb::core::{FlatVector, Inserter, ListVector};
use duckdb::Connection;
use petgraph::algo::astar;
use petgraph::stable_graph::{EdgeIndex, NodeIndex};
use petgraph::visit::EdgeRef;
use petgraph::Direction::{Incoming, Outgoing};
use std::collections::hash_map::Entry;
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
    let end = start.saturating_add(capacity).min(total);
    // Callers index `rows[start..end]` and write `end - start` rows into a vector of
    // `capacity` slots, so both bounds are checked here rather than at every call site.
    assert!(start < end, "chunk is not empty");
    assert!(end <= total, "chunk ends within the rows");
    assert!(end - start <= capacity, "chunk fits in the output vector");
    Some((start, end))
}

/// DuckDB's per-call vector capacity (typically 2048). Read at runtime so it
/// stays correct if DuckDB is built with a non-default vector size.
pub fn chunk_capacity() -> usize {
    // SAFETY: `duckdb_vector_size` takes no arguments and only reads a
    // compile-time constant of the loaded DuckDB library.
    unsafe { duckdb::ffi::duckdb_vector_size() as usize }
}

use crate::functions::params::{hops_to_i32, HOPS_MAX};
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

/// BFS parent links: each reached node maps to the node and edge it was first reached
/// by, or `None` for the traversal's start.
type Parents = HashMap<NodeIndex, Option<(NodeIndex, EdgeIndex)>>;

/// Matches the core's read-only query cap (one million rows); BFS emits at most one row
/// per node, so only very wide traversals of the largest graphs reach it.
pub const TRAVERSE_ROWS_MAX: usize = 1_000_000;

pub fn load_graph_for_path(
    db_path: &str,
) -> Result<std::sync::Arc<LoadedGraph>, Box<dyn std::error::Error>> {
    let conn = Connection::open(db_path)?;
    Ok(crate::graph::cache::get_or_load(&conn, db_path)?)
}

/// Breadth-first traversal from `source_id`, one row per reached node that
/// matches `filter_type`. Returns an error rather than a silently truncated
/// result once more than `rows_max` rows would be emitted.
pub fn collect_traverse_rows(
    loaded: &LoadedGraph,
    source_id: &str,
    max_hops: usize,
    direction: TraversalDirection,
    filter_type: Option<&str>,
    rows_max: usize,
) -> Result<Vec<TraverseRow>, Box<dyn std::error::Error>> {
    assert!(rows_max > 0);
    let Some(start) = loaded.node_map.get(source_id).copied() else {
        return Ok(Vec::new());
    };
    let max_hops = clamp_hops(loaded, max_hops);
    let filter_type = filter_type.filter(|value| !value.is_empty());

    let mut rows = Vec::new();
    // Every reached node, with the parent and edge it was reached by (`None` for the
    // start), so it doubles as the visited set. Keyed by node rather than an array
    // over the whole graph: memory follows the nodes this call visits, so a
    // one-hop query of a ten-million-node graph stays small.
    let mut parents: Parents = HashMap::from([(start, None)]);
    let mut queue = VecDeque::from([(start, 0usize)]);

    while let Some((node, hops)) = queue.pop_front() {
        assert!(hops <= max_hops);
        if hops > 0 {
            let resource = &loaded.graph[node];
            if type_matches(&resource.resource_type, filter_type) {
                if rows.len() == rows_max {
                    return Err(format!(
                        "graph_traverse result exceeds {rows_max} rows; \
                         lower max_hops or set filter_type"
                    )
                    .into());
                }
                rows.push(traverse_row(loaded, &parents, start, node, hops));
            }
        }

        if hops == max_hops {
            continue;
        }
        for_each_neighbor(&loaded.graph, node, direction, |neighbor, edge| {
            if let Entry::Vacant(entry) = parents.entry(neighbor) {
                entry.insert(Some((node, edge)));
                queue.push_back((neighbor, hops + 1));
            }
        });
    }

    assert!(rows.len() <= rows_max);
    assert!(parents.len() <= loaded.graph.node_count());
    Ok(rows)
}

fn traverse_row(
    loaded: &LoadedGraph,
    parents: &Parents,
    start: NodeIndex,
    node: NodeIndex,
    hops: usize,
) -> TraverseRow {
    let resource = &loaded.graph[node];
    let (path_ids, relationship_types) = reconstruct_path(loaded, parents, start, node, hops);
    TraverseRow {
        node_id: resource.id.clone(),
        node_type: resource.resource_type.clone(),
        node_name: resource.name.clone(),
        region: resource.region.clone(),
        account_id: resource.account_id.clone(),
        provider: resource.provider.clone(),
        hop_count: hops_to_i32(hops),
        path_ids,
        relationship_types,
    }
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

    let total_hops = i32::try_from(path_nodes.len().saturating_sub(1)).unwrap_or(i32::MAX);
    path_nodes
        .iter()
        .enumerate()
        .map(|(index, node)| {
            let resource = &loaded.graph[*node];
            let incoming_rel_type = if index == 0 {
                None
            } else {
                cheapest_edge(&loaded.graph, path_nodes[index - 1], *node, weighted)
                    .map(|edge| loaded.graph[edge].relationship_type.clone())
            };

            PathRow {
                hop_number: i32::try_from(index).unwrap_or(i32::MAX),
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

/// The edge A* would have taken from `source` to `target`: the cheapest of
/// any parallel edges, first in adjacency order on ties. `find_edge` returns
/// an arbitrary parallel edge, which can name the wrong relationship type.
fn cheapest_edge(
    graph: &CloudGraph,
    source: NodeIndex,
    target: NodeIndex,
    weighted: bool,
) -> Option<EdgeIndex> {
    graph
        .edges_connecting(source, target)
        .min_by_key(|edge| edge_cost(edge.weight(), weighted))
        .map(|edge| edge.id())
}

pub fn collect_reachable_row(
    loaded: &LoadedGraph,
    source_id: &str,
    target_type: &str,
    max_hops: usize,
) -> ReachableRow {
    let Some(start) = loaded.node_map.get(source_id).copied() else {
        return ReachableRow {
            is_reachable: false,
            match_count: 0,
            closest_hop: None,
            example_id: None,
        };
    };
    let max_hops = clamp_hops(loaded, max_hops);
    let filter = (!target_type.is_empty()).then_some(target_type);

    // Focused BFS — no parent tracking, no path reconstruction. BFS visits in
    // non-decreasing hop order so the first match is the closest; later matches
    // only increment match_count.
    let mut match_count: i64 = 0;
    let mut closest_hop: Option<i32> = None;
    let mut example_id: Option<String> = None;
    // Hashed, not a bitset over the whole graph: memory follows the visited nodes.
    let mut visited = HashSet::from([start]);
    let mut queue = VecDeque::from([(start, 0usize)]);

    while let Some((node, hops)) = queue.pop_front() {
        assert!(hops <= max_hops);
        if hops > 0 {
            let resource = &loaded.graph[node];
            if type_matches(&resource.resource_type, filter) {
                match_count += 1;
                if closest_hop.is_none() {
                    closest_hop = Some(hops_to_i32(hops));
                    example_id = Some(resource.id.clone());
                }
            }
        }

        if hops == max_hops {
            continue;
        }
        for_each_neighbor(
            &loaded.graph,
            node,
            TraversalDirection::Outbound,
            |neighbor, _| {
                if visited.insert(neighbor) {
                    queue.push_back((neighbor, hops + 1));
                }
            },
        );
    }

    assert_eq!(closest_hop.is_some(), match_count > 0);
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
    let max_hops = clamp_hops(loaded, max_hops);

    // Focused BFS — no parent tracking, no path reconstruction. Group inline
    // by resource_type.
    let mut grouped: BTreeMap<String, BlastRow> = BTreeMap::new();
    // Hashed, not a bitset over the whole graph: memory follows the visited nodes.
    let mut visited = HashSet::from([start]);
    let mut queue = VecDeque::from([(start, 0usize)]);

    while let Some((node, hops)) = queue.pop_front() {
        assert!(hops <= max_hops);
        if hops > 0 {
            let resource = &loaded.graph[node];
            let entry = grouped
                .entry(resource.resource_type.clone())
                .or_insert_with(|| BlastRow {
                    resource_type: resource.resource_type.clone(),
                    reachable_count: 0,
                    max_hop_distance: 0,
                    sample_ids: Vec::new(),
                });
            entry.reachable_count += 1;
            entry.max_hop_distance = entry.max_hop_distance.max(hops_to_i32(hops));
            if entry.sample_ids.len() < 5 {
                entry.sample_ids.push(resource.id.clone());
            }
        }

        if hops == max_hops {
            continue;
        }
        for_each_neighbor(
            &loaded.graph,
            node,
            TraversalDirection::Outbound,
            |neighbor, _| {
                if visited.insert(neighbor) {
                    queue.push_back((neighbor, hops + 1));
                }
            },
        );
    }

    grouped.into_values().collect()
}

pub fn write_varchar_list_column(
    vector: &mut ListVector<'_>,
    values: &[Vec<String>],
) -> Result<(), Box<dyn std::error::Error>> {
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
    assert!(
        offset == total_values,
        "list entries cover the child vector exactly"
    );
    vector.set_len(total_values);
    Ok(())
}

pub fn write_optional_varchar(vector: &mut FlatVector<'_>, row_idx: usize, value: &Option<String>) {
    match value {
        Some(value) => vector.insert(row_idx, value),
        None => vector.set_null(row_idx),
    }
}

/// Bounds a BFS depth for every caller, not just SQL (bind already rejects
/// values past `HOPS_MAX`). No shortest path is longer than the node count, so
/// hops past it only cost loop iterations that can never find anything new.
fn clamp_hops(loaded: &LoadedGraph, max_hops: usize) -> usize {
    let clamped = max_hops.min(HOPS_MAX).min(loaded.graph.node_count());
    assert!(clamped <= HOPS_MAX);
    clamped
}

/// Calls `visit(neighbor, edge)` for every edge of `node` in `direction`:
/// outgoing edges first, then incoming. Neighbors reached by parallel edges are
/// reported once per edge; the caller's visited set keeps the first, so no
/// neighbor list is collected per node.
fn for_each_neighbor(
    graph: &CloudGraph,
    node: NodeIndex,
    direction: TraversalDirection,
    mut visit: impl FnMut(NodeIndex, EdgeIndex),
) {
    if matches!(
        direction,
        TraversalDirection::Outbound | TraversalDirection::Both
    ) {
        for edge in graph.edges_directed(node, Outgoing) {
            visit(edge.target(), edge.id());
        }
    }

    if matches!(
        direction,
        TraversalDirection::Inbound | TraversalDirection::Both
    ) {
        for edge in graph.edges_directed(node, Incoming) {
            visit(edge.source(), edge.id());
        }
    }
}

/// Case-insensitive ASCII substring test that allocates nothing: it compares
/// byte windows with `eq_ignore_ascii_case` instead of lowercasing both strings
/// for every node. An empty `needle` matches everything, like `str::contains`.
pub fn contains_ignore_ascii_case(haystack: &str, needle: &str) -> bool {
    if needle.is_empty() {
        return true;
    }
    if needle.len() > haystack.len() {
        return false;
    }
    haystack
        .as_bytes()
        .windows(needle.len())
        .any(|window| window.eq_ignore_ascii_case(needle.as_bytes()))
}

fn type_matches(resource_type: &str, filter_type: Option<&str>) -> bool {
    filter_type.is_none_or(|filter| contains_ignore_ascii_case(resource_type, filter))
}

/// Walks `parents` from `node` back to `start`. A node `hops` away has exactly
/// `hops` ancestors, which bounds the walk.
fn reconstruct_path(
    loaded: &LoadedGraph,
    parents: &Parents,
    start: NodeIndex,
    node: NodeIndex,
    hops: usize,
) -> (Vec<String>, Vec<String>) {
    let mut path_ids = Vec::with_capacity(hops + 1);
    let mut relationship_types = Vec::with_capacity(hops);
    path_ids.push(loaded.graph[node].id.clone());
    let mut current = node;

    for _ in 0..hops {
        let (parent, edge) = parents[&current].expect("every reached node but start has a parent");
        relationship_types.push(loaded.graph[edge].relationship_type.clone());
        current = parent;
        path_ids.push(loaded.graph[current].id.clone());
    }
    assert_eq!(current, start);
    assert_eq!(path_ids.len(), relationship_types.len() + 1);

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
        .and_then(|value| {
            value
                .as_u64()
                .or_else(|| value.as_i64().and_then(|v| u64::try_from(v).ok()))
        })
        .filter(|value| *value > 0)
        .unwrap_or(1)
}

#[allow(dead_code)]
fn _resource(_resource: &ResourceNode) {}

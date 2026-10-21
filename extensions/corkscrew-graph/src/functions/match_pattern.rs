use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::Result;
use petgraph::stable_graph::NodeIndex;
use std::cell::Cell;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, contains_ignore_ascii_case, next_chunk};
use crate::functions::params::{bind_data, guard, param_varchar, BoxError};
use crate::graph::loader::{CloudGraph, ResourceNode, NODE_COUNT_MAX};
use crate::graph::vf2_impl::StableGraphRef;
use crate::patterns::{load_pattern, PatternGraph, PatternNode, PATTERN_NODES_MAX};

/// Distinct matches returned per call; kept at the historical 256 so existing callers
/// see the same result size, now with an explicit `truncated` flag.
pub const MATCHES_MAX: usize = 256;

/// `node_eq` calls allowed per search: about 10 per node at `NODE_COUNT_MAX`, which is
/// a few seconds of substring compares, and enough for one full candidate scan.
pub const MATCH_STEPS_MAX: u64 = 100_000_000;
const _: () = assert!(MATCH_STEPS_MAX >= NODE_COUNT_MAX as u64);

#[derive(Clone, Copy, Debug)]
pub struct MatchLimits {
    pub matches_max: usize,
    pub steps_max: u64,
}

impl MatchLimits {
    pub const DEFAULT: MatchLimits = MatchLimits {
        matches_max: MATCHES_MAX,
        steps_max: MATCH_STEPS_MAX,
    };
}

pub struct MatchRows {
    pub rows: Vec<MatchPatternRow>,
    /// True when the cap was reached and the search either found one more match or
    /// ran out of steps looking for it.
    pub truncated: bool,
}

#[repr(C)]
pub struct MatchPatternBindData {
    pub db_path: String,
    pub pattern_spec: String,
}

#[derive(Clone, Debug)]
pub struct MatchPatternRow {
    pub match_id: i32,
    pub pattern_node: String,
    pub resource_id: String,
    pub resource_type: String,
    pub resource_name: String,
    pub region: String,
    pub account_id: String,
}

pub struct MatchPatternInitData {
    cursor: AtomicUsize,
    rows: Vec<MatchPatternRow>,
    /// True when more than `MATCHES_MAX` matches exist; emitted on every row.
    truncated: bool,
}

pub struct GraphMatchPatternVTab;

impl VTab for GraphMatchPatternVTab {
    type InitData = MatchPatternInitData;
    type BindData = MatchPatternBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, BoxError> {
        guard("GraphMatchPatternVTab::bind", || {
            bind.add_result_column("match_id", LogicalTypeHandle::from(LogicalTypeId::Integer));
            bind.add_result_column(
                "pattern_node",
                LogicalTypeHandle::from(LogicalTypeId::Varchar),
            );
            bind.add_result_column(
                "resource_id",
                LogicalTypeHandle::from(LogicalTypeId::Varchar),
            );
            bind.add_result_column(
                "resource_type",
                LogicalTypeHandle::from(LogicalTypeId::Varchar),
            );
            bind.add_result_column(
                "resource_name",
                LogicalTypeHandle::from(LogicalTypeId::Varchar),
            );
            bind.add_result_column("region", LogicalTypeHandle::from(LogicalTypeId::Varchar));
            bind.add_result_column(
                "account_id",
                LogicalTypeHandle::from(LogicalTypeId::Varchar),
            );
            bind.add_result_column(
                "was_truncated",
                LogicalTypeHandle::from(LogicalTypeId::Boolean),
            );

            Ok(MatchPatternBindData {
                db_path: param_varchar(bind, 0, "db_path")?,
                pattern_spec: param_varchar(bind, 1, "pattern_spec")?,
            })
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, BoxError> {
        guard("GraphMatchPatternVTab::init", || {
            let bind_ref = bind_data::<Self>(init)?;
            let loaded = crate::functions::common::load_graph_for_path(&bind_ref.db_path)?;
            let pattern = load_pattern(&bind_ref.pattern_spec)?;
            let matches = collect_match_rows(&pattern, &loaded.graph, MatchLimits::DEFAULT)?;

            Ok(MatchPatternInitData {
                cursor: AtomicUsize::new(0),
                rows: matches.rows,
                truncated: matches.truncated,
            })
        })
    }

    fn func(func: &TableFunctionInfo<Self>, output: &mut DataChunkHandle) -> Result<(), BoxError> {
        guard("GraphMatchPatternVTab::func", || {
            let init_data = func.get_init_data();
            let Some((start, end)) =
                next_chunk(&init_data.cursor, init_data.rows.len(), chunk_capacity())
            else {
                output.set_len(0);
                return Ok(());
            };

            let slice = &init_data.rows[start..end];
            let mut v0 = output.flat_vector(0);
            let v1 = output.flat_vector(1);
            let v2 = output.flat_vector(2);
            let v3 = output.flat_vector(3);
            let v4 = output.flat_vector(4);
            let v5 = output.flat_vector(5);
            let v6 = output.flat_vector(6);
            let mut v7 = output.flat_vector(7);
            let id_slice = v0.as_mut_slice::<i32>();
            let truncated_slice = v7.as_mut_slice::<bool>();

            for (index, row) in slice.iter().enumerate() {
                id_slice[index] = row.match_id;
                v1.insert(index, &row.pattern_node);
                v2.insert(index, &row.resource_id);
                v3.insert(index, &row.resource_type);
                v4.insert(index, &row.resource_name);
                v5.insert(index, &row.region);
                v6.insert(index, &row.account_id);
                truncated_slice[index] = init_data.truncated;
            }

            output.set_len(slice.len());
            Ok(())
        })
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        ])
    }
}

pub fn collect_match_rows(
    pattern: &PatternGraph,
    data_graph: &CloudGraph,
    limits: MatchLimits,
) -> std::result::Result<MatchRows, BoxError> {
    assert!(limits.matches_max > 0);
    assert!(
        limits.matches_max <= i32::MAX as usize,
        "match_id is a SQL INTEGER"
    );
    assert!(limits.steps_max > 0);
    assert!(pattern.graph.node_count() <= PATTERN_NODES_MAX);

    if pattern.graph.node_count() > data_graph.node_count()
        || pattern.graph.edge_count() > data_graph.edge_count()
    {
        return Ok(MatchRows {
            rows: Vec::new(),
            truncated: false,
        });
    }

    // Every `node_eq` call is one step. Past the budget it answers false, which
    // prunes every remaining candidate, so VF2 unwinds in at most
    // (pattern nodes x data nodes) further calls instead of finishing its search.
    let steps = Cell::new(0u64);
    let node_eq = |pattern_node: &PatternNode, data_node: &ResourceNode| {
        let step = steps.get().saturating_add(1);
        steps.set(step);
        step <= limits.steps_max && node_matches(pattern_node, data_node)
    };
    let data_view = StableGraphRef(data_graph);
    let isomorphisms = vf2::subgraph_isomorphisms(&pattern.graph, &data_view)
        .node_eq(node_eq)
        .edge_eq(edge_matches)
        .iter();

    let mut rows = Vec::new();
    let mut match_count: usize = 0;
    let mut truncated = false;
    for mapping in isomorphisms {
        if match_count == limits.matches_max {
            // A match past the cap exists, so the result is incomplete.
            truncated = true;
            break;
        }
        push_mapping_rows(pattern, data_graph, match_count, &mapping, &mut rows);
        match_count += 1;
    }

    let budget_exhausted = steps.get() > limits.steps_max;
    if budget_exhausted && match_count == limits.matches_max {
        // Every row the caller can receive was collected; only the probe for one
        // more ran out, so report "possibly more" instead of discarding the rows.
        truncated = true;
    } else if budget_exhausted {
        return Err(format!(
            "graph_match_pattern exceeded its step budget of {} node comparisons; \
             add or narrow type_filter on more pattern nodes",
            limits.steps_max
        )
        .into());
    }
    assert!(match_count <= limits.matches_max);
    if truncated {
        assert_eq!(match_count, limits.matches_max);
    }
    assert_eq!(rows.len(), match_count * pattern.graph.node_count());
    Ok(MatchRows { rows, truncated })
}

fn push_mapping_rows(
    pattern: &PatternGraph,
    data_graph: &CloudGraph,
    match_index: usize,
    mapping: &[usize],
    rows: &mut Vec<MatchPatternRow>,
) {
    assert_eq!(mapping.len(), pattern.node_order.len());
    let match_id = i32::try_from(match_index).expect("matches_max fits in i32");
    for (pattern_pos, data_pos) in mapping.iter().enumerate() {
        let pattern_node = &pattern.graph[pattern.node_order[pattern_pos]];
        let data_node = &data_graph[NodeIndex::new(*data_pos)];
        rows.push(MatchPatternRow {
            match_id,
            pattern_node: pattern_node.label.clone(),
            resource_id: data_node.id.clone(),
            resource_type: data_node.resource_type.clone(),
            resource_name: data_node.name.clone(),
            region: data_node.region.clone(),
            account_id: data_node.account_id.clone(),
        });
    }
}

fn node_matches(pattern_node: &PatternNode, data_node: &ResourceNode) -> bool {
    pattern_node
        .type_filter
        .as_deref()
        .is_none_or(|filter| contains_ignore_ascii_case(&data_node.resource_type, filter))
}

fn edge_matches(
    pattern_edge: &crate::patterns::PatternEdge,
    data_edge: &crate::graph::loader::RelationshipEdge,
) -> bool {
    pattern_edge
        .rel_filter
        .as_ref()
        .is_none_or(|filter| data_edge.relationship_type.eq_ignore_ascii_case(filter))
}

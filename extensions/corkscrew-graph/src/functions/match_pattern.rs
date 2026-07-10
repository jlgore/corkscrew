use duckdb::core::{DataChunkHandle, Inserter, LogicalTypeHandle, LogicalTypeId};
use duckdb::vtab::{BindInfo, InitInfo, TableFunctionInfo, VTab};
use duckdb::Result;
use petgraph::stable_graph::NodeIndex;
use std::sync::atomic::AtomicUsize;

use crate::functions::common::{chunk_capacity, next_chunk};
use crate::graph::loader::{CloudGraph, ResourceNode};
use crate::graph::vf2_impl::StableGraphRef;
use crate::patterns::{load_pattern, PatternGraph};

const MAX_MATCHES: usize = 256;

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
}

pub struct GraphMatchPatternVTab;

impl VTab for GraphMatchPatternVTab {
    type InitData = MatchPatternInitData;
    type BindData = MatchPatternBindData;

    fn bind(bind: &BindInfo) -> Result<Self::BindData, Box<dyn std::error::Error>> {
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

        Ok(MatchPatternBindData {
            db_path: bind.get_parameter(0).to_string(),
            pattern_spec: bind.get_parameter(1).to_string(),
        })
    }

    fn init(init: &InitInfo) -> Result<Self::InitData, Box<dyn std::error::Error>> {
        let bind_ref = unsafe {
            init.get_bind_data::<MatchPatternBindData>()
                .as_ref()
                .unwrap()
        };
        let loaded = crate::functions::common::load_graph_for_path(&bind_ref.db_path)?;
        let pattern = load_pattern(&bind_ref.pattern_spec)?;
        let rows = collect_match_rows(&pattern, &loaded.graph, MAX_MATCHES);

        Ok(MatchPatternInitData {
            cursor: AtomicUsize::new(0),
            rows,
        })
    }

    fn func(
        func: &TableFunctionInfo<Self>,
        output: &mut DataChunkHandle,
    ) -> Result<(), Box<dyn std::error::Error>> {
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
        let id_slice = v0.as_mut_slice::<i32>();

        for (index, row) in slice.iter().enumerate() {
            id_slice[index] = row.match_id;
            v1.insert(index, &row.pattern_node);
            v2.insert(index, &row.resource_id);
            v3.insert(index, &row.resource_type);
            v4.insert(index, &row.resource_name);
            v5.insert(index, &row.region);
            v6.insert(index, &row.account_id);
        }

        output.set_len(slice.len());
        Ok(())
    }

    fn parameters() -> Option<Vec<LogicalTypeHandle>> {
        Some(vec![
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
            LogicalTypeHandle::from(LogicalTypeId::Varchar),
        ])
    }
}

fn collect_match_rows(
    pattern: &PatternGraph,
    data_graph: &CloudGraph,
    max_matches: usize,
) -> Vec<MatchPatternRow> {
    if pattern.graph.node_count() > data_graph.node_count()
        || pattern.graph.edge_count() > data_graph.edge_count()
    {
        return Vec::new();
    }

    // Cap at max_matches. Callers detect truncation by checking whether
    // COUNT(DISTINCT match_id) equals MAX_MATCHES — see README.
    let data_view = StableGraphRef(data_graph);
    let isomorphisms = vf2::subgraph_isomorphisms(&pattern.graph, &data_view)
        .node_eq(node_matches)
        .edge_eq(edge_matches)
        .iter()
        .take(max_matches);

    let mut rows = Vec::new();
    for (match_id, mapping) in isomorphisms.enumerate() {
        for (pattern_pos, data_pos) in mapping.iter().enumerate() {
            let pattern_idx = pattern.node_order[pattern_pos];
            let pattern_node = &pattern.graph[pattern_idx];
            let data_node = &data_graph[NodeIndex::new(*data_pos)];
            rows.push(MatchPatternRow {
                match_id: match_id as i32,
                pattern_node: pattern_node.label.clone(),
                resource_id: data_node.id.clone(),
                resource_type: data_node.resource_type.clone(),
                resource_name: data_node.name.clone(),
                region: data_node.region.clone(),
                account_id: data_node.account_id.clone(),
            });
        }
    }
    rows
}

fn node_matches(pattern_node: &crate::patterns::PatternNode, data_node: &ResourceNode) -> bool {
    pattern_node.type_filter.as_ref().map_or(true, |filter| {
        data_node
            .resource_type
            .to_ascii_lowercase()
            .contains(&filter.to_ascii_lowercase())
    })
}

fn edge_matches(
    pattern_edge: &crate::patterns::PatternEdge,
    data_edge: &crate::graph::loader::RelationshipEdge,
) -> bool {
    pattern_edge.rel_filter.as_ref().map_or(true, |filter| {
        data_edge.relationship_type.eq_ignore_ascii_case(filter)
    })
}

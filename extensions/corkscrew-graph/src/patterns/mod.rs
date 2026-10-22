use petgraph::graph::DiGraph;
use petgraph::graph::NodeIndex;
use serde::Deserialize;
use std::collections::HashMap;

pub mod builtin;

/// VF2 explores up to (data nodes)^(pattern nodes) partial mappings, so the pattern size
/// is the exponent of the search; the largest builtin has 4 nodes, 8 leaves headroom.
pub const PATTERN_NODES_MAX: usize = 8;
/// A directed pattern without parallel edges has at most n(n-1) edges.
pub const PATTERN_EDGES_MAX: usize = PATTERN_NODES_MAX * (PATTERN_NODES_MAX - 1);

#[derive(Clone, Debug, Deserialize)]
pub struct PatternDefinition {
    pub name: String,
    #[serde(default)]
    pub description: String,
    pub nodes: Vec<PatternNodeDefinition>,
    pub edges: Vec<PatternEdgeDefinition>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PatternNodeDefinition {
    pub label: String,
    #[serde(default)]
    pub type_filter: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PatternEdgeDefinition {
    pub from: String,
    pub to: String,
    #[serde(default)]
    pub rel_filter: Option<String>,
}

#[derive(Clone, Debug)]
pub struct PatternNode {
    pub label: String,
    pub type_filter: Option<String>,
}

#[derive(Clone, Debug)]
pub struct PatternEdge {
    pub rel_filter: Option<String>,
}

#[derive(Clone, Debug)]
pub struct PatternGraph {
    pub graph: DiGraph<PatternNode, PatternEdge>,
    pub node_order: Vec<NodeIndex>,
}

impl PatternGraph {
    pub fn from_definition(
        definition: PatternDefinition,
    ) -> Result<Self, Box<dyn std::error::Error>> {
        validate_definition(&definition)?;
        let mut graph = DiGraph::<PatternNode, PatternEdge>::new();
        let mut label_map = HashMap::new();
        let mut node_order = Vec::with_capacity(definition.nodes.len());

        for node in definition.nodes {
            let index = graph.add_node(PatternNode {
                label: node.label.clone(),
                type_filter: node.type_filter,
            });
            label_map.insert(node.label, index);
            node_order.push(index);
        }

        for edge in definition.edges {
            let from = *label_map
                .get(&edge.from)
                .ok_or_else(|| format!("unknown pattern node '{}'", edge.from))?;
            let to = *label_map
                .get(&edge.to)
                .ok_or_else(|| format!("unknown pattern node '{}'", edge.to))?;
            graph.add_edge(
                from,
                to,
                PatternEdge {
                    rel_filter: edge.rel_filter,
                },
            );
        }

        assert!(graph.node_count() <= PATTERN_NODES_MAX);
        assert_eq!(node_order.len(), graph.node_count());
        Ok(Self { graph, node_order })
    }
}

/// Rejects patterns whose VF2 search cannot be bounded: too many nodes or edges, or no
/// filter at all, which makes every data node a candidate for every pattern node and
/// every edge a candidate for every pattern edge. One non-empty node `type_filter` or
/// edge `rel_filter` prunes the search; the step budget bounds what remains.
fn validate_definition(definition: &PatternDefinition) -> Result<(), Box<dyn std::error::Error>> {
    let name = &definition.name;
    let node_count = definition.nodes.len();
    if node_count == 0 {
        return Err(format!("pattern '{name}' must have at least one node").into());
    }
    if node_count > PATTERN_NODES_MAX {
        return Err(format!(
            "pattern '{name}' has {node_count} nodes; patterns may have at most \
             {PATTERN_NODES_MAX} nodes"
        )
        .into());
    }
    let edge_count = definition.edges.len();
    if edge_count > PATTERN_EDGES_MAX {
        return Err(format!(
            "pattern '{name}' has {edge_count} edges; patterns may have at most \
             {PATTERN_EDGES_MAX} edges"
        )
        .into());
    }
    let node_anchored = definition.nodes.iter().any(|node| {
        node.type_filter
            .as_deref()
            .is_some_and(|filter| !filter.is_empty())
    });
    let edge_anchored = definition.edges.iter().any(|edge| {
        edge.rel_filter
            .as_deref()
            .is_some_and(|filter| !filter.is_empty())
    });
    if !node_anchored && !edge_anchored {
        return Err(format!(
            "pattern '{name}' needs at least one node with a non-empty type_filter \
             or one edge with a non-empty rel_filter"
        )
        .into());
    }
    Ok(())
}

pub fn load_pattern(pattern_spec: &str) -> Result<PatternGraph, Box<dyn std::error::Error>> {
    if let Some(definition) = builtin::lookup(pattern_spec) {
        return PatternGraph::from_definition(definition);
    }

    let definition: PatternDefinition = serde_json::from_str(pattern_spec)?;
    PatternGraph::from_definition(definition)
}

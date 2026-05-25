use petgraph::graph::DiGraph;
use petgraph::graph::NodeIndex;
use serde::Deserialize;
use std::collections::HashMap;

pub mod builtin;

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
    pub name: String,
    pub description: String,
    pub graph: DiGraph<PatternNode, PatternEdge>,
    pub node_order: Vec<NodeIndex>,
}

impl PatternGraph {
    pub fn from_definition(definition: PatternDefinition) -> Result<Self, Box<dyn std::error::Error>> {
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

        Ok(Self {
            name: definition.name,
            description: definition.description,
            graph,
            node_order,
        })
    }
}

pub fn load_pattern(pattern_spec: &str) -> Result<PatternGraph, Box<dyn std::error::Error>> {
    if let Some(definition) = builtin::lookup(pattern_spec) {
        return PatternGraph::from_definition(definition);
    }

    let definition: PatternDefinition = serde_json::from_str(pattern_spec)?;
    PatternGraph::from_definition(definition)
}

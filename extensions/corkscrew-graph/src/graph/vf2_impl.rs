// `vf2::Graph` adapter for `petgraph::stable_graph::StableGraph`.
//
// vf2 1.0 only ships an impl for `petgraph::Graph`; without an adapter we'd
// have to clone the entire `StableGraph` into a `Graph` on every cache load
// just so `vf2::subgraph_isomorphisms` could traverse it. Rust's orphan rule
// blocks a direct blanket impl on the foreign type, so we wrap by reference.
// Mirrors the upstream impl at vf2-1.0.1/src/petgraph.rs — same trait surface,
// same NodeIndex semantics (usize → `petgraph::stable_graph::NodeIndex::<Ix>`).
use petgraph::adj::IndexType;
use petgraph::stable_graph::StableGraph;
use petgraph::EdgeType;
use std::fmt::Debug;
use vf2::{Direction, Graph, NodeIndex};

/// Zero-cost wrapper that adapts a borrowed `StableGraph` to `vf2::Graph`.
pub struct StableGraphRef<'a, N, E, Ty, Ix>(pub &'a StableGraph<N, E, Ty, Ix>);

impl<'a, N, E, Ty, Ix> Graph for StableGraphRef<'a, N, E, Ty, Ix>
where
    N: Debug,
    E: Debug,
    Ty: EdgeType,
    Ix: IndexType,
{
    type NodeLabel = N;
    type EdgeLabel = E;

    #[inline]
    fn is_directed(&self) -> bool {
        self.0.is_directed()
    }

    #[inline]
    fn node_count(&self) -> usize {
        self.0.node_count()
    }

    #[inline]
    fn node_label(&self, index: NodeIndex) -> Option<&Self::NodeLabel> {
        self.0.node_weight(petgraph::stable_graph::NodeIndex::<Ix>::new(index))
    }

    #[inline]
    fn neighbors(&self, node: NodeIndex, direction: Direction) -> impl Iterator<Item = NodeIndex> {
        self.0
            .neighbors_directed(
                petgraph::stable_graph::NodeIndex::<Ix>::new(node),
                match direction {
                    Direction::Outgoing => petgraph::Direction::Outgoing,
                    Direction::Incoming => petgraph::Direction::Incoming,
                },
            )
            .map(|neighbor| neighbor.index())
    }

    #[inline]
    fn contains_edge(&self, source: NodeIndex, target: NodeIndex) -> bool {
        self.0.contains_edge(
            petgraph::stable_graph::NodeIndex::<Ix>::new(source),
            petgraph::stable_graph::NodeIndex::<Ix>::new(target),
        )
    }

    #[inline]
    fn edge_label(&self, source: NodeIndex, target: NodeIndex) -> Option<&Self::EdgeLabel> {
        self.0
            .find_edge(
                petgraph::stable_graph::NodeIndex::<Ix>::new(source),
                petgraph::stable_graph::NodeIndex::<Ix>::new(target),
            )
            .and_then(|index| self.0.edge_weight(index))
    }
}

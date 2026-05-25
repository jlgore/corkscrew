// Re-compile the pure-Rust modules from the sibling extension crate
// against a `bundled` duckdb. We do NOT pull in `corkscrew-graph` as a
// path dependency, because that would unify the duckdb feature set
// (loadable-extension vs bundled) and break things.

pub mod graph {
    pub mod schema {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/graph/schema.rs"));
    }

    pub mod loader {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/graph/loader.rs"));
    }

    pub mod cache {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/graph/cache.rs"));
    }

    pub mod vf2_impl {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/graph/vf2_impl.rs"));
    }
}

pub mod patterns {
    pub mod builtin {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/patterns/builtin.rs"));
    }

    pub mod defs {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/patterns/mod.rs"));
    }

    pub use defs::*;
}

pub mod functions {
    pub mod common {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/common.rs"));
    }

    pub mod info {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/info.rs"));
    }

    pub mod list_patterns {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/list_patterns.rs"));
    }

    pub mod traverse {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/traverse.rs"));
    }

    pub mod blast {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/blast.rs"));
    }

    pub mod paths {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/paths.rs"));
    }

    pub mod reachable {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/reachable.rs"));
    }

    pub mod match_pattern {
        include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/functions/match_pattern.rs"));
    }
}

pub mod sql_macros {
    include!(concat!(env!("CARGO_MANIFEST_DIR"), "/../corkscrew-graph/src/sql_macros.rs"));
}
